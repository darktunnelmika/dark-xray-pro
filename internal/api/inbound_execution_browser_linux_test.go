//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

// This is an actual compiled-browser login and subscription lifecycle against
// a real active native process, TLS Agent API and disposable SQLite database.
// The browser script neither mocks responses nor directly mutates the API.
func TestManagedInboundRealBrowserSubscriptionLifecycle(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_MANAGED_BROWSER") != "1" {
		t.Skip("explicit managed browser acceptance only")
	}
	webRoot, script := os.Getenv("DARK_PRO_TEST_WEB_DIST"), os.Getenv("DARK_PRO_TEST_MANAGED_BROWSER_SCRIPT")
	if !filepath.IsAbs(webRoot) || !filepath.IsAbs(script) {
		t.Fatal("absolute compiled UI and browser script paths required")
	}
	for _, path := range []string{filepath.Join(webRoot, "index.html"), script} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("missing browser fixture input", path, err)
		}
	}
	core := managedTestCore(t)
	s, plan, cookie, csrf := creationPlanAPIFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableInboundExecution(InboundExecutionConfig{Directory: filepath.Join(t.TempDir(), "hub"), Native: core}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.inboundExecution.local.Close)
	client, err := s.business.CreateClient(ctx, business.NewClient{EventID: "managed-browser-client-0001", ClientID: "managed-browser-client", RepresentativeID: "owner", Name: "Managed browser client", DurationDays: 30, VolumeBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, input, output any, status int) {
		t.Helper()
		w := browserRequest(t, s.Handler(), method, path, input, cookie, csrf, "https://"+browserHost)
		if w.Code != status {
			t.Fatalf("fixture %s %s failed: HTTP%d %s", method, path, w.Code, w.Body.String())
		}
		if output != nil && json.Unmarshal(w.Body.Bytes(), output) != nil {
			t.Fatal("invalid fixture API response")
		}
	}
	port := managedTestPort(t)
	plan.EventID = "managed-browser-plan-0001"
	plan.Profile = business.InboundInput{ID: 501, Name: "Managed browser inbound", Spec: &inboundspec.Spec{Version: 1, Protocol: "http", Network: "tcp", Security: "none"}}
	plan.Nodes = []business.NativeInboundPlanNode{{NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "127.0.0.1", DirectPort: port, DirectHosts: []string{"127.0.0.1"}, TunnelHosts: []string{}}}
	request("POST", "/api/v1/workspace/native-inbound-plans", plan, nil, 201)
	var state ix.State
	request("GET", "/api/v1/workspace/native-inbounds/501/execution", nil, &state, 200)
	input := ix.CommandInput{EventID: "managed-browser-activate-0001", Action: "activate", ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: []string{client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Disposable real browser acceptance"}
	var preview ix.Preview
	request("POST", "/api/v1/workspace/native-inbounds/501/execution/preview", input, &preview, 200)
	if !preview.Valid {
		t.Fatal("fixture execution preview rejected", preview.Blockers)
	}
	input.ReviewSHA = preview.ReviewSHA
	request("POST", "/api/v1/workspace/native-inbounds/501/execution/commands", input, nil, 202)

	agent := httptest.NewUnstartedServer(s.AgentHandler())
	agent.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	agent.StartTLS()
	defer agent.Close()
	worker, err := inboundruntime.Open(inboundruntime.Config{Directory: filepath.Join(t.TempDir(), "node"), NodeID: "de", InstallationID: strings.Repeat("b", 32), Key: []byte(strings.Repeat("3", 64)), Native: core})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	exchange, err := inboundruntime.NewHTTPExchange(agent.Client(), agent.URL, strings.Repeat("3", 64), "de", strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Step(ctx, exchange); err != nil {
		t.Fatal("fixture activation", err)
	}
	bundle, found, err := s.business.InboundExecutionBundle(ctx, s.inboundKey, "de", strings.Repeat("b", 32))
	if err != nil || !found || len(bundle.Principals) != 1 {
		t.Fatal("fixture accounting identity missing", err)
	}
	target, stopEcho := managedEcho(t)
	defer stopEcho()
	payload := bytes.Repeat([]byte("compiled-browser-real-payload-"), 256)
	if err = managedHTTP(port, strings.Split(bundle.Principals[0].Counter, ">>>")[1], client.UUID, target, payload); err != nil {
		t.Fatal("fixture real payload", err)
	}
	if err = worker.Step(ctx, exchange); err != nil {
		t.Fatal(err)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); worker.Run(workerCtx, exchange) }()
	defer func() { stopWorker(); <-workerDone }()

	owner := s.Handler()
	var mu sync.Mutex
	observed := map[string]int{}
	mux := http.NewServeMux()
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/v1/inbound-subscriptions/") {
			path = "/api/v1/inbound-subscriptions/[redacted]"
		}
		mu.Lock()
		observed[r.Method+" "+path]++
		mu.Unlock()
		owner.ServeHTTP(w, r)
	}))
	mux.Handle("/", webui.Directory(webRoot))
	ui := httptest.NewServer(mux)
	defer ui.Close()
	private := t.TempDir()
	resultPath, controlPath := filepath.Join(private, "result.json"), filepath.Join(private, "control.json")
	control, _ := json.Marshal(map[string]any{"username": ownerUsername, "password": ownerPassword, "result_path": resultPath, "client_id": client.ID, "inbound_id": 501})
	if err = os.WriteFile(controlPath, control, 0600); err != nil {
		t.Fatal(err)
	}
	browser := exec.CommandContext(ctx, "node", script)
	browser.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+ui.URL, "DARK_PRO_QA_CONTROL="+controlPath)
	output, err := browser.CombinedOutput()
	if err != nil {
		mu.Lock()
		requests, _ := json.Marshal(observed)
		mu.Unlock()
		t.Fatalf("managed compiled browser failed: %v; requests=%s\n%s", err, requests, strings.ReplaceAll(string(output), ownerPassword, "[fixture credential]"))
	}
	if !bytes.Contains(output, []byte("MANAGED_UI_SUBSCRIPTION_VERIFIED\n")) {
		t.Fatal("missing positive compiled browser acceptance")
	}
	var result struct {
		Verified           bool   `json:"verified"`
		ClientID           string `json:"client_id"`
		InboundID          uint64 `json:"inbound_id"`
		IssuedGeneration   uint64 `json:"issued_generation"`
		RevokedGeneration  uint64 `json:"revoked_generation"`
		DownloadBytes      int64  `json:"download_bytes"`
		BearerBeforeStatus int    `json:"bearer_before_status"`
		BearerAfterStatus  int    `json:"bearer_after_status"`
		TokenStored        bool   `json:"token_stored"`
		UnexpectedRequests int    `json:"unexpected_requests"`
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil || json.Unmarshal(raw, &result) != nil || !result.Verified || result.ClientID != client.ID || result.InboundID != 501 || result.IssuedGeneration != 1 || result.RevokedGeneration != 2 || result.DownloadBytes <= 0 || result.BearerBeforeStatus != 200 || result.BearerAfterStatus != 404 || result.TokenStored || result.UnexpectedRequests != 0 {
		t.Fatal("compiled browser acceptance incomplete", err)
	}
	usage, err := s.business.ClientUsage(ctx, client.ID)
	if err != nil || usage.Total != int64(len(payload)*2) {
		t.Fatal("subscription UI altered metered traffic", usage.Total, err)
	}
	t.Log("MANAGED_UI_SUBSCRIPTION_VERIFIED")
}
