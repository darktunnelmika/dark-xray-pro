package inboundspec

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"regexp"
	"strings"
)

var certFields = map[string]rule{
	"id":          shortText(),
	"certificate": array(textRule(), 2048), "key": array(textRule(), 2048), "certificateFile": shortText(), "keyFile": shortText(),
	"usage": enum("encipherment", "verify", "issue"), "ocspStapling": integer(0, 31536000), "oneTimeLoading": flag(), "buildChain": flag(),
}
var tlsFields = map[string]rule{
	"serverName": shortText(), "minVersion": enum("", "1.0", "1.1", "1.2", "1.3"), "maxVersion": enum("", "1.0", "1.1", "1.2", "1.3"),
	"cipherSuites": shortText(), "rejectUnknownSni": flag(), "disableSystemRoot": flag(), "enableSessionResumption": flag(),
	"certificates": array(object(certFields), 32), "alpn": array(enum("h3", "h2", "http/1.1"), 3), "echServerKeys": textRule(),
	"curvePreferences": stringList(), "masterKeyLog": shortText(), "useSystemCA": flag(),
}
var limitFallbackRule = object(map[string]rule{"afterBytes": integer(0, 1<<53), "bytesPerSec": integer(0, 1<<53), "burstBytesPerSec": integer(0, 1<<53)})
var realityFields = map[string]rule{
	"show": flag(), "xver": integer(0, 2), "target": oneof(shortText(), integer(1, 65535)), "serverNames": array(shortText(), 64), "privateKey": shortText(),
	"minClientVer": shortText(), "maxClientVer": shortText(), "maxTimediff": integer(0, 1<<53), "shortIds": array(shortText(), 64), "mldsa65Seed": shortText(),
	"masterKeyLog": shortText(), "limitFallbackUpload": limitFallbackRule, "limitFallbackDownload": limitFallbackRule,
}

var fingerprints = []string{"", "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized", "randomizednoalpn", "unsafe"}
var clientFields = map[string]rule{
	"fingerprint": enum(fingerprints...), "serverName": shortText(), "flow": enum("", "xtls-rprx-vision", "xtls-rprx-vision-udp443"), "publicKey": shortText(), "shortId": shortText(), "spiderX": shortText(), "mldsa65Verify": textRule(),
	"encryption": textRule(), "security": enum("auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero"), "alterId": integer(0, 0),
	"echConfigList": textRule(), "pinnedPeerCertSha256": array(shortText(), 32), "verifyPeerCertByName": shortText(), "allowInsecure": flag(),
	"mux":          object(map[string]rule{"enabled": flag(), "concurrency": integer(-1, 1024), "xudpConcurrency": integer(-1, 1024), "xudpProxyUDP443": enum("reject", "allow", "skip")}),
	"preSharedKey": shortText(), "keepAlive": integer(0, 65535), "allowedIPs": stringList(), "dns": stringList(),
	"host": shortText(), "headers": dictionary(shortText()), "path": shortText(), "adTag": shortText(),
}

func validWGKey(key string) bool {
	b, e := base64.StdEncoding.DecodeString(key)
	return e == nil && len(b) == 32 && !allZero(b)
}
func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
func decodeRealityKey(key string) ([]byte, error) {
	b, e := base64.RawURLEncoding.DecodeString(key)
	if e != nil || len(b) != 32 || allZero(b) {
		return nil, ErrInvalid
	}
	return b, nil
}
func validateSSKey(method, password string) error {
	if password == "" {
		return invalid("settings.password", "password_required")
	}
	if strings.HasPrefix(method, "2022-") {
		n := 32
		if method == "2022-blake3-aes-128-gcm" {
			n = 16
		}
		b, e := base64.StdEncoding.DecodeString(password)
		if e != nil || len(b) != n {
			return invalid("settings.password", "shadowsocks_key_invalid")
		}
	}
	return nil
}
func validFileRef(path string) bool {
	return path != "" && len(path) <= 4096 && !strings.ContainsAny(path, "\x00\r\n") && filepath.IsAbs(path)
}

func validateCertificate(cert map[string]any, path string) error {
	usage := str(cert, "usage")
	if usage == "" {
		usage = "encipherment"
		cert["usage"] = usage
	}
	inlineCert := strings.Join(texts(cert["certificate"]), "\n")
	inlineKey := strings.Join(texts(cert["key"]), "\n")
	cf, kf := str(cert, "certificateFile"), str(cert, "keyFile")
	if cf != "" || kf != "" {
		if inlineCert != "" || inlineKey != "" {
			return invalid(path, "certificate_mode_conflict")
		}
		if !validFileRef(cf) {
			return invalid(path+".certificateFile", "absolute_certificate_path_required")
		}
		if usage != "verify" && !validFileRef(kf) {
			return invalid(path+".keyFile", "absolute_key_path_required")
		}
		return nil
	}
	if inlineCert == "" {
		return invalid(path+".certificate", "certificate_required")
	}
	rest := []byte(inlineCert)
	count := 0
	for len(strings.TrimSpace(string(rest))) > 0 {
		block, next := pem.Decode(rest)
		if block == nil || block.Type != "CERTIFICATE" {
			return invalid(path+".certificate", "certificate_pem_invalid")
		}
		if _, e := x509.ParseCertificate(block.Bytes); e != nil {
			return invalid(path+".certificate", "certificate_invalid")
		}
		count++
		rest = next
	}
	if count == 0 {
		return invalid(path+".certificate", "certificate_required")
	}
	if usage != "verify" {
		if _, e := tls.X509KeyPair([]byte(inlineCert), []byte(inlineKey)); e != nil {
			return invalid(path+".key", "certificate_key_mismatch")
		}
	}
	return nil
}

func validateSecurity(s *Spec) error {
	if s.Security != "tls" && len(s.TLS) > 0 {
		return invalid("tls", "inactive_security_fields")
	}
	if s.Security != "reality" && len(s.Reality) > 0 {
		return invalid("reality", "inactive_security_fields")
	}
	if s.Security == "tls" {
		fields := addFields(tlsFields, map[string]rule{"echSockopt": object(sockoptFields)})
		if e := checkObject(s.TLS, fields, "tls"); e != nil {
			return e
		}
		putDefault(s.TLS, "minVersion", "1.2")
		putDefault(s.TLS, "maxVersion", "1.3")
		if min, max := str(s.TLS, "minVersion"), str(s.TLS, "maxVersion"); min != "" && max != "" && min > max {
			return invalid("tls.maxVersion", "tls_version_order")
		}
		if s.Network == "hysteria" || s.Network == "masque" || s.Network == "tuic" {
			if max := str(s.TLS, "maxVersion"); max != "" && max != "1.3" {
				return invalid("tls.maxVersion", "quic_requires_tls13")
			}
			s.TLS["minVersion"] = "1.3"
		}
		if _, ok := s.TLS["alpn"]; !ok {
			if has([]string{"hysteria", "masque", "tuic"}, s.Protocol) {
				s.TLS["alpn"] = []any{"h3"}
			} else if s.Network == "ws" {
				s.TLS["alpn"] = []any{"http/1.1"}
			} else {
				s.TLS["alpn"] = []any{"h2", "http/1.1"}
			}
		}
		if s.Network == "ws" {
			for _, a := range texts(s.TLS["alpn"]) {
				if a != "http/1.1" {
					return invalid("tls.alpn", "websocket_requires_http11")
				}
			}
		}
		certs := list(s.TLS["certificates"])
		hasServerCert := false
		for i, c := range certs {
			m := c.(map[string]any)
			if err := validateCertificate(m, fmt.Sprintf("tls.certificates.%d", i)); err != nil {
				return err
			}
			if str(m, "usage") != "verify" {
				hasServerCert = true
			}
		}
		if s.Protocol == "tuic" {
			a := obj(s.Settings, "server")
			if str(a, "certificate") != "" && str(a, "private_key") != "" {
				if !validFileRef(str(a, "certificate")) || !validFileRef(str(a, "private_key")) {
					return invalid("settings.server", "tuic_certificate_paths_required")
				}
				hasServerCert = true
			}
		}
		if !hasServerCert {
			return invalid("tls.certificates", "server_certificate_required")
		}
		if keys := str(s.TLS, "echServerKeys"); keys != "" {
			if b, e := base64.StdEncoding.DecodeString(keys); e != nil || len(b) == 0 {
				return invalid("tls.echServerKeys", "ech_key_encoding_invalid")
			}
		}
	}
	if s.Security == "reality" {
		if e := checkObject(s.Reality, realityFields, "reality"); e != nil {
			return e
		}
		b, e := decodeRealityKey(str(s.Reality, "privateKey"))
		if e != nil {
			return invalid("reality.privateKey", "private_key_required")
		}
		key, e := ecdh.X25519().NewPrivateKey(b)
		if e != nil {
			return invalid("reality.privateKey", "private_key_invalid")
		}
		pub := base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
		if x := str(s.Client, "publicKey"); x != "" && x != pub {
			return invalid("client.publicKey", "reality_public_key_mismatch")
		}
		s.Client["publicKey"] = pub
		if target, ok := s.Reality["target"].(string); ok {
			if !validHostPort(target) && !strings.HasPrefix(target, "/") && !strings.HasPrefix(target, "@") {
				return invalid("reality.target", "target_invalid")
			}
		} else if n, ok := number(s.Reality["target"]); !ok || n < 1 || n > 65535 {
			return invalid("reality.target", "target_required")
		}
		names := texts(s.Reality["serverNames"])
		if len(names) == 0 {
			return invalid("reality.serverNames", "server_names_required")
		}
		for _, name := range names {
			if name != "" && !validHost(name) {
				return invalid("reality.serverNames", "server_name_invalid")
			}
		}
		ids := texts(s.Reality["shortIds"])
		if len(ids) == 0 {
			return invalid("reality.shortIds", "short_ids_required")
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if len(id) > 16 || len(id)%2 != 0 || seen[id] {
				return invalid("reality.shortIds", "short_id_invalid")
			}
			if _, e := hex.DecodeString(id); e != nil {
				return invalid("reality.shortIds", "short_id_invalid")
			}
			seen[id] = true
		}
		for _, field := range []string{"minClientVer", "maxClientVer"} {
			if v := str(s.Reality, field); v != "" && !regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`).MatchString(v) {
				return invalid("reality."+field, "client_version_invalid")
			}
		}
		if seed := str(s.Reality, "mldsa65Seed"); seed != "" {
			if _, e := decodeRealityKey(seed); e != nil || seed == str(s.Reality, "privateKey") {
				return invalid("reality.mldsa65Seed", "seed_invalid")
			}
		}
	}
	return nil
}

func validateClient(s *Spec) error {
	if e := checkObject(s.Client, clientFields, "client"); e != nil {
		return e
	}
	if v := str(s.Client, "serverName"); v != "" && !validHost(v) {
		return invalid("client.serverName", "server_name_invalid")
	}
	if boolean(s.Client, "allowInsecure") && s.Protocol != "tuic" {
		return invalid("client.allowInsecure", "removed_by_engine_use_certificate_pinning")
	}
	if v := str(s.Client, "flow"); v != "" {
		if s.Protocol != "vless" {
			return invalid("client.flow", "vless_required")
		}
		encrypted := str(s.Settings, "decryption") != "none" && str(s.Settings, "decryption") != ""
		if !encrypted && (s.Network != "tcp" || !has([]string{"tls", "reality"}, s.Security)) {
			return invalid("client.flow", "vision_requires_tcp_security_or_vless_encryption")
		}
	}
	if x := obj(s.Client, "mux"); boolean(x, "enabled") && (s.Network == "xhttp" || str(s.Client, "flow") != "" || !has([]string{"vmess", "vless", "trojan", "shadowsocks", "http", "mixed"}, s.Protocol)) {
		return invalid("client.mux", "mux_not_supported")
	}
	if fp := str(s.Client, "fingerprint"); fp != "" && has([]string{"hysteria", "masque", "tuic"}, s.Protocol) {
		return invalid("client.fingerprint", "utls_not_supported_for_quic")
	}
	if s.Security == "reality" && str(s.Client, "fingerprint") == "unsafe" {
		return invalid("client.fingerprint", "reality_fingerprint_invalid")
	}
	if spider := str(s.Client, "spiderX"); spider != "" && (!strings.HasPrefix(spider, "/") || strings.ContainsAny(spider, "\r\n")) {
		return invalid("client.spiderX", "absolute_path_required")
	}
	if v := str(s.Client, "publicKey"); v != "" {
		if _, e := decodeRealityKey(v); e != nil {
			return invalid("client.publicKey", "public_key_invalid")
		}
	}
	if v := str(s.Client, "mldsa65Verify"); v != "" {
		b, e := base64.RawURLEncoding.DecodeString(v)
		if e != nil || len(b) != 1952 {
			return invalid("client.mldsa65Verify", "mldsa_public_key_invalid")
		}
	}
	if v := str(s.Client, "shortId"); v != "" {
		if s.Security != "reality" || !has(texts(s.Reality["shortIds"]), v) {
			return invalid("client.shortId", "short_id_not_allowed")
		}
	}
	if values, exists := s.Client["pinnedPeerCertSha256"]; exists {
		pins := []any{}
		for _, pin := range texts(values) {
			v, ok := normalizePin(pin)
			if !ok {
				return invalid("client.pinnedPeerCertSha256", "sha256_pin_invalid")
			}
			pins = append(pins, v)
		}
		s.Client["pinnedPeerCertSha256"] = pins
	}
	for _, cidr := range texts(s.Client["allowedIPs"]) {
		if _, e := netip.ParsePrefix(cidr); e != nil {
			return invalid("client.allowedIPs", "cidr_required")
		}
	}
	if p := str(s.Client, "preSharedKey"); p != "" && !validWGKey(p) {
		return invalid("client.preSharedKey", "preshared_key_invalid")
	}
	return nil
}

func normalizePin(pin string) (string, bool) {
	s := strings.ReplaceAll(strings.TrimSpace(pin), ":", "")
	if b, e := hex.DecodeString(s); e == nil && len(b) == 32 {
		return hex.EncodeToString(b), true
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if b, e := encoding.DecodeString(pin); e == nil && len(b) == 32 {
			return hex.EncodeToString(b), true
		}
	}
	return "", false
}

// EnsureSecrets is an explicit CREATE helper. Normalize and MergeSecrets never
// call it. Certificate ownership/issuance remains with the caller.
func EnsureSecrets(input Spec) (Spec, error) {
	s, e := cloneSpec(input)
	if e != nil {
		return Spec{}, e
	}
	if s.Settings == nil {
		s.Settings = map[string]any{}
	}
	if s.Reality == nil {
		s.Reality = map[string]any{}
	}
	randomKey := func(n int, url bool) (string, error) {
		b := make([]byte, n)
		if _, e := rand.Read(b); e != nil {
			return "", e
		}
		if url {
			return base64.RawURLEncoding.EncodeToString(b), nil
		}
		return base64.StdEncoding.EncodeToString(b), nil
	}
	fill := func(m map[string]any, k string, n int, url bool) error {
		if str(m, k) != "" {
			return nil
		}
		v, e := randomKey(n, url)
		if e != nil {
			return e
		}
		m[k] = v
		return nil
	}
	if s.Security == "reality" {
		if e = fill(s.Reality, "privateKey", 32, true); e != nil {
			return Spec{}, e
		}
		if len(texts(s.Reality["shortIds"])) == 0 {
			b := make([]byte, 8)
			if _, e = rand.Read(b); e != nil {
				return Spec{}, e
			}
			s.Reality["shortIds"] = []any{hex.EncodeToString(b)}
		}
	}
	switch s.Protocol {
	case "wireguard":
		e = fill(s.Settings, "secretKey", 32, false)
	case "amneziawg":
		a := obj(s.Settings, "server")
		if a == nil {
			a = map[string]any{}
			s.Settings["server"] = a
		}
		e = fill(a, "privateKey", 32, false)
	case "shadowsocks":
		n := 32
		if str(s.Settings, "method") == "2022-blake3-aes-128-gcm" {
			n = 16
		}
		e = fill(s.Settings, "password", n, false)
	case "mtproto":
		e = fill(s.Settings, "secret", 32, true)
	}
	if e != nil {
		return Spec{}, e
	}
	if e = assignRowIDs(&s, true); e != nil {
		return Spec{}, e
	}
	return s, nil
}

func realityPublic(s Spec) (string, error) {
	b, e := decodeRealityKey(str(s.Reality, "privateKey"))
	if e != nil {
		return "", e
	}
	k, e := ecdh.X25519().NewPrivateKey(b)
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

func hostWithoutPort(v string) string {
	if h, _, e := net.SplitHostPort(v); e == nil {
		return h
	}
	return v
}
