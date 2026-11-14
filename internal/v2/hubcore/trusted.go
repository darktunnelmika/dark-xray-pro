package hubcore

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// TrustedIPs lists addresses never counted toward client IP limits.
func (s *Store) TrustedIPs(ctx context.Context) []string {
	v := s.Setting(ctx, "trusted_ips")
	if v == "" {
		return nil
	}
	return strings.Split(v, "\n")
}

// PutTrustedIPs saves the list (IPs or CIDRs, one per entry).
func (s *Store) PutTrustedIPs(ctx context.Context, list []string) error {
	var clean []string
	for _, x := range list {
		x = strings.TrimSpace(x)
		if x == "" {
			continue
		}
		ok := net.ParseIP(x) != nil
		if !ok {
			_, _, err := net.ParseCIDR(x)
			ok = err == nil
		}
		if !ok {
			return fmt.Errorf("%w: %q is not an IP address or network", ErrInvalid, x)
		}
		clean = append(clean, x)
	}
	if len(clean) > 200 {
		return fmt.Errorf("%w: at most 200 entries", ErrInvalid)
	}
	return s.SetSetting(ctx, "trusted_ips", strings.Join(clean, "\n"))
}
