package hubcore

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Migration from the old Dark Xray Pro panel (the "business" SQLite store).
// The old database is never written: it is copied first (main file + WAL)
// and only the copy is read. Everything lands in one v2 transaction, so a
// preview (dry run) shows exactly what an import would do and an import
// either completes or changes nothing.
//
// Moved: resellers and sub-sellers with their credit, clients (UUID kept,
// so configs built from it keep their identity), plans, IP limits, expiry
// or first-connection terms, usage totals, groups, panel logins (old
// passwords keep working and are upgraded on first sign-in), and active
// subscription links — old /api/v1/... subscription URLs answer with the
// client's v2 subscription. Not moved: old inbounds and nodes (v2 has its
// own; clients are attached to the inbounds chosen for the import) and
// per-event history (credit ledger and operation logs; totals are kept).

// DefaultOldPanelDB is where the old panel keeps its database on a HUB
// installed by the old installer.
const DefaultOldPanelDB = "/var/lib/dark-xray-pro-lab/review/business.sqlite"

type MigrateOptions struct {
	Inbounds        []string `json:"inbounds"`         // v2 inbounds every moved client gets
	IncludeArchived bool     `json:"include_archived"` // also move archived clients (disabled)
	DryRun          bool     `json:"dry_run"`
}

type MigrateReport struct {
	DryRun   bool     `json:"dry_run"`
	Reps     int      `json:"reps"`
	Sellers  int      `json:"sellers"`
	Clients  int      `json:"clients"`
	Disabled int      `json:"disabled"` // of those, moved disabled (archived/disabled in the old panel)
	Usage    int      `json:"usage"`    // clients with usage carried over
	Groups   int      `json:"groups"`
	Logins   int      `json:"logins"`
	Links    int      `json:"links"` // old subscription URLs that keep working
	Skipped  []string `json:"skipped"`
	Notes    []string `json:"notes"`
}

func (r *MigrateReport) skip(f string, a ...any) { r.Skipped = append(r.Skipped, fmt.Sprintf(f, a...)) }

// CopyOldDatabase makes a private, consistent copy of a live SQLite file
// (main + WAL) in dir and returns its path. The source is only read.
func CopyOldDatabase(src, dir string) (string, error) {
	dst := filepath.Join(dir, "old-panel-copy.sqlite")
	for _, ext := range []string{"", "-wal"} {
		_ = os.Remove(dst + ext)
		in, err := os.Open(src + ext)
		if err != nil {
			if ext != "" && os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		out, err := os.OpenFile(dst+ext, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			in.Close()
			return "", err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", err
		}
	}
	return dst, nil
}

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var oldNameRE = regexp.MustCompile(`[^A-Za-z0-9._@+-]+`)

// v2Email turns an old client id into a valid v2 name.
func v2Email(id string) string {
	e := strings.Trim(oldNameRE.ReplaceAllString(id, "-"), "-.")
	if len(e) > 128 {
		e = e[:128]
	}
	if e == "" || !emailRE.MatchString(e) {
		h := sha256.Sum256([]byte(id))
		e = "old-" + hex.EncodeToString(h[:4])
	}
	return e
}

var oldIDRE = regexp.MustCompile(`[^a-z0-9._-]+`)

func v2ID(id string) string {
	x := strings.Trim(oldIDRE.ReplaceAllString(strings.ToLower(id), "-"), "-.")
	if len(x) > 63 {
		x = x[:63]
	}
	if x == "" || !idRE.MatchString(x) {
		h := sha256.Sum256([]byte(id))
		x = "old-" + hex.EncodeToString(h[:4])
	}
	return x
}

func tableExists(ctx context.Context, db *sql.DB, name string) bool {
	var n int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	return n > 0
}

// MigrateFromOld imports an old-panel database copy (see CopyOldDatabase).
func (s *Store) MigrateFromOld(ctx context.Context, oldPath string, opt MigrateOptions) (MigrateReport, error) {
	rep := MigrateReport{DryRun: opt.DryRun, Skipped: []string{}, Notes: []string{}}
	u := url.URL{Scheme: "file", Path: oldPath, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	old, err := sql.Open("sqlite", u.String())
	if err != nil {
		return rep, err
	}
	defer old.Close()
	for _, t := range []string{"clients", "representatives", "auth_users"} {
		if !tableExists(ctx, old, t) {
			return rep, fmt.Errorf("%w: this is not an old Dark Xray Pro database (no %s table)", ErrInvalid, t)
		}
	}
	for _, tag := range opt.Inbounds {
		var n int
		if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbounds WHERE tag=?`, tag).Scan(&n); err != nil || n == 0 {
			return rep, fmt.Errorf("%w: inbound %q does not exist", ErrInvalid, tag)
		}
	}
	if len(opt.Inbounds) == 0 {
		rep.Notes = append(rep.Notes, "no inbounds chosen: clients are moved but get no connections until you assign inbounds")
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()

	// Resellers. The old owner pseudo-representative becomes "no reseller".
	repMap := map[string]string{"owner": "", "": ""}
	rows, err := old.QueryContext(ctx, `SELECT id,display_name,kind,enabled,volume_credit_bytes,unlimited_credit_units FROM representatives ORDER BY id`)
	if err != nil {
		return rep, err
	}
	type oldRep struct {
		id, name, kind string
		enabled        bool
		vol, units     int64
	}
	var reps []oldRep
	for rows.Next() {
		var r oldRep
		if err := rows.Scan(&r.id, &r.name, &r.kind, &r.enabled, &r.vol, &r.units); err != nil {
			rows.Close()
			return rep, err
		}
		reps = append(reps, r)
	}
	rows.Close()
	suspended := map[string]bool{}
	if tableExists(ctx, old, "representative_controls") {
		r2, err := old.QueryContext(ctx, `SELECT representative_id,login_suspended FROM representative_controls`)
		if err == nil {
			for r2.Next() {
				var id string
				var s bool
				if r2.Scan(&id, &s) == nil {
					suspended[id] = s
				}
			}
			r2.Close()
		}
	}
	for _, r := range reps {
		if r.kind == "owner" || r.id == "owner" {
			continue
		}
		id := v2ID(r.id)
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, id).Scan(&exists)
		if exists > 0 {
			rep.skip("reseller %s: a reseller with id %s already exists", r.id, id)
			repMap[r.id] = id
			continue
		}
		name := r.name
		if name == "" {
			name = r.id
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reps(id,name,enabled,volume_credit,unlimited_units,created_unix) VALUES(?,?,?,?,?,?)`,
			id, name, r.enabled && !suspended[r.id], max(r.vol, 0), max(r.units, 0), now); err != nil {
			return rep, fmt.Errorf("reseller %s: %w", r.id, err)
		}
		if r.vol > 0 || r.units > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(unix,rep_id,actor,kind,volume_delta,units_delta,note) VALUES(?,?,?,?,?,?,?)`,
				now, id, "migration", "adjust", r.vol, r.units, "credit moved from the old panel"); err != nil {
				return rep, err
			}
		}
		repMap[r.id] = id
		rep.Reps++
	}
	// Sub-sellers (under a reseller).
	if tableExists(ctx, old, "representative_sellers_v2") {
		r3, err := old.QueryContext(ctx, `SELECT id,representative_id,name,enabled,reserved_bytes,reserved_units FROM representative_sellers_v2`)
		if err != nil {
			return rep, err
		}
		type seller struct {
			id, parent, name string
			enabled          bool
			vol, units       int64
		}
		var sellers []seller
		for r3.Next() {
			var x seller
			if err := r3.Scan(&x.id, &x.parent, &x.name, &x.enabled, &x.vol, &x.units); err != nil {
				r3.Close()
				return rep, err
			}
			sellers = append(sellers, x)
		}
		r3.Close()
		for _, x := range sellers {
			parent, ok := repMap[x.parent]
			if !ok || parent == "" {
				rep.skip("sub-seller %s: its reseller %s was not moved", x.id, x.parent)
				continue
			}
			id := v2ID(x.id)
			var exists int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, id).Scan(&exists)
			if exists > 0 {
				rep.skip("sub-seller %s: id %s already exists", x.id, id)
				repMap[x.id] = id
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO reps(id,name,enabled,volume_credit,unlimited_units,created_unix,parent_id) VALUES(?,?,?,?,?,?,?)`,
				id, or2(x.name, x.id), x.enabled, max(x.vol, 0), max(x.units, 0), now, parent); err != nil {
				return rep, fmt.Errorf("sub-seller %s: %w", x.id, err)
			}
			repMap[x.id] = id
			rep.Sellers++
		}
	}

	// Clients.
	where := "WHERE c.archived=0"
	if opt.IncludeArchived {
		where = ""
	}
	usageJoin, usageCols := "", "0,0"
	if tableExists(ctx, old, "pro_usage_accounts") {
		usageJoin, usageCols = "LEFT JOIN pro_usage_accounts u ON u.client_id=c.id", "COALESCE(u.uplink,0),COALESCE(u.downlink,0)"
	}
	cr, err := old.QueryContext(ctx, `SELECT c.id,c.representative_id,c.display_name,c.uuid,c.plan_kind,c.ip_count,c.volume_bytes,c.duration_days,
c.created_unix,c.expires_unix,c.deployment_state,c.archived,`+usageCols+` FROM clients c `+usageJoin+` `+where+` ORDER BY c.created_unix`)
	if err != nil {
		return rep, err
	}
	type oldClient struct {
		id, repID, name, uuid, plan, state string
		ips                                int
		vol, days, created, expires        int64
		archived                           bool
		up, down                           int64
	}
	var clients []oldClient
	for cr.Next() {
		var c oldClient
		if err := cr.Scan(&c.id, &c.repID, &c.name, &c.uuid, &c.plan, &c.ips, &c.vol, &c.days, &c.created, &c.expires, &c.state, &c.archived, &c.up, &c.down); err != nil {
			cr.Close()
			return rep, err
		}
		clients = append(clients, c)
	}
	cr.Close()
	emailOf := map[string]string{}
	for _, c := range clients {
		email := v2Email(c.id)
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, email).Scan(&exists)
		if exists > 0 {
			rep.skip("client %s: %s already exists in this panel", c.id, email)
			continue
		}
		repID, ok := repMap[c.repID]
		if !ok {
			repID = ""
			rep.Notes = append(rep.Notes, fmt.Sprintf("client %s: reseller %s was not moved, the client belongs to the owner", c.id, c.repID))
		}
		enabled := !c.archived && c.state != "disabled"
		cl := Client{Email: email, UUID: strings.ToLower(c.uuid), Enabled: enabled, VolumeBytes: c.vol, MaxIPs: max(c.ips, 0), Inbounds: opt.Inbounds}
		if c.plan == "unlimited" || c.vol <= 0 {
			cl.VolumeBytes = -1
		}
		if c.expires > 0 {
			cl.ExpiresUnix = c.expires
		} else if c.days > 0 {
			cl.DurationSeconds = c.days * 86400
		}
		if !uuidRE.MatchString(cl.UUID) {
			cl.UUID = ""
		}
		if err := putClientTx(ctx, tx, cl, now); err != nil {
			return rep, fmt.Errorf("client %s: %w", c.id, err)
		}
		plan := "volume"
		if c.plan == "unlimited" {
			plan = "unlimited"
		}
		note := ""
		if c.name != "" && c.name != c.id {
			note = c.name
		}
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET rep_id=?, plan=?, note=?, created_unix=? WHERE email=?`, repID, plan, note, max(c.created, 1), email); err != nil {
			return rep, err
		}
		if c.up+c.down > 0 {
			seen := c.created
			if _, err := tx.ExecContext(ctx, `INSERT INTO client_usage(email,up,down,first_seen_unix,last_seen_unix) VALUES(?,?,?,?,?)`, email, c.up, c.down, seen, seen); err != nil {
				return rep, err
			}
			rep.Usage++
		}
		emailOf[c.id] = email
		rep.Clients++
		if !enabled {
			rep.Disabled++
		}
	}

	// Groups (owner-level in v2; the old reseller groups keep their names).
	if tableExists(ctx, old, "client_groups") {
		gr, err := old.QueryContext(ctx, `SELECT id,name FROM client_groups`)
		if err != nil {
			return rep, err
		}
		type grp struct{ id, name string }
		var groups []grp
		for gr.Next() {
			var g grp
			if gr.Scan(&g.id, &g.name) == nil {
				groups = append(groups, g)
			}
		}
		gr.Close()
		for _, g := range groups {
			id := v2ID("old-" + g.id)
			if _, err := tx.ExecContext(ctx, `INSERT INTO client_groups(id,name,created_unix) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, id, or2(g.name, g.id), now); err != nil {
				return rep, err
			}
			mr, err := old.QueryContext(ctx, `SELECT client_id FROM client_group_members WHERE group_id=?`, g.id)
			if err != nil {
				return rep, err
			}
			var members []string
			for mr.Next() {
				var cid string
				if mr.Scan(&cid) == nil {
					members = append(members, cid)
				}
			}
			mr.Close()
			for _, cid := range members {
				if e, ok := emailOf[cid]; ok {
					if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO client_group_members(group_id,email) VALUES(?,?)`, id, e); err != nil {
						return rep, err
					}
				}
			}
			rep.Groups++
		}
	}

	// Panel logins: the old argon2id hash is kept; Login verifies it and
	// replaces it with bcrypt on the first successful sign-in.
	lr, err := old.QueryContext(ctx, `SELECT username,representative_id,role,password_salt,password_hash,disabled FROM auth_users`)
	if err != nil {
		return rep, err
	}
	type login struct {
		user, repID, role string
		salt, hash        []byte
		disabled          bool
	}
	var logins []login
	for lr.Next() {
		var l login
		if err := lr.Scan(&l.user, &l.repID, &l.role, &l.salt, &l.hash, &l.disabled); err != nil {
			lr.Close()
			return rep, err
		}
		logins = append(logins, l)
	}
	lr.Close()
	for _, l := range logins {
		if l.disabled {
			rep.skip("login %s: disabled in the old panel", l.user)
			continue
		}
		user := v2ID(l.user)
		var exists int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE username=?`, user).Scan(&exists)
		if exists > 0 {
			rep.skip("login %s: %s already exists here (keep using the v2 password)", l.user, user)
			continue
		}
		role, repID := "owner", ""
		if l.role != "owner" {
			r, ok := repMap[l.repID]
			if !ok || r == "" {
				rep.skip("login %s: its reseller was not moved", l.user)
				continue
			}
			role, repID = "rep", r
		}
		hash := "argon2id:" + base64.RawStdEncoding.EncodeToString(l.salt) + ":" + base64.RawStdEncoding.EncodeToString(l.hash)
		if _, err := tx.ExecContext(ctx, `INSERT INTO admins(username,pass_hash,created_unix,role,rep_id) VALUES(?,?,?,?,?)`, user, hash, now, role, repID); err != nil {
			return rep, fmt.Errorf("login %s: %w", l.user, err)
		}
		if user != l.user {
			rep.Notes = append(rep.Notes, fmt.Sprintf("login %s signs in as %s", l.user, user))
		}
		rep.Logins++
	}

	// Active subscription links.
	if tableExists(ctx, old, "inbound_execution_subscription_heads") {
		sr, err := old.QueryContext(ctx, `SELECT h.client_id,c.token_sha FROM inbound_execution_subscription_heads h
JOIN inbound_execution_subscription_credentials c ON c.id=h.credential_id WHERE h.state='active'`)
		if err == nil {
			type link struct{ client, sha string }
			var links []link
			for sr.Next() {
				var l link
				if sr.Scan(&l.client, &l.sha) == nil {
					links = append(links, l)
				}
			}
			sr.Close()
			for _, l := range links {
				e, ok := emailOf[l.client]
				if !ok {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO legacy_sub_tokens(token_sha,kind,email) VALUES(?,?,?)`, l.sha, "managed", e); err != nil {
					return rep, err
				}
				rep.Links++
			}
		}
	}
	if opt.DryRun {
		return rep, nil // rollback by defer
	}
	if err := tx.Commit(); err != nil {
		return rep, err
	}
	return rep, nil
}

const legacySchema = `
CREATE TABLE IF NOT EXISTS legacy_sub_tokens (
  token_sha TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  email TEXT NOT NULL REFERENCES clients(email) ON DELETE CASCADE ON UPDATE CASCADE
);
`

func (s *Store) ensureLegacySchema() error {
	_, err := s.db.Exec(legacySchema)
	return err
}

// Old token hashes (see internal/business): managed inbound subscriptions
// and lab subscriptions.
func legacyTokenSHA(kind, token string) string {
	prefix := "DARK-PRO-MANAGED-INBOUND-SUBSCRIPTION-v1\x00"
	if kind == "lab" {
		prefix = "DARK-PRO-LAB-SUBSCRIPTION-TOKEN-v1\x00"
	}
	h := sha256.Sum256([]byte(prefix + token))
	return hex.EncodeToString(h[:])
}

// legacySubToken maps an old subscription token to the client's v2 token.
func (s *Store) legacySubToken(ctx context.Context, kind, token string) (string, error) {
	if len(token) != 43 {
		return "", ErrNotFound
	}
	var v2 string
	err := s.ro.QueryRowContext(ctx, `SELECT c.sub_token FROM legacy_sub_tokens l JOIN clients c ON c.email=l.email WHERE l.token_sha=? AND l.kind=?`,
		legacyTokenSHA(kind, token), kind).Scan(&v2)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v2, err
}

// oldArgonMatch checks a password against an "argon2id:<salt>:<hash>"
// value with the old panel's parameters (3 passes, 32 MiB, 2 lanes).
func oldArgonMatch(stored, pass string) bool {
	parts := strings.Split(stored, ":")
	if len(parts) != 3 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[1])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pass), salt, 3, 32*1024, 2, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
