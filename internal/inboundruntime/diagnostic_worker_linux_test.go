//go:build linux

package inboundruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os/exec"
	"reflect"
	"sort"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type diagnosticRefusedRenewal struct{ *fixtureProcess }

func (p *diagnosticRefusedRenewal) Renew(time.Time) bool { p.renewals++; return false }

type diagnosticExchange struct {
	*fixtureExchange
	pollErr  error
	usageErr error
	onPoll   func()
}

func (x *diagnosticExchange) Poll(ctx context.Context, in PollInput) (*Delivery, error) {
	if x.onPoll != nil {
		x.onPoll()
	}
	if x.pollErr != nil {
		x.polls++
		return nil, x.pollErr
	}
	return x.fixtureExchange.Poll(ctx, in)
}
func (x *diagnosticExchange) Usage(ctx context.Context, in ix.UsageReport) (ix.UsageReceipt, error) {
	if x.usageErr != nil {
		x.usageCalls++
		return ix.UsageReceipt{}, x.usageErr
	}
	return x.fixtureExchange.Usage(ctx, in)
}

type diagnosticReceiptOutcome struct {
	Sequence uint64
	Final    bool
	Counters []ix.Counter
}
type diagnosticWorkerOutcome struct {
	Phase                                string
	Sequence                             uint64
	HasEpoch, HasProcess, FinalAccepted  bool
	Pending, PendingFinal                bool
	LastCounters                         []ix.Counter
	Stops, Finals, Renewals, Activations int
	Polls, Usage, Register               int
	ErrorClass                           diagnosticReason
	HasError                             bool
	Receipts                             []diagnosticReceiptOutcome
}

func diagnosticOutcome(w *Worker, p *fixtureProcess, x *fixtureExchange, err error) diagnosticWorkerOutcome {
	o := diagnosticWorkerOutcome{
		Phase: w.state.Phase, Sequence: w.state.Sequence, HasEpoch: w.state.Epoch != nil,
		HasProcess: w.process != nil, FinalAccepted: w.state.FinalAccepted,
		Pending: w.state.Pending != nil, LastCounters: w.state.LastCounters,
		Stops: p.stops, Finals: p.finals, Renewals: p.renewals, Activations: p.activations,
		Polls: x.polls, Usage: x.usageCalls, Register: x.registerCalls, HasError: err != nil,
	}
	if err != nil {
		o.ErrorClass = diagnosticErrorReason(err)
	}
	if w.state.Pending != nil {
		o.PendingFinal = w.state.Pending.Final
	}
	for seq, r := range x.receipts {
		o.Receipts = append(o.Receipts, diagnosticReceiptOutcome{seq, r.Final, r.Counters})
	}
	sort.Slice(o.Receipts, func(i, j int) bool { return o.Receipts[i].Sequence < o.Receipts[j].Sequence })
	return o
}

func TestDiagnosticsActualDrainReasonsPreserveRuntimeOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		phase  string
		setup  func(*testing.T, *Worker, *fixtureProcess, *fixtureExchange)
		action func(*Worker, *fixtureExchange) error
		reason diagnosticReason
		drain  bool
	}{
		{name: "healthy_renewal", phase: "active"},
		{name: "lease_runway", phase: "active", drain: true, reason: diagnosticLeaseRunway,
			setup: func(_ *testing.T, w *Worker, _ *fixtureProcess, _ *fixtureExchange) {
				w.deadline = time.Now().Add(5 * time.Second)
			}},
		{name: "delivery_absent", phase: "active", drain: true, reason: diagnosticDeliveryAbsent,
			setup: func(_ *testing.T, _ *Worker, _ *fixtureProcess, x *fixtureExchange) { x.delivery = nil }},
		{name: "withdrawal", phase: "active", drain: true, reason: diagnosticWithdrawal,
			setup: func(t *testing.T, w *Worker, _ *fixtureProcess, x *fixtureExchange) {
				x.delivery = stoppedDelivery(t, w)
			}},
		{name: "generation_change_during_admission", phase: "registered", drain: true, reason: diagnosticGenerationChange,
			setup: func(t *testing.T, w *Worker, _ *fixtureProcess, x *fixtureExchange) {
				b := cloneBundle(*w.state.Bundle)
				b.Generation++
				x.delivery = fixtureDelivery(t, w.cfg.Key, b)
			}},
		{name: "bundle_change_during_admission", phase: "registered", drain: true, reason: diagnosticBundleChange,
			setup: func(t *testing.T, w *Worker, _ *fixtureProcess, x *fixtureExchange) {
				b := cloneBundle(*w.state.Bundle)
				b.NativeConfig = append(b.NativeConfig, '\n')
				x.delivery = fixtureDelivery(t, w.cfg.Key, b)
			}},
		{name: "renewal_refused", phase: "registered", drain: true, reason: diagnosticRenewalRefused,
			setup: func(_ *testing.T, w *Worker, p *fixtureProcess, _ *fixtureExchange) {
				w.process = &diagnosticRefusedRenewal{p}
			}},
		{name: "counter_failure", phase: "active", drain: true, reason: diagnosticCounterFailure,
			setup: func(_ *testing.T, w *Worker, p *fixtureProcess, _ *fixtureExchange) {
				p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
				w.process = &failedSampleProcess{fixtureProcess: p}
			}},
		{name: "alias_fence", phase: "active", drain: true, reason: diagnosticAliasFence,
			action: func(w *Worker, _ *fixtureExchange) error {
				return w.fencePublicAliases(context.Background(), ErrPublicAliasIdentity)
			}},
		{name: "close_is_not_a_final_drain", phase: "active",
			action: func(w *Worker, _ *fixtureExchange) error { w.Close(); return nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var baseline diagnosticWorkerOutcome
			for _, enabled := range []bool{false, true} {
				w, p, x := fixtureWorker(t, tc.phase)
				d := &Diagnostics{}
				if enabled {
					w.cfg.Diagnostics = d
				}
				if tc.setup != nil {
					tc.setup(t, w, p, x)
				}
				var err error
				if tc.action != nil {
					err = tc.action(w, x)
				} else {
					err = w.Step(context.Background(), x)
				}
				got := diagnosticOutcome(w, p, x, err)
				if !enabled {
					baseline = got
					continue
				}
				if !reflect.DeepEqual(got, baseline) {
					t.Fatalf("diagnostics altered process/receipt outcome: got=%+v baseline=%+v", got, baseline)
				}
				var drains uint64
				for reason := diagnosticReason(0); reason < diagnosticReasonCount; reason++ {
					drains += diagnosticCount(d, diagnosticDrain, reason)
				}
				if tc.drain && (drains != 1 || diagnosticCount(d, diagnosticDrain, tc.reason) != 1) {
					t.Fatal("actual drain initiator was missing, duplicated, or misclassified", d.Snapshot())
				}
				if !tc.drain && drains != 0 {
					t.Fatal("renewal/Close falsely reported an accounting drain")
				}
				if tc.name == "close_is_not_a_final_drain" && (p.finals != 0 || diagnosticCount(d, diagnosticWorkerStop, diagnosticWorkerClose) != 1) {
					t.Fatal("Close was mislabeled as graceful final capture")
				}
			}
		})
	}
}

func TestDiagnosticsFinalReplayDoesNotInventSecondDrain(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	d := &Diagnostics{}
	w.cfg.Diagnostics = d
	w.deadline = time.Now().Add(5 * time.Second)
	x.loseFinal = true
	if err := w.Step(context.Background(), x); !errors.Is(err, errLostReceipt) {
		t.Fatal("expected exact lost receipt", err)
	}
	if w.state.Pending == nil || !w.state.Pending.Final || p.stops != 0 {
		t.Fatal("unaccepted final was not retained")
	}
	pendingSHA := ReportSHA(*w.state.Pending)
	if diagnosticCount(d, diagnosticDrain, diagnosticLeaseRunway) != 1 ||
		diagnosticCount(d, diagnosticFinalCapture, diagnosticSucceeded) != 1 ||
		diagnosticCount(d, diagnosticFinalReceipt, diagnosticSucceeded) != 0 {
		t.Fatal("drain, captured final and accepted receipt were conflated")
	}
	x.delivery = nil
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if p.stops != 1 || w.state.Epoch != nil || ReportSHA(x.receipts[2]) != pendingSHA {
		t.Fatal("diagnostics changed final replay")
	}
	if diagnosticCount(d, diagnosticDrain, diagnosticLeaseRunway) != 1 ||
		diagnosticCount(d, diagnosticFinalCapture, diagnosticSucceeded) != 1 ||
		diagnosticCount(d, diagnosticFinalReceipt, diagnosticSucceeded) != 1 {
		t.Fatal("final replay double-counted a drain or lost acceptance")
	}
}

func TestDiagnosticsDistinguishUsageAndPollBeforeLeaseDrain(t *testing.T) {
	for _, stage := range []diagnosticStage{diagnosticUsage, diagnosticPoll} {
		t.Run(diagnosticStageNames[stage], func(t *testing.T) {
			w, _, base := fixtureWorker(t, "active")
			d := &Diagnostics{}
			w.cfg.Diagnostics = d
			privateErr := &url.Error{Op: "POST", URL: "https://private.invalid/?token=never-log", Err: context.DeadlineExceeded}
			x := &diagnosticExchange{fixtureExchange: base}
			if stage == diagnosticUsage {
				r := w.makeReport(w.state.LastCounters, false)
				w.state.Pending = &r
				w.deadline = time.Now().Add(5 * time.Second)
				x.usageErr = privateErr
			} else {
				x.pollErr = privateErr
				x.onPoll = func() { w.deadline = time.Now().Add(5 * time.Second) }
			}
			if err := w.Step(context.Background(), x); err != privateErr {
				t.Fatal("diagnostics wrapped or replaced the original error")
			}
			if diagnosticCount(d, stage, diagnosticDeadline) != 1 ||
				diagnosticCount(d, diagnosticStep, diagnosticDeadline) != 1 ||
				diagnosticCount(d, diagnosticDrain, diagnosticLeaseRunway) != 1 ||
				diagnosticCount(d, diagnosticDrain, diagnosticCounterFailure) != 0 {
				t.Fatal("network stage and drain initiation were conflated", d.Snapshot())
			}
			if stage == diagnosticUsage && (base.polls != 0 || w.state.Pending == nil || w.state.Pending.Final || w.state.Phase != "sealed_waiting_receipt") {
				t.Fatal("pending ordinary receipt was overwritten or polling continued")
			}
			raw, err := json.Marshal(d.Snapshot())
			if err != nil || stringsContainsPrivate(raw) {
				t.Fatal("sensitive transport data escaped through snapshot")
			}
		})
	}
}

func stringsContainsPrivate(raw []byte) bool {
	for _, private := range []string{"private.invalid", "token=", "never-log", "fixture-epoch", "runtime-node", "customer-0001"} {
		if bytes.Contains(raw, []byte(private)) {
			return true
		}
	}
	return false
}

func TestDiagnosticsNativeExpiryCountsOnlyCurrentTimer(t *testing.T) {
	d := &Diagnostics{}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &Native{cmd: cmd, binary: &Binary{diagnostics: d}, leaseSerial: 2, dir: t.TempDir(), done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	t.Cleanup(p.Stop)
	p.expireLease(1)
	if p.ending || diagnosticCount(d, diagnosticNativeStop, diagnosticLeaseExpired) != 0 {
		t.Fatal("obsolete timer was counted or fenced current renewal")
	}
	p.expireLease(2)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("current expiry did not stop its owned process")
	}
	p.expireLease(2)
	p.Stop()
	if diagnosticCount(d, diagnosticNativeStop, diagnosticLeaseExpired) != 1 ||
		diagnosticCount(d, diagnosticNativeStop, diagnosticStopRequested) != 0 {
		t.Fatal("expiry or later cleanup counted the same stop twice")
	}
}

func TestDiagnosticsCounterFinalFailureAndDeadProcessStayUncertain(t *testing.T) {
	for _, dead := range []bool{false, true} {
		w, p, x := fixtureWorker(t, "active")
		d := &Diagnostics{}
		w.cfg.Diagnostics = d
		if dead {
			p.dead = true
		} else {
			w.process = &failedSampleProcess{fixtureProcess: p, finalErr: ErrRuntime}
		}
		_ = w.Step(context.Background(), x)
		if w.state.Phase != "uncertain" || w.process != nil || len(x.receipts) != 0 {
			t.Fatal("diagnostics weakened uncertainty fencing")
		}
		if dead {
			if diagnosticCount(d, diagnosticCoreState, diagnosticProcessLost) != 1 ||
				diagnosticCount(d, diagnosticDrain, diagnosticCounterFailure) != 0 {
				t.Fatal("dead process was mislabeled as a counter-driven final")
			}
		} else if diagnosticCount(d, diagnosticFinalCapture, diagnosticRuntime) != 1 ||
			diagnosticCount(d, diagnosticFinalCapture, diagnosticSucceeded) != 0 {
			t.Fatal("failed final was reported as durable success")
		}
	}
}

func TestDiagnosticsShutdownRecordsCaptureAndCloseSeparately(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	d := &Diagnostics{}
	w.cfg.Diagnostics = d
	// Simulate an existing exporter owner so this control-flow test does not
	// emit into the host journal. Exporter ownership/lifetime is tested apart.
	d.exporterActive.Store(true)
	defer d.exporterActive.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.Run(ctx, x)
	if !w.closed || p.stops != 1 || !w.state.FinalAccepted ||
		diagnosticCount(d, diagnosticDrain, diagnosticShutdown) != 1 ||
		diagnosticCount(d, diagnosticFinalCapture, diagnosticSucceeded) != 1 ||
		diagnosticCount(d, diagnosticFinalReceipt, diagnosticSucceeded) != 1 ||
		diagnosticCount(d, diagnosticWorkerStop, diagnosticWorkerClose) != 1 {
		t.Fatal("shutdown capture, receipt and Close were conflated", d.Snapshot())
	}
	if diagnosticCount(d, diagnosticDrain, diagnosticWorkerClose) != 0 {
		t.Fatal("Close invented a second final drain")
	}
}

func TestDiagnosticsFailedFinalRetryKeepsFirstDrainReason(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	d := &Diagnostics{}
	w.cfg.Diagnostics = d
	w.deadline = time.Now().Add(5 * time.Second)
	wrapped := &failedSampleProcess{fixtureProcess: p, finalErr: ErrRuntime}
	w.process = wrapped
	if err := w.Step(context.Background(), x); !errors.Is(err, ErrRuntime) {
		t.Fatal("expected temporary final-query failure", err)
	}
	nonce := w.state.DrainNonce
	if nonce == "" || w.state.Phase != "draining" || p.stops != 0 {
		t.Fatal("failed final changed drain/retry state")
	}
	wrapped.finalErr = nil
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if p.stops != 1 || w.state.DrainNonce != nonce ||
		diagnosticCount(d, diagnosticDrain, diagnosticLeaseRunway) != 1 ||
		diagnosticCount(d, diagnosticDrain, diagnosticRecovery) != 0 ||
		diagnosticCount(d, diagnosticFinalCapture, diagnosticRuntime) != 1 ||
		diagnosticCount(d, diagnosticFinalCapture, diagnosticSucceeded) != 1 {
		t.Fatal("retry replaced or repeated the first drain reason", d.Snapshot())
	}
}
