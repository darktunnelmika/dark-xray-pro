package usageledger

import "context"

// Explicit startup-only opt-in. Legacy health rows remain unchanged and never
// count as new round coverage. Older readers must reject this database.
const HealthRoundsBusinessSchemaVersion = 12

func HasHealthRoundsSchema(v int) bool { return v == HealthRoundsBusinessSchemaVersion }

var healthRoundsSchema = []string{
	`CREATE TABLE pro_health_rounds(id TEXT PRIMARY KEY,handover_id TEXT NOT NULL REFERENCES native_metered_handovers(id),node_id TEXT NOT NULL,installation_id TEXT NOT NULL,endpoint_sha TEXT NOT NULL,manifest TEXT NOT NULL,manifest_sha TEXT NOT NULL,created INTEGER NOT NULL,expires INTEGER NOT NULL,CHECK(expires>created))`,
	`CREATE INDEX pro_health_rounds_handover ON pro_health_rounds(handover_id,created,id)`,
	`CREATE TABLE pro_health_round_members(round_id TEXT NOT NULL REFERENCES pro_health_rounds(id),inbound_id INTEGER NOT NULL,intent_id TEXT NOT NULL UNIQUE REFERENCES pro_health_intents(id),binding TEXT NOT NULL,PRIMARY KEY(round_id,inbound_id))`,
	`CREATE TABLE pro_health_round_receipts(challenge_id TEXT PRIMARY KEY REFERENCES pro_health_challenges(id),round_id TEXT NOT NULL,inbound_id INTEGER NOT NULL,receipt TEXT NOT NULL,accepted INTEGER NOT NULL,UNIQUE(round_id,inbound_id),FOREIGN KEY(round_id,inbound_id) REFERENCES pro_health_round_members(round_id,inbound_id))`,
	`CREATE TRIGGER pro_health_rounds_no_update BEFORE UPDATE ON pro_health_rounds BEGIN SELECT RAISE(ABORT,'health round immutable'); END`,
	`CREATE TRIGGER pro_health_rounds_no_delete BEFORE DELETE ON pro_health_rounds BEGIN SELECT RAISE(ABORT,'health round retained'); END`,
	`CREATE TRIGGER pro_health_round_members_no_update BEFORE UPDATE ON pro_health_round_members BEGIN SELECT RAISE(ABORT,'health round member immutable'); END`,
	`CREATE TRIGGER pro_health_round_members_no_delete BEFORE DELETE ON pro_health_round_members BEGIN SELECT RAISE(ABORT,'health round member retained'); END`,
	`CREATE TRIGGER pro_health_round_receipts_no_update BEFORE UPDATE ON pro_health_round_receipts BEGIN SELECT RAISE(ABORT,'health round receipt immutable'); END`,
	`CREATE TRIGGER pro_health_round_receipts_no_delete BEFORE DELETE ON pro_health_round_receipts BEGIN SELECT RAISE(ABORT,'health round receipt retained'); END`,
	`CREATE TRIGGER pro_health_round_receipt_scope BEFORE INSERT ON pro_health_round_receipts WHEN NOT EXISTS(SELECT 1 FROM pro_health_round_members m JOIN pro_health_rounds r ON r.id=m.round_id JOIN pro_health_intents i ON i.id=m.intent_id JOIN pro_health_challenges c ON c.intent_id=m.intent_id WHERE c.id=NEW.challenge_id AND m.round_id=NEW.round_id AND m.inbound_id=NEW.inbound_id AND c.handover_id=r.handover_id AND i.handover_id=r.handover_id AND i.node_id=r.node_id AND i.installation_id=r.installation_id AND i.inbound_id=m.inbound_id AND i.binding=m.binding AND i.endpoint_sha=r.endpoint_sha AND NEW.accepted>=r.created AND NEW.accepted<r.expires) BEGIN SELECT RAISE(ABORT,'health round receipt scope mismatch'); END`,
}

// EnableHealthRounds adds only the reviewed round audit schema. It creates no
// health work, probe identities, runtime authority or publication state.
func (l *Ledger) EnableHealthRounds(ctx context.Context) error {
	return l.write(ctx, func(c *ledgerConn) error {
		var v int
		if err := c.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
			return err
		}
		if !HasHealthQueueSchema(v) {
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
		if HasHealthRoundsSchema(v) {
			return nil
		}
		for _, ddl := range healthRoundsSchema {
			if _, err := c.ExecContext(ctx, ddl); err != nil {
				return err
			}
		}
		_, err := c.ExecContext(ctx, `PRAGMA user_version=12`)
		return err
	})
}
