package business

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

type executionHistoryCursor struct {
	Epoch     string
	Sequence  uint64
	Principal string
}
type executionHistoryBatch struct {
	Next             executionHistoryCursor
	Scanned, Removed int
	Done             bool
}

// pruneExecutionZeroBatch is an internal maintenance primitive, not enabled by
// Open or any request path. The caller must supply an exclusively held,
// schema-validated dedicated WAL connection with busy_timeout=0. Never use the
// accounting writer pool. A failed attempt returns the original cursor.
// The 50ms context bounds cancellable work, not an uninterruptible fsync.
func pruneExecutionZeroBatch(parent context.Context, conn *sql.Conn, cursor executionHistoryCursor) (result executionHistoryBatch, err error) {
	result.Next = cursor
	ctx, cancel := context.WithTimeout(parent, 50*time.Millisecond)
	defer cancel()
	var busy, sync, foreign int
	var journal string
	for _, p := range []struct {
		q   string
		out *int
	}{
		{"PRAGMA busy_timeout", &busy}, {"PRAGMA synchronous", &sync}, {"PRAGMA foreign_keys", &foreign},
	} {
		if err = conn.QueryRowContext(ctx, p.q).Scan(p.out); err != nil {
			return
		}
	}
	if err = conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		return
	}
	if busy != 0 || sync != 2 || foreign != 1 || journal != "wal" {
		err = ErrUnavailable
		return
	}
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Cancellation must never return a connection with a live transaction or
		// an absent immutable trigger to its pool.
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if _, e := conn.ExecContext(cleanup, "ROLLBACK"); e != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			err = errors.Join(err, e)
		}
		result = executionHistoryBatch{Next: cursor}
	}()
	rows, e := conn.QueryContext(ctx, `SELECT epoch_id,sequence,principal_id FROM inbound_execution_usage_deltas
 WHERE (epoch_id,sequence,principal_id)>(?,?,?)
 ORDER BY epoch_id,sequence,principal_id LIMIT 128`, cursor.Epoch, cursor.Sequence, cursor.Principal)
	if e != nil {
		err = e
		return
	}
	keys := []executionHistoryCursor{}
	for rows.Next() {
		var k executionHistoryCursor
		if e = rows.Scan(&k.Epoch, &k.Sequence, &k.Principal); e != nil {
			rows.Close()
			err = e
			return
		}
		keys = append(keys, k)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		err = e
		return
	}
	candidate := executionHistoryBatch{Next: cursor, Scanned: len(keys), Done: len(keys) == 0}
	if len(keys) > 0 {
		var ddl string
		if err = conn.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='trigger' AND name='inbound_execution_usage_deltas_no_delete'").Scan(&ddl); err != nil {
			return
		}
		if _, err = conn.ExecContext(ctx, "DROP TRIGGER inbound_execution_usage_deltas_no_delete"); err != nil {
			return
		}
		cutoff := time.Now().Add(-24 * time.Hour).UnixNano()
		for _, k := range keys {
			var r sql.Result
			r, err = conn.ExecContext(ctx, `DELETE FROM inbound_execution_usage_deltas
 WHERE epoch_id=? AND sequence=? AND principal_id=?
 AND upload=0 AND download=0 AND sequence>1 AND EXISTS(
 SELECT 1 FROM inbound_execution_epochs e
 JOIN inbound_execution_usage_receipts r ON r.epoch_id=e.epoch_id AND r.sequence=inbound_execution_usage_deltas.sequence
 JOIN inbound_execution_usage_receipts f ON f.epoch_id=e.epoch_id AND f.sequence=e.sequence AND f.final=1
 WHERE e.epoch_id=inbound_execution_usage_deltas.epoch_id AND e.state='sealed'
 AND r.final=0 AND r.sequence<e.sequence AND r.created<? AND f.created<?
 AND NOT EXISTS(SELECT 1 FROM inbound_execution_usage_holds h WHERE h.epoch_id=e.epoch_id)
 AND NOT EXISTS(SELECT 1 FROM inbound_execution_liveness_holds h WHERE h.epoch_id=e.epoch_id)
 AND NOT EXISTS(SELECT 1 FROM inbound_execution_reconciliations x WHERE x.epoch_id=e.epoch_id))`, k.Epoch, k.Sequence, k.Principal, cutoff, cutoff)
			if err != nil {
				return
			}
			var n int64
			n, err = r.RowsAffected()
			if err != nil {
				return
			}
			candidate.Removed += int(n)
		}
		if _, err = conn.ExecContext(ctx, ddl); err != nil {
			return
		}
		candidate.Next = keys[len(keys)-1]
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return
	}
	committed = true
	result = candidate
	return
}
