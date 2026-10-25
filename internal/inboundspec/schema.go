package inboundspec

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// These schemas describe accepted values, not arbitrary passthrough JSON.
// Unknown keys fail instead of being silently discarded by the engine.
type rule struct {
	kind     string
	min, max int64
	enums    []string
	fields   map[string]rule
	item     *rule
	choices  []rule
}

func textRule() rule                     { return rule{kind: "string", max: 65536} }
func shortText() rule                    { return rule{kind: "string", max: 4096} }
func enum(v ...string) rule              { return rule{kind: "string", max: 4096, enums: v} }
func integer(min, max int64) rule        { return rule{kind: "integer", min: min, max: max} }
func flag() rule                         { return rule{kind: "bool"} }
func array(item rule, max int64) rule    { return rule{kind: "array", max: max, item: &item} }
func object(fields map[string]rule) rule { return rule{kind: "object", fields: fields} }
func dictionary(item rule) rule          { return rule{kind: "dictionary", max: 256, item: &item} }
func oneof(rs ...rule) rule              { return rule{kind: "union", choices: rs} }
func stringList() rule                   { return array(shortText(), 256) }
func boundedRange() rule                 { return rule{kind: "range", min: 0, max: 2147483647} }
func pathJoin(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}

func check(value any, r rule, path string, depth int) error {
	if depth > 24 {
		return invalid(path, "too_deep")
	}
	switch r.kind {
	case "string":
		s, ok := value.(string)
		if !ok || !utf8.ValidString(s) || int64(len(s)) > r.max || strings.ContainsRune(s, 0) {
			return invalid(path, "string_invalid")
		}
		if len(r.enums) > 0 && !has(r.enums, s) {
			return invalid(path, "enum_invalid")
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return invalid(path, "boolean_required")
		}
	case "integer":
		n, ok := number(value)
		if !ok || n < r.min || n > r.max {
			return invalid(path, "integer_out_of_range")
		}
	case "range":
		if _, _, ok := parseRange(value, r.min, r.max); !ok {
			return invalid(path, "range_invalid")
		}
	case "array":
		a, ok := value.([]any)
		if !ok || int64(len(a)) > r.max {
			return invalid(path, "array_invalid")
		}
		for i, v := range a {
			if err := check(v, *r.item, fmt.Sprintf("%s.%d", path, i), depth+1); err != nil {
				return err
			}
		}
	case "object", "dictionary":
		m, ok := value.(map[string]any)
		if !ok {
			return invalid(path, "object_required")
		}
		if len(m) > 256 {
			return invalid(path, "too_many_fields")
		}
		for k, v := range m {
			if len(k) > 256 || strings.ContainsAny(k, "\x00\r\n") || k == "__proto__" || k == "constructor" || k == "prototype" {
				return invalid(path, "key_invalid")
			}
			if r.kind == "dictionary" {
				if err := check(v, *r.item, path+".*", depth+1); err != nil {
					return err
				}
				continue
			}
			child, found := r.fields[k]
			if !found {
				return invalid(path, "unknown_field")
			}
			if err := check(v, child, pathJoin(path, k), depth+1); err != nil {
				return err
			}
		}
	case "union":
		for _, v := range r.choices {
			if check(value, v, path, depth+1) == nil {
				return nil
			}
		}
		return invalid(path, "value_invalid")
	default:
		return invalid(path, "schema_invalid")
	}
	return nil
}

func parseRange(v any, min, max int64) (int64, int64, bool) {
	if n, ok := number(v); ok {
		return n, n, n >= min && n <= max
	}
	s, ok := v.(string)
	if !ok {
		return 0, 0, false
	}
	if s == "" {
		return 0, 0, true
	}
	parts := strings.Split(s, "-")
	if len(parts) > 2 {
		return 0, 0, false
	}
	lo, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil {
		return 0, 0, false
	}
	hi := lo
	if len(parts) == 2 {
		hi, e = strconv.ParseInt(parts[1], 10, 64)
	}
	return lo, hi, e == nil && lo >= min && hi >= lo && hi <= max
}

func checkObject(m map[string]any, fields map[string]rule, path string) error {
	return check(m, object(fields), path, 0)
}
func addFields(base map[string]rule, more map[string]rule) map[string]rule {
	out := map[string]rule{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}

var fallbackRule = object(map[string]rule{
	"name": shortText(), "alpn": shortText(), "path": shortText(), "dest": oneof(shortText(), integer(1, 65535)), "xver": integer(0, 2),
})
var sniffingRule = object(map[string]rule{"enabled": flag(), "destOverride": array(enum("http", "tls", "quic", "fakedns", "fakedns+others"), 5), "metadataOnly": flag(), "routeOnly": flag(), "domainsExcluded": stringList(), "ipsExcluded": stringList()})

var awgServerFields = map[string]rule{
	"privateKey": shortText(), "publicKey": shortText(), "subnetIp": shortText(), "subnetCidr": integer(1, 32), "mtu": integer(576, 65535),
	"primaryDns": shortText(), "secondaryDns": shortText(), "externalInterface": shortText(), "ipv6Enabled": flag(), "ipv6Subnet": shortText(), "ipv6ExternalInterface": shortText(),
	"jc": integer(0, 4294967295), "jmin": integer(0, 4294967295), "jmax": integer(0, 4294967295), "s1": integer(0, 1552), "s2": integer(0, 1608), "s3": integer(0, 1636), "s4": integer(0, 32),
	"h1": shortText(), "h2": shortText(), "h3": shortText(), "h4": shortText(), "i1": shortText(), "i2": shortText(), "i3": shortText(), "i4": shortText(), "i5": shortText(),
	"headerProtectionKey": shortText(), "contentPaddingAddition": shortText(), "rekeyAfterTime": shortText(), "rekeyTimeout": shortText(), "rejectAfterTime": shortText(), "keepaliveTimeout": shortText(), "maxHandshakeAttempts": shortText(), "randomTrailers": flag(), "disableCookies": flag(),
}
var tuicServerFields = map[string]rule{
	"certificate": textRule(), "private_key": textRule(), "congestion_control": enum("bbr", "cubic", "new_reno"), "alpn": stringList(), "udp_relay_mode": enum("native", "quic"), "zero_rtt_handshake": flag(),
	"log_level": enum("info", "warn", "error", "debug"), "max_idle_time": integer(1, 86400), "authentication_timeout": integer(1, 600), "max_udp_relay_packet_size": integer(1, 65245), "sni": shortText(),
}

var settingSchemas = map[string]map[string]rule{
	"vless":       {"decryption": textRule(), "encryption": textRule(), "fallbacks": array(fallbackRule, 64), "testseed": array(integer(1, 4294967295), 4)},
	"vmess":       {},
	"trojan":      {"fallbacks": array(fallbackRule, 64)},
	"shadowsocks": {"method": enum("2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305", "aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "chacha20-ietf-poly1305", "xchacha20-poly1305", "xchacha20-ietf-poly1305", "none", "plain"), "password": textRule(), "network": enum("tcp", "udp", "tcp,udp"), "ivCheck": flag()},
	"wireguard":   {"secretKey": shortText(), "mtu": integer(576, 65535), "dns": oneof(shortText(), stringList()), "noKernelTun": flag(), "subnetIp": shortText(), "subnetCidr": integer(1, 30), "reserved": array(integer(0, 255), 3)},
	"hysteria":    {"version": integer(2, 2)},
	"http":        {"allowTransparent": flag()},
	"mixed":       {"auth": enum("password", "noauth"), "udp": flag(), "ip": shortText()},
	"tunnel":      {"rewriteAddress": shortText(), "rewritePort": integer(1, 65535), "portMap": dictionary(shortText()), "allowedNetwork": enum("tcp", "udp", "tcp,udp"), "followRedirect": flag()},
	"tun":         {"name": shortText(), "desc": shortText(), "mtu": integer(0, 65535), "gateway": stringList(), "dns": stringList(), "userLevel": integer(0, 4294967295), "autoSystemRoutingTable": stringList(), "autoOutboundsInterface": shortText(), "autoSystemDnsToGateway": flag(), "autoSystemWfpBlockLeak": array(enum("dns", "misconfigtun"), 2)},
	"mtproto":     {"fakeTlsDomain": shortText(), "secret": shortText(), "proxyProtocolListener": flag(), "preferIp": enum("prefer-ipv6", "prefer-ipv4", "only-ipv6", "only-ipv4"), "debug": flag(), "domainFronting": object(map[string]rule{"ip": shortText(), "port": integer(1, 65535), "proxyProtocol": flag()}), "throttleMaxConnections": integer(0, 10000000), "pendingHandshakesMaxPerIp": integer(0, 1000000), "routeThroughXray": flag(), "outboundTag": shortText(), "publicIpv4": shortText(), "publicIpv6": shortText()},
	"amneziawg":   {"server": object(awgServerFields)},
	"tuic":        {"server": object(tuicServerFields)},
	"masque":      {"address": array(shortText(), 2), "mtu": integer(1280, 65535)},
}

var uuidPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,63}$`)

func validHost(host string) bool {
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "\x00\r\n /\\@%?#") {
		return false
	}
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return true
	}
	for _, part := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, c := range part {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}
func validHostPort(s string) bool {
	h, p, e := net.SplitHostPort(s)
	n, pe := strconv.Atoi(p)
	return e == nil && pe == nil && n > 0 && n <= 65535 && validHost(h)
}

func validateSettings(s *Spec) error {
	m := s.Settings
	for _, key := range []string{"clients", "users", "accounts", "peers"} {
		if _, ok := m[key]; ok {
			return invalid("settings."+key, "managed_user_field")
		}
	}
	fields := settingSchemas[s.Protocol]
	if !has([]string{"mtproto", "amneziawg", "tuic"}, s.Protocol) {
		fields = addFields(fields, map[string]rule{"sniffing": sniffingRule})
	}
	if err := checkObject(m, fields, "settings"); err != nil {
		return err
	}
	switch s.Protocol {
	case "vless":
		putDefault(m, "decryption", "none")
		if e := str(m, "encryption"); e != "" {
			if c := str(s.Client, "encryption"); c != "" && c != e {
				return invalid("client.encryption", "conflicting_encryption")
			}
			s.Client["encryption"] = e
			delete(m, "encryption")
		}
		if v, ok := m["testseed"]; ok && len(list(v)) != 4 {
			return invalid("settings.testseed", "four_values_required")
		}
	case "shadowsocks":
		if boolean(m, "ivCheck") {
			return invalid("settings.ivCheck", "not_supported_by_pinned_engine")
		}
		putDefault(m, "method", "2022-blake3-aes-256-gcm")
		putDefault(m, "network", "tcp,udp")
		if err := validateSSKey(str(m, "method"), str(m, "password")); err != nil {
			return err
		}
	case "hysteria":
		putDefault(m, "version", 2)
	case "wireguard":
		if !validWGKey(str(m, "secretKey")) {
			return invalid("settings.secretKey", "private_key_required")
		}
		putDefault(m, "subnetIp", "10.66.0.0")
		putDefault(m, "subnetCidr", 16)
		if err := validateSubnet(str(m, "subnetIp"), num(m, "subnetCidr"), "settings"); err != nil {
			return err
		}
		if r, ok := m["reserved"]; ok && len(list(r)) != 3 {
			return invalid("settings.reserved", "three_bytes_required")
		}
	case "http":
		putDefault(m, "allowTransparent", false)
	case "mixed":
		putDefault(m, "auth", "password")
		putDefault(m, "udp", false)
		putDefault(m, "ip", "127.0.0.1")
		if net.ParseIP(str(m, "ip")) == nil {
			return invalid("settings.ip", "ip_required")
		}
	case "tunnel":
		putDefault(m, "allowedNetwork", "tcp,udp")
		if !boolean(m, "followRedirect") && (str(m, "rewriteAddress") == "" || num(m, "rewritePort") == 0) && len(obj(m, "portMap")) == 0 {
			return invalid("settings", "tunnel_destination_required")
		}
		if a := str(m, "rewriteAddress"); a != "" && !validHost(a) {
			return invalid("settings.rewriteAddress", "host_invalid")
		}
		for p, d := range obj(m, "portMap") {
			n, e := strconv.Atoi(p)
			v, _ := d.(string)
			if e != nil || n < 1 || n > 65535 || !validHostPort(v) {
				return invalid("settings.portMap", "port_mapping_invalid")
			}
		}
	case "tun":
		if boolean(m, "autoSystemDnsToGateway") {
			return invalid("settings.autoSystemDnsToGateway", "tun_host_dns_takeover_unsupported")
		}
		putDefault(m, "name", "xray0")
		putDefault(m, "mtu", 1500)
		if len(str(m, "name")) > 15 || !namePattern.MatchString(str(m, "name")) {
			return invalid("settings.name", "interface_name_invalid")
		}
		for _, g := range texts(m["gateway"]) {
			if _, e := netip.ParsePrefix(g); e != nil {
				return invalid("settings.gateway", "cidr_required")
			}
		}
	case "mtproto":
		putDefault(m, "fakeTlsDomain", "www.cloudflare.com")
		if !validHost(str(m, "fakeTlsDomain")) || net.ParseIP(str(m, "fakeTlsDomain")) != nil {
			return invalid("settings.fakeTlsDomain", "dns_required")
		}
		for _, k := range []string{"publicIpv4", "publicIpv6"} {
			if v := str(m, k); v != "" {
				ip := net.ParseIP(v)
				if ip == nil || (k == "publicIpv4") != (ip.To4() != nil) {
					return invalid("settings."+k, "ip_family_invalid")
				}
			}
		}
	case "amneziawg":
		a := obj(m, "server")
		if a == nil {
			return invalid("settings.server", "server_required")
		}
		if !validWGKey(str(a, "privateKey")) {
			return invalid("settings.server.privateKey", "private_key_required")
		}
		// The owned userspace adapter has no host address/interface lifecycle.
		// Accepting these Sanaei options would promise a source alias or device
		// binding that the running worker cannot provide.
		for _, field := range []string{"externalInterface", "ipv6ExternalInterface"} {
			if str(a, field) != "" {
				return invalid("settings.server."+field, "amneziawg_host_alias_unsupported")
			}
		}
		if mtu := num(a, "mtu"); mtu != 0 && (mtu < 1280 || mtu > 9000) {
			return invalid("settings.server.mtu", "amneziawg_mtu_range")
		}
		putDefault(a, "subnetIp", "10.8.0.0")
		putDefault(a, "subnetCidr", 16)
		if err := validateSubnet(str(a, "subnetIp"), num(a, "subnetCidr"), "settings.server"); err != nil {
			return err
		}
		if num(a, "jmin") > num(a, "jmax") && num(a, "jmax") != 0 {
			return invalid("settings.server.jmax", "junk_range_invalid")
		}
		if boolean(a, "ipv6Enabled") {
			p, e := netip.ParsePrefix(str(a, "ipv6Subnet"))
			if e != nil || !p.Addr().Is6() {
				return invalid("settings.server.ipv6Subnet", "ipv6_prefix_required")
			}
			if p.Bits() > 126 {
				return invalid("settings.server.ipv6Subnet", "amneziawg_ipv6_prefix_too_small")
			}
		}
	case "tuic":
		a := obj(m, "server")
		if a == nil {
			a = map[string]any{}
			m["server"] = a
		}
		putDefault(a, "congestion_control", "bbr")
		putDefault(a, "udp_relay_mode", "native")
		putDefault(a, "max_idle_time", 15)
		putDefault(a, "authentication_timeout", 3)
		putDefault(a, "max_udp_relay_packet_size", 1500)
		putDefault(a, "alpn", []any{"h3", "spdy/3.1"})
	case "masque":
		putDefault(m, "address", []any{"10.14.0.1/24", "fd14::1/64"})
		seen := map[bool]bool{}
		for _, a := range texts(m["address"]) {
			p, e := netip.ParsePrefix(a)
			if e != nil || seen[p.Addr().Is4()] {
				return invalid("settings.address", "one_prefix_per_family_required")
			}
			seen[p.Addr().Is4()] = true
		}
	}
	if fallbacks, ok := m["fallbacks"]; ok {
		if len(list(fallbacks)) > 0 && (s.Network != "tcp" || s.Security != "tls") {
			return invalid("settings.fallbacks", "fallback_requires_tcp_tls")
		}
		for i, f := range list(fallbacks) {
			v := f.(map[string]any)
			d := v["dest"]
			if text, ok := d.(string); ok {
				if !validHostPort(text) && !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "@") {
					if n, e := strconv.Atoi(text); e != nil || n < 1 || n > 65535 {
						return invalid(fmt.Sprintf("settings.fallbacks.%d.dest", i), "destination_invalid")
					}
				}
			} else if _, ok := number(d); !ok {
				return invalid(fmt.Sprintf("settings.fallbacks.%d.dest", i), "destination_required")
			}
			if p := str(v, "path"); p != "" && (!strings.HasPrefix(p, "/") || len(p) > 55 || strings.ContainsAny(p, "\r\n")) {
				return invalid("settings.fallbacks.path", "fallback_path_invalid")
			}
		}
	}
	return nil
}

func validateSubnet(ip string, bits int64, path string) error {
	a, e := netip.ParseAddr(ip)
	if e != nil || !a.Is4() || bits < 1 || bits > 30 {
		return invalid(path+".subnetIp", "ipv4_subnet_required")
	}
	return nil
}
