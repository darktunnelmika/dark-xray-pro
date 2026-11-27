package hubcore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWheel(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	for _, e := range []string{"alice", "bob"} {
		if err := s.TgLink(ctx, 700, e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SpinWheel(ctx, "", 700); !errors.Is(err, ErrNotFound) {
		t.Fatalf("off: %v", err)
	}
	if err := s.PutWheelFor(ctx, "", WheelConfig{On: true, Slices: []WheelSlice{{Kind: "bad", Value: 1, Weight: 1}, {Kind: "gb", Value: 1, Weight: 1}}}); err == nil {
		t.Fatal("bad kind")
	}
	if err := s.PutWheelFor(ctx, "", WheelConfig{On: true, Slices: []WheelSlice{{Kind: "gb", Value: 1, Weight: 1}}}); err == nil {
		t.Fatal("one slice")
	}
	// GB goes to the service that can take it (bob is unlimited).
	put := func(sl ...WheelSlice) {
		t.Helper()
		if err := s.PutWheelFor(ctx, "", WheelConfig{On: true, NeedService: true, Slices: sl}); err != nil {
			t.Fatal(err)
		}
	}
	put(WheelSlice{Kind: "gb", Value: 2, Weight: 1}, WheelSlice{Kind: "gb", Value: 2, Weight: 1})
	if st := s.WheelStatus(ctx, "", 700); st["on"] != true || st["today"] != nil {
		t.Fatalf("status %v", st)
	}
	sp, err := s.SpinWheel(ctx, "", 700)
	if err != nil || sp.Kind != "gb" || sp.Target != "alice" {
		t.Fatalf("gb %+v %v", sp, err)
	}
	if c, _ := s.ClientByEmail(ctx, "alice"); c.VolumeBytes != 1000+2<<30 {
		t.Fatalf("alice volume %d", c.VolumeBytes)
	}
	if _, err := s.SpinWheel(ctx, "", 700); !errors.Is(err, ErrSpun) {
		t.Fatalf("twice: %v", err)
	}
	if st := s.WheelStatus(ctx, "", 700); st["today"] == nil {
		t.Fatal("today")
	}
	// The next Tehran day: the wallet.
	*now = now.Add(24 * time.Hour)
	put(WheelSlice{Kind: "wallet", Value: 5000, Weight: 1}, WheelSlice{Kind: "wallet", Value: 5000, Weight: 1})
	if sp, err := s.SpinWheel(ctx, "", 700); err != nil || sp.Kind != "wallet" || s.TomanBalance(ctx, "", 700) != 5000 {
		t.Fatalf("wallet %+v %v %d", sp, err, s.TomanBalance(ctx, "", 700))
	}
	// And a one-time discount code.
	*now = now.Add(24 * time.Hour)
	put(WheelSlice{Kind: "discount", Value: 15, Weight: 1}, WheelSlice{Kind: "none", Weight: 1})
	for {
		sp, err := s.SpinWheel(ctx, "", 700)
		if err != nil {
			t.Fatal(err)
		}
		if sp.Kind == "none" { // try again tomorrow
			*now = now.Add(24 * time.Hour)
			continue
		}
		if !strings.HasPrefix(sp.Code, "WH") {
			t.Fatalf("code %+v", sp)
		}
		found := false
		list, _ := s.DiscountsOf(ctx, "")
		for _, d := range list {
			found = found || (d.Code == sp.Code && d.Percent == 15 && d.MaxUses == 1)
		}
		if !found {
			t.Fatal("discount not made")
		}
		break
	}
	// Days only when a service can take them; no service, no spin.
	if _, err := s.SpinWheel(ctx, "", 701); !errors.Is(err, ErrPolicy) {
		t.Fatalf("no service: %v", err)
	}
	if n := s.WheelToday(ctx, "")["spins"]; n != 1 {
		t.Fatalf("today %d", n)
	}
}
