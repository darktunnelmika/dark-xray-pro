package inboundspec

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

var quicFields = map[string]rule{
	"congestion": enum("reno", "bbr", "brutal", "force-brutal"), "bbrProfile": enum("conservative", "standard", "aggressive"), "debug": flag(), "brutalUp": shortText(), "brutalDown": shortText(), "brutalDisableLossCompensation": flag(),
	"initStreamReceiveWindow": integer(0, 1<<53), "maxStreamReceiveWindow": integer(0, 1<<53), "initConnectionReceiveWindow": integer(0, 1<<53), "maxConnectionReceiveWindow": integer(0, 1<<53),
	"maxIdleTimeout": integer(4, 120), "keepAlivePeriod": integer(2, 60), "disablePathMTUDiscovery": flag(), "disableChromeParrot": flag(), "disableGSO": flag(), "maxIncomingStreams": integer(8, 2147483647), "disableStatelessReset": flag(),
	// This authoring metadata is exported to the client as a udphop mask. The
	// server engine does not implement it and engineStream removes this key.
	"udpHop": object(map[string]rule{"ports": shortText(), "interval": boundedRange()}),
}

func transformRule(depth int) rule {
	arg := map[string]rule{"type": enum("array", "str", "hex", "base64"), "bytes": oneof(textRule(), array(integer(0, 255), 65536)), "u64": integer(0, 1<<53), "reuse": shortText(), "metadata": shortText()}
	if depth < 8 {
		arg["transform"] = transformRule(depth + 1)
	}
	return object(map[string]rule{"op": enum("concat", "slice", "xor16", "xor32", "be16", "be32", "le16", "le32", "le64", "pad", "truncate", "add", "sub", "and", "or", "shl", "shr"), "args": array(object(arg), 64)})
}
func customItemRule(tcp bool) rule {
	f := map[string]rule{"rand": integer(0, 65536), "randRange": boundedRange(), "capture": shortText(), "type": enum("array", "str", "hex", "base64"), "reuse": shortText(), "transform": transformRule(0), "packet": oneof(textRule(), array(integer(0, 255), 65536))}
	if tcp {
		f["delay"] = boundedRange()
	}
	return object(f)
}
func maskFields(kind string, tcp bool) map[string]rule {
	switch kind {
	case "fragment":
		return map[string]rule{"packets": shortText(), "lengths": array(boundedRange(), 64), "delays": array(boundedRange(), 64), "maxSplit": boundedRange()}
	case "sudoku":
		return map[string]rule{"password": textRule(), "ascii": shortText(), "customTable": textRule(), "customTables": array(textRule(), 64), "paddingMin": integer(0, 4294967295), "paddingMax": integer(0, 4294967295)}
	case "header-custom":
		if tcp {
			return map[string]rule{"clients": array(array(customItemRule(true), 256), 256), "servers": array(array(customItemRule(true), 256), 256), "errors": array(array(customItemRule(true), 256), 256)}
		}
		return map[string]rule{"mode": enum("", "prefix", "standalone"), "client": array(customItemRule(false), 256), "server": array(customItemRule(false), 256)}
	case "xmc":
		return map[string]rule{"hostname": shortText(), "password": textRule(), "profiles": array(object(map[string]rule{"username": shortText(), "uuid": shortText(), "texturesValue": textRule(), "texturesSignature": textRule()}), 64)}
	case "salamander":
		return map[string]rule{"password": textRule(), "packetSize": boundedRange()}
	case "mkcp-legacy":
		return map[string]rule{"header": enum("", "dns", "dtls", "srtp", "utp", "wechat", "wireguard"), "value": textRule()}
	case "xdns":
		return map[string]rule{"domains": array(object(map[string]rule{"names": stringList(), "lenLimit": integer(0, 255), "labelLimit": integer(0, 63), "types": array(integer(0, 65535), 64), "edns0": integer(0, 65535)}), 128), "resolvers": array(object(map[string]rule{"addrs": stringList()}), 128), "extraPoll": integer(0, 2147483647)}
	case "xicmp":
		return map[string]rule{"dgram": flag(), "ips": stringList()}
	case "realm":
		return map[string]rule{"url": shortText(), "stunServers": stringList(), "ipMode": enum("", "dual", "v4", "v6"), "portMapping": object(map[string]rule{"enabled": flag(), "timeout": integer(0, 2147483647), "lifetime": integer(0, 2147483647)}), "tlsConfig": object(addFields(tlsFields, map[string]rule{"fingerprint": enum(fingerprints...), "echConfigList": textRule(), "pinnedPeerCertSha256": shortText(), "verifyPeerCertByName": shortText()}))}
	case "noise":
		return map[string]rule{"reset": boundedRange(), "noise": array(object(map[string]rule{"rand": boundedRange(), "randRange": boundedRange(), "type": enum("array", "str", "hex", "base64", "exp"), "packet": oneof(textRule(), array(integer(0, 255), 65536)), "delay": boundedRange()}), 256)}
	}
	return nil
}

func validateFinalMask(fm map[string]any, network, protocol string) error {
	for key := range fm {
		if !has([]string{"tcp", "udp", "quicParams"}, key) {
			return invalid("stream.finalmask", "unknown_field")
		}
	}
	if q, exists := fm["quicParams"]; exists {
		if network != "xhttp" && protocol != "hysteria" {
			return invalid("stream.finalmask.quicParams", "quic_transport_required")
		}
		if err := check(q, object(quicFields), "stream.finalmask.quicParams", 0); err != nil {
			return err
		}
		m := q.(map[string]any)
		for _, pair := range [][2]string{{"initStreamReceiveWindow", "maxStreamReceiveWindow"}, {"initConnectionReceiveWindow", "maxConnectionReceiveWindow"}} {
			if num(m, pair[1]) > 0 && num(m, pair[0]) > num(m, pair[1]) {
				return invalid("stream.finalmask.quicParams."+pair[1], "window_order_invalid")
			}
		}
		if hop := obj(m, "udpHop"); hop != nil {
			if !validPortList(str(hop, "ports")) {
				return invalid("stream.finalmask.quicParams.udpHop.ports", "port_range_invalid")
			}
			if lo, _, ok := parseRange(hop["interval"], 1, 86400); !ok || lo < 1 {
				return invalid("stream.finalmask.quicParams.udpHop.interval", "interval_required")
			}
		}
	}
	for _, direction := range []string{"tcp", "udp"} {
		v, exists := fm[direction]
		if !exists {
			continue
		}
		masks, ok := v.([]any)
		if !ok || len(masks) > 64 {
			return invalid("stream.finalmask."+direction, "mask_list_invalid")
		}
		if len(masks) > 0 {
			if direction == "tcp" && (!has(genericNetworks, network) || network == "kcp" || protocol == "wireguard") {
				return invalid("stream.finalmask.tcp", "tcp_transport_required")
			}
			if direction == "udp" && network != "kcp" && protocol != "hysteria" && protocol != "wireguard" {
				return invalid("stream.finalmask.udp", "udp_transport_required")
			}
		}
		for i, item := range masks {
			path := fmt.Sprintf("stream.finalmask.%s.%d", direction, i)
			m, ok := item.(map[string]any)
			if !ok {
				return invalid(path, "mask_object_required")
			}
			for k := range m {
				if k != "type" && k != "settings" && k != "id" {
					return invalid(path, "unknown_field")
				}
			}
			kind := str(m, "type")
			tcp := direction == "tcp"
			if tcp && !has([]string{"fragment", "header-custom", "sudoku", "xmc"}, kind) {
				return invalid(path+".type", "tcp_mask_invalid")
			}
			if !tcp && !has([]string{"salamander", "mkcp-legacy", "xdns", "xicmp", "realm", "header-custom", "noise", "sudoku"}, kind) {
				return invalid(path+".type", "udp_mask_invalid_or_client_only")
			}
			if protocol == "hysteria" && kind != "salamander" {
				return invalid(path+".type", "hysteria_requires_salamander")
			}
			if kind == "salamander" && protocol != "hysteria" && protocol != "wireguard" {
				return invalid(path+".type", "salamander_protocol_mismatch")
			}
			settings := obj(m, "settings")
			if settings == nil {
				settings = map[string]any{}
				m["settings"] = settings
			}
			if err := checkObject(settings, maskFields(kind, tcp), path+".settings"); err != nil {
				return err
			}
			switch kind {
			case "fragment":
				if len(list(settings["lengths"])) == 0 {
					return invalid(path+".settings.lengths", "fragment_lengths_required")
				}
				p := str(settings, "packets")
				if p != "tlshello" && p != "" {
					if _, _, ok := parseRange(p, 1, 2147483647); !ok {
						return invalid(path+".settings.packets", "packet_range_invalid")
					}
				}
				for _, v := range list(settings["lengths"]) {
					if lo, _, ok := parseRange(v, 1, 65536); !ok || lo == 0 {
						return invalid(path+".settings.lengths", "positive_fragment_length_required")
					}
				}
			case "sudoku":
				if num(settings, "paddingMin") > num(settings, "paddingMax") {
					return invalid(path+".settings.paddingMax", "padding_order_invalid")
				}
			case "salamander":
				if len(str(settings, "password")) < 4 {
					return invalid(path+".settings.password", "password_too_short")
				}
				if v, exists := settings["packetSize"]; exists {
					lo, hi, ok := parseRange(v, 1, 2048)
					if !ok || lo < 1 || hi > 2048 {
						return invalid(path+".settings.packetSize", "gecko_packet_size_invalid")
					}
				}
			case "xmc":
				if str(settings, "password") == "" || len(list(settings["profiles"])) == 0 {
					return invalid(path+".settings", "xmc_identity_required")
				}
				for _, v := range list(settings["profiles"]) {
					p := v.(map[string]any)
					for _, k := range []string{"username", "uuid", "texturesValue", "texturesSignature"} {
						if str(p, k) == "" {
							return invalid(path+".settings.profiles", "xmc_profile_incomplete")
						}
					}
				}
			case "xicmp":
				for _, ip := range texts(settings["ips"]) {
					if _, e := netip.ParseAddr(ip); e != nil {
						return invalid(path+".settings.ips", "ip_required")
					}
				}
			case "realm":
				u, e := url.Parse(str(settings, "url"))
				if e != nil || (u.Scheme != "realm" && u.Scheme != "realm+http") || !validHost(u.Hostname()) || u.User == nil || u.User.String() == "" || strings.Trim(u.Path, "/") == "" {
					return invalid(path+".settings.url", "realm_url_required")
				}
				if len(texts(settings["stunServers"])) == 0 {
					return invalid(path+".settings.stunServers", "stun_servers_required")
				}
				for _, host := range texts(settings["stunServers"]) {
					if !validHostPort(host) {
						return invalid(path+".settings.stunServers", "host_port_required")
					}
				}
			case "header-custom":
				if err := validateCustomTree(settings, path+".settings"); err != nil {
					return err
				}
			case "noise":
				for _, v := range list(settings["noise"]) {
					if err := validatePacket(v.(map[string]any), path+".settings.noise"); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func validPortList(s string) bool {
	if s == "" {
		return false
	}
	for _, v := range strings.Split(s, ",") {
		a, b, ok := parseRange(strings.TrimSpace(v), 1, 65535)
		if !ok || a < 1 || b > 65535 {
			return false
		}
	}
	return true
}
func validatePacket(m map[string]any, path string) error {
	if v, exists := m["packet"]; exists {
		switch str(m, "type") {
		case "hex":
			s, ok := v.(string)
			if !ok {
				return invalid(path, "hex_packet_required")
			}
			if _, e := hex.DecodeString(s); e != nil {
				return invalid(path, "hex_packet_invalid")
			}
		case "base64":
			s, ok := v.(string)
			if !ok {
				return invalid(path, "base64_packet_required")
			}
			if _, e := base64.StdEncoding.DecodeString(s); e != nil {
				return invalid(path, "base64_packet_invalid")
			}
		case "str", "exp":
			if _, ok := v.(string); !ok {
				return invalid(path, "string_packet_required")
			}
		case "", "array":
			if _, ok := v.([]any); !ok {
				return invalid(path, "byte_array_required")
			}
		}
	}
	return nil
}
func validateCustomTree(value any, path string) error {
	switch v := value.(type) {
	case map[string]any:
		if _, exists := v["packet"]; exists {
			if err := validatePacket(v, path); err != nil {
				return err
			}
		}
		if _, isItem := v["capture"]; isItem || v["packet"] != nil || v["rand"] != nil || v["reuse"] != nil {
			n := 0
			if v["packet"] != nil {
				n++
			}
			if num(v, "rand") > 0 {
				n++
			}
			if str(v, "reuse") != "" {
				n++
			}
			if v["transform"] != nil {
				n++
			}
			if n > 1 {
				return invalid(path, "exclusive_item_sources")
			}
		}
		if v["args"] != nil {
			if str(v, "op") == "" || len(list(v["args"])) == 0 {
				return invalid(path, "transform_arguments_required")
			}
			for _, a := range list(v["args"]) {
				m := a.(map[string]any)
				n := 0
				for _, k := range []string{"bytes", "u64", "reuse", "metadata", "transform"} {
					if _, ok := m[k]; ok {
						n++
					}
				}
				if n != 1 {
					return invalid(path+".args", "exactly_one_transform_source_required")
				}
			}
		}
		for k, c := range v {
			if err := validateCustomTree(c, path+"."+k); err != nil {
				return err
			}
		}
	case []any:
		for i, c := range v {
			if err := validateCustomTree(c, fmt.Sprintf("%s.%d", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
