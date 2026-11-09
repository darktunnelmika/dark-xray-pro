package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

const sessionCookieName = "__Host-darkpro_session"
const browserAuthenticationBudget = 5 * time.Second

func browserAuthUnavailable(w http.ResponseWriter, code string) {
	w.Header().Set("Retry-After", "5")
	respond(w, http.StatusServiceUnavailable, map[string]string{"error": code})
}

// Bounded global login pressure and bounded expensive Argon2 operations.
// Production will also require edge rate limiting and account lockout policy.
type loginGate struct {
	mu       sync.Mutex
	window   time.Time
	attempts int
	perUser  map[string]int
	parallel chan struct{}
}

func newLoginGate() *loginGate {
	return &loginGate{
		parallel: make(chan struct{}, 3), perUser: make(map[string]int),
	}
}
func (g *loginGate) admit(username string) bool {
	username = strings.ToLower(username)
	g.mu.Lock()
	now := time.Now()
	if now.Sub(g.window) >= time.Minute {
		g.window = now
		g.attempts = 0
		g.perUser = make(map[string]int)
	}
	if g.attempts >= 240 || g.perUser[username] >= 10 ||
		(len(g.perUser) >= 4096 && g.perUser[username] == 0) {
		g.mu.Unlock()
		return false
	}
	g.attempts++
	g.perUser[username]++
	g.mu.Unlock()
	select {
	case g.parallel <- struct{}{}:
		return true
	default:
		return false
	}
}
func (g *loginGate) release() { <-g.parallel }

func sameOriginMutation(r *http.Request) bool {
	return sameOriginMutationMedia(r, "application/json")
}

// The binary upload route opts into its own exact media type. Every existing
// browser mutation continues to use the JSON-only wrapper above.
func sameOriginMutationMedia(r *http.Request, requiredMedia string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := "https"
	host := r.Host
	if host == "localhost" || host == "127.0.0.1" || host == "[::1]" {
		scheme = "http"
	} else if h, _, err := net.SplitHostPort(host); err == nil &&
		(h == "localhost" || h == "127.0.0.1" || h == "::1") {
		scheme = "http"
	}
	// The external reverse proxy terminates TLS but MUST forward the original
	// Host unchanged and never honor an untrusted X-Forwarded-Host.
	if u.Scheme != scheme && !(scheme == "http" && u.Scheme == "https") {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && media == requiredMedia
}
func sessionCookie(token string, expiry time.Time) *http.Cookie {
	return &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", Expires: expiry,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	}
}
func clearSessionCookie() *http.Cookie {
	c := sessionCookie("", time.Unix(1, 0))
	c.MaxAge = -1
	return c
}
func (s *Server) csrfValue(token string) string {
	m := hmac.New(sha256.New, []byte(s.ownerKey))
	_, _ = m.Write([]byte("DARK-PRO-BROWSER-CSRF-v1:"))
	_, _ = m.Write([]byte(token))
	return hex.EncodeToString(m.Sum(nil))
}

// Only these display routes may authorize from an independent committed read.
// Other reads and every mutation retain the original writer-backed verification.
func displaySessionSnapshot(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v1/session/status" || r.URL.Path == "/api/v1/workspace/nodes" {
		return true
	}
	// Only the exact read-only managed execution status route may use the
	// committed reader. Commands, previews and reconciliation keep strict auth.
	id, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspace/native-inbounds/")
	if !ok {
		return false
	}
	id, ok = strings.CutSuffix(id, "/execution")
	if !ok || id == "" || id[0] == '0' || len(id) > 16 {
		return false
	}
	for _, digit := range id {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func (s *Server) signedSession(r *http.Request) (business.LoginSession, error) {
	if s.business == nil {
		return business.LoginSession{}, business.ErrUnavailable
	}
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return business.LoginSession{}, business.ErrUnauthenticated
	}
	// Bound authentication only; authorized handlers keep the original context.
	ctx, cancel := context.WithTimeout(r.Context(), browserAuthenticationBudget)
	defer cancel()
	if displaySessionSnapshot(r) {
		return s.business.VerifyDisplaySession(ctx, c.Value)
	}
	return s.business.VerifySession(ctx, c.Value)
}
func (s *Server) authorizeBrowser(w http.ResponseWriter, r *http.Request, mutate, ownerOnly bool) (business.LoginSession, bool) {
	return s.authorizeBrowserMedia(w, r, mutate, ownerOnly, "application/json")
}
func (s *Server) authorizeBrowserMedia(w http.ResponseWriter, r *http.Request, mutate, ownerOnly bool, requiredMedia string) (business.LoginSession, bool) {
	ctx, span := s.business.BeginDiagnostic(r.Context(), business.DiagnosticOperationSessionVerify, business.DiagnosticSourceAPI)
	var diagnosticErr error
	defer func() { span.End(diagnosticErr) }()
	span.Enter(business.DiagnosticPhaseAuth)
	// The diagnostic context carries only numeric span ancestry. Authentication
	// still bounds its own child; the caller's downstream request is unchanged.
	session, err := s.signedSession(r.WithContext(ctx))
	diagnosticErr = err
	if err != nil {
		span.Enter(business.DiagnosticPhaseResponse)
		if errors.Is(err, business.ErrUnauthenticated) {
			respond(w, http.StatusUnauthorized, map[string]string{"error": "session_required"})
		} else {
			browserAuthUnavailable(w, "session_unavailable")
		}
		return business.LoginSession{}, false
	}
	if ownerOnly && session.User.Role != "owner" {
		span.Enter(business.DiagnosticPhaseResponse)
		respond(w, http.StatusForbidden, map[string]string{"error": "owner_permission_required"})
		return business.LoginSession{}, false
	}
	if session.User.Role == "representative" {
		key, allowed := requiredRepresentativePermission(r)
		if !allowed || !repPermissionAllowed(session.User.Permissions, key) {
			span.Enter(business.DiagnosticPhaseResponse)
			respond(w, http.StatusForbidden, map[string]string{"error": "representative_permission_denied"})
			return business.LoginSession{}, false
		}
	}
	if mutate {
		if !sameOriginMutationMedia(r, requiredMedia) {
			code := "same_origin_json_required"
			if requiredMedia != "application/json" {
				code = "same_origin_upload_required"
			}
			span.Enter(business.DiagnosticPhaseResponse)
			respond(w, http.StatusForbidden, map[string]string{"error": code})
			return business.LoginSession{}, false
		}
		got := r.Header.Get("X-CSRF-Token")
		expected := s.csrfValue(session.Token)
		if len(got) != len(expected) || !hmac.Equal([]byte(got), []byte(expected)) {
			span.Enter(business.DiagnosticPhaseResponse)
			respond(w, http.StatusForbidden, map[string]string{"error": "invalid_csrf"})
			return business.LoginSession{}, false
		}
	}
	return session, true
}
func (s *Server) attachBrowserAuth(mux *http.ServeMux) {
	if s.business == nil {
		return
	}
	if s.loginGate == nil {
		s.loginGate = newLoginGate()
	}

	mux.HandleFunc("GET /api/v1/session/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := s.business.BeginDiagnostic(r.Context(), business.DiagnosticOperationSessionVerify, business.DiagnosticSourceAPI)
		var diagnosticErr error
		defer func() { span.End(diagnosticErr) }()
		span.Enter(business.DiagnosticPhaseAuth)
		r = r.WithContext(ctx)
		state, err := s.signedSession(r)
		diagnosticErr = err
		span.Enter(business.DiagnosticPhaseResponse)
		if err != nil {
			if errors.Is(err, business.ErrUnauthenticated) {
				respond(w, http.StatusOK, map[string]any{"enabled": true, "authenticated": false})
			} else {
				browserAuthUnavailable(w, "session_unavailable")
			}
			return
		}
		respond(w, http.StatusOK, map[string]any{
			"enabled": true, "authenticated": true, "user": state.User,
			"expires_unix": state.ExpiresUnix, "csrf_token": s.csrfValue(state.Token),
		})
	})
	mux.HandleFunc("POST /api/v1/session/login", func(w http.ResponseWriter, r *http.Request) {
		diagnosticContext, span := s.business.BeginDiagnostic(r.Context(), business.DiagnosticOperationSessionLogin, business.DiagnosticSourceAPI)
		var diagnosticErr error
		defer func() { span.End(diagnosticErr) }()
		r = r.WithContext(diagnosticContext)
		span.Enter(business.DiagnosticPhaseAuth)
		if !sameOriginMutation(r) {
			span.Enter(business.DiagnosticPhaseResponse)
			respond(w, http.StatusForbidden, map[string]string{"error": "same_origin_json_required"})
			return
		}
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		span.Enter(business.DiagnosticPhaseDecode)
		if err := decode(w, r, 2048, &input); err != nil {
			diagnosticErr = err
			span.Enter(business.DiagnosticPhaseResponse)
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		span.Enter(business.DiagnosticPhaseAuth)
		if !s.loginGate.admit(input.Username) {
			span.Enter(business.DiagnosticPhaseResponse)
			w.Header().Set("Retry-After", "60")
			respond(w, http.StatusTooManyRequests, map[string]string{"error": "login_temporarily_rate_limited"})
			return
		}
		defer s.loginGate.release()
		ctx, cancel := context.WithTimeout(r.Context(), browserAuthenticationBudget)
		span.Enter(business.DiagnosticPhaseWork)
		state, err := s.business.Login(ctx, input.Username, input.Password)
		cancel()
		diagnosticErr = err
		span.Enter(business.DiagnosticPhaseResponse)
		if err != nil {
			if errors.Is(err, business.ErrUnauthenticated) {
				respond(w, http.StatusUnauthorized, map[string]string{"error": "invalid_credentials"})
			} else {
				browserAuthUnavailable(w, "login_unavailable")
			}
			return
		}
		http.SetCookie(w, sessionCookie(state.Token, time.Unix(state.ExpiresUnix, 0)))
		respond(w, http.StatusOK, map[string]any{
			"authenticated": true, "user": state.User,
			"expires_unix": state.ExpiresUnix, "csrf_token": s.csrfValue(state.Token),
		})
	})
	mux.HandleFunc("POST /api/v1/session/logout", func(w http.ResponseWriter, r *http.Request) {
		state, ok := s.authorizeBrowser(w, r, true, false)
		if !ok {
			return
		}
		if err := s.business.Logout(r.Context(), state.Token); err != nil {
			respond(w, http.StatusServiceUnavailable, map[string]string{"error": "logout_unavailable"})
			return
		}
		http.SetCookie(w, clearSessionCookie())
		respond(w, http.StatusOK, map[string]bool{"logged_out": true})
	})
	mux.HandleFunc("POST /api/v1/session/change-password", func(w http.ResponseWriter, r *http.Request) {
		state, ok := s.authorizeBrowser(w, r, true, false)
		if !ok {
			return
		}
		var input struct {
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		if err := decode(w, r, 1024, &input); err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		if err := s.business.RotatePassword(r.Context(), state.User.ID, input.OldPassword, input.NewPassword); err != nil {
			if errors.Is(err, business.ErrUnauthenticated) {
				respond(w, http.StatusUnauthorized, map[string]string{"error": "current_password_invalid"})
			} else if errors.Is(err, business.ErrInvalid) {
				respond(w, http.StatusUnprocessableEntity, map[string]string{"error": "new_password_invalid"})
			} else if errors.Is(err, business.ErrConflict) {
				respond(w, http.StatusConflict, map[string]string{"error": "password_update_failed"})
			} else {
				browserAuthUnavailable(w, "password_change_unavailable")
			}
			return
		}
		http.SetCookie(w, clearSessionCookie())
		respond(w, http.StatusOK, map[string]bool{"password_changed": true, "sessions_revoked": true})
	})
}
