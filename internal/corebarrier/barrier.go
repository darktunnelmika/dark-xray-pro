// Package corebarrier is copied verbatim into the pinned experimental Xray
// build. It tracks all counter-producing work in the admitted direct TCP path.
// It is not an independent proof for arbitrary Xray configurations.
package corebarrier

import (
	"context"
	"errors"
	"os"
	"sync"
)

const Protocol = "darkpro-final-tcp-v1/"

var ErrBarrier = errors.New("final TCP counter barrier unavailable or conflicting")

type contextKey struct{}
type gate struct {
	mu       sync.Mutex
	closing  bool
	count    int
	next     uint64
	roots    map[uint64]func()
	zero     chan struct{}
	finished chan struct{}
	nonce    string
	values   map[string]int64
}

func newGate() *gate { return &gate{roots: make(map[uint64]func()), zero: make(chan struct{})} }

var processGate = newGate()

// An explicit supervisor option is required. Stock clients and ordinary core
// invocations keep their existing behavior.
func Enabled() bool { return os.Getenv("DARK_PRO_FINAL_TCP_V1") == "1" }

func Enter(ctx context.Context, interrupt func()) (context.Context, func(), bool) {
	if !Enabled() {
		return ctx, func() {}, true
	}
	return processGate.enter(ctx, interrupt)
}

func (g *gate) enter(ctx context.Context, interrupt func()) (context.Context, func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return ctx, func() {}, false
	}
	g.next++
	id := g.next
	g.roots[id] = interrupt
	g.count++
	var once sync.Once
	return context.WithValue(ctx, contextKey{}, g), func() {
		once.Do(func() {
			g.mu.Lock()
			delete(g.roots, id)
			g.releaseLocked()
			g.mu.Unlock()
		})
	}, true
}

// Track is called synchronously BEFORE launching a task. A live tracked parent
// may fork while draining; its children remain part of the same barrier even
// when task.Run returns early on cancellation or the first error.
func Track(ctx context.Context) func() {
	g, _ := ctx.Value(contextKey{}).(*gate)
	if g == nil {
		return func() {}
	}
	g.mu.Lock()
	if g.count == 0 {
		g.mu.Unlock()
		panic("darkpro: work escaped its tracked lifetime")
	}
	g.count++
	g.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); g.releaseLocked(); g.mu.Unlock() }) }
}

func Tracked(ctx context.Context) bool { _, ok := ctx.Value(contextKey{}).(*gate); return ok }

func (g *gate) releaseLocked() {
	g.count--
	if g.closing && g.count == 0 {
		close(g.zero)
	}
}

func Snapshot(ctx context.Context, nonce string, read func() map[string]int64) (map[string]int64, error) {
	if !Enabled() {
		return nil, ErrBarrier
	}
	return processGate.snapshot(ctx, nonce, read)
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
		g.closing, g.nonce = true, nonce
		g.finished = make(chan struct{})
		closers := make([]func(), 0, len(g.roots))
		for _, stop := range g.roots {
			closers = append(closers, stop)
		}
		if g.count == 0 {
			close(g.zero)
		}
		// The drain persists after an RPC timeout. No second request can reopen
		// admission, replace the nonce, or cause a second counter read.
		go func() {
			for _, stop := range closers {
				stop()
			}
			<-g.zero
			values := read()
			g.mu.Lock()
			g.values = clone(values)
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
		return clone(g.values), nil
	}
}

func clone(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
