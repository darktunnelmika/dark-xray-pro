package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestOwnerOnlyRepresentativeDirectoryQueryAndRealCounts(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	if _, err := s.business.CreateRepresentative(ctx, "rep-search", "نماینده سرچ"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.CreateRepresentative(ctx, "rep-other", "Other"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx,
		"rep-search", "rep-directory-user", "private-directory-password-123"); err != nil {
		t.Fatal(err)
	}
	ownerCookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	repCookie, _ := browserLogin(t, s, "rep-directory-user", "private-directory-password-123")
	read := func(cookie *http.Cookie, path string) (int, map[string]any) {
		resp := browserRequest(t, s.Handler(), "GET", path, nil, cookie, "", "")
		var data map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return resp.Code, data
	}
	path := "/api/v1/workspace/representatives?limit=25&offset=0&q=%D9%86%D9%85%D8%A7%DB%8C%D9%86%D8%AF%D9%87&state=all&sort=name"
	if code, _ := read(nil, path); code != 401 {
		t.Fatalf("anonymous reps exposed: %d", code)
	}
	if code, _ := read(repCookie, path); code != 403 {
		t.Fatalf("representative enumerated other accounts: %d", code)
	}
	if code, result := read(ownerCookie, path); code != 200 ||
		result["total"] != float64(1) || result["has_more"] != false || result["q"] != "نماینده" {
		t.Fatalf("owner search failed: %d %+v", code, result)
	} else {
		rows := result["representatives"].([]any)
		row := rows[0].(map[string]any)
		if row["id"] != "rep-search" || row["client_count"] != float64(0) || row["archived_count"] != float64(0) {
			t.Fatalf("record counters were invented: %+v", row)
		}
	}
	for _, url := range []string{
		"/api/v1/workspace/representatives?limit=201",
		"/api/v1/workspace/representatives?q=A&q=B",
		"/api/v1/workspace/representatives?representative_id=rep-search",
		"/api/v1/workspace/representatives?state=active",
		"/api/v1/workspace/representatives?sort=%3BDROP",
		"/api/v1/workspace/representatives?unknown=x",
	} {
		if code, _ := read(ownerCookie, url); code != 422 {
			t.Fatalf("unsafe representative lookup allowed %s -> %d", url, code)
		}
	}
	if code := browserRequest(t, s.AgentHandler(), "GET", path, nil, ownerCookie, "", "").Code; code == 200 {
		t.Fatal("TLS Agent interface leaked owner directory")
	}
	// Neither a directory request nor a rejected query may alter credit.
	rep, err := s.business.Representative(ctx, "rep-search")
	if err != nil || rep.VolumeCreditBytes != 0 || rep.UnlimitedCreditUnits != 0 {
		t.Fatalf("owner directory changed credit %+v %v", rep, err)
	}
}
