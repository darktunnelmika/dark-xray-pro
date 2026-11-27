package hubcore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Secret panel address: with a path set, the panel's pages and its login
// answer 404 until the browser has opened https://host/<path>/ once (it
// then carries a gate cookie). Scanners and guessers never see a login
// page. Scripts with an API key or the admin token are not affected, and
// subscriptions, the Mini App and node installs stay public.

const gateCookie = "dark_gate"

var panelPathRE = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

type gateState struct {
	sync.Mutex
	path string
	at   time.Time
}

func (s *Store) panelPath(ctx context.Context) string {
	s.gate.Lock()
	defer s.gate.Unlock()
	if time.Since(s.gate.at) > 10*time.Second {
		s.gate.path, s.gate.at = s.Setting(ctx, "panel_path"), time.Now()
	}
	return s.gate.path
}

// SetPanelPath stores the secret path ("" turns it off).
func (s *Store) SetPanelPath(ctx context.Context, p string) error {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p != "" && !panelPathRE.MatchString(p) {
		return fmt.Errorf("%w: the path needs 6–64 letters, digits, - or _", ErrInvalid)
	}
	if err := s.SetSetting(ctx, "panel_path", p); err != nil {
		return err
	}
	s.gate.Lock()
	s.gate.path, s.gate.at = p, time.Now()
	s.gate.Unlock()
	return nil
}

func gateValue(path string) string {
	sum := sha256.Sum256([]byte("dark-gate|" + path))
	return hex.EncodeToString(sum[:16])
}

func setGate(w http.ResponseWriter, r *http.Request, path string) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: gateCookie, Value: gateValue(path), Path: "/", MaxAge: 365 * 86400,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// gated tells whether a request needs the gate: the panel pages and the
// sign-in. Everything else under /api/ checks its own credentials.
func gated(r *http.Request) bool {
	p := r.URL.Path
	for _, pub := range []string{"/sub/", "/app/", "/tgapp/", "/install/", "/migrate/", "/api/v1/"} {
		if strings.HasPrefix(p, pub) {
			return false
		}
	}
	if strings.HasPrefix(p, "/api/") {
		return p == "/api/v2/auth/login"
	}
	return true
}

func (s *Store) panelGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secret := s.panelPath(r.Context())
		if secret == "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/"+secret || r.URL.Path == "/"+secret+"/" {
			setGate(w, r, secret)
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if gated(r) && r.Header.Get("Authorization") == "" {
			c, err := r.Cookie(gateCookie)
			if err != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(gateValue(secret))) != 1 {
				http.NotFound(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// DefaultPanelName is what the panel calls itself until the owner names it.
const DefaultPanelName = "Dark Xray Pro"

// PanelName is the panel's own name (header, sign-in page, browser title).
func (s *Store) PanelName(ctx context.Context) string {
	if n := strings.TrimSpace(s.Setting(ctx, "panel_name")); n != "" {
		return n
	}
	return DefaultPanelName
}

// SetPanelName names the panel; empty goes back to the default.
func (s *Store) SetPanelName(ctx context.Context, name string) error {
	name = strings.Join(strings.Fields(name), " ")
	if len([]rune(name)) > 40 || strings.ContainsAny(name, "<>") {
		return fmt.Errorf("%w: the panel name is up to 40 characters, without < or >", ErrInvalid)
	}
	return s.SetSetting(ctx, "panel_name", name)
}
