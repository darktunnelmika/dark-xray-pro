package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestOwnerAndRepresentativeGlobalClientSearchIsolation(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	if _, err := s.business.CreateRepresentative(ctx, "rep-a", "Representative A"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.CreateRepresentative(ctx, "rep-b", "Representative B"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.AdjustCredits(ctx, business.CreditChange{
		EventID: "global-search-grant-00001", RepresentativeID: "rep-a", VolumeDeltaBytes: 1000,
		Reason: "search test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.AdjustCredits(ctx, business.CreditChange{
		EventID: "global-search-grant-00002", RepresentativeID: "rep-b", VolumeDeltaBytes: 1000,
		Reason: "search test",
	}); err != nil {
		t.Fatal(err)
	}
	for i, item := range []struct{ id, rep, name string }{
		{"secret-a", "rep-a", "Private A only"},
		{"secret-b", "rep-b", "Private B only"},
		{"owner-test", "owner", "Owner Q search"},
	} {
		if _, err := s.business.CreateClient(ctx, business.NewClient{
			EventID:  "api-global-create-00" + strings.Repeat("0", 9) + string(rune('0'+i)),
			ClientID: item.id, RepresentativeID: item.rep, Name: item.name,
			DurationDays: 30, VolumeBytes: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "rep-a", "rep-search-a", "private-rep-search-passphrase-123"); err != nil {
		t.Fatal(err)
	}
	ownerCookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	repCookie, _ := browserLogin(t, s, "rep-search-a", "private-rep-search-passphrase-123")
	read := func(cookie *http.Cookie, path string) (int, map[string]any) {
		r := browserRequest(t, s.Handler(), "GET", path, nil, cookie, "", "")
		var data map[string]any
		if err := json.Unmarshal(r.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return r.Code, data
	}
	if code, _ := read(nil, "/api/v1/workspace/clients?q=private&limit=50"); code != 401 {
		t.Fatalf("anonymous enumerated clients: %d", code)
	}
	if code, r := read(repCookie, "/api/v1/workspace/clients?q=private&state=all&sort=name&plan=volume&limit=25"); code != 200 ||
		r["total"] != float64(1) || r["representative_id"] != "rep-a" ||
		len(r["clients"].([]any)) != 1 || r["has_more"] != false {
		t.Fatalf("representative search failed: %d %+v", code, r)
	}
	if code, r := read(repCookie, "/api/v1/workspace/clients?q=secret-b&limit=25"); code != 200 ||
		r["total"] != float64(0) || len(r["clients"].([]any)) != 0 {
		t.Fatalf("representative searched other rep: %d %+v", code, r)
	}
	if code, _ := read(repCookie, "/api/v1/workspace/clients?representative_id=rep-b&q=secret"); code != 403 {
		t.Fatalf("representative chose forbidden account: %d", code)
	}
	if code, r := read(ownerCookie, "/api/v1/workspace/clients?representative_id=rep-b&q=secret&limit=25"); code != 200 ||
		r["total"] != float64(1) || len(r["clients"].([]any)) != 1 {
		t.Fatalf("owner scoped search %d %+v", code, r)
	}
	if code, r := read(ownerCookie, "/api/v1/workspace/clients?limit=2&offset=0&sort=name"); code != 200 ||
		r["total"] != float64(3) || r["has_more"] != true || len(r["clients"].([]any)) != 2 {
		t.Fatalf("page-1 count/sort invalid: %d %+v", code, r)
	}
	if code, r := read(ownerCookie, "/api/v1/workspace/clients?limit=2&offset=2&sort=name"); code != 200 ||
		r["total"] != float64(3) || r["has_more"] != false || len(r["clients"].([]any)) != 1 {
		t.Fatalf("page-2 count/sort invalid: %d %+v", code, r)
	}
	for _, path := range []string{
		"/api/v1/workspace/clients?sort=%3BDELETE+FROM+clients",
		"/api/v1/workspace/clients?limit=201",
		"/api/v1/workspace/clients?state=live",
		"/api/v1/workspace/clients?unknown=x",
		"/api/v1/workspace/clients?q=A&q=B",
		"/api/v1/workspace/clients?representative_id=rep-a&representative_id=rep-b",
	} {
		if code, _ := read(ownerCookie, path); code != 422 {
			t.Fatalf("unsafe query unexpectedly permitted (%s) -> %d", path, code)
		}
	}
	// The private Agent-only TLS interface must never gain browser finances.
	if status := browserRequest(t, s.AgentHandler(), "GET", "/api/v1/workspace/clients?q=secret", nil, ownerCookie, "", "").Code; status == 200 {
		t.Fatal("Agent interface exported private client directory")
	}
}
