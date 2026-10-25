// Package corebarrierv2 is copied into the separately pinned inbound Xray
// runtime. Its initial gate is closed. Only the supervisor's zero-receipt
// activation opens admission; finalization permanently closes it and joins
// registered resources, roots, asynchronous descendants and counter I/O.
//
// The proof applies to the audited upstream overlay, not arbitrary Xray builds.
package corebarrierv2

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"time"
)

const (
	Protocol         = "darkpro-inbound-v2/"
	ControlTag       = "dark-pro-usage-api"
	DNSTag           = "dark-pro-inbound-dns-upstream"
	TUNDNSCapability = Protocol + "tun-dns-activation-v1"
)

var ErrBarrier = errors.New("inbound counter barrier is unavailable or conflicting")

type contextKey struct{}
type lifetime struct {
	gate *gate
	id   uint64
	refs int
}

type activationHook struct {
	apply    func(context.Context) error
	rollback func(context.Context) error
}

type gate struct {
	mu             sync.Mutex
	ready          bool
	active         bool
	closing        bool
	activation     string
	activators     map[string]activationHook
	activateDone   chan struct{}
	activateCancel context.CancelFunc
	activateErr    error
	nonce          string
	next           uint64
	count          int
	roots          map[uint64]func()
	resources      map[string]func() error
	changed        chan struct{}
	zero           chan struct{}
	finished       chan struct{}
	values         map[string]int64
	err            error
}

func newGate() *gate {
	return &gate{roots: make(map[uint64]func()), resources: make(map[string]func() error), activators: make(map[string]activationHook), changed: make(chan struct{}), zero: make(chan struct{})}
}

var processGate = newGate()

func Enabled() bool { return os.Getenv("DARK_PRO_INBOUND_V2") == "1" }

// IsControl exempts only the immutable supervisor API tag. Candidate validation
// must reserve this tag and its loopback endpoint; customer tags cannot use it.
func IsControl(tag string) bool { return Enabled() && tag == ControlTag }

// MarkReady is called after core.Instance.Start has successfully started every
// configured feature. Merely accepting a StatsService connection is not ready.
func MarkReady() {
	if Enabled() {
		processGate.mu.Lock()
		processGate.ready = true
		processGate.mu.Unlock()
	}
}

func Health() (ready, active, sealed bool) {
	g := processGate
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ready, g.active && !g.closing, g.finished != nil && g.values != nil && g.err == nil
}

func Activate(nonce string) error {
	return ActivateContext(context.Background(), nonce)
}

// ActivateContext opens admission only after each bounded host-resource hook
// succeeds. The nonce-bound operation survives a lost RPC response. Final can
// cancel it, waits for rollback, and never races resource closure against apply.
func ActivateContext(ctx context.Context, nonce string) error {
	if !Enabled() {
		return ErrBarrier
	}
	return processGate.activateContext(ctx, nonce)
}

func (g *gate) activate(nonce string) error {
	return g.activateContext(context.Background(), nonce)
}

func (g *gate) activateContext(ctx context.Context, nonce string) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrBarrier
	}
	g.mu.Lock()
	if !validNonce(nonce) || !g.ready || g.closing || (g.activation != "" && g.activation != nonce) {
		g.mu.Unlock()
		return ErrBarrier
	}
	if g.activateDone == nil {
		g.activation = nonce
		g.activateDone = make(chan struct{})
		applyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		g.activateCancel = cancel
		tags := make([]string, 0, len(g.activators))
		for tag := range g.activators {
			tags = append(tags, tag)
		}
		sort.Strings(tags)
		hooks := make([]activationHook, 0, len(tags))
		for _, tag := range tags {
			hooks = append(hooks, g.activators[tag])
		}
		go g.runActivation(applyCtx, cancel, hooks)
	}
	done := g.activateDone
	g.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		g.mu.Lock()
		defer g.mu.Unlock()
		if !g.active || g.closing || g.activateErr != nil {
			return ErrBarrier
		}
		return nil
	}
}

func (g *gate) runActivation(ctx context.Context, cancel context.CancelFunc, hooks []activationHook) {
	defer cancel()
	var failure error
	attempted := 0
	for _, hook := range hooks {
		if ctx.Err() != nil {
			failure = ErrBarrier
			break
		}
		attempted++
		if err := hook.apply(ctx); err != nil {
			failure = ErrBarrier
			break
		}
	}
	g.mu.Lock()
	if failure == nil && ctx.Err() == nil && !g.closing {
		g.active = true
		close(g.changed)
		g.changed = make(chan struct{})
		close(g.activateDone)
		g.mu.Unlock()
		return
	}
	g.mu.Unlock()
	// Every attempted hook is rolled back, including one whose apply timed out
	// after an external service may have accepted its mutation.
	rollbackCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	for i := attempted - 1; i >= 0; i-- {
		_ = hooks[i].rollback(rollbackCtx)
	}
	g.mu.Lock()
	g.activateErr = ErrBarrier
	close(g.activateDone)
	g.mu.Unlock()
}

// RegisterActivation is only available during initial construction. Hooks
// must honor their bounded context; the paired resource closer must retry any
// cleanup failure before an immutable final receipt can be produced.
func RegisterActivation(tag string, apply, rollback func(context.Context) error) error {
	if !Enabled() {
		return ErrBarrier
	}
	return processGate.registerActivation(tag, apply, rollback)
}

func (g *gate) registerActivation(tag string, apply, rollback func(context.Context) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if tag == "" || tag == ControlTag || apply == nil || rollback == nil || g.ready || g.closing || g.activation != "" {
		return ErrBarrier
	}
	if _, exists := g.activators[tag]; exists {
		return ErrBarrier
	}
	g.activators[tag] = activationHook{apply: apply, rollback: rollback}
	return nil
}

// WaitForActivation is used only by the owned WireGuard packet reader, which
// must not terminate its receive loop or count packets during preparation.
func WaitForActivation() bool {
	if !Enabled() {
		return true
	}
	g := processGate
	for {
		g.mu.Lock()
		active, closing, changed := g.active, g.closing, g.changed
		g.mu.Unlock()
		if closing || active {
			return active && !closing
		}
		<-changed
	}
}

func Enter(ctx context.Context, tag string, interrupt func()) (context.Context, func(), bool) {
	if !Enabled() || tag == ControlTag {
		return ctx, func() {}, true
	}
	return processGate.enter(ctx, interrupt)
}

func (g *gate) enter(ctx context.Context, interrupt func()) (context.Context, func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ctx == nil || !g.active || g.closing {
		return ctx, func() {}, false
	}
	g.next++
	life := &lifetime{gate: g, id: g.next, refs: 1}
	g.roots[life.id] = interrupt
	g.count++
	return context.WithValue(ctx, contextKey{}, life), release(life), true
}

func release(life *lifetime) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			g := life.gate
			g.mu.Lock()
			defer g.mu.Unlock()
			life.refs--
			g.count--
			if life.refs == 0 {
				delete(g.roots, life.id)
			}
			if g.closing && g.count == 0 {
				close(g.zero)
			}
		})
	}
}

// Track must run before a goroutine is launched. A tracked parent may finish
// first; its interrupt callback remains registered until its final child exits.
// Descendants may finish/fork while draining, but an escaped dead lifetime is a
// programming error and terminates the core instead of producing false proof.
func Track(ctx context.Context) func() {
	life, _ := ctx.Value(contextKey{}).(*lifetime)
	if life == nil {
		return func() {}
	}
	g := life.gate
	g.mu.Lock()
	if life.refs == 0 {
		g.mu.Unlock()
		panic("darkpro inbound: work escaped its tracked lifetime")
	}
	life.refs++
	g.count++
	g.mu.Unlock()
	return release(life)
}

func Tracked(ctx context.Context) bool {
	_, ok := ctx.Value(contextKey{}).(*lifetime)
	return ok
}

func Go(ctx context.Context, run func()) {
	finish := Track(ctx)
	go func() { defer finish(); run() }()
}

// Preserve carries proof ownership through upstream's timeout-only detached
// contexts without changing the normal cancellation semantics of XUDP.
func Preserve(from, to context.Context) context.Context {
	if life, ok := from.Value(contextKey{}).(*lifetime); ok {
		return context.WithValue(to, contextKey{}, life)
	}
	return to
}

// BeginIO fences counter-producing I/O itself, including late calls through
// retained UDP writers and raw splice paths. The release must follow the final
// counter update, not just the underlying read/write system call.
func BeginIO(interrupt func()) (func(), bool) {
	if !Enabled() {
		return func() {}, true
	}
	_, finish, ok := processGate.enter(context.Background(), interrupt)
	return finish, ok
}

// RegisterResource retains each customer handler until finalization. Closing
// the handler stops its actual sockets, storage poller or TUN/netstack device.
// The private control handler remains available for immutable receipt retries.
func RegisterResource(tag string, closeResource func() error) error {
	if !Enabled() || tag == ControlTag {
		return nil
	}
	g := processGate
	g.mu.Lock()
	defer g.mu.Unlock()
	if tag == "" || closeResource == nil || g.closing || g.resources[tag] != nil {
		return ErrBarrier
	}
	g.resources[tag] = closeResource
	return nil
}

func Snapshot(ctx context.Context, nonce string, read func() map[string]int64) (map[string]int64, error) {
	if !Enabled() {
		return nil, ErrBarrier
	}
	return processGate.snapshot(ctx, nonce, read)
}

func (g *gate) snapshot(ctx context.Context, nonce string, read func() map[string]int64) (map[string]int64, error) {
	if ctx == nil || ctx.Err() != nil || !validNonce(nonce) {
		return nil, ErrBarrier
	}
	g.mu.Lock()
	if g.closing && g.nonce != nonce {
		g.mu.Unlock()
		return nil, ErrBarrier
	}
	if !g.closing {
		if read == nil {
			g.mu.Unlock()
			return nil, ErrBarrier
		}
		g.closing, g.active, g.nonce = true, false, nonce
		close(g.changed)
		g.changed = make(chan struct{})
		g.finished = make(chan struct{})
		activationDone, activationCancel := g.activateDone, g.activateCancel
		stops := make([]func(), 0, len(g.roots))
		for _, stop := range g.roots {
			if stop != nil {
				stops = append(stops, stop)
			}
		}
		resources := make([]func() error, 0, len(g.resources))
		for _, stop := range g.resources {
			resources = append(resources, stop)
		}
		if g.count == 0 {
			close(g.zero)
		}
		// The operation survives an RPC timeout. No retry can reopen the gate,
		// replace its nonce, or cause a second counter read.
		go func() {
			if activationCancel != nil {
				activationCancel()
			}
			if activationDone != nil {
				<-activationDone
			}
			var closes sync.WaitGroup
			var errorsMu sync.Mutex
			var closeErr error
			for _, stop := range stops {
				closes.Add(1)
				go func(stop func()) { defer closes.Done(); stop() }(stop)
			}
			for _, stop := range resources {
				closes.Add(1)
				go func(stop func() error) {
					defer closes.Done()
					if err := stop(); err != nil {
						errorsMu.Lock()
						closeErr = ErrBarrier
						errorsMu.Unlock()
					}
				}(stop)
			}
			closes.Wait()
			<-g.zero
			var values map[string]int64
			if closeErr == nil {
				values = clone(read())
			}
			g.mu.Lock()
			g.values, g.err = values, closeErr
			close(g.finished)
			g.mu.Unlock()
		}()
	}
	finished := g.finished
	g.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-finished:
		g.mu.Lock()
		defer g.mu.Unlock()
		return clone(g.values), g.err
	}
}

func validNonce(n string) bool {
	if len(n) != 64 {
		return false
	}
	for _, c := range n {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func clone(in map[string]int64) map[string]int64 {
	if in == nil {
		return nil
	}
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
