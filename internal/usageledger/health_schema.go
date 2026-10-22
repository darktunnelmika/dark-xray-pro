package usageledger

import "context"

// HealthBusinessSchemaVersion is an explicit startup-only opt-in. Older HUBs
// must refuse this database rather than forget consumed health challenges.
const HealthBusinessSchemaVersion = 10
const HealthQueueBusinessSchemaVersion = 11

func HasProbeSchema(v int) bool {
	return v == ProbeBusinessSchemaVersion || HasHealthSchema(v)
}
func HasHealthSchema(v int) bool {
	return v == HealthBusinessSchemaVersion || HasHealthQueueSchema(v)
}
func HasHealthQueueSchema(v int) bool {
	return v == HealthQueueBusinessSchemaVersion || HasHealthRoundsSchema(v)
}

var healthQueueSchema = []string{
	`CREATE TABLE pro_health_intents(id TEXT PRIMARY KEY,handover_id TEXT NOT NULL REFERENCES native_metered_handovers(id),node_id TEXT NOT NULL,installation_id TEXT NOT NULL,inbound_id INTEGER NOT NULL,endpoint_sha TEXT NOT NULL,binding TEXT NOT NULL,expires INTEGER NOT NULL,created INTEGER NOT NULL)`,
	`CREATE INDEX pro_health_intents_node ON pro_health_intents(node_id,installation_id,created)`,
	`CREATE TRIGGER pro_health_intents_no_update BEFORE UPDATE ON pro_health_intents BEGIN SELECT RAISE(ABORT,'health intent immutable'); END`,
	`CREATE TRIGGER pro_health_intents_no_delete BEFORE DELETE ON pro_health_intents BEGIN SELECT RAISE(ABORT,'health intent retained'); END`,
}

var healthSchema = []string{
	`CREATE TABLE pro_health_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`INSERT INTO pro_health_meta VALUES(1,1)`,
	`CREATE TABLE pro_health_challenges(id TEXT PRIMARY KEY,intent_id TEXT NOT NULL UNIQUE,handover_id TEXT NOT NULL REFERENCES native_metered_handovers(id),intent_sha TEXT NOT NULL,request TEXT NOT NULL,expires INTEGER NOT NULL)`,
	`CREATE INDEX pro_health_handover ON pro_health_challenges(handover_id,expires)`,
	`CREATE TABLE pro_health_receipts(challenge_id TEXT PRIMARY KEY REFERENCES pro_health_challenges(id),handover_id TEXT NOT NULL UNIQUE REFERENCES native_metered_handovers(id),receipt TEXT NOT NULL,accepted INTEGER NOT NULL)`,
	`CREATE TRIGGER pro_health_challenges_no_update BEFORE UPDATE ON pro_health_challenges BEGIN SELECT RAISE(ABORT,'health challenge immutable'); END`,
	`CREATE TRIGGER pro_health_challenges_no_delete BEFORE DELETE ON pro_health_challenges BEGIN SELECT RAISE(ABORT,'health challenge retained'); END`,
	`CREATE TRIGGER pro_health_receipts_no_update BEFORE UPDATE ON pro_health_receipts BEGIN SELECT RAISE(ABORT,'health receipt immutable'); END`,
	`CREATE TRIGGER pro_health_receipts_no_delete BEFORE DELETE ON pro_health_receipts BEGIN SELECT RAISE(ABORT,'health receipt retained'); END`,
	`CREATE TRIGGER pro_health_receipt_scope BEFORE INSERT ON pro_health_receipts WHEN NOT EXISTS(SELECT 1 FROM pro_health_challenges WHERE id=NEW.challenge_id AND handover_id=NEW.handover_id) BEGIN SELECT RAISE(ABORT,'health receipt scope mismatch'); END`,
}

func verifyHealthSchema(ctx context.Context, c *ledgerConn, version int) error {
	if err := verifyLabSubscriptionsSchema(ctx, c, version); err != nil {
		return err
	}
	if !HasHealthSchema(version) {
		var n int
		if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_health_*'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrSchema
		}
		return nil
	}
	statements := append([]string(nil), healthSchema...)
	if HasHealthQueueSchema(version) {
		statements = append(statements, healthQueueSchema...)
	}
	if HasHealthRoundsSchema(version) {
		statements = append(statements, healthRoundsSchema...)
	}
	if err := verifySchemaObjects(ctx, c, statements, "pro_health_*"); err != nil {
		return err
	}
	var n int
	if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_health_meta WHERE id=1 AND version=1`).Scan(&n); err != nil || n != 1 {
		return ErrSchema
	}
	return nil
}

// EnableHealthChallenges creates no challenge and grants no runtime authority.
func (l *Ledger) EnableHealthChallenges(ctx context.Context) error {
	return l.write(ctx, func(c *ledgerConn) error {
		var v int
		if err := c.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
			return err
		}
		if !HasProbeSchema(v) {
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
		if HasHealthSchema(v) {
			return nil
		}
		for _, ddl := range healthSchema {
			if _, err := c.ExecContext(ctx, ddl); err != nil {
				return err
			}
		}
		_, err := c.ExecContext(ctx, `PRAGMA user_version=10`)
		return err
	})
}

func (l *Ledger) EnableHealthQueue(ctx context.Context) error {
	return l.write(ctx, func(c *ledgerConn) error {
		var v int
		if err := c.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
			return err
		}
		if !HasHealthSchema(v) {
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
		if HasHealthQueueSchema(v) {
			return nil
		}
		for _, ddl := range healthQueueSchema {
			if _, err := c.ExecContext(ctx, ddl); err != nil {
				return err
			}
		}
		_, err := c.ExecContext(ctx, `PRAGMA user_version=11`)
		return err
	})
}
