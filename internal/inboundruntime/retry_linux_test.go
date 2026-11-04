//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

type retryExchange struct {
	*fixtureExchange
	poll func(context.Context) (*Delivery, error)
}

func (x *retryExchange) Poll(ctx context.Context, _ PollInput) (*Delivery, error) { return x.poll(ctx) }
func idleWorker(t *testing.T) *Worker {
	t.Helper()
	w, _, _ := fixtureWorker(t, "prepared")
	w.process = nil
	w.state = journal{Version: 1, NodeID: w.cfg.NodeID, InstallationID: w.cfg.InstallationID, Phase: "idle"}
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	return w
}
func TestIdleRetryBoundResetAndAccountingCadence(t *testing.T) {
	var retry idleRetry
	previous := time.Duration(0)
	for i := 0; i < 30; i++ {
		delay := retry.next(true, 249*time.Millisecond)
		if delay < previous || delay > 8*time.Second {
			t.Fatalf("unbounded retry: %v", delay)
		}
		previous = delay
	}
	// Success OR entering active/unsettled accounting must discard the backoff.
	if got := retry.next(false, 249*time.Millisecond); got != time.Second {
		t.Fatalf("accounting delayed: %v", got)
	}
	if got := retry.next(true, 0); got != time.Second {
		t.Fatalf("retry did not reset: %v", got)
	}
}
func TestWorkerSlowPollDoesNotCatchUpBufferedTicks(t *testing.T) {
	w := idleWorker(t)
	calls := make(chan time.Time, 4)
	finished := make(chan time.Time, 1)
	first := true
	x := &retryExchange{fixtureExchange: &fixtureExchange{}, poll: func(ctx context.Context) (*Delivery, error) {
		calls <- time.Now()
		if first {
			first = false
			select {
			case <-time.After(1200 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			finished <- time.Now()
		}
		return nil, errors.New("synthetic unavailable HUB")
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx, x) }()
	defer func() { cancel(); <-done }()
	<-calls
	var end time.Time
	select {
	case end = <-finished:
	case <-time.After(4 * time.Second):
		t.Fatal("poll did not finish")
	}
	select {
	case next := <-calls:
		if next.Sub(end) < 900*time.Millisecond {
			t.Fatal("slow request caused immediate retry")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("retry stalled")
	}
}
func TestBlockedNodeDoesNotBlockHealthyNodeAndCancelInterruptsRetry(t *testing.T) {
	blocked, healthy := idleWorker(t), idleWorker(t)
	entered := make(chan struct{})
	slow := &retryExchange{fixtureExchange: &fixtureExchange{}, poll: func(ctx context.Context) (*Delivery, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() }}
	pulses := make(chan struct{}, 8)
	fast := &retryExchange{fixtureExchange: &fixtureExchange{}, poll: func(context.Context) (*Delivery, error) { pulses <- struct{}{}; return nil, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	a, b := make(chan struct{}), make(chan struct{})
	go func() { defer close(a); blocked.Run(ctx, slow) }()
	go func() { defer close(b); healthy.Run(ctx, fast) }()
	defer cancel()
	<-entered
	for i := 0; i < 2; i++ {
		select {
		case <-pulses:
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatal("one blocked node stalled healthy worker")
		}
	}
	cancel()
	for _, done := range []chan struct{}{a, b} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("cancellation blocked by retry wait")
		}
	}
}

// Lose acknowledgements after the receiver committed the report. Exercise Run,
// not just Step, so retry scheduling and graceful final settlement participate.
type retryUsageExchange struct {
	*fixtureExchange
	reports []ix.UsageReport
	cancel  context.CancelFunc
}

func (x *retryUsageExchange) Usage(ctx context.Context, in ix.UsageReport) (ix.UsageReceipt, error) {
	receipt, err := x.fixtureExchange.Usage(ctx, in)
	if err != nil {
		return receipt, err
	}
	if !in.Final && in.Sequence == 2 {
		x.reports = append(x.reports, in)
		if len(x.reports) < 3 {
			return ix.UsageReceipt{}, errLostReceipt
		}
		x.cancel()
	}
	return receipt, nil
}
func TestWorkerRunLostUsageACKPreservesReportAndDoesNotReactivate(t *testing.T) {
	w, p, base := fixtureWorker(t, "active")
	p.values[0].Upload = 123
	p.values[0].Download = 456
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	x := &retryUsageExchange{fixtureExchange: base, cancel: cancel}
	w.Run(ctx, x)
	if len(x.reports) != 3 {
		t.Fatalf("expected exact receipt retries, got %d", len(x.reports))
	}
	sha := ReportSHA(x.reports[0])
	for _, report := range x.reports {
		if ReportSHA(report) != sha || report.Counters[0].Upload != 123 || report.Counters[0].Download != 456 {
			t.Fatal("retry changed durable usage")
		}
	}
	if p.activations != 0 || x.registerCalls != 0 {
		t.Fatal("lost usage reply reactivated an existing process")
	}
	foundFinal := false
	for _, report := range x.receipts {
		if report.Final {
			foundFinal = true
			if report.Counters[0].Upload != 123 || report.Counters[0].Download != 456 {
				t.Fatal("shutdown changed cumulative usage")
			}
		}
	}
	if !foundFinal {
		t.Fatal("shutdown did not settle final usage")
	}
}
