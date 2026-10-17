package api

import (
	"archive/zip"
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

	"github.com/darktunnelmika/dark-xray-pro/internal/nodebootstrap"
)

func TestNodeInstallerBundleRequiresOwnerAndContainsNoSecrets(t *testing.T) {
	s := enabledOwner(t)
	h := s.BrowserHandler()
	path := "/api/v1/workspace/nodes/installer-bundle"
	req := func(cookie *http.Cookie, auth string, method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://"+browserHost+path, nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	ownerCookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	if got := req(nil, "", http.MethodGet); got.Code != 401 {
		t.Fatalf("anonymous download: %d", got.Code)
	}
	if got := req(nil, ownerToken, http.MethodGet); got.Code != 401 {
		t.Fatalf("bearer bypass: %d", got.Code)
	}
	if got := req(ownerCookie, "", http.MethodPost); got.Code != 405 {
		t.Fatalf("POST to binary: %d", got.Code)
	}
	if got := req(ownerCookie, "", http.MethodGet); got.Code != 503 {
		t.Fatalf("missing release binary should fail closed: %d", got.Code)
	}

	f := filepath.Join(t.TempDir(), "dark-pro-agent")
	raw := []byte("distinct NEW QA PRO Agent binary")
	if err := os.WriteFile(f, raw, 0755); err != nil {
		t.Fatal(err)
	}
	b, err := nodebootstrap.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnableNodeInstaller(b); err != nil {
		t.Fatal(err)
	}
	s.SetNodeTransportScope("loopback_only")
	roster := httptest.NewRecorder()
	s.BrowserHandler().ServeHTTP(roster, func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "https://"+browserHost+"/api/v1/workspace/nodes", nil)
		r.AddCookie(ownerCookie)
		return r
	}())
	var state map[string]any
	if err := json.Unmarshal(roster.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state["installer_available"] != true || state["agent_transport_scope"] != "loopback_only" {
		t.Fatalf("false installation capability: %v", state)
	}
	if strings.Contains(roster.Body.String(), "agent_token") || strings.Contains(roster.Body.String(), "pair_code") {
		t.Fatal("Node roster leaked enrollment secrets")
	}
	w := req(ownerCookie, "", http.MethodGet)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/zip") {
		t.Fatalf("owner bundle unavailable: %d %v", w.Code, w.Header())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || len(z.File) != 3 {
		t.Fatalf("invalid owner bundle: %v", err)
	}
	for _, bad := range []string{ownerToken, ownerPassword, "agent_token", "__Host-darkpro_session"} {
		if bytes.Contains(w.Body.Bytes(), []byte(bad)) {
			t.Fatalf("download leaked confidential data: %s", bad)
		}
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "bundle-rep", "Bundle Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "bundle-rep", "bundle-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	repCookie, _ := browserLogin(t, s, "bundle-rep", repPassword)
	if got := req(repCookie, "", http.MethodGet); got.Code != 403 {
		t.Fatalf("rep downloaded Node binary: %d", got.Code)
	}

	agent := httptest.NewRequest("GET", "https://agent.test"+path, nil)
	agent.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	agentW := httptest.NewRecorder()
	s.AgentHandler().ServeHTTP(agentW, agent)
	if agentW.Code != 404 {
		t.Fatalf("Agent TLS listener exposed package endpoint: %d", agentW.Code)
	}
}
