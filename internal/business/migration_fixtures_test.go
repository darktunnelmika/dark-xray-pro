package business

import (
	"strings"
	"testing"
)

// Historical fixtures start from Open's current schema before reconstructing
// an older release. Promotion did not exist in those releases, so remove its
// empty additive schema before dropping lifecycle columns referenced by its
// immutable guards. This helper is test-only and refuses to erase ownership.
func removePromotionSchemaForHistoricalFixture(t *testing.T, s *Store) {
	t.Helper()
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	tables := []string{"native_inbound_plan_promotion_clients", "native_inbound_plan_promotions", "native_inbound_plan_promotion_nodes"}
	for _, table := range tables {
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("historical fixture contains promotion ownership in %s", table)
		}
	}
	rows, err := tx.Query(`SELECT name FROM sqlite_master WHERE type='trigger' AND
 (name GLOB 'native_promotion_*' OR name IN ('native_plan_block_enable','native_plan_block_target_insert','native_plan_block_client_bind')) ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var triggers []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		triggers = append(triggers, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range triggers {
		if _, err = tx.Exec(`DROP TRIGGER "` + strings.ReplaceAll(name, `"`, `""`) + `"`); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range tables {
		if _, err = tx.Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Restore the strict authoring guards that existed before promotion. Each
	// fixture can now remove its documented historical columns as before.
	if err = ensureNativeInboundPlans(s.db); err != nil {
		t.Fatal(err)
	}
}
