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

func routeAPIFixture(t *testing.T, binary string, port uint16) (*Server, []byte) {
	t.Helper()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0x41}, 32)
	ctx := context.Background()
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
	routeAPIDraft(t, s, key, 0, port)
	if e = s.EnableRouteTransactions("route-qa", installID, binary); e != nil {
		t.Fatal(e)
	}
	return s, key
}
func routeAPIDraft(t *testing.T, s *Server, key []byte, revision uint64, port uint16) {
	t.Helper()
	_, e := s.business.SaveNativeRouteDraft(context.Background(), key, business.NativeRouteDraftInput{EventID: fmt.Sprintf("route-api-draft-%04d", revision), InboundID: 7612, NodeID: "route-qa", InstallationID: installID, ExpectedInboundRevision: 1, ExpectedDraftRevision: revision, DirectPort: port, Reason: "Loopback only owner review"})
	if e != nil {
		t.Fatal(e)
	}
}
func routeOwnerRequest(status mx.TransactionStatus, id int, action, stage string, revision uint64) business.RouteCommandRequest {
	in := business.RouteCommandRequest{ID: fmt.Sprintf("%032x", id), Action: action, StageID: stage, RuntimeInstance: status.Instance, Expected: mx.Condition(status.Observed)}
	if action == "stage" {
		in.Drafts = []business.RouteDraftRef{{InboundID: 7612, InboundRevision: 1, DraftRevision: revision}}
	}
	return in
}
func TestRouteQueueAPIAuthenticatesOwnerAndAgentWithoutPrivateLeak(t *testing.T) {
	s, key := routeAPIFixture(t, strings.Repeat("a", 64), 27612)
	p := mx.SignTransactionPoll([]byte(nodeToken), mx.TransactionPoll{NodeID: "route-qa", InstallationID: installID, BinarySHA: strings.Repeat("a", 64), At: time.Now().UnixNano(), Status: mx.TransactionStatus{Instance: strings.Repeat("b", 32), Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}})
	pollPath := "/api/v1/agent/route-transactions/poll"
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
	base := "/api/v1/workspace/native-nodes/route-qa/route-transactions"
	input := routeOwnerRequest(p.Status, 1, "stage", "", 1)
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
	state, e := s.business.RouteChannel(context.Background(), key, "route-qa")
	if e != nil || state.Commands[0].State != "abandoned_unknown" {
		t.Fatal(e)
	}
}
