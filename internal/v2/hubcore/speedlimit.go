package hubcore

import (
	"context"
	"fmt"
	"sort"
	"strconv"
)

// Per-user speed limit. Xray has no rate limit, so a limited user's
// traffic that would leave the node directly is routed to the node agent's
// local SOCKS limiter (username = email, password = kbit/s), which shares
// one bucket per direction across all of the user's connections.

const (
	limiterPort     = 62051 // nodeagent.LimiterAddr
	limiterMinAgent = "v2.10.0"
	maxSpeedKbps    = 10_000_000 // 10 Gbit/s
)

// SetSpeedLimit sets a client's limit in kbit/s; 0 removes it.
func (s *Store) SetSpeedLimit(ctx context.Context, email string, kbps int) error {
	if kbps < 0 || kbps > maxSpeedKbps || (kbps > 0 && kbps < 64) {
		return fmt.Errorf("%w: the speed limit is 64 kbit/s to 10 Gbit/s, or 0 for none", ErrInvalid)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE clients SET speed_kbps=? WHERE email=?`, kbps, email)
	if err != nil {
		return err
	}
	if c, _ := r.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// speedRouting adds a limiter outbound and rule for each limited client on
// this node. Rules go last, so blocks and the owner's routing still apply
// first; only traffic headed for the default "direct" exit is limited.
func (s *Store) speedRouting(ctx context.Context, agentVer string, clients map[string][]Client, outbounds []any, routing map[string]any) []any {
	if semver(agentVer, limiterMinAgent) < 0 || len(outbounds) == 0 {
		return outbounds // older agents have no limiter
	}
	if first, _ := outbounds[0].(map[string]any); first == nil || first["tag"] != "direct" {
		return outbounds
	}
	seen := map[string]bool{}
	var emails []string
	for _, list := range clients {
		for _, c := range list {
			if !seen[c.Email] {
				seen[c.Email] = true
				emails = append(emails, c.Email)
			}
		}
	}
	if len(emails) == 0 {
		return outbounds
	}
	limits := map[string]int{}
	rows, err := s.ro.QueryContext(ctx, `SELECT email,speed_kbps FROM clients WHERE speed_kbps>0`)
	if err != nil {
		return outbounds
	}
	for rows.Next() {
		var e string
		var k int
		if rows.Scan(&e, &k) == nil && seen[e] {
			limits[e] = k
		}
	}
	rows.Close()
	if len(limits) == 0 {
		return outbounds
	}
	sort.Strings(emails)
	rules, _ := routing["rules"].([]any)
	n := 0
	for _, e := range emails {
		k, ok := limits[e]
		if !ok {
			continue
		}
		n++
		tag := "dark-limit-" + strconv.Itoa(n)
		outbounds = append(outbounds, map[string]any{"tag": tag, "protocol": "socks", "settings": map[string]any{
			"servers": []any{map[string]any{"address": "127.0.0.1", "port": limiterPort,
				"users": []any{map[string]any{"user": e, "pass": strconv.Itoa(k)}}}}}})
		rules = append(rules, map[string]any{"type": "field", "user": []string{e}, "outboundTag": tag})
	}
	routing["rules"] = rules
	return outbounds
}
