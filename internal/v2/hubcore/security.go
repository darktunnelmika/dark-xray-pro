package hubcore

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// realIP is the visitor's address. Behind a local reverse proxy (nginx on
// the same host, or a private network) the proxy's X-Forwarded-For /
// X-Real-IP name the visitor; from anywhere else those headers are ignored,
// so nobody can pick their own address.
func realIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	p := net.ParseIP(ip)
	if p == nil || !(p.IsLoopback() || p.IsPrivate()) {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if c := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(c) != nil {
			return c
		}
	}
	if x := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(x) != nil {
		return x
	}
	return ip
}

// Login protection: wrong passwords are counted per address and per
// login+address. An address that keeps guessing is locked out for a while
// (longer each time); a login is never locked for everyone, so an attacker
// cannot keep the owner out.
const (
	guardWindow    = 15 * time.Minute
	guardIPMax     = 10 // wrong passwords from one address in the window
	guardUserIPMax = 5  // for one login from one address
	guardBaseLock  = 15 * time.Minute
	guardMaxLock   = 24 * time.Hour
)

type guardEntry struct {
	fails   []time.Time
	until   time.Time
	strikes int
	user    string
}

type loginGuard struct {
	mu sync.Mutex
	m  map[string]*guardEntry
}

// Lockout is a locked address shown in the panel.
type Lockout struct {
	Key     string `json:"key"`
	IP      string `json:"ip"`
	User    string `json:"user,omitempty"`
	Until   int64  `json:"until"`
	Strikes int    `json:"strikes"`
}

// blocked reports how long the address (or login from it) must wait.
func (g *loginGuard) blocked(ip, user string, now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	var wait time.Duration
	for _, k := range []string{"ip:" + ip, "u:" + strings.ToLower(user) + "|" + ip} {
		if e := g.m[k]; e != nil && now.Before(e.until) {
			if d := e.until.Sub(now); d > wait {
				wait = d
			}
		}
	}
	return wait
}

// fail counts a wrong password; it returns the new lock (0 if none).
func (g *loginGuard) fail(ip, user string, now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[string]*guardEntry{}
	}
	var locked time.Duration
	for _, x := range []struct {
		key string
		max int
	}{{"ip:" + ip, guardIPMax}, {"u:" + strings.ToLower(user) + "|" + ip, guardUserIPMax}} {
		e := g.m[x.key]
		if e == nil {
			e = &guardEntry{}
			g.m[x.key] = e
		}
		if strings.HasPrefix(x.key, "u:") {
			e.user = user
		}
		kept := e.fails[:0]
		for _, t := range e.fails {
			if now.Sub(t) < guardWindow {
				kept = append(kept, t)
			}
		}
		e.fails = append(kept, now)
		if len(e.fails) >= x.max {
			d := guardBaseLock << e.strikes
			if d > guardMaxLock || d <= 0 {
				d = guardMaxLock
			}
			e.strikes++
			e.until, e.fails = now.Add(d), nil
			if d > locked {
				locked = d
			}
		}
	}
	// Forget stale entries so the map stays small.
	if len(g.m) > 5000 {
		for k, e := range g.m {
			if now.After(e.until) && len(e.fails) == 0 {
				delete(g.m, k)
			}
		}
	}
	return locked
}

// success clears the wrong-password counters of that address.
func (g *loginGuard) success(ip, user string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Counters restart; strikes stay, so a repeat offender still waits longer.
	for _, k := range []string{"u:" + strings.ToLower(user) + "|" + ip, "ip:" + ip} {
		if e := g.m[k]; e != nil {
			e.fails = nil
		}
	}
}

func (g *loginGuard) list(now time.Time) []Lockout {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := []Lockout{}
	for k, e := range g.m {
		if !now.Before(e.until) {
			continue
		}
		ip := strings.TrimPrefix(k, "ip:")
		if i := strings.LastIndex(k, "|"); strings.HasPrefix(k, "u:") && i > 0 {
			ip = k[i+1:]
		}
		out = append(out, Lockout{Key: k, IP: ip, User: e.user, Until: e.until.Unix(), Strikes: e.strikes})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Until > out[j].Until })
	return out
}

// unlock lifts every lock of an address ("" = all).
func (g *loginGuard) unlock(ip string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for k, e := range g.m {
		if ip == "" || k == "ip:"+ip || strings.HasSuffix(k, "|"+ip) {
			if !e.until.IsZero() {
				n++
			}
			delete(g.m, k)
		}
	}
	return n
}

// ownerNotify, when set, tells the owners something urgent (Telegram).
var ownerNotify func(ctx context.Context, text string)

// SetOwnerNotify installs the owner alert channel.
func SetOwnerNotify(f func(ctx context.Context, text string)) { ownerNotify = f }

func notifyOwners(ctx context.Context, text string) {
	if f := ownerNotify; f != nil {
		go f(context.WithoutCancel(ctx), text)
	}
}

func lockText(ip, user string, d time.Duration) string {
	return fmt.Sprintf("🔒 ورود به پنل از %s برای %s بسته شد (رمز اشتباه پشت سر هم، نام ورود: %s). از بخش تیم ← امنیت ورود می‌توانید باز کنید.",
		ip, roundDur(d), user)
}

func roundDur(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%d ساعت", int(d.Hours()+0.5))
	}
	return fmt.Sprintf("%d دقیقه", int(d.Minutes()+0.5))
}

// ---------- Sessions ----------

// Session is one signed-in browser of a login.
type Session struct {
	ID       string `json:"id"` // first 16 hex of the token hash: names it, cannot sign in
	User     string `json:"user"`
	IP       string `json:"ip"`
	Agent    string `json:"agent"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"last_seen"`
	Expires  int64  `json:"expires"`
	Current  bool   `json:"current"`
}

func (s *Store) ensureSessionColumns() error {
	for _, c := range []struct{ col, def string }{
		{"created_unix", "INTEGER NOT NULL DEFAULT 0"}, {"last_seen_unix", "INTEGER NOT NULL DEFAULT 0"},
		{"ip", "TEXT NOT NULL DEFAULT ''"}, {"agent", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumn("sessions", c.col, c.def); err != nil {
			return err
		}
	}
	return nil
}

// noteSession stores where a new session signed in from.
func (s *Store) noteSession(ctx context.Context, tok string, r *http.Request) {
	agent := r.UserAgent()
	if len(agent) > 200 {
		agent = agent[:200]
	}
	now := s.now().Unix()
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET created_unix=?, last_seen_unix=?, ip=?, agent=? WHERE token_sha256=?`,
		now, now, realIP(r), agent, hashToken(tok))
}

// touchSession records activity at most every few minutes (not a write
// per request).
func (s *Store) touchSession(ctx context.Context, tok string, r *http.Request) {
	now := s.now().Unix()
	var last int64
	h := hashToken(tok)
	if s.ro.QueryRowContext(ctx, `SELECT last_seen_unix FROM sessions WHERE token_sha256=?`, h).Scan(&last) != nil || now-last < 300 {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_unix=?, ip=? WHERE token_sha256=?`, now, realIP(r), h)
}

// Sessions lists the sessions of one login ("" = every login).
func (s *Store) Sessions(ctx context.Context, user, currentTok string) ([]Session, error) {
	q := `SELECT token_sha256,username,ip,agent,created_unix,last_seen_unix,expires_unix FROM sessions WHERE expires_unix>=?`
	args := []any{s.now().Unix()}
	if user != "" {
		q += ` AND username=?`
		args = append(args, user)
	}
	rows, err := s.ro.QueryContext(ctx, q+` ORDER BY last_seen_unix DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cur := ""
	if currentTok != "" {
		cur = hashToken(currentTok)
	}
	out := []Session{}
	for rows.Next() {
		var x Session
		var h string
		if err := rows.Scan(&h, &x.User, &x.IP, &x.Agent, &x.Created, &x.LastSeen, &x.Expires); err != nil {
			return nil, err
		}
		x.ID, x.Current = h[:16], h == cur
		out = append(out, x)
	}
	return out, rows.Err()
}

// EndSession signs out one session of a login ("" user = any login).
func (s *Store) EndSession(ctx context.Context, user, id string) error {
	if len(id) != 16 {
		return ErrInvalid
	}
	q, args := `DELETE FROM sessions WHERE substr(token_sha256,1,16)=?`, []any{id}
	if user != "" {
		q += ` AND username=?`
		args = append(args, user)
	}
	r, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// EndOtherSessions signs out every session of a login except this one.
func (s *Store) EndOtherSessions(ctx context.Context, user, keepTok string) (int64, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE username=? AND token_sha256<>?`, user, hashToken(keepTok))
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
