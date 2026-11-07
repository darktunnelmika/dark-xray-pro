//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type stoppedObserveExchange struct {
	*fixtureExchange
	failures int
}

func (x *stoppedObserveExchange) Observe(ctx context.Context, in ix.Observation) error {
	x.observations = append(x.observations, in)
	if err := ctx.Err(); err != nil {
		return err
	}
	if x.failures > 0 {
		x.failures--
		return errLostReceipt
	}
	return nil
}

func settledObservationWorker(t *testing.T) (*Worker, *stoppedObserveExchange) {
	t.Helper()
	w, _, base := fixtureWorker(t, "prepared")
	base.delivery = stoppedDelivery(t, w)
	b := base.delivery.Bundle
	w.process = nil
	w.state = journal{
		Version: 1, NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID,
		Generation: b.Generation, Bundle: &b, Phase: "idle", FinalAccepted: true,
	}
	w.deadline = time.Unix(0, base.delivery.Lease.Expires)
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	return w, &stoppedObserveExchange{fixtureExchange: base}
}

func TestStoppedObservationRefreshIsBoundedAndIdentityChangesAreImmediate(t *testing.T) {
	w, x := settledObservationWorker(t)
	now := time.Now()
	for _, elapsed := range []time.Duration{0, time.Second, 4999 * time.Millisecond, 5 * time.Second} {
		// Lease timestamps change with every poll even though the runtime is empty.
		w.deadline = now.Add(30*time.Second + elapsed)
		if err := w.observeAt(context.Background(), x, "stopped", "", false, now.Add(elapsed)); err != nil {
			t.Fatal(err)
		}
	}
	if len(x.observations) != 2 || x.observations[1].ObservedUnixNano != now.Add(5*time.Second).UnixNano() {
		t.Fatal("unchanged stopped report did not refresh at five seconds")
	}
	for i, change := range []func(*ix.Bundle){
		func(b *ix.Bundle) { b.Generation++ },
		func(b *ix.Bundle) { b.OperationID = "fixture-stop-new-operation" },
		func(b *ix.Bundle) { b.ConfigSHA = "changed-config" },
		func(b *ix.Bundle) { b.ManifestSHA = "changed-manifest" },
	} {
		change(w.state.Bundle)
		if err := w.observeAt(context.Background(), x, "stopped", "", false, now.Add(5100*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		if len(x.observations) != 3+i {
			t.Fatal("changed stopped identity was suppressed")
		}
	}
}

func TestStoppedObservationFailedDeliveryAndCancellationRemainRetryable(t *testing.T) {
	w, x := settledObservationWorker(t)
	now := time.Now()
	x.failures = 1
	if err := w.observeAt(context.Background(), x, "stopped", "", false, now); !errors.Is(err, errLostReceipt) {
		t.Fatal(err)
	}
	if err := w.observeAt(context.Background(), x, "stopped", "", false, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := w.observeAt(context.Background(), x, "stopped", "", false, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if len(x.observations) != 2 {
		t.Fatal("failed delivery was cached or successful retry was not coalesced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.observeAt(ctx, x, "stopped", "", false, now.Add(3*time.Millisecond)); !errors.Is(err, context.Canceled) {
		t.Fatal("cached observation hid cancellation", err)
	}
	if err := w.observeAt(context.Background(), x, "stopped", "", false, now.Add(4*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if len(x.observations) != 4 {
		t.Fatal("canceled attempt suppressed the next retry")
	}
}

func TestStoppedObservationNeverCoalescesUnsettledOrNonStoppedState(t *testing.T) {
	tests := []struct {
		name        string
		state, code string
		ready       bool
		change      func(*Worker)
	}{
		{name: "active", state: "active", ready: true},
		{name: "failed", state: "failed"},
		{name: "blocked", state: "blocked", code: "host_resource_cleanup_pending"},
		{name: "uncertain", state: "failed", code: "accounting_tail_uncertain"},
		{name: "error", state: "stopped", code: "accounting_tail_uncertain"},
		{name: "ready", state: "stopped", ready: true},
		{name: "process", state: "stopped", change: func(w *Worker) { w.process = &fixtureProcess{} }},
		{name: "epoch", state: "stopped", change: func(w *Worker) { w.state.Epoch = &ix.EpochInput{EpochID: "unsettled-epoch"} }},
		{name: "pending", state: "stopped", change: func(w *Worker) { w.state.Pending = &ix.UsageReport{Sequence: 1} }},
		{name: "final_unaccepted", state: "stopped", change: func(w *Worker) { w.state.FinalAccepted = false }},
		{name: "not_idle", state: "stopped", change: func(w *Worker) { w.state.Phase = "prepared" }},
		{name: "listener", state: "stopped", change: func(w *Worker) { w.state.Bundle.Listeners = []ix.Listener{{Port: 12345}} }},
		{name: "component", state: "stopped", change: func(w *Worker) { w.state.Bundle.Components = []ix.Component{{}} }},
		{name: "persistence_failure", state: "stopped", change: func(w *Worker) { w.persistFailed = true }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, x := settledObservationWorker(t)
			now := time.Now()
			if tc.change != nil {
				tc.change(w)
			}
			for i := 0; i < 2; i++ {
				if err := w.observeAt(context.Background(), x, tc.state, tc.code, tc.ready, now.Add(time.Duration(i)*time.Millisecond)); err != nil {
					t.Fatal(err)
				}
			}
			if len(x.observations) != 2 {
				t.Fatal("observation outside settled empty state was suppressed")
			}
			// Avoid test fixture state being mistaken for a live process at cleanup.
			w.process = nil
			w.state.Epoch, w.state.Pending = nil, nil
		})
	}
}

func TestStoppedObservationKeepsPollingAndAcknowledgesNextCommand(t *testing.T) {
	w, x := settledObservationWorker(t)
	for i := 0; i < 3; i++ {
		if err := w.Step(context.Background(), x); err != nil {
			t.Fatal(err)
		}
	}
	if x.polls != 3 || len(x.observations) != 1 || x.usageCalls != 0 {
		t.Fatalf("stopped polling changed: polls=%d observations=%d usage=%d", x.polls, len(x.observations), x.usageCalls)
	}
	b := cloneBundle(x.delivery.Bundle)
	b.Generation++
	b.OperationID = "fixture-next-stop-command"
	x.delivery = fixtureDelivery(t, w.cfg.Key, b)
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if x.polls != 4 || len(x.observations) != 2 || x.observations[1].Generation != b.Generation {
		t.Fatal("new command acknowledgement waited for the refresh interval")
	}
}

func TestStoppedObservationRestartDoesNotReuseAcknowledgementCache(t *testing.T) {
	w, x := settledObservationWorker(t)
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	cfg := w.cfg
	w.Close()
	restarted, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if err := restarted.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if len(x.observations) != 2 {
		t.Fatal("restarted worker skipped its first stopped acknowledgement")
	}
}

func TestStoppedObservationActiveAccountingCadenceIsUnchanged(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	for i := 0; i < 3; i++ {
		if err := w.Step(context.Background(), x); err != nil {
			t.Fatal(err)
		}
	}
	if x.usageCalls != 3 || x.polls != 3 || p.renewals != 3 || len(x.observations) != 3 {
		t.Fatal("active accounting, renewal or observation was coalesced")
	}
}

func TestStoppedObservationClockRegressionDoesNotSuppressDelivery(t *testing.T) {
	w, x := settledObservationWorker(t)
	now := time.Now()
	for _, at := range []time.Time{now, now.Add(-time.Second)} {
		if err := w.observeAt(context.Background(), x, "stopped", "", false, at); err != nil {
			t.Fatal(err)
		}
	}
	if len(x.observations) != 2 {
		t.Fatal("a backwards clock suppressed stopped reporting")
	}
}

func TestStoppedObservationTransitionInvalidatesPreviousSuccess(t *testing.T) {
	w, x := settledObservationWorker(t)
	now := time.Now()
	for i, state := range []string{"stopped", "blocked", "stopped"} {
		if err := w.observeAt(context.Background(), x, state, "", false, now.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if len(x.observations) != 3 {
		t.Fatal("return from blocked state reused a stale stopped acknowledgement")
	}
}
