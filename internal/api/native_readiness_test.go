package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func TestOwnerNativeReadinessReportsRealPrerequisitesAndNeverAuthorizesMeteredRuntime(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{7}, 32)
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	cookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	path := "/api/v1/workspace/native-inbounds-state"
	read := func(cookie *http.Cookie) (NativeReadiness, int, string) {
		response := browserRequest(t, s.BrowserHandler(), "GET", path, nil, cookie, "", "")
		var value struct {
			Preflight NativeReadiness `json:"preflight"`
		}
		if response.Code == http.StatusOK {
			if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
		}
		return value.Preflight, response.Code, response.Body.String()
	}
	if _, code, _ := read(nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous accessed private readiness: %d", code)
	}
	first, code, raw := read(cookie)
	if code != http.StatusOK {
		t.Fatalf("owner readiness unavailable: %d %s", code, raw)
	}
	if first.DeploymentReady || first.CustomerDataPlaneVerified || first.SubscriptionPublished ||
		first.PairedNodes != 0 || first.OnlineNodes != 0 || first.FreshNodes != 0 ||
		first.UsageLedgerEnabled || first.BlockingReason != "remote_metered_admission_not_wired" {
		t.Fatalf("empty PRO lab falsely presented as deployable: %+v", first)
	}

	invitation, err := s.registry.Issue("qa-ready-node", "Independent QA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.registry.Claim(invitation.PairCode, "qa-ready-node", installID, nodeToken); err != nil {
		t.Fatal(err)
	}
	boot := strings.Repeat("b", 32)
	if err := s.fleet.Heartbeat(telemetry.Pulse{
		NodeID: "qa-ready-node", InstallationID: installID, BootID: boot, Sequence: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.fleet.Record(telemetry.Sample{
		NodeID: "qa-ready-node", InstallationID: installID, Name: "Independent QA",
		BootID: boot, Sequence: 1, Interface: "eth0", CPUCores: 2,
		CPUPercent: 5, RAMTotal: 1 << 30, RAMUsed: 1 << 28,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	input := business.InboundInput{ID: 9091, Name: "Private QA REALITY", Transport: "tcp",
		RealityTarget: "www.example.org:443",
		ServerNames:   []string{"www.example.org"}, ShortIDs: []string{"aa11"},
	}
	if _, err := s.business.CreateInbound(ctx, key, input); err != nil {
		t.Fatal(err)
	}
	if err := s.business.PutInboundTarget(ctx, business.InboundTarget{
		InboundID: 9091, NodeID: "qa-ready-node", InstallationID: installID,
		ListenIP: "127.0.0.1", DirectPort: 19443,
	}); err != nil {
		t.Fatal(err)
	}
	ready, code, raw := read(cookie)
	if code != http.StatusOK {
		t.Fatalf("owner ready snapshot unavailable: %d %s", code, raw)
	}
	if ready.PairedNodes != 1 || ready.OnlineNodes != 1 || ready.FreshNodes != 1 ||
		ready.ConfiguredInbounds != 1 || ready.ConfiguredTargets != 1 ||
		ready.ClientBindings != 0 || !ready.UsageLedgerEnabled {
		t.Fatalf("real preflight prerequisite counts wrong: %+v", ready)
	}
	if ready.DeploymentReady || ready.CustomerDataPlaneVerified || ready.SubscriptionPublished ||
		ready.RemoteMeteredAdmission != "trusted_remote_epoch_registration_pending" ||
		ready.BlockingReason != "remote_metered_admission_not_wired" {
		t.Fatalf("preflight invented authorization: %+v", ready)
	}
	if strings.Contains(raw, nodeToken) || strings.Contains(raw, invitation.PairCode) ||
		strings.Contains(raw, "private_key") {
		t.Fatal("preflight leaked installation or REALITY private credentials")
	}
	repeated, code, _ := read(cookie)
	if code != http.StatusOK || repeated != ready {
		t.Fatal("read-only native preflight changed state without input")
	}
	// Even with a fully paired Node, fresh heartbeat, Inbound, target and
	// metering ledger, the existing production Agent GET is 503-gated until
	// a trusted distributed birth+receipt protocol is actually integrated.
	req := httptest.NewRequest("GET",
		"https://agent.test/api/v1/agent/native-runtime?node_id=qa-ready-node&installation_id="+installID, nil)
	req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	req.Header.Set("Authorization", "Bearer "+nodeToken)
	w := httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "metered_runtime_admission_pending") {
		t.Fatalf("unreceipted metered listener might start: %d %s", w.Code, w.Body.String())
	}
}
