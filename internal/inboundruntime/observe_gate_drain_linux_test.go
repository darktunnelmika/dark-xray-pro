//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

type stalledObserveExchange struct {
	*fixtureExchange
	entered, release, finished chan struct{}
}

func (x *stalledObserveExchange) Observe(_ context.Context, in ix.Observation) error {
	close(x.entered)
	<-x.release // Model a durable commit that ignores cancellation.
	defer close(x.finished)
	return nil
}

func TestObserveCommitGateCrossesRunwayWithoutLosingFinal(t *testing.T) {
	w, p, base := fixtureWorker(t, "active")
	p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
	x := &stalledObserveExchange{base, make(chan struct{}), make(chan struct{}), make(chan struct{})}
	exchange := WithDeadlines(x)
	observeCtx, stopObserve := context.WithCancel(context.Background())
	observed := make(chan error, 1)
	go func() { observed <- exchange.Observe(observeCtx, ix.Observation{}) }()
	<-x.entered
	stopObserve()
	if err := <-observed; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			close(x.release)
			<-x.finished
		}
	}()
	// Start above the drain threshold. The ordinary report is saved, then waits
	// behind the earlier Observe. Crossing the threshold must seal independently.
	w.deadline = time.Now().Add(13 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err := w.Step(ctx, exchange)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if w.state.Phase != "sealed_waiting_receipt" || w.state.Pending == nil || w.state.Pending.Final || p.finals != 1 || w.state.LastCounters[0].Download != 73 {
		t.Fatal("gate stall lost authoritative tail", w.state.Phase, p.finals)
	}
	pending := *w.state.Pending
	if len(base.receipts) != 0 {
		t.Fatal("timeout fabricated receipt")
	}
	close(x.release)
	<-x.finished
	released = true
	cfg := w.cfg
	w.Close()
	recovered, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	base.delivery = stoppedDelivery(t, recovered)
	if err = recovered.Step(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if ReportSHA(base.receipts[pending.Sequence]) != ReportSHA(pending) {
		t.Fatal("ordinary outbox changed")
	}
	final := base.receipts[pending.Sequence+1]
	if !final.Final || final.Counters[0].Upload != 41 || final.Counters[0].Download != 73 || recovered.state.Epoch != nil {
		t.Fatal("durable final not settled after restart")
	}
}
