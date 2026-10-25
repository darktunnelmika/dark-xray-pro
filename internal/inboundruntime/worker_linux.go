//go:build linux

package inboundruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type BinaryConfig struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Config struct {
	Directory      string
	NodeID         string
	InstallationID string
	Key            []byte
	Native         BinaryConfig
	External       map[string]BinaryConfig
}

type Worker struct {
	mu            sync.Mutex
	cfg           Config
	journal       *privateJournal
	state         journal
	native        *Binary
	external      map[string]*Binary
	process       ownedProcess
	deadline      time.Time
	closed        bool
	persistFailed bool
}

// Only processes created by this worker implement this interface in
// production. It also lets the journal and receipt state machine be exercised
// independently of a particular protocol binary.
type ownedProcess interface {
	Stop()
	Alive() bool
	Renew(time.Time) bool
	Identity() string
	BinarySHAs() map[string]string
	Activate(context.Context, string) error
	Health(context.Context) error
	Counters(context.Context) ([]inboundexec.Counter, error)
	Final(context.Context, string) ([]inboundexec.Counter, error)
}

func Open(cfg Config) (*Worker, error) {
	j, state, e := openJournal(cfg.Directory, cfg.Key, cfg.NodeID, cfg.InstallationID)
	if e != nil {
		return nil, e
	}
	w := &Worker{cfg: cfg, journal: j, state: state, external: map[string]*Binary{}}
	w.native, e = OpenBinary(cfg.Native.Path, cfg.Native.SHA256)
	if e != nil {
		j.close()
		return nil, e
	}
	for engine, desc := range cfg.External {
		if engine != "tuic" && engine != "amneziawg" && engine != "mtproto" {
			w.Close()
			return nil, ErrRuntime
		}
		b, err := OpenBinary(desc.Path, desc.SHA256)
		if err != nil {
			w.Close()
			return nil, err
		}
		w.external[engine] = b
	}
	// A previous process is never adopted. Its incarnation may have produced a
	// final unobserved byte; only a durable sealed report permits recovery.
	if state.Epoch != nil && !state.FinalAccepted && (state.Pending == nil || !state.Pending.Final) {
		// A sealed sample captured while an earlier ordinary receipt was
		// pending is sufficient. Every other lost incarnation remains fenced;
		// absence of a report is never converted into a fictitious zero seal.
		if state.Phase != "sealed_waiting_receipt" {
			w.state.Phase = "uncertain"
		}
		if e = j.save(w.state); e != nil {
			w.Close()
			return nil, e
		}
	}
	return w, nil
}

func zeroCounters(b *inboundexec.Bundle) []inboundexec.Counter {
	out := []inboundexec.Counter{}
	if b != nil {
		for _, p := range b.Principals {
			out = append(out, inboundexec.Counter{PrincipalID: p.ID})
		}
	}
	return out
}

func (w *Worker) Capabilities() Capabilities {
	m := map[string]string{"xray": w.native.SHA256}
	for k, b := range w.external {
		m[k] = b.SHA256
	}
	return Capabilities{Version: 1, Binaries: m}
}

func (w *Worker) save() error {
	if w.persistFailed {
		return ErrPersistence
	}
	if err := w.journal.save(w.state); err != nil {
		// The in-memory state may be newer than the durable journal. Never
		// transmit that speculative report on a later Step, or continue serving
		// traffic after a counter/nonce/receipt durability failure.
		w.persistFailed = true
		if w.process != nil {
			w.process.Stop()
			w.process = nil
		}
		return ErrPersistence
	}
	return nil
}

func (w *Worker) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	if w.process != nil {
		w.process.Stop()
		w.process = nil
	}
	if w.native != nil {
		_ = w.native.Close()
	}
	for _, b := range w.external {
		_ = b.Close()
	}
	w.journal.close()
}

// Run is independent of browser requests. Its own lease timer stops the child
// even if polling, usage delivery, or the controller goroutine stalls.
func (w *Worker) Run(ctx context.Context, x Exchange) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	defer w.Close()
	for {
		if ctx.Err() != nil {
			shutdown, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			w.mu.Lock()
			if !w.closed && !w.persistFailed {
				_ = w.captureFinal(shutdown)
				if w.state.Pending != nil && !w.persistFailed {
					_ = w.sendPending(shutdown, x)
				}
			}
			w.mu.Unlock()
			cancel()
			return
		}
		attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
		_ = w.Step(attempt, x)
		cancel()
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
}

// Step performs one durable reconciliation. All outbound mutations can be
// repeated verbatim after a lost response; a cursor advances only on its exact
// receipt. It is exported to exercise real engines without scheduler sleeps.
func (w *Worker) Step(ctx context.Context, x Exchange) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || x == nil {
		return ErrRuntime
	}
	if w.persistFailed {
		_ = w.observe(ctx, x, "failed", "accounting_tail_uncertain", false)
		return ErrPersistence
	}
	if w.process != nil && !w.process.Alive() && !w.state.FinalAccepted && w.state.Phase != "sealed_waiting_receipt" && (w.state.Pending == nil || !w.state.Pending.Final) {
		if err := w.markUncertain(); err != nil {
			return err
		}
	}
	if w.state.Phase == "uncertain" {
		// A broken ordinary outbox must not postpone the financial hold. Known
		// samples remain replayable, but no missing tail becomes a zero seal.
		_ = w.observe(ctx, x, "failed", "accounting_tail_uncertain", false)
		if w.state.Pending != nil {
			if err := w.sendPending(ctx, x); err != nil {
				return err
			}
		}
		return w.resolveUncertain(ctx, x)
	}
	if w.state.Pending != nil {
		if e := w.sendPending(ctx, x); e != nil {
			w.maybeDrain(ctx)
			return e
		}
	}
	if w.state.Phase == "sealed_waiting_receipt" && w.state.Pending == nil {
		w.state.Sequence++
		r := w.makeReport(w.state.LastCounters, true)
		w.state.Pending = &r
		w.state.Phase = "final_pending"
		if e := w.save(); e != nil {
			return e
		}
		if e := w.sendPending(ctx, x); e != nil {
			return e
		}
	}
	if w.state.FinalAccepted && w.state.Epoch != nil {
		if w.process != nil {
			w.process.Stop()
			w.process = nil
		}
		w.state.Epoch = nil
		w.state.Phase = "idle"
		w.state.Pending = nil
		w.state.Sequence = 0
		w.state.LastCounters = nil
		if e := w.save(); e != nil {
			return e
		}
	}
	if w.process != nil && (w.state.Phase == "prepared" || w.state.Phase == "registered" || w.state.Phase == "zero_pending" || w.state.Phase == "activation_intent") {
		return w.admit(ctx, x)
	}
	if w.process != nil && (w.state.Phase == "draining" || w.state.Phase == "final_pending") {
		return w.drain(ctx, x)
	}
	if w.process != nil && w.state.Phase == "active" {
		if time.Until(w.deadline) < 12*time.Second {
			return w.drain(ctx, x)
		}
		values, e := w.process.Counters(ctx)
		if e != nil {
			_ = w.markUncertain()
			_ = w.observe(ctx, x, "failed", "accounting_tail_uncertain", false)
			return e
		}
		w.state.Sequence++
		r := w.makeReport(values, false)
		w.state.Pending = &r
		w.state.LastCounters = values
		if e = w.save(); e != nil {
			return e
		}
		if e = w.sendPending(ctx, x); e != nil {
			w.maybeDrain(ctx)
			return e
		}
	}
	delivery, e := x.Poll(ctx, PollInput{NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Capabilities: w.Capabilities()})
	if e != nil {
		w.maybeDrain(ctx)
		return e
	}
	if delivery == nil {
		if w.process != nil {
			return w.drain(ctx, x)
		}
		return nil
	}
	if e = VerifyDelivery(w.cfg.Key, *delivery, w.cfg.NodeID, w.cfg.InstallationID, time.Now()); e != nil {
		w.maybeDrain(ctx)
		return e
	}
	if delivery.Bundle.Generation == 0 {
		return ErrProof
	}
	bundle := delivery.Bundle
	if bundle.Generation < w.state.Generation {
		return errors.New("inbound_generation_regressed")
	}
	if w.state.Bundle != nil && bundle.Generation == w.state.Generation && (bundle.ConfigSHA != w.state.Bundle.ConfigSHA || bundle.ManifestSHA != w.state.Bundle.ManifestSHA) {
		return errors.New("inbound_generation_conflict")
	}
	deadline := time.Unix(0, delivery.Lease.Expires)
	if w.process != nil && bundle.Generation == w.state.Generation && w.state.Phase == "active" {
		if !w.process.Renew(deadline) {
			return ErrLease
		}
		w.deadline = deadline
		if e = w.process.Health(ctx); e != nil {
			_ = w.observe(ctx, x, "degraded", "engine_health_failed", false)
			return e
		}
		return w.observe(ctx, x, "active", "", true)
	}
	if bundle.Generation != w.state.Generation && len(bundle.Listeners) > 0 {
		if e = validateGroup(ctx, w.native, w.external, bundle); e != nil {
			return e
		}
	}
	if w.process != nil || w.state.Epoch != nil {
		if e = w.drain(ctx, x); e != nil {
			return e
		}
		return nil
	}
	if len(bundle.Listeners) == 0 && len(bundle.Components) == 0 {
		w.state = journal{Version: 1, NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Generation: bundle.Generation, Bundle: &bundle, Phase: "idle", FinalAccepted: true}
		w.deadline = deadline
		if e = w.save(); e != nil {
			return e
		}
		return w.observe(ctx, x, "stopped", "", false)
	}
	// Keep an old active process untouched until the complete replacement has
	// passed offline validation. No external candidate may silently disappear.
	if e = validateGroup(ctx, w.native, w.external, bundle); e != nil {
		old := w.state.Bundle
		w.state.Bundle = &bundle
		w.deadline = deadline
		_ = w.observe(ctx, x, "failed", "engine_validation_failed", false)
		w.state.Bundle = old
		return e
	}
	w.state = journal{Version: 1, NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Generation: bundle.Generation, Bundle: &bundle, Phase: "preparing"}
	if e = w.save(); e != nil {
		return e
	}
	group, e := startGroup(ctx, w.cfg.Directory, w.native, w.external, bundle, deadline)
	if e != nil {
		_ = w.observe(ctx, x, "failed", "engine_start_failed", false)
		return e
	}
	w.process = group
	w.deadline = deadline
	id, e := randomID(16)
	if e != nil {
		group.Stop()
		w.process = nil
		return e
	}
	nonce, e := randomID(32)
	if e != nil {
		group.Stop()
		w.process = nil
		return e
	}
	w.state.Epoch = &inboundexec.EpochInput{EpochID: id, NodeID: bundle.NodeID, InstallationID: bundle.InstallationID, Generation: bundle.Generation, ConfigSHA: bundle.ConfigSHA, ManifestSHA: bundle.ManifestSHA, ProcessIdentity: group.Identity(), BinarySHAs: group.BinarySHAs()}
	w.state.ActivateNonce = nonce
	w.state.Phase = "prepared"
	if e = w.save(); e != nil {
		group.Stop()
		w.process = nil
		return e
	}
	return w.admit(ctx, x)
}

func (w *Worker) admit(ctx context.Context, x Exchange) error {
	if w.process == nil || w.state.Epoch == nil {
		return ErrRuntime
	}
	if time.Until(w.deadline) < 12*time.Second {
		return w.drain(ctx, x)
	}
	if w.state.Phase == "prepared" {
		receipt, e := x.Register(ctx, *w.state.Epoch)
		if e != nil {
			w.maybeDrain(ctx)
			return e
		}
		if !receipt.Registered || receipt.EpochID != w.state.Epoch.EpochID || receipt.ProcessIdentity != w.state.Epoch.ProcessIdentity {
			return ErrProof
		}
		w.state.Phase = "registered"
		if e = w.save(); e != nil {
			return e
		}
	}
	if w.state.Phase == "registered" {
		values, e := w.process.Counters(ctx)
		if e != nil {
			return e
		}
		for _, v := range values {
			if v.Upload != 0 || v.Download != 0 {
				return ErrProof
			}
		}
		w.state.Sequence = 1
		report := w.makeReport(values, false)
		w.state.Pending = &report
		w.state.LastCounters = values
		w.state.Phase = "zero_pending"
		if e = w.save(); e != nil {
			return e
		}
	}
	if e := w.sendPending(ctx, x); e != nil {
		w.maybeDrain(ctx)
		return e
	}
	// Registration and a zero receipt are durable accounting facts, not
	// permission to admit traffic. They must remain replayable after revoke.
	// Obtain current, short-lived authority again immediately before opening
	// the gate; an old generation is sealed while it is still closed.
	delivery, e := x.Poll(ctx, PollInput{NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Capabilities: w.Capabilities()})
	if e != nil {
		w.maybeDrain(ctx)
		return e
	}
	if delivery == nil {
		return w.drain(ctx, x)
	}
	if e = VerifyDelivery(w.cfg.Key, *delivery, w.cfg.NodeID, w.cfg.InstallationID, time.Now()); e != nil {
		w.maybeDrain(ctx)
		return e
	}
	b := delivery.Bundle
	if b.Generation != w.state.Generation || b.ConfigSHA != w.state.Bundle.ConfigSHA || b.ManifestSHA != w.state.Bundle.ManifestSHA {
		return w.drain(ctx, x)
	}
	deadline := time.Unix(0, delivery.Lease.Expires)
	if time.Until(deadline) < 12*time.Second || !w.process.Renew(deadline) {
		return w.drain(ctx, x)
	}
	w.deadline = deadline
	w.state.Phase = "activation_intent"
	if e = w.save(); e != nil {
		return e
	}
	if e = w.process.Activate(ctx, w.state.ActivateNonce); e != nil {
		return e
	}
	w.state.Phase = "active"
	if e = w.save(); e != nil {
		return e
	}
	return w.observe(ctx, x, "active", "", true)
}

func (w *Worker) makeReport(values []inboundexec.Counter, final bool) inboundexec.UsageReport {
	e := w.state.Epoch
	r := inboundexec.UsageReport{EpochID: e.EpochID, NodeID: e.NodeID, InstallationID: e.InstallationID, Generation: e.Generation, Sequence: w.state.Sequence, ProcessIdentity: e.ProcessIdentity, Final: final, Counters: values}
	if final {
		r.FinalNonce = w.state.DrainNonce
		r.FinalProofSHA = FinalProofSHA(e.ProcessIdentity, r.FinalNonce, values)
		if r.Sequence == 1 {
			birth := *e
			r.InitialEpoch = &birth
		}
	}
	return r
}

func (w *Worker) sendPending(ctx context.Context, x Exchange) error {
	if w.persistFailed {
		return ErrPersistence
	}
	if w.state.Pending == nil {
		return nil
	}
	r := *w.state.Pending
	receipt, e := x.Usage(ctx, r)
	if e != nil {
		return e
	}
	if receipt.EpochID != r.EpochID || receipt.Sequence != r.Sequence || receipt.SHA256 != ReportSHA(r) || receipt.Final != r.Final {
		return ErrProof
	}
	w.state.Pending = nil
	if r.Final {
		w.state.FinalAccepted = true
		w.state.Phase = "final_accepted"
	}
	return w.save()
}

func (w *Worker) markUncertain() error {
	w.state.Phase = "uncertain"
	err := w.save()
	if w.process != nil {
		w.process.Stop()
		w.process = nil
	}
	return err
}

func (w *Worker) resolveUncertain(ctx context.Context, x Exchange) error {
	if w.state.Epoch == nil || w.state.Pending != nil {
		return ErrUncertain
	}
	delivery, err := x.Poll(ctx, PollInput{NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Capabilities: w.Capabilities(), UncertainEpochID: w.state.Epoch.EpochID})
	if err != nil {
		return err
	}
	if delivery == nil || delivery.Clearance == nil {
		return ErrUncertain
	}
	if err = VerifyDelivery(w.cfg.Key, *delivery, w.cfg.NodeID, w.cfg.InstallationID, time.Now()); err != nil {
		return err
	}
	c := *delivery.Clearance
	if c.EpochID != w.state.Epoch.EpochID || c.Generation != w.state.Epoch.Generation || c.EpochSHA != inboundexec.Digest(*w.state.Epoch) {
		return ErrProof
	}
	if w.process != nil {
		w.process.Stop()
		w.process = nil
	}
	w.state.Resolution = &journalResolution{Epoch: *w.state.Epoch, Clearance: c}
	w.state.Epoch = nil
	w.state.Phase = "idle"
	w.state.Sequence = 0
	w.state.LastCounters = nil
	w.state.Pending = nil
	w.state.ActivateNonce = ""
	w.state.DrainNonce = ""
	w.state.FinalAccepted = false
	return w.save()
}

// Drain persists its nonce before requesting the irreversible engine barrier.
// The final counters survive process exit, network loss and supervisor restart.
func (w *Worker) drain(ctx context.Context, x Exchange) error {
	if w.state.Pending != nil {
		if e := w.sendPending(ctx, x); e != nil {
			w.maybeDrain(ctx)
			return e
		}
	}
	if w.state.Epoch == nil {
		return nil
	}
	if w.state.FinalAccepted {
		if w.process != nil {
			w.process.Stop()
			w.process = nil
		}
		w.state.Epoch = nil
		w.state.Phase = "idle"
		return w.save()
	}
	if w.process == nil {
		return ErrUncertain
	}
	if w.state.DrainNonce == "" {
		nonce, e := randomID(32)
		if e != nil {
			return e
		}
		w.state.DrainNonce = nonce
		w.state.Phase = "draining"
		if e = w.save(); e != nil {
			return e
		}
	}
	values, e := w.process.Final(ctx, w.state.DrainNonce)
	if e != nil {
		return e
	}
	if e = w.validateInitialFinal(values); e != nil {
		return e
	}
	w.state.Sequence++
	r := w.makeReport(values, true)
	w.state.Pending = &r
	w.state.LastCounters = values
	w.state.Phase = "final_pending"
	if e = w.save(); e != nil {
		return e
	}
	if e = w.sendPending(ctx, x); e != nil {
		return e
	}
	w.process.Stop()
	w.process = nil
	w.state.Epoch = nil
	w.state.Phase = "idle"
	return w.save()
}

// Loss of the HUB starts the drain before the hard lease cutoff. A blocked
// receipt is retained locally; the gate remains closed while replaying it.
func (w *Worker) maybeDrain(ctx context.Context) {
	if w.process == nil || w.state.Epoch == nil || w.state.Pending != nil && w.state.Pending.Final || time.Until(w.deadline) > 12*time.Second {
		return
	}
	drain, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = w.captureFinal(drain)
}

func (w *Worker) captureFinal(ctx context.Context) error {
	if w.process == nil || w.state.Epoch == nil {
		return nil
	}
	if w.state.Pending != nil && w.state.Pending.Final || w.state.Phase == "sealed_waiting_receipt" || w.state.FinalAccepted {
		return nil
	}
	if w.state.DrainNonce == "" {
		var e error
		w.state.DrainNonce, e = randomID(32)
		if e != nil {
			return e
		}
		w.state.Phase = "draining"
		if e = w.save(); e != nil {
			return e
		}
	}
	values, e := w.process.Final(ctx, w.state.DrainNonce)
	if e != nil {
		return e
	}
	if e = w.validateInitialFinal(values); e != nil {
		return e
	}
	// A pending ordinary report cannot be overwritten. Its exact receipt must
	// arrive first; retain the final sample separately until then.
	if w.state.Pending != nil {
		w.state.LastCounters = values
		w.state.Phase = "sealed_waiting_receipt"
		return w.save()
	}
	w.state.Sequence++
	r := w.makeReport(values, true)
	w.state.Pending = &r
	w.state.LastCounters = values
	w.state.Phase = "final_pending"
	return w.save()
}

func (w *Worker) validateInitialFinal(values []inboundexec.Counter) error {
	if w.state.Sequence != 0 {
		return nil
	}
	for _, c := range values {
		if c.Upload != 0 || c.Download != 0 {
			// A closed, never-admitted birth cannot acquire an invented zero
			// baseline when a faulty engine reports bytes. Preserve uncertainty.
			if err := w.markUncertain(); err != nil {
				return err
			}
			return ErrProof
		}
	}
	return nil
}

func (w *Worker) observe(ctx context.Context, x Exchange, state, code string, ready bool) error {
	b := w.state.Bundle
	if b == nil {
		return nil
	}
	o := inboundexec.Observation{NodeID: b.NodeID, InstallationID: b.InstallationID, Generation: b.Generation, OperationID: b.OperationID, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, State: state, ErrorCode: code, ObservedUnixNano: time.Now().UnixNano(), LeaseExpires: w.deadline.UnixNano(), ProcessReady: ready, HealthState: "unknown", AccountingState: "pending"}
	if w.state.Epoch != nil {
		o.EpochID = w.state.Epoch.EpochID
	}
	if ready {
		o.HealthState = "engine_ready"
		o.AccountingState = "accepted"
	}
	if state == "stopped" {
		o.HealthState = "stopped"
		o.AccountingState = "settled"
	}
	if code == "accounting_tail_uncertain" {
		o.AccountingState = "uncertain"
		if w.state.Epoch != nil {
			birth := *w.state.Epoch
			o.InitialEpoch = &birth
		}
	}
	return x.Observe(ctx, o)
}

// Remove only the owned ephemeral birth directories after all durable recovery
// decisions. No path from a business configuration is a cleanup target.
func (w *Worker) cleanupBirths() {
	entries, _ := os.ReadDir(w.cfg.Directory)
	for _, entry := range entries {
		if entry.IsDir() && len(entry.Name()) > 6 && entry.Name()[:6] == "birth-" {
			_ = os.RemoveAll(filepath.Join(w.cfg.Directory, entry.Name()))
		}
	}
}

func cloneBundle(b inboundexec.Bundle) inboundexec.Bundle {
	raw, _ := json.Marshal(b)
	var c inboundexec.Bundle
	_ = json.Unmarshal(raw, &c)
	return c
}
