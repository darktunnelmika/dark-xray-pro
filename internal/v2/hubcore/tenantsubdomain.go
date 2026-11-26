package hubcore

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A connected shop's own subscription domains.
//
// A shop that moves its customers here from another panel keeps their
// subscription links: it points the old subscription domain at the HUB and
// the HUB answers that domain on the old port and path with the same
// client's subscription (the import keeps every client's old subscription
// id as its token). Several domains may share one port: the certificate is
// picked by name (SNI) and the handler by Host, next to resellers' moved
// domains (legacy.go).
//
// Customers not brought over yet must never get an empty answer: some apps
// read an empty list as "no configs" and delete what they have. An unknown
// id is passed to the old panel (fallback) when one is set, and is a 404
// otherwise or when the old panel has nothing for it either.

const tenantSubSchema = `
CREATE TABLE IF NOT EXISTS tenant_sub_domains (
  domain TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  port INTEGER NOT NULL,
  tls INTEGER NOT NULL DEFAULT 1,
  prefix TEXT NOT NULL DEFAULT '',
  fallback TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  dns_unix INTEGER NOT NULL DEFAULT 0,
  created_unix INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS tenant_sub_domains_tenant ON tenant_sub_domains(tenant_id);
`

func (s *Store) ensureTenantSubSchema() error {
	_, err := s.db.Exec(tenantSubSchema)
	return err
}

// TenantSubDomain is one domain a shop's old links use.
type TenantSubDomain struct {
	Domain   string `json:"domain"`
	Port     int    `json:"port"`
	TLS      bool   `json:"tls"`
	Prefix   string `json:"prefix"`   // e.g. "/2fmgkwhmfhyat8zr/"; empty = any path ending in the id
	Fallback string `json:"fallback"` // the old panel's subscription origin for unknown ids, e.g. https://203.0.113.5:2096
	Enabled  bool   `json:"enabled"`
	DNSUnix  int64  `json:"dns_unix"` // when the domain was seen pointing here (0 = not yet)
	// Read only, for the panel.
	CertStatus string `json:"cert_status,omitempty"`
	PortError  string `json:"port_error,omitempty"`
}

var (
	subDomainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	subPrefixRE = regexp.MustCompile(`^/([A-Za-z0-9._~-]+/)*$`)
)

func tenantSubCertID(domain string) string {
	return "tsub-" + strings.NewReplacer(".", "-").Replace(domain)
}

// TenantSubDomains lists a shop's domains.
func (s *Store) TenantSubDomains(ctx context.Context, tenant string) ([]TenantSubDomain, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT d.domain,d.port,d.tls,d.prefix,d.fallback,d.enabled,d.dns_unix,COALESCE(a.status,'')
FROM tenant_sub_domains d LEFT JOIN acme_certs a ON a.id='tsub-'||replace(d.domain,'.','-') WHERE d.tenant_id=? ORDER BY d.domain`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TenantSubDomain{}
	legacyNetMu.Lock()
	defer legacyNetMu.Unlock()
	for rows.Next() {
		var d TenantSubDomain
		if err := rows.Scan(&d.Domain, &d.Port, &d.TLS, &d.Prefix, &d.Fallback, &d.Enabled, &d.DNSUnix, &d.CertStatus); err != nil {
			return nil, err
		}
		d.PortError = legacyPortErr[d.Port]
		out = append(out, d)
	}
	return out, rows.Err()
}

// PutTenantSubDomain adds or changes one of a shop's domains.
func (s *Store) PutTenantSubDomain(ctx context.Context, tenant string, d TenantSubDomain) error {
	d.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d.Domain), "."))
	d.Prefix = strings.TrimSpace(d.Prefix)
	d.Fallback = strings.TrimRight(strings.TrimSpace(d.Fallback), "/")
	if !subDomainRE.MatchString(d.Domain) || len(d.Domain) > 253 {
		return fmt.Errorf("%w: domain", ErrInvalid)
	}
	if d.Port < 1 || d.Port > 65535 || legacyReserved(d.Port) {
		return fmt.Errorf("%w: port %d cannot be used (the HUB uses it or it is below 1024)", ErrInvalid, d.Port)
	}
	if d.Prefix != "" && (!subPrefixRE.MatchString(d.Prefix) || len(d.Prefix) > 128) {
		return fmt.Errorf("%w: the path prefix starts and ends with / (e.g. /2fmgkwhmfhyat8zr/)", ErrInvalid)
	}
	if d.Fallback != "" {
		u, err := url.Parse(d.Fallback)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
			return fmt.Errorf("%w: the fallback is the old panel's address, like https://203.0.113.5:2096", ErrInvalid)
		}
	}
	var t string
	if s.ro.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=?`, tenant).Scan(&t) != nil {
		return ErrNotFound
	}
	var owner string
	if s.ro.QueryRowContext(ctx, `SELECT tenant_id FROM tenant_sub_domains WHERE domain=?`, d.Domain).Scan(&owner) == nil && owner != tenant {
		return fmt.Errorf("%w: %s belongs to another connected shop", ErrInvalid, d.Domain)
	}
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_reps WHERE domain=? AND step>0`, d.Domain).Scan(&n)
	if n > 0 {
		return fmt.Errorf("%w: %s is a reseller's moved domain", ErrInvalid, d.Domain)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tenant_sub_domains(domain,tenant_id,port,tls,prefix,fallback,enabled,created_unix) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(domain) DO UPDATE SET port=excluded.port, tls=excluded.tls, prefix=excluded.prefix, fallback=excluded.fallback, enabled=excluded.enabled`,
		d.Domain, tenant, d.Port, d.TLS, d.Prefix, d.Fallback, d.Enabled, s.now().Unix())
	return err
}

// DeleteTenantSubDomain removes a domain (its certificate stays, unused).
func (s *Store) DeleteTenantSubDomain(ctx context.Context, tenant, domain string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM tenant_sub_domains WHERE tenant_id=? AND domain=?`, tenant, strings.ToLower(domain))
	if err != nil {
		return err
	}
	if k, _ := r.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	return nil
}

// CheckTenantSubDomain looks whether the domain points at the HUB and, when
// it does, orders its certificate.
func (s *Store) CheckTenantSubDomain(ctx context.Context, tenant, domain string) (bool, string, error) {
	var d TenantSubDomain
	if s.ro.QueryRowContext(ctx, `SELECT domain,tls FROM tenant_sub_domains WHERE tenant_id=? AND domain=?`, tenant, strings.ToLower(domain)).Scan(&d.Domain, &d.TLS) != nil {
		return false, "", ErrNotFound
	}
	ips, err := lookupFresh(ctx, d.Domain)
	if err != nil || len(ips) == 0 {
		return false, "the domain does not resolve yet", nil
	}
	hub := HubIPs()
	for _, ip := range ips {
		ok := false
		for _, h := range hub {
			ok = ok || ip == h
		}
		if !ok {
			return false, "the domain still points to " + ip, nil
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tenant_sub_domains SET dns_unix=? WHERE domain=?`, s.now().Unix(), d.Domain); err != nil {
		return false, "", err
	}
	if d.TLS && legacyACME {
		if err := s.PutACMECert(ctx, tenantSubCertID(d.Domain), []string{d.Domain}, ACMEHTTPHub, ""); err != nil {
			return true, "certificate: " + err.Error(), nil
		}
	}
	return true, "", nil
}

// retryTenantSubCerts asks again for a failed certificate every 10 minutes.
func (s *Store) retryTenantSubCerts(ctx context.Context) {
	if !legacyACME {
		return
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT a.id FROM acme_certs a JOIN tenant_sub_domains d ON a.id='tsub-'||replace(d.domain,'.','-')
WHERE d.enabled=1 AND d.dns_unix>0 AND a.status='failed' AND a.last_try < ?`, s.now().Unix()-600)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_ = s.RenewACMECert(ctx, id)
	}
}

// tenantSubWanted adds the shops' domain ports to the legacy listeners.
func (s *Store) tenantSubWanted(ctx context.Context, want map[int]string) {
	rows, err := s.ro.QueryContext(ctx, `SELECT DISTINCT port,tls FROM tenant_sub_domains WHERE enabled=1 ORDER BY port`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var port int
		var tlsOn bool
		if rows.Scan(&port, &tlsOn) != nil || port <= 0 || legacyReserved(port) {
			continue
		}
		k := "sub:http"
		if tlsOn {
			k = "sub:tls"
		}
		switch cur, taken := want[port]; {
		case !taken:
			want[port] = k
		case cur == "sub:http" && k == "sub:tls":
			want[port] = k // TLS wins a shared port; a pass-through port is never taken
		}
	}
}

// tenantSubOf finds the shop domain a request came to.
func (s *Store) tenantSubOf(ctx context.Context, host string) (string, TenantSubDomain, bool) {
	var t string
	var d TenantSubDomain
	if s.ro.QueryRowContext(ctx, `SELECT tenant_id,domain,port,prefix,fallback FROM tenant_sub_domains WHERE domain=? AND enabled=1`, host).
		Scan(&t, &d.Domain, &d.Port, &d.Prefix, &d.Fallback) != nil {
		return "", d, false
	}
	return t, d, true
}

// tenantSubToken finds the old id in the path: after the domain's prefix,
// after one of the imported panel's paths (legacy_sub_paths), or the last
// part of the path when the domain has no prefix.
func (s *Store) tenantSubToken(r *http.Request, d TenantSubDomain) (tok, format string, ok bool) {
	p := r.URL.Path
	if d.Prefix != "" {
		if rest, found := strings.CutPrefix(p, d.Prefix); found && subIDRE.MatchString(strings.TrimSuffix(rest, "/")) {
			return strings.TrimSuffix(rest, "/"), "", true
		}
	}
	if tok, f, found := s.legacyPath(r); found {
		return tok, f, true
	}
	if d.Prefix == "" {
		p = strings.TrimSuffix(p, "/")
		if t := p[strings.LastIndex(p, "/")+1:]; subIDRE.MatchString(t) {
			return t, "", true
		}
	}
	return "", "", false
}

// serveTenantSub answers one request to a shop's domain.
func (s *Store) serveTenantSub(w http.ResponseWriter, r *http.Request, sub http.Handler, tenant string, d TenantSubDomain) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tok, format, ok := s.tenantSubToken(r, d)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var email string
	if s.ro.QueryRowContext(r.Context(), `SELECT email FROM clients WHERE sub_token=? AND rep_id=?`, tok, tenantRep(tenant)).Scan(&email) != nil {
		s.tenantSubFallback(w, r, d)
		return
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path, r2.URL.RawPath = "/sub/"+tok, ""
	if format != "" {
		q := r2.URL.Query()
		q.Set("format", format)
		r2.URL.RawQuery = q.Encode()
	}
	sub.ServeHTTP(w, r2)
}

// fallbackClient reaches the old panel. Its address is the owner's setting,
// so private addresses are allowed only in tests (legacyLocalOK).
var fallbackClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, fmt.Errorf("resolve %s: %v", host, err)
			}
			if !legacyLocalOK && !publicIP(ips[0]) {
				return nil, errors.New("the fallback must be a public address")
			}
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		MaxIdleConnsPerHost:   8,
	},
}

const fallbackMaxBody = 2 << 20

// fallbackHeaders are the old panel's answer headers passed on.
var fallbackHeaders = []string{"Content-Type", "Content-Disposition", "Subscription-Userinfo", "Profile-Update-Interval", "Profile-Title",
	"Profile-Web-Page-Url", "Support-Url", "Announce", "Routing-Enable", "Routing"}

// tenantSubFallback passes an unknown id to the old panel. The old panel is
// reached by its address but asked with the domain's name (Host and SNI),
// so its certificate for the domain is checked as usual. Anything but a
// non-empty 200 answer is a 404 here: never an empty list.
func (s *Store) tenantSubFallback(w http.ResponseWriter, r *http.Request, d TenantSubDomain) {
	if d.Fallback == "" {
		http.NotFound(w, r)
		return
	}
	u, err := url.Parse(d.Fallback)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target := *u
	target.Path, target.RawPath, target.RawQuery = r.URL.Path, r.URL.RawPath, r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	req.Host = d.Domain
	if p := strconv.Itoa(d.Port); d.Port != 443 && d.Port != 80 {
		req.Host = d.Domain + ":" + p
	}
	for _, h := range []string{"User-Agent", "Accept", "Accept-Language", "X-Hwid", "X-Device-Os", "X-Device-Model", "X-Ver-Os"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	client := fallbackClient
	if u.Scheme == "https" {
		tr := fallbackClient.Transport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{ServerName: d.Domain, MinVersion: tls.VersionTLS12, InsecureSkipVerify: fallbackInsecure}
		client = &http.Client{Timeout: fallbackClient.Timeout, CheckRedirect: fallbackClient.CheckRedirect, Transport: tr}
		defer tr.CloseIdleConnections()
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("tenant sub %s: fallback: %v", d.Domain, err)
		http.NotFound(w, r)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, fallbackMaxBody+1))
	if err != nil || resp.StatusCode != http.StatusOK || len(body) > fallbackMaxBody || strings.TrimSpace(string(body)) == "" {
		http.NotFound(w, r)
		return
	}
	for _, h := range fallbackHeaders {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Dark-Fallback", "1")
	_, _ = w.Write(body)
}

// fallbackInsecure is for tests only (their old panel has a test certificate).
var fallbackInsecure = false
