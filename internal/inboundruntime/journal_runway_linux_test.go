//go:build linux

package inboundruntime

import (
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

type journalSealProbe struct {
	*fixtureProcess
	sealed chan struct{}
}

func (p *journalSealProbe) Final(ctx context.Context, nonce string) ([]ix.Counter, error) {
	v, e := p.fixtureProcess.Final(ctx, nonce)
	close(p.sealed)
	return v, e
}
func TestBlockedJournalSealsBeforeWriteCompletesAndReplaysExactTail(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "after_receipt"
		if pending {
			name = "ordinary_pending"
		}
		t.Run(name, func(t *testing.T) {
			w, p, x := fixtureWorker(t, "active")
			p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
			var original ix.UsageReport
			if pending {
				w.state.Sequence++
				original = w.makeReport(p.values, false)
				w.state.Pending = &original
				if err := w.save(); err != nil {
					t.Fatal(err)
				}
			}
			probe := &journalSealProbe{p, make(chan struct{})}
			w.process = probe
			w.deadline = time.Now().Add(12*time.Second + 30*time.Millisecond)
			release := make(chan struct{})
			finished := make(chan error, 1)
			writes := 0
			go func() {
				finished <- w.saveServing(func() error {
					writes++
					if writes == 1 {
						<-release
					}
					return w.journal.save(w.state)
				})
			}()
			select {
			case <-probe.sealed:
			case <-time.After(2 * time.Second):
				close(release)
				<-finished
				t.Fatal("blocked journal prevented timely final barrier")
			}
			select {
			case <-finished:
				t.Fatal("save returned before original write joined")
			default:
			}
			close(release)
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if writes != 2 || w.state.LastCounters[0].Download != 73 {
				t.Fatal("sealed tail was not durably saved")
			}
			if pending && (w.state.Phase != "sealed_waiting_receipt" || ReportSHA(*w.state.Pending) != ReportSHA(original)) {
				t.Fatal("ordinary outbox overwritten")
			}
			if !pending && (w.state.Pending == nil || !w.state.Pending.Final) {
				t.Fatal("missing final outbox")
			}
			finalSequence := w.state.Sequence
			if pending {
				finalSequence++
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
			final := x.receipts[finalSequence]
			if !final.Final || final.Counters[0].Download != 73 || reopened.state.Epoch != nil {
				t.Fatal("sealed tail not settled after restart")
			}
			if pending && ReportSHA(x.receipts[original.Sequence]) != ReportSHA(original) {
				t.Fatal("ordinary receipt changed")
			}
		})
	}
}

type realJournalSealProbe struct {
	ownedProcess
	sealed chan struct{}
}

func (p *realJournalSealProbe) Final(ctx context.Context, nonce string) ([]ix.Counter, error) {
	values, err := p.ownedProcess.Final(ctx, nonce)
	close(p.sealed)
	return values, err
}
func TestRealJournalStallKeepsFinalAfterHardLeaseKill(t *testing.T) {
	f := newRealCounterFailureFixture(t)
	known := f.knownAndUnreportedTraffic(t)
	w, x := f.worker, f.exchange
	probe := &realJournalSealProbe{w.process, make(chan struct{})}
	w.process = probe
	if !probe.Renew(time.Now().Add(time.Second)) {
		t.Fatal("short native test lease refused")
	}
	// Accelerate the guard trigger independently from the native hard-kill clock.
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
	case <-probe.sealed:
	case <-time.After(2 * time.Second):
		close(release)
		<-done
		t.Fatal("final barrier missed")
	}
	time.Sleep(1200 * time.Millisecond)
	alive := probe.Alive()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("native hard lease did not stop test engine")
	}
	if w.state.Pending == nil || !w.state.Pending.Final || w.state.Pending.Counters[0].Download <= known.Counters[0].Download {
		t.Fatal("unreported final tail lost")
	}
	final := *w.state.Pending
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
	if ReportSHA(x.receipts[final.Sequence]) != ReportSHA(final) || reopened.state.Epoch != nil {
		t.Fatal("exact final replay failed")
	}
}
