//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

type stalledReceiptExchange struct {
	*fixtureExchange
	release, finished, entered chan struct{}
}

func (x *stalledReceiptExchange) Usage(ctx context.Context, report ix.UsageReport) (ix.UsageReceipt, error) {
	close(x.entered)
	<-x.release
	defer close(x.finished)
	return x.fixtureExchange.Usage(ctx, report)
}
func TestLocalStoreStallPreservesFinalAndReplaysExactReceiptAfterRestart(t *testing.T) {
	w, p, base := fixtureWorker(t, "active")
	p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
	w.deadline = time.Now().Add(11 * time.Second)
	w.state.Sequence++
	pending := w.makeReport(p.values, false)
	w.state.Pending = &pending
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	x := &stalledReceiptExchange{base, make(chan struct{}), make(chan struct{}), make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- w.Step(ctx, WithDeadlines(x)) }()
	select {
	case <-x.entered:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("store call did not begin")
	}
	cancel()
	err := <-result
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if w.state.Phase != "sealed_waiting_receipt" || w.state.Pending == nil || w.state.Pending.Final || ReportSHA(*w.state.Pending) != ReportSHA(pending) || w.state.LastCounters[0].Download != 73 {
		t.Fatal("blocked local receipt lost final or replaced pending report", w.state.Phase)
	}
	close(x.release)
	<-x.finished
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	base.delivery = stoppedDelivery(t, reopened)
	if err = reopened.Step(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	final := base.receipts[3]
	if !final.Final || final.Counters[0].Upload != 41 || final.Counters[0].Download != 73 || reopened.state.Epoch != nil || ReportSHA(base.receipts[2]) != ReportSHA(pending) {
		t.Fatal("restart failed exact replay and measured final settlement")
	}
}
