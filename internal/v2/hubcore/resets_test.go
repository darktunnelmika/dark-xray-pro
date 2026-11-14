package hubcore

import (
	"context"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestResetsAndIPLog(t *testing.T) {
	s, now := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if _, err := s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 1, Traffic: []wire.UserTraffic{{Email: "alice", Up: 300, Down: 500}},
		Online: []wire.UserOnline{{Email: "alice", IPs: []string{"203.0.113.5", "198.51.100.9"}}}}); err != nil {
		t.Fatal(err)
	}
	u, _ := s.ClientUsage(ctx, "alice")
	if u.Up+u.Down != 800 {
		t.Fatalf("usage %+v", u)
	}
	ips, _ := s.ClientIPs(ctx, "alice")
	if len(ips) != 2 || !ips[0].Online {
		t.Fatalf("ips %+v", ips)
	}
	if err := s.ResetUsage(ctx, "alice", "manual"); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.ClientUsage(ctx, "alice"); u.Up+u.Down != 0 || u.FirstSeen == 0 {
		t.Fatalf("after reset %+v (first seen must stay)", u)
	}
	if lim, _ := s.limits(ctx, "de-1"); lim[0].Email != "alice" || lim[0].RemainingBytes != 1000 {
		t.Fatalf("node limits after reset %+v", lim)
	}
	if r, _ := s.UsageResets(ctx, "alice"); len(r) != 1 || r[0].Up != 300 || r[0].Reason != "manual" {
		t.Fatalf("reset log %+v", r)
	}
	if err := s.ResetUsage(ctx, "nobody", "manual"); err != ErrNotFound {
		t.Fatalf("unknown client: %v", err)
	}
	// Periodic.
	if err := s.SetResetPeriod(ctx, "alice", "year"); err == nil {
		t.Fatal("bad period accepted")
	}
	if err := s.SetResetPeriod(ctx, "alice", "week"); err != nil {
		t.Fatal(err)
	}
	s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 2, Traffic: []wire.UserTraffic{{Email: "alice", Up: 100}}})
	*now = now.Add(6 * 24 * time.Hour)
	if n := s.runDueResets(ctx); n != 0 {
		t.Fatal("reset before the week ended")
	}
	*now = now.Add(25 * time.Hour)
	if n := s.runDueResets(ctx); n != 1 {
		t.Fatalf("weekly reset not run: %d", n)
	}
	if u, _ := s.ClientUsage(ctx, "alice"); u.Up != 0 {
		t.Fatal("not reset")
	}
	if n := s.runDueResets(ctx); n != 0 {
		t.Fatal("reset twice")
	}
	list, _ := s.Clients(ctx)
	for _, c := range list {
		if c.Email == "alice" && (c.ResetPeriod != "week" || c.NextReset != c.LastReset+7*86400) {
			t.Fatalf("view %+v", c)
		}
	}
	// IP log pruned after 30 days.
	*now = now.Add(31 * 24 * time.Hour)
	if err := s.Prune(ctx, 400); err != nil {
		t.Fatal(err)
	}
	if ips, _ := s.ClientIPs(ctx, "alice"); len(ips) != 0 {
		t.Fatalf("old ips kept %+v", ips)
	}
}
