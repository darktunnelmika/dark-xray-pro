package hubcore

import (
	"context"
	"fmt"
	"strconv"
)

// Inbound speed cap: one shared speed for everyone on an inbound (per
// node), e.g. a 15 Mbit/s inbound for unlimited plans next to a VIP one
// with none; the IP limit keeps any one account from taking it all. Xray
// has no rate limit, so the inbound's traffic that would leave the node
// directly goes to the node agent's local SOCKS limiter (username = the
// inbound, password = kbit/s): one bucket per direction.

const (
	limiterPort     = 62051 // nodeagent.LimiterAddr
	limiterMinAgent = "v2.10.0"
	maxSpeedKbps    = 10_000_000 // 10 Gbit/s
)

// SetInboundSpeed sets an inbound's shared cap in kbit/s; 0 removes it.
func (s *Store) SetInboundSpeed(ctx context.Context, tag string, kbps int) error {
	if kbps < 0 || kbps > maxSpeedKbps || (kbps > 0 && kbps < 64) {
		return fmt.Errorf("%w: the speed cap is 64 kbit/s to 10 Gbit/s, or 0 for none", ErrInvalid)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE inbounds SET speed_kbps=? WHERE tag=?`, kbps, tag)
	if err != nil {
		return err
	}
	if c, _ := r.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// speedRouting adds a limiter outbound and rule per capped inbound. Rules go
// last, so blocks and the owner's routing still apply first; only traffic
// headed for the default "direct" exit is capped. Direct, tunnel and
// fallback copies of an inbound share its cap.
func speedRouting(agentVer string, ins []nodeInbound, outbounds []any, routing map[string]any) []any {
	if semver(agentVer, limiterMinAgent) < 0 || len(outbounds) == 0 {
		return outbounds // older agents have no limiter
	}
	if first, _ := outbounds[0].(map[string]any); first == nil || first["tag"] != "direct" {
		return outbounds
	}
	rules, _ := routing["rules"].([]any)
	for idx, in := range ins {
		if in.speedKbps <= 0 {
			continue
		}
		tags := []string{in.tag}
		if in.tunnel > 0 {
			tags = append(tags, in.tag+tunnelSuffix)
		}
		for i := 0; i < 8; i++ {
			tags = append(tags, fmt.Sprintf("%s%s%d", in.tag, fallbackSuffix, i))
		}
		tag := "dark-limit-" + strconv.Itoa(idx)
		outbounds = append(outbounds, map[string]any{"tag": tag, "protocol": "socks", "settings": map[string]any{
			"servers": []any{map[string]any{"address": "127.0.0.1", "port": limiterPort,
				"users": []any{map[string]any{"user": "inbound:" + in.tag, "pass": strconv.Itoa(in.speedKbps)}}}}}})
		rules = append(rules, map[string]any{"type": "field", "inboundTag": tags, "outboundTag": tag})
	}
	routing["rules"] = rules
	return outbounds
}
