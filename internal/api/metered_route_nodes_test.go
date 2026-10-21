package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func TestMeteredRouteNodesKeepIndependentAuthorityAndQueues(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x41}, 32)
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	nodes := []business.MeteredRouteNode{}
	tokens := []string{strings.Repeat("1", 64), strings.Repeat("2", 64)}
	for i, id := range []string{"node-a", "node-b"} {
		install := strings.Repeat(fmt.Sprint(i+1), 32)
		ticket, err := s.registry.Issue(id, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.registry.Claim(ticket.PairCode, id, install, tokens[i]); err != nil {
			t.Fatal(err)
		}
		inbound := uint64(8101 + i)
		port := uint16(28101 + i)
		if _, err = s.business.CreateInbound(ctx, key, business.InboundInput{ID: inbound, Name: id, Transport: "tcp", RealityTarget: "qa.invalid:443", ServerNames: []string{"qa.example.org"}, ShortIDs: []string{"aa11"}}); err != nil {
			t.Fatal(err)
		}
		if err = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: inbound, NodeID: id, InstallationID: install, ListenIP: "127.0.0.1", DirectPort: port}); err != nil {
			t.Fatal(err)
		}
		client, err := s.business.CreateClient(ctx, business.NewClient{EventID: "multi-node-client-event-" + id, ClientID: "multi-node-client-" + id, RepresentativeID: "owner", Name: id, DurationDays: 30, VolumeBytes: 1 << 20})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.business.BindInboundClients(ctx, inbound, []string{client.ID}, true); err != nil {
			t.Fatal(err)
		}
		if _, err = s.business.SaveNativeRouteDraft(ctx, key, business.NativeRouteDraftInput{EventID: "multi-node-draft-" + id, InboundID: inbound, NodeID: id, InstallationID: install, ExpectedInboundRevision: 1, ExpectedDraftRevision: 0, DirectPort: port, Reason: "Independent loopback channel"}); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, business.MeteredRouteNode{NodeID: id, InstallationID: install, BinarySHA: strings.Repeat("a", 64)})
	}
	if err := s.EnableMeteredRouteNodes(nodes); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableMeteredRouteNodes(nodes); err == nil {
		t.Fatal("live allowlist changed")
	}
	h := s.Handler()
	agent := s.AgentHandler()
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	discovery := browserRequest(t, h, "GET", "/api/v1/workspace/metered-route-transactions", nil, cookie, "", "")
	var scope struct {
		Enabled bool                        `json:"enabled"`
		Nodes   []business.MeteredRouteNode `json:"nodes"`
	}
	if discovery.Code != 200 || json.Unmarshal(discovery.Body.Bytes(), &scope) != nil || !scope.Enabled || len(scope.Nodes) != 2 || scope.Nodes[0].NodeID != "node-a" {
		t.Fatal("discovery", discovery.Code, discovery.Body.String())
	}
	call := func(token, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		agent.ServeHTTP(w, r)
		return w
	}
	polls := []mx.TransactionPoll{}
	for i, node := range nodes {
		poll := mx.SignMeteredTransactionPoll([]byte(tokens[i]), mx.TransactionPoll{NodeID: node.NodeID, InstallationID: node.InstallationID, BinarySHA: node.BinarySHA, At: time.Now().UnixNano(), Status: mx.TransactionStatus{Usage: &mx.MeteredCondition{}, Instance: strings.Repeat(fmt.Sprint(i+3), 32), Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}})
		if w := call(tokens[i], "/api/v1/agent/metered-route-transactions/poll", poll); w.Code != 204 {
			t.Fatalf("poll %s: %d %s", node.NodeID, w.Code, w.Body.String())
		}
		polls = append(polls, poll)
		input := business.MeteredRouteCommandRequest{ID: fmt.Sprintf("%032x", i+1), Action: "stage", RuntimeInstance: poll.Status.Instance, Expected: mx.Condition(poll.Status.Observed), Usage: poll.Status.Usage, Drafts: []business.RouteDraftRef{{InboundID: uint64(8101 + i), InboundRevision: 1, DraftRevision: 1}}}
		path := "/api/v1/workspace/native-nodes/" + node.NodeID + "/metered-route-transactions"
		if w := browserRequest(t, h, "POST", path, input, cookie, csrf, "https://"+browserHost); w.Code != 202 {
			t.Fatalf("queue %s: %d %s", node.NodeID, w.Code, w.Body.String())
		}
	}
	for i, node := range nodes {
		poll := polls[i]
		poll.At = time.Now().UnixNano()
		poll = mx.SignMeteredTransactionPoll([]byte(tokens[i]), poll)
		w := call(tokens[i], "/api/v1/agent/metered-route-transactions/poll", poll)
		var delivery mx.TransactionDelivery
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &delivery) != nil || delivery.Command.NodeID != node.NodeID || delivery.Command.InstallationID != node.InstallationID || delivery.Command.Serial != 1 || delivery.Command.ID != fmt.Sprintf("%032x", i+1) {
			t.Fatalf("cross-node delivery %s: %d %s", node.NodeID, w.Code, w.Body.String())
		}
	}
	if w := call(tokens[0], "/api/v1/agent/metered-route-transactions/poll", polls[1]); w.Code != 401 {
		t.Fatalf("cross-token poll accepted: %d", w.Code)
	}
	if w := call(tokens[0], "/api/v1/agent/metered-route-transactions/receipt", mx.TransactionReceipt{NodeID: nodes[1].NodeID, InstallationID: nodes[1].InstallationID}); w.Code != 401 {
		t.Fatalf("cross-token receipt accepted: %d", w.Code)
	}
	foreign := "/api/v1/workspace/native-nodes/node-b/metered-route-transactions/" + fmt.Sprintf("%032x", 1) + "/abandon"
	if w := browserRequest(t, h, "POST", foreign, map[string]bool{"confirm_execution_may_have_occurred": true}, cookie, csrf, "https://"+browserHost); w.Code < 400 {
		t.Fatalf("cross-node abandon: %d", w.Code)
	}
	for _, node := range nodes {
		view, err := s.business.MeteredRouteChannel(ctx, key, node.NodeID)
		if err != nil || len(view.Commands) != 1 || view.Commands[0].State != "delivered" {
			t.Fatal("other channel changed", node.NodeID, err)
		}
	}
}
