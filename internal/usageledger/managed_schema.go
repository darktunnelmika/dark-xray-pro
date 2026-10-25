package usageledger

import "context"

// Managed execution's reader fence is independent of the historical usage
// feature chain. Retain and verify the exact earlier schema instead of
// implicitly enabling probe/health/subscription features during migration.
func underlyingBusinessSchema(ctx context.Context, c sqlWriter, version int) (int, error) {
	var count int
	if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name='inbound_execution_meta'`).Scan(&count); err != nil {
		return 0, err
	}
	if version != 16 {
		if count != 0 {
			return 0, ErrSchema
		}
		return version, nil
	}
	var base, managed int
	if count != 1 || c.QueryRowContext(ctx, `SELECT version,base_schema FROM inbound_execution_meta WHERE id=1`).Scan(&managed, &base) != nil || managed != 1 || base < 8 || base > 15 {
		return 0, ErrSchema
	}
	return base, nil
}
