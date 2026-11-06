package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type phaseAPIResult struct {
	status int
	header http.Header
	body   map[string]any
}

func phaseAPIResultOf(t *testing.T, response *httptest.ResponseRecorder) phaseAPIResult {
	t.Helper()
	var body map[string]any
	decoder := json.NewDecoder(bytes.NewReader(response.Body.Bytes()))
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		t.Fatal("diagnostic parity fixture returned invalid JSON")
	}
	// The two reads have different wall times; authority and readiness deadlines
	// remain compared exactly. No credential or error body is printed on failure.
	delete(body, "server_time_unix_milli")
	return phaseAPIResult{response.Code, response.Header().Clone(), body}
}

func TestBusinessPhaseDiagnosticsAPIReadParity(t *testing.T) {
	s, cookie, _, client := subscriptionViewAPIFixture(t)
	applySubscriptionViewFixture(t, s, client)
	handler := s.Handler()
	base := "/api/v1/workspace/clients/" + client.ID + "/inbound-subscription"
	cases := []struct {
		name, path string
		cookie     *http.Cookie
		status     int
	}{
		{"session", "/api/v1/session/status", cookie, 200},
		{"runtime", "/api/v1/workspace/native-inbounds/501/execution", cookie, 200},
		{"metadata_legacy", base, cookie, 200},
		{"metadata_v2", base + "?view=2", cookie, 200},
		{"metadata_invalid_view", base + "?view=2&view=2", cookie, 400},
		{"runtime_anonymous", "/api/v1/workspace/native-inbounds/501/execution", nil, 401},
		{"metadata_anonymous", base + "?view=2", nil, 401},
	}
	before := make([]phaseAPIResult, len(cases))
	for i, item := range cases {
		response := browserRequest(t, handler, "GET", item.path, nil, item.cookie, "", "")
		if response.Code != item.status {
			t.Fatalf("disabled diagnostics: %s status %d", item.name, response.Code)
		}
		before[i] = phaseAPIResultOf(t, response)
	}
	if before[3].body["runtime_ready"] != true || before[3].body["available"] != true {
		t.Fatal("fixture did not exercise an applied, ready metadata response")
	}
	if err := s.business.EnablePhaseDiagnostics(); err != nil {
		t.Fatal("enable diagnostics")
	}
	// No exporter starts: instrumentation cannot depend on a journal consumer.
	for i, item := range cases {
		response := browserRequest(t, handler, "GET", item.path, nil, item.cookie, "", "")
		if !reflect.DeepEqual(before[i], phaseAPIResultOf(t, response)) {
			t.Fatalf("diagnostics changed %s response, headers or authority", item.name)
		}
	}
}

func TestBusinessPhaseDiagnosticsLoginCookieAndFailureParity(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			s, path := authAvailabilityServer(t)
			if enabled {
				if err := s.business.EnablePhaseDiagnostics(); err != nil {
					t.Fatal("enable diagnostics")
				}
			}
			cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
			if csrf != s.csrfValue(cookie.Value) || !cookie.Expires.After(time.Now()) {
				t.Fatal("diagnostics changed the session cookie/CSRF contract")
			}
			handler := s.Handler()
			bad := browserRequest(t, handler, "POST", "/api/v1/session/login",
				map[string]string{"username": ownerUsername, "password": "incorrect-password"}, nil, "", "https://"+browserHost)
			if bad.Code != 401 || len(bad.Result().Cookies()) != 0 || bad.Header().Get("Retry-After") != "" {
				t.Fatal("diagnostics changed invalid-credential semantics")
			}
			crossOrigin := browserRequest(t, handler, "POST", "/api/v1/session/login",
				map[string]string{"username": ownerUsername, "password": ownerPassword}, nil, "", "https://other.example")
			if crossOrigin.Code != 403 || len(crossOrigin.Result().Cookies()) != 0 {
				t.Fatal("diagnostics changed same-origin enforcement")
			}
			if err := s.business.Close(); err != nil {
				t.Fatal("close temporary store")
			}
			requireAuthUnavailable(t, browserRequest(t, handler, "GET", "/api/v1/session/status",
				nil, cookie, "", ""), "session_unavailable")
			requireAuthUnavailable(t, browserRequest(t, handler, "POST", "/api/v1/session/login",
				map[string]string{"username": ownerUsername, "password": ownerPassword}, nil, "", "https://"+browserHost), "login_unavailable")
			reopenAuthAvailabilityStore(t, s, path)
			if enabled {
				if err := s.business.EnablePhaseDiagnostics(); err != nil {
					t.Fatal("enable reopened diagnostics")
				}
			}
			if _, err := s.business.VerifySession(context.Background(), cookie.Value); err != nil {
				t.Fatal("temporary diagnostic-mode failure invalidated a durable session")
			}
		})
	}
}

func TestBusinessPhaseDiagnosticsPreserveRequestCancellation(t *testing.T) {
	s, cookie, _, client := subscriptionViewAPIFixture(t)
	applySubscriptionViewFixture(t, s, client)
	handler := s.Handler()
	for _, enabled := range []bool{false, true} {
		if enabled {
			if err := s.business.EnablePhaseDiagnostics(); err != nil {
				t.Fatal("enable diagnostics")
			}
		}
		deadline := time.Now().Add(time.Minute)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		request := httptest.NewRequestWithContext(ctx, "GET", "https://"+browserHost+"/api/v1/workspace/me", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		session, ok := s.authorizeBrowser(response, request, false, false)
		if !ok || request.Context() != ctx || request.Context().Err() != nil {
			cancel()
			t.Fatal("diagnostic authentication changed its enclosing request context")
		}
		got, present := request.Context().Deadline()
		if !present || !got.Equal(deadline) {
			cancel()
			t.Fatal("authentication child deadline escaped into the authorized request")
		}
		if _, err := s.business.Representative(request.Context(), session.User.RepresentativeID); err != nil {
			cancel()
			t.Fatal("authentication child cancellation escaped into downstream work")
		}
		cancel()
		if !errors.Is(request.Context().Err(), context.Canceled) {
			t.Fatal("diagnostics detached the caller's cancellation")
		}
		for _, item := range []struct{ method, path, code string }{
			{"GET", "/api/v1/session/status", "session_unavailable"},
			{"GET", "/api/v1/workspace/native-inbounds/501/execution", "session_unavailable"},
			{"GET", "/api/v1/workspace/clients/" + client.ID + "/inbound-subscription?view=2", "session_unavailable"},
			{"POST", "/api/v1/session/login", "login_unavailable"},
		} {
			raw := []byte(nil)
			if item.method == "POST" {
				raw, _ = json.Marshal(map[string]string{"username": ownerUsername, "password": ownerPassword})
			}
			req := httptest.NewRequestWithContext(ctx, item.method, "https://"+browserHost+item.path, bytes.NewReader(raw))
			req.Header.Set("Origin", "https://"+browserHost)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(cookie)
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			requireAuthUnavailable(t, out, item.code)
			if req.Context() != ctx || !errors.Is(req.Context().Err(), context.Canceled) {
				t.Fatal("instrumented handler changed cancellation propagation")
			}
		}
		if _, err := s.business.VerifySession(context.Background(), cookie.Value); err != nil {
			t.Fatal("cancelled diagnostic request revoked a valid session")
		}
	}
}

func TestBusinessPhaseDiagnosticsAgentDenialParity(t *testing.T) {
	s, _, _, client := subscriptionViewAPIFixture(t)
	applySubscriptionViewFixture(t, s, client)
	handler := s.AgentHandler()
	read := func(operation string, tlsVersion uint16, body string) phaseAPIResult {
		req := httptest.NewRequest("POST", "https://agent.example/api/v1/agent/inbound-execution/"+operation, strings.NewReader(body))
		req.TLS = &tls.ConnectionState{Version: tlsVersion}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+strings.Repeat("f", 64))
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return phaseAPIResultOf(t, out)
	}
	type probe struct {
		operation string
		version   uint16
		body      string
		status    int
	}
	var cases []probe
	for _, operation := range []string{"usage", "observation"} {
		cases = append(cases,
			probe{operation, tls.VersionTLS12, `{}`, 426},
			probe{operation, tls.VersionTLS13, `{"node_id":"missing-node","installation_id":"` + strings.Repeat("b", 32) + `"}`, 401})
	}
	before := make([]phaseAPIResult, len(cases))
	for i, item := range cases {
		before[i] = read(item.operation, item.version, item.body)
		if before[i].status != item.status {
			t.Fatal("agent denial fixture did not reach its expected boundary")
		}
	}
	if err := s.business.EnablePhaseDiagnostics(); err != nil {
		t.Fatal("enable diagnostics")
	}
	for i, item := range cases {
		if !reflect.DeepEqual(before[i], read(item.operation, item.version, item.body)) {
			t.Fatal("diagnostics changed Agent TLS/identity denial")
		}
	}
}

func phaseAPIFixtureReplay(t *testing.T, s *Server, observed ix.Observation) ix.UsageReport {
	t.Helper()
	bundle, found, err := s.business.InboundExecutionBundle(context.Background(), s.inboundKey, observed.NodeID, observed.InstallationID)
	if err != nil || !found || len(bundle.Principals) != 1 {
		t.Fatal("diagnostic replay fixture unavailable")
	}
	// This is the existing fixture's synthetic process identity, never a running
	// core. Its sequence-one zero counters have already been durably accepted.
	member, err := json.Marshal(ix.ProcessMember{
		BootID: "00000000-0000-0000-0000-000000000001", PID: 1, StartTicks: 1,
		BinarySHA: strings.Repeat("b", 64), ConfigSHA: ix.BytesSHA(bundle.NativeConfig),
	})
	if err != nil {
		t.Fatal("encode synthetic member")
	}
	group, err := json.Marshal(map[string]string{"xray": string(member)})
	if err != nil {
		t.Fatal("encode synthetic group")
	}
	return ix.UsageReport{
		EpochID: observed.EpochID, NodeID: observed.NodeID, InstallationID: observed.InstallationID,
		Generation: observed.Generation, Sequence: 1, ProcessIdentity: string(group),
		Counters: []ix.Counter{{PrincipalID: bundle.Principals[0].ID, Upload: 0, Download: 0}},
	}
}

func TestBusinessPhaseDiagnosticsFullQueueDoesNotWaitForExporter(t *testing.T) {
	s, _, _, client := subscriptionViewAPIFixture(t)
	observed := applySubscriptionViewFixture(t, s, client)
	report := phaseAPIFixtureReplay(t, s, observed)
	exchange := localInboundExchange{s: s}
	want, err := exchange.Usage(context.Background(), report)
	if err != nil || exchange.Observe(context.Background(), observed) != nil {
		t.Fatal("disabled local exchange fixture failed")
	}
	if err := s.business.EnablePhaseDiagnostics(); err != nil {
		t.Fatal("enable diagnostics")
	}
	handler := s.Handler()
	done := make(chan string, 1)
	go func() {
		// The collector queue is 256 fixed records. With no exporter running,
		// these completed scopes necessarily fill it and exercise drop behavior.
		for i := 0; i < 512; i++ {
			req := httptest.NewRequest("GET", "https://"+browserHost+"/api/v1/session/status", nil)
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			if out.Code != 200 || len(out.Result().Cookies()) != 0 {
				done <- "anonymous_response_changed"
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		got, err := exchange.Usage(ctx, report)
		if err != nil || !reflect.DeepEqual(got, want) {
			done <- "replay_receipt_changed"
			return
		}
		if err := exchange.Observe(ctx, observed); err != nil {
			done <- "observation_changed"
			return
		}
		done <- ""
	}()
	select {
	case failure := <-done:
		if failure != "" {
			t.Fatal(failure)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP scopes or local receipt waited for an absent diagnostic exporter")
	}
}
