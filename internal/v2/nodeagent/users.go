package nodeagent

import (
	"encoding/json"
	"sort"
)

// ConfigUser is one client entry of one inbound, as rendered by the HUB.
type ConfigUser struct {
	Tag      string
	Protocol string
	Email    string
	ID       string
	Flow     string
	Password string
}

// InboundUsers maps inbound tag -> email -> user.
type InboundUsers map[string]map[string]ConfigUser

// ParseUsers lists the client entries of every inbound in a config.
func ParseUsers(raw []byte) (InboundUsers, error) {
	var cfg struct {
		Inbounds []struct {
			Tag      string                     `json:"tag"`
			Protocol string                     `json:"protocol"`
			Settings map[string]json.RawMessage `json:"settings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	out := InboundUsers{}
	for _, in := range cfg.Inbounds {
		for _, key := range []string{"clients", "users", "accounts"} {
			var list []struct {
				Email    string `json:"email"`
				ID       string `json:"id"`
				Flow     string `json:"flow"`
				Password string `json:"password"`
			}
			v, ok := in.Settings[key]
			if !ok || json.Unmarshal(v, &list) != nil {
				continue
			}
			for _, c := range list {
				if c.Email == "" {
					continue
				}
				if out[in.Tag] == nil {
					out[in.Tag] = map[string]ConfigUser{}
				}
				out[in.Tag][c.Email] = ConfigUser{Tag: in.Tag, Protocol: in.Protocol, Email: c.Email, ID: c.ID, Flow: c.Flow, Password: c.Password}
			}
		}
	}
	return out, nil
}

// Skeleton returns the config with every client list emptied. Two configs
// with the same skeleton differ only in users, which Xray can change live
// without a restart.
func Skeleton(raw []byte) (string, error) {
	cfg, err := FilterConfig(raw, map[string]bool{})
	if err != nil {
		return "", err
	}
	return configID(cfg), nil
}

// UserOp is one live change.
type UserOp struct {
	Add  bool
	User ConfigUser
}

// DiffUsers lists removals first, then additions, in a stable order. A user
// whose account changed (new UUID or password) is removed and re-added.
func DiffUsers(from, to InboundUsers) []UserOp {
	var ops []UserOp
	tags := map[string]bool{}
	for t := range from {
		tags[t] = true
	}
	for t := range to {
		tags[t] = true
	}
	sorted := make([]string, 0, len(tags))
	for t := range tags {
		sorted = append(sorted, t)
	}
	sort.Strings(sorted)
	var adds []UserOp
	for _, t := range sorted {
		emails := map[string]bool{}
		for e := range from[t] {
			emails[e] = true
		}
		for e := range to[t] {
			emails[e] = true
		}
		list := make([]string, 0, len(emails))
		for e := range emails {
			list = append(list, e)
		}
		sort.Strings(list)
		for _, e := range list {
			old, had := from[t][e]
			cur, want := to[t][e]
			switch {
			case had && !want:
				ops = append(ops, UserOp{User: old})
			case !had && want:
				adds = append(adds, UserOp{Add: true, User: cur})
			case had && want && old != cur:
				ops = append(ops, UserOp{User: old})
				adds = append(adds, UserOp{Add: true, User: cur})
			}
		}
	}
	return append(ops, adds...)
}
