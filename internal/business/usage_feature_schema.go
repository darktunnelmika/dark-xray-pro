package business

import (
	"context"
	"database/sql"
)

// usageFeatureSchemaVersion resolves the immutable accounting feature chain
// retained beneath managed inbound execution's v16 reader fence. It never
// enables a feature and rejects a malformed/missing v16 base pin.
func usageFeatureSchemaVersion(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int, error) {
	var outer int
	if err := q.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&outer); err != nil {
		return 0, err
	}
	if outer != 16 {
		return outer, nil
	}
	var managed, base int
	if err := q.QueryRowContext(ctx, `SELECT version,base_schema FROM inbound_execution_meta WHERE id=1`).Scan(&managed, &base); err != nil {
		return 0, ErrUnavailable
	}
	if managed != 1 || base < 8 || base > 15 {
		return 0, ErrUnavailable
	}
	return base, nil
}
