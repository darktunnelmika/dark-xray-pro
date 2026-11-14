package hubcore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

const (
	apiTag  = "api"
	apiPort = 10085
)

func parseTemplate(raw []byte) (map[string]any, error) {
	var t map[string]any
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("%w: inbound template is not JSON", ErrInvalid)
	}
	p, _ := t["protocol"].(string)
	if plainProtocols[p] {
		if _, ok := t["port"]; !ok {
			return nil, fmt.Errorf("%w: inbound template needs a port", ErrInvalid)
		}
		return t, nil
	}
	if _, ok := clientShapes[p]; !ok {
		return nil, fmt.Errorf("%w: protocol %q is not a managed multi-user protocol", ErrInvalid, p)
	}
	if _, ok := t["port"]; !ok {
		return nil, fmt.Errorf("%w: inbound template needs a port", ErrInvalid)
	}
	if p == "shadowsocks" {
		st, _ := t["settings"].(map[string]any)
		m, _ := st["method"].(string)
		pw, _ := st["password"].(string)
		key, err := base64.StdEncoding.DecodeString(pw)
		if sub.SS2022KeySize(m) == 0 || err != nil || len(key) != sub.SS2022KeySize(m) {
			return nil, fmt.Errorf("%w: shadowsocks inbounds must use a 2022 method with a matching base64 server key", ErrInvalid)
		}
	}
	return t, nil
}

// clientShapes builds the per-protocol client entry. Every entry carries
// the email: it is the stats key and the enforcement key on the Node.
var clientShapes = map[string]func(c Client, flow string) map[string]any{
	"vless": func(c Client, flow string) map[string]any {
		m := map[string]any{"id": c.UUID, "email": c.Email}
		if flow != "" {
			m["flow"] = flow
		}
		return m
	},
	"vmess":  func(c Client, _ string) map[string]any { return map[string]any{"id": c.UUID, "email": c.Email} },
	"trojan": func(c Client, _ string) map[string]any { return map[string]any{"password": c.Secret, "email": c.Email} },
	// Hysteria2 (QUIC, UDP): the client's secret is its auth string.
	"hysteria": func(c Client, _ string) map[string]any { return map[string]any{"auth": c.Secret, "email": c.Email} },
	// TUIC (QUIC, UDP) runs beside Xray in the node agent: uuid + secret.
	"tuic": func(c Client, _ string) map[string]any {
		return map[string]any{"uuid": c.UUID, "password": c.Secret, "email": c.Email}
	},
	// Shadowsocks 2022 multi-user: each client gets a PSK derived from its
	// secret; the second argument carries the inbound's method.
	// WireGuard peers are built by wgPeers (keys and addresses); this entry
	// only marks the protocol as managed.
	"wireguard": func(c Client, _ string) map[string]any { return map[string]any{"email": c.Email} },
	// AmneziaWG runs beside Xray in the node agent; peers as for WireGuard.
	"amneziawg": func(c Client, _ string) map[string]any { return map[string]any{"email": c.Email} },
	"shadowsocks": func(c Client, method string) map[string]any {
		return map[string]any{"password": sub.SS2022UserKey(c.Secret, method), "email": c.Email}
	},
}

// sideProtocols run beside Xray in the node agent (v2.6+), never in Xray.
var sideProtocols = map[string]bool{"amneziawg": true, "mtproto": true, "tuic": true}

// sideMinor is the agent v2.x minor version that runs each side protocol.
var sideMinor = map[string]int{"amneziawg": 6, "mtproto": 6, "tuic": 7}

// agentAtLeast compares a node agent version such as "v2.6.0".
func agentAtLeast(v string, major, minor int) bool {
	var a, b, c int
	if n, _ := fmt.Sscanf(strings.TrimPrefix(v, "v"), "%d.%d.%d", &a, &b, &c); n < 2 {
		return false
	}
	return a > major || (a == major && b >= minor)
}

// stripSide removes AmneziaWG and MTProto inbounds from a rendered config
// (Xray's own check would refuse them; the agent runs them itself).
func stripSide(cfg []byte) []byte {
	var c map[string]any
	if json.Unmarshal(cfg, &c) != nil {
		return cfg
	}
	ins, _ := c["inbounds"].([]any)
	kept := ins[:0]
	for _, x := range ins {
		if m, _ := x.(map[string]any); m != nil {
			if p, _ := m["protocol"].(string); sideProtocols[p] {
				continue
			}
		}
		kept = append(kept, x)
	}
	c["inbounds"] = kept
	b, err := json.Marshal(c)
	if err != nil {
		return cfg
	}
	return b
}

// plainProtocols are inbounds without managed clients: a SOCKS or HTTP
// proxy (optionally with its own fixed accounts) and port forwarding
// (dokodemo-door, called tunnel in newer Xray).
var plainProtocols = map[string]bool{"socks": true, "http": true, "dokodemo-door": true, "tunnel": true, "mtproto": true}

type nodeInbound struct {
	tag            string
	template       []byte
	listen         string
	direct, tunnel int
}

// RenderNode builds the full Xray config for one node: its enabled
// inbounds with every assigned client, plus the stats API the agent needs.
// The output is deterministic so its hash identifies the revision.
func (s *Store) RenderNode(ctx context.Context, nodeID string) ([]byte, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT i.tag,i.template,r.listen_ip,r.direct_port,r.tunnel_port FROM inbound_routes r
JOIN inbounds i ON i.tag=r.tag WHERE r.node_id=? AND i.enabled=1 ORDER BY i.tag`, nodeID)
	if err != nil {
		return nil, err
	}
	var ins []nodeInbound
	for rows.Next() {
		var n nodeInbound
		var tpl string
		if err := rows.Scan(&n.tag, &tpl, &n.listen, &n.direct, &n.tunnel); err != nil {
			rows.Close()
			return nil, err
		}
		n.template = []byte(tpl)
		ins = append(ins, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	clients, err := s.clientsByInbound(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	inbounds := []any{map[string]any{
		"tag": apiTag, "listen": "127.0.0.1", "port": apiPort, "protocol": "dokodemo-door",
		"settings": map[string]any{"address": "127.0.0.1"},
	}}
	var agentVer string
	_ = s.ro.QueryRowContext(ctx, `SELECT agent_version FROM nodes WHERE id=?`, nodeID).Scan(&agentVer)
	for idx, in := range ins {
		t, err := parseTemplate(in.template)
		if err != nil {
			return nil, fmt.Errorf("inbound %s: %w", in.tag, err)
		}
		if p, _ := t["protocol"].(string); sideProtocols[p] && !agentAtLeast(agentVer, 2, sideMinor[p]) {
			continue // an older agent would hand it to Xray, which refuses the whole config
		}
		fbs, _ := t[fallbackKey].([]any)
		delete(t, fallbackKey)
		wgNet := wgSubnet(t)
		delete(t, wgKey)
		t["tag"] = in.tag
		t["port"] = in.direct
		t["listen"] = "0.0.0.0"
		if in.listen != "" {
			t["listen"] = in.listen
		}
		if err := s.resolveCerts(ctx, t); err != nil {
			return nil, fmt.Errorf("inbound %s: %w", in.tag, err)
		}
		proto := t["protocol"].(string)
		settings, _ := t["settings"].(map[string]any)
		if settings == nil {
			settings = map[string]any{}
		}
		if plainProtocols[proto] {
			// SOCKS, HTTP and port-forward inbounds carry no clients:
			// they go out exactly as written (their traffic is still
			// counted per inbound).
			t["settings"] = settings
			inbounds = append(inbounds, t)
			continue
		}
		flow, _ := settings["flow"].(string)
		delete(settings, "flow")
		if proto == "shadowsocks" {
			flow, _ = settings["method"].(string)
		}
		if proto == "wireguard" || proto == "amneziawg" {
			peers, err := s.wgPeers(ctx, in.tag, wgNet, clients[in.tag])
			if err != nil {
				return nil, fmt.Errorf("inbound %s: %w", in.tag, err)
			}
			settings["peers"] = peers
		} else {
			list := make([]any, 0, len(clients[in.tag]))
			for _, c := range clients[in.tag] {
				list = append(list, clientShapes[proto](c, flow))
			}
			settings["clients"] = list
		}
		if proto == "vless" {
			if _, ok := settings["decryption"]; !ok {
				settings["decryption"] = "none"
			}
		}
		t["settings"] = settings
		if len(fbs) > 0 {
			// Fallback children: internal WebSocket inbounds the TLS port
			// hands requests to by path. Their traffic is counted under the
			// parent tag (fallbackSuffix is stripped when usage arrives).
			var routes []any
			for i, x := range fbs {
				fb, _ := x.(map[string]any)
				fproto, _ := fb["protocol"].(string)
				fpath, _ := fb["path"].(string)
				shape := clientShapes[fproto]
				if shape == nil || fpath == "" || i > 7 {
					continue
				}
				port := fallbackPortBase + idx*8 + i
				clist := make([]any, 0, len(clients[in.tag]))
				for _, c := range clients[in.tag] {
					clist = append(clist, shape(c, ""))
				}
				cs := map[string]any{"clients": clist}
				if fproto == "vless" {
					cs["decryption"] = "none"
				}
				child := map[string]any{"tag": fmt.Sprintf("%s%s%d", in.tag, fallbackSuffix, i), "listen": "127.0.0.1", "port": port, "protocol": fproto,
					"settings": cs, "streamSettings": map[string]any{"network": "ws", "security": "none", "wsSettings": map[string]any{"path": fpath}}}
				if sn, ok := t["sniffing"]; ok {
					child["sniffing"] = sn
				}
				inbounds = append(inbounds, child)
				routes = append(routes, map[string]any{"path": fpath, "dest": port, "xver": 0})
			}
			if len(routes) > 0 {
				settings["fallbacks"] = routes
			}
		}
		inbounds = append(inbounds, t)
		if in.tunnel > 0 {
			// Tunnel: the same inbound on its own port and tag, so its
			// traffic is counted apart from Direct.
			tt := map[string]any{}
			for k, v := range t {
				tt[k] = v
			}
			tt["tag"], tt["port"] = in.tag+tunnelSuffix, in.tunnel
			inbounds = append(inbounds, tt)
		}
	}
	fwd, err := s.swapForwarders(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	inbounds = append(inbounds, fwd...)
	var swapTags []string
	for _, f := range fwd {
		if m, ok := f.(map[string]any); ok {
			if tag, _ := m["tag"].(string); tag != "" {
				swapTags = append(swapTags, tag)
			}
		}
	}
	outbounds, routing, dns, routeOnly := s.nodeRouting(ctx, nodeID, swapTags)
	if routeOnly {
		for _, x := range inbounds {
			m, _ := x.(map[string]any)
			if sn, ok := m["sniffing"].(map[string]any); ok && sn["enabled"] == true {
				sn["routeOnly"] = true
			}
		}
	}
	cfg := map[string]any{
		"log":   map[string]any{"loglevel": "warning"},
		"api":   map[string]any{"tag": apiTag, "services": []string{"StatsService", "HandlerService"}},
		"stats": map[string]any{},
		"policy": map[string]any{
			"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true, "statsUserOnline": true}},
			"system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true, "statsOutboundUplink": true, "statsOutboundDownlink": true},
		},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   routing,
	}
	if dns != nil {
		cfg["dns"] = dns
	}
	s.applyOverlay(ctx, cfg)
	return json.Marshal(cfg) // map keys are sorted: deterministic
}

func (s *Store) clientsByInbound(ctx context.Context, nodeID string) (map[string][]Client, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT ci.tag,c.email,c.uuid,c.secret FROM client_inbounds ci
JOIN clients c ON c.email=ci.email JOIN inbound_routes r ON r.tag=ci.tag WHERE r.node_id=? ORDER BY ci.tag,c.email`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Client{}
	for rows.Next() {
		var tag string
		var c Client
		if err := rows.Scan(&tag, &c.Email, &c.UUID, &c.Secret); err != nil {
			return nil, err
		}
		out[tag] = append(out[tag], c)
	}
	return out, rows.Err()
}

// nodeClients lists every client assigned to an inbound on the node.
func (s *Store) nodeClients(ctx context.Context, nodeID string) ([]Client, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT DISTINCT c.email,c.enabled,c.volume_bytes,c.expires_unix,c.duration_seconds,c.max_ips
FROM clients c JOIN client_inbounds ci ON ci.email=c.email JOIN inbound_routes r ON r.tag=ci.tag WHERE r.node_id=? ORDER BY c.email`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Client
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.Email, &c.Enabled, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds, &c.MaxIPs); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
