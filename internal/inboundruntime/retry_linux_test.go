//go:build linux

package inboundruntime

import (
	"context"
	"errors"
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
