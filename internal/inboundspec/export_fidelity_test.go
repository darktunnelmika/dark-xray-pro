package inboundspec

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVMessShareLinkPreservesTransportConventions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings map[string]any
		want     map[string]any
	}{
		{"grpc", map[string]any{"serviceName": "my-service", "multiMode": true}, map[string]any{"type": "multi", "path": "my-service"}},
		{"xhttp", map[string]any{"mode": "stream-one", "path": "/upload"}, map[string]any{"type": "stream-one", "path": "/upload"}},
		{"kcp", map[string]any{"mtu": 1280, "tti": 30}, map[string]any{"mtu": float64(1280), "tti": float64(30)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, "vmess")
			s.Network = tc.name
			s.Stream[tc.name+"Settings"] = tc.settings
			link, e := ExportClient(s, "uri", testUsers()[0], Endpoint{Address: "example.test", Port: 443})
			if e != nil {
				t.Fatal(e)
			}
			raw, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(string(link), "vmess://"))
			if e != nil {
				t.Fatal(e)
			}
			var got map[string]any
			if e = json.Unmarshal(raw, &got); e != nil {
				t.Fatal(e)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("VMess %s lost %s: got %v, want %v", tc.name, key, got[key], want)
				}
			}
		})
	}
}

func TestURICannotSilentlyDropClientConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, protocol string
		change         func(*Spec)
		jsonContains   string
	}{
		{"vless_mux", "vless", func(s *Spec) { s.Client["mux"] = map[string]any{"enabled": true, "concurrency": 8} }, `"mux"`},
		{"vmess_mux", "vmess", func(s *Spec) { s.Client["mux"] = map[string]any{"enabled": true, "concurrency": 8} }, `"mux"`},
		{"http_mux", "http", func(s *Spec) { s.Client["mux"] = map[string]any{"enabled": true} }, `"mux"`},
		{"wg_server_dns_string", "wireguard", func(s *Spec) { s.Settings["dns"] = "192.0.2.53" }, `"remoteDNS":["192.0.2.53"]`},
		{"wg_server_dns_list", "wireguard", func(s *Spec) { s.Settings["dns"] = []any{"192.0.2.53"} }, `"remoteDNS":["192.0.2.53"]`},
		{"wg_allowed_routes", "wireguard", func(s *Spec) { s.Client["allowedIPs"] = []any{"10.0.0.0/8"} }, `"allowedIPs":["10.0.0.0/8"]`},
		{"wg_reserved_bytes", "wireguard", func(s *Spec) { s.Settings["reserved"] = []any{1, 2, 3} }, `"reserved":[1,2,3]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, tc.protocol)
			tc.change(&s)
			ep := Endpoint{Address: "example.test", Port: 443}
			if _, e := ExportClient(s, "uri", testUsers()[0], ep); !errors.Is(e, ErrUnsupportedFormat) {
				t.Fatalf("lossy link was accepted or failed with the wrong error: %v", e)
			}
			b, e := ExportClient(s, "xray", testUsers()[0], ep)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Contains(b, []byte(tc.jsonContains)) {
				t.Fatal("JSON alternative also lost the client setting")
			}
		})
	}
	// An explicitly disabled mux has no effect in the pinned engine.
	s := fixture(t, "vless")
	s.Client["mux"] = map[string]any{"enabled": false, "concurrency": 8}
	if _, e := ExportClient(s, "uri", testUsers()[0], Endpoint{Address: "example.test", Port: 443}); e != nil {
		t.Fatal(e)
	}
}

func TestNativeWireGuardConfigRejectsXrayReservedBytes(t *testing.T) {
	s := fixture(t, "wireguard")
	s.Settings["reserved"] = []any{1, 0, 0}
	ep := Endpoint{Address: "example.test", Port: 443}
	for _, format := range []string{"wireguard", "conf"} {
		if _, e := ExportClient(s, format, testUsers()[0], ep); !errors.Is(e, ErrUnsupportedFormat) {
			t.Fatalf("%s silently dropped reserved bytes: %v", format, e)
		}
	}
	s.Settings["reserved"] = []any{0, 0, 0}
	if _, e := ExportClient(s, "conf", testUsers()[0], ep); e != nil {
		t.Fatal(e)
	}
}

func TestSingBoxRejectsUnrepresentedTransportTuning(t *testing.T) {
	for _, tc := range []struct {
		network, key string
		value, zero  any
	}{
		{"grpc", "idle_timeout", 60, 0},
		{"grpc", "health_check_timeout", 30, 0},
		{"grpc", "initial_windows_size", 65536, 0},
		{"grpc", "permit_without_stream", true, false},
		{"grpc", "user_agent", "fixture-client", ""},
		{"ws", "heartbeatPeriod", 30, 0},
	} {
		t.Run(tc.network+"_"+tc.key, func(t *testing.T) {
			s := fixture(t, "vless")
			s.Network = tc.network
			transport := map[string]any{tc.key: tc.value}
			s.Stream[tc.network+"Settings"] = transport
			ep := Endpoint{Address: "example.test", Port: 443}
			if _, e := ExportClient(s, "sing-box", testUsers()[0], ep); !errors.Is(e, ErrUnsupportedFormat) {
				t.Fatalf("lossy sing-box conversion was accepted: %v", e)
			}
			xray, e := ExportClient(s, "xray", testUsers()[0], ep)
			if e != nil {
				t.Fatal(e)
			}
			value, _ := json.Marshal(tc.value)
			if !bytes.Contains(xray, []byte(`"`+tc.key+`":`+string(value))) {
				t.Fatal("native JSON alternative lost the selected setting")
			}
			transport[tc.key] = tc.zero
			if _, e = ExportClient(s, "sing-box", testUsers()[0], ep); e != nil {
				t.Fatalf("an empty or disabled option blocked the compatible baseline: %v", e)
			}
		})
	}
}

func TestTUICURIRejectsUnrepresentedTLSVerification(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"pinnedPeerCertSha256", []any{strings.Repeat("01", 32)}},
		{"echConfigList", "AA=="},
		{"verifyPeerCertByName", "example.test"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			s := fixture(t, "tuic")
			s.Client[tc.key] = tc.value
			ep := Endpoint{Address: "example.test", Port: 443}
			for _, format := range []string{"uri", "sing-box", "json"} {
				if _, e := ExportClient(s, format, testUsers()[0], ep); !errors.Is(e, ErrUnsupportedFormat) {
					t.Fatalf("%s silently dropped TUIC %s: %v", format, tc.key, e)
				}
			}
			delete(s.Client, tc.key)
			if _, e := ExportClient(s, "uri", testUsers()[0], ep); e != nil {
				t.Fatalf("compatible TUIC baseline was rejected: %v", e)
			}
		})
	}
}
