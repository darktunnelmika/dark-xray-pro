package inboundexec

import (
	"encoding/json"
	"fmt"
)

// TUIC and userspace AWG currently own their direct socket dialers. They cannot
// truthfully implement an Xray proxy outbound, balancer or a customized freedom
// dialer. Refuse any possibly matching policy instead of silently bypassing it.
func validateDirectComponentRoutes(components []Component, outbounds, rules []json.RawMessage) error {
	plain := map[string]bool{}
	defaultTag := ""
	for i, raw := range outbounds {
		var out map[string]json.RawMessage
		if json.Unmarshal(raw, &out) != nil {
			return ErrInvalid
		}
		var tag, protocol string
		if json.Unmarshal(out["tag"], &tag) != nil || tag == "" || json.Unmarshal(out["protocol"], &protocol) != nil {
			return ErrInvalid
		}
		if i == 0 {
			defaultTag = tag
		}
		compatible := protocol == "freedom"
		for field, value := range out {
			switch field {
			case "tag", "protocol":
			case "settings":
				var settings map[string]any
				if json.Unmarshal(value, &settings) != nil || len(settings) != 0 {
					compatible = false
				}
			default:
				compatible = false
			}
		}
		plain[tag] = compatible
	}
	unsupported := func(engine string) error {
		return fmt.Errorf("%s requires an explicitly compatible direct outbound: %w", engine, ErrInvalid)
	}
	for _, c := range components {
		if c.Engine != "tuic" && c.Engine != "amneziawg" {
			continue
		}
		var manifest map[string]json.RawMessage
		if json.Unmarshal(c.Manifest, &manifest) != nil {
			return ErrInvalid
		}
		selected := defaultTag
		if raw, ok := manifest["outbound_tag"]; ok {
			if json.Unmarshal(raw, &selected) != nil {
				return ErrInvalid
			}
			if selected == "" {
				selected = defaultTag
			}
		}
		if raw, ok := manifest["requires_socks_bridge"]; ok {
			var bridge bool
			if json.Unmarshal(raw, &bridge) != nil || bridge {
				return unsupported(c.Engine)
			}
		}
		if !plain[selected] {
			return unsupported(c.Engine)
		}
		for _, raw := range rules {
			var rule map[string]json.RawMessage
			if json.Unmarshal(raw, &rule) != nil {
				return ErrInvalid
			}
			if tags, ok := rule["inboundTag"]; ok {
				var ids []string
				if json.Unmarshal(tags, &ids) != nil {
					return ErrInvalid
				}
				matches := len(ids) == 0
				for _, id := range ids {
					matches = matches || id == c.Tag
				}
				if !matches {
					continue
				}
			}
			var target string
			if _, ok := rule["balancerTag"]; ok {
				return unsupported(c.Engine)
			}
			if json.Unmarshal(rule["outboundTag"], &target) != nil || !plain[target] {
				return unsupported(c.Engine)
			}
		}
	}
	return nil
}
