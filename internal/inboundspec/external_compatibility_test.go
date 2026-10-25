package inboundspec

import (
	"errors"
	"strings"
	"testing"
)

func TestTUNNameFitsManagedLinuxInterface(t *testing.T) {
	for _, name := range []string{strings.Repeat("a", 16), strings.Repeat("a", 64), ".reserved", "رابط"} {
		s := fixture(t, "tun")
		s.Settings["name"] = name
		_, err := Normalize(s)
		var field *FieldError
		if !errors.As(err, &field) || field.Path != "settings.name" || field.Code != "interface_name_invalid" {
			t.Fatalf("invalid kernel interface name was accepted: %q %v", name, err)
		}
	}
	s := fixture(t, "tun")
	s.Settings["name"] = "tun-" + strings.Repeat("a", 11)
	if _, err := Normalize(s); err != nil {
		t.Fatalf("15-byte interface name rejected: %v", err)
	}
}

func TestTUICOnlyAdmitsImplementedTLS13Settings(t *testing.T) {
	for _, tc := range []struct{ field, value, code string }{
		{"minVersion", "1.2", "tuic_requires_tls13"},
		{"maxVersion", "1.2", "tuic_requires_tls13"},
		{"cipherSuites", "TLS_AES_128_GCM_SHA256", "tuic_cipher_suites_unsupported"},
		{"cipherSuites", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "tuic_cipher_suites_unsupported"},
	} {
		t.Run(tc.field+"/"+tc.value, func(t *testing.T) {
			s := fixture(t, "tuic")
			s.TLS[tc.field] = tc.value
			_, err := Normalize(s)
			var field *FieldError
			if !errors.As(err, &field) || field.Path != "tls."+tc.field || field.Code != tc.code {
				t.Fatalf("unimplemented TLS setting was not identified: %v", err)
			}
		})
	}
	for _, explicit := range []bool{false, true} {
		s := fixture(t, "tuic")
		if explicit {
			s.TLS["minVersion"], s.TLS["maxVersion"] = "1.3", "1.3"
		}
		n, err := Normalize(s)
		if err != nil || str(n.TLS, "minVersion") != "1.3" || str(n.TLS, "maxVersion") != "1.3" {
			t.Fatalf("valid TUIC TLS1.3 rejected: %v", err)
		}
	}
}

func TestAmneziaWGRequiresOwnedHostAliasesAndSupportedTunnelBounds(t *testing.T) {
	for _, tc := range []struct {
		name, field, code string
		value             any
	}{
		{"external interface", "externalInterface", "amneziawg_host_alias_unsupported", "eth0"},
		{"IPv6 source alias", "ipv6ExternalInterface", "amneziawg_host_alias_unsupported", "eth0"},
		{"small MTU", "mtu", "amneziawg_mtu_range", 1200},
		{"large MTU", "mtu", "amneziawg_mtu_range", 9001},
		{"small IPv6 subnet", "ipv6Subnet", "amneziawg_ipv6_prefix_too_small", "fd42::/128"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t, "amneziawg")
			server := obj(s.Settings, "server")
			server[tc.field] = tc.value
			if tc.field == "ipv6Subnet" {
				server["ipv6Enabled"] = true
			}
			_, err := Normalize(s)
			var field *FieldError
			if !errors.As(err, &field) || field.Path != "settings.server."+tc.field || field.Code != tc.code {
				t.Fatalf("unimplemented AWG setting was not identified: %v", err)
			}
		})
	}
	s := fixture(t, "amneziawg")
	server := obj(s.Settings, "server")
	server["ipv6Enabled"], server["ipv6Subnet"], server["mtu"] = true, "fd42::/64", 1280
	n, err := Normalize(s)
	if err != nil || !boolean(obj(n.Settings, "server"), "ipv6Enabled") || str(obj(n.Settings, "server"), "ipv6Subnet") != "fd42::/64" {
		t.Fatalf("supported userspace IPv6 tunnel rejected: %v", err)
	}
}

func TestTUNRejectsUnwiredHostDNSTakeoverWithoutRemovingOwnedRouting(t *testing.T) {
	s := fixture(t, "tun")
	s.Settings["autoSystemDnsToGateway"] = true
	s.Settings["autoSystemRoutingTable"] = []any{"0.0.0.0/0"}
	_, err := Normalize(s)
	var field *FieldError
	if !errors.As(err, &field) || field.Path != "settings.autoSystemDnsToGateway" || field.Code != "tun_host_dns_takeover_unsupported" {
		t.Fatalf("host resolver takeover was silently accepted: %v", err)
	}
	if !boolean(s.Settings, "autoSystemDnsToGateway") {
		t.Fatal("validation changed the submitted resolver request")
	}
	s.Settings["autoSystemDnsToGateway"] = false
	n, err := Normalize(s)
	if err != nil || len(texts(n.Settings["autoSystemRoutingTable"])) != 1 || len(texts(n.Settings["gateway"])) != 1 {
		t.Fatalf("ordinary owned TUN routing was removed: %v", err)
	}
}
