package corebarrierv2

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

type testFinalCapture struct{ published chan FinalRecord }

func (c *testFinalCapture) Binding() string   { return strings.Repeat("b", 64) }
func (c *testFinalCapture) ConfigSHA() string { return strings.Repeat("c", 64) }
func (c *testFinalCapture) Publish(r FinalRecord) error {
	select {
	case c.published <- r:
	default:
	}
	return nil
}
func leasedGate(t *testing.T, soft time.Duration) (*gate, *testFinalCapture) {
	t.Helper()
	g := newGate()
	g.ready = true
	c := &testFinalCapture{make(chan FinalRecord, 1)}
	g.lease = &nativeLease{hard: time.Now().Add(LeaseFinalRunway + soft), capture: c, read: func() map[string]int64 { return map[string]int64{"positive": 37} }}
	t.Cleanup(func() {
		g.mu.Lock()
		if g.lease.timer != nil {
			g.lease.timer.Stop()
		}
		g.mu.Unlock()
	})
	return g, c
}
func TestAutonomousLeaseClosesAndJoinsBeforePublishingFinal(t *testing.T) {
	g, c := leasedGate(t, time.Second)
	nonce := strings.Repeat("a", 64)
	if e := g.activate(nonce); e != nil {
		t.Fatal(e)
	}
	interrupted := make(chan struct{})
	_, finish, ok := g.enter(context.Background(), func() { close(interrupted) })
	if !ok {
		t.Fatal("admission refused before lease")
	}
	g.mu.Lock()
	g.armLeaseLocked()
	g.mu.Unlock()
	select {
	case <-interrupted:
	case <-time.After(3 * time.Second):
		t.Fatal("independent lease did not close owned work")
	}
	select {
	case <-c.published:
		t.Fatal("counter capture preceded I/O join")
	default:
	}
	if _, release, ok := g.enter(context.Background(), func() {}); ok {
		release()
		t.Fatal("expired lease admitted work")
	}
	finish()
	select {
	case r := <-c.published:
		if !r.Final || !r.Autonomous || r.Counters["positive"] != 37 {
			t.Fatal("genuine final lost", r)
		}
	case <-time.After(time.Second):
		t.Fatal("no joined final capture")
	}
	got, e := g.snapshot(context.Background(), nonce, func() map[string]int64 { t.Fatal("final was reread"); return nil })
	if e != nil || got["positive"] != 37 {
		t.Fatal(e, got)
	}
	if g.renewAutonomousLease(context.Background(), time.Now().Add(30*time.Second).UnixNano(), strings.Repeat("d", 64)) == nil {
		t.Fatal("sealed core was revived")
	}
}
func TestAutonomousSnapshotBindsOnlyTheFirstReceiptNonce(t *testing.T) {
	for _, auto := range []bool{true, false} {
		g, c := leasedGate(t, time.Second)
		first := strings.Repeat("a", 64)
		second := strings.Repeat("d", 64)
		nonce := first
		if auto {
			nonce = ""
		}
		values, e := g.snapshotInternal(context.Background(), nonce, g.lease.read, auto)
		if e != nil || values["positive"] != 37 {
			t.Fatal(e)
		}
		published := <-c.published
		if published.Autonomous != auto || published.Nonce != nonce {
			t.Fatal("wrong capture binding")
		}
		for i := 0; i < 2; i++ {
			if _, e = g.snapshot(context.Background(), first, nil); e != nil {
				t.Fatal("exact receipt retry failed", e)
			}
		}
		if _, e = g.snapshot(context.Background(), second, nil); e == nil {
			t.Fatal("second nonce accepted")
		}
		if _, e = g.snapshotInternal(context.Background(), "", nil, true); e != nil {
			t.Fatal("independent guard could not join existing final", e)
		}
	}
}
func TestRenewedNativeLeaseIgnoresItsOldTimer(t *testing.T) {
	g, _ := leasedGate(t, time.Second)
	if e := g.activate(strings.Repeat("a", 64)); e != nil {
		t.Fatal(e)
	}
	g.mu.Lock()
	g.armLeaseLocked()
	old := g.lease.serial
	g.mu.Unlock()
	deadline := time.Now().Add(30 * time.Second).UnixNano()
	nonce := strings.Repeat("e", 64)
	if e := g.renewAutonomousLease(context.Background(), deadline, nonce); e != nil {
		t.Fatal(e)
	}
	g.sealLease(old)
	if _, active, sealed := g.health(); !active || sealed {
		t.Fatal("stale deadline closed renewed authority")
	}
	if e := g.renewAutonomousLease(context.Background(), deadline, nonce); e != nil {
		t.Fatal("exact renewal retry failed", e)
	}
	if e := g.renewAutonomousLease(context.Background(), deadline-1, nonce); e == nil {
		t.Fatal("renewal identity changed")
	}
	if e := g.renewAutonomousLease(context.Background(), time.Now().Add(32*time.Second).UnixNano(), strings.Repeat("f", 64)); e == nil {
		t.Fatal("lease authority extended beyond protocol bound")
	}
}
func TestAutonomousExpiryWithoutSnapshotterDoesNotInventZero(t *testing.T) {
	g, c := leasedGate(t, time.Second)
	g.lease.read = nil
	if _, e := g.snapshotInternal(context.Background(), "", nil, true); e == nil {
		t.Fatal("missing native snapshot became zero proof")
	}
	if _, active, sealed := g.health(); active || sealed {
		t.Fatal("missing capture changed authority or proof")
	}
	select {
	case <-c.published:
		t.Fatal("published fabricated zero")
	default:
	}
}
func TestInvalidLeaseCaptureConfigurationNeverOpensAdmission(t *testing.T) {
	t.Setenv("DARK_PRO_NATIVE_FINAL_FD", "1")
	t.Setenv("DARK_PRO_NATIVE_FINAL_BINDING", strings.Repeat("a", 64))
	t.Setenv("DARK_PRO_NATIVE_CONFIG_SHA256", strings.Repeat("b", 64))
	t.Setenv("DARK_PRO_NATIVE_LEASE_DEADLINE", strconv.FormatInt(time.Now().Add(30*time.Second).UnixNano(), 10))
	g := newGate()
	g.configureLeaseFromEnvironment()
	g.ready = true
	if e := g.activate(strings.Repeat("c", 64)); e == nil {
		t.Fatal("invalid control setup opened admission")
	}
}
