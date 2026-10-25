package inboundexec

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

const tunDNSOutboundTag = "dark-pro-inbound-dns"
const tunDNSUpstreamTag = "dark-pro-inbound-dns-upstream"

type tunDNSSettings struct {
	Name     string   `json:"name"`
	Enabled  bool     `json:"autoSystemDnsToGateway"`
	Servers  []string `json:"dns"`
	Gateways []string `json:"gateway"`
	Routes   []string `json:"autoSystemRoutingTable"`
}

type tunDNSTarget struct {
	tag     string
	address netip.Addr
	servers []string
}

func tunDNSInvalid(field, code string) error {
	return &inboundspec.FieldError{Path: "profile.spec.settings." + field, Code: code}
}

func tunDNSUnicast(a netip.Addr) bool {
	return a.IsValid() && a.Zone() == "" && !a.Is4In6() && !a.IsUnspecified() && !a.IsLoopback() && !a.IsMulticast() && (a.IsGlobalUnicast() || a.IsLinkLocalUnicast())
}

// compileTUNDNS builds the complete bounded resolver path. The authoring gate
// remains separate until the installed core proves activation and cleanup.
func compileTUNDNS(inbounds, outbounds, rules, policies []json.RawMessage) (json.RawMessage, []json.RawMessage, []json.RawMessage, error) {
	var existing json.RawMessage
	for _, raw := range policies {
		var policy map[string]any
		if json.Unmarshal(raw, &policy) != nil || policy == nil {
			return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_policy_conflict")
		}
		canonical, _ := json.Marshal(policy)
		if existing != nil && !bytes.Equal(existing, canonical) {
			return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_policy_conflict")
		}
		existing = canonical
	}
	var targets []tunDNSTarget
	var captured []netip.Prefix
	var tags []string
	interfaces := map[string]bool{}
	for _, raw := range inbounds {
		var inbound struct {
			Tag      string          `json:"tag"`
			Protocol string          `json:"protocol"`
			Settings json.RawMessage `json:"settings"`
		}
		if json.Unmarshal(raw, &inbound) != nil {
			return nil, nil, nil, ErrInvalid
		}
		tags = append(tags, inbound.Tag)
		if inbound.Protocol != "tun" {
			continue
		}
		var settings tunDNSSettings
		if json.Unmarshal(inbound.Settings, &settings) != nil {
			return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_policy_invalid")
		}
		if settings.Name != "" {
			if interfaces[settings.Name] {
				return nil, nil, nil, tunDNSInvalid("name", "execution_resource_conflict")
			}
			interfaces[settings.Name] = true
		}
		var selected netip.Prefix
		for _, value := range settings.Gateways {
			p, err := netip.ParsePrefix(value)
			if err != nil || !tunDNSUnicast(p.Addr()) {
				return nil, nil, nil, tunDNSInvalid("gateway", "tun_dns_gateway_invalid")
			}
			captured = append(captured, p.Masked())
			if !selected.IsValid() || !selected.Addr().Is4() && p.Addr().Is4() {
				selected = p
			}
		}
		for _, value := range settings.Routes {
			p, err := netip.ParsePrefix(value)
			if err != nil || p.Addr().Is4In6() || p.Addr().Zone() != "" {
				return nil, nil, nil, tunDNSInvalid("autoSystemRoutingTable", "tun_dns_route_invalid")
			}
			captured = append(captured, p.Masked())
		}
		if !settings.Enabled {
			continue
		}
		if inbound.Tag == "" || !selected.IsValid() || len(settings.Servers) == 0 {
			return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_explicit_upstream_required")
		}
		address := selected.Addr().Next()
		if !tunDNSUnicast(address) || !selected.Contains(address) || selected.Addr().Is4() && (selected.Addr() == selected.Masked().Addr() || !selected.Contains(address.Next())) {
			return nil, nil, nil, tunDNSInvalid("gateway", "tun_dns_gateway_invalid")
		}
		servers := make([]string, 0, len(settings.Servers))
		seen := map[string]bool{}
		for _, value := range settings.Servers {
			a, err := netip.ParseAddr(value)
			if err != nil || !tunDNSUnicast(a) || seen[a.String()] {
				return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_explicit_upstream_required")
			}
			seen[a.String()] = true
			servers = append(servers, a.String())
		}
		targets = append(targets, tunDNSTarget{tag: inbound.Tag, address: address, servers: servers})
	}
	if len(targets) == 0 {
		return existing, outbounds, rules, nil
	}
	for _, raw := range outbounds {
		var outbound struct {
			Tag string `json:"tag"`
		}
		if json.Unmarshal(raw, &outbound) != nil {
			return nil, nil, nil, ErrInvalid
		}
		tags = append(tags, outbound.Tag)
	}
	for _, tag := range tags {
		if tag == tunDNSOutboundTag || tag == tunDNSUpstreamTag {
			return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_reserved_tag_conflict")
		}
	}
	for _, raw := range rules {
		var rule struct {
			InboundTag  []string `json:"inboundTag"`
			OutboundTag string   `json:"outboundTag"`
		}
		if json.Unmarshal(raw, &rule) != nil {
			return nil, nil, nil, ErrInvalid
		}
		for _, tag := range append(rule.InboundTag, rule.OutboundTag) {
			if tag == tunDNSOutboundTag || tag == tunDNSUpstreamTag {
				return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_reserved_tag_conflict")
			}
		}
	}
	servers := targets[0].servers
	for _, target := range targets {
		if Digest(target.servers) != Digest(servers) {
			return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_policy_conflict")
		}
		for _, server := range target.servers {
			a := netip.MustParseAddr(server)
			for _, p := range captured {
				if p.Contains(a) {
					return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_upstream_route_loop")
				}
			}
		}
	}
	// The reserved upstream identity must retain a literal direct path. Do
	// not silently route DNS through an unreviewed proxy bootstrap dependency.
	if err := validateDirectComponentRoutes([]Component{{Engine: "tuic", Tag: tunDNSUpstreamTag, Manifest: []byte(`{}`)}}, outbounds, rules); err != nil {
		return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_upstream_route_unsupported")
	}
	dns, _ := json.Marshal(map[string]any{"servers": servers, "tag": tunDNSUpstreamTag, "queryStrategy": "UseIP", "useSystemHosts": false})
	if existing != nil && !bytes.Equal(existing, dns) {
		return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_policy_conflict")
	}
	priority := make([]json.RawMessage, 0, len(targets)+len(rules))
	for _, target := range targets {
		for _, raw := range rules {
			if tunDNSRuleMayMatch(raw, target) {
				return nil, nil, nil, tunDNSInvalid("dns", "tun_dns_route_conflict")
			}
		}
		bits := 128
		if target.address.Is4() {
			bits = 32
		}
		rule, _ := json.Marshal(map[string]any{"type": "field", "inboundTag": []string{target.tag}, "ip": []string{netip.PrefixFrom(target.address, bits).String()}, "port": "53", "network": "tcp,udp", "outboundTag": tunDNSOutboundTag})
		priority = append(priority, rule)
	}
	priority = append(priority, rules...)
	outbound, _ := json.Marshal(map[string]any{"tag": tunDNSOutboundTag, "protocol": "dns", "settings": map[string]any{"rewriteAddress": servers[0], "rewritePort": 53, "rules": []any{map[string]any{"action": "hijack", "qType": "1,28"}, map[string]any{"action": "direct"}}}})
	result := append(append([]json.RawMessage{}, outbounds...), outbound)
	return dns, result, priority, nil
}

func tunDNSRuleMayMatch(raw json.RawMessage, target tunDNSTarget) bool {
	var rule map[string]json.RawMessage
	if json.Unmarshal(raw, &rule) != nil {
		return true
	}
	if value, ok := rule["inboundTag"]; ok {
		var tags []string
		if json.Unmarshal(value, &tags) != nil {
			return true
		}
		matches := len(tags) == 0
		for _, tag := range tags {
			matches = matches || tag == target.tag
		}
		if !matches {
			return false
		}
	}
	if value, ok := rule["port"]; ok {
		var ports string
		if json.Unmarshal(value, &ports) != nil {
			var port uint16
			if json.Unmarshal(value, &port) != nil {
				return true
			}
			ports = strconv.Itoa(int(port))
		}
		matches := false
		for _, part := range strings.Split(ports, ",") {
			limits := strings.Split(part, "-")
			lo, err := strconv.Atoi(strings.TrimSpace(limits[0]))
			if err != nil || len(limits) > 2 {
				return true
			}
			hi := lo
			if len(limits) == 2 {
				hi, err = strconv.Atoi(strings.TrimSpace(limits[1]))
				if err != nil {
					return true
				}
			}
			matches = matches || lo <= 53 && hi >= 53
		}
		if !matches {
			return false
		}
	}
	if value, ok := rule["ip"]; ok {
		var values []string
		if json.Unmarshal(value, &values) != nil {
			return true
		}
		matches := len(values) == 0
		for _, value := range values {
			p, err := netip.ParsePrefix(value)
			if err != nil {
				a, e := netip.ParseAddr(value)
				if e != nil {
					return true
				}
				p = netip.PrefixFrom(a, a.BitLen())
			}
			matches = matches || p.Contains(target.address)
		}
		if !matches {
			return false
		}
	}
	return true
}
