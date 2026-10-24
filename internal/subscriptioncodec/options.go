package subscriptioncodec

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxConnectionPathBytes = 2048
	// A bounded authoring policy, below the core's signed 32-bit range. The
	// padding is allocated for HTTP requests/responses, not stored metadata.
	MaxXHTTPPaddingBytes = 65536
)

// XHTTPOptions is the supported intersection of server transport settings and
// client share-link settings. Padding is a validated integer or integer range,
// never arbitrary XHTTP extra JSON. Empty strings retain the core defaults.
type XHTTPOptions struct {
	Path          string `json:"path,omitempty"`
	Host          string `json:"host,omitempty"`
	Mode          string `json:"mode,omitempty"`
	XPaddingBytes string `json:"x_padding_bytes,omitempty"`
}

// WebSocketOptions and HTTPUpgradeOptions intentionally expose only the
// paired server/client settings. Early-data query controls, arbitrary headers
// and PROXY protocol require separate contracts and are not accepted here.
type WebSocketOptions struct {
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
}

type HTTPUpgradeOptions struct {
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
}

// NormalizeSecurity keeps the original REALITY representation byte-compatible.
// TLS must be explicit; an unknown spelling is never treated as a default.
func NormalizeSecurity(security string) (string, error) {
	switch security {
	case "", "reality":
		return "", nil
	case "tls":
		return "tls", nil
	default:
		return "", &OptionError{Field: "security", Code: "security_invalid"}
	}
}

func NormalizeWebSocketOptions(options *WebSocketOptions) (*WebSocketOptions, error) {
	if options == nil {
		return nil, nil
	}
	o := *options
	if err := validateHTTPTransport(o.Path, o.Host, "websocket"); err != nil {
		return nil, err
	}
	if o == (WebSocketOptions{}) {
		return nil, nil
	}
	return &o, nil
}

func NormalizeHTTPUpgradeOptions(options *HTTPUpgradeOptions) (*HTTPUpgradeOptions, error) {
	if options == nil {
		return nil, nil
	}
	o := *options
	if err := validateHTTPTransport(o.Path, o.Host, "http_upgrade"); err != nil {
		return nil, err
	}
	if o == (HTTPUpgradeOptions{}) {
		return nil, nil
	}
	return &o, nil
}

func validateHTTPTransport(path, host, section string) error {
	// WebSocket builds a URI by concatenation, whereas HTTPUpgrade assigns
	// URL.Path. An encoded percent escape has different meanings in those
	// paths. The shared grammar uses literal ASCII paths and no hidden ?ed=.
	valid := path == "" || (len(path) <= MaxConnectionPathBytes && strings.HasPrefix(path, "/") &&
		!strings.HasPrefix(path, "//") && !strings.ContainsAny(path, "%?#\\"))
	for _, b := range []byte(path) {
		if b < 33 || b > 126 {
			valid = false
		}
	}
	if !valid {
		return &OptionError{Field: "path", Code: section + "_path_invalid"}
	}
	if host != "" && (!ValidAuthority(host) || strings.ContainsAny(host, ":[]")) {
		return &OptionError{Field: "host", Code: section + "_host_invalid"}
	}
	return nil
}

// ClientOptions belongs to exported client connections. It must never be
// emitted as server REALITY settings or inferred from a server candidate.
type ClientOptions struct {
	Fingerprint string `json:"fingerprint,omitempty"`
	SpiderX     string `json:"spider_x,omitempty"`
}

// OptionError identifies a fixed public field without echoing submitted text.
type OptionError struct {
	Field string
	Code  string
}

func (e *OptionError) Error() string { return e.Code }
func (e *OptionError) Unwrap() error { return ErrInvalid }

func NormalizeXHTTPOptions(options *XHTTPOptions) (*XHTTPOptions, error) {
	if options == nil {
		return nil, nil
	}
	o := *options
	if !validConnectionPath(o.Path, false) {
		return nil, &OptionError{Field: "path", Code: "xhttp_path_invalid"}
	}
	// The pinned core's server compares a port-stripped request hostname to
	// the literal configured Host. A shared host:port or bracketed IPv6 value
	// therefore cannot match itself. Restrict this paired field to bare DNS/
	// IPv4; public endpoint addresses and gRPC authority use other contracts.
	if o.Host != "" && (!ValidAuthority(o.Host) || strings.ContainsAny(o.Host, ":[]")) {
		return nil, &OptionError{Field: "host", Code: "xhttp_host_invalid"}
	}
	switch o.Mode {
	case "", "auto", "packet-up", "stream-up", "stream-one":
	default:
		return nil, &OptionError{Field: "mode", Code: "xhttp_mode_invalid"}
	}
	if !validXHTTPPadding(o.XPaddingBytes) {
		return nil, &OptionError{Field: "x_padding_bytes", Code: "xhttp_padding_invalid"}
	}
	if o == (XHTTPOptions{}) {
		return nil, nil
	}
	return &o, nil
}

func NormalizeClientOptions(options *ClientOptions) (*ClientOptions, error) {
	if options == nil {
		return nil, nil
	}
	o := *options
	// These are the pinned core's stable GUI preset aliases. Unsafe, removed
	// fingerprints and arbitrary names are not silently replaced with Chrome.
	switch o.Fingerprint {
	case "", "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized":
	default:
		return nil, &OptionError{Field: "fingerprint", Code: "client_fingerprint_invalid"}
	}
	if !validConnectionPath(o.SpiderX, true) {
		return nil, &OptionError{Field: "spider_x", Code: "client_spider_x_invalid"}
	}
	if o == (ClientOptions{}) {
		return nil, nil
	}
	return &o, nil
}

func validConnectionPath(value string, queryAllowed bool) bool {
	if value == "" {
		return true
	}
	if len(value) > MaxConnectionPathBytes || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\#") || (!queryAllowed && strings.Contains(value, "?")) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	u, err := url.ParseRequestURI(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	// REALITY treats these parameters as crawler control ranges rather than
	// request query text. They need a separate typed, bounded contract.
	if queryAllowed {
		for _, reserved := range []string{"p", "c", "t", "i", "r"} {
			if _, exists := query[reserved]; exists {
				return false
			}
		}
	}
	return true
}

func validXHTTPPadding(value string) bool {
	if value == "" {
		return true
	}
	parts := strings.Split(value, "-")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	var minimum uint64
	for _, part := range parts {
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil || n == 0 || n > MaxXHTTPPaddingBytes || strconv.FormatUint(n, 10) != part || n < minimum {
			return false
		}
		minimum = n
	}
	return true
}

// ValidAuthority validates the HTTP authority grammar used by gRPC.
// It preserves spelling; these values are HTTP metadata, not endpoint aliases.
func ValidAuthority(value string) bool {
	if len(value) == 0 || len(value) > 259 || strings.ContainsAny(value, " /\\@%?#\t\r\n") {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	host := value
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		ip := net.ParseIP(value[1 : len(value)-1])
		return ip != nil && strings.Contains(value, ":")
	}
	if strings.Contains(value, ":") {
		var port string
		var err error
		host, port, err = net.SplitHostPort(value)
		if err != nil {
			return false
		}
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != port {
			return false
		}
		if strings.HasPrefix(value, "[") && (net.ParseIP(host) == nil || !strings.Contains(host, ":")) {
			return false
		}
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !dnsLabelRE.MatchString(label) {
			return false
		}
	}
	return true
}
