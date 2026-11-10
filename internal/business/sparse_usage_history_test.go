package business

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"os"
	"path/filepath"
	"testing"
)

func TestSparseIntermediateZeroHistoryRestoresAndContinues(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "sparse-history-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "sparse-history-epoch-0001")
	f.ready(t, b, e)
	reports := []ix.UsageReport{executionReport(b, e, 1, 0, 0, false)}
	for seq := uint64(2); seq <= 64; seq++ {
		r := executionReport(b, e, seq, 0, 0, false)
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
			t.Fatal(err)
		}
		reports = append(reports, r)
	}
	var receipts, deltas int
	if err := f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=?", e.EpochID).Scan(&receipts); err != nil || receipts != 64 {
		t.Fatal("idle replay/freshness receipts lost", receipts, err)
	}
	if err := f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_deltas WHERE epoch_id=?", e.EpochID).Scan(&deltas); err != nil || deltas != len(b.Principals) {
		t.Fatal("idle principals multiplied ledger rows", deltas, err)
	}
	for seq := uint64(65); seq <= 66; seq++ {
		r := executionReport(b, e, seq, 17, 31, false)
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
			t.Fatal(err)
		}
		reports = append(reports, r)
	}
	var created int64
	if err := f.s.db.QueryRow("SELECT created FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=66", e.EpochID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executionAccountingFreshUntil(ctx, tx, e.NodeID, e.InstallationID, e.Generation, e.EpochID, created)
	tx.Rollback()
	if err != nil {
		t.Fatal("sparse idle report lost accounting freshness", err)
	}
	dst := filepath.Join(t.TempDir(), "snapshot.sqlite")
	if _, err = f.s.db.Exec("VACUUM INTO ?", dst); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dst, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateOfflineBackup(ctx, dst); err != nil {
		t.Fatal(err)
	}
	if err = ValidateOfflineBackupAssets(ctx, dst, f.key); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range reports {
		got, err := restored.AcceptInboundExecutionUsage(ctx, f.key, r)
		if err != nil || !got.Replayed {
			t.Fatal("sparse receipt replay changed accounting", r.Sequence, err)
		}
	}
	bad := executionReport(b, e, 50, 1, 0, false)
	if _, err = restored.AcceptInboundExecutionUsage(ctx, f.key, bad); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting idle replay accepted", err)
	}
	final := executionReport(b, e, 67, 17, 31, true)
	if _, err = restored.AcceptInboundExecutionUsage(ctx, f.key, final); err != nil {
		t.Fatal("restored open epoch did not settle", err)
	}
	replay, err := restored.AcceptInboundExecutionUsage(ctx, f.key, final)
	if err != nil || !replay.Replayed {
		t.Fatal("final replay lost", err)
	}
	if err = restored.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_deltas WHERE epoch_id=?", e.EpochID).Scan(&deltas); err != nil || deltas != 3*len(b.Principals) {
		t.Fatal("baseline/nonzero/final evidence changed", deltas, err)
	}
	var up, down int64
	if err = restored.db.QueryRow("SELECT uplink,downlink FROM pro_usage_accounts WHERE client_id=?", f.client.ID).Scan(&up, &down); err != nil || up != 17 || down != 31 {
		t.Fatal("sparse receipt charged again", up, down, err)
	}
	if err = restored.db.QueryRow("SELECT SUM(upload),SUM(download) FROM inbound_execution_usage_deltas WHERE client_id=?", f.client.ID).Scan(&up, &down); err != nil || up != 17 || down != 31 {
		t.Fatal("sparse ledger totals changed", up, down, err)
	}
	var enc []byte
	if err = restored.db.QueryRow("SELECT report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=67", e.EpochID).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	var stored ix.UsageReport
	if err = openPlanValue(f.key, executionAAD("usage-report", e.EpochID, 67), enc, &stored); err != nil || ix.Digest(stored) != ix.Digest(final) {
		t.Fatal("full final proof lost", err)
	}
	var originalSequence int
	if err = f.s.db.QueryRow("SELECT sequence FROM inbound_execution_epochs WHERE epoch_id=?", e.EpochID).Scan(&originalSequence); err != nil || originalSequence != 66 {
		t.Fatal("restore mutated source", originalSequence, err)
	}
}
