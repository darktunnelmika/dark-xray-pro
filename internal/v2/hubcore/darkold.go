package hubcore

import (
	"compress/gzip"
	"context"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

// Moving from the DARK XRAY panel (0.10, Python). A one-time link gives a
// command to run on the old server: it exports what the move needs from a
// read-only view of the live database (resellers, logins, clients, Restore
// links, shop, wallets, bot tokens; not the traffic history) and uploads it
// here once. A preview shows exactly what an import would do; the import is
// one transaction.
//
// Clients keep their UUID, Trojan password, limits, expiry, usage, owner
// and subscription link: the old /sub/<token> and the Restore links
// (/sub/<id> on the old 3x-ui domains) answer with the client's v2
// subscription once those domains point here.

//go:embed darkold_export.py
var darkOldExporter []byte

type darkOldUpload struct {
	tok     string
	expires time.Time
}

var darkOld struct {
	sync.Mutex
	up darkOldUpload
}

func (s *Store) darkOldPath() string { return s.path + ".darkold" }

// DarkOldStart issues the one-time export link (2 hours).
func (s *Store) DarkOldStart(base string) string {
	tok := randomHex(20)
	darkOld.Lock()
	darkOld.up = darkOldUpload{tok: tok, expires: time.Now().Add(2 * time.Hour)}
	darkOld.Unlock()
	b := strings.TrimRight(base, "/") + "/migrate/" + tok
	return "curl -fsSL " + b + "/export.py -o /tmp/dark-export.py && /opt/dark-xray/.venv/bin/python /tmp/dark-export.py " + b + "/upload"
}

func darkOldTokenOK(tok string) bool {
	darkOld.Lock()
	defer darkOld.Unlock()
	return darkOld.up.tok != "" && time.Now().Before(darkOld.up.expires) &&
		subtle.ConstantTimeCompare([]byte(tok), []byte(darkOld.up.tok)) == 1
}

// DarkOldHandler serves /migrate/<tok>/export.py and the upload.
func DarkOldHandler(s *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /migrate/{tok}/export.py", func(w http.ResponseWriter, r *http.Request) {
		if !darkOldTokenOK(r.PathValue("tok")) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/x-python; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(darkOldExporter)
	})
	mux.HandleFunc("POST /migrate/{tok}/upload", func(w http.ResponseWriter, r *http.Request) {
		if !darkOldTokenOK(r.PathValue("tok")) {
			http.NotFound(w, r)
			return
		}
		gz, err := gzip.NewReader(io.LimitReader(r.Body, 512<<20))
		if err != nil {
			http.Error(w, "not a gzip upload", http.StatusBadRequest)
			return
		}
		tmp := s.darkOldPath() + ".tmp"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, err = io.Copy(out, io.LimitReader(gz, 2<<30))
		out.Close()
		if err == nil {
			_, err = darkOldSummary(r.Context(), tmp)
		}
		if err != nil {
			os.Remove(tmp)
			http.Error(w, "the upload is not a DARK XRAY export: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := os.Rename(tmp, s.darkOldPath()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		darkOld.Lock()
		darkOld.up = darkOldUpload{} // used once
		darkOld.Unlock()
		_, _ = w.Write([]byte("✅ uploaded — open the new panel to preview and import\n"))
	})
	return mux
}

type DarkOldStatus struct {
	Uploaded bool           `json:"uploaded"`
	At       int64          `json:"at,omitempty"`
	Origin   string         `json:"origin,omitempty"`
	Version  string         `json:"version,omitempty"`
	Counts   map[string]int `json:"counts,omitempty"`
}

func darkOldOpen(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	return sql.Open("sqlite", u.String())
}

func darkOldSummary(ctx context.Context, path string) (DarkOldStatus, error) {
	db, err := darkOldOpen(path)
	if err != nil {
		return DarkOldStatus{}, err
	}
	defer db.Close()
	st := DarkOldStatus{Uploaded: true, Counts: map[string]int{}}
	if !tableExists(ctx, db, "owners") || !tableExists(ctx, db, "core_clients") || !tableExists(ctx, db, "export_meta") {
		return st, errors.New("missing tables")
	}
	_ = db.QueryRowContext(ctx, `SELECT v FROM export_meta WHERE k='public_origin'`).Scan(&st.Origin)
	_ = db.QueryRowContext(ctx, `SELECT v FROM export_meta WHERE k='version'`).Scan(&st.Version)
	for k, q := range map[string]string{
		"resellers": `SELECT COUNT(*) FROM api_admins WHERE role='reseller'`,
		"clients":   `SELECT COUNT(*) FROM clients`,
		"restore":   `SELECT COUNT(*) FROM restore_subscriptions WHERE deleted_at=0`,
		"bots":      `SELECT COUNT(*) FROM export_bot_tokens`,
		"wallets":   `SELECT COUNT(*) FROM customer_wallets`,
		"products":  `SELECT COUNT(*) FROM commerce_products`,
	} {
		var n int
		if db.QueryRowContext(ctx, q).Scan(&n) == nil {
			st.Counts[k] = n
		}
	}
	return st, nil
}

// DarkOldStatus reports the uploaded export, if any.
func (s *Store) DarkOldStatus(ctx context.Context) DarkOldStatus {
	fi, err := os.Stat(s.darkOldPath())
	if err != nil {
		return DarkOldStatus{}
	}
	st, err := darkOldSummary(ctx, s.darkOldPath())
	if err != nil {
		return DarkOldStatus{}
	}
	st.At = fi.ModTime().Unix()
	return st
}

// DarkOldDiscard deletes the uploaded export.
func (s *Store) DarkOldDiscard() error {
	err := os.Remove(s.darkOldPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

type DarkOldOptions struct {
	Inbounds        []string `json:"inbounds"`         // v2 inbounds every moved client gets
	RestoreInbounds []string `json:"restore_inbounds"` // for the Restore clients (default: Inbounds)
	DryRun          bool     `json:"dry_run"`
}

type DarkOldReport struct {
	DryRun   bool     `json:"dry_run"`
	Reps     int      `json:"reps"`
	Logins   int      `json:"logins"`
	Clients  int      `json:"clients"`
	Restore  int      `json:"restore"`
	Disabled int      `json:"disabled"`
	Usage    int      `json:"usage"`
	Links    int      `json:"links"`
	Telegram int      `json:"telegram"` // clients linked to a Telegram account
	Groups   int      `json:"groups"`
	Domains  []string `json:"domains"` // domains to point here so old links keep working
	Skipped  []string `json:"skipped"`
	Notes    []string `json:"notes"`
}

func (r *DarkOldReport) skip(f string, a ...any) { r.Skipped = append(r.Skipped, fmt.Sprintf(f, a...)) }
func (r *DarkOldReport) note(f string, a ...any) { r.Notes = append(r.Notes, fmt.Sprintf(f, a...)) }

type darkOldBody struct {
	ID         string `json:"id"`
	Password   string `json:"password"`
	TotalGB    int64  `json:"totalGB"`
	ExpiryTime int64  `json:"expiryTime"`
	LimitIP    int    `json:"limitIp"`
	LimitHWID  int    `json:"limitHwid"`
	Enable     *bool  `json:"enable"`
	Group      string `json:"group"`
	Comment    string `json:"comment"`
	TgID       any    `json:"tgId"`
}

func (b darkOldBody) tg() int64 {
	switch v := b.TgID.(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	}
	return 0
}

// ImportDarkOld moves the uploaded export (or previews it).
func (s *Store) ImportDarkOld(ctx context.Context, opt DarkOldOptions) (DarkOldReport, error) {
	rep := DarkOldReport{DryRun: opt.DryRun, Skipped: []string{}, Notes: []string{}, Domains: []string{}}
	if _, err := os.Stat(s.darkOldPath()); err != nil {
		return rep, fmt.Errorf("%w: upload the export from the old server first", ErrInvalid)
	}
	if len(opt.RestoreInbounds) == 0 {
		opt.RestoreInbounds = opt.Inbounds
	}
	for _, tag := range append(append([]string{}, opt.Inbounds...), opt.RestoreInbounds...) {
		var n int
		if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbounds WHERE tag=?`, tag).Scan(&n); err != nil || n == 0 {
			return rep, fmt.Errorf("%w: inbound %q does not exist", ErrInvalid, tag)
		}
	}
	old, err := darkOldOpen(s.darkOldPath())
	if err != nil {
		return rep, err
	}
	defer old.Close()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()
	now := s.now().Unix()

	// Resellers: api_admins role=reseller, named by owner_profiles.
	type owner struct {
		id, role, hash, name string
		volume, units        int64
		disabled             bool
	}
	var owners []owner
	rows, err := old.QueryContext(ctx, `SELECT a.id,a.role,a.password_hash,a.disabled,COALESCE(p.name,''),COALESCE(o.volume_credit_bytes,0),COALESCE(o.unlimited_credit,0),COALESCE(o.account_disabled,0)
FROM api_admins a LEFT JOIN owner_profiles p ON p.id=a.id LEFT JOIN owners o ON o.id=a.id ORDER BY a.id`)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var o owner
		var adis, odis bool
		if err := rows.Scan(&o.id, &o.role, &o.hash, &adis, &o.name, &o.volume, &o.units, &odis); err != nil {
			rows.Close()
			return rep, err
		}
		o.disabled = adis || odis
		owners = append(owners, o)
	}
	rows.Close()
	repOf := map[string]string{} // old owner id -> v2 rep id ("" = the owner)
	for _, o := range owners {
		if o.role == "owner" {
			repOf[o.id] = ""
			continue
		}
		id := v2ID(o.id)
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, id).Scan(&exists)
		if exists == 0 {
			name := o.name
			if name == "" {
				name = o.id
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO reps(id,name,enabled,volume_credit,unlimited_units,created_unix) VALUES(?,?,?,?,?,?)`,
				id, name, !o.disabled, max(o.volume, 0), max(o.units, 0), now); err != nil {
				return rep, fmt.Errorf("reseller %s: %w", o.id, err)
			}
			rep.Reps++
		} else {
			rep.note("reseller %s already exists here; its clients join it", id)
		}
		repOf[o.id] = id
	}
	// Logins keep their passwords (scrypt, upgraded to bcrypt on first sign-in).
	for _, o := range owners {
		user := v2ID(o.id)
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE username=?`, user).Scan(&exists)
		if exists > 0 {
			rep.skip("login %s: already exists here (keep using the v2 password)", user)
			continue
		}
		if !strings.HasPrefix(o.hash, "scrypt$") {
			rep.skip("login %s: unknown password format", o.id)
			continue
		}
		role, repID := "owner", ""
		if o.role != "owner" {
			role, repID = "rep", repOf[o.id]
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admins(username,pass_hash,created_unix,role,rep_id) VALUES(?,?,?,?,?)`, user, o.hash, now, role, repID); err != nil {
			return rep, fmt.Errorf("login %s: %w", o.id, err)
		}
		rep.Logins++
	}

	// Native clients.
	type oc struct {
		email, owner, token, body, state string
		limitIP                          int
		quota, expires, up, down         int64
		extDisabled                      bool
		created                          float64
		unlimited, waitDays              int64
	}
	var list []oc
	rows, err = old.QueryContext(ctx, `SELECT c.id,c.owner,c.limit_ip,c.quota_bytes,c.expires_at,m.public_token,m.state,m.external_disabled,m.created_at,k.body,k.up,k.down
FROM clients c JOIN managed_clients m ON m.email=c.id JOIN core_clients k ON k.email=c.id ORDER BY m.created_at`)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var c oc
		if err := rows.Scan(&c.email, &c.owner, &c.limitIP, &c.quota, &c.expires, &c.token, &c.state, &c.extDisabled, &c.created, &c.body, &c.up, &c.down); err != nil {
			rows.Close()
			return rep, err
		}
		list = append(list, c)
	}
	rows.Close()
	unl := map[string]bool{}
	if tableExists(ctx, old, "unlimited_entitlements") {
		r2, err := old.QueryContext(ctx, `SELECT client_id,state_json FROM unlimited_entitlements`)
		if err == nil {
			for r2.Next() {
				var id, st string
				if r2.Scan(&id, &st) == nil {
					var e struct {
						Segments [][]int64 `json:"segments"`
						Units    int64     `json:"units"`
						Untimed  bool      `json:"legacy_untimed"`
					}
					if json.Unmarshal([]byte(st), &e) == nil && (len(e.Segments) > 0 || e.Units > 0 || e.Untimed) {
						unl[id] = true
					}
				}
			}
			r2.Close()
		}
	}
	// Plans bought with "start on first connection" that have not started.
	wait := map[string]int64{}
	if tableExists(ctx, old, "commerce_orders") {
		if r3, err := old.QueryContext(ctx, `SELECT client_id,duration_days FROM commerce_orders WHERE status='provisioned_waiting_activation' AND client_id!=''`); err == nil {
			for r3.Next() {
				var id string
				var d int64
				if r3.Scan(&id, &d) == nil && d > 0 {
					wait[id] = d
				}
			}
			r3.Close()
		}
	}
	groups := map[string]string{} // owner|name -> v2 group id
	addGroup := func(ownerID, name, email string) error {
		if name == "" {
			return nil
		}
		key := ownerID + "|" + name
		gid, ok := groups[key]
		if !ok {
			gname := name
			if r := repOf[ownerID]; r != "" {
				gname = r + " · " + name
			}
			gid = v2ID("old-" + gname)
			var n int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_groups WHERE id=?`, gid).Scan(&n)
			if n == 0 {
				if _, err := tx.ExecContext(ctx, `INSERT INTO client_groups(id,name,created_unix) VALUES(?,?,?)`, gid, gname, now); err != nil {
					return err
				}
				rep.Groups++
			}
			groups[key] = gid
		}
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO client_group_members(group_id,email) VALUES(?,?)`, gid, email)
		return err
	}
	put := func(email, uuid, secret, token string, enabled bool, vol, expires, dur int64, ips int, inbounds []string) error {
		cl := Client{Email: email, UUID: strings.ToLower(uuid), Secret: secret, Enabled: enabled, VolumeBytes: vol,
			ExpiresUnix: expires, DurationSeconds: dur, MaxIPs: max(ips, 0), Inbounds: inbounds, SubToken: token}
		if !uuidRE.MatchString(cl.UUID) {
			cl.UUID = ""
		}
		var n int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE sub_token=?`, token).Scan(&n)
		if n > 0 {
			cl.SubToken = ""
			rep.note("client %s: its subscription link is already used here; it gets a new one", email)
		} else if token != "" {
			rep.Links++
		}
		return putClientTx(ctx, tx, cl, now)
	}
	for _, c := range list {
		if c.state == "deleted" {
			continue
		}
		email := v2Email(c.email)
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, email).Scan(&exists)
		if exists > 0 {
			rep.skip("client %s: already exists here", email)
			continue
		}
		var b darkOldBody
		_ = json.Unmarshal([]byte(c.body), &b)
		enabled := !c.extDisabled && (b.Enable == nil || *b.Enable)
		vol := c.quota
		if vol <= 0 {
			vol = -1
		}
		var dur int64
		expires := c.expires
		if expires == 0 && wait[c.email] > 0 {
			dur = wait[c.email] * 86400
		}
		if err := put(email, b.ID, b.Password, c.token, enabled, vol, expires, dur, c.limitIP, opt.Inbounds); err != nil {
			return rep, fmt.Errorf("client %s: %w", c.email, err)
		}
		plan := "volume"
		if unl[c.email] || c.quota <= 0 {
			plan = "unlimited"
		}
		repID, ok := repOf[c.owner]
		if !ok {
			rep.note("client %s: its reseller %s was not found; it belongs to the owner", email, c.owner)
		}
		created := int64(c.created)
		if created <= 0 {
			created = now
		}
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET rep_id=?, plan=?, note=?, created_unix=?, hwid_limit=? WHERE email=?`,
			repID, plan, strings.TrimSpace(b.Comment), created, max(b.LimitHWID, 0), email); err != nil {
			return rep, err
		}
		if c.up+c.down > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,?,?,?)`, email, c.up, c.down, created, created); err != nil {
				return rep, err
			}
			rep.Usage++
		}
		if tg := b.tg(); tg > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tg_links(chat_id,email) VALUES(?,?)`, tg, email); err != nil {
				return rep, err
			}
			rep.Telegram++
		}
		if err := addGroup(c.owner, b.Group, email); err != nil {
			return rep, err
		}
		rep.Clients++
		if !enabled {
			rep.Disabled++
		}
	}

	// Restore clients: users of the old 3x-ui panels, reached by their old
	// links (/sub/<id> on the old domains).
	domains := map[string]bool{}
	if tableExists(ctx, old, "restore_subscriptions") {
		type rc struct {
			email, legacyURL, path, body string
			enabled                      bool
			up, down, total, expire      int64
			created                      float64
		}
		var rl []rc
		rows, err = old.QueryContext(ctx, `SELECT r.core_email,r.legacy_url,r.legacy_path,r.enabled,r.legacy_upload,r.legacy_download,r.legacy_total,r.legacy_expire,r.created_at,
COALESCE(k.body,'{}'),COALESCE((SELECT SUM(up) FROM restore_usage u WHERE u.restore_id=r.id),0),
COALESCE((SELECT SUM(down) FROM restore_usage u WHERE u.restore_id=r.id),0)
FROM restore_subscriptions r LEFT JOIN core_clients k ON k.email=r.core_email WHERE r.deleted_at=0 AND r.promoted_at=0`)
		if err != nil {
			return rep, err
		}
		for rows.Next() {
			var c rc
			var dup, ddown int64
			if err := rows.Scan(&c.email, &c.legacyURL, &c.path, &c.enabled, &c.up, &c.down, &c.total, &c.expire, &c.created, &c.body, &dup, &ddown); err != nil {
				rows.Close()
				return rep, err
			}
			c.up += dup // usage before (old 3x-ui) and since the Restore
			c.down += ddown
			rl = append(rl, c)
		}
		rows.Close()
		for _, c := range rl {
			email := v2Email(c.email)
			var exists int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, email).Scan(&exists)
			if exists > 0 {
				rep.skip("restore client %s: already exists here", email)
				continue
			}
			var b darkOldBody
			_ = json.Unmarshal([]byte(c.body), &b)
			token := ""
			if p := strings.TrimPrefix(c.path, "/sub/"); p != c.path && p != "" && !strings.Contains(p, "/") {
				token = p
			}
			vol := c.total
			if vol <= 0 {
				vol = -1
			}
			expires := c.expire
			if expires > 1e11 { // milliseconds
				expires /= 1000
			}
			if expires < 0 {
				expires = 0
			}
			if err := put(email, b.ID, b.Password, token, c.enabled, vol, expires, 0, b.LimitIP, opt.RestoreInbounds); err != nil {
				return rep, fmt.Errorf("restore client %s: %w", c.email, err)
			}
			created := int64(c.created)
			if created <= 0 {
				created = now
			}
			plan := "volume"
			if c.total <= 0 {
				plan = "unlimited"
			}
			if _, err := tx.ExecContext(ctx, `UPDATE clients SET plan=?, note=?, created_unix=? WHERE email=?`, plan, "Restore", created, email); err != nil {
				return rep, err
			}
			if used := c.up + c.down; used > 0 {
				if _, err := tx.ExecContext(ctx, `INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,?,?,?)`, email, c.up, c.down, created, created); err != nil {
					return rep, err
				}
				rep.Usage++
			}
			if err := addGroup("", "Restore", email); err != nil {
				return rep, err
			}
			if u, err := url.Parse(c.legacyURL); err == nil && u.Host != "" {
				domains[u.Host] = true
			}
			rep.Restore++
			if !c.enabled {
				rep.Disabled++
			}
		}
	}
	var origin string
	_ = old.QueryRowContext(ctx, `SELECT v FROM export_meta WHERE k='public_origin'`).Scan(&origin)
	if u, err := url.Parse(origin); err == nil && u.Host != "" {
		domains[u.Host] = true
	}
	for d := range domains {
		rep.Domains = append(rep.Domains, d)
	}
	sort.Strings(rep.Domains)
	if opt.DryRun {
		return rep, nil
	}
	if err := tx.Commit(); err != nil {
		return rep, err
	}
	_ = s.SetSetting(ctx, "darkold_imported", strconv.FormatInt(now, 10))
	return rep, nil
}

// darkOldScryptMatch checks "scrypt$16384$8$1$<salt hex>$<hash hex>" (the
// DARK XRAY panel's login hashes).
func darkOldScryptMatch(stored, pass string) bool {
	p := strings.Split(stored, "$")
	if len(p) != 6 || p[0] != "scrypt" {
		return false
	}
	n, e1 := strconv.Atoi(p[1])
	r, e2 := strconv.Atoi(p[2])
	q, e3 := strconv.Atoi(p[3])
	salt, e4 := hex.DecodeString(p[4])
	want, e5 := hex.DecodeString(p[5])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || n > 1<<20 || r > 32 || q > 16 || len(want) == 0 {
		return false
	}
	got, err := scrypt.Key([]byte(pass), salt, n, r, q, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
