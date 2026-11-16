package hubcore

import (
	"context"
	"encoding/json"
	"net"
	"sort"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// ipOverWindow is how fresh a Node's online report must be to count.
const ipOverWindow = 30

// ipOver returns the users among limits whose distinct source addresses on
// all Nodes together exceed their IP limit. Each Node only sees its own
// connections, so without this a limit of 2 allowed 2 on every Node.
// Trusted addresses (tunnel relays, NATs) never count.
func (s *Store) ipOver(ctx context.Context, now int64, limits []wire.UserLimit, trusted []string) []string {
	max := map[string]int{}
	for _, l := range limits {
		if l.MaxIPs > 0 && l.Enabled {
			max[l.Email] = l.MaxIPs
		}
	}
	if len(max) == 0 {
		return nil
	}
	nets := trustedNets(trusted)
	rows, err := s.ro.QueryContext(ctx, `SELECT email,ips FROM online WHERE seen_unix>=?`, now-ipOverWindow)
	if err != nil {
		return nil
	}
	defer rows.Close()
	seen := map[string]map[string]bool{}
	for rows.Next() {
		var email, raw string
		if rows.Scan(&email, &raw) != nil {
			continue
		}
		if _, ok := max[email]; !ok {
			continue
		}
		var ips []string
		if json.Unmarshal([]byte(raw), &ips) != nil {
			continue
		}
		for _, ip := range ips {
			if isTrusted(ip, nets) {
				continue
			}
			if seen[email] == nil {
				seen[email] = map[string]bool{}
			}
			seen[email][ip] = true
		}
	}
	var out []string
	for email, ips := range seen {
		if len(ips) > max[email] {
			out = append(out, email)
		}
	}
	sort.Strings(out)
	return out
}

func trustedNets(list []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, t := range list {
		if !strings.Contains(t, "/") {
			if ip := net.ParseIP(t); ip != nil {
				if ip.To4() != nil {
					t += "/32"
				} else {
					t += "/128"
				}
			}
		}
		if _, n, err := net.ParseCIDR(t); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

func isTrusted(s string, nets []*net.IPNet) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
