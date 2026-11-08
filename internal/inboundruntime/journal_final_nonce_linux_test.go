//go:build linux

package inboundruntime

import (
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

type delayedNonceFinal struct {
	*fixtureProcess
	nonce   string
	entered chan struct{}
}

func (p *delayedNonceFinal) Final(ctx context.Context, nonce string) ([]ix.Counter, error) {
	if p.nonce == "" {
		p.nonce = nonce
		close(p.entered)
		return nil, context.DeadlineExceeded
	}
	if p.nonce != nonce {
		return nil, ErrProof
	}
	return p.fixtureProcess.Final(ctx, nonce)
}
func TestJournalRunwayFinalTimeoutKeepsNonceForExactRetry(t *testing.T) {
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
	release := make(chan struct{})
	done := make(chan error, 1)
	writes := 0
	go func() {
		done <- w.saveServing(func() error {
			writes++
			if writes == 1 {
				<-release
			}
			return w.journal.save(w.state)
		})
	}()
	select {
	case <-probe.entered:
	case <-time.After(2 * time.Second):
		close(release)
		<-done
		t.Fatal("final not attempted")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if w.state.DrainNonce != probe.nonce || w.state.Phase != "draining" {
		t.Fatal("timed-out core final nonce lost; retry will conflict", w.state.Phase)
	}
	if w.state.Pending == nil || ReportSHA(*w.state.Pending) != ReportSHA(ordinary) || w.state.Pending.Final {
		t.Fatal("ordinary receipt changed or final fabricated")
	}
	if err := w.captureFinal(context.Background(), diagnosticRecovery); err != nil {
		t.Fatal("same-nonce retry failed", err)
	}
	if w.state.Phase != "sealed_waiting_receipt" || w.state.LastCounters[0].Download != 73 {
		t.Fatal("authoritative final not preserved")
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
	if ReportSHA(base.receipts[ordinary.Sequence]) != ReportSHA(ordinary) || !final.Final || final.FinalNonce != probe.nonce || final.Counters[0].Download != 73 || reopened.state.Epoch != nil {
		t.Fatal("exact ordinary/final replay lost")
	}
}
