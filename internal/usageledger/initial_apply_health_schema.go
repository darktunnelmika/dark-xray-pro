package usageledger

import "context"

// This reviewed startup migration keeps all older proof and encrypted snapshot
// bytes intact. An older reader must refuse v15, including a database with only
// preparation history and no successful protocol proof yet.
const InitialApplyHealthBusinessSchemaVersion = 15

func HasInitialApplyHealthSchema(v int) bool { return v == InitialApplyHealthBusinessSchemaVersion }

var initialApplyHealthSchema = []string{
	`CREATE TABLE pro_initial_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`INSERT INTO pro_initial_meta VALUES(1,1)`,
	`CREATE TABLE pro_initial_preparations(inbound_id INTEGER PRIMARY KEY REFERENCES native_inbound_plan_promotions(inbound_id),event_id TEXT NOT NULL UNIQUE,actor_id TEXT NOT NULL REFERENCES auth_users(id),promotion_event_id TEXT NOT NULL UNIQUE REFERENCES native_inbound_plan_promotions(event_id),promotion_sha TEXT NOT NULL,probe_id TEXT NOT NULL UNIQUE REFERENCES pro_probe_accounts(probe_id),fingerprint TEXT NOT NULL,created INTEGER NOT NULL CHECK(created>0),receipt_enc BLOB NOT NULL)`,
	`CREATE TABLE pro_initial_rounds(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL REFERENCES auth_users(id),inbound_id INTEGER NOT NULL REFERENCES pro_initial_preparations(inbound_id),promotion_event_id TEXT NOT NULL REFERENCES native_inbound_plan_promotions(event_id),node_id TEXT NOT NULL,installation_id TEXT NOT NULL,apply_command_id TEXT NOT NULL REFERENCES native_metered_route_commands(id),initial_apply_sha TEXT NOT NULL,endpoint_sha TEXT NOT NULL,manifest TEXT NOT NULL,manifest_sha TEXT NOT NULL,created INTEGER NOT NULL CHECK(created>0),expires INTEGER NOT NULL,CHECK(expires>created AND expires-created<=8000000000))`,
	`CREATE INDEX pro_initial_rounds_node ON pro_initial_rounds(node_id,installation_id,created)`,
	`CREATE TABLE pro_initial_challenges(id TEXT PRIMARY KEY,round_id TEXT NOT NULL UNIQUE REFERENCES pro_initial_rounds(id),request TEXT NOT NULL,expires INTEGER NOT NULL)`,
	`CREATE TABLE pro_initial_receipts(challenge_id TEXT PRIMARY KEY REFERENCES pro_initial_challenges(id),round_id TEXT NOT NULL UNIQUE REFERENCES pro_initial_rounds(id),receipt TEXT NOT NULL,accepted INTEGER NOT NULL)`,
	`CREATE TABLE pro_initial_snapshots(id TEXT PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,actor_id TEXT NOT NULL REFERENCES auth_users(id),client_id TEXT NOT NULL REFERENCES clients(id),fingerprint TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0 AND revision<9007199254740991),inbound_id INTEGER NOT NULL REFERENCES pro_initial_preparations(inbound_id),promotion_event_id TEXT NOT NULL REFERENCES native_inbound_plan_promotions(event_id),round_id TEXT NOT NULL REFERENCES pro_initial_rounds(id),initial_apply_sha TEXT NOT NULL,node_id TEXT NOT NULL,installation_id TEXT NOT NULL,endpoint_sha TEXT NOT NULL,manifest_sha TEXT NOT NULL,binding_sha TEXT NOT NULL,body_sha TEXT NOT NULL,payload_enc BLOB NOT NULL,created INTEGER NOT NULL CHECK(created>0),expires INTEGER NOT NULL,UNIQUE(client_id,revision),CHECK(expires>created AND expires-created<=8000000000))`,
	`CREATE TABLE pro_initial_snapshot_heads(client_id TEXT PRIMARY KEY REFERENCES clients(id),revision INTEGER NOT NULL CHECK(revision>0 AND revision<9007199254740991),snapshot_id TEXT NOT NULL UNIQUE REFERENCES pro_initial_snapshots(id))`,
	`CREATE TRIGGER pro_initial_meta_no_update BEFORE UPDATE ON pro_initial_meta BEGIN SELECT RAISE(ABORT,'initial apply schema immutable'); END`,
	`CREATE TRIGGER pro_initial_meta_no_delete BEFORE DELETE ON pro_initial_meta BEGIN SELECT RAISE(ABORT,'initial apply schema retained'); END`,
	`CREATE TRIGGER pro_initial_preparations_no_update BEFORE UPDATE ON pro_initial_preparations BEGIN SELECT RAISE(ABORT,'initial preparation immutable'); END`,
	`CREATE TRIGGER pro_initial_preparations_no_delete BEFORE DELETE ON pro_initial_preparations BEGIN SELECT RAISE(ABORT,'initial preparation retained'); END`,
	`CREATE TRIGGER pro_initial_rounds_no_update BEFORE UPDATE ON pro_initial_rounds BEGIN SELECT RAISE(ABORT,'initial round immutable'); END`,
	`CREATE TRIGGER pro_initial_rounds_no_delete BEFORE DELETE ON pro_initial_rounds BEGIN SELECT RAISE(ABORT,'initial round retained'); END`,
	`CREATE TRIGGER pro_initial_challenges_no_update BEFORE UPDATE ON pro_initial_challenges BEGIN SELECT RAISE(ABORT,'initial challenge immutable'); END`,
	`CREATE TRIGGER pro_initial_challenges_no_delete BEFORE DELETE ON pro_initial_challenges BEGIN SELECT RAISE(ABORT,'initial challenge retained'); END`,
	`CREATE TRIGGER pro_initial_receipts_no_update BEFORE UPDATE ON pro_initial_receipts BEGIN SELECT RAISE(ABORT,'initial receipt immutable'); END`,
	`CREATE TRIGGER pro_initial_receipts_no_delete BEFORE DELETE ON pro_initial_receipts BEGIN SELECT RAISE(ABORT,'initial receipt retained'); END`,
	`CREATE TRIGGER pro_initial_receipt_scope BEFORE INSERT ON pro_initial_receipts WHEN NOT EXISTS(SELECT 1 FROM pro_initial_challenges c JOIN pro_initial_rounds r ON r.id=c.round_id WHERE c.id=NEW.challenge_id AND r.id=NEW.round_id AND c.expires=r.expires AND NEW.accepted>=r.created AND NEW.accepted<r.expires) BEGIN SELECT RAISE(ABORT,'initial receipt scope mismatch'); END`,
	`CREATE TRIGGER pro_initial_snapshots_no_update BEFORE UPDATE ON pro_initial_snapshots BEGIN SELECT RAISE(ABORT,'initial snapshot immutable'); END`,
	`CREATE TRIGGER pro_initial_snapshots_no_delete BEFORE DELETE ON pro_initial_snapshots BEGIN SELECT RAISE(ABORT,'initial snapshot retained'); END`,
	`CREATE TRIGGER pro_initial_snapshot_heads_no_delete BEFORE DELETE ON pro_initial_snapshot_heads BEGIN SELECT RAISE(ABORT,'initial snapshot head retained'); END`,
	`CREATE TRIGGER pro_initial_snapshot_head_insert BEFORE INSERT ON pro_initial_snapshot_heads WHEN NEW.revision!=1 OR NOT EXISTS(SELECT 1 FROM pro_initial_snapshots s WHERE s.id=NEW.snapshot_id AND s.client_id=NEW.client_id AND s.revision=NEW.revision) BEGIN SELECT RAISE(ABORT,'initial snapshot head mismatch'); END`,
	`CREATE TRIGGER pro_initial_snapshot_head_update BEFORE UPDATE ON pro_initial_snapshot_heads WHEN NEW.client_id!=OLD.client_id OR NEW.revision!=OLD.revision+1 OR NOT EXISTS(SELECT 1 FROM pro_initial_snapshots s WHERE s.id=NEW.snapshot_id AND s.client_id=NEW.client_id AND s.revision=NEW.revision) BEGIN SELECT RAISE(ABORT,'initial snapshot head mismatch'); END`,
}

var subscriptionDeliverySchema = []string{
	`CREATE TABLE pro_delivery_events(client_id TEXT NOT NULL REFERENCES clients(id),delivery_revision INTEGER NOT NULL CHECK(delivery_revision>0 AND delivery_revision<9007199254740991),event_id TEXT NOT NULL UNIQUE,scope TEXT NOT NULL CHECK(scope IN('lab_single_handover','lab_initial_apply')),snapshot_id TEXT NOT NULL,snapshot_revision INTEGER NOT NULL CHECK(snapshot_revision>0 AND snapshot_revision<9007199254740991),created INTEGER NOT NULL CHECK(created>0),PRIMARY KEY(client_id,delivery_revision),UNIQUE(scope,snapshot_id))`,
	`CREATE TABLE pro_delivery_heads(client_id TEXT PRIMARY KEY REFERENCES clients(id),delivery_revision INTEGER NOT NULL,scope TEXT NOT NULL,snapshot_id TEXT NOT NULL,FOREIGN KEY(client_id,delivery_revision) REFERENCES pro_delivery_events(client_id,delivery_revision))`,
	`CREATE TRIGGER pro_delivery_events_no_update BEFORE UPDATE ON pro_delivery_events BEGIN SELECT RAISE(ABORT,'delivery authority immutable'); END`,
	`CREATE TRIGGER pro_delivery_events_no_delete BEFORE DELETE ON pro_delivery_events BEGIN SELECT RAISE(ABORT,'delivery authority retained'); END`,
	`CREATE TRIGGER pro_delivery_heads_no_delete BEFORE DELETE ON pro_delivery_heads BEGIN SELECT RAISE(ABORT,'delivery head retained'); END`,
	`CREATE TRIGGER pro_delivery_event_scope BEFORE INSERT ON pro_delivery_events WHEN (NEW.scope='lab_single_handover' AND NOT EXISTS(SELECT 1 FROM pro_subscription_snapshots s WHERE s.id=NEW.snapshot_id AND s.client_id=NEW.client_id AND s.revision=NEW.snapshot_revision AND s.event_id=NEW.event_id)) OR (NEW.scope='lab_initial_apply' AND NOT EXISTS(SELECT 1 FROM pro_initial_snapshots s WHERE s.id=NEW.snapshot_id AND s.client_id=NEW.client_id AND s.revision=NEW.snapshot_revision AND s.event_id=NEW.event_id)) BEGIN SELECT RAISE(ABORT,'delivery snapshot scope mismatch'); END`,
	`CREATE TRIGGER pro_delivery_head_insert BEFORE INSERT ON pro_delivery_heads WHEN NEW.delivery_revision!=1 OR NOT EXISTS(SELECT 1 FROM pro_delivery_events e WHERE e.client_id=NEW.client_id AND e.delivery_revision=NEW.delivery_revision AND e.scope=NEW.scope AND e.snapshot_id=NEW.snapshot_id) BEGIN SELECT RAISE(ABORT,'delivery head mismatch'); END`,
	`CREATE TRIGGER pro_delivery_head_update BEFORE UPDATE ON pro_delivery_heads WHEN NEW.client_id!=OLD.client_id OR NEW.delivery_revision!=OLD.delivery_revision+1 OR NOT EXISTS(SELECT 1 FROM pro_delivery_events e WHERE e.client_id=NEW.client_id AND e.delivery_revision=NEW.delivery_revision AND e.scope=NEW.scope AND e.snapshot_id=NEW.snapshot_id) BEGIN SELECT RAISE(ABORT,'delivery head mismatch'); END`,
}

func verifyInitialApplyHealthSchema(ctx context.Context, c *ledgerConn, version int) error {
	if !HasInitialApplyHealthSchema(version) {
		var n int
		if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_initial_*' OR name GLOB 'pro_delivery_*'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrSchema
		}
		return nil
	}
	if err := verifySchemaObjects(ctx, c, initialApplyHealthSchema, "pro_initial_*"); err != nil {
		return err
	}
	if err := verifySchemaObjects(ctx, c, subscriptionDeliverySchema, "pro_delivery_*"); err != nil {
		return err
	}
	var n int
	if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_initial_meta WHERE id=1 AND version=1`).Scan(&n); err != nil || n != 1 {
		return ErrSchema
	}
	return nil
}

func (l *Ledger) EnableInitialApplyHealth(ctx context.Context) error {
	return l.write(ctx, func(c *ledgerConn) error {
		var v int
		if err := c.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
			return err
		}
		if !HasLabSubscriptionsSchema(v) {
			return ErrSchema
		}
		if err := verifySchema(ctx, c); err != nil {
			return err
		}
		if err := verifyProbeSchema(ctx, c, v); err != nil {
			return err
		}
		if err := verifyHealthSchema(ctx, c, v); err != nil {
			return err
		}
		if HasInitialApplyHealthSchema(v) {
			return nil
		}
		// Install the monotonic v14 schema, without creating consumer policy or
		// approving any endpoint. No existing consumer row is changed.
		if !HasConsumerManifestsSchema(v) {
			for _, ddl := range consumerManifestsSchema {
				if _, err := c.ExecContext(ctx, ddl); err != nil {
					return err
				}
			}
		}
		for _, group := range [][]string{initialApplyHealthSchema, subscriptionDeliverySchema} {
			for _, ddl := range group {
				if _, err := c.ExecContext(ctx, ddl); err != nil {
					return err
				}
			}
		}
		// Preserve the exact current local snapshot. The global journal starts
		// at one even when the origin has a much older local revision.
		if _, err := c.ExecContext(ctx, `INSERT INTO pro_delivery_events(client_id,delivery_revision,event_id,scope,snapshot_id,snapshot_revision,created) SELECT h.client_id,1,s.event_id,'lab_single_handover',s.id,s.revision,s.created FROM pro_subscription_snapshot_heads h JOIN pro_subscription_snapshots s ON s.id=h.snapshot_id AND s.client_id=h.client_id AND s.revision=h.revision`); err != nil {
			return err
		}
		if _, err := c.ExecContext(ctx, `INSERT INTO pro_delivery_heads(client_id,delivery_revision,scope,snapshot_id) SELECT client_id,1,scope,snapshot_id FROM pro_delivery_events`); err != nil {
			return err
		}
		_, err := c.ExecContext(ctx, `PRAGMA user_version=15`)
		return err
	})
}
