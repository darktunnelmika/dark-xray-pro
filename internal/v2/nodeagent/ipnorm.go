package nodeagent

import (
	"net"
	"sort"
)

// CountKey is how an address counts toward an IP limit: an IPv4 address as
// itself, an IPv6 address by its /64 network, because phones rotate their
// IPv6 address (privacy extensions) inside the same /64 all the time.
func CountKey(s string) string {
	ip := net.ParseIP(s)
	if ip == nil || ip.To4() != nil {
		return s
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// Collapse maps each user's addresses to their count keys, deduplicated.
func Collapse(online map[string][]string) map[string][]string {
	out := make(map[string][]string, len(online))
	for e, ips := range online {
		seen := map[string]bool{}
		for _, ip := range ips {
			seen[CountKey(ip)] = true
		}
		keys := make([]string, 0, len(seen))
		for k := range seen {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out[e] = keys
	}
	return out
}
