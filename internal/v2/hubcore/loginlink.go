package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"net/http"
	"time"
)

// One-time sign-in links: a partner's website can put a "go to your panel"
// button next to a reseller panel it sold. A link works once, for ten
// minutes, and opens a normal session for that login. A login with two-step
// sign-in on gets no link (a link must not skip the code).

const loginLinkSchema = `CREATE TABLE IF NOT EXISTS login_links (
  sha TEXT PRIMARY KEY,
  username TEXT NOT NULL,
  expires_unix INTEGER NOT NULL,
  used_unix INTEGER NOT NULL DEFAULT 0,
  created_by TEXT NOT NULL DEFAULT ''
);`

const (
	loginLinkTTL  = 10 * time.Minute
	loginLinkPath = "/api/v2/auth/link/"
)

func (s *Store) ensureLoginLinkSchema() error {
	_, err := s.db.Exec(loginLinkSchema)
	return err
}

// NewLoginLink makes a link token for a login.
func (s *Store) NewLoginLink(ctx context.Context, user, by string) (string, int64, error) {
	var totp bool
	err := s.ro.QueryRowContext(ctx, `SELECT totp_secret<>'' FROM admins WHERE username=?`, user).Scan(&totp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	if err != nil {
		return "", 0, err
	}
	if totp {
		return "", 0, fmt.Errorf("%w: two-step sign-in is on for this login, so a link cannot sign it in", ErrPolicy)
	}
	tok := randomHex(32)
	now := s.now().Unix()
	exp := now + int64(loginLinkTTL.Seconds())
	_, _ = s.db.ExecContext(ctx, `DELETE FROM login_links WHERE expires_unix<?`, now-86400)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO login_links(sha,username,expires_unix,created_by) VALUES(?,?,?,?)`, hashToken(tok), user, exp, by); err != nil {
		return "", 0, err
	}
	return tok, exp, nil
}

// useLoginLink spends a link and returns a new session token for its login.
func (s *Store) useLoginLink(ctx context.Context, tok string) (user, session string, err error) {
	now := s.now().Unix()
	r, err := s.db.ExecContext(ctx, `UPDATE login_links SET used_unix=? WHERE sha=? AND used_unix=0 AND expires_unix>?`, now, hashToken(tok), now)
	if err != nil {
		return "", "", err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return "", "", ErrAuth
	}
	if err := s.db.QueryRowContext(ctx, `SELECT username FROM login_links WHERE sha=?`, hashToken(tok)).Scan(&user); err != nil {
		return "", "", ErrAuth
	}
	p, err := s.principal(ctx, user)
	if err != nil {
		return "", "", ErrAuth
	}
	if p.Role == "rep" {
		var on bool
		if s.ro.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=?`, p.RepID).Scan(&on) != nil || !on {
			return "", "", ErrAuth
		}
	}
	var totp bool
	if s.ro.QueryRowContext(ctx, `SELECT totp_secret<>'' FROM admins WHERE username=?`, user).Scan(&totp) != nil || totp {
		return "", "", ErrAuth
	}
	session = randomHex(32)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_sha256,username,expires_unix) VALUES(?,?,?)`,
		hashToken(session), user, s.now().Add(sessionTTL).Unix()); err != nil {
		return "", "", err
	}
	return user, session, nil
}

func (s *Store) loginLinkRoute(mux routeMux) {
	mux.HandleFunc("GET "+loginLinkPath+"{token}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		ctx := r.Context()
		user, session, err := s.useLoginLink(ctx, r.PathValue("token"))
		if err != nil {
			s.Audit(ctx, Principal{User: "link", Role: "login"}, r, http.StatusGone)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusGone)
			name := html.EscapeString(s.PanelName(ctx))
			_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<body style="font-family:sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;text-align:center"><h2>%s</h2>
<p dir="rtl">این لینک ورود منقضی شده یا قبلاً استفاده شده است. یک لینک تازه بگیرید.</p>
<p>This sign-in link has expired or was already used. Ask for a new one.</p></body>`, name, name)
			return
		}
		s.noteSession(ctx, session, r)
		setSessionCookie(w, r, session, int(sessionTTL.Seconds()))
		if p := s.panelPath(ctx); p != "" {
			setGate(w, r, p) // the browser gets the way in with the session
		}
		s.Audit(ctx, Principal{User: user, Role: "login"}, r, http.StatusOK)
		http.Redirect(w, r, "/", http.StatusFound)
	})
}
