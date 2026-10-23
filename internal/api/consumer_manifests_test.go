package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func consumerAPIApprovals(f *handoverAPI) []mx.ConsumerEndpointApproval {
	return []mx.ConsumerEndpointApproval{
		{NodeID: f.plan.TargetNode, InstallationID: f.plan.TargetInstallation, InboundID: 7612, Address: "11.0.0.2", Port: 28613},
		{NodeID: f.plan.TargetNode, InstallationID: f.plan.TargetInstallation, InboundID: 7612, Address: "11.0.0.2", Port: 28615},
	}
}

func newConsumerManifestAPI(t *testing.T) *handoverAPI {
	t.Helper()
	f := newHealthRoundAPI(t)
	if err := f.s.EnableLabSubscriptions(); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableConsumerEndpoints(consumerAPIApprovals(f)); err != nil {
		t.Fatal(err)
	}
	return f
}

func consumerAPIMutation(event, action string, revision uint64, port uint16) map[string]any {
	members := []map[string]any{}
	if action == "replace" {
		members = append(members, map[string]any{"inbound_id": 7612, "consumer_address": "11.0.0.2", "consumer_port": port})
	}
	return map[string]any{"event_id": event, "action": action, "expected_revision": revision, "members": members, "confirmed": true}
}

func TestConsumerManifestsStartupAndExactApprovalScope(t *testing.T) {
	f := newHealthRoundAPI(t)
	approvals := consumerAPIApprovals(f)
	if err := f.s.EnableConsumerEndpoints(approvals); err == nil || f.s.consumerManifestsEnabled {
		t.Fatal("consumer routes enabled without explicit N18")
	}
	path := handoverBase + "/" + f.plan.ID + "/consumer-manifest"
	w := browserRequest(t, f.s.BrowserHandler(), "GET", path, nil, f.cookie, "", "")
	labSubscriptionDecode(t, w, http.StatusOK, nil)
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("default-off consumer discovery missing")
	}
	labSubscriptionDecode(t, f.post(t, path, consumerAPIMutation(handoverTestID(5400), "replace", 0, 28613)), http.StatusConflict, nil)
	if err := f.s.EnableLabSubscriptions(); err != nil {
		t.Fatal(err)
	}
	for _, modify := range []func(*mx.ConsumerEndpointApproval){
		func(a *mx.ConsumerEndpointApproval) { a.NodeID = "not-approved" },
		func(a *mx.ConsumerEndpointApproval) { a.InstallationID = handoverTestID(999) },
		func(a *mx.ConsumerEndpointApproval) { a.Address = "127.0.0.1" },
		func(a *mx.ConsumerEndpointApproval) { a.InboundID = 0 },
	} {
		bad := append([]mx.ConsumerEndpointApproval(nil), approvals...)
		modify(&bad[0])
		if err := f.s.EnableConsumerEndpoints(bad); err == nil || f.s.consumerManifestsEnabled {
			t.Fatal("startup accepted unapproved or invalid endpoint authority")
		}
	}
	if err := f.s.EnableConsumerEndpoints(approvals); err != nil {
		t.Fatal(err)
	}
	approvals[0].Address = "12.0.0.1"
	if err := f.s.EnableConsumerEndpoints(consumerAPIApprovals(f)); err == nil {
		t.Fatal("startup switch changed twice")
	}
	var view business.ConsumerManifestView
	w = browserRequest(t, f.s.BrowserHandler(), "GET", path, nil, f.cookie, "", "")
	labSubscriptionDecode(t, w, http.StatusOK, &view)
	if view.State != "absent" || view.Revision != 0 || len(view.RequiredInbounds) != 1 || view.RequiredInbounds[0].OriginPort != 27613 || len(view.ApprovedEndpoints) != 2 || view.ApprovedEndpoints[0].Address != "11.0.0.2" {
		t.Fatal("startup authority was mutated or complete applied review was omitted", view)
	}
	labSubscriptionNoSecret(t, w, f.tokens...)
	w = browserRequest(t, f.s.BrowserHandler(), "GET", "/api/v1/workspace/metered-route-transactions", nil, f.cookie, "", "")
	if !strings.Contains(w.Body.String(), `"consumer_manifests_enabled":true`) {
		t.Fatal("consumer startup discovery missing")
	}
}

func TestConsumerManifestsOwnerAndStrictHTTPBoundaries(t *testing.T) {
	f := newConsumerManifestAPI(t)
	h := f.s.BrowserHandler()
	base := handoverBase + "/" + f.plan.ID
	path, roundPath := base+"/consumer-manifest", base+"/consumer-health-rounds"
	mutation := consumerAPIMutation(handoverTestID(5410), "replace", 0, 28613)
	round := map[string]any{"id": handoverTestID(5411), "consumer_manifest_id": handoverTestID(5412), "consumer_manifest_revision": 1, "confirmed": true}
	ctx := context.Background()
	if _, err := f.s.business.CreateRepresentative(ctx, "consumer-rep", "Consumer representative"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.ProvisionRepresentativeLogin(ctx, "consumer-rep", "consumer-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	rep, csrf := browserLogin(t, f.s, "consumer-rep", repPassword)
	for _, tc := range []struct {
		method, path string
		body         any
	}{{"GET", path, nil}, {"POST", path, mutation}, {"POST", roundPath, round}} {
		labSubscriptionDecode(t, browserRequest(t, h, tc.method, tc.path, tc.body, nil, "", ""), http.StatusUnauthorized, nil)
		labSubscriptionDecode(t, request(t, h, tc.method, tc.path, ownerToken, tc.body), http.StatusUnauthorized, nil)
		labSubscriptionDecode(t, browserRequest(t, h, tc.method, tc.path, tc.body, rep, csrf, "https://"+browserHost), http.StatusForbidden, nil)
		r := httptest.NewRequest(tc.method, "https://"+browserHost+tc.path, strings.NewReader(`{}`))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		r.AddCookie(f.cookie)
		w := httptest.NewRecorder()
		f.s.AgentHandler().ServeHTTP(w, r)
		labSubscriptionDecode(t, w, http.StatusNotFound, nil)
	}
	for _, target := range []string{path, roundPath} {
		for _, headers := range [][2]string{{"", "https://" + browserHost}, {"bad", "https://" + browserHost}, {f.csrf, "https://foreign.invalid"}} {
			labSubscriptionDecode(t, browserRequest(t, h, "POST", target, mutation, f.cookie, headers[0], headers[1]), http.StatusForbidden, nil)
		}
		for _, method := range []string{"PUT", "PATCH", "DELETE"} {
			labSubscriptionDecode(t, browserRequest(t, h, method, target, mutation, f.cookie, f.csrf, "https://"+browserHost), http.StatusMethodNotAllowed, nil)
		}
		labSubscriptionDecode(t, f.post(t, target+"?endpoint=11.0.0.3", mutation), http.StatusUnprocessableEntity, nil)
	}
	for _, field := range []string{"endpoint", "verifier_url", "node_id", "installation_id", "lease", "expires_unix_nano", "consumer_manifest_sha"} {
		bad := consumerAPIMutation(handoverTestID(5410), "replace", 0, 28613)
		bad[field] = "caller-controlled"
		labSubscriptionDecode(t, f.post(t, path, bad), http.StatusUnprocessableEntity, nil)
	}
	for _, members := range []any{nil, []map[string]any{{"inbound_id": 7612, "consumer_address": "127.0.0.1", "consumer_port": 28613}}, []map[string]any{{"inbound_id": 7612, "origin_port": 27613, "consumer_address": "11.0.0.2", "consumer_port": 28613}}} {
		bad := consumerAPIMutation(handoverTestID(5410), "replace", 0, 28613)
		bad["members"] = members
		labSubscriptionDecode(t, f.post(t, path, bad), http.StatusUnprocessableEntity, nil)
	}
	bad := consumerAPIMutation(handoverTestID(5410), "replace", 0, 28613)
	bad["confirmed"] = false
	labSubscriptionDecode(t, f.post(t, path, bad), http.StatusUnprocessableEntity, nil)
	bad = consumerAPIMutation(handoverTestID(5410), "replace", 0, 28614)
	labSubscriptionDecode(t, f.post(t, path, bad), http.StatusConflict, nil)
}

func TestConsumerManifestsBoundRoundSnapshotAndRevocation(t *testing.T) {
	f := newConsumerManifestAPI(t)
	ctx := context.Background()
	h := f.s.BrowserHandler()
	base := handoverBase + "/" + f.plan.ID
	path := base + "/consumer-manifest"
	mutation := consumerAPIMutation(handoverTestID(5420), "replace", 0, 28613)
	var manifest, replay business.ConsumerManifest
	labSubscriptionDecode(t, f.post(t, path, mutation), http.StatusCreated, &manifest)
	labSubscriptionDecode(t, f.post(t, path, mutation), http.StatusOK, &replay)
	if !manifest.Current || manifest.ID == manifest.EventID || replay.ID != manifest.ID || replay.Revision != 1 || !replay.Replayed || len(manifest.Members) != 1 || manifest.Members[0].OriginPort != 27613 {
		t.Fatal("immutable manifest identity, origin binding or retry changed")
	}
	labSubscriptionDecode(t, f.post(t, base+"/health-rounds", map[string]any{"id": handoverTestID(5421), "confirmed": true}), http.StatusConflict, nil)
	body := map[string]any{"id": handoverTestID(5422), "consumer_manifest_id": manifest.ID, "consumer_manifest_revision": manifest.Revision, "confirmed": true}
	var round, again business.MeteredHealthRound
	labSubscriptionDecode(t, f.post(t, base+"/consumer-health-rounds", body), http.StatusAccepted, &round)
	labSubscriptionDecode(t, f.post(t, base+"/consumer-health-rounds", body), http.StatusAccepted, &again)
	if round.ID != again.ID || round.Expires != again.Expires || round.ConsumerManifestID != manifest.ID || round.ConsumerManifestRevision != 1 || round.ConsumerManifestSHA != manifest.ConsumerManifestSHA || round.Members[0].Port != 27613 || round.Members[0].ConsumerAddress != "11.0.0.2" || round.Members[0].ConsumerPort != 28613 {
		t.Fatal("consumer round lost exact reviewed endpoint or original deadline")
	}
	q, found, err := f.s.business.PollMeteredHealth(ctx, f.key, []byte(f.tokens[1]), f.p[1].NodeID, f.p[1].InstallationID, roundEndpoint())
	if err != nil || !found || q.Version != mx.ConsumerProtocolHealthVersion || q.ConsumerManifestSHA != manifest.ConsumerManifestSHA || q.ConsumerPort != 28613 {
		t.Fatal("consumer round did not produce the exact signed v3 challenge", err)
	}
	// Synthetic signing exercises HTTP-to-Store authority; the independent
	// isolated real-core acceptance proves actual endpoint traversal.
	responseSHA := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"))
	proof := mx.ProtocolHealthReceipt{Version: mx.ConsumerProtocolHealthVersion, RequestSHA: handoverHash(q), ResponseSHA: hex.EncodeToString(responseSHA[:]), Completed: time.Now().UnixNano()}
	raw, _ := json.Marshal(proof)
	mac := hmac.New(sha256.New, []byte(f.tokens[1]))
	mac.Write([]byte("DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v3\x00"))
	mac.Write(raw)
	proof.Signature = hex.EncodeToString(mac.Sum(nil))
	if _, err = f.s.business.ConsumeMeteredHealthProof(ctx, f.key, []byte(f.tokens[1]), q.ID, roundEndpoint(), proof); err != nil {
		t.Fatal("consume consumer proof", err)
	}
	labSubscriptionDecode(t, f.post(t, labSubscriptionClientPath+"/issue", map[string]any{"event_id": handoverTestID(5423), "expected_generation": 0}), http.StatusOK, nil)
	_, consumer := labSubscriptionReveal(t, f, labSubscriptionClientPath, 1, f.cookie, f.csrf)
	snapshotBody := map[string]any{"event_id": handoverTestID(5424), "client_id": "handover-customer", "round_id": round.ID, "expected_revision": 0}
	labSubscriptionDecode(t, f.post(t, base+"/lab-subscription-snapshot", snapshotBody), http.StatusConflict, nil)
	snapshotBody["consumer_manifest_id"], snapshotBody["consumer_manifest_revision"] = manifest.ID, manifest.Revision
	var snapshot business.LabSubscriptionSnapshot
	labSubscriptionDecode(t, f.post(t, base+"/lab-subscription-snapshot", snapshotBody), http.StatusCreated, &snapshot)
	if snapshot.ConsumerManifestID != manifest.ID || snapshot.ConsumerManifestSHA != manifest.ConsumerManifestSHA || snapshot.ExpiresUnixNano > round.Expires || !snapshot.Current {
		t.Fatal("consumer snapshot proof binding missing")
	}
	w := request(t, h, "GET", consumer+"?format=raw", "", nil)
	labSubscriptionDecode(t, w, http.StatusOK, nil)
	if !strings.Contains(w.Body.String(), "@11.0.0.2:28613?") || strings.Contains(w.Body.String(), "@127.0.0.1:") {
		t.Fatal("consumer download used an origin shortcut")
	}
	var replacement business.ConsumerManifest
	labSubscriptionDecode(t, f.post(t, path, consumerAPIMutation(handoverTestID(5425), "replace", 1, 28615)), http.StatusCreated, &replacement)
	labSubscriptionDecode(t, request(t, h, "GET", consumer, "", nil), http.StatusNotFound, nil)
	labSubscriptionDecode(t, f.post(t, path, mutation), http.StatusOK, &replay)
	if replay.Current || replay.Revision != 1 || !replay.Replayed {
		t.Fatal("old manifest retry reactivated a superseded mapping")
	}
	labSubscriptionDecode(t, f.post(t, base+"/lab-subscription-snapshot", snapshotBody), http.StatusOK, &snapshot)
	if snapshot.Current {
		t.Fatal("old consumer snapshot survived mapping replacement")
	}
	labSubscriptionDecode(t, f.post(t, path, consumerAPIMutation(handoverTestID(5426), "revoke", 2, 0)), http.StatusCreated, &replacement)
	if replacement.State != "revoked" || replacement.Current || replacement.Revision != 3 {
		t.Fatal("manifest revoke did not advance the immutable head")
	}
	labSubscriptionDecode(t, f.post(t, base+"/consumer-health-rounds", map[string]any{"id": handoverTestID(5427), "consumer_manifest_id": manifest.ID, "consumer_manifest_revision": 1, "confirmed": true}), http.StatusConflict, nil)
	for _, node := range []string{f.plan.SourceNode, f.plan.TargetNode} {
		prior := f.s.meteredRouteNodes[node]
		f.s.meteredRouteNodes[node] = handoverTestID(999)
		labSubscriptionDecode(t, browserRequest(t, h, "GET", path, nil, f.cookie, "", ""), http.StatusConflict, nil)
		labSubscriptionDecode(t, f.post(t, path, mutation), http.StatusConflict, nil)
		f.s.meteredRouteNodes[node] = prior
	}
}
