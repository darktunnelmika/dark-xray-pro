package usageledger

import (
	"context"
	"strings"
)

// A database with the right columns but a missing immutable-audit trigger or
// an altered eligibility view is not the reviewed schema. Verify the exact
// versioned DDL at startup, rather than silently adopting damaged policy.
func verifySchema(ctx context.Context, c *ledgerConn) error {
	return verifySchemaObjects(ctx, c, schema, "pro_usage_*")
}

func verifySchemaObjects(ctx context.Context, c *ledgerConn, statements []string, pattern string) error {
	expected := make(map[string]string)
	kinds := make(map[string]string)
	canonical := func(raw string) string { return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ") }
	for _, ddl := range statements {
		fields := strings.Fields(ddl)
		if len(fields) < 3 || fields[0] != "CREATE" {
			continue
		}
		name := strings.SplitN(fields[2], "(", 2)[0]
		expected[name] = canonical(ddl)
		kinds[name] = strings.ToLower(fields[1])
	}
	rows, err := c.QueryContext(ctx, "SELECT name,type,sql FROM sqlite_master WHERE name GLOB ?", pattern)
	if err != nil {
		return ErrSchema
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var name, kind, ddl string
		if rows.Scan(&name, &kind, &ddl) != nil {
			return ErrSchema
		}
		wanted, ok := expected[name]
		if !ok || kind != kinds[name] || canonical(ddl) != wanted {
			return ErrSchema
		}
		found++
	}
	if rows.Err() != nil || found != len(expected) {
		return ErrSchema
	}
	return nil
}
