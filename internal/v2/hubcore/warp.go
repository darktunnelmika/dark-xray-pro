package hubcore

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// warpAPI is replaceable in tests.
var warpAPI = "https://api.cloudflareclient.com/v0a2483/reg"

// RegisterWARP creates a free Cloudflare WARP account and returns a ready
// Xray WireGuard outbound for it.
func RegisterWARP(ctx context.Context) (json.RawMessage, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pub := base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	priv := base64.StdEncoding.EncodeToString(key.Bytes())
	body, _ := json.Marshal(map[string]any{"key": pub, "install_id": "", "fcm_token": "", "tos": time.Now().UTC().Format(time.RFC3339),
		"type": "Android", "model": "PC", "locale": "en_US", "warp_enabled": true})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, warpAPI, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "okhttp/3.12.1")
	req.Header.Set("CF-Client-Version", "a-6.30-3596")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("warp: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("warp: HTTP %d", resp.StatusCode)
	}
	var r struct {
		Config struct {
			ClientID string `json:"client_id"`
			Peers    []struct {
				PublicKey string `json:"public_key"`
				Endpoint  struct {
					Host string `json:"host"`
				} `json:"endpoint"`
			} `json:"peers"`
			Interface struct {
				Addresses struct {
					V4 string `json:"v4"`
					V6 string `json:"v6"`
				} `json:"addresses"`
			} `json:"interface"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || len(r.Config.Peers) == 0 {
		return nil, fmt.Errorf("warp: unexpected answer")
	}
	p := r.Config.Peers[0]
	endpoint := p.Endpoint.Host
	if endpoint == "" || strings.HasSuffix(endpoint, ":0") {
		endpoint = warpEndpointV4
	}
	addr := []string{}
	if a := r.Config.Interface.Addresses.V4; a != "" {
		addr = append(addr, a+"/32")
	}
	if a := r.Config.Interface.Addresses.V6; a != "" {
		addr = append(addr, a+"/128")
	}
	settings := map[string]any{"secretKey": priv, "address": addr, "mtu": 1280,
		"peers": []any{map[string]any{"publicKey": p.PublicKey, "endpoint": endpoint}}}
	if id, err := base64.StdEncoding.DecodeString(r.Config.ClientID); err == nil && len(id) == 3 {
		settings["reserved"] = []int{int(id[0]), int(id[1]), int(id[2])}
	}
	return json.Marshal(map[string]any{"protocol": "wireguard", "settings": settings})
}

// warpEndpointV4 is Cloudflare's WARP endpoint by its IPv4 address. The
// name engage.cloudflareclient.com also resolves to IPv6, and Xray then
// picks either at random; on servers whose IPv6 path to it drops UDP the
// tunnel comes up only some of the time (measured on a node: 8 of 8 over
// IPv4, 7 of 8 over IPv6, 8 of 10 by name).
const warpEndpointV4 = "162.159.192.1:2408"

// pinWARPEndpoint rewrites a WARP outbound's endpoint from the name to the
// IPv4 address, in place. Accounts saved before this keep working as saved;
// only what the nodes are given changes.
func pinWARPEndpoint(out map[string]any) {
	set, _ := out["settings"].(map[string]any)
	peers, _ := set["peers"].([]any)
	for _, p := range peers {
		peer, _ := p.(map[string]any)
		if ep, _ := peer["endpoint"].(string); strings.HasPrefix(ep, "engage.cloudflareclient.com:") {
			peer["endpoint"] = "162.159.192.1:" + strings.TrimPrefix(ep, "engage.cloudflareclient.com:")
		}
	}
}

func (s *Store) ensureWARPSchema() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS warp_accounts (
  node_id TEXT PRIMARY KEY,
  config TEXT NOT NULL,
  created_unix INTEGER NOT NULL
)`)
	return err
}

// nodeWARP returns a node's own WARP outbound, or nil when it has none.
func (s *Store) nodeWARP(ctx context.Context, nodeID string) map[string]any {
	var raw string
	if s.ro.QueryRowContext(ctx, `SELECT config FROM warp_accounts WHERE node_id=?`, nodeID).Scan(&raw) != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil || m["protocol"] == nil {
		return nil
	}
	return m
}

// registerWARP is replaceable in tests.
var registerWARP = RegisterWARP

// EnsureWARP makes sure the routing has a WARP outbound and that this node
// has a WARP account of its own, registering what is missing. It returns
// the outbound's tag.
func (s *Store) EnsureWARP(ctx context.Context, nodeID string) (string, error) {
	var n int
	if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE id=?`, nodeID).Scan(&n); err != nil || n == 0 {
		return "", ErrNotFound
	}
	r := s.Routing(ctx)
	tag := ""
	for _, o := range r.Outbounds {
		if o.Kind == "warp" && onNode(o.Nodes, nodeID) {
			tag = o.Tag
			break
		}
	}
	if s.nodeWARP(ctx, nodeID) == nil {
		cfg, err := registerWARP(ctx)
		if err != nil {
			return "", err
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO warp_accounts(node_id,config,created_unix) VALUES(?,?,?)
ON CONFLICT(node_id) DO UPDATE SET config=excluded.config, created_unix=excluded.created_unix`, nodeID, string(cfg), s.now().Unix()); err != nil {
			return "", err
		}
		if tag == "" { // the first WARP anywhere: the same account serves as the shared one
			tag = "warp"
			for _, o := range r.Outbounds {
				if o.Tag == tag {
					tag = "warp-cf"
				}
			}
			r.Outbounds = append(r.Outbounds, RouteOutbound{Tag: tag, Remark: "Cloudflare WARP", Kind: "warp", Config: cfg})
			if err := s.PutRouting(ctx, r); err != nil {
				return "", err
			}
		}
	}
	if tag == "" {
		return "", fmt.Errorf("%w: the WARP outbound is limited to other nodes", ErrInvalid)
	}
	return tag, nil
}

// SetNodeWARP saves a node's WARP choice for one path ("direct" or
// "tunnel"), giving the node a WARP account first when it turns it on.
func (s *Store) SetNodeWARP(ctx context.Context, nodeID, path, mode string) error {
	if path != "direct" && path != "tunnel" {
		return fmt.Errorf("%w: unknown path %q", ErrInvalid, path)
	}
	if mode != "" {
		if _, err := s.EnsureWARP(ctx, nodeID); err != nil {
			return err
		}
	}
	r := s.Routing(ctx)
	if r.WARP == nil {
		r.WARP = map[string]NodeWARP{}
	}
	w := r.WARP[nodeID]
	if path == "direct" {
		w.Direct = mode
	} else {
		w.Tunnel = mode
	}
	r.WARP[nodeID] = w
	return s.PutRouting(ctx, r)
}
