package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// A connected shop's clients, as the shop's API sees them. Every function
// here takes the tenant and works only inside its name space; names in and
// out are the shop's own (without the "<tenant>+" prefix).

// tenantNameRE: what a shop may call a customer. The stored name adds the
// tenant prefix, so it stays a valid client name (emailRE).
var tenantNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,99}$`)

// secretRE: a Trojan/Shadowsocks password a shop may bring along.
var secretRE = regexp.MustCompile(`^[A-Za-z0-9._~+=-]{4,128}$`)

// subTokenRE: a subscription token a shop may bring along (what /sub/ accepts).
var subTokenRE = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

// externalRefRE: the shop's own id for the client (an order, a reseller…).
var externalRefRE = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)

// TenantClientIn is a create or a change. On a change every nil field is
// left as it is; on a create, nil means the default.
type TenantClientIn struct {
	Name           string   `json:"name"`
	ExternalRef    *string  `json:"external_ref"`
	UUID           string   `json:"uuid"`            // create only (or with rotate); empty = new
	TrojanPassword string   `json:"trojan_password"` // create only; empty = new
	SubToken       string   `json:"sub_token"`       // create only; empty = new
	Enabled        *bool    `json:"enabled"`
	VolumeBytes    *int64   `json:"volume_bytes"` // -1 = unlimited (the default)
	ExpiresUnix    *int64   `json:"expires_unix"` // 0 = no expiry
	Days           *int64   `json:"days"`         // term starting now (instead of expires_unix)
	OnHoldDays     *int64   `json:"on_hold_days"` // term starting at the first connection
	MaxIPs         *int     `json:"max_ips"`      // 0 = no limit
	MaxDevices     *int     `json:"max_devices"`  // 0 = no limit
	Inbounds       []string `json:"inbounds"`     // nil = every inbound the shop may give
	Note           *string  `json:"note"`
}

// TenantClient is a client as its shop sees it.
type TenantClient struct {
	Name            string   `json:"name"`
	ExternalRef     string   `json:"external_ref"`
	Status          string   `json:"status"` // active | disabled | expired | limited | on_hold
	Enabled         bool     `json:"enabled"`
	UUID            string   `json:"uuid"`
	TrojanPassword  string   `json:"trojan_password"`
	SubToken        string   `json:"sub_token"`
	VolumeBytes     int64    `json:"volume_bytes"` // -1 = unlimited
	UsedBytes       int64    `json:"used_bytes"`
	UpBytes         int64    `json:"up_bytes"`
	DownBytes       int64    `json:"down_bytes"`
	RemainingBytes  int64    `json:"remaining_bytes"` // -1 = unlimited
	ExpiresUnix     int64    `json:"expires_unix"`    // the set expiry (0 = none or not started)
	EffectiveExpiry int64    `json:"effective_expiry_unix"`
	DurationSeconds int64    `json:"on_hold_seconds,omitempty"`
	MaxIPs          int      `json:"max_ips"`
	MaxDevices      int      `json:"max_devices"`
	Inbounds        []string `json:"inbounds"`
	Online          bool     `json:"online"`
	FirstSeen       int64    `json:"first_seen_unix"`
	LastSeen        int64    `json:"last_seen_unix"`
	CreatedUnix     int64    `json:"created_unix"`
	Note            string   `json:"note"`
	Rev             int64    `json:"rev"`
	SubFetched      int64    `json:"sub_fetched_unix"` // last subscription fetch (0 = never since it came here)
	SubURL          string   `json:"subscription_url,omitempty"`
}

// tenantClientView turns a client into the shop's view.
func (s *Store) tenantClientView(ctx context.Context, tenant string, c ClientView) TenantClient {
	return s.tenantClientViews(ctx, tenant, []ClientView{c})[0]
}

// tenantClientViews turns many clients into the shop's view, reading the
// shop-only fields of all of them in one query.
func (s *Store) tenantClientViews(ctx context.Context, tenant string, list []ClientView) []TenantClient {
	type extra struct {
		secret, ref     string
		created, r1, r2 int64
	}
	more := map[string]extra{}
	emails := make([]string, len(list))
	for i, c := range list {
		emails[i] = c.Email
	}
	if rows, err := s.ro.QueryContext(ctx, `SELECT c.email,c.secret,c.external_ref,c.created_unix,c.rev,COALESCE(u.rev,0)
FROM clients c LEFT JOIN client_usage u ON u.email=c.email WHERE c.email IN (SELECT value FROM json_each(?))`, jsonList(emails)); err == nil {
		for rows.Next() {
			var e string
			var x extra
			if rows.Scan(&e, &x.secret, &x.ref, &x.created, &x.r1, &x.r2) == nil {
				more[e] = x
			}
		}
		rows.Close()
	}
	fetched := s.subFetchedAt(ctx, emails)
	out := make([]TenantClient, len(list))
	for i, c := range list {
		tc := s.tenantClientBase(tenant, c)
		x := more[c.Email]
		tc.TrojanPassword, tc.ExternalRef, tc.CreatedUnix, tc.Rev = x.secret, x.ref, x.created, max(x.r1, x.r2)
		tc.SubFetched = fetched[c.Email]
		out[i] = tc
	}
	return out
}

func (s *Store) tenantClientBase(tenant string, c ClientView) TenantClient {
	tc := TenantClient{Name: tenantName(tenant, c.Email), Status: pgStatus(c, s.now().Unix()), Enabled: c.Enabled,
		UUID: c.UUID, SubToken: c.SubToken, VolumeBytes: c.VolumeBytes, UsedBytes: c.Usage.Up + c.Usage.Down,
		UpBytes: c.Usage.Up, DownBytes: c.Usage.Down, RemainingBytes: c.Remaining, ExpiresUnix: c.ExpiresUnix,
		EffectiveExpiry: c.EffectiveUntil, MaxIPs: c.MaxIPs, MaxDevices: c.HWIDLimit, Inbounds: c.Inbounds,
		Online: c.Online, FirstSeen: c.Usage.FirstSeen, LastSeen: c.Usage.LastSeen, Note: c.Note}
	if c.ExpiresUnix == 0 && c.DurationSeconds > 0 {
		tc.DurationSeconds = c.DurationSeconds
	}
	if tc.VolumeBytes < 0 {
		tc.VolumeBytes, tc.RemainingBytes = -1, -1
	}
	if tc.Inbounds == nil {
		tc.Inbounds = []string{}
	}
	return tc
}

// tenantOwn reads one of the tenant's clients by the shop's name.
func (s *Store) tenantOwn(ctx context.Context, tenant, name string) (ClientView, error) {
	if !tenantNameRE.MatchString(name) {
		return ClientView{}, ErrNotFound
	}
	list, err := s.clientViews(ctx, "c.email=? AND c.rep_id=?", tenantEmail(tenant, name), tenantRep(tenant))
	if err != nil {
		return ClientView{}, err
	}
	if len(list) == 0 {
		return ClientView{}, ErrNotFound
	}
	return list[0], nil
}

// TenantClient reads one client.
func (s *Store) TenantClient(ctx context.Context, tenant, name string) (TenantClient, error) {
	c, err := s.tenantOwn(ctx, tenant, name)
	if err != nil {
		return TenantClient{}, err
	}
	return s.tenantClientView(ctx, tenant, c), nil
}

// TenantClientByRef finds a client by the shop's own reference.
func (s *Store) TenantClientByRef(ctx context.Context, tenant, ref string) (TenantClient, error) {
	var email string
	if ref == "" || s.ro.QueryRowContext(ctx, `SELECT email FROM clients WHERE rep_id=? AND external_ref=?`, tenantRep(tenant), ref).Scan(&email) != nil {
		return TenantClient{}, ErrNotFound
	}
	return s.TenantClient(ctx, tenant, tenantName(tenant, email))
}

// TenantClientPage lists the tenant's clients by name, after a name (paging).
func (s *Store) TenantClientPage(ctx context.Context, tenant, after string, limit int) ([]TenantClient, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	list, err := s.clientViews(ctx, "c.rep_id=? AND c.email>? AND c.email IN (SELECT email FROM clients WHERE rep_id=? AND email>? ORDER BY email LIMIT ?)",
		tenantRep(tenant), tenantEmail(tenant, after), tenantRep(tenant), tenantEmail(tenant, after), limit)
	if err != nil {
		return nil, err
	}
	out := s.tenantClientViews(ctx, tenant, list)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// tenantApply fills c from what the shop sent.
func (s *Store) tenantApply(ctx context.Context, t Tenant, c *Client, in TenantClientIn, isNew bool) error {
	now := s.now().Unix()
	if in.VolumeBytes != nil {
		switch v := *in.VolumeBytes; {
		case v < 0:
			c.VolumeBytes = -1
		case v == 0:
			return fmt.Errorf("%w: volume_bytes is a positive number of bytes, or -1 for unlimited", ErrInvalid)
		default:
			c.VolumeBytes = v
		}
	} else if isNew {
		c.VolumeBytes = -1
	}
	terms := 0
	for _, set := range []bool{in.ExpiresUnix != nil, in.Days != nil, in.OnHoldDays != nil} {
		if set {
			terms++
		}
	}
	if terms > 1 {
		return fmt.Errorf("%w: send only one of expires_unix, days and on_hold_days", ErrInvalid)
	}
	switch {
	case in.ExpiresUnix != nil:
		if *in.ExpiresUnix < 0 || (*in.ExpiresUnix > 0 && *in.ExpiresUnix <= now && isNew) {
			return fmt.Errorf("%w: expires_unix is in the past", ErrInvalid)
		}
		c.ExpiresUnix, c.DurationSeconds = *in.ExpiresUnix, 0
	case in.Days != nil:
		if *in.Days <= 0 || *in.Days > 3650 {
			return fmt.Errorf("%w: days is 1 to 3650", ErrInvalid)
		}
		c.ExpiresUnix, c.DurationSeconds = now+*in.Days*86400, 0
	case in.OnHoldDays != nil:
		if *in.OnHoldDays <= 0 || *in.OnHoldDays > 3650 {
			return fmt.Errorf("%w: on_hold_days is 1 to 3650", ErrInvalid)
		}
		c.ExpiresUnix, c.DurationSeconds = 0, *in.OnHoldDays*86400
	}
	if in.Enabled != nil {
		c.Enabled = *in.Enabled
	} else if isNew {
		c.Enabled = true
	}
	if in.MaxIPs != nil {
		if *in.MaxIPs < 0 || *in.MaxIPs > 1000 {
			return fmt.Errorf("%w: max_ips is 0 to 1000", ErrInvalid)
		}
		c.MaxIPs = *in.MaxIPs
	}
	if in.MaxDevices != nil && (*in.MaxDevices < 0 || *in.MaxDevices > 100) {
		return fmt.Errorf("%w: max_devices is 0 to 100", ErrInvalid)
	}
	if in.Inbounds != nil || isNew {
		allowed := s.TenantInbounds(ctx, t)
		tags := in.Inbounds
		if len(tags) == 0 {
			tags = allowed
		}
		seen := map[string]bool{}
		c.Inbounds = c.Inbounds[:0]
		for _, tag := range tags {
			if !contains(allowed, tag) {
				return fmt.Errorf("%w: inbound %q is not available to this shop", ErrInvalid, tag)
			}
			if !seen[tag] {
				seen[tag] = true
				c.Inbounds = append(c.Inbounds, tag)
			}
		}
		if len(c.Inbounds) == 0 {
			return fmt.Errorf("%w: no inbound is available to this shop", ErrInvalid)
		}
	}
	if in.ExternalRef != nil && *in.ExternalRef != "" && !externalRefRE.MatchString(*in.ExternalRef) {
		return fmt.Errorf("%w: external_ref is 1–128 printable characters without spaces", ErrInvalid)
	}
	if in.Note != nil && len(*in.Note) > 2000 {
		return fmt.Errorf("%w: the note is too long", ErrInvalid)
	}
	return nil
}

// checkCredentials validates credentials a shop brings along on a create,
// so a moved customer keeps the configs and link they already have.
func (s *Store) checkCredentials(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, in TenantClientIn) error {
	var x int
	if in.UUID != "" {
		if !uuidRE.MatchString(strings.ToLower(in.UUID)) {
			return fmt.Errorf("%w: uuid looks like 1b2c3d4e-0000-4000-8000-000000000000", ErrInvalid)
		}
		if q.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE uuid=?`, strings.ToLower(in.UUID)).Scan(&x) == nil {
			return fmt.Errorf("%w: this uuid is already in use", ErrConflict)
		}
	}
	if in.TrojanPassword != "" {
		if !secretRE.MatchString(in.TrojanPassword) {
			return fmt.Errorf("%w: trojan_password is 4–128 letters, digits or . _ ~ + = -", ErrInvalid)
		}
		if q.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE secret=?`, in.TrojanPassword).Scan(&x) == nil {
			return fmt.Errorf("%w: this trojan_password is already in use", ErrConflict)
		}
	}
	if in.SubToken != "" {
		if !subTokenRE.MatchString(in.SubToken) { // shorter tokens do not open /sub/<token>
			return fmt.Errorf("%w: sub_token is 16–64 letters, digits, _ or -", ErrInvalid)
		}
		if q.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE sub_token=?`, in.SubToken).Scan(&x) == nil {
			return fmt.Errorf("%w: this sub_token is already in use", ErrConflict)
		}
	}
	return nil
}

// ErrConflict is a name, reference or credential that is already taken.
var ErrConflict = errors.New("conflict")

// tenantCaps checks the tenant's caps inside tx, after the change.
func tenantCaps(ctx context.Context, tx *sql.Tx, t Tenant) error {
	if t.MaxClients <= 0 && t.MaxBytes <= 0 {
		return nil
	}
	var n int
	var vol int64
	var unlimited int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN volume_bytes>0 THEN volume_bytes ELSE 0 END),0),
COALESCE(SUM(CASE WHEN volume_bytes<0 THEN 1 ELSE 0 END),0) FROM clients WHERE rep_id=?`, tenantRep(t.ID)).Scan(&n, &vol, &unlimited); err != nil {
		return err
	}
	if t.MaxClients > 0 && n > t.MaxClients {
		return fmt.Errorf("%w: this shop may have at most %d clients", ErrPolicy, t.MaxClients)
	}
	if t.MaxBytes > 0 && unlimited > 0 {
		return fmt.Errorf("%w: this shop has a volume cap, so its clients cannot be unlimited", ErrPolicy)
	}
	if t.MaxBytes > 0 && vol > t.MaxBytes {
		return fmt.Errorf("%w: this shop's clients may have at most %s together", ErrPolicy, gbText(t.MaxBytes))
	}
	return nil
}

// tenantWrite stores c (and the shop-only fields) for the tenant inside tx.
func (s *Store) tenantWrite(ctx context.Context, tx *sql.Tx, t Tenant, c Client, in TenantClientIn, isNew bool) error {
	ctx = withTenantAccess(ctx, t.ID)
	if err := putClientTx(ctx, tx, c, s.now().Unix()); err != nil {
		return err
	}
	plan := "volume"
	if c.VolumeBytes < 0 {
		plan = "unlimited"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET rep_id=?, plan=? WHERE email=?`, tenantRep(t.ID), plan, c.Email); err != nil {
		return err
	}
	if in.ExternalRef != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET external_ref=? WHERE email=?`, *in.ExternalRef, c.Email); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return fmt.Errorf("%w: external_ref %q is already used by another client", ErrConflict, *in.ExternalRef)
			}
			return err
		}
	}
	if in.MaxDevices != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET hwid_limit=? WHERE email=?`, *in.MaxDevices, c.Email); err != nil {
			return err
		}
	}
	if in.Note != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET note=? WHERE email=?`, strings.TrimSpace(*in.Note), c.Email); err != nil {
			return err
		}
	}
	return tenantCaps(ctx, tx, t)
}

// tenantReady is the tenant, if it may make changes now.
func (s *Store) tenantReady(ctx context.Context, id string) (Tenant, error) {
	t, err := s.TenantByID(ctx, id)
	if err != nil {
		return t, ErrAuth
	}
	if !t.Enabled {
		return t, fmt.Errorf("%w: %v", ErrPolicy, ErrTenantOff)
	}
	return t, nil
}

// TenantCreate makes one client.
func (s *Store) TenantCreate(ctx context.Context, tenant string, in TenantClientIn) (TenantClient, error) {
	t, err := s.tenantReady(ctx, tenant)
	if err != nil {
		return TenantClient{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TenantClient{}, err
	}
	defer tx.Rollback()
	if err := s.tenantCreateTx(ctx, tx, t, in); err != nil {
		return TenantClient{}, err
	}
	if err := tx.Commit(); err != nil {
		return TenantClient{}, err
	}
	return s.TenantClient(ctx, tenant, in.Name)
}

func (s *Store) tenantCreateTx(ctx context.Context, tx *sql.Tx, t Tenant, in TenantClientIn) error {
	in.Name = strings.TrimSpace(in.Name)
	if !tenantNameRE.MatchString(in.Name) {
		return fmt.Errorf("%w: name is 1–100 English letters, digits or . _ @ - (starting with a letter or digit)", ErrInvalid)
	}
	email := tenantEmail(t.ID, in.Name)
	var x int
	if tx.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=? COLLATE NOCASE`, email).Scan(&x) == nil {
		return fmt.Errorf("%w: client %q already exists", ErrConflict, in.Name)
	}
	if err := s.checkCredentials(ctx, tx, in); err != nil {
		return err
	}
	c := Client{Email: email, UUID: strings.ToLower(in.UUID), Secret: in.TrojanPassword, SubToken: in.SubToken}
	if err := s.tenantApply(ctx, t, &c, in, true); err != nil {
		return err
	}
	return s.tenantWrite(ctx, tx, t, c, in, true)
}

// TenantUpdate changes one client; nil fields stay as they are.
func (s *Store) TenantUpdate(ctx context.Context, tenant, name string, in TenantClientIn) (TenantClient, error) {
	t, err := s.tenantReady(ctx, tenant)
	if err != nil {
		return TenantClient{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TenantClient{}, err
	}
	defer tx.Rollback()
	if err := s.tenantUpdateTx(ctx, tx, t, name, in); err != nil {
		return TenantClient{}, err
	}
	if err := tx.Commit(); err != nil {
		return TenantClient{}, err
	}
	return s.TenantClient(ctx, tenant, name)
}

func (s *Store) tenantUpdateTx(ctx context.Context, tx *sql.Tx, t Tenant, name string, in TenantClientIn) error {
	if in.UUID != "" || in.TrojanPassword != "" || in.SubToken != "" {
		return fmt.Errorf("%w: credentials are set when a client is made; use the rotate actions to change them", ErrInvalid)
	}
	cur, err := s.tenantOwnTx(ctx, tx, t.ID, name)
	if err != nil {
		return err
	}
	c := cur
	c.UUID, c.Secret, c.SubToken = "", "", "" // kept as they are
	if err := s.tenantApply(ctx, t, &c, in, false); err != nil {
		return err
	}
	return s.tenantWrite(ctx, tx, t, c, in, false)
}

// tenantOwnTx reads the stored client inside tx.
func (s *Store) tenantOwnTx(ctx context.Context, tx *sql.Tx, tenant, name string) (Client, error) {
	var c Client
	if !tenantNameRE.MatchString(name) {
		return c, ErrNotFound
	}
	err := tx.QueryRowContext(ctx, `SELECT email,enabled,volume_bytes,expires_unix,duration_seconds,max_ips FROM clients WHERE email=? AND rep_id=?`,
		tenantEmail(tenant, name), tenantRep(tenant)).Scan(&c.Email, &c.Enabled, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds, &c.MaxIPs)
	if err != nil {
		return c, ErrNotFound
	}
	rows, err := tx.QueryContext(ctx, `SELECT tag FROM client_inbounds WHERE email=? ORDER BY tag`, c.Email)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return c, err
		}
		c.Inbounds = append(c.Inbounds, tag)
	}
	return c, rows.Err()
}

// TenantRenewIn adds to a client: volume, days, or both, optionally after
// clearing what it used.
type TenantRenewIn struct {
	AddBytes   int64 `json:"add_bytes"`
	AddDays    int64 `json:"add_days"`
	ResetUsage bool  `json:"reset_usage"`
	Enable     bool  `json:"enable"` // also switch it on
}

// TenantRenew adds volume and days. Days are added to the current expiry, or
// to now when it has passed; an unstarted (on-hold) term gets longer.
func (s *Store) TenantRenew(ctx context.Context, tenant, name string, in TenantRenewIn) (TenantClient, error) {
	t, err := s.tenantReady(ctx, tenant)
	if err != nil {
		return TenantClient{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TenantClient{}, err
	}
	defer tx.Rollback()
	if err := s.tenantRenewTx(ctx, tx, t, name, in); err != nil {
		return TenantClient{}, err
	}
	if err := tx.Commit(); err != nil {
		return TenantClient{}, err
	}
	return s.TenantClient(ctx, tenant, name)
}

func (s *Store) tenantRenewTx(ctx context.Context, tx *sql.Tx, t Tenant, name string, in TenantRenewIn) error {
	if in.AddBytes < 0 || in.AddDays < 0 || in.AddDays > 3650 || (in.AddBytes == 0 && in.AddDays == 0 && !in.ResetUsage && !in.Enable) {
		return fmt.Errorf("%w: send add_bytes and/or add_days (positive), reset_usage or enable", ErrInvalid)
	}
	c, err := s.tenantOwnTx(ctx, tx, t.ID, name)
	if err != nil {
		return err
	}
	now := s.now().Unix()
	if in.AddBytes > 0 && c.VolumeBytes > 0 {
		c.VolumeBytes += in.AddBytes
	}
	if in.AddDays > 0 {
		add := in.AddDays * 86400
		switch {
		case c.ExpiresUnix == 0 && c.DurationSeconds > 0:
			var first int64
			_ = tx.QueryRowContext(ctx, `SELECT first_seen_unix FROM client_usage WHERE email=?`, c.Email).Scan(&first)
			if first == 0 {
				c.DurationSeconds += add
			} else {
				c.ExpiresUnix, c.DurationSeconds = max(first+c.DurationSeconds, now)+add, 0
			}
		case c.ExpiresUnix > 0:
			c.ExpiresUnix = max(c.ExpiresUnix, now) + add
		}
		// No expiry stays no expiry.
	}
	if in.Enable {
		c.Enabled = true
	}
	if err := s.tenantWrite(ctx, tx, t, c, TenantClientIn{}, false); err != nil {
		return err
	}
	if in.ResetUsage {
		if err := resetUsageTx(ctx, tx, c.Email, "api", now); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, now, "", c.Email, "renew")
	return err
}

// TenantDelete removes one client.
func (s *Store) TenantDelete(ctx context.Context, tenant, name string) error {
	if _, err := s.tenantReady(ctx, tenant); err != nil {
		return err
	}
	if _, err := s.tenantOwn(ctx, tenant, name); err != nil {
		return err
	}
	return s.DeleteClient(ctx, tenantEmail(tenant, name))
}

// TenantResetUsage clears what a client used.
func (s *Store) TenantResetUsage(ctx context.Context, tenant, name string) (TenantClient, error) {
	if _, err := s.tenantReady(ctx, tenant); err != nil {
		return TenantClient{}, err
	}
	if _, err := s.tenantOwn(ctx, tenant, name); err != nil {
		return TenantClient{}, err
	}
	if err := s.ResetUsage(ctx, tenantEmail(tenant, name), "api"); err != nil {
		return TenantClient{}, err
	}
	return s.TenantClient(ctx, tenant, name)
}

// TenantRotate gives a client a new subscription link and/or new credentials.
func (s *Store) TenantRotate(ctx context.Context, tenant, name string, creds, link bool) (TenantClient, error) {
	if _, err := s.tenantReady(ctx, tenant); err != nil {
		return TenantClient{}, err
	}
	if _, err := s.tenantOwn(ctx, tenant, name); err != nil {
		return TenantClient{}, err
	}
	if err := s.RotateClient(ctx, tenantEmail(tenant, name), creds, link); err != nil {
		return TenantClient{}, err
	}
	return s.TenantClient(ctx, tenant, name)
}
