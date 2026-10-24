//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

// Real compiled browser, authentication, HTTP handlers and SQLite. Signed empty
// Agent polls keep the reserved channel observable, but no runtime commands or
// core processes are created by this preparation-only acceptance. The separate
// isolated-network core test proves Stage/Apply and actual accounted traffic.
func TestRealBrowserInboundPlanPromotion(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_PROMOTION_BROWSER") != "1" {
		t.Skip("explicit real-browser plan promotion acceptance only")
	}
	webRoot, script := os.Getenv("DARK_PRO_TEST_WEB_DIST"), os.Getenv("DARK_PRO_TEST_PROMOTION_BROWSER_SCRIPT")
	if !filepath.IsAbs(webRoot) || !filepath.IsAbs(script) {
		t.Fatal("absolute compiled UI and browser script required")
	}
	for _, path := range []string{filepath.Join(webRoot, "index.html"), script} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("browser acceptance input missing", path)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	f := newPromotionAPIFixture(t, true)
	const otherID = uint64(8124)
	otherClient, err := f.s.business.CreateClient(ctx, business.NewClient{ClientID: "promotion-unrelated-client", EventID: "promotion-browser-other-client", RepresentativeID: "owner", Name: "Preserved unrelated customer", DurationDays: 30, VolumeBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.business.CreateInbound(ctx, f.key, business.InboundInput{ID: otherID, Name: "Unrelated saved native profile", Transport: "tcp", RealityTarget: "untouched.example.org:443", ServerNames: []string{"untouched.example.org"}, ShortIDs: []string{"aa22"}}); err != nil {
		t.Fatal(err)
	}
	if err = f.s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: otherID, NodeID: "unrelated-node", InstallationID: strings.Repeat("d", 32), ListenIP: "127.0.0.1", DirectPort: 28124}); err != nil {
		t.Fatal(err)
	}
	if err = f.s.business.BindInboundClients(ctx, otherID, []string{otherClient.ID}, true); err != nil {
		t.Fatal(err)
	}
	beforeClients, err := f.s.business.ListClients(ctx, "", 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := f.s.business.ListEvents(ctx, "", 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeRepresentatives, err := f.s.business.ListRepresentatives(ctx, 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeOther, err := f.s.business.NativeInbound(ctx, f.key, otherID)
	if err != nil {
		t.Fatal(err)
	}
	beforeConfig, err := f.s.business.BuildNodeConfig(ctx, f.key, "unrelated-node", strings.Repeat("d", 32))
	if err != nil {
		t.Fatal(err)
	}

	owner, agent := f.s.Handler(), f.s.AgentHandler()
	requests := map[string]int{}
	var requestMu sync.Mutex
	mux := http.NewServeMux()
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMu.Lock()
		requests[r.Method+" "+r.URL.Path]++
		requestMu.Unlock()
		owner.ServeHTTP(w, r)
	}))
	mux.Handle("/", webui.Directory(webRoot))
	ui := httptest.NewServer(mux)
	defer ui.Close()
	pollContext, stopPolling := context.WithCancel(ctx)
	pollDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pollContext.Done():
				pollDone <- nil
				return
			case <-ticker.C:
			}
			poll := mx.SignMeteredTransactionPoll([]byte(nodeToken), mx.TransactionPoll{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID, BinarySHA: f.node.BinarySHA, At: time.Now().UnixNano(), Status: f.status})
			raw, e := json.Marshal(poll)
			if e != nil {
				pollDone <- e
				return
			}
			r := httptest.NewRequest("POST", "/api/v1/agent/metered-route-transactions/poll", bytes.NewReader(raw)).WithContext(pollContext)
			r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
			r.Header.Set("Authorization", "Bearer "+nodeToken)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			agent.ServeHTTP(w, r)
			if w.Code != 204 {
				if pollContext.Err() != nil {
					pollDone <- nil
				} else {
					pollDone <- fmt.Errorf("signed empty poll status %d", w.Code)
				}
				return
			}
		}
	}()
	defer stopPolling()
	private := t.TempDir()
	resultPath, controlPath := filepath.Join(private, "promotion-result.json"), filepath.Join(private, "browser-control.json")
	control, err := json.Marshal(map[string]any{"username": ownerUsername, "password": ownerPassword, "result_path": resultPath, "inbound_id": 8123})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(controlPath, control, 0600); err != nil {
		t.Fatal(err)
	}
	browser := exec.CommandContext(ctx, "node", script)
	browser.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+ui.URL, "DARK_PRO_QA_CONTROL="+controlPath)
	output, err := browser.CombinedOutput()
	stopPolling()
	if pollErr := <-pollDone; pollErr != nil {
		t.Fatal(pollErr)
	}
	if err != nil {
		t.Fatalf("real promotion browser failed: %v\n%s", err, output)
	}
	var result struct {
		Pass      bool   `json:"pass"`
		InboundID uint64 `json:"inbound_id"`
		EventID   string `json:"event_id"`
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil || json.Unmarshal(raw, &result) != nil || !result.Pass || result.InboundID != 8123 || result.EventID == "" {
		t.Fatal("browser promotion result missing", err, string(output))
	}
	state, err := f.s.business.NativeInboundPlanPromotionState(ctx, f.key, 8123)
	if err != nil || !state.Promoted || state.Receipt == nil || state.Receipt.EventID != result.EventID || state.Receipt.RuntimeChanged || state.Receipt.SubscriptionPublished || !reflect.DeepEqual(state.Receipt.ClientIDs, f.input.ClientIDs) {
		t.Fatal("browser did not persist reviewed promotion", err)
	}
	in, err := f.s.business.NativeInbound(ctx, f.key, 8123)
	if err != nil || !in.Enabled || in.Revision != 2 || in.BoundClients != 2 || in.PublicKey != f.plan.Profile.PublicKey {
		t.Fatal("browser promotion changed identity or incomplete atomic write", err)
	}
	plan, err := f.s.business.NativeInboundPlan(ctx, f.key, 8123)
	if err != nil || plan.Editable || plan.EditBlocker != "plan_promoted" || plan.PlanRevision != f.plan.PlanRevision || !reflect.DeepEqual(plan.Nodes, f.plan.Nodes) {
		t.Fatal("browser promotion lost saved plan", err)
	}
	channel, err := f.s.business.MeteredRouteChannel(ctx, f.key, f.node.NodeID)
	if err != nil || len(channel.Commands) != 0 || channel.Status.Observed.ProcessReady {
		t.Fatal("browser preparation crossed execution boundary", err)
	}
	afterClients, err := f.s.business.ListClients(ctx, "", 200, 0)
	if err != nil || !reflect.DeepEqual(beforeClients, afterClients) {
		t.Fatal("browser promotion changed clients", err)
	}
	afterEvents, err := f.s.business.ListEvents(ctx, "", 200, 0)
	if err != nil || !reflect.DeepEqual(beforeEvents, afterEvents) {
		t.Fatal("browser promotion changed financial history", err)
	}
	afterRepresentatives, err := f.s.business.ListRepresentatives(ctx, 200, 0)
	if err != nil || !reflect.DeepEqual(beforeRepresentatives, afterRepresentatives) {
		t.Fatal("browser promotion changed credit", err)
	}
	afterOther, err := f.s.business.NativeInbound(ctx, f.key, otherID)
	if err != nil || !reflect.DeepEqual(beforeOther, afterOther) {
		t.Fatal("browser promotion changed sibling profile", err)
	}
	afterConfig, err := f.s.business.BuildNodeConfig(ctx, f.key, "unrelated-node", strings.Repeat("d", 32))
	if err != nil || !reflect.DeepEqual(beforeConfig.Candidate, afterConfig.Candidate) {
		t.Fatal("browser promotion changed unrelated runtime", err)
	}
	requestMu.Lock()
	defer requestMu.Unlock()
	if requests["POST "+f.base()] != 1 || requests["POST "+f.base()+"/preview"] < 1 {
		t.Fatal("browser did not perform one reviewed promotion", requests)
	}
	for request, count := range requests {
		if strings.HasPrefix(request, "POST ") && request != "POST /api/v1/session/login" && request != "POST "+f.base() && request != "POST "+f.base()+"/preview" {
			t.Fatal("browser made unrelated mutation", request, count)
		}
	}
	t.Log("PLAN_PROMOTION_BROWSER_PASS: authenticated compiled UI; exact atomic preparation; preserved profile and customers; no runtime command")
}
