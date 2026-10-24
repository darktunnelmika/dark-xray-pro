package usageledger

import "context"

// Consumer manifests are an explicit startup-only extension. Older readers
// must reject this database rather than fall back to an origin-only proof.
const ConsumerManifestsBusinessSchemaVersion = 14

func HasConsumerManifestsSchema(v int) bool {
	return v == ConsumerManifestsBusinessSchemaVersion || HasInitialApplyHealthSchema(v)
}

var consumerManifestsSchema = []string{
	`CREATE TABLE pro_consumer_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`INSERT INTO pro_consumer_meta VALUES(1,1)`,
	`CREATE TABLE pro_consumer_manifests(id TEXT PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,actor_id TEXT NOT NULL REFERENCES auth_users(id),fingerprint TEXT NOT NULL,handover_id TEXT NOT NULL REFERENCES native_metered_handovers(id),node_id TEXT NOT NULL,installation_id TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0 AND revision<9007199254740991),state TEXT NOT NULL CHECK(state IN ('active','revoked')),manifest TEXT NOT NULL,manifest_sha TEXT NOT NULL UNIQUE,binding_sha TEXT NOT NULL,created INTEGER NOT NULL CHECK(created>0),UNIQUE(handover_id,revision))`,
	`CREATE TABLE pro_consumer_manifest_heads(handover_id TEXT PRIMARY KEY REFERENCES native_metered_handovers(id),revision INTEGER NOT NULL CHECK(revision>0 AND revision<9007199254740991),manifest_id TEXT NOT NULL UNIQUE REFERENCES pro_consumer_manifests(id))`,
	`CREATE TRIGGER pro_consumer_meta_no_update BEFORE UPDATE ON pro_consumer_meta BEGIN SELECT RAISE(ABORT,'consumer schema immutable'); END`,
	`CREATE TRIGGER pro_consumer_meta_no_delete BEFORE DELETE ON pro_consumer_meta BEGIN SELECT RAISE(ABORT,'consumer schema retained'); END`,
	`CREATE TRIGGER pro_consumer_manifests_no_update BEFORE UPDATE ON pro_consumer_manifests BEGIN SELECT RAISE(ABORT,'consumer manifest immutable'); END`,
	`CREATE TRIGGER pro_consumer_manifests_no_delete BEFORE DELETE ON pro_consumer_manifests BEGIN SELECT RAISE(ABORT,'consumer manifest retained'); END`,
	`CREATE TRIGGER pro_consumer_manifest_heads_no_delete BEFORE DELETE ON pro_consumer_manifest_heads BEGIN SELECT RAISE(ABORT,'consumer manifest head retained'); END`,
	`CREATE TRIGGER pro_consumer_manifest_head_insert BEFORE INSERT ON pro_consumer_manifest_heads WHEN NEW.revision!=1 OR NOT EXISTS(SELECT 1 FROM pro_consumer_manifests m WHERE m.id=NEW.manifest_id AND m.handover_id=NEW.handover_id AND m.revision=NEW.revision) BEGIN SELECT RAISE(ABORT,'consumer manifest head mismatch'); END`,
	`CREATE TRIGGER pro_consumer_manifest_head_update BEFORE UPDATE ON pro_consumer_manifest_heads WHEN NEW.handover_id!=OLD.handover_id OR NEW.revision!=OLD.revision+1 OR NOT EXISTS(SELECT 1 FROM pro_consumer_manifests m WHERE m.id=NEW.manifest_id AND m.handover_id=NEW.handover_id AND m.revision=NEW.revision) BEGIN SELECT RAISE(ABORT,'consumer manifest head mismatch'); END`,
}

func verifyConsumerManifestsSchema(ctx context.Context, c *ledgerConn, version int) error {
	if !HasConsumerManifestsSchema(version) {
		var n int
		if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_consumer_*'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrSchema
		}
		return nil
	}
	if err := verifySchemaObjects(ctx, c, consumerManifestsSchema, "pro_consumer_*"); err != nil {
		return err
	}
	var n int
	if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_consumer_meta WHERE id=1 AND version=1`).Scan(&n); err != nil || n != 1 {
		return ErrSchema
	}
	return nil
}

func (l *Ledger) EnableConsumerManifests(ctx context.Context) error {
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
		if HasConsumerManifestsSchema(v) {
			return nil
		}
		for _, ddl := range consumerManifestsSchema {
			if _, err := c.ExecContext(ctx, ddl); err != nil {
				return err
			}
		}
		_, err := c.ExecContext(ctx, `PRAGMA user_version=14`)
		return err
	})
}
