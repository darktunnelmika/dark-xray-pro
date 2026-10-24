package xrayconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func creationFixture() ([]CreationNode, []CreationSwap) {
	return []CreationNode{
			{NodeID: "de-exit", InstallationID: strings.Repeat("a", 32), ListenIP: "192.0.2.10", DirectPort: 10443, TunnelPort: 11680},
			{NodeID: "fr-direct", InstallationID: strings.Repeat("b", 32), ListenIP: "192.0.2.20", DirectPort: 10443},
		}, []CreationSwap{
			{SourceNodeID: "nl-relay", SourceInstallationID: strings.Repeat("c", 32), SourceListenIP: "192.0.2.30", SourcePort: 16801, DestinationNodeID: "de-exit", DestinationPort: 16802},
		}
}

func TestCreationTopologyKeepsDirectTunnelAndDedicatedSwap(t *testing.T) {
	nodes, swaps := creationFixture()
	p := testProfile(7)
	got, err := RenderCreationTopology(p, nodes, swaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 3 || len(got.SHA256) != 64 {
		t.Fatal("source-only relay is missing from the complete topology")
	}
	for _, node := range got.Nodes {
		c := node.Candidate
		if c.CanActivate || !c.RequiresXrayTest || !c.RequiresDataPlaneProof || !c.RequiresSubscriptionCommit {
			t.Fatal("authoring compilation must not authorize runtime delivery")
		}
		var doc struct {
			Inbounds []struct {
				Listen   string         `json:"listen"`
				Port     uint16         `json:"port"`
				Protocol string         `json:"protocol"`
				Settings map[string]any `json:"settings"`
			} `json:"inbounds"`
			Routing struct {
				Rules []map[string]any `json:"rules"`
			} `json:"routing"`
		}
		if err := json.Unmarshal(c.Config, &doc); err != nil {
			t.Fatal(err)
		}
		switch node.NodeID {
		case "de-exit":
			if len(c.Listeners) != 3 || len(doc.Inbounds) != 3 {
				t.Fatal("adding Swap removed an independent destination path")
			}
			ports := map[uint16]bool{}
			for _, in := range doc.Inbounds {
				ports[in.Port] = true
				clients, ok := in.Settings["clients"].([]any)
				if in.Protocol != "vless" || in.Listen != "192.0.2.10" || !ok || len(clients) != 2 {
					t.Fatal("customer authentication and counters must remain at the exit")
				}
			}
			if !ports[10443] || !ports[11680] || !ports[16802] {
				t.Fatal("destination's three ports were conflated")
			}
		case "fr-direct":
			if len(c.Listeners) != 1 || c.Listeners[0].Kind != "direct" || len(doc.Routing.Rules) != 0 {
				t.Fatal("Swap changed an unrelated destination Node")
			}
		case "nl-relay":
			if len(doc.Inbounds) != 1 || len(c.Listeners) != 1 || c.Listeners[0].Kind != "swap_relay" || c.Listeners[0].Attribution != "transit_not_billable" {
				t.Fatal("relay was incorrectly rendered as a customer listener")
			}
			in := doc.Inbounds[0]
			if in.Protocol != "dokodemo-door" || in.Port != 16801 || in.Settings["address"] != "192.0.2.10" || in.Settings["port"] != float64(16802) || len(doc.Routing.Rules) != 1 {
				t.Fatal("relay does not point to the destination's dedicated listener")
			}
			if _, exists := in.Settings["clients"]; exists || bytes.Contains(c.Config, []byte(uuidA)) || bytes.Contains(c.Config, []byte(p.RealityPrivateKey)) {
				t.Fatal("carrier received customer credentials or Reality private material")
			}
		}
	}
	public, err := json.Marshal(got)
	if err != nil || bytes.Contains(public, []byte(p.RealityPrivateKey)) || bytes.Contains(public, []byte(uuidA)) || bytes.Contains(public, []byte("\"config\"")) {
		t.Fatal("private candidate bytes leaked through the public topology summary")
	}
	nodes[0], nodes[1] = nodes[1], nodes[0]
	again, err := RenderCreationTopology(p, nodes, swaps)
	if err != nil || again.SHA256 != got.SHA256 {
		t.Fatal("changing form row order changed the proposed topology")
	}
}

func TestCreationTopologyTunnelReuseDoesNotDuplicateAuthentication(t *testing.T) {
	nodes, swaps := creationFixture()
	swaps[0].DestinationPort = nodes[0].TunnelPort
	got, err := RenderCreationTopology(testProfile(7), nodes, swaps)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range got.Nodes {
		if n.NodeID == "de-exit" {
			if len(n.Candidate.Listeners) != 2 {
				t.Fatal("the shared destination Tunnel was duplicated")
			}
			for _, listener := range n.Candidate.Listeners {
				if listener.Kind == "swap" || listener.Kind == "shared" {
					t.Fatal("reusing an existing Tunnel cannot invent a separate Swap traffic counter")
				}
				if listener.Kind == "tunnel" && listener.Attribution != "shared_tunnel_swap" {
					t.Fatal("a shared destination cannot claim separate path attribution")
				}
			}
		}
	}
}

func TestCreationTopologyRefusesCollisionsIdentityMismatchAndCycles(t *testing.T) {
	tests := []struct {
		name string
		edit func(*[]CreationNode, *[]CreationSwap)
	}{
		{"direct-tunnel-collision", func(n *[]CreationNode, _ *[]CreationSwap) { (*n)[0].TunnelPort = (*n)[0].DirectPort }},
		{"direct-swap-collision", func(n *[]CreationNode, s *[]CreationSwap) { (*s)[0].DestinationPort = (*n)[0].DirectPort }},
		{"relay-ordinary-tunnel-collision", func(n *[]CreationNode, s *[]CreationSwap) {
			*n = append(*n, CreationNode{NodeID: "nl-relay", InstallationID: strings.Repeat("c", 32), ListenIP: "192.0.2.30", TunnelPort: (*s)[0].SourcePort})
		}},
		{"relay-bind-mismatch", func(n *[]CreationNode, _ *[]CreationSwap) {
			*n = append(*n, CreationNode{NodeID: "nl-relay", InstallationID: strings.Repeat("c", 32), ListenIP: "192.0.2.99", DirectPort: 18888})
		}},
		{"relay-installation-mismatch", func(n *[]CreationNode, _ *[]CreationSwap) {
			*n = append(*n, CreationNode{NodeID: "nl-relay", InstallationID: strings.Repeat("d", 32), ListenIP: "192.0.2.30", DirectPort: 18888})
		}},
		{"reserved-relay-port", func(_ *[]CreationNode, s *[]CreationSwap) { (*s)[0].SourcePort = 24443 }},
		{"reserved-destination-port", func(_ *[]CreationNode, s *[]CreationSwap) { (*s)[0].DestinationPort = 8443 }},
		{"self-relay", func(_ *[]CreationNode, s *[]CreationSwap) { (*s)[0].SourceNodeID = "de-exit" }},
		{"unknown-destination", func(_ *[]CreationNode, s *[]CreationSwap) { (*s)[0].DestinationNodeID = "missing" }},
		{"empty-node", func(n *[]CreationNode, _ *[]CreationSwap) { (*n)[1].DirectPort = 0 }},
		{"cycle", func(n *[]CreationNode, s *[]CreationSwap) {
			*n = append(*n, CreationNode{NodeID: "nl-relay", InstallationID: strings.Repeat("c", 32), ListenIP: "192.0.2.30", DirectPort: 18888})
			*s = append(*s, CreationSwap{SourceNodeID: "de-exit", SourceInstallationID: strings.Repeat("a", 32), SourceListenIP: "192.0.2.10", SourcePort: 16803, DestinationNodeID: "nl-relay", DestinationPort: 16804})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			n, s := creationFixture()
			test.edit(&n, &s)
			if _, err := RenderCreationTopology(testProfile(7), n, s); err == nil {
				t.Fatal("invalid topology was accepted")
			}
		})
	}
}

func TestRealPinnedCreationTopologyOffline(t *testing.T) {
	bin, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if bin == "" && pin == "" {
		t.Skip("no separately pinned Xray binary configured")
	}
	if bin == "" || pin == "" || os.Getenv("DARK_PRO_LAB_MODE") != "1" {
		t.Fatal("explicit LAB mode, absolute pinned binary and SHA256 are required")
	}
	for _, advanced := range []bool{false, true} {
		for _, transport := range []string{"tcp", "grpc"} {
			for _, reuse := range []bool{false, true} {
				p := testProfile(7)
				p.Transport = transport
				if transport == "grpc" {
					p.GRPCService = "dark-creation"
				}
				if advanced {
					p.Options = fullProfileOptions(transport)
				}
				nodes, swaps := creationFixture()
				if reuse {
					swaps[0].DestinationPort = nodes[0].TunnelPort
				}
				topology, err := RenderCreationTopology(p, nodes, swaps)
				if err != nil {
					t.Fatal(err)
				}
				for _, node := range topology.Nodes {
					result, err := ValidateOffline(context.Background(), node.Candidate, bin, pin)
					if err != nil || !result.XraySyntaxValidated || result.CanActivate {
						t.Fatalf("pinned core rejected %s on %s (Tunnel reuse %t, advanced %t): %v", transport, node.NodeID, reuse, advanced, err)
					}
				}
			}
		}
	}
}
