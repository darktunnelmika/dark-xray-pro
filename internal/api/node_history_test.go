package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/nodehistory"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func TestOwnerHistoryIsRealAuthenticatedAndSeparatedFromBilling(t *testing.T) {
	s := enabledOwner(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "history.sqlite")
	hist, err := nodehistory.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer hist.Close()
	if err := s.EnableNodeHistory(hist); err != nil {
		t.Fatal(err)
	}
	ticket, err := s.registry.Issue("freshhist-01", "Fresh NEW PRO host")
	if err != nil {
		t.Fatal(err)
	}
	install := strings.Repeat("b", 32)
	token := strings.Repeat("d", 64)
	if _, err = s.registry.Claim(ticket.PairCode, "freshhist-01", install, token); err != nil {
		t.Fatal(err)
	}
	ck, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	h := s.BrowserHandler()
	ownerHistory := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "https://"+browserHost+path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	pathAPI := "/api/v1/workspace/nodes/freshhist-01/history?window=1h"
	if r := ownerHistory(pathAPI, nil); r.Code != 401 {
		t.Fatalf("anonymous history: %d", r.Code)
	}
	if r := ownerHistory(pathAPI, nil); r.Code != 401 {
		t.Fatalf("anonymous history cannot be bypassed: %d", r.Code)
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "history-rep", "History Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "history-rep", "history-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	repCookie, _ := browserLogin(t, s, "history-rep", repPassword)
	if r := ownerHistory(pathAPI, repCookie); r.Code != 403 {
		t.Fatalf("representative read fleet history: %d", r.Code)
	}

	if r := ownerHistory("/api/v1/workspace/nodes/missing/history?window=1h", ck); r.Code != 404 {
		t.Fatalf("unknown node: %d", r.Code)
	}
	if r := ownerHistory("/api/v1/workspace/nodes/freshhist-01/history?window=all", ck); r.Code != 422 {
		t.Fatalf("unbounded history: %d", r.Code)
	}

	// Signed Agent binds exact new Node installation and sends actual host
	// measurements, not a browser mutation. No subscriber or usage keys appear.
	diskUsed, diskTotal := uint64(20<<30), uint64(80<<30)
	load := float64(0.95)
	tcp := uint64(12)
	uptime := uint64(3600)
	sample := telemetry.Sample{
		NodeID: "freshhist-01", InstallationID: install, Name: "Agent cannot overwrite", BootID: strings.Repeat("a", 32),
		Sequence: 1, Interface: "eth0", CPUCores: 4, CPUPercent: 26.25, RAMTotal: 8 << 30, RAMUsed: 4 << 30,
		RXBytes: 100, TXBytes: 200, DiskUsedBytes: &diskUsed, DiskTotalBytes: &diskTotal, Load1: &load, Load5: &load, Load15: &load, UptimeSeconds: &uptime, TCPInUse: &tcp,
	}
	send := func(samp telemetry.Sample, auth string) int {
		raw, err := json.Marshal(samp)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "https://agent.example/api/v1/agent/heartbeat", bytes.NewReader(raw))
		req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.AgentHandler().ServeHTTP(w, req)
		return w.Code
	}
	if got := send(sample, "wrong"); got != 401 {
		t.Fatalf("wrong token accepted: %d", got)
	}
	if r := ownerHistory(pathAPI, ck); r.Code != 200 || !strings.Contains(r.Body.String(), `"points":[]`) {
		t.Fatalf("unauthorized attempt wrote data: %d %s", r.Code, r.Body.String())
	}
	if got := send(sample, token); got != 204 {
		t.Fatalf("authenticated first sample failed %d", got)
	}
	sample.Sequence = 2
	sample.CPUPercent = 47.50
	sample.RXBytes += 500000
	if got := send(sample, token); got != 204 {
		t.Fatalf("authenticated counter update failed %d", got)
	}
	r := ownerHistory(pathAPI, ck)
	if r.Code != 200 {
		t.Fatalf("history: %d %s", r.Code, r.Body.String())
	}
	var payload struct {
		Scope  string              `json:"scope"`
		Points []nodehistory.Point `json:"points"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Scope != "host_interface_not_billable" || len(payload.Points) != 1 ||
		payload.Points[0].CPUPercent != 26.25 || payload.Points[0].DiskTotalBytes == nil ||
		*payload.Points[0].DiskTotalBytes != float64(diskTotal) || payload.Points[0].TCPInUse == nil ||
		*payload.Points[0].TCPInUse != float64(tcp) {
		t.Fatalf("historical point was forged or missing: %+v", payload)
	}
	if err = hist.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := nodehistory.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	points, err := reopened.Query(context.Background(), "freshhist-01", install, "1h")
	if err != nil || len(points) != 1 {
		t.Fatalf("restart lost verified metrics: %+v %v", points, err)
	}
	another, err := reopened.Query(context.Background(), "freshhist-01", strings.Repeat("c", 32), "1h")
	if err != nil || len(another) != 0 {
		t.Fatalf("changed install saw previous metrics: %v %v", another, err)
	}
	if _, err = s.business.Representative(context.Background(), "owner"); err != nil {
		t.Fatal(err)
	}
	// Metrics are not inserted into the business/usage/credit ledger.
	if rows, err := s.business.ListEvents(context.Background(), "", 10, 0); err != nil || len(rows) != 0 {
		t.Fatalf("history touched business ledger: %v %v", rows, err)
	}
}
