//go:build linux

package managedxray

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type usageAdmission struct {
	FinalCounterProtocol string      `json:"final_counter_protocol,omitempty"`
	DrainNonce           string      `json:"drain_nonce,omitempty"`
	FinalReceipt         *uc.Receipt `json:"final_receipt,omitempty"`
	TrafficBarrier       string      `json:"traffic_barrier,omitempty"`
	Settlement           *uc.Receipt `json:"settlement,omitempty"`
	StatsEndpoint        string      `json:"stats_endpoint"`
	BootstrapSHA         string      `json:"bootstrap_sha"`
}

// PrepareUsageAdmission is a two-phase start on a fresh runtime or a proven
// settled pretraffic birth with a NEW monotonic signed revision. The signed
// revision is validated, but Xray starts with NO customer inbounds. Only its
// private API socket exists. The returned process birth must be registered by
// the trusted HUB before ActivateUsageAdmission is called. No baseline or
// quota authority is invented here; this is not an HTTP admission endpoint.
func (r *Runtime) PrepareUsageAdmission(ctx context.Context, v Revision, l Lease) (uc.ProcessToken, error) {
	if ctx == nil || ctx.Err() != nil {
		return uc.ProcessToken{}, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.fenced {
		return uc.ProcessToken{}, ErrClosed
	}
	if !r.canPrepareUsageLocked() {
		return uc.ProcessToken{}, ErrUsageSettlement
	}
	saved := freeze(v)
	if saved.Sequence <= r.state.Highwater || l.Issued <= r.state.LastLeaseIssued {
		return uc.ProcessToken{}, ErrStale
	}
	if !r.validLease(l, saved) {
		return uc.ProcessToken{}, ErrLease
	}
	if err := validateLayout(saved.candidate(), r.options.ListenIP, r.reserved); err != nil {
		return uc.ProcessToken{}, err
	}
	bootstrap, _, endpoint, err := xrayconfig.UsageBootstrap(saved.candidate())
	if err != nil {
		return uc.ProcessToken{}, err
	}
	if r.options.ExperimentalFinalTCP && !finalTCPScope(saved.Raw) {
		return uc.ProcessToken{}, ErrInvalid
	}
	if r.reserved[bootstrap.Listeners[0].NodePort] {
		return uc.ProcessToken{}, ErrInvalid
	}
	if _, err = xrayconfig.ValidateOffline(ctx, saved.candidate(), r.options.Binary, r.options.BinarySHA); err != nil {
		return uc.ProcessToken{}, err
	}
	if _, err = xrayconfig.ValidateOffline(ctx, bootstrap, r.options.Binary, r.options.BinarySHA); err != nil {
		return uc.ProcessToken{}, err
	}
	if ctx.Err() != nil || !r.validLease(l, saved) {
		return uc.ProcessToken{}, ErrLease
	}
	next := r.state
	next.Version = 4
	next.Previous = next.Good
	next.Usage = nil
	next.Highwater = saved.Sequence
	next.DesiredSHA = saved.SHA
	next.DesiredManifest = saved.Manifest
	next.Good = saved
	next.Pending = nil
	next.LastLeaseIssued = l.Issued
	next.Admission = &usageAdmission{StatsEndpoint: endpoint, BootstrapSHA: bootstrap.SHA256, TrafficBarrier: pretrafficNeverInstalled}
	if r.options.ExperimentalFinalTCP {
		next.Version = 5
		next.Admission.FinalCounterProtocol = uc.FinalTCPProtocol
	}
	next.Phase = "usage_preparing_api_only"
	if err = r.persist(next); err != nil {
		return uc.ProcessToken{}, err
	}
	p, err := startChild(ctx, bootstrap, r.options.Binary, leaseDeadline(l), r.options.ExperimentalFinalTCP)
	if err != nil {
		r.failAdmissionLocked("usage_prepare_failed")
		return uc.ProcessToken{}, err
	}
	r.process = p
	r.active = saved
	r.lease = l
	r.deadline = leaseDeadline(l)
	if ctx.Err() != nil || !r.validLease(l, saved) {
		r.failAdmissionLocked("usage_prepare_expired")
		return uc.ProcessToken{}, ErrLease
	}
	token, err := uc.Process(p.cmd.Process.Pid)
	if err != nil {
		r.failAdmissionLocked("usage_prepare_birth_unverified")
		return uc.ProcessToken{}, err
	}
	next = r.state
	next.Phase = "usage_prepared_no_customer_listeners"
	if err = r.persist(next); err != nil {
		return uc.ProcessToken{}, err
	}
	return token, nil
}

func (r *Runtime) failAdmissionLocked(reason string) {
	if r.observer != nil {
		r.observer.cancel()
	}
	r.process.stop()
	r.process = nil
	r.active = nil
	if !r.closed && !r.fenced {
		next := r.state
		next.Phase = reason
		_ = r.persist(next)
	}
}

// ActivateUsageAdmission opens only the exact inbounds of the signed revision,
// AFTER a complete zero observation has an exact durable HUB receipt and the
// independent observer has sampled this API-only process. Any ambiguous or
// failed operation leaves a sticky settlement fence; no automatic retry can
// skip the receipt. The caller owns ctx for this operation, not the worker's
// lifetime. Runtime.Close and the original short lease supervise that lifetime.
func (r *Runtime) ActivateUsageAdmission(ctx context.Context, o UsageObservation) (result Snapshot, err error) {
	if ctx == nil || ctx.Err() != nil {
		return r.Snapshot(), ErrInvalid
	}
	o, err = validateObservation(o)
	if err != nil {
		return r.Snapshot(), err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.fenced {
		return r.snapshot(), ErrClosed
	}
	if r.state.Admission == nil || r.state.Usage != nil || r.observer != nil || r.state.Phase != "usage_prepared_no_customer_listeners" {
		return r.snapshot(), ErrUsageSettlement
	}
	if o.StatsEndpoint != r.state.Admission.StatsEndpoint {
		return r.snapshot(), uc.ErrEpoch
	}
	if err = r.checkObservationLocked(o); err != nil {
		return r.snapshot(), err
	}
	bootstrap, payload, endpoint, err := xrayconfig.UsageBootstrap(r.active.candidate())
	if err != nil || bootstrap.SHA256 != r.state.Admission.BootstrapSHA {
		return r.snapshot(), ErrInvalid
	}
	directory := usageDirectory(r.options.Directory, o.Epoch.ID)
	if _, e := os.Lstat(directory); !errors.Is(e, os.ErrNotExist) {
		return r.snapshot(), ErrUsageSettlement
	}
	next := r.state
	next.Usage = &observedUsage{Epoch: o.Epoch, StatsEndpoint: endpoint}
	next.Phase = "usage_waiting_zero_receipt"
	if err = r.persist(next); err != nil {
		return r.snapshot(), err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			r.failAdmissionLocked("usage_admission_failed_stopped")
			result = r.snapshot()
		}
	}()
	if r.state.Admission.FinalCounterProtocol != "" {
		if err = uc.VerifyFinalTCPCapability(ctx, r.options.Binary, r.options.BinarySHA, endpoint, o.Epoch); err != nil {
			return r.snapshot(), err
		}
	}
	box, err := uc.Open(uc.Options{Directory: directory, Key: usageKey(r.options.StateKey, o.Epoch.ID), Epoch: o.Epoch, Create: true})
	if err != nil {
		return r.snapshot(), err
	}
	ownedBox := true
	defer func() {
		if ownedBox {
			_ = box.Close()
		}
	}()
	zeros := make([]uc.Usage, len(o.Epoch.Bindings))
	for i, b := range o.Epoch.Bindings {
		zeros[i].ClientID = b.ClientID
	}
	baseline, err := box.Observe(zeros, false)
	if err != nil {
		return r.snapshot(), err
	}
	if baseline.Sequence != 1 {
		return r.snapshot(), uc.ErrConflict
	}
	sent, err := o.Sender.FlushOne(ctx, box)
	if err != nil || !sent {
		if err == nil {
			err = uc.ErrReceipt
		}
		return r.snapshot(), err
	}
	pending, err := box.Pending()
	if err != nil || len(pending) != 0 {
		return r.snapshot(), uc.ErrReceipt
	}
	receipt := uc.Receipt{EpochID: baseline.EpochID, Sequence: baseline.Sequence, SHA: baseline.SHA}
	counters, err := uc.NewFreshCounters(o.Epoch, baseline, receipt)
	if err != nil {
		return r.snapshot(), err
	}
	// No actual first-use counters may already contain traffic in the empty core.
	values, err := counters.Collect(ctx, r.options.Binary, r.options.BinarySHA, endpoint)
	if err != nil {
		return r.snapshot(), err
	}
	for _, value := range values {
		if value.Uplink != 0 || value.Downlink != 0 {
			return r.snapshot(), uc.ErrConflict
		}
	}
	if ctx.Err() != nil || !r.validLease(r.lease, r.active) {
		return r.snapshot(), ErrLease
	}
	// Persist the point of no return BEFORE creating an observer or attempting
	// AddInbound. A crash here is conservatively an uncertain traffic epoch.
	next = r.state
	admission := *next.Admission
	admission.TrafficBarrier = pretrafficActivationPossible
	next.Admission = &admission
	if err = r.persist(next); err != nil {
		return r.snapshot(), err
	}
	observerCtx, cancel := context.WithCancel(context.Background())
	observer := &usageObserver{settle: make(chan finalTCPRequest), options: o, process: r.process, box: box, binary: r.options.Binary, pin: r.options.BinarySHA, cancel: cancel, done: make(chan struct{}), lastSaved: time.Now(), fresh: counters, ready: make(chan struct{}), status: UsageObservationStatus{Required: true, Running: true, Receipts: 1, SettlementRequired: true}}
	r.observer = observer
	ownedBox = false
	go observer.run(observerCtx)
	select {
	case <-observer.ready:
	case <-observer.done:
		return r.snapshot(), ErrUsageSettlement
	case <-ctx.Done():
		return r.snapshot(), ctx.Err()
	}
	if !observer.healthy() || !r.validLease(r.lease, r.active) {
		return r.snapshot(), ErrLease
	}
	next = r.state
	next.Phase = "usage_activating_signed_inbounds"
	if err = r.persist(next); err != nil {
		return r.snapshot(), err
	}
	if err = r.installAdmissionInbounds(ctx, payload, endpoint, o.Epoch); err != nil {
		return r.snapshot(), err
	}
	wait, cancelWait := context.WithTimeout(ctx, 4*time.Second)
	defer cancelWait()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for !r.process.bound(r.active.Listeners) {
		if r.process.dead() || !observer.healthy() {
			return r.snapshot(), ErrRuntime
		}
		select {
		case <-wait.Done():
			return r.snapshot(), wait.Err()
		case <-tick.C:
		}
	}
	if ctx.Err() != nil || !observer.healthy() || !r.validLease(r.lease, r.active) {
		return r.snapshot(), ErrLease
	}
	next = r.state
	next.Phase = "ready_observed_admission"
	if err = r.persist(next); err != nil {
		return r.snapshot(), err
	}
	succeeded = true
	return r.snapshot(), nil
}

// No shell command or user-supplied path is executed. The private payload is
// derived from the frozen signed revision, and only the captured core's API is
// addressed. CLI output is discarded because it can contain customer secrets.
func (r *Runtime) installAdmissionInbounds(ctx context.Context, payload []byte, endpoint string, epoch uc.Epoch) error {
	if err := r.checkObservationLocked(UsageObservation{Epoch: epoch, StatsEndpoint: endpoint}); err != nil {
		return err
	}
	if _, err := xrayconfig.ValidateOffline(ctx, r.active.candidate(), r.options.Binary, r.options.BinarySHA); err != nil {
		return err
	}
	if !r.validLease(r.lease, r.active) {
		return ErrLease
	}
	if err := uc.VerifyOwnedStatsEndpoint(epoch, endpoint); err != nil {
		return err
	}
	path := filepath.Join(r.process.dir, "admission-inbounds.json")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		return err
	}
	defer os.Remove(path)
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(call, r.options.Binary, "api", "adi", "--server="+endpoint, "--timeout=2", path)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 250 * time.Millisecond
	if cmd.Run() != nil {
		return ErrRuntime
	}
	return r.checkObservationLocked(UsageObservation{Epoch: epoch, StatsEndpoint: endpoint})
}

func validPersistedAdmission(j journal) bool {
	if (j.Version != 3 && j.Version != 4 && j.Version != 5) || j.Admission == nil || j.Good == nil {
		return false
	}
	if j.Version == 3 && (j.Admission.TrafficBarrier != "" || j.Admission.Settlement != nil) {
		return false
	}
	if j.Version >= 4 && !validPretrafficJournal(j) {
		return false
	}
	if !validFinalTCPJournal(j) {
		return false
	}
	if j.Version == 5 && !finalTCPScope(j.Good.Raw) {
		return false
	}
	bootstrap, _, endpoint, err := xrayconfig.UsageBootstrap(j.Good.candidate())
	if err != nil || j.Admission.StatsEndpoint != endpoint || j.Admission.BootstrapSHA != bootstrap.SHA256 {
		return false
	}
	digest := sha256.Sum256(j.Good.Raw)
	return hex.EncodeToString(digest[:]) == j.Good.SHA
}
