package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/configstage"
	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
)

func stageAgentRequest(t *testing.T, s *Server, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if payload != nil {
		var err error
		raw, err = json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	r.Header.Set("Authorization", "Bearer "+newToken)
	if payload != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(w, r)
	return w
}

func checkedReplacement(t *testing.T, s *Server) replacement.Operation {
	t.Helper()
	h := s.Handler()
	created := request(t, h, "POST", "/api/v1/admin/replacements", ownerToken, testPlan())
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var op replacement.Operation
	if err := json.Unmarshal(created.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	checks, err := replacement.Expect(op.Preview)
	if err != nil {
		t.Fatal(err)
	}
	for i := range checks {
		checks[i].Reachable = true
	}
	reported := request(t, h, "POST", "/api/v1/agent/replacement-report", newToken,
		replacement.Evidence{OperationID: op.ID, ManifestSHA256: op.Preview.ManifestSHA256, NewNodeID: op.NewNodeID, InstallationID: op.InstallationID, Probes: checks})
	if reported.Code != 200 {
		t.Fatalf("TCP proof: %d", reported.Code)
	}
	if err := json.Unmarshal(reported.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	return op
}

func TestCancelledReplacementFencesPrivateStageAndFreshOperation(t *testing.T) {
	s, store, op, profiles := setupStage(t)
	h := s.Handler()
	candidate := map[string]any{"profiles": profiles, "trusted_xray_sha256": expectedXraySHA}
	path := "/api/v1/admin/replacements/" + op.ID + "/candidate"
	staged := request(t, h, "POST", path, ownerToken, candidate)
	if staged.Code != 201 {
		t.Fatalf("stage: %d", staged.Code)
	}
	var meta configstage.Metadata
	if err := json.Unmarshal(staged.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	poll := "/api/v1/agent/config-stage?node_id=new-de&installation_id=" + newInstallation
	if w := stageAgentRequest(t, s, "GET", poll, nil); w.Code != 200 {
		t.Fatalf("initial delivery: %d", w.Code)
	}
	current, err := s.replacements.Get(op.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelled := request(t, h, "POST", "/api/v1/admin/replacements/"+op.ID+"/cancel", ownerToken, map[string]any{"expected_revision": current.Revision})
	if cancelled.Code != 200 {
		t.Fatalf("cancel: %d", cancelled.Code)
	}
	if w := stageAgentRequest(t, s, "GET", poll, nil); w.Code != 204 {
		t.Fatalf("cancelled secret redelivered: %d", w.Code)
	}
	proof := configstage.Proof{OperationID: op.ID, NodeID: op.NewNodeID, InstallationID: op.InstallationID, ManifestSHA256: meta.ManifestSHA256, ConfigSHA256: meta.ConfigSHA256, TrustedXraySHA256: meta.TrustedXraySHA256, SyntaxValid: true}
	if w := stageAgentRequest(t, s, "POST", "/api/v1/agent/config-stage-report", proof); w.Code != 409 {
		t.Fatalf("late proof accepted: %d", w.Code)
	}
	if w := request(t, h, "POST", path, ownerToken, candidate); w.Code != 409 {
		t.Fatalf("cancelled stage recreated: %d", w.Code)
	}
	retained, err := store.Get(op.ID)
	if err != nil || retained.Status != configstage.Pending {
		t.Fatal("stale stage history erased or marked verified", err)
	}
	fresh := checkedReplacement(t, s)
	if fresh.ID == op.ID {
		t.Fatal("new operation reused identity")
	}
	freshPath := "/api/v1/admin/replacements/" + fresh.ID + "/candidate"
	if w := request(t, h, "POST", freshPath, ownerToken, candidate); w.Code != 201 {
		t.Fatalf("fresh stage blocked by cancelled history: %d %s", w.Code, w.Body.String())
	}
	delivery := stageAgentRequest(t, s, "GET", poll, nil)
	if delivery.Code != 200 {
		t.Fatalf("fresh delivery: %d %s", delivery.Code, delivery.Body.String())
	}
	var got configstage.Delivery
	if err := json.Unmarshal(delivery.Body.Bytes(), &got); err != nil || got.Metadata.OperationID != fresh.ID {
		t.Fatal("wrong private candidate delivered", err)
	}
}

func TestCancelledReplacementCannotPrepareOrSelectRetainedLABVersion(t *testing.T) {
	s, versions, meta := labRevisionServer(t)
	h := s.Handler()
	prepare := "/api/v1/admin/lab-revisions/" + meta.OperationID + "/prepare"
	created := request(t, h, "POST", prepare, ownerToken, nil)
	if created.Code != 201 {
		t.Fatalf("prepare: %d", created.Code)
	}
	var version struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	op, err := s.replacements.Get(meta.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "POST", "/api/v1/admin/replacements/"+op.ID+"/cancel", ownerToken, map[string]any{"expected_revision": op.Revision}); w.Code != 200 {
		t.Fatalf("cancel: %d", w.Code)
	}
	if w := request(t, h, "POST", prepare, ownerToken, nil); w.Code != 409 {
		t.Fatalf("cancelled LAB prepare: %d", w.Code)
	}
	generation := versions.Snapshot().Generation
	if w := request(t, h, "POST", "/api/v1/admin/lab-revisions/select", ownerToken, map[string]any{"version_id": version.ID, "expected_generation": generation}); w.Code != 409 {
		t.Fatalf("cancelled LAB selection: %d", w.Code)
	}
	if snap := versions.Snapshot(); snap.SelectedVersion != "" || snap.Generation != generation || snap.ActualXrayProcessChanged {
		t.Fatal("cancelled version selected")
	}
}

func TestConcurrentCancellationFencesLateStageReceipt(t *testing.T) {
	s, store, op, profiles := setupStage(t)
	h := s.Handler()
	staged := request(t, h, "POST", "/api/v1/admin/replacements/"+op.ID+"/candidate", ownerToken,
		map[string]any{"profiles": profiles, "trusted_xray_sha256": expectedXraySHA})
	if staged.Code != 201 {
		t.Fatalf("stage: %d", staged.Code)
	}
	var meta configstage.Metadata
	if err := json.Unmarshal(staged.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	current, err := s.replacements.Get(op.ID)
	if err != nil {
		t.Fatal(err)
	}
	proof := configstage.Proof{OperationID: op.ID, NodeID: op.NewNodeID, InstallationID: op.InstallationID,
		ManifestSHA256: meta.ManifestSHA256, ConfigSHA256: meta.ConfigSHA256,
		TrustedXraySHA256: meta.TrustedXraySHA256, SyntaxValid: true}
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		codes <- stageAgentRequest(t, s, "POST", "/api/v1/agent/config-stage-report", proof).Code
	}()
	go func() {
		defer wg.Done()
		<-start
		codes <- request(t, h, "POST", "/api/v1/admin/replacements/"+op.ID+"/cancel", ownerToken, map[string]any{"expected_revision": current.Revision}).Code
	}()
	close(start)
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200]+counts[409] != 2 || (counts[200] != 1 && counts[200] != 2) {
		t.Fatalf("unexpected cancel/report outcomes: %v", counts)
	}
	cancelled, err := s.replacements.Get(op.ID)
	if err != nil || cancelled.Phase != replacement.Cancelled {
		t.Fatal("cancel not terminal", err)
	}
	recorded, err := store.Get(op.ID)
	if err != nil || recorded.CanActivate {
		t.Fatal("unsafe stage", err)
	}
	if w := stageAgentRequest(t, s, "GET", "/api/v1/agent/config-stage?node_id=new-de&installation_id="+newInstallation, nil); w.Code != 204 {
		t.Fatalf("post-cancel config leaked: %d", w.Code)
	}
	if w := stageAgentRequest(t, s, "POST", "/api/v1/agent/config-stage-report", proof); w.Code != 409 {
		t.Fatalf("post-cancel receipt accepted: %d", w.Code)
	}
}
