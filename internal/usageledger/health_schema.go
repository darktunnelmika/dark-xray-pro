package usageledger

import "context"

// HealthBusinessSchemaVersion is an explicit startup-only opt-in. Older HUBs
// must refuse this database rather than forget consumed health challenges.
const HealthBusinessSchemaVersion = 10

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
	if version != HealthBusinessSchemaVersion {
		var n int
		if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_health_*'`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrSchema
		}
		return nil
	}
	if err := verifySchemaObjects(ctx, c, healthSchema, "pro_health_*"); err != nil {
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
		if v != ProbeBusinessSchemaVersion && v != HealthBusinessSchemaVersion {
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
		if v == HealthBusinessSchemaVersion {
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
