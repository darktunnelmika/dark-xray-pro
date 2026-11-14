package hubcore

import (
	"context"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestOutboundTraffic(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	for seq, out := range [][]wire.UserTraffic{
		{{Email: "direct", Up: 10, Down: 90}, {Email: "warp", Up: 5, Down: 45}, {Email: "api", Up: 1}},
		{{Email: "warp", Up: 0, Down: 50}},
	} {
		if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: uint64(seq + 1), Outbounds: out}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.OutboundTraffic(ctx, 7)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	for _, o := range list {
		want := map[string]int64{"direct": 100, "warp": 100}[o.Tag]
		if o.Total != want || o.Today != want || o.ByNode["de-1"] != want {
			t.Fatalf("%+v", o)
		}
	}
}
