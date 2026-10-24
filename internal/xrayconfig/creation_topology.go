package xrayconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
)

// CreationNode describes a customer listener destination in an unsent setup
// plan. An ordinary Tunnel has its own listener and is never a relay source.
type CreationNode struct {
	NodeID         string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	ListenIP       string `json:"listen_ip"`
	DirectPort     uint16 `json:"direct_port"`
	TunnelPort     uint16 `json:"tunnel_port"`
}

// CreationSwap is a dedicated TCP carrier. The external entry address/port is
// deliberately absent: that manual forwarding mapping is not provisioned by
// an Xray listener, and must not be inferred from the source bind address.
type CreationSwap struct {
	Name                 string `json:"name"`
	SourceNodeID         string `json:"source_node_id"`
	SourceInstallationID string `json:"source_installation_id"`
	SourceListenIP       string `json:"source_listen_ip"`
	SourcePort           uint16 `json:"source_port"`
	DestinationNodeID    string `json:"destination_node_id"`
	DestinationPort      uint16 `json:"destination_port"`
}

type CreationNodeCandidate struct {
	NodeID         string    `json:"node_id"`
	InstallationID string    `json:"installation_id"`
	ListenIP       string    `json:"listen_ip"`
	Candidate      Candidate `json:"candidate"`
}

// CreationTopology contains PRIVATE, partial Node configurations for offline
// validation. It is not a runtime manifest: existing Inbounds on each Node are
// intentionally not replaced or merged here. No delivery path consumes it.
// Candidate.Config remains excluded from JSON/browser responses.
type CreationTopology struct {
	Nodes  []CreationNodeCandidate `json:"nodes"`
	SHA256 string                  `json:"sha256"`
}

type creationNodeBuild struct {
	node      CreationNode
	inbounds  []any
	listeners []Listener
	ports     map[uint16]bool
	outbounds []outbound
	rules     []map[string]any
}

func creationIdentity(node, installation, listen string) bool {
	if node == "" || len(node) > 128 || strings.TrimSpace(node) != node || strings.ContainsAny(node, "\x00\r\n") || len(installation) != 32 {
		return false
	}
	if _, err := hex.DecodeString(installation); err != nil {
		return false
	}
	ip := net.ParseIP(listen)
	return ip != nil && !ip.IsUnspecified() && !ip.IsMulticast()
}

func creationPort(port uint16) bool {
	return port >= 1024 && port != 2087 && port != 8443 && port != 24443
}

func creationCustomerInbound(p Profile, listen string, port uint16, kind string) inbound {
	x := inbound{
		Listen: listen, Port: port, Tag: fmt.Sprintf("dxp-in-%d-%s", p.InboundID, kind), Protocol: "vless",
		Stream: profileStream(p),
	}
	x.Settings.Decryption = "none"
	x.Settings.Clients = make([]xrayUser, 0, len(p.Users))
	users := append([]User(nil), p.Users...)
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	for _, user := range users {
		client := xrayUser{ID: user.UUID, Email: user.Email, Level: 0}
		if p.Transport == "tcp" {
			client.Flow = "xtls-rprx-vision"
		}
		x.Settings.Clients = append(x.Settings.Clients, client)
	}
	applyInboundOptions(&x, p.Options)
	return x
}

// RenderCreationTopology validates and compiles all three independent paths.
// Authentication and user counters exist only at customer destinations. The
// source's dokodemo-door forwards raw TCP to the chosen destination listener;
// it contains no copied customer credentials and never becomes the exit.
// A destination may deliberately reuse its own ordinary Tunnel listener.
func RenderCreationTopology(profile Profile, nodes []CreationNode, swaps []CreationSwap) (CreationTopology, error) {
	var empty CreationTopology
	if validProfile(profile) != nil || len(nodes) == 0 || len(nodes) > 256 || len(swaps) > 256 {
		return empty, ErrInvalid
	}
	byNode := make(map[string]*creationNodeBuild, len(nodes)+len(swaps))
	destinations := make(map[string]bool, len(nodes))
	newNode := func(n CreationNode) *creationNodeBuild {
		n.ListenIP = net.ParseIP(n.ListenIP).String()
		b := &creationNodeBuild{node: n, inbounds: []any{}, listeners: []Listener{}, ports: map[uint16]bool{}, outbounds: []outbound{{Tag: "direct", Protocol: "freedom"}}}
		byNode[n.NodeID] = b
		return b
	}
	addCustomer := func(b *creationNodeBuild, port uint16, kind string) error {
		if !creationPort(port) || b.ports[port] {
			return ErrConflict
		}
		b.ports[port] = true
		x := creationCustomerInbound(profile, b.node.ListenIP, port, kind)
		b.inbounds = append(b.inbounds, x)
		b.listeners = append(b.listeners, Listener{InboundID: profile.InboundID, NodePort: port, Kind: kind, Tag: x.Tag, Attribution: "distinct"})
		return nil
	}
	for _, node := range nodes {
		if !creationIdentity(node.NodeID, node.InstallationID, node.ListenIP) || byNode[node.NodeID] != nil {
			return empty, ErrInvalid
		}
		b := newNode(node)
		destinations[node.NodeID] = true
		if node.DirectPort != 0 {
			if err := addCustomer(b, node.DirectPort, "direct"); err != nil {
				return empty, err
			}
		}
		if node.TunnelPort != 0 {
			if err := addCustomer(b, node.TunnelPort, "tunnel"); err != nil {
				return empty, err
			}
		}
	}
	orderedSwaps := append([]CreationSwap(nil), swaps...)
	sort.Slice(orderedSwaps, func(i, j int) bool {
		if orderedSwaps[i].SourceNodeID == orderedSwaps[j].SourceNodeID {
			return orderedSwaps[i].DestinationNodeID < orderedSwaps[j].DestinationNodeID
		}
		return orderedSwaps[i].SourceNodeID < orderedSwaps[j].SourceNodeID
	})
	swapDestinations := make(map[string]bool, len(swaps))
	edges := make(map[string][]string)
	for _, swap := range orderedSwaps {
		destination := byNode[swap.DestinationNodeID]
		if destination == nil || !destinations[swap.DestinationNodeID] || swapDestinations[swap.DestinationNodeID] || swap.SourceNodeID == swap.DestinationNodeID ||
			!creationIdentity(swap.SourceNodeID, swap.SourceInstallationID, swap.SourceListenIP) || !creationPort(swap.SourcePort) || !creationPort(swap.DestinationPort) ||
			swap.DestinationPort == destination.node.DirectPort {
			return empty, ErrInvalid
		}
		swapDestinations[swap.DestinationNodeID] = true
		edges[swap.SourceNodeID] = append(edges[swap.SourceNodeID], swap.DestinationNodeID)
		source := byNode[swap.SourceNodeID]
		if source == nil {
			source = newNode(CreationNode{NodeID: swap.SourceNodeID, InstallationID: swap.SourceInstallationID, ListenIP: swap.SourceListenIP})
		} else if source.node.InstallationID != swap.SourceInstallationID || source.node.ListenIP != net.ParseIP(swap.SourceListenIP).String() {
			return empty, ErrConflict
		}
		if source.ports[swap.SourcePort] {
			return empty, ErrConflict
		}
		source.ports[swap.SourcePort] = true
		if swap.DestinationPort != destination.node.TunnelPort {
			if err := addCustomer(destination, swap.DestinationPort, "swap"); err != nil {
				return empty, err
			}
		} else {
			// Both connection paths terminate on one listener. User traffic is
			// counted once here, but its ordinary-Tunnel/Swap origin is unknown.
			for i := range destination.listeners {
				if destination.listeners[i].NodePort == swap.DestinationPort {
					destination.listeners[i].Attribution = "shared_tunnel_swap"
				}
			}
		}
		digest := sha256.Sum256([]byte(swap.SourceNodeID + "\x00" + swap.DestinationNodeID))
		tag := fmt.Sprintf("dxp-in-%d-swap-relay-%x", profile.InboundID, digest[:6])
		outTag := tag + "-out"
		source.inbounds = append(source.inbounds, map[string]any{
			"listen": source.node.ListenIP, "port": swap.SourcePort, "tag": tag, "protocol": "dokodemo-door",
			"settings":       map[string]any{"address": destination.node.ListenIP, "port": swap.DestinationPort, "network": "tcp", "followRedirect": false},
			"streamSettings": map[string]string{"network": "tcp", "security": "none"},
		})
		source.listeners = append(source.listeners, Listener{InboundID: profile.InboundID, NodePort: swap.SourcePort, Kind: "swap_relay", Tag: tag, Attribution: "transit_not_billable"})
		source.outbounds = append(source.outbounds, outbound{Tag: outTag, Protocol: "freedom"})
		source.rules = append(source.rules, map[string]any{"type": "field", "inboundTag": []string{tag}, "outboundTag": outTag})
	}
	// Reject cycles in the complete proposed graph, independently of row order.
	seen := map[string]uint8{}
	var visit func(string) bool
	visit = func(node string) bool {
		if seen[node] == 1 {
			return false
		}
		if seen[node] == 2 {
			return true
		}
		seen[node] = 1
		for _, next := range edges[node] {
			if !visit(next) {
				return false
			}
		}
		seen[node] = 2
		return true
	}
	for node := range byNode {
		if !visit(node) {
			return empty, ErrConflict
		}
	}
	nodeIDs := make([]string, 0, len(byNode))
	for id := range byNode {
		nodeIDs = append(nodeIDs, id)
	}
	if len(nodeIDs) > 256 {
		return empty, ErrInvalid
	}
	sort.Strings(nodeIDs)
	result := CreationTopology{Nodes: make([]CreationNodeCandidate, 0, len(nodeIDs))}
	for _, id := range nodeIDs {
		b := byNode[id]
		if len(b.inbounds) == 0 {
			return empty, ErrInvalid
		}
		doc := map[string]any{
			"log": map[string]string{"loglevel": "none"}, "stats": map[string]any{},
			"policy": map[string]any{"levels": map[string]any{"0": map[string]bool{"statsUserUplink": true, "statsUserDownlink": true}},
				"system": map[string]bool{"statsInboundUplink": true, "statsInboundDownlink": true}},
			"inbounds": b.inbounds, "outbounds": b.outbounds,
		}
		if len(b.rules) > 0 {
			doc["routing"] = map[string]any{"domainStrategy": "AsIs", "rules": b.rules}
		}
		config, err := json.Marshal(doc)
		if err != nil || len(config) > MaxCandidateBytes {
			return empty, ErrInvalid
		}
		digest := sha256.Sum256(config)
		sort.Slice(b.listeners, func(i, j int) bool { return b.listeners[i].Tag < b.listeners[j].Tag })
		manifest, err := json.Marshal(struct {
			Purpose      string     `json:"purpose"`
			Node         string     `json:"node"`
			Installation string     `json:"installation"`
			ConfigSHA    string     `json:"config_sha"`
			Listeners    []Listener `json:"listeners"`
		}{"creation-topology-offline-v1", id, b.node.InstallationID, hex.EncodeToString(digest[:]), b.listeners})
		if err != nil {
			return empty, ErrInvalid
		}
		manifestDigest := sha256.Sum256(manifest)
		result.Nodes = append(result.Nodes, CreationNodeCandidate{NodeID: id, InstallationID: b.node.InstallationID, ListenIP: b.node.ListenIP,
			Candidate: Candidate{Config: config, SHA256: hex.EncodeToString(digest[:]), ManifestSHA256: hex.EncodeToString(manifestDigest[:]),
				Listeners: b.listeners, RequiresXrayTest: true, RequiresDataPlaneProof: true, RequiresSubscriptionCommit: true}})
	}
	manifest, err := json.Marshal(result.Nodes)
	if err != nil {
		return empty, ErrInvalid
	}
	digest := sha256.Sum256(manifest)
	result.SHA256 = hex.EncodeToString(digest[:])
	return result, nil
}
