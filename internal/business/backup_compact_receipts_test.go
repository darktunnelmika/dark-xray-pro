package business

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestCompactOfflineReceiptsPreservesReplayAccountingAndFinal(t *testing.T) {
	for _, sealed := range []bool{false, true} {
		name := "unsettled"
		if sealed {
			name = "settled"
		}
		t.Run(name, func(t *testing.T) {
			f := newExecutionFixture(t)
			defer f.s.Close()
			ctx := context.Background()
			_, err := f.s.BindInboundExecutionClients(ctx, f.key, "owner", 501, ix.BindingsInput{
				EventID: "compact-bind-0001", ExpectedRevision: 0, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Compact backup test",
			})
			if err != nil {
				t.Fatal(err)
			}
			f.command(t, "compact-activate-0001", "activate")
			b := f.bundle(t)
			e := f.birth(t, b, "compact-epoch-0001")
			baseline := executionReport(b, e, 1, 0, 0, false)
			sample := executionReport(b, e, 2, 100, 200, false)
			final := executionReport(b, e, 3, 100, 200, true)
			reports := []ix.UsageReport{baseline, sample}
			if sealed {
				reports = append(reports, final)
			}
			for _, r := range reports {
				if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
					t.Fatal(err)
				}
			}
			var original, terminal []byte
			if err = f.s.db.QueryRow("SELECT report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=1", e.EpochID).Scan(&original); err != nil {
				t.Fatal(err)
			}
			if sealed {
				if err = f.s.db.QueryRow("SELECT report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=3", e.EpochID).Scan(&terminal); err != nil {
					t.Fatal(err)
				}
			}
			dst := filepath.Join(t.TempDir(), "snapshot.sqlite")
			if _, err = f.s.db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(dst, 0600); err != nil {
				t.Fatal(err)
			}
			if err = CompactOfflineExecutionReceipts(ctx, dst, bytes.Repeat([]byte{0x62}, 32)); err == nil {
				t.Fatal("wrong key accepted")
			}
			if err = CompactOfflineExecutionReceipts(ctx, dst, f.key); err != nil {
				t.Fatal(err)
			}
			if err = CompactOfflineExecutionReceipts(ctx, dst, f.key); err != nil {
				t.Fatal("not idempotent", err)
			}
			restored, err := Open(dst)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			var actual []byte
			if err = restored.db.QueryRow("SELECT report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=1", e.EpochID).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if sealed {
				if len(actual) >= len(original) {
					t.Fatal("body was not reduced")
				}
				var digest string
				if openPlanValue(f.key, executionAAD("usage-report", e.EpochID, 1), actual, &digest) != nil || digest != ix.Digest(baseline) {
					t.Fatal("invalid replay proof")
				}
				if err = restored.db.QueryRow("SELECT report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=3", e.EpochID).Scan(&actual); err != nil || !bytes.Equal(actual, terminal) {
					t.Fatal("final proof changed", err)
				}
			} else if !bytes.Equal(actual, original) {
				t.Fatal("unsettled body changed")
			}
			for _, r := range reports {
				got, err := restored.AcceptInboundExecutionUsage(ctx, f.key, r)
				if err != nil || !got.Replayed {
					t.Fatal("exact replay failed", err)
				}
			}
			altered := sample
			altered.Counters = append([]ix.Counter(nil), sample.Counters...)
			altered.Counters[0].Upload++
			if _, err = restored.AcceptInboundExecutionUsage(ctx, f.key, altered); !errors.Is(err, ErrConflict) {
				t.Fatal("conflicting replay accepted", err)
			}
			var up, down int64
			if err = restored.db.QueryRow("SELECT uplink,downlink FROM pro_usage_accounts WHERE client_id=?", f.client.ID).Scan(&up, &down); err != nil || up != 100 || down != 200 {
				t.Fatal("totals changed", up, down, err)
			}
			if err = restored.db.QueryRow("SELECT SUM(upload),SUM(download) FROM inbound_execution_usage_deltas WHERE client_id=?", f.client.ID).Scan(&up, &down); err != nil || up != 100 || down != 200 {
				t.Fatal("deltas changed", up, down, err)
			}
			if _, err = restored.db.Exec("UPDATE inbound_execution_usage_receipts SET sha256=sha256"); err == nil {
				t.Fatal("immutable trigger missing")
			}
			if err = f.s.db.QueryRow("SELECT report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=1", e.EpochID).Scan(&actual); err != nil || !bytes.Equal(actual, original) {
				t.Fatal("source changed", err)
			}
		})
	}
}
