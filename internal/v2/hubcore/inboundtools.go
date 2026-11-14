package hubcore

import (
	"context"
	"crypto/ecdh"
	"crypto/mlkem"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// Keys the inbound editor asks for. Nothing here is stored until the
// owner saves the inbound.
type KeyRequest struct {
	Kind    string `json:"kind"`    // reality | shortid | vlessenc-x25519 | vlessenc-mlkem
	Private string `json:"private"` // reality: derive the public key of a pasted private key
}

type KeyReply struct {
	Private    string `json:"private,omitempty"`
	Public     string `json:"public,omitempty"`
	ShortID    string `json:"short_id,omitempty"`
	Decryption string `json:"decryption,omitempty"` // VLESS encryption: server side
	Encryption string `json:"encryption,omitempty"` // and what clients send
}

const vlessEncPrefix = "mlkem768x25519plus.native."

// GenerateKeys makes REALITY keys, short ids and VLESS encryption pairs in
// the formats Xray 26 expects ("xray x25519" / "xray vlessenc").
func GenerateKeys(r KeyRequest) (KeyReply, error) {
	enc := base64.RawURLEncoding
	switch r.Kind {
	case "reality":
		if r.Private != "" {
			raw, err := enc.DecodeString(strings.TrimSpace(r.Private))
			if err != nil || len(raw) != 32 {
				return KeyReply{}, fmt.Errorf("%w: not an X25519 private key", ErrInvalid)
			}
			k, err := ecdh.X25519().NewPrivateKey(raw)
			if err != nil {
				return KeyReply{}, fmt.Errorf("%w: %v", ErrInvalid, err)
			}
			return KeyReply{Private: r.Private, Public: enc.EncodeToString(k.PublicKey().Bytes())}, nil
		}
		priv, pub, err := RealityKeys()
		return KeyReply{Private: priv, Public: pub}, err
	case "shortid":
		b := make([]byte, 8)
		_, err := rand.Read(b)
		return KeyReply{ShortID: hex.EncodeToString(b)}, err
	case "vlessenc-x25519":
		k, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return KeyReply{}, err
		}
		return KeyReply{Decryption: vlessEncPrefix + "600s." + enc.EncodeToString(k.Bytes()),
			Encryption: vlessEncPrefix + "0rtt." + enc.EncodeToString(k.PublicKey().Bytes())}, nil
	case "vlessenc-mlkem":
		dk, err := mlkem.GenerateKey768()
		if err != nil {
			return KeyReply{}, err
		}
		return KeyReply{Decryption: vlessEncPrefix + "600s." + enc.EncodeToString(dk.Bytes()),
			Encryption: vlessEncPrefix + "0rtt." + enc.EncodeToString(dk.EncapsulationKey().Bytes())}, nil
	}
	return KeyReply{}, fmt.Errorf("%w: unknown key kind", ErrInvalid)
}

// inboundNodes lists the nodes an inbound runs on.
func (s *Store) inboundNodes(ctx context.Context, tag string) ([]string, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT node_id FROM inbound_routes WHERE tag=? UNION SELECT node_id FROM inbounds WHERE tag=? AND node_id<>''`, tag, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// PutInboundChecked saves an edited inbound, then has Xray check the new
// config of every node it runs on. If Xray refuses it the previous version
// is put back, so a typo in the editor never takes a node down.
func (s *Store) PutInboundChecked(ctx context.Context, tag, nodeID string, template, meta []byte, enabled bool) error {
	old, oldErr := s.InboundRaw(ctx, tag)
	if err := s.PutInboundMeta(ctx, tag, nodeID, template, meta, enabled); err != nil {
		return err
	}
	nodes, err := s.inboundNodes(ctx, tag)
	if err != nil {
		return err
	}
	for _, id := range nodes {
		cfg, err := s.RenderNode(ctx, id)
		if err == nil {
			err = s.testConfig(ctx, cfg)
		}
		if err != nil {
			if oldErr == nil {
				_ = s.PutInboundMeta(ctx, tag, old.NodeID, old.Template, old.Meta, old.Enabled)
			} else {
				_ = s.DeleteInbound(ctx, tag)
			}
			return fmt.Errorf("%w: node %s: %v", ErrInvalid, id, err)
		}
	}
	return nil
}

// CloneRequest copies an inbound under a new tag and port.
type CloneRequest struct {
	Tag     string `json:"tag"`
	Port    int    `json:"port"`
	Clients bool   `json:"clients"` // give the copy to the same clients
}

// CloneInbound copies an inbound with fresh REALITY keys (and a fresh
// Shadowsocks server key), on the same nodes but a new port.
func (s *Store) CloneInbound(ctx context.Context, src string, req CloneRequest) error {
	if !idRE.MatchString(req.Tag) || req.Port < 1 || req.Port > 65535 {
		return fmt.Errorf("%w: a new tag and port are needed", ErrInvalid)
	}
	var x int
	if s.ro.QueryRowContext(ctx, `SELECT 1 FROM inbounds WHERE tag=?`, req.Tag).Scan(&x) == nil {
		return fmt.Errorf("%w: an inbound named %s already exists", ErrInvalid, req.Tag)
	}
	raw, err := s.InboundRaw(ctx, src)
	if err != nil {
		return err
	}
	var t map[string]any
	if err := json.Unmarshal(raw.Template, &t); err != nil {
		return fmt.Errorf("%w: inbound template is not JSON", ErrInvalid)
	}
	var m map[string]any
	_ = json.Unmarshal(raw.Meta, &m)
	if m == nil {
		m = map[string]any{}
	}
	oldPort := intOf(t["port"])
	t["port"] = req.Port
	if st, _ := t["streamSettings"].(map[string]any); st != nil {
		if rs, _ := st["realitySettings"].(map[string]any); rs != nil {
			k, err := GenerateKeys(KeyRequest{Kind: "reality"})
			if err != nil {
				return err
			}
			sid, _ := GenerateKeys(KeyRequest{Kind: "shortid"})
			rs["privateKey"], rs["shortIds"] = k.Private, []any{sid.ShortID[:8]}
			m["public_key"], m["short_id"] = k.Public, sid.ShortID[:8]
		}
	}
	if set, _ := t["settings"].(map[string]any); set != nil && t["protocol"] == "shadowsocks" {
		meth, _ := set["method"].(string)
		if n := sub.SS2022KeySize(meth); n > 0 {
			b := make([]byte, n)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			set["password"] = base64.StdEncoding.EncodeToString(b)
		}
	}
	if eps, _ := m["endpoints"].([]any); eps != nil {
		for _, e := range eps {
			if ep, _ := e.(map[string]any); ep != nil && intOf(ep["port"]) == oldPort {
				ep["port"] = req.Port
			}
		}
	}
	tb, _ := json.Marshal(t)
	mb, _ := json.Marshal(m)
	routes, err := s.Routes(ctx, src)
	if err != nil {
		return err
	}
	if err := s.PutInboundMeta(ctx, req.Tag, raw.NodeID, tb, mb, raw.Enabled); err != nil {
		return err
	}
	if len(routes) > 0 {
		for i := range routes {
			r := &routes[i]
			for j := range r.Hosts {
				if r.Hosts[j].Port == r.DirectPort || r.Hosts[j].Path == "direct" && r.Hosts[j].Port == oldPort {
					r.Hosts[j].Port = req.Port
				}
			}
			r.DirectPort, r.TunnelPort = req.Port, 0
			kept := r.Hosts[:0]
			for _, h := range r.Hosts {
				if h.Path != "tunnel" {
					kept = append(kept, h)
				}
			}
			r.Hosts = kept
		}
		if err := s.PutRoutes(ctx, req.Tag, routes); err != nil {
			_ = s.DeleteInbound(ctx, req.Tag)
			return err
		}
	}
	if req.Clients {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO client_inbounds(email,tag) SELECT email,? FROM client_inbounds WHERE tag=?`, req.Tag, src); err != nil {
			return err
		}
	}
	return nil
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

// RotateClient gives a client new credentials (UUID and secret: every
// config stops working until the customer refreshes the subscription)
// and/or a new subscription link (the old link stops answering).
func (s *Store) RotateClient(ctx context.Context, email string, creds, link bool) error {
	if !creds && !link {
		return fmt.Errorf("%w: nothing to change", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var x int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=?`, email).Scan(&x); err != nil {
		return ErrNotFound
	}
	if creds {
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET uuid=?, secret=? WHERE email=?`, RandomUUID(), randomHex(16), email); err != nil {
			return err
		}
	}
	if link {
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET sub_token=? WHERE email=?`, randomHex(16), email); err != nil {
			return err
		}
		_, _ = tx.ExecContext(ctx, `DELETE FROM legacy_sub_tokens WHERE email=?`, email)
	}
	return tx.Commit()
}

// ClientLink is one ready config of a client's subscription.
type ClientLink struct {
	Name string `json:"name"`
	Link string `json:"link"`
	Kind string `json:"kind"` // vless, vmess, trojan, ss, hysteria2, wireguard
}

// ClientLinks lists every config in a client's subscription, one per
// inbound address, without the info entries.
func (s *Store) ClientLinks(ctx context.Context, email string) ([]ClientLink, error) {
	links, _, err := s.Subscription(ctx, email)
	if err != nil {
		return nil, err
	}
	out := []ClientLink{}
	for _, l := range links {
		if strings.Contains(l, "00000000-0000-0000-0000-000000000000@127.0.0.1:1") {
			continue
		}
		kind, _, _ := strings.Cut(l, "://")
		name := ""
		if i := strings.LastIndex(l, "#"); i >= 0 {
			name, _ = url.PathUnescape(l[i+1:])
		}
		if kind == "vmess" {
			if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(l, "vmess://")); err == nil {
				var v struct {
					PS string `json:"ps"`
				}
				if json.Unmarshal(b, &v) == nil {
					name = v.PS
				}
			}
		}
		out = append(out, ClientLink{Name: name, Link: l, Kind: kind})
	}
	return out, nil
}

// canSeeClient tells whether the signed-in principal may see this client
// (owners and staff see all, resellers only their own).
func (s *Store) canSeeClient(r *http.Request, email string) bool {
	list, err := s.scopedClients(r.Context(), who(r))
	if err != nil {
		return false
	}
	for _, c := range list {
		if c.Email == email {
			return true
		}
	}
	return false
}
