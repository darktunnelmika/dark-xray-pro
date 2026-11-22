package hubcore

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
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
	if _, err := s.db.Exec(authSchema); err != nil {
		return err
	}
	return s.ensureSessionColumns()
}

// SetAdmin creates or resets an owner login.
func (s *Store) SetAdmin(ctx context.Context, user, pass string) error {
	// A password pasted from a chat often carries a space or a line break at
	// either end; nobody means that to be part of it.
	pass = strings.TrimSpace(pass)
	if !idRE.MatchString(user) {
		return fmt.Errorf("%w: the sign-in name may only have lowercase letters, digits, dot, dash and underscore", ErrInvalid)
	}
	if len(pass) < 10 {
		return fmt.Errorf("%w: the password needs at least 10 characters", ErrInvalid)
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

// Login checks a password and returns a new session token. A login with
// two-step on needs LoginTOTP.
func (s *Store) Login(ctx context.Context, user, pass string) (string, error) {
	return s.LoginTOTP(ctx, user, pass, "")
}

// LoginTOTP checks the password, then the authenticator code when the login
// has two-step on (ErrTOTPRequired when it is missing).
func (s *Store) LoginTOTP(ctx context.Context, user, pass, code string) (string, error) {
	user = s.canonicalUser(ctx, user)
	secret, last, err := s.verifyPassword(ctx, user, pass)
	if t := strings.TrimSpace(pass); err != nil && t != pass && t != "" {
		// Pasted with a stray space or line break around it.
		secret, last, err = s.verifyPassword(ctx, user, t)
	}
	if err != nil {
		return "", err
	}
	if err := s.checkTOTP(ctx, user, secret, last, code); err != nil {
		return "", err
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

// verifyPassword checks a password and returns the login's TOTP state.
// canonicalUser matches a sign-in name regardless of letter case (logins
// moved from the old panel were stored in lower case, people type
// "Mehdi"), preferring an exact match.
func (s *Store) canonicalUser(ctx context.Context, user string) string {
	var got string
	if s.ro.QueryRowContext(ctx, `SELECT username FROM admins WHERE username=? COLLATE NOCASE ORDER BY username=? DESC LIMIT 1`, user, user).Scan(&got) == nil {
		return got
	}
	return user
}

func (s *Store) verifyPassword(ctx context.Context, user, pass string) (secret string, last int64, err error) {
	var h string
	err = s.ro.QueryRowContext(ctx, `SELECT pass_hash,totp_secret,totp_last FROM admins WHERE username=?`, user).Scan(&h, &secret, &last)
	if err != nil {
		// Spend comparable time on unknown users.
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(pass))
		return "", 0, ErrAuth
	}
	if strings.HasPrefix(h, "scrypt$") {
		// A login moved from the DARK XRAY panel: same upgrade path.
		if !darkOldScryptMatch(h, pass) {
			return "", 0, ErrAuth
		}
		if nh, err := bcrypt.GenerateFromPassword([]byte(pass), 12); err == nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE admins SET pass_hash=? WHERE username=? AND pass_hash=?`, string(nh), user, h)
		}
		return secret, last, nil
	}
	if strings.HasPrefix(h, "argon2id:") {
		// A login moved from the old panel: check its argon2id hash, then
		// upgrade it to bcrypt so the old format disappears.
		if !oldArgonMatch(h, pass) {
			return "", 0, ErrAuth
		}
		if nh, err := bcrypt.GenerateFromPassword([]byte(pass), 12); err == nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE admins SET pass_hash=? WHERE username=? AND pass_hash=?`, string(nh), user, h)
		}
		return secret, last, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(h), []byte(pass)) != nil {
		return "", 0, ErrAuth
	}
	return secret, last, nil
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
		return s.apiKeyPrincipal(r.Context(), tok)
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
	s.touchSession(r.Context(), c.Value, r)
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
