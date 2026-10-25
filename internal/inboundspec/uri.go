package inboundspec

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func firstHeader(headers map[string]any, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			if s, ok := v.(string); ok {
				return s
			}
			if a := texts(v); len(a) > 0 {
				return a[0]
			}
		}
	}
	return ""
}
func onlyHostHeader(headers map[string]any) bool {
	for k := range headers {
		if !strings.EqualFold(k, "host") {
			return false
		}
	}
	return true
}

func exportURI(s Spec, u User, ep Endpoint) ([]byte, error) {
	if s.Network == "xdrive" || s.Protocol == "masque" || s.Protocol == "tunnel" || s.Protocol == "tun" {
		return nil, unsupported("uri; use json for this transport or protocol")
	}
	if boolean(obj(s.Client, "mux"), "enabled") {
		return nil, unsupported("uri client mux settings; use json")
	}
	endpoint := net.JoinHostPort(ep.Address, strconv.Itoa(int(ep.Port)))
	if s.Protocol == "mtproto" {
		secret := UserSecret(s, u)
		if !validMTProtoSecret(secret) {
			return nil, invalid("identity", "mtproto_secret_invalid")
		}
		q := url.Values{"server": {ep.Address}, "port": {strconv.Itoa(int(ep.Port))}, "secret": {secret}}
		return []byte((&url.URL{Scheme: "tg", Host: "proxy", RawQuery: q.Encode()}).String()), nil
	}
	if s.Protocol == "amneziawg" {
		conf, e := exportWireGuard(s, u, ep)
		if e != nil {
			return nil, e
		}
		return []byte("vpn://" + base64.RawURLEncoding.EncodeToString(conf)), nil
	}
	if s.Protocol == "wireguard" {
		if str(s.Client, "preSharedKey") != "" || len(texts(s.Client["dns"])) > 0 || num(s.Client, "keepAlive") > 0 || len(texts(s.Client["allowedIPs"])) > 0 || str(s.Settings, "dns") != "" || len(texts(s.Settings["dns"])) > 0 || hasWireGuardReserved(s) || len(clientFinalMask(s)) > 0 {
			return nil, unsupported("uri; use wireguard config to preserve peer options")
		}
		priv, _, e := DeriveWireGuardIdentity(s, u)
		if e != nil {
			return nil, e
		}
		pub, e := WireGuardServerPublicKey(s)
		if e != nil {
			return nil, e
		}
		addresses, e := WireGuardAddresses(s, u)
		if e != nil {
			return nil, e
		}
		q := url.Values{"publickey": {pub}, "address": {strings.Join(addresses, ",")}}
		if mtu := num(s.Settings, "mtu"); mtu > 0 {
			q.Set("mtu", strconv.FormatInt(mtu, 10))
		}
		return []byte((&url.URL{Scheme: "wireguard", Host: endpoint, User: url.User(priv), RawQuery: q.Encode(), Fragment: ep.Name}).String()), nil
	}
	if s.Protocol == "tuic" {
		if len(texts(s.Client["pinnedPeerCertSha256"])) > 0 || str(s.Client, "echConfigList") != "" || str(s.Client, "verifyPeerCertByName") != "" {
			return nil, unsupported("TUIC URI TLS verification or ECH extensions")
		}
		a := obj(s.Settings, "server")
		name, e := clientSNI(s, ep)
		if e != nil {
			return nil, e
		}
		q := url.Values{"sni": {name}, "alpn": {strings.Join(texts(a["alpn"]), ",")}, "congestion_control": {str(a, "congestion_control")}, "udp_relay_mode": {str(a, "udp_relay_mode")}, "allow_insecure": {"0"}}
		if boolean(s.Client, "allowInsecure") {
			q.Set("allow_insecure", "1")
		}
		q.Set("zero_rtt_handshake", strconv.FormatBool(boolean(a, "zero_rtt_handshake")))
		return []byte((&url.URL{Scheme: "tuic", Host: endpoint, User: url.UserPassword(u.UUID, PasswordFor(s, u)), RawQuery: q.Encode(), Fragment: ep.Name}).String()), nil
	}
	if s.Protocol == "http" || s.Protocol == "mixed" {
		scheme := "http"
		if s.Protocol == "mixed" {
			scheme = "socks5"
		}
		link := url.URL{Scheme: scheme, Host: endpoint, Fragment: ep.Name}
		if s.Protocol != "mixed" || str(s.Settings, "auth") != "noauth" {
			link.User = url.UserPassword(u.Email, PasswordFor(s, u))
		}
		return []byte(link.String()), nil
	}
	stream, e := clientStream(s, u, ep)
	if e != nil {
		return nil, e
	}
	q := url.Values{"type": {s.Network}, "security": {s.Security}}
	if s.Security == "tls" {
		tls := obj(stream, "tlsSettings")
		for from, to := range map[string]string{"serverName": "sni", "fingerprint": "fp", "echConfigList": "ech", "verifyPeerCertByName": "vcn", "pinnedPeerCertSha256": "pcs"} {
			if v := str(tls, from); v != "" {
				q.Set(to, v)
			}
		}
		if alpn := texts(tls["alpn"]); len(alpn) > 0 {
			q.Set("alpn", strings.Join(alpn, ","))
		}
	}
	if s.Security == "reality" {
		reality := obj(stream, "realitySettings")
		for from, to := range map[string]string{"serverName": "sni", "fingerprint": "fp", "publicKey": "pbk", "shortId": "sid", "spiderX": "spx", "mldsa65Verify": "pqv"} {
			if v, ok := reality[from].(string); ok {
				q.Set(to, v)
			}
		}
	}
	if fm := obj(stream, "finalmask"); len(fm) > 0 {
		b, _ := json.Marshal(fm)
		q.Set("fm", string(b))
	}
	t := obj(stream, s.Network+"Settings")
	switch s.Network {
	case "tcp":
		h := obj(t, "header")
		if str(h, "type") == "http" {
			r := obj(h, "request")
			if len(obj(h, "response")) > 0 || !onlyHostHeader(obj(r, "headers")) || (str(r, "method") != "" && str(r, "method") != "GET") {
				return nil, unsupported("uri RAW custom headers; use json")
			}
			q.Set("headerType", "http")
			q.Set("path", strings.Join(texts(r["path"]), ","))
			q.Set("host", firstHeader(obj(r, "headers"), "Host"))
		}
	case "kcp":
		for k, v := range t {
			if k == "mtu" || k == "tti" {
				q.Set(k, strconv.FormatInt(num(t, k), 10))
				continue
			}
			if k == "cwndMultiplier" && num(t, k) == 1 {
				continue
			}
			if n, ok := number(v); ok && n != 0 {
				return nil, unsupported("uri KCP advanced tuning; use json")
			}
		}
	case "ws", "httpupgrade":
		if !onlyHostHeader(obj(t, "headers")) || num(t, "heartbeatPeriod") > 0 {
			return nil, unsupported("uri custom HTTP headers; use json")
		}
		q.Set("path", str(t, "path"))
		h := str(t, "host")
		if h == "" {
			h = firstHeader(obj(t, "headers"), "Host")
		}
		if h != "" {
			q.Set("host", h)
		}
	case "grpc":
		for k, v := range t {
			if k == "serviceName" || k == "authority" || k == "multiMode" {
				continue
			}
			if n, ok := number(v); ok && n == 0 {
				continue
			}
			if b, ok := v.(bool); ok && !b {
				continue
			}
			if v == "" {
				continue
			}
			return nil, unsupported("uri gRPC advanced settings; use json")
		}
		q.Set("serviceName", str(t, "serviceName"))
		if a := str(t, "authority"); a != "" {
			q.Set("authority", a)
		}
		if boolean(t, "multiMode") {
			q.Set("mode", "multi")
		}
	case "xhttp":
		q.Set("path", str(t, "path"))
		q.Set("host", str(t, "host"))
		q.Set("mode", str(t, "mode"))
		extra := cloneMap(t)
		for _, k := range []string{"path", "host", "mode"} {
			delete(extra, k)
		}
		if len(extra) > 0 {
			b, _ := json.Marshal(extra)
			q.Set("extra", string(b))
		}
	}
	if s.Protocol == "hysteria" {
		q.Del("type")
		q.Del("security")
		if pins := texts(s.Client["pinnedPeerCertSha256"]); len(pins) > 0 {
			out := make([]string, 0, len(pins))
			for _, p := range pins {
				v, _ := normalizePin(p)
				out = append(out, v)
			}
			q.Set("pinSHA256", strings.Join(out, ","))
			q.Del("pcs")
		}
		for _, v := range list(obj(s.Stream, "finalmask")["udp"]) {
			m := v.(map[string]any)
			if str(m, "type") == "salamander" {
				a := obj(m, "settings")
				q.Set("obfs", "salamander")
				if v, ok := a["packetSize"]; ok {
					q.Set("obfs", "gecko")
					q.Set("obfs-packet-size", stringValue(v))
				}
				q.Set("obfs-password", str(a, "password"))
			}
		}
		if h := obj(obj(obj(s.Stream, "finalmask"), "quicParams"), "udpHop"); h != nil {
			q.Set("mport", str(h, "ports"))
		}
		return []byte((&url.URL{Scheme: "hysteria2", Host: endpoint, User: url.User(PasswordFor(s, u)), RawQuery: q.Encode(), Fragment: ep.Name}).String()), nil
	}
	if s.Protocol == "vmess" {
		security := str(s.Client, "security")
		if security == "" {
			security = "auto"
		}
		m := map[string]any{"v": "2", "ps": ep.Name, "add": ep.Address, "port": strconv.Itoa(int(ep.Port)), "id": u.UUID, "aid": "0", "scy": security, "net": s.Network, "type": "none", "tls": ""}
		if s.Security == "tls" {
			m["tls"] = "tls"
		}
		for _, k := range []string{"host", "path", "sni", "alpn", "fp", "ech", "vcn", "pcs", "fm", "extra", "mode", "serviceName", "authority"} {
			if v := q.Get(k); v != "" {
				m[k] = v
			}
		}
		if v := q.Get("headerType"); v != "" {
			m["type"] = v
		}
		if s.Network == "grpc" {
			m["path"] = str(t, "serviceName")
			if boolean(t, "multiMode") {
				m["type"] = "multi"
			}
		}
		if s.Network == "xhttp" {
			m["type"] = str(t, "mode")
		}
		if s.Network == "kcp" {
			for _, key := range []string{"mtu", "tti"} {
				if value, exists := t[key]; exists {
					m[key] = value
				}
			}
		}
		b, e := json.Marshal(m)
		if e != nil {
			return nil, e
		}
		return []byte("vmess://" + base64.StdEncoding.EncodeToString(b)), nil
	}
	scheme := s.Protocol
	credentials := u.UUID
	switch s.Protocol {
	case "vless":
		enc := str(s.Client, "encryption")
		if enc == "" {
			enc = "none"
		}
		if str(s.Settings, "decryption") != "none" && enc == "none" {
			return nil, invalid("client.encryption", "vless_client_encryption_required")
		}
		q.Set("encryption", enc)
		if flow := str(s.Client, "flow"); flow != "" {
			q.Set("flow", flow)
		}
	case "trojan":
		credentials = PasswordFor(s, u)
	case "shadowsocks":
		scheme = "ss"
		method := str(s.Settings, "method")
		password := PasswordFor(s, u)
		if strings.HasPrefix(method, "2022-blake3-aes-") {
			password = str(s.Settings, "password") + ":" + password
		}
		credentials = base64.RawURLEncoding.EncodeToString([]byte(method + ":" + password))
	default:
		return nil, unsupported("uri")
	}
	return []byte((&url.URL{Scheme: scheme, Host: endpoint, User: url.User(credentials), RawQuery: q.Encode(), Fragment: ep.Name}).String()), nil
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if n, ok := number(v); ok {
		return strconv.FormatInt(n, 10)
	}
	return ""
}
