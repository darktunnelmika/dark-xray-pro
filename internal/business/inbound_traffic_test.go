package business

import (
	"context"
	"testing"
	"time"
)

func TestInboundPathTrafficDoesNotDoubleChargeAndSurvivesDeletion(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.plan.EventID = "traffic-enable-meters-0001"
	f.plan.ExpectedPlanRevision = 1
	f.plan.ExpectedInboundRevision = 1
	f.plan.Profile.Spec.PathTraffic = true
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, f.plan); err != nil {
		t.Fatal(err)
	}
	f.command(t, "traffic-activate-0001", "activate")
	b := f.bundle(t)
	var billable, observational int
	for _, p := range b.Principals {
		if p.Billable {
			billable++
		} else {
			observational++
		}
	}
	if billable != 1 || observational != 1 {
		t.Fatalf("need separate customer and listener meters: %d %d", billable, observational)
	}
	e := f.birth(t, b, "traffic-epoch-0001")
	f.ready(t, b, e)
	r := executionReport(b, e, 2, 100, 200, false)
	for i, p := range b.Principals {
		if !p.Billable {
			r.Counters[i].Upload = 120
			r.Counters[i].Download = 250
		}
	}
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
		t.Fatal(err)
	}
	usage, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || usage.Uplink != 100 || usage.Downlink != 200 {
		t.Fatal("observational bytes charged or replay duplicated", usage, err)
	}
	got, err := f.s.InboundTraffic(ctx, f.key, 501)
	if err != nil || len(got.Paths) != 1 {
		t.Fatal(got, err)
	}
	p := got.Paths[0]
	if !p.Measured || !p.Fresh || p.Upload != 120 || p.Download != 250 {
		t.Fatal("incorrect committed path counters", p)
	}
	history, err := f.s.InboundTrafficHistory(ctx, 501, "1h")
	var historyUpload, historyDownload uint64
	for _, bucket := range history.Buckets {
		historyUpload += bucket.Upload
		historyDownload += bucket.Download
	}
	if err != nil || len(history.Buckets) == 0 || historyUpload != 120 || historyDownload != 250 {
		t.Fatal("history duplicated customer or replay", history, err)
	}
	// Traffic reads must proceed while the sole writer is reserved by other work.
	conn, err := f.s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	got, err = f.s.InboundTraffic(readCtx, f.key, 501)
	cancel()
	conn.Close()
	if err != nil || len(got.Paths) != 1 {
		t.Fatal("traffic blocked on writer", err)
	}
	// A closed epoch cannot look live, and its confirmed totals remain readable.
	final := executionReport(b, e, 3, 100, 200, true)
	final.Counters = r.Counters
	final.FinalProofSHA = executionFinalSHA(final)
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final); err != nil {
		t.Fatal(err)
	}
	got, err = f.s.InboundTraffic(ctx, f.key, 501)
	if err != nil || len(got.Paths) != 1 || got.Paths[0].Fresh || got.Paths[0].Upload != 120 {
		t.Fatal("closed epoch became live or lost usage", got, err)
	}
	f.command(t, "traffic-delete-0001", "archive")
	visible, err := f.s.ListVisibleInbounds(ctx, f.key)
	if err != nil || len(visible) != 0 {
		t.Fatal("deleted inbound remains selectable", visible, err)
	}
	all, err := f.s.ListInbounds(ctx, f.key)
	if err != nil || len(all) != 1 {
		t.Fatal("audit identity deleted", all, err)
	}
	usage, err = f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || usage.Uplink != 100 || usage.Downlink != 200 {
		t.Fatal("deletion changed accounting", usage, err)
	}
	var receipts int
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=?", e.EpochID).Scan(&receipts); err != nil || receipts != 3 {
		t.Fatal("deletion removed receipts", receipts, err)
	}
}

