package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
)

func TestOwnerNodeRenameAuthorizationAndCAS(t *testing.T) {
	s := enabledOwner(t)
	h := s.BrowserHandler()
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	ticket, err := s.registry.Issue("lab-name", "Before")
	if err != nil {
		t.Fatal(err)
	}
	install, token := strings.Repeat("a", 32), strings.Repeat("b", 64)
	if _, err := s.registry.Claim(ticket.PairCode, "lab-name", install, token); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspace/nodes/lab-name"
	x := noderegistry.RenameRequest{EventID: strings.Repeat("c", 32), InstallationID: install, ExpectedRevision: 1, Name: "After"}
	for _, tc := range []struct {
		cookie       *http.Cookie
		csrf, origin string
		want         int
	}{
		{nil, "", "", 401}, {cookie, "", "https://" + browserHost, 403}, {cookie, csrf, "https://evil.example", 403},
	} {
		w := browserRequest(t, h, "POST", base+"/rename", x, tc.cookie, tc.csrf, tc.origin)
		if w.Code != tc.want {
			t.Fatalf("authorization: %d want %d", w.Code, tc.want)
		}
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "name-rep", "Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "name-rep", "name-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	rep, repCSRF := browserLogin(t, s, "name-rep", repPassword)
	for _, method := range []string{"GET", "POST"} {
		path := base + "/name"
		if method == "POST" {
			path = base + "/rename"
		}
		if w := browserRequest(t, h, method, path, x, rep, repCSRF, "https://"+browserHost); w.Code != 403 {
			t.Fatalf("representative allowed: %d", w.Code)
		}
	}
	for i := 0; i < 2; i++ {
		w := browserRequest(t, h, "POST", base+"/rename", x, cookie, csrf, "https://"+browserHost)
		if w.Code != 200 {
			t.Fatalf("rename: %d %s", w.Code, w.Body.String())
		}
	}
	x.EventID = strings.Repeat("d", 32)
	x.Name = "Stale"
	if w := browserRequest(t, h, "POST", base+"/rename", x, cookie, csrf, "https://"+browserHost); w.Code != 409 {
		t.Fatalf("stale write accepted: %d", w.Code)
	}
	for _, body := range []map[string]any{
		{"actor_id": "forged"}, {"expected_revision": 1.5}, {"unexpected": true},
	} {
		if w := browserRequest(t, h, "POST", base+"/rename", body, cookie, csrf, "https://"+browserHost); w.Code != 400 {
			t.Fatalf("bad input: %d", w.Code)
		}
	}
	w := browserRequest(t, h, "GET", base+"/name", nil, cookie, "", "")
	var state noderegistry.NameState
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Revision != 2 || state.Node.Name != "After" || len(state.Recent) != 1 || state.Recent[0].ActorID == "" {
		t.Fatalf("state: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), token) || strings.Contains(w.Body.String(), ticket.PairCode) || strings.Contains(w.Body.String(), "token_hash") {
		t.Fatal("secret exposed")
	}
	if _, ok := s.registry.Authenticate("lab-name", install, token); !ok {
		t.Fatal("agent identity changed")
	}
	if w := browserRequest(t, h, "GET", "/api/v1/workspace/nodes/missing/name", nil, cookie, "", ""); w.Code != 404 {
		t.Fatalf("missing node: %d", w.Code)
	}
}
