package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/hostmetrics"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubmonitor"
)

func TestHUBSnapshotOwnerCookieNotBearerOrRepresentative(t *testing.T) {
	s := enabledOwner(t)
	path := "/api/v1/workspace/hub/metrics"
	h := s.BrowserHandler()
	get := func(cookie *http.Cookie, bearerToken string, method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "https://"+browserHost+path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if bearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+bearerToken)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	ownerCookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	if r := get(nil, "", http.MethodGet); r.Code != 401 {
		t.Fatalf("anon metrics HTTP=%d", r.Code)
	}
	if r := get(nil, ownerToken, http.MethodGet); r.Code != 401 {
		t.Fatal("bearer-only bypass of owner session")
	}
	if r := get(ownerCookie, "", http.MethodGet); r.Code != 503 {
		t.Fatal("missing sampler must not fabricate host info")
	}
	if r := get(ownerCookie, "", http.MethodPost); r.Code != 405 {
		t.Fatal("write endpoint unexpectedly available")
	}

	if _, err := s.business.CreateRepresentative(context.Background(), "hubview-rep", "Hub View Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "hubview-rep", "hubview-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	repCookie, _ := browserLogin(t, s, "hubview-rep", repPassword)
	if r := get(repCookie, "", http.MethodGet); r.Code != 403 {
		t.Fatal("representative can view host statistics")
	}

	at := time.Date(2026, 10, 17, 9, 20, 0, 0, time.UTC)
	rx := uint64(100)
	raw := hostmetrics.Raw{TotalCPU: 100, IdleCPU: 40, RAMTotal: 8 << 30, RAMUsed: 4 << 30,
		RXBytes: rx, TXBytes: 100, Interface: "eth0"}
	monitor := hubmonitor.New(func() (hostmetrics.Raw, error) { return raw, nil },
		func() (*uint64, error) { return nil, nil }, func() time.Time { return at })
	if err := s.EnableHubMonitor(monitor); err != nil {
		t.Fatal(err)
	}
	monitor.Sample()
	at = at.Add(2 * time.Second)
	raw.TotalCPU = 200
	raw.IdleCPU = 80
	raw.RXBytes += 500
	monitor.Sample()
	r := get(ownerCookie, "", http.MethodGet)
	if r.Code != 200 {
		t.Fatalf("owner metrics HTTP=%d %s", r.Code, r.Body.String())
	}
	if !strings.Contains(r.Body.String(), `"scope":"hub_host_physical_not_billable"`) {
		t.Fatal("host scope mislabeled")
	}
	if strings.Contains(r.Body.String(), "agent_token") || strings.Contains(r.Body.String(), "credit") || strings.Contains(r.Body.String(), "bearer") {
		t.Fatal("unexpected customer/secret/ledger fields")
	}
	var payload hubmonitor.Snapshot
	if err := json.Unmarshal(r.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CPUPercent == nil || payload.RXBitsPerSecond == nil || !payload.MetricsCurrent ||
		*payload.CPUPercent != 60 || *payload.RXBitsPerSecond != 2000 {
		t.Fatalf("real HUB sample missing: %+v", payload)
	}
	if r := get(repCookie, "", http.MethodGet); r.Code != 403 {
		t.Fatal("rep read live host after enable")
	}
	// Even a TLS Agent request cannot access owner-only host monitoring.
	agentReq := httptest.NewRequest("GET", "https://agent.example"+path, nil)
	agentReq.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	agentResponse := httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(agentResponse, agentReq)
	if agentResponse.Code != 404 {
		t.Fatalf("agent listener exposed private HUB metrics: %d", agentResponse.Code)
	}
}
