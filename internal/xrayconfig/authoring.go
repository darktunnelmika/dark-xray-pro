package xrayconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

// EngineCandidate keeps private adapter configurations out of browser previews.
// A supervisor must explicitly support the named engine before installation.
type EngineCandidate struct {
	Engine   string `json:"engine"`
	Tag      string `json:"tag"`
	SHA256   string `json:"sha256"`
	Config   []byte `json:"-"`
	Manifest []byte `json:"-"`
}

func validateAuthoringProfile(p Profile) error {
	if p.InboundID == 0 || len(p.Users) > 20000 || p.Options != nil || p.TLS != nil || p.RealityPrivateKey != "" {
		return ErrInvalid
	}
	if err := inboundspec.Validate(*p.Spec); err != nil {
		return err
	}
	users, uuids := map[string]bool{}, map[string]bool{}
	for _, user := range p.Users {
		email, uuid := strings.ToLower(user.Email), strings.ToLower(user.UUID)
		if !uuidRE.MatchString(user.UUID) || !emailRE.MatchString(user.Email) || strings.Contains(user.Email, "..") || users[email] || uuids[uuid] {
			return ErrInvalid
		}
		users[email], uuids[uuid] = true, true
	}
	return nil
}

func renderAuthoringInbound(p Profile, listen string, port uint16, tag string) (any, *EngineCandidate, error) {
	users := make([]inboundspec.User, len(p.Users))
	for i, user := range p.Users {
		users[i] = inboundspec.User{ID: user.ID, UUID: user.UUID, Email: user.Email, Password: user.Password, Addresses: user.Addresses}
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	rendered, err := inboundspec.Render(*p.Spec, listen, port, tag, users)
	if err != nil {
		return nil, nil, err
	}
	if rendered.Engine == "xray" {
		return rendered.Inbound, nil, nil
	}
	config, err := json.Marshal(rendered.Config)
	if err != nil || len(config) > MaxCandidateBytes {
		return nil, nil, ErrInvalid
	}
	manifest, err := json.Marshal(rendered.Manifest)
	if err != nil {
		return nil, nil, ErrInvalid
	}
	hash := sha256.New()
	_, _ = hash.Write(config)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(manifest)
	return nil, &EngineCandidate{Engine: rendered.Engine, Tag: tag, SHA256: hex.EncodeToString(hash.Sum(nil)), Config: config, Manifest: manifest}, nil
}

func authoringRelayNetwork(p Profile) string {
	if p.Spec == nil {
		return "tcp"
	}
	switch p.Spec.Network {
	case "kcp", "hysteria", "masque", "wireguard", "amneziawg", "tuic":
		return "udp"
	}
	if p.Spec.Protocol == "shadowsocks" || p.Spec.Protocol == "mixed" {
		return "tcp,udp"
	}
	if p.Spec.Protocol == "tunnel" {
		if network, ok := p.Spec.Settings["allowedNetwork"].(string); ok {
			return network
		}
	}
	return "tcp"
}

func renderAuthoringAssigned(node, installation, listen string, rows []Assignment) (Candidate, error) {
	// The legacy aggregate renderer accepts several independent profiles. New
	// profiles reuse the same checked topology compiler and merge only configs
	// addressed to this exact installation.
	var inbounds []any
	var listeners []Listener
	var engines []EngineCandidate
	ports, ids := map[uint16]bool{}, map[uint64]bool{}
	for _, row := range rows {
		if ids[row.Profile.InboundID] {
			return Candidate{}, ErrConflict
		}
		ids[row.Profile.InboundID] = true
		if row.DirectPort != 0 && row.DirectPort == row.SwapPort {
			return Candidate{}, ErrAmbiguousAttribution
		}
		topology, err := RenderCreationTopology(row.Profile, []CreationNode{{NodeID: node, InstallationID: installation, ListenIP: listen, DirectPort: row.DirectPort, TunnelPort: row.SwapPort}}, nil)
		if err != nil || len(topology.Nodes) != 1 {
			return Candidate{}, ErrInvalid
		}
		candidate := topology.Nodes[0].Candidate
		for _, listener := range candidate.Listeners {
			if ports[listener.NodePort] {
				return Candidate{}, ErrConflict
			}
			ports[listener.NodePort] = true
			listeners = append(listeners, listener)
		}
		engines = append(engines, candidate.EngineConfigs...)
		var doc struct {
			Inbounds []any `json:"inbounds"`
		}
		if json.Unmarshal(candidate.Config, &doc) != nil {
			return Candidate{}, ErrInvalid
		}
		inbounds = append(inbounds, doc.Inbounds...)
	}
	if len(listeners) == 0 {
		return Candidate{}, ErrInvalid
	}
	doc := map[string]any{"log": map[string]string{"loglevel": "none"}, "stats": map[string]any{},
		"policy":   map[string]any{"levels": map[string]any{"0": map[string]bool{"statsUserUplink": true, "statsUserDownlink": true}}, "system": map[string]bool{"statsInboundUplink": true, "statsInboundDownlink": true}},
		"inbounds": inbounds, "outbounds": []outbound{{Tag: "direct", Protocol: "freedom"}}}
	raw, err := json.Marshal(doc)
	if err != nil || len(raw) > MaxCandidateBytes {
		return Candidate{}, ErrInvalid
	}
	digest := sha256.Sum256(raw)
	manifest, _ := json.Marshal(struct {
		Node, Installation, SHA string
		Listeners               []Listener
		Engines                 []EngineCandidate
	}{node, installation, hex.EncodeToString(digest[:]), listeners, engines})
	md := sha256.Sum256(manifest)
	return Candidate{Config: raw, SHA256: hex.EncodeToString(digest[:]), ManifestSHA256: hex.EncodeToString(md[:]), Listeners: listeners, EngineConfigs: engines,
		RequiresXrayTest: len(inbounds) > 0, RequiresDataPlaneProof: true, RequiresSubscriptionCommit: true}, nil
}
