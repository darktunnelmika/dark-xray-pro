// Package subscriptioncodec renders bounded, deterministic VLESS subscription
// documents. It performs no I/O and grants no publication or route authority.
package subscriptioncodec

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Format names are explicit; the codec never guesses from a user agent.
type Format string

const (
	FormatRaw        Format = "raw"
	FormatBase64     Format = "base64"
	MaxRoutes               = 256
	MaxDocumentBytes        = 1 << 20
)

var (
	ErrInvalid = errors.New("invalid subscription routes")
	ErrFormat  = errors.New("unsupported subscription format")
	uuidRE     = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
	dnsLabelRE = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	shortIDRE  = regexp.MustCompile(`^[0-9a-f]{0,16}$`)
	serviceRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// Route contains only the connection fields intended for one consumer. UUID,
// PublicKey and ShortID are consumer credentials and must remain protected even
// though PublicKey is derived from a server key. Server private keys, accounting
// emails, camouflage destinations and internal listener tags have no field here.
//
// Host is an explicit bare IP address or ASCII DNS name, without URL syntax or
// IPv6 brackets. The caller must establish authority for this endpoint. Loopback
// addresses are allowed so isolated LAB delivery does not invent a public route.
// Kind is the native listener kind: direct, swap or shared. InboundID and Kind
// are used only for validation; they are not added to the share URI or its label.
type Route struct {
	InboundID     uint64         `json:"inbound_id"`
	Kind          string         `json:"kind"`
	Host          string         `json:"host"`
	Port          uint16         `json:"port"`
	Label         string         `json:"label"`
	UUID          string         `json:"uuid"`
	Transport     string         `json:"transport"`
	GRPCService   string         `json:"grpc_service,omitempty"`
	GRPCAuthority string         `json:"grpc_authority,omitempty"`
	GRPCMultiMode bool           `json:"grpc_multi_mode,omitempty"`
	XHTTP         *XHTTPOptions  `json:"xhttp,omitempty"`
	Client        *ClientOptions `json:"client,omitempty"`
	ServerName    string         `json:"server_name"`
	PublicKey     string         `json:"public_key"`
	ShortID       string         `json:"short_id"`
}

// Render returns one URI per line, sorted by its complete canonical URI, with a
// final newline. Base64 is standard padded base64 of exactly the raw document,
// without an additional newline or wrapping. A document belongs to exactly one
// UUID; empty documents and duplicate connection descriptions fail closed.
//
// The supported URI fields follow the XTLS share-link proposal:
// https://github.com/XTLS/Xray-core/discussions/716 . TCP uses Vision; gRPC uses
// the selected gun/multi mode and never inherits a Vision flow. XHTTP extra is
// constructed only from its typed padding setting, not untrusted JSON.
func Render(routes []Route, format Format) ([]byte, error) {
	if format != FormatRaw && format != FormatBase64 {
		return nil, ErrFormat
	}
	if len(routes) == 0 || len(routes) > MaxRoutes {
		return nil, ErrInvalid
	}
	lines := make([]string, 0, len(routes))
	seen := make(map[string]bool, len(routes))
	consumerUUID := ""
	for _, route := range routes {
		r, ok := normalize(route)
		if !ok || consumerUUID != "" && consumerUUID != r.UUID {
			return nil, ErrInvalid
		}
		consumerUUID = r.UUID
		query := map[string]string{
			"encryption": "none", "fp": "chrome", "pbk": r.PublicKey,
			"security": "reality", "sid": r.ShortID, "sni": r.ServerName,
			"type": r.Transport,
		}
		if r.Client != nil {
			if r.Client.Fingerprint != "" {
				query["fp"] = r.Client.Fingerprint
			}
			if r.Client.SpiderX != "" {
				query["spx"] = r.Client.SpiderX
			}
		}
		if r.Transport == "tcp" {
			query["flow"] = "xtls-rprx-vision"
		} else if r.Transport == "grpc" {
			query["mode"] = "gun"
			if r.GRPCMultiMode {
				query["mode"] = "multi"
			}
			query["serviceName"] = r.GRPCService
			if r.GRPCAuthority != "" {
				query["authority"] = r.GRPCAuthority
			}
		} else {
			query["mode"], query["path"] = "auto", "/"
			if r.XHTTP != nil {
				if r.XHTTP.Path != "" {
					query["path"] = r.XHTTP.Path
				}
				if r.XHTTP.Host != "" {
					query["host"] = r.XHTTP.Host
				}
				if r.XHTTP.Mode != "" {
					query["mode"] = r.XHTTP.Mode
				}
				if r.XHTTP.XPaddingBytes != "" {
					extra, err := json.Marshal(struct {
						XPaddingBytes string `json:"xPaddingBytes"`
					}{r.XHTTP.XPaddingBytes})
					if err != nil {
						return nil, ErrInvalid
					}
					query["extra"] = string(extra)
				}
			}
		}
		keys := make([]string, 0, len(query))
		for key := range query {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+escapeComponent(query[key]))
		}
		connection := "vless://" + r.UUID + "@" + net.JoinHostPort(r.Host, strconv.Itoa(int(r.Port))) + "?" + strings.Join(parts, "&")
		if seen[connection] {
			return nil, ErrInvalid
		}
		seen[connection] = true
		lines = append(lines, connection+"#"+escapeComponent(r.Label))
	}
	sort.Strings(lines)
	raw := []byte(strings.Join(lines, "\n") + "\n")
	if len(raw) > MaxDocumentBytes {
		return nil, ErrInvalid
	}
	if format == FormatBase64 {
		if base64.StdEncoding.EncodedLen(len(raw)) > MaxDocumentBytes {
			return nil, ErrInvalid
		}
		encoded := make([]byte, base64.StdEncoding.EncodedLen(len(raw)))
		base64.StdEncoding.Encode(encoded, raw)
		return encoded, nil
	}
	return raw, nil
}

func normalize(r Route) (Route, bool) {
	if r.InboundID == 0 || r.InboundID > 1<<53-1 ||
		(r.Kind != "direct" && r.Kind != "swap" && r.Kind != "shared") ||
		r.Port == 0 || !uuidRE.MatchString(r.UUID) ||
		!validLabel(r.Label) || !validDNS(r.ServerName) ||
		!shortIDRE.MatchString(r.ShortID) || len(r.ShortID)%2 != 0 {
		return Route{}, false
	}
	var err error
	r.XHTTP, err = NormalizeXHTTPOptions(r.XHTTP)
	if err != nil {
		return Route{}, false
	}
	r.Client, err = NormalizeClientOptions(r.Client)
	if err != nil {
		return Route{}, false
	}
	if r.Transport != "grpc" && (r.GRPCService != "" || r.GRPCAuthority != "" || r.GRPCMultiMode) || r.Transport != "xhttp" && r.XHTTP != nil {
		return Route{}, false
	}
	switch r.Transport {
	case "tcp", "xhttp":
	case "grpc":
		if !serviceRE.MatchString(r.GRPCService) || r.GRPCAuthority != "" && !ValidAuthority(r.GRPCAuthority) {
			return Route{}, false
		}
	default:
		return Route{}, false
	}
	key, err := base64.RawURLEncoding.Strict().DecodeString(r.PublicKey)
	if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != r.PublicKey {
		return Route{}, false
	}
	var nonzero byte
	for _, b := range key {
		nonzero |= b
	}
	if nonzero == 0 {
		return Route{}, false
	}
	if ip, err := netip.ParseAddr(r.Host); err == nil {
		if ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			return Route{}, false
		}
		r.Host = ip.Unmap().String()
	} else {
		if !validDNS(r.Host) {
			return Route{}, false
		}
		r.Host = strings.ToLower(r.Host)
	}
	r.UUID = strings.ToLower(r.UUID)
	// Preserve the exact configured SNI. REALITY checks it against the literal
	// serverNames map; DNS case-insensitivity does not authorize rewriting it.
	return r, true
}

func validDNS(name string) bool {
	if len(name) == 0 || len(name) > 253 || net.ParseIP(name) != nil {
		return false
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return false
	}
	numeric := true
	for _, label := range labels {
		if !dnsLabelRE.MatchString(label) {
			return false
		}
		for _, r := range label {
			if r < '0' || r > '9' {
				numeric = false
			}
		}
	}
	// Do not reinterpret a malformed, ambiguous IPv4 address as a DNS name.
	return !numeric
}

func validLabel(label string) bool {
	if len(label) == 0 || len(label) > 256 || !utf8.ValidString(label) || strings.TrimSpace(label) == "" {
		return false
	}
	for _, r := range label {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// Percent-encode UTF-8 bytes with the RFC 3986 unreserved set. In particular,
// spaces are %20, never the form-encoding '+' representation.
func escapeComponent(value string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-._~", rune(b)) {
			out.WriteByte(b)
		} else {
			out.WriteByte('%')
			out.WriteByte(hex[b>>4])
			out.WriteByte(hex[b&15])
		}
	}
	return out.String()
}
