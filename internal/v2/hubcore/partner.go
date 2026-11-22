package hubcore

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The partner API: a key that can make reseller panels and nothing else, so
// another bot or a website can sell reseller panels on its own. The owner
// makes the key in the panel and gives it limits:
//
//   - the most credit (GB and unlimited-plan units) it may ever hand out,
//     counted over all panels it made;
//   - a template reseller whose rules (limits, inbounds, prices) every new
//     panel copies (none = the default rules).
//
// A key sees and changes only the panels it made itself. It cannot delete a
// panel or take credit back: the owner does that in the panel. Format:
// dp_<id>_<secret>; only the secret's hash is kept.
//
//	GET  /api/v2/partner                      the key: limits and what is used
//	POST /api/v2/partner/reps                 make a panel {name, id?, username?, password?, credit_gb, units}
//	GET  /api/v2/partner/reps                 its panels
//	GET  /api/v2/partner/reps/{id}            one panel
//	POST /api/v2/partner/reps/{id}/credit     add credit {credit_gb, units}
//	POST /api/v2/partner/reps/{id}/enable     turn on
//	POST /api/v2/partner/reps/{id}/disable    turn off
//	POST /api/v2/partner/reps/{id}/password   a new random password

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
	_, err := s.db.Exec(partnerSchema)
	return err
}

// PartnerKey is a key without its secret.
type PartnerKey struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Enabled    bool    `json:"enabled"`
	MaxGB      float64 `json:"max_gb"`    // 0 = no limit
	MaxUnits   int64   `json:"max_units"` // 0 = no limit
	GivenGB    float64 `json:"given_gb"`
	GivenUnits int64   `json:"given_units"`
	Template   string  `json:"template"`
	Created    int64   `json:"created_unix"`
	LastUsed   int64   `json:"last_used"`
	LastIP     string  `json:"last_ip"`
	Panels     int     `json:"panels"`
	// Key is only set right after it was made.
	Key string `json:"key,omitempty"`
}

const gibF = float64(1 << 30)

func (s *Store) scanPartner(row interface{ Scan(...any) error }) (PartnerKey, error) {
	var k PartnerKey
	var maxB, givenB int64
	err := row.Scan(&k.ID, &k.Name, &k.Enabled, &maxB, &k.MaxUnits, &givenB, &k.GivenUnits, &k.Template, &k.Created, &k.LastUsed, &k.LastIP, &k.Panels)
	k.MaxGB, k.GivenGB = float64(maxB)/gibF, float64(givenB)/gibF
	return k, err
}

const partnerCols = `k.id,k.name,k.enabled,k.max_bytes,k.max_units,k.given_bytes,k.given_units,k.template,k.created_unix,k.last_used,k.last_ip,
(SELECT COUNT(*) FROM partner_reps p WHERE p.key_id=k.id)`

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

// PartnerKeyIn is what the owner sets on a key.
type PartnerKeyIn struct {
	Name     string  `json:"name"`
	MaxGB    float64 `json:"max_gb"`
	MaxUnits int64   `json:"max_units"`
	Template string  `json:"template"`
	Enabled  *bool   `json:"enabled"`
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

// partnerAuth is the key behind a request (Bearer dp_<id>_<secret>).
func (s *Store) partnerAuth(r *http.Request) (PartnerKey, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		tok = r.Header.Get("X-API-Key")
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(tok), "dp_")
	if !ok {
		return PartnerKey{}, false
	}
	id, secret, ok := strings.Cut(rest, "_")
	if !ok || id == "" || secret == "" {
		return PartnerKey{}, false
	}
	var sha string
	if s.ro.QueryRowContext(r.Context(), `SELECT secret_sha256 FROM partner_keys WHERE id=? AND enabled=1`, id).Scan(&sha) != nil || subtle.ConstantTimeCompare([]byte(sha), []byte(hashToken(secret))) != 1 {
		return PartnerKey{}, false
	}
	k, err := s.partnerKey(r.Context(), id)
	if err != nil {
		return PartnerKey{}, false
	}
	if now := s.now().Unix(); now-k.LastUsed >= 60 {
		_, _ = s.db.ExecContext(r.Context(), `UPDATE partner_keys SET last_used=?, last_ip=? WHERE id=?`, now, realIP(r), id)
	}
	return k, true
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

// PartnerPanel is a reseller panel as the partner sees it.
type PartnerPanel struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Username string  `json:"username"`
	Enabled  bool    `json:"enabled"`
	CreditGB float64 `json:"credit_gb"`
	Units    int64   `json:"units"`
	Clients  int     `json:"clients"`
	UsedGB   float64 `json:"used_gb"`
	PanelURL string  `json:"panel_url"`
	Password string  `json:"password,omitempty"` // only when just made
	Message  string  `json:"message,omitempty"`  // ready-to-send text, only when just made
}

// partnerPanels lists a key's panels (one when id is set).
func (s *Store) partnerPanels(ctx context.Context, keyID, id string) ([]PartnerPanel, error) {
	reps, err := s.Reps(ctx)
	if err != nil {
		return nil, err
	}
	mine := map[string]bool{}
	rows, err := s.ro.QueryContext(ctx, `SELECT rep_id FROM partner_reps WHERE key_id=?`, keyID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r string
		if rows.Scan(&r) == nil {
			mine[r] = true
		}
	}
	rows.Close()
	out := []PartnerPanel{}
	for _, r := range reps {
		if !mine[r.ID] || (id != "" && r.ID != id) {
			continue
		}
		user := ""
		if l := s.RepLogins(ctx, r.ID); len(l) > 0 {
			user = l[0]
		}
		out = append(out, PartnerPanel{ID: r.ID, Name: r.Name, Username: user, Enabled: r.Enabled,
			CreditGB: float64(r.VolumeCredit) / gibF, Units: r.UnlimitedUnits, Clients: r.Clients, UsedGB: float64(r.UsedBytes) / gibF})
	}
	if id != "" && len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
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

func partnerMessage(url, user, pass, name string) string {
	return fmt.Sprintf("پنل نمایندگی %s آماده است ✅\n\n🌐 آدرس پنل:\n%s\n\n👤 نام کاربری: %s\n🔑 رمز: %s\n\nبعد از اولین ورود، رمز را از تنظیمات عوض کنید.", name, url, user, pass)
}

func partnerRoutes(s *Store, mux *http.ServeMux) {
	auth := func(h func(http.ResponseWriter, *http.Request, PartnerKey)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			k, ok := s.partnerAuth(r)
			if !ok {
				w.Header().Set("WWW-Authenticate", "Bearer")
				pgErr(w, http.StatusUnauthorized, "invalid or disabled key")
				return
			}
			if r.Method != http.MethodGet {
				sw := &statusWriter{ResponseWriter: w}
				h(sw, r, k)
				s.Audit(r.Context(), Principal{User: "partner:" + k.Name, Role: "owner"}, r, sw.status)
				return
			}
			h(w, r, k)
		}
	}
	fail := func(w http.ResponseWriter, err error) {
		switch {
		case errors.Is(err, ErrNotFound):
			pgErr(w, http.StatusNotFound, "panel not found")
		case errors.Is(err, ErrInvalid), errors.Is(err, ErrPolicy):
			pgErr(w, http.StatusUnprocessableEntity, err.Error())
		default:
			pgErr(w, http.StatusInternalServerError, "internal error")
		}
	}
	type creditIn struct {
		CreditGB float64 `json:"credit_gb"`
		Units    int64   `json:"units"`
	}
	read := func(w http.ResponseWriter, r *http.Request, v any) bool {
		if r.ContentLength == 0 {
			return true
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(v); err != nil {
			pgErr(w, http.StatusBadRequest, "invalid JSON")
			return false
		}
		return true
	}
	one := func(w http.ResponseWriter, r *http.Request, k PartnerKey) (PartnerPanel, bool) {
		list, err := s.partnerPanels(r.Context(), k.ID, r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return PartnerPanel{}, false
		}
		list[0].PanelURL = s.panelAddress(r)
		return list[0], true
	}

	mux.HandleFunc("GET /api/v2/partner", auth(func(w http.ResponseWriter, r *http.Request, k PartnerKey) {
		k.LastIP = ""
		pgJSON(w, k)
	}))
	mux.HandleFunc("GET /api/v2/partner/reps", auth(func(w http.ResponseWriter, r *http.Request, k PartnerKey) {
		list, err := s.partnerPanels(r.Context(), k.ID, "")
		if err != nil {
			fail(w, err)
			return
		}
		u := s.panelAddress(r)
		for i := range list {
			list[i].PanelURL = u
		}
		pgJSON(w, list)
	}))
	mux.HandleFunc("GET /api/v2/partner/reps/{id}", auth(func(w http.ResponseWriter, r *http.Request, k PartnerKey) {
		if p, ok := one(w, r, k); ok {
			pgJSON(w, p)
		}
	}))
	mux.HandleFunc("POST /api/v2/partner/reps", auth(func(w http.ResponseWriter, r *http.Request, k PartnerKey) {
		var in struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			Username string  `json:"username"`
			Password string  `json:"password"`
			CreditGB float64 `json:"credit_gb"`
			Units    int64   `json:"units"`
		}
		if !read(w, r, &in) {
			return
		}
		if in.CreditGB < 0 || in.Units < 0 || in.CreditGB > 1e6 {
			pgErr(w, http.StatusUnprocessableEntity, "credit cannot be negative")
			return
		}
		in.ID = strings.ToLower(strings.TrimSpace(in.ID))
		if in.ID == "" {
			for range 5 {
				if id := "p" + randomHex(3); s.RepNameFree(r.Context(), id) {
					in.ID = id
					break
				}
			}
		}
		if in.Password == "" {
			in.Password = RandomPassword()
		}
		bytes := int64(in.CreditGB * gibF)
		if !s.partnerGive(r.Context(), k.ID, bytes, in.Units) {
			pgErr(w, http.StatusForbidden, "this key's credit limit is used up")
			return
		}
		rc := RepCreate{ID: in.ID, Name: in.Name, Username: in.Username, Password: in.Password, CreditGB: in.CreditGB, Units: in.Units}
		if k.Template != "" {
			if pol, err := s.RepPolicyOf(r.Context(), k.Template); err == nil {
				rc.Policy = &pol
			}
		}
		if err := s.CreateRep(r.Context(), "partner:"+k.Name, rc); err != nil {
			s.partnerUngive(r.Context(), k.ID, bytes, in.Units)
			fail(w, err)
			return
		}
		_, _ = s.db.ExecContext(r.Context(), `INSERT OR REPLACE INTO partner_reps(rep_id,key_id,created_unix) VALUES(?,?,?)`, rc.ID, k.ID, s.now().Unix())
		list, err := s.partnerPanels(r.Context(), k.ID, rc.ID)
		if err != nil {
			fail(w, err)
			return
		}
		p := list[0]
		p.PanelURL, p.Password = s.panelAddress(r), strings.TrimSpace(in.Password)
		p.Message = partnerMessage(p.PanelURL, p.Username, p.Password, p.Name)
		pgJSON(w, p)
	}))
	mux.HandleFunc("POST /api/v2/partner/reps/{id}/credit", auth(func(w http.ResponseWriter, r *http.Request, k PartnerKey) {
		p, ok := one(w, r, k)
		if !ok {
			return
		}
		var in creditIn
		if !read(w, r, &in) {
			return
		}
		if in.CreditGB < 0 || in.Units < 0 || (in.CreditGB == 0 && in.Units == 0) || in.CreditGB > 1e6 {
			pgErr(w, http.StatusUnprocessableEntity, "credit_gb or units: more than 0 (credit can only be added)")
			return
		}
		bytes := int64(in.CreditGB * gibF)
		if !s.partnerGive(r.Context(), k.ID, bytes, in.Units) {
			pgErr(w, http.StatusForbidden, "this key's credit limit is used up")
			return
		}
		if err := s.AdjustCredit(r.Context(), "partner:"+k.Name, p.ID, bytes, in.Units, "partner API"); err != nil {
			s.partnerUngive(r.Context(), k.ID, bytes, in.Units)
			fail(w, err)
			return
		}
		if p, ok := one(w, r, k); ok {
			pgJSON(w, p)
		}
	}))
	for _, on := range []bool{true, false} {
		verb := "disable"
		if on {
			verb = "enable"
		}
		mux.HandleFunc("POST /api/v2/partner/reps/{id}/"+verb, auth(func(w http.ResponseWriter, r *http.Request, k PartnerKey) {
			p, ok := one(w, r, k)
			if !ok {
				return
			}
			if err := s.PutRep(r.Context(), Rep{ID: p.ID, Name: p.Name, Enabled: on}); err != nil {
				fail(w, err)
				return
			}
			if p, ok := one(w, r, k); ok {
				pgJSON(w, p)
			}
		}))
	}
	mux.HandleFunc("POST /api/v2/partner/reps/{id}/password", auth(func(w http.ResponseWriter, r *http.Request, k PartnerKey) {
		p, ok := one(w, r, k)
		if !ok {
			return
		}
		pass := RandomPassword()
		if err := s.SetRepLogin(r.Context(), p.Username, pass, p.ID); err != nil {
			fail(w, err)
			return
		}
		p.Password = pass
		p.Message = partnerMessage(p.PanelURL, p.Username, pass, p.Name)
		pgJSON(w, p)
	}))
}
