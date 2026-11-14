package hubcore

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Free certificates (Let's Encrypt by default) are issued on the HUB and
// stored in the certificate store under the requested id, so inbounds that
// reference {"dark_cert":"id"} pick up every renewal on their next sync.
//
// Two ways to prove the domain:
//   - dns_cf:  a TXT record through the Cloudflare API (any domain,
//     wildcards, no open ports needed);
//   - http_node: the node the domain points at answers the HTTP-01
//     challenge on port 80 for a few minutes (task "acme_http").

const acmeSchema = `
CREATE TABLE IF NOT EXISTS acme_certs (
  id TEXT PRIMARY KEY,
  domains TEXT NOT NULL,
  method TEXT NOT NULL,
  node_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  last_error TEXT NOT NULL DEFAULT '',
  last_try INTEGER NOT NULL DEFAULT 0
);
`

const (
	ACMEDNSCloudflare = "dns_cf"
	ACMEHTTPNode      = "http_node"

	acmeDefaultDir = "https://acme-v02.api.letsencrypt.org/directory"
	// Renew when fewer than this many days remain.
	acmeRenewDays = 30
)

// ACMESettings are the owner's ACME options. The Cloudflare token is
// write-only: reads only say whether one is set.
type ACMESettings struct {
	Email     string `json:"email"`
	Directory string `json:"directory"`
	CFToken   string `json:"cf_token,omitempty"`
	CFSet     bool   `json:"cf_set"`
}

// ACMECert is one managed certificate.
type ACMECert struct {
	ID        string   `json:"id"`
	Domains   []string `json:"domains"`
	Method    string   `json:"method"`
	NodeID    string   `json:"node_id,omitempty"`
	Status    string   `json:"status"` // pending, issuing, ok, failed
	LastError string   `json:"last_error,omitempty"`
	LastTry   int64    `json:"last_try,omitempty"`
	NotAfter  int64    `json:"not_after,omitempty"`
	DaysLeft  int64    `json:"days_left"`
}

var acmeRun sync.Map // id -> struct{}: one issuance per certificate at a time

func (s *Store) ensureACMESchema() error {
	_, err := s.db.Exec(acmeSchema)
	return err
}

func (s *Store) ACMESettings(ctx context.Context) ACMESettings {
	a := ACMESettings{Email: s.Setting(ctx, "acme:email"), Directory: s.Setting(ctx, "acme:directory"), CFSet: s.Setting(ctx, "acme:cf_token") != ""}
	if a.Directory == "" {
		a.Directory = acmeDefaultDir
	}
	return a
}

// PutACMESettings saves options; an empty token keeps the stored one and
// "-" clears it.
func (s *Store) PutACMESettings(ctx context.Context, a ACMESettings) error {
	a.Email = strings.TrimSpace(a.Email)
	if a.Email != "" && !strings.Contains(a.Email, "@") {
		return fmt.Errorf("%w: email", ErrInvalid)
	}
	dir := strings.TrimSpace(a.Directory)
	if dir != "" && !strings.HasPrefix(dir, "https://") {
		return fmt.Errorf("%w: directory must be https", ErrInvalid)
	}
	if a.Email != s.Setting(ctx, "acme:email") {
		// The account carries the contact; register again with the new one.
		_ = s.SetSetting(ctx, "acme:account_key", "")
	}
	if err := s.SetSetting(ctx, "acme:email", a.Email); err != nil {
		return err
	}
	if dir != s.Setting(ctx, "acme:directory") {
		// A different CA needs its own account.
		_ = s.SetSetting(ctx, "acme:account_key", "")
	}
	if err := s.SetSetting(ctx, "acme:directory", dir); err != nil {
		return err
	}
	switch tok := strings.TrimSpace(a.CFToken); tok {
	case "":
	case "-":
		return s.SetSetting(ctx, "acme:cf_token", "")
	default:
		return s.SetSetting(ctx, "acme:cf_token", tok)
	}
	return nil
}

// ACMECerts lists managed certificates with their expiry.
func (s *Store) ACMECerts(ctx context.Context) ([]ACMECert, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT a.id,a.domains,a.method,a.node_id,a.status,a.last_error,a.last_try,COALESCE(c.not_after,0)
FROM acme_certs a LEFT JOIN certs c ON c.id=a.id ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ACMECert{}
	now := s.now().Unix()
	for rows.Next() {
		var c ACMECert
		var d string
		if err := rows.Scan(&c.ID, &d, &c.Method, &c.NodeID, &c.Status, &c.LastError, &c.LastTry, &c.NotAfter); err != nil {
			return nil, err
		}
		c.Domains = splitTags(d)
		if c.NotAfter > 0 {
			c.DaysLeft = (c.NotAfter - now) / 86400
		}
		if _, busy := acmeRun.Load(c.ID); busy {
			c.Status = "issuing"
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PutACMECert registers (or changes) a managed certificate and starts its
// first issuance in the background.
func (s *Store) PutACMECert(ctx context.Context, id string, domains []string, method, nodeID string) error {
	if !idRE.MatchString(id) {
		return fmt.Errorf("%w: id", ErrInvalid)
	}
	var clean []string
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if !acmeDomainOK(d) {
			return fmt.Errorf("%w: domain %q", ErrInvalid, d)
		}
		clean = append(clean, d)
	}
	if len(clean) == 0 || len(clean) > 20 {
		return fmt.Errorf("%w: 1 to 20 domains", ErrInvalid)
	}
	switch method {
	case ACMEDNSCloudflare:
		if s.Setting(ctx, "acme:cf_token") == "" {
			return fmt.Errorf("%w: set a Cloudflare API token first", ErrInvalid)
		}
		nodeID = ""
	case ACMEHTTPNode:
		for _, d := range clean {
			if strings.HasPrefix(d, "*.") {
				return fmt.Errorf("%w: wildcards need the Cloudflare DNS method", ErrInvalid)
			}
		}
		if nodeID == "" {
			nodeID = s.nodeForDomain(ctx, clean[0])
		}
		var n int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE id=?`, nodeID).Scan(&n)
		if n == 0 {
			return fmt.Errorf("%w: pick the node %s points to", ErrInvalid, clean[0])
		}
	default:
		return fmt.Errorf("%w: method", ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO acme_certs(id,domains,method,node_id,status) VALUES(?,?,?,?,'pending')
ON CONFLICT(id) DO UPDATE SET domains=excluded.domains, method=excluded.method, node_id=excluded.node_id, status='pending', last_error=''`,
		id, strings.Join(clean, ","), method, nodeID)
	if err != nil {
		return err
	}
	go s.issueACME(context.WithoutCancel(ctx), id)
	return nil
}

// RenewACMECert starts an issuance now.
func (s *Store) RenewACMECert(ctx context.Context, id string) error {
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM acme_certs WHERE id=?`, id).Scan(&n)
	if n == 0 {
		return ErrNotFound
	}
	go s.issueACME(context.WithoutCancel(ctx), id)
	return nil
}

// DeleteACMECert stops managing a certificate; the certificate itself stays
// in the store until deleted there.
func (s *Store) DeleteACMECert(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM acme_certs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RunACME renews due certificates twice a day until ctx ends.
func (s *Store) RunACME(ctx context.Context) {
	t := time.NewTimer(2 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.renewDue(ctx)
		t.Reset(12 * time.Hour)
	}
}

func (s *Store) renewDue(ctx context.Context) {
	now := s.now().Unix()
	rows, err := s.ro.QueryContext(ctx, `SELECT a.id FROM acme_certs a LEFT JOIN certs c ON c.id=a.id
WHERE COALESCE(c.not_after,0) < ? AND a.last_try < ?`, now+acmeRenewDays*86400, now-6*3600)
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
		s.issueACME(ctx, id) // one at a time
	}
}

func (s *Store) issueACME(ctx context.Context, id string) {
	if _, busy := acmeRun.LoadOrStore(id, struct{}{}); busy {
		return
	}
	defer acmeRun.Delete(id)
	var domains, method, nodeID string
	if err := s.ro.QueryRowContext(ctx, `SELECT domains,method,node_id FROM acme_certs WHERE id=?`, id).Scan(&domains, &method, &nodeID); err != nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE acme_certs SET status='issuing', last_try=? WHERE id=?`, s.now().Unix(), id)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	err := s.obtain(ctx, id, splitTags(domains), method, nodeID)
	status, msg := "ok", ""
	if err != nil {
		status, msg = "failed", err.Error()
		if len(msg) > 400 {
			msg = msg[:400]
		}
		log.Printf("acme %s: %v", id, err)
	} else {
		log.Printf("acme %s: issued for %s", id, domains)
	}
	_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE acme_certs SET status=?, last_error=? WHERE id=?`, status, msg, id)
}

// acmeClient returns a client with a persistent account key.
func (s *Store) acmeClient(ctx context.Context) (*acme.Client, error) {
	set := s.ACMESettings(ctx)
	var key *ecdsa.PrivateKey
	if p := s.Setting(ctx, "acme:account_key"); p != "" {
		if b, _ := pem.Decode([]byte(p)); b != nil {
			key, _ = x509.ParseECPrivateKey(b.Bytes)
		}
	}
	fresh := key == nil
	if fresh {
		var err error
		if key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
			return nil, err
		}
	}
	c := &acme.Client{Key: key, DirectoryURL: set.Directory, UserAgent: "dark-hub2"}
	if fresh {
		// The email is optional: it only gets expiry notices from the CA.
		acct := &acme.Account{}
		if set.Email != "" {
			acct.Contact = []string{"mailto:" + set.Email}
		}
		_, err := c.Register(ctx, acct, acme.AcceptTOS)
		if err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
			return nil, fmt.Errorf("account: %w", err)
		}
		der, _ := x509.MarshalECPrivateKey(key)
		_ = s.SetSetting(ctx, "acme:account_key", string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})))
	}
	return c, nil
}

func (s *Store) obtain(ctx context.Context, id string, domains []string, method, nodeID string) error {
	c, err := s.acmeClient(ctx)
	if err != nil {
		return err
	}
	order, err := c.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return fmt.Errorf("order: %w", err)
	}
	var cleanup []func()
	defer func() {
		for _, f := range cleanup {
			f()
		}
	}()
	for _, u := range order.AuthzURLs {
		z, err := c.GetAuthorization(ctx, u)
		if err != nil {
			return err
		}
		if z.Status == acme.StatusValid {
			continue
		}
		want := "dns-01"
		if method == ACMEHTTPNode {
			want = "http-01"
		}
		var chal *acme.Challenge
		for _, ch := range z.Challenges {
			if ch.Type == want {
				chal = ch
			}
		}
		if chal == nil {
			return fmt.Errorf("%s: the CA offers no %s challenge", z.Identifier.Value, want)
		}
		name := strings.TrimPrefix(z.Identifier.Value, "*.")
		switch method {
		case ACMEDNSCloudflare:
			val, err := c.DNS01ChallengeRecord(chal.Token)
			if err != nil {
				return err
			}
			cf := &cloudflare{Token: s.Setting(ctx, "acme:cf_token")}
			rec, err := cf.addTXT(ctx, "_acme-challenge."+name, val)
			if err != nil {
				return fmt.Errorf("cloudflare: %w", err)
			}
			cleanup = append(cleanup, func() { _ = cf.delete(context.Background(), rec) })
			waitTXT(ctx, "_acme-challenge."+name, val)
		case ACMEHTTPNode:
			body, err := c.HTTP01ChallengeResponse(chal.Token)
			if err != nil {
				return err
			}
			if err := s.serveHTTP01(ctx, nodeID, name, c.HTTP01ChallengePath(chal.Token), body); err != nil {
				return err
			}
		}
		if _, err := c.Accept(ctx, chal); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := c.WaitAuthorization(ctx, z.URI); err != nil {
			return fmt.Errorf("%s: validation failed: %w", name, err)
		}
	}
	if order, err = c.WaitOrder(ctx, order.URI); err != nil {
		return fmt.Errorf("order: %w", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: domains}, key)
	if err != nil {
		return err
	}
	chain, _, err := c.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	var certPEM bytes.Buffer
	for _, der := range chain {
		_ = pem.Encode(&certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})
	return s.PutCert(ctx, id, certPEM.String(), string(keyPEM))
}

// serveHTTP01 has the node answer the challenge, then checks from the HUB
// that the domain really reaches it before telling the CA to look.
func (s *Store) serveHTTP01(ctx context.Context, nodeID, domain, path, body string) error {
	tid, err := s.QueueTask(ctx, nodeID, wire.TaskACMEHTTP, map[string]any{"path": path, "body": body, "seconds": 240})
	if err != nil {
		return err
	}
	for {
		t, err := s.TaskByID(ctx, tid)
		if err != nil {
			return err
		}
		if t.Status == "failed" {
			return fmt.Errorf("node %s could not listen on port 80: %s", nodeID, t.Error)
		}
		if t.Status == "expired" {
			return fmt.Errorf("node %s did not answer", nodeID)
		}
		if t.Status == "done" {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	hc := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var last error
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+domain+path, nil)
		resp, err := hc.Do(req)
		if err == nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if strings.TrimSpace(string(b)) == body {
				return nil
			}
			err = fmt.Errorf("got HTTP %d with other content (is port 80 of %s forwarded elsewhere?)", resp.StatusCode, domain)
		}
		last = err
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("http://%s does not reach node %s: %v", domain, nodeID, last)
}

// nodeForDomain picks the node whose public address the domain resolves to.
func (s *Store) nodeForDomain(ctx context.Context, domain string) string {
	ips, err := net.DefaultResolver.LookupHost(ctx, domain)
	if err != nil {
		return ""
	}
	for _, ip := range ips {
		var id string
		if s.ro.QueryRowContext(ctx, `SELECT id FROM nodes WHERE public_ip=? LIMIT 1`, ip).Scan(&id) == nil {
			return id
		}
	}
	return ""
}

func acmeDomainOK(d string) bool {
	return validHost(strings.TrimPrefix(d, "*.")) && strings.Contains(d, ".") && net.ParseIP(d) == nil
}

func validHost(d string) bool {
	if d == "" || len(d) > 253 {
		return false
	}
	for _, r := range d {
		if !(r == '.' || r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// waitTXT polls public resolvers until the record is visible (or ~2 min).
func waitTXT(ctx context.Context, name, val string) {
	resolvers := []string{"1.1.1.1:53", "8.8.8.8:53"}
	for i := 0; i < 24; i++ {
		seen := 0
		for _, addr := range resolvers {
			r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				return d.DialContext(ctx, network, addr)
			}}
			txt, _ := r.LookupTXT(ctx, name)
			for _, t := range txt {
				if t == val {
					seen++
					break
				}
			}
		}
		if seen == len(resolvers) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

// cloudflare is a minimal DNS API client (API token with Zone.DNS edit).
type cloudflare struct {
	Token string
	Base  string // tests
	HTTP  *http.Client
}

type cfRecord struct{ zone, id string }

func (c *cloudflare) do(ctx context.Context, method, path string, in, out any) error {
	base := c.Base
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env struct {
		Success bool                       `json:"success"`
		Errors  []struct{ Message string } `json:"errors"`
		Result  json.RawMessage            `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&env); err != nil {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !env.Success {
		var msgs []string
		for _, e := range env.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("%s", strings.Join(msgs, "; "))
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// zone finds the zone for name by trying its parent domains.
func (c *cloudflare) zone(ctx context.Context, name string) (string, error) {
	labels := strings.Split(name, ".")
	for i := 0; i+1 < len(labels); i++ {
		cand := strings.Join(labels[i:], ".")
		var zones []struct{ ID string }
		if err := c.do(ctx, http.MethodGet, "/zones?name="+cand, nil, &zones); err != nil {
			return "", err
		}
		if len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	return "", fmt.Errorf("no Cloudflare zone for %s in this account", name)
}

func (c *cloudflare) addTXT(ctx context.Context, name, val string) (cfRecord, error) {
	z, err := c.zone(ctx, name)
	if err != nil {
		return cfRecord{}, err
	}
	var rec struct{ ID string }
	err = c.do(ctx, http.MethodPost, "/zones/"+z+"/dns_records", map[string]any{"type": "TXT", "name": name, "content": val, "ttl": 60}, &rec)
	return cfRecord{zone: z, id: rec.ID}, err
}

func (c *cloudflare) delete(ctx context.Context, r cfRecord) error {
	if r.id == "" {
		return nil
	}
	return c.do(ctx, http.MethodDelete, "/zones/"+r.zone+"/dns_records/"+r.id, nil, nil)
}
