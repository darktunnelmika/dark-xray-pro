package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnifiedBrowserSurfaceRequiresOwnerSessionForFleet(t *testing.T) {
	s := enabledOwner(t)
	h := s.BrowserHandler()
	get := func(cookie *http.Cookie, token string) int {
		r := httptest.NewRequest("GET", "/api/v1/fleet", nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if got := get(nil, ""); got != 401 {
		t.Fatalf("anonymous fleet: %d", got)
	}
	if got := get(nil, ownerToken); got != 401 {
		t.Fatalf("browser bearer bypass: %d", got)
	}
	ownerCookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	if got := get(ownerCookie, ""); got != 200 {
		t.Fatalf("owner fleet: %d", got)
	}
	ctx := context.Background()
	if _, err := s.business.CreateRepresentative(ctx, "shellrep", "Shell representative"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "shellrep", "shellrep", repPassword); err != nil {
		t.Fatal(err)
	}
	repCookie, _ := browserLogin(t, s, "shellrep", repPassword)
	if got := get(repCookie, ""); got != 403 {
		t.Fatalf("representative saw owner fleet: %d", got)
	}
	if err := s.business.Logout(ctx, ownerCookie.Value); err != nil {
		t.Fatal(err)
	}
	if got := get(ownerCookie, ""); got != 401 {
		t.Fatalf("revoked session: %d", got)
	}
	r := httptest.NewRequest("GET", "/api/v1/session/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("public session discovery unavailable")
	}
}
