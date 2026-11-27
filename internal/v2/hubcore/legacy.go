package hubcore

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Old users: a reseller the owner allows brings its customers from another
// panel (3x-ui, Marzban, PasarGuard…) by their subscription links.
//
//  1. It pastes the links; the HUB reads each one (usage, volume, expiry,
//     UUID) and makes the client under the reseller, keeping the old
//     subscription id as its token.
//  2. It points its subscription domain at the HUB. The HUB then answers
//     that domain on the old port with the new subscription (its own
//     certificate, issued here), and for three days passes the old config
//     ports through to the old server, so nobody is cut before their app
//     fetches the subscription again.
//  3. Done: apps update and land on this panel's servers.
//
// Old users cannot be renewed. Only their use from now on is paid for, from
// the reseller's volume credit as it happens; when the credit runs out they
// are switched off, and back on when credit comes.

const oldUsersSchema = `CREATE TABLE IF NOT EXISTS legacy_reps (
  rep_id TEXT PRIMARY KEY REFERENCES reps(id) ON DELETE CASCADE,
  allowed INTEGER NOT NULL DEFAULT 0,
  domain TEXT NOT NULL DEFAULT '',
  sub_port INTEGER NOT NULL DEFAULT 0,
  sub_tls INTEGER NOT NULL DEFAULT 1,
  old_ip TEXT NOT NULL DEFAULT '',
  fwd_ports TEXT NOT NULL DEFAULT '',
  fwd_until INTEGER NOT NULL DEFAULT 0,
  step INTEGER NOT NULL DEFAULT 0,
  dns_unix INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS legacy_clients (
  email TEXT PRIMARY KEY,
  rep_id TEXT NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0,
  billed INTEGER NOT NULL DEFAULT 0,
  credit_off INTEGER NOT NULL DEFAULT 0,
  fetched_unix INTEGER NOT NULL DEFAULT 0,
  imported_unix INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS legacy_clients_rep ON legacy_clients(rep_id);`

// LegacyForward is how long the old config ports are passed through.
const LegacyForward = 72 * 3600

func (s *Store) ensureOldUsersSchema() error {
	_, err := s.db.Exec(oldUsersSchema)
	return err
}

var (
	hubIPsMu sync.RWMutex
	hubIPs   []string
)

// SetHubIPs records the HUB's public addresses (main sets them from -hosts).
func SetHubIPs(hosts string) {
	var ips []string
	for _, h := range strings.Split(hosts, ",") {
		if ip := net.ParseIP(strings.TrimSpace(h)); ip != nil {
			ips = append(ips, ip.String())
		}
	}
	hubIPsMu.Lock()
	hubIPs = ips
	hubIPsMu.Unlock()
}

func HubIPs() []string {
	hubIPsMu.RLock()
	defer hubIPsMu.RUnlock()
	return append([]string(nil), hubIPs...)
}

// LegacyRep is one reseller's move.
type LegacyRep struct {
	Allowed  bool   `json:"allowed"`
	Domain   string `json:"domain"`
	SubPort  int    `json:"sub_port"`
	SubTLS   bool   `json:"sub_tls"`
	OldIP    string `json:"old_ip"`
	FwdPorts []int  `json:"fwd_ports"`
	FwdUntil int64  `json:"fwd_until"`
	Step     int    `json:"step"` // 0 links, 1 domain, 2 done
	DNSUnix  int64  `json:"dns_unix"`
}

func (s *Store) LegacyOf(ctx context.Context, rep string) LegacyRep {
	v := LegacyRep{SubTLS: true}
	var ports string
	_ = s.ro.QueryRowContext(ctx, `SELECT allowed,domain,sub_port,sub_tls,old_ip,fwd_ports,fwd_until,step,dns_unix FROM legacy_reps WHERE rep_id=?`, rep).
		Scan(&v.Allowed, &v.Domain, &v.SubPort, &v.SubTLS, &v.OldIP, &ports, &v.FwdUntil, &v.Step, &v.DNSUnix)
	v.FwdPorts = splitPorts(ports)
	return v
}

func splitPorts(s string) []int {
	out := []int{}
	for _, f := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(f); err == nil && n > 0 && n < 65536 {
			out = append(out, n)
		}
	}
	return out
}

func joinPorts(p []int) string {
	sort.Ints(p)
	var b []string
	for i, n := range p {
		if i > 0 && p[i-1] == n {
			continue
		}
		b = append(b, strconv.Itoa(n))
	}
	return strings.Join(b, ",")
}

// PutLegacyAllowed is the owner's switch for one reseller.
func (s *Store) PutLegacyAllowed(ctx context.Context, rep string, allowed bool) error {
	var n int
	if s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, rep).Scan(&n) != nil || n != 1 {
		return ErrNotFound
	}
	if allowed && s.IsSubSeller(ctx, rep) {
		return fmt.Errorf("%w: sub-sellers cannot bring old users", ErrPolicy)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO legacy_reps(rep_id,allowed) VALUES(?,?) ON CONFLICT(rep_id) DO UPDATE SET allowed=excluded.allowed`, rep, allowed)
	return err
}

func isLegacyQ(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, email string) bool {
	var n int
	_ = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_clients WHERE email=?`, email).Scan(&n)
	return n > 0
}

// IsLegacy reports whether a client came in as an old user.
func (s *Store) IsLegacy(ctx context.Context, email string) bool { return isLegacyQ(ctx, s.ro, email) }

// ---- reading an old subscription ----

// legacySub is what one old subscription link told.
type legacySub struct {
	Token          string
	Host           string
	Port           int
	TLS            bool
	Up, Down, Tot  int64
	Expire         int64
	UUID, Secret   string
	Name           string
	Configs        [][2]string // host, port of each config
	SameHostConfig bool
}

var errNotPublic = errors.New("the address is not a public one")

// Tests read from a local server and issue no certificates.
var (
	legacyLocalOK = false
	legacyACME    = true
)

// publicIP tells whether an address is on the public internet (used for
// every fetch or post the HUB makes to an address someone else chose).
func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil && (v4[0] == 0 || v4[0] == 100 && v4[1]&0xc0 == 64) { // 0/8, 100.64/10
		return false
	}
	return true
}

// legacyClient fetches only from public addresses, checked at connect time
// so a name cannot be re-pointed inside the network between check and use.
var legacyClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{Timeout: 10 * time.Second, Control: func(_, addr string, _ syscall.RawConn) error {
			host, _, _ := net.SplitHostPort(addr)
			if !legacyLocalOK && !publicIP(net.ParseIP(host)) {
				return errNotPublic
			}
			return nil
		}}).DialContext,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true}, // old panels often have odd certificates; only reading
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConnsPerHost: 4,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// parseLegacyLink checks a subscription address and splits it.
func parseLegacyLink(raw string) (*url.URL, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return nil, "", errors.New("not a subscription address")
	}
	p := strings.TrimSuffix(u.Path, "/")
	tok := p[strings.LastIndex(p, "/")+1:]
	if !subIDRE.MatchString(tok) {
		return nil, "", errors.New("the address has no subscription id")
	}
	if len(tok) < 16 {
		return nil, "", errors.New("the subscription id is shorter than 16 characters")
	}
	return u, tok, nil
}

func (s *Store) readLegacySub(ctx context.Context, raw string) (legacySub, error) {
	var ls legacySub
	u, tok, err := parseLegacyLink(raw)
	if err != nil {
		return ls, err
	}
	ls.Token, ls.Host, ls.TLS = tok, strings.ToLower(u.Hostname()), u.Scheme == "https"
	ls.Port, _ = strconv.Atoi(u.Port())
	if ls.Port == 0 {
		ls.Port = 80
		if ls.TLS {
			ls.Port = 443
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "v2rayNG/1.9.0") // look like an app, so panels answer with the plain list
	resp, err := legacyClient.Do(req)
	if err != nil {
		if errors.Is(err, errNotPublic) {
			return ls, errNotPublic
		}
		return ls, fmt.Errorf("could not read: %v", shortErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ls, fmt.Errorf("the panel answered HTTP %d", resp.StatusCode)
	}
	info := resp.Header.Get("Subscription-Userinfo")
	if info == "" {
		return ls, errors.New("the answer has no usage information")
	}
	for _, kv := range strings.Split(info, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(kv), "=")
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		switch strings.ToLower(k) {
		case "upload":
			ls.Up = max(0, n)
		case "download":
			ls.Down = max(0, n)
		case "total":
			ls.Tot = max(0, n)
		case "expire":
			ls.Expire = max(0, n)
		}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	text := strings.TrimSpace(string(body))
	if !strings.Contains(text, "://") {
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if b, err := enc.DecodeString(strings.Join(strings.Fields(text), "")); err == nil {
				text = string(b)
				break
			}
		}
	}
	var frags []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		host, port, id, secret, frag := parseConfigLink(line)
		if host == "" {
			continue
		}
		ls.Configs = append(ls.Configs, [2]string{host, port})
		if ls.UUID == "" && id != "" {
			ls.UUID = id
		}
		if ls.Secret == "" && secret != "" {
			ls.Secret = secret
		}
		if frag != "" {
			frags = append(frags, frag)
		}
	}
	if len(ls.Configs) == 0 {
		return ls, errors.New("the subscription has no configs (expired or disabled?)")
	}
	ls.Name = legacyName(frags, tok)
	return ls, nil
}

func shortErr(err error) string {
	m := err.Error()
	if i := strings.LastIndex(m, ": "); i >= 0 {
		m = m[i+2:]
	}
	if len(m) > 80 {
		m = m[:80]
	}
	return m
}

// parseConfigLink reads the address of a vless/vmess/trojan/ss link and the
// user's id in it.
func parseConfigLink(line string) (host, port, id, secret, frag string) {
	switch {
	case strings.HasPrefix(line, "vmess://"):
		b := strings.TrimPrefix(line, "vmess://")
		var raw []byte
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			if d, err := enc.DecodeString(b); err == nil {
				raw = d
				break
			}
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			return
		}
		host, _ = m["add"].(string)
		port = fmt.Sprint(m["port"])
		id, _ = m["id"].(string)
		frag, _ = m["ps"].(string)
	case strings.HasPrefix(line, "vless://"), strings.HasPrefix(line, "trojan://"), strings.HasPrefix(line, "ss://"):
		u, err := url.Parse(line)
		if err != nil {
			return
		}
		host, port, frag = u.Hostname(), u.Port(), u.Fragment
		user := u.User.Username()
		switch u.Scheme {
		case "vless":
			id = user
		case "trojan":
			secret = user
		}
	default:
		return
	}
	if !uuidRE.MatchString(id) {
		id = ""
	}
	host = strings.ToLower(host)
	return
}

var nameCleanRE = regexp.MustCompile(`[^A-Za-z0-9._@+]+`)

// legacyName finds the user's name in the config names: with several
// configs it is the part they share (3x-ui names them inbound-email-info).
func legacyName(frags []string, tok string) string {
	clean := func(s string) string { return strings.Trim(nameCleanRE.ReplaceAllString(s, ""), "._+@") }
	split := func(f string) []string {
		var out []string
		for _, p := range strings.FieldsFunc(f, func(r rune) bool { return r == '-' || r == '|' || r == ' ' || r == '_' }) {
			if c := clean(p); len(c) >= 2 {
				out = append(out, c)
			}
		}
		return out
	}
	name := ""
	if len(frags) > 1 {
		count := map[string]int{}
		for _, f := range frags {
			seen := map[string]bool{}
			for _, p := range split(f) {
				if !seen[p] {
					seen[p] = true
					count[p]++
				}
			}
		}
		for _, p := range split(frags[0]) {
			if count[p] == len(frags) {
				name = p
				break
			}
		}
	}
	if name == "" && len(frags) > 0 {
		if parts := split(frags[0]); len(parts) >= 2 {
			name = parts[1]
		} else if len(parts) == 1 {
			name = parts[0]
		}
	}
	if len(name) > 40 {
		name = name[:40]
	}
	if name == "" || !emailRE.MatchString(name) {
		name = "old-" + tok[:min(8, len(tok))]
	}
	return name
}

// ---- the import job ----

// LegacyFail is one link that did not come in.
type LegacyFail struct {
	Link   string `json:"link"`
	Reason string `json:"reason"`
}

// LegacyJob is the progress of reading links.
type LegacyJob struct {
	Running bool         `json:"running"`
	Total   int          `json:"total"`
	Done    int          `json:"done"`
	Added   int          `json:"added"`
	Already int          `json:"already"`
	Failed  []LegacyFail `json:"failed"`
}

var (
	legacyJobsMu sync.Mutex
	legacyJobs   = map[string]*LegacyJob{}
)

func legacyJob(rep string) LegacyJob {
	legacyJobsMu.Lock()
	defer legacyJobsMu.Unlock()
	if j, ok := legacyJobs[rep]; ok {
		c := *j
		c.Failed = append([]LegacyFail(nil), j.Failed...)
		return c
	}
	return LegacyJob{Failed: []LegacyFail{}}
}

// StartLegacyImport reads the links in the background.
func (s *Store) StartLegacyImport(ctx context.Context, rep string, links []string) error {
	lr := s.LegacyOf(ctx, rep)
	if !lr.Allowed || s.IsSubSeller(ctx, rep) {
		return fmt.Errorf("%w: bringing old users is not allowed for this reseller", ErrPolicy)
	}
	if lr.Step != 0 {
		return fmt.Errorf("%w: the domain is already moved; links cannot be read any more", ErrPolicy)
	}
	var clean []string
	seen := map[string]bool{}
	for _, l := range links {
		l = strings.TrimSpace(l)
		if l != "" && !seen[l] {
			seen[l] = true
			clean = append(clean, l)
		}
	}
	if len(clean) == 0 || len(clean) > 5000 {
		return fmt.Errorf("%w: 1 to 5000 links", ErrInvalid)
	}
	legacyJobsMu.Lock()
	if j, ok := legacyJobs[rep]; ok && j.Running {
		legacyJobsMu.Unlock()
		return fmt.Errorf("%w: links are still being read", ErrPolicy)
	}
	job := &LegacyJob{Running: true, Total: len(clean), Failed: []LegacyFail{}}
	legacyJobs[rep] = job
	legacyJobsMu.Unlock()
	go s.runLegacyImport(context.WithoutCancel(ctx), rep, clean, job)
	return nil
}

func (s *Store) runLegacyImport(ctx context.Context, rep string, links []string, job *LegacyJob) {
	defer func() {
		legacyJobsMu.Lock()
		job.Running = false
		legacyJobsMu.Unlock()
	}()
	var mu sync.Mutex // one client written at a time
	work := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for l := range work {
				added, err := s.importLegacyLink(ctx, rep, l, &mu)
				legacyJobsMu.Lock()
				job.Done++
				switch {
				case err != nil:
					job.Failed = append(job.Failed, LegacyFail{Link: l, Reason: err.Error()})
				case added:
					job.Added++
				default:
					job.Already++
				}
				legacyJobsMu.Unlock()
			}
		}()
	}
	for _, l := range links {
		work <- l
	}
	close(work)
	wg.Wait()
}

// importLegacyLink brings one user in; false with no error = already here.
func (s *Store) importLegacyLink(ctx context.Context, rep, link string, mu *sync.Mutex) (bool, error) {
	u, tok, err := parseLegacyLink(link)
	if err != nil {
		return false, err
	}
	lr := s.LegacyOf(ctx, rep)
	host := strings.ToLower(u.Hostname())
	if lr.Domain != "" && host != lr.Domain {
		return false, fmt.Errorf("another domain than %s", lr.Domain)
	}
	var owner string
	if s.ro.QueryRowContext(ctx, `SELECT c.rep_id FROM clients c WHERE c.sub_token=? AND c.sub_token<>''`, tok).Scan(&owner) == nil {
		if owner == rep && s.ro.QueryRowContext(ctx, `SELECT 1 FROM legacy_clients l JOIN clients c ON c.email=l.email WHERE c.sub_token=? AND c.sub_token<>''`, tok).Scan(new(int)) == nil {
			return false, nil
		}
		return false, errors.New("this subscription id is already used here")
	}
	ls, err := s.readLegacySub(ctx, link)
	if err != nil {
		return false, err
	}
	mu.Lock()
	defer mu.Unlock()
	lr = s.LegacyOf(ctx, rep)
	if lr.Domain == "" {
		// The first link fixes the domain and the old server's address.
		ips, err := net.DefaultResolver.LookupHost(ctx, ls.Host)
		if err != nil || len(ips) == 0 {
			return false, errors.New("the domain does not resolve")
		}
		for _, ip := range ips {
			for _, h := range HubIPs() {
				if ip == h {
					return false, errors.New("the domain already points to this panel; read the links before moving it")
				}
			}
		}
		if !legacyLocalOK && !publicIP(net.ParseIP(ips[0])) {
			return false, errors.New("the domain points to a private address")
		}
		lr.Domain, lr.SubPort, lr.SubTLS, lr.OldIP = ls.Host, ls.Port, ls.TLS, ips[0]
	}
	// Config ports on the same domain (or the old server's address) are
	// passed through while apps update.
	ports := lr.FwdPorts
	for _, c := range ls.Configs {
		if c[0] == lr.Domain || c[0] == lr.OldIP {
			if n, err := strconv.Atoi(c[1]); err == nil && n > 0 && n < 65536 && n != lr.SubPort && !contains(intsToStr(ports), c[1]) && len(ports) < legacyMaxPorts {
				ports = append(ports, n)
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO legacy_reps(rep_id,allowed,domain,sub_port,sub_tls,old_ip,fwd_ports) VALUES(?,1,?,?,?,?,?)
ON CONFLICT(rep_id) DO UPDATE SET domain=excluded.domain, sub_port=excluded.sub_port, sub_tls=excluded.sub_tls, old_ip=excluded.old_ip, fwd_ports=excluded.fwd_ports`,
		rep, lr.Domain, lr.SubPort, lr.SubTLS, lr.OldIP, joinPorts(ports)); err != nil {
		return false, err
	}
	pol, _ := s.RepPolicyOf(ctx, rep)
	inb := pol.Inbounds
	if len(inb) == 0 {
		list, _ := s.Inbounds(ctx)
		for _, ib := range list {
			inb = append(inb, ib.Tag)
		}
	}
	email := ls.Name
	for i := 2; ; i++ {
		var n int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, email).Scan(&n)
		if n == 0 {
			break
		}
		if i > 999 {
			return false, errors.New("no free name")
		}
		email = fmt.Sprintf("%s-%d", ls.Name, i)
	}
	id := ls.UUID
	if id != "" {
		var n int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE uuid=?`, id).Scan(&n)
		if n > 0 {
			id = ""
		}
	}
	plan, vol := "volume", ls.Tot
	if vol == 0 {
		plan, vol = "unlimited", -1
	}
	c := Client{Email: email, UUID: id, Secret: ls.Secret, Enabled: true, VolumeBytes: vol, ExpiresUnix: ls.Expire / expDiv(ls.Expire), Inbounds: inb, SubToken: ls.Token}
	now := s.now().Unix()
	if _, err := s.ImportClients(ctx, []ClientRecord{{Client: c, Plan: plan, RepID: rep, Note: "old user · " + ls.Host, Usage: Usage{Up: ls.Up, Down: ls.Down, FirstSeen: now}}}); err != nil {
		return false, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO legacy_clients(email,rep_id,last_used,imported_unix) VALUES(?,?,?,?)`, email, rep, ls.Up+ls.Down, now); err != nil {
		return false, err
	}
	return true, nil
}

// expDiv: some panels send the expiry in milliseconds.
func expDiv(v int64) int64 {
	if v > 100_000_000_000 {
		return 1000
	}
	return 1
}

// ---- the domain step ----

// LegacyStep moves between "links" (0) and "domain" (1).
func (s *Store) LegacyStep(ctx context.Context, rep string, step int) error {
	lr := s.LegacyOf(ctx, rep)
	if !lr.Allowed {
		return ErrPolicy
	}
	if lr.Step == 2 || step < 0 || step > 1 {
		return fmt.Errorf("%w: already done", ErrPolicy)
	}
	if step == 1 {
		var n int
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM legacy_clients WHERE rep_id=?`, rep).Scan(&n)
		if n == 0 || lr.Domain == "" {
			return fmt.Errorf("%w: bring at least one user first", ErrPolicy)
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE legacy_reps SET step=? WHERE rep_id=?`, step, rep)
	return err
}

// LegacyCheckDNS finishes the move once the domain points at the HUB: the
// certificate is ordered, the old ports open here, the pass-through starts.
func (s *Store) LegacyCheckDNS(ctx context.Context, rep string) (bool, string, error) {
	lr := s.LegacyOf(ctx, rep)
	if !lr.Allowed || lr.Step != 1 {
		return false, "", ErrPolicy
	}
	ips, err := lookupFresh(ctx, lr.Domain)
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
	now := s.now().Unix()
	if _, err := s.db.ExecContext(ctx, `UPDATE legacy_reps SET step=2, dns_unix=?, fwd_until=? WHERE rep_id=?`, now, now+LegacyForward, rep); err != nil {
		return false, "", err
	}
	if lr.SubTLS && legacyACME {
		if err := s.PutACMECert(ctx, "legacy-"+rep, []string{lr.Domain}, ACMEHTTPHub, ""); err != nil {
			log.Printf("legacy %s: certificate: %v", rep, err)
		}
	}
	return true, "", nil
}

// LegacyStatus is the whole picture for the panel.
type LegacyStatus struct {
	LegacyRep
	HubIPs    []string          `json:"hub_ips"`
	Clients   int               `json:"clients"`
	Updated   int               `json:"updated"`
	Off       int               `json:"off"`
	Billed    int64             `json:"billed"`
	Job       LegacyJob         `json:"job"`
	Cert      string            `json:"cert"` // pending | issuing | ok | failed
	CertError string            `json:"cert_error"`
	PortErr   map[string]string `json:"port_errors"`
}

func (s *Store) LegacyStatusOf(ctx context.Context, rep string) LegacyStatus {
	v := LegacyStatus{LegacyRep: s.LegacyOf(ctx, rep), HubIPs: HubIPs(), Job: legacyJob(rep), PortErr: map[string]string{}}
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(fetched_unix>0),0),COALESCE(SUM(credit_off),0),COALESCE(SUM(billed),0) FROM legacy_clients WHERE rep_id=?`, rep).
		Scan(&v.Clients, &v.Updated, &v.Off, &v.Billed)
	_ = s.ro.QueryRowContext(ctx, `SELECT status,last_error FROM acme_certs WHERE id=?`, "legacy-"+rep).Scan(&v.Cert, &v.CertError)
	for _, p := range append([]int{v.SubPort}, v.FwdPorts...) {
		if p > 0 && legacyReserved(p) {
			v.PortErr[strconv.Itoa(p)] = "this port is used by the panel server itself"
		}
	}
	legacyNetMu.Lock()
	for p, e := range legacyPortErr {
		if p == v.SubPort || contains(intsToStr(v.FwdPorts), strconv.Itoa(p)) {
			v.PortErr[strconv.Itoa(p)] = e
		}
	}
	legacyNetMu.Unlock()
	return v
}

func intsToStr(p []int) []string {
	out := make([]string, len(p))
	for i, n := range p {
		out[i] = strconv.Itoa(n)
	}
	return out
}

// ---- use-based billing ----

// chargeLegacy takes what old users used since the last look from their
// reseller's credit, and switches them off (or back on) with the credit.
// lookupFresh asks public resolvers directly, so a change the local cache
// has not seen yet still counts (IPv4: what the old configs used).
func lookupFresh(ctx context.Context, host string) ([]string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []string{ip.String()}, nil
	}
	for _, srv := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, "udp", srv)
		}}
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		ips, err := r.LookupIP(c, "ip4", host)
		cancel()
		if err == nil && len(ips) > 0 {
			out := make([]string, len(ips))
			for i, ip := range ips {
				out[i] = ip.String()
			}
			return out, nil
		}
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out, nil
}

// retryLegacyCerts asks again for a moved domain's certificate that failed
// (often only because a resolver still had the old address), every 10 minutes.
func (s *Store) retryLegacyCerts(ctx context.Context) {
	if !legacyACME {
		return
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT a.id FROM acme_certs a JOIN legacy_reps l ON a.id='legacy-'||l.rep_id
WHERE l.step=2 AND a.status='failed' AND a.last_try < ?`, s.now().Unix()-600)
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

func (s *Store) chargeLegacy(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM legacy_clients WHERE email NOT IN (SELECT email FROM clients)`)
	rows, err := s.ro.QueryContext(ctx, `SELECT l.email,l.rep_id,l.last_used,COALESCE(u.up+u.down,0) FROM legacy_clients l LEFT JOIN client_usage u ON u.email=l.email`)
	if err != nil {
		return
	}
	type row struct {
		email, rep string
		last, now  int64
	}
	var list []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.email, &r.rep, &r.last, &r.now) == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	reps := map[string]bool{}
	for _, r := range list {
		reps[r.rep] = true
		if r.now == r.last {
			continue
		}
		delta := r.now - r.last
		if r.now < r.last {
			delta = r.now // usage was reset: everything since counts
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		_, e1 := tx.ExecContext(ctx, `UPDATE reps SET volume_credit=MAX(0, volume_credit-?) WHERE id=?`, delta, r.rep)
		_, e2 := tx.ExecContext(ctx, `UPDATE legacy_clients SET last_used=?, billed=billed+? WHERE email=?`, r.now, delta, r.email)
		if e1 != nil || e2 != nil {
			tx.Rollback()
			continue
		}
		_ = tx.Commit()
	}
	for rep := range reps {
		var credit int64
		if s.ro.QueryRowContext(ctx, `SELECT volume_credit FROM reps WHERE id=?`, rep).Scan(&credit) != nil {
			continue
		}
		if credit <= 0 {
			r, _ := s.db.ExecContext(ctx, `UPDATE legacy_clients SET credit_off=1 WHERE rep_id=? AND credit_off=0 AND email IN (SELECT email FROM clients WHERE enabled=1)`, rep)
			if n, _ := r.RowsAffected(); n > 0 {
				_, _ = s.db.ExecContext(ctx, `UPDATE clients SET enabled=0 WHERE email IN (SELECT email FROM legacy_clients WHERE rep_id=? AND credit_off=1)`, rep)
				s.tellRep(ctx, rep, fmt.Sprintf("⛔️ اعتبار حجمی شما تمام شد؛ %d کاربر قدیمی قطع شدند. با شارژ اعتبار خودکار وصل می‌شوند.", n))
			}
		} else {
			r, _ := s.db.ExecContext(ctx, `UPDATE clients SET enabled=1 WHERE email IN (SELECT email FROM legacy_clients WHERE rep_id=? AND credit_off=1)`, rep)
			if n, _ := r.RowsAffected(); n > 0 {
				_, _ = s.db.ExecContext(ctx, `UPDATE legacy_clients SET credit_off=0 WHERE rep_id=? AND credit_off=1`, rep)
				s.tellRep(ctx, rep, fmt.Sprintf("✅ اعتبار شارژ شد؛ %d کاربر قدیمی دوباره وصل شدند.", n))
			}
		}
	}
}

func (s *Store) tellRep(ctx context.Context, rep, text string) {
	for _, c := range s.TgRepOwners(ctx, rep) {
		s.tellFrom(ctx, rep, c, text)
	}
}

// ---- listeners: the old subscription port and the pass-through ----

var (
	legacyNetMu   sync.Mutex
	legacyPortErr = map[int]string{}
)

type legacyListener struct {
	key   string // "sub:tls", "sub:http" or "fwd:<target>"
	close func()
}

// RunLegacy keeps the listeners in step with the moves and bills use, every
// 30 seconds until ctx ends. fallback is served when no certificate fits.
func (s *Store) RunLegacy(ctx context.Context, fallback tls.Certificate) {
	open := map[int]*legacyListener{}
	sub := s.legacySubHandler()
	getCert := s.PanelCertificate(fallback)
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		s.chargeLegacy(ctx)
		s.retryLegacyCerts(ctx)
		s.retryTenantSubCerts(ctx)
		if err := s.flushSubFetches(ctx); err != nil {
			log.Printf("subscription fetches: %v", err)
		}
		if s.now().Minute() == 0 {
			s.pruneSubFetches(ctx)
		}
		want := s.legacyWanted(ctx)
		for port, l := range open {
			if want[port] != l.key {
				l.close()
				delete(open, port)
			}
		}
		for port, key := range want {
			if _, ok := open[port]; ok {
				continue
			}
			l, err := s.openLegacy(port, key, sub, getCert)
			legacyNetMu.Lock()
			if err != nil {
				legacyPortErr[port] = shortErr(err)
			} else {
				delete(legacyPortErr, port)
				open[port] = l
			}
			legacyNetMu.Unlock()
		}
		select {
		case <-ctx.Done():
			for _, l := range open {
				l.close()
			}
			_ = s.flushSubFetches(context.Background())
			return
		case <-t.C:
		}
	}
}

// legacyMaxPorts caps the config ports one move passes through.
const legacyMaxPorts = 10

// legacyReserved are ports the HUB machine uses itself; an old panel's port
// never takes one of them.
func legacyReserved(p int) bool {
	switch p {
	case 22, 53, 80, 443, 2087, 2097, 28080, 28443:
		return true
	}
	return p < 1024 && p != 443
}

// legacyWanted is port → what should listen there.
func (s *Store) legacyWanted(ctx context.Context) map[int]string {
	want := map[int]string{}
	// Moves already done keep working even if the permission is taken back.
	rows, err := s.ro.QueryContext(ctx, `SELECT sub_port,sub_tls,old_ip,fwd_ports,fwd_until FROM legacy_reps WHERE step=2 ORDER BY dns_unix`)
	if err != nil {
		return want
	}
	defer rows.Close()
	now := s.now().Unix()
	for rows.Next() {
		var port int
		var tlsOn bool
		var oldIP, ports string
		var until int64
		if rows.Scan(&port, &tlsOn, &oldIP, &ports, &until) != nil {
			continue
		}
		k := "sub:http"
		if tlsOn {
			k = "sub:tls"
		}
		if _, taken := want[port]; !taken && port > 0 && !legacyReserved(port) {
			want[port] = k
		}
		if ip := net.ParseIP(oldIP); now < until && ip != nil && (legacyLocalOK || publicIP(ip)) {
			for _, p := range splitPorts(ports) {
				if _, taken := want[p]; !taken && !legacyReserved(p) {
					want[p] = "fwd:" + net.JoinHostPort(oldIP, strconv.Itoa(p))
				}
			}
		}
	}
	rows.Close()
	s.tenantSubWanted(ctx, want)
	return want
}

func (s *Store) openLegacy(port int, key string, sub http.Handler, getCert func(*tls.ClientHelloInfo) (*tls.Certificate, error)) (*legacyListener, error) {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	if target, ok := strings.CutPrefix(key, "fwd:"); ok {
		var mu sync.Mutex
		conns := map[net.Conn]bool{}
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					up, err := net.DialTimeout("tcp", target, 10*time.Second)
					if err != nil {
						c.Close()
						return
					}
					mu.Lock()
					conns[c], conns[up] = true, true
					mu.Unlock()
					done := make(chan struct{}, 2)
					go func() { _, _ = io.Copy(up, c); done <- struct{}{} }()
					go func() { _, _ = io.Copy(c, up); done <- struct{}{} }()
					<-done
					c.Close()
					up.Close()
					mu.Lock()
					delete(conns, c)
					delete(conns, up)
					mu.Unlock()
				}()
			}
		}()
		log.Printf("legacy: passing :%d through to %s", port, target)
		return &legacyListener{key: key, close: func() {
			ln.Close()
			mu.Lock()
			for c := range conns {
				c.Close()
			}
			mu.Unlock()
		}}, nil
	}
	srv := &http.Server{Handler: sub, ReadHeaderTimeout: 10 * time.Second}
	if key == "sub:tls" {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: getCert}
		go func() { _ = srv.ServeTLS(ln, "", "") }()
	} else {
		go func() { _ = srv.Serve(ln) }()
	}
	log.Printf("legacy: old subscriptions on :%d", port)
	return &legacyListener{key: key, close: func() { _ = srv.Close() }}, nil
}

// legacySubHandler answers an old subscription address (any path ending in
// the old id) with the client's subscription here, and notes the fetch.
func (s *Store) legacySubHandler() http.Handler {
	sub := SubHandler(s)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimSuffix(r.URL.Path, "/")
		tok := p[strings.LastIndex(p, "/")+1:]
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if t, d, ok := s.tenantSubOf(r.Context(), host); ok {
			s.serveTenantSub(w, r, sub, t, d)
			return
		}
		var email string
		if !subIDRE.MatchString(tok) || s.ro.QueryRowContext(r.Context(), `SELECT c.email FROM clients c JOIN legacy_clients l ON l.email=c.email
JOIN legacy_reps lr ON lr.rep_id=l.rep_id WHERE c.sub_token=? AND c.sub_token<>'' AND lr.domain=?`, tok, host).Scan(&email) != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = s.db.ExecContext(r.Context(), `UPDATE legacy_clients SET fetched_unix=? WHERE email=?`, s.now().Unix(), email)
		r2 := r.Clone(r.Context())
		r2.URL.Path, r2.URL.RawPath = "/sub/"+tok, ""
		sub.ServeHTTP(w, r2)
	})
}

// ---- HTTP ----

func legacyRoutes(s *Store, mux *docMux, guard, member func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v2/admin/reps/{id}/legacy", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.LegacyStatusOf(r.Context(), r.PathValue("id")), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/reps/{id}/legacy", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Allowed bool `json:"allowed"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.PutLegacyAllowed(r.Context(), r.PathValue("id"), in.Allowed)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	rep := func(f func(w http.ResponseWriter, r *http.Request, rep string)) http.HandlerFunc {
		return member(func(w http.ResponseWriter, r *http.Request) {
			p := who(r)
			if p.Role != "rep" || !s.LegacyOf(r.Context(), p.RepID).Allowed {
				http.NotFound(w, r)
				return
			}
			f(w, r, p.RepID)
		})
	}
	mux.HandleFunc("GET /api/v2/rep/legacy", rep(func(w http.ResponseWriter, r *http.Request, id string) {
		reply(w, s.LegacyStatusOf(r.Context(), id), nil)
	}))
	mux.HandleFunc("POST /api/v2/rep/legacy/import", rep(func(w http.ResponseWriter, r *http.Request, id string) {
		var in struct {
			Links string `json:"links"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.StartLegacyImport(r.Context(), id, strings.Fields(in.Links))
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/rep/legacy/step", rep(func(w http.ResponseWriter, r *http.Request, id string) {
		var in struct {
			Step int `json:"step"`
		}
		if !decode(w, r, &in) {
			return
		}
		err := s.LegacyStep(r.Context(), id, in.Step)
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/rep/legacy/check", rep(func(w http.ResponseWriter, r *http.Request, id string) {
		ok, why, err := s.LegacyCheckDNS(r.Context(), id)
		reply(w, map[string]any{"ok": ok, "reason": why}, err)
	}))
}

// ---- HTTP-01 on the HUB ----

var (
	acmeHubMu  sync.Mutex
	acmeHubMap = map[string]string{} // path → body
	acmeHubLn  net.Listener
)

// serveHTTP01Hub answers a challenge from the HUB: as a file in
// DARK_HUB2_ACME_WEBROOT when a web server in front owns port 80, else on
// port 80 itself. It checks the domain really reaches it first.
func serveHTTP01Hub(ctx context.Context, domain, path, body string) (func(), error) {
	var done func()
	if root := acmeWebroot(); root != "" {
		f := root + path
		if err := writeFileAll(f, body); err != nil {
			return nil, fmt.Errorf("webroot: %w", err)
		}
		done = func() { _ = removeFile(f) }
	} else {
		acmeHubMu.Lock()
		if acmeHubLn == nil {
			ln, err := net.Listen("tcp", ":80")
			if err != nil {
				acmeHubMu.Unlock()
				return nil, fmt.Errorf("port 80 is busy (set DARK_HUB2_ACME_WEBROOT to the web server's challenge folder): %v", err)
			}
			acmeHubLn = ln
			go func() {
				_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					acmeHubMu.Lock()
					b, ok := acmeHubMap[r.URL.Path]
					acmeHubMu.Unlock()
					if !ok {
						http.NotFound(w, r)
						return
					}
					_, _ = io.WriteString(w, b)
				}))
			}()
		}
		acmeHubMap[path] = body
		acmeHubMu.Unlock()
		done = func() {
			acmeHubMu.Lock()
			delete(acmeHubMap, path)
			acmeHubMu.Unlock()
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
				return done, nil
			}
			err = fmt.Errorf("got HTTP %d with other content", resp.StatusCode)
		}
		last = err
		time.Sleep(2 * time.Second)
	}
	done()
	return nil, fmt.Errorf("http://%s does not reach this HUB: %v", domain, last)
}

func acmeWebroot() string { return strings.TrimRight(os.Getenv("DARK_HUB2_ACME_WEBROOT"), "/") }

func writeFileAll(f, body string) error {
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(body), 0o644)
}

func removeFile(f string) error { return os.Remove(f) }

// repPutLegacy is a reseller changing an old user: on/off and the IP limit
// only; there is no renewal. Switching one on needs credit.
func (s *Store) repPutLegacy(ctx context.Context, repID string, c Client, plan string) error {
	var owner, oldPlan string
	var old Client
	if err := s.ro.QueryRowContext(ctx, `SELECT rep_id,plan,volume_bytes,expires_unix,duration_seconds,enabled FROM clients WHERE email=?`, c.Email).
		Scan(&owner, &oldPlan, &old.VolumeBytes, &old.ExpiresUnix, &old.DurationSeconds, &old.Enabled); err != nil || owner != repID {
		return ErrNotFound
	}
	if plan != oldPlan || c.VolumeBytes != old.VolumeBytes || c.ExpiresUnix != old.ExpiresUnix || c.DurationSeconds != old.DurationSeconds {
		return fmt.Errorf("%w: old users cannot be renewed; make a new client", ErrPolicy)
	}
	if c.MaxIPs < 0 || c.MaxIPs > 1000 {
		return ErrInvalid
	}
	var oldIPs int
	_ = s.ro.QueryRowContext(ctx, `SELECT max_ips FROM clients WHERE email=?`, c.Email).Scan(&oldIPs)
	if c.Enabled != old.Enabled || c.MaxIPs != oldIPs {
		pol, err := s.RepPolicyOf(ctx, repID)
		if err != nil {
			return err
		}
		if pol.DenyEdit {
			return fmt.Errorf("%w: changing client settings is not allowed for this representative", ErrPolicy)
		}
	}
	if c.Enabled && !old.Enabled {
		var credit int64
		_ = s.ro.QueryRowContext(ctx, `SELECT volume_credit FROM reps WHERE id=?`, repID).Scan(&credit)
		if credit <= 0 {
			return fmt.Errorf("%w: no volume credit left", ErrCredit)
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE clients SET enabled=?, max_ips=? WHERE email=?`, c.Enabled, c.MaxIPs, c.Email); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE legacy_clients SET credit_off=0 WHERE email=?`, c.Email)
	return err
}
