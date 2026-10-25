package inboundexec

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

var ErrInvalid = errors.New("invalid managed inbound execution")
var ErrConflict = errors.New("managed inbound execution conflict")

func Digest(v any) string        { raw, _ := json.Marshal(v); return BytesSHA(raw) }
func BytesSHA(raw []byte) string { d := sha256.Sum256(raw); return hex.EncodeToString(d[:]) }

// ConfigDigest includes all worker configurations, including external workers.
func ConfigDigest(b Bundle) string {
	return Digest(struct {
		Native     []byte
		Components []Component
	}{b.NativeConfig, b.Components})
}
func ManifestDigest(b Bundle) string {
	b.Signature = ""
	b.ManifestSHA = ""
	b.NativeConfig = nil
	b.Components = append([]Component(nil), b.Components...)
	for i := range b.Components {
		b.Components[i].Config = nil
		b.Components[i].Manifest = nil
	}
	return Digest(b)
}
func SealBundle(key []byte, b Bundle) (Bundle, error) {
	if len(key) < 16 || b.Version != Version || b.Generation == 0 || b.NodeID == "" || len(b.InstallationID) != 32 {
		return Bundle{}, ErrInvalid
	}
	b.Components = append([]Component(nil), b.Components...)
	b.ConfigSHA = ConfigDigest(b)
	b.ManifestSHA = ManifestDigest(b)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("dark-pro/inbound-execution/v1\x00" + b.ManifestSHA))
	b.Signature = hex.EncodeToString(h.Sum(nil))
	return b, nil
}
func VerifyBundle(key []byte, b Bundle) error {
	sealed, err := SealBundle(key, b)
	if err != nil || sealed.ConfigSHA != b.ConfigSHA || sealed.ManifestSHA != b.ManifestSHA || !hmac.Equal([]byte(sealed.Signature), []byte(b.Signature)) {
		return ErrInvalid
	}
	return nil
}

type DefaultCompiler struct{}

func (DefaultCompiler) Compile(ctx context.Context, r CompileRequest) (Bundle, error) {
	return Compile(ctx, r)
}

func socketNetwork(s *inboundspec.Spec) string {
	if s == nil {
		return "tcp"
	}
	protocols := []string{}
	switch a := s.TLS["alpn"].(type) {
	case []any:
		for _, v := range a {
			if value, ok := v.(string); ok {
				protocols = append(protocols, value)
			}
		}
	case []string:
		protocols = a
	}
	if s.Protocol == "masque" {
		tcp, udp := false, false
		for _, a := range protocols {
			if a == "h2" {
				tcp = true
			}
			if a == "h3" {
				udp = true
			}
		}
		if tcp && udp {
			return "tcp,udp"
		}
		if tcp {
			return "tcp"
		}
		return "udp"
	}
	if s.Network == "xhttp" && len(protocols) == 1 && protocols[0] == "h3" {
		return "udp"
	}
	switch s.Network {
	case "kcp", "wireguard", "hysteria", "amneziawg", "tuic":
		return "udp"
	}
	if s.Protocol == "shadowsocks" {
		if network, ok := s.Settings["network"].(string); ok {
			return network
		}
		return "tcp,udp"
	}
	if s.Protocol == "mixed" {
		if udp, ok := s.Settings["udp"].(bool); ok && udp {
			return "tcp,udp"
		}
		return "tcp"
	}
	if s.Protocol == "tunnel" {
		if n, ok := s.Settings["allowedNetwork"].(string); ok {
			return n
		}
	}
	return "tcp"
}

// Compile combines complete sibling snapshots, preserving typed transport and
// engine configurations. It creates no lease and makes no readiness claim.
func Compile(ctx context.Context, r CompileRequest) (Bundle, error) {
	b := Bundle{Version: Version, NodeID: r.NodeID, InstallationID: r.InstallationID, Generation: r.Generation, OperationID: r.OperationID,
		Revisions: []RevisionRef{}, Components: []Component{}, Listeners: []Listener{}, Principals: []Principal{}}
	if r.NodeID == "" || len(r.InstallationID) != 32 || r.Generation == 0 {
		return b, ErrInvalid
	}
	items := append([]CompileItem(nil), r.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Ref.InboundID < items[j].Ref.InboundID })
	inbounds := []json.RawMessage{}
	outbounds := []json.RawMessage{}
	rules := []json.RawMessage{}
	outTags := map[string]string{}
	tags := map[string]bool{}
	principalIDs := map[string]string{}
	refs := map[uint64]bool{}
	type socket struct {
		listen, network string
		port            uint16
	}
	var sockets []socket
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return b, err
		}
		if refs[item.Ref.InboundID] {
			return b, ErrConflict
		}
		refs[item.Ref.InboundID] = true
		for _, n := range item.Topology.Nodes {
			if n.NodeID != r.NodeID {
				continue
			}
			if n.InstallationID != r.InstallationID {
				return b, ErrConflict
			}
			b.Revisions = append(b.Revisions, item.Ref)
			var doc struct {
				Inbounds  []json.RawMessage `json:"inbounds"`
				Outbounds []json.RawMessage `json:"outbounds"`
				Routing   struct {
					Rules []json.RawMessage `json:"rules"`
				} `json:"routing"`
			}
			if json.Unmarshal(n.Candidate.Config, &doc) != nil {
				return b, ErrInvalid
			}
			inbounds = append(inbounds, doc.Inbounds...)
			rules = append(rules, doc.Routing.Rules...)
			for _, raw := range doc.Outbounds {
				var o struct {
					Tag string `json:"tag"`
				}
				if json.Unmarshal(raw, &o) != nil || o.Tag == "" {
					return b, ErrInvalid
				}
				sha := BytesSHA(raw)
				if old, ok := outTags[o.Tag]; ok {
					if old != sha {
						return b, ErrConflict
					}
					continue
				}
				outTags[o.Tag] = sha
				outbounds = append(outbounds, raw)
			}
			for _, e := range n.Candidate.EngineConfigs {
				b.Components = append(b.Components, Component{Engine: e.Engine, Tag: e.Tag, Config: append([]byte(nil), e.Config...), Manifest: append([]byte(nil), e.Manifest...), SHA256: BytesSHA(e.Config)})
			}
			customer := false
			for _, l := range n.Candidate.Listeners {
				if tags[l.Tag] {
					return b, ErrConflict
				}
				tags[l.Tag] = true
				v := Listener{InboundID: l.InboundID, Tag: l.Tag, Engine: "xray", Protocol: "vless", Transport: item.Profile.Transport, Security: "reality", Kind: l.Kind, Listen: n.ListenIP, Port: l.NodePort, Network: socketNetwork(item.Profile.Spec), ResourceKind: "socket", AccountingMode: item.AccountingMode}
				if s := item.Profile.Spec; s != nil {
					v.Protocol, v.Transport, v.Security = s.Protocol, s.Network, s.Security
					for _, p := range inboundspec.Catalog() {
						if p.Protocol == s.Protocol {
							v.Engine = p.Engine
						}
					}
					if s.Protocol == "tun" {
						v.ResourceKind = "interface"
						v.Port = 0
					}
					if s.Network == "xdrive" {
						v.ResourceKind = "storage"
						v.Port = 0
						v.Network = ""
					}
				}
				if l.Kind == "swap_relay" {
					v.Engine = "xray"
					v.Protocol = "dokodemo-door"
					v.Security = "none"
					v.Transport = "tcp"
					v.AccountingMode = AccountingOwnerAggregate
				} else {
					customer = true
				}
				if v.ResourceKind == "socket" {
					for _, network := range strings.Split(v.Network, ",") {
						for _, prior := range sockets {
							if prior.port == v.Port && prior.network == network && (prior.listen == v.Listen || net.ParseIP(prior.listen).IsUnspecified() || net.ParseIP(v.Listen).IsUnspecified()) {
								return b, ErrConflict
							}
						}
						sockets = append(sockets, socket{v.Listen, network, v.Port})
					}
				}
				b.Listeners = append(b.Listeners, v)
				if v.AccountingMode == AccountingOwnerAggregate {
					role := "owner_aggregate"
					if l.Kind == "swap_relay" {
						role = "transit"
					}
					p := Principal{ID: "aggregate-" + BytesSHA([]byte(r.NodeID + "\x00" + l.Tag))[:32], Engine: v.Engine, Role: role, Counter: "inbound>>>" + l.Tag + ">>>traffic", InboundID: l.InboundID}
					if v.Engine != "xray" {
						p.ComponentTag = l.Tag
					}
					b.Principals = append(b.Principals, p)
				}
				if l.Kind != "swap_relay" && v.Engine != "xray" && item.AccountingMode == AccountingPerClient {
					for _, base := range item.Principals {
						p := base
						p.ComponentTag = l.Tag
						p.ID = "exec-" + BytesSHA([]byte(base.ID + "\x00" + l.Tag))[:32]
						b.Principals = append(b.Principals, p)
					}
				}
			}
			if customer && item.AccountingMode == AccountingPerClient {
				for _, p := range item.Principals {
					if p.Engine != "xray" {
						continue
					}
					sha := Digest(p)
					if old, ok := principalIDs[p.ID]; ok {
						if old != sha {
							return b, ErrConflict
						}
						continue
					}
					principalIDs[p.ID] = sha
					b.Principals = append(b.Principals, p)
				}
			}
		}
	}
	if len(outbounds) == 0 {
		outbounds = []json.RawMessage{json.RawMessage(`{"tag":"direct","protocol":"freedom"}`)}
	}
	if err := validateDirectComponentRoutes(b.Components, outbounds, rules); err != nil {
		return b, err
	}
	doc := map[string]any{"log": map[string]string{"loglevel": "none"}, "stats": map[string]any{}, "policy": map[string]any{"levels": map[string]any{"0": map[string]bool{"statsUserUplink": true, "statsUserDownlink": true}}, "system": map[string]bool{"statsInboundUplink": true, "statsInboundDownlink": true}}, "inbounds": inbounds, "outbounds": outbounds}
	if len(rules) > 0 {
		doc["routing"] = map[string]any{"domainStrategy": "AsIs", "rules": rules}
	}
	raw, err := json.Marshal(doc)
	if err != nil || len(raw) > 16<<20 {
		return b, ErrInvalid
	}
	b.NativeConfig = raw
	sort.Slice(b.Listeners, func(i, j int) bool { return b.Listeners[i].Tag < b.Listeners[j].Tag })
	sort.Slice(b.Principals, func(i, j int) bool { return b.Principals[i].ID < b.Principals[j].ID })
	sort.Slice(b.Components, func(i, j int) bool { return b.Components[i].Tag < b.Components[j].Tag })
	for i, p := range b.Principals {
		if p.ID == "" || p.Counter == "" || i > 0 && b.Principals[i-1].ID == p.ID {
			return b, fmt.Errorf("principal identity: %w", ErrInvalid)
		}
	}
	b.ConfigSHA = ConfigDigest(b)
	b.ManifestSHA = ManifestDigest(b)
	return b, nil
}
