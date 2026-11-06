package business

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestRepresentativeServiceGrantAuditReplayAndRevocation(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	newRep(t, s, "grant-rep")
	if _, e := s.BootstrapOwner(ctx, "grant-owner", "grant-password-safe-12345"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateInbound(ctx, bytes.Repeat([]byte{5}, 32), nativeInput()); e != nil {
		t.Fatal(e)
	}
	in := ServiceGrantChange{EventID: "service-grant-event-0001", RepresentativeID: "grant-rep", InboundID: 10, Enabled: true, Reason: "Allow selected service"}
	if _, e := s.ChangeRepresentativeServiceGrant(ctx, "foreign", in); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal(e)
	}
	g, e := s.ChangeRepresentativeServiceGrant(ctx, "owner", in)
	if e != nil || g.Revision != 1 || !g.Enabled {
		t.Fatalf("grant %v %v", g, e)
	}
	replay, e := s.ChangeRepresentativeServiceGrant(ctx, "owner", in)
	if e != nil || replay != g {
		t.Fatalf("replay %v %v", replay, e)
	}
	in.Reason = "different"
	if _, e = s.ChangeRepresentativeServiceGrant(ctx, "owner", in); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	in.EventID = "service-grant-event-0002"
	in.Enabled = false
	if _, e = s.ChangeRepresentativeServiceGrant(ctx, "owner", in); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	in.ExpectedRevision = 1
	g, e = s.ChangeRepresentativeServiceGrant(ctx, "owner", in)
	if e != nil || g.Enabled || g.Revision != 2 {
		t.Fatalf("revoke %v %v", g, e)
	}
	current, e := s.RepresentativeServiceGrant(ctx, "grant-rep", 10)
	if e != nil || current != g {
		t.Fatalf("read %v %v", current, e)
	}
	if _, e = s.db.Exec("DELETE FROM representative_service_grants"); e == nil {
		t.Fatal("mutable audit")
	}
	if _, e = s.db.Exec("UPDATE representative_service_grants SET enabled=1"); e == nil {
		t.Fatal("mutable audit")
	}
	if e = validateServiceGrantData(ctx, s.db); e != nil {
		t.Fatal(e)
	}
	var count int
	s.db.QueryRow("SELECT count(*) FROM representative_service_grants").Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
}
