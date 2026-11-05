package business

import (
	"context"
	"errors"
	"testing"
)

func TestManualClientIdentityReplayCollisionAndScope(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "manual-rep")
	grant(t, s, "manual-rep", "manual-credit-grant-001", 1000, 10)
	n := NewClient{EventID: "manual-client-create-001", ClientID: "manual-a", RepresentativeID: "manual-rep", Name: "Manual client", DurationDays: 30, VolumeBytes: 100, UUID: "12345678-1234-4234-8234-123456789abc"}
	c, err := s.CreateClientScoped(ctx, n, "manual-rep")
	if err != nil || c.UUID != n.UUID {
		t.Fatal(c, err)
	}
	if _, err = s.CreateClientScoped(ctx, n, "manual-rep"); err != nil {
		t.Fatal("replay", err)
	}
	rep, _ := s.Representative(ctx, "manual-rep")
	if rep.VolumeCreditBytes != 900 {
		t.Fatal("double charge", rep)
	}
	n.EventID = "manual-client-create-002"
	n.ClientID = "manual-b"
	if _, err = s.CreateClientScoped(ctx, n, "manual-rep"); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate identity accepted", err)
	}
	rep, _ = s.Representative(ctx, "manual-rep")
	if rep.VolumeCreditBytes != 900 {
		t.Fatal("collision charged", rep)
	}
	n.UUID = "bad"
	if _, err = s.CreateClientScoped(ctx, n, "manual-rep"); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid identity", err)
	}
	n.UUID = "12345678-1234-4234-8234-123456789abd"
	if _, err = s.CreateClientScoped(ctx, n, "other-rep"); !errors.Is(err, ErrOwnershipDenied) {
		t.Fatal("cross tenant", err)
	}
}
