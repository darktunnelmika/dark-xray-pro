package business

import (
	"context"
	"database/sql"
	"time"
)

// This function is only called inside the private backup projection transaction.
// Keep the source schema, receipt identities and all nonzero financial evidence.
// Old readers can use the restored sparse history, but old backup CLIs reject
// the explicit manifest projection feature instead of silently accepting it.
func compactOfflineZeroDeltas(ctx context.Context, tx *sql.Tx) (int64, error) {
	cutoff := time.Now().Add(-24 * time.Hour).UnixNano()
	const trigger = "inbound_execution_usage_deltas_no_delete"
	var ddl string
	if err := tx.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?", trigger).Scan(&ddl); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "DROP TRIGGER "+trigger); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM inbound_execution_usage_deltas
 WHERE upload=0 AND download=0 AND sequence>1 AND EXISTS(
 SELECT 1 FROM inbound_execution_epochs e
 JOIN inbound_execution_usage_receipts r ON r.epoch_id=e.epoch_id AND r.sequence=inbound_execution_usage_deltas.sequence
 JOIN inbound_execution_usage_receipts f ON f.epoch_id=e.epoch_id AND f.sequence=e.sequence AND f.final=1
 WHERE e.epoch_id=inbound_execution_usage_deltas.epoch_id AND e.state='sealed'
 AND r.final=0 AND r.sequence<e.sequence AND r.created<? AND f.created<?
 AND NOT EXISTS(SELECT 1 FROM inbound_execution_usage_holds h WHERE h.epoch_id=e.epoch_id)
 AND NOT EXISTS(SELECT 1 FROM inbound_execution_liveness_holds h WHERE h.epoch_id=e.epoch_id)
 AND NOT EXISTS(SELECT 1 FROM inbound_execution_reconciliations x WHERE x.epoch_id=e.epoch_id))`, cutoff, cutoff)
	if err != nil {
		return 0, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, ddl); err != nil {
		return 0, err
	}
	return removed, nil
}
