package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

// ErrOfflineBackupAssets never includes ciphertext, keys, settings or file paths.
var ErrOfflineBackupAssets = errors.New("offline PRO backup has unsupported assets or unrecoverable inbound ciphertext")

// This is a per-record V1 compatibility limit, not an in-memory database limit.
// Rows are visited individually, including immutable historical rows.
const maxBackupInboundPayload = 32 << 20

// ValidateOfflineBackupAssets checks recoverability of native profiles, saved
// plans and all managed-execution ciphertext, including disabled and historical
// state. V1 rejects dynamic external file dependencies instead of omitting them.
// It must be paired with ValidateOfflineBackup for exact schema/integrity checks
// and with the caller's inventory/maintenance lock for other state and keys.
// No file referenced inside a profile is opened, and no runtime is activated.
func ValidateOfflineBackupAssets(ctx context.Context, path string, inboundKey []byte) error {
	if ctx == nil {
		return ErrOfflineBackupAssets
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	before, err := backupSnapshotPath(path)
	if err != nil {
		return ErrOfflineBackupAssets
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1&_pragma=query_only(1)&_pragma=trusted_schema(0)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return ErrOfflineBackupAssets
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if err = validateOfflineBackupAssetsDB(ctx, db, inboundKey); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrOfflineBackupAssets
	}
	after, err := backupSnapshotPath(path)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return ErrOfflineBackupAssets
	}
	return ctx.Err()
}

// Every expression here is an internal constant. CASE bounds SQLite-to-Go
// allocation before a malformed/oversized encrypted value is materialized.
func backupPayloadQuery(table, column, binding string) string {
	return fmt.Sprintf(`SELECT %s,length(%s),CASE WHEN length(%s)<=%d THEN %s ELSE NULL END FROM %s`,
		binding, column, column, maxBackupInboundPayload, column, table)
}

func backupPayloadRows(ctx context.Context, db *sql.DB, query string, visit func(string, []byte) error) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return ErrOfflineBackupAssets
	}
	defer rows.Close()
	for rows.Next() {
		var binding string
		var size int64
		var payload []byte
		if rows.Scan(&binding, &size, &payload) != nil || len(binding) > 2048 || size < 1 || size > maxBackupInboundPayload || int64(len(payload)) != size {
			return ErrOfflineBackupAssets
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if visit(binding, payload) != nil {
			return ErrOfflineBackupAssets
		}
	}
	return rows.Err()
}

func validateOfflineBackupAssetsDB(ctx context.Context, db *sql.DB, key []byte) error {
	var version int
	if db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version) != nil || (version != 8 && version != 16) {
		return ErrOfflineBackupAssets
	}
	if len(key) != 0 && len(key) != 32 {
		return ErrOfflineBackupAssets
	}
	if err := backupPayloadRows(ctx, db, backupPayloadQuery("native_inbounds", "profile_enc", "CAST(id AS TEXT)"), func(id string, raw []byte) error {
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return err
		}
		p, err := openProfile(key, raw, n)
		if err != nil {
			return err
		}
		return backupCheckValue(p)
	}); err != nil {
		return err
	}
	if err := backupPayloadRows(ctx, db, backupPayloadQuery("native_inbound_plans", "payload_enc", "printf('plan:%d:%d',inbound_id,revision)"), func(aad string, raw []byte) error {
		var p NativeInboundPlan
		if openPlanValue(key, aad, raw, &p) != nil {
			return ErrOfflineBackupAssets
		}
		return backupCheckValue(p)
	}); err != nil {
		return err
	}
	if err := backupPayloadRows(ctx, db, backupPayloadQuery("native_inbound_plan_events", "receipt_enc", "printf('receipt:%s:%d:%d',event_id,inbound_id,revision)"), func(aad string, raw []byte) error {
		var p NativeInboundPlanReceipt
		if openPlanValue(key, aad, raw, &p) != nil {
			return ErrOfflineBackupAssets
		}
		return backupCheckValue(p)
	}); err != nil {
		return err
	}
	if version != 16 {
		return nil
	}
	if err := backupPayloadRows(ctx, db, backupPayloadQuery("inbound_execution_revisions", "payload_enc", "printf('managed-inbound-execution-v1/revision/%d/%d',inbound_id,revision)"), func(aad string, raw []byte) error {
		var snapshot executionSnapshot
		if openPlanValue(key, aad, raw, &snapshot) != nil {
			return ErrOfflineBackupAssets
		}
		return backupCheckValue(snapshot)
	}); err != nil {
		return err
	}
	if err := backupPayloadRows(ctx, db, backupPayloadQuery("inbound_execution_bundles", "payload_enc", "printf('managed-inbound-execution-v1/bundle/%s/%s/%d',node_id,installation_id,generation)"), func(aad string, raw []byte) error {
		var bundle ix.Bundle
		if openPlanValue(key, aad, raw, &bundle) != nil || ix.VerifyBundle(key, bundle) != nil {
			return ErrOfflineBackupAssets
		}
		if len(bundle.NativeConfig) != 0 && backupCheckJSON(bundle.NativeConfig) != nil {
			return ErrOfflineBackupAssets
		}
		for _, component := range bundle.Components {
			if backupCheckJSON(component.Config) != nil || backupCheckJSON(component.Manifest) != nil {
				return ErrOfflineBackupAssets
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// These records contain no file payloads, but authenticating every historical
	// envelope proves that the saved inbound key can recover the whole managed
	// execution history. Do not filter by enabled/current/active status.
	for _, q := range []struct{ table, column, binding, where string }{
		{"inbound_execution_heads", "binding_enc", "printf('managed-inbound-execution-v1/bindings/%d/%d',inbound_id,revision)", ""},
		{"inbound_execution_events", "payload_enc", "printf('managed-inbound-execution-v1/event/%s/%d',event_id,revision)", ""},
		{"inbound_execution_operations", "payload_enc", "printf('managed-inbound-execution-v1/operation/%s/%d',id,revision)", ""},
		{"inbound_execution_nodes", "observation_enc", "printf('managed-inbound-execution-v1/observation/%s/%s/%d',node_id,installation_id,observed_generation)", " WHERE observed_generation<>0 OR length(observation_enc)<>0"},
		{"inbound_execution_epochs", "payload_enc", "printf('managed-inbound-execution-v1/epoch/%s/%d',epoch_id,generation)", ""},
		{"inbound_execution_epochs", "cursor_enc", "printf('managed-inbound-execution-v1/usage-cursor/%s/%d',epoch_id,sequence)", ""},
		{"inbound_execution_usage_receipts", "report_enc", "printf('managed-inbound-execution-v1/usage-report/%s/%d',epoch_id,sequence)", ""},
		{"inbound_execution_reconciliations", "payload_enc", "printf('managed-inbound-execution-v1/owner-loss/%s/%d',event_id,revision)", ""},
		{"inbound_execution_reconciliations", "clearance_enc", "printf('managed-inbound-execution-v1/owner-loss-clearance/%s/%d',epoch_id,revision)", ""},
		{"inbound_execution_binary_approvals", "payload_enc", "printf('managed-inbound-execution-v1/binary-approval/%s/%s/%s/%d',node_id,installation_id,pins_sha,generation)", ""},
	} {
		if err := backupPayloadRows(ctx, db, backupPayloadQuery(q.table, q.column, q.binding)+q.where, func(aad string, raw []byte) error {
			var value json.RawMessage
			return openPlanValue(key, aad, raw, &value)
		}); err != nil {
			return err
		}
	}
	return backupManagedCredentials(ctx, db, key)
}

func backupManagedCredentials(ctx context.Context, db *sql.DB, key []byte) error {
	rows, err := db.QueryContext(ctx, `SELECT id,client_id,generation,token_sha,created,CASE WHEN length(token_enc)<=256 THEN token_enc ELSE NULL END FROM inbound_execution_subscription_credentials`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var b subscriptionCredentialBinding
		var enc []byte
		if rows.Scan(&b.ID, &b.ClientID, &b.Generation, &b.TokenSHA, &b.Created, &enc) != nil {
			return ErrOfflineBackupAssets
		}
		raw, err := openSubscription(key, "managed-credential", b, enc)
		if err != nil || !validSubscriptionToken(string(raw)) || executionTokenHash(string(raw)) != b.TokenSHA {
			return ErrOfflineBackupAssets
		}
	}
	return rows.Err()
}

func backupCheckValue(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return ErrOfflineBackupAssets
	}
	return backupCheckJSON(raw)
}

func backupCheckJSON(raw []byte) error {
	if len(raw) == 0 || len(raw) > maxBackupInboundPayload {
		return ErrOfflineBackupAssets
	}
	var value any
	if usagewire.Decode(raw, &value) != nil {
		return ErrOfflineBackupAssets
	}
	return backupCheckExternal(value)
}

func backupCheckExternal(value any) error {
	switch object := value.(type) {
	case map[string]any:
		for name, child := range object {
			lower := strings.ToLower(name)
			text, isString := child.(string)
			switch lower {
			case "network":
				if text == "xdrive" {
					return ErrOfflineBackupAssets
				}
			case "xdrive", "xdrivesettings":
				if child != nil {
					return ErrOfflineBackupAssets
				}
			case "certificatefile", "keyfile", "masterkeylog", "dir", "certificate_path", "key_path":
				if !isString || text != "" {
					return ErrOfflineBackupAssets
				}
			case "certificate", "private_key":
				if isString && filepath.IsAbs(text) {
					return ErrOfflineBackupAssets
				}
			case "file_references":
				if items, ok := child.([]any); !ok || len(items) != 0 {
					return ErrOfflineBackupAssets
				}
			}
			if err := backupCheckExternal(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range object {
			if err := backupCheckExternal(child); err != nil {
				return err
			}
		}
	}
	return nil
}
