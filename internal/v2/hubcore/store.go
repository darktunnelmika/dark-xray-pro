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
	"time"

	_ "modernc.org/sqlite"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid")
	ErrAuth     = errors.New("unauthorized")
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
	db  *sql.DB
	ro  *sql.DB
	now func() time.Time
}

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
	st := &Store{db: db, ro: ro, now: time.Now}
	if err := st.ensureAuthSchema(); err != nil {
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
	if !emailRE.MatchString(c.Email) || c.MaxIPs < 0 || c.DurationSeconds < 0 {
		return ErrInvalid
	}
	// An update that omits credentials keeps the existing ones, so editing a
	// client never breaks its connections or its subscription link.
	var oldUUID, oldSecret, oldTok string
	_ = s.ro.QueryRowContext(ctx, `SELECT uuid,secret,sub_token FROM clients WHERE email=?`, c.Email).Scan(&oldUUID, &oldSecret, &oldTok)
	c.UUID = firstNonEmpty(c.UUID, oldUUID, RandomUUID())
	c.Secret = firstNonEmpty(c.Secret, oldSecret, randomHex(16))
	c.SubToken = firstNonEmpty(c.SubToken, oldTok, randomHex(16))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO clients(email,uuid,secret,enabled,volume_bytes,expires_unix,duration_seconds,max_ips,sub_token,created_unix)
VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(email) DO UPDATE SET uuid=excluded.uuid, secret=excluded.secret, enabled=excluded.enabled,
volume_bytes=excluded.volume_bytes, expires_unix=excluded.expires_unix, duration_seconds=excluded.duration_seconds, max_ips=excluded.max_ips,
sub_token=excluded.sub_token`,
		c.Email, c.UUID, c.Secret, c.Enabled, c.VolumeBytes, c.ExpiresUnix, c.DurationSeconds, c.MaxIPs, c.SubToken, s.now().Unix())
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM client_inbounds WHERE email=?`, c.Email); err != nil {
		return err
	}
	for _, tag := range c.Inbounds {
		if _, err = tx.ExecContext(ctx, `INSERT INTO client_inbounds(email,tag) VALUES(?,?)`, c.Email, tag); err != nil {
			return fmt.Errorf("inbound %q: %w", tag, err)
		}
	}
	return tx.Commit()
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO inbounds(tag,node_id,template,meta,enabled) VALUES(?,?,?,?,?)
ON CONFLICT(tag) DO UPDATE SET node_id=excluded.node_id, template=excluded.template, meta=excluded.meta, enabled=excluded.enabled`,
		tag, nodeID, string(template), string(meta), enabled)
	return err
}

// Subscription returns the client's share links and userinfo header.
func (s *Store) Subscription(ctx context.Context, email string) (links []string, userinfo string, err error) {
	var c Client
	err = s.ro.QueryRowContext(ctx, `SELECT email,uuid,secret,enabled,volume_bytes,expires_unix,duration_seconds,max_ips FROM clients WHERE email=?`, email).
		Scan(&c.Email, &c.UUID, &c.Secret, &c.Enabled, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds, &c.MaxIPs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT i.tag,i.template,i.meta FROM inbounds i JOIN client_inbounds ci ON ci.tag=i.tag
WHERE ci.email=? AND i.enabled=1 ORDER BY i.tag`, email)
	if err != nil {
		return nil, "", err
	}
	var ins []sub.Inbound
	for rows.Next() {
		var in sub.Inbound
		var tpl, meta string
		if err := rows.Scan(&in.Tag, &tpl, &meta); err != nil {
			rows.Close()
			return nil, "", err
		}
		in.Template = []byte(tpl)
		if err := json.Unmarshal([]byte(meta), &in.Meta); err != nil {
			rows.Close()
			return nil, "", err
		}
		ins = append(ins, in)
	}
	rows.Close()
	if !c.Enabled {
		ins = nil
	}
	links, err = sub.Links(sub.Account{Email: c.Email, UUID: c.UUID, Secret: c.Secret}, ins)
	if err != nil {
		return nil, "", err
	}
	u, err := s.ClientUsage(ctx, email)
	if err != nil {
		return nil, "", err
	}
	total := c.VolumeBytes
	if total < 0 {
		total = 0
	}
	return links, sub.Userinfo(u.Up, u.Down, total, EffectiveExpiry(c, u)), nil
}

// ClientView is a client with its live usage, for the panel.
type ClientView struct {
	Client
	Usage          Usage
	EffectiveUntil int64
	Remaining      int64
	Online         bool
}

// Clients lists every client with usage, newest first.
func (s *Store) Clients(ctx context.Context) ([]ClientView, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT c.email,c.uuid,c.enabled,c.volume_bytes,c.expires_unix,c.duration_seconds,c.max_ips,c.sub_token,
COALESCE(u.up,0),COALESCE(u.down,0),COALESCE(u.first_seen_unix,0),COALESCE(u.last_seen_unix,0),
EXISTS(SELECT 1 FROM online o WHERE o.email=c.email AND o.seen_unix>=?)
FROM clients c LEFT JOIN client_usage u ON u.email=c.email ORDER BY c.created_unix DESC, c.email`, s.now().Unix()-120)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientView
	for rows.Next() {
		var v ClientView
		if err := rows.Scan(&v.Email, &v.UUID, &v.Enabled, &v.VolumeBytes, &v.ExpiresUnix, &v.DurationSeconds, &v.MaxIPs, &v.SubToken,
			&v.Usage.Up, &v.Usage.Down, &v.Usage.FirstSeen, &v.Usage.LastSeen, &v.Online); err != nil {
			return nil, err
		}
		v.EffectiveUntil = EffectiveExpiry(v.Client, v.Usage)
		v.Remaining = Remaining(v.Client, v.Usage)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		tags, err := s.ro.QueryContext(ctx, `SELECT tag FROM client_inbounds WHERE email=? ORDER BY tag`, out[i].Email)
		if err != nil {
			return nil, err
		}
		for tags.Next() {
			var t string
			if err := tags.Scan(&t); err != nil {
				tags.Close()
				return nil, err
			}
			out[i].Inbounds = append(out[i].Inbounds, t)
		}
		tags.Close()
	}
	return out, nil
}

// DeleteClient removes a client; its usage history is kept for accounting.
func (s *Store) DeleteClient(ctx context.Context, email string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM clients WHERE email=?`, email)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
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
