package hubcore

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// RealityKeys returns an X25519 key pair encoded the way Xray expects
// (unpadded URL-safe base64).
func RealityKeys() (private, public string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(k.Bytes()), enc.EncodeToString(k.PublicKey().Bytes()), nil
}

// PresetRequest describes an inbound in owner terms; the HUB fills in keys
// and every Xray detail.
type PresetRequest struct {
	Tag       string         `json:"tag"`
	NodeID    string         `json:"node_id"`
	Preset    string         `json:"preset"` // vless-reality-tcp | vless-reality-grpc | vless-reality-xhttp | vless-ws | trojan-ws
	Port      int            `json:"port"`
	SNI       string         `json:"sni"`  // REALITY target name, e.g. www.speedtest.net
	Path      string         `json:"path"` // ws/xhttp path or gRPC service name
	Remark    string         `json:"remark"`
	Endpoints []sub.Endpoint `json:"endpoints"` // Direct and/or Tunnel addresses shown in subscriptions
	Cert      string         `json:"cert"`      // TLS presets: stored certificate id; SNI is the domain
}

// Presets lists what the panel offers.
var Presets = []string{"vless-reality-tcp", "vless-reality-grpc", "vless-reality-xhttp", "vless-ws", "vmess-ws", "vmess-tcp", "trojan-ws", "ss2022",
	"vless-tls-ws", "vless-tls-grpc", "vless-tls-xhttp", "vmess-tls-ws", "trojan-tls-tcp"}

// BuildPreset renders the template and client metadata for a preset.
func BuildPreset(p PresetRequest) (template, meta []byte, err error) {
	if p.Port < 1 || p.Port > 65535 || len(p.Endpoints) == 0 {
		return nil, nil, fmt.Errorf("%w: port and at least one endpoint are required", ErrInvalid)
	}
	m := sub.Meta{Endpoints: p.Endpoints, Remark: p.Remark, Fingerprint: "chrome"}
	t := map[string]any{"listen": "0.0.0.0", "port": p.Port, "protocol": "vless", "settings": map[string]any{},
		"sniffing": map[string]any{"enabled": true, "destOverride": []string{"http", "tls", "quic"}}}
	reality := func(network string, extra map[string]any) error {
		if p.SNI == "" {
			return fmt.Errorf("%w: REALITY needs a target name (sni)", ErrInvalid)
		}
		priv, pub, err := RealityKeys()
		if err != nil {
			return err
		}
		sidb := make([]byte, 4)
		if _, err := rand.Read(sidb); err != nil {
			return err
		}
		sid := hex.EncodeToString(sidb)
		m.PublicKey, m.ShortID = pub, sid
		st := map[string]any{"network": network, "security": "reality", "realitySettings": map[string]any{
			"dest": p.SNI + ":443", "serverNames": []string{p.SNI}, "privateKey": priv, "shortIds": []string{sid}}}
		for k, v := range extra {
			st[k] = v
		}
		t["streamSettings"] = st
		return nil
	}
	tlsStream := func(network string, extra map[string]any) error {
		if p.Cert == "" || p.SNI == "" {
			return fmt.Errorf("%w: TLS presets need a certificate and its domain (sni)", ErrInvalid)
		}
		st := map[string]any{"network": network, "security": "tls", "tlsSettings": map[string]any{
			"serverName": p.SNI, "alpn": []string{"h2", "http/1.1"}, "certificates": []any{map[string]any{"dark_cert": p.Cert}}}}
		for k, v := range extra {
			st[k] = v
		}
		t["streamSettings"] = st
		return nil
	}
	path := p.Path
	switch p.Preset {
	case "vless-tls-ws", "vmess-tls-ws":
		if path == "" {
			path = "/" + randomHex(6)
		}
		if p.Preset == "vmess-tls-ws" {
			t["protocol"] = "vmess"
		}
		err = tlsStream("ws", map[string]any{"wsSettings": map[string]any{"path": path}})
	case "vless-tls-grpc":
		if path == "" {
			path = randomHex(6)
		}
		err = tlsStream("grpc", map[string]any{"grpcSettings": map[string]any{"serviceName": path}})
	case "vless-tls-xhttp":
		if path == "" {
			path = "/" + randomHex(6)
		}
		err = tlsStream("xhttp", map[string]any{"xhttpSettings": map[string]any{"path": path, "mode": "auto"}})
	case "trojan-tls-tcp":
		t["protocol"] = "trojan"
		err = tlsStream("tcp", nil)
	case "vless-reality-tcp":
		t["settings"] = map[string]any{"flow": "xtls-rprx-vision"}
		err = reality("tcp", nil)
	case "vless-reality-grpc":
		if path == "" {
			path = randomHex(6)
		}
		err = reality("grpc", map[string]any{"grpcSettings": map[string]any{"serviceName": path}})
	case "vless-reality-xhttp":
		if path == "" {
			path = "/" + randomHex(6)
		}
		err = reality("xhttp", map[string]any{"xhttpSettings": map[string]any{"path": path, "mode": "auto"}})
	case "ss2022":
		// Shadowsocks 2022 with AES-128-GCM: no TLS layer, fits tunnels and
		// clients that cannot do REALITY.
		key := make([]byte, 16)
		if _, err := rand.Read(key); err != nil {
			return nil, nil, err
		}
		t["protocol"] = "shadowsocks"
		t["settings"] = map[string]any{"method": "2022-blake3-aes-128-gcm", "password": base64.StdEncoding.EncodeToString(key), "network": "tcp,udp"}
	case "vmess-tcp":
		t["protocol"] = "vmess"
		t["streamSettings"] = map[string]any{"network": "tcp", "security": "none"}
	case "vless-ws", "trojan-ws", "vmess-ws":
		if path == "" {
			path = "/" + randomHex(6)
		}
		switch p.Preset {
		case "trojan-ws":
			t["protocol"] = "trojan"
		case "vmess-ws":
			t["protocol"] = "vmess"
		}
		// Plain WebSocket is meant to sit behind a CDN or TLS front.
		t["streamSettings"] = map[string]any{"network": "ws", "security": "none", "wsSettings": map[string]any{"path": path}}
	default:
		return nil, nil, fmt.Errorf("%w: unknown preset %q", ErrInvalid, p.Preset)
	}
	if err != nil {
		return nil, nil, err
	}
	if template, err = json.Marshal(t); err != nil {
		return nil, nil, err
	}
	meta, err = json.Marshal(m)
	return template, meta, err
}

// CreatePreset builds and stores a preset inbound.
func (s *Store) CreatePreset(ctx context.Context, p PresetRequest) error {
	if p.Cert != "" {
		var x int
		if err := s.ro.QueryRowContext(ctx, `SELECT 1 FROM certs WHERE id=?`, p.Cert).Scan(&x); err != nil {
			return fmt.Errorf("%w: unknown certificate %q", ErrInvalid, p.Cert)
		}
	}
	tpl, meta, err := BuildPreset(p)
	if err != nil {
		return err
	}
	return s.PutInboundMeta(ctx, p.Tag, p.NodeID, tpl, meta, true)
}

// Overview is the dashboard summary.
type Overview struct {
	Nodes, NodesOnline         int
	Clients, ClientsActive     int
	OnlineUsers                int
	TrafficToday, TrafficTotal int64
}

func (s *Store) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	now := s.now()
	nodes, err := s.Nodes(ctx)
	if err != nil {
		return o, err
	}
	o.Nodes = len(nodes)
	for _, n := range nodes {
		if n.Online(now) {
			o.NodesOnline++
		}
	}
	clients, err := s.Clients(ctx)
	if err != nil {
		return o, err
	}
	o.Clients = len(clients)
	for _, c := range clients {
		if c.Enabled && c.Remaining != 0 && (c.EffectiveUntil == 0 || c.EffectiveUntil > now.Unix()) {
			o.ClientsActive++
		}
		if c.Online {
			o.OnlineUsers++
		}
		o.TrafficTotal += c.Usage.Up + c.Usage.Down
	}
	day := now.UTC().Format("2006-01-02")
	err = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(up+down),0) FROM usage_daily WHERE day=?`, day).Scan(&o.TrafficToday)
	return o, err
}

// InboundView is an inbound without its private key.
type InboundView struct {
	Tag      string          `json:"tag"`
	NodeID   string          `json:"node_id"`
	Protocol string          `json:"protocol"`
	Network  string          `json:"network"`
	Security string          `json:"security"`
	Port     int             `json:"port"`
	Enabled  bool            `json:"enabled"`
	Meta     json.RawMessage `json:"meta"`
	Clients  int             `json:"clients"`
	Today    int64           `json:"traffic_today"`
	Total    int64           `json:"traffic_total"`
}

func (s *Store) Inbounds(ctx context.Context) ([]InboundView, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT i.tag,i.node_id,i.template,i.meta,i.enabled,(SELECT COUNT(*) FROM client_inbounds ci WHERE ci.tag=i.tag),
(SELECT COALESCE(SUM(up+down),0) FROM inbound_traffic t WHERE t.tag=i.tag AND t.day=?),
(SELECT COALESCE(SUM(up+down),0) FROM inbound_traffic t WHERE t.tag=i.tag)
FROM inbounds i ORDER BY i.node_id,i.tag`, s.now().UTC().Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InboundView
	for rows.Next() {
		var v InboundView
		var tpl, meta string
		if err := rows.Scan(&v.Tag, &v.NodeID, &tpl, &meta, &v.Enabled, &v.Clients, &v.Today, &v.Total); err != nil {
			return nil, err
		}
		var t struct {
			Protocol string `json:"protocol"`
			Port     int    `json:"port"`
			Stream   struct {
				Network  string `json:"network"`
				Security string `json:"security"`
			} `json:"streamSettings"`
		}
		_ = json.Unmarshal([]byte(tpl), &t)
		v.Protocol, v.Port, v.Network, v.Security, v.Meta = t.Protocol, t.Port, t.Stream.Network, t.Stream.Security, json.RawMessage(meta)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) DeleteInbound(ctx context.Context, tag string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM inbounds WHERE tag=?`, tag)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
