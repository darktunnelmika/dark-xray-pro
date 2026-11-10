package business

import (
	"context"
	"errors"
	"testing"
)

func TestManagedInboundIdleReportsAvoidAccountWrites(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "idle-write-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "idle-write-epoch-0001")
	// Observe actual SQL writes, not just equal final balances.
	for _, stmt := range []string{
		"CREATE TABLE test_usage_account_writes(n INTEGER)",
		"CREATE TRIGGER test_usage_account_write AFTER UPDATE OF uplink,downlink ON pro_usage_accounts BEGIN INSERT INTO test_usage_account_writes VALUES(1); END",
	} {
		if _, err := f.s.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	f.ready(t, b, e)
	for _, report := range []struct {
		seq   uint64
		final bool
	}{{2, false}, {3, false}, {4, true}} {
		r := executionReport(b, e, report.seq, 17, 31, report.final)
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
			t.Fatal(err)
		}
		replay, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r)
		if err != nil || !replay.Replayed {
			t.Fatal("lost replay receipt", replay, err)
		}
	}
	var writes, receipts, deltas int
	if err := f.s.db.QueryRow("SELECT COUNT(*) FROM test_usage_account_writes").Scan(&writes); err != nil || writes != 1 {
		t.Fatal("idle reports rewrote account totals", writes, err)
	}
	if err := f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=?", e.EpochID).Scan(&receipts); err != nil || receipts != 4 {
		t.Fatal("idle receipt lost", receipts, err)
	}
	if err := f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_deltas WHERE epoch_id=?", e.EpochID).Scan(&deltas); err != nil || deltas != 3*len(b.Principals) {
		t.Fatal("baseline, changed or final delta evidence changed", deltas, err)
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Uplink != 17 || account.Downlink != 31 {
		t.Fatal("accounting changed", account, err)
	}
	var state string
	var seq uint64
	if err := f.s.db.QueryRow("SELECT state,sequence FROM inbound_execution_epochs WHERE epoch_id=?", e.EpochID).Scan(&state, &seq); err != nil || state != "sealed" || seq != 4 {
		t.Fatal("idle final did not seal", state, seq, err)
	}
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 5, 17, 31, false)); !errors.Is(err, ErrConflict) {
		t.Fatal("sealed epoch reopened", err)
	}
}

func TestManagedInboundIdleReportsKeepQuotaAndConflictChecks(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "idle-quota-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "idle-quota-epoch-0001")
	f.ready(t, b, e)
	for _, seq := range []uint64{2, 3} {
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, seq, 1<<20, 0, false)); err != nil {
			t.Fatal(err)
		}
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Eligible || account.Uplink != 1<<20 {
		t.Fatal("quota enforcement changed", account, err)
	}
	if err := f.s.InboundExecutionAuthority(ctx, f.key, b.NodeID, b.InstallationID, b.Generation); !errors.Is(err, ErrConflict) {
		t.Fatal("exhausted quota gained authority", err)
	}
	for _, bad := range []struct{ seq, up uint64 }{{3, (1 << 20) + 1}, {4, (1 << 20) - 1}, {5, 1 << 20}} {
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, bad.seq, bad.up, 0, false)); !errors.Is(err, ErrConflict) {
			t.Fatal("accepted conflicting or out-of-order counters", bad, err)
		}
	}
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 4, 1<<20, 0, true)); err != nil {
		t.Fatal("exhausted idle final refused", err)
	}
}
