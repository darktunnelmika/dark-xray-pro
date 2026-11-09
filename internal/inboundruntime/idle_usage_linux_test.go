//go:build linux

package inboundruntime

import (
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

func TestWorkerIdleUsageRetainsJournalButPollsAndReportsChanges(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	before := readIdleJournalFile(t, w)
	calls, polls, renewals := x.usageCalls, x.polls, p.renewals
	seq := w.state.Sequence
	for i := 0; i < 3; i++ {
		if err := w.Step(context.Background(), x); err != nil {
			t.Fatal(err)
		}
	}
	if x.usageCalls != calls || x.polls != polls+3 || p.renewals != renewals+3 || w.state.Sequence != seq {
		t.Fatal("idle accounting skipped authority checks or rewrote receipts")
	}
	requireIdleJournalUnchanged(t, before, readIdleJournalFile(t, w))
	p.values = append([]ix.Counter(nil), p.values...)
	p.values[0].Download++
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if x.usageCalls != calls+1 || w.state.Sequence != seq+1 || w.state.LastCounters[0].Download != p.values[0].Download {
		t.Fatal("new bytes were delayed")
	}
	// Force the monotonic elapsed interval without sleeping in the test.
	w.lastUsageReceiptAt = time.Now().Add(-5 * time.Second)
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if x.usageCalls != calls+2 {
		t.Fatal("idle heartbeat not refreshed")
	}
	// A stop must seal and settle even immediately after an identical heartbeat.
	x.delivery = stoppedDelivery(t, w)
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if !w.state.FinalAccepted || p.finals != 1 {
		t.Fatal("idle suppression delayed final capture")
	}
}
func TestWorkerIdleUsageRequiresExactFreshEpochWitness(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if w.usageReportDue(p.values, now) {
		t.Fatal("fresh equal counters not coalesced")
	}
	if !w.usageReportDue(p.values, w.lastUsageReceiptAt.Add(5*time.Second)) {
		t.Fatal("heartbeat boundary")
	}
	if !w.usageReportDue(p.values, w.lastUsageReceiptAt.Add(-time.Second)) {
		t.Fatal("backward clock")
	}
	w.lastUsageReceiptEpoch = "different-epoch"
	if !w.usageReportDue(p.values, now) {
		t.Fatal("different epoch reused witness")
	}
	w.lastUsageReceiptEpoch = w.state.Epoch.EpochID
	w.lastUsageReceiptAt = time.Time{}
	if !w.usageReportDue(p.values, now) {
		t.Fatal("restart invented receipt freshness")
	}
}

type usageAttemptClock struct {
	Exchange
	entered time.Time
}

func (x *usageAttemptClock) Usage(ctx context.Context, r ix.UsageReport) (ix.UsageReceipt, error) {
	x.entered = time.Now()
	return x.Exchange.Usage(ctx, r)
}

func TestWorkerIdleUsageIntervalIncludesReceiptWait(t *testing.T) {
	w, p, next := fixtureWorker(t, "active")
	x := &usageAttemptClock{Exchange: next}
	before := time.Now()
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if x.entered.IsZero() || w.lastUsageReceiptAt.Before(before) || w.lastUsageReceiptAt.After(x.entered) {
		t.Fatal("idle interval started after usage delivery", before, x.entered, w.lastUsageReceiptAt)
	}
	// Advance only the decision clock, not wall time. At five seconds after
	// entering Usage the request and acknowledgement wait already count.
	if !w.usageReportDue(p.values, x.entered.Add(5*time.Second)) {
		t.Fatal("receipt wait extended the accounting heartbeat")
	}
	if w.state.Pending != nil || w.lastUsageReceiptEpoch != w.state.Epoch.EpochID {
		t.Fatal("heartbeat witness exists without an exact accepted receipt")
	}
}
