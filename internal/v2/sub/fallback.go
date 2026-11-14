package sub

import (
	"encoding/json"
)

// ExpandFallbacks adds one inbound per fallback route of a TLS inbound
// (template key "dark_fallbacks"): the same address, port and TLS
// settings, but a WebSocket path and the route's protocol. Every format
// (links, sing-box, Clash, Xray JSON) then lists them like any other.
func ExpandFallbacks(ins []Inbound) []Inbound {
	out := make([]Inbound, 0, len(ins))
	for _, in := range ins {
		out = append(out, in)
		var t map[string]any
		if json.Unmarshal(in.Template, &t) != nil {
			continue
		}
		fbs, _ := t["dark_fallbacks"].([]any)
		st, _ := t["streamSettings"].(map[string]any)
		if len(fbs) == 0 || st == nil || st["security"] != "tls" {
			continue
		}
		tls, _ := st["tlsSettings"].(map[string]any)
		for _, x := range fbs {
			fb, _ := x.(map[string]any)
			proto, _ := fb["protocol"].(string)
			path, _ := fb["path"].(string)
			name, _ := fb["name"].(string)
			if proto == "" || path == "" {
				continue
			}
			ctls := map[string]any{"serverName": "", "alpn": []string{"http/1.1"}}
			if tls != nil {
				ctls["serverName"] = tls["serverName"]
			}
			child := map[string]any{"protocol": proto, "settings": map[string]any{},
				"streamSettings": map[string]any{"network": "ws", "security": "tls", "tlsSettings": ctls, "wsSettings": map[string]any{"path": path}}}
			raw, _ := json.Marshal(child)
			c := in
			c.Template = raw
			c.Meta.Remark = or(in.Meta.Remark, in.Tag) + " " + name
			out = append(out, c)
		}
	}
	return out
}
