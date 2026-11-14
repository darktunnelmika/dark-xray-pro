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
