package inboundspec

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testTLS(t *testing.T) map[string]any {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	c := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.test"}, DNSNames: []string{"example.test"}, NotBefore: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, c, c, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	pk, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	priv := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}))
	return map[string]any{"serverName": "example.test", "certificates": []any{map[string]any{"certificate": asStrings(strings.Split(strings.TrimSpace(cert), "\n")), "key": asStrings(strings.Split(strings.TrimSpace(priv), "\n"))}}}
}
func fixture(t *testing.T, protocol string) Spec {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	s := Spec{Version: 1, Protocol: protocol, Network: "tcp", Security: "none", Settings: map[string]any{}, Stream: map[string]any{}, Client: map[string]any{}}
	switch protocol {
	case "vless":
		s.Settings["decryption"] = "none"
	case "trojan":
		s.Security = "tls"
		s.TLS = testTLS(t)
	case "shadowsocks":
		s.Settings = map[string]any{"method": "2022-blake3-aes-256-gcm", "password": base64.StdEncoding.EncodeToString(key)}
	case "wireguard":
		s.Network = protocol
		s.Settings["secretKey"] = base64.StdEncoding.EncodeToString(key)
	case "hysteria", "masque", "tuic":
		s.Network = protocol
		s.Security = "tls"
		s.TLS = testTLS(t)
	case "mtproto":
		s.Network = protocol
		s.Settings = map[string]any{"fakeTlsDomain": "example.test", "secret": "synthetic-fixture-only-do-not-deploy"}
	case "amneziawg":
		s.Network = protocol
		s.Settings = map[string]any{"server": map[string]any{"privateKey": base64.StdEncoding.EncodeToString(key), "subnetIp": "10.8.0.0", "subnetCidr": 16, "jc": 5, "jmin": 10, "jmax": 50, "s1": 30, "s2": 45, "s3": 10, "s4": 5, "h1": "1", "h2": "2", "h3": "3", "h4": "4"}}
	case "tun":
		s.Network = protocol
		s.Settings = map[string]any{"name": "protest0", "mtu": 1500, "gateway": []any{"10.90.0.1/24"}, "dns": []any{"1.1.1.1"}}
	case "tunnel":
		s.Settings = map[string]any{"rewriteAddress": "127.0.0.1", "rewritePort": 443}
	}
	return s
}
func testUsers() []User {
	return []User{{ID: 1, UUID: "11111111-1111-4111-8111-111111111111", Email: "one@example.test"}, {ID: 2, UUID: "22222222-2222-4222-8222-222222222222", Email: "two@example.test"}}
}
func realityFixture(t *testing.T, network string) Spec {
	s := fixture(t, "vless")
	s.Network = network
	s.Security = "reality"
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 10)
	}
	s.Reality = map[string]any{"privateKey": base64.RawURLEncoding.EncodeToString(key), "target": "example.test:443", "serverNames": []any{"example.test"}, "shortIds": []any{"1122334455667788"}}
	s.Client["fingerprint"] = "chrome"
	return s
}

func TestEveryProtocolRendersAndExportsWithoutServerKeys(t *testing.T) {
	if len(Catalog()) != 14 {
		t.Fatal("catalog lost protocol")
	}
	for _, info := range Catalog() {
		t.Run(info.Protocol, func(t *testing.T) {
			s := fixture(t, info.Protocol)
			n, e := Normalize(s)
			if e != nil {
				t.Fatal(e)
			}
			users := testUsers()
			if info.ListenerOnly {
				users = nil
			}
			r, e := Render(n, "127.0.0.1", 19001, "fixture", users)
			if e != nil {
				t.Fatal(e)
			}
			if r.Engine != info.Engine {
				t.Fatal("engine mismatch")
			}
			if info.Engine != "xray" {
				if r.Inbound != nil || r.Config == nil || r.Manifest["adapter"] == nil {
					t.Fatal("external engine masquerades as Xray")
				}
			} else if r.Inbound["protocol"] != info.Protocol {
				t.Fatal("runtime protocol changed")
			}
			if info.ListenerOnly {
				if _, e = ExportClient(n, "json", testUsers()[0], Endpoint{Address: "example.test", Port: 19001}); !errors.Is(e, ErrUnsupportedFormat) {
					t.Fatal("listener exported an invented proxy client")
				}
				return
			}
			b, e := ExportClient(n, "json", users[0], Endpoint{Address: "example.test", Port: 19001, Name: "fixture"})
			if e != nil {
				t.Fatal(e)
			}
			if !json.Valid(b) {
				t.Fatal("invalid JSON export")
			}
			if bytes.Contains(b, []byte(users[1].UUID)) || bytes.Contains(b, []byte(users[1].Email)) {
				t.Fatal("export exposed another account")
			}
			for _, needle := range []string{str(n.Reality, "privateKey"), str(n.Reality, "mldsa65Seed"), wireguardServerKey(n), str(obj(n.Settings, "server"), "private_key"), str(n.TLS, "echServerKeys")} {
				if needle != "" && bytes.Contains(b, []byte(needle)) {
					t.Fatal("client export leaked server key")
				}
			}
			if bytes.Contains(b, []byte("BEGIN PRIVATE KEY")) {
				t.Fatal("client export leaked TLS key")
			}
		})
	}
}

func TestPortableProtocolFixtures(t *testing.T) {
	raw, e := os.ReadFile("testdata/protocols.json")
	if e != nil {
		t.Fatal(e)
	}
	var specs []Spec
	if e = json.Unmarshal(raw, &specs); e != nil {
		t.Fatal(e)
	}
	if len(specs) != 14 {
		t.Fatal("expected fourteen fixtures")
	}
	seen := map[string]bool{}
	for _, s := range specs {
		if seen[s.Protocol] {
			t.Fatal("duplicate fixture")
		}
		seen[s.Protocol] = true
		n, e := Normalize(s)
		if e != nil {
			t.Fatalf("%s: %v", s.Protocol, e)
		}
		again, e := Normalize(n)
		if e != nil {
			t.Fatal(e)
		}
		a, _ := json.Marshal(n)
		b, _ := json.Marshal(again)
		if !bytes.Equal(a, b) {
			t.Fatalf("%s normalization is not idempotent", s.Protocol)
		}
	}
}

func TestRedactionPreservesOmittedSecretsAndHonorsClears(t *testing.T) {
	s := realityFixture(t, "tcp")
	n, e := Normalize(s)
	if e != nil {
		t.Fatal(e)
	}
	pub := Public(n)
	b, _ := json.Marshal(pub)
	if bytes.Contains(b, []byte(str(n.Reality, "privateKey"))) {
		t.Fatal("public private key")
	}
	if !has(pub.SecretPaths, "reality.privateKey") {
		t.Fatal("missing presence marker")
	}
	merged, e := MergeSecrets(n, pub.Spec)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Normalize(merged); e != nil {
		t.Fatal(e)
	}
	if str(merged.Reality, "privateKey") != str(n.Reality, "privateKey") {
		t.Fatal("omitted secret changed")
	}
	pub.Reality["privateKey"] = ""
	cleared, e := MergeSecrets(n, pub.Spec)
	if e != nil {
		t.Fatal(e)
	}
	if str(cleared.Reality, "privateKey") != "" {
		t.Fatal("explicit clear resurrected")
	}
	if _, e = Normalize(cleared); e == nil {
		t.Fatal("cleared required key accepted")
	}
	tlsSpec := fixture(t, "trojan")
	tlsN, e := Normalize(tlsSpec)
	if e != nil {
		t.Fatal(e)
	}
	tlsPub := Public(tlsN)
	if !has(tlsPub.SecretPaths, "tls.certificates.0.certificate") || !has(tlsPub.SecretPaths, "tls.certificates.0.key") {
		t.Fatal("certificate material not write-only")
	}
	tlsMerged, e := MergeSecrets(tlsN, tlsPub.Spec)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Normalize(tlsMerged); e != nil {
		t.Fatal(e)
	}
	tlsPub.TLS["certificates"] = []any{}
	tlsMerged, e = MergeSecrets(tlsN, tlsPub.Spec)
	if e != nil {
		t.Fatal(e)
	}
	if len(list(tlsMerged.TLS["certificates"])) != 0 {
		t.Fatal("removed certificate reappeared")
	}
	if len(list(tlsN.TLS["certificates"])) != 1 {
		t.Fatal("public/merge mutated stored profile")
	}
}

func TestStrictSchemaAndCapabilityValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Spec)
	}{
		{"owned_clients", func(s *Spec) { s.Settings["clients"] = []any{map[string]any{"id": "attacker"}} }},
		{"unknown_settings", func(s *Spec) { s.Settings["opaquePlugin"] = map[string]any{"command": "ignored"} }},
		{"inactive_transport", func(s *Spec) { s.Stream["wsSettings"] = map[string]any{"path": "/"} }},
		{"reality_ws", func(s *Spec) { s.Network = "ws"; s.Security = "reality" }},
		{"typed_range", func(s *Spec) { s.Stream["sockopt"] = map[string]any{"tcpUserTimeout": 1.5} }},
		{"unknown_nested", func(s *Spec) {
			s.Stream["sockopt"] = map[string]any{"customSockopt": []any{map[string]any{"type": "int", "level": "6", "opt": "1", "value": "1", "command": "bad"}}}
		}},
		{"unknown_root", func(s *Spec) { s.Version = 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, "vless")
			tc.change(&s)
			if _, e := Normalize(s); !errors.Is(e, ErrInvalid) {
				t.Fatal("invalid spec accepted")
			}
		})
	}
	if _, e := Decode([]byte(`{"version":1,"protocol":"vmess","network":"tcp","security":"none","unrecognized":true}`)); e == nil {
		t.Fatal("unknown top-level field accepted")
	}
	s := fixture(t, "vless")
	s.Network = "RAW"
	n, e := Normalize(s)
	if e != nil || n.Network != "tcp" {
		t.Fatal("RAW alias")
	}
}

func TestUserCredentialsAgreeWithServerAndStayIndependent(t *testing.T) {
	s, e := Normalize(fixture(t, "wireguard"))
	if e != nil {
		t.Fatal(e)
	}
	users := testUsers()
	r, e := Render(s, "127.0.0.1", 19001, "wg", users)
	if e != nil {
		t.Fatal(e)
	}
	b, e := ExportClient(s, "xray", users[0], Endpoint{Address: "example.test", Port: 19001})
	if e != nil {
		t.Fatal(e)
	}
	var cfg map[string]any
	if e = json.Unmarshal(b, &cfg); e != nil {
		t.Fatal(e)
	}
	client := cfg["outbounds"].([]any)[0].(map[string]any)
	private := str(obj(client, "settings"), "secretKey")
	derived, pub, e := DeriveWireGuardIdentity(s, users[0])
	if e != nil || private != derived {
		t.Fatal("peer private key changed")
	}
	peers := obj(r.Inbound, "settings")["peers"].([]any)
	found := false
	for _, v := range peers {
		p := v.(map[string]any)
		if str(p, "email") == users[0].Email && str(p, "publicKey") == pub {
			found = true
		}
	}
	if !found {
		t.Fatal("client key does not match server peer")
	}
	other, _, _ := DeriveWireGuardIdentity(s, users[1])
	if other == private {
		t.Fatal("two users share private key")
	}
	if _, e = Render(s, "127.0.0.1", 19001, "wg", nil); e == nil {
		t.Fatal("unbound listener allowed")
	}
	collision := append([]User(nil), users...)
	collision[1].ID = collision[0].ID
	if _, e = Render(s, "127.0.0.1", 19001, "wg", collision); e == nil {
		t.Fatal("address collision silently accepted")
	}
}

func TestXDriveAndClientOnlyTransportOptions(t *testing.T) {
	s := fixture(t, "vless")
	s.Network = "xdrive"
	s.Stream = map[string]any{"address": "example.test", "port": 443, "xdriveSettings": map[string]any{"service": "Google Drive", "remoteFolder": "folder", "secrets": []any{"client-id", "client-secret", "refresh-token"}, "concurrency": 4}}
	n, e := Normalize(s)
	if e != nil {
		t.Fatal(e)
	}
	p := Public(n)
	b, _ := json.Marshal(p)
	if bytes.Contains(b, []byte("client-secret")) || !has(p.SecretPaths, "stream.xdriveSettings.secrets") {
		t.Fatal("storage credentials exposed")
	}
	if _, e = ExportClient(n, "uri", testUsers()[0], Endpoint{Address: "example.test", Port: 443}); !errors.Is(e, ErrUnsupportedFormat) {
		t.Fatal("invented XDrive URI")
	}
	if _, e = ExportClient(n, "xray", testUsers()[0], Endpoint{Address: "example.test", Port: 443}); e != nil {
		t.Fatal(e)
	}
	s = fixture(t, "vless")
	s.Network = "xhttp"
	s.Stream = map[string]any{"xhttpSettings": map[string]any{"path": "/edge", "mode": "packet-up", "xmux": map[string]any{"maxConnections": "3-5"}, "serverMaxHeaderBytes": 1024}}
	n, e = Normalize(s)
	if e != nil {
		t.Fatal(e)
	}
	r, e := Render(n, "127.0.0.1", 19001, "xhttp", testUsers())
	if e != nil {
		t.Fatal(e)
	}
	server := obj(obj(r.Inbound, "streamSettings"), "xhttpSettings")
	if _, ok := server["xmux"]; ok {
		t.Fatal("client pool settings leaked into listener")
	}
	b, e = ExportClient(n, "xray", testUsers()[0], Endpoint{Address: "example.test", Port: 19001})
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(b, []byte("maxConnections")) || bytes.Contains(b, []byte("serverMaxHeaderBytes")) {
		t.Fatal("client/server XHTTP fields not separated")
	}
}

// Engine acceptance is opt-in and parses configurations only. -test never
// opens listeners or dials configured destinations. CI pins the executable.
func TestPinnedCoreAcceptsNativeMatrix(t *testing.T) {
	bin := os.Getenv("DXP_INBOUNDSPEC_XRAY")
	expected := os.Getenv("DXP_INBOUNDSPEC_XRAY_SHA256")
	if bin == "" && expected == "" {
		t.Skip("set DXP_INBOUNDSPEC_XRAY and DXP_INBOUNDSPEC_XRAY_SHA256 for pinned core acceptance")
	}
	if bin == "" || len(expected) != 64 {
		t.Fatal("both core path and SHA256 are required")
	}
	f, e := os.Open(bin)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.New()
	_, e = io.Copy(h, f)
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		t.Fatal("core binary could not be hashed")
	}
	if fmt.Sprintf("%x", h.Sum(nil)) != expected {
		t.Fatal("pinned core SHA256 mismatch")
	}
	var cases []struct {
		name string
		s    Spec
	}
	for _, p := range Catalog() {
		if p.Engine == "xray" {
			cases = append(cases, struct {
				name string
				s    Spec
			}{p.Protocol, fixture(t, p.Protocol)})
		}
	}
	for _, network := range genericNetworks {
		s := fixture(t, "vless")
		s.Network = network
		if network == "xdrive" {
			s.Stream = map[string]any{"xdriveSettings": map[string]any{"service": "local", "remoteFolder": t.TempDir()}}
		}
		cases = append(cases, struct {
			name string
			s    Spec
		}{"transport_" + network, s})
	}
	for _, network := range []string{"tcp", "grpc", "xhttp"} {
		cases = append(cases, struct {
			name string
			s    Spec
		}{"reality_" + network, realityFixture(t, network)})
	}
	for _, network := range []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"} {
		s := fixture(t, "vless")
		s.Network = network
		s.Security = "tls"
		s.TLS = testTLS(t)
		cases = append(cases, struct {
			name string
			s    Spec
		}{"tls_" + network, s})
	}
	{
		s := fixture(t, "trojan")
		s.TLS["curvePreferences"] = []any{"X25519", "CurveP256"}
		s.TLS["enableSessionResumption"] = true
		cert := list(s.TLS["certificates"])[0].(map[string]any)
		block, _ := pem.Decode([]byte(strings.Join(texts(cert["certificate"]), "\n")))
		pin := sha256.Sum256(block.Bytes)
		s.Client["pinnedPeerCertSha256"] = []any{base64.StdEncoding.EncodeToString(pin[:])}
		s.Client["verifyPeerCertByName"] = "example.test"
		s.Client["fingerprint"] = "chrome"
		cases = append(cases, struct {
			name string
			s    Spec
		}{"tls_certificate_pinning", s})
	}
	{
		s := fixture(t, "vless")
		s.Network = "grpc"
		s.Stream = map[string]any{"grpcSettings": map[string]any{"serviceName": "GunService", "authority": "example.test", "multiMode": true, "idle_timeout": 60, "health_check_timeout": 30, "permit_without_stream": true, "initial_windows_size": 65536, "user_agent": "pro-fixture"}, "sockopt": map[string]any{"tcpFastOpen": true, "tcpMptcp": true, "tcpcongestion": "cubic", "tcpUserTimeout": 10000, "tcpKeepAliveIdle": 30, "tcpKeepAliveInterval": 10, "customSockopt": []any{map[string]any{"system": "linux", "network": "tcp", "type": "int", "level": "1", "opt": "2", "value": 1}}}}
		cases = append(cases, struct {
			name string
			s    Spec
		}{"grpc_and_sockopt_tuning", s})
	}
	{
		s := fixture(t, "vless")
		s.Stream = map[string]any{"finalmask": map[string]any{"tcp": []any{map[string]any{"type": "fragment", "settings": map[string]any{"packets": "1-3", "lengths": []any{"100-200"}, "delays": []any{"1-2"}, "maxSplit": "10-20"}}, map[string]any{"type": "header-custom", "settings": map[string]any{"clients": []any{[]any{map[string]any{"type": "hex", "packet": "0102"}}}, "servers": []any{[]any{map[string]any{"type": "str", "packet": "ok"}}}}}}}}
		cases = append(cases, struct {
			name string
			s    Spec
		}{"finalmask_tcp", s})
	}
	{
		s := fixture(t, "hysteria")
		s.Stream = map[string]any{"finalmask": map[string]any{"udp": []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": "synthetic-obfuscator", "packetSize": "512-1200"}}}, "quicParams": map[string]any{"congestion": "bbr", "bbrProfile": "conservative", "maxIdleTimeout": 30, "keepAlivePeriod": 10, "udpHop": map[string]any{"ports": "20000-20100", "interval": "5-10"}}}}
		cases = append(cases, struct {
			name string
			s    Spec
		}{"hysteria_gecko_quic_hop", s})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, _ := protocolInfo(tc.s.Protocol)
			users := testUsers()
			if info.ListenerOnly {
				users = nil
			}
			r, e := Render(tc.s, "127.0.0.1", 19001, "test", users)
			if e != nil {
				t.Fatal(e)
			}
			b, e := json.Marshal(map[string]any{"inbounds": []any{r.Inbound}, "outbounds": []any{map[string]any{"protocol": "freedom", "tag": "direct"}}})
			if e != nil {
				t.Fatal(e)
			}
			f := filepath.Join(t.TempDir(), "server.json")
			if e = os.WriteFile(f, b, 0600); e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command(bin, "run", "-test", "-config", f)
			output, e := cmd.CombinedOutput()
			if e != nil {
				t.Errorf("pinned core rejected server: %v; %s", e, safeCoreError(output, tc.s))
				return
			}
			if !info.ListenerOnly {
				b, e = ExportClient(tc.s, "xray", testUsers()[0], Endpoint{Address: "example.test", Port: 19001})
				if e != nil {
					t.Fatal(e)
				}
				f = filepath.Join(t.TempDir(), "client.json")
				if e = os.WriteFile(f, b, 0600); e != nil {
					t.Fatal(e)
				}
				output, e = exec.Command(bin, "run", "-test", "-config", f).CombinedOutput()
				if e != nil {
					t.Errorf("pinned core rejected client: %v; %s", e, safeCoreError(output, tc.s))
				}
			}
		})
	}
}
func safeCoreError(output []byte, s Spec) string {
	v := string(output)
	for _, root := range specRoots(&s) {
		var walk func(any)
		walk = func(x any) {
			switch m := x.(type) {
			case map[string]any:
				for k, z := range m {
					if isSecretField("", k) {
						if text, ok := z.(string); ok && text != "" {
							v = strings.ReplaceAll(v, text, "[redacted]")
						}
					}
					walk(z)
				}
			case []any:
				for _, z := range m {
					walk(z)
				}
			}
		}
		walk(root)
	}
	if len(v) > 1600 {
		v = v[:1600]
	}
	return v
}
