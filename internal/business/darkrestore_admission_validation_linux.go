//go:build linux

package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
)

func validateDarkRestoreAdmissions(ctx context.Context, tx *sql.Tx) error {
	present, err := admissionSchema(ctx, tx)
	if err != nil {
		return err
	}
	if !present {
		var cutover int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name='darkrestore_cutover_receipts_v1'").Scan(&cutover); err != nil {
			return err
		}
		if cutover == 0 {
			return nil
		}
		promotionPresent, promotionErr := darkRestorePreparedPromotionSchema(ctx, tx)
		if promotionErr != nil {
			return promotionErr
		}
		promotionClause := ""
		if promotionPresent {
			promotionClause = " AND NOT EXISTS(SELECT 1 FROM darkrestore_prepared_promotions_v1 p WHERE p.client_id=c.id)"
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM clients c JOIN darkrestore_cutover_receipts_v1 r ON r.client_id=c.id WHERE c.deployment_state!='disabled' AND json_extract(r.receipt_json,'$.subscription_delivery_scope')='darkrestore_prepared_not_applied_v1'"+promotionClause).Scan(&n); err != nil || n != 0 {
			return ErrUnavailable
		}
		return nil
	}
	births, err := birthSchema(ctx, tx)
	if err != nil || !births {
		return ErrUnavailable
	}
	rows, err := tx.QueryContext(ctx, "SELECT epoch_id,plan_sha,event_id,receipt_json FROM darkrestore_native_admission_v1")
	if err != nil {
		return err
	}
	type row struct {
		epoch, sha, event string
		raw               []byte
	}
	var entries []row
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.epoch, &r.sha, &r.event, &r.raw); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range entries {
		var permit DarkRestoreAdmission
		if len(r.raw) > 131072 || json.Unmarshal(r.raw, &permit) != nil || permit.EventID != r.event || !eventRE.MatchString(r.event) || !idRE.MatchString(permit.ActorID) || permit.Registered.Epoch.ID != r.epoch || permit.Snapshot.ClientID != permit.Registered.Plan.ClientID || permit.Snapshot.DeploymentState != "disabled" {
			return ErrUnavailable
		}
		sha, err := permit.Registered.Plan.Digest()
		if err != nil || sha != r.sha || permit.Snapshot.StateSHA != permit.Registered.Plan.StateSHA {
			return ErrUnavailable
		}
		snap, err := hashDarkRestoreCutoverSnapshotV1(permit.Snapshot)
		if err != nil || snap.StateSHA != permit.Snapshot.StateSHA {
			return ErrUnavailable
		}
		var registered []byte
		if err = tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_native_birth_v1 WHERE plan_sha=? AND epoch_id=?", sha, r.epoch).Scan(&registered); err != nil {
			return ErrUnavailable
		}
		canonical, _ := json.Marshal(permit.Registered)
		if !bytes.Equal(canonical, registered) {
			return ErrUnavailable
		}
	}
	promotionPresent, promotionErr := darkRestorePreparedPromotionSchema(ctx, tx)
	if promotionErr != nil {
		return promotionErr
	}
	promotionClause := ""
	if promotionPresent {
		promotionClause = " AND NOT EXISTS(SELECT 1 FROM darkrestore_prepared_promotions_v1 p WHERE p.client_id=c.id)"
	}
	var unbound int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM clients c JOIN darkrestore_cutover_receipts_v1 r ON r.client_id=c.id WHERE c.deployment_state!='disabled' AND json_extract(r.receipt_json,'$.subscription_delivery_scope')='darkrestore_prepared_not_applied_v1' AND NOT EXISTS(SELECT 1 FROM darkrestore_native_admission_v1 a JOIN darkrestore_native_birth_v1 b ON b.plan_sha=a.plan_sha WHERE b.client_id=c.id)"+promotionClause).Scan(&unbound); err != nil || unbound != 0 {
		return ErrUnavailable
	}

	return nil
}
