package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
	"github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)

func TestOwnerBrowserReplacementInspectionAndCancelBoundary(t *testing.T) {
	s := enabledOwner(t)
	for _, id := range []string{"old-de", "new-de"} {
		ticket, err := s.registry.Issue(id, id)
		if err != nil {
			t.Fatal(err)
		}
		install := strings.Repeat("a", 32)
		token := strings.Repeat("b", 64)
		if id == "new-de" {
			install = newInstallation
			token = newToken
		}
		if _, err = s.registry.Claim(ticket.PairCode, id, install, token); err != nil {
			t.Fatal(err)
		}
	}
	j, err := replacement.Open(filepath.Join(t.TempDir(), "private", "journal.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnableReplacementLab(j); err != nil {
		t.Fatal(err)
	}
	h := s.BrowserHandler()
	owner, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	path := "/api/v1/workspace/nodes/old-de/replacements"
	if w := browserRequest(t, h, "GET", path, nil, nil, "", ""); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "replace-rep", "Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "replace-rep", "replace-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	rep, repCSRF := browserLogin(t, s, "replace-rep", repPassword)
	if w := browserRequest(t, h, "GET", path, nil, rep, "", ""); w.Code != 403 {
		t.Fatalf("representative read: %d", w.Code)
	}
	if w := browserRequest(t, h, "GET", "/api/v1/workspace/nodes/missing/replacements", nil, owner, "", ""); w.Code != 404 {
		t.Fatalf("missing node: %d", w.Code)
	}
	read := func(id string) []replacement.Operation {
		t.Helper()
		w := browserRequest(t, h, "GET", "/api/v1/workspace/nodes/"+id+"/replacements", nil, owner, "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), newToken) || strings.Contains(w.Body.String(), nodeToken) {
			t.Fatalf("unsafe read: %d %s", w.Code, w.Body.String())
		}
		var result struct {
			NodeID      string                  `json:"node_id"`
			Operations  []replacement.Operation `json:"operations"`
			CanActivate bool                    `json:"can_activate"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.NodeID != id || result.CanActivate || result.Operations == nil {
			t.Fatalf("invalid read: %v %s", err, w.Body.String())
		}
		return result.Operations
	}
	if len(read("old-de")) != 0 {
		t.Fatal("unexpected operation")
	}
	op, err := j.Create(reservationPlanForBrowser(t), newInstallation)
	if err != nil {
		t.Fatal(err)
	}
	if len(read("old-de")) != 1 || len(read("new-de")) != 1 {
		t.Fatal("node roles missing")
	}
	other := path + "/" + op.ID + "/cancel"
	for _, tc := range []struct {
		cookie       *http.Cookie
		csrf, origin string
		want         int
	}{
		{nil, "", "", 401}, {rep, repCSRF, "https://" + browserHost, 403},
		{owner, "", "https://" + browserHost, 403}, {owner, csrf, "https://evil.example", 403},
	} {
		w := browserRequest(t, h, "POST", other, map[string]any{"expected_revision": op.Revision}, tc.cookie, tc.csrf, tc.origin)
		if w.Code != tc.want {
			t.Fatalf("unsafe cancel: %d want %d", w.Code, tc.want)
		}
	}
	if w := browserRequest(t, h, "POST", other, map[string]any{"expected_revision": op.Revision + 1}, owner, csrf, "https://"+browserHost); w.Code != 409 {
		t.Fatalf("wrong revision: %d", w.Code)
	}
	if w := browserRequest(t, h, "POST", other, map[string]any{"expected_revision": 1.5}, owner, csrf, "https://"+browserHost); w.Code != 400 {
		t.Fatalf("fractional revision: %d", w.Code)
	}
	ticket, err := s.registry.Issue("unrelated", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.registry.Claim(ticket.PairCode, "unrelated", strings.Repeat("c", 32), strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if w := browserRequest(t, h, "POST", "/api/v1/workspace/nodes/unrelated/replacements/"+op.ID+"/cancel", map[string]any{"expected_revision": op.Revision}, owner, csrf, "https://"+browserHost); w.Code != 404 {
		t.Fatalf("foreign node: %d", w.Code)
	}
	if w := browserRequest(t, h, "POST", other, map[string]any{"expected_revision": op.Revision}, owner, csrf, "https://"+browserHost); w.Code != 200 {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	if w := browserRequest(t, h, "POST", other, map[string]any{"expected_revision": op.Revision}, owner, csrf, "https://"+browserHost); w.Code != 200 {
		t.Fatalf("lost reply replay: %d", w.Code)
	}
	rows := read("old-de")
	if len(rows) != 1 || rows[0].Phase != replacement.Cancelled || rows[0].CanActivate {
		t.Fatal("history not terminal")
	}
	if w := browserRequest(t, h, "POST", path+"/"+op.ID+"/activate", map[string]any{}, owner, csrf, "https://"+browserHost); w.Code != 404 {
		t.Fatalf("activation exposed: %d", w.Code)
	}
}

func reservationPlanForBrowser(t *testing.T) routeplan.Preview {
	t.Helper()
	draft, err := routeplan.New("old-de", "new-de")
	if err != nil {
		t.Fatal(err)
	}
	if err = draft.SetDirect(1, 8569); err != nil {
		t.Fatal(err)
	}
	preview, err := draft.Preview()
	if err != nil {
		t.Fatal(err)
	}
	return preview
}
