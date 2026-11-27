package hubcore

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The partner API: a key that makes and runs reseller panels and nothing
// else, so another bot or a website can sell reseller panels on its own.
// The owner makes the key in the panel and gives it limits:
//
//   - the most credit (GB and unlimited-plan units) it may ever hand out,
//     counted over all panels it made;
//   - a template reseller whose rules (limits, inbounds, prices) every new
//     panel starts from (none = the default rules), and the packages of the
//     reseller sale it may sell; a key can only tighten those rules;
//   - scopes: deleting panels, taking credit back and reading the panels'
//     customers are off unless the owner turns them on;
//   - optionally the addresses it may be used from, an expiry and a rate.
//
// A key sees and changes only the panels it made itself. Format:
// dp_<id>_<secret>; only the secret's hash is kept. The routes are listed
// in partnerapi.go; the shared rules (errors, Idempotency-Key, rates,
// events) in extapi.go.

const partnerSchema = `
CREATE TABLE IF NOT EXISTS partner_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  secret_sha256 TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  max_bytes INTEGER NOT NULL DEFAULT 0,
  max_units INTEGER NOT NULL DEFAULT 0,
  given_bytes INTEGER NOT NULL DEFAULT 0,
  given_units INTEGER NOT NULL DEFAULT 0,
  template TEXT NOT NULL DEFAULT '',
  created_unix INTEGER NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0,
  last_ip TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS partner_reps (
  rep_id TEXT PRIMARY KEY,
  key_id TEXT NOT NULL,
  created_unix INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS partner_reps_key ON partner_reps(key_id);
`

func (s *Store) ensurePartnerSchema() error {
	if _, err := s.db.Exec(partnerSchema); err != nil {
		return err
	}
	for _, c := range []struct{ col, def string }{
		{"scopes", "TEXT NOT NULL DEFAULT '" + strings.Join(partnerDefaultScopes, ",") + "'"},
		{"allowed_ips", "TEXT NOT NULL DEFAULT ''"},
		{"expires_unix", "INTEGER NOT NULL DEFAULT 0"},
		{"rate_per_min", "INTEGER NOT NULL DEFAULT 0"},
		{"packages", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumn("partner_keys", c.col, c.def); err != nil {
			return err
		}
	}
	hadGiven := s.hasColumn("partner_reps", "given_bytes")
	for _, c := range []struct{ col, def string }{
		{"external_ref", "TEXT NOT NULL DEFAULT ''"},
		{"note", "TEXT NOT NULL DEFAULT ''"},
		{"package", "TEXT NOT NULL DEFAULT ''"},
		{"ceiling", "TEXT NOT NULL DEFAULT ''"},
		{"given_bytes", "INTEGER NOT NULL DEFAULT 0"},
		{"given_units", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.addColumn("partner_reps", c.col, c.def); err != nil {
			return err
		}
	}
	// Rows of resellers deleted before deleting a reseller cleaned them up.
	if _, err := s.db.Exec(`DELETE FROM partner_reps WHERE rep_id NOT IN (SELECT id FROM reps)`); err != nil {
		return err
	}
	if !hadGiven {
		// What each key gave each panel so far, from the credit ledger.
		if _, err := s.db.Exec(`UPDATE partner_reps SET
given_bytes=MAX(0,COALESCE((SELECT SUM(l.volume_delta) FROM credit_ledger l WHERE l.rep_id=partner_reps.rep_id AND l.kind='adjust' AND l.actor LIKE 'partner:%'),0)),
given_units=MAX(0,COALESCE((SELECT SUM(l.units_delta) FROM credit_ledger l WHERE l.rep_id=partner_reps.rep_id AND l.kind='adjust' AND l.actor LIKE 'partner:%'),0))`); err != nil {
			return err
		}
	}
	// external_ref is unique per key; should two panels share one, the
	// older keeps it.
	if _, err := s.db.Exec(`UPDATE partner_reps SET external_ref='' WHERE external_ref<>'' AND rowid NOT IN
(SELECT MIN(rowid) FROM partner_reps WHERE external_ref<>'' GROUP BY key_id, external_ref)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DROP INDEX IF EXISTS partner_reps_ref`); err != nil {
		return err
	}
	_, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS partner_reps_ref_u ON partner_reps(key_id, external_ref) WHERE external_ref<>''`)
	return err
}

// hasColumn tells whether a table has a column.
func (s *Store) hasColumn(table, col string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, col).Scan(&n)
	return n > 0
}

// errRefTaken means another panel of the key has that external_ref.
func refTaken(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE") && strings.Contains(err.Error(), "external_ref")
}

// Partner key scopes. A key made before scopes existed has the default set.
const (
	scopePanelsRead   = "panels:read"   // its panels, their credit, history and numbers
	scopePanelsWrite  = "panels:write"  // make panels, change them, on/off, passwords, sign-in links
	scopeCreditAdd    = "credit:add"    // add credit to its panels
	scopeCreditRevoke = "credit:revoke" // take unused credit back (off unless the owner allows)
	scopePanelsDelete = "panels:delete" // delete its panels (off unless the owner allows)
	scopeClientsRead  = "clients:read"  // read its panels' customers (off unless the owner allows)
	scopeEvents       = "events"        // read its events and set its webhook
)

var (
	partnerScopes        = []string{scopeClientsRead, scopeCreditAdd, scopeCreditRevoke, scopeEvents, scopePanelsDelete, scopePanelsRead, scopePanelsWrite}
	partnerDefaultScopes = []string{scopeCreditAdd, scopeEvents, scopePanelsRead, scopePanelsWrite}
)

// PartnerKey is a key without its secret.
type PartnerKey struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	MaxGB      float64  `json:"max_gb"`    // 0 = no limit
	MaxUnits   int64    `json:"max_units"` // 0 = no limit
	GivenGB    float64  `json:"given_gb"`
	GivenUnits int64    `json:"given_units"`
	Template   string   `json:"template"`
	Created    int64    `json:"created_unix"`
	LastUsed   int64    `json:"last_used"`
	LastIP     string   `json:"last_ip"`
	Panels     int      `json:"panels"`
	Scopes     []string `json:"scopes"`
	AllowedIPs []string `json:"allowed_ips"`  // empty: any address
	Expires    int64    `json:"expires_unix"` // 0: never
	RatePerMin int      `json:"rate_per_min"` // 0: the default
	Packages   []string `json:"packages"`     // reseller-sale packages it may sell; empty: all
	// Left to hand out: -1 = no limit.
	RemainingGB    float64 `json:"remaining_gb"`
	RemainingUnits int64   `json:"remaining_units"`
	// Key is only set right after it was made.
	Key string `json:"key,omitempty"`
}

const gibF = float64(1 << 30)

func (s *Store) scanPartner(row interface{ Scan(...any) error }) (PartnerKey, error) {
	var k PartnerKey
	var maxB, givenB int64
	var scopes, ips, pkgs string
	err := row.Scan(&k.ID, &k.Name, &k.Enabled, &maxB, &k.MaxUnits, &givenB, &k.GivenUnits, &k.Template, &k.Created, &k.LastUsed, &k.LastIP, &k.Panels,
		&scopes, &ips, &k.Expires, &k.RatePerMin, &pkgs)
	k.MaxGB, k.GivenGB = float64(maxB)/gibF, float64(givenB)/gibF
	k.Scopes, k.AllowedIPs, k.Packages = splitTags(scopes), splitTags(ips), splitTags(pkgs)
	k.RemainingGB, k.RemainingUnits = -1, -1
	if maxB > 0 {
		k.RemainingGB = float64(max(0, maxB-givenB)) / gibF
	}
	if k.MaxUnits > 0 {
		k.RemainingUnits = max(0, k.MaxUnits-k.GivenUnits)
	}
	return k, err
}

const partnerCols = `k.id,k.name,k.enabled,k.max_bytes,k.max_units,k.given_bytes,k.given_units,k.template,k.created_unix,k.last_used,k.last_ip,
(SELECT COUNT(*) FROM partner_reps p WHERE p.key_id=k.id),k.scopes,k.allowed_ips,k.expires_unix,k.rate_per_min,k.packages`

// PartnerKeys lists the keys.
func (s *Store) PartnerKeys(ctx context.Context) ([]PartnerKey, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+partnerCols+` FROM partner_keys k ORDER BY k.created_unix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PartnerKey{}
	for rows.Next() {
		k, err := s.scanPartner(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) partnerKey(ctx context.Context, id string) (PartnerKey, error) {
	k, err := s.scanPartner(s.ro.QueryRowContext(ctx, `SELECT `+partnerCols+` FROM partner_keys k WHERE k.id=?`, id))
	if err != nil {
		return PartnerKey{}, ErrNotFound
	}
	return k, nil
}

// PartnerKeyIn is what the owner sets on a key. The pointer fields keep
// their value when left out of a change (and take the default on a new key).
type PartnerKeyIn struct {
	Name       string    `json:"name"`
	MaxGB      float64   `json:"max_gb"`
	MaxUnits   int64     `json:"max_units"`
	Template   string    `json:"template"`
	Enabled    *bool     `json:"enabled"`
	Scopes     *[]string `json:"scopes"`
	AllowedIPs *[]string `json:"allowed_ips"`
	Expires    *int64    `json:"expires_unix"`
	RatePerMin *int      `json:"rate_per_min"`
	Packages   *[]string `json:"packages"`
}

func (s *Store) checkPartnerIn(ctx context.Context, in *PartnerKeyIn) error {
	in.Name, in.Template = strings.TrimSpace(in.Name), strings.TrimSpace(in.Template)
	if in.Name == "" || len(in.Name) > 64 {
		return fmt.Errorf("%w: the key needs a name (up to 64 letters)", ErrInvalid)
	}
	if in.MaxGB < 0 || in.MaxUnits < 0 || in.MaxGB > 1e7 {
		return fmt.Errorf("%w: limits cannot be negative", ErrInvalid)
	}
	if in.Template != "" {
		if _, err := s.RepPolicyOf(ctx, in.Template); err != nil {
			return fmt.Errorf("%w: template reseller %q not found", ErrInvalid, in.Template)
		}
	}
	if in.Scopes != nil {
		sc, err := normScopes(*in.Scopes, partnerScopes)
		if err != nil {
			return err
		}
		in.Scopes = &sc
	}
	if in.AllowedIPs != nil {
		ips, err := normIPs(*in.AllowedIPs)
		if err != nil {
			return err
		}
		in.AllowedIPs = &ips
	}
	if in.Expires != nil && *in.Expires < 0 {
		return fmt.Errorf("%w: expires_unix is 0 (never) or a Unix time", ErrInvalid)
	}
	if in.RatePerMin != nil {
		if err := checkKeyRate(*in.RatePerMin); err != nil {
			return err
		}
	}
	if in.Packages != nil {
		known := []string{}
		for _, p := range s.RepSale(ctx).Packages {
			known = append(known, p.ID)
		}
		pk, err := normNames(*in.Packages, known, false)
		if err != nil {
			return err
		}
		in.Packages = &pk
	}
	return nil
}

// keySettings writes a key's optional settings that were sent.
func (s *Store) partnerKeySettings(ctx context.Context, id string, in PartnerKeyIn) error {
	set := func(col string, v any) error {
		_, err := s.db.ExecContext(ctx, `UPDATE partner_keys SET `+col+`=? WHERE id=?`, v, id)
		return err
	}
	if in.Scopes != nil {
		if err := set("scopes", strings.Join(*in.Scopes, ",")); err != nil {
			return err
		}
	}
	if in.AllowedIPs != nil {
		if err := set("allowed_ips", strings.Join(*in.AllowedIPs, ",")); err != nil {
			return err
		}
	}
	if in.Expires != nil {
		if err := set("expires_unix", *in.Expires); err != nil {
			return err
		}
	}
	if in.RatePerMin != nil {
		if err := set("rate_per_min", *in.RatePerMin); err != nil {
			return err
		}
	}
	if in.Packages != nil {
		if err := set("packages", strings.Join(*in.Packages, ",")); err != nil {
			return err
		}
	}
	return nil
}

// CreatePartnerKey makes a key and returns it once.
func (s *Store) CreatePartnerKey(ctx context.Context, in PartnerKeyIn) (PartnerKey, error) {
	if err := s.checkPartnerIn(ctx, &in); err != nil {
		return PartnerKey{}, err
	}
	id, secret := randomHex(4), randomHex(24)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO partner_keys(id,name,secret_sha256,max_bytes,max_units,template,created_unix) VALUES(?,?,?,?,?,?,?)`,
		id, in.Name, hashToken(secret), int64(in.MaxGB*gibF), in.MaxUnits, in.Template, s.now().Unix()); err != nil {
		return PartnerKey{}, err
	}
	if err := s.partnerKeySettings(ctx, id, in); err != nil {
		return PartnerKey{}, err
	}
	k, err := s.partnerKey(ctx, id)
	k.Key = "dp_" + id + "_" + secret
	return k, err
}

// UpdatePartnerKey changes a key's name, limits, template or state.
func (s *Store) UpdatePartnerKey(ctx context.Context, id string, in PartnerKeyIn) (PartnerKey, error) {
	if err := s.checkPartnerIn(ctx, &in); err != nil {
		return PartnerKey{}, err
	}
	on := true
	if in.Enabled != nil {
		on = *in.Enabled
	}
	r, err := s.db.ExecContext(ctx, `UPDATE partner_keys SET name=?,max_bytes=?,max_units=?,template=?,enabled=? WHERE id=?`,
		in.Name, int64(in.MaxGB*gibF), in.MaxUnits, in.Template, on, id)
	if err != nil {
		return PartnerKey{}, err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return PartnerKey{}, ErrNotFound
	}
	if err := s.partnerKeySettings(ctx, id, in); err != nil {
		return PartnerKey{}, err
	}
	return s.partnerKey(ctx, id)
}

// DeletePartnerKey removes a key. The panels it made stay, as the owner's.
func (s *Store) DeletePartnerKey(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM partner_keys WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM partner_reps WHERE key_id=?`, id)
	return err
}

// partnerCaller is the key behind a request (Bearer dp_<id>_<secret>, or
// the same in X-API-Key).
func (s *Store) partnerCaller(r *http.Request) (apiCaller, error) {
	id, secret, ok := splitKey(bearerKey(r), "dp_")
	if !ok {
		return apiCaller{}, errKeyInvalid
	}
	ctx := r.Context()
	var sha, name, scopes, ips string
	var exp, last int64
	var rate int
	if s.ro.QueryRowContext(ctx, `SELECT secret_sha256,name,scopes,allowed_ips,expires_unix,rate_per_min,last_used FROM partner_keys WHERE id=? AND enabled=1`, id).
		Scan(&sha, &name, &scopes, &ips, &exp, &rate, &last) != nil || subtle.ConstantTimeCompare([]byte(sha), []byte(hashToken(secret))) != 1 {
		return apiCaller{}, errKeyInvalid
	}
	now := s.now().Unix()
	if exp > 0 && now >= exp {
		return apiCaller{}, errKeyExpired
	}
	if now-last >= 60 {
		_, _ = s.db.ExecContext(ctx, `UPDATE partner_keys SET last_used=?, last_ip=? WHERE id=?`, now, realIP(r), id)
	}
	return apiCaller{Kind: "partner", KeyID: id, Name: name, Scopes: scopeSet(scopes), IPs: prefixes(ips), Rate: rate, Expires: exp}, nil
}

// partnerGive reserves credit under a key's limits; false = over the limit.
func (s *Store) partnerGive(ctx context.Context, keyID string, bytes, units int64) bool {
	r, err := s.db.ExecContext(ctx, `UPDATE partner_keys SET given_bytes=given_bytes+?, given_units=given_units+?
WHERE id=? AND (max_bytes=0 OR given_bytes+?<=max_bytes) AND (max_units=0 OR given_units+?<=max_units)`, bytes, units, keyID, bytes, units)
	if err != nil {
		return false
	}
	n, _ := r.RowsAffected()
	return n == 1
}

func (s *Store) partnerUngive(ctx context.Context, keyID string, bytes, units int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE partner_keys SET given_bytes=MAX(0,given_bytes-?), given_units=MAX(0,given_units-?) WHERE id=?`, bytes, units, keyID)
}

// partnerGavePanel counts credit a key gave one of its panels.
func (s *Store) partnerGavePanel(ctx context.Context, keyID, repID string, bytes, units int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE partner_reps SET given_bytes=given_bytes+?, given_units=given_units+? WHERE rep_id=? AND key_id=?`, bytes, units, repID, keyID)
}

// partnerGivenTo is what a key has given a panel and not taken back.
func (s *Store) partnerGivenTo(ctx context.Context, keyID, repID string) (bytes, units int64) {
	_ = s.db.QueryRowContext(ctx, `SELECT given_bytes,given_units FROM partner_reps WHERE rep_id=? AND key_id=?`, repID, keyID).Scan(&bytes, &units)
	return
}

// partnerRelease lets a key hand out again credit that came back from one
// of its panels, but never more than the key gave that panel: credit the
// owner added there is not the key's.
func (s *Store) partnerRelease(ctx context.Context, keyID, repID string, bytes, units int64) {
	gb, gu := s.partnerGivenTo(ctx, keyID, repID)
	rb, ru := min(max(bytes, 0), gb), min(max(units, 0), gu)
	if rb == 0 && ru == 0 {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE partner_reps SET given_bytes=given_bytes-?, given_units=given_units-? WHERE rep_id=? AND key_id=?`, rb, ru, repID, keyID)
	s.partnerUngive(ctx, keyID, rb, ru)
}

// panelAddress is where resellers sign in.
func (s *Store) panelAddress(r *http.Request) string {
	ctx := r.Context()
	path := strings.Trim(s.Setting(ctx, "panel_path"), "/")
	if path != "" {
		path += "/"
	}
	if ph := s.PanelHTTPSSettings(ctx); ph.Port > 0 {
		host := ph.Domain
		if host == "" {
			host, _, _ = strings.Cut(r.Host, ":")
		}
		return "https://" + host + ":" + strconv.Itoa(ph.Port) + "/" + path
	}
	return strings.TrimRight(publicBase("", r), "/") + "/" + path
}
