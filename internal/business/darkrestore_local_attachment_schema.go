package business

import (
	"context"
	"database/sql"
	"strings"
)

// This extension is optional for databases that were imported before a local
// source could be sealed. The header marker detects a lost optional table on
// reopen and during offline backup inspection.
const darkRestoreLocalAttachmentApplicationID = 1146244404 // DRM4

var darkRestoreLocalAttachmentSchemaV1 = []string{
	"CREATE TABLE darkrestore_local_settlements_v1(client_id TEXT PRIMARY KEY REFERENCES darkrestore_import_entries_v1(client_id),source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64),source_id TEXT NOT NULL,terminal_epoch_id TEXT NOT NULL UNIQUE,receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL,FOREIGN KEY(source_sha256,source_id) REFERENCES darkrestore_import_entries_v1(source_sha256,source_id))",
	"CREATE TRIGGER darkrestore_local_settlements_v1_no_update BEFORE UPDATE ON darkrestore_local_settlements_v1 BEGIN SELECT RAISE(ABORT,'dark restore settlement immutable'); END",
	"CREATE TRIGGER darkrestore_local_settlements_v1_no_delete BEFORE DELETE ON darkrestore_local_settlements_v1 BEGIN SELECT RAISE(ABORT,'dark restore settlement immutable'); END",
}

func darkRestoreLocalAttachmentSchemaState(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_local_settlements_v1*'").Scan(&n); err != nil {
		return false, ErrUnavailable
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreLocalAttachmentSchemaV1) {
		return false, ErrUnavailable
	}
	var app int
	if err := q.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil || (app != darkRestoreLocalAttachmentApplicationID && app != darkRestoreCutoverApplicationID) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreLocalAttachmentSchemaV1 {
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

func ensureDarkRestoreLocalAttachmentSchema(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreLocalAttachmentSchemaState(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	for _, ddl := range darkRestoreLocalAttachmentSchemaV1 {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA application_id=1146244404"); err != nil {
		return err
	}
	return nil
}
