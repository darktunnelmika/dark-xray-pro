package business

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Only a committed opt-in to the import schema changes the insert trigger.
// Enabling any client later still goes through native_dirty_clients_config.
const originalClientInsertTrigger = "CREATE TRIGGER native_dirty_clients_new AFTER INSERT ON clients BEGIN UPDATE native_config_generation SET value=value+1 WHERE id=1; END"
const quarantinedClientInsertTrigger = "CREATE TRIGGER native_dirty_clients_new AFTER INSERT ON clients WHEN NEW.deployment_state!='disabled' BEGIN UPDATE native_config_generation SET value=value+1 WHERE id=1; END"

func verifyClientInsertTrigger(ctx context.Context, q sellerQueryV2, want string) error {
	var actual string
	if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='trigger' AND name='native_dirty_clients_new' AND tbl_name='clients'").Scan(&actual); err != nil || actual != want {
		return ErrUnavailable
	}
	return nil
}

var darkRestoreImportSchemaV1 = []string{
	"CREATE TABLE darkrestore_import_meta_v1(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))",
	"CREATE TABLE darkrestore_import_receipts_v1(event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),actor_user_id TEXT NOT NULL REFERENCES auth_users(id),source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64),review_sha256 TEXT NOT NULL CHECK(length(review_sha256)=64),request_json BLOB NOT NULL,receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)",
	"CREATE TABLE darkrestore_import_entries_v1(source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64),source_id TEXT NOT NULL,client_id TEXT NOT NULL UNIQUE REFERENCES clients(id),client_uuid TEXT NOT NULL UNIQUE,source_group_id TEXT NOT NULL,target_group_id TEXT NOT NULL REFERENCES client_groups(id),representative_id TEXT NOT NULL REFERENCES representatives(id),historical_bytes TEXT NOT NULL,source_status TEXT NOT NULL CHECK(source_status IN('requires_manual_review','archive_history_only')),event_id TEXT NOT NULL REFERENCES darkrestore_import_receipts_v1(event_id),created_unix INTEGER NOT NULL,PRIMARY KEY(source_sha256,source_id))",
	"CREATE TABLE darkrestore_group_history_v1(source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64),source_group_id TEXT NOT NULL,source_name TEXT NOT NULL,target_group_id TEXT NOT NULL REFERENCES client_groups(id),representative_id TEXT NOT NULL REFERENCES representatives(id),source_count INTEGER NOT NULL CHECK(source_count>=0),archived_count INTEGER NOT NULL CHECK(archived_count>=0),promoted_count INTEGER NOT NULL CHECK(promoted_count>=0),historical_bytes TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES darkrestore_import_receipts_v1(event_id),created_unix INTEGER NOT NULL,PRIMARY KEY(source_sha256,source_group_id))",
	"CREATE TABLE darkrestore_import_skips_v1(source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64),source_id TEXT NOT NULL,source_group_id TEXT NOT NULL,reason TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES darkrestore_import_receipts_v1(event_id),created_unix INTEGER NOT NULL,PRIMARY KEY(source_sha256,source_id,reason))",
	"CREATE TABLE darkrestore_event_archives_v1(event_id TEXT PRIMARY KEY REFERENCES darkrestore_import_receipts_v1(event_id),source_sha256 TEXT NOT NULL UNIQUE,record_count INTEGER NOT NULL CHECK(record_count>0),sealed_history BLOB NOT NULL)",
	"CREATE TRIGGER darkrestore_hold_activation_v1 BEFORE UPDATE OF deployment_state ON clients WHEN NEW.deployment_state!='disabled' AND EXISTS(SELECT 1 FROM darkrestore_import_entries_v1 WHERE client_id=OLD.id) BEGIN SELECT RAISE(ABORT,'dark restore cutover required'); END",
	"CREATE INDEX darkrestore_import_recent_v1 ON darkrestore_import_receipts_v1(created_unix DESC,event_id DESC)",
	"CREATE INDEX darkrestore_import_entries_rep_v1 ON darkrestore_import_entries_v1(representative_id,client_id)",
	"CREATE INDEX darkrestore_import_entries_group_v1 ON darkrestore_import_entries_v1(target_group_id,client_id)",
	"CREATE INDEX darkrestore_import_skips_event_v1 ON darkrestore_import_skips_v1(event_id,source_id)",
}

func init() {
	for _, table := range []string{"darkrestore_import_meta_v1", "darkrestore_import_receipts_v1", "darkrestore_import_entries_v1", "darkrestore_group_history_v1", "darkrestore_import_skips_v1", "darkrestore_event_archives_v1"} {
		for _, action := range []string{"UPDATE", "DELETE"} {
			darkRestoreImportSchemaV1 = append(darkRestoreImportSchemaV1,
				"CREATE TRIGGER "+table+"_no_"+strings.ToLower(action)+" BEFORE "+action+" ON "+table+" BEGIN SELECT RAISE(ABORT,'dark restore import history immutable'); END")
		}
	}
}

func darkRestoreSchemaState(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_*'").Scan(&n); err != nil {
		return false, ErrUnavailable
	}
	var app int
	if err := q.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return false, ErrUnavailable
	}
	if n == 0 {
		if app != 0 {
			return false, ErrUnavailable
		}
		return false, nil
	}
	birthPresent, birthErr := birthSchema(ctx, q)
	if birthErr != nil {
		return false, birthErr
	}
	if birthPresent {
		n -= len(darkRestoreBirthDDL)
	}
	admissionPresent, admissionErr := admissionSchema(ctx, q)
	if admissionErr != nil {
		return false, admissionErr
	}
	if admissionPresent {
		n -= len(darkRestoreAdmissionDDL)
	}
	listenerPresent, listenerErr := listenerApplySchema(ctx, q)
	if listenerErr != nil {
		return false, listenerErr
	}
	if listenerPresent {
		n -= len(darkRestoreListenerDDL)
	}
	servingPresent, servingErr := darkRestoreServingHealthSchema(ctx, q)
	if servingErr != nil {
		return false, servingErr
	}
	if servingPresent {
		n -= len(darkRestoreServingHealthDDL)
	}
	connectionPresent, connectionErr := connectionSchema(ctx, q)
	if connectionErr != nil {
		return false, connectionErr
	}
	if connectionPresent {
		n -= len(darkRestoreConnectionDDL)
	}
	probePresent, probeErr := darkRestoreSystemProbeRoundSchema(ctx, q)
	if probeErr != nil {
		return false, probeErr
	}
	if probePresent {
		n -= len(darkRestoreSystemProbeRoundDDL)
	}
	baseCount := len(darkRestoreImportSchemaV1)
	localCount := baseCount + len(darkRestoreLocalAttachmentSchemaV1)
	cutoverCount := localCount + len(darkRestoreCutoverSchemaV1)
	if n != baseCount && n != localCount && n != cutoverCount {
		return false, ErrUnavailable
	}
	if n == baseCount && app != 0 ||
		n == localCount && app != darkRestoreLocalAttachmentApplicationID ||
		n == cutoverCount && app != darkRestoreCutoverApplicationID {
		return false, ErrUnavailable
	}
	var version int
	if err := q.QueryRowContext(ctx, "SELECT version FROM darkrestore_import_meta_v1 WHERE id=1").Scan(&version); err != nil || version != 1 {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreImportSchemaV1 {
		fields := strings.Fields(ddl)
		at := 2
		if fields[1] == "UNIQUE" {
			at = 3
		}
		name := strings.SplitN(fields[at], "(", 2)[0]
		want := ddl
		if name == "darkrestore_hold_activation_v1" && n == cutoverCount {
			want = darkRestoreCutoverActivationTrigger
		}
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil ||
			strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(want), " ") {
			return false, ErrUnavailable
		}
	}
	if err := verifyClientInsertTrigger(ctx, q, quarantinedClientInsertTrigger); err != nil {
		return false, err
	}
	if n >= localCount {
		present, err := darkRestoreLocalAttachmentSchemaState(ctx, q)
		if err != nil || !present {
			return false, ErrUnavailable
		}
	}
	if n == cutoverCount {
		present, err := darkRestoreCutoverSchemaState(ctx, q)
		if err != nil || !present {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

func (s *Store) EnableDarkRestoreImportV1(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ownership, err := ownershipSchemaState(ctx, tx)
	if err != nil || !ownership {
		return ErrUnavailable
	}
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil {
		return err
	}
	if !present {
		if err = verifyClientInsertTrigger(ctx, tx, originalClientInsertTrigger); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DROP TRIGGER native_dirty_clients_new"); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, quarantinedClientInsertTrigger); err != nil {
			return err
		}
		for _, ddl := range darkRestoreImportSchemaV1 {
			if _, err = tx.ExecContext(ctx, ddl); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_import_meta_v1(id,version) VALUES(1,1)"); err != nil {
			return err
		}
	}
	if err = ensureDarkRestoreLocalAttachmentSchema(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func rejectDarkRestoreEvent(ctx context.Context, tx *sql.Tx, eventID string) error {
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM darkrestore_import_receipts_v1 WHERE event_id=?", eventID).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return ErrConflict
	}
	return nil
}

func darkRestoreSourceAlreadyImported(ctx context.Context, tx *sql.Tx, sourceSHA string) (bool, error) {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM darkrestore_import_entries_v1 WHERE source_sha256=?", sourceSHA).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM darkrestore_import_receipts_v1 WHERE source_sha256=?", sourceSHA).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func darkRestoreReadGroup(ctx context.Context, tx *sql.Tx, id string) (Group, error) {
	g, err := readGroup(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	return g, err
}

var ErrDarkRestoreCutover = errors.New("darkrestore_cutover_required")

// An offline snapshot proves history, not the last byte served by the old
// identity. A later settlement must prove that boundary before activation.
func requireDarkRestoreCutover(ctx context.Context, tx *sql.Tx, id string) error {
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return err
	}
	var imported bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM darkrestore_import_entries_v1 WHERE client_id=?)", id).Scan(&imported); err != nil {
		return ErrUnavailable
	}
	if imported {
		return requireDarkRestoreFinalCutover(ctx, tx, id)
	}
	return nil
}
