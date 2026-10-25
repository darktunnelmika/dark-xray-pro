// Package inboundspec describes private inbound configurations independently of
// the database, HTTP API, and runtime supervisor. It never starts a process or
// makes a network connection. All credential-bearing results are private.
package inboundspec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const Version = 1
const MaxSpecBytes = 1 << 20

var ErrInvalid = errors.New("invalid inbound specification")
var ErrUnsupportedFormat = errors.New("unsupported client export format")

// Spec is an encrypted-at-rest document. Do not serialize it into public API
// responses; use Public instead. Inner keys are the engine's canonical keys.
type Spec struct {
	Version  int            `json:"version"`
	Protocol string         `json:"protocol"`
	Network  string         `json:"network"`
	Security string         `json:"security"`
	Settings map[string]any `json:"settings,omitempty"`
	Stream   map[string]any `json:"stream,omitempty"`
	TLS      map[string]any `json:"tls,omitempty"`
	Reality  map[string]any `json:"reality,omitempty"`
	Client   map[string]any `json:"client,omitempty"`
}

type PublicSpec struct {
	Spec
	SecretPaths []string `json:"secret_paths"`
}

// User is supplied by the account store, never by Spec.Settings. Addresses can
// be assigned by a managed IP allocator; otherwise an ID-based address is used.
type User struct {
	ID        uint64   `json:"id,omitempty"`
	UUID      string   `json:"uuid"`
	Email     string   `json:"email"`
	Password  string   `json:"password,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
}

type Endpoint struct {
	Address string `json:"address"`
	Port    uint16 `json:"port"`
	Name    string `json:"name,omitempty"`
	SNI     string `json:"sni,omitempty"`
}

// Rendered describes one engine, not a promise that a machine has that engine.
// Xray uses Inbound. External engines use Config and a versioned adapter
// Manifest. Every field can contain private credentials.
type Rendered struct {
	Engine   string         `json:"engine"`
	Inbound  map[string]any `json:"inbound,omitempty"`
	Config   map[string]any `json:"config,omitempty"`
	Manifest map[string]any `json:"manifest,omitempty"`
}

type ProtocolInfo struct {
	Protocol      string   `json:"protocol"`
	Engine        string   `json:"engine"`
	Networks      []string `json:"networks"`
	Securities    []string `json:"securities"`
	ExportFormats []string `json:"export_formats"`
	UserBinding   bool     `json:"user_binding"`
	Dedicated     bool     `json:"dedicated"`
	ListenerOnly  bool     `json:"listener_only"`
}

type FieldError struct {
	Path string `json:"path"`
	Code string `json:"code"`
}

func (e *FieldError) Error() string   { return e.Code + ": " + e.Path }
func (e *FieldError) Unwrap() error   { return ErrInvalid }
func invalid(path, code string) error { return &FieldError{Path: path, Code: code} }

var genericNetworks = []string{"tcp", "kcp", "ws", "grpc", "httpupgrade", "xhttp", "xdrive"}

// Catalog returns independent slices so a caller cannot mutate validation.
func Catalog() []ProtocolInfo {
	var result []ProtocolInfo
	for _, p := range []string{"vless", "vmess", "trojan", "shadowsocks", "wireguard", "hysteria", "http", "mixed", "tunnel", "tun", "mtproto", "amneziawg", "tuic", "masque"} {
		x := ProtocolInfo{Protocol: p, Engine: "xray", Networks: []string{"tcp"}, Securities: []string{"none"}, UserBinding: true, ExportFormats: []string{"json", "xray", "uri"}}
		switch p {
		case "vless", "trojan":
			x.Networks = append([]string(nil), genericNetworks...)
			x.Securities = []string{"none", "tls", "reality"}
		case "vmess", "shadowsocks":
			x.Networks = append([]string(nil), genericNetworks...)
			x.Securities = []string{"none", "tls"}
		case "hysteria", "masque":
			x.Networks = []string{p}
			x.Securities = []string{"tls"}
			x.Dedicated = true
			if p == "masque" {
				x.ExportFormats = []string{"json", "xray"}
			}
		case "wireguard":
			x.Networks = []string{p}
			x.Dedicated = true
			x.ExportFormats = []string{"json", "xray", "wireguard", "uri"}
		case "tunnel":
			x.UserBinding = false
			x.ListenerOnly = true
			x.ExportFormats = []string{}
		case "tun":
			x.Networks = []string{p}
			x.UserBinding = false
			x.ListenerOnly = true
			x.Dedicated = true
			x.ExportFormats = []string{}
		case "mtproto", "amneziawg", "tuic":
			x.Engine = p
			x.Networks = []string{p}
			x.Dedicated = true
			x.ExportFormats = []string{"json", "uri"}
			if p == "amneziawg" {
				x.ExportFormats = []string{"json", "wireguard", "uri"}
			}
			if p == "tuic" {
				x.Securities = []string{"tls"}
			}
		}
		if has([]string{"vless", "vmess", "trojan", "shadowsocks", "http", "mixed", "hysteria", "tuic"}, p) {
			x.ExportFormats = append(x.ExportFormats, "sing-box")
		}
		result = append(result, x)
	}
	return result
}

func protocolInfo(protocol string) (ProtocolInfo, bool) {
	for _, v := range Catalog() {
		if v.Protocol == protocol {
			return v, true
		}
	}
	return ProtocolInfo{}, false
}

// Decode rejects unknown top-level keys, trailing JSON, and oversized input.
func Decode(raw []byte) (Spec, error) {
	var s Spec
	if len(raw) > MaxSpecBytes {
		return s, invalid("spec", "too_large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(&s); err != nil {
		return Spec{}, invalid("spec", "invalid_json")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return Spec{}, invalid("spec", "invalid_json")
	}
	return Normalize(s)
}

func cloneSpec(s Spec) (Spec, error) {
	b, err := json.Marshal(s)
	if err != nil || len(b) > MaxSpecBytes {
		return Spec{}, invalid("spec", "invalid_size_or_value")
	}
	var out Spec
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err = d.Decode(&out); err != nil {
		return Spec{}, ErrInvalid
	}
	return out, nil
}
func cloneMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	b, _ := json.Marshal(v)
	var out map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	_ = d.Decode(&out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}
func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func str(m map[string]any, k string) string         { v, _ := m[k].(string); return v }
func boolean(m map[string]any, k string) bool       { v, _ := m[k].(bool); return v }
func list(v any) []any                              { a, _ := v.([]any); return a }
func texts(v any) []string {
	var out []string
	switch a := v.(type) {
	case []string:
		return append([]string(nil), a...)
	case []any:
		for _, x := range a {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}
func putDefault(m map[string]any, k string, v any) {
	if _, ok := m[k]; !ok {
		m[k] = v
	}
}
func has(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
func number(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		i, e := n.Int64()
		return i, e == nil
	case int:
		return int64(n), true
	case int64:
		return n, true
	case uint64:
		if n <= 1<<53 {
			return int64(n), true
		}
	case float64:
		if n >= -1<<53 && n <= 1<<53 && n == float64(int64(n)) {
			return int64(n), true
		}
	}
	return 0, false
}
func num(m map[string]any, k string) int64 { n, _ := number(m[k]); return n }
func asStrings(v []string) []any {
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

func Validate(s Spec) error { _, err := Normalize(s); return err }

// Normalize is deterministic, deep-copies its input, and never creates keys.
// Apply MergeSecrets before this call when editing a redacted document.
func Normalize(input Spec) (Spec, error) {
	s, err := cloneSpec(input)
	if err != nil {
		return Spec{}, err
	}
	if s.Version == 0 {
		s.Version = Version
	}
	if s.Version != Version {
		return Spec{}, invalid("version", "unsupported_version")
	}
	s.Protocol = strings.ToLower(strings.TrimSpace(s.Protocol))
	s.Network = strings.ToLower(strings.TrimSpace(s.Network))
	s.Security = strings.ToLower(strings.TrimSpace(s.Security))
	if s.Network == "raw" {
		s.Network = "tcp"
	}
	info, ok := protocolInfo(s.Protocol)
	if !ok {
		return Spec{}, invalid("protocol", "unsupported_protocol")
	}
	if s.Network == "" {
		s.Network = info.Networks[0]
	}
	if s.Security == "" {
		s.Security = info.Securities[0]
	}
	if !has(info.Networks, s.Network) {
		return Spec{}, invalid("network", "protocol_transport_mismatch")
	}
	if !has(info.Securities, s.Security) {
		return Spec{}, invalid("security", "protocol_security_mismatch")
	}
	if s.Security == "tls" && has(genericNetworks, s.Network) && !has([]string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"}, s.Network) {
		return Spec{}, invalid("security", "transport_tls_mismatch")
	}
	if s.Security == "reality" && !has([]string{"tcp", "grpc", "xhttp"}, s.Network) {
		return Spec{}, invalid("security", "transport_reality_mismatch")
	}
	if s.Settings == nil {
		s.Settings = map[string]any{}
	}
	if s.Stream == nil {
		s.Stream = map[string]any{}
	}
	if s.TLS == nil {
		s.TLS = map[string]any{}
	}
	if s.Reality == nil {
		s.Reality = map[string]any{}
	}
	if s.Client == nil {
		s.Client = map[string]any{}
	}
	if v, ok := s.Client["fp"]; ok {
		if _, exists := s.Client["fingerprint"]; exists {
			return Spec{}, invalid("client.fingerprint", "duplicate_alias")
		}
		s.Client["fingerprint"] = v
		delete(s.Client, "fp")
	}
	if v, ok := s.Reality["dest"]; ok {
		if t, exists := s.Reality["target"]; exists && t != v {
			return Spec{}, invalid("reality.target", "duplicate_alias")
		}
		s.Reality["target"] = v
		delete(s.Reality, "dest")
	}
	if v, ok := s.Reality["maxTimeDiff"]; ok {
		if _, exists := s.Reality["maxTimediff"]; exists {
			return Spec{}, invalid("reality.maxTimediff", "duplicate_alias")
		}
		s.Reality["maxTimediff"] = v
		delete(s.Reality, "maxTimeDiff")
	}
	if err = validateSettings(&s); err != nil {
		return Spec{}, err
	}
	if err = validateStream(&s); err != nil {
		return Spec{}, err
	}
	if err = validateSecurity(&s); err != nil {
		return Spec{}, err
	}
	if err = validateClient(&s); err != nil {
		return Spec{}, err
	}
	if err = ensureStableRowIDs(&s); err != nil {
		return Spec{}, err
	}
	return s, nil
}

func unsupported(format string) error { return fmt.Errorf("%w: %s", ErrUnsupportedFormat, format) }
