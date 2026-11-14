package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The Xray overlay holds extra top-level sections merged into every
// node's config (log level, policy timeouts, DNS details, observatory,
// fakedns, reverse, metrics). Sections the panel manages itself (inbounds,
// outbounds, routing, api, stats) are refused, and the user-stats policy
// flags the agent depends on are always restored after the merge.

const overlayKey = "xray_overlay"

var overlayAllowed = map[string]bool{"log": true, "policy": true, "dns": true, "observatory": true, "burstObservatory": true,
	"fakedns": true, "reverse": true, "metrics": true, "transport": true}

// XrayOverlay returns the stored overlay ("{}" if none).
func (s *Store) XrayOverlay(ctx context.Context) json.RawMessage {
	if v := s.Setting(ctx, overlayKey); v != "" {
		return json.RawMessage(v)
	}
	return json.RawMessage(`{}`)
}

// PutXrayOverlay validates, stores and checks the overlay on every node.
func (s *Store) PutXrayOverlay(ctx context.Context, raw json.RawMessage) error {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("%w: not a JSON object: %v", ErrInvalid, err)
	}
	var bad []string
	for k := range m {
		if !overlayAllowed[k] {
			bad = append(bad, k)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("%w: sections managed by the panel or unknown: %s", ErrInvalid, strings.Join(bad, ", "))
	}
	clean, _ := json.Marshal(m)
	old := s.Setting(ctx, overlayKey)
	if err := s.SetSetting(ctx, overlayKey, string(clean)); err != nil {
		return err
	}
	if err := s.checkAllNodes(ctx); err != nil {
		_ = s.SetSetting(ctx, overlayKey, old)
		return err
	}
	return nil
}

// applyOverlay merges the overlay into a rendered config map.
func (s *Store) applyOverlay(ctx context.Context, cfg map[string]any) {
	raw := s.Setting(ctx, overlayKey)
	if raw == "" || raw == "{}" {
		return
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	for k, v := range m {
		if !overlayAllowed[k] {
			continue
		}
		if cur, ok := cfg[k].(map[string]any); ok {
			if add, ok := v.(map[string]any); ok {
				cfg[k] = deepMerge(cur, add)
				continue
			}
		}
		cfg[k] = v
	}
	// The agent's metering depends on these.
	pol, _ := cfg["policy"].(map[string]any)
	if pol == nil {
		pol = map[string]any{}
		cfg["policy"] = pol
	}
	levels, _ := pol["levels"].(map[string]any)
	if levels == nil {
		levels = map[string]any{}
		pol["levels"] = levels
	}
	l0, _ := levels["0"].(map[string]any)
	if l0 == nil {
		l0 = map[string]any{}
		levels["0"] = l0
	}
	l0["statsUserUplink"], l0["statsUserDownlink"], l0["statsUserOnline"] = true, true, true
	sys, _ := pol["system"].(map[string]any)
	if sys == nil {
		sys = map[string]any{}
		pol["system"] = sys
	}
	sys["statsInboundUplink"], sys["statsInboundDownlink"] = true, true
}

func deepMerge(dst, src map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		if a, ok := out[k].(map[string]any); ok {
			if b, ok := v.(map[string]any); ok {
				out[k] = deepMerge(a, b)
				continue
			}
		}
		out[k] = v
	}
	return out
}
