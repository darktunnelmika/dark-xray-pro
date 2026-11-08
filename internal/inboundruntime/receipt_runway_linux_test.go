//go:build linux

package inboundruntime

import (
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

type runwayReceiptExchange struct {
	*fixtureExchange
	t       *testing.T
	process *fixtureProcess
}

func (x *runwayReceiptExchange) Usage(ctx context.Context, r ix.UsageReport) (ix.UsageReceipt, error) {
	if x.process.finals == 0 {
		x.t.Fatal("ordinary delivery entered with exhausted drain runway before final capture")
	}
	return x.fixtureExchange.Usage(ctx, r)
}
func TestPendingReceiptPreservesFinalBeforeBlockingDeliveryAtLowRunway(t *testing.T) {
	w, p, base := fixtureWorker(t, "active")
	p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
	w.deadline = time.Now().Add(6 * time.Second)
	w.state.Sequence++
	pending := w.makeReport(p.values, false)
	w.state.Pending = &pending
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	x := &runwayReceiptExchange{base, t, p}
	if err := w.sendPending(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if w.state.Phase != "sealed_waiting_receipt" || w.state.Pending != nil || w.state.LastCounters[0].Download != 73 || ReportSHA(base.receipts[2]) != ReportSHA(pending) {
		t.Fatal("final snapshot or exact receipt lost")
	}
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
	if !base.receipts[3].Final || base.receipts[3].Counters[0].Download != 73 || reopened.state.Epoch != nil {
		t.Fatal("final settlement lost after restart")
	}
}
