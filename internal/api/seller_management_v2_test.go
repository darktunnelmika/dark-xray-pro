package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestSellerManagementAPIGateAndAgentIsolation(t *testing.T) {
	s := enabledOwner(t)
	owner, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/workspace/representatives/repA/seller-permission"},
		{"POST", "/api/v1/workspace/representatives/repA/seller-permission"},
		{"POST", "/api/v1/workspace/representatives/repA/sellers/alice/state"},
	} {
		if w := browserRequest(t, s.Handler(), route.method, route.path, map[string]any{}, owner, csrf, "https://"+browserHost); w.Code != http.StatusNotFound {
			t.Fatal("management route exposed before gate", route, w.Code, w.Body.String())
		}
	}
	if err := s.EnableSellerQuotaV2Lab(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/workspace/representatives/repA/seller-permission",
		"/api/v1/workspace/representatives/repA/sellers",
	} {
		req := httptest.NewRequest(http.MethodGet, "https://"+browserHost+path, nil)
		req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		req.AddCookie(owner)
		w := httptest.NewRecorder()
		s.AgentHandler().ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatal("valid TLS Agent listener routed seller management", path, w.Code, w.Body.String())
		}
	}
}

func TestSellerManagementAPIScopedGrantStateAndLegacyControls(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	for _, id := range []string{"repAccessA", "repAccessB"} {
		if _, err := s.business.CreateRepresentative(ctx, id, "Representative "+id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.ProvisionRepresentativeLogin(ctx, id, strings.ToLower(id), repPassword); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.AdjustCredits(ctx, business.CreditChange{EventID: "seller-api-access-seed-0001", RepresentativeID: "repAccessA",
		VolumeDeltaBytes: 1000, UnlimitedDeltaUnits: 100, Reason: "Seed real representative balance"}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSellerQuotaV2Lab(ctx); err != nil {
		t.Fatal(err)
	}
	h := s.BrowserHandler()
	owner, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
	rep, repCSRF := browserLogin(t, s, "repaccessa", repPassword)
	other, _ := browserLogin(t, s, "repaccessb", repPassword)
	base := "/api/v1/workspace/representatives/repAccessA"
	permissionPath := base + "/seller-permission"
	request := func(method, path string, body any, cookie *http.Cookie, token string) *httptest.ResponseRecorder {
		return browserRequest(t, h, method, path, body, cookie, token, "https://"+browserHost)
	}
	if w := request("GET", permissionPath, nil, nil, ""); w.Code != 401 {
		t.Fatal("anonymous permission read", w.Code)
	}
	if w := request("GET", permissionPath, nil, rep, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"manage_sellers":false`) || !strings.Contains(w.Body.String(), `"revision":0`) {
		t.Fatal("denied representative cannot inspect own permission", w.Code, w.Body.String())
	}
	if w := request("GET", permissionPath, nil, other, ""); w.Code != 403 {
		t.Fatal("other representative inspected private permission", w.Code)
	}
	if w := request("GET", base+"/sellers", nil, rep, ""); w.Code != 403 {
		t.Fatal("seller rights inherited from old permissions", w.Code)
	}
	grant := map[string]any{"event_id": "seller-api-access-grant-0001", "representative_id": "repAccessA",
		"expected_revision": 0, "manage_sellers": true, "reason": "Owner grant"}
	if w := request("POST", permissionPath, grant, rep, repCSRF); w.Code != 403 {
		t.Fatal("representative granted own permission", w.Code)
	}
	if w := request("POST", permissionPath, grant, owner, ""); w.Code != 403 {
		t.Fatal("permission update missed CSRF", w.Code)
	}
	if w := browserRequest(t, h, "POST", permissionPath, grant, owner, ownerCSRF, "https://foreign.example"); w.Code != 403 {
		t.Fatal("permission update accepted foreign origin", w.Code)
	}
	for _, missing := range []string{"expected_revision", "manage_sellers"} {
		invalid := make(map[string]any)
		for k, v := range grant {
			if k != missing {
				invalid[k] = v
			}
		}
		if w := request("POST", permissionPath, invalid, owner, ownerCSRF); w.Code != 422 {
			t.Fatal("partial permission request accepted", missing, w.Code)
		}
	}
	if w := request("POST", permissionPath, grant, owner, ownerCSRF); w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatal("owner grant failed", w.Code, w.Body.String())
	}
	if w := request("GET", base+"/sellers", nil, rep, ""); w.Code != 401 {
		t.Fatal("grant did not revoke earlier session", w.Code)
	}
	rep, repCSRF = browserLogin(t, s, "repaccessa", repPassword)
	if w := request("GET", "/api/v1/session/status", nil, rep, ""); !strings.Contains(w.Body.String(), `"manage_sellers":true`) {
		t.Fatal("session missing granted permission", w.Body.String())
	}
	// The V9 endpoint still accepts exactly seven legacy permission keys.
	legacy := map[string]any{"event_id": "seller-api-legacy-control-0001", "representative_id": "repAccessA",
		"expected_revision": 0, "display_name": "Renamed representative", "login_suspended": false,
		"reason": "Legacy display-name change", "permissions": map[string]bool{
			"view_clients": true, "create_clients": true, "renew_clients": true, "edit_clients": true,
			"manage_clients": true, "manage_groups": true, "view_finance": true, "manage_sellers": true,
		}}
	if w := request("POST", base+"/control", legacy, owner, ownerCSRF); w.Code != 422 {
		t.Fatal("legacy control accepted eighth permission key", w.Code)
	}
	delete(legacy["permissions"].(map[string]bool), "manage_sellers")
	if w := request("POST", base+"/control", legacy, owner, ownerCSRF); w.Code != 200 {
		t.Fatal("legacy seven-key control changed", w.Code, w.Body.String())
	}
	rep, repCSRF = browserLogin(t, s, "repaccessa", repPassword)
	if w := request("POST", base+"/control", legacy, owner, ownerCSRF); w.Code != 200 || !strings.Contains(w.Body.String(), `"replayed":true`) {
		t.Fatal("legacy event replay failed", w.Code, w.Body.String())
	}
	if w := request("GET", permissionPath, nil, rep, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"manage_sellers":true`) {
		t.Fatal("legacy replay revoked grant/session", w.Code, w.Body.String())
	}
	create := map[string]any{"event_id": "seller-api-scoped-create-0001", "representative_id": "repAccessA",
		"seller_id": "sellerAlice", "name": "Alice", "telegram_user_id": 12345}
	if w := request("POST", base+"/sellers", create, rep, repCSRF); w.Code != 201 {
		t.Fatal("granted representative create failed", w.Code, w.Body.String())
	}
	for _, path := range []string{base + "/sellers", base + "/sellers/sellerAlice"} {
		if w := request("GET", path, nil, rep, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":0`) {
			t.Fatal("granted representative read failed", w.Code, w.Body.String())
		}
	}
	quota := map[string]any{"event_id": "seller-api-scoped-reserve-0001", "representative_id": "repAccessA",
		"seller_id": "sellerAlice", "action": "allocate", "bytes": "200", "units": "20", "reason": "Scoped quota"}
	if w := request("POST", base+"/sellers/sellerAlice/quota", quota, rep, repCSRF); w.Code != 200 {
		t.Fatal("granted representative quota failed", w.Code, w.Body.String())
	}
	state := map[string]any{"event_id": "seller-api-disable-state-0001", "representative_id": "repAccessA",
		"seller_id": "sellerAlice", "expected_revision": 0, "enabled": false, "reason": "Disable seller"}
	foreignBase := "/api/v1/workspace/representatives/repAccessB"
	for _, item := range []struct {
		method, path string
		body         any
	}{
		{"GET", foreignBase + "/sellers", nil},
		{"GET", foreignBase + "/sellers/sellerAlice", nil},
		{"POST", foreignBase + "/sellers", create},
		{"POST", foreignBase + "/sellers/sellerAlice/quota", quota},
		{"POST", foreignBase + "/sellers/sellerAlice/state", state},
	} {
		if w := request(item.method, item.path, item.body, rep, repCSRF); w.Code != 403 {
			t.Fatal("granted representative crossed parent boundary", item.path, w.Code, w.Body.String())
		}
	}
	statePath := base + "/sellers/sellerAlice/state"
	if w := request("POST", statePath, state, rep, ""); w.Code != 403 {
		t.Fatal("state mutation missed CSRF", w.Code)
	}
	if w := request("POST", statePath+"?role=owner", state, rep, repCSRF); w.Code != 422 {
		t.Fatal("mutation query bypass accepted", w.Code)
	}
	if w := request("GET", base+"/sellers?limit=25;unknown=1", nil, rep, ""); w.Code != 422 {
		t.Fatal("malformed query silently accepted", w.Code)
	}
	if w := request("POST", statePath, state, rep, repCSRF); w.Code != 200 {
		t.Fatal("state mutation failed", w.Code, w.Body.String())
	} else {
		var result business.SellerStateReceiptV2
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Seller.Enabled || result.Seller.Revision != 1 || result.Seller.ReservedUnits != 20 {
			t.Fatal("state mutation changed reserves", result, err)
		}
	}
	if w := request("POST", statePath, state, rep, repCSRF); w.Code != 200 || !strings.Contains(w.Body.String(), `"replayed":true`) {
		t.Fatal("state exact replay failed", w.Code, w.Body.String())
	}
	quota["event_id"] = "seller-api-disabled-reclaim-0001"
	quota["action"] = "reclaim"
	if w := request("POST", base+"/sellers/sellerAlice/quota", quota, rep, repCSRF); w.Code != 200 {
		t.Fatal("disabled seller reserve cannot be reclaimed", w.Code, w.Body.String())
	}
	grant["event_id"], grant["expected_revision"], grant["manage_sellers"] = "seller-api-revoke-access-0001", 1, false
	if w := request("POST", permissionPath, grant, owner, ownerCSRF); w.Code != 200 {
		t.Fatal("owner revoke failed", w.Code, w.Body.String())
	}
	if w := request("POST", statePath, state, rep, repCSRF); w.Code != 401 {
		t.Fatal("revoked session replayed state operation", w.Code)
	}
}
