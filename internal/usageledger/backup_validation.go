package usageledger

import (
	"context"
	"database/sql"
)

// ValidateReadOnlySchema checks an already checkpointed, immutable snapshot.
// It never calls Open, enables a feature, repairs a policy, or starts a writer.
// The caller must open the DB read-only and set query_only=1/trusted_schema=0.
func ValidateReadOnlySchema(ctx context.Context, db *sql.DB) error {
	if db == nil || ctx == nil {
		return ErrSchema
	}
	raw, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer raw.Close()
	c := &ledgerConn{raw}
	var queryOnly, trusted, version int
	if c.QueryRowContext(ctx, `PRAGMA query_only`).Scan(&queryOnly) != nil || queryOnly != 1 ||
		c.QueryRowContext(ctx, `PRAGMA trusted_schema`).Scan(&trusted) != nil || trusted != 0 ||
		c.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version) != nil {
		return ErrSchema
	}
	managed := version == 16
	if version != 8 && !managed {
		return ErrSchema
	}
	version, err = underlyingBusinessSchema(ctx, c, version)
	if err != nil || version < 8 || version > 15 {
		return ErrSchema
	}
	if err := verifyHealthSchema(ctx, c, version); err != nil {
		return err
	}
	if err := verifyProbeSchema(ctx, c, version); err != nil {
		return err
	}
	var count int
	if c.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_usage_*'`).Scan(&count) != nil {
		return ErrSchema
	}
	if count == 0 {
		if managed || HasProbeSchema(version) {
			return ErrSchema
		}
		return ctx.Err()
	}
	var usageVersion int
	if c.QueryRowContext(ctx, `SELECT version FROM pro_usage_meta WHERE id=1`).Scan(&usageVersion) != nil || usageVersion != SchemaVersion {
		return ErrSchema
	}
	if err := verifySchema(ctx, c); err != nil {
		return err
	}
	if c.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_config_generation WHERE id=1 AND value>=1 AND value<9007199254740991`).Scan(&count) != nil || count != 1 {
		return ErrSchema
	}
	return ctx.Err()
}
