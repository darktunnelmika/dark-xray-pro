package hubcore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSalesReport(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Unix(1794800000, 0)
	s.now = func() time.Time { return now }
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "ali", 500<<30, 0, "")
	mk := func(email string, gb int64) {
		if err := s.RepPutClient(ctx, "ali", "ali", Client{Email: email, Enabled: true, VolumeBytes: gb << 30, ExpiresUnix: now.Unix() + 30*86400, Inbounds: []string{"in1"}}, "volume"); err != nil {
			t.Fatal(err)
		}
	}
	// One sale in the previous week, two this week, then a renewal.
	now = now.Add(-10 * 24 * time.Hour)
	mk("old-1", 10)
	now = now.Add(10 * 24 * time.Hour)
	mk("a-1", 20)
	mk("a-2", 30)
	if _, err := s.RenewClient(ctx, Principal{User: "ali", Role: "rep", RepID: "ali"}, "a-1", "volume", 50, 30); err != nil {
		t.Fatal(err)
	}
	r, err := s.SalesReport(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	var ali SalesRep
	for _, x := range r.Reps {
		if x.RepID == "ali" {
			ali = x
		}
	}
	if ali.New != 2 || ali.Renew != 1 || ali.SoldBytes != 100<<30 || ali.PrevCount != 1 || ali.PrevNet != 10<<30 {
		t.Fatalf("ali: %+v", ali)
	}
	if ali.Refund != 20<<30 || ali.Net != 80<<30 { // the renewal gave back a-1's unused 20 GB
		t.Fatalf("refund: %+v", ali)
	}
	if len(r.Daily) != 7 || r.Daily[6].Count != 3 || r.Daily[6].Bytes != 100<<30 {
		t.Fatalf("daily: %+v", r.Daily)
	}
}

func TestHiddenTunnels(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.HideTunnel(ctx, "nl-1", "ir72", true); err != nil {
		t.Fatal(err)
	}
	_ = s.HideTunnel(ctx, "nl-1", "ir85", true)
	_ = s.HideTunnel(ctx, "nl-1", "ir85", false)
	if h := s.hiddenTunnels(ctx); !h["nl-1:ir72"] || h["nl-1:ir85"] || len(h) != 1 {
		t.Fatalf("hidden: %v", h)
	}
	if s.HideTunnel(ctx, "nl-1", "a,b", true) == nil {
		t.Fatal("bad iface accepted")
	}
}
