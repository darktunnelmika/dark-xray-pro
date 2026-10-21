package business

import (
	"context"
	"strings"
	"testing"
)

func TestMeteredRouteBatchClaimIsAtomic(t *testing.T) {
	s, _, _, _, cfg := usageSetup(t)
	ctx := context.Background()
	valid := MeteredRouteNode{NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, BinarySHA: strings.Repeat("a", 64)}
	missing := MeteredRouteNode{NodeID: "missing-target", InstallationID: strings.Repeat("b", 32), BinarySHA: strings.Repeat("b", 64)}
	if err := s.ClaimMeteredRouteChannels(ctx, []MeteredRouteNode{valid, missing}); err == nil {
		t.Fatal("invalid member accepted")
	}
	if owned, err := s.MeteredRouteChannelOwned(ctx, valid.NodeID); err != nil || owned {
		t.Fatal("failed batch retained earlier claim", err)
	}
	if err := s.ClaimMeteredRouteChannels(ctx, []MeteredRouteNode{valid, valid}); err == nil {
		t.Fatal("duplicate member accepted")
	}
	if err := s.ClaimMeteredRouteChannels(ctx, []MeteredRouteNode{valid}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimMeteredRouteChannels(ctx, []MeteredRouteNode{valid}); err != nil {
		t.Fatal("restart/retry of immutable claim", err)
	}
	changed := valid
	changed.BinarySHA = strings.Repeat("f", 64)
	if err := s.ClaimMeteredRouteChannels(ctx, []MeteredRouteNode{changed}); err == nil {
		t.Fatal("pin changed")
	}
}
