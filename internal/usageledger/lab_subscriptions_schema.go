package usageledger

import "context"

// LabSubscriptionsBusinessSchemaVersion is an explicit startup opt-in. A
// database carrying bearer revocation and immutable delivery state must never
// be opened by an older reader that would forget those constraints.
const LabSubscriptionsBusinessSchemaVersion = 13

func HasLabSubscriptionsSchema(v int) bool { return v == LabSubscriptionsBusinessSchemaVersion }

var labSubscriptionsSchema = []string{
	`CREATE TABLE pro_subscription_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`INSERT INTO pro_subscription_meta VALUES(1,1)`,
	`CREATE TABLE pro_subscription_credentials(id TEXT PRIMARY KEY,client_id TEXT NOT NULL REFERENCES clients(id),generation INTEGER NOT NULL CHECK(generation>0 AND generation<9007199254740991),token_sha TEXT NOT NULL UNIQUE,token_enc BLOB NOT NULL,created INTEGER NOT NULL CHECK(created>0),UNIQUE(client_id,generation))`,
	`CREATE TABLE pro_subscription_token_events(event_id TEXT PRIMARY KEY,actor_id TEXT NOT NULL REFERENCES auth_users(id),client_id TEXT NOT NULL REFERENCES clients(id),fingerprint TEXT NOT NULL,action TEXT NOT NULL CHECK(action IN('issue','rotate','revoke')),generation INTEGER NOT NULL CHECK(generation>0 AND generation<9007199254740991),state TEXT NOT NULL CHECK(state IN('active','revoked')),credential_id TEXT REFERENCES pro_subscription_credentials(id),created INTEGER NOT NULL CHECK(created>0),UNIQUE(client_id,generation),CHECK((state='active' AND credential_id IS NOT NULL AND action!='revoke') OR (state='revoked' AND credential_id IS NULL AND action='revoke')))`,
	`CREATE TABLE pro_subscription_token_heads(client_id TEXT PRIMARY KEY REFERENCES clients(id),generation INTEGER NOT NULL CHECK(generation>0 AND generation<9007199254740991),state TEXT NOT NULL CHECK(state IN('active','revoked')),credential_id TEXT REFERENCES pro_subscription_credentials(id),event_id TEXT NOT NULL UNIQUE REFERENCES pro_subscription_token_events(event_id),CHECK((state='active' AND credential_id IS NOT NULL) OR (state='revoked' AND credential_id IS NULL)))`,
	`CREATE TABLE pro_subscription_snapshots(id TEXT PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,actor_id TEXT NOT NULL REFERENCES auth_users(id),client_id TEXT NOT NULL REFERENCES clients(id),fingerprint TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0 AND revision<9007199254740991),handover_id TEXT NOT NULL REFERENCES native_metered_handovers(id),round_id TEXT NOT NULL REFERENCES pro_health_rounds(id),node_id TEXT NOT NULL,installation_id TEXT NOT NULL,endpoint_sha TEXT NOT NULL,manifest_sha TEXT NOT NULL,binding_sha TEXT NOT NULL,body_sha TEXT NOT NULL,payload_enc BLOB NOT NULL,created INTEGER NOT NULL CHECK(created>0),expires INTEGER NOT NULL,route_count INTEGER NOT NULL CHECK(route_count BETWEEN 1 AND 256),UNIQUE(client_id,revision),CHECK(expires>created AND expires-created<=8000000000))`,
	`CREATE TABLE pro_subscription_snapshot_heads(client_id TEXT PRIMARY KEY REFERENCES clients(id),revision INTEGER NOT NULL CHECK(revision>0 AND revision<9007199254740991),snapshot_id TEXT NOT NULL UNIQUE REFERENCES pro_subscription_snapshots(id))`,
	`CREATE TRIGGER pro_subscription_meta_no_update BEFORE UPDATE ON pro_subscription_meta BEGIN SELECT RAISE(ABORT,'subscription schema immutable'); END`,
	`CREATE TRIGGER pro_subscription_meta_no_delete BEFORE DELETE ON pro_subscription_meta BEGIN SELECT RAISE(ABORT,'subscription schema retained'); END`,
	`CREATE TRIGGER pro_subscription_credentials_no_update BEFORE UPDATE ON pro_subscription_credentials BEGIN SELECT RAISE(ABORT,'subscription credential immutable'); END`,
	`CREATE TRIGGER pro_subscription_credentials_no_delete BEFORE DELETE ON pro_subscription_credentials BEGIN SELECT RAISE(ABORT,'subscription credential retained'); END`,
	`CREATE TRIGGER pro_subscription_token_events_no_update BEFORE UPDATE ON pro_subscription_token_events BEGIN SELECT RAISE(ABORT,'subscription token event immutable'); END`,
	`CREATE TRIGGER pro_subscription_token_events_no_delete BEFORE DELETE ON pro_subscription_token_events BEGIN SELECT RAISE(ABORT,'subscription token event retained'); END`,
	`CREATE TRIGGER pro_subscription_snapshots_no_update BEFORE UPDATE ON pro_subscription_snapshots BEGIN SELECT RAISE(ABORT,'subscription snapshot immutable'); END`,
	`CREATE TRIGGER pro_subscription_snapshots_no_delete BEFORE DELETE ON pro_subscription_snapshots BEGIN SELECT RAISE(ABORT,'subscription snapshot retained'); END`,
	`CREATE TRIGGER pro_subscription_token_heads_no_delete BEFORE DELETE ON pro_subscription_token_heads BEGIN SELECT RAISE(ABORT,'subscription token head retained'); END`,
	`CREATE TRIGGER pro_subscription_snapshot_heads_no_delete BEFORE DELETE ON pro_subscription_snapshot_heads BEGIN SELECT RAISE(ABORT,'subscription snapshot head retained'); END`,
	`CREATE TRIGGER pro_subscription_token_event_scope BEFORE INSERT ON pro_subscription_token_events WHEN NEW.credential_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pro_subscription_credentials c WHERE c.id=NEW.credential_id AND c.client_id=NEW.client_id AND c.generation=NEW.generation) BEGIN SELECT RAISE(ABORT,'subscription credential scope mismatch'); END`,
	`CREATE TRIGGER pro_subscription_token_head_insert BEFORE INSERT ON pro_subscription_token_heads WHEN NEW.generation!=1 OR NOT EXISTS(SELECT 1 FROM pro_subscription_token_events e WHERE e.event_id=NEW.event_id AND e.client_id=NEW.client_id AND e.generation=NEW.generation AND e.state=NEW.state AND e.credential_id IS NEW.credential_id) BEGIN SELECT RAISE(ABORT,'subscription token head mismatch'); END`,
	`CREATE TRIGGER pro_subscription_token_head_update BEFORE UPDATE ON pro_subscription_token_heads WHEN NEW.client_id!=OLD.client_id OR NEW.generation!=OLD.generation+1 OR NOT EXISTS(SELECT 1 FROM pro_subscription_token_events e WHERE e.event_id=NEW.event_id AND e.client_id=NEW.client_id AND e.generation=NEW.generation AND e.state=NEW.state AND e.credential_id IS NEW.credential_id) BEGIN SELECT RAISE(ABORT,'subscription token head mismatch'); END`,
	`CREATE TRIGGER pro_subscription_snapshot_head_insert BEFORE INSERT ON pro_subscription_snapshot_heads WHEN NEW.revision!=1 OR NOT EXISTS(SELECT 1 FROM pro_subscription_snapshots s WHERE s.id=NEW.snapshot_id AND s.client_id=NEW.client_id AND s.revision=NEW.revision) BEGIN SELECT RAISE(ABORT,'subscription snapshot head mismatch'); END`,
	`CREATE TRIGGER pro_subscription_snapshot_head_update BEFORE UPDATE ON pro_subscription_snapshot_heads WHEN NEW.client_id!=OLD.client_id OR NEW.revision!=OLD.revision+1 OR NOT EXISTS(SELECT 1 FROM pro_subscription_snapshots s WHERE s.id=NEW.snapshot_id AND s.client_id=NEW.client_id AND s.revision=NEW.revision) BEGIN SELECT RAISE(ABORT,'subscription snapshot head mismatch'); END`,
}

func verifyLabSubscriptionsSchema(ctx context.Context, c *ledgerConn, version int) error {
	if !HasLabSubscriptionsSchema(version) {
		var n int
		if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_subscription_*'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrSchema
		}
		return nil
	}
	if err := verifySchemaObjects(ctx, c, labSubscriptionsSchema, "pro_subscription_*"); err != nil {
		return err
	}
	var n int
	if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_subscription_meta WHERE id=1 AND version=1`).Scan(&n); err != nil || n != 1 {
		return ErrSchema
	}
	return nil
}

// EnableLabSubscriptions adds no credentials, snapshots, runtime authority or
// accounting mutations. It depends on complete N17 round support.
func (l *Ledger) EnableLabSubscriptions(ctx context.Context) error {
	return l.write(ctx, func(c *ledgerConn) error {
		var v int
		if err := c.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
			return err
		}
		if !HasHealthRoundsSchema(v) {
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
		if HasLabSubscriptionsSchema(v) {
			return nil
		}
		for _, ddl := range labSubscriptionsSchema {
			if _, err := c.ExecContext(ctx, ddl); err != nil {
				return err
			}
		}
		_, err := c.ExecContext(ctx, `PRAGMA user_version=13`)
		return err
	})
}
