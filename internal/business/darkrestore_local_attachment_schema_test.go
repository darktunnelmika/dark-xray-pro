package business

import (
	"context"
	"errors"
	"testing"
)

func TestDarkRestoreOptionalLocalSettlementUpgradeAndDowngradeGuard(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	req := importRequest(t, f, syntheticImportIntent(1), "darkrestore-optional-upgrade-0001")
	original, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		"DROP TRIGGER darkrestore_local_settlements_v1_no_update",
		"DROP TRIGGER darkrestore_local_settlements_v1_no_delete",
		"DROP TABLE darkrestore_local_settlements_v1",
		"PRAGMA application_id=0",
	} {
		if _, err = f.s.db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("prior M2 schema was rejected", err)
	}
	if err = f.s.EnableDarkRestoreImportV1(ctx); err != nil {
		t.Fatal("in place optional upgrade", err)
	}
	var app int
	if err = f.s.db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil || app != darkRestoreLocalAttachmentApplicationID {
		t.Fatal("new schema has no durable downgrade marker", app, err)
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil || !replay.Replayed || replay.EventID != original.EventID {
		t.Fatal("original import receipt changed on upgrade", err)
	}
	for _, ddl := range []string{
		"DROP TRIGGER darkrestore_local_settlements_v1_no_update",
		"DROP TRIGGER darkrestore_local_settlements_v1_no_delete",
		"DROP TABLE darkrestore_local_settlements_v1",
	} {
		if _, err = f.s.db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); !errors.Is(err, ErrUnavailable) {
		t.Fatal("lost local epoch table accepted as older schema", err)
	}
}
