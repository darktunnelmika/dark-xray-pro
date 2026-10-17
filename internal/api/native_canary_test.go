package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func TestOwnerOnlyUSAProCanaryStagesOneLoopbackIdentityNeverActivates(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	key := []byte(strings.Repeat("x", 32))
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	route := "/api/v1/workspace/native-canary/stage"
	payload := map[string]string{"confirmation": ownerCanaryConfirmation}
	call := func(c *http.Cookie, csrf string, payload any) *httptest.ResponseRecorder {
		return browserRequest(t, s.Handler(), "POST", route, payload, c, csrf, "https://"+browserHost)
	}
	if w := call(nil, "", payload); w.Code != 401 {
		t.Fatalf("anonymous staged test customer: %d", w.Code)
	}
	if w := call(cookie, csrf, map[string]string{"confirmation": "wrong"}); w.Code != 422 {
		t.Fatalf("missing explicit consent accepted: %d", w.Code)
	}
	if w := call(cookie, "", payload); w.Code != 403 {
		t.Fatalf("CSRF missing allowed canary: %d", w.Code)
	}
	if w := call(cookie, csrf, payload); w.Code != 404 {
		t.Fatalf("unpaired Node staged customer: %d", w.Code)
	}
	inv, err := s.registry.Issue(ownerCanaryNode, "QA Node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.registry.Claim(inv.PairCode, ownerCanaryNode, installID, nodeToken); err != nil {
		t.Fatal(err)
	}
	if w := call(cookie, csrf, payload); w.Code != 503 {
		t.Fatalf("offline USA Node should fail closed: %d", w.Code)
	}
	boot := strings.Repeat("d", 32)
	if err = s.fleet.Heartbeat(telemetry.Pulse{NodeID: ownerCanaryNode,
		InstallationID: installID, BootID: boot, Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.fleet.Record(telemetry.Sample{NodeID: ownerCanaryNode,
		InstallationID: installID, BootID: boot, Name: "Independent QA",
		Sequence: 1, Interface: "eth0", CPUCores: 4, CPUPercent: 12.5,
		RAMTotal: 1 << 30, RAMUsed: 1 << 28}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.business.CreateRepresentative(ctx, "qa-r", "QA representative"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.business.ProvisionRepresentativeLogin(ctx, "qa-r", "qa-r", "pass-qa-representative-12345"); err != nil {
		t.Fatal(err)
	}
	repCookie, repCSRF := browserLogin(t, s, "qa-r", "pass-qa-representative-12345")
	if w := call(repCookie, repCSRF, payload); w.Code != 403 {
		t.Fatalf("rep can create owner canary: %d", w.Code)
	}
	w := call(cookie, csrf, payload)
	if w.Code != 201 {
		t.Fatalf("staging failed: HTTP %d %s", w.Code, w.Body.String())
	}
	var result map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["node_id"] != ownerCanaryNode || result["client_id"] != ownerCanaryClient ||
		result["listen_ip"] != "127.0.0.1" || result["customer_listener_started"] != false ||
		result["subscription_published"] != false {
		t.Fatalf("unsafe staging response: %#v", result)
	}
	list, err := s.business.ListClients(ctx, "", 200, 0)
	if err != nil || len(list) != 1 || list[0].ID != ownerCanaryClient {
		t.Fatalf("created unrelated/missing client: %+v %v", list, err)
	}
	inbounds, err := s.business.ListInbounds(ctx, key)
	if err != nil || len(inbounds) != 1 || inbounds[0].ID != ownerCanaryInbound || inbounds[0].BoundClients != 1 {
		t.Fatalf("wrong saved Inbound: %+v %v", inbounds, err)
	}
	targets, err := s.business.ListInboundTargets(ctx)
	if err != nil || len(targets) != 1 || targets[0].ListenIP != "127.0.0.1" ||
		targets[0].DirectPort != ownerCanaryPort || targets[0].SwapPort != 0 {
		t.Fatalf("staged a non-loopback public listener: %+v %v", targets, err)
	}
	usage, err := s.business.ClientUsage(ctx, ownerCanaryClient)
	if err != nil || !usage.Known || !usage.Eligible || usage.Total != 0 || usage.Limit != ownerCanaryVolume {
		t.Fatalf("canary account baseline unsafe: %+v %v", usage, err)
	}
	retry := call(cookie, csrf, payload)
	if retry.Code != 201 {
		t.Fatalf("same authenticated staging retry failed: %d", retry.Code)
	}
	list, err = s.business.ListClients(ctx, "", 200, 0)
	if err != nil || len(list) != 1 {
		t.Fatal("idempotent owner action created extra identities")
	}
	if w = call(cookie, csrf, map[string]any{"confirmation": ownerCanaryConfirmation, "force": true}); w.Code != 422 {
		t.Fatalf("unknown force/bypass field accepted: %d", w.Code)
	}
	if err = s.EnableRemoteMeteredNode(ownerCanaryNode); err != nil {
		t.Fatal(err)
	}
	if w = call(cookie, csrf, payload); w.Code != 503 {
		t.Fatalf("live metered mode allowed staging extra accounts: %d", w.Code)
	}
	// Staging DOES NOT create a usage epoch or a runtime lease, and the
	// browser cannot start the Xray listener through this endpoint.
	if got, err := s.business.NativeRuntimeStatus(ctx, ownerCanaryNode, installID); err != nil || got.ProcessReady {
		t.Fatalf("staging forged a live Xray process: %+v %v", got, err)
	}
}

func TestCanaryRefusesPreexistingCustomer(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	if err := s.EnableNativeInbounds([]byte(strings.Repeat("s", 32))); err != nil {
		t.Fatal(err)
	}
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	inv, err := s.registry.Issue(ownerCanaryNode, "QA Node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.registry.Claim(inv.PairCode, ownerCanaryNode, installID, nodeToken); err != nil {
		t.Fatal(err)
	}
	boot := strings.Repeat("b", 32)
	if err = s.fleet.Heartbeat(telemetry.Pulse{NodeID: ownerCanaryNode, InstallationID: installID, BootID: boot, Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.fleet.Record(telemetry.Sample{NodeID: ownerCanaryNode,
		InstallationID: installID, BootID: boot, Name: "QA Node", Sequence: 1,
		Interface: "eth0", CPUCores: 2, CPUPercent: 3, RAMTotal: 1 << 30, RAMUsed: 1 << 28}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.business.CreateClient(ctx, business.NewClient{
		EventID: "other-pro-client-exists-0001", ClientID: "unrelated-client",
		RepresentativeID: "owner", Name: "Existing", DurationDays: 1, VolumeBytes: 1 << 20,
	}); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	w := browserRequest(t, s.Handler(), "POST", "/api/v1/workspace/native-canary/stage",
		map[string]string{"confirmation": ownerCanaryConfirmation}, cookie, csrf, "https://"+browserHost)
	if w.Code != 409 {
		t.Fatalf("unrelated customer case overwritten: %d", w.Code)
	}
	list, err := s.business.ListClients(ctx, "", 200, 0)
	if err != nil || len(list) != 1 || list[0].ID != "unrelated-client" {
		t.Fatal("preexisting customer's account unexpectedly altered")
	}
}
