package business

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestServiceGrantOfflineBackupOldAndIncompleteSchema(t *testing.T) {
	ctx := context.Background()
	old := offlineValidationFixture(t, false)
	mutateOfflineFixture(t, old, "DROP TRIGGER service_grants_no_update", "DROP TRIGGER service_grants_no_delete", "DROP TABLE representative_service_grants")
	if _, e := ValidateOfflineBackup(ctx, old); e != nil {
		t.Fatalf("legacy %v", e)
	}
	broken := offlineValidationFixture(t, false)
	mutateOfflineFixture(t, broken, "DROP TRIGGER service_grants_no_delete")
	if _, e := ValidateOfflineBackup(ctx, broken); !errors.Is(e, ErrOfflineBackupInvalid) {
		t.Fatalf("partial schema %v", e)
	}
}
func TestServiceGrantOfflineBackupRejectsContradictoryHistory(t *testing.T) {
	s, path := opened(t)
	ctx := context.Background()
	newRep(t, s, "backup-grant")
	if _, e := s.BootstrapOwner(ctx, "backup-owner", "backup-owner-password-12345"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateInbound(ctx, bytes.Repeat([]byte{5}, 32), nativeInput()); e != nil {
		t.Fatal(e)
	}
	in := ServiceGrantChange{EventID: "backup-grant-event-0001", RepresentativeID: "backup-grant", InboundID: 10, Enabled: true, Reason: "Backup grant"}
	if _, e := s.ChangeRepresentativeServiceGrant(ctx, "owner", in); e != nil {
		t.Fatal(e)
	}
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e := ValidateOfflineBackup(ctx, path); e != nil {
		t.Fatalf("valid grant backup %v", e)
	}
	mutateOfflineFixture(t, path, "DROP TRIGGER service_grants_no_update", "UPDATE representative_service_grants SET revision=3", serviceGrantSchema[1])
	if _, e := ValidateOfflineBackup(ctx, path); !errors.Is(e, ErrOfflineBackupInvalid) {
		t.Fatalf("revision gap accepted %v", e)
	}

}
