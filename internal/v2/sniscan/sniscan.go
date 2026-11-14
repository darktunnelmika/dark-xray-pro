// Package sniscan finds good REALITY camouflage targets as seen from a
// Node's own network. A good target answers on 443 with TLS 1.3 and h2,
// presents a certificate that verifies for the name, and is close to the
// Node: a site in the same datacenter makes the relayed handshake look
// local and fast.
//
// Two sources of candidates are combined: a list of well-known domains,
// and (optionally) the names on certificates served by neighbouring
// addresses in the Node's own /24.
package sniscan

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Args is the task input.
type Args struct {
	Domains []string `json:"domains"`
	// Subnet also scans the /24 around IP for certificate names.
	Subnet bool   `json:"subnet"`
	IP     string `json:"ip,omitempty"`
}

// Candidate is one probed domain.
type Candidate struct {
	Domain    string `json:"domain"`
	IP        string `json:"ip,omitempty"`
	OK        bool   `json:"ok"`
	TLS13     bool   `json:"tls13"`
	H2        bool   `json:"h2"`
	LatencyMS int    `json:"latency_ms"`
	Issuer    string `json:"issuer,omitempty"`
	Neighbor  bool   `json:"neighbor,omitempty"`
	Score     int    `json:"score"`
	Error     string `json:"error,omitempty"`
}

// Result is the task output, best candidates first.
type Result struct {
	Candidates []Candidate `json:"candidates"`
	Scanned    int         `json:"scanned"`
	Neighbors  int         `json:"neighbors"`
	TookMS     int         `json:"took_ms"`
}

// Scanner holds tunables; the zero value is usable.
type Scanner struct {
	Timeout     time.Duration // per probe, default 4s
	Concurrency int           // default 24
	// Dial is replaceable in tests.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Lookup is replaceable in tests.
	Lookup func(ctx context.Context, host string) ([]net.IP, error)
	Roots  *x509.CertPool
}

const maxCandidates = 120

// Run probes every candidate and returns them scored.
func (s Scanner) Run(ctx context.Context, a Args) Result {
	start := time.Now()
	s.defaults()
	seen := map[string]bool{}
	var names []string
	add := func(d string, n bool, list *[]string) {
		d = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(d, ".")))
		if !validName(d) || seen[d] || len(names) >= maxCandidates {
			return
		}
		seen[d] = true
		*list = append(*list, d)
	}
	neighbor := map[string]bool{}
	var res Result
	if a.Subnet {
		for _, d := range s.neighbors(ctx, a.IP) {
			before := len(names)
			add(d, true, &names)
			if len(names) > before {
				neighbor[d] = true
			}
		}
		res.Neighbors = len(neighbor)
	}
	for _, d := range a.Domains {
		add(d, false, &names)
	}
	out := make([]Candidate, len(names))
	sem := make(chan struct{}, s.Concurrency)
	var wg sync.WaitGroup
	for i, d := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, d string) {
			defer wg.Done()
			defer func() { <-sem }()
			c := s.probe(ctx, d)
			c.Neighbor = neighbor[d]
			c.Score = score(c)
			out[i] = c
		}(i, d)
	}
	wg.Wait()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].LatencyMS < out[j].LatencyMS
	})
	res.Candidates = out
	res.Scanned = len(out)
	res.TookMS = int(time.Since(start) / time.Millisecond)
	return res
}

func (s *Scanner) defaults() {
	if s.Timeout <= 0 {
		s.Timeout = 4 * time.Second
	}
	if s.Concurrency <= 0 {
		s.Concurrency = 24
	}
	if s.Dial == nil {
		d := &net.Dialer{Timeout: s.Timeout}
		s.Dial = d.DialContext
	}
	if s.Lookup == nil {
		s.Lookup = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
}

// probe resolves d, then times TCP connect plus the TLS handshake.
func (s Scanner) probe(ctx context.Context, d string) Candidate {
	c := Candidate{Domain: d}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout+2*time.Second)
	defer cancel()
	ips, err := s.Lookup(ctx, d)
	if err != nil || len(ips) == 0 {
		c.Error = "dns"
		return c
	}
	ip := ips[0]
	for _, x := range ips {
		if x.To4() != nil {
			ip = x
			break
		}
	}
	c.IP = ip.String()
	t0 := time.Now()
	raw, err := s.Dial(ctx, "tcp", net.JoinHostPort(c.IP, "443"))
	if err != nil {
		c.Error = "connect"
		return c
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(s.Timeout))
	conn := tls.Client(raw, &tls.Config{ServerName: d, NextProtos: []string{"h2", "http/1.1"}, MinVersion: tls.VersionTLS12, RootCAs: s.Roots})
	if err := conn.HandshakeContext(ctx); err != nil {
		var ve *tls.CertificateVerificationError
		if errors.As(err, &ve) {
			c.Error = "certificate"
		} else {
			c.Error = "tls"
		}
		return c
	}
	c.LatencyMS = int(time.Since(t0) / time.Millisecond)
	st := conn.ConnectionState()
	c.TLS13 = st.Version == tls.VersionTLS13
	c.H2 = st.NegotiatedProtocol == "h2"
	if len(st.PeerCertificates) > 0 {
		iss := st.PeerCertificates[0].Issuer
		if len(iss.Organization) > 0 {
			c.Issuer = iss.Organization[0]
		} else {
			c.Issuer = iss.CommonName
		}
	}
	c.OK = true
	return c
}

// score: TLS 1.3 and h2 are what REALITY needs; then lower latency wins
// and a same-datacenter neighbour gets a bonus.
func score(c Candidate) int {
	if !c.OK {
		return 0
	}
	v := 100
	if !c.TLS13 {
		v -= 70
	}
	if !c.H2 {
		v -= 25
	}
	v -= c.LatencyMS / 8
	if c.Neighbor {
		v += 10
	}
	if v < 1 {
		v = 1
	}
	if v > 100 {
		v = 100
	}
	return v
}

// neighbors collects certificate names served by the /24 around ip.
func (s Scanner) neighbors(ctx context.Context, ip string) []string {
	p := net.ParseIP(ip).To4()
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	var mu sync.Mutex
	var names []string
	sem := make(chan struct{}, 64)
	var wg sync.WaitGroup
	for i := 1; i < 255; i++ {
		if byte(i) == p[3] {
			continue
		}
		addr := net.IPv4(p[0], p[1], p[2], byte(i)).String()
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			got := s.certNames(ctx, addr)
			mu.Lock()
			names = append(names, got...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Strings(names)
	return names
}

func (s Scanner) certNames(ctx context.Context, addr string) []string {
	dctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	raw, err := s.Dial(dctx, "tcp", net.JoinHostPort(addr, "443"))
	if err != nil {
		return nil
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(2 * time.Second))
	// Discovery only: the names found here are verified properly by probe.
	conn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(dctx); err != nil {
		return nil
	}
	st := conn.ConnectionState()
	if len(st.PeerCertificates) == 0 {
		return nil
	}
	leaf := st.PeerCertificates[0]
	list := append([]string{}, leaf.DNSNames...)
	if len(list) == 0 && leaf.Subject.CommonName != "" {
		list = append(list, leaf.Subject.CommonName)
	}
	var out []string
	for _, n := range list {
		if len(out) == 4 {
			break
		}
		if validName(strings.ToLower(n)) {
			out = append(out, strings.ToLower(n))
		}
	}
	return out
}

// validName accepts plain hostnames only: no wildcards, IPs or localhost.
func validName(d string) bool {
	if d == "" || len(d) > 253 || strings.ContainsAny(d, "*/: ") || !strings.Contains(d, ".") {
		return false
	}
	if net.ParseIP(d) != nil || strings.HasSuffix(d, ".local") || strings.HasSuffix(d, ".localhost") {
		return false
	}
	for _, r := range d {
		if !(r == '.' || r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// Defaults are commonly used, TLS 1.3 + h2 capable camouflage domains.
var Defaults = []string{
	"www.microsoft.com", "www.apple.com", "dl.google.com", "www.google.com", "www.speedtest.net",
	"addons.mozilla.org", "www.mozilla.org", "www.samsung.com", "www.amazon.com", "aws.amazon.com",
	"www.nvidia.com", "www.intel.com", "www.amd.com", "www.asus.com", "www.dell.com", "www.hp.com",
	"www.lenovo.com", "www.oracle.com", "www.cisco.com", "www.vmware.com", "www.tesla.com",
	"www.bing.com", "learn.microsoft.com", "update.microsoft.com", "swdist.apple.com", "gateway.icloud.com",
	"www.cloudflare.com", "www.yahoo.com", "www.ebay.com", "www.paypal.com", "www.python.org",
	"go.dev", "github.com", "www.wikipedia.org", "www.lovelive-anime.jp", "www.sony.com",
	"www.logitech.com", "www.ikea.com", "www.booking.com", "www.spotify.com", "www.adobe.com",
	"www.zoom.us", "www.dropbox.com", "www.hetzner.com", "www.ovhcloud.com", "www.digitalocean.com",
}
