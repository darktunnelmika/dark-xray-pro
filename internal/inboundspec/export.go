package inboundspec

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ExportClient produces private material for ONE authorized business identity.
// json/xray is a complete Xray configuration for Xray protocols; external
// protocols use their documented client adapter shape. It never returns a
// server credential merely because that field exists in the saved spec.
func ExportClient(input Spec, format string, identity User, endpoint Endpoint) ([]byte, error) {
	s, e := Normalize(input)
	if e != nil {
		return nil, e
	}
	if e = validateUsers([]User{identity}); e != nil {
		return nil, e
	}
	endpoint.Address = strings.Trim(endpoint.Address, "[]")
	if !validHost(endpoint.Address) || endpoint.Port == 0 {
		return nil, invalid("endpoint", "endpoint_invalid")
	}
	if endpoint.SNI != "" && !validHost(endpoint.SNI) {
		return nil, invalid("endpoint.sni", "sni_invalid")
	}
	if strings.ContainsAny(endpoint.Name, "\x00\r\n") || len(endpoint.Name) > 512 {
		return nil, invalid("endpoint.name", "name_invalid")
	}
	info, _ := protocolInfo(s.Protocol)
	if info.ListenerOnly {
		return nil, unsupported(format)
	}
	format = strings.ToLower(format)
	if format == "" {
		format = "json"
	}
	switch format {
	case "uri":
		return exportURI(s, identity, endpoint)
	case "wireguard", "conf":
		if s.Protocol != "wireguard" && s.Protocol != "amneziawg" {
			return nil, unsupported(format)
		}
		return exportWireGuard(s, identity, endpoint)
	case "sing-box":
		return exportSingBox(s, identity, endpoint)
	case "json", "xray":
		if info.Engine == "xray" {
			out, e := clientXrayOutbound(s, identity, endpoint)
			if e != nil {
				return nil, e
			}
			return json.Marshal(map[string]any{"outbounds": []any{out}})
		}
		if format == "xray" {
			return nil, unsupported(format)
		}
		return externalClientJSON(s, identity, endpoint)
	default:
		return nil, unsupported(format)
	}
}

func clientSNI(s Spec, ep Endpoint) (string, error) {
	name := ep.SNI
	if name == "" {
		name = str(s.Client, "serverName")
	}
	if name == "" {
		name = str(s.TLS, "serverName")
	}
	if name == "" && s.Protocol == "tuic" {
		name = str(obj(s.Settings, "server"), "sni")
	}
	if name == "" && s.Security == "reality" {
		for _, v := range texts(s.Reality["serverNames"]) {
			if v != "" && !strings.HasPrefix(v, "*.") {
				name = v
				break
			}
		}
	}
	if name == "" {
		name = ep.Address
	}
	if !validHost(name) {
		return "", invalid("client.serverName", "concrete_server_name_required")
	}
	if s.Security == "reality" {
		matched := false
		for _, allowed := range texts(s.Reality["serverNames"]) {
			if allowed == name {
				matched = true
			}
		}
		if !matched {
			return "", invalid("client.serverName", "sni_not_allowed_by_server")
		}
	}
	return name, nil
}

func clientTLS(s Spec, ep Endpoint) (map[string]any, error) {
	sni, e := clientSNI(s, ep)
	if e != nil {
		return nil, e
	}
	t := map[string]any{"serverName": sni}
	for _, k := range []string{"alpn", "minVersion", "maxVersion"} {
		if v, ok := s.TLS[k]; ok {
			t[k] = v
		}
	}
	for _, k := range []string{"fingerprint", "echConfigList", "verifyPeerCertByName"} {
		if v := str(s.Client, k); v != "" {
			t[k] = v
		}
	}
	if pins := texts(s.Client["pinnedPeerCertSha256"]); len(pins) > 0 {
		t["pinnedPeerCertSha256"] = strings.Join(pins, ",")
	}
	return t, nil
}

func clientReality(s Spec, ep Endpoint) (map[string]any, error) {
	sni, e := clientSNI(s, ep)
	if e != nil {
		return nil, e
	}
	pub, e := realityPublic(s)
	if e != nil {
		return nil, e
	}
	id := str(s.Client, "shortId")
	if id == "" {
		ids := texts(s.Reality["shortIds"])
		if len(ids) > 0 {
			id = ids[0]
		}
	}
	fp := str(s.Client, "fingerprint")
	if fp == "" {
		fp = "chrome"
	}
	spider := str(s.Client, "spiderX")
	if spider == "" {
		spider = "/"
	}
	r := map[string]any{"serverName": sni, "publicKey": pub, "shortId": id, "fingerprint": fp, "spiderX": spider}
	if v := str(s.Client, "mldsa65Verify"); v != "" {
		r["mldsa65Verify"] = v
	} else if str(s.Reality, "mldsa65Seed") != "" {
		return nil, invalid("client.mldsa65Verify", "mldsa_verification_key_required")
	}
	return r, nil
}

func clientFinalMask(s Spec) map[string]any {
	fm := cloneMap(obj(s.Stream, "finalmask"))
	stripMaskIDs(fm)
	if q := obj(fm, "quicParams"); q != nil {
		if hop := obj(q, "udpHop"); hop != nil {
			udp := list(fm["udp"])
			udp = append(udp, map[string]any{"type": "udphop", "settings": map[string]any{"mode": "intervalRemote", "remotePorts": str(hop, "ports"), "interval": hop["interval"]}})
			fm["udp"] = udp
			delete(q, "udpHop")
		}
	}
	return fm
}

func clientStream(s Spec, u User, ep Endpoint) (map[string]any, error) {
	out := map[string]any{"network": s.Network, "security": s.Security}
	if source := obj(s.Stream, s.Network+"Settings"); source != nil {
		t := cloneMap(source)
		delete(t, "acceptProxyProtocol")
		switch s.Network {
		case "hysteria":
			delete(t, "masquerade")
			t["auth"] = PasswordFor(s, u)
		case "masque":
			t["user"] = u.Email
			t["pass"] = PasswordFor(s, u)
			if v := str(s.Client, "host"); v != "" {
				t["host"] = v
			}
			if v := str(s.Client, "path"); v != "" {
				t["path"] = v
			}
			if headers := obj(s.Client, "headers"); headers != nil {
				t["headers"] = cloneMap(headers)
			}
		case "xhttp":
			for _, k := range []string{"scMaxBufferedPosts", "scStreamUpServerSecs", "serverMaxHeaderBytes"} {
				delete(t, k)
			}
		}
		out[s.Network+"Settings"] = t
	}
	if s.Network == "xdrive" {
		for _, k := range []string{"address", "port"} {
			if v, ok := s.Stream[k]; ok {
				out[k] = v
			}
		}
	}
	if fm := clientFinalMask(s); len(fm) > 0 {
		out["finalmask"] = fm
	}
	var e error
	if s.Security == "tls" {
		out["tlsSettings"], e = clientTLS(s, ep)
	}
	if s.Security == "reality" {
		out["realitySettings"], e = clientReality(s, ep)
	}
	return out, e
}

func clientXrayOutbound(s Spec, u User, ep Endpoint) (map[string]any, error) {
	protocol := s.Protocol
	if protocol == "mixed" {
		protocol = "socks"
	}
	tag := ep.Name
	if tag == "" {
		tag = "proxy"
	}
	out := map[string]any{"tag": tag, "protocol": protocol}
	settings := map[string]any{}
	server := map[string]any{"address": ep.Address, "port": ep.Port}
	switch s.Protocol {
	case "vless", "vmess":
		account := map[string]any{"id": u.UUID}
		if s.Protocol == "vless" {
			enc := str(s.Client, "encryption")
			if enc == "" {
				enc = "none"
			}
			if str(s.Settings, "decryption") != "none" && enc == "none" {
				return nil, invalid("client.encryption", "vless_client_encryption_required")
			}
			account["encryption"] = enc
			if flow := str(s.Client, "flow"); flow != "" {
				account["flow"] = flow
			}
		} else {
			security := str(s.Client, "security")
			if security == "" {
				security = "auto"
			}
			account["security"] = security
			account["alterId"] = 0
		}
		server["users"] = []any{account}
		settings["vnext"] = []any{server}
	case "trojan":
		server["password"] = PasswordFor(s, u)
		settings["servers"] = []any{server}
	case "shadowsocks":
		method := str(s.Settings, "method")
		password := PasswordFor(s, u)
		if strings.HasPrefix(method, "2022-blake3-aes-") {
			password = str(s.Settings, "password") + ":" + password
		}
		server["method"] = method
		server["password"] = password
		settings["servers"] = []any{server}
	case "http", "mixed":
		if s.Protocol != "mixed" || str(s.Settings, "auth") != "noauth" {
			server["users"] = []any{map[string]any{"user": u.Email, "pass": PasswordFor(s, u)}}
		}
		settings["servers"] = []any{server}
	case "hysteria":
		settings = server
		settings["version"] = 2
	case "masque":
		settings = server
		if dns := texts(s.Client["dns"]); len(dns) > 0 {
			settings["remoteDNS"] = dns
		}
	case "wireguard":
		privateKey, _, e := DeriveWireGuardIdentity(s, u)
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
		allowed := texts(s.Client["allowedIPs"])
		if len(allowed) == 0 {
			allowed = []string{"0.0.0.0/0", "::/0"}
		}
		peer := map[string]any{"publicKey": pub, "endpoint": net.JoinHostPort(ep.Address, strconv.Itoa(int(ep.Port))), "allowedIPs": allowed}
		if v := str(s.Client, "preSharedKey"); v != "" {
			peer["preSharedKey"] = v
		}
		if k := num(s.Client, "keepAlive"); k > 0 {
			peer["keepAlive"] = k
		}
		settings = map[string]any{"secretKey": privateKey, "address": addresses, "peers": []any{peer}, "noKernelTun": true}
		for _, k := range []string{"mtu", "reserved"} {
			if v, ok := s.Settings[k]; ok {
				settings[k] = v
			}
		}
		if dns := texts(s.Client["dns"]); len(dns) > 0 {
			settings["remoteDNS"] = dns
		} else if v, ok := s.Settings["dns"]; ok {
			switch x := v.(type) {
			case string:
				if x != "" {
					settings["remoteDNS"] = strings.Split(x, ",")
				}
			case []any:
				settings["remoteDNS"] = texts(x)
			}
		}
		if fm := clientFinalMask(s); len(fm) > 0 {
			out["streamSettings"] = map[string]any{"finalmask": fm}
		}
	default:
		return nil, unsupported("xray")
	}
	out["settings"] = settings
	if !has([]string{"http", "mixed", "wireguard"}, s.Protocol) {
		stream, e := clientStream(s, u, ep)
		if e != nil {
			return nil, e
		}
		out["streamSettings"] = stream
	}
	if m := obj(s.Client, "mux"); m != nil {
		out["mux"] = cloneMap(m)
	}
	return out, nil
}

func externalClientJSON(s Spec, u User, ep Endpoint) ([]byte, error) {
	switch s.Protocol {
	case "tuic":
		return exportSingBox(s, u, ep)
	case "mtproto":
		return json.Marshal(map[string]any{"format": "mtproto-client-v1", "server": ep.Address, "port": ep.Port, "secret": UserSecret(s, u), "name": ep.Name})
	case "amneziawg":
		conf, e := exportWireGuard(s, u, ep)
		if e != nil {
			return nil, e
		}
		return json.Marshal(map[string]any{"format": "amneziawg-conf-v1", "name": ep.Name, "config": string(conf)})
	}
	return nil, unsupported("json")
}

func hasWireGuardReserved(s Spec) bool {
	for _, value := range list(s.Settings["reserved"]) {
		if n, ok := number(value); ok && n != 0 {
			return true
		}
	}
	return false
}

func exportWireGuard(s Spec, u User, ep Endpoint) ([]byte, error) {
	if s.Protocol == "wireguard" && len(clientFinalMask(s)) > 0 {
		return nil, unsupported("wireguard with Xray FinalMask; use xray")
	}
	if s.Protocol == "wireguard" && hasWireGuardReserved(s) {
		return nil, unsupported("wireguard with Xray reserved bytes; use xray")
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
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\n", priv, strings.Join(addresses, ", "))
	dns := texts(s.Client["dns"])
	source := s.Settings
	if s.Protocol == "amneziawg" {
		source = obj(s.Settings, "server")
		if len(dns) == 0 {
			for _, k := range []string{"primaryDns", "secondaryDns"} {
				if v := str(source, k); v != "" {
					dns = append(dns, v)
				}
			}
		}
	} else if len(dns) == 0 {
		switch d := s.Settings["dns"].(type) {
		case string:
			if d != "" {
				dns = strings.Split(d, ",")
			}
		case []any:
			dns = texts(d)
		}
	}
	if len(dns) > 0 {
		for _, d := range dns {
			if !validHost(strings.TrimSpace(d)) {
				return nil, invalid("client.dns", "dns_invalid")
			}
		}
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(dns, ", "))
	}
	if mtu := num(source, "mtu"); mtu > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", mtu)
	}
	if s.Protocol == "amneziawg" {
		for _, entry := range [][2]string{{"jc", "Jc"}, {"jmin", "Jmin"}, {"jmax", "Jmax"}, {"s1", "S1"}, {"s2", "S2"}, {"s3", "S3"}, {"s4", "S4"}, {"h1", "H1"}, {"h2", "H2"}, {"h3", "H3"}, {"h4", "H4"}, {"i1", "I1"}, {"i2", "I2"}, {"i3", "I3"}, {"i4", "I4"}, {"i5", "I5"}, {"headerProtectionKey", "HeaderProtectionKey"}, {"contentPaddingAddition", "ContentPaddingAddition"}, {"rekeyAfterTime", "RekeyAfterTime"}, {"rekeyTimeout", "RekeyTimeout"}, {"rejectAfterTime", "RejectAfterTime"}, {"keepaliveTimeout", "KeepaliveTimeout"}, {"maxHandshakeAttempts", "MaxHandshakeAttempts"}, {"randomTrailers", "RandomTrailers"}, {"disableCookies", "DisableCookies"}} {
			v, exists := source[entry[0]]
			if !exists {
				continue
			}
			value := fmt.Sprint(v)
			if value == "" {
				continue
			}
			if strings.ContainsAny(value, "\r\n") {
				return nil, invalid("settings.server."+entry[0], "single_line_value_required")
			}
			fmt.Fprintf(&b, "%s = %s\n", entry[1], value)
		}
	}
	allowed := texts(s.Client["allowedIPs"])
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nAllowedIPs = %s\nEndpoint = %s\n", pub, strings.Join(allowed, ", "), net.JoinHostPort(ep.Address, strconv.Itoa(int(ep.Port))))
	if p := str(s.Client, "preSharedKey"); p != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", p)
	}
	if n := num(s.Client, "keepAlive"); n > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", n)
	}
	return []byte(b.String()), nil
}

func exportSingBox(s Spec, u User, ep Endpoint) ([]byte, error) {
	// No lossy conversion of Xray-only transports, masks, or VLESS encryption.
	if !has([]string{"vless", "vmess", "trojan", "shadowsocks", "http", "mixed", "hysteria", "tuic"}, s.Protocol) || len(clientFinalMask(s)) > 0 || has([]string{"kcp", "xhttp", "xdrive"}, s.Network) || obj(s.Client, "mux") != nil {
		return nil, unsupported("sing-box")
	}
	if s.Protocol == "vless" && str(s.Settings, "decryption") != "none" {
		return nil, unsupported("sing-box VLESS encryption")
	}
	p := s.Protocol
	if p == "mixed" {
		p = "socks"
	}
	if p == "hysteria" {
		p = "hysteria2"
	}
	o := map[string]any{"type": p, "tag": "proxy", "server": ep.Address, "server_port": ep.Port}
	if ep.Name != "" {
		o["tag"] = ep.Name
	}
	switch s.Protocol {
	case "vless":
		o["uuid"] = u.UUID
		if flow := str(s.Client, "flow"); flow != "" {
			o["flow"] = strings.TrimSuffix(flow, "-udp443")
		}
	case "vmess":
		o["uuid"] = u.UUID
		v := str(s.Client, "security")
		if v == "" {
			v = "auto"
		}
		o["security"] = v
		o["alter_id"] = 0
	case "trojan", "hysteria":
		o["password"] = PasswordFor(s, u)
	case "shadowsocks":
		o["method"] = str(s.Settings, "method")
		password := PasswordFor(s, u)
		if strings.HasPrefix(str(s.Settings, "method"), "2022-blake3-aes-") {
			password = str(s.Settings, "password") + ":" + password
		}
		o["password"] = password
	case "http", "mixed":
		if s.Protocol != "mixed" || str(s.Settings, "auth") != "noauth" {
			o["username"] = u.Email
			o["password"] = PasswordFor(s, u)
		}
	case "tuic":
		o["uuid"] = u.UUID
		o["password"] = PasswordFor(s, u)
		server := obj(s.Settings, "server")
		o["congestion_control"] = str(server, "congestion_control")
		o["udp_relay_mode"] = str(server, "udp_relay_mode")
		o["zero_rtt_handshake"] = boolean(server, "zero_rtt_handshake")
	}
	if s.Security != "none" {
		name, e := clientSNI(s, ep)
		if e != nil {
			return nil, e
		}
		t := map[string]any{"enabled": true, "server_name": name}
		if str(s.Client, "echConfigList") != "" || len(texts(s.Client["pinnedPeerCertSha256"])) > 0 || str(s.Client, "verifyPeerCertByName") != "" || str(s.Client, "mldsa65Verify") != "" {
			return nil, unsupported("sing-box TLS extensions")
		}
		if a := texts(s.TLS["alpn"]); len(a) > 0 {
			t["alpn"] = a
		}
		if s.Protocol == "tuic" {
			t["alpn"] = texts(obj(s.Settings, "server")["alpn"])
			if boolean(s.Client, "allowInsecure") {
				t["insecure"] = true
			}
		}
		if fp := str(s.Client, "fingerprint"); fp != "" {
			t["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		if s.Security == "reality" {
			r, e := clientReality(s, ep)
			if e != nil {
				return nil, e
			}
			if str(r, "mldsa65Verify") != "" {
				return nil, unsupported("sing-box MLDSA65")
			}
			t["reality"] = map[string]any{"enabled": true, "public_key": r["publicKey"], "short_id": r["shortId"]}
			if t["utls"] == nil {
				t["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
			}
		}
		o["tls"] = t
	}
	if has([]string{"ws", "grpc", "httpupgrade"}, s.Network) {
		src := obj(s.Stream, s.Network+"Settings")
		tr := map[string]any{"type": s.Network}
		switch s.Network {
		case "grpc":
			if boolean(src, "multiMode") {
				return nil, unsupported("sing-box gRPC multiMode")
			}
			if num(src, "idle_timeout") > 0 || num(src, "health_check_timeout") > 0 || num(src, "initial_windows_size") > 0 || boolean(src, "permit_without_stream") || str(src, "user_agent") != "" {
				return nil, unsupported("sing-box gRPC tuning; use xray")
			}
			tr["service_name"] = str(src, "serviceName")
			if str(src, "authority") != "" {
				return nil, unsupported("sing-box gRPC authority")
			}
		case "ws", "httpupgrade":
			if num(src, "heartbeatPeriod") > 0 {
				return nil, unsupported("sing-box WebSocket heartbeat; use xray")
			}
			tr["path"] = str(src, "path")
			headers := cloneMap(obj(src, "headers"))
			if h := str(src, "host"); h != "" {
				headers["Host"] = h
			}
			if len(headers) > 0 {
				tr["headers"] = headers
			}
		}
		o["transport"] = tr
	}
	if s.Network == "tcp" && str(obj(obj(s.Stream, "tcpSettings"), "header"), "type") == "http" {
		return nil, unsupported("sing-box RAW HTTP header")
	}
	return json.Marshal(map[string]any{"outbounds": []any{o}})
}
