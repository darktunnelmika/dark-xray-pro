package sub

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Format picks the subscription body for a client app: explicit ?format=
// wins, otherwise the User-Agent decides; everything else gets Base64 links.
func Format(query, userAgent string) string {
	switch strings.ToLower(query) {
	case "singbox", "sing-box", "json":
		return "singbox"
	case "clash", "mihomo", "yaml":
		return "clash"
	case "links", "base64", "v2ray":
		return "base64"
	case "xray", "xray-json", "v2ray-json":
		return "xray"
	}
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "sing-box") || strings.Contains(ua, "sfa") || strings.Contains(ua, "sfi") || strings.Contains(ua, "hiddify-next/") && strings.Contains(ua, "singbox"):
		return "singbox"
	case strings.Contains(ua, "clash") || strings.Contains(ua, "mihomo") || strings.Contains(ua, "stash"):
		return "clash"
	}
	return "base64"
}

type outbound struct {
	tag, proto, server string
	port               int
	uuid, password     string
	flow               string
	t                  template
	m                  Meta
	frag               bool // this address asks for TLS fragmentation (Xray JSON)
	mux                int  // mux concurrency (Xray JSON, 0 = off)
}

func outbounds(a Account, ins []Inbound, format string) ([]outbound, error) {
	sorted := append([]Inbound(nil), ins...)
	byRank(sorted)
	var out []outbound
	seen := map[string]int{}
	for _, in := range sorted {
		base, err := parse(in.Template)
		if err != nil {
			return nil, fmt.Errorf("inbound %s: %w", in.Tag, err)
		}
		t := base
		switch t.Protocol {
		case "vless", "vmess", "trojan", "hysteria", "tuic":
		case "wireguard":
			if in.Meta.WGPrivateKey == "" {
				continue
			}
		case "shadowsocks":
			if !SSKnown(t.Settings.Method) {
				continue
			}
		default:
			continue
		}
		if t.Protocol == "vless" && in.Meta.VLESSEncryption != "" && format == "singbox" {
			continue // sing-box does not speak VLESS encryption
		}
		for _, ep := range in.Meta.Endpoints {
			if ep.excluded(format) {
				continue
			}
			t, m := forEndpoint(base, in.Meta, ep)
			tag := remark(a, in, ep)
			if n := seen[tag]; n > 0 {
				tag += " " + strconv.Itoa(n+1)
			}
			seen[remark(a, in, ep)]++
			o := outbound{tag: tag, proto: t.Protocol, server: ep.Address, port: ep.Port, uuid: a.UUID, password: a.Secret, t: t, m: m, frag: ep.Fragment, mux: ep.Mux}
			if t.Protocol == "shadowsocks" {
				o.password = ssPassword(t, a.Secret)
			}
			if t.Protocol == "vless" && t.Stream.Network == "tcp" {
				o.flow = t.Settings.Flow
			}
			out = append(out, o)
		}
	}
	return out, nil
}

// SingBox renders a complete sing-box client config: one outbound per
// endpoint, an automatic best-latency group and a manual selector.
func SingBox(a Account, ins []Inbound) ([]byte, error) {
	obs, err := outbounds(a, ins, "singbox")
	if err != nil {
		return nil, err
	}
	var list, endpoints []any
	var tags []string
	for _, o := range obs {
		ob := map[string]any{"type": o.proto, "tag": o.tag, "server": o.server, "server_port": o.port}
		switch o.proto {
		case "vless":
			ob["uuid"] = o.uuid
			if o.flow != "" {
				ob["flow"] = o.flow
			}
		case "vmess":
			ob["uuid"], ob["security"], ob["alter_id"] = o.uuid, "auto", 0
		case "trojan":
			ob["password"] = o.password
		case "shadowsocks":
			ob["method"], ob["password"] = o.t.Settings.Method, o.password
		case "wireguard":
			// sing-box 1.11+: WireGuard is an endpoint, usable by tag like an outbound.
			endpoints = append(endpoints, map[string]any{"type": "wireguard", "tag": o.tag, "address": []string{o.m.WGAddress},
				"private_key": o.m.WGPrivateKey, "mtu": or0(o.m.MTU, 1420),
				"peers": []any{map[string]any{"address": o.server, "port": o.port, "public_key": o.m.PublicKey, "allowed_ips": []string{"0.0.0.0/0", "::/0"}}}})
			tags = append(tags, o.tag)
			continue
		case "tuic":
			ob["uuid"], ob["password"] = o.uuid, o.password
			ob["congestion_control"], ob["udp_relay_mode"] = or(o.t.Settings.CongestionControl, "bbr"), "native"
			ob["tls"] = map[string]any{"enabled": true, "server_name": o.t.Stream.TLS.ServerName, "alpn": []string{"h3"}, "insecure": o.t.Stream.insecure}
			list = append(list, ob)
			tags = append(tags, o.tag)
			continue
		case "hysteria":
			ob["type"], ob["password"] = "hysteria2", o.password
			ob["tls"] = map[string]any{"enabled": true, "server_name": o.t.Stream.TLS.ServerName, "alpn": []string{"h3"}, "insecure": o.t.Stream.insecure}
			list = append(list, ob)
			tags = append(tags, o.tag)
			continue
		}
		s := o.t.Stream
		switch s.Security {
		case "reality":
			ob["tls"] = map[string]any{"enabled": true, "server_name": first(s.Reality.ServerNames),
				"utls":    map[string]any{"enabled": true, "fingerprint": or(o.m.Fingerprint, "chrome")},
				"reality": map[string]any{"enabled": true, "public_key": o.m.PublicKey, "short_id": or(o.m.ShortID, first(s.Reality.ShortIDs))}}
		case "tls":
			tls := map[string]any{"enabled": true, "server_name": s.TLS.ServerName, "utls": map[string]any{"enabled": true, "fingerprint": or(o.m.Fingerprint, "chrome")}}
			if s.insecure {
				tls["insecure"] = true
			}
			if len(s.TLS.ALPN) > 0 {
				tls["alpn"] = s.TLS.ALPN
			}
			ob["tls"] = tls
		}
		switch s.Network {
		case "grpc":
			ob["transport"] = map[string]any{"type": "grpc", "service_name": s.GRPC.ServiceName}
		case "ws":
			tr := map[string]any{"type": "ws", "path": or(s.WS.Path, "/")}
			if s.WS.Host != "" {
				tr["headers"] = map[string]any{"Host": s.WS.Host}
			}
			ob["transport"] = tr
		case "httpupgrade":
			ob["transport"] = map[string]any{"type": "httpupgrade", "path": or(s.HTTPUpgrade.Path, "/"), "host": s.HTTPUpgrade.Host}
		case "xhttp", "splithttp", "kcp":
			// sing-box has no xhttp or mKCP transport; such endpoints stay link-only.
			continue
		}
		if _, _, ok := s.tcpHTTP(); ok {
			continue // TCP with HTTP obfuscation: link-only
		}
		list = append(list, ob)
		tags = append(tags, o.tag)
	}
	groups := []any{}
	if len(tags) > 0 {
		groups = append(groups,
			map[string]any{"type": "selector", "tag": "Dark VPN", "outbounds": append([]string{"Auto"}, tags...), "default": "Auto"},
			map[string]any{"type": "urltest", "tag": "Auto", "outbounds": tags, "url": "https://www.gstatic.com/generate_204", "interval": "3m"})
	}
	cfg := map[string]any{
		"log":       map[string]any{"level": "warn"},
		"dns":       map[string]any{"servers": []any{map[string]any{"tag": "remote", "address": "https://1.1.1.1/dns-query", "detour": "Dark VPN"}}},
		"inbounds":  []any{map[string]any{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30"}, "auto_route": true, "strict_route": true}},
		"outbounds": append(append(groups, list...), map[string]any{"type": "direct", "tag": "direct"}),
		"route":     map[string]any{"auto_detect_interface": true, "final": "Dark VPN"},
	}
	if len(endpoints) > 0 {
		cfg["endpoints"] = endpoints
	}
	if a.IranDirect && len(tags) > 0 {
		base := "https://raw.githubusercontent.com/Chocolate4U/Iran-sing-box-rules/rule-set/"
		route := cfg["route"].(map[string]any)
		route["rule_set"] = []any{
			map[string]any{"tag": "geosite-ir", "type": "remote", "format": "binary", "url": base + "geosite-ir.srs", "download_detour": "Dark VPN"},
			map[string]any{"tag": "geoip-ir", "type": "remote", "format": "binary", "url": base + "geoip-ir.srs", "download_detour": "Dark VPN"},
		}
		route["rules"] = []any{
			map[string]any{"domain_suffix": []string{".ir"}, "outbound": "direct"},
			map[string]any{"rule_set": []string{"geosite-ir", "geoip-ir"}, "outbound": "direct"},
		}
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func yq(s string) string { // YAML double-quoted scalar
	b, _ := json.Marshal(s)
	return string(b)
}

// Clash renders a mihomo (Clash Meta) profile.
func Clash(a Account, ins []Inbound) ([]byte, error) {
	obs, err := outbounds(a, ins, "clash")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: warning\nproxies:\n")
	var names []string
	for _, o := range obs {
		s := o.t.Stream
		if s.Network == "xhttp" || s.Network == "splithttp" || s.Network == "httpupgrade" || s.Network == "kcp" {
			continue // not supported by mihomo; link-only
		}
		if _, _, ok := s.tcpHTTP(); ok {
			continue // TCP with HTTP obfuscation: link-only
		}
		ctype := o.proto
		switch ctype {
		case "shadowsocks":
			ctype = "ss"
		case "hysteria":
			ctype = "hysteria2"
		}
		if ctype == "wireguard" {
			fmt.Fprintf(&b, "  - name: %s\n    type: wireguard\n    server: %s\n    port: %d\n    udp: true\n", yq(o.tag), yq(o.server), o.port)
			fmt.Fprintf(&b, "    private-key: %s\n    public-key: %s\n    ip: %s\n    mtu: %d\n", yq(o.m.WGPrivateKey), yq(o.m.PublicKey), yq(strings.TrimSuffix(o.m.WGAddress, "/32")), or0(o.m.MTU, 1420))
			names = append(names, o.tag)
			continue
		}
		fmt.Fprintf(&b, "  - name: %s\n    type: %s\n    server: %s\n    port: %d\n    udp: true\n", yq(o.tag), ctype, yq(o.server), o.port)
		switch o.proto {
		case "vless":
			fmt.Fprintf(&b, "    uuid: %s\n", yq(o.uuid))
			if o.m.VLESSEncryption != "" {
				fmt.Fprintf(&b, "    encryption: %s\n", yq(o.m.VLESSEncryption))
			}
			if o.flow != "" {
				fmt.Fprintf(&b, "    flow: %s\n", yq(o.flow))
			}
		case "vmess":
			fmt.Fprintf(&b, "    uuid: %s\n    alterId: 0\n    cipher: auto\n", yq(o.uuid))
		case "trojan":
			fmt.Fprintf(&b, "    password: %s\n", yq(o.password))
		case "tuic":
			fmt.Fprintf(&b, "    uuid: %s\n    password: %s\n    sni: %s\n    alpn: [h3]\n    congestion-controller: %s\n    udp-relay-mode: native\n    reduce-rtt: true\n",
				yq(o.uuid), yq(o.password), yq(o.t.Stream.TLS.ServerName), or(o.t.Settings.CongestionControl, "bbr"))
			if o.t.Stream.insecure {
				b.WriteString("    skip-cert-verify: true\n")
			}
			names = append(names, o.tag)
			continue
		case "hysteria":
			fmt.Fprintf(&b, "    password: %s\n    sni: %s\n    alpn: [h3]\n", yq(o.password), yq(o.t.Stream.TLS.ServerName))
			if o.t.Stream.insecure {
				b.WriteString("    skip-cert-verify: true\n")
			}
			if o.m.PinSHA256 != "" {
				fmt.Fprintf(&b, "    fingerprint: %s\n", yq(o.m.PinSHA256))
			}
			names = append(names, o.tag)
			continue
		case "shadowsocks":
			fmt.Fprintf(&b, "    cipher: %s\n    password: %s\n", yq(o.t.Settings.Method), yq(o.password))
			names = append(names, o.tag)
			continue
		}
		fmt.Fprintf(&b, "    network: %s\n", s.Network)
		switch s.Security {
		case "reality":
			fmt.Fprintf(&b, "    tls: true\n    servername: %s\n    client-fingerprint: %s\n    reality-opts:\n      public-key: %s\n      short-id: %s\n",
				yq(first(s.Reality.ServerNames)), or(o.m.Fingerprint, "chrome"), yq(o.m.PublicKey), yq(or(o.m.ShortID, first(s.Reality.ShortIDs))))
		case "tls":
			fmt.Fprintf(&b, "    tls: true\n    servername: %s\n    client-fingerprint: %s\n", yq(s.TLS.ServerName), or(o.m.Fingerprint, "chrome"))
			if s.insecure {
				b.WriteString("    skip-cert-verify: true\n")
			}
		}
		switch s.Network {
		case "grpc":
			fmt.Fprintf(&b, "    grpc-opts:\n      grpc-service-name: %s\n", yq(s.GRPC.ServiceName))
		case "ws":
			fmt.Fprintf(&b, "    ws-opts:\n      path: %s\n", yq(or(s.WS.Path, "/")))
			if s.WS.Host != "" {
				fmt.Fprintf(&b, "      headers:\n        Host: %s\n", yq(s.WS.Host))
			}
		}
		names = append(names, o.tag)
	}
	b.WriteString("proxy-groups:\n  - name: \"Dark VPN\"\n    type: select\n    proxies:\n      - \"Auto\"\n")
	for _, n := range names {
		fmt.Fprintf(&b, "      - %s\n", yq(n))
	}
	b.WriteString("  - name: \"Auto\"\n    type: url-test\n    url: https://www.gstatic.com/generate_204\n    interval: 180\n    proxies:\n")
	for _, n := range names {
		fmt.Fprintf(&b, "      - %s\n", yq(n))
	}
	if len(names) == 0 {
		b.WriteString("      - DIRECT\n")
	}
	b.WriteString("rules:\n")
	if a.IranDirect && len(names) > 0 {
		b.WriteString("  - DOMAIN-SUFFIX,ir,DIRECT\n  - GEOIP,IR,DIRECT\n")
	}
	b.WriteString("  - MATCH,Dark VPN\n")
	return []byte(b.String()), nil
}
