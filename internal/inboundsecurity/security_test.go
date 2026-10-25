package inboundsecurity

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

type fixtureConnection struct {
	net.Conn
	remote net.Addr
}

func (c fixtureConnection) RemoteAddr() net.Addr { return c.remote }

// All handshakes run over net.Pipe. The injected virtual peer address lets the
// production public-address policy run unchanged; no socket reaches a network.
func tlsFixture(t *testing.T, version uint16, protocols []string, curves []tls.CurveID) (Config, *atomic.Int32, []byte) {
	t.Helper()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fixture CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "target.example"},
		DNSNames: []string{"target.example", "other.example"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(12 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, root, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	serverConfig := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}},
		MinVersion: version, MaxVersion: version, NextProtos: protocols, CurvePreferences: curves}
	var dials atomic.Int32
	config := Config{RootCAs: pool, Now: func() time.Time { return now }, ProbeTimeout: time.Second, FindTimeout: 5 * time.Second,
		Resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		}),
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || (address != "8.8.8.8:443" && address != "8.8.8.8:8443") {
				t.Errorf("connection was not pinned to the fixture: %q %q", network, address)
			}
			dials.Add(1)
			client, server := net.Pipe()
			remote, err := net.ResolveTCPAddr("tcp", address) // literal address only; no DNS.
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				defer server.Close()
				secured := tls.Server(server, serverConfig)
				_ = secured.HandshakeContext(ctx)
			}()
			return fixtureConnection{Conn: client, remote: remote}, nil
		},
	}
	return config, &dials, leafDER
}

func serviceForTest(t *testing.T, config Config) *Service {
	t.Helper()
	service, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestProbeMeasuresTLSAndChecksChainIndependentlyFromName(t *testing.T) {
	config, dials, leaf := tlsFixture(t, tls.VersionTLS13, []string{"h2"}, []tls.CurveID{tls.X25519})
	service := serviceForTest(t, config)
	request := ProbeRequest{Scope: "hub", Target: "target.example:443"}
	result, err := service.Probe(context.Background(), request)
	if err != nil || !result.OK || !result.Suitable || !result.TLS13 || !result.H2 || !result.X25519 ||
		result.Scope != "hub" || result.IP != "8.8.8.8" || result.ServerName != "target.example" || result.CurveID != "X25519 (forced)" ||
		result.HybridKeyExchange != "not_measured" || !result.Certificate.Valid || result.CertChainBytes <= len(leaf) {
		t.Fatalf("unexpected TLS assessment: %+v, %v", result, err)
	}
	wantHash := sha256.Sum256(leaf)
	if len(result.Certificate.Certificates) != 2 || result.Certificate.Certificates[0].SHA256Hex != hex.EncodeToString(wantHash[:]) ||
		result.Certificate.Certificates[0].SHA256Base64 != base64.StdEncoding.EncodeToString(wantHash[:]) {
		t.Fatal("certificate hashes did not describe the actual peer DER")
	}
	request.ServerName = "wrong.example"
	result, err = service.Probe(context.Background(), request)
	if err != nil || !result.OK || result.Suitable || !result.Certificate.ChainValid || result.Certificate.HostnameValid || result.Reason != "certificate_name_mismatch" {
		t.Fatalf("SNI mismatch was confused with trust: %+v, %v", result, err)
	}
	if dials.Load() != 2 {
		t.Fatal("unexpected connection count")
	}
}

func TestProbeReportsUntrustedCertificateWithoutClaimingSuitable(t *testing.T) {
	config, _, _ := tlsFixture(t, tls.VersionTLS13, []string{"h2"}, []tls.CurveID{tls.X25519})
	config.RootCAs = x509.NewCertPool()
	result, err := serviceForTest(t, config).Probe(context.Background(), ProbeRequest{Scope: "hub", Target: "target.example:443"})
	if err != nil || !result.OK || result.Suitable || result.Certificate.ChainValid || !result.Certificate.HostnameValid || result.Reason != "certificate_chain_untrusted" {
		t.Fatalf("untrusted chain accepted: %+v %v", result, err)
	}
}

func TestProbeRejectsMissingTLS13H2OrX25519(t *testing.T) {
	for _, item := range []struct {
		name    string
		version uint16
		alpn    []string
		curves  []tls.CurveID
		reason  string
	}{
		{"tls12", tls.VersionTLS12, []string{"h2"}, []tls.CurveID{tls.X25519}, "tls13_required"},
		{"http11", tls.VersionTLS13, []string{"http/1.1"}, []tls.CurveID{tls.X25519}, "h2_not_negotiated"},
		{"p256", tls.VersionTLS13, []string{"h2"}, []tls.CurveID{tls.CurveP256}, "tls_handshake_failed"},
	} {
		t.Run(item.name, func(t *testing.T) {
			config, _, _ := tlsFixture(t, item.version, item.alpn, item.curves)
			result, err := serviceForTest(t, config).Probe(context.Background(), ProbeRequest{Scope: "hub", Target: "target.example:443"})
			if err != nil || result.Suitable || result.Reason != item.reason {
				t.Fatalf("incompatible target accepted: %+v %v", result, err)
			}
		})
	}
}

func TestIPCandidateDiscoversSNIOnSamePinnedAddress(t *testing.T) {
	config, dials, _ := tlsFixture(t, tls.VersionTLS13, []string{"h2"}, []tls.CurveID{tls.X25519})
	config.Resolver = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		t.Fatal("IP candidate or certificate SAN unexpectedly triggered DNS")
		return nil, nil
	})
	result, err := serviceForTest(t, config).Probe(context.Background(), ProbeRequest{Scope: "hub", Target: "8.8.8.8:443"})
	if err != nil || !result.Suitable || result.Target != "8.8.8.8:443" || result.ServerName != "target.example" || result.ServerNameSource != "certificate_san" || dials.Load() != 2 {
		t.Fatalf("SNI discovery did not preserve pinned target: %+v %v", result, err)
	}
}

func TestPublicTargetAdmissionAndDNSRebinding(t *testing.T) {
	for _, value := range []string{"127.0.0.1:443", "10.1.2.3:443", "100.64.0.1:443", "169.254.169.254:80", "192.0.2.1:443", "224.0.0.1:443", "[::1]:443", "[::ffff:8.8.8.8]:443", "[64:ff9b::808:808]:443", "[2001:db8::1]:443", "[2002:808:808::1]:443", "localhost:443", "127.1:443", "2130706433:443", "https://target.example:443", "user@target.example:443", "target.example:0443", "target.example:0", "target.example:443/path", "target.example:443\n"} {
		if _, _, err := parseTarget(value, ""); err == nil {
			t.Errorf("unsafe/ambiguous target accepted: %q", value)
		}
	}
	for _, value := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicAddress(netip.MustParseAddr(value)) {
			t.Errorf("public fixture denied: %s", value)
		}
	}
	config, dials, _ := tlsFixture(t, tls.VersionTLS13, []string{"h2"}, []tls.CurveID{tls.X25519})
	config.Resolver = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	})
	result, err := serviceForTest(t, config).Probe(context.Background(), ProbeRequest{Scope: "hub", Target: "target.example:443"})
	if err != nil || result.Error != "dns_contains_nonpublic_address" || dials.Load() != 0 {
		t.Fatalf("mixed DNS answer reached a connection: %+v %v", result, err)
	}
	config, _, _ = tlsFixture(t, tls.VersionTLS13, []string{"h2"}, []tls.CurveID{tls.X25519})
	config.DialContext = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		server.Close()
		return fixtureConnection{Conn: client, remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 443}}, nil
	}
	result, err = serviceForTest(t, config).Probe(context.Background(), ProbeRequest{Scope: "hub", Target: "target.example:443"})
	if err != nil || result.Error != "pinned_address_mismatch" {
		t.Fatalf("changed peer address accepted: %+v %v", result, err)
	}
}

func TestNodeScopeNeverFallsBackToHub(t *testing.T) {
	config, dials, _ := tlsFixture(t, tls.VersionTLS13, []string{"h2"}, []tls.CurveID{tls.X25519})
	request := ProbeRequest{Scope: "node", NodeID: "node-1", Target: "target.example:443"}
	service := serviceForTest(t, config)
	if _, err := service.Probe(context.Background(), request); errorCode(err) != "selected_node_transport_unavailable" || dials.Load() != 0 {
		t.Fatal("missing node transport silently used HUB")
	}
	if _, err := service.ProbeLocal(context.Background(), request); errorCode(err) != "local_probe_scope_mismatch" {
		t.Fatal("HUB local probe mislabeled as node")
	}
	config.NodeProbe = func(_ context.Context, node string, q ProbeRequest) (ProbeResult, error) {
		return ProbeResult{Scope: "hub", Target: q.Target}, nil
	}
	if _, err := serviceForTest(t, config).Probe(context.Background(), request); errorCode(err) != "selected_node_probe_scope_mismatch" || dials.Load() != 0 {
		t.Fatal("mismatched node result accepted")
	}
	config.NodeProbe = nil
	config.LocalNodeID = "node-1"
	result, err := serviceForTest(t, config).ProbeLocal(context.Background(), request)
	if err != nil || !result.Suitable || result.Scope != "node" || result.NodeID != "node-1" {
		t.Fatalf("matching agent scope failed: %+v %v", result, err)
	}
}

func TestFindPresetsCIDRBoundsAndCancellation(t *testing.T) {
	config, dials, _ := tlsFixture(t, tls.VersionTLS13, []string{"h2"}, []tls.CurveID{tls.X25519})
	service := serviceForTest(t, config)
	capability := service.Capabilities()
	if len(capability.DefaultTargets) != 10 || dials.Load() != 0 {
		t.Fatal("capabilities scanned presets or did not expose them")
	}
	capability.DefaultTargets[0].Target = "127.0.0.1:443"
	if service.Capabilities().DefaultTargets[0].Target == "127.0.0.1:443" {
		t.Fatal("capabilities exposed mutable policy")
	}
	config.DefaultTargets = []Target{{"target.example:443", ""}, {"target.example:8443", ""}}
	service = serviceForTest(t, config)
	result, err := service.Find(context.Background(), FindRequest{Scope: "hub"})
	if err != nil || result.Source != "defaults" || result.Total != 2 || result.Completed != 2 || result.Suitable != 2 || result.Cancelled {
		t.Fatalf("preset finder failed: %+v %v", result, err)
	}
	for _, cidr := range []string{"8.8.8.0/23", "8.8.8.1/24", "127.0.0.0/24", "2001:db8::/120"} {
		if _, _, err := service.findTargets(FindRequest{CIDR: cidr}); err == nil {
			t.Errorf("unsafe CIDR accepted: %s", cidr)
		}
	}
	targets, source, err := service.findTargets(FindRequest{CIDR: "8.8.8.0/24"})
	if err != nil || source != "cidr" || len(targets) != 256 || targets[0].Target != "8.8.8.0:443" || targets[255].Target != "8.8.8.255:443" {
		t.Fatal("bounded public CIDR expansion failed")
	}
	if _, _, err := service.findTargets(FindRequest{CIDR: "8.8.8.0/24", Targets: []Target{{"target.example:443", ""}}}); err == nil {
		t.Fatal("ambiguous list and CIDR accepted")
	}
	before := dials.Load()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err = service.Find(ctx, FindRequest{Scope: "hub"})
	if err != nil || !result.Cancelled || result.Completed != 0 || dials.Load() != before {
		t.Fatalf("cancelled finder started a probe: %+v %v", result, err)
	}
}

func TestProbeCancellationAndGlobalConcurrencyBound(t *testing.T) {
	var active, maximum atomic.Int32
	config := Config{MaxConcurrency: 2, ProbeTimeout: 15 * time.Millisecond, FindTimeout: 30 * time.Millisecond,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			current := active.Add(1)
			for old := maximum.Load(); current > old && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
			}
			defer active.Add(-1)
			<-ctx.Done()
			return nil, ctx.Err()
		}}
	result, err := serviceForTest(t, config).Find(context.Background(), FindRequest{Scope: "hub", CIDR: "8.8.8.0/24"})
	if err != nil || !result.Cancelled || result.Completed >= result.Total || maximum.Load() > 2 || active.Load() != 0 {
		t.Fatalf("finder exceeded bound or leaked work: completed=%d total=%d active=%d maximum=%d error=%v", result.Completed, result.Total, active.Load(), maximum.Load(), err)
	}
}

func TestAlreadyCancelledProbeDoesNotResolveOrDial(t *testing.T) {
	var resolutions, dials atomic.Int32
	service := serviceForTest(t, Config{
		Resolver: resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
			resolutions.Add(1)
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		}),
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, context.Canceled
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := service.Probe(ctx, ProbeRequest{Scope: "hub", Target: "target.example:443"})
	if err != nil || result.Error != "probe_cancelled" || resolutions.Load() != 0 || dials.Load() != 0 {
		t.Fatal("cancelled single probe started network work")
	}
}

func TestGeneratedKeysAndCertificateRoundtrip(t *testing.T) {
	service := serviceForTest(t, Config{})
	keys, err := service.GenerateX25519()
	if err != nil || keys.Kind != "x25519" || !canonicalBase64(keys.PrivateKey, base64.RawURLEncoding, 32) || !canonicalBase64(keys.PublicKey, base64.RawURLEncoding, 32) {
		t.Fatal("invalid X25519 pair")
	}
	privateBytes, _ := base64.RawURLEncoding.DecodeString(keys.PrivateKey)
	private, _ := ecdh.X25519().NewPrivateKey(privateBytes)
	if base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()) != keys.PublicKey {
		t.Fatal("X25519 public key does not match private key")
	}
	generated, err := service.GenerateCertificate(GenerateCertificateRequest{ServerNames: []string{"target.example", "127.0.0.1"}, ValidDays: 365})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tls.X509KeyPair([]byte(generated.CertificatePEM), []byte(generated.PrivateKeyPEM)); err != nil {
		t.Fatal("generated certificate and key do not match")
	}
	if generated.Certificate.ChainValid || generated.Certificate.Valid || !generated.Certificate.HostnameValid || !generated.Certificate.TimeValid {
		t.Fatal("self-signed certificate was presented as publicly trusted")
	}
	inspection, err := service.InspectCertificate(generated.CertificatePEM, "target.example")
	if err != nil || len(inspection.Certificates) != 1 || len(inspection.Certificates[0].SHA256Hex) != 64 || len(inspection.Certificates[0].SPKISHA256Hex) != 64 {
		t.Fatal("PEM metadata roundtrip failed")
	}
	for _, bad := range []string{generated.PrivateKeyPEM, "garbage\n" + generated.CertificatePEM, generated.CertificatePEM + "garbage", strings.Repeat("x", MaxPEMBytes+1)} {
		if _, err := service.InspectCertificate(bad, "target.example"); err == nil {
			t.Fatal("non-certificate or oversized PEM accepted")
		}
	}
	block, _ := pem.Decode([]byte(generated.CertificatePEM))
	hash := sha256.Sum256(block.Bytes)
	if inspection.Certificates[0].SHA256Hex != hex.EncodeToString(hash[:]) {
		t.Fatal("PEM hash was not DER SHA256")
	}
	if _, err := service.GenerateCertificate(GenerateCertificateRequest{ServerNames: []string{"target.example"}, ValidDays: 3651}); err == nil {
		t.Fatal("unbounded certificate validity accepted")
	}
}
