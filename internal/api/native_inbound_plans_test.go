package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func creationPlanAPIFixture(t *testing.T) (*Server, business.NativeInboundPlanInput, *http.Cookie, string) {
	t.Helper()
	s := enabledOwner(t)
	if err := s.EnableNativeInbounds(bytes.Repeat([]byte{0xc9}, 32)); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"nl", "de"} {
		ticket, err := s.registry.Issue(id, "Paired "+id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.registry.Claim(ticket.PairCode, id, strings.Repeat(string(rune('a'+i)), 32), strings.Repeat(string(rune('2'+i)), 64)); err != nil {
			t.Fatal(err)
		}
	}
	in := business.NativeInboundPlanInput{EventID: "creation-api-event-0001", Reason: "Owner reviewed full setup",
		Profile: business.InboundInput{ID: 501, Name: "Complete creation", Transport: "tcp", RealityTarget: "www.example.org:443", ServerNames: []string{"www.example.org"}, ShortIDs: []string{"aa11", "bb22"}},
		Nodes: []business.NativeInboundPlanNode{{NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20",
			DirectPort: 21185, DirectHosts: []string{"de.example.org"}, TunnelPort: 26185, TunnelHosts: []string{"de-tunnel.example.org"},
			SwapEnabled: true, SwapSourceNodeID: "nl", SwapSourceInstallationID: strings.Repeat("a", 32), SwapSourceListenIP: "198.51.100.10",
			SwapSourcePort: 31000, SwapEntryAddress: "entry.example.org", SwapEntryPort: 443, SwapDestinationPort: 26185}},
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	return s, in, cookie, csrf
}

func TestCreationPlanAPIAdvancedOptionsRoundtripAndValidation(t *testing.T) {
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbound-plans"
	capability := browserRequest(t, s.Handler(), "GET", base+"/capabilities", nil, cookie, "", "")
	if capability.Code != http.StatusOK || !strings.Contains(capability.Body.String(), `"profile_options":true`) {
		t.Fatal("missing advanced authoring capability", capability.Code)
	}
	in.Profile.Transport = "grpc"
	in.Profile.GRPCService = "new_service"
	in.Profile.Options = &xrayconfig.ProfileOptions{
		GRPCAuthority: "edge.example.org:443", GRPCMultiMode: true,
		RealityMaxTimeDiff: 60000, RealityMinClientVer: "1.8.0", RealityMaxClientVer: "255.255.255",
	}
	created := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost)
	var receipt business.NativeInboundPlanReceipt
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &receipt) != nil ||
		receipt.Plan.Profile.Options == nil || !receipt.Plan.Profile.Options.GRPCMultiMode || receipt.Plan.Profile.Options.GRPCAuthority != "edge.example.org:443" {
		t.Fatal("HTTP save lost options", created.Code, created.Body.String())
	}
	get := browserRequest(t, s.Handler(), "GET", base+"/501", nil, cookie, "", "")
	var reopened struct {
		Plan business.NativeInboundPlan `json:"plan"`
	}
	if get.Code != 200 || json.Unmarshal(get.Body.Bytes(), &reopened) != nil || reopened.Plan.Profile.Options == nil || reopened.Plan.Profile.Options.RealityMaxTimeDiff != 60000 {
		t.Fatal("HTTP reopen lost options", get.Code, get.Body.String())
	}
	in.EventID = "advanced-api-edit-0001"
	in.ExpectedInboundRevision, in.ExpectedPlanRevision = 1, 1
	in.Profile.Options.RealityMaxTimeDiff = 75000
	edited := browserRequest(t, s.Handler(), "PUT", base+"/501", in, cookie, csrf, "https://"+browserHost)
	var update business.NativeInboundPlanReceipt
	if edited.Code != 200 || json.Unmarshal(edited.Body.Bytes(), &update) != nil || update.Plan.Profile.Options.RealityMaxTimeDiff != 75000 || update.Plan.Profile.PublicKey != receipt.Plan.Profile.PublicKey {
		t.Fatal("HTTP edit lost option or changed identity", edited.Code, edited.Body.String())
	}
	in.EventID = "advanced-api-invalid-0001"
	in.ExpectedInboundRevision, in.ExpectedPlanRevision = 2, 2
	in.Profile.Options.RealityXver = 3
	invalid := browserRequest(t, s.Handler(), "PUT", base+"/501", in, cookie, csrf, "https://"+browserHost)
	if invalid.Code != 422 || !strings.Contains(invalid.Body.String(), `"code":"profile_options_invalid"`) {
		t.Fatal("invalid setting accepted", invalid.Code, invalid.Body.String())
	}
	for _, options := range []map[string]any{
		{"unexpected": true}, {"reality_max_time_diff": 0.5}, {"grpc_multi_mode": "true"},
		{"reality_max_time_diff": 9223372036855}, {"reality_xver": -1},
	} {
		data, _ := json.Marshal(in)
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		body["profile"].(map[string]any)["options"] = options
		response := browserRequest(t, s.Handler(), "PUT", base+"/501", body, cookie, csrf, "https://"+browserHost)
		if response.Code != 422 {
			t.Fatal("unknown or mistyped advanced option accepted", response.Code)
		}
	}
	get = browserRequest(t, s.Handler(), "GET", base+"/501", nil, cookie, "", "")
	if get.Code != 200 || json.Unmarshal(get.Body.Bytes(), &reopened) != nil || reopened.Plan.InboundRevision != 2 || reopened.Plan.Profile.Options.RealityMaxTimeDiff != 75000 {
		t.Fatal("rejected options changed saved state", get.Code)
	}
}

func TestCreationPlanAPIOwnerPreviewCreateReopenUpdateAndCapability(t *testing.T) {
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbound-plans"
	request := func(method, path string, body any) *http.Response {
		t.Helper()
		return browserRequest(t, s.Handler(), method, path, body, cookie, csrf, "https://"+browserHost).Result()
	}
	capability := browserRequest(t, s.Handler(), "GET", base+"/capabilities", nil, cookie, "", "")
	if capability.Code != 200 || !strings.Contains(capability.Body.String(), `"available":true`) || !strings.Contains(capability.Body.String(), `"plans":[]`) {
		t.Fatal("capability/index missing", capability.Code, capability.Body.String())
	}
	preview := browserRequest(t, s.Handler(), "POST", base+"/preview", in, cookie, csrf, "https://"+browserHost)
	if preview.Code != 200 {
		t.Fatal("preview rejected", preview.Code, preview.Body.String())
	}
	var shape business.NativeInboundPlanPreview
	if err := json.Unmarshal(preview.Body.Bytes(), &shape); err != nil || !shape.Valid || !shape.TopologyValidated || len(shape.Topology.Nodes) != 2 || shape.Plan.Profile.Enabled {
		t.Fatal("preview response", shape, err)
	}
	for _, forbidden := range []string{"privateKey", "reality_private_key", "dokodemo-door", "\"config\"", "\"can_activate\":true"} {
		if strings.Contains(preview.Body.String(), forbidden) {
			t.Fatal("preview leaks private configuration or claims Apply", forbidden)
		}
	}
	created := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost)
	if created.Code != 201 {
		t.Fatal("atomic create rejected", created.Code, created.Body.String())
	}
	var receipt business.NativeInboundPlanReceipt
	if err := json.Unmarshal(created.Body.Bytes(), &receipt); err != nil || receipt.Plan.Profile.Enabled || receipt.RuntimeChanged || receipt.Plan.PlanRevision != 1 || receipt.Plan.Profile.PublicKey == "" {
		t.Fatal("unsafe creation receipt", receipt, err)
	}
	if response := request("POST", base, in); response.StatusCode != 200 {
		t.Fatal("lost response retry was not idempotent", response.StatusCode)
	}
	capability = browserRequest(t, s.Handler(), "GET", base+"/capabilities", nil, cookie, "", "")
	if !strings.Contains(capability.Body.String(), `"inbound_id":501`) || !strings.Contains(capability.Body.String(), `"plan_revision":1`) {
		t.Fatal("saved plan not discoverable", capability.Body.String())
	}
	get := browserRequest(t, s.Handler(), "GET", base+"/501", nil, cookie, "", "")
	var saved struct {
		Plan              business.NativeInboundPlan `json:"plan"`
		ReferencesCurrent bool                       `json:"references_current"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &saved); get.Code != 200 || err != nil || !saved.ReferencesCurrent || !saved.Plan.Editable || len(saved.Plan.Nodes) != 1 || saved.Plan.Nodes[0].SwapSourcePort != 31000 || saved.Plan.Nodes[0].SwapEntryPort != 443 {
		t.Fatal("reopen lost independent relay/entry mapping", get.Code, saved, err)
	}
	in.EventID = "creation-api-update-0001"
	in.ExpectedPlanRevision = 1
	in.ExpectedInboundRevision = 1
	in.Profile.Name = "Edited creation"
	in.Nodes[0].SwapEntryAddress = ""
	updated := browserRequest(t, s.Handler(), "PUT", base+"/501", in, cookie, csrf, "https://"+browserHost)
	if updated.Code != 200 {
		t.Fatal("revision-guarded update rejected", updated.Code, updated.Body.String())
	}
	var next business.NativeInboundPlanReceipt
	if err := json.Unmarshal(updated.Body.Bytes(), &next); err != nil || next.Plan.PlanRevision != 2 || next.Plan.InboundRevision != 2 || next.Plan.Status != "WAITING_HOST" || next.Plan.Profile.PublicKey != receipt.Plan.Profile.PublicKey {
		t.Fatal("update lost key/revision/waiting status", next, err)
	}
	in.EventID = "creation-api-stale-update-0001"
	if response := request("PUT", base+"/501", in); response.StatusCode != 409 {
		t.Fatal("stale editor overwrote saved plan", response.StatusCode)
	}
	targets, err := s.business.ListInboundTargets(context.Background())
	if err != nil || len(targets) != 0 {
		t.Fatal("creation materialized runtime targets", targets, err)
	}
}

func TestCreationPlanAPIOwnerCSRFNodeIdentityAndAgentIsolation(t *testing.T) {
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbound-plans"
	for _, tc := range []struct {
		method, path  string
		actor         *http.Cookie
		token, origin string
		want          int
	}{
		{"GET", base + "/capabilities", nil, "", "", 401},
		{"POST", base + "/preview", nil, csrf, "https://" + browserHost, 401},
		{"POST", base, cookie, "bad", "https://" + browserHost, 403},
		{"POST", base + "/preview", cookie, csrf, "https://foreign.example.org", 403},
		{"PUT", base + "/501", cookie, "", "https://" + browserHost, 403},
	} {
		var body any
		if tc.method != "GET" {
			body = in
		}
		response := browserRequest(t, s.Handler(), tc.method, tc.path, body, tc.actor, tc.token, tc.origin)
		if response.Code != tc.want {
			t.Fatal("owner/CSRF protection failed", tc.method, tc.path, response.Code, response.Body.String())
		}
	}
	mutated := in
	mutated.Nodes = append([]business.NativeInboundPlanNode(nil), in.Nodes...)
	mutated.Nodes[0].SwapSourceInstallationID = strings.Repeat("c", 32)
	response := browserRequest(t, s.Handler(), "POST", base, mutated, cookie, csrf, "https://"+browserHost)
	if response.Code != 409 || !strings.Contains(response.Body.String(), `"pairing_changed"`) {
		t.Fatal("stale relay-only source installation accepted", response.Code, response.Body.String())
	}
	mutated = in
	mutated.Nodes = append([]business.NativeInboundPlanNode(nil), in.Nodes...)
	mutated.Nodes[0].InstallationID = strings.Repeat("c", 32)
	if w := browserRequest(t, s.Handler(), "POST", base+"/preview", mutated, cookie, csrf, "https://"+browserHost); w.Code != 409 {
		t.Fatal("stale destination installation accepted", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", base, map[string]any{"unexpected": true}, cookie, csrf, "https://"+browserHost); w.Code != 422 {
		t.Fatal("unknown JSON field accepted", w.Code)
	}
	for _, path := range []string{base + "/capabilities", base + "/501"} {
		if w := browserRequest(t, s.AgentHandler(), "GET", path, nil, cookie, "", ""); w.Code == 200 {
			t.Fatal("Agent listener exposed creation authoring", path)
		}
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "rep-creation", "Representative creation"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "rep-creation", "rep-creation-login", repPassword); err != nil {
		t.Fatal(err)
	}
	repCookie, repCSRF := browserLogin(t, s, "rep-creation-login", repPassword)
	for _, method := range []string{"GET", "POST", "PUT"} {
		path := base
		var body any = in
		if method == "GET" {
			path += "/capabilities"
			body = nil
		}
		if method == "PUT" {
			path += "/501"
		}
		w := browserRequest(t, s.Handler(), method, path, body, repCookie, repCSRF, "https://"+browserHost)
		if w.Code != 403 {
			t.Fatal("representative accessed owner creation setup", method, w.Code)
		}
	}
}

func TestCreationPlanAPIReopensStaleReferencesWithoutAllowingSave(t *testing.T) {
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbound-plans"
	if w := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Simulate a registry that no longer contains the original installation.
	registry, err := noderegistry.Open(filepath.Join(t.TempDir(), "private", "registry.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.registry = registry
	get := browserRequest(t, s.Handler(), "GET", base+"/501", nil, cookie, "", "")
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"references_current":false`) || !strings.Contains(get.Body.String(), `"swap_source_port":31000`) {
		t.Fatal("stale registry destroyed saved author content", get.Code, get.Body.String())
	}
	// The already committed event is a receipt read, not a new mutation.
	if replay := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost); replay.Code != 200 || !strings.Contains(replay.Body.String(), `"replayed":true`) {
		t.Fatal("lost-response retry could not recover historical receipt after registry change", replay.Code, replay.Body.String())
	}
	in.EventID = "creation-stale-registry-update-0001"
	in.ExpectedPlanRevision = 1
	in.ExpectedInboundRevision = 1
	if w := browserRequest(t, s.Handler(), "PUT", base+"/501", in, cookie, csrf, "https://"+browserHost); w.Code != 409 {
		t.Fatal("stale saved references were not revalidated", w.Code)
	}
}

func TestCreationPlanAPICapabilityDisabledWithoutNativeKey(t *testing.T) {
	s := enabledOwner(t)
	cookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	w := browserRequest(t, s.Handler(), "GET", "/api/v1/workspace/native-inbound-plans/capabilities", nil, cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatal("disabled capability undiscoverable", w.Code, w.Body.String())
	}
}

func TestCreationPlanAPIGenericOwnerOperationsCannotBypassStaging(t *testing.T) {
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	if w := browserRequest(t, s.Handler(), "POST", "/api/v1/workspace/native-inbound-plans", in, cookie, csrf, "https://"+browserHost); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	client, err := s.business.CreateClient(context.Background(), business.NewClient{EventID: "api-plan-boundary-client-0001", ClientID: "api-plan-boundary-client", RepresentativeID: "owner", Name: "Existing client", DurationDays: 1, VolumeBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspace/native-inbounds/501"
	profile := in.Profile
	profile.Name = "Attempt generic edit"
	for _, attempt := range []struct {
		path string
		body any
	}{
		{base + "/lifecycle", business.InboundMutation{EventID: "api-plan-generic-enable-0001", ExpectedRevision: 1, Action: "enable", Reason: "Generic enable"}},
		{base + "/lifecycle", business.InboundMutation{EventID: "api-plan-generic-edit-0001", ExpectedRevision: 1, Action: "edit", Profile: &profile, Reason: "Generic edit"}},
		{base + "/target", business.InboundTarget{InboundID: 501, NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20", DirectPort: 21185}},
		{base + "/clients", map[string]any{"client_ids": []string{client.ID}, "attach": true}},
	} {
		if w := browserRequest(t, s.Handler(), "POST", attempt.path, attempt.body, cookie, csrf, "https://"+browserHost); w.Code != 409 {
			t.Fatal("generic owner API bypassed plan boundary", attempt.path, w.Code, w.Body.String())
		}
	}
	in.EventID = "api-plan-legitimate-edit-0001"
	in.ExpectedPlanRevision = 1
	in.ExpectedInboundRevision = 1
	in.Profile.Name = "Edited through complete plan"
	if w := browserRequest(t, s.Handler(), "PUT", "/api/v1/workspace/native-inbound-plans/501", in, cookie, csrf, "https://"+browserHost); w.Code != 200 {
		t.Fatal("legitimate plan edit blocked", w.Code, w.Body.String())
	}
	archive := business.InboundMutation{EventID: "api-plan-logical-cancel-0001", ExpectedRevision: 2, Action: "archive", Reason: "Cancel staged setup"}
	if w := browserRequest(t, s.Handler(), "POST", base+"/lifecycle", archive, cookie, csrf, "https://"+browserHost); w.Code != 200 {
		t.Fatal("safe cancellation blocked", w.Code, w.Body.String())
	}
}
