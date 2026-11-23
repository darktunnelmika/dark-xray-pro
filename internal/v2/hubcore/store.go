// Package hubcore is the v2 HUB execution core: node registry, desired
// config, and compact usage accounting. It never holds a lock or a
// transaction while waiting on the network, and nothing it does can stop a
// Node's traffic: Nodes keep serving with their last state.
package hubcore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid")
	ErrAuth     = errors.New("unauthorized")
	// ErrTOTPRequired: the password was right and the login has two-step on.
	ErrTOTPRequired = errors.New("2fa code required")
)

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// emailRE: client names double as Xray stat keys, so ">>>" and spaces are
// impossible by construction.
var emailRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+-]{0,127}$`)

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  token_sha256 TEXT NOT NULL,
  created_unix INTEGER NOT NULL,
  last_seen_unix INTEGER NOT NULL DEFAULT 0,
  agent_version TEXT NOT NULL DEFAULT '',
  xray_running INTEGER NOT NULL DEFAULT 0,
  xray_restarts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  spooled INTEGER NOT NULL DEFAULT 0,
  reported_revision INTEGER NOT NULL DEFAULT 0,
  config_revision INTEGER NOT NULL DEFAULT 0,
  config_sha256 TEXT NOT NULL DEFAULT '',
  usage_seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS clients (
  email TEXT PRIMARY KEY,
  uuid TEXT NOT NULL,
  secret TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  volume_bytes INTEGER NOT NULL DEFAULT -1,
  expires_unix INTEGER NOT NULL DEFAULT 0,
  duration_seconds INTEGER NOT NULL DEFAULT 0,
  max_ips INTEGER NOT NULL DEFAULT 0,
  sub_token TEXT NOT NULL DEFAULT '',
  created_unix INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS clients_sub_token ON clients(sub_token) WHERE sub_token<>'';
CREATE TABLE IF NOT EXISTS inbounds (
  tag TEXT PRIMARY KEY,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  template TEXT NOT NULL,
  meta TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS client_inbounds (
  email TEXT NOT NULL REFERENCES clients(email) ON DELETE CASCADE,
  tag TEXT NOT NULL REFERENCES inbounds(tag) ON DELETE CASCADE,
  PRIMARY KEY(email, tag)
);
CREATE TABLE IF NOT EXISTS client_usage (
  email TEXT PRIMARY KEY,
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  first_seen_unix INTEGER NOT NULL DEFAULT 0,
  last_seen_unix INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS usage_daily (
  email TEXT NOT NULL,
  node_id TEXT NOT NULL,
  day TEXT NOT NULL,
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(email, node_id, day)
);
CREATE TABLE IF NOT EXISTS online (
  email TEXT NOT NULL,
  node_id TEXT NOT NULL,
  ips TEXT NOT NULL,
  seen_unix INTEGER NOT NULL,
  PRIMARY KEY(email, node_id)
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  unix INTEGER NOT NULL,
  node_id TEXT NOT NULL,
  email TEXT NOT NULL,
  kind TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_unix ON events(unix);
`

// Store holds all v2 HUB state in one small SQLite file. A single writer
// connection serialises short transactions; readers use their own pool.
type Store struct {
	logins loginGuard // wrong-password counters (security.go)
	gate   gateState  // secret panel path (panelpath.go)

	db   *sql.DB
	ro   *sql.DB
	now  func() time.Time
	path string

	distMu  sync.Mutex
	distDir string // dark-node and xray for installs and self-update
	distSHA string
	distMod time.Time
}

// tmpDir is next to the database, so snapshots stay on the same disk.
func (s *Store) tmpDir() string { return filepath.Dir(s.path) }

func Open(path string) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	q := "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	u := url.URL{Scheme: "file", Path: path, RawQuery: q}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	ru := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)"}
	ro, err := sql.Open("sqlite", ru.String())
	if err != nil {
		db.Close()
		return nil, err
	}
	ro.SetMaxOpenConns(4)
	st := &Store{db: db, ro: ro, now: time.Now, path: path}
	if err := st.ensureInboundLimitColumns(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureAuthSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureRepSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureStaffSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureTOTPSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureAPIKeySchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureWGSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureOutboundTrafficSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureOldUsersSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureRepCtlSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureSellerSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureInstallSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureManageSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureSwapSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureCertSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureShopSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureTgSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureTgRepSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureTgAdminSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureBotLookSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensurePartnerSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensurePGSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureWARPSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureTicketSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureOutboxSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureRenameSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureRepSaleSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureResaleSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureLegacySchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureRouteSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureTaskSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureACMESchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureTgSchedSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureResetSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensurePlanSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureTomanSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureAutoRenewSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureDiscountSchema(); err != nil {
		st.Close()
		return nil, err
	}
	if err := st.ensureCommerceSchema(); err != nil { // alters products and orders: after the shop schema
		st.Close()
		return nil, err
	}
	// Last: its first fill reads tables the steps above create.
	if err := st.ensureTgBotUsers(); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error {
	return errors.Join(s.ro.Close(), s.db.Close())
}

// SetClock is for tests.
func (s *Store) SetClock(now func() time.Time) { s.now = now }

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// newRowID is the row id for a new row of table: past every id the table
// ever gave out, so the id of a deleted row never goes to another one.
// Sales bots keep users, templates and groups by these numbers.
func newRowID(ctx context.Context, tx *sql.Tx, table string) (int64, error) {
	key := "rowid_hw_" + table
	var hw, top int64
	_ = tx.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM settings WHERE key=?`, key).Scan(&hw)
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(rowid),0) FROM `+table).Scan(&top); err != nil {
		return 0, err
	}
	id := max(hw, top) + 1
	_, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, fmt.Sprint(id))
	return id, err
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// RandomUUID returns a v4 UUID string.
func RandomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// CreateNode registers a node and returns its token (shown once).
func (s *Store) CreateNode(ctx context.Context, id, name string) (string, error) {
	if !idRE.MatchString(id) {
		return "", ErrInvalid
	}
	tok := randomHex(32)
	_, err := s.db.ExecContext(ctx, `INSERT INTO nodes(id,name,token_sha256,created_unix) VALUES(?,?,?,?)`,
		id, name, hashToken(tok), s.now().Unix())
	return tok, err
}

// RotateNodeToken replaces a node token (the old one stops working).
func (s *Store) RotateNodeToken(ctx context.Context, id string) (string, error) {
	tok := randomHex(32)
	r, err := s.db.ExecContext(ctx, `UPDATE nodes SET token_sha256=? WHERE id=?`, hashToken(tok), id)
	if err != nil {
		return "", err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return "", ErrNotFound
	}
	return tok, nil
}

// AuthNode checks a node token in constant time.
func (s *Store) AuthNode(ctx context.Context, id, tok string) error {
	var want string
	err := s.ro.QueryRowContext(ctx, `SELECT token_sha256 FROM nodes WHERE id=?`, id).Scan(&want)
	if err != nil {
		return ErrAuth
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(hashToken(tok))) != 1 {
		return ErrAuth
	}
	return nil
}

// Client is the v2 HUB's view of a subscription. VolumeBytes < 0 means
// unlimited. DurationSeconds > 0 with ExpiresUnix == 0 means the term
// starts on the first byte the client sends.
type Client struct {
	Email           string
	UUID            string
	Secret          string
	Enabled         bool
	VolumeBytes     int64
	ExpiresUnix     int64
	DurationSeconds int64
	MaxIPs          int
	Inbounds        []string
	SubToken        string
}

func (s *Store) PutClient(ctx context.Context, c Client) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := putClientTx(ctx, tx, c, s.now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// putClientTx writes a client inside the caller's transaction, so a charge
// and the change it pays for commit or fail together.
func putClientTx(ctx context.Context, tx *sql.Tx, c Client, now int64) error {
	if !emailRE.MatchString(c.Email) || c.MaxIPs < 0 || c.DurationSeconds < 0 {
		return ErrInvalid
	}
	// An update that omits credentials keeps the existing ones, so editing a
	// client never breaks its connections or its subscription link.
	var oldUUID, oldSecret, oldTok string
	var rowid any // NULL keeps an existing row's id
	if errors.Is(tx.QueryRowContext(ctx, `SELECT uuid,secret,sub_token FROM clients WHERE email=?`, c.Email).Scan(&oldUUID, &oldSecret, &oldTok), sql.ErrNoRows) {
		id, err := newRowID(ctx, tx, "clients")
		if err != nil {
			return err
		}
		rowid = id
	}
	c.UUID = firstNonEmpty(c.UUID, oldUUID, RandomUUID())
	c.Secret = firstNonEmpty(c.Secret, oldSecret, randomHex(16))
	c.SubToken = firstNonEmpty(c.SubToken, oldTok, randomHex(16))
	_, err := tx.ExecContext(ctx, `INSERT INTO clients(rowid,email,uuid,secret,enabled,volume_bytes,expires_unix,duration_seconds,max_ips,sub_token,created_unix)
VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(email) DO UPDATE SET uuid=excluded.uuid, secret=excluded.secret, enabled=excluded.enabled,
volume_bytes=excluded.volume_bytes, expires_unix=excluded.expires_unix, duration_seconds=excluded.duration_seconds, max_ips=excluded.max_ips,
sub_token=excluded.sub_token`,
		rowid, c.Email, c.UUID, c.Secret, c.Enabled, c.VolumeBytes, c.ExpiresUnix, c.DurationSeconds, c.MaxIPs, c.SubToken, now)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM client_inbounds WHERE email=?`, c.Email); err != nil {
		return err
	}
	for _, tag := range c.Inbounds {
		if _, err = tx.ExecContext(ctx, `INSERT INTO client_inbounds(email,tag) VALUES(?,?)`, c.Email, tag); err != nil {
			return fmt.Errorf("%w: inbound %q", ErrInvalid, tag)
		}
	}
	return nil
}

// PutInbound stores an Xray inbound template for one node. The HUB fills
// settings.clients when it renders the node config. meta is the client-side
// metadata (public endpoints, REALITY public key) used for subscriptions.
func (s *Store) PutInbound(ctx context.Context, tag, nodeID string, template []byte, enabled bool) error {
	return s.PutInboundMeta(ctx, tag, nodeID, template, nil, enabled)
}

func (s *Store) PutInboundMeta(ctx context.Context, tag, nodeID string, template, meta []byte, enabled bool) error {
	if !idRE.MatchString(tag) || tag == apiTag {
		return ErrInvalid
	}
	if _, err := parseTemplate(template); err != nil {
		return err
	}
	if len(meta) == 0 {
		meta = []byte("{}")
	}
	var m sub.Meta
	if err := json.Unmarshal(meta, &m); err != nil {
		return fmt.Errorf("%w: inbound meta: %v", ErrInvalid, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cur string
	var rowid any // NULL keeps an existing row's id
	if tx.QueryRowContext(ctx, `SELECT node_id FROM inbounds WHERE tag=?`, tag).Scan(&cur) == nil {
		var x int
		if nodeID == "" || tx.QueryRowContext(ctx, `SELECT 1 FROM inbound_routes WHERE tag=?`, tag).Scan(&x) == nil {
			nodeID = cur // a placed inbound keeps its routes; edit them with PutRoutes
		}
	} else {
		id, err := newRowID(ctx, tx, "inbounds")
		if err != nil {
			return err
		}
		rowid = id
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO inbounds(rowid,tag,node_id,template,meta,enabled) VALUES(?,?,?,?,?,?)
ON CONFLICT(tag) DO UPDATE SET node_id=excluded.node_id, template=excluded.template, meta=excluded.meta, enabled=excluded.enabled`,
		rowid, tag, nodeID, string(template), string(meta), enabled); err != nil {
		return err
	}
	if err := s.legacyRoute(ctx, tx, tag, nodeID, template, meta); err != nil {
		return err
	}
	return tx.Commit()
}

// Subscription returns the client's share links and userinfo header.
func (s *Store) Subscription(ctx context.Context, email string) (links []string, userinfo string, err error) {
	acc, ins, info, err := s.subscriptionData(ctx, email)
	if err != nil {
		return nil, "", err
	}
	links, err = sub.Links(acc, ins)
	return links, info, err
}

// SubscriptionBody renders the subscription in the requested format
// ("base64", "singbox" or "clash").
func (s *Store) SubscriptionBody(ctx context.Context, email, format string) (body []byte, contentType, userinfo string, err error) {
	acc, ins, info, err := s.subscriptionData(ctx, email)
	if err != nil {
		return nil, "", "", err
	}
	switch format {
	case "singbox":
		body, err = sub.SingBox(acc, ins)
		return body, "application/json; charset=utf-8", info, err
	case "clash":
		body, err = sub.Clash(acc, ins)
		return body, "text/yaml; charset=utf-8", info, err
	case "xray":
		body, err = sub.XrayJSON(acc, ins)
		return body, "application/json; charset=utf-8", info, err
	}
	links, err := sub.Links(acc, ins)
	return []byte(sub.Base64(links)), "text/plain; charset=utf-8", info, err
}

func (s *Store) subscriptionData(ctx context.Context, email string) (sub.Account, []sub.Inbound, string, error) {
	var c Client
	err := s.ro.QueryRowContext(ctx, `SELECT email,uuid,secret,enabled,volume_bytes,expires_unix,duration_seconds,max_ips FROM clients WHERE email=?`, email).
		Scan(&c.Email, &c.UUID, &c.Secret, &c.Enabled, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds, &c.MaxIPs)
	if errors.Is(err, sql.ErrNoRows) {
		return sub.Account{}, nil, "", ErrNotFound
	}
	if err != nil {
		return sub.Account{}, nil, "", err
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT i.tag,i.template,i.meta FROM inbounds i JOIN client_inbounds ci ON ci.tag=i.tag
WHERE ci.email=? AND i.enabled=1 ORDER BY i.tag`, email)
	if err != nil {
		return sub.Account{}, nil, "", err
	}
	var ins []sub.Inbound
	for rows.Next() {
		var in sub.Inbound
		var tpl, meta string
		if err := rows.Scan(&in.Tag, &tpl, &meta); err != nil {
			rows.Close()
			return sub.Account{}, nil, "", err
		}
		in.Template = []byte(tpl)
		if err := json.Unmarshal([]byte(meta), &in.Meta); err != nil {
			rows.Close()
			return sub.Account{}, nil, "", err
		}
		ins = append(ins, in)
	}
	rows.Close()
	for i := range ins {
		eps, err := s.routeEndpoints(ctx, ins[i].Tag)
		if err != nil {
			return sub.Account{}, nil, "", err
		}
		extra, err := s.swapEndpoints(ctx, ins[i].Tag)
		if err != nil {
			return sub.Account{}, nil, "", err
		}
		ins[i].Meta.Endpoints = append(eps, extra...)
	}
	for i := range ins {
		var t map[string]any
		if json.Unmarshal(ins[i].Template, &t) != nil || (t["protocol"] != "wireguard" && t["protocol"] != "amneziawg") {
			continue
		}
		ip, err := s.wgIP(ctx, ins[i].Tag, c.Email, wgSubnet(t))
		if err != nil {
			return sub.Account{}, nil, "", err
		}
		ins[i].Meta.WGPrivateKey, _ = WGKeys(c.Secret)
		ins[i].Meta.WGAddress = ip + "/32"
	}
	ins = sub.ExpandFallbacks(ins)
	ins = s.rankInbounds(ctx, ins)
	if !c.Enabled {
		ins = nil
	}
	u, err := s.ClientUsage(ctx, email)
	if err != nil {
		return sub.Account{}, nil, "", err
	}
	total := c.VolumeBytes
	if total < 0 {
		total = 0
	}
	return s.subAccount(ctx, c, u), ins, sub.Userinfo(u.Up, u.Down, total, EffectiveExpiry(c, u)), nil
}

// ClientView is a client with its live usage, for the panel.
type ClientView struct {
	Client
	RepID          string
	Plan           string
	Note           string
	Usage          Usage
	EffectiveUntil int64
	Remaining      int64
	Online         bool
	ResetPeriod    string    `json:"reset_period"`
	LastReset      int64     `json:"last_reset"`
	NextReset      int64     `json:"next_reset,omitempty"`
	NextPlan       *NextPlan `json:"next_plan,omitempty"`
	HWIDLimit      int       `json:"hwid_limit"`
	SpeedKbps      int       `json:"speed_kbps"`         // 0 = no limit
	StartBy        int64     `json:"start_by,omitempty"` // on hold: the term starts by then even if unused
	Trial          bool      `json:"trial,omitempty"`    // a free trial from a Telegram shop
	Legacy         bool      `json:"legacy,omitempty"`   // an old user brought from another panel (legacy.go)
}

// Clients lists every client with usage, newest first.
func (s *Store) Clients(ctx context.Context) ([]ClientView, error) {
	return s.clientViews(ctx, "", "")
}

// clientView is one client, read on its own (not the whole list).
func (s *Store) clientView(ctx context.Context, email string) (ClientView, error) {
	list, err := s.clientViews(ctx, "c.email=?", email)
	if err != nil {
		return ClientView{}, err
	}
	if len(list) == 0 {
		return ClientView{}, ErrNotFound
	}
	return list[0], nil
}

// clientViews reads clients; where (with one argument) narrows them to one
// email ("c.email=?") or one seller ("c.rep_id=?").
func (s *Store) clientViews(ctx context.Context, where, arg string) ([]ClientView, error) {
	q := `SELECT c.email,c.uuid,c.enabled,c.volume_bytes,c.expires_unix,c.duration_seconds,c.max_ips,c.sub_token,c.rep_id,c.plan,c.note,
COALESCE(u.up,0),COALESCE(u.down,0),COALESCE(u.first_seen_unix,0),COALESCE(u.last_seen_unix,0),
EXISTS(SELECT 1 FROM online o WHERE o.email=c.email AND o.seen_unix>=?),c.reset_period,c.last_reset_unix,c.next_plan,c.hwid_limit,c.start_by_unix,c.trial,
EXISTS(SELECT 1 FROM legacy_clients l WHERE l.email=c.email)
FROM clients c LEFT JOIN client_usage u ON u.email=c.email`
	args := []any{s.now().Unix() - 120}
	if where != "" {
		q += " WHERE " + where
		args = append(args, arg)
	}
	rows, err := s.ro.QueryContext(ctx, q+` ORDER BY c.created_unix DESC, c.email`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientView
	for rows.Next() {
		var v ClientView
		var np string
		if err := rows.Scan(&v.Email, &v.UUID, &v.Enabled, &v.VolumeBytes, &v.ExpiresUnix, &v.DurationSeconds, &v.MaxIPs, &v.SubToken, &v.RepID, &v.Plan, &v.Note,
			&v.Usage.Up, &v.Usage.Down, &v.Usage.FirstSeen, &v.Usage.LastSeen, &v.Online, &v.ResetPeriod, &v.LastReset, &np, &v.HWIDLimit, &v.StartBy, &v.Trial, &v.Legacy); err != nil {
			return nil, err
		}
		if np != "" {
			var p NextPlan
			if json.Unmarshal([]byte(np), &p) == nil {
				v.NextPlan = &p
			}
		}
		if v.ResetPeriod != "" && v.LastReset > 0 {
			v.NextReset = nextReset(v.ResetPeriod, v.LastReset)
		}
		v.EffectiveUntil = EffectiveExpiry(v.Client, v.Usage)
		v.Remaining = Remaining(v.Client, v.Usage)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Every client's inbounds in one query (not one per client).
	at := make(map[string]int, len(out))
	for i := range out {
		at[out[i].Email] = i
	}
	tq, targs := `SELECT email,tag FROM client_inbounds ORDER BY email,tag`, []any(nil)
	switch where {
	case "c.email=?":
		tq, targs = `SELECT email,tag FROM client_inbounds WHERE email=? ORDER BY tag`, []any{arg}
	case "c.rep_id=?":
		tq, targs = `SELECT i.email,i.tag FROM client_inbounds i JOIN clients c ON c.email=i.email WHERE c.rep_id=? ORDER BY i.email,i.tag`, []any{arg}
	}
	tags, err := s.ro.QueryContext(ctx, tq, targs...)
	if err != nil {
		return nil, err
	}
	defer tags.Close()
	for tags.Next() {
		var e, t string
		if err := tags.Scan(&e, &t); err != nil {
			return nil, err
		}
		if i, ok := at[e]; ok {
			out[i].Inbounds = append(out[i].Inbounds, t)
		}
	}
	return out, tags.Err()
}

// DeleteClient removes a client; its usage history is kept for accounting.
func (s *Store) DeleteClient(ctx context.Context, email string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := keepRepUsage(ctx, tx, email); err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `DELETE FROM clients WHERE email=?`, email)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if err := forgetClient(ctx, tx, email); err != nil {
		return err
	}
	return tx.Commit()
}

// keepRepUsage adds a reseller's client's usage to the reseller's lifetime
// total before the client is deleted, so the total never shrinks.
func keepRepUsage(ctx context.Context, tx *sql.Tx, email string) error {
	_, err := tx.ExecContext(ctx, `UPDATE reps SET used_base=used_base+COALESCE((SELECT up+down FROM client_usage WHERE email=?),0)
WHERE id=(SELECT rep_id FROM clients WHERE email=? AND rep_id<>'')`, email, email)
	return err
}

// forgetClient removes a deleted client's live state, so a new client
// with the same name starts clean (no old usage, first-use time, devices,
// tunnel addresses or Telegram links). Daily history, events and the
// credit ledger stay for reports.
func forgetClient(ctx context.Context, tx *sql.Tx, email string) error {
	for _, table := range []string{"client_usage", "online", "ip_log", "client_devices", "wg_ips", "tg_links", "usage_resets"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE email=?`, email); err != nil {
			return err
		}
	}
	return nil
}

// EmailBySubToken resolves a public subscription token.
func (s *Store) EmailBySubToken(ctx context.Context, tok string) (string, error) {
	if len(tok) < 16 {
		return "", ErrNotFound
	}
	var email string
	err := s.ro.QueryRowContext(ctx, `SELECT email FROM clients WHERE sub_token=?`, tok).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// Usage is a client's metered total.
type Usage struct {
	Up, Down            int64
	FirstSeen, LastSeen int64
}

func (s *Store) ClientUsage(ctx context.Context, email string) (Usage, error) {
	var u Usage
	err := s.ro.QueryRowContext(ctx, `SELECT up,down,first_seen_unix,last_seen_unix FROM client_usage WHERE email=?`, email).
		Scan(&u.Up, &u.Down, &u.FirstSeen, &u.LastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return Usage{}, nil
	}
	return u, err
}

// EffectiveExpiry resolves first-connection terms.
func EffectiveExpiry(c Client, u Usage) int64 {
	if c.ExpiresUnix > 0 {
		return c.ExpiresUnix
	}
	if c.DurationSeconds > 0 && u.FirstSeen > 0 {
		return u.FirstSeen + c.DurationSeconds
	}
	return 0
}

// Remaining returns remaining volume, or -1 for unlimited.
func Remaining(c Client, u Usage) int64 {
	if c.VolumeBytes < 0 {
		return -1
	}
	r := c.VolumeBytes - u.Up - u.Down
	if r < 0 {
		return 0
	}
	return r
}

func firstNonEmpty(v ...string) string {
	for _, x := range v {
		if x != "" {
			return x
		}
	}
	return ""
}
