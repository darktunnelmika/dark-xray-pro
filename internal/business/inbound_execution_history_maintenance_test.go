package business

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestExecutionHistoryMaintenancePreservesAccounting(t *testing.T) {
	for _, mode := range []string{"old", "recent", "open", "uncertain", "hold", "liveness"} {
		t.Run(mode, func(t *testing.T) {
			f := newExecutionFixture(t)
			defer f.s.Close()
			ctx := context.Background()
			if _, err := f.s.BindInboundExecutionClients(ctx, f.key, "owner", 501, ix.BindingsInput{EventID: "zero-backup-bind-0001", ExpectedRevision: 0, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Sparse zero backup test"}); err != nil {
				t.Fatal(err)
			}
			f.command(t, "zero-activate-0001", "activate")
			b := f.bundle(t)
			e := f.birth(t, b, "zero-backup-epoch-0001")
			reports := []ix.UsageReport{
				executionReport(b, e, 1, 0, 0, false),
				executionReport(b, e, 2, 100, 200, false),
				executionReport(b, e, 3, 100, 200, false),
				executionReport(b, e, 4, 100, 200, mode != "open" && mode != "uncertain"),
			}
			for _, r := range reports {
				if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
					t.Fatal(err)
				}
			}
			// Age the fixture only; production receipt timestamps remain immutable.
			if mode != "recent" {
				var ddl string
				if err := f.s.db.QueryRow("SELECT sql FROM sqlite_master WHERE name='inbound_execution_usage_receipts_no_update'").Scan(&ddl); err != nil {
					t.Fatal(err)
				}
				tx, err := f.s.db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				for _, q := range []string{"DROP TRIGGER inbound_execution_usage_receipts_no_update", "UPDATE inbound_execution_usage_receipts SET created=created", ddl} {
					if q == "UPDATE inbound_execution_usage_receipts SET created=created" {
						_, err = tx.Exec("UPDATE inbound_execution_usage_receipts SET created=?", time.Now().Add(-48*time.Hour).UnixNano())
					} else {
						_, err = tx.Exec(q)
					}
					if err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "uncertain" {
				if _, err := f.s.db.Exec("UPDATE inbound_execution_epochs SET state='uncertain' WHERE epoch_id=?", e.EpochID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "hold" {
				if _, err := f.s.db.Exec("INSERT INTO inbound_execution_usage_holds VALUES(?,?,?,?)", e.EpochID, f.client.ID, "protected", time.Now().UnixNano()); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "liveness" {
				if _, err := f.s.db.Exec("INSERT INTO inbound_execution_liveness_holds VALUES(?,?,?,?,?)", e.EpochID, f.client.ID, 4, time.Now().UnixNano(), time.Now().UnixNano()); err != nil {
					t.Fatal(err)
				}
			}
			var terminal []byte
			if err := f.s.db.QueryRow("SELECT report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=4", e.EpochID).Scan(&terminal); err != nil {
				t.Fatal(err)
			}
			private := filepath.Join(t.TempDir(), "private")
			if err := os.Mkdir(private, 0700); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(private, "snapshot.sqlite")
			if _, err := f.s.db.Exec("VACUUM INTO ?", dst); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dst, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := ValidateOfflineBackup(ctx, dst)
			if err != nil {
				t.Fatal(err)
			}

			u := url.URL{Scheme: "file", Path: dst, RawQuery: "mode=rw&_pragma=journal_mode(WAL)&_pragma=busy_timeout(0)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_pragma=trusted_schema(OFF)"}
			maintenance, err := sql.Open("sqlite", u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer maintenance.Close()
			conn, err := maintenance.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if mode == "old" {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				if _, err = pruneExecutionZeroBatch(canceled, conn, executionHistoryCursor{}); err == nil {
					t.Fatal("cancellation ignored")
				}
				holder, err := maintenance.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = holder.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				got, err := pruneExecutionZeroBatch(ctx, conn, executionHistoryCursor{})
				holder.ExecContext(ctx, "ROLLBACK")
				holder.Close()
				if err == nil || got.Next != (executionHistoryCursor{}) || time.Since(start) > time.Second {
					t.Fatal("busy writer was not skipped", got, err)
				}
				// Fail after the permanent immutable trigger has been dropped.
				if _, err = conn.ExecContext(ctx, "CREATE TEMP TRIGGER fail_history_delete BEFORE DELETE ON inbound_execution_usage_deltas BEGIN SELECT RAISE(ABORT, 'synthetic history failure'); END"); err != nil {
					t.Fatal(err)
				}
				got, err = pruneExecutionZeroBatch(ctx, conn, executionHistoryCursor{})
				if err == nil || got.Next != (executionHistoryCursor{}) || got.Removed != 0 {
					t.Fatal("failed batch advanced", got, err)
				}
				var triggers int
				if err = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name='inbound_execution_usage_deltas_no_delete'").Scan(&triggers); err != nil || triggers != 1 {
					t.Fatal("rollback lost immutable trigger", triggers, err)
				}
				if _, err = conn.ExecContext(ctx, "DROP TRIGGER fail_history_delete"); err != nil {
					t.Fatal(err)
				}
			}
			for pass := 0; pass < 2; pass++ {
				cursor := executionHistoryCursor{}
				for {
					got, err := pruneExecutionZeroBatch(ctx, conn, cursor)
					if err != nil {
						t.Fatal(err)
					}
					if got.Scanned > 128 {
						t.Fatal("unbounded scan", got)
					}
					cursor = got.Next
					if got.Done {
						break
					}
				}
			}
			conn.Close()
			maintenance.Close()
			after, err := ValidateOfflineBackup(ctx, dst)
			if err != nil {
				t.Fatal(err)
			}
			removed := int64(0)
			if mode == "old" {
				removed = int64(len(b.Principals))
			}
			for name, count := range before.TableCounts {
				if name == "inbound_execution_usage_deltas" {
					count -= removed
				}
				if after.TableCounts[name] != count {
					t.Fatal("unexpected table count", name, after.TableCounts[name], count)
				}
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
					t.Fatal("lost replay", err)
				}
			}
			bad := reports[2]
			bad.Counters = append([]ix.Counter(nil), bad.Counters...)
			bad.Counters[0].Upload++
			if _, err = restored.AcceptInboundExecutionUsage(ctx, f.key, bad); !errors.Is(err, ErrConflict) {
				t.Fatal("conflicting replay accepted", err)
			}
			if mode == "open" || mode == "uncertain" {
				if _, err = restored.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 5, 110, 220, true)); err != nil {
					t.Fatal("restored settlement failed", err)
				}
			}
			var up, down int64
			wantUp, wantDown := int64(100), int64(200)
			if mode == "open" || mode == "uncertain" {
				wantUp, wantDown = 110, 220
			}
			if err = restored.db.QueryRow("SELECT uplink,downlink FROM pro_usage_accounts WHERE client_id=?", f.client.ID).Scan(&up, &down); err != nil || up != wantUp || down != wantDown {
				t.Fatal("account changed", up, down, err)
			}
			if err = restored.db.QueryRow("SELECT SUM(upload),SUM(download) FROM inbound_execution_usage_deltas WHERE client_id=?", f.client.ID).Scan(&up, &down); err != nil || up != wantUp || down != wantDown {
				t.Fatal("ledger changed", up, down, err)
			}
			var actual []byte
			if err = restored.db.QueryRow("SELECT report_enc FROM inbound_execution_usage_receipts WHERE epoch_id=? AND sequence=4", e.EpochID).Scan(&actual); err != nil || !bytes.Equal(actual, terminal) {
				t.Fatal("terminal/latest proof changed", err)
			}
			if _, err = restored.db.Exec("DELETE FROM inbound_execution_usage_deltas"); err == nil {
				t.Fatal("immutability lost")
			}
			var sourceRows int64
			if err = f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_deltas").Scan(&sourceRows); err != nil || sourceRows != before.TableCounts["inbound_execution_usage_deltas"] {
				t.Fatal("source changed", err)
			}
		})
	}
}

func TestExecutionHistoryMaintenanceBoundedCursor(t *testing.T) {
 f:=newExecutionFixture(t)
 defer f.s.Close()
 ctx:=context.Background()
 f.command(t,"history-bounded-activate-0001","activate")
 b:=f.bundle(t)
 e:=f.birth(t,b,"history-bounded-epoch-0001")
 for seq:=uint64(1);seq<=131;seq++ {
  up,down:=uint64(17),uint64(31)
  if seq==1 {up,down=0,0}
  if _,err:=f.s.AcceptInboundExecutionUsage(ctx,f.key,executionReport(b,e,seq,up,down,seq==131));err!=nil {t.Fatal(err)}
 }
 var ddl string
 if err:=f.s.db.QueryRow("SELECT sql FROM sqlite_master WHERE name='inbound_execution_usage_receipts_no_update'").Scan(&ddl);err!=nil{t.Fatal(err)}
 tx,err:=f.s.db.Begin();if err!=nil{t.Fatal(err)}
 for _,q:=range []string{"DROP TRIGGER inbound_execution_usage_receipts_no_update","UPDATE inbound_execution_usage_receipts SET created=1",ddl}{
  if _,err=tx.Exec(q);err!=nil{tx.Rollback();t.Fatal(err)}
 }
 if err=tx.Commit();err!=nil{t.Fatal(err)}
 u:=url.URL{Scheme:"file",Path:f.path,RawQuery:"mode=rw&_pragma=busy_timeout(0)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_pragma=trusted_schema(OFF)"}
 db,err:=sql.Open("sqlite",u.String());if err!=nil{t.Fatal(err)}
 defer db.Close()
 conn,err:=db.Conn(ctx);if err!=nil{t.Fatal(err)}
 defer conn.Close()
 first,err:=pruneExecutionZeroBatch(ctx,conn,executionHistoryCursor{})
 if err!=nil || first.Scanned!=128 || first.Removed!=126 || first.Done {t.Fatal("first batch not bounded",first,err)}
 second,err:=pruneExecutionZeroBatch(ctx,conn,first.Next)
 if err!=nil || second.Scanned!=3 || second.Removed!=2 || second.Done {t.Fatal("continuation lost rows",second,err)}
 last,err:=pruneExecutionZeroBatch(ctx,conn,second.Next)
 if err!=nil || !last.Done || last.Removed!=0 {t.Fatal("scan did not finish",last,err)}
 // A lost in-memory cursor is safe: replaying the sweep only sees protected rows.
 restart,err:=pruneExecutionZeroBatch(ctx,conn,executionHistoryCursor{})
 if err!=nil || restart.Removed!=0 || restart.Scanned!=3 {t.Fatal("restart was not idempotent",restart,err)}
 var receipts int
 if err=f.s.db.QueryRow("SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=?",e.EpochID).Scan(&receipts);err!=nil || receipts!=131 {t.Fatal("receipts pruned",receipts,err)}
}
