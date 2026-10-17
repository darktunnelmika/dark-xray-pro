package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOwnerNodeManagerPairInviteAndRevocation(t *testing.T) {
	s := enabledOwner(t)
	h := s.BrowserHandler()
	ownerCookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	request := func(method, path string, body any, c *http.Cookie, token, origin string) *httptest.ResponseRecorder {
		return browserRequest(t, h, method, path, body, c, token, origin)
	}
	// New API never trusts a bearer for an anonymous browser request.
	secret := map[string]string{"node_id": "qa-new-01", "name": "NEW PRO QA Node"}
	if got := request("GET", "/api/v1/workspace/nodes", nil, nil, "", ""); got.Code != 401 {
		t.Fatalf("anonymous roster: %d", got.Code)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites", secret, nil, "", "https://"+browserHost); got.Code != 401 {
		t.Fatalf("anonymous invitation: %d", got.Code)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites", secret, ownerCookie, "", "https://"+browserHost); got.Code != 403 {
		t.Fatalf("CSRF missing: %d", got.Code)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites", secret, ownerCookie, csrf, "https://malicious.example"); got.Code != 403 {
		t.Fatalf("cross-origin: %d", got.Code)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites", map[string]any{"node_id": "qa-new-01", "name": "new", "unexpected": "field"}, ownerCookie, csrf, "https://"+browserHost); got.Code != 400 {
		t.Fatalf("unknown field: %d", got.Code)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites", map[string]string{"node_id": "bad/id", "name": "bad"}, ownerCookie, csrf, "https://"+browserHost); got.Code != 422 {
		t.Fatalf("bad ID: %d", got.Code)
	}
	if got := request("GET", "/api/v1/workspace/nodes", nil, ownerCookie, "", ""); got.Code != 200 || strings.Contains(got.Body.String(), "pair_code") {
		t.Fatalf("owner roster invalid: %d", got.Code)
	}

	create := request("POST", "/api/v1/workspace/nodes/invites", secret, ownerCookie, csrf, "https://"+browserHost)
	if create.Code != 201 {
		t.Fatalf("cannot issue new Node code: %d %s", create.Code, create.Body.String())
	}
	var issued struct {
		NodeID   string `json:"node_id"`
		PairCode string `json:"pair_code"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if issued.NodeID != "qa-new-01" || len(issued.PairCode) != 64 {
		t.Fatal("invalid one-time code")
	}
	if got := request("GET", "/api/v1/workspace/nodes", nil, ownerCookie, "", ""); strings.Contains(got.Body.String(), issued.PairCode) || strings.Contains(got.Body.String(), "pair_code") {
		t.Fatal("one-time secret re-exposed")
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites", secret, ownerCookie, csrf, "https://"+browserHost); got.Code != 409 {
		t.Fatalf("duplicate invite: %d", got.Code)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites/qa-new-01/cancel", map[string]any{}, ownerCookie, "", "https://"+browserHost); got.Code != 403 {
		t.Fatalf("cancel without CSRF: %d", got.Code)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites/qa-new-01/cancel", map[string]any{}, ownerCookie, csrf, "https://"+browserHost); got.Code != 200 {
		t.Fatalf("cancel refused: %d", got.Code)
	}
	if _, err := s.registry.Claim(issued.PairCode, "qa-new-01", strings.Repeat("1", 32), strings.Repeat("a", 64)); err == nil {
		t.Fatal("cancelled pairing code still works")
	}
	second := request("POST", "/api/v1/workspace/nodes/invites", secret, ownerCookie, csrf, "https://"+browserHost)
	if second.Code != 201 {
		t.Fatalf("cannot reissue after cancel: %d", second.Code)
	}
	var again struct {
		PairCode string `json:"pair_code"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if again.PairCode == issued.PairCode {
		t.Fatal("cancelled code reused")
	}
	if _, err := s.registry.Claim(again.PairCode, "qa-new-01", strings.Repeat("1", 32), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	list := request("GET", "/api/v1/workspace/nodes", nil, ownerCookie, "", "")
	if list.Code != 200 {
		t.Fatalf("list: %d", list.Code)
	}
	if strings.Contains(list.Body.String(), again.PairCode) || strings.Contains(list.Body.String(), strings.Repeat("a", 64)) {
		t.Fatal("roster leaked credentials")
	}
	var roster struct {
		Nodes []ownerNode `json:"nodes"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &roster); err != nil {
		t.Fatal(err)
	}
	if len(roster.Nodes) != 1 || roster.Nodes[0].Name != "NEW PRO QA Node" || roster.Nodes[0].Status != "offline" {
		t.Fatalf("fabricated live Node: %+v", roster)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites/qa-new-01/cancel", map[string]any{}, ownerCookie, csrf, "https://"+browserHost); got.Code != 404 {
		t.Fatalf("paired Node can be cancelled: %d", got.Code)
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "qa-rep", "QA rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "qa-rep", "qa-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	repCookie, repCSRF := browserLogin(t, s, "qa-rep", repPassword)
	if got := request("GET", "/api/v1/workspace/nodes", nil, repCookie, "", ""); got.Code != 403 {
		t.Fatalf("rep can read Node roster: %d", got.Code)
	}
	if got := request("POST", "/api/v1/workspace/nodes/invites", map[string]string{"node_id": "rep-danger", "name": "Rep"}, repCookie, repCSRF, "https://"+browserHost); got.Code != 403 {
		t.Fatalf("rep can issue invite: %d", got.Code)
	}
}
