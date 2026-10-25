package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundsecurity"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
)

func inboundProbeAgentFixture(t *testing.T) (*Server, *atomic.Int32) {
	t.Helper()
	s, registry, _, _ := newLab(t)
	for _, node := range []struct{ id, installation, token string }{
		{"probe-a", installID, nodeToken}, {"probe-b", strings.Repeat("b", 32), otherToken},
	} {
		invite, err := registry.Issue(node.id, node.id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Claim(invite.PairCode, node.id, node.installation, node.token); err != nil {
			t.Fatal(err)
		}
	}
	var hubDials atomic.Int32
	if err := s.EnableInboundSecurity(inboundsecurity.Config{NodeProbe: s.ProbeSelectedNode,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			hubDials.Add(1)
			return nil, errors.New("HUB must not execute a selected-node probe")
		}}); err != nil {
		t.Fatal(err)
	}
	return s, &hubDials
}

func probeAgentRequest(t *testing.T, s *Server, operation, token string, body any, version uint16) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://probe.example/api/v1/agent/inbound-security/"+operation, bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if version == 0 {
		request.TLS = nil
	} else {
		request.TLS = &tls.ConnectionState{Version: version}
	}
	response := httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(response, request)
	return response
}

func waitInboundProbeJob(t *testing.T, s *Server) *inboundProbeJob {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		s.inboundProbeJobs.Lock()
		for _, job := range s.inboundProbeJobs.jobs {
			s.inboundProbeJobs.Unlock()
			return job
		}
		s.inboundProbeJobs.Unlock()
		select {
		case <-deadline.C:
			t.Fatal("selected-node probe never queued")
		case <-tick.C:
		}
	}
}

type inboundProbeOutcome struct {
	result inboundsecurity.ProbeResult
	err    error
}

func TestInboundProbeAgentAuthenticatesSelectedInstallationAndResult(t *testing.T) {
	s, hubDials := inboundProbeAgentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request := inboundsecurity.ProbeRequest{Scope: "node", NodeID: "probe-a", Target: "8.8.8.8:443", ServerName: "target.example"}
	done := make(chan inboundProbeOutcome, 1)
	go func() {
		result, err := s.inboundSecurity.Probe(ctx, request)
		done <- inboundProbeOutcome{result, err}
	}()
	job := waitInboundProbeJob(t, s)
	poll := map[string]any{"node_id": "probe-a", "installation_id": installID}
	for _, version := range []uint16{0, tls.VersionTLS12} {
		if response := probeAgentRequest(t, s, "poll", nodeToken, poll, version); response.Code != http.StatusUpgradeRequired {
			t.Fatal("probe job crossed an unverified TLS transport", response.Code)
		}
	}
	for _, token := range []string{"", ownerToken, otherToken} {
		if response := probeAgentRequest(t, s, "poll", token, poll, tls.VersionTLS13); response.Code != http.StatusUnauthorized {
			t.Fatal("wrong credential received a probe job", response.Code)
		}
	}
	otherPoll := map[string]any{"node_id": "probe-b", "installation_id": strings.Repeat("b", 32)}
	if response := probeAgentRequest(t, s, "poll", otherToken, otherPoll, tls.VersionTLS13); response.Code != http.StatusNoContent {
		t.Fatal("different authenticated Node received the job", response.Code)
	}
	response := probeAgentRequest(t, s, "poll", nodeToken, poll, tls.VersionTLS13)
	var delivered struct {
		ID      string                       `json:"id"`
		Request inboundsecurity.ProbeRequest `json:"request"`
		Expires int64                        `json:"expires_unix"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &delivered) != nil || delivered.ID != job.ID || delivered.Request != request ||
		delivered.Expires <= time.Now().Unix() || strings.Contains(response.Body.String(), nodeToken) || strings.Contains(response.Body.String(), installID) {
		t.Fatal("invalid selected-installation job envelope", response.Code)
	}
	if second := probeAgentRequest(t, s, "poll", nodeToken, poll, tls.VersionTLS13); second.Code != http.StatusNoContent {
		t.Fatal("same job was delivered to a second worker", second.Code)
	}
	result := inboundsecurity.ProbeResult{Scope: "hub", NodeID: "probe-a", Target: request.Target, ServerName: request.ServerName}
	body := map[string]any{"node_id": "probe-a", "installation_id": installID, "id": delivered.ID, "result": result}
	if response := probeAgentRequest(t, s, "result", nodeToken, body, tls.VersionTLS13); response.Code != http.StatusUnprocessableEntity {
		t.Fatal("HUB result relabeled as selected Node", response.Code)
	}
	result.Scope = "node"
	result.ServerName = "wrong.example"
	body["result"] = result
	if response := probeAgentRequest(t, s, "result", nodeToken, body, tls.VersionTLS13); response.Code != http.StatusUnprocessableEntity {
		t.Fatal("result changed explicitly requested SNI", response.Code)
	}
	result.ServerName = request.ServerName
	result.Error, result.Reason = "tcp_connect_failed", "tcp_connect_failed"
	body["result"] = result
	if response := probeAgentRequest(t, s, "result", nodeToken, body, tls.VersionTLS13); response.Code != http.StatusNoContent {
		t.Fatal("authenticated selected Node could not return its result", response.Code)
	}
	select {
	case outcome := <-done:
		if outcome.err != nil || outcome.result.Scope != "node" || outcome.result.NodeID != "probe-a" || outcome.result.Error != "tcp_connect_failed" {
			t.Fatal("selected Node result was lost or changed", outcome.err)
		}
	case <-ctx.Done():
		t.Fatal("selected-node request never completed")
	}
	if response := probeAgentRequest(t, s, "result", nodeToken, body, tls.VersionTLS13); response.Code != http.StatusConflict {
		t.Fatal("probe result replay accepted", response.Code)
	}
	if hubDials.Load() != 0 {
		t.Fatal("selected-node probe used the HUB network")
	}
}

func TestInboundProbeAgentRejectsExpiredCancelledAndReplacedJobs(t *testing.T) {
	s, hubDials := inboundProbeAgentFixture(t)
	request := inboundsecurity.ProbeRequest{Scope: "node", NodeID: "probe-a", Target: "8.8.8.8:443"}
	result := inboundsecurity.ProbeResult{Scope: "node", NodeID: "probe-a", Target: request.Target}
	job := &inboundProbeJob{ID: strings.Repeat("c", 32), Request: request, Expires: time.Now().Add(-time.Second).Unix(), installation: installID,
		delivered: true, result: make(chan inboundsecurity.ProbeResult, 1)}
	s.inboundProbeJobs.jobs = map[string]*inboundProbeJob{job.ID: job}
	body := map[string]any{"node_id": "probe-a", "installation_id": installID, "id": job.ID, "result": result}
	if response := probeAgentRequest(t, s, "result", nodeToken, body, tls.VersionTLS13); response.Code != http.StatusConflict {
		t.Fatal("expired result accepted", response.Code)
	}
	job.Expires = time.Now().Add(time.Second).Unix()
	job.installation = strings.Repeat("d", 32)
	if response := probeAgentRequest(t, s, "result", nodeToken, body, tls.VersionTLS13); response.Code != http.StatusConflict {
		t.Fatal("result for an obsolete installation accepted", response.Code)
	}
	delete(s.inboundProbeJobs.jobs, job.ID)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.ProbeSelectedNode(ctx, "probe-a", request); done <- err }()
	job = waitInboundProbeJob(t, s)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled node job returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled node job did not stop")
	}
	s.inboundProbeJobs.Lock()
	remaining := len(s.inboundProbeJobs.jobs)
	s.inboundProbeJobs.Unlock()
	if remaining != 0 {
		t.Fatal("cancelled request left a queued scan")
	}
	body["id"] = job.ID
	if response := probeAgentRequest(t, s, "result", nodeToken, body, tls.VersionTLS13); response.Code != http.StatusConflict {
		t.Fatal("cancelled result accepted", response.Code)
	}
	if hubDials.Load() != 0 {
		t.Fatal("expired or cancelled job caused HUB fallback")
	}
}

func TestInboundProbeRechecksInstallationAfterResultArrival(t *testing.T) {
	s, _ := inboundProbeAgentFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request := inboundsecurity.ProbeRequest{Scope: "node", NodeID: "probe-a", Target: "8.8.8.8:443"}
	done := make(chan error, 1)
	go func() { _, err := s.ProbeSelectedNode(ctx, "probe-a", request); done <- err }()
	job := waitInboundProbeJob(t, s)
	replacement, err := noderegistry.Open(filepath.Join(t.TempDir(), "replacement", "registry.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	invite, err := replacement.Issue("probe-a", "Replacement installation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replacement.Claim(invite.PairCode, "probe-a", strings.Repeat("e", 32), otherToken); err != nil {
		t.Fatal(err)
	}
	// Publication of the job synchronizes the old registry read; sending its
	// result synchronizes the final identity check after this replacement.
	s.registry = replacement
	job.result <- inboundsecurity.ProbeResult{Scope: "node", NodeID: "probe-a", Target: request.Target}
	select {
	case err := <-done:
		if err == nil || err.Error() != "selected_node_identity_changed" {
			t.Fatal("result survived installation replacement", err)
		}
	case <-ctx.Done():
		t.Fatal("replaced installation check did not complete")
	}
}

func TestInboundProbeAgentRejectsAmbiguousJSON(t *testing.T) {
	s, _ := inboundProbeAgentFixture(t)
	raw := `{"node_id":"probe-b","node_id":"probe-a","installation_id":"` + installID + `"}`
	request := httptest.NewRequest(http.MethodPost, "https://probe.example/api/v1/agent/inbound-security/poll", strings.NewReader(raw))
	request.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	request.Header.Set("Authorization", "Bearer "+nodeToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatal("duplicate Agent identity field accepted", response.Code)
	}
}
