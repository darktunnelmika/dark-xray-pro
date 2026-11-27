package hubcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// A PasarGuard-compatible API, so sales bots made for PasarGuard (Mirza
// above all) can use this panel as if it were one. Only what those bots
// call is served, under PasarGuard's own paths and shapes:
//
//	POST   /api/admin/token                 username+password (form) -> bearer token
//	GET    /api/admin                       who the token is
//	GET    /api/system                      counts and memory
//	GET    /api/groups                      inbounds, as PasarGuard groups
//	GET    /api/nodes                       nodes (owner only; empty for a reseller)
//	GET    /api/users                       users, ?status= &username= &offset= &limit=
//	POST   /api/user                        create
//	GET    /api/user/{username}             read
//	PUT    /api/user/{username}             change
//	DELETE /api/user/{username}             delete
//	POST   /api/user/{username}/reset       reset usage
//	POST   /api/user/{username}/revoke_sub  new link and credentials
//	GET    /api/user/{username}/sub_update  last subscription fetch
//
// Each seller (the owner, or a reseller) has its own username and password
// for it. A reseller's token sees only its own clients and its allowed
// inbounds, and every change goes through the reseller rules: credit,
// policy and refunds, exactly like the panel. While a reseller has it on,
// its own Dark sales bot is off (the bot that serves its customers is the
// other one). Sub-sellers do not get it: they sell their reseller's plans.

const pgSchema = `
CREATE TABLE IF NOT EXISTS pg_api (
  seller TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  pass_hash TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_unix INTEGER NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0,
  last_ip TEXT NOT NULL DEFAULT '',
  made INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS pg_tokens (
  sha TEXT PRIMARY KEY,
  seller TEXT NOT NULL,
  expires_unix INTEGER NOT NULL
);
`

const pgTokenTTL = 24 * time.Hour

// PGAllGroups is a name a bot can read like a user to learn every group
// (inbound) the seller may give.
const PGAllGroups = "all-groups"

func (s *Store) ensurePGSchema() error {
	_, err := s.db.Exec(pgSchema)
	return err
}

// PGAPI is a seller's PasarGuard-compatible access, without the password.
type PGAPI struct {
	Enabled  bool   `json:"enabled"`
	Username string `json:"username"`
	Created  int64  `json:"created_unix"`
	LastUsed int64  `json:"last_used"`
	LastIP   string `json:"last_ip"`
	Made     int64  `json:"made"`  // users created through it
	Users    int    `json:"users"` // the seller's clients now
	// Password is only set right after it was made.
	Password string `json:"password,omitempty"`
}

// PGAPIOf is a seller's access ("" = the owner).
func (s *Store) PGAPIOf(ctx context.Context, seller string) PGAPI {
	var a PGAPI
	_ = s.ro.QueryRowContext(ctx, `SELECT enabled,username,created_unix,last_used,last_ip,made FROM pg_api WHERE seller=?`, seller).
		Scan(&a.Enabled, &a.Username, &a.Created, &a.LastUsed, &a.LastIP, &a.Made)
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE rep_id=?`, seller).Scan(&a.Users)
	return a
}

// MirzaMode tells whether a reseller sells through the PasarGuard API (and
// so its own Dark bot stays off).
func (s *Store) MirzaMode(ctx context.Context, seller string) bool {
	if seller == "" {
		return false
	}
	var on bool
	_ = s.ro.QueryRowContext(ctx, `SELECT enabled FROM pg_api WHERE seller=?`, seller).Scan(&on)
	return on
}

// EnablePGAPI turns a seller's access on with a fresh password (made the
// first time, replaced on every call) and returns it once.
func (s *Store) EnablePGAPI(ctx context.Context, seller string) (PGAPI, error) {
	if seller != "" {
		var enabled bool
		if s.ro.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=?`, seller).Scan(&enabled) != nil {
			return PGAPI{}, ErrNotFound
		}
		if s.IsSubSeller(ctx, seller) {
			return PGAPI{}, fmt.Errorf("%w: sub-sellers sell their reseller's plans and cannot connect a sales bot", ErrPolicy)
		}
	}
	pass := RandomPassword() + RandomPassword()
	h, err := bcrypt.GenerateFromPassword([]byte(pass), 10)
	if err != nil {
		return PGAPI{}, err
	}
	user := "mirza"
	if seller != "" {
		user = "mirza_" + seller
	}
	now := s.now().Unix()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO pg_api(seller,username,pass_hash,enabled,created_unix) VALUES(?,?,?,1,?)
ON CONFLICT(seller) DO UPDATE SET pass_hash=excluded.pass_hash, enabled=1`, seller, user, string(h), now); err != nil {
		return PGAPI{}, err
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM pg_tokens WHERE seller=?`, seller) // the old password's sessions end
	a := s.PGAPIOf(ctx, seller)
	a.Password = pass
	return a, nil
}

// DisablePGAPI turns a seller's access off; its tokens stop at once.
func (s *Store) DisablePGAPI(ctx context.Context, seller string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE pg_api SET enabled=0 WHERE seller=?`, seller); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM pg_tokens WHERE seller=?`, seller)
	return err
}

func pgSHA(tok string) string {
	h := sha256.Sum256([]byte("pg|" + tok))
	return hex.EncodeToString(h[:])
}

// pgLogin checks a username and password and makes a token.
func (s *Store) pgLogin(ctx context.Context, user, pass string) (string, error) {
	var seller, hash string
	var enabled bool
	err := s.ro.QueryRowContext(ctx, `SELECT seller,pass_hash,enabled FROM pg_api WHERE username=?`, user).Scan(&seller, &hash, &enabled)
	if err != nil || !enabled {
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(pass)) // same time either way
		return "", ErrAuth
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)) != nil {
		return "", ErrAuth
	}
	if seller != "" {
		var on bool
		if s.ro.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=?`, seller).Scan(&on) != nil || !on {
			return "", ErrAuth
		}
	}
	tok := randomHex(32)
	now := s.now().Unix()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM pg_tokens WHERE expires_unix<?`, now)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO pg_tokens(sha,seller,expires_unix) VALUES(?,?,?)`, pgSHA(tok), seller, now+int64(pgTokenTTL.Seconds())); err != nil {
		return "", err
	}
	return tok, nil
}

// pgAuth is the seller behind a bearer token.
func (s *Store) pgAuth(r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return "", false
	}
	ctx := r.Context()
	var seller string
	now := s.now().Unix()
	if s.ro.QueryRowContext(ctx, `SELECT seller FROM pg_tokens WHERE sha=? AND expires_unix>?`, pgSHA(strings.TrimSpace(tok)), now).Scan(&seller) != nil {
		return "", false
	}
	var enabled bool
	var last int64
	if s.ro.QueryRowContext(ctx, `SELECT enabled,last_used FROM pg_api WHERE seller=?`, seller).Scan(&enabled, &last) != nil || !enabled {
		return "", false
	}
	if seller != "" {
		var on bool
		if s.ro.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=?`, seller).Scan(&on) != nil || !on {
			return "", false
		}
	}
	if now-last >= 60 { // "last seen" without a write per request
		_, _ = s.db.ExecContext(ctx, `UPDATE pg_api SET last_used=?, last_ip=? WHERE seller=?`, now, realIP(r), seller)
	}
	return seller, true
}

// ---- groups: the seller's inbounds, numbered ----

type pgGroup struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	InboundTag []string `json:"inbound_tags"`
	TotalUsers int      `json:"total_users"`
	IsDisabled bool     `json:"is_disabled"`
}

// pgGroups lists the inbounds a seller may give, each with a stable number
// (the inbound's row id).
func (s *Store) pgGroups(ctx context.Context, seller string) ([]pgGroup, error) {
	allowed := map[string]bool{}
	for _, t := range s.SellerInbounds(ctx, seller) {
		allowed[t] = true
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT i.rowid,i.tag,i.enabled,(SELECT COUNT(*) FROM client_inbounds ci JOIN clients c ON c.email=ci.email WHERE ci.tag=i.tag AND c.rep_id=?)
FROM inbounds i ORDER BY i.rowid`, seller)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []pgGroup{}
	for rows.Next() {
		var g pgGroup
		var tag string
		var on bool
		if err := rows.Scan(&g.ID, &tag, &on, &g.TotalUsers); err != nil {
			return nil, err
		}
		if !allowed[tag] {
			continue
		}
		g.Name, g.InboundTag, g.IsDisabled = tag, []string{tag}, !on
		out = append(out, g)
	}
	return out, rows.Err()
}

// pgTags turns group numbers into inbound tags; unknown or not allowed
// numbers are an error. No numbers means every allowed inbound.
func (s *Store) pgTags(ctx context.Context, seller string, ids []int64) ([]string, error) {
	groups, err := s.pgGroups(ctx, seller)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		out := []string{}
		for _, g := range groups {
			if !g.IsDisabled {
				out = append(out, g.Name)
			}
		}
		return out, nil
	}
	by := map[int64]string{}
	for _, g := range groups {
		by[g.ID] = g.Name
	}
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		t, ok := by[id]
		if !ok {
			return nil, fmt.Errorf("%w: group %d not found", ErrInvalid, id)
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

// ---- users ----

type pgUser struct {
	ID                     int64          `json:"id"`
	Username               string         `json:"username"`
	Status                 string         `json:"status"`
	UsedTraffic            int64          `json:"used_traffic"`
	LifetimeUsedTraffic    int64          `json:"lifetime_used_traffic"`
	DataLimit              int64          `json:"data_limit"`
	DataLimitResetStrategy string         `json:"data_limit_reset_strategy"`
	Expire                 *string        `json:"expire"`
	OnHoldExpireDuration   *int64         `json:"on_hold_expire_duration"`
	OnHoldTimeout          *string        `json:"on_hold_timeout"`
	ProxySettings          map[string]any `json:"proxy_settings"`
	GroupIDs               []int64        `json:"group_ids"`
	Note                   string         `json:"note"`
	SubUpdatedAt           *string        `json:"sub_updated_at"`
	SubLastUserAgent       *string        `json:"sub_last_user_agent"`
	OnlineAt               *string        `json:"online_at"`
	CreatedAt              string         `json:"created_at"`
	EditAt                 *string        `json:"edit_at"`
	SubscriptionURL        string         `json:"subscription_url"`
	Links                  []string       `json:"links"`
	Admin                  map[string]any `json:"admin"`
	NextPlan               any            `json:"next_plan"`
}

func pgTime(unix int64) *string {
	if unix <= 0 {
		return nil
	}
	t := time.Unix(unix, 0).UTC().Format("2006-01-02T15:04:05Z")
	return &t
}

// pgStatus is a client's state in PasarGuard's words.
func pgStatus(c ClientView, now int64) string {
	used := c.Usage.Up + c.Usage.Down
	switch {
	case !c.Enabled:
		return "disabled"
	case c.ExpiresUnix == 0 && c.DurationSeconds > 0 && c.Usage.FirstSeen == 0:
		return "on_hold"
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
		return "expired"
	case c.VolumeBytes > 0 && used >= c.VolumeBytes:
		return "limited"
	}
	return "active"
}

// pgView turns a client into a PasarGuard user. links=false skips building
// the configs (lists).
func (s *Store) pgView(ctx context.Context, r *http.Request, base string, c ClientView, groups []pgGroup, links bool) pgUser {
	now := s.now().Unix()
	u := pgUser{Username: c.Email, Status: pgStatus(c, now), UsedTraffic: c.Usage.Up + c.Usage.Down,
		DataLimitResetStrategy: "no_reset", Note: c.Note, GroupIDs: []int64{}, Links: []string{},
		Admin: map[string]any{"username": or2(c.RepID, "owner")}, OnlineAt: pgTime(c.Usage.LastSeen),
		SubscriptionURL: subURL(base, r, c.SubToken)}
	var created int64
	_ = s.ro.QueryRowContext(ctx, `SELECT rowid,created_unix FROM clients WHERE email=?`, c.Email).Scan(&u.ID, &created)
	if t := pgTime(created); t != nil {
		u.CreatedAt = *t
	}
	u.LifetimeUsedTraffic = u.UsedTraffic
	var before int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(up+down),0) FROM usage_resets WHERE email=?`, c.Email).Scan(&before)
	u.LifetimeUsedTraffic += before
	if c.VolumeBytes > 0 {
		u.DataLimit = c.VolumeBytes
	}
	switch {
	case c.ExpiresUnix > 0:
		u.Expire = pgTime(c.ExpiresUnix)
	case c.DurationSeconds > 0 && c.Usage.FirstSeen > 0:
		u.Expire = pgTime(c.Usage.FirstSeen + c.DurationSeconds) // started at its first connection
	case c.DurationSeconds > 0:
		d := c.DurationSeconds
		u.OnHoldExpireDuration = &d
	}
	u.ProxySettings = map[string]any{
		"vmess":       map[string]any{"id": c.UUID},
		"vless":       map[string]any{"id": c.UUID, "flow": ""},
		"trojan":      map[string]any{"password": c.Secret},
		"shadowsocks": map[string]any{"password": c.Secret, "method": "chacha20-ietf-poly1305"},
	}
	by := map[string]int64{}
	for _, g := range groups {
		by[g.Name] = g.ID
	}
	for _, t := range c.Inbounds {
		if id, ok := by[t]; ok {
			u.GroupIDs = append(u.GroupIDs, id)
		}
	}
	var at int64
	var agent string
	if s.ro.QueryRowContext(ctx, `SELECT last_unix,agent FROM client_devices WHERE email=? ORDER BY last_unix DESC LIMIT 1`, c.Email).Scan(&at, &agent) == nil {
		u.SubUpdatedAt = pgTime(at)
		if agent != "" {
			u.SubLastUserAgent = &agent
		}
	}
	if links {
		if l, _, err := s.Subscription(ctx, c.Email); err == nil && l != nil {
			u.Links = l
		}
	}
	return u
}

// pgIn is what a bot sends to create or change a user. Every field is
// optional on a change.
type pgIn struct {
	Username             string          `json:"username"`
	Status               string          `json:"status"`
	DataLimit            *int64          `json:"data_limit"`
	Expire               json.RawMessage `json:"expire"`
	OnHoldExpireDuration *int64          `json:"on_hold_expire_duration"`
	GroupIDs             json.RawMessage `json:"group_ids"`
	Inbounds             json.RawMessage `json:"inbounds"` // Mirza sends the groups here on renewals
	Note                 *string         `json:"note"`
}

// pgExpire reads an expiry given as a Unix time, an ISO time, 0 or null.
// set=false means the field was not sent.
func pgExpire(raw json.RawMessage) (unix int64, set bool, err error) {
	v := strings.TrimSpace(string(raw))
	if v == "" {
		return 0, false, nil
	}
	if v == "null" || v == "0" || v == `""` {
		return 0, true, nil
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return int64(n), true, nil
	}
	var str string
	if json.Unmarshal(raw, &str) != nil {
		return 0, true, fmt.Errorf("%w: expire", ErrInvalid)
	}
	if n, err := strconv.ParseInt(str, 10, 64); err == nil {
		return n, true, nil
	}
	for _, f := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(f, str); err == nil {
			return t.Unix(), true, nil
		}
	}
	return 0, true, fmt.Errorf("%w: expire %q is not a time", ErrInvalid, str)
}

// pgIDs reads a list of group numbers (anything else, such as Marzban's
// inbound map, is ignored). set=false means none were sent.
func pgIDs(raw json.RawMessage) (ids []int64, set bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var list []json.Number
	if json.Unmarshal(raw, &list) != nil {
		return nil, false
	}
	for _, n := range list {
		if v, err := n.Int64(); err == nil {
			ids = append(ids, v)
		}
	}
	return ids, true
}

// pgApply changes c by what a bot sent.
func (s *Store) pgApply(ctx context.Context, seller string, c *Client, in pgIn, isNew bool) error {
	now := s.now().Unix()
	if in.DataLimit != nil {
		if *in.DataLimit > 0 {
			c.VolumeBytes = *in.DataLimit
		} else {
			c.VolumeBytes = -1
		}
	} else if isNew {
		c.VolumeBytes = -1
	}
	exp, set, err := pgExpire(in.Expire)
	if err != nil {
		return err
	}
	if set {
		if exp > 0 && exp <= now && isNew {
			return fmt.Errorf("%w: expire is in the past", ErrInvalid)
		}
		c.ExpiresUnix, c.DurationSeconds = max(exp, 0), 0
	}
	switch in.Status {
	case "", "active":
		if in.Status == "active" || isNew {
			c.Enabled = true
		}
	case "disabled":
		c.Enabled = false
	case "on_hold":
		c.Enabled = true
		if in.OnHoldExpireDuration == nil || *in.OnHoldExpireDuration <= 0 {
			return fmt.Errorf("%w: on_hold needs on_hold_expire_duration", ErrInvalid)
		}
		c.ExpiresUnix, c.DurationSeconds = 0, *in.OnHoldExpireDuration
	default:
		return fmt.Errorf("%w: status %q", ErrInvalid, in.Status)
	}
	ids, gset := pgIDs(in.GroupIDs)
	if !gset {
		ids, gset = pgIDs(in.Inbounds)
	}
	if gset || isNew {
		tags, err := s.pgTags(ctx, seller, ids)
		if err != nil {
			return err
		}
		if len(tags) == 0 {
			return fmt.Errorf("%w: no inbound is available for this user", ErrInvalid)
		}
		c.Inbounds = tags
	}
	return nil
}

// pgSave stores a client for the seller: as the owner, or through the
// reseller rules (credit and policy).
func (s *Store) pgSave(ctx context.Context, seller string, c Client) error {
	plan := "volume"
	if c.VolumeBytes < 0 {
		plan = "unlimited"
	}
	if seller == "" {
		if err := s.PutClient(ctx, c); err != nil {
			return err
		}
		_, err := s.db.ExecContext(ctx, `UPDATE clients SET plan=? WHERE email=?`, plan, c.Email)
		return err
	}
	return s.RepPutClient(ctx, "mirza", seller, c, plan)
}

// pgResetRep resets a reseller's client's usage. The used part becomes
// usable again, so a volume plan is charged what was used (the unused part
// was paid when it was given), on the credit ledger like every charge.
func (s *Store) pgResetRep(ctx context.Context, seller, email string) error {
	_, err := s.repResetUsage(ctx, "mirza", seller, email)
	return err
}

// ---- HTTP ----

func pgErr(w http.ResponseWriter, code int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"detail": detail})
}

func pgJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// pgFail answers a store error the way PasarGuard would.
func pgFail(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case errors.Is(err, ErrNotFound):
		pgErr(w, http.StatusNotFound, "User not found")
	case errors.Is(err, ErrCredit):
		pgErr(w, http.StatusBadRequest, "Not enough credit in the panel")
	case errors.Is(err, ErrPolicy):
		pgErr(w, http.StatusForbidden, msg)
	case errors.Is(err, ErrInvalid):
		pgErr(w, http.StatusUnprocessableEntity, msg)
	case errors.Is(err, ErrAuth):
		pgErr(w, http.StatusForbidden, "This panel account is disabled")
	default:
		pgErr(w, http.StatusInternalServerError, "internal error")
	}
}

func pgRoutes(s *Store, mux *http.ServeMux, subBase string) {
	auth := func(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			seller, ok := s.pgAuth(r)
			if !ok {
				w.Header().Set("WWW-Authenticate", "Bearer")
				pgErr(w, http.StatusUnauthorized, "Could not validate credentials")
				return
			}
			if r.Method != http.MethodGet {
				sw := &statusWriter{ResponseWriter: w}
				h(sw, r, seller)
				p := Principal{User: "mirza", Role: "owner"}
				if seller != "" {
					p = Principal{User: "mirza", Role: "rep", RepID: seller}
				}
				s.Audit(r.Context(), p, r, sw.status)
				return
			}
			h(w, r, seller)
		}
	}
	base := func(r *http.Request, seller string) string {
		if seller != "" {
			if b := s.repBase(r.Context(), seller); b != "" {
				return b
			}
		}
		return subBase
	}
	// mine reads one of the seller's clients.
	mine := func(r *http.Request, seller string) (ClientView, error) {
		c, err := s.clientView(r.Context(), r.PathValue("username"))
		if err != nil || c.RepID != seller {
			return ClientView{}, ErrNotFound
		}
		return c, nil
	}
	view := func(w http.ResponseWriter, r *http.Request, seller, email string) {
		c, err := s.clientView(r.Context(), email)
		if err != nil {
			pgFail(w, err)
			return
		}
		groups, _ := s.pgGroups(r.Context(), seller)
		pgJSON(w, s.pgView(r.Context(), r, base(r, seller), c, groups, true))
	}

	mux.HandleFunc("POST /api/admin/token", func(w http.ResponseWriter, r *http.Request) {
		user, pass := r.PostFormValue("username"), r.PostFormValue("password")
		if user == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var in struct{ Username, Password string }
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
			user, pass = in.Username, in.Password
		}
		user = strings.TrimSpace(user)
		ip := realIP(r)
		key := "pg:" + user
		if wait := s.logins.blocked(ip, key, s.now()); wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			pgErr(w, http.StatusTooManyRequests, "Too many wrong passwords; try again later")
			return
		}
		tok, err := s.pgLogin(r.Context(), user, strings.TrimSpace(pass))
		if err != nil {
			if d := s.logins.fail(ip, key, s.now()); d > 0 {
				notifyOwners(r.Context(), lockText(ip, key, d))
			}
			time.Sleep(300 * time.Millisecond)
			pgErr(w, http.StatusUnauthorized, "Incorrect username or password")
			return
		}
		s.logins.success(ip, key)
		pgJSON(w, map[string]string{"access_token": tok, "token_type": "bearer"})
	})
	mux.HandleFunc("GET /api/admin", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		a := s.PGAPIOf(r.Context(), seller)
		pgJSON(w, map[string]any{"username": a.Username, "is_sudo": seller == "", "is_disabled": false, "telegram_id": nil, "discord_webhook": nil})
	}))
	mux.HandleFunc("GET /api/system", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		list, err := s.clientViews(r.Context(), "c.rep_id=?", seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		now := s.now().Unix()
		count := map[string]int{}
		online := 0
		var traffic int64
		for _, c := range list {
			count[pgStatus(c, now)]++
			if c.Online {
				online++
			}
			traffic += c.Usage.Up + c.Usage.Down
		}
		memUsed, memTotal := memInfo()
		pgJSON(w, map[string]any{"version": "Dark Xray Pro " + HubVersion, "mem_total": memTotal, "mem_used": memUsed,
			"cpu_cores": 0, "cpu_usage": 0, "total_user": len(list), "online_users": online,
			"active_users": count["active"], "users_active": count["active"], "on_hold_users": count["on_hold"],
			"disabled_users": count["disabled"], "expired_users": count["expired"], "limited_users": count["limited"],
			"incoming_bandwidth": 0, "outgoing_bandwidth": traffic, "incoming_bandwidth_speed": 0, "outgoing_bandwidth_speed": 0})
	}))
	mux.HandleFunc("GET /api/groups", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		g, err := s.pgGroups(r.Context(), seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		pgJSON(w, map[string]any{"groups": g, "total": len(g)})
	}))
	mux.HandleFunc("GET /api/nodes", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		out := []map[string]any{}
		if seller == "" {
			nodes, _ := s.Nodes(r.Context())
			now := s.now().Unix()
			for i, n := range nodes {
				st, msg := "connected", ""
				if now-n.LastSeen > 120 {
					st, msg = "error", "not connected to the panel"
				} else if !n.XrayRunning {
					st, msg = "error", "xray is not running"
				}
				out = append(out, map[string]any{"id": i + 1, "name": or2(n.Name, n.ID), "address": n.PublicIP, "status": st, "message": msg, "xray_version": nil})
			}
		}
		pgJSON(w, out)
	}))
	mux.HandleFunc("GET /api/users", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		list, err := s.clientViews(r.Context(), "c.rep_id=?", seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		q := r.URL.Query()
		status, name := q.Get("status"), strings.ToLower(q.Get("username"))
		if name == "" {
			name = strings.ToLower(q.Get("search"))
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		groups, _ := s.pgGroups(r.Context(), seller)
		now := s.now().Unix()
		b := base(r, seller)
		users := []pgUser{}
		total := 0
		for _, c := range list {
			if status != "" && pgStatus(c, now) != status {
				continue
			}
			if name != "" && !strings.Contains(strings.ToLower(c.Email), name) {
				continue
			}
			total++
			if total <= offset || (limit > 0 && len(users) >= limit) {
				continue
			}
			users = append(users, s.pgView(r.Context(), r, b, c, groups, false))
		}
		pgJSON(w, map[string]any{"users": users, "total": total})
	}))
	mux.HandleFunc("POST /api/user", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		var in pgIn
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			pgErr(w, http.StatusUnprocessableEntity, "invalid JSON")
			return
		}
		in.Username = strings.TrimSpace(in.Username)
		if len(in.Username) < 3 || !emailRE.MatchString(in.Username) {
			pgErr(w, http.StatusUnprocessableEntity, "username: 3-128 letters, digits, . _ @ + -")
			return
		}
		var x int
		if s.ro.QueryRowContext(r.Context(), `SELECT 1 FROM clients WHERE email=?`, in.Username).Scan(&x) == nil {
			pgErr(w, http.StatusConflict, "User already exists")
			return
		}
		c := Client{Email: in.Username}
		if err := s.pgApply(r.Context(), seller, &c, in, true); err != nil {
			pgFail(w, err)
			return
		}
		if err := s.pgSave(r.Context(), seller, c); err != nil {
			pgFail(w, err)
			return
		}
		if in.Note != nil {
			_ = s.SetClientNote(r.Context(), c.Email, clip(*in.Note, 2000))
		}
		_, _ = s.db.ExecContext(r.Context(), `UPDATE pg_api SET made=made+1 WHERE seller=?`, seller)
		view(w, r, seller, c.Email)
	}))
	mux.HandleFunc("GET /api/user/{username}", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		c, err := mine(r, seller)
		if err != nil && r.PathValue("username") == PGAllGroups {
			// Not a real user: every inbound the seller may give, for a bot
			// that learns its groups by reading one user.
			groups, _ := s.pgGroups(r.Context(), seller)
			u := pgUser{Username: PGAllGroups, Status: "disabled", DataLimitResetStrategy: "no_reset", GroupIDs: []int64{}, Links: []string{},
				ProxySettings: map[string]any{}, Admin: map[string]any{"username": or2(seller, "owner")}}
			for _, g := range groups {
				if !g.IsDisabled {
					u.GroupIDs = append(u.GroupIDs, g.ID)
				}
			}
			pgJSON(w, u)
			return
		}
		if err != nil {
			pgFail(w, err)
			return
		}
		groups, _ := s.pgGroups(r.Context(), seller)
		pgJSON(w, s.pgView(r.Context(), r, base(r, seller), c, groups, true))
	}))
	mux.HandleFunc("PUT /api/user/{username}", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		cur, err := mine(r, seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		var in pgIn
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			pgErr(w, http.StatusUnprocessableEntity, "invalid JSON")
			return
		}
		c := cur.Client
		c.UUID, c.Secret, c.SubToken = "", "", "" // credentials stay as they are
		if err := s.pgApply(r.Context(), seller, &c, in, false); err != nil {
			pgFail(w, err)
			return
		}
		if err := s.pgSave(r.Context(), seller, c); err != nil {
			pgFail(w, err)
			return
		}
		if in.Note != nil {
			_ = s.SetClientNote(r.Context(), c.Email, clip(*in.Note, 2000))
		}
		view(w, r, seller, c.Email)
	}))
	mux.HandleFunc("DELETE /api/user/{username}", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		c, err := mine(r, seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		if seller == "" {
			err = s.DeleteClient(r.Context(), c.Email)
		} else {
			_, _, err = s.RepDeleteClient(r.Context(), "mirza", seller, c.Email)
		}
		if err != nil {
			pgFail(w, err)
			return
		}
		pgJSON(w, map[string]string{"detail": "User successfully deleted"})
	}))
	mux.HandleFunc("POST /api/user/{username}/reset", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		c, err := mine(r, seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		if seller == "" {
			err = s.ResetUsage(r.Context(), c.Email, "api")
		} else {
			err = s.pgResetRep(r.Context(), seller, c.Email)
		}
		if err != nil {
			pgFail(w, err)
			return
		}
		view(w, r, seller, c.Email)
	}))
	mux.HandleFunc("POST /api/user/{username}/revoke_sub", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		c, err := mine(r, seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		if err := s.RotateClient(r.Context(), c.Email, true, true); err != nil {
			pgFail(w, err)
			return
		}
		view(w, r, seller, c.Email)
	}))
	mux.HandleFunc("GET /api/user/{username}/sub_update", auth(func(w http.ResponseWriter, r *http.Request, seller string) {
		c, err := mine(r, seller)
		if err != nil {
			pgFail(w, err)
			return
		}
		type upd struct {
			CreatedAt string `json:"created_at"`
			UserAgent string `json:"user_agent"`
		}
		ups := []upd{}
		rows, err := s.ro.QueryContext(r.Context(), `SELECT last_unix,agent FROM client_devices WHERE email=? ORDER BY last_unix DESC LIMIT 10`, c.Email)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var at int64
				var agent string
				if rows.Scan(&at, &agent) == nil {
					ups = append(ups, upd{*pgTime(max(at, 1)), agent})
				}
			}
		}
		pgJSON(w, map[string]any{"updates": ups, "count": len(ups)})
	}))
}

// pgAdminRoutes lets the panel show, turn on (with a fresh password) and
// turn off a seller's access.
func pgAdminRoutes(s *Store, mux *docMux, path string, wrap func(http.HandlerFunc) http.HandlerFunc, seller func(*http.Request) (string, bool)) {
	mux.HandleFunc("GET "+path, wrap(func(w http.ResponseWriter, r *http.Request) {
		id, ok := seller(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		reply(w, s.PGAPIOf(r.Context(), id), nil)
	}))
	mux.HandleFunc("POST "+path, wrap(func(w http.ResponseWriter, r *http.Request) {
		id, ok := seller(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		a, err := s.EnablePGAPI(r.Context(), id)
		reply(w, a, err)
	}))
	mux.HandleFunc("DELETE "+path, wrap(func(w http.ResponseWriter, r *http.Request) {
		id, ok := seller(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		err := s.DisablePGAPI(r.Context(), id)
		reply(w, s.PGAPIOf(r.Context(), id), err)
	}))
}
