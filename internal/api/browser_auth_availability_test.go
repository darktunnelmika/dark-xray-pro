package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func authAvailabilityServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private", "auth-availability.sqlite")
	s := businessLabAt(t, path)
	if _, err := s.business.BootstrapOwner(context.Background(), ownerUsername, ownerPassword); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func reopenAuthAvailabilityStore(t *testing.T, s *Server, path string) {
	t.Helper()
	store, err := business.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s.business = store
}

func requireAuthUnavailable(t *testing.T, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "5" {
		t.Fatalf("authentication failure status/retry mismatch: status=%d retry=%q",
			response.Code, response.Header().Get("Retry-After"))
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("temporary authentication failure changed the browser cookie")
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	// No username, raw DB error, token, permissions or false logout result.
	if len(payload) != 1 || payload["error"] != code {
		t.Fatalf("authentication failure was not the expected opaque %q result", code)
	}
}

func TestBrowserAuthAvailabilityClosedStorePreservesCookieAndRecovers(t *testing.T) {
	s, path := authAvailabilityServer(t)
	s.inboundKey = bytes.Repeat([]byte{0x42}, 32)
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	handler := s.Handler()
	if err := s.business.Close(); err != nil {
		t.Fatal(err)
	}
	requests := []struct {
		method, path string
		body         any
		code         string
	}{
		{"GET", "/api/v1/session/status", nil, "session_unavailable"},
		{"GET", "/api/v1/workspace/me", nil, "session_unavailable"},
		{"GET", "/api/v1/workspace/native-inbounds/501/execution", nil, "session_unavailable"},
		{"POST", "/api/v1/session/logout", map[string]any{}, "session_unavailable"},
		{"POST", "/api/v1/workspace/representatives", map[string]string{
			"id": "auth-unavailable-rep", "name": "Must not be created",
		}, "session_unavailable"},
		{"POST", "/api/v1/session/login", map[string]string{
			"username": ownerUsername, "password": ownerPassword,
		}, "login_unavailable"},
	}
	for _, item := range requests {
		t.Run(item.path, func(t *testing.T) {
			response := browserRequest(t, handler, item.method, item.path, item.body,
				cookie, csrf, "https://"+browserHost)
			requireAuthUnavailable(t, response, item.code)
		})
	}

	reopenAuthAvailabilityStore(t, s, path)
	status := browserRequest(t, handler, "GET", "/api/v1/session/status", nil, cookie, "", "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"authenticated":true`) {
		t.Fatal("existing cookie did not recover after the store reopened")
	}
	if response := browserRequest(t, handler, "GET", "/api/v1/workspace/me", nil, cookie, "", ""); response.Code != http.StatusOK {
		t.Fatal("verified identity did not recover its original workspace access")
	}
	if _, err := s.business.Representative(context.Background(), "auth-unavailable-rep"); !errors.Is(err, business.ErrNotFound) {
		t.Fatalf("unavailable authentication allowed a protected mutation: %v", err)
	}
	bad := browserRequest(t, handler, "POST", "/api/v1/session/login",
		map[string]string{"username": ownerUsername, "password": "wrong-password"}, nil, "", "https://"+browserHost)
	if bad.Code != http.StatusUnauthorized || len(bad.Result().Cookies()) != 0 || bad.Header().Get("Retry-After") != "" {
		t.Fatal("genuine bad credentials lost their unauthorized semantics")
	}
	logout := browserRequest(t, handler, "POST", "/api/v1/session/logout", map[string]any{}, cookie, csrf, "https://"+browserHost)
	if logout.Code != http.StatusOK {
		t.Fatal("recovered session could not be explicitly revoked")
	}
	if response := browserRequest(t, handler, "GET", "/api/v1/workspace/me", nil, cookie, "", ""); response.Code != http.StatusUnauthorized {
		t.Fatal("a genuinely revoked cookie retained authorization")
	}
}

func TestBrowserAuthAvailabilityCancelledLookupIsNotLogout(t *testing.T) {
	s := enabledOwner(t)
	cookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	handler := s.Handler()
	for _, item := range []struct{ method, path, code string }{
		{"GET", "/api/v1/session/status", "session_unavailable"},
		{"GET", "/api/v1/workspace/me", "session_unavailable"},
		{"POST", "/api/v1/session/login", "login_unavailable"},
	} {
		t.Run(item.path, func(t *testing.T) {
			var raw []byte
			if item.method == "POST" {
				var err error
				raw, err = json.Marshal(map[string]string{"username": ownerUsername, "password": ownerPassword})
				if err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			request := httptest.NewRequestWithContext(ctx, item.method,
				"https://"+browserHost+item.path, bytes.NewReader(raw))
			request.AddCookie(cookie)
			request.Header.Set("Origin", "https://"+browserHost)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			requireAuthUnavailable(t, response, item.code)
		})
	}
	if response := browserRequest(t, handler, "GET", "/api/v1/workspace/me", nil, cookie, "", ""); response.Code != http.StatusOK {
		t.Fatal("cancelled authentication revoked the durable valid session")
	}
}

type authAvailabilityBody struct {
	io.Reader
	beforeRead func()
}

func (body *authAvailabilityBody) Read(buffer []byte) (int, error) {
	if body.beforeRead != nil {
		before := body.beforeRead
		body.beforeRead = nil
		before()
	}
	return body.Reader.Read(buffer)
}

func TestBrowserAuthAvailabilityPasswordLookupFailureIsTemporary(t *testing.T) {
	s, path := authAvailabilityServer(t)
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	raw, err := json.Marshal(map[string]string{
		"old_password": ownerPassword, "new_password": "replacement-after-read-password",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The password route authenticates before reading its JSON body.
	// Closing the real store on that read tests a failure after authorization,
	// rather than only exercising the earlier session-verification branch.
	body := &authAvailabilityBody{Reader: bytes.NewReader(raw), beforeRead: func() {
		if err := s.business.Close(); err != nil {
			t.Fatal(err)
		}
	}}
	request := httptest.NewRequest("POST", "https://"+browserHost+"/api/v1/session/change-password", body)
	request.AddCookie(cookie)
	request.Header.Set("Origin", "https://"+browserHost)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	requireAuthUnavailable(t, response, "password_change_unavailable")
	reopenAuthAvailabilityStore(t, s, path)
	if _, err := s.business.VerifySession(context.Background(), cookie.Value); err != nil {
		t.Fatalf("failed password lookup revoked the existing session: %v", err)
	}
	browserLogin(t, s, ownerUsername, ownerPassword)
}

func TestBrowserAuthAvailabilityDoesNotShortenAuthorizedRequest(t *testing.T) {
	s := enabledOwner(t)
	cookie, _ := browserLogin(t, s, ownerUsername, ownerPassword)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, "GET", "https://"+browserHost+"/api/v1/workspace/me", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	session, ok := s.authorizeBrowser(response, request, false, false)
	if !ok {
		t.Fatal("valid request was not authorized")
	}
	if request.Context() != ctx || request.Context().Err() != nil {
		t.Fatal("authentication cancelled or replaced the enclosing request context")
	}
	if _, bounded := request.Context().Deadline(); bounded {
		t.Fatal("authentication imposed its deadline on the authorized operation")
	}
	// Real downstream work still succeeds after the authentication child
	// context has been cancelled and its resources released.
	if _, err := s.business.Representative(request.Context(), session.User.RepresentativeID); err != nil {
		t.Fatalf("authentication cancellation escaped into authorized work: %v", err)
	}
}

func TestExecutionStatusDisplayAuthenticationIsNarrow(t *testing.T) {
	for _, test := range []struct {
		method, path string
		display      bool
	}{
		{"GET", "/api/v1/workspace/native-inbounds/501/execution", true},
		{"GET", "/api/v1/workspace/native-inbounds/501/execution?unexpected=1", true},
		{"POST", "/api/v1/workspace/native-inbounds/501/execution", false},
		{"POST", "/api/v1/workspace/native-inbounds/501/execution/commands", false},
		{"POST", "/api/v1/workspace/native-inbounds/501/execution/preview", false},
		{"POST", "/api/v1/workspace/native-inbounds/501/execution/reconciliations", false},
		{"GET", "/api/v1/workspace/native-inbounds/501/execution/commands", false},
		{"GET", "/api/v1/workspace/native-inbounds/501/execution/reconciliations/event", false},
		{"GET", "/api/v1/workspace/native-inbounds/501/execution/traffic", false},
		{"GET", "/api/v1/workspace/native-inbounds/501/execution/", false},
		{"GET", "/api/v1/workspace/native-inbounds/0/execution", false},
		{"GET", "/api/v1/workspace/native-inbounds/0501/execution", false},
		{"GET", "/api/v1/workspace/native-inbounds/501/other/execution", false},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "https://"+browserHost+test.path, nil)
			if got := displaySessionSnapshot(request); got != test.display {
				t.Fatalf("display authentication=%v, want %v", got, test.display)
			}
		})
	}
}

func TestExecutionStatusDisplayAuthenticationEnforcesRevocation(t *testing.T) {
	s, _ := authAvailabilityServer(t)
	s.inboundKey = bytes.Repeat([]byte{0x42}, 32)
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	handler := s.Handler()
	path := "/api/v1/workspace/native-inbounds/501/execution"
	if response := browserRequest(t, handler, "GET", path, nil, cookie, "", ""); response.Code != http.StatusOK {
		t.Fatalf("valid display session rejected: %d", response.Code)
	}
	if response := browserRequest(t, handler, "POST", "/api/v1/session/logout", map[string]any{}, cookie, csrf, "https://"+browserHost); response.Code != http.StatusOK {
		t.Fatal("session logout failed")
	}
	if response := browserRequest(t, handler, "GET", path, nil, cookie, "", ""); response.Code != http.StatusUnauthorized {
		t.Fatal("execution status used a revoked display session")
	}
}
