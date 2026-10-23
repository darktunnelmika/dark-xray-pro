package main

import (
	"testing"
)

func TestConsumerEndpointPolicyRequiresExplicitMatchingStartupAuthority(t *testing.T) {
	raw := `[{"node_id":"node-a","installation_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","inbound_id":42,"address":"11.0.0.2","port":14443}]`
	if p, err := consumerEndpointPolicy(false, false, false, "node-a", ""); err != nil || p != nil {
		t.Fatal("default-off policy changed", err)
	}
	if p, err := consumerEndpointPolicy(true, true, true, "node-a", raw); err != nil || len(p) != 1 || p[0].InboundID != 42 {
		t.Fatal("explicit policy refused", err)
	}
	for _, tc := range []struct {
		enabled, health, metered bool
		node, raw                string
	}{
		{false, true, true, "node-a", raw},
		{true, false, true, "node-a", raw},
		{true, true, false, "node-a", raw},
		{true, true, true, "node-a", ""},
		{true, true, true, "node-a", "[]"},
		{true, true, true, "node-b", raw},
		{true, true, true, "node-a", raw + "{}"},
	} {
		if _, err := consumerEndpointPolicy(tc.enabled, tc.health, tc.metered, tc.node, tc.raw); err == nil {
			t.Fatal("invalid startup authority accepted")
		}
	}
}
