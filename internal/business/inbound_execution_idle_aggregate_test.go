package business

import (
	"context"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

func TestManagedInboundIdleAggregateAvoidsWrites(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	in := f.plan
	in.EventID = "execution-tunnel-plan-0001"
	in.Profile = InboundInput{ID: 502, Name: "Owner tunnel", Spec: &inboundspec.Spec{Version: 1, Protocol: "tunnel", Network: "tcp", Security: "none", Settings: map[string]any{"rewriteAddress": "example.org", "rewritePort": 443, "allowedNetwork": "tcp"}}}
	in.Nodes = append([]NativeInboundPlanNode{}, in.Nodes...)
	in.Nodes[0].DirectPort = 23185
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, in); err != nil {
		t.Fatal(err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 502)
	if err != nil {
		t.Fatal(err)
	}
	cmd := ix.CommandInput{EventID: "execution-aggregate-activate-0001", Action: "activate", ExpectedRevision: 0, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: []string{}, AccountingMode: ix.AccountingOwnerAggregate, Reason: "Owner aggregate listener"}
	p, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", 502, cmd)
	if err != nil || !p.Valid {
		t.Fatal(p, err)
	}
	cmd.ReviewSHA = p.ReviewSHA
	if _, err = f.s.QueueInboundExecution(ctx, f.key, "owner", 502, cmd); err != nil {
		t.Fatal(err)
	}
	b := f.bundle(t)
	if len(b.Principals) != 1 || b.Principals[0].Billable || b.Principals[0].ClientID != "" || b.Principals[0].Role != "owner_aggregate" {
		t.Fatal("fabricated retail attribution", b.Principals)
	}
	e := f.birth(t, b, "epoch-aggregate-0001")

	defer f.s.Close()
	for _, stmt := range []string{
		"CREATE TABLE test_aggregate_writes(kind TEXT)",
		"CREATE TRIGGER test_aggregate_insert AFTER INSERT ON inbound_execution_aggregate_totals BEGIN INSERT INTO test_aggregate_writes VALUES('insert'); END",
		"CREATE TRIGGER test_aggregate_update AFTER UPDATE ON inbound_execution_aggregate_totals BEGIN INSERT INTO test_aggregate_writes VALUES('update'); END",
	} {
		if _, err := f.s.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	f.ready(t, b, e)
	// A baseline must still create the aggregate row even though its counters are zero.
	var inserts, updates int
	if err := f.s.db.QueryRow("SELECT COUNT(*) FROM test_aggregate_writes WHERE kind='insert'").Scan(&inserts); err != nil || inserts != 1 {
		t.Fatal("baseline aggregate missing", inserts, err)
	}
	for _, x := range []struct {
		seq, up, down uint64
		final         bool
	}{
		{2, 0, 0, false}, {3, 47, 83, false}, {4, 47, 83, false}, {5, 47, 90, false}, {6, 47, 90, true},
	} {
		r := executionReport(b, e, x.seq, x.up, x.down, x.final)
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
			t.Fatal(err)
		}
		receipt, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r)
		if err != nil || !receipt.Replayed {
			t.Fatal("replay lost", receipt, err)
		}
	}
	if err := f.s.db.QueryRow("SELECT COUNT(*) FROM test_aggregate_writes WHERE kind='update'").Scan(&updates); err != nil || updates != 2 {
		t.Fatal("idle aggregate rewritten", updates, err)
	}
	var up, down int64
	if err := f.s.db.QueryRow("SELECT upload,download FROM inbound_execution_aggregate_totals").Scan(&up, &down); err != nil || up != 47 || down != 90 {
		t.Fatal("aggregate changed", up, down, err)
	}
	for _, table := range []string{"inbound_execution_usage_receipts", "inbound_execution_usage_deltas"} {
		var n int
		want := 6
		if table == "inbound_execution_usage_deltas" {
			want = 4
		}
		if err := f.s.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE epoch_id=?", e.EpochID).Scan(&n); err != nil || n != want {
			t.Fatal("audit evidence lost", table, n, err)
		}
	}
	var finalState string
	var seq int
	if err := f.s.db.QueryRow("SELECT state,sequence FROM inbound_execution_epochs WHERE epoch_id=?", e.EpochID).Scan(&finalState, &seq); err != nil || finalState != "sealed" || seq != 6 {
		t.Fatal("final not durable", finalState, seq, err)
	}
	usage, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || usage.Uplink != 0 || usage.Downlink != 0 {
		t.Fatal("aggregate charged customer", usage, err)
	}
}
