package business

import (
	"context"
	"database/sql"
	"strings"
)

const darkRestoreCutoverApplicationID = 1146244405 // DRM5

const darkRestoreCutoverActivationTrigger = "CREATE TRIGGER darkrestore_hold_activation_v1 BEFORE UPDATE OF deployment_state ON clients WHEN NEW.deployment_state!='disabled' AND EXISTS(SELECT 1 FROM darkrestore_import_entries_v1 WHERE client_id=OLD.id) AND NOT EXISTS(SELECT 1 FROM darkrestore_cutover_receipts_v1 WHERE client_id=OLD.id) BEGIN SELECT RAISE(ABORT,'dark restore cutover required'); END"

var darkRestoreCutoverSchemaV1 = []string{
	"CREATE TABLE darkrestore_cutover_evidence_v1(client_id TEXT PRIMARY KEY REFERENCES darkrestore_import_entries_v1(client_id),inventory_sha256 TEXT NOT NULL CHECK(length(inventory_sha256)=64),coverage_approval_sha256 TEXT NOT NULL CHECK(length(coverage_approval_sha256)=64),coverage_authority_sha256 TEXT NOT NULL CHECK(length(coverage_authority_sha256)=64),settled_historical_bytes TEXT NOT NULL,transition_opened_unix_nano INTEGER NOT NULL CHECK(transition_opened_unix_nano>0),receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)",
	"CREATE TABLE darkrestore_cutover_receipts_v1(client_id TEXT PRIMARY KEY REFERENCES darkrestore_cutover_evidence_v1(client_id),subscription_revision INTEGER NOT NULL CHECK(subscription_revision>0),subscription_delivered_unix_nano INTEGER NOT NULL CHECK(subscription_delivered_unix_nano>0),remaining_budget_bytes TEXT NOT NULL,receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)",
	"CREATE TRIGGER darkrestore_cutover_evidence_v1_no_update BEFORE UPDATE ON darkrestore_cutover_evidence_v1 BEGIN SELECT RAISE(ABORT,'dark restore cutover evidence immutable'); END",
	"CREATE TRIGGER darkrestore_cutover_evidence_v1_no_delete BEFORE DELETE ON darkrestore_cutover_evidence_v1 BEGIN SELECT RAISE(ABORT,'dark restore cutover evidence immutable'); END",
	"CREATE TRIGGER darkrestore_cutover_receipts_v1_no_update BEFORE UPDATE ON darkrestore_cutover_receipts_v1 BEGIN SELECT RAISE(ABORT,'dark restore cutover receipt immutable'); END",
	"CREATE TRIGGER darkrestore_cutover_receipts_v1_no_delete BEFORE DELETE ON darkrestore_cutover_receipts_v1 BEGIN SELECT RAISE(ABORT,'dark restore cutover receipt immutable'); END",
}

func darkRestoreCutoverSchemaState(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_cutover_*_v1*'").Scan(&n); err != nil {
		return false, ErrUnavailable
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreCutoverSchemaV1) {
		return false, ErrUnavailable
	}
	var app int
	if err := q.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil || app != darkRestoreCutoverApplicationID {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreCutoverSchemaV1 {
		fields := strings.Fields(ddl)
		name := strings.SplitN(fields[2], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil ||
			strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(ddl), " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

func ensureDarkRestoreCutoverSchema(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreCutoverSchemaState(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	local, err := darkRestoreLocalAttachmentSchemaState(ctx, tx)
	if err != nil || !local {
		return ErrUnavailable
	}
	for _, ddl := range darkRestoreCutoverSchemaV1 {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DROP TRIGGER darkrestore_hold_activation_v1"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, darkRestoreCutoverActivationTrigger); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA application_id=1146244405"); err != nil {
		return err
	}
	return nil
}

// EnableDarkRestoreCutoverV1 is an explicit owner-only schema upgrade. Enabling
// the import feature alone never opens M5.
func (s *Store) EnableDarkRestoreCutoverV1(ctx context.Context, token string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return err
	}
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return ErrUnavailable
	}
	if err = ensureDarkRestoreCutoverSchema(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
