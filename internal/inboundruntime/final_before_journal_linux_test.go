//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"os"
	"testing"
)

func TestFinalCapturePrecedesFailingJournalAndNeverSendsUndurableUsage(t *testing.T) {
	for _, mode := range []string{"capture", "drain"} {
		t.Run(mode, func(t *testing.T) {
			w, p, x := fixtureWorker(t, "active")
			p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
			// Keep the last durable journal intact, but make every subsequent write fail.
			dir := w.journal.dir
			saved := dir + "-saved"
			if err := os.Rename(dir, saved); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dir, []byte("unavailable journal directory"), 0600); err != nil {
				t.Fatal(err)
			}
			defer func() { os.Remove(dir); os.Rename(saved, dir) }()
			var err error
			if mode == "capture" {
				err = w.captureFinal(context.Background(), diagnosticLeaseRunway)
			} else {
				err = w.drain(context.Background(), x, diagnosticLeaseRunway)
			}
			if !errors.Is(err, ErrPersistence) {
				t.Fatalf("want persistence failure, got %v", err)
			}
			if p.finals != 1 {
				t.Fatal("journal failure prevented closing admission and measuring final counters")
			}
			if !w.persistFailed || w.process != nil || p.stops != 1 {
				t.Fatal("persistence failure did not fence process")
			}
			_ = w.Step(context.Background(), x)
			if x.usageCalls != 0 || len(x.receipts) != 0 {
				t.Fatal("undurable final escaped to accounting")
			}
		})
	}
}

// Model the hard lease kill after the real engine has sealed counters but before
// the worker can persist them (for example, when fsync exhausts lease runway).
type killedAfterSealProcess struct{ ownedProcess }

func (p *killedAfterSealProcess) Final(ctx context.Context, nonce string) ([]ix.Counter, error) {
	values, err := p.ownedProcess.Final(ctx, nonce)
	if err == nil {
		p.ownedProcess.Stop()
	}
	return values, err
}
func TestRealSealedTailSurvivesEngineExitBeforeJournalSave(t *testing.T) {
	f := newRealCounterFailureFixture(t)
	known := f.knownAndUnreportedTraffic(t)
	w, x := f.worker, f.exchange
	w.process = &killedAfterSealProcess{w.process}
	if err := w.captureFinal(context.Background(), diagnosticLeaseRunway); err != nil {
		t.Fatal(err)
	}
	if w.process.Alive() {
		t.Fatal("test engine unexpectedly alive")
	}
	if w.state.Pending == nil || !w.state.Pending.Final {
		t.Fatal("missing durable final")
	}
	final := *w.state.Pending
	if final.Counters[0].Download <= known.Counters[0].Download {
		t.Fatal("unreported real tail lost")
	}
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	x.delivery = stoppedDelivery(t, reopened)
	if err = reopened.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	got, ok := x.receipts[final.Sequence]
	if !ok || ReportSHA(got) != ReportSHA(final) || reopened.state.Epoch != nil {
		t.Fatal("exact durable final not settled after engine exit")
	}
}
