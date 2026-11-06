package api

import (
	"context"
	"encoding/json"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"strings"
	"testing"
	"time"
)

func TestRepresentativeServiceHTTPScopedChoicesAndMutationBoundary(t *testing.T) {
	s, plan, owner, ownerCSRF := creationPlanAPIFixture(t)
	ctx := context.Background()
	plan.Profile = business.InboundInput{ID: 501, Name: "Delegated HTTP service", Spec: &inboundspec.Spec{Version: 1, Protocol: "vless", Network: "tcp", Security: "none", Settings: map[string]any{"decryption": "none"}}}
	plan.Nodes = []business.NativeInboundPlanNode{{NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20", DirectPort: 21185, DirectHosts: []string{"de.example.org"}}}

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

	// Establish a synthetic active runtime with durable zero counters. The HTTP
	// acceptance below uses real sessions and SQLite, not a mocked API response.
	if _, e := s.business.BindInboundExecutionClients(ctx, s.inboundKey, "owner", 501, ix.BindingsInput{EventID: "http-owner-binding-001", ExpectedRevision: 0, ClientIDs: []string{"client-service-b"}, AccountingMode: ix.AccountingPerClient, Reason: "Preserve other representative"}); e != nil {
		t.Fatal(e)
	}
	state, e := s.business.InboundExecution(ctx, s.inboundKey, 501)
	if e != nil {
		t.Fatal(e)
	}
	initial := ix.CommandInput{EventID: "http-owner-activate-001", Action: "activate", ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: state.ClientIDs, AccountingMode: ix.AccountingPerClient, Reason: "Synthetic initial activation"}
	preview, e := s.business.PreviewInboundExecution(ctx, s.inboundKey, "owner", 501, initial)
	if e != nil || !preview.Valid {
		t.Fatal(preview, e)
	}
	initial.ReviewSHA = preview.ReviewSHA
	if _, e = s.business.QueueInboundExecution(ctx, s.inboundKey, "owner", 501, initial); e != nil {
		t.Fatal(e)
	}
	bundle, ok, e := s.business.InboundExecutionBundle(ctx, s.inboundKey, "de", strings.Repeat("b", 32))
	if e != nil || !ok {
		t.Fatal(e)
	}
	member, _ := json.Marshal(ix.ProcessMember{BootID: "00000000-0000-0000-0000-000000000001", PID: 1, StartTicks: 1, BinarySHA: strings.Repeat("a", 64), ConfigSHA: ix.BytesSHA([]byte("synthetic-http-process"))})
	process, _ := json.Marshal(map[string]string{"xray": string(member)})
	epoch := ix.EpochInput{EpochID: "http-delegated-epoch-001", NodeID: bundle.NodeID, InstallationID: bundle.InstallationID, Generation: bundle.Generation, ConfigSHA: bundle.ConfigSHA, ManifestSHA: bundle.ManifestSHA, BinarySHAs: map[string]string{"xray": strings.Repeat("a", 64)}, ProcessIdentity: string(process)}
	if _, e = s.business.RegisterInboundExecutionEpoch(ctx, s.inboundKey, epoch); e != nil {
		t.Fatal(e)
	}
	report := ix.UsageReport{EpochID: epoch.EpochID, NodeID: epoch.NodeID, InstallationID: epoch.InstallationID, Generation: epoch.Generation, Sequence: 1, ProcessIdentity: epoch.ProcessIdentity, Counters: []ix.Counter{}}
	for _, principal := range bundle.Principals {
		report.Counters = append(report.Counters, ix.Counter{PrincipalID: principal.ID})
	}
	if _, e = s.business.AcceptInboundExecutionUsage(ctx, s.inboundKey, report); e != nil {
		t.Fatal(e)
	}
	if e = s.business.ObserveInboundExecution(ctx, s.inboundKey, ix.Observation{NodeID: bundle.NodeID, InstallationID: bundle.InstallationID, Generation: bundle.Generation, OperationID: bundle.OperationID, ConfigSHA: bundle.ConfigSHA, ManifestSHA: bundle.ManifestSHA, State: "active", EpochID: epoch.EpochID, ObservedUnixNano: time.Now().UnixNano(), LeaseExpires: time.Now().Add(25 * time.Second).UnixNano(), ProcessReady: true, HealthState: "healthy", AccountingState: "accepted"}); e != nil {
		t.Fatal(e)
	}
	read = browserRequest(t, s.Handler(), "GET", base, nil, cookie, "", "")
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &choices) != nil || len(choices.Services) != 1 || !choices.Services[0].Ready {
		t.Fatal("active scoped choice", read.Code, read.Body.String())
	}
	choice := choices.Services[0]
	body := map[string]any{"event_id": "http-delegate-add-001", "grant_revision": choice.GrantRevision, "execution_revision": choice.ExecutionRevision, "plan_revision": choice.PlanRevision, "inbound_revision": choice.InboundRevision, "add": true, "reason": "Add own client through HTTP"}
	w := browserRequest(t, s.Handler(), "POST", base+"/501/preview", body, cookie, csrf, "https://"+browserHost)
	var result business.RepresentativeServiceResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Valid || result.ReviewSHA == "" {
		t.Fatalf("HTTP preview %d %s", w.Code, w.Body.String())
	}
	body["review_sha"] = result.ReviewSHA
	for attempt := 0; attempt < 2; attempt++ {
		w = browserRequest(t, s.Handler(), "POST", base+"/501/commands", body, cookie, csrf, "https://"+browserHost)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.OperationID != "http-delegate-add-001" || result.Replayed != (attempt == 1) {
			t.Fatalf("HTTP admission/replay %d %s", w.Code, w.Body.String())
		}
	}
	state, e = s.business.InboundExecution(ctx, s.inboundKey, 501)
	if e != nil || len(state.ClientIDs) != 2 {
		t.Fatal("membership preservation", state.ClientIDs, e)
	}
	read = browserRequest(t, s.Handler(), "GET", base, nil, cookie, "", "")
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &choices) != nil || len(choices.Services) != 1 || !choices.Services[0].Assigned || choices.Services[0].Applied {
		t.Fatal("admission incorrectly exposed as applied", read.Code, read.Body.String())
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
