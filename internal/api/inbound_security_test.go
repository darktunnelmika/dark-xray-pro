package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundsecurity"
)

const inboundSecurityBase = "/api/v1/workspace/inbound-security"

func inboundSecurityAPIFixture(t *testing.T) (*Server, *http.Cookie, string, *atomic.Int32) {
	t.Helper()
	s := enabledOwner(t)
	var dials atomic.Int32
	if err := s.EnableInboundSecurity(inboundsecurity.Config{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("local fixture never opens a socket")
		},
	}); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	return s, cookie, csrf, &dials
}

func TestInboundSecurityAPIRequiresOwnerOriginAndCSRF(t *testing.T) {
	s, cookie, csrf, dials := inboundSecurityAPIFixture(t)
	handler := s.Handler()
	if response := browserRequest(t, handler, "GET", inboundSecurityBase+"/capabilities", nil, nil, "", ""); response.Code != http.StatusUnauthorized {
		t.Fatal("anonymous capability access", response.Code)
	}
	for _, attempt := range []struct {
		cookie *http.Cookie
		csrf   string
		origin string
		status int
	}{
		{nil, csrf, "https://" + browserHost, http.StatusUnauthorized},
		{cookie, "", "https://" + browserHost, http.StatusForbidden},
		{cookie, csrf, "https://evil.example", http.StatusForbidden},
		{cookie, csrf, "", http.StatusForbidden},
	} {
		response := browserRequest(t, handler, "POST", inboundSecurityBase+"/keys/x25519", map[string]any{}, attempt.cookie, attempt.csrf, attempt.origin)
		if response.Code != attempt.status {
			t.Fatal("generation bypassed browser authorization", response.Code, attempt.status)
		}
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "rep-security", "Representative security"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "rep-security", "rep-security-login", repPassword); err != nil {
		t.Fatal(err)
	}
	repCookie, repCSRF := browserLogin(t, s, "rep-security-login", repPassword)
	for _, route := range []struct{ method, path string }{{"GET", "/capabilities"}, {"POST", "/keys/x25519"}, {"POST", "/reality/scan"}} {
		response := browserRequest(t, handler, route.method, inboundSecurityBase+route.path, map[string]any{}, repCookie, repCSRF, "https://"+browserHost)
		if response.Code != http.StatusForbidden {
			t.Fatal("representative accessed owner security tools", response.Code)
		}
	}
	if response := browserRequest(t, s.AgentHandler(), "GET", inboundSecurityBase+"/capabilities", nil, cookie, "", ""); response.Code == http.StatusOK {
		t.Fatal("Agent listener exposed owner security tools")
	}
	if dials.Load() != 0 {
		t.Fatal("unauthorized request caused a network attempt")
	}
}

func TestInboundSecurityAPIKeyCertificateAndExplicitScope(t *testing.T) {
	s, cookie, csrf, dials := inboundSecurityAPIFixture(t)
	handler := s.Handler()
	call := func(path string, body any) *httptest.ResponseRecorder {
		return browserRequest(t, handler, "POST", inboundSecurityBase+path, body, cookie, csrf, "https://"+browserHost)
	}
	capabilities := browserRequest(t, handler, "GET", inboundSecurityBase+"/capabilities", nil, cookie, "", "")
	var capability inboundsecurity.Capabilities
	if capabilities.Code != http.StatusOK || json.Unmarshal(capabilities.Body.Bytes(), &capability) != nil || !capability.Enabled ||
		!capability.Keys["x25519"] || capability.Keys["mldsa65"] || capability.NodeProbeAvailable || len(capability.DefaultTargets) != 10 || dials.Load() != 0 {
		t.Fatal("capabilities misrepresented tools or started a scan", capabilities.Code)
	}
	generated := call("/keys/x25519", map[string]any{})
	var keys inboundsecurity.KeyPair
	if generated.Code != http.StatusOK || json.Unmarshal(generated.Body.Bytes(), &keys) != nil || keys.Kind != "x25519" || len(keys.PrivateKey) != 43 || len(keys.PublicKey) != 43 {
		t.Fatal("X25519 generation failed", generated.Code)
	}
	if generated.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("key response can be cached")
	}
	if response := call("/keys/mldsa65", map[string]any{}); response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "pinned_xray_core_required") {
		t.Fatal("missing pinned core was not explicit", response.Code)
	}
	certificateResponse := call("/certificates/generate", inboundsecurity.GenerateCertificateRequest{ServerNames: []string{"target.example"}, ValidDays: 365})
	var certificate inboundsecurity.GeneratedCertificate
	if certificateResponse.Code != http.StatusOK || json.Unmarshal(certificateResponse.Body.Bytes(), &certificate) != nil || certificate.CertificatePEM == "" || certificate.PrivateKeyPEM == "" {
		t.Fatal("self-signed certificate generation failed", certificateResponse.Code)
	}
	inspection := call("/certificates/inspect", map[string]string{"pem": certificate.CertificatePEM, "server_name": "target.example"})
	if inspection.Code != http.StatusOK || !strings.Contains(inspection.Body.String(), "spki_sha256_base64") || strings.Contains(inspection.Body.String(), "PRIVATE KEY") || strings.Contains(inspection.Body.String(), "certificate_pem") {
		t.Fatal("certificate inspection leaked material or lost hashes", inspection.Code)
	}
	request := inboundsecurity.ProbeRequest{Scope: "node", NodeID: "paired-node", Target: "8.8.8.8:443", ServerName: "target.example"}
	response := call("/reality/scan", request)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "selected_node_transport_unavailable") || dials.Load() != 0 {
		t.Fatal("selected node probe fell back to hub", response.Code)
	}
	request.Scope, request.NodeID, request.Target = "hub", "", "127.0.0.1:443"
	if response := call("/certificates/remote", request); response.Code != http.StatusUnprocessableEntity || dials.Load() != 0 {
		t.Fatal("remote certificate tool admitted private target", response.Code)
	}
	request.Target = "8.8.8.8:443"
	response = call("/certificates/remote", request)
	var result inboundsecurity.ProbeResult
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.OK || result.Scope != "hub" || result.Error != "tcp_connect_failed" || dials.Load() != 1 {
		t.Fatal("remote certificate network failure was misrepresented", response.Code)
	}
}

func TestInboundSecurityAPIRejectsAmbiguousJSONBeforeAction(t *testing.T) {
	s, cookie, csrf, dials := inboundSecurityAPIFixture(t)
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"scope":"hub","target":"8.8.8.8:443","server_name":""}`,
		`{"scope":"hub","node_id":null,"target":"8.8.8.8:443","server_name":""}`,
		`{"scope":"hub","scope":"node","node_id":"","target":"8.8.8.8:443","server_name":""}`,
		`{"Scope":"hub","node_id":"","target":"8.8.8.8:443","server_name":""}`,
		`{"scope":"hub","node_id":"","target":"8.8.8.8:443","server_name":"","extra":true}`,
		`{"scope":"hub","node_id":"","target":"8.8.8.8:443","server_name":""} {}`,
		`{"scope":"hub","node_id":"","target":"` + strings.Repeat("x", 4096) + `","server_name":""}`,
	} {
		response := rawInboundSecurityRequest(s.Handler(), "/reality/scan", raw, cookie, csrf)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatal("ambiguous security request accepted", response.Code)
		}
	}
	for _, raw := range []string{
		`{"scope":"hub","node_id":"","targets":[{"target":"8.8.8.8:443","target":"127.0.0.1:443","server_name":""}]}`,
		`{"scope":"hub","node_id":"","targets":[{"target":"8.8.8.8:443","server_name":null}]}`,
		`{"scope":"hub","node_id":"","targets":[{"target":"8.8.8.8:443","server_name":"","extra":true}]}`,
	} {
		response := rawInboundSecurityRequest(s.Handler(), "/reality/find", raw, cookie, csrf)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatal("ambiguous nested target accepted", response.Code)
		}
	}
	if dials.Load() != 0 {
		t.Fatal("invalid JSON caused a connection")
	}
}

func rawInboundSecurityRequest(handler http.Handler, path, raw string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("POST", "https://"+browserHost+inboundSecurityBase+path, strings.NewReader(raw))
	request.AddCookie(cookie)
	request.Header.Set("Origin", "https://"+browserHost)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type securityDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *securityDeadlineWriter) SetWriteDeadline(value time.Time) error {
	w.deadline = value
	return nil
}

func TestInboundSecurityFindExtendsOnlyItsWriteDeadline(t *testing.T) {
	s, cookie, csrf, dials := inboundSecurityAPIFixture(t)
	request := httptest.NewRequest("POST", "https://"+browserHost+inboundSecurityBase+"/reality/find", strings.NewReader(`{"scope":"hub","node_id":"","targets":[{"target":"8.8.8.8:443","server_name":""}]}`))
	request.AddCookie(cookie)
	request.Header.Set("Origin", "https://"+browserHost)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	writer := &securityDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now()
	s.Handler().ServeHTTP(writer, request)
	if writer.Code != http.StatusOK || writer.deadline.Before(before.Add(90*time.Second)) || writer.deadline.After(time.Now().Add(96*time.Second)) || dials.Load() != 1 {
		t.Fatal("finder did not receive its bounded write deadline", writer.Code)
	}
	request = httptest.NewRequest("POST", "https://"+browserHost+inboundSecurityBase+"/keys/x25519", strings.NewReader(`{}`))
	request.AddCookie(cookie)
	request.Header.Set("Origin", "https://"+browserHost)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	writer = &securityDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	s.Handler().ServeHTTP(writer, request)
	if writer.Code != http.StatusOK || !writer.deadline.IsZero() {
		t.Fatal("finder deadline escaped onto unrelated security request", writer.Code)
	}
}
