package hubcore

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// A connected shop's API keys: dt_<id>_<secret>. Only the secret's SHA-256
// is kept; the key is shown once. A key may expire and may be limited to a
// list of addresses (IPs or CIDR ranges): the shop's server.

// TenantKey is a key without its secret.
type TenantKey struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	AllowIPs    []string `json:"allow_ips"`
	ExpiresUnix int64    `json:"expires_unix"` // 0 = never
	CreatedUnix int64    `json:"created_unix"`
	LastUsed    int64    `json:"last_used"`
	LastIP      string   `json:"last_ip"`
}

// cleanAllowIPs checks an address list: IPs or CIDR ranges.
func cleanAllowIPs(list []string) ([]string, error) {
	out := []string{}
	for _, v := range list {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if net.ParseIP(v) == nil {
			if _, _, err := net.ParseCIDR(v); err != nil {
				return nil, fmt.Errorf("%w: %q is not an IP address or range", ErrInvalid, v)
			}
		}
		if !contains(out, v) {
			out = append(out, v)
		}
	}
	if len(out) > 50 {
		return nil, fmt.Errorf("%w: at most 50 addresses", ErrInvalid)
	}
	return out, nil
}

// CreateTenantKey makes a key for a tenant and returns it (once).
func (s *Store) CreateTenantKey(ctx context.Context, tenant, name string, allowIPs []string, days int) (string, TenantKey, error) {
	if _, err := s.TenantByID(ctx, tenant); err != nil {
		return "", TenantKey{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return "", TenantKey{}, fmt.Errorf("%w: a key name (up to 64 characters) is required", ErrInvalid)
	}
	if days < 0 || days > 3650 {
		return "", TenantKey{}, fmt.Errorf("%w: expiry is 0 (never) to 3650 days", ErrInvalid)
	}
	ips, err := cleanAllowIPs(allowIPs)
	if err != nil {
		return "", TenantKey{}, err
	}
	id, secret := randomHex(4), randomHex(24)
	now := s.now().Unix()
	var exp int64
	if days > 0 {
		exp = now + int64(days)*86400
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO tenant_keys(id,tenant_id,name,secret_sha256,allow_ips,expires_unix,created_unix) VALUES(?,?,?,?,?,?,?)`,
		id, tenant, name, hashToken(secret), strings.Join(ips, ","), exp, now); err != nil {
		return "", TenantKey{}, err
	}
	return "dt_" + id + "_" + secret, TenantKey{ID: id, Name: name, AllowIPs: ips, ExpiresUnix: exp, CreatedUnix: now}, nil
}

// TenantKeys lists a tenant's keys.
func (s *Store) TenantKeys(ctx context.Context, tenant string) ([]TenantKey, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,name,allow_ips,expires_unix,created_unix,last_used,last_ip FROM tenant_keys WHERE tenant_id=? ORDER BY created_unix`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TenantKey{}
	for rows.Next() {
		var k TenantKey
		var ips string
		if err := rows.Scan(&k.ID, &k.Name, &ips, &k.ExpiresUnix, &k.CreatedUnix, &k.LastUsed, &k.LastIP); err != nil {
			return nil, err
		}
		k.AllowIPs = splitList(ips)
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteTenantKey revokes a key at once.
func (s *Store) DeleteTenantKey(ctx context.Context, tenant, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM tenant_keys WHERE tenant_id=? AND id=?`, tenant, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func splitList(v string) []string {
	out := []string{}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ipAllowed reports whether ip is in the list (an empty list allows all).
func ipAllowed(list []string, ip string) bool {
	if len(list) == 0 {
		return true
	}
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	for _, v := range list {
		if strings.Contains(v, "/") {
			if _, n, err := net.ParseCIDR(v); err == nil && n.Contains(p) {
				return true
			}
		} else if q := net.ParseIP(v); q != nil && q.Equal(p) {
			return true
		}
	}
	return false
}

// tenantAuth is the tenant behind a request's key (Authorization: Bearer
// dt_… or X-API-Key: dt_…). why tells the caller what failed, without
// saying which keys exist.
func (s *Store) tenantAuth(r *http.Request) (tenant, keyID string, why string) {
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if tok == "" || tok == r.Header.Get("Authorization") {
		tok = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	rest, ok := strings.CutPrefix(tok, "dt_")
	if !ok {
		return "", "", "missing or malformed key"
	}
	id, secret, ok := strings.Cut(rest, "_")
	if !ok || id == "" || secret == "" {
		return "", "", "missing or malformed key"
	}
	ctx := r.Context()
	var sum, ips string
	var exp, last int64
	if err := s.ro.QueryRowContext(ctx, `SELECT tenant_id,secret_sha256,allow_ips,expires_unix,last_used FROM tenant_keys WHERE id=?`, id).
		Scan(&tenant, &sum, &ips, &exp, &last); err != nil || !constantEqual(sum, hashToken(secret)) {
		_ = constantEqual(hashToken(secret), hashToken(secret)) // similar work either way
		return "", "", "invalid key"
	}
	now := s.now().Unix()
	if exp > 0 && now >= exp {
		return "", "", "this key has expired"
	}
	ip := realIP(r)
	if !ipAllowed(splitList(ips), ip) {
		return "", "", "this key may not be used from " + ip
	}
	if now-last >= 60 { // "last used" without a write per request
		_, _ = s.db.ExecContext(ctx, `UPDATE tenant_keys SET last_used=?, last_ip=? WHERE id=?`, now, ip, id)
	}
	return tenant, id, ""
}
