package hubcore

import (
	"context"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestIPOverCountsEveryNode(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	n := now.Unix()
	ins := func(email, node, ips string, at int64) {
		if _, err := s.db.Exec(`INSERT OR REPLACE INTO online(email,node_id,ips,seen_unix) VALUES(?,?,?,?)`, email, node, ips, at); err != nil {
			t.Fatal(err)
		}
	}
	ins("a", "de-1", `["1.1.1.1"]`, n)
	ins("a", "fi-1", `["2.2.2.2"]`, n)              // 2 in total on two nodes
	ins("b", "de-1", `["3.3.3.3","10.77.10.1"]`, n) // the tunnel relay never counts
	ins("c", "de-1", `["4.4.4.4"]`, n)
	ins("c", "fi-1", `["5.5.5.5"]`, n-120) // a stale report does not count
	ins("d", "de-1", `["6.6.6.6"]`, n)
	ins("d", "fi-1", `["6.6.6.6"]`, n)                            // the same address on two nodes is one
	ins("f", "de-1", `["2a01:5ec0:1::1","2a01:5ec0:1::abcd"]`, n) // one phone rotating IPv6
	limits := []wire.UserLimit{{Email: "a", Enabled: true, MaxIPs: 1}, {Email: "b", Enabled: true, MaxIPs: 1},
		{Email: "c", Enabled: true, MaxIPs: 1}, {Email: "d", Enabled: true, MaxIPs: 1}, {Email: "f", Enabled: true, MaxIPs: 1}, {Email: "e", Enabled: true, MaxIPs: 0}}
	got := s.ipOver(ctx, n, limits, []string{"10.0.0.0/8"})
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("only a is over across nodes: %v", got)
	}
}
