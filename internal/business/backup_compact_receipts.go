package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// CompactOfflineExecutionReceipts operates ONLY on a disposable standalone
// snapshot held by its caller. Never pass a live or authoritative database.
// It preserves every receipt key/hash/time, every delta, final proof, cursor,
// account, hold and non-settled epoch. Only redundant non-final report bodies
// of genuinely finalized epochs become authenticated replay digests.
func CompactOfflineExecutionReceipts(ctx context.Context, path string, key []byte) error {
	check, err := ValidateOfflineBackup(ctx, path)
	if err != nil {
		return err
	}
	if check.SchemaVersion != ix.SchemaVersion {
		return nil
	}
	if err = ValidateOfflineBackupAssets(ctx, path, key); err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_pragma=journal_mode(DELETE)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_pragma=trusted_schema(0)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return ErrOfflineBackupInvalid
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The immutable trigger is restored inside the same transaction. It is never
	// removed from the source database, and no receipt is deleted or re-keyed.
	const trigger = "inbound_execution_usage_receipts_no_update"
	var ddl string
	if err = tx.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?", trigger).Scan(&ddl); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DROP TRIGGER "+trigger); err != nil {
		return err
	}
	var lastEpoch string
	var lastSequence uint64
	for {
		rows, e := tx.QueryContext(ctx, `SELECT r.epoch_id,r.sequence,r.sha256,r.report_enc
   FROM inbound_execution_usage_receipts r
   JOIN inbound_execution_epochs e ON e.epoch_id=r.epoch_id
   JOIN inbound_execution_usage_receipts f ON f.epoch_id=e.epoch_id AND f.sequence=e.sequence AND f.final=1
   WHERE e.state='sealed' AND r.final=0 AND r.sequence<e.sequence
   AND NOT EXISTS(SELECT 1 FROM inbound_execution_usage_holds h WHERE h.epoch_id=e.epoch_id)
   AND NOT EXISTS(SELECT 1 FROM inbound_execution_reconciliations x WHERE x.epoch_id=e.epoch_id)
   AND (r.epoch_id,r.sequence)>(?,?)
   ORDER BY r.epoch_id,r.sequence LIMIT 256`, lastEpoch, lastSequence)
		if e != nil {
			return e
		}
		type receipt struct {
			epoch, hash string
			sequence    uint64
			enc         []byte
		}
		batch := []receipt{}
		batchBytes := 0
		for rows.Next() {
			var r receipt
			if e = rows.Scan(&r.epoch, &r.sequence, &r.hash, &r.enc); e != nil {
				rows.Close()
				return e
			}
			batch = append(batch, r)
			batchBytes += len(r.enc)
			if batchBytes >= 8<<20 {
				break
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			aad := executionAAD("usage-report", r.epoch, r.sequence)
			var raw json.RawMessage
			if openPlanValue(key, aad, r.enc, &raw) != nil {
				return ErrOfflineBackupAssets
			}
			var digest string
			if json.Unmarshal(raw, &digest) == nil {
				if digest != r.hash || !routeSHA.MatchString(digest) {
					return ErrOfflineBackupAssets
				}
			} else {
				var report ix.UsageReport
				if openPlanValue(key, aad, r.enc, &report) != nil || report.Final || report.InitialEpoch != nil || report.EpochID != r.epoch || report.Sequence != r.sequence || ix.Digest(report) != r.hash {
					return ErrOfflineBackupAssets
				}
				// Replay already uses the immutable hash, not the full body. Final refund
				// proofs still use their original full report. Deltas retain all counters.
				enc, e := sealPlanValue(key, aad, r.hash)
				if e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, "UPDATE inbound_execution_usage_receipts SET report_enc=? WHERE epoch_id=? AND sequence=?", enc, r.epoch, r.sequence); e != nil {
					return e
				}
			}
			lastEpoch, lastSequence = r.epoch, r.sequence
		}
	}
	if _, err = tx.ExecContext(ctx, ddl); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, "VACUUM"); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	after, err := ValidateOfflineBackup(ctx, path)
	if err != nil || after.Fingerprint != check.Fingerprint {
		return ErrOfflineBackupInvalid
	}
	for table, n := range check.TableCounts {
		if after.TableCounts[table] != n {
			return ErrOfflineBackupInvalid
		}
	}
	return ValidateOfflineBackupAssets(ctx, path, key)
}

func backupExecutionReceiptAssets(ctx context.Context, db *sql.DB, key []byte) error {
	rows, err := db.QueryContext(ctx, "SELECT epoch_id,sequence,sha256,final,report_enc FROM inbound_execution_usage_receipts")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var epoch, hash string
		var sequence uint64
		var final bool
		var enc []byte
		if rows.Scan(&epoch, &sequence, &hash, &final, &enc) != nil {
			return ErrOfflineBackupAssets
		}
		aad := executionAAD("usage-report", epoch, sequence)
		var raw json.RawMessage
		if !routeSHA.MatchString(hash) || openPlanValue(key, aad, enc, &raw) != nil {
			return ErrOfflineBackupAssets
		}
		var digest string
		if json.Unmarshal(raw, &digest) == nil {
			if final || digest != hash {
				return ErrOfflineBackupAssets
			}
			continue
		}
		var report ix.UsageReport
		if openPlanValue(key, aad, enc, &report) != nil || report.EpochID != epoch || report.Sequence != sequence || report.Final != final || ix.Digest(report) != hash {
			return ErrOfflineBackupAssets
		}
	}
	return rows.Err()
}
