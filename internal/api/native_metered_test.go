package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestDistributedMeteredOwnerOptInTLSRegistrationAndFailClosed(t *testing.T) {
	s := enabledOwner(t)
	ctx := context.Background()
	key := bytes.Repeat([]byte{7}, 32)
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	inv, err := s.registry.Issue("remote-metered-qa", "Fresh only")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.registry.Claim(inv.PairCode, "remote-metered-qa", installID, nodeToken); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableRemoteMeteredNode("unpaired"); err == nil {
		t.Fatal("unknown Node enabled remote metered delivery")
	}
	customer, err := s.business.CreateClient(ctx, business.NewClient{
		ClientID: "metered-qa-client", EventID: "metered-qa-client-first-001",
		RepresentativeID: "owner", Name: "Isolated dummy", DurationDays: 30, VolumeBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := business.InboundInput{ID: 987, Name: "Remote test", Transport: "tcp",
		RealityTarget: "www.example.org:443", ServerNames: []string{"www.example.org"},
		ShortIDs: []string{"aa11"},
	}
	if _, err = s.business.CreateInbound(ctx, key, input); err != nil {
		t.Fatal(err)
	}
	if err = s.business.BindInboundClients(ctx, 987, []string{customer.ID}, true); err != nil {
		t.Fatal(err)
	}
	if err = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 987,
		NodeID: "remote-metered-qa", InstallationID: installID, ListenIP: "127.0.0.1",
		DirectPort: 19606}); err != nil {
		t.Fatal(err)
	}
	// Isolated TLS Agent surface, never the owner browser.
	makeCall := func(method, path, token string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest(method, "https://test-agent"+path, bytes.NewReader(raw))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		w := httptest.NewRecorder()
		s.AgentHandler().ServeHTTP(w, request)
		return w
	}
	q := "?node_id=remote-metered-qa&installation_id=" + installID
	route := "/api/v1/agent/native-metered-runtime" + q
	if w := makeCall("GET", route, nodeToken, nil); w.Code != 503 {
		t.Fatalf("metered route enabled by default: %d", w.Code)
	}
	if err = s.EnableRemoteMeteredNode("remote-metered-qa"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", otherToken, ownerToken} {
		if w := makeCall("GET", route, token, nil); w.Code != 401 {
			t.Fatalf("unauthorized credential disclosed metered config: %d", w.Code)
		}
	}
	// The same path is not reachable through owner/browser HTTP even with
	// an otherwise valid paired Agent bearer. AgentHandler TLS-only.
	request := httptest.NewRequest("GET", route, nil)
	request.Header.Set("Authorization", "Bearer "+nodeToken)
	w := httptest.NewRecorder()
	s.BrowserHandler().ServeHTTP(w, request)
	if w.Code != 404 {
		t.Fatalf("browser exposed agent-only metering: %d", w.Code)
	}
	nonTLS := httptest.NewRequest("GET", route, nil)
	nonTLS.Header.Set("Authorization", "Bearer "+nodeToken)
	w = httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(w, nonTLS)
	if w.Code != 426 {
		t.Fatalf("plaintext exposed metered delivery: %d", w.Code)
	}

	w = makeCall("GET", route, nodeToken, nil)
	if w.Code != 200 {
		t.Fatalf("metered delivery refused: %d %s", w.Code, w.Body.String())
	}
	var got managedxray.Delivery
	if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.NodeID != "remote-metered-qa" ||
		got.ConfigBase64 == "" || !managedxray.ValidLeaseSignature(got.Lease, nodeToken) {
		t.Fatal("malformed signed metered delivery")
	}
	if _, _, stats, err := xrayconfig.UsageBootstrap(got.Candidate); err == nil {
		// The config bytes are transmitted as base64, not under Candidate.Config.
		t.Fatalf("candidate incorrectly trusted without matching decoded bytes: %s", stats)
	}
	// Treat this as a trust boundary: only the Agent decodes the exact
	// byte payload and validates its digest before preparing.
	raw, err := base64.StdEncoding.Strict().DecodeString(got.ConfigBase64)
	if err != nil {
		t.Fatal(err)
	}
	got.Candidate.Config = raw
	_, _, stats, err := xrayconfig.UsageBootstrap(got.Candidate)
	if err != nil || stats != managedxray.MeteredStatsEndpoint {
		t.Fatalf("new HUB derived an unsafe metering endpoint: %v %s", err, stats)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != got.Lease.ConfigSHA {
		t.Fatal("signed lease hashes differ from final metered bytes")
	}
	birth := uc.ProcessToken{BootID: "00000000-0000-0000-0000-000000000009", PID: 55667, StartTicks: 24691231}
	proof, err := managedxray.SignBirthReport(nodeToken, got.Lease, birth)
	if err != nil {
		t.Fatal(err)
	}
	bad := proof
	bad.ProcessStartTicks++
	if w := makeCall("POST", "/api/v1/agent/native-metered-register", nodeToken, bad); w.Code != 401 {
		t.Fatalf("tampered claimed process got epoch: %d", w.Code)
	}
	if w := makeCall("POST", "/api/v1/agent/native-metered-register", otherToken, proof); w.Code != 401 {
		t.Fatalf("wrong Agent registered epoch: %d", w.Code)
	}
	if w := makeCall("POST", "/api/v1/agent/native-metered-register", nodeToken,
		map[string]string{"node_id": "remote-metered-qa"}); w.Code != 422 {
		t.Fatalf("malformed trusted registration accepted: %d", w.Code)
	}
	accepted := makeCall("POST", "/api/v1/agent/native-metered-register", nodeToken, proof)
	if accepted.Code != 201 {
		t.Fatalf("valid trusted registration failed: %d %s", accepted.Code, accepted.Body.String())
	}
	var epoch uc.Epoch
	if err = json.Unmarshal(accepted.Body.Bytes(), &epoch); err != nil {
		t.Fatal(err)
	}
	if epoch.ConfigSHA != got.Lease.ConfigSHA || epoch.PID != birth.PID || epoch.KernelBootID != birth.BootID ||
		len(epoch.Bindings) != 1 || epoch.Bindings[0].ClientID != customer.ID {
		t.Fatalf("HUB bound unknown process/UUID to metered epoch: %+v", epoch)
	}
	retry := makeCall("POST", "/api/v1/agent/native-metered-register", nodeToken, proof)
	if retry.Code != 201 || retry.Body.String() != accepted.Body.String() {
		t.Fatalf("ambiguous receipt retry changed epoch: %d", retry.Code)
	}
	changed := proof
	changed.PID = 55668
	changed, err = managedxray.SignBirthReport(nodeToken, got.Lease, uc.ProcessToken{BootID: birth.BootID, PID: changed.PID, StartTicks: birth.StartTicks})
	if err != nil {
		t.Fatal(err)
	}
	if w := makeCall("POST", "/api/v1/agent/native-metered-register", nodeToken, changed); w.Code == 201 {
		t.Fatal("one delivered revision authorized a SECOND unsupervised birth")
	}
	readiness, code, _ := func() (NativeReadiness, int, string) {
		cookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
		response := browserRequest(t, s.Handler(), "GET", "/api/v1/workspace/native-inbounds-state", nil, cookie, "", "")
		var doc struct {
			Preflight NativeReadiness `json:"preflight"`
		}
		_ = json.Unmarshal(response.Body.Bytes(), &doc)
		return doc.Preflight, response.Code, response.Body.String()
	}()
	if code != 200 || readiness.DeploymentReady || readiness.CustomerDataPlaneVerified {
		t.Fatal("registered birth misrepresented successful customer transport")
	}
	// Short lease expires independently of future updates; no API request
	// can extend it without a new signed same-scope delivery.
	if got.Lease.Expires <= time.Now().UnixNano() {
		t.Fatal("lease unexpectedly expired during fast API tests")
	}
}
