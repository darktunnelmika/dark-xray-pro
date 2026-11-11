//go:build linux

package inboundruntime

import (
	"context"
	"os"
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
			// Exercise the observed 30-second first-write stall with the
			// production lease length. Other runs use the accelerated guard.
			labStall := pending && os.Getenv("DARK_PRO_TEST_30S_JOURNAL") == "1"
			sealTimeout := 2 * time.Second
			if labStall {
				w.deadline = time.Now().Add(30 * time.Second)
				sealTimeout = 25 * time.Second
			}
			start := time.Now()
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
			case <-time.After(sealTimeout):
				close(release)
				<-finished
				t.Fatal("blocked journal prevented timely final barrier")
			}
			select {
			case <-finished:
				t.Fatal("save returned before original write joined")
			default:
			}
			if labStall {
				time.Sleep(time.Until(start.Add(30 * time.Second)))
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

type renewalDuringSaveExchange struct {
	*fixtureExchange
	worker *Worker
	mode string
}
func (x *renewalDuringSaveExchange) Poll(ctx context.Context, in PollInput) (*Delivery, error) {
	if x.mode == "timeout" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if x.mode == "absent" { return nil, nil }
	d := *x.delivery
	if x.mode == "stale" { return &d, nil }
	now := time.Now()
	d.Lease.Issued = now.UnixNano()
	d.Lease.Expires = now.Add(30*time.Second).UnixNano()
	d.Lease, _ = SignLease(x.worker.cfg.Key, d.Lease)
	if x.mode == "tampered" { d.Lease.Signature = "invalid" }
	return &d, nil
}
func TestJournalWaitRenewalRequiresFreshScopedAuthority(t *testing.T) {
	for _, mode := range []string{"fresh", "stale", "absent", "tampered", "timeout", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			w, p, x := fixtureWorker(t, "active")
			exchange := &renewalDuringSaveExchange{x,w,mode}
			w.deadline = time.Unix(0,x.delivery.Lease.Expires)
			if mode == "fresh" { w.deadline = time.Now().Add(17*time.Second) }
			ctx,cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" { cancel() }
			before := w.deadline
			ok := w.renewWhileSaving(ctx,exchange)
			if ok != (mode=="fresh") { t.Fatalf("unexpected renewal %v",ok) }
			if !ok && (!w.deadline.Equal(before) || p.renewals != 0) {
				t.Fatal("rejected authority extended execution")
			}
		})
	}
}
func TestThirtySecondFirstWriteWithFreshAuthority(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_30S_JOURNAL")!="1" { t.Skip("explicit 30-second journal acceptance") }
	w,p,x := fixtureWorker(t,"active")
	p.values[0].Upload=41
	w.state.Sequence++
	r:=w.makeReport(p.values,false)
	w.state.Pending=&r
	w.state.LastCounters=p.values
	w.deadline=time.Now().Add(30*time.Second)
	exchange:=&renewalDuringSaveExchange{x,w,"fresh"}
	w.journalRenewal=func() bool { return w.renewWhileSaving(context.Background(),exchange) }
	writes:=0
	err:=w.saveServing(func() error {
		writes++
		if writes==1 {time.Sleep(30*time.Second)}
		return w.journal.save(w.state)
	})
	if err!=nil {t.Fatal(err)}
	if writes!=1 || p.renewals!=1 || p.finals!=0 || p.stops!=0 || time.Until(w.deadline)<=0 {
		t.Fatal("fresh authority failed to preserve the active process")
	}
	if ReportSHA(*w.state.Pending)!=ReportSHA(r) { t.Fatal("ordinary report changed") }
	if err=w.sendPending(context.Background(),x);err!=nil {t.Fatal(err)}
	if len(x.receipts)!=1 || ReportSHA(x.receipts[r.Sequence])!=ReportSHA(r) {t.Fatal("receipt mismatch")}
}
