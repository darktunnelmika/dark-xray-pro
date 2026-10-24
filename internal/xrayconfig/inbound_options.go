package xrayconfig

import (
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxSniffingRules     = 256
	MaxSniffingRuleBytes = 512
	MaxFallbacks         = 32
	// The core peeks at the first 64 bytes. A 55-byte path also fits after
	// the longest supported HTTP method, without pretending to parse a URL.
	MaxFallbackPathBytes = 55
)

// SniffingOptions is the public profile form. The renderer maps these keys to
// Xray's camelCase SniffingObject, without changing rule text or order.
type SniffingOptions struct {
	Enabled         bool     `json:"enabled"`
	DestOverride    []string `json:"dest_override,omitempty"`
	MetadataOnly    bool     `json:"metadata_only,omitempty"`
	RouteOnly       bool     `json:"route_only,omitempty"`
	DomainsExcluded []string `json:"domains_excluded,omitempty"`
	IPsExcluded     []string `json:"ips_excluded,omitempty"`
}

// Fallback describes an explicit TCP fallback endpoint. Unix sockets, external
// file references and special serve destinations require a separate managed
// resource contract and are not accepted by this profile format.
type Fallback struct {
	Path string `json:"path,omitempty"`
	// Reserved for a future transport contract. In the pinned REALITY
	// handshake the negotiated ALPN is empty, so nonempty selectors are
	// accepted by core syntax but cannot dispatch traffic. Reject them here.
	ALPN string `json:"alpn,omitempty"`
	Dest string `json:"dest"`
	Xver int    `json:"xver,omitempty"`
}

// ProfileOptionError identifies a safe editor section without echoing any
// user-provided rule or destination in the error message.
type ProfileOptionError struct {
	Field string
	Code  string
}

func (e *ProfileOptionError) Error() string { return e.Code }
func (e *ProfileOptionError) Unwrap() error { return ErrInvalid }

func sniffingInvalid() error { return &ProfileOptionError{Field: "sniffing", Code: "sniffing_invalid"} }
func fallbacksInvalid() error {
	return &ProfileOptionError{Field: "fallbacks", Code: "fallbacks_invalid"}
}

func copySniffing(s *SniffingOptions) *SniffingOptions {
	if s == nil || !s.Enabled && !s.MetadataOnly && !s.RouteOnly && len(s.DestOverride) == 0 && len(s.DomainsExcluded) == 0 && len(s.IPsExcluded) == 0 {
		return nil
	}
	result := *s
	result.DestOverride = append([]string(nil), s.DestOverride...)
	result.DomainsExcluded = append([]string(nil), s.DomainsExcluded...)
	result.IPsExcluded = append([]string(nil), s.IPsExcluded...)
	return &result
}

func normalizeSniffing(s *SniffingOptions) (*SniffingOptions, error) {
	if s == nil {
		return nil, nil
	}
	if len(s.DestOverride) > 4 || len(s.DomainsExcluded) > MaxSniffingRules || len(s.IPsExcluded) > MaxSniffingRules {
		return nil, sniffingInvalid()
	}
	seen := map[string]bool{}
	for _, protocol := range s.DestOverride {
		if (protocol != "http" && protocol != "tls" && protocol != "quic" && protocol != "fakedns") || seen[protocol] {
			return nil, sniffingInvalid()
		}
		seen[protocol] = true
	}
	for _, rule := range s.DomainsExcluded {
		if !validSniffingDomainRule(rule) {
			return nil, sniffingInvalid()
		}
	}
	for _, rule := range s.IPsExcluded {
		if !validSniffingIPRule(rule) {
			return nil, sniffingInvalid()
		}
	}
	return copySniffing(s), nil
}

func validRuleText(rule string) bool {
	if len(rule) == 0 || len(rule) > MaxSniffingRuleBytes || !utf8.ValidString(rule) || strings.TrimSpace(rule) != rule {
		return false
	}
	for _, r := range rule {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validSniffingDomainRule(rule string) bool {
	if !validRuleText(rule) {
		return false
	}
	prefix, value, hasPrefix := strings.Cut(rule, ":")
	if !hasPrefix {
		return true
	}
	if value == "" {
		return false
	}
	switch prefix {
	case "regexp":
		_, err := regexp.Compile(value)
		return err == nil
	case "keyword":
		return true
	case "full", "domain":
		if ip, err := netip.ParseAddr(value); err == nil {
			return ip.Zone() == ""
		}
		return !strings.ContainsAny(value, ":[]") && validGRPCAuthority(value)
	default:
		// In particular, do not accept geosite/ext file references when the
		// Node bundle does not manage or prove the corresponding assets.
		return false
	}
}

func validSniffingIPRule(rule string) bool {
	if !validRuleText(rule) {
		return false
	}
	if strings.Contains(rule, "/") {
		prefix, err := netip.ParsePrefix(rule)
		if err != nil {
			return false
		}
		// Xray treats an IPv4-mapped literal as IPv4 and uses a 32-bit mask.
		return !prefix.Addr().Is4In6() || prefix.Bits() <= 32
	}
	address, err := netip.ParseAddr(rule)
	return err == nil && address.Zone() == ""
}

func normalizeFallbacks(transport string, input []Fallback) ([]Fallback, error) {
	if len(input) == 0 {
		return nil, nil
	}
	if transport != "tcp" || len(input) > MaxFallbacks {
		return nil, fallbacksInvalid()
	}
	seen := map[[2]string]bool{}
	for _, row := range input {
		if row.Xver < 0 || row.Xver > 2 || !validFallbackPath(row.Path) || row.ALPN != "" || !validFallbackDestination(row.Dest) {
			return nil, fallbacksInvalid()
		}
		key := [2]string{row.ALPN, row.Path}
		if seen[key] {
			return nil, fallbacksInvalid()
		}
		seen[key] = true
	}
	return append([]Fallback(nil), input...), nil
}

func validFallbackPath(path string) bool {
	if path == "" {
		return true
	}
	if !utf8.ValidString(path) || len(path) > MaxFallbackPathBytes || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return false
	}
	for _, r := range path {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validFallbackDestination(dest string) bool {
	if p, err := strconv.ParseUint(dest, 10, 16); err == nil {
		return p > 0 && strconv.FormatUint(p, 10) == dest
	}
	if _, _, err := net.SplitHostPort(dest); err != nil {
		return false
	}
	return validGRPCAuthority(dest)
}

type sniffing struct {
	Enabled         bool     `json:"enabled"`
	DestOverride    []string `json:"destOverride,omitempty"`
	MetadataOnly    bool     `json:"metadataOnly,omitempty"`
	RouteOnly       bool     `json:"routeOnly,omitempty"`
	DomainsExcluded []string `json:"domainsExcluded,omitempty"`
	IPsExcluded     []string `json:"ipsExcluded,omitempty"`
}

func applyInboundOptions(in *inbound, options *ProfileOptions) {
	if options == nil {
		return
	}
	if s := copySniffing(options.Sniffing); s != nil {
		in.Sniffing = &sniffing{Enabled: s.Enabled, DestOverride: s.DestOverride, MetadataOnly: s.MetadataOnly, RouteOnly: s.RouteOnly, DomainsExcluded: s.DomainsExcluded, IPsExcluded: s.IPsExcluded}
	}
	in.Settings.Fallbacks = append([]Fallback(nil), options.Fallbacks...)
}

func optionsFromInbound(in inbound) *ProfileOptions {
	o := optionsFromStream(in.Stream)
	if s := in.Sniffing; s != nil {
		o.Sniffing = &SniffingOptions{Enabled: s.Enabled, DestOverride: append([]string(nil), s.DestOverride...), MetadataOnly: s.MetadataOnly, RouteOnly: s.RouteOnly, DomainsExcluded: append([]string(nil), s.DomainsExcluded...), IPsExcluded: append([]string(nil), s.IPsExcluded...)}
	}
	o.Fallbacks = append([]Fallback(nil), in.Settings.Fallbacks...)
	return o
}
