package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestAuthenticatedBulkClientStateOwnerRepCSRFAndTransactionFence(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	for _, rep := range []string{"bulkRepA", "bulkRepB"} {
		if _, err := s.business.CreateRepresentative(ctx, rep, "Private "+rep); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.AdjustCredits(ctx, business.CreditChange{
			EventID: "client-bulk-grant-00" + rep, RepresentativeID: rep, VolumeDeltaBytes: 1000, Reason: "test",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.CreateClient(ctx, business.NewClient{
			EventID: "client-bulk-created-00" + rep, ClientID: "client-" + rep,
			RepresentativeID: rep, Name: "Private " + rep, DurationDays: 30, VolumeBytes: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.ProvisionRepresentativeLogin(ctx, "bulkRepA", "bulk-user-a", "bulk-representative-secret-abc123"); err != nil {
		t.Fatal(err)
	}
	ownerCookie, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
	repCookie, repCSRF := browserLogin(t, s, "bulk-user-a", "bulk-representative-secret-abc123")
	path := "/api/v1/workspace/clients/bulk/state"
	input := business.BulkClientState{
		EventID: "bulk-test-disable-000000001", ClientIDs: []string{"client-bulkRepA", "client-bulkRepB"},
		Action: "disable", Reason: "Owner reviewed multi-tenant QA",
	}
	req := func(cookie *http.Cookie, csrf string, body any, origin string) *http.Response {
		return browserRequest(t, s.Handler(), "POST", path, body, cookie, csrf, origin).Result()
	}
	if got := req(nil, "", input, "").StatusCode; got != 401 {
		t.Fatalf("anonymous bulk state: %d", got)
	}
	if got := req(repCookie, "", input, "https://"+browserHost).StatusCode; got != 403 {
		t.Fatalf("missing CSRF: %d", got)
	}
	if got := req(repCookie, repCSRF, input, "https://"+browserHost).StatusCode; got != 403 {
		t.Fatalf("rep bulk touched another tenant: %d", got)
	}
	for _, id := range input.ClientIDs {
		c, err := s.business.Client(ctx, id)
		if err != nil || c.DeploymentState != "pending_node_apply" {
			t.Fatalf("cross-rep check partially applied %s %v", id, err)
		}
	}
	good := business.BulkClientState{
		EventID: "bulk-rep-disable-0000001", ClientIDs: []string{"client-bulkRepA"},
		Action: "disable", Reason: "Rep own client only",
	}
	if got := req(repCookie, repCSRF, good, "https://"+browserHost).StatusCode; got != 200 {
		t.Fatalf("rep bulk own record forbidden: %d", got)
	}
	if got := req(repCookie, repCSRF, good, "https://"+browserHost).StatusCode; got != 200 {
		t.Fatalf("identical representative retry not idempotent: %d", got)
	}
	ownerInput := business.BulkClientState{
		EventID: "bulk-owner-disable-0000001", ClientIDs: []string{"client-bulkRepB"},
		Action: "disable", Reason: "Owner QA",
	}
	result := browserRequest(t, s.Handler(), "POST", path, ownerInput, ownerCookie, ownerCSRF, "https://"+browserHost)
	if result.Code != 200 {
		t.Fatalf("owner scoped bulk: %d %s", result.Code, result.Body.String())
	}
	var info struct {
		Count                  int  `json:"count"`
		AppliedToXray          bool `json:"applied_to_xray"`
		CustomerTrafficChanged bool `json:"customer_traffic_changed"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Count != 1 || info.AppliedToXray || info.CustomerTrafficChanged {
		t.Fatalf("bulk changed customer data plane: %+v", info)
	}
	for _, id := range input.ClientIDs {
		c, err := s.business.Client(ctx, id)
		if err != nil || c.DeploymentState != "disabled" || c.Archived {
			t.Fatalf("incorrect final business state %s %v", id, err)
		}
	}
	repList := browserRequest(t, s.Handler(), "GET",
		"/api/v1/workspace/clients?state=disabled&representative_id=bulkRepB",
		nil, repCookie, "", "")
	if repList.Code != 403 {
		t.Fatalf("representative bulk state leaked other owner: %d", repList.Code)
	}
	if got := req(ownerCookie, ownerCSRF, map[string]any{
		"event_id": "bulk-owner-invalid-0000001", "client_ids": []string{"client-bulkRepB"},
		"action": "reset_traffic", "reason": "unsafe billing history wipe",
	}, "https://"+browserHost).StatusCode; got != 422 {
		t.Fatalf("unsafe bulk data loss action allowed: %d", got)
	}
	if got := req(ownerCookie, ownerCSRF, ownerInput, "https://evil.example").StatusCode; got != 403 {
		t.Fatalf("cross-site POST accepted: %d", got)
	}
	agent := browserRequest(t, s.AgentHandler(), "POST", path, ownerInput, ownerCookie, ownerCSRF, "https://"+browserHost)
	if agent.Code == 200 {
		t.Fatal("Agent-only surface allowed client finance operation")
	}
}
