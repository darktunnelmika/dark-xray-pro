//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func fixtureClearance(t *testing.T, w *Worker) ix.EpochClearance {
	t.Helper()
	e := w.state.Epoch
	c, err := ix.SealClearance(w.cfg.Key, ix.EpochClearance{Version: 1, EventID: "fixture-owner-loss-0001", EpochID: e.EpochID, NodeID: e.NodeID, InstallationID: e.InstallationID, Generation: e.Generation, EpochSHA: ix.Digest(*e), KnownSequence: w.state.Sequence, KnownCountersSHA: ix.Digest(w.state.LastCounters), Policy: "owner_loss", CreatedUnixNano: time.Now().UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestWorkerOwnerLossWaitsForKnownReceiptAndPersistsExactClearance(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	w.state.Sequence = 2
	w.state.LastCounters = []ix.Counter{{PrincipalID: "customer-0001", Upload: 31, Download: 47}}
	r := w.makeReport(w.state.LastCounters, false)
	w.state.Pending = &r
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	c := fixtureClearance(t, w)
	x.delivery, x.loseOrdinary, p.dead = &Delivery{Clearance: &c}, true, true
	if err := w.Step(context.Background(), x); !errors.Is(err, errLostReceipt) {
		t.Fatal(err)
	}
	if w.state.Phase != "uncertain" || w.state.Pending == nil || w.state.Resolution != nil || x.polls != 0 || len(x.observations) != 1 || x.observations[0].InitialEpoch == nil {
		t.Fatal("owner loss erased known bytes before their exact receipt")
	}
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if w.state.Epoch != nil || w.state.Phase != "idle" || w.state.Resolution == nil || w.state.Resolution.Clearance.Signature != c.Signature || w.process != nil || p.activations != 0 || len(x.receipts) != 1 || x.receipts[2].Counters[0].Upload != 31 || x.pollInputs[0].UncertainEpochID != c.EpochID {
		t.Fatal("clearance changed accounting evidence or became admission authority")
	}
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.state.Resolution == nil || reopened.state.Epoch != nil || reopened.state.Phase != "idle" {
		t.Fatal("owner decision was not durable across restart")
	}
	if err = reopened.Step(context.Background(), x); !errors.Is(err, ErrProof) {
		t.Fatal("ordinary executor accepted clearance-only packet as a lease", err)
	}
	x.delivery = stoppedDelivery(t, reopened)
	if err = reopened.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if reopened.state.Generation != 2 || reopened.state.Resolution != nil || reopened.process != nil {
		t.Fatal("a separate stopped generation did not supersede cleared local state")
	}
}

func TestWorkerOwnerLossRejectsForeignOrTamperedClearance(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*ix.EpochClearance)
	}{
		{"different_epoch", func(c *ix.EpochClearance) { c.EpochID = "another-epoch" }},
		{"different_incarnation", func(c *ix.EpochClearance) { c.EpochSHA = strings.Repeat("d", 64) }},
		{"different_generation", func(c *ix.EpochClearance) { c.Generation++ }},
		{"different_installation", func(c *ix.EpochClearance) { c.InstallationID = strings.Repeat("c", 32) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, p, x := fixtureWorker(t, "active")
			c := fixtureClearance(t, w)
			tc.edit(&c)
			var err error
			c, err = ix.SealClearance(w.cfg.Key, c)
			if err != nil {
				t.Fatal(err)
			}
			x.delivery, p.dead = &Delivery{Clearance: &c}, true
			if err = w.Step(context.Background(), x); err == nil {
				t.Fatal("a different valid owner decision cleared this epoch")
			}
			if w.state.Phase != "uncertain" || w.state.Epoch == nil || w.state.Resolution != nil || p.activations != 0 {
				t.Fatal("wrong-scope clearance changed the failed incarnation")
			}
		})
	}
}

func TestWorkerUnregisteredClosedFinalCarriesBirthIdentity(t *testing.T) {
	w, p, x := fixtureWorker(t, "prepared")
	w.deadline = time.Now().Add(5 * time.Second)
	x.loseFinal = true
	if err := w.Step(context.Background(), x); !errors.Is(err, errLostReceipt) {
		t.Fatal(err)
	}
	r := w.state.Pending
	if p.activations != 0 || p.finals != 1 || x.registerCalls != 0 || r == nil || r.Sequence != 1 || !r.Final || r.InitialEpoch == nil || ix.Digest(*r.InitialEpoch) != ix.Digest(*w.state.Epoch) || r.Counters[0].Upload != 0 || r.FinalNonce == "" {
		t.Fatal("unregistered closed birth lacked its genuine zero-final identity")
	}
	sha := ReportSHA(*r)
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
	if len(x.receipts) != 1 || ReportSHA(x.receipts[1]) != sha || reopened.state.Epoch != nil || reopened.state.Generation != 2 {
		t.Fatal("lost unregistered-final response changed or stranded settlement")
	}
}

func TestNativeLeaseExpiryUsesCurrentRenewalNotWallClock(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &Native{cmd: cmd, done: make(chan struct{}), leaseSerial: 2, deadline: time.Now().Add(time.Hour), dir: t.TempDir()}
	go func() { _ = cmd.Wait(); close(p.done) }()
	defer p.Stop()
	p.expireLease(1)
	if p.ending {
		t.Fatal("an obsolete timer killed a renewed process")
	}
	// The current timer has already elapsed monotonically. A changed wall
	// clock must not turn expiration into a permanent no-op.
	p.expireLease(2)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("wall-clock backstep stranded an expired admission lease")
	}
}
