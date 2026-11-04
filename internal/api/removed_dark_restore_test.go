package api

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemovedDarkRestoreHasNoOwnerOrAgentRoutes(t *testing.T) {
	f := newOwnershipAPI(t)
	paths := []string{"stage", "quote", "commit", "recent", "receipts/restore-request-0001", "receipts/restore-request-0001/history/0", "handoff", "handoff/readiness", "handoff/attach", "handoff/prepare", "handoff/promotion", "handoff/promotion/preview", "handoff/apply-acceptance", "handoff/apply-acceptance/preview", "handoff/quarantine", "handoff/quarantine/admission", "handoff/quarantine/admission/evidence", "handoff/quarantine/settlement", "handoff/quarantine/connection-check"}
	for _, path := range paths {
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			w := browserRequest(t, f.h, method, "/api/v1/workspace/dark-restore/"+path, map[string]any{}, f.owner, f.ownerCSRF, "https://"+browserHost)
			if w.Code != http.StatusNotFound {
				t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
			}
		}
	}
	for _, path := range []string{"darkrestore-runtime", "darkrestore-quarantine"} {
		r := httptest.NewRequest("POST", "https://lab.invalid/api/v1/agent/"+path, strings.NewReader("{}"))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		w := httptest.NewRecorder()
		f.s.AgentHandler().ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("agent %s: %d", path, w.Code)
		}
	}
	w := browserRequest(t, f.h, "GET", "/api/v1/workspace/me", nil, f.owner, f.ownerCSRF, "https://"+browserHost)
	var result struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("workspace: %d %s", w.Code, w.Body.String())
	}
	for k := range result.Capabilities {
		if strings.HasPrefix(k, "darkrestore_") {
			t.Fatalf("retired capability advertised: %s", k)
		}
	}
	if !result.Capabilities["client_ownership_v1"] {
		t.Fatal("native client ownership was removed")
	}
}
