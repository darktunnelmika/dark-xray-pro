package inboundexec

import (
	"strings"
	"testing"
)

func TestEpochClearanceAuthenticatesExactOwnerLossScope(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	c, err := SealClearance(key, EpochClearance{Version: 1, EventID: "owner-loss-event", EpochID: "closed-epoch", NodeID: "node-a", InstallationID: strings.Repeat("a", 32), Generation: 17, EpochSHA: strings.Repeat("b", 64), KnownSequence: 5, KnownCountersSHA: strings.Repeat("c", 64), Policy: "owner_loss", CreatedUnixNano: 42})
	if err != nil || VerifyClearance(key, c) != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*EpochClearance){
		func(c *EpochClearance) { c.EventID += "-other" },
		func(c *EpochClearance) { c.EpochID += "-other" },
		func(c *EpochClearance) { c.NodeID += "-other" },
		func(c *EpochClearance) { c.InstallationID = strings.Repeat("d", 32) },
		func(c *EpochClearance) { c.Generation++ },
		func(c *EpochClearance) { c.EpochSHA = strings.Repeat("d", 64) },
		func(c *EpochClearance) { c.KnownSequence++ },
		func(c *EpochClearance) { c.KnownCountersSHA = strings.Repeat("d", 64) },
		func(c *EpochClearance) { c.Policy = "assume_zero" },
		func(c *EpochClearance) { c.CreatedUnixNano++ },
	} {
		bad := c
		edit(&bad)
		if VerifyClearance(key, bad) == nil {
			t.Fatal("owner-loss authority accepted a changed scope or evidence")
		}
	}
	if VerifyClearance([]byte(strings.Repeat("z", 32)), c) == nil {
		t.Fatal("clearance crossed a paired installation key")
	}
}
