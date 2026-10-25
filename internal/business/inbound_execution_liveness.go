package business

import (
	"context"
	"database/sql"
	"time"
)

// A lease is at most thirty seconds and each normal renewal follows an
// acknowledged report within the worker's eight-second step deadline. Forty-five
// seconds therefore includes the entire outstanding lease plus request margin.
const executionReportLiveness = 45 * time.Second

// This read-time guard complements the durable sweep. Failure or delay of the
// background writer must not renew another node for the same customer while a
// previously admitted incarnation has an unobserved financial tail.
func executionClientHasStaleEpoch(ctx context.Context, tx *sql.Tx, client string, now int64) (bool, error) {
	var stale int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inbound_execution_usage_deltas baseline JOIN inbound_execution_epochs e ON e.epoch_id=baseline.epoch_id JOIN inbound_execution_usage_receipts latest ON latest.epoch_id=e.epoch_id AND latest.sequence=e.sequence WHERE baseline.client_id=? AND baseline.sequence=1 AND baseline.role='customer' AND e.state='open' AND latest.created<=?)`, client, now-int64(executionReportLiveness)).Scan(&stale)
	return stale != 0, err
}

// SweepInboundExecutionLiveness is run by the HUB independently of remote
// Agents and browsers. Timeout holds are provisional: an authentic final seal
// can settle them; confirmed loss of an incarnation promotes them permanently.
func (s *Store) SweepInboundExecutionLiveness(ctx context.Context, key []byte) (bool, error) {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	changed, err := sweepExecutionLiveness(ctx, tx, key, time.Now().UnixNano())
	if err != nil {
		return false, err
	}
	return changed, tx.Commit()
}

func sweepExecutionLiveness(ctx context.Context, tx *sql.Tx, key []byte, now int64) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.epoch_id,e.node_id,e.installation_id,e.generation,e.sequence,r.created FROM inbound_execution_epochs e JOIN inbound_execution_usage_receipts r ON r.epoch_id=e.epoch_id AND r.sequence=e.sequence WHERE e.state='open' AND e.sequence>0 AND r.final=0 AND r.created<=? ORDER BY e.epoch_id`, now-int64(executionReportLiveness))
	if err != nil {
		return false, err
	}
	type expired struct {
		epoch, node, install string
		generation, sequence uint64
		received             int64
	}
	entries := []expired{}
	for rows.Next() {
		var e expired
		if err = rows.Scan(&e.epoch, &e.node, &e.install, &e.generation, &e.sequence, &e.received); err != nil {
			rows.Close()
			return false, err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	changed := false
	for _, e := range entries {
		b, err := readExecutionBundle(ctx, tx, key, e.node, e.install, e.generation)
		if err != nil {
			return false, err
		}
		seen := map[string]bool{}
		for _, p := range b.Principals {
			if !p.Billable || seen[p.ClientID] {
				continue
			}
			if p.ClientID == "" {
				return false, ErrUnavailable
			}
			seen[p.ClientID] = true
			result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO inbound_execution_liveness_holds(epoch_id,client_id,sequence,last_received,created) VALUES(?,?,?,?,?)`, e.epoch, p.ClientID, e.sequence, e.received, now)
			if err != nil {
				return false, err
			}
			inserted, err := result.RowsAffected()
			if err != nil {
				return false, err
			}
			if inserted == 0 {
				continue
			}
			result, err = tx.ExecContext(ctx, `UPDATE pro_usage_accounts SET holds=holds+1 WHERE client_id=? AND holds<9223372036854775807`, p.ClientID)
			if err != nil {
				return false, err
			}
			n, err := result.RowsAffected()
			if err != nil || n != 1 {
				return false, ErrUnavailable
			}
			changed = true
		}
	}
	if changed {
		if _, err = tx.ExecContext(ctx, `UPDATE native_config_generation SET value=value+1 WHERE id=1 AND value<9007199254740991`); err != nil {
			return false, err
		}
	}
	return changed, nil
}

func resolveExecutionLivenessHold(ctx context.Context, tx *sql.Tx, epoch, client, kind string, now int64) (bool, error) {
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inbound_execution_liveness_holds h LEFT JOIN inbound_execution_liveness_resolutions r ON r.epoch_id=h.epoch_id AND r.client_id=h.client_id WHERE h.epoch_id=? AND h.client_id=? AND r.epoch_id IS NULL)`, epoch, client).Scan(&active); err != nil {
		return false, err
	}
	if active == 0 {
		return false, nil
	}
	if kind != "settled" && kind != "uncertain" {
		return false, ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_execution_liveness_resolutions(epoch_id,client_id,kind,created) VALUES(?,?,?,?)`, epoch, client, kind, now); err != nil {
		return false, err
	}
	if kind == "settled" {
		result, err := tx.ExecContext(ctx, `UPDATE pro_usage_accounts SET holds=holds-1 WHERE client_id=? AND holds>0`, client)
		if err != nil {
			return false, err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

func settleExecutionLiveness(ctx context.Context, tx *sql.Tx, epoch string, now int64) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT h.client_id FROM inbound_execution_liveness_holds h LEFT JOIN inbound_execution_liveness_resolutions r ON r.epoch_id=h.epoch_id AND r.client_id=h.client_id WHERE h.epoch_id=? AND r.epoch_id IS NULL ORDER BY h.client_id`, epoch)
	if err != nil {
		return false, err
	}
	clients := []string{}
	for rows.Next() {
		var client string
		if err = rows.Scan(&client); err != nil {
			rows.Close()
			return false, err
		}
		clients = append(clients, client)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	changed := false
	for _, client := range clients {
		settled, err := resolveExecutionLivenessHold(ctx, tx, epoch, client, "settled", now)
		if err != nil {
			return false, err
		}
		changed = changed || settled
	}
	return changed, nil
}
