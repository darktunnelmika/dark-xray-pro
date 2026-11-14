package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Staff logins share the owner's scope with fewer rights:
//
//   - admin:  everything except staff, audit, backup, finance and the raw
//     Xray overlay (the owner's business);
//   - viewer: read-only.
//
// They are stored in admins.role; principal() reports Role "owner" with
// Level set, so every per-client scope check treats them as full-scope
// while the HTTP guards apply the narrower rights.

var staffRoles = map[string]bool{"owner": true, "admin": true, "viewer": true}

// ownerOnly lists admin API prefixes a staff admin cannot reach.
var ownerOnly = []string{
	"/api/v2/admin/staff", "/api/v2/admin/audit", "/api/v2/admin/backup", "/api/v2/admin/restore",
	"/api/v2/admin/finance", "/api/v2/admin/xray/overlay", "/api/v2/admin/apikeys",
}

func isOwnerOnly(path string) bool {
	for _, p := range ownerOnly {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// allowed applies the staff level to one request.
func (p Principal) allowed(r *http.Request) bool {
	switch p.Level {
	case "":
		return true
	case "admin":
		return !isOwnerOnly(r.URL.Path)
	case "viewer":
		read := r.Method == http.MethodGet || r.Method == http.MethodHead
		own := r.URL.Path == "/api/v2/auth/password" || strings.HasPrefix(r.URL.Path, "/api/v2/auth/2fa")
		return (read || own) && !isOwnerOnly(r.URL.Path)
	}
	return false
}

type StaffLogin struct {
	Username    string `json:"username"`
	Role        string `json:"role"`
	CreatedUnix int64  `json:"created_unix"`
	LastSeen    int64  `json:"last_seen"`
	TOTP        bool   `json:"totp"`
}

// Staff lists the owner and staff logins (representatives have their own page).
func (s *Store) Staff(ctx context.Context) ([]StaffLogin, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT a.username,a.role,a.created_unix,
COALESCE((SELECT MAX(unix) FROM audit_log l WHERE l.actor=a.username),0), a.totp_secret<>''
FROM admins a WHERE a.role IN ('owner','admin','viewer') ORDER BY a.role<>'owner', a.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StaffLogin{}
	for rows.Next() {
		var l StaffLogin
		if err := rows.Scan(&l.Username, &l.Role, &l.CreatedUnix, &l.LastSeen, &l.TOTP); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// PutStaff creates a login or changes its role and/or password. An empty
// password keeps the current one (required for a new login).
func (s *Store) PutStaff(ctx context.Context, user, role, pass string) error {
	if !idRE.MatchString(user) || !staffRoles[role] {
		return fmt.Errorf("%w: username and a role (owner, admin, viewer) are required", ErrInvalid)
	}
	var cur string
	err := s.ro.QueryRowContext(ctx, `SELECT role FROM admins WHERE username=?`, user).Scan(&cur)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if len(pass) < 10 {
			return fmt.Errorf("%w: a new login needs a password of at least 10 characters", ErrInvalid)
		}
	case err != nil:
		return err
	case cur == "rep":
		return fmt.Errorf("%w: %s is a representative login", ErrInvalid, user)
	case cur == "owner" && role != "owner":
		if err := s.keepAnOwner(ctx, user); err != nil {
			return err
		}
	}
	if pass != "" {
		if err := s.SetAdmin(ctx, user, pass); err != nil {
			return fmt.Errorf("%w: password must be at least 10 characters", ErrInvalid)
		}
	}
	res, err := s.db.ExecContext(ctx, `UPDATE admins SET role=?, rep_id='' WHERE username=?`, role, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if cur != "" && cur != role {
		// A role change takes effect at once: end that login's sessions.
		_, err = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE username=?`, user)
	}
	return err
}

// DeleteStaff removes a staff or owner login (never the last owner).
func (s *Store) DeleteStaff(ctx context.Context, user string) error {
	var cur string
	if err := s.ro.QueryRowContext(ctx, `SELECT role FROM admins WHERE username=?`, user).Scan(&cur); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if cur == "rep" {
		return fmt.Errorf("%w: remove representatives from their page", ErrInvalid)
	}
	if cur == "owner" {
		if err := s.keepAnOwner(ctx, user); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM admins WHERE username=?`, user)
	return err
}

func (s *Store) keepAnOwner(ctx context.Context, except string) error {
	var n int
	if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE role='owner' AND username<>?`, except).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: the panel needs at least one owner login", ErrInvalid)
	}
	return nil
}

// --- audit log ---

const auditSchema = `
CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  unix INTEGER NOT NULL,
  actor TEXT NOT NULL,
  role TEXT NOT NULL,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  status INTEGER NOT NULL,
  ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS audit_log_unix ON audit_log(unix);
CREATE INDEX IF NOT EXISTS audit_log_actor ON audit_log(actor, unix);
`

func (s *Store) ensureStaffSchema() error {
	if _, err := s.db.Exec(auditSchema); err != nil {
		return err
	}
	return s.addColumn("admins", "totp_secret", "TEXT NOT NULL DEFAULT ''")
}

type AuditEntry struct {
	ID     int64  `json:"id"`
	Unix   int64  `json:"unix"`
	Actor  string `json:"actor"`
	Role   string `json:"role"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	IP     string `json:"ip"`
}

// Audit records one change. Bodies are never stored (they hold passwords,
// keys and tokens); the path names what was changed.
func (s *Store) Audit(ctx context.Context, p Principal, r *http.Request, status int) {
	role := p.Role
	if p.Level != "" {
		role = p.Level
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	path := r.URL.Path
	if len(path) > 300 {
		path = path[:300]
	}
	_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `INSERT INTO audit_log(unix,actor,role,method,path,status,ip) VALUES(?,?,?,?,?,?,?)`,
		s.now().Unix(), p.User, role, r.Method, path, status, ip)
}

// AuditLog returns newest-first entries, optionally for one actor or a
// path fragment (e.g. a client's email), before an id for paging.
func (s *Store) AuditLog(ctx context.Context, actor, contains string, before int64, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT id,unix,actor,role,method,path,status,ip FROM audit_log
WHERE id<? AND (?='' OR actor=?) AND (?='' OR instr(path,?)>0) ORDER BY id DESC LIMIT ?`, before, actor, actor, contains, contains, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Unix, &e.Actor, &e.Role, &e.Method, &e.Path, &e.Status, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// statusWriter remembers the response status for the audit log.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
