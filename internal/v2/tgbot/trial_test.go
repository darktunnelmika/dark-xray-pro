package tgbot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func hasButton(kb *Keyboard, text string) bool {
	if kb == nil {
		return false
	}
	for _, r := range kb.Rows {
		for _, b := range r {
			if strings.Contains(b.Text, text) {
				return true
			}
		}
	}
	return false
}

func TestTrialSettingsFromTheBot(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://x", Logf: t.Logf}
	const cust = int64(4242)

	// Off by default: the customer's menu has no trial button.
	b.Handle(ctx, msg(cust, "/start"))
	for _, m := range api.take() {
		if hasButton(m.kb, "تست رایگان") {
			t.Fatal("the trial is off by default")
		}
	}
	// The owner sets volume and length, then turns it on, all in the bot.
	b.Handle(ctx, cb(ownerID, "a:settings"))
	if m := find(t, api.take(), ownerID, "تنظیمات ربات"); !hasButton(m.kb, "تست رایگان") {
		t.Fatal("bot settings must lead to the trial")
	}
	b.Handle(ctx, cb(ownerID, "a:trial"))
	find(t, api.take(), ownerID, "خاموش")
	b.Handle(ctx, cb(ownerID, "a:trial:gb"))
	api.take()
	b.Handle(ctx, msg(ownerID, "۲"))
	find(t, api.take(), ownerID, "2 گیگ")
	// Under one gigabyte too: megabytes with a unit, or a fraction of a gigabyte.
	b.Handle(ctx, cb(ownerID, "a:trial:gb"))
	api.take()
	b.Handle(ctx, msg(ownerID, "0.5"))
	find(t, api.take(), ownerID, "512 مگ")
	b.Handle(ctx, cb(ownerID, "a:trial:gb"))
	api.take()
	b.Handle(ctx, msg(ownerID, "۵۰ مگ"))
	find(t, api.take(), ownerID, "50 مگ")
	b.Handle(ctx, cb(ownerID, "a:trial:gb"))
	api.take()
	b.Handle(ctx, msg(ownerID, "50m"))
	find(t, api.take(), ownerID, "50 مگ")
	b.Handle(ctx, cb(ownerID, "a:trial:h"))
	api.take()
	b.Handle(ctx, msg(ownerID, "abc"))
	find(t, api.take(), ownerID, "فقط یک عدد")
	b.Handle(ctx, msg(ownerID, "48"))
	find(t, api.take(), ownerID, "2 روز")
	b.Handle(ctx, cb(ownerID, "a:trial:on"))
	find(t, api.take(), ownerID, "🟢 روشن")
	if p := s.TrialPolicy(ctx); !p.Enabled || p.GB != 0 || p.MB != 50 || p.Hours != 48 || len(p.Inbounds) != 1 {
		t.Fatalf("%+v", p)
	}
	// Now the customer sees it, takes it once, and it is marked.
	b.Handle(ctx, msg(cust, "/start"))
	seen := false
	for _, m := range api.take() {
		seen = seen || hasButton(m.kb, "تست رایگان")
	}
	if !seen {
		t.Fatal("the trial button must show once it is on")
	}
	b.Handle(ctx, cb(cust, "trial"))
	find(t, api.take(), cust, "50 مگ")
	c, err := s.ClientByEmail(ctx, "trial-4242")
	if err != nil || !c.Trial || c.VolumeBytes != 50<<20 {
		t.Fatalf("%+v %v", c, err)
	}
	b.Handle(ctx, cb(cust, "trial"))
	find(t, api.take(), cust, "قبلاً")
	// Off again: nobody else can take one.
	b.Handle(ctx, cb(ownerID, "a:trial:off"))
	find(t, api.take(), ownerID, "🔴 خاموش")
	b.Handle(ctx, cb(cust+1, "trial"))
	find(t, api.take(), cust+1, "در دسترس نیست")
}

func TestResellerTrialIsTheirOwn(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	_ = s.PutRep(ctx, hubcore.Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "ali", 10<<30, 0, "")
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, SubBase: "https://x", Logf: t.Logf, Seller: "ali"}
	const repChat, cust = int64(3000), int64(5151)
	code, _ := s.NewTgLinkCode(ctx, "ali")
	b.Handle(ctx, msg(repChat, "/start rep_"+code))
	if m := find(t, api.take(), repChat, "اعتبار"); !hasButton(m.kb, "تست رایگان") {
		t.Fatal("the reseller's home must lead to their trial")
	}
	b.Handle(ctx, cb(repChat, "R:trial:h"))
	api.take()
	b.Handle(ctx, msg(repChat, "1"))
	find(t, api.take(), repChat, "1 روز")
	b.Handle(ctx, cb(repChat, "R:trial:on"))
	find(t, api.take(), repChat, "🟢 روشن")
	if s.TrialPolicy(ctx).Enabled {
		t.Fatal("the owner's trial stays off")
	}
	b.Handle(ctx, cb(cust, "trial"))
	find(t, api.take(), cust, "فعال شد")
	c, err := s.ClientByEmail(ctx, "trial-5151")
	if err != nil || !c.Trial || c.RepID != "ali" {
		t.Fatalf("%+v %v", c, err)
	}
}
