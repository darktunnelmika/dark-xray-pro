package sub

import (
	"encoding/json"
)

// Fragment configures TLS ClientHello fragmentation and UDP noise in Xray
// client configs, which gets connections past some DPI filters.
type Fragment struct {
	Packets  string `json:"packets"`  // "tlshello" (default) or "1-3"
	Length   string `json:"length"`   // e.g. "100-200"
	Interval string `json:"interval"` // ms, e.g. "10-20"
	Noise    bool   `json:"noise"`    // random UDP noise before QUIC/UDP traffic
}

// XrayJSON renders the Xray client format apps such as v2rayNG, v2rayN,
// Happ and Streisand import: a JSON array with one complete config per
// server. Unlike share links it can carry fragment, noise and routing.
func XrayJSON(a Account, ins []Inbound) ([]byte, error) {
	obs, err := outbounds(a, ins, "xray")
	if err != nil {
		return nil, err
	}
	var list []any
	for _, o := range obs {
		proxy := xrayOutbound(o, a.Fragment != nil)
		if proxy == nil {
			continue
		}
		outs := []any{proxy, map[string]any{"tag": "direct", "protocol": "freedom"}, map[string]any{"tag": "block", "protocol": "blackhole"}}
		if f := a.Fragment; f != nil {
			fs := map[string]any{"fragment": map[string]any{"packets": or(f.Packets, "tlshello"), "length": or(f.Length, "100-200"), "interval": or(f.Interval, "10-20")}}
			if f.Noise {
				fs["noises"] = []any{map[string]any{"type": "rand", "packet": "10-20", "delay": "10-16"}}
			}
			outs = append(outs, map[string]any{"tag": "fragment", "protocol": "freedom", "settings": fs,
				"streamSettings": map[string]any{"sockopt": map[string]any{"tcpNoDelay": true}}})
		}
		rules := []any{map[string]any{"type": "field", "ip": []string{"geoip:private"}, "outboundTag": "direct"}}
		if a.IranDirect {
			rules = append(rules,
				map[string]any{"type": "field", "domain": []string{"regexp:\\.ir$"}, "outboundTag": "direct"},
				map[string]any{"type": "field", "ip": []string{"geoip:ir"}, "outboundTag": "direct"})
		}
		list = append(list, map[string]any{
			"remarks": o.tag,
			"log":     map[string]any{"loglevel": "warning"},
			"dns":     map[string]any{"servers": []any{"https://1.1.1.1/dns-query", "8.8.8.8"}},
			"inbounds": []any{
				map[string]any{"tag": "socks", "listen": "127.0.0.1", "port": 10808, "protocol": "socks", "settings": map[string]any{"udp": true},
					"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}},
				map[string]any{"tag": "http", "listen": "127.0.0.1", "port": 10809, "protocol": "http"},
			},
			"outbounds": outs,
			"routing":   map[string]any{"domainStrategy": "IPIfNonMatch", "rules": rules},
		})
	}
	if list == nil {
		list = []any{}
	}
	return json.MarshalIndent(list, "", "  ")
}

// xrayOutbound turns an endpoint into an Xray "proxy" outbound (nil for
// protocols the client format does not cover).
func xrayOutbound(o outbound, fragment bool) map[string]any {
	ob := map[string]any{"tag": "proxy", "protocol": o.proto}
	switch o.proto {
	case "vless":
		u := map[string]any{"id": o.uuid, "encryption": "none"}
		if o.flow != "" {
			u["flow"] = o.flow
		}
		ob["settings"] = map[string]any{"vnext": []any{map[string]any{"address": o.server, "port": o.port, "users": []any{u}}}}
	case "vmess":
		ob["settings"] = map[string]any{"vnext": []any{map[string]any{"address": o.server, "port": o.port,
			"users": []any{map[string]any{"id": o.uuid, "alterId": 0, "security": "auto"}}}}}
	case "trojan":
		ob["settings"] = map[string]any{"servers": []any{map[string]any{"address": o.server, "port": o.port, "password": o.password}}}
	case "shadowsocks":
		ob["settings"] = map[string]any{"servers": []any{map[string]any{"address": o.server, "port": o.port, "method": o.t.Settings.Method, "password": o.password}}}
	default:
		return nil // hysteria and others stay link-only
	}
	s := o.t.Stream
	st := map[string]any{"network": s.Network, "security": s.Security}
	switch s.Security {
	case "reality":
		rs := map[string]any{"serverName": first(s.Reality.ServerNames), "publicKey": o.m.PublicKey,
			"shortId": or(o.m.ShortID, first(s.Reality.ShortIDs)), "fingerprint": or(o.m.Fingerprint, "chrome")}
		if o.m.SpiderX != "" {
			rs["spiderX"] = o.m.SpiderX
		}
		st["realitySettings"] = rs
	case "tls":
		ts := map[string]any{"serverName": s.TLS.ServerName, "fingerprint": or(o.m.Fingerprint, "chrome")}
		if len(s.TLS.ALPN) > 0 {
			ts["alpn"] = s.TLS.ALPN
		}
		if o.m.PinSHA256 != "" {
			ts["pinnedPeerCertSha256"] = o.m.PinSHA256
		}
		st["tlsSettings"] = ts
	}
	switch s.Network {
	case "ws":
		st["wsSettings"] = map[string]any{"path": or(s.WS.Path, "/"), "host": s.WS.Host}
	case "grpc":
		st["grpcSettings"] = map[string]any{"serviceName": s.GRPC.ServiceName}
	case "httpupgrade":
		st["httpupgradeSettings"] = map[string]any{"path": or(s.HTTPUpgrade.Path, "/"), "host": s.HTTPUpgrade.Host}
	case "xhttp", "splithttp":
		xs := map[string]any{"path": or(s.XHTTP.Path, "/"), "host": s.XHTTP.Host}
		if s.XHTTP.Mode != "" {
			xs["mode"] = s.XHTTP.Mode
		}
		st["network"] = "xhttp"
		st["xhttpSettings"] = xs
	}
	if fragment {
		st["sockopt"] = map[string]any{"dialerProxy": "fragment"}
	}
	ob["streamSettings"] = st
	return ob
}
