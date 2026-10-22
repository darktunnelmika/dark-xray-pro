package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMeteredHealthOwnerAndAgentBoundaries(t *testing.T) {
	f := newHandoverAPI(t)
	e := mx.ProtocolHealthEndpoint{Address: "11.0.0.1:443", ServerName: "verifier.example.com", CertificateSHA: strings.Repeat("a", 64)}
	if err := f.s.EnableMeteredHealth(e); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableMeteredHealth(e); err == nil {
		t.Fatal("startup config mutated")
	}
	path := handoverBase + "/" + handoverTestID(31) + "/health"
	body := map[string]any{"id": handoverTestID(240), "inbound_id": 7612, "confirm_probe": true}
	h := f.s.BrowserHandler()
	if w := browserRequest(t, h, "POST", path, body, nil, "", ""); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	if w := browserRequest(t, h, "POST", path, body, f.cookie, "", "https://"+browserHost); w.Code != 403 {
		t.Fatal("missing csrf", w.Code)
	}
	if w := browserRequest(t, h, "POST", path, body, f.cookie, f.csrf, "https://evil.example"); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	// No source stop/target Apply and no system identity: a valid browser
	// cannot manufacture execution rights or use a financial customer.
	if w := browserRequest(t, h, "POST", path, body, f.cookie, f.csrf, "https://"+browserHost); w.Code == 202 {
		t.Fatal("premature probe queued")
	}
	body["endpoint"] = "https://arbitrary.example"
	if w := browserRequest(t, h, "POST", path, body, f.cookie, f.csrf, "https://"+browserHost); w.Code != 422 {
		t.Fatal("browser selected verifier", w.Code)
	}
	for _, path := range []string{"poll", "receipt"} {
		url := "/api/v1/agent/metered-health/" + path
		r := httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+f.tokens[0])
		w := httptest.NewRecorder()
		f.s.AgentHandler().ServeHTTP(w, r)
		if w.Code != 426 {
			t.Fatal("plain HTTP", w.Code)
		}
		r = httptest.NewRequest(http.MethodPost, url, strings.NewReader(`{}`))
		r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == 200 || w.Code == 204 {
			t.Fatal("browser surfaced agent health")
		}
	}
	p := mx.SignMeteredTransactionPoll([]byte(f.tokens[0]), f.p[1])
	raw, _ := json.Marshal(p)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent/metered-health/poll", bytes.NewReader(raw))
	r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	r.Header.Set("Authorization", "Bearer "+f.tokens[0])
	w := httptest.NewRecorder()
	f.s.AgentHandler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("source token acquired destination health", w.Code)
	}
}
