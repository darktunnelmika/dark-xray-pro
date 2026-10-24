package xrayconfig

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"
)

func tlsFixtureMaterial(t *testing.T, edit func(*x509.Certificate)) (*TLSMaterial, string) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated TLS test CA"},
		NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "one.example.org"},
		DNSNames: []string{"one.example.org", "*.example.net"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if edit != nil {
		edit(leaf)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, root, &leafKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	rootPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}))
	return &TLSMaterial{
		CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})) + rootPEM,
		PrivateKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
	}, rootPEM
}

func tlsFixtureProfile(t *testing.T, transport string, material *TLSMaterial) Profile {
	t.Helper()
	p := testProfile(1)
	p.Security, p.Transport, p.RealityTarget, p.RealityPrivateKey, p.ShortIDs = "tls", transport, "", "", nil
	p.TLS = material
	p.ServerNames = []string{"one.example.org", "edge.example.net"}
	p.Options = &ProfileOptions{Client: &ClientOptions{Fingerprint: "firefox"}}
	switch transport {
	case "grpc":
		p.GRPCService = "typed-tls"
		p.Options.GRPCAuthority, p.Options.GRPCMultiMode = "grpc.example.org:443", true
	case "xhttp":
		p.Options.XHTTP = &XHTTPOptions{Path: "/typed-tls", Host: "transport.example.org", Mode: "auto", XPaddingBytes: "256-256"}
	case "ws":
		p.Options.WebSocket = &WebSocketOptions{Path: "/typed-tls", Host: "transport.example.org"}
	case "httpupgrade":
		p.Options.HTTPUpgrade = &HTTPUpgradeOptions{Path: "/typed-tls", Host: "transport.example.org"}
	}
	return p
}

func TestTLSMaterialCanonicalMetadataAndClockBoundary(t *testing.T) {
	material, _ := tlsFixtureMaterial(t, nil)
	input := *material
	input.CertificatePEM = " \n" + strings.ReplaceAll(input.CertificatePEM, "\n", "\r\n") + "\t"
	input.PrivateKeyPEM = "\n" + input.PrivateKeyPEM + "\n"
	normalized, metadata, err := NormalizeTLSMaterial(&input)
	if err != nil || normalized == &input || *normalized != *material {
		t.Fatal("PEM canonicalization failed", err)
	}
	block, _ := pem.Decode([]byte(material.CertificatePEM))
	leaf, _ := x509.ParseCertificate(block.Bytes)
	leafHash, publicHash := sha256.Sum256(leaf.Raw), sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	if metadata.SHA256 != hex.EncodeToString(leafHash[:]) || metadata.PublicKeySHA256 != hex.EncodeToString(publicHash[:]) ||
		metadata.KeyType != "ECDSA" || !reflect.DeepEqual(metadata.DNSNames, leaf.DNSNames) ||
		metadata.NotBeforeUnix != leaf.NotBefore.Unix() || metadata.NotAfterUnix != leaf.NotAfter.Unix() {
		t.Fatal("certificate metadata is not derived from DER")
	}
	metadata.DNSNames[0] = "changed.example.org"
	_, again, _ := NormalizeTLSMaterial(material)
	if again.DNSNames[0] != "one.example.org" {
		t.Fatal("metadata aliases certificate names")
	}
	public, _ := json.Marshal(metadata)
	if bytes.Contains(public, []byte("PRIVATE KEY")) || bytes.Contains(public, []byte("CERTIFICATE")) {
		t.Fatal("metadata contains PEM")
	}
	if err := ValidateTLSCertificateTime(material, time.Now()); err != nil {
		t.Fatal(err)
	}
	expired, _ := tlsFixtureMaterial(t, func(c *x509.Certificate) {
		c.NotBefore = time.Now().Add(-12 * time.Hour)
		c.NotAfter = time.Now().Add(-time.Hour)
	})
	if _, _, err := NormalizeTLSMaterial(expired); err != nil {
		t.Fatal("expired saved material must remain readable", err)
	}
	if err := ValidateProfile(tlsFixtureProfile(t, "tcp", expired)); err != nil {
		t.Fatal("structural profile validation consulted the clock", err)
	}
	if err := ValidateTLSCertificateTime(expired, time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatal("expired material allowed for fresh save")
	}
	if err := ValidateTLSCertificateTime(material, leaf.NotBefore.Add(-time.Second)); !errors.Is(err, ErrInvalid) {
		t.Fatal("not-yet-valid leaf accepted")
	}
}

func TestTLSMaterialRejectsMalformedPartialAndMismatchedCredentials(t *testing.T) {
	material, root := tlsFixtureMaterial(t, nil)
	other, _ := tlsFixtureMaterial(t, nil)
	tests := map[string]func(*TLSMaterial){
		"no-cert":             func(m *TLSMaterial) { m.CertificatePEM = "" },
		"no-key":              func(m *TLSMaterial) { m.PrivateKeyPEM = "" },
		"large-cert":          func(m *TLSMaterial) { m.CertificatePEM = strings.Repeat("x", MaxTLSCertificatePEMBytes+1) },
		"large-key":           func(m *TLSMaterial) { m.PrivateKeyPEM = strings.Repeat("x", MaxTLSPrivateKeyPEMBytes+1) },
		"wrong-key":           func(m *TLSMaterial) { m.PrivateKeyPEM = other.PrivateKeyPEM },
		"extra-key":           func(m *TLSMaterial) { m.PrivateKeyPEM += m.PrivateKeyPEM },
		"certificate-garbage": func(m *TLSMaterial) { m.CertificatePEM = "private-junk" + m.CertificatePEM },
		"malformed-leading-certificate": func(m *TLSMaterial) {
			m.CertificatePEM = "-----BEGIN CERTIFICATE-----\n!not-base64!\n-----END CERTIFICATE-----\n" + m.CertificatePEM
		},
		"malformed-leading-key": func(m *TLSMaterial) {
			m.PrivateKeyPEM = "-----BEGIN PRIVATE KEY-----\n!not-base64!\n-----END PRIVATE KEY-----\n" + m.PrivateKeyPEM
		},
		"unterminated-leading-certificate": func(m *TLSMaterial) {
			m.CertificatePEM = "-----BEGIN CERTIFICATE-----\n!not-base64!\n" + m.CertificatePEM
		},
		"unterminated-leading-key": func(m *TLSMaterial) {
			m.PrivateKeyPEM = "-----BEGIN PRIVATE KEY-----\n!not-base64!\n" + m.PrivateKeyPEM
		},
		"key-garbage":     func(m *TLSMaterial) { m.PrivateKeyPEM += "private-junk" },
		"duplicate-chain": func(m *TLSMaterial) { m.CertificatePEM += root },
		"wrong-issuer": func(m *TLSMaterial) {
			b, _ := pem.Decode([]byte(m.CertificatePEM))
			_, tail := pem.Decode([]byte(other.CertificatePEM))
			m.CertificatePEM = string(pem.EncodeToMemory(b)) + string(tail)
		},
		"wrong-order": func(m *TLSMaterial) { m.CertificatePEM = root + m.CertificatePEM },
	}
	for name, edit := range tests {
		t.Run(name, func(t *testing.T) {
			value := *material
			edit(&value)
			_, _, err := NormalizeTLSMaterial(&value)
			var detail *ProfileValidationError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &detail) || strings.Contains(err.Error(), "private-junk") {
				t.Fatal("credential failure was accepted or leaked input", err)
			}
		})
	}
	for _, edit := range []func(*x509.Certificate){
		func(c *x509.Certificate) { c.IsCA = true }, func(c *x509.Certificate) { c.DNSNames = nil },
		func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} },
		func(c *x509.Certificate) {
			c.ExtKeyUsage = nil
			c.UnknownExtKeyUsage = []asn1.ObjectIdentifier{{1, 2, 3, 4, 5}}
		},
		func(c *x509.Certificate) {
			c.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3, 4, 5}, Critical: true, Value: []byte{0x05, 0x00}}}
		},
		func(c *x509.Certificate) { c.DNSNames = []string{"one.example.org", "bad name.example.org"} },
		func(c *x509.Certificate) { c.DNSNames = []string{"one.example.org", "x.*.example.org"} },
		func(c *x509.Certificate) { c.DNSNames = []string{"one.example.org", strings.Repeat("a.", 127) + "org"} },
		func(c *x509.Certificate) {
			c.DNSNames = make([]string, MaxTLSCertificateDNSNames+1)
			for i := range c.DNSNames {
				c.DNSNames[i] = "one.example.org"
			}
		},
	} {
		m, _ := tlsFixtureMaterial(t, edit)
		if _, _, err := NormalizeTLSMaterial(m); !errors.Is(err, ErrInvalid) {
			t.Fatal("unusable server certificate accepted")
		}
	}
}

func TestTLSRendersEveryTransportAndRetainsAdmissionBoundary(t *testing.T) {
	material, _ := tlsFixtureMaterial(t, nil)
	for _, transport := range []string{"tcp", "grpc", "xhttp", "ws", "httpupgrade"} {
		t.Run(transport, func(t *testing.T) {
			p := tlsFixtureProfile(t, transport, material)
			p.Options.Sniffing = &SniffingOptions{Enabled: true, DestOverride: []string{"http", "tls"}, RouteOnly: true}
			assert := func(c Candidate) {
				t.Helper()
				var doc struct {
					Inbounds []inbound `json:"inbounds"`
				}
				if json.Unmarshal(c.Config, &doc) != nil {
					t.Fatal("bad candidate")
				}
				for _, in := range doc.Inbounds {
					if in.Protocol == "dokodemo-door" {
						if in.Stream.TLS != nil || bytes.Contains(c.Config, []byte("PRIVATE KEY")) {
							t.Fatal("relay received TLS private material")
						}
						continue
					}
					if in.Stream.Security != "tls" || in.Stream.TLS == nil || in.Stream.Reality != nil || in.Stream.Network != transport || in.Sniffing == nil {
						t.Fatal("customer security or sniffing changed")
					}
					if in.Stream.TLS.MinVersion != "1.3" || !in.Stream.TLS.RejectUnknownSNI || len(in.Stream.TLS.Certificates) != 1 || !in.Stream.TLS.Certificates[0].OneTimeLoading {
						t.Fatal("unsafe TLS settings")
					}
					for _, user := range in.Settings.Clients {
						if (transport == "tcp") != (user.Flow == "xtls-rprx-vision") {
							t.Fatal("transport flow mismatch")
						}
					}
				}
				public, _ := json.Marshal(c)
				if bytes.Contains(public, []byte("PRIVATE KEY")) || c.CanActivate {
					t.Fatal("candidate leaked material or gained authority")
				}
			}
			legacy, err := Render(planFor(t, 10443, 11680), []Profile{p})
			if err != nil {
				t.Fatal(err)
			}
			assert(legacy)
			assigned, err := RenderAssigned("tls-test", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}})
			if err != nil {
				t.Fatal(err)
			}
			assert(assigned)
			nodes, swaps := creationFixture()
			topology, err := RenderCreationTopology(p, nodes, swaps)
			if err != nil {
				t.Fatal(err)
			}
			for _, node := range topology.Nodes {
				assert(node.Candidate)
			}
			if _, err := LabSubscriptionRoutes(assigned, uuidA, map[uint64]string{1: "TLS"}); !errors.Is(err, ErrInvalid) {
				t.Fatal("TLS entered TCP-only subscription extraction")
			}
			if _, err := WithUsageAPI(assigned, "127.0.0.1:39041"); !errors.Is(err, ErrInvalid) {
				t.Fatal("TLS entered metered admission")
			}
		})
	}
}

func TestTLSProfileRejectsCrossSecurityFieldsAndUncoveredSNI(t *testing.T) {
	material, _ := tlsFixtureMaterial(t, nil)
	tests := map[string]func(*Profile){
		"missing-material":    func(p *Profile) { p.TLS = nil },
		"uncovered-sni":       func(p *Profile) { p.ServerNames = []string{"not-covered.example.org"} },
		"target":              func(p *Profile) { p.RealityTarget = "one.example.org:443" },
		"private-reality-key": func(p *Profile) { p.RealityPrivateKey = testProfile(1).RealityPrivateKey },
		"shortid":             func(p *Profile) { p.ShortIDs = []string{""} },
		"reality-option":      func(p *Profile) { p.Options.RealityShow = true },
		"spiderx":             func(p *Profile) { p.Options.Client.SpiderX = "/" },
		"fallback":            func(p *Profile) { p.Options.Fallbacks = []Fallback{{Dest: "8080"}} },
		"cross-transport":     func(p *Profile) { p.Options.WebSocket = &WebSocketOptions{Path: "/wrong"} },
	}
	for name, edit := range tests {
		t.Run(name, func(t *testing.T) {
			p := tlsFixtureProfile(t, "tcp", material)
			edit(&p)
			if err := ValidateProfile(p); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid TLS profile accepted", err)
			}
		})
	}
	p := testProfile(1)
	p.TLS = material
	if ValidateProfile(p) == nil {
		t.Fatal("REALITY accepted TLS key")
	}
	for _, transport := range []string{"ws", "httpupgrade"} {
		p = testProfile(1)
		p.Transport = transport
		if ValidateProfile(p) == nil {
			t.Fatal("REALITY accepted unsupported HTTP transport")
		}
	}
	p = testProfile(1)
	before, err := RenderAssigned("legacy", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}})
	if err != nil {
		t.Fatal(err)
	}
	p.Security = "reality"
	after, err := RenderAssigned("legacy", strings.Repeat("a", 32), "127.0.0.1", []Assignment{{Profile: p, DirectPort: 10443}})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("explicit REALITY changed legacy candidate", err)
	}
}

func TestTLSSettingsCannotHideInsideRealityAccountingCandidate(t *testing.T) {
	for _, key := range []string{"tlsSettings", "wsSettings", "httpupgradeSettings"} {
		t.Run(key, func(t *testing.T) {
			candidate := labSubscriptionCandidate(t)
			editLabSubscriptionConfig(t, &candidate, func(doc map[string]any) {
				stream := doc["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)
				stream[key] = map[string]any{}
			})
			if _, _, _, err := UsageBootstrap(candidate); !errors.Is(err, ErrInvalid) {
				t.Fatal("accounting silently dropped unsupported transport/security settings")
			}
			if _, err := LabSubscriptionRoutes(candidate, uuidA, map[uint64]string{11: "first", 22: "second"}); !errors.Is(err, ErrInvalid) {
				t.Fatal("LAB export silently dropped unsupported transport/security settings")
			}
		})
	}
}
