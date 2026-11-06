package api

import (
	"context"
	"testing"
)

func TestRepresentativeServiceGrantOwnerBoundary(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	if _, e := s.business.CreateRepresentative(ctx, "grant-rep", "Grant rep"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.business.ProvisionRepresentativeLogin(ctx, "grant-rep", "grant-login", "grant-safe-password-12345"); e != nil {
		t.Fatal(e)
	}
	owner, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	rep, repCSRF := browserLogin(t, s, "grant-login", "grant-safe-password-12345")
	path := "/api/v1/workspace/representatives/grant-rep/service-grants/10"
	if w := browserRequest(t, s.Handler(), "GET", path, nil, nil, "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := browserRequest(t, s.Handler(), "GET", path, nil, rep, "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	body := map[string]any{"event_id": "grant-api-event-00001", "expected_revision": 0, "enabled": true, "reason": "Grant test"}
	if w := browserRequest(t, s.Handler(), "POST", path, body, rep, repCSRF, "https://"+browserHost); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", path, body, owner, "", "https://"+browserHost); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", path, body, owner, csrf, "https://foreign.invalid"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	delete(body, "enabled")
	if w := browserRequest(t, s.Handler(), "POST", path, body, owner, csrf, "https://"+browserHost); w.Code != 422 {
		t.Fatalf("missing explicit decision %d %s", w.Code, w.Body.String())
	}
}
