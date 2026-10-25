package business

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

var inboundExecutionSchema = []string{
	`CREATE TABLE inbound_execution_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1),base_schema INTEGER NOT NULL CHECK(base_schema BETWEEN 8 AND 15))`,
	`CREATE TABLE inbound_execution_binary_approvals(node_id TEXT NOT NULL,installation_id TEXT NOT NULL,generation INTEGER NOT NULL,pins_sha TEXT NOT NULL,config_sha TEXT NOT NULL,manifest_sha TEXT NOT NULL,payload_enc BLOB NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(node_id,installation_id,generation,pins_sha))`,
	`CREATE TABLE inbound_execution_heads(inbound_id INTEGER PRIMARY KEY REFERENCES native_inbounds(id),revision INTEGER NOT NULL,desired_revision INTEGER NOT NULL DEFAULT 0,active_revision INTEGER NOT NULL DEFAULT 0,desired_state TEXT NOT NULL,observed_state TEXT NOT NULL,binding_enc BLOB NOT NULL,last_event_id TEXT NOT NULL,pending_operation TEXT NOT NULL DEFAULT '',active_operation TEXT NOT NULL DEFAULT '')`,
	`CREATE TABLE inbound_execution_events(event_id TEXT PRIMARY KEY,inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),revision INTEGER NOT NULL,kind TEXT NOT NULL,actor_id TEXT NOT NULL,fingerprint TEXT NOT NULL,payload_enc BLOB NOT NULL,created INTEGER NOT NULL,UNIQUE(inbound_id,revision))`,
	`CREATE TABLE inbound_execution_revisions(inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),revision INTEGER NOT NULL,event_id TEXT NOT NULL UNIQUE REFERENCES inbound_execution_events(event_id),payload_enc BLOB NOT NULL,PRIMARY KEY(inbound_id,revision))`,
	`CREATE TABLE inbound_execution_operations(id TEXT PRIMARY KEY REFERENCES inbound_execution_events(event_id),inbound_id INTEGER NOT NULL,revision INTEGER NOT NULL,action TEXT NOT NULL,state TEXT NOT NULL,created INTEGER NOT NULL,updated INTEGER NOT NULL,payload_enc BLOB NOT NULL)`,
	`CREATE TABLE inbound_execution_nodes(node_id TEXT PRIMARY KEY,installation_id TEXT NOT NULL,desired_generation INTEGER NOT NULL DEFAULT 0,observed_generation INTEGER NOT NULL DEFAULT 0,observation_enc BLOB NOT NULL DEFAULT X'')`,
	`CREATE TABLE inbound_execution_bundles(node_id TEXT NOT NULL REFERENCES inbound_execution_nodes(node_id),installation_id TEXT NOT NULL,generation INTEGER NOT NULL,operation_id TEXT NOT NULL,config_sha TEXT NOT NULL,manifest_sha TEXT NOT NULL,payload_enc BLOB NOT NULL,PRIMARY KEY(node_id,generation))`,
	`CREATE TABLE inbound_execution_operation_nodes(operation_id TEXT NOT NULL REFERENCES inbound_execution_operations(id),node_id TEXT NOT NULL,installation_id TEXT NOT NULL,generation INTEGER NOT NULL,PRIMARY KEY(operation_id,node_id),FOREIGN KEY(node_id,generation) REFERENCES inbound_execution_bundles(node_id,generation))`,
	`CREATE TABLE inbound_execution_epochs(epoch_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,installation_id TEXT NOT NULL,generation INTEGER NOT NULL,identity_sha TEXT NOT NULL,process_identity TEXT NOT NULL,payload_enc BLOB NOT NULL,state TEXT NOT NULL CHECK(state IN('open','sealed','uncertain')),sequence INTEGER NOT NULL DEFAULT 0,cursor_enc BLOB NOT NULL,created INTEGER NOT NULL,UNIQUE(node_id,installation_id,process_identity),FOREIGN KEY(node_id,generation) REFERENCES inbound_execution_bundles(node_id,generation))`,
	`CREATE UNIQUE INDEX inbound_execution_epoch_open ON inbound_execution_epochs(node_id) WHERE state!='sealed'`,
	`CREATE TABLE inbound_execution_usage_receipts(epoch_id TEXT NOT NULL REFERENCES inbound_execution_epochs(epoch_id),sequence INTEGER NOT NULL,sha256 TEXT NOT NULL,final INTEGER NOT NULL CHECK(final IN(0,1)),report_enc BLOB NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(epoch_id,sequence))`,
	`CREATE TABLE inbound_execution_epoch_failures(epoch_id TEXT PRIMARY KEY REFERENCES inbound_execution_epochs(epoch_id),reason TEXT NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE inbound_execution_usage_holds(epoch_id TEXT NOT NULL REFERENCES inbound_execution_epochs(epoch_id),client_id TEXT NOT NULL REFERENCES pro_usage_accounts(client_id),reason TEXT NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(epoch_id,client_id))`,
	`CREATE TABLE inbound_execution_usage_deltas(epoch_id TEXT NOT NULL,sequence INTEGER NOT NULL,principal_id TEXT NOT NULL,client_id TEXT NOT NULL,role TEXT NOT NULL,upload INTEGER NOT NULL CHECK(typeof(upload)='integer' AND upload>=0),download INTEGER NOT NULL CHECK(typeof(download)='integer' AND download>=0),PRIMARY KEY(epoch_id,sequence,principal_id),FOREIGN KEY(epoch_id,sequence) REFERENCES inbound_execution_usage_receipts(epoch_id,sequence))`,
	`CREATE INDEX inbound_execution_baseline_accounts ON inbound_execution_usage_deltas(client_id,epoch_id) WHERE sequence=1 AND role='customer'`,
	`CREATE TABLE inbound_execution_liveness_holds(epoch_id TEXT NOT NULL REFERENCES inbound_execution_epochs(epoch_id),client_id TEXT NOT NULL REFERENCES pro_usage_accounts(client_id),sequence INTEGER NOT NULL,last_received INTEGER NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(epoch_id,client_id))`,
	`CREATE TABLE inbound_execution_liveness_resolutions(epoch_id TEXT NOT NULL,client_id TEXT NOT NULL,kind TEXT NOT NULL CHECK(kind IN('settled','uncertain')),created INTEGER NOT NULL,PRIMARY KEY(epoch_id,client_id),FOREIGN KEY(epoch_id,client_id) REFERENCES inbound_execution_liveness_holds(epoch_id,client_id))`,
	`CREATE TABLE inbound_execution_reconciliations(event_id TEXT PRIMARY KEY,epoch_id TEXT NOT NULL UNIQUE REFERENCES inbound_execution_epochs(epoch_id),inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),actor_id TEXT NOT NULL,revision INTEGER NOT NULL,fingerprint TEXT NOT NULL,review_sha TEXT NOT NULL,payload_enc BLOB NOT NULL,clearance_enc BLOB NOT NULL,created INTEGER NOT NULL)`,
	`CREATE TABLE inbound_execution_usage_hold_resolutions(epoch_id TEXT NOT NULL,client_id TEXT NOT NULL,event_id TEXT NOT NULL REFERENCES inbound_execution_reconciliations(event_id),created INTEGER NOT NULL,PRIMARY KEY(epoch_id,client_id),FOREIGN KEY(epoch_id,client_id) REFERENCES inbound_execution_usage_holds(epoch_id,client_id))`,
	`CREATE TABLE inbound_execution_aggregate_totals(principal_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,inbound_id INTEGER NOT NULL,role TEXT NOT NULL,upload INTEGER NOT NULL CHECK(typeof(upload)='integer' AND upload>=0),download INTEGER NOT NULL CHECK(typeof(download)='integer' AND download>=0),CHECK(upload<=9223372036854775807-download))`,
	`CREATE TABLE inbound_execution_subscription_credentials(id TEXT PRIMARY KEY,client_id TEXT NOT NULL REFERENCES clients(id),generation INTEGER NOT NULL,token_sha TEXT NOT NULL UNIQUE,token_enc BLOB NOT NULL,created INTEGER NOT NULL,UNIQUE(client_id,generation))`,
	`CREATE TABLE inbound_execution_subscription_events(event_id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,client_id TEXT NOT NULL REFERENCES clients(id),generation INTEGER NOT NULL,action TEXT NOT NULL,state TEXT NOT NULL,credential_id TEXT NOT NULL REFERENCES inbound_execution_subscription_credentials(id),fingerprint TEXT NOT NULL,created INTEGER NOT NULL,UNIQUE(client_id,generation))`,
	`CREATE TABLE inbound_execution_subscription_heads(client_id TEXT PRIMARY KEY REFERENCES clients(id),generation INTEGER NOT NULL,state TEXT NOT NULL CHECK(state IN('active','revoked')),credential_id TEXT NOT NULL REFERENCES inbound_execution_subscription_credentials(id),event_id TEXT NOT NULL REFERENCES inbound_execution_subscription_events(event_id))`,
	`CREATE TRIGGER inbound_execution_subscription_head_no_delete BEFORE DELETE ON inbound_execution_subscription_heads BEGIN SELECT RAISE(ABORT,'managed subscription history retained'); END`,
	`CREATE TRIGGER inbound_execution_head_no_delete BEFORE DELETE ON inbound_execution_heads BEGIN SELECT RAISE(ABORT,'managed execution history retained'); END`,
	`CREATE TRIGGER inbound_execution_node_identity BEFORE UPDATE OF node_id,installation_id ON inbound_execution_nodes BEGIN SELECT RAISE(ABORT,'managed installation immutable'); END`,
	`CREATE TRIGGER inbound_execution_node_no_delete BEFORE DELETE ON inbound_execution_nodes BEGIN SELECT RAISE(ABORT,'managed execution owner retained'); END`,
	`CREATE TRIGGER inbound_execution_epoch_identity BEFORE UPDATE OF epoch_id,node_id,installation_id,generation,identity_sha,process_identity,payload_enc,created ON inbound_execution_epochs BEGIN SELECT RAISE(ABORT,'managed epoch identity immutable'); END`,
	`CREATE TRIGGER inbound_execution_epoch_no_delete BEFORE DELETE ON inbound_execution_epochs BEGIN SELECT RAISE(ABORT,'managed epoch retained'); END`,
	`CREATE TRIGGER inbound_execution_operation_identity BEFORE UPDATE OF id,inbound_id,revision,action,created,payload_enc ON inbound_execution_operations BEGIN SELECT RAISE(ABORT,'managed operation immutable'); END`,
	`CREATE TRIGGER inbound_execution_operation_no_delete BEFORE DELETE ON inbound_execution_operations BEGIN SELECT RAISE(ABORT,'managed operation retained'); END`,
	`CREATE TRIGGER inbound_execution_block_native_delivery BEFORE INSERT ON native_runtime_delivery WHEN EXISTS(SELECT 1 FROM inbound_execution_nodes WHERE node_id=NEW.node_id) BEGIN SELECT RAISE(ABORT,'managed executor owns node'); END`,
	`CREATE TRIGGER inbound_execution_block_native_delivery_update BEFORE UPDATE ON native_runtime_delivery WHEN EXISTS(SELECT 1 FROM inbound_execution_nodes WHERE node_id=NEW.node_id) BEGIN SELECT RAISE(ABORT,'managed executor owns node'); END`,
	`CREATE TRIGGER inbound_execution_block_route_claim BEFORE INSERT ON native_route_channels WHEN EXISTS(SELECT 1 FROM inbound_execution_nodes WHERE node_id=NEW.node_id) BEGIN SELECT RAISE(ABORT,'managed executor owns node'); END`,
	`CREATE TRIGGER inbound_execution_block_metered_claim BEFORE INSERT ON native_metered_route_channels WHEN EXISTS(SELECT 1 FROM inbound_execution_nodes WHERE node_id=NEW.node_id) BEGIN SELECT RAISE(ABORT,'managed executor owns node'); END`,
	`CREATE TRIGGER inbound_execution_block_native_target BEFORE INSERT ON native_inbound_targets WHEN EXISTS(SELECT 1 FROM inbound_execution_nodes WHERE node_id=NEW.node_id) BEGIN SELECT RAISE(ABORT,'managed executor owns node'); END`,
	`CREATE TRIGGER inbound_execution_block_legacy_lifecycle BEFORE UPDATE OF enabled,archived ON native_inbounds WHEN (NEW.enabled!=OLD.enabled OR NEW.archived!=OLD.archived) AND EXISTS(SELECT 1 FROM inbound_execution_heads WHERE inbound_id=NEW.id) BEGIN SELECT RAISE(ABORT,'managed inbound lifecycle required'); END`,
}

func init() {
	for _, table := range []string{"inbound_execution_meta", "inbound_execution_binary_approvals", "inbound_execution_events", "inbound_execution_revisions", "inbound_execution_bundles", "inbound_execution_operation_nodes", "inbound_execution_usage_receipts", "inbound_execution_usage_deltas", "inbound_execution_subscription_credentials", "inbound_execution_subscription_events", "inbound_execution_epoch_failures", "inbound_execution_usage_holds", "inbound_execution_liveness_holds", "inbound_execution_liveness_resolutions", "inbound_execution_reconciliations", "inbound_execution_usage_hold_resolutions"} {
		for _, action := range []string{"UPDATE", "DELETE"} {
			inboundExecutionSchema = append(inboundExecutionSchema, fmt.Sprintf("CREATE TRIGGER %s_no_%s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'managed execution audit immutable'); END", table, strings.ToLower(action), action, table))
		}
	}
}

// EnableInboundExecution is startup-only and deliberately bumps user_version:
// older binaries must refuse this database rather than ignore managed leases.
func (s *Store) EnableInboundExecution(ctx context.Context) error {
	if err := s.EnableUsageLedger(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version == ix.SchemaVersion {
		if err = verifyInboundExecutionSchema(ctx, tx); err != nil {
			return err
		}
		return tx.Commit()
	}
	// This release does not migrate legacy health/subscription generations or
	// old production installations. Their gates must retain their exact schema
	// contract until a separately reviewed forward migration exists.
	if version != 8 {
		return ErrUnavailable
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'inbound_execution_*'`).Scan(&n); err != nil || n != 0 {
		return ErrUnavailable
	}
	for _, q := range inboundExecutionSchema {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_meta(id,version,base_schema) VALUES(1,1,?)`, version); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `PRAGMA user_version=16`); err != nil {
		return err
	}
	return tx.Commit()
}

func verifyInboundExecutionSchema(ctx context.Context, q inboundReader) error {
	var base, version int
	if err := q.QueryRowContext(ctx, `SELECT version,base_schema FROM inbound_execution_meta WHERE id=1`).Scan(&version, &base); err != nil || version != 1 || base < 8 || base > 15 {
		return ErrUnavailable
	}
	// Verify the immutable policy as well as table presence; a partially copied
	// schema must not turn a downgrade or stale writer into execution authority.
	for _, ddl := range inboundExecutionSchema {
		parts := strings.Fields(ddl)
		if len(parts) < 3 {
			return ErrUnavailable
		}
		name := parts[2]
		if parts[1] == "UNIQUE" {
			name = parts[3]
		}
		if i := strings.IndexByte(name, '('); i >= 0 {
			name = name[:i]
		}
		var actual string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&actual); err != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(ddl), " ") {
			return ErrUnavailable
		}
	}
	return nil
}

func executionSchemaPresent(ctx context.Context, q inboundReader) (bool, error) {
	var version int
	if err := q.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return false, err
	}
	return version == ix.SchemaVersion, nil
}

func executionNodeClaim(ctx context.Context, tx *sql.Tx, node, install string) error {
	var old string
	err := tx.QueryRowContext(ctx, `SELECT installation_id FROM inbound_execution_nodes WHERE node_id=?`, node).Scan(&old)
	if err == nil {
		if old != install {
			return ErrConflict
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	for _, table := range []string{"native_route_channels", "native_metered_route_channels", "native_runtime_delivery", "native_inbound_targets", "pro_usage_epochs"} {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE node_id=?`, node).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_nodes(node_id,installation_id) VALUES(?,?)`, node, install)
	return err
}
