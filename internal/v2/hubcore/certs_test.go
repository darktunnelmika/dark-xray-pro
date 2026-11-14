package hubcore

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

func testCert(t *testing.T, domain string, notAfter time.Time) (string, string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain},
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}))
}

func TestCertificatesAndTLSPresets(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	cert, key := testCert(t, "cdn.example.com", now.Add(60*24*time.Hour))
	_, otherKey := testCert(t, "x.example.com", now.Add(60*24*time.Hour))
	if err := s.PutCert(ctx, "cdn", cert, otherKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched key: %v", err)
	}
	oldCert, oldKey := testCert(t, "old.example.com", now.Add(-time.Hour))
	if err := s.PutCert(ctx, "old", oldCert, oldKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired cert: %v", err)
	}
	if err := s.PutCert(ctx, "cdn", cert, key); err != nil {
		t.Fatal(err)
	}
	req := PresetRequest{Tag: "de-tls", NodeID: "de-1", Preset: "vless-tls-ws", Port: 8443, SNI: "cdn.example.com", Cert: "cdn",
		Endpoints: []sub.Endpoint{{Name: "Direct", Address: "cdn.example.com", Port: 443}}}
	if err := s.CreatePreset(ctx, req); err != nil {
		t.Fatal(err)
	}
	bad := req
	bad.Tag, bad.Cert = "de-tls2", "missing"
	if err := s.CreatePreset(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing cert: %v", err)
	}
	if err := s.PutClient(ctx, Client{Email: "carol", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-tls"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.RenderNode(ctx, "de-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), "dark_cert") || !strings.Contains(string(cfg), "BEGIN CERTIFICATE") {
		t.Fatal("certificate reference must be resolved inline")
	}
	links, _, _ := s.Subscription(ctx, "carol")
	if len(links) != 1 || !strings.Contains(links[0], "security=tls") || !strings.Contains(links[0], "sni=cdn.example.com") {
		t.Fatal(links)
	}
	list, _ := s.Certs(ctx)
	if len(list) != 1 || list[0].DaysLeft != 60 || list[0].Domains[0] != "cdn.example.com" || len(list[0].UsedBy) != 1 {
		t.Fatalf("%+v", list)
	}
	if err := s.DeleteCert(ctx, "cdn"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("delete in use: %v", err)
	}
	if err := s.DeleteInbound(ctx, "de-tls"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCert(ctx, "cdn"); err != nil {
		t.Fatal(err)
	}
}

func TestFallbackPreset(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "de-1", "Germany"); err != nil {
		t.Fatal(err)
	}
	cert, key := testCert(t, "edge.example.com", now.Add(60*24*time.Hour))
	if err := s.PutCert(ctx, "edge", cert, key); err != nil {
		t.Fatal(err)
	}
	req := PresetRequest{Tag: "de-fb", NodeID: "de-1", Preset: "vless-tls-fallback", Port: 443, SNI: "edge.example.com", Cert: "edge",
		Endpoints: []sub.Endpoint{{Name: "Direct", Address: "edge.example.com", Port: 443}}}
	if err := s.CreatePreset(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := s.PutClient(ctx, Client{Email: "dave", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-fb"}}); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.RenderNode(ctx, "de-1")
	if err != nil {
		t.Fatal(err)
	}
	c := string(cfg)
	for _, want := range []string{`"fallbacks"`, `"de-fb__fb0"`, `"de-fb__fb1"`, `"de-fb__fb2"`, `"dest":41000`, `"listen":"127.0.0.1"`, `xtls-rprx-vision`} {
		if !strings.Contains(c, want) {
			t.Fatalf("missing %s in %s", want, c)
		}
	}
	if strings.Contains(c, "dark_fallbacks") {
		t.Fatal("internal key leaked into config")
	}
	links, _, _ := s.Subscription(ctx, "dave")
	if len(links) != 4 {
		t.Fatalf("want parent + 3 fallback links, got %v", links)
	}
	joined := strings.Join(links, "\n")
	for _, want := range []string{"vless://", "vmess://", "trojan://", "type=ws", "flow=xtls-rprx-vision"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, links)
		}
	}
	if parentTag("de-fb__fb2") != "de-fb" || parentTag("de-fb") != "de-fb" {
		t.Fatal("parentTag")
	}
	hu := PresetRequest{Tag: "de-hu", NodeID: "de-1", Preset: "vless-tls-httpupgrade", Port: 8443, SNI: "edge.example.com", Cert: "edge",
		Endpoints: []sub.Endpoint{{Name: "Direct", Address: "edge.example.com", Port: 8443}}}
	if err := s.CreatePreset(ctx, hu); err != nil {
		t.Fatal(err)
	}
	if err := s.PutClient(ctx, Client{Email: "erin", Enabled: true, VolumeBytes: -1, Inbounds: []string{"de-hu"}}); err != nil {
		t.Fatal(err)
	}
	if hl, _, _ := s.Subscription(ctx, "erin"); len(hl) != 1 || !strings.Contains(hl[0], "type=httpupgrade") || !strings.Contains(hl[0], "security=tls") {
		t.Fatalf("httpupgrade link: %v", hl)
	}
	bad := PresetRequest{Tag: "de-x", NodeID: "de-1", Preset: "vless-ws", Port: 41003}
	if err := s.CreatePreset(ctx, bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reserved port: %v", err)
	}
}
