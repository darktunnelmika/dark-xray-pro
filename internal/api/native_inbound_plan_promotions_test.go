package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

type promotionAPIFixture struct {
	s       *Server
	key     []byte
	cookie  *http.Cookie
	csrf    string
	plan    business.NativeInboundPlan
	input   business.NativeInboundPlanPromotionInput
	clients []business.Client
	node    business.MeteredRouteNode
	status  mx.TransactionStatus
}

func newPromotionAPIFixture(t *testing.T, enabled bool) promotionAPIFixture {
	t.Helper()
	ctx := context.Background()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0x62}, 32)
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	node := business.MeteredRouteNode{NodeID: "promotion-qa", InstallationID: installID, BinarySHA: strings.Repeat("a", 64)}
	ticket, err := s.registry.Issue(node.NodeID, "Disposable promotion Node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.registry.Claim(ticket.PairCode, node.NodeID, node.InstallationID, nodeToken); err != nil {
		t.Fatal(err)
	}
	if enabled {
		if err = s.EnableMeteredRouteNodesForPlanPromotion([]business.MeteredRouteNode{node}); err != nil {
			t.Fatal(err)
		}
	}
	f := promotionAPIFixture{s: s, key: key, node: node}
	for i := 0; i < 2; i++ {
		c, err := s.business.CreateClient(ctx, business.NewClient{ClientID: fmt.Sprintf("promotion-client-%d", i), EventID: fmt.Sprintf("promotion-create-client-%d", i), RepresentativeID: "owner", Name: fmt.Sprintf("Synthetic client %d", i), DurationDays: 30, VolumeBytes: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		f.clients = append(f.clients, c)
	}
	f.cookie, f.csrf = browserLogin(t, s, ownerUsername, ownerPassword)
	create := business.NativeInboundPlanInput{EventID: fmt.Sprintf("%032x", 901), Reason: "Save complete isolated plan",
		Profile: business.InboundInput{ID: 8123, Name: "Saved plan execution", Transport: "tcp", RealityTarget: "qa.example.org:443", ServerNames: []string{"qa.example.org"}, ShortIDs: []string{"aa11"}},
		Nodes:   []business.NativeInboundPlanNode{{NodeID: node.NodeID, InstallationID: node.InstallationID, ListenIP: "127.0.0.1", DirectPort: 28123, DirectHosts: []string{"127.0.0.1"}}}}
	w := f.post(t, "/api/v1/workspace/native-inbound-plans", create)
	var receipt business.NativeInboundPlanReceipt
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil {
		t.Fatal("save fixture plan", w.Code, w.Body.String())
	}
	f.plan = receipt.Plan
	f.input = business.NativeInboundPlanPromotionInput{EventID: fmt.Sprintf("%032x", 902), ExpectedPlanRevision: f.plan.PlanRevision, ExpectedInboundRevision: f.plan.InboundRevision, ClientIDs: []string{f.clients[0].ID, f.clients[1].ID}, Reason: "Review first metered LAB execution"}
	if enabled {
		f.poll(t)
	}
	return f
}

func (f *promotionAPIFixture) base() string {
	return "/api/v1/workspace/native-inbound-plans/8123/promotion"
}
func (f *promotionAPIFixture) post(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return browserRequest(t, f.s.Handler(), http.MethodPost, path, body, f.cookie, f.csrf, "https://"+browserHost)
}
func (f *promotionAPIFixture) poll(t *testing.T) {
	t.Helper()
	f.status = mx.TransactionStatus{Usage: &mx.MeteredCondition{}, Instance: strings.Repeat("c", 32), Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}
	poll := mx.SignMeteredTransactionPoll([]byte(nodeToken), mx.TransactionPoll{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID, BinarySHA: f.node.BinarySHA, At: time.Now().UnixNano(), Status: f.status})
	raw, err := json.Marshal(poll)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent/metered-route-transactions/poll", bytes.NewReader(raw))
	r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	r.Header.Set("Authorization", "Bearer "+nodeToken)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.s.AgentHandler().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("fresh empty signed Agent observation", w.Code, w.Body.String())
	}
}

func (f *promotionAPIFixture) review(t *testing.T) business.NativeInboundPlanPromotionPreview {
	t.Helper()
	w := f.post(t, f.base()+"/preview", f.input)
	var preview business.NativeInboundPlanPromotionPreview
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || !preview.Eligible || len(preview.Blockers) != 0 || len(preview.ReviewSHA256) != 64 || preview.RuntimeChanged || preview.SubscriptionPublished {
		t.Fatal("eligible exact promotion review", w.Code, w.Body.String())
	}
	return preview
}

func (f *promotionAPIFixture) assertPaused(t *testing.T) {
	t.Helper()
	in, err := f.s.business.NativeInbound(context.Background(), f.key, 8123)
	if err != nil || in.Enabled || in.BoundClients != 0 || in.Revision != f.plan.InboundRevision {
		t.Fatal("failed/read-only operation changed saved plan", in, err)
	}
	if _, err = f.s.business.NativeInboundTarget(context.Background(), 8123, f.node.NodeID); err == nil {
		t.Fatal("read-only operation materialized a target")
	}
}

func TestInboundPlanPromotionOwnerAtomicReplayAndStage(t *testing.T) {
	f := newPromotionAPIFixture(t, true)
	w := browserRequest(t, f.s.Handler(), http.MethodGet, f.base(), nil, f.cookie, "", "")
	var state struct {
		Available bool `json:"available"`
		business.NativeInboundPlanPromotionState
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || !state.Available || state.Promoted || len(state.Blockers) != 0 {
		t.Fatal("promotion discovery", w.Code, w.Body.String())
	}
	preview := f.review(t)
	f.assertPaused(t)
	f.input.ReviewSHA256 = preview.ReviewSHA256
	if w = f.post(t, f.base(), f.input); w.Code != 422 {
		t.Fatal("promotion without explicit confirmation", w.Code)
	}
	f.assertPaused(t)
	w = f.post(t, f.base(), nativeInboundPlanPromotionWire{NativeInboundPlanPromotionInput: f.input, Confirmed: true})
	var receipt business.NativeInboundPlanPromotionReceipt
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || !receipt.Promoted || receipt.Replayed || receipt.RuntimeChanged || receipt.SubscriptionPublished || receipt.ReviewSHA256 != preview.ReviewSHA256 || receipt.InboundRevision != 2 || receipt.DraftRevision != 1 {
		t.Fatal("atomic promotion receipt", w.Code, w.Body.String())
	}
	for _, secret := range []string{f.clients[0].UUID, f.clients[1].UUID, "private_key", "reality_private_key", "\"config\":"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("public promotion response leaked credentials")
		}
	}
	in, err := f.s.business.NativeInbound(context.Background(), f.key, 8123)
	if err != nil || !in.Enabled || in.BoundClients != 2 || in.PublicKey != f.plan.Profile.PublicKey || in.Revision != receipt.InboundRevision {
		t.Fatal("promotion lost native identity/bindings", err)
	}
	target, err := f.s.business.NativeInboundTarget(context.Background(), 8123, f.node.NodeID)
	if err != nil || target.DirectPort != 28123 || target.ListenIP != "127.0.0.1" || target.SwapPort != 0 {
		t.Fatal("promotion changed saved target", target, err)
	}
	draft, err := f.s.business.NativeRouteDraft(context.Background(), f.key, 8123, f.node.NodeID)
	if err != nil || draft.ExpectedInboundRevision != 2 || draft.DraftRevision != 1 || draft.DirectPort != target.DirectPort || draft.SwapEnabled || draft.ApplyAllowed {
		t.Fatal("promotion did not produce exact nonactivating route draft", err)
	}
	channel, err := f.s.business.MeteredRouteChannel(context.Background(), f.key, f.node.NodeID)
	if err != nil || len(channel.Commands) != 0 || channel.Status.Observed.ProcessReady {
		t.Fatal("promotion silently started execution", err)
	}
	w = f.post(t, f.base(), nativeInboundPlanPromotionWire{NativeInboundPlanPromotionInput: f.input, Confirmed: true})
	var replay business.NativeInboundPlanPromotionReceipt
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || !replay.Replayed {
		t.Fatal("exact replay", w.Code, w.Body.String())
	}
	replay.Replayed = false
	if !reflect.DeepEqual(receipt, replay) {
		t.Fatal("promotion replay changed historical receipt")
	}
	plan, err := f.s.business.NativeInboundPlan(context.Background(), f.key, 8123)
	if err != nil || plan.Editable || plan.EditBlocker != "plan_promoted" || plan.Profile.PublicKey != f.plan.Profile.PublicKey {
		t.Fatal("promoted plan history unavailable", err)
	}
	for _, body := range []any{
		map[string]any{"action": "disable", "event_id": fmt.Sprintf("%032x", 920), "expected_revision": 2, "reason": "Generic mutation must remain blocked"},
		map[string]any{"action": "archive", "event_id": fmt.Sprintf("%032x", 921), "expected_revision": 2, "reason": "Generic archive must remain blocked"},
	} {
		if w = f.post(t, "/api/v1/workspace/native-inbounds/8123/lifecycle", body); w.Code < 400 {
			t.Fatal("generic lifecycle bypassed promotion", w.Code)
		}
	}
	if w = f.post(t, "/api/v1/workspace/native-inbounds/8123/clients", map[string]any{"client_ids": []string{f.clients[0].ID}, "attach": false}); w.Code < 400 {
		t.Fatal("generic detach bypassed frozen promotion")
	}
	for _, before := range f.clients {
		after, e := f.s.business.Client(context.Background(), before.ID)
		if e != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("promotion changed customer or financial record", e)
		}
	}
	stage := business.MeteredRouteCommandRequest{ID: fmt.Sprintf("%032x", 930), Action: "stage", RuntimeInstance: f.status.Instance, Expected: mx.Condition(f.status.Observed), Usage: f.status.Usage, Drafts: []business.RouteDraftRef{{InboundID: 8123, InboundRevision: receipt.InboundRevision, DraftRevision: receipt.DraftRevision}}}
	w = f.post(t, "/api/v1/workspace/native-nodes/"+f.node.NodeID+"/metered-route-transactions", stage)
	if w.Code != 202 {
		t.Fatal("promoted saved plan cannot enter existing Stage queue", w.Code, w.Body.String())
	}
}

func TestInboundPlanPromotionWireAndAuthority(t *testing.T) {
	f := newPromotionAPIFixture(t, true)
	h := f.s.Handler()
	for _, tc := range []struct {
		method, path string
		cookie       *http.Cookie
		csrf, origin string
		want         int
	}{
		{"GET", f.base(), nil, "", "", 401},
		{"POST", f.base() + "/preview", nil, f.csrf, "https://" + browserHost, 401},
		{"POST", f.base(), f.cookie, "bad", "https://" + browserHost, 403},
		{"POST", f.base() + "/preview", f.cookie, f.csrf, "https://foreign.example.org", 403},
	} {
		var body any
		if tc.method == "POST" {
			body = f.input
		}
		w := browserRequest(t, h, tc.method, tc.path, body, tc.cookie, tc.csrf, tc.origin)
		if w.Code != tc.want {
			t.Fatal("promotion authorization", tc.path, w.Code)
		}
	}
	if _, err := f.s.business.CreateRepresentative(context.Background(), "promotion-rep", "Promotion representative"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.ProvisionRepresentativeLogin(context.Background(), "promotion-rep", "promotion-login", repPassword); err != nil {
		t.Fatal(err)
	}
	rep, csrf := browserLogin(t, f.s, "promotion-login", repPassword)
	for _, path := range []string{f.base(), f.base() + "/preview"} {
		if w := browserRequest(t, h, "POST", path, f.input, rep, csrf, "https://"+browserHost); w.Code != 403 {
			t.Fatal("representative promotion", w.Code)
		}
	}
	if w := browserRequest(t, h, "GET", f.base(), nil, rep, "", ""); w.Code != 403 {
		t.Fatal("representative discovery", w.Code)
	}
	for _, path := range []string{f.base(), f.base() + "/preview"} {
		if w := browserRequest(t, f.s.AgentHandler(), "POST", path, f.input, f.cookie, f.csrf, "https://"+browserHost); w.Code < 400 {
			t.Fatal("owner promotion exposed on Agent plane")
		}
	}
	r := httptest.NewRequest("GET", f.base(), nil)
	r.Header.Set("Authorization", "Bearer "+f.s.ownerKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("bearer substituted for owner browser session", w.Code)
	}
	raw, err := json.Marshal(f.input)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{`,"event_id":"another-event"}`, `,"confirmed":false,"confirmed":true}`, `,"client_ids":null}`, `,"review_sha256":null}`, `,"node_id":"foreign"}`, `,"runtime_apply":true}`} {
		bad := json.RawMessage(append(append([]byte(nil), raw[:len(raw)-1]...), []byte(suffix)...))
		if w := f.post(t, f.base()+"/preview", bad); w.Code != 422 {
			t.Fatal("ambiguous promotion JSON", w.Code)
		}
	}
	for _, path := range []string{"/api/v1/workspace/native-inbound-plans/08123/promotion", f.base() + "?enable=true"} {
		if w := browserRequest(t, h, "GET", path, nil, f.cookie, "", ""); w.Code != 422 {
			t.Fatal("ambiguous promotion path", w.Code)
		}
	}
	f.assertPaused(t)
}

func TestInboundPlanPromotionStartupIsExplicitAndOneNode(t *testing.T) {
	f := newPromotionAPIFixture(t, false)
	w := browserRequest(t, f.s.Handler(), "GET", f.base(), nil, f.cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) || !strings.Contains(w.Body.String(), `"code":"promotion_disabled"`) {
		t.Fatal("default-off discovery", w.Code, w.Body.String())
	}
	if w = f.post(t, f.base()+"/preview", f.input); w.Code != 409 || !strings.Contains(w.Body.String(), "promotion_disabled") {
		t.Fatal("browser enabled promotion", w.Code)
	}
	if err := f.s.EnableMeteredRouteNodes([]business.MeteredRouteNode{f.node}); err == nil {
		t.Fatal("old startup path acquired empty channel")
	}
	if f.s.meteredRouteNodes != nil || f.s.inboundPlanPromotionEnabled {
		t.Fatal("failed claim partially enabled HTTP scope")
	}
	if err := f.s.EnableMeteredRouteNodesForPlanPromotion([]business.MeteredRouteNode{f.node, f.node}); err == nil {
		t.Fatal("multiple startup promotion Nodes accepted")
	}
	if err := f.s.EnableMeteredRouteNodesForPlanPromotion([]business.MeteredRouteNode{f.node}); err != nil {
		t.Fatal("explicit empty startup reservation", err)
	}
	f.poll(t)
	f.review(t)
	if err := f.s.EnableMeteredRouteNodesForPlanPromotion([]business.MeteredRouteNode{f.node}); err == nil {
		t.Fatal("startup allowlist changed while running")
	}
	f.assertPaused(t)
}
