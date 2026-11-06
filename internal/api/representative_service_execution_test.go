package api

import (
	"context"
	"encoding/json"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"testing"
)

func TestRepresentativeServiceHTTPScopedChoicesAndMutationBoundary(t *testing.T) {
	s, plan, owner, ownerCSRF := creationPlanAPIFixture(t)
	ctx := context.Background()
	if e := s.business.EnableInboundExecution(ctx); e != nil {
		t.Fatal(e)
	}
	s.inboundExecution = &inboundExecutionService{}
	if _, e := s.business.SaveNativeInboundPlan(ctx, s.inboundKey, plan); e != nil {
		t.Fatal(e)
	}
	for _, rep := range []string{"service-a", "service-b"} {
		if _, e := s.business.CreateRepresentative(ctx, rep, rep); e != nil {
			t.Fatal(e)
		}
		if _, e := s.business.AdjustCredits(ctx, business.CreditChange{EventID: "credit-service-event-" + rep, RepresentativeID: rep, VolumeDeltaBytes: 1 << 30, Reason: "Test credit"}); e != nil {
			t.Fatal(e)
		}
		if _, e := s.business.CreateClient(ctx, business.NewClient{EventID: "create-service-event-" + rep, ClientID: "client-" + rep, RepresentativeID: rep, Name: rep, DurationDays: 30, VolumeBytes: 1 << 20}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.business.ProvisionRepresentativeLogin(ctx, "service-a", "service-login", "service-password-12345"); e != nil {
		t.Fatal(e)
	}
	cookie, csrf := browserLogin(t, s, "service-login", "service-password-12345")
	g := business.ServiceGrantChange{EventID: "service-http-grant-0001", RepresentativeID: "service-a", InboundID: 501, Enabled: true, Reason: "Selected service"}
	if _, e := s.business.ChangeRepresentativeServiceGrant(ctx, "owner", g); e != nil {
		t.Fatal(e)
	}
	base := "/api/v1/workspace/clients/client-service-a/services"
	read := browserRequest(t, s.Handler(), "GET", base, nil, cookie, "", "")
	var choices business.RepresentativeServiceChoices
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &choices) != nil || len(choices.Services) != 1 || choices.Services[0].Ready {
		t.Fatalf("choices %d %s", read.Code, read.Body.String())
	}
	for _, path := range []string{"/api/v1/workspace/clients/client-service-b/services", base + "?representative_id=service-b"} {
		w := browserRequest(t, s.Handler(), "GET", path, nil, cookie, "", "")
		if w.Code == 200 {
			t.Fatal("foreign/overridden scope accepted", path)
		}
	}
	for _, action := range []string{"preview", "commands"} {
		path := base + "/501/" + action
		body := map[string]any{"event_id": "http-service-command-001", "grant_revision": 1, "execution_revision": 0, "plan_revision": 1, "inbound_revision": 1, "add": true, "reason": "Change own client"}
		if w := browserRequest(t, s.Handler(), "POST", path, body, nil, "", "https://"+browserHost); w.Code != 401 {
			t.Fatal(w.Code)
		}
		if w := browserRequest(t, s.Handler(), "POST", path, body, cookie, "", "https://"+browserHost); w.Code != 403 {
			t.Fatal(w.Code)
		}
		if w := browserRequest(t, s.Handler(), "POST", path, body, owner, ownerCSRF, "https://"+browserHost); w.Code != 403 {
			t.Fatal(w.Code)
		}
		body["client_ids"] = []string{"client-service-b"}
		if w := browserRequest(t, s.Handler(), "POST", path, body, cookie, csrf, "https://"+browserHost); w.Code != 422 {
			t.Fatalf("replacement list accepted %d", w.Code)
		}
		delete(body, "client_ids")
		delete(body, "add")
		if w := browserRequest(t, s.Handler(), "POST", path, body, cookie, csrf, "https://"+browserHost); w.Code != 422 {
			t.Fatalf("implicit action accepted %d", w.Code)
		}
	}
	g.EventID = "service-http-revoke-001"
	g.Enabled = false
	g.ExpectedRevision = 1
	if _, e := s.business.ChangeRepresentativeServiceGrant(ctx, "owner", g); e != nil {
		t.Fatal(e)
	}
	read = browserRequest(t, s.Handler(), "GET", base, nil, cookie, "", "")
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &choices) != nil || len(choices.Services) != 0 {
		t.Fatal("revoked service exposed", read.Code)
	}
}
