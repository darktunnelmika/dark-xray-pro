package subscriptioncodec

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestTLSURIsCarryTransportSettingsWithoutRealityCredentials(t *testing.T) {
	for _, transport := range []string{"tcp", "grpc", "xhttp", "ws", "httpupgrade"} {
		t.Run(transport, func(t *testing.T) {
			r := codecRoute()
			r.Security, r.Transport, r.PublicKey, r.ShortID = "tls", transport, "", ""
			r.Client = &ClientOptions{Fingerprint: "firefox"}
			switch transport {
			case "grpc":
				r.GRPCService, r.GRPCAuthority, r.GRPCMultiMode = "tls-grpc", "transport.example.org:443", true
			case "xhttp":
				r.XHTTP = &XHTTPOptions{Path: "/xhttp", Host: "transport.example.org", Mode: "packet-up", XPaddingBytes: "256-512"}
			case "ws":
				r.WebSocket = &WebSocketOptions{Path: "/Case+Path", Host: "transport.example.org"}
			case "httpupgrade":
				r.HTTPUpgrade = &HTTPUpgradeOptions{Path: "/Case+Path", Host: "transport.example.org"}
			}
			raw, err := Render([]Route{r}, FormatRaw)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := Render([]Route{r}, FormatBase64)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := base64.StdEncoding.DecodeString(string(encoded))
			if err != nil || !bytes.Equal(decoded, raw) {
				t.Fatal("TLS base64 differs")
			}
			u, err := url.Parse(strings.TrimSuffix(string(raw), "\n"))
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			if q.Get("security") != "tls" || q.Get("type") != transport || q.Get("fp") != "firefox" || q.Get("sni") != r.ServerName || q.Get("encryption") != "none" {
				t.Fatal("TLS link lost required fields", q)
			}
			for _, key := range []string{"pbk", "sid", "spx", "allowInsecure", "certificate", "key"} {
				if q.Has(key) {
					t.Fatal("TLS URI contains server secret or REALITY field", key)
				}
			}
			if (transport == "tcp") != (q.Get("flow") == "xtls-rprx-vision") {
				t.Fatal("flow was applied to wrong transport")
			}
			switch transport {
			case "tcp":
				if q.Get("alpn") != "h2,http/1.1" {
					t.Fatal("wrong TCP ALPN")
				}
			case "grpc":
				if q.Get("alpn") != "h2" || q.Get("mode") != "multi" || q.Get("authority") != r.GRPCAuthority || q.Get("serviceName") != r.GRPCService {
					t.Fatal("gRPC fields changed")
				}
			case "xhttp":
				if q.Get("alpn") != "h2" || q.Get("mode") != "packet-up" || q.Get("path") != "/xhttp" || q.Get("extra") != `{"xPaddingBytes":"256-512"}` {
					t.Fatal("XHTTP fields changed")
				}
			default:
				if q.Get("alpn") != "http/1.1" || q.Get("path") != "/Case+Path" || q.Get("host") != "transport.example.org" {
					t.Fatal("HTTP transport fields changed")
				}
			}
		})
	}
}

func TestTLSCodecRejectsMixedSecurityAndTransport(t *testing.T) {
	base := codecRoute()
	base.Security, base.PublicKey, base.ShortID = "tls", "", ""
	for name, edit := range map[string]func(*Route){
		"unknown-security":  func(r *Route) { r.Security = "none" },
		"public-key":        func(r *Route) { r.PublicKey = codecRoute().PublicKey },
		"shortid":           func(r *Route) { r.ShortID = "aa" },
		"spiderx":           func(r *Route) { r.Client = &ClientOptions{SpiderX: "/"} },
		"wrong-websocket":   func(r *Route) { r.WebSocket = &WebSocketOptions{Path: "/"} },
		"wrong-httpupgrade": func(r *Route) { r.HTTPUpgrade = &HTTPUpgradeOptions{Path: "/"} },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			edit(&r)
			if _, err := Render([]Route{r}, FormatRaw); !errors.Is(err, ErrInvalid) {
				t.Fatal("mixed TLS route accepted")
			}
		})
	}
	for _, transport := range []string{"ws", "httpupgrade"} {
		r := codecRoute()
		r.Transport = transport
		if _, err := Render([]Route{r}, FormatRaw); !errors.Is(err, ErrInvalid) {
			t.Fatal("REALITY accepted unsupported transport")
		}
	}
	legacy := codecRoute()
	before, _ := Render([]Route{legacy}, FormatRaw)
	legacy.Security = "reality"
	after, err := Render([]Route{legacy}, FormatRaw)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("REALITY default URI changed")
	}
}

func TestHTTPTransportOptionsHaveOwnedBoundedLiteralPaths(t *testing.T) {
	ws := &WebSocketOptions{Path: "/Case+Path", Host: "Edge.example.org"}
	normalized, err := NormalizeWebSocketOptions(ws)
	if err != nil || normalized == ws || *normalized != *ws {
		t.Fatal("WebSocket normalization failed", err)
	}
	normalized.Path = "/changed"
	if ws.Path != "/Case+Path" {
		t.Fatal("aliased WebSocket input")
	}
	upgrade := &HTTPUpgradeOptions{Path: "/Case+Path", Host: "192.0.2.1"}
	u, err := NormalizeHTTPUpgradeOptions(upgrade)
	if err != nil || u == upgrade || *u != *upgrade {
		t.Fatal("HTTPUpgrade normalization failed", err)
	}
	if v, e := NormalizeWebSocketOptions(&WebSocketOptions{}); e != nil || v != nil {
		t.Fatal("empty options not canonical")
	}
	if v, e := NormalizeHTTPUpgradeOptions(&HTTPUpgradeOptions{}); e != nil || v != nil {
		t.Fatal("empty options not canonical")
	}
	for _, path := range []string{"relative", "//host/path", "/a%20b", "/a?ed=2560", "/a#frag", "/a\\b", "/a b", "/خط", "/a\x00", strings.Repeat("/", MaxConnectionPathBytes+1)} {
		if _, err := NormalizeWebSocketOptions(&WebSocketOptions{Path: path}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid WebSocket path accepted %q", path)
		}
		if _, err := NormalizeHTTPUpgradeOptions(&HTTPUpgradeOptions{Path: path}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid HTTPUpgrade path accepted %q", path)
		}
	}
	for _, host := range []string{"domain.example:443", "[::1]", "https://domain.example", "bad host", "host\r\nInjected: yes"} {
		if _, err := NormalizeWebSocketOptions(&WebSocketOptions{Host: host}); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid paired Host accepted")
		}
	}
}
