package hubcore

import (
	"context"
	"sort"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// Subscriptions list the healthiest node first, so the config a customer
// taps first is the best one. A down node's configs go to the end, or are
// left out when the owner asks for it (never all of them).

const downScore = 1000

// nodeScores rates every node: lower is better.
func (s *Store) nodeScores(ctx context.Context) map[string]int {
	out := map[string]int{}
	nodes, err := s.Nodes(ctx)
	if err != nil {
		return out
	}
	now := s.now()
	for _, n := range nodes {
		switch {
		case !n.Online(now):
			out[n.ID] = downScore + 100
		case !n.XrayRunning:
			out[n.ID] = downScore
		case n.Host != nil:
			sc := n.Host.CPUPercent
			if n.Host.RAMTotal > 0 {
				sc += 25 * float64(n.Host.RAMUsed) / float64(n.Host.RAMTotal)
			}
			sc += float64(n.Host.OnlineUsers) / 5
			out[n.ID] = int(sc)
		default:
			out[n.ID] = 50
		}
	}
	return out
}

// rankInbounds sorts each inbound's addresses by node health, ranks the
// inbounds by their best address, and drops down nodes' addresses if asked.
func (s *Store) rankInbounds(ctx context.Context, ins []sub.Inbound) []sub.Inbound {
	if s.Setting(ctx, "sub_smart") == "0" || len(ins) == 0 {
		return ins
	}
	scores := s.nodeScores(ctx)
	score := func(e sub.Endpoint) int {
		if v, ok := scores[e.Node]; ok {
			return v
		}
		return 50 // an address without a known node: middle
	}
	hide := s.Setting(ctx, "sub_hide_down") == "1"
	alive := 0
	for _, in := range ins {
		for _, e := range in.Meta.Endpoints {
			if score(e) < downScore {
				alive++
			}
		}
	}
	for i := range ins {
		eps := append([]sub.Endpoint(nil), ins[i].Meta.Endpoints...)
		sort.SliceStable(eps, func(a, b int) bool { return score(eps[a]) < score(eps[b]) })
		if hide && alive > 0 {
			kept := eps[:0]
			for _, e := range eps {
				if score(e) < downScore {
					kept = append(kept, e)
				}
			}
			eps = kept
		}
		ins[i].Meta.Endpoints = eps
		ins[i].Rank = downScore * 2
		if len(eps) > 0 {
			ins[i].Rank = score(eps[0])
		}
	}
	if hide && alive > 0 {
		kept := ins[:0]
		for _, in := range ins {
			if len(in.Meta.Endpoints) > 0 {
				kept = append(kept, in)
			}
		}
		ins = kept
	}
	return ins
}
