package business

import (
	"context"
	"database/sql"
	"time"
)

// MarkInboundExecutionEpochUncertain records a missing terminal observation.
// It adds financial reconciliation holds across every billable identity and
// never fabricates a final counter value or releases an earlier hold.
func (s *Store) MarkInboundExecutionEpochUncertain(ctx context.Context, key []byte, node, install, epoch, reason string) error {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = markExecutionEpochUncertain(ctx, tx, key, node, install, epoch, reason); err != nil {
		return err
	}
	return tx.Commit()
}
func markExecutionEpochUncertain(ctx context.Context, tx *sql.Tx, key []byte, node, install, epoch, reason string) error {
	if !idRE.MatchString(epoch) || !validName(reason, 200) {
		return ErrInvalid
	}
	var storedNode, storedInstall, state string
	var generation uint64
	if err := tx.QueryRowContext(ctx, `SELECT node_id,installation_id,generation,state FROM inbound_execution_epochs WHERE epoch_id=?`, epoch).Scan(&storedNode, &storedInstall, &generation, &state); err != nil {
		return err
	}
	if storedNode != node || storedInstall != install || state == "sealed" {
		return ErrConflict
	}
	if state == "uncertain" {
		return nil
	}
	b, err := readExecutionBundle(ctx, tx, key, node, install, generation)
	if err != nil {
		return err
	}
	now := time.Now().UnixNano()
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_epoch_failures(epoch_id,reason,created) VALUES(?,?,?)`, epoch, reason, now); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, p := range b.Principals {
		if !p.Billable || seen[p.ClientID] {
			continue
		}
		if p.ClientID == "" {
			return ErrUnavailable
		}
		seen[p.ClientID] = true
		if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_usage_holds(epoch_id,client_id,reason,created) VALUES(?,?,?,?)`, epoch, p.ClientID, reason, now); err != nil {
			return err
		}
		transferred, err := resolveExecutionLivenessHold(ctx, tx, epoch, p.ClientID, "uncertain", now)
		if err != nil {
			return err
		}
		if transferred {
			// The same financial tail already owns one provisional hold.
			// Promotion records permanent uncertainty without charging it twice.
			continue
		}
		res, e := tx.ExecContext(ctx, `UPDATE pro_usage_accounts SET holds=holds+1 WHERE client_id=? AND holds<9223372036854775807`, p.ClientID)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil || n != 1 {
			return ErrUnavailable
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE inbound_execution_epochs SET state='uncertain' WHERE epoch_id=? AND state='open'`, epoch); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE native_config_generation SET value=value+1 WHERE id=1 AND value<9007199254740991`)
	return err
}
