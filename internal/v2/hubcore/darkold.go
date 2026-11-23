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
	Uploaded  bool           `json:"uploaded"`
	At        int64          `json:"at,omitempty"`
	Origin    string         `json:"origin,omitempty"`
	Version   string         `json:"version,omitempty"`
	Counts    map[string]int `json:"counts,omitempty"`
	Resellers []string       `json:"resellers,omitempty"`
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
	if rr, err := db.QueryContext(ctx, `SELECT id FROM api_admins WHERE role='reseller' ORDER BY id`); err == nil {
		for rr.Next() {
			var id string
			if rr.Scan(&id) == nil {
				st.Resellers = append(st.Resellers, id)
			}
		}
		rr.Close()
	}
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
	// IncludeRestore also moves the Dark Restore clients (old 3x-ui links);
	// off by default — Dark Restore is retired.
	IncludeRestore bool `json:"include_restore"`
	// OwnerBot makes the owner's old bot the panel's main bot, so the
	// owner's customers keep the bot they know.
	OwnerBot bool `json:"owner_bot"`
	// DropReps are old reseller usernames left behind together with all
	// their clients.
	DropReps []string `json:"drop_reps"`
	DryRun   bool     `json:"dry_run"`
}

type DarkOldReport struct {
	DryRun     bool            `json:"dry_run"`
	Reps       int             `json:"reps"`
	Logins     int             `json:"logins"`
	Clients    int             `json:"clients"`
	Restore    int             `json:"restore"`
	Disabled   int             `json:"disabled"`
	Ended      int             `json:"ended"` // expired or used up (switched off in the old panel; enforced here)
	Usage      int             `json:"usage"`
	Links      int             `json:"links"`
	Telegram   int             `json:"telegram"` // clients linked to a Telegram account
	Groups     int             `json:"groups"`
	Cards      int             `json:"cards"`              // sellers' cards for card-to-card payment
	Bots       int             `json:"bots"`               // resellers' own Telegram bots
	Wallets    int             `json:"wallets"`            // customers' toman wallets with a balance
	Products   int             `json:"products"`           // plans for sale
	Dropped    int             `json:"dropped"`            // clients of left-behind resellers
	Credit     []DarkOldCredit `json:"credit"`             // how each reseller's credit was worked out
	Orders     int             `json:"orders"`             // old orders kept as history
	Topups     int             `json:"topups"`             // old wallet top-ups kept as history
	WalletRows int             `json:"wallet_rows"`        // old wallet history rows
	LedgerRows int             `json:"ledger_rows"`        // old credit history rows
	Unsettled  int             `json:"unsettled"`          // orders/top-ups left open in the old panel (settled by hand, left out)
	MainBot    string          `json:"main_bot,omitempty"` // the owner's old bot now serving as the main bot
	Domains    []string        `json:"domains"`            // domains to point here so old links keep working
	Skipped    []string        `json:"skipped"`
	Notes      []string        `json:"notes"`
}

// DarkOldCredit explains one reseller's moved volume credit (bytes): the
// old panel's free capacity plus the unused volume of expired clients.
type DarkOldCredit struct {
	Rep      string `json:"rep"`
	Cap      int64  `json:"cap"`
	InUse    int64  `json:"in_use"`
	Expired  int64  `json:"expired_back"`
	Credit   int64  `json:"credit"`
	OldUnits int64  `json:"old_units"` // unlimited units left in the old panel (not moved)
	Lifetime int64  `json:"lifetime"`  // total traffic of the reseller's users ever
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
	rows, err := old.QueryContext(ctx, `SELECT a.id,a.role,a.password_hash,a.disabled,COALESCE(p.name,''),COALESCE(o.volume_credit_bytes,0),COALESCE(o.unlimited_credit,0)-COALESCE(o.unlimited_spent,0),COALESCE(o.account_disabled,0)
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
	drop := map[string]bool{}
	for _, d := range opt.DropReps {
		if d = strings.TrimSpace(d); d != "" {
			drop[d] = true
		}
	}
	// The old panel's capacity in use: the volume of every client it holds.
	alloc := map[string]int64{}
	if r2, err := old.QueryContext(ctx, `SELECT owner,COALESCE(SUM(CASE WHEN quota_bytes>0 THEN quota_bytes ELSE 0 END),0) FROM clients GROUP BY owner`); err == nil {
		for r2.Next() {
			var o string
			var b int64
			if r2.Scan(&o, &b) == nil {
				alloc[o] = b
			}
		}
		r2.Close()
	}
	lifetime := map[string]int64{}
	if tableExists(ctx, old, "traffic_totals") {
		if r2, err := old.QueryContext(ctx, `SELECT owner,SUM(up_bytes+down_bytes) FROM traffic_totals GROUP BY owner`); err == nil {
			for r2.Next() {
				var o string
				var b int64
				if r2.Scan(&o, &b) == nil {
					lifetime[o] = b
				}
			}
			r2.Close()
		}
	}
	repOf := map[string]string{}         // old owner id -> v2 rep id ("" = the owner)
	fresh := map[string]*DarkOldCredit{} // resellers created by this import
	for _, o := range owners {
		if o.role == "owner" {
			repOf[o.id] = ""
			continue
		}
		if drop[o.id] {
			rep.note("reseller %s is left behind with all its clients", o.id)
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
			// Credit here is spent on every sale, so only the old panel's free
			// capacity moves (expired clients' unused volume is added below).
			// Unlimited units are not moved: resellers sell volume only.
			cr := &DarkOldCredit{Rep: id, Cap: max(o.volume, 0), InUse: alloc[o.id], OldUnits: max(o.units, 0), Lifetime: lifetime[o.id]}
			cr.Credit = max(cr.Cap-cr.InUse, 0)
			if _, err := tx.ExecContext(ctx, `INSERT INTO reps(id,name,enabled,volume_credit,unlimited_units,created_unix) VALUES(?,?,?,?,0,?)`,
				id, name, !o.disabled, cr.Credit, now); err != nil {
				return rep, fmt.Errorf("reseller %s: %w", o.id, err)
			}
			fresh[id] = cr
			rep.Reps++
		} else {
			rep.note("reseller %s already exists here; its clients join it", id)
		}
		repOf[o.id] = id
	}
	// Logins keep their passwords (scrypt, upgraded to bcrypt on first sign-in).
	for _, o := range owners {
		if drop[o.id] {
			continue
		}
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
	rows, err = old.QueryContext(ctx, `SELECT c.id,c.owner,c.limit_ip,c.quota_bytes,c.expires_at,m.public_token,m.state,m.external_disabled,m.created_at,k.body,k.up,k.down,COALESCE(c.used_bytes,0)
FROM clients c JOIN managed_clients m ON m.email=c.id JOIN core_clients k ON k.email=c.id ORDER BY m.created_at`)
	if err != nil {
		return rep, err
	}
	for rows.Next() {
		var c oc
		var used int64
		if err := rows.Scan(&c.email, &c.owner, &c.limitIP, &c.quota, &c.expires, &c.token, &c.state, &c.extDisabled, &c.created, &c.body, &c.up, &c.down, &used); err != nil {
			rows.Close()
			return rep, err
		}
		// clients.used_bytes is what the old panel metered across every node
		// (and enforced); core_clients only counts the local core.
		if used > c.up+c.down {
			c.down += used - c.up - c.down
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
		if drop[c.owner] {
			rep.Dropped++
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
		if !enabled && !c.extDisabled {
			// The old panel switches clients off when they expire or run out;
			// here the panel enforces that itself, and a renewal must just work.
			if (c.expires > 0 && c.expires < now) || (c.quota > 0 && c.up+c.down >= c.quota) {
				enabled = true
				rep.Ended++
			}
		}
		vol := c.quota
		if vol <= 0 {
			vol = -1
		}
		// A reseller's expired volume client gives its unused volume back
		// (as the panel does from now on); it keeps only what it used, so a
		// renewal adds new, paid volume.
		if cr := fresh[repOf[c.owner]]; cr != nil && vol > 0 && !unl[c.email] && c.expires > 0 && c.expires < now {
			if used := max(c.up+c.down, 1); used < vol {
				cr.Expired += vol - used
				vol = used
			}
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

	if err := s.darkOldCredit(ctx, tx, old, fresh, repOf, now, &rep); err != nil {
		return rep, err
	}

	// Restore clients: users of the old 3x-ui panels, reached by their old
	// links (/sub/<id> on the old domains).
	domains := map[string]bool{}
	if opt.IncludeRestore && tableExists(ctx, old, "restore_subscriptions") {
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
	if err := s.darkOldShop(ctx, tx, old, repOf, opt, now, &rep); err != nil {
		return rep, err
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
	// The owner's old bot (applied after the import is saved).
	var ownerTok, ownerUser string
	var ownerAdmin int64
	if opt.OwnerBot && tableExists(ctx, old, "export_bot_tokens") {
		_ = old.QueryRowContext(ctx, `SELECT b.token,b.bot_username,b.admin_telegram_id FROM export_bot_tokens b JOIN api_admins a ON a.id=b.owner WHERE a.role='owner' LIMIT 1`).Scan(&ownerTok, &ownerUser, &ownerAdmin)
		if ownerTok == "" {
			rep.note("the owner had no bot in the old panel")
		} else {
			rep.MainBot = "@" + ownerUser
		}
	}
	if opt.DryRun {
		return rep, nil
	}
	if err := tx.Commit(); err != nil {
		return rep, err
	}
	_ = s.SetSetting(ctx, "darkold_imported", strconv.FormatInt(now, 10))
	if ownerTok != "" {
		owners := s.TelegramOwners(ctx)
		if ownerAdmin > 0 && !slicesContains(owners, ownerAdmin) {
			owners = append(owners, ownerAdmin)
		}
		if c := telegramControl(); c.Apply != nil {
			if user, err := c.Apply(ctx, ownerTok, owners); err != nil {
				rep.MainBot = ""
				rep.note("the owner's old bot could not be started: %v — set it in Settings → Telegram", err)
			} else {
				rep.MainBot = "@" + user
			}
		}
	}
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

func setSettingTx(ctx context.Context, tx *sql.Tx, k, v string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

// darkOldShop moves the sellers' cards, bots, wallets and plans.
func (s *Store) darkOldShop(ctx context.Context, tx *sql.Tx, old *sql.DB, repOf map[string]string, opt DarkOldOptions, now int64, rep *DarkOldReport) error {
	seller := func(owner string) (string, bool) {
		r, ok := repOf[owner]
		return r, ok
	}
	// Cards (manual card-to-card gateways).
	if tableExists(ctx, old, "commerce_gateways") {
		rows, err := old.QueryContext(ctx, `SELECT owner,card_number,card_holder,bank_name FROM commerce_gateways WHERE enabled=1 AND card_number<>'' ORDER BY updated_at`)
		if err != nil {
			return err
		}
		type card struct{ owner, num, holder, bank string }
		var cards []card
		for rows.Next() {
			var c card
			if rows.Scan(&c.owner, &c.num, &c.holder, &c.bank) == nil {
				cards = append(cards, c)
			}
		}
		rows.Close()
		done := map[string]bool{}
		for _, c := range cards {
			sl, ok := seller(c.owner)
			if !ok || done[sl] {
				continue
			}
			if s.CardFor(ctx, sl).Number != "" {
				rep.note("card of %s: one is already set here; kept", firstNonEmpty(sl, "owner"))
				done[sl] = true
				continue
			}
			num := strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, c.num)
			if len(num) < 16 || len(num) > 26 {
				continue
			}
			for k, v := range map[string]string{"pay_card_number": num, "pay_card_holder": c.holder, "pay_card_bank": c.bank} {
				if err := setSettingTx(ctx, tx, cardKey(k, sl), strings.TrimSpace(v)); err != nil {
					return err
				}
			}
			done[sl] = true
			rep.Cards++
		}
	}
	// Resellers' own bots (the owner's bot stays the panel's main bot).
	if tableExists(ctx, old, "export_bot_tokens") {
		rows, err := old.QueryContext(ctx, `SELECT owner,token,enabled,admin_telegram_id FROM export_bot_tokens`)
		if err != nil {
			return err
		}
		type bot struct {
			owner, token string
			enabled      bool
			admin        int64
		}
		var bots []bot
		for rows.Next() {
			var b bot
			if rows.Scan(&b.owner, &b.token, &b.enabled, &b.admin) == nil {
				bots = append(bots, b)
			}
		}
		rows.Close()
		for _, b := range bots {
			sl, ok := seller(b.owner)
			if !ok {
				continue
			}
			if sl == "" {
				rep.note("the owner's old bot is not moved: the panel's main bot (Settings → Telegram) serves the owner")
				continue
			}
			if !botTokenRE.MatchString(b.token) {
				continue
			}
			if cur := s.repBotRaw(ctx, sl); cur.Token != "" {
				rep.note("bot of %s: one is already set here; kept", sl)
				continue
			}
			v, _ := json.Marshal(RepBot{Token: b.token, Enabled: b.enabled})
			if err := setSettingTx(ctx, tx, "rep_bot:"+sl, string(v)); err != nil {
				return err
			}
			if b.admin > 0 {
				if _, err := tx.ExecContext(ctx, `UPDATE reps SET tg_chat=? WHERE id=? AND tg_chat=0`, b.admin, sl); err != nil {
					return err
				}
			}
			rep.Bots++
		}
	}
	// Customers' toman wallets, with their history.
	if err := s.darkOldWallets(ctx, tx, old, repOf, rep); err != nil {
		return err
	}
	// Plans: each old price becomes one product.
	if tableExists(ctx, old, "commerce_prices") && tableExists(ctx, old, "commerce_products") {
		rows, err := old.QueryContext(ctx, `SELECT p.id,p.owner,pr.name,pr.kind,pr.active,pr.visible,p.label,p.price_minor,p.duration_days,p.volume_bytes,p.ip_limit,p.active,COALESCE(p.activation_mode,''),
COALESCE(pr.category,''),COALESCE(pr.renewal_enabled,1),COALESCE(pr.sale_limit_per_user,0)
FROM commerce_prices p JOIN commerce_products pr ON pr.id=p.product_id AND pr.owner=p.owner ORDER BY p.owner,p.price_minor`)
		if err != nil {
			return err
		}
		type pr struct {
			id, owner, name, kind, label, mode, cat string
			renewable                               bool
			limit                                   int
			pActive, pVisible, active               bool
			price, days, vol                        int64
			ips                                     int
		}
		var list []pr
		for rows.Next() {
			var x pr
			if err := rows.Scan(&x.id, &x.owner, &x.name, &x.kind, &x.pActive, &x.pVisible, &x.label, &x.price, &x.days, &x.vol, &x.ips, &x.active, &x.mode, &x.cat, &x.renewable, &x.limit); err == nil {
				list = append(list, x)
			}
		}
		rows.Close()
		for _, x := range list {
			sl, ok := seller(x.owner)
			if !ok || x.days <= 0 || x.days > 3650 {
				continue
			}
			id := v2ID("old-" + x.owner + "-" + x.id)
			var n int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM products WHERE id=?`, id).Scan(&n)
			if n > 0 {
				continue
			}
			plan, gb := "volume", x.vol>>30
			if x.kind == "unlimited" || x.vol <= 0 {
				plan, gb = "unlimited", 0
			}
			status := "draft"
			if x.active && x.pActive && x.pVisible {
				status = "published"
			}
			if sl != "" && plan == "unlimited" {
				status = "draft" // resellers sell volume only for now
			}
			name := strings.TrimSpace(x.name)
			if l := strings.TrimSpace(x.label); l != "" && l != name {
				name = strings.TrimSpace(name + " · " + l)
			}
			if len(name) > 120 {
				name = name[:120]
			}
			cat := strings.TrimSpace(x.cat)
			if cat == "General" || len(cat) > 40 {
				cat = ""
			}
			rowid, err := newRowID(ctx, tx, "products")
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO products(rowid,id,seller,name,plan,gb,days,max_ips,inbounds,price,status,first_use,created_unix,category,no_renew,per_buyer) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				rowid, id, sl, name, plan, gb, x.days, max(x.ips, 0), strings.Join(opt.Inbounds, ","), max(x.price, 0), status, x.mode == "first_connection", now,
				cat, !x.renewable, max(min(x.limit, 1000), 0)); err != nil {
				return fmt.Errorf("product %s: %w", x.id, err)
			}
			rep.Products++
		}
	}
	return s.darkOldHistory(ctx, tx, old, repOf, rep)
}

func slicesContains(list []int64, v int64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
