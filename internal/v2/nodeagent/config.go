package nodeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// FilterConfig returns the Xray config with every client whose email is not
// in allowed removed from every inbound. The HUB renders one config holding
// all users; the Node decides locally who is served right now.
//
// It also verifies the config enables the stats API the agent relies on.
func FilterConfig(raw []byte, allowed map[string]bool) ([]byte, error) {
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("config is not JSON: %w", err)
	}
	inbounds, _ := cfg["inbounds"].([]any)
	if len(inbounds) == 0 {
		return nil, errors.New("config has no inbounds")
	}
	for _, ib := range inbounds {
		in, ok := ib.(map[string]any)
		if !ok {
			continue
		}
		settings, ok := in["settings"].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"clients", "users", "accounts"} {
			list, ok := settings[key].([]any)
			if !ok {
				continue
			}
			kept := make([]any, 0, len(list))
			for _, c := range list {
				m, ok := c.(map[string]any)
				if !ok {
					continue
				}
				email, _ := m["email"].(string)
				if email == "" {
					// Unnamed accounts cannot be metered or limited; they
					// are never served by a managed Node.
					continue
				}
				if allowed[email] {
					kept = append(kept, c)
				}
			}
			settings[key] = kept
		}
	}
	return json.Marshal(cfg)
}

// ConfigEmails lists every client email in a HUB config, sorted.
func ConfigEmails(raw []byte) ([]string, error) {
	var cfg struct {
		Inbounds []struct {
			Settings map[string]json.RawMessage `json:"settings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, in := range cfg.Inbounds {
		for _, key := range []string{"clients", "users", "accounts"} {
			var list []struct {
				Email string `json:"email"`
			}
			if v, ok := in.Settings[key]; ok && json.Unmarshal(v, &list) == nil {
				for _, c := range list {
					if c.Email != "" {
						seen[c.Email] = true
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Strings(out)
	return out, nil
}
