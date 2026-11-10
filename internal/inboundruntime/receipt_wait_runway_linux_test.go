//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

type blockedRunwayExchange struct {
	*fixtureExchange
	entered    chan struct{}
	release    chan struct{}
	replyError bool
}

func (x *blockedRunwayExchange) Usage(ctx context.Context, r ix.UsageReport) (ix.UsageReceipt, error) {
	close(x.entered)
	<-x.release // Deliberately ignores cancellation, like an in-flight local commit.
	receipt, err := x.fixtureExchange.Usage(context.Background(), r)
	if x.replyError {
		return ix.UsageReceipt{}, context.DeadlineExceeded
	}
	return receipt, err
}
func TestBlockedUsageWaitCapturesFinalBeforeReceiptAndReplaysAfterRestart(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		name := "success"
		if ambiguous {
			name = "committed_response_lost"
		}
		t.Run(name, func(t *testing.T) {
			w, p, base := fixtureWorker(t, "active")
			p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
			w.state.Sequence++
			ordinary := w.makeReport(p.values, false)
			w.state.Pending = &ordinary
			if err := w.save(); err != nil {
				t.Fatal(err)
			}
			probe := &journalSealProbe{p, make(chan struct{})}
			w.process = probe
			w.deadline = time.Now().Add(12*time.Second + 30*time.Millisecond)
			x := &blockedRunwayExchange{base, make(chan struct{}), make(chan struct{}), ambiguous}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			done := make(chan error, 1)
			go func() { done <- w.sendPending(ctx, x) }()
			select {
			case <-probe.sealed:
			case <-time.After(2 * time.Second):
				close(x.release)
				<-done
				t.Fatal("usage wait consumed the native final runway")
			}
			select {
			case <-done:
				t.Fatal("returned while the first commit is still in flight")
			default:
			}
			close(x.release)
			err := <-done
			if ambiguous && !errors.Is(err, context.DeadlineExceeded) || !ambiguous && err != nil {
				t.Fatal(err)
			}
			if w.state.Phase != "sealed_waiting_receipt" || w.state.LastCounters[0].Download != 73 {
				t.Fatal("genuine final sample lost")
			}
			if ambiguous && (w.state.Pending == nil || ReportSHA(*w.state.Pending) != ReportSHA(ordinary)) {
				t.Fatal("ambiguous ordinary outbox changed")
			}
			if !ambiguous && w.state.Pending != nil {
				t.Fatal("exact ordinary receipt not acknowledged")
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
			final := base.receipts[ordinary.Sequence+1]
			if ReportSHA(base.receipts[ordinary.Sequence]) != ReportSHA(ordinary) || !final.Final || final.Counters[0].Upload != 41 || final.Counters[0].Download != 73 || reopened.state.Epoch != nil {
				t.Fatal("ordinary/final exact replay failed")
			}
		})
	}
}

func TestBlockedUsageFinalTimeoutKeepsNonceAndDoesNotInventTail(t *testing.T) {
	w, p, base := fixtureWorker(t, "active")
	p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
	w.state.Sequence++
	ordinary := w.makeReport(p.values, false)
	w.state.Pending = &ordinary
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	probe := &delayedNonceFinal{fixtureProcess: p, entered: make(chan struct{})}
	w.process = probe
	w.deadline = time.Now().Add(12*time.Second + 30*time.Millisecond)
	x := &blockedRunwayExchange{base, make(chan struct{}), make(chan struct{}), false}
	done := make(chan error, 1)
	go func() { done <- w.sendPending(context.Background(), x) }()
	select {
	case <-probe.entered:
	case <-time.After(2 * time.Second):
		close(x.release)
		<-done
		t.Fatal("final not attempted")
	}
	close(x.release)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if w.state.Phase != "draining" || w.state.DrainNonce != probe.nonce || w.state.Pending == nil || ReportSHA(*w.state.Pending) != ReportSHA(ordinary) || w.state.Pending.Final {
		t.Fatal("ambiguous final was fabricated or retry nonce changed")
	}
	if err := w.sendPending(context.Background(), base); err != nil {
		t.Fatal("same-nonce recovery failed", err)
	}
	if w.state.Phase != "sealed_waiting_receipt" || w.state.Pending != nil || w.state.LastCounters[0].Download != 73 {
		t.Fatal("real retry sample not retained")
	}
}

func TestWorkerRealUsageWaitFinalReplay(t *testing.T) {
	f := newRealCounterFailureFixture(t)
	known := f.knownAndUnreportedTraffic(t)
	w, base := f.worker, f.exchange
	w.state.Sequence++
	ordinary := w.makeReport(known.Counters, false)
	w.state.Pending = &ordinary
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	probe := &realJournalSealProbe{w.process, make(chan struct{})}
	w.process = probe
	if !probe.Renew(time.Now().Add(1500 * time.Millisecond)) {
		t.Fatal("short real lease refused")
	}
	w.deadline = time.Now().Add(12*time.Second + 30*time.Millisecond)
	x := &blockedRunwayExchange{base, make(chan struct{}), make(chan struct{}), false}
	done := make(chan error, 1)
	go func() { done <- w.sendPending(context.Background(), x) }()
	select {
	case <-probe.sealed:
	case <-time.After(2 * time.Second):
		close(x.release)
		<-done
		t.Fatal("real final barrier missed while commit blocked")
	}
	f.traffic(t, false)
	deadline := time.Now().Add(3 * time.Second)
	for probe.Alive() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	alive := probe.Alive()
	close(x.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("native hard lease was extended")
	}
	if w.state.Phase != "sealed_waiting_receipt" || w.state.Pending != nil || w.state.LastCounters[0].Download <= known.Counters[0].Download {
		t.Fatal("real unreported tail lost")
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
	final := base.receipts[ordinary.Sequence+1]
	if ReportSHA(base.receipts[ordinary.Sequence]) != ReportSHA(ordinary) || !final.Final || final.Counters[0].Download <= known.Counters[0].Download || reopened.state.Epoch != nil {
		t.Fatal("real exact tail replay failed after hard lease shutdown")
	}
}
