package sniscan

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunScoresAndOrders(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	d := &net.Dialer{Timeout: time.Second}
	s := Scanner{
		Roots: roots,
		Lookup: func(ctx context.Context, host string) ([]net.IP, error) {
			if host == "missing.example.org" {
				return nil, &net.DNSError{Err: "nx", Name: host}
			}
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		},
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		},
	}
	res := s.Run(context.Background(), Args{Domains: []string{"example.com", "other.example.net", "missing.example.org", "*.bad", "example.com"}})
	if res.Scanned != 3 {
		t.Fatalf("scanned %d, want 3 (dedupe and invalid names dropped): %+v", res.Scanned, res.Candidates)
	}
	best := res.Candidates[0]
	if best.Domain != "example.com" || !best.OK || !best.TLS13 || !best.H2 || best.Score < 50 {
		t.Fatalf("best %+v", best)
	}
	byName := map[string]Candidate{}
	for _, c := range res.Candidates {
		byName[c.Domain] = c
	}
	if c := byName["other.example.net"]; c.OK || c.Error != "certificate" {
		t.Fatalf("name mismatch should fail verification: %+v", c)
	}
	if c := byName["missing.example.org"]; c.OK || c.Error != "dns" {
		t.Fatalf("dns failure: %+v", c)
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"www.microsoft.com", "a-b.example.io"} {
		if !validName(ok) {
			t.Errorf("%s rejected", ok)
		}
	}
	for _, bad := range []string{"", "*.x.com", "1.2.3.4", "localhost", "x.local", "a b.com", "ex_ample.com", "srv.5.101.216.99.nip.io", "x.sslip.io"} {
		if validName(bad) {
			t.Errorf("%s accepted", bad)
		}
	}
}
