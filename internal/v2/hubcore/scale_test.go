package hubcore

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// TestScale times the hot paths with many clients. It runs only when
// DARK_SCALE is set (e.g. DARK_SCALE=5000) and prints the timings.
func TestScale(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("DARK_SCALE"))
	if n == 0 {
		t.Skip("set DARK_SCALE=<clients> to run")
	}
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	timed := func(name string, f func() error) time.Duration {
		t0 := time.Now()
		if err := f(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		d := time.Since(t0)
		t.Logf("%-28s %8.1f ms", name, float64(d.Microseconds())/1000)
		return d
	}
	timed(fmt.Sprintf("create %d clients", n), func() error {
		for start := 1; start <= n; start += 1000 {
			if _, err := s.BulkCreateClients(ctx, BulkCreate{Prefix: "u", Start: start, Count: min(1000, n-start+1), Pad: 5, Volume: 50 << 30, Days: 30, Inbounds: []string{"de-vless"}}); err != nil {
				return err
			}
		}
		return nil
	})
	var list []ClientView
	timed("list clients", func() (err error) { list, err = s.Clients(ctx); return })
	if len(list) < n {
		t.Fatalf("only %d clients", len(list))
	}
	timed("overview", func() error { _, err := s.Overview(ctx); return err })
	timed("render node config", func() error { _, err := s.RenderNode(ctx, "de-1"); return err })
	timed("one subscription", func() error { _, _, err := s.Subscription(ctx, "u00042"); return err })
	b := wire.UsageBatch{NodeID: "de-1", Seq: 1, FromUnix: 1, ToUnix: 2}
	for i := 1; i <= n; i++ {
		b.Traffic = append(b.Traffic, wire.UserTraffic{Email: fmt.Sprintf("u%05d", i), Up: 1000, Down: 50000})
		if i%3 == 0 {
			b.Online = append(b.Online, wire.UserOnline{Email: fmt.Sprintf("u%05d", i), IPs: []string{"5.1.2.3"}})
		}
	}
	timed("usage batch (all clients)", func() error { _, err := s.ApplyUsage(ctx, "de-1", b); return err })
	b.Seq = 2
	timed("usage batch again", func() error { _, err := s.ApplyUsage(ctx, "de-1", b); return err })
	timed("list clients after usage", func() (err error) { list, err = s.Clients(ctx); return })
	timed("CSV export", func() error { var w discard; return s.WriteClientsCSV(ctx, &w, "https://x") })
	timed("node sync (config+limits)", func() error {
		_, err := s.Sync(ctx, "de-1", wire.SyncRequest{NodeID: "de-1"})
		return err
	})
	scaleTenant(t, s, n, timed)
}

// scaleTenant times the connected shop paths with n shop clients next to
// the owner's: bulk create, concurrent usage batches from several nodes
// while single API calls run, the list and the change feed.
func scaleTenant(t *testing.T, s *Store, n int, timed func(string, func() error) time.Duration) {
	ctx := context.Background()
	if err := s.PutTenant(ctx, Tenant{ID: "shop", Name: "Shop", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	var worst time.Duration
	timed(fmt.Sprintf("tenant: bulk create %d (500 a call)", n), func() error {
		for start := 0; start < n; start += 500 {
			items := make([]TenantClientIn, 0, 500)
			for i := start; i < min(start+500, n); i++ {
				items = append(items, TenantClientIn{Name: fmt.Sprintf("c%06d", i), VolumeBytes: i64(50 << 30), Days: i64(30), ExternalRef: strp(fmt.Sprintf("o-%d", i))})
			}
			t0 := time.Now()
			res, ok, err := s.TenantBulkCreate(ctx, "shop", items, false)
			if err != nil || !ok {
				return fmt.Errorf("bulk: %v %v", err, ok)
			}
			for _, r := range res {
				if !r.OK {
					return fmt.Errorf("item %s: %s", r.Name, r.Error)
				}
			}
			worst = max(worst, time.Since(t0))
		}
		return nil
	})
	t.Logf("%-28s %8.1f ms", "  slowest bulk call", float64(worst.Microseconds())/1000)
	nodes := []string{"de-1"}
	for i := 2; i <= 4; i++ {
		id := fmt.Sprintf("n-%d", i)
		if _, err := s.CreateNode(ctx, id, id); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, id)
	}
	batch := func(node string, seq uint64) wire.UsageBatch {
		b := wire.UsageBatch{NodeID: node, Seq: seq, FromUnix: 1, ToUnix: 2}
		for i := 0; i < n; i++ {
			b.Traffic = append(b.Traffic, wire.UserTraffic{Email: tenantEmail("shop", fmt.Sprintf("c%06d", i)), Up: 100, Down: 1000})
			if i%3 == 0 {
				b.Online = append(b.Online, wire.UserOnline{Email: tenantEmail("shop", fmt.Sprintf("c%06d", i)), IPs: []string{"5.1.2.3"}})
			}
		}
		return b
	}
	// The single calls a shop makes, timed alone and while the nodes report.
	single := func(label string, rounds int) {
		var lat []time.Duration
		for i := 0; i < rounds; i++ {
			name := fmt.Sprintf("c%06d", (i*7919)%n)
			t0 := time.Now()
			if _, err := s.TenantClient(ctx, "shop", name); err != nil {
				t.Fatal(err)
			}
			lat = append(lat, time.Since(t0))
			t0 = time.Now()
			if _, err := s.TenantUpdate(ctx, "shop", name, TenantClientIn{MaxIPs: new(int)}); err != nil {
				t.Fatal(err)
			}
			lat = append(lat, time.Since(t0))
			t0 = time.Now()
			if _, err := s.TenantRenew(ctx, "shop", name, TenantRenewIn{AddDays: 1}); err != nil {
				t.Fatal(err)
			}
			lat = append(lat, time.Since(t0))
		}
		sortDur(lat)
		t.Logf("%-28s p50 %6.1f ms  p99 %6.1f ms  max %6.1f ms (%d calls)", label,
			ms(lat[len(lat)/2]), ms(lat[len(lat)*99/100]), ms(lat[len(lat)-1]), len(lat))
	}
	single("tenant: single calls, idle", 100)
	batches := make([]wire.UsageBatch, len(nodes))
	for i, id := range nodes {
		batches[i] = batch(id, 1)
	}
	done := make(chan time.Duration, len(nodes))
	t0 := time.Now()
	for i, id := range nodes {
		go func(id string, b wire.UsageBatch) {
			t1 := time.Now()
			if _, err := s.ApplyUsage(ctx, id, b); err != nil {
				t.Error(err)
			}
			done <- time.Since(t1)
		}(id, batches[i])
	}
	single("tenant: single calls, busy", 30)
	var slowest time.Duration
	for range nodes {
		slowest = max(slowest, <-done)
	}
	t.Logf("%-28s %8.1f ms (slowest batch %.1f ms, %d nodes x %d clients)", "usage batches, concurrent", ms(time.Since(t0)), ms(slowest), len(nodes), n)
	timed("tenant: list all (500 a page)", func() error {
		after, total := "", 0
		for {
			page, err := s.TenantClientPage(ctx, "shop", after, 500)
			if err != nil {
				return err
			}
			total += len(page)
			if len(page) < 500 {
				break
			}
			after = page[len(page)-1].Name
		}
		if total != n {
			return fmt.Errorf("listed %d of %d", total, n)
		}
		return nil
	})
	var cursor string
	timed("tenant: feed, everything", func() error {
		got := 0
		for {
			list, next, more, err := s.TenantChanges(ctx, "shop", cursor, 1000)
			if err != nil {
				return err
			}
			got, cursor = got+len(list), next
			if !more {
				break
			}
		}
		if got != n {
			return fmt.Errorf("feed had %d of %d", got, n)
		}
		return nil
	})
	b := batch("de-1", 2)
	b.Traffic = b.Traffic[:n/10]
	if _, err := s.ApplyUsage(ctx, "de-1", b); err != nil {
		t.Fatal(err)
	}
	timed("tenant: feed after 10% used", func() error {
		got := 0
		for {
			list, next, more, err := s.TenantChanges(ctx, "shop", cursor, 1000)
			if err != nil {
				return err
			}
			got, cursor = got+len(list), next
			if !more {
				break
			}
		}
		if got != n/10 {
			return fmt.Errorf("feed had %d, want %d", got, n/10)
		}
		return nil
	})
	timed("tenant: one change, empty feed", func() error {
		list, _, _, err := s.TenantChanges(ctx, "shop", cursor, 1000)
		if err == nil && len(list) != 0 {
			err = fmt.Errorf("%d changes", len(list))
		}
		return err
	})
	timed("owner: list clients", func() error { _, err := s.Clients(ctx); return err })
	timed("tenant: summary", func() error { _, err := s.Tenants(ctx); return err })
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func sortDur(v []time.Duration) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
