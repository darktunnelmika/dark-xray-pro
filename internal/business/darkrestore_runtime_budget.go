package business

import (
	"context"
	"database/sql"
)

// Historical export validation and permission to start a serving process are
// separate. A predecessor's active M5 identity without enforced historical quota
// must not silently regain its full entitlement when opened by a new binary.
func validateDarkRestoreRuntimeBudgets(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id FROM clients c JOIN darkrestore_import_entries_v1 e ON e.client_id=c.id WHERE c.archived=0 AND c.deployment_state!='disabled'`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		evidence, e := readCutoverEvidence(ctx, tx, id)
		if e != nil || evidence == nil || len(evidence.NativeDocument) == 0 || verifyCutoverHistoricalBudget(ctx, tx, evidence) != nil {
			return ErrUnavailable
		}
	}
	return nil
}
