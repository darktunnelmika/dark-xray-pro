package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func meteredRouteAPIFixture(t *testing.T, binary string, port uint16) (*Server, []byte) {
	t.Helper()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0x41}, 32)
	ctx := context.Background()
	if e := s.business.EnableUsageLedger(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.EnableNativeInbounds(key); e != nil {
		t.Fatal(e)
	}
	ticket, e := s.registry.Issue("route-qa", "Disposable route Agent")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.registry.Claim(ticket.PairCode, "route-qa", installID, nodeToken); e != nil {
		t.Fatal(e)
	}
	if _, e = s.business.CreateInbound(ctx, key, business.InboundInput{ID: 7612, Name: "Route queue QA", Transport: "tcp", RealityTarget: "qa.invalid:443", ServerNames: []string{"qa.example.org"}, ShortIDs: []string{"aa11"}}); e != nil {
		t.Fatal(e)
	}
	if e = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 7612, NodeID: "route-qa", InstallationID: installID, ListenIP: "127.0.0.1", DirectPort: port}); e != nil {
		t.Fatal(e)
	}
	customer, e := s.business.CreateClient(ctx, business.NewClient{ClientID: "metered-route-qa", EventID: "metered-route-qa-client-001", RepresentativeID: "owner", Name: "Disposable metered route", DurationDays: 30, VolumeBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.business.BindInboundClients(ctx, 7612, []string{customer.ID}, true); e != nil {
		t.Fatal(e)
	}
	meteredRouteAPIDraft(t, s, key, 0, port)
	if e = s.EnableMeteredRouteTransactions("route-qa", installID, binary); e != nil {
		t.Fatal(e)
	}
	return s, key
}
func meteredRouteAPIDraft(t *testing.T, s *Server, key []byte, revision uint64, port uint16) {
	t.Helper()
	_, e := s.business.SaveNativeRouteDraft(context.Background(), key, business.NativeRouteDraftInput{EventID: fmt.Sprintf("route-api-draft-%04d", revision), InboundID: 7612, NodeID: "route-qa", InstallationID: installID, ExpectedInboundRevision: 1, ExpectedDraftRevision: revision, DirectPort: port, Reason: "Loopback only owner review"})
	if e != nil {
		t.Fatal(e)
	}
}
func meteredRouteOwnerRequest(status mx.TransactionStatus, id int, action, stage string, revision uint64) business.MeteredRouteCommandRequest {
	in := business.MeteredRouteCommandRequest{Usage: status.Usage, ID: fmt.Sprintf("%032x", id), Action: action, StageID: stage, RuntimeInstance: status.Instance, Expected: mx.Condition(status.Observed)}
	if action == "stage" {
		in.Drafts = []business.RouteDraftRef{{InboundID: 7612, InboundRevision: 1, DraftRevision: revision}}
	}
	return in
}
func TestMeteredRouteQueueAPIAuthenticatesOwnerAndAgentWithoutPrivateLeak(t *testing.T) {
	s, key := meteredRouteAPIFixture(t, strings.Repeat("a", 64), 27612)
	p := mx.SignMeteredTransactionPoll([]byte(nodeToken), mx.TransactionPoll{NodeID: "route-qa", InstallationID: installID, BinarySHA: strings.Repeat("a", 64), At: time.Now().UnixNano(), Status: mx.TransactionStatus{Usage: &mx.MeteredCondition{}, Instance: strings.Repeat("b", 32), Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}})
	pollPath := "/api/v1/agent/metered-route-transactions/poll"
	call := func(h http.Handler, tlsEnabled bool, token string, payload any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(payload)
		r := httptest.NewRequest("POST", pollPath, bytes.NewReader(raw))
		if tlsEnabled {
			r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		}
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call(s.AgentHandler(), false, nodeToken, p); w.Code != 426 {
		t.Fatal("plaintext Agent path", w.Code)
	}
	if w := call(s.Handler(), true, nodeToken, p); w.Code != 404 {
		t.Fatal("local owner listener exposed Agent command transport", w.Code)
	}
	if w := call(s.AgentHandler(), true, "wrong", p); w.Code != 401 {
		t.Fatal("token bypass", w.Code)
	}
	if w := call(s.AgentHandler(), true, nodeToken, p); w.Code != 204 {
		t.Fatal("valid poll", w.Code, w.Body.String())
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	discovery := "/api/v1/workspace/metered-route-transactions"
	if w := browserRequest(t, s.Handler(), "GET", discovery, nil, nil, "", ""); w.Code != 401 {
		t.Fatal("anonymous discovery", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "GET", discovery, nil, cookie, "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":true`) || !strings.Contains(w.Body.String(), `"node_id":"route-qa"`) {
		t.Fatal("owner scope discovery", w.Code, w.Body.String())
	}
	base := "/api/v1/workspace/native-nodes/route-qa/metered-route-transactions"
	input := meteredRouteOwnerRequest(p.Status, 1, "stage", "", 1)
	if w := browserRequest(t, s.Handler(), "POST", base, input, nil, csrf, "https://"+browserHost); w.Code != 401 {
		t.Fatal("anonymous queue", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", base, input, cookie, "bad", "https://"+browserHost); w.Code != 403 {
		t.Fatal("CSRF", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", base, input, cookie, csrf, "https://foreign.example"); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", base, input, cookie, csrf, "https://"+browserHost); w.Code != 202 {
		t.Fatal("owner queue", w.Code, w.Body.String())
	}
	w := browserRequest(t, s.Handler(), "GET", base, nil, cookie, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var control business.MeteredRouteControlView
	if e := json.Unmarshal(w.Body.Bytes(), &control); e != nil || control.ServerTimeMillis == 0 || len(control.Review) != 1 || control.Review[0].DirectPort != 27612 || control.Controls.Stage {
		t.Fatal("invalid browser review or pending command enabled", e)
	}
	for _, secret := range []string{"privateKey", "config_base64", "payload_enc"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("owner view leaked", secret)
		}
	}
	if w := browserRequest(t, s.AgentHandler(), "GET", base, nil, cookie, "", ""); w.Code == 200 {
		t.Fatal("Agent got owner surface")
	}
	abandon := base + "/" + input.ID + "/abandon"
	if w := browserRequest(t, s.Handler(), "POST", abandon, map[string]bool{"confirm_execution_may_have_occurred": false}, cookie, csrf, "https://"+browserHost); w.Code != 422 {
		t.Fatal("ambiguous cancellation without confirmation", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", abandon, map[string]bool{"confirm_execution_may_have_occurred": true}, cookie, csrf, "https://"+browserHost); w.Code != 200 || strings.Contains(w.Body.String(), `"process_stopped":true`) {
		t.Fatal("false stop confirmation", w.Code, w.Body.String())
	}
	if _, e := s.business.CreateRepresentative(context.Background(), "route-rep", "Rep"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.business.ProvisionRepresentativeLogin(context.Background(), "route-rep", "route-representative", "route-password-123456"); e != nil {
		t.Fatal(e)
	}
	rep, repCSRF := browserLogin(t, s, "route-representative", "route-password-123456")
	if w := browserRequest(t, s.Handler(), "GET", discovery, nil, rep, "", ""); w.Code != 403 {
		t.Fatal("representative discovered owner executor scope", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "GET", base, nil, rep, "", ""); w.Code != 403 {
		t.Fatal("representative read command queue", w.Code)
	}
	if w := browserRequest(t, s.Handler(), "POST", base, input, rep, repCSRF, "https://"+browserHost); w.Code != 403 {
		t.Fatal("representative queued command", w.Code)
	}
	if e := s.business.EnableUsageLedger(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := s.EnableRemoteMeteredNode("route-qa"); e == nil {
		t.Fatal("mixed metered/transaction worker allowed")
	}
	state, e := s.business.MeteredRouteChannel(context.Background(), key, "route-qa")
	if e != nil || state.Commands[0].State != "abandoned_unknown" {
		t.Fatal(e)
	}
}

func TestMeteredRouteQueueDiscoveryCannotEnableOrClaimNode(t *testing.T) {
	s := enabledOwner(t)
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	path := "/api/v1/workspace/metered-route-transactions"
	w := browserRequest(t, s.Handler(), "GET", path, nil, cookie, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) || strings.Contains(w.Body.String(), `"node_id"`) {
		t.Fatal("disabled discovery", w.Code, w.Body.String())
	}
	w = browserRequest(t, s.Handler(), "POST", path, map[string]any{"enabled": true, "node_id": "usa-pro-01"}, cookie, csrf, "https://"+browserHost)
	if w.Code < 400 || s.meteredRouteNode != "" || s.meteredRouteInstall != "" {
		t.Fatal("browser enabled executor", w.Code)
	}
	if w = browserRequest(t, s.AgentHandler(), "GET", path, nil, cookie, "", ""); w.Code == 200 {
		t.Fatal("Agent exposed owner discovery")
	}
}
