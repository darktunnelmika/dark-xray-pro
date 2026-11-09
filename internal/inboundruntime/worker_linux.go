//go:build linux

package inboundruntime

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
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
	Diagnostics    *Diagnostics
}

type Worker struct {
	mu                    sync.Mutex
	cfg                   Config
	journal               *privateJournal
	state                 journal
	native                *Binary
	external              map[string]*Binary
	process               ownedProcess
	deadline              time.Time
	closed                bool
	persistFailed         bool
	publicAliasKernel     publicAliasKernel
	stoppedObservationSHA string
	stoppedObservationAt  time.Time
	lastUsageReceiptAt    time.Time
	lastUsageReceiptEpoch string
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
	w.native.diagnostics = cfg.Diagnostics
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
		b.diagnostics = cfg.Diagnostics
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
	host := DetectHostCapabilities(w.native)
	return Capabilities{Version: 1, Binaries: m, Host: &host}
}

func (w *Worker) save() error {
	if w.persistFailed {
		return ErrPersistence
	}
	write := func() error { return w.journal.save(w.state) }
	var err error
	if w.process != nil && w.state.Epoch != nil && w.state.Phase == "active" && !w.state.FinalAccepted && !w.deadline.IsZero() {
		err = w.saveServing(write)
	} else {
		err = write()
	}
	if err != nil {
		// The in-memory state may be newer than the durable journal. Never
		// transmit that speculative report on a later Step, or continue serving
		// traffic after a counter/nonce/receipt durability failure.
		w.persistFailed = true
		if w.process != nil {
			w.cfg.Diagnostics.record(diagnosticWorkerStop, diagnosticPersistence)
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
		w.cfg.Diagnostics.record(diagnosticWorkerStop, diagnosticWorkerClose)
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
	var retries idleRetry
	stopDiagnostics := w.cfg.Diagnostics.startJournal()
	defer stopDiagnostics()
	defer w.Close()
	for {
		if ctx.Err() != nil {
			shutdown, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			w.mu.Lock()
			if !w.closed && !w.persistFailed {
				_ = w.captureFinal(shutdown, diagnosticShutdown)
				if w.state.Pending != nil && !w.persistFailed {
					_ = w.sendPending(shutdown, x)
				}
			}
			w.mu.Unlock()
			cancel()
			return
		}
		attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := w.Step(attempt, x)
		cancel()
		w.mu.Lock()
		// Never delay admission, lease renewal, an uncertain epoch, or an
		// unsettled receipt for the sake of idle-node retry throttling.
		idle := w.process == nil && w.state.Epoch == nil && w.state.Pending == nil && !w.persistFailed
		w.mu.Unlock()
		delay := retries.next(err != nil && idle, time.Duration(rand.Int64N(int64(250*time.Millisecond))))
		// Start after completion: a buffered ticker must not turn a slow
		// failed request into an immediate second attempt.
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

// Step performs one durable reconciliation. All outbound mutations can be
// repeated verbatim after a lost response; a cursor advances only on its exact
// receipt. It is exported to exercise real engines without scheduler sleeps.
func (w *Worker) Step(ctx context.Context, x Exchange) (stepErr error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	defer func() { w.cfg.Diagnostics.recordError(diagnosticStep, stepErr) }()
	if w.closed || x == nil {
		return ErrRuntime
	}
	if w.persistFailed {
		_ = w.observe(ctx, x, "failed", "accounting_tail_uncertain", false)
		return ErrPersistence
	}
	if w.process != nil && !w.process.Alive() && !w.state.FinalAccepted && w.state.Phase != "sealed_waiting_receipt" && (w.state.Pending == nil || !w.state.Pending.Final) {
		w.cfg.Diagnostics.record(diagnosticCoreState, diagnosticProcessLost)
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
			w.cfg.Diagnostics.record(diagnosticWorkerStop, diagnosticFinalAccepted)
			w.process.Stop()
			w.process = nil
		}
		if err := w.cleanupPublicAliases(ctx); err != nil {
			_ = w.observe(ctx, x, "blocked", "host_resource_cleanup_pending", false)
			return err
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
	if w.process == nil && w.state.Epoch == nil {
		if err := w.cleanupPublicAliases(ctx); err != nil {
			_ = w.observe(ctx, x, "blocked", "host_resource_cleanup_pending", false)
			return err
		}
	}
	if w.process != nil && (w.state.Phase == "prepared" || w.state.Phase == "registered" || w.state.Phase == "zero_pending" || w.state.Phase == "activation_intent") {
		return w.admit(ctx, x)
	}
	if w.process != nil && (w.state.Phase == "draining" || w.state.Phase == "final_pending") {
		return w.drain(ctx, x, diagnosticRecovery)
	}
	if w.process != nil && w.state.Phase == "active" {
		if time.Until(w.deadline) < 12*time.Second {
			return w.drain(ctx, x, diagnosticLeaseRunway)
		}
		values, e := w.process.Counters(ctx)
		w.cfg.Diagnostics.recordError(diagnosticCounters, e)
		if e != nil {
			// Preserve authoritative final counters before declaring an uncertain tail.
			finalCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			finalErr := w.captureFinal(finalCtx, diagnosticCounterFailure)
			cancel()
			if finalErr == nil {
				return e
			}
			_ = w.markUncertain()
			_ = w.observe(ctx, x, "failed", "accounting_tail_uncertain", false)
			return e
		}
		// Poll/lease/health still run every step. Only identical acknowledged
		// counters may wait for the bounded accounting heartbeat.
		if w.usageReportDue(values, time.Now()) {
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
	}
	if w.process != nil && w.state.Epoch != nil && (w.state.Phase == "sealed_waiting_receipt" || time.Until(w.deadline) <= 12*time.Second) {
		return w.drain(ctx, x, diagnosticLeaseRunway)
	}
	poll := PollInput{NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Capabilities: w.Capabilities()}
	if w.state.Resolution != nil {
		poll.ReconciledEpochID = w.state.Resolution.Epoch.EpochID
	}
	delivery, e := x.Poll(ctx, poll)
	w.cfg.Diagnostics.recordError(diagnosticPoll, e)
	if e != nil {
		w.maybeDrain(ctx)
		return e
	}
	if delivery == nil {
		if w.process != nil {
			return w.drain(ctx, x, diagnosticDeliveryAbsent)
		}
		return nil
	}
	if e = VerifyDelivery(w.cfg.Key, *delivery, w.cfg.NodeID, w.cfg.InstallationID, time.Now()); e != nil {
		w.cfg.Diagnostics.record(diagnosticPoll, diagnosticProof)
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
	deadline := verifiedLeaseDeadline(delivery.Lease, time.Now())
	if resolution := w.state.Resolution; resolution != nil && (len(bundle.Listeners) > 0 || len(bundle.Components) > 0) {
		if delivery.Lease.ResumeEpochID != resolution.Epoch.EpochID || delivery.Lease.ResumeEpochSHA != inboundexec.Digest(resolution.Epoch) || delivery.Lease.ResumeOperationID == "" {
			return ErrProof
		}
	}
	if w.process != nil && bundle.Generation == w.state.Generation && w.state.Phase == "active" {
		if e = w.renewPublicAliases(ctx, deadline); e != nil {
			return w.fencePublicAliases(ctx, e)
		}
		if !w.process.Renew(deadline) {
			return ErrLease
		}
		w.deadline = deadline
		if e = w.process.Health(ctx); e != nil {
			w.cfg.Diagnostics.recordError(diagnosticHealth, e)
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
		if e = w.drain(ctx, x, w.deliveryDrainReason(bundle)); e != nil {
			return e
		}
		return nil
	}
	if len(bundle.Listeners) == 0 && len(bundle.Components) == 0 {
		w.state = journal{Version: 1, NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Generation: bundle.Generation, Bundle: &bundle, Phase: "idle", FinalAccepted: true, Resolution: w.state.Resolution}
		w.deadline = deadline
		if !w.journal.matchesPersisted(w.state) {
			if e = w.save(); e != nil {
				return e
			}
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
	authorities, e := w.preparePublicAliases(ctx, bundle, deadline)
	if e != nil {
		_ = w.observe(ctx, x, "failed", "host_resource_prepare_failed", false)
		return e
	}
	group, e := startGroup(ctx, w.cfg.Directory, w.native, w.external, bundle, deadline, authorities)
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
		return w.drain(ctx, x, diagnosticLeaseRunway)
	}
	if w.state.Phase == "prepared" {
		receipt, e := x.Register(ctx, *w.state.Epoch)
		w.cfg.Diagnostics.recordError(diagnosticRegister, e)
		if e != nil {
			w.maybeDrain(ctx)
			return e
		}
		if !receipt.Registered || receipt.EpochID != w.state.Epoch.EpochID || receipt.ProcessIdentity != w.state.Epoch.ProcessIdentity {
			w.cfg.Diagnostics.record(diagnosticRegister, diagnosticProof)
			return ErrProof
		}
		w.state.Phase = "registered"
		if e = w.save(); e != nil {
			return e
		}
	}
	if w.state.Phase == "registered" {
		values, e := w.process.Counters(ctx)
		w.cfg.Diagnostics.recordError(diagnosticCounters, e)
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
	if w.state.Phase == "sealed_waiting_receipt" || time.Until(w.deadline) <= 12*time.Second {
		return w.drain(ctx, x, diagnosticLeaseRunway)
	}
	// Registration and a zero receipt are durable accounting facts, not
	// permission to admit traffic. They must remain replayable after revoke.
	// Obtain current, short-lived authority again immediately before opening
	// the gate; an old generation is sealed while it is still closed.
	delivery, e := x.Poll(ctx, PollInput{NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Capabilities: w.Capabilities()})
	w.cfg.Diagnostics.recordError(diagnosticPoll, e)
	if e != nil {
		w.maybeDrain(ctx)
		return e
	}
	if delivery == nil {
		return w.drain(ctx, x, diagnosticDeliveryAbsent)
	}
	if e = VerifyDelivery(w.cfg.Key, *delivery, w.cfg.NodeID, w.cfg.InstallationID, time.Now()); e != nil {
		w.cfg.Diagnostics.record(diagnosticPoll, diagnosticProof)
		w.maybeDrain(ctx)
		return e
	}
	b := delivery.Bundle
	if b.Generation != w.state.Generation || b.ConfigSHA != w.state.Bundle.ConfigSHA || b.ManifestSHA != w.state.Bundle.ManifestSHA {
		return w.drain(ctx, x, w.deliveryDrainReason(b))
	}
	deadline := verifiedLeaseDeadline(delivery.Lease, time.Now())
	if e = w.renewPublicAliases(ctx, deadline); e != nil {
		return w.fencePublicAliases(ctx, e)
	}
	if time.Until(deadline) < 12*time.Second {
		return w.drain(ctx, x, diagnosticLeaseRunway)
	}
	if !w.process.Renew(deadline) {
		return w.drain(ctx, x, diagnosticRenewalRefused)
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
	// A preceding durable write may consume the lease runway. Capture before
	// another network/store wait, not only after that wait has timed out.
	// Keep the ordinary outbox exact; captureFinal stores the sealed sample
	// separately until that receipt has been acknowledged.
	if w.process != nil && w.state.Epoch != nil && !w.state.Pending.Final &&
		!w.state.FinalAccepted && w.state.Phase != "sealed_waiting_receipt" && time.Until(w.deadline) <= 12*time.Second {
		finalCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := w.captureFinal(finalCtx, diagnosticLeaseRunway)
		cancel()
		if err != nil {
			return err
		}
	}
	r := *w.state.Pending
	started := time.Now()
	receipt, e := x.Usage(ctx, r)
	w.cfg.Diagnostics.recordError(diagnosticUsage, e)
	if e != nil {
		return e
	}
	if receipt.EpochID != r.EpochID || receipt.Sequence != r.Sequence || receipt.SHA256 != ReportSHA(r) || receipt.Final != r.Final {
		w.cfg.Diagnostics.record(diagnosticUsage, diagnosticProof)
		return ErrProof
	}
	w.state.Pending = nil
	if r.Final {
		w.state.FinalAccepted = true
		w.cfg.Diagnostics.record(diagnosticFinalReceipt, diagnosticSucceeded)
		w.state.Phase = "final_accepted"
	}
	if err := w.save(); err != nil {
		return err
	}
	// This witness is intentionally volatile: reopen must send an immediate
	// report. Only an exact durable receipt enables coalescing, but its
	// interval starts before delivery: slow acknowledgement or journal I/O
	// must not grant an additional five seconds without a usage report.
	if receipt.Replayed {
		// Replay settles the outbox but says nothing about the original
		// receipt's age. Refresh active accounting before coalescing again.
		w.lastUsageReceiptAt = time.Time{}
		w.lastUsageReceiptEpoch = ""
	} else {
		w.lastUsageReceiptAt = started
		w.lastUsageReceiptEpoch = r.EpochID
	}
	return nil
}

func (w *Worker) usageReportDue(values []inboundexec.Counter, now time.Time) bool {
	if w.state.Epoch == nil || w.state.Pending != nil || w.lastUsageReceiptAt.IsZero() ||
		w.lastUsageReceiptEpoch != w.state.Epoch.EpochID || !slices.Equal(values, w.state.LastCounters) {
		return true
	}
	age := now.Sub(w.lastUsageReceiptAt)
	return age < 0 || age >= 5*time.Second
}

func (w *Worker) markUncertain() error {
	w.state.Phase = "uncertain"
	err := w.save()
	if w.process != nil {
		w.cfg.Diagnostics.record(diagnosticWorkerStop, diagnosticUncertain)
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
	w.cfg.Diagnostics.recordError(diagnosticPoll, err)
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
	if err = w.cleanupPublicAliases(ctx); err != nil {
		return err
	}
	w.state.Resolution = &journalResolution{Epoch: *w.state.Epoch, Clearance: c, Bundle: cloneBundle(*w.state.Bundle)}
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

// Drain seals admission before journal I/O, then persists the nonce and final
// counters together before delivery. A crash before that save remains uncertain;
// no unmeasured tail is ever converted into an accepted final.
func (w *Worker) drain(ctx context.Context, x Exchange, reason diagnosticReason) error {
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
			w.cfg.Diagnostics.record(diagnosticWorkerStop, diagnosticFinalAccepted)
			w.process.Stop()
			w.process = nil
		}
		if e := w.cleanupPublicAliases(ctx); e != nil {
			return e
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
		w.cfg.Diagnostics.record(diagnosticDrain, reason)
		// Seal before disk I/O can consume the lease. Persist nonce and final
		// together below; no report is sent before that durable save succeeds.
	}
	values, e := w.process.Final(ctx, w.state.DrainNonce)
	w.cfg.Diagnostics.recordError(diagnosticFinalCapture, e)
	if e != nil {
		return e
	}
	if e = w.validateInitialFinal(values); e != nil {
		w.cfg.Diagnostics.recordError(diagnosticFinalCapture, e)
		return e
	}
	w.state.Sequence++
	r := w.makeReport(values, true)
	w.state.Pending = &r
	w.state.LastCounters = values
	w.state.Phase = "final_pending"
	if e = w.saveFinalCapture(); e != nil {
		return e
	}
	if e = w.sendPending(ctx, x); e != nil {
		return e
	}
	w.cfg.Diagnostics.record(diagnosticWorkerStop, diagnosticFinalAccepted)
	w.process.Stop()
	w.process = nil
	if e = w.cleanupPublicAliases(ctx); e != nil {
		return e
	}
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
	_ = w.captureFinal(drain, diagnosticLeaseRunway)
}

func (w *Worker) captureFinal(ctx context.Context, reason diagnosticReason) error {
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
		w.cfg.Diagnostics.record(diagnosticDrain, reason)
		// Seal before disk I/O can consume the lease. Persist nonce and final
		// together below; no report is sent before that durable save succeeds.
	}
	values, e := w.process.Final(ctx, w.state.DrainNonce)
	w.cfg.Diagnostics.recordError(diagnosticFinalCapture, e)
	if e != nil {
		return e
	}
	if e = w.validateInitialFinal(values); e != nil {
		w.cfg.Diagnostics.recordError(diagnosticFinalCapture, e)
		return e
	}
	// A pending ordinary report cannot be overwritten. Its exact receipt must
	// arrive first; retain the final sample separately until then.
	if w.state.Pending != nil {
		w.state.LastCounters = values
		w.state.Phase = "sealed_waiting_receipt"
		return w.saveFinalCapture()
	}
	w.state.Sequence++
	r := w.makeReport(values, true)
	w.state.Pending = &r
	w.state.LastCounters = values
	w.state.Phase = "final_pending"
	return w.saveFinalCapture()
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

// Stopped observations are display/operation acknowledgements, not traffic
// authority. Refresh them periodically even when no configuration changes.
const stoppedObservationInterval = 5 * time.Second

func (w *Worker) observe(ctx context.Context, x Exchange, state, code string, ready bool) error {
	return w.observeAt(ctx, x, state, code, ready, time.Now())
}

func (w *Worker) observeAt(ctx context.Context, x Exchange, state, code string, ready bool, now time.Time) error {
	b := w.state.Bundle
	if b == nil {
		return nil
	}
	o := inboundexec.Observation{NodeID: b.NodeID, InstallationID: b.InstallationID, Generation: b.Generation, OperationID: b.OperationID, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, State: state, ErrorCode: code, ObservedUnixNano: now.UnixNano(), LeaseExpires: w.deadline.UnixNano(), ProcessReady: ready, HealthState: "unknown", AccountingState: "pending"}
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
	// Only an already settled, empty runtime can coalesce the same successful
	// stopped acknowledgement. Polling and every accounting/active observation
	// retain their existing cadence. A transition or failed delivery clears it.
	stopped := state == "stopped" && code == "" && !ready &&
		w.state.Phase == "idle" && w.state.FinalAccepted &&
		w.process == nil && w.state.Epoch == nil && w.state.Pending == nil &&
		len(b.Listeners) == 0 && len(b.Components) == 0 &&
		!w.persistFailed && !w.closed
	var fingerprint string
	if stopped {
		unchanged := o
		// Neither timestamp grants authority for an empty, stopped bundle.
		unchanged.ObservedUnixNano, unchanged.LeaseExpires = 0, 0
		fingerprint = inboundexec.Digest(unchanged)
		age := now.Sub(w.stoppedObservationAt)
		if ctx.Err() == nil && fingerprint == w.stoppedObservationSHA &&
			age >= 0 && age < stoppedObservationInterval {
			return nil
		}
	}
	w.stoppedObservationSHA = ""
	w.stoppedObservationAt = time.Time{}
	err := x.Observe(ctx, o)
	w.cfg.Diagnostics.recordError(diagnosticObserve, err)
	if err == nil && stopped {
		w.stoppedObservationSHA = fingerprint
		// Use creation time: a slow acknowledgement must not extend freshness.
		w.stoppedObservationAt = now
	}
	return err
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

// idleRetry belongs to one worker. A dead node cannot slow another node or
// synchronize all reconnects against the HUB. Maximum idle recovery delay is
// eight seconds; active accounting retains the one-second cadence.
type idleRetry struct{ delay time.Duration }

func (r *idleRetry) next(failedIdle bool, jitter time.Duration) time.Duration {
	if !failedIdle {
		r.delay = 0
		return time.Second
	}
	if r.delay == 0 {
		r.delay = time.Second
	} else {
		r.delay *= 2
	}
	if r.delay > 8*time.Second {
		r.delay = 8 * time.Second
	}
	delay := r.delay + jitter
	if delay > 8*time.Second {
		delay = 8 * time.Second
	}
	return delay
}

// Diagnostics classify the triggering decision without changing the delivery.
func (w *Worker) deliveryDrainReason(b inboundexec.Bundle) diagnosticReason {
	if len(b.Listeners) == 0 && len(b.Components) == 0 {
		return diagnosticWithdrawal
	}
	if b.Generation != w.state.Generation {
		return diagnosticGenerationChange
	}
	return diagnosticBundleChange
}

func (w *Worker) saveFinalCapture() error {
	err := w.save()
	if err != nil {
		w.cfg.Diagnostics.recordError(diagnosticFinalCapture, err)
	} else {
		w.cfg.Diagnostics.record(diagnosticFinalCapture, diagnosticSucceeded)
	}
	return err
}
