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

func p0GFixture(t *testing.T) (*Server, *http.Cookie, string, string) {
	t.Helper()
	ctx := context.Background()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0x58}, 32)
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	for _, n := range []struct{ id, inst, token string }{
		{"nl", strings.Repeat("a", 32), strings.Repeat("1", 64)},
		{"de", strings.Repeat("b", 32), strings.Repeat("2", 64)},
	} {
		ticket, err := s.registry.Issue(n.id, "Synthetic "+n.id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.registry.Claim(ticket.PairCode, n.id, n.inst, n.token); err != nil {
			t.Fatal(err)
		}
	}
	in := business.InboundInput{ID: 1185, Name: "P0G QA Reality",
		Transport: "tcp", RealityTarget: "www.example.org:443",
		ServerNames: []string{"www.example.org"}, ShortIDs: []string{"aa11"}}
	if _, err := s.business.CreateInbound(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	for _, tgt := range []business.InboundTarget{
		{InboundID: 1185, NodeID: "nl", InstallationID: strings.Repeat("a", 32),
			ListenIP: "198.51.100.10", SwapPort: 26000},
		{InboundID: 1185, NodeID: "de", InstallationID: strings.Repeat("b", 32),
			ListenIP: "198.51.100.20", DirectPort: 21185, SwapPort: 26001},
	} {
		if err := s.business.PutInboundTarget(ctx, tgt); err != nil {
			t.Fatal(err)
		}
	}
	draft := business.NativeRouteDraftInput{EventID: "p0g-synthetic-route-draft-001",
		InboundID: 1185, NodeID: "de", InstallationID: strings.Repeat("b", 32),
		ExpectedInboundRevision: 1,
		DirectPort:              21185, DirectHosts: []string{"de.example.org"},
		SwapEnabled: true, SwapUpstreamNodeID: "nl",
		SwapTunnelHost: "nl-proxy.example.org", SwapTunnelPort: 26000, SwapNodePort: 26001,
		Reason: "QA two-hop readiness only"}
	if _, err := s.business.SaveNativeRouteDraft(ctx, key, draft); err != nil {
		t.Fatal(err)
	}
	owner, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	return s, owner, csrf, "/api/v1/workspace/native-inbounds/1185/route-drafts/de/readiness"
}
func p0GRead(t *testing.T, s *Server, path string, cookie *http.Cookie) (NativeRouteReadiness, int, string) {
	t.Helper()
	res := browserRequest(t, s.Handler(), "GET", path, nil, cookie, "", "")
	var data NativeRouteReadiness
	if res.Code == 200 {
		if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
	}
	return data, res.Code, res.Body.String()
}
func p0GContains(a []string, needle string) bool {
	for _, v := range a {
		if v == needle {
			return true
		}
	}
	return false
}
func TestP0GReadinessSeparatesTargetUpstreamAndNoFabricatedRouteACK(t *testing.T) {
	s, owner, _, path := p0GFixture(t)
	if _, code, _ := p0GRead(t, s, path, nil); code != 401 {
		t.Fatal("anonymous read accepted", code)
	}
	first, code, raw := p0GRead(t, s, path, owner)
	if code != 200 || !first.PlanConsistent || first.DraftRevision != 1 ||
		first.Target.NodeID != "de" || first.Upstream == nil || first.Upstream.NodeID != "nl" ||
		!first.Target.PairingMatched || !first.Upstream.PairingMatched {
		t.Fatal("fresh signed identity unavailable", code, first, raw)
	}
	if first.Target.Online || first.Upstream.Online || first.Target.MetricsCurrent || first.Upstream.MetricsCurrent ||
		first.ApplyAllowed || first.RouteSignedAckVerified || first.CustomerDataPlaneVerified ||
		first.SwapForwardingVerified || first.UniqueBillingVerified || first.AutomaticRollbackVerified ||
		first.SubscriptionPublished {
		t.Fatal("offline/new draft invented runtime evidence", first)
	}
	for _, block := range []string{"target_agent_offline", "upstream_agent_offline",
		"route_stage_receipt_missing", "swap_forwarding_unverified", "unique_billing_unverified"} {
		if !p0GContains(first.Blockers, block) {
			t.Fatal("missing prerequisite", block, first.Blockers)
		}
	}
	for _, secret := range []string{"nl-proxy.example.org", strings.Repeat("1", 64),
		strings.Repeat("2", 64), "private_key", "client_uuid"} {
		if strings.Contains(raw, secret) {
			t.Fatal("readiness leaks sensitive route details")
		}
	}
	// A fresh TARGET heartbeat must never refresh an independently
	// disconnected/stale upstream.
	if err := s.fleet.Heartbeat(telemetry.Pulse{NodeID: "de",
		InstallationID: strings.Repeat("b", 32), BootID: strings.Repeat("e", 32), Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	after, code, _ := p0GRead(t, s, path, owner)
	if code != 200 || !after.Target.Online || after.Upstream == nil || after.Upstream.Online ||
		!p0GContains(after.Blockers, "upstream_agent_offline") {
		t.Fatal("downstream heartbeats hid offline upstream", code, after)
	}
	if err := s.fleet.Heartbeat(telemetry.Pulse{NodeID: "nl",
		InstallationID: strings.Repeat("a", 32), BootID: strings.Repeat("f", 32), Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	both, code, _ := p0GRead(t, s, path, owner)
	if code != 200 || !both.Target.Online || both.Upstream == nil || !both.Upstream.Online ||
		both.ApplyAllowed || both.RouteSignedAckVerified || both.UniqueBillingVerified ||
		!p0GContains(both.Blockers, "target_native_runtime_not_ready") ||
		!p0GContains(both.Blockers, "upstream_native_runtime_not_ready") {
		t.Fatal("fresh heartbeat mistaken for signed route and billing", both, code)
	}
}

func TestP0GReadinessDetectsStaleNativeBindingWithoutAnyMutation(t *testing.T) {
	s, owner, _, path := p0GFixture(t)
	ctx := context.Background()
	// Simulate a changed NATIVE binding after the owner saved a draft.
	before, code, _ := p0GRead(t, s, path, owner)
	if code != 200 || !before.PlanConsistent {
		t.Fatal("fixture not consistent")
	}
	changed := business.InboundTarget{InboundID: 1185, NodeID: "de",
		InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20",
		DirectPort: 21186, SwapPort: 26001}
	if err := s.business.PutInboundTarget(ctx, changed); err != nil {
		t.Fatal(err)
	}
	after, code, _ := p0GRead(t, s, path, owner)
	if code != 200 || after.PlanConsistent || after.ApplyAllowed ||
		!p0GContains(after.Blockers, "target_attachment_changed") ||
		!p0GContains(after.Blockers, "plan_not_consistent") {
		t.Fatal("stale native target incorrectly accepted", code, after)
	}
	current, err := s.business.NativeInboundTarget(ctx, 1185, "de")
	if err != nil || current.DirectPort != 21186 {
		t.Fatal("read-only report mutated binding", current, err)
	}
	original, err := s.business.NativeRouteDraft(ctx, bytes.Repeat([]byte{0x58}, 32), 1185, "de")
	if err != nil || original.DirectPort != 21185 || original.DraftRevision != 1 {
		t.Fatal("readiness silently rewrote encrypted prior owner intent", original, err)
	}
}
func TestP0GReadinessOwnerOnlyAndAgentCannotAccess(t *testing.T) {
	s, owner, csrf, path := p0GFixture(t)
	if _, code, _ := p0GRead(t, s, path, owner); code != 200 {
		t.Fatal("owner denied", code)
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "p0g-rep", "Synthetic Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(),
		"p0g-rep", "p0g-rep-login", "password-for-p0g-test-123"); err != nil {
		t.Fatal(err)
	}
	repCookie, _ := browserLogin(t, s, "p0g-rep-login", "password-for-p0g-test-123")
	if _, code, _ := p0GRead(t, s, path, repCookie); code != 403 {
		t.Fatal("representative observed owner-only Node/route state", code)
	}
	if w := browserRequest(t, s.Handler(), "POST", path, map[string]string{},
		owner, csrf, "https://"+browserHost); w.Code != 405 {
		t.Fatal("write verb allowed for immutable readiness", w.Code)
	}
	agent := httptest.NewRequest(http.MethodGet,
		"https://agent.test"+path, nil)
	agent.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	agent.Header.Set("Authorization", "Bearer "+strings.Repeat("2", 64))
	w := httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(w, agent)
	if w.Code != 404 {
		t.Fatal("private owner readiness exposed over Agent transport", w.Code)
	}
}
