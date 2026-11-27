package hubcore

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
)

// Reseller API keys: a reseller's own bot or website sells and runs its
// customers through /api/v2/reseller with one of these, under the same
// rules as the reseller's panel (credit, policy, refunds). The reseller
// makes them in its panel; the owner, or the partner that made the panel,
// can make them for it too. Format: dr_<id>_<secret>; only the secret's
// hash is kept.

const resKeySchema = `
CREATE TABLE IF NOT EXISTS rep_api_keys (
  id TEXT PRIMARY KEY,
  rep_id TEXT NOT NULL,
  name TEXT NOT NULL,
  secret_sha256 TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  scopes TEXT NOT NULL,
  allowed_ips TEXT NOT NULL DEFAULT '',
  expires_unix INTEGER NOT NULL DEFAULT 0,
  rate_per_min INTEGER NOT NULL DEFAULT 0,
  created_unix INTEGER NOT NULL,
  created_by TEXT NOT NULL DEFAULT '',
  last_used INTEGER NOT NULL DEFAULT 0,
  last_ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS rep_api_keys_rep ON rep_api_keys(rep_id);
`

func (s *Store) ensureResKeySchema() error {
	_, err := s.db.Exec(resKeySchema)
	return err
}

// Reseller key scopes.
const (
	scopeAccountRead   = "account:read"   // credit, rules, inbounds, plans, numbers, history, price quotes
	scopeResClientsR   = "clients:read"   // customers, their links, usage, devices
	scopeResClientsW   = "clients:write"  // make, change, renew, reset, new link, on/off
	scopeResClientsDel = "clients:delete" // delete customers (refunds follow the rules)
	scopeSellers       = "sellers"        // sub-sellers: make, change, move credit
)

var (
	resellerScopes        = []string{scopeAccountRead, scopeResClientsDel, scopeResClientsR, scopeResClientsW, scopeEvents, scopeSellers}
	resellerDefaultScopes = []string{scopeAccountRead, scopeResClientsDel, scopeResClientsR, scopeResClientsW, scopeEvents}
)

const maxResKeys = 20

// ResKey is a reseller API key without its secret.
type ResKey struct {
	ID         string   `json:"id"`
	RepID      string   `json:"rep_id"`
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	Scopes     []string `json:"scopes"`
	AllowedIPs []string `json:"allowed_ips"`
	Expires    int64    `json:"expires_unix"`
	RatePerMin int      `json:"rate_per_min"`
	Created    int64    `json:"created_unix"`
	CreatedBy  string   `json:"created_by"`
	LastUsed   int64    `json:"last_used"`
	LastIP     string   `json:"last_ip"`
	// Key is only set right after it was made.
	Key string `json:"key,omitempty"`
}

// ResKeyIn is what is set on a key; pointer fields left out keep their
// value (or take the default on a new key).
type ResKeyIn struct {
	Name       string    `json:"name"`
	Enabled    *bool     `json:"enabled"`
	Scopes     *[]string `json:"scopes"`
	AllowedIPs *[]string `json:"allowed_ips"`
	Expires    *int64    `json:"expires_unix"`
	Days       int       `json:"days"` // instead of expires_unix: valid this many days (0 = never)
	RatePerMin *int      `json:"rate_per_min"`
}

const resKeyCols = `id,rep_id,name,enabled,scopes,allowed_ips,expires_unix,rate_per_min,created_unix,created_by,last_used,last_ip`

func scanResKey(row scanner) (ResKey, error) {
	var k ResKey
	var scopes, ips string
	err := row.Scan(&k.ID, &k.RepID, &k.Name, &k.Enabled, &scopes, &ips, &k.Expires, &k.RatePerMin, &k.Created, &k.CreatedBy, &k.LastUsed, &k.LastIP)
	k.Scopes, k.AllowedIPs = splitTags(scopes), splitTags(ips)
	return k, err
}

func (s *Store) checkResKeyIn(in *ResKeyIn, isNew bool) error {
	in.Name = strings.TrimSpace(in.Name)
	if (isNew || in.Name != "") && (in.Name == "" || len(in.Name) > 64 || strings.ContainsAny(in.Name, "/:")) {
		return fmt.Errorf("%w: the key needs a name (up to 64 letters, no / or :)", ErrInvalid)
	}
	if in.Scopes != nil {
		sc, err := normScopes(*in.Scopes, resellerScopes)
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
	if in.Days < 0 || in.Days > 3650 {
		return fmt.Errorf("%w: days is 0 (never) to 3650", ErrInvalid)
	}
	if in.Days > 0 {
		exp := s.now().Unix() + int64(in.Days)*86400
		in.Expires = &exp
	}
	if in.Expires != nil && *in.Expires < 0 {
		return fmt.Errorf("%w: expires_unix is 0 (never) or a Unix time", ErrInvalid)
	}
	if in.RatePerMin != nil {
		return checkKeyRate(*in.RatePerMin)
	}
	return nil
}

// CreateResKey makes a key for a reseller and returns it once.
func (s *Store) CreateResKey(ctx context.Context, repID, by string, in ResKeyIn) (ResKey, error) {
	var x int
	if s.ro.QueryRowContext(ctx, `SELECT 1 FROM reps WHERE id=?`, repID).Scan(&x) != nil {
		return ResKey{}, ErrNotFound
	}
	if err := s.checkResKeyIn(&in, true); err != nil {
		return ResKey{}, err
	}
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM rep_api_keys WHERE rep_id=?`, repID).Scan(&n)
	if n >= maxResKeys {
		return ResKey{}, fmt.Errorf("%w: at most %d keys per reseller", ErrInvalid, maxResKeys)
	}
	scopes := resellerDefaultScopes
	if in.Scopes != nil {
		scopes = *in.Scopes
	}
	if s.IsSubSeller(ctx, repID) { // sub-sellers cannot open sellers
		kept := []string{}
		for _, sc := range scopes {
			if sc != scopeSellers {
				kept = append(kept, sc)
			}
		}
		scopes = kept
	}
	ips := []string{}
	if in.AllowedIPs != nil {
		ips = *in.AllowedIPs
	}
	var exp int64
	if in.Expires != nil {
		exp = *in.Expires
	}
	rate := 0
	if in.RatePerMin != nil {
		rate = *in.RatePerMin
	}
	id, secret := randomHex(4), randomHex(24)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO rep_api_keys(id,rep_id,name,secret_sha256,scopes,allowed_ips,expires_unix,rate_per_min,created_unix,created_by)
VALUES(?,?,?,?,?,?,?,?,?,?)`, id, repID, in.Name, hashToken(secret), strings.Join(scopes, ","), strings.Join(ips, ","), exp, rate, s.now().Unix(), by); err != nil {
		return ResKey{}, err
	}
	k, err := s.resKey(ctx, repID, id)
	k.Key = "dr_" + id + "_" + secret
	return k, err
}

func (s *Store) resKey(ctx context.Context, repID, id string) (ResKey, error) {
	k, err := scanResKey(s.ro.QueryRowContext(ctx, `SELECT `+resKeyCols+` FROM rep_api_keys WHERE id=? AND rep_id=?`, id, repID))
	if err != nil {
		return ResKey{}, ErrNotFound
	}
	return k, nil
}

// ResKeys lists a reseller's keys.
func (s *Store) ResKeys(ctx context.Context, repID string) ([]ResKey, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+resKeyCols+` FROM rep_api_keys WHERE rep_id=? ORDER BY created_unix,id`, repID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ResKey{}
	for rows.Next() {
		k, err := scanResKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// UpdateResKey changes a key's name, state or settings.
func (s *Store) UpdateResKey(ctx context.Context, repID, id string, in ResKeyIn) (ResKey, error) {
	if _, err := s.resKey(ctx, repID, id); err != nil {
		return ResKey{}, err
	}
	if err := s.checkResKeyIn(&in, false); err != nil {
		return ResKey{}, err
	}
	set := func(col string, v any) error {
		_, err := s.db.ExecContext(ctx, `UPDATE rep_api_keys SET `+col+`=? WHERE id=? AND rep_id=?`, v, id, repID)
		return err
	}
	var err error
	if in.Name != "" {
		err = set("name", in.Name)
	}
	if err == nil && in.Enabled != nil {
		err = set("enabled", *in.Enabled)
	}
	if err == nil && in.Scopes != nil {
		sc := *in.Scopes
		if s.IsSubSeller(ctx, repID) {
			kept := []string{}
			for _, x := range sc {
				if x != scopeSellers {
					kept = append(kept, x)
				}
			}
			sc = kept
		}
		err = set("scopes", strings.Join(sc, ","))
	}
	if err == nil && in.AllowedIPs != nil {
		err = set("allowed_ips", strings.Join(*in.AllowedIPs, ","))
	}
	if err == nil && in.Expires != nil {
		err = set("expires_unix", *in.Expires)
	}
	if err == nil && in.RatePerMin != nil {
		err = set("rate_per_min", *in.RatePerMin)
	}
	if err != nil {
		return ResKey{}, err
	}
	return s.resKey(ctx, repID, id)
}

// DeleteResKey removes a key; it stops at once.
func (s *Store) DeleteResKey(ctx context.Context, repID, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM rep_api_keys WHERE id=? AND rep_id=?`, id, repID)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// resellerCaller is the key behind a reseller API request (Bearer
// dr_<id>_<secret>, or X-API-Key).
func (s *Store) resellerCaller(r *http.Request) (apiCaller, error) {
	id, secret, ok := splitKey(bearerKey(r), "dr_")
	if !ok {
		return apiCaller{}, errKeyInvalid
	}
	ctx := r.Context()
	var rep, sha, name, scopes, ips string
	var exp, last int64
	var rate int
	if s.ro.QueryRowContext(ctx, `SELECT rep_id,secret_sha256,name,scopes,allowed_ips,expires_unix,rate_per_min,last_used FROM rep_api_keys WHERE id=? AND enabled=1`, id).
		Scan(&rep, &sha, &name, &scopes, &ips, &exp, &rate, &last) != nil || subtle.ConstantTimeCompare([]byte(sha), []byte(hashToken(secret))) != 1 {
		return apiCaller{}, errKeyInvalid
	}
	now := s.now().Unix()
	if exp > 0 && now >= exp {
		return apiCaller{}, errKeyExpired
	}
	var on bool
	if s.ro.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=?`, rep).Scan(&on) != nil {
		return apiCaller{}, errKeyInvalid
	}
	if !on {
		return apiCaller{}, errAccountOff
	}
	if now-last >= 60 {
		_, _ = s.db.ExecContext(ctx, `UPDATE rep_api_keys SET last_used=?, last_ip=? WHERE id=?`, now, realIP(r), id)
	}
	return apiCaller{Kind: "reseller", KeyID: id, Name: name, RepID: rep, Scopes: scopeSet(scopes), IPs: prefixes(ips), Rate: rate, Expires: exp}, nil
}

// resKeyRoutes lets a reseller (in its panel), the owner (for any
// reseller) and a partner (for its panels) manage reseller API keys.
// repOf says whose keys a request is about ("" + false = refuse).
func (s *Store) resKeyRoutes(register func(pattern string, h http.HandlerFunc), prefix string, repOf func(*http.Request) (string, string, bool)) {
	// One shape for every caller; reply is the panel's own answer style.
	register("GET "+prefix, func(w http.ResponseWriter, r *http.Request) {
		rep, _, ok := repOf(r)
		if !ok {
			reply(w, nil, ErrNotFound)
			return
		}
		list, err := s.ResKeys(r.Context(), rep)
		reply(w, list, err)
	})
	register("POST "+prefix, func(w http.ResponseWriter, r *http.Request) {
		rep, by, ok := repOf(r)
		if !ok {
			reply(w, nil, ErrNotFound)
			return
		}
		var in ResKeyIn
		if !decode(w, r, &in) {
			return
		}
		k, err := s.CreateResKey(r.Context(), rep, by, in)
		reply(w, k, err)
	})
	register("PUT "+prefix+"/{key}", func(w http.ResponseWriter, r *http.Request) {
		rep, _, ok := repOf(r)
		if !ok {
			reply(w, nil, ErrNotFound)
			return
		}
		var in ResKeyIn
		if !decode(w, r, &in) {
			return
		}
		k, err := s.UpdateResKey(r.Context(), rep, r.PathValue("key"), in)
		reply(w, k, err)
	})
	register("DELETE "+prefix+"/{key}", func(w http.ResponseWriter, r *http.Request) {
		rep, _, ok := repOf(r)
		if !ok {
			reply(w, nil, ErrNotFound)
			return
		}
		err := s.DeleteResKey(r.Context(), rep, r.PathValue("key"))
		reply(w, map[string]bool{"ok": err == nil}, err)
	})
}
