package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestV9OwnerControlsRepresentativesRBACSessionRevokeAndActivity(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	for _, id := range []string{"v9A", "v9B"} {
		if _, err := s.business.CreateRepresentative(ctx, id, "Active "+id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.AdjustCredits(ctx, business.CreditChange{
			EventID: "v9-api-rep-credit-grant-" + id, RepresentativeID: id,
			VolumeDeltaBytes: 3 << 30, UnlimitedDeltaUnits: 30, Reason: "Initial controlled credit",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.CreateClient(ctx, business.NewClient{
			EventID: "v9-api-rep-create-client-" + id, ClientID: "client-" + id,
			RepresentativeID: id, Name: "Customer " + id, DurationDays: 30, VolumeBytes: 1 << 30,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "v9A", "v9api-account", "safe-v9-account-password-123"); err != nil {
		t.Fatal(err)
	}
	owner, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
	rep, repCSRF := browserLogin(t, s, "v9api-account", "safe-v9-account-password-123")
	base := "/api/v1/workspace/representatives/v9A"
	get := func(path string, cookie *http.Cookie) *httpResponse {
		w := browserRequest(t, s.Handler(), "GET", path, nil, cookie, "", "")
		return &httpResponse{Code: w.Code, Body: w.Body.String()}
	}
	post := func(path string, cookie *http.Cookie, csrf string, body any, origin string) *httpResponse {
		w := browserRequest(t, s.Handler(), "POST", path, body, cookie, csrf, origin)
		return &httpResponse{Code: w.Code, Body: w.Body.String()}
	}
	if w := get(base+"/control", nil); w.Code != 401 {
		t.Fatal("anonymous can read representative control", w)
	}
	if w := get(base+"/control", rep); w.Code != 403 {
		t.Fatal("representative can administer account", w)
	}
	if w := get(base+"/control", owner); w.Code != 200 {
		t.Fatal("owner cannot inspect account controls", w)
	}
	if w := get(base+"/activity", rep); w.Code != 200 {
		t.Fatal("own activity denied", w)
	}
	if w := get("/api/v1/workspace/representatives/v9B/activity", rep); w.Code != 403 {
		t.Fatal("foreign representative activity leaked", w)
	}
	rights := business.DefaultRepresentativePermissions()
	rights.CreateClients = false
	rights.ManageGroups = false
	rights.ViewFinance = false
	change := business.RepresentativeControlChange{
		EventID: "v9-api-control-change-00000001", RepresentativeID: "v9A", ExpectedRevision: 0,
		DisplayName: "Limited sales representative", LoginSuspended: false, Permissions: rights,
		Reason: "Owner-approved read-only finance restriction",
	}
	if w := post(base+"/control", rep, repCSRF, change, "https://"+browserHost); w.Code != 403 {
		t.Fatal("representative changed own permissions", w)
	}
	if w := post(base+"/control", owner, "", change, "https://"+browserHost); w.Code != 403 {
		t.Fatal("missing CSRF changed permissions", w)
	}
	if w := post(base+"/control", owner, ownerCSRF, change, "https://evil.example"); w.Code != 403 {
		t.Fatal("CSRF origin not checked", w)
	}
	response := post(base+"/control", owner, ownerCSRF, change, "https://"+browserHost)
	if response.Code != 200 {
		t.Fatal("owner edit rejected", response)
	}
	var receipt business.RepresentativeControlReceipt
	if err := json.Unmarshal([]byte(response.Body), &receipt); err != nil || receipt.Replayed ||
		!receipt.SessionsRevoked || receipt.Control.Permissions.CreateClients || receipt.Control.ActiveSessions != 0 {
		t.Fatal("control receipt incorrect", receipt, err)
	}
	// No formerly authenticated request may survive the owner policy update.
	if w := get("/api/v1/workspace/me", rep); w.Code != 401 {
		t.Fatal("old representative session survived", w)
	}
	rep, repCSRF = browserLogin(t, s, "v9api-account", "safe-v9-account-password-123")
	if w := get("/api/v1/workspace/clients?limit=50&offset=0", rep); w.Code != 200 {
		t.Fatal("allowed client directory denied", w)
	}
	if w := get("/api/v1/workspace/events", rep); w.Code != 403 {
		t.Fatal("finance permission bypassed", w)
	}
	if w := get(base+"/activity", rep); w.Code != 403 {
		t.Fatal("activity permission bypassed", w)
	}
	if w := post("/api/v1/workspace/clients", rep, repCSRF, map[string]any{
		"event_id": "v9-api-forbidden-create-000001", "client_id": "forbidden-v9",
		"representative_id": "v9A", "name": "Should not create",
		"duration_days": 30, "volume_bytes": 1024, "ip_count": 0,
	}, "https://"+browserHost); w.Code != 403 {
		t.Fatal("create permission bypassed", w)
	}
	if w := post("/api/v1/workspace/groups", rep, repCSRF, map[string]any{
		"id": "v9-no-group", "representative_id": "v9A", "name": "Forbidden", "color": "#123456",
	}, "https://"+browserHost); w.Code != 403 {
		t.Fatal("group write permission bypassed", w)
	}
	// The independent RenewClients permission remains granted, even with
	// CreateClients disabled: least-privilege instead of one global switch.
	renewed := post("/api/v1/workspace/clients/client-v9A/extend", rep, repCSRF,
		business.ExtendClient{EventID: "v9-api-allowed-renew-event-01", ClientID: "client-v9A",
			AdditionalDays: 1, Reason: "Permitted independent renewal"},
		"https://"+browserHost)
	if renewed.Code != 200 {
		t.Fatal("allowed renewal wrongfully blocked", renewed)
	}
	if w := get("/api/v1/workspace/nodes", rep); w.Code != 403 {
		t.Fatal("infrastructure access leaked", w)
	}
	if w := get("/api/v1/workspace/representatives/v9A/control/events", rep); w.Code != 403 {
		t.Fatal("owner-only audit leaked", w)
	}
	if w := get(base+"/control/events?limit=25&offset=0", owner); w.Code != 200 {
		t.Fatal("owner control event history not readable", w)
	}
	if w := get(base+"/activity", owner); w.Code != 200 {
		t.Fatal("owner activity read not accessible", w)
	}
	if w := get("/api/v1/workspace/representatives/v9B/control", owner); w.Code != 200 {
		t.Fatal("other representative affected", w)
	}
	if w := post(base+"/control", owner, ownerCSRF, map[string]any{
		"event_id": "v9-api-invalid-unknown-00001", "representative_id": "v9A",
		"expected_revision": 1, "display_name": "bad", "login_suspended": false,
		"permissions": rights, "reason": "test", "unknown": true,
	}, "https://"+browserHost); w.Code != 400 {
		t.Fatal("unknown control field accepted", w)
	}
	// Every permission must be explicit; partial JSON must not unexpectedly
	// remove all of a representative's rights.
	partial := map[string]any{
		"event_id":          "v9-partial-role-must-be-rejected-0001",
		"representative_id": "v9A", "expected_revision": 1,
		"display_name": "Limited sales representative", "login_suspended": false,
		"permissions": map[string]bool{"create_clients": true},
		"reason":      "Missing six important permission keys",
	}
	if w := post(base+"/control", owner, ownerCSRF, partial, "https://"+browserHost); w.Code != 422 {
		t.Fatalf("partial permission update was accepted: %d", w.Code)
	}
	if w := post("/api/v1/workspace/representatives/owner/control", owner, ownerCSRF, change, "https://"+browserHost); w.Code != 422 {
		t.Fatal("owner identity could be modified via representative endpoint", w)
	}
}
