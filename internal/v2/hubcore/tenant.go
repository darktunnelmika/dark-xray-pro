package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Connected shops ("tenants") are outside sales systems — a website with its
// own resellers, wallets and bots — that keep their customers on this panel.
// The panel is the source of truth for the clients (credentials, volume,
// expiry, subscription); the shop only sells. A tenant is not a reseller:
// it has no credit, no refunds, no Telegram bot and no shop here, and its
// clients never mix with the owner's or the resellers'.
//
// How a tenant's clients are kept apart:
//
//   - clients.rep_id is "~<tenant id>". "~" can never start a reseller id
//     (idRE), so every existing "is this my client?" check (rep_id=seller,
//     with "" for the owner) already fails for them, and the owner's own
//     scope (rep_id='') leaves them out.
//   - Their names are "<tenant id>+<name>", so two shops (or a shop and the
//     owner) can use the same customer names. The tenant API only ever sees
//     <name>.
//   - Everything the owner's side lists or counts (the client list, reports,
//     reminders, the bots, the online list, the panel webhook) leaves them
//     out; the owner sees a summary per tenant and, only where the tenant's
//     owner_access allows it, a client's details — each look is logged.
//
// Changes to a tenant's clients advance a revision (rev) through triggers,
// so the tenant can read "everything that changed since my last cursor";
// usage batches stamp one revision per batch on client_usage.

const tenantMark = "~"

// tenantRep is the rep_id a tenant's clients carry.
func tenantRep(id string) string { return tenantMark + id }

// tenantOfRep is the tenant behind a rep_id, if it is one.
func tenantOfRep(rep string) (string, bool) {
	if strings.HasPrefix(rep, tenantMark) {
		return rep[len(tenantMark):], true
	}
	return "", false
}

// TenantOfClient reports the tenant a client belongs to ("" = none).
func (s *Store) TenantOfClient(ctx context.Context, email string) string {
	var rep string
	_ = s.ro.QueryRowContext(ctx, `SELECT rep_id FROM clients WHERE email=?`, email).Scan(&rep)
	t, _ := tenantOfRep(rep)
	return t
}

// ownerWorld is the SQL condition (clients alias c) for the panel's own
// clients: the owner's and the resellers', never a connected shop's.
const ownerWorld = "substr(c.rep_id,1,1)<>'~'"

// tenantSep joins a tenant id and a customer name into a client name.
const tenantSep = "+"

func tenantEmail(tenant, name string) string { return tenant + tenantSep + name }

// tenantName is the customer name inside a tenant's client name.
func tenantName(tenant, email string) string {
	return strings.TrimPrefix(email, tenant+tenantSep)
}

const tenantSchema = `
CREATE TABLE IF NOT EXISTS tenants (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  inbounds TEXT NOT NULL DEFAULT '[]',
  max_clients INTEGER NOT NULL DEFAULT 0,
  max_bytes INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT '',
  owner_access TEXT NOT NULL DEFAULT 'summary',
  webhook_url TEXT NOT NULL DEFAULT '',
  webhook_secret TEXT NOT NULL DEFAULT '',
  webhook_enabled INTEGER NOT NULL DEFAULT 0,
  webhook_cursor INTEGER NOT NULL DEFAULT 0,
  webhook_last_ok INTEGER NOT NULL DEFAULT 0,
  webhook_last_try INTEGER NOT NULL DEFAULT 0,
  webhook_last_error TEXT NOT NULL DEFAULT '',
  created_unix INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tenant_keys (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  secret_sha256 TEXT NOT NULL,
  allow_ips TEXT NOT NULL DEFAULT '',
  expires_unix INTEGER NOT NULL DEFAULT 0,
  created_unix INTEGER NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0,
  last_ip TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS tenant_tombstones (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tenant_id TEXT NOT NULL,
  name TEXT NOT NULL,
  rev INTEGER NOT NULL,
  unix INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS tenant_tombstones_rev ON tenant_tombstones(tenant_id, rev);
CREATE TABLE IF NOT EXISTS rev_seq (id INTEGER PRIMARY KEY CHECK(id=1), v INTEGER NOT NULL);
INSERT OR IGNORE INTO rev_seq(id,v) VALUES(1,0);
`

// tenantTriggers run after the columns exist. The tombstone column is
// "name", not "email": a rename moves every email column to the new name,
// and a tombstone must keep the name that went away.
const tenantTriggers = `
CREATE INDEX IF NOT EXISTS clients_rep ON clients(rep_id);
CREATE UNIQUE INDEX IF NOT EXISTS clients_ext_ref ON clients(rep_id, external_ref) WHERE external_ref<>'';
CREATE INDEX IF NOT EXISTS events_tenant ON events(tenant_id, id) WHERE tenant_id<>'';
CREATE TRIGGER IF NOT EXISTS clients_tenant_ins AFTER INSERT ON clients
WHEN substr(NEW.rep_id,1,1)='~' BEGIN
  UPDATE rev_seq SET v=v+1 WHERE id=1;
  UPDATE clients SET rev=(SELECT v FROM rev_seq WHERE id=1) WHERE rowid=NEW.rowid;
END;
CREATE TRIGGER IF NOT EXISTS clients_tenant_upd AFTER UPDATE ON clients
WHEN (substr(NEW.rep_id,1,1)='~' OR substr(OLD.rep_id,1,1)='~') AND NEW.rev=OLD.rev BEGIN
  UPDATE rev_seq SET v=v+1 WHERE id=1;
  INSERT INTO tenant_tombstones(tenant_id,name,rev,unix)
    SELECT substr(OLD.rep_id,2), OLD.email, (SELECT v FROM rev_seq WHERE id=1), CAST(strftime('%s','now') AS INTEGER)
    WHERE substr(OLD.rep_id,1,1)='~' AND (NEW.rep_id<>OLD.rep_id OR NEW.email<>OLD.email);
  UPDATE clients SET rev=(SELECT v FROM rev_seq WHERE id=1) WHERE rowid=NEW.rowid AND substr(NEW.rep_id,1,1)='~';
END;
CREATE TRIGGER IF NOT EXISTS clients_tenant_del AFTER DELETE ON clients
WHEN substr(OLD.rep_id,1,1)='~' BEGIN
  UPDATE rev_seq SET v=v+1 WHERE id=1;
  INSERT INTO tenant_tombstones(tenant_id,name,rev,unix)
    VALUES(substr(OLD.rep_id,2), OLD.email, (SELECT v FROM rev_seq WHERE id=1), CAST(strftime('%s','now') AS INTEGER));
END;
CREATE TRIGGER IF NOT EXISTS events_tenant_ins AFTER INSERT ON events
WHEN EXISTS(SELECT 1 FROM clients WHERE email=NEW.email AND substr(rep_id,1,1)='~') BEGIN
  UPDATE events SET tenant_id=(SELECT substr(rep_id,2) FROM clients WHERE email=NEW.email) WHERE id=NEW.id;
END;
`

func (s *Store) ensureTenantSchema() error {
	if _, err := s.db.Exec(tenantSchema); err != nil {
		return err
	}
	for _, c := range []struct{ table, col, def string }{
		{"clients", "external_ref", "TEXT NOT NULL DEFAULT ''"},
		{"clients", "rev", "INTEGER NOT NULL DEFAULT 0"},
		{"client_usage", "rev", "INTEGER NOT NULL DEFAULT 0"},
		{"events", "tenant_id", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumn(c.table, c.col, c.def); err != nil {
			return err
		}
	}
	_, err := s.db.Exec(tenantTriggers)
	return err
}

// Tenant is a connected shop.
type Tenant struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Inbounds    []string `json:"inbounds"`    // inbounds its clients may use; empty = every sellable inbound
	MaxClients  int      `json:"max_clients"` // 0 = no cap
	MaxBytes    int64    `json:"max_bytes"`   // cap on the volume of all its limited clients together; 0 = no cap
	Note        string   `json:"note"`
	OwnerAccess string   `json:"owner_access"` // summary | read | write
	CreatedUnix int64    `json:"created_unix"`
}

// TenantSummary is what the owner sees of a tenant by default.
type TenantSummary struct {
	Tenant
	Clients     int   `json:"clients"`
	Active      int   `json:"active"`
	Online      int   `json:"online"`
	Volume      int64 `json:"volume"` // sum of the limited clients' volume
	Used        int64 `json:"used"`
	Keys        int   `json:"keys"`
	WebhookOn   bool  `json:"webhook_on"`
	WebhookLast int64 `json:"webhook_last_ok"`
}

var (
	ErrTenantOff = errors.New("this connected shop is switched off")
)

func validOwnerAccess(a string) bool { return a == "summary" || a == "read" || a == "write" }

// PutTenant creates or changes a tenant (owner only).
func (s *Store) PutTenant(ctx context.Context, t Tenant) error {
	t.Name, t.Note = strings.TrimSpace(t.Name), strings.TrimSpace(t.Note)
	if t.OwnerAccess == "" {
		t.OwnerAccess = "summary"
	}
	switch {
	case !idRE.MatchString(t.ID) || len(t.ID) > 24:
		return fmt.Errorf("%w: the id is 1–24 lower-case letters, digits, . _ -", ErrInvalid)
	case t.Name == "" || len(t.Name) > 80:
		return fmt.Errorf("%w: a name (up to 80 characters) is required", ErrInvalid)
	case len(t.Note) > 2000:
		return fmt.Errorf("%w: the note is too long", ErrInvalid)
	case t.MaxClients < 0 || t.MaxBytes < 0:
		return fmt.Errorf("%w: caps cannot be negative", ErrInvalid)
	case !validOwnerAccess(t.OwnerAccess):
		return fmt.Errorf("%w: owner access is summary, read or write", ErrInvalid)
	}
	ins := []string{}
	for _, tag := range t.Inbounds {
		var n int
		if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbounds WHERE tag=?`, tag).Scan(&n); n == 0 {
			return fmt.Errorf("%w: inbound %q does not exist", ErrInvalid, tag)
		}
		if !contains(ins, tag) {
			ins = append(ins, tag)
		}
	}
	raw, _ := json.Marshal(ins)
	var exists int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE id=?`, t.ID).Scan(&exists)
	if exists == 0 {
		// A new tenant may not take over names that already exist.
		var n int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE substr(email,1,?)=? COLLATE NOCASE`, len(t.ID)+1, t.ID+tenantSep).Scan(&n)
		if n > 0 {
			return fmt.Errorf("%w: %d clients already have names starting with %q; pick another id", ErrInvalid, n, t.ID+tenantSep)
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO tenants(id,name,enabled,inbounds,max_clients,max_bytes,note,owner_access,created_unix) VALUES(?,?,?,?,?,?,?,?,?)`,
			t.ID, t.Name, t.Enabled, string(raw), t.MaxClients, t.MaxBytes, t.Note, t.OwnerAccess, s.now().Unix())
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE tenants SET name=?, enabled=?, inbounds=?, max_clients=?, max_bytes=?, note=?, owner_access=? WHERE id=?`,
		t.Name, t.Enabled, string(raw), t.MaxClients, t.MaxBytes, t.Note, t.OwnerAccess, t.ID)
	return err
}

const tenantCols = `id,name,enabled,inbounds,max_clients,max_bytes,note,owner_access,created_unix`

func scanTenant(sc interface{ Scan(...any) error }) (Tenant, error) {
	var t Tenant
	var ins string
	if err := sc.Scan(&t.ID, &t.Name, &t.Enabled, &ins, &t.MaxClients, &t.MaxBytes, &t.Note, &t.OwnerAccess, &t.CreatedUnix); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return t, ErrNotFound
		}
		return t, err
	}
	_ = json.Unmarshal([]byte(ins), &t.Inbounds)
	if t.Inbounds == nil {
		t.Inbounds = []string{}
	}
	return t, nil
}

// TenantByID reads one tenant.
func (s *Store) TenantByID(ctx context.Context, id string) (Tenant, error) {
	return scanTenant(s.ro.QueryRowContext(ctx, `SELECT `+tenantCols+` FROM tenants WHERE id=?`, id))
}

// Tenants lists the tenants with their summaries, for the owner.
func (s *Store) Tenants(ctx context.Context) ([]TenantSummary, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+tenantCols+`,webhook_enabled,webhook_last_ok FROM tenants ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []TenantSummary
	for rows.Next() {
		var ts TenantSummary
		var ins string
		if err := rows.Scan(&ts.ID, &ts.Name, &ts.Enabled, &ins, &ts.MaxClients, &ts.MaxBytes, &ts.Note, &ts.OwnerAccess, &ts.CreatedUnix, &ts.WebhookOn, &ts.WebhookLast); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal([]byte(ins), &ts.Inbounds)
		if ts.Inbounds == nil {
			ts.Inbounds = []string{}
		}
		out = append(out, ts)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := s.now().Unix()
	for i := range out {
		rep := tenantRep(out[i].ID)
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*),
COALESCE(SUM(CASE WHEN c.enabled=1 AND (c.expires_unix=0 OR c.expires_unix>?) AND (c.volume_bytes<0 OR COALESCE(u.up+u.down,0)<c.volume_bytes) THEN 1 ELSE 0 END),0),
COALESCE(SUM(CASE WHEN c.volume_bytes>0 THEN c.volume_bytes ELSE 0 END),0), COALESCE(SUM(u.up+u.down),0)
FROM clients c LEFT JOIN client_usage u ON u.email=c.email WHERE c.rep_id=?`, now, rep).
			Scan(&out[i].Clients, &out[i].Active, &out[i].Volume, &out[i].Used)
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(DISTINCT o.email) FROM online o JOIN clients c ON c.email=o.email WHERE c.rep_id=? AND o.seen_unix>=?`, rep, now-120).Scan(&out[i].Online)
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenant_keys WHERE tenant_id=?`, out[i].ID).Scan(&out[i].Keys)
	}
	if out == nil {
		out = []TenantSummary{}
	}
	return out, nil
}

// DeleteTenant removes a tenant. A tenant that still has clients cannot be
// removed: move or delete them first, so nothing is lost by a slip.
func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE rep_id=?`, tenantRep(id)).Scan(&n)
	if n > 0 {
		return fmt.Errorf("%w: this shop still has %d clients; move or delete them first", ErrInvalid, n)
	}
	r, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id=?`, id)
	if err != nil {
		return err
	}
	if k, _ := r.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM tenant_tombstones WHERE tenant_id=?`, id)
	_ = s.SetSetting(ctx, "rep_brand:"+tenantRep(id), "")
	return nil
}

// TenantInbounds are the inbounds a tenant's clients may get.
func (s *Store) TenantInbounds(ctx context.Context, t Tenant) []string {
	if len(t.Inbounds) > 0 {
		return t.Inbounds
	}
	return s.SellerInbounds(ctx, "")
}

// TenantBrand is the tenant's look for its customers (the reseller brand
// model, kept under the tenant's own key).
func (s *Store) TenantBrand(ctx context.Context, id string) RepBrand {
	return s.RepBrand(ctx, tenantRep(id))
}

// PutTenantBrand saves the tenant's look, with the same checks as a reseller's.
func (s *Store) PutTenantBrand(ctx context.Context, id string, b RepBrand) error {
	if _, err := s.TenantByID(ctx, id); err != nil {
		return err
	}
	return s.putBrand(ctx, tenantRep(id), b)
}

// reservedName reports whether a new client name would sit in a tenant's
// name space ("<tenant>+...").
func reservedName(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, name string) bool {
	i := strings.Index(name, tenantSep)
	if i <= 0 {
		return false
	}
	var n int
	_ = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE id=? COLLATE NOCASE`, name[:i]).Scan(&n)
	return n > 0
}

type tenantAccessKey struct{}

// withTenantAccess marks ctx as allowed to write one tenant's clients: the
// tenant's own API, or the owner where the tenant allows it.
func withTenantAccess(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantAccessKey{}, tenant)
}

func tenantAccess(ctx context.Context) string {
	t, _ := ctx.Value(tenantAccessKey{}).(string)
	return t
}

// --- the owner's view of a tenant's client ---

// tenantGuard decides whether the caller may use an admin route that names a
// client (the {email} path value). Resellers never see a tenant's clients;
// the owner may look (GET) when the tenant allows read or write, and change
// when it allows write. Every look and every refusal is written to the
// audit log with the tenant's name (a GET is otherwise not logged). The
// request it returns carries the write permission for the tenant.
func (s *Store) tenantGuard(w http.ResponseWriter, r *http.Request, p Principal) (*http.Request, bool) {
	email := r.PathValue("email")
	if email == "" {
		return r, true
	}
	t := s.TenantOfClient(r.Context(), email)
	if t == "" {
		return r, true
	}
	if p.Role != "owner" {
		http.Error(w, "not found", http.StatusNotFound)
		return r, false
	}
	tp := Principal{User: p.User + " [tenant:" + t + "]", Role: p.Role, Level: p.Level}
	ten, err := s.TenantByID(r.Context(), t)
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	if err != nil || ten.OwnerAccess == "summary" || (!read && ten.OwnerAccess != "write") {
		verb := "change"
		if read {
			verb = "see"
		}
		http.Error(w, fmt.Sprintf("this client belongs to the connected shop %q; its settings do not let the panel owner %s it", t, verb), http.StatusForbidden)
		s.Audit(r.Context(), tp, r, http.StatusForbidden)
		return r, false
	}
	if read {
		s.Audit(r.Context(), tp, r, http.StatusOK)
		return r, true
	}
	return r.WithContext(withTenantAccess(r.Context(), t)), true
}

// publicName is the name a customer sees: a tenant's clients without the
// tenant prefix.
func (s *Store) publicName(ctx context.Context, email string) string {
	if t := s.TenantOfClient(ctx, email); t != "" {
		return tenantName(t, email)
	}
	return email
}

type renameAcrossKey struct{}

// withRenameAcross lets a move between the owner, a reseller and a tenant
// rename clients into or out of a tenant's name space.
func withRenameAcross(ctx context.Context) context.Context {
	return context.WithValue(ctx, renameAcrossKey{}, true)
}

func renameAcross(ctx context.Context) bool {
	v, _ := ctx.Value(renameAcrossKey{}).(bool)
	return v
}

// tenantSafeName turns a client name from another panel into a name a shop
// may use ("+" and anything else outside tenantNameRE become "-").
func tenantSafeName(n string) string {
	b := []byte(n)
	for i, ch := range b {
		ok := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == '@' || ch == '-'
		if !ok {
			b[i] = '-'
		}
	}
	out := strings.TrimLeft(string(b), "._@-")
	if len(out) > 100 {
		out = out[:100]
	}
	if out == "" || !tenantNameRE.MatchString(out) {
		h := hashToken(n)
		out = "old-" + h[:8]
	}
	return out
}
