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
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
