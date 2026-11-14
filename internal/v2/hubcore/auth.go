package hubcore

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const authSchema = `
CREATE TABLE IF NOT EXISTS admins (
  username TEXT PRIMARY KEY,
  pass_hash TEXT NOT NULL,
  created_unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token_sha256 TEXT PRIMARY KEY,
  username TEXT NOT NULL REFERENCES admins(username) ON DELETE CASCADE,
  expires_unix INTEGER NOT NULL
);
`

const (
	sessionCookie = "dark_session"
	sessionTTL    = 7 * 24 * time.Hour
)

func (s *Store) ensureAuthSchema() error {
	_, err := s.db.Exec(authSchema)
	return err
}

// SetAdmin creates or resets an owner login.
func (s *Store) SetAdmin(ctx context.Context, user, pass string) error {
	if !idRE.MatchString(user) || len(pass) < 10 {
		return ErrInvalid
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pass), 12)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admins(username,pass_hash,created_unix) VALUES(?,?,?)
ON CONFLICT(username) DO UPDATE SET pass_hash=excluded.pass_hash`, user, string(h), s.now().Unix())
	if err == nil {
		// A password reset ends every existing session of that login.
		_, err = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE username=?`, user)
	}
	return err
}

// Login checks a password and returns a new session token.
func (s *Store) Login(ctx context.Context, user, pass string) (string, error) {
	var h string
	err := s.ro.QueryRowContext(ctx, `SELECT pass_hash FROM admins WHERE username=?`, user).Scan(&h)
	if err != nil {
		// Spend comparable time on unknown users.
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(pass))
		return "", ErrAuth
	}
	if bcrypt.CompareHashAndPassword([]byte(h), []byte(pass)) != nil {
		return "", ErrAuth
	}
	tok := randomHex(32)
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_unix<?`, now.Unix()); err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions(token_sha256,username,expires_unix) VALUES(?,?,?)`,
		hashToken(tok), user, now.Add(sessionTTL).Unix())
	return tok, err
}

// SessionUser resolves a session token.
func (s *Store) SessionUser(ctx context.Context, tok string) (string, error) {
	var user string
	err := s.ro.QueryRowContext(ctx, `SELECT username FROM sessions WHERE token_sha256=? AND expires_unix>=?`,
		hashToken(tok), s.now().Unix()).Scan(&user)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrAuth
	}
	return user, err
}

func (s *Store) Logout(ctx context.Context, tok string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_sha256=?`, hashToken(tok))
	return err
}

// authorize accepts the session cookie, or the bearer admin token used by
// scripts (which acts as the owner). Mutations through a cookie must carry
// X-Dark-Request (a custom header a cross-site form cannot send), in
// addition to SameSite=Strict.
func authorize(s *Store, adminToken string, r *http.Request) (Principal, bool) {
	if tok, err := bearer(r.Header.Get("Authorization")); err == nil {
		if adminToken != "" && constantEqual(tok, adminToken) {
			return Principal{User: "api", Role: "owner"}, true
		}
		return Principal{}, false
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return Principal{}, false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Dark-Request") != "1" {
		return Principal{}, false
	}
	user, err := s.SessionUser(r.Context(), c.Value)
	if err != nil {
		return Principal{}, false
	}
	p, err := s.principal(r.Context(), user)
	if err != nil {
		return Principal{}, false
	}
	if p.Role == "rep" {
		var enabled bool
		if s.ro.QueryRowContext(r.Context(), `SELECT enabled FROM reps WHERE id=?`, p.RepID).Scan(&enabled) != nil || !enabled {
			return Principal{}, false
		}
	}
	return p, true
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, tok string, maxAge int) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}

var (
	dummyOnce sync.Once
	dummy     []byte
)

func dummyHash() []byte {
	dummyOnce.Do(func() { dummy, _ = bcrypt.GenerateFromPassword([]byte("dark-unknown-user"), 12) })
	return dummy
}

func constantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
