package inboundspec

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

func validateUsers(users []User) error {
	if len(users) > 20000 {
		return invalid("users", "too_many_users")
	}
	ids, emails := map[string]bool{}, map[string]bool{}
	for _, u := range users {
		if !uuidPattern.MatchString(u.UUID) || u.Email == "" || len(u.Email) > 254 || strings.ContainsAny(u.Email, "\x00\r\n") || len(u.Password) > 65536 || strings.ContainsRune(u.Password, 0) {
			return invalid("users", "identity_invalid")
		}
		key := strings.ToLower(u.UUID)
		email := strings.ToLower(u.Email)
		if ids[key] || emails[email] {
			return invalid("users", "duplicate_identity")
		}
		ids[key] = true
		emails[email] = true
	}
	return nil
}

func deriveMaterial(secret, purpose string, u User) []byte {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("dark-pro/inbound-spec/v1/" + purpose + "\x00"))
	h.Write([]byte(strings.ToLower(u.UUID)))
	return h.Sum(nil)
}

// PasswordFor uses the business-owned password or UUID. Shadowsocks 2022 keys
// are independently derived per account from the server key and UUID.
func PasswordFor(s Spec, u User) string {
	if u.Password != "" {
		return u.Password
	}
	if s.Protocol == "shadowsocks" && strings.HasPrefix(str(s.Settings, "method"), "2022-") {
		n := 32
		if str(s.Settings, "method") == "2022-blake3-aes-128-gcm" {
			n = 16
		}
		return base64.StdEncoding.EncodeToString(deriveMaterial(str(s.Settings, "password"), "shadowsocks-user", u)[:n])
	}
	return u.UUID
}

// UserSecret returns an MTProto FakeTLS secret; its suffix is the configured
// cover domain and its 128-bit secret is bound to this business identity.
func UserSecret(s Spec, u User) string {
	if u.Password != "" {
		return u.Password
	}
	return "ee" + hex.EncodeToString(deriveMaterial(str(s.Settings, "secret"), "mtproto-user", u)[:16]) + hex.EncodeToString([]byte(str(s.Settings, "fakeTlsDomain")))
}

func wireguardServerKey(s Spec) string {
	if s.Protocol == "amneziawg" {
		return str(obj(s.Settings, "server"), "privateKey")
	}
	return str(s.Settings, "secretKey")
}
func DeriveWireGuardIdentity(s Spec, u User) (privateKey, publicKey string, err error) {
	if !uuidPattern.MatchString(u.UUID) {
		return "", "", invalid("identity", "uuid_required")
	}
	secret := wireguardServerKey(s)
	if !validWGKey(secret) {
		return "", "", invalid("settings", "wireguard_server_key_required")
	}
	b := deriveMaterial(secret, "wireguard-peer", u)
	b[0] &= 248
	b[31] &= 127
	b[31] |= 64
	k, e := ecdh.X25519().NewPrivateKey(b)
	if e != nil {
		return "", "", e
	}
	return base64.StdEncoding.EncodeToString(b), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}
func WireGuardServerPublicKey(s Spec) (string, error) {
	b, e := base64.StdEncoding.DecodeString(wireguardServerKey(s))
	if e != nil {
		return "", ErrInvalid
	}
	k, e := ecdh.X25519().NewPrivateKey(b)
	if e != nil {
		return "", ErrInvalid
	}
	return base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

func wireguardSubnet(s Spec) (netip.Prefix, error) {
	m := s.Settings
	if s.Protocol == "amneziawg" {
		m = obj(m, "server")
	}
	a, e := netip.ParseAddr(str(m, "subnetIp"))
	bits := num(m, "subnetCidr")
	if e != nil || !a.Is4() || bits < 1 || bits > 30 {
		return netip.Prefix{}, ErrInvalid
	}
	return netip.PrefixFrom(a, int(bits)).Masked(), nil
}

// WireGuardAddresses produces a stable peer address. Explicit allocations take
// precedence. ID-based allocation never changes when another user is removed.
func WireGuardAddresses(s Spec, u User) ([]string, error) {
	p, e := wireguardSubnet(s)
	if e != nil {
		return nil, e
	}
	if len(u.Addresses) > 0 {
		out := append([]string(nil), u.Addresses...)
		for _, v := range out {
			a, e := netip.ParsePrefix(v)
			if e != nil || !p.Contains(a.Addr()) || !a.Addr().Is4() || a.Bits() != 32 {
				return nil, invalid("users.addresses", "peer_host_address_outside_subnet")
			}
		}
		return out, nil
	}
	capacity := (uint64(1) << uint(32-p.Bits())) - 3
	if capacity == 0 {
		return nil, invalid("settings.subnetCidr", "subnet_has_no_client_addresses")
	}
	var ipv6 netip.Prefix
	if s.Protocol == "amneziawg" && boolean(obj(s.Settings, "server"), "ipv6Enabled") {
		ipv6, e = netip.ParsePrefix(str(obj(s.Settings, "server"), "ipv6Subnet"))
		if e != nil || !ipv6.Addr().Is6() || ipv6.Addr().Is4In6() || ipv6.Bits() > 126 {
			return nil, invalid("settings.server.ipv6Subnet", "ipv6_subnet_too_small")
		}
		ipv6 = ipv6.Masked()
		if hostBits := uint(128 - ipv6.Bits()); hostBits < 64 {
			// Reserve the subnet address and the server at base+1. IPv6 has
			// no broadcast address, so a /126 has two usable peer addresses.
			v6Capacity := (uint64(1) << hostBits) - 2
			if v6Capacity < capacity {
				capacity = v6Capacity
			}
		}
	}
	id := u.ID
	if id == 0 {
		h := sha256.Sum256([]byte(strings.ToLower(u.UUID)))
		id = binary.BigEndian.Uint64(h[:8])%capacity + 1
	}
	if id > capacity {
		return nil, invalid("users.id", "subnet_capacity_exceeded")
	}
	a := p.Addr().As4()
	base := binary.BigEndian.Uint32(a[:])
	binary.BigEndian.PutUint32(a[:], base+uint32(id)+1)
	out := []string{netip.AddrFrom4(a).String() + "/32"}
	if ipv6.IsValid() {
		b := ipv6.Addr().As16()
		// Add to the masked base instead of replacing its low 64 bits: a
		// /65…/126 prefix may already own bits in that half of the address.
		offset := id + 1
		for i := len(b) - 1; i >= 0 && offset != 0; i-- {
			n := uint64(b[i]) + offset
			b[i], offset = byte(n), n>>8
		}
		peer := netip.AddrFrom16(b)
		if offset != 0 || !ipv6.Contains(peer) {
			return nil, invalid("users.id", "subnet_capacity_exceeded")
		}
		out = append(out, peer.String()+"/128")
	}
	return out, nil
}
func wireguardServerAddress(s Spec) (string, error) {
	p, e := wireguardSubnet(s)
	if e != nil {
		return "", e
	}
	return p.Addr().Next().String() + "/" + strconv.Itoa(p.Bits()), nil
}

func boundUsers(users []User) []any {
	out := make([]any, 0, len(users))
	for _, u := range users {
		out = append(out, map[string]any{"id": u.ID, "uuid": u.UUID, "email": u.Email})
	}
	return out
}

func Render(input Spec, listen string, port uint16, tag string, users []User) (Rendered, error) {
	s, e := Normalize(input)
	if e != nil {
		return Rendered{}, e
	}
	if len(tag) == 0 || len(tag) > 128 || strings.ContainsAny(tag, "\x00\r\n") {
		return Rendered{}, invalid("tag", "tag_invalid")
	}
	if s.Protocol != "tun" {
		if port == 0 {
			return Rendered{}, invalid("port", "port_required")
		}
		if listen == "" {
			listen = "0.0.0.0"
		}
		if net.ParseIP(listen) == nil && !strings.HasPrefix(listen, "/") && !strings.HasPrefix(listen, "@") {
			return Rendered{}, invalid("listen", "listen_address_invalid")
		}
	}
	if e = validateUsers(users); e != nil {
		return Rendered{}, e
	}
	info, _ := protocolInfo(s.Protocol)
	noAccounts := info.ListenerOnly || s.Protocol == "mixed" && str(s.Settings, "auth") == "noauth"
	if noAccounts && len(users) > 0 {
		return Rendered{}, invalid("users", "listener_has_no_accounts")
	}
	if !noAccounts && len(users) == 0 {
		return Rendered{}, invalid("users", "bound_user_required")
	}
	sorted := append([]User(nil), users...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].UUID < sorted[j].UUID })
	users = sorted
	manifest := map[string]any{"version": 1, "engine": info.Engine, "protocol": s.Protocol, "network": s.Network, "listen": listen, "port": port, "tag": tag, "user_bindings": boundUsers(users), "requires_engine_validation": true}
	if files := fileReferences(s); len(files) > 0 {
		manifest["file_references"] = files
	}
	if info.Engine != "xray" {
		return renderExternal(s, listen, port, tag, users, manifest)
	}
	settings := cloneMap(s.Settings)
	sniff := settings["sniffing"]
	delete(settings, "sniffing")
	in := map[string]any{"tag": tag, "protocol": s.Protocol, "settings": settings}
	if s.Protocol != "tun" {
		in["listen"] = listen
		in["port"] = port
	}
	if sniff != nil {
		in["sniffing"] = sniff
	}
	clients := make([]any, 0, len(users))
	switch s.Protocol {
	case "vless":
		for _, u := range users {
			x := map[string]any{"id": u.UUID, "email": u.Email, "level": 0}
			if flow := str(s.Client, "flow"); flow != "" {
				x["flow"] = strings.TrimSuffix(flow, "-udp443")
			}
			clients = append(clients, x)
		}
		settings["clients"] = clients
	case "vmess":
		for _, u := range users {
			clients = append(clients, map[string]any{"id": u.UUID, "email": u.Email, "level": 0, "alterId": 0})
		}
		settings["clients"] = clients
	case "trojan":
		for _, u := range users {
			clients = append(clients, map[string]any{"password": PasswordFor(s, u), "email": u.Email, "level": 0})
		}
		settings["clients"] = clients
	case "shadowsocks":
		delete(settings, "ivCheck")
		method := str(settings, "method")
		if method == "2022-blake3-chacha20-poly1305" || method == "none" || method == "plain" {
			if len(users) != 1 {
				return Rendered{}, invalid("users", "cipher_supports_one_user")
			}
			settings["password"] = PasswordFor(s, users[0])
			settings["email"] = users[0].Email
			break
		}
		for _, u := range users {
			password := PasswordFor(s, u)
			if strings.HasPrefix(method, "2022-") {
				if e := validateSSKey(method, password); e != nil {
					return Rendered{}, invalid("users.password", "shadowsocks_user_key_invalid")
				}
				clients = append(clients, map[string]any{"password": password, "email": u.Email, "level": 0})
			} else {
				clients = append(clients, map[string]any{"method": method, "password": password, "email": u.Email, "level": 0})
			}
		}
		settings["clients"] = clients
	case "hysteria":
		for _, u := range users {
			clients = append(clients, map[string]any{"auth": PasswordFor(s, u), "email": u.Email, "level": 0})
		}
		settings["clients"] = clients
	case "masque":
		for _, u := range users {
			clients = append(clients, map[string]any{"pass": PasswordFor(s, u), "email": u.Email, "level": 0})
		}
		settings["clients"] = clients
	case "http", "mixed":
		if s.Protocol == "mixed" && str(settings, "auth") == "noauth" {
			manifest["user_attribution"] = false
		} else {
			for _, u := range users {
				clients = append(clients, map[string]any{"user": u.Email, "pass": PasswordFor(s, u)})
			}
			settings["accounts"] = clients
			manifest["user_attribution"] = true
		}
	case "wireguard":
		peers, e := wireguardPeers(s, users)
		if e != nil {
			return Rendered{}, e
		}
		settings["peers"] = peers
		addr, e := wireguardServerAddress(s)
		if e != nil {
			return Rendered{}, e
		}
		settings["address"] = []string{addr}
		delete(settings, "subnetIp")
		delete(settings, "subnetCidr")
		delete(settings, "dns")
	case "tun":
		manifest["requires_net_admin"] = true
	}
	if !has([]string{"http", "mixed", "tun"}, s.Protocol) {
		if s.Protocol == "wireguard" || s.Protocol == "tunnel" {
			stream := cloneMap(s.Stream)
			delete(stream, "wireguardSettings")
			if len(stream) > 0 {
				in["streamSettings"] = stream
			}
		} else {
			in["streamSettings"] = engineStream(s)
		}
	}
	manifest["adapter"] = "xray-inbound-v1"
	return Rendered{Engine: "xray", Inbound: in, Manifest: manifest}, nil
}

func wireguardPeers(s Spec, users []User) ([]any, error) {
	peers := make([]any, 0, len(users))
	seen := map[string]bool{}
	for _, u := range users {
		_, pub, e := DeriveWireGuardIdentity(s, u)
		if e != nil {
			return nil, e
		}
		addresses, e := WireGuardAddresses(s, u)
		if e != nil {
			return nil, e
		}
		for _, ip := range addresses {
			if seen[ip] {
				return nil, invalid("users.addresses", "peer_address_collision")
			}
			seen[ip] = true
		}
		p := map[string]any{"publicKey": pub, "allowedIPs": addresses, "email": u.Email, "level": 0}
		if ps := str(s.Client, "preSharedKey"); ps != "" {
			p["preSharedKey"] = ps
		}
		if n := num(s.Client, "keepAlive"); n > 0 {
			p["keepAlive"] = n
		}
		peers = append(peers, p)
	}
	return peers, nil
}

func fileReferences(s Spec) []string {
	seen := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "certificateFile" || k == "keyFile" || k == "masterKeyLog" || k == "dir" {
					if p, ok := v.(string); ok && p != "" {
						seen[p] = true
					}
				}
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	for _, m := range specRoots(&s) {
		walk(m)
	}
	if s.Protocol == "tuic" {
		for _, k := range []string{"certificate", "private_key"} {
			if p := str(obj(s.Settings, "server"), k); p != "" {
				seen[p] = true
			}
		}
	}
	out := []string{}
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func renderExternal(s Spec, listen string, port uint16, tag string, users []User, manifest map[string]any) (Rendered, error) {
	if net.ParseIP(listen) == nil {
		return Rendered{}, invalid("listen", "external_engine_requires_ip_listener")
	}
	manifest["supervisor_owned_management"] = true
	switch s.Protocol {
	case "mtproto":
		cfg := map[string]any{"bind-to": net.JoinHostPort(listen, strconv.Itoa(int(port)))}
		for from, to := range map[string]string{"proxyProtocolListener": "proxy-protocol-listener", "preferIp": "prefer-ip", "debug": "debug", "publicIpv4": "public-ipv4", "publicIpv6": "public-ipv6"} {
			if v, ok := s.Settings[from]; ok {
				cfg[to] = v
			}
		}
		if d := obj(s.Settings, "domainFronting"); len(d) > 0 {
			df := map[string]any{}
			for a, b := range map[string]string{"ip": "host", "port": "port", "proxyProtocol": "proxy-protocol"} {
				if v, ok := d[a]; ok {
					df[b] = v
				}
			}
			cfg["domain-fronting"] = df
		}
		if n := num(s.Settings, "throttleMaxConnections"); n > 0 {
			cfg["throttle"] = map[string]any{"max-connections": n}
		}
		if n := num(s.Settings, "pendingHandshakesMaxPerIp"); n > 0 {
			cfg["defense"] = map[string]any{"pending-handshakes": map[string]any{"max-per-ip": n}}
		}
		secrets := map[string]any{}
		for _, u := range users {
			secret := UserSecret(s, u)
			if !validMTProtoSecret(secret) {
				return Rendered{}, invalid("users.password", "mtproto_secret_invalid")
			}
			secrets[u.Email] = secret
		}
		cfg["secrets"] = secrets
		if ad := str(s.Client, "adTag"); ad != "" {
			if b, e := hex.DecodeString(ad); e != nil || len(b) != 16 {
				return Rendered{}, invalid("client.adTag", "ad_tag_invalid")
			}
			tags := map[string]any{}
			for _, u := range users {
				tags[u.Email] = ad
			}
			cfg["secret-ad-tags"] = tags
		}
		if boolean(s.Settings, "routeThroughXray") {
			manifest["requires_socks_bridge"] = true
			manifest["outbound_tag"] = str(s.Settings, "outboundTag")
		}
		manifest["adapter"] = "mtg-multi-v1"
		manifest["config_format"] = "toml"
		return Rendered{Engine: "mtproto", Config: cfg, Manifest: manifest}, nil
	case "tuic":
		server := cloneMap(obj(s.Settings, "server"))
		clients := make([]any, 0, len(users))
		for _, u := range users {
			clients = append(clients, map[string]any{"uuid": u.UUID, "password": PasswordFor(s, u), "email": u.Email})
		}
		cfg := map[string]any{"listen": net.JoinHostPort(listen, strconv.Itoa(int(port))), "server": server, "clients": clients, "tls": cloneMap(s.TLS)}
		manifest["adapter"] = "tuic-v5-v1"
		manifest["config_format"] = "tuic-adapter-json"
		manifest["requires_udp"] = true
		return Rendered{Engine: "tuic", Config: cfg, Manifest: manifest}, nil
	case "amneziawg":
		peers, e := wireguardPeers(s, users)
		if e != nil {
			return Rendered{}, e
		}
		server := cloneMap(obj(s.Settings, "server"))
		pub, e := WireGuardServerPublicKey(s)
		if e != nil {
			return Rendered{}, e
		}
		server["publicKey"] = pub
		address, e := wireguardServerAddress(s)
		if e != nil {
			return Rendered{}, e
		}
		cfg := map[string]any{"listen": listen, "listenPort": port, "address": []string{address}, "server": server, "peers": peers}
		manifest["adapter"] = "amneziawg-v3.1-v1"
		manifest["config_format"] = "amneziawg-adapter-json"
		manifest["requires_udp"] = true
		manifest["requires_managed_tunnel_network"] = true
		return Rendered{Engine: "amneziawg", Config: cfg, Manifest: manifest}, nil
	}
	return Rendered{}, invalid("protocol", "external_adapter_missing")
}

func validMTProtoSecret(s string) bool {
	if !strings.HasPrefix(s, "ee") || len(s) <= 34 {
		return false
	}
	b, e := hex.DecodeString(s[2:])
	return e == nil && len(b) > 16 && validHost(string(b[16:]))
}

// RenderTOML is the mtg adapter's deterministic configuration serializer. It
// quotes all user-derived keys and values and never assembles shell commands.
func RenderTOML(config map[string]any) (string, error) {
	var b strings.Builder
	var section func(map[string]any, []string) error
	section = func(m map[string]any, path []string) error {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := m[k]
			if _, ok := v.(map[string]any); ok {
				continue
			}
			var value string
			switch x := v.(type) {
			case string:
				value = strconv.Quote(x)
			case bool:
				value = strconv.FormatBool(x)
			default:
				if n, ok := number(v); ok {
					value = strconv.FormatInt(n, 10)
				} else {
					return invalid("config", "toml_value_invalid")
				}
			}
			fmt.Fprintf(&b, "%s = %s\n", strconv.Quote(k), value)
		}
		for _, k := range keys {
			child, ok := m[k].(map[string]any)
			if !ok {
				continue
			}
			parts := append(append([]string(nil), path...), k)
			quoted := make([]string, len(parts))
			for i, p := range parts {
				quoted[i] = strconv.Quote(p)
			}
			fmt.Fprintf(&b, "\n[%s]\n", strings.Join(quoted, "."))
			if e := section(child, parts); e != nil {
				return e
			}
		}
		return nil
	}
	if e := section(config, nil); e != nil {
		return "", e
	}
	return b.String(), nil
}
