//go:build linux

package usageledger

import (
	"context"
	"errors"
	"testing"
)

func TestUsageSchemaRejectsMissingAuditTriggerAndAlteredPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changes []string
	}{
		{"missing-receipt-protection", []string{"DROP TRIGGER pro_usage_receipts_no_delete"}},
		{"fake-eligibility", []string{"DROP VIEW pro_usage_eligibility", "CREATE VIEW pro_usage_eligibility AS SELECT id AS client_id,1 AS eligible FROM clients"}},
		{"unexpected-private-policy", []string{"CREATE TABLE pro_usage_unreviewed(client_id TEXT)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := testDB(t)
			ctx := context.Background()
			if _, e := Open(ctx, db); e != nil {
				t.Fatal(e)
			}
			if _, e := Open(ctx, db); e != nil {
				t.Fatal("valid schema failed second open", e)
			}
			for _, sql := range tc.changes {
				if _, e := db.Exec(sql); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := Open(ctx, db); !errors.Is(e, ErrSchema) {
				t.Fatalf("damaged metering schema was accepted: %v", e)
			}
		})
	}
}
