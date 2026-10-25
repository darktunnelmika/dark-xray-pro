//go:build linux

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

// A real browser signs in and authors the complete plan through the compiled UI
// and actual owner API. All records, credentials and Nodes are disposable test
// fixtures. Rendering is offline: no Agent, Xray process or customer connection
// is started, and the existing fixture's runtime assignment must remain intact.
func TestRealBrowserInboundCreationPlan(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_CREATION_BROWSER") != "1" {
		t.Skip("explicit real-browser Inbound creation acceptance only")
	}
	webRoot, script := os.Getenv("DARK_PRO_TEST_WEB_DIST"), os.Getenv("DARK_PRO_TEST_CREATION_BROWSER_SCRIPT")
	if !filepath.IsAbs(webRoot) || !filepath.IsAbs(script) {
		t.Fatal("absolute compiled UI and browser acceptance script paths required")
	}
	for _, path := range []string{filepath.Join(webRoot, "index.html"), script} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("compiled UI and browser acceptance script must exist", path, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0xc7}, 32)
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	for i, node := range []struct{ id, name string }{{"de", "Germany"}, {"fr", "France"}, {"nl", "Netherlands"}} {
		ticket, err := s.registry.Issue(node.id, node.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.registry.Claim(ticket.PairCode, node.id, strings.Repeat(string(rune('a'+i)), 32), strings.Repeat(string(rune('2'+i)), 64)); err != nil {
			t.Fatal(err)
		}
	}

	// Preserve a nonempty, unrelated client/target snapshot rather than merely
	// checking that empty tables stay empty. This profile is never executed.
	const existingID = uint64(70001)
	const existingClientID = "creation-unrelated-client"
	if _, err := s.business.CreateClient(ctx, business.NewClient{EventID: "creation-existing-client-0001", ClientID: existingClientID,
		RepresentativeID: "owner", Name: "Untouched fixture client", DurationDays: 30, VolumeBytes: 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.CreateInbound(ctx, key, business.InboundInput{ID: existingID, Name: "Untouched existing profile", Transport: "tcp",
		RealityTarget: "existing.example.org:443", ServerNames: []string{"existing.example.org"}, ShortIDs: []string{"77aa"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.business.BindInboundClients(ctx, existingID, []string{existingClientID}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: existingID, NodeID: "de", InstallationID: strings.Repeat("a", 32),
		ListenIP: "198.51.100.10", DirectPort: 20443}); err != nil {
		t.Fatal(err)
	}
	beforeTargets, err := s.business.ListInboundTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeClients, err := s.business.ListClients(ctx, "", 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := s.business.ListEvents(ctx, "", 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeRepresentatives, err := s.business.ListRepresentatives(ctx, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeProfiles, err := s.business.ListInbounds(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	beforeConfig, err := s.business.BuildNodeConfig(ctx, key, "de", strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}

	// Observe real requests without replacing their responses or bypassing auth.
	owner := s.Handler()
	requests := make(map[string]int)
	var requestsMu sync.Mutex
	mux := http.NewServeMux()
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requests[r.Method+" "+r.URL.Path]++
		requestsMu.Unlock()
		owner.ServeHTTP(w, r)
	}))
	mux.Handle("/", webui.Directory(webRoot))
	ui := httptest.NewServer(mux)
	defer ui.Close()
	private := t.TempDir()
	resultPath := filepath.Join(private, "creation-result.json")
	controlPath := filepath.Join(private, "browser-control.json")
	control, err := json.Marshal(map[string]string{"username": ownerUsername, "password": ownerPassword, "result_path": resultPath})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(controlPath, control, 0600); err != nil {
		t.Fatal(err)
	}
	browser := exec.CommandContext(ctx, "node", script)
	browser.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+ui.URL, "DARK_PRO_QA_CONTROL="+controlPath)
	output, err := browser.CombinedOutput()
	if err != nil {
		requestsMu.Lock()
		observed, _ := json.Marshal(requests)
		requestsMu.Unlock()
		t.Fatalf("real browser creation failed: %v; observed API requests: %s\n%s", err, observed, strings.ReplaceAll(string(output), ownerPassword, "[fixture password]"))
	}
	if !bytes.Contains(output, []byte("NATIVE_UI_CREATION_PLAN_VERIFIED\n")) {
		t.Fatal("browser completed without verifying persisted creation and edit")
	}
	if !bytes.Contains(output, []byte("NATIVE_UI_NETWORK_ENDPOINTS_VERIFIED\n")) {
		t.Fatal("browser omitted XHTTP and public endpoint acceptance")
	}
	var proof struct {
		InboundID        uint64 `json:"inbound_id"`
		CreatedPublicKey string `json:"created_public_key"`
		CreatedEventID   string `json:"created_event_id"`
		UpdatedEventID   string `json:"updated_event_id"`
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil || json.Unmarshal(raw, &proof) != nil || proof.InboundID == 0 || proof.InboundID == existingID || proof.CreatedPublicKey == "" ||
		len(proof.CreatedEventID) < 16 || len(proof.UpdatedEventID) < 16 || proof.CreatedEventID == proof.UpdatedEventID {
		t.Fatal("browser proof is absent or incomplete", err)
	}
	plan, err := s.business.NativeInboundPlan(ctx, key, proof.InboundID)
	if err != nil {
		t.Fatal("UI did not persist the full encrypted plan", err)
	}
	if plan.PlanRevision != 3 || plan.InboundRevision != 3 || plan.Profile.Revision != 3 || plan.Status != "SAVED_DISABLED" || !plan.Editable ||
		plan.RuntimeApply || plan.Profile.Enabled || plan.Profile.Archived || plan.Profile.BoundClients != 0 || plan.Profile.PublicKey != proof.CreatedPublicKey ||
		plan.LastEventID != proof.UpdatedEventID || plan.Reason != "Real browser network and endpoints" {
		t.Fatal("UI edit lost disabled state, identity, revision or event semantics")
	}
	wantProfile := business.InboundInput{ID: proof.InboundID, Name: "Real browser creation edited", Transport: "xhttp", Security: "reality",
		RealityTarget: "example.org:443", ServerNames: []string{"example.org"}, ShortIDs: []string{"aabb", "ccdd", "eeff"},
		Spec: &inboundspec.Spec{Version: 1, Protocol: "vless", Network: "xhttp", Security: "reality",
			Settings: map[string]any{"fallbacks": []any{}, "sniffing": map[string]any{"enabled": false, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true,
				"domainsExcluded": []string{"domain:example.net", "full:keep.example.org"}, "ipsExcluded": []string{"192.0.2.0/24"}}},
			Stream: map[string]any{"xhttpSettings": map[string]any{"path": "/xhttp", "host": "cdn.example.org", "mode": "packet-up", "xPaddingBytes": "200-400"}},
			Reality: map[string]any{"target": "example.org:443", "serverNames": []string{"example.org"}, "shortIds": []string{"aabb", "ccdd", "eeff"},
				"xver": 2, "maxTimediff": 120000, "minClientVer": "1.8.0", "maxClientVer": "25.9.11"},
			Client: map[string]any{"fingerprint": "firefox", "spiderX": "/news", "publicKey": proof.CreatedPublicKey}}}
	actualProfileJSON, actualErr := json.Marshal(plan.Profile.InboundInput)
	wantProfileJSON, wantErr := json.Marshal(wantProfile)
	if actualErr != nil || wantErr != nil || !bytes.Equal(actualProfileJSON, wantProfileJSON) {
		t.Fatal("stored profile differs from the browser's complete edit")
	}
	if plan.Profile.TLS != nil || plan.Profile.Options != nil || len(inboundspec.SecretPaths(*plan.Profile.Spec)) != 0 ||
		!reflect.DeepEqual(plan.Profile.SpecSecretPaths, []string{"reality.privateKey", "settings.decryption"}) {
		t.Fatal("Spec persistence lost its write-only contract or mixed legacy fields")
	}
	wantNodes := []business.NativeInboundPlanNode{
		{NodeID: "de", InstallationID: strings.Repeat("a", 32), ListenIP: "198.51.100.10",
			DirectPort: 10443, DirectHosts: []string{"2001:db8::10"}, TunnelPort: 1185, TunnelHosts: []string{"tunnel.example.org"},
			DirectEndpoints: []business.NativeInboundEndpoint{{Address: "2001:db8::10", Port: 443, Label: "Public IPv6", Enabled: true, SNI: "example.org", Fingerprint: "safari", SpiderX: "/endpoint"},
				{Address: "shared.example.org", Enabled: false}},
			SwapEnabled: true, SwapSourceNodeID: "nl", SwapSourceInstallationID: strings.Repeat("c", 32), SwapSourceListenIP: "198.51.100.30",
			SwapSourcePort: 16801, SwapEntryAddress: "entry.example.org", SwapEntryPort: 444, SwapDestinationPort: 18443},
		{NodeID: "fr", InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20", DirectPort: 11443,
			DirectHosts: []string{"fr.example.org", "fr-edge.example.org"}, TunnelPort: 1285, TunnelHosts: []string{"fr-tunnel.example.org"}},
	}
	if !reflect.DeepEqual(plan.Nodes, wantNodes) {
		t.Fatal("stored Node matrix lost an independent Direct, Tunnel or relay mapping")
	}
	index, err := s.business.ListNativeInboundPlans(ctx, key)
	if err != nil || len(index) != 1 || index[0].InboundID != proof.InboundID || index[0].PlanRevision != 3 {
		t.Fatal("saved plan is not discoverable exactly once", err)
	}
	profiles, err := s.business.ListInbounds(ctx, key)
	if err != nil || len(profiles) != len(beforeProfiles)+1 {
		t.Fatal("UI created duplicate profiles or removed an existing profile", err)
	}
	savedProfileJSON, err := json.Marshal(plan.Profile)
	if err != nil {
		t.Fatal("saved public profile did not serialize")
	}
	for _, profile := range profiles {
		if profile.ID == existingID && !reflect.DeepEqual(profile, beforeProfiles[0]) {
			t.Fatal("native profile differs from the saved plan or existing fixture")
		}
		if profile.ID == proof.InboundID {
			// The encrypted plan and public profile use distinct JSON decode
			// paths. Compare every serialized field, allowing only the Go
			// numeric representation (json.Number versus float64) to differ.
			listedJSON, err := json.Marshal(profile)
			if err != nil || !bytes.Equal(listedJSON, savedProfileJSON) {
				t.Fatal("native public profile differs from the complete saved plan")
			}
		}
	}
	targets, err := s.business.ListInboundTargets(ctx)
	if err != nil || !reflect.DeepEqual(targets, beforeTargets) {
		t.Fatal("creation changed operational Node targets", err)
	}
	clients, err := s.business.ListClients(ctx, "", 200, 0)
	if err != nil || !reflect.DeepEqual(clients, beforeClients) {
		t.Fatal("creation changed customer identities or quotas", err)
	}
	events, err := s.business.ListEvents(ctx, "", 200, 0)
	if err != nil || !reflect.DeepEqual(events, beforeEvents) {
		t.Fatal("creation changed financial events", err)
	}
	representatives, err := s.business.ListRepresentatives(ctx, 200, 0)
	if err != nil || !reflect.DeepEqual(representatives, beforeRepresentatives) {
		t.Fatal("creation changed representative credit or accounts", err)
	}
	bindings, err := s.business.InboundClientIDs(ctx, existingID)
	if err != nil || !reflect.DeepEqual(bindings, []string{existingClientID}) {
		t.Fatal("creation changed existing client bindings", err)
	}
	bindings, err = s.business.InboundClientIDs(ctx, proof.InboundID)
	if err != nil || len(bindings) != 0 {
		t.Fatal("creation bound a client to the paused setup", err)
	}
	afterConfig, err := s.business.BuildNodeConfig(ctx, key, "de", strings.Repeat("a", 32))
	if err != nil || afterConfig.Generation != beforeConfig.Generation || afterConfig.Sequence != beforeConfig.Sequence ||
		afterConfig.ConfigSHA != beforeConfig.ConfigSHA || afterConfig.ManifestSHA != beforeConfig.ManifestSHA ||
		afterConfig.UserCount != beforeConfig.UserCount || !bytes.Equal(afterConfig.Candidate.Config, beforeConfig.Candidate.Config) {
		t.Fatal("creation invalidated or replaced the existing offline runtime candidate", err)
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	planPath := "/api/v1/workspace/native-inbound-plans"
	wantMutations := map[string]int{"POST /api/v1/session/login": 1, "POST " + planPath + "/preview": 4, "POST " + planPath: 1,
		"PUT " + planPath + "/" + strconv.FormatUint(proof.InboundID, 10): 2}
	for request, count := range requests {
		if strings.HasPrefix(request, "GET ") || strings.HasPrefix(request, "HEAD ") {
			continue
		}
		if wantMutations[request] != count {
			t.Fatal("browser made an unexpected mutation or duplicate request", request, count)
		}
	}
	for request, count := range wantMutations {
		if requests[request] != count {
			t.Fatal("browser did not use the real authenticated creation API", request, requests[request])
		}
	}
	if requests["GET "+planPath+"/"+strconv.FormatUint(proof.InboundID, 10)] == 0 {
		t.Fatal("browser did not reopen the persisted plan through the real API")
	}
	t.Log("real UI login, TCP/gRPC/XHTTP setup edits, public IPv6 endpoints, persisted readback, and unchanged runtime/customer snapshots verified")
}
