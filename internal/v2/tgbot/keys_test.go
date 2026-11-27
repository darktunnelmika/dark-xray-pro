package tgbot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func keyTexts(rows [][]KeyButton) [][]string {
	var out [][]string
	for _, r := range rows {
		var t []string
		for _, k := range r {
			t = append(t, k.Text)
		}
		out = append(out, t)
	}
	return out
}

func TestCustomerKeys(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://sub.example", Now: time.Now, Logf: t.Logf}
	got := keyTexts(b.customerKeys(ctx, buyerID))
	if len(got) != 2 || got[0][0] != "🛍 خرید اشتراک" || got[0][1] != "👤 حساب من" || got[1][0] != "📚 آموزش اتصال" || got[1][1] != "💬 پشتیبانی" {
		t.Fatalf("plain keys %v", got)
	}
	if err := s.PutWheelFor(ctx, "", hubcore.WheelConfig{On: true, Slices: []hubcore.WheelSlice{{Kind: "gb", Value: 1, Weight: 1}, {Kind: "none", Weight: 1}}}); err != nil {
		t.Fatal(err)
	}
	rows := b.customerKeys(ctx, buyerID)
	got = keyTexts(rows)
	if len(got) != 3 || got[0][1] != "🎡 گردونهٔ شانس" || got[1][1] != "🌐 وضعیت سرورها" || rows[0][0].Style != "success" || rows[0][1].Style != "primary" {
		t.Fatalf("wheel keys %v", got)
	}
	for in, want := range map[string]string{"🎡 گردونهٔ شانس": "wheel", "گردونهٔ شانس": "wheel", "👤 حساب من": "me", "حساب من": "me", "🎁 تست رایگان": "trial",
		"🌐 وضعیت سرورها": "status", "🤝 پنل نمایندگی": "panel", "🛍 خرید اشتراک": "shop"} {
		if a := keyboardAction(in); a != want {
			t.Errorf("%q → %q, want %q", in, a, want)
		}
	}
	// The keys work: the wheel spins (only "none" fits a chat without services), the account opens.
	b.Handle(ctx, msg(buyerID, "🎡 گردونهٔ شانس"))
	find(t, api.take(), buyerID, "گردونهٔ شانس")
	b.Handle(ctx, msg(buyerID, "👤 حساب من"))
	if len(api.take()) == 0 {
		t.Fatal("account")
	}
	if keysSig(rows) == keysSig(b.customerKeys(ctx, buyerID)[:1]) {
		t.Fatal("sig")
	}
}
