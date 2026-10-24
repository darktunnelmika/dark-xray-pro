package xrayconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
)

// Assignment binds a native Inbound to an explicit NEW physical installation.
type Assignment struct {
	Profile    Profile `json:"profile"`
	DirectPort uint16  `json:"direct_port"`
	SwapPort   uint16  `json:"swap_port"`
}

// RenderAssigned retains empty client lists so removing the last credential
// does not leave a previously deployed user active. It never opens a socket.
func RenderAssigned(nodeID, installationID, listenIP string, assignments []Assignment) (Candidate, error) {
	ip := net.ParseIP(listenIP)
	if nodeID == "" || len(nodeID) > 128 || len(installationID) != 32 || ip == nil || ip.IsUnspecified() || ip.IsMulticast() || len(assignments) == 0 || len(assignments) > 256 {
		return Candidate{}, ErrInvalid
	}
	if _, err := hex.DecodeString(installationID); err != nil {
		return Candidate{}, ErrInvalid
	}
	rows := append([]Assignment(nil), assignments...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Profile.InboundID < rows[j].Profile.InboundID })
	seenID := map[uint64]bool{}
	seenPort := map[uint16]bool{}
	inbounds := make([]inbound, 0, len(rows)*2)
	listeners := make([]Listener, 0, len(rows)*2)
	for _, row := range rows {
		p := row.Profile
		if err := validProfile(p); err != nil {
			return Candidate{}, ErrInvalid
		}
		if seenID[p.InboundID] || (row.DirectPort == 0 && row.SwapPort == 0) {
			return Candidate{}, ErrConflict
		}
		seenID[p.InboundID] = true
		add := func(port uint16, kind string) error {
			if port < 1024 || seenPort[port] {
				return ErrConflict
			}
			seenPort[port] = true
			tag := fmt.Sprintf("dxp-in-%d-%s", p.InboundID, kind)
			users := append([]User(nil), p.Users...)
			sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
			x := inbound{Listen: ip.String(), Port: port, Tag: tag, Protocol: "vless", Stream: profileStream(p)}
			x.Settings.Decryption = "none"
			x.Settings.Clients = make([]xrayUser, 0, len(users))
			for _, u := range users {
				c := xrayUser{ID: u.UUID, Email: u.Email, Level: 0}
				if p.Transport == "tcp" {
					c.Flow = "xtls-rprx-vision"
				}
				x.Settings.Clients = append(x.Settings.Clients, c)
			}
			scope := "distinct"
			if kind == "shared" {
				scope = "ambiguous_shared_port"
			}
			listeners = append(listeners, Listener{InboundID: p.InboundID, NodePort: port, Kind: kind, Tag: tag, Attribution: scope})
			inbounds = append(inbounds, x)
			return nil
		}
		if row.DirectPort != 0 && row.DirectPort == row.SwapPort {
			if err := add(row.DirectPort, "shared"); err != nil {
				return Candidate{}, err
			}
		} else {
			if row.DirectPort != 0 {
				if err := add(row.DirectPort, "direct"); err != nil {
					return Candidate{}, err
				}
			}
			if row.SwapPort != 0 {
				if err := add(row.SwapPort, "swap"); err != nil {
					return Candidate{}, err
				}
			}
		}
	}
	doc := struct {
		Log       map[string]string `json:"log"`
		Stats     map[string]any    `json:"stats"`
		Policy    map[string]any    `json:"policy"`
		Inbounds  []inbound         `json:"inbounds"`
		Outbounds []outbound        `json:"outbounds"`
	}{
		Log: map[string]string{"loglevel": "none"}, Stats: map[string]any{}, Policy: map[string]any{"levels": map[string]any{"0": map[string]bool{"statsUserUplink": true, "statsUserDownlink": true}}, "system": map[string]bool{"statsInboundUplink": true, "statsInboundDownlink": true}}, Inbounds: inbounds, Outbounds: []outbound{{Tag: "direct", Protocol: "freedom"}},
	}
	raw, err := json.Marshal(doc)
	if err != nil || len(raw) > MaxCandidateBytes {
		return Candidate{}, ErrInvalid
	}
	sha := sha256.Sum256(raw)
	manifest, _ := json.Marshal(struct {
		Node         string     `json:"node"`
		Installation string     `json:"installation"`
		Listen       string     `json:"listen"`
		SHA          string     `json:"config_sha"`
		Listeners    []Listener `json:"listeners"`
	}{nodeID, installationID, ip.String(), hex.EncodeToString(sha[:]), listeners})
	m := sha256.Sum256(manifest)
	return Candidate{Config: raw, SHA256: hex.EncodeToString(sha[:]), ManifestSHA256: hex.EncodeToString(m[:]), Listeners: listeners, RequiresXrayTest: true, RequiresDataPlaneProof: true, RequiresSubscriptionCommit: true}, nil
}
