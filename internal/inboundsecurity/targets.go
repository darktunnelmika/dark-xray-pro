package inboundsecurity

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

var blockedPublicPrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// Reject translation, transition, documentation and special-use IPv6.
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}

func publicAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" || address.Is4In6() || !address.IsGlobalUnicast() || address.IsPrivate() {
		return false
	}
	if address.Is6() && !netip.MustParsePrefix("2000::/3").Contains(address) {
		return false
	}
	for _, prefix := range blockedPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func validDNSName(name string) bool {
	if len(name) < 3 || len(name) > 253 || strings.TrimSpace(name) != name || strings.HasSuffix(name, ".") || net.ParseIP(name) != nil {
		return false
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	// Numeric/abbreviated IPv4 spellings must not enter a DNS search path.
	for _, character := range labels[len(labels)-1] {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') {
			return true
		}
	}
	return false
}

func parseTarget(target, serverName string) (string, int, error) {
	if len(target) > 260 || target == "" || strings.TrimSpace(target) != target || strings.ContainsAny(target, "/\\@?#%\x00\r\n\t ") {
		return "", 0, failure("invalid_target")
	}
	host, portText, err := net.SplitHostPort(target)
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || port < 1 || port > 65535 || strconv.Itoa(port) != portText {
		return "", 0, failure("invalid_target")
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.String() != host || !publicAddress(address) {
			return "", 0, failure("target_not_public")
		}
	} else if !validDNSName(host) {
		return "", 0, failure("invalid_target")
	}
	if serverName != "" && !validCertificateName(serverName, false) {
		return "", 0, failure("invalid_server_name")
	}
	return strings.ToLower(host), port, nil
}

func (s *Service) resolvePinned(ctx context.Context, host string) (netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		if !publicAddress(address) {
			return netip.Addr{}, failure("target_not_public")
		}
		return address, nil
	}
	answers, err := s.config.Resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return netip.Addr{}, failure("dns_lookup_failed")
	}
	if len(answers) == 0 || len(answers) > 16 {
		return netip.Addr{}, failure("dns_answers_invalid")
	}
	addresses := make([]netip.Addr, 0, len(answers))
	for _, answer := range answers {
		address, ok := netip.AddrFromSlice(answer.IP)
		if !ok || answer.Zone != "" {
			return netip.Addr{}, failure("dns_answers_invalid")
		}
		// net.IP represents ordinary IPv4 answers as mapped 16-byte values.
		address = address.Unmap()
		if !publicAddress(address) {
			return netip.Addr{}, failure("dns_contains_nonpublic_address")
		}
		addresses = append(addresses, address)
	}
	// All answers have been checked before any connection. Dial exactly one
	// literal IP; never resolve the original host or discovered SNI again.
	return addresses[0], nil
}

func remoteMatches(connection net.Conn, address netip.Addr, port int) bool {
	remote := connection.RemoteAddr()
	if remote == nil {
		return false
	}
	host, portText, err := net.SplitHostPort(remote.String())
	actual, parseErr := netip.ParseAddr(host)
	actualPort, portErr := strconv.Atoi(portText)
	return err == nil && parseErr == nil && portErr == nil && actual.Unmap() == address && actualPort == port
}

func (s *Service) findTargets(request FindRequest) ([]Target, string, error) {
	if request.CIDR != "" && len(request.Targets) != 0 {
		return nil, "", failure("ambiguous_find_targets")
	}
	if request.CIDR != "" {
		prefix, err := netip.ParsePrefix(request.CIDR)
		if err != nil || prefix != prefix.Masked() || prefix.Addr().Is4In6() || prefix.Addr().Zone() != "" {
			return nil, "", failure("invalid_target_cidr")
		}
		hostBits := prefix.Addr().BitLen() - prefix.Bits()
		if hostBits > 8 || hostBits < 0 {
			return nil, "", failure("cidr_too_large")
		}
		port := request.Port
		if port == 0 {
			port = 443
		}
		if port < 1 || port > 65535 || (request.ServerName != "" && !validCertificateName(request.ServerName, false)) {
			return nil, "", failure("invalid_find_request")
		}
		count := 1 << uint(hostBits)
		targets := make([]Target, 0, count)
		for address, i := prefix.Addr(), 0; i < count; address, i = address.Next(), i+1 {
			if !publicAddress(address) {
				return nil, "", failure("target_not_public")
			}
			targets = append(targets, Target{Target: net.JoinHostPort(address.String(), strconv.Itoa(port)), ServerName: request.ServerName})
		}
		return targets, "cidr", nil
	}
	if request.Port != 0 || request.ServerName != "" {
		return nil, "", failure("cidr_options_without_cidr")
	}
	targets, source := request.Targets, "targets"
	if len(targets) == 0 {
		targets, source = s.config.DefaultTargets, "defaults"
	}
	if len(targets) == 0 || len(targets) > MaxTargets {
		return nil, "", failure("invalid_target_count")
	}
	seen := make(map[Target]bool, len(targets))
	for _, target := range targets {
		if _, _, err := parseTarget(target.Target, target.ServerName); err != nil {
			return nil, "", err
		}
		if seen[target] {
			return nil, "", failure("duplicate_target")
		}
		seen[target] = true
	}
	return append([]Target(nil), targets...), source, nil
}
