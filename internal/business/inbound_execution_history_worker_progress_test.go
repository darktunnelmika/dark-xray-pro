package business

import (
	"context"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestExecutionHistoryWorkerProgressWithAccounting(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "worker-progress-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "worker-progress-sealed-0001")
	reports := make([]ix.UsageReport, 0, 35)
	for seq := uint64(1); seq <= 35; seq++ {
		up, down := uint64(17), uint64(31)
		if seq == 1 {
			up, down = 0, 0
		}
		r := executionReport(b, e, seq, up, down, seq == 35)
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
			t.Fatal(err)
		}
		reports = append(reports, r)
	}
	seedLegacyZeroHistory(t, f, b, e, 3, 34)
	var ddl string
	if err := f.s.db.QueryRow("SELECT sql FROM sqlite_master WHERE name='inbound_execution_usage_receipts_no_update'").Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	tx, err := f.s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"DROP TRIGGER inbound_execution_usage_receipts_no_update", "UPDATE inbound_execution_usage_receipts SET created=1", ddl} {
		if _, err = tx.Exec(q); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	active := f.birth(t, b, "worker-progress-active-0001")
	worker, err := openExecutionHistoryMaintainer(f.path, f.s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.close()
	// Hold a DEFERRED accounting snapshot, not a SQLite write lock.
	// A second writer must not commit between this read and its upgrade.
	holder, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	var before int
	if err = holder.QueryRowContext(ctx, "SELECT COUNT(*) FROM inbound_execution_usage_deltas WHERE epoch_id=?", e.EpochID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1300 * time.Millisecond)
	var after int
	if err = holder.QueryRowContext(ctx, "SELECT COUNT(*) FROM inbound_execution_usage_deltas WHERE epoch_id=?", e.EpochID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != 35 || after != before {
		t.Fatal("accounting snapshot changed", before, after)
	}
	if _, err = holder.ExecContext(ctx, "UPDATE inbound_execution_epochs SET state=state WHERE epoch_id=?", e.EpochID); err != nil {
		t.Fatal("maintenance invalidated accounting snapshot", err)
	}
	if err = holder.Commit(); err != nil {
		t.Fatal(err)
	}
	for seq := uint64(1); seq <= 40; seq++ {
		up, down := seq*7, seq*11
		if seq == 1 {
			up, down = 0, 0
		}
		r := executionReport(b, active, seq, up, down, seq == 40)
		start := time.Now()
		if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
			t.Fatal("accounting during maintenance", err)
		}
		if time.Since(start) > time.Second {
			t.Fatal("accounting delayed by maintenance")
		}
		time.Sleep(100 * time.Millisecond)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var remaining int
		if err = f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_deltas WHERE epoch_id=?", e.EpochID).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker did not retry/progress: %d", remaining)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err = worker.close(); err != nil {
		t.Fatal(err)
	}
	// A new worker must safely rescan after losing its in-memory cursor.
	worker, err = openExecutionHistoryMaintainer(f.path, f.s.db)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	if err = worker.close(); err != nil {
		t.Fatal(err)
	}
	for _, r := range reports {
		result, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r)
		if err != nil || !result.Replayed {
			t.Fatal("pruned history lost replay", err)
		}
	}
	var up, down int64
	if err = f.s.db.QueryRow("SELECT SUM(upload),SUM(download) FROM inbound_execution_usage_deltas").Scan(&up, &down); err != nil || up != 297 || down != 471 {
		t.Fatal("ledger changed", up, down, err)
	}
	var receipts int
	if err = f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_receipts").Scan(&receipts); err != nil || receipts != 75 {
		t.Fatal("receipts changed", receipts, err)
	}
	if _, err = f.s.db.Exec("DELETE FROM inbound_execution_usage_deltas"); err == nil {
		t.Fatal("immutable trigger lost")
	}
}
