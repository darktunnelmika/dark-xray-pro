package tgbot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestMenuLeadsWithWhatTheCustomerNeeds(t *testing.T) {
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
	const cust = int64(7300)
	menu := func() sent {
		b.Handle(ctx, msg(cust, "/start"))
		for _, m := range api.take() {
			if m.chat == cust && strings.Contains(m.text, "سلام") {
				return m
			}
		}
		t.Fatal("no menu")
		return sent{}
	}
	first := func(m sent) string { return m.kb.Rows[0][0].Text }

	// A new customer: a greeting, the pitch, and buying first. Support is on
	// the bottom keyboard, not repeated here.
	m := menu()
	if !strings.Contains(m.text, "خوش آمدید") || first(m) != "🛍 خرید اشتراک" || hasButton(m.kb, "پشتیبانی") || hasButton(m.kb, "سرویس من") {
		t.Fatalf("new customer: %q %+v", m.text, m.kb.Rows)
	}
	_ = s.PutTrialPolicy(ctx, hubcore.TrialPolicy{Enabled: true, GB: 1, Hours: 24, MaxIPs: 1})
	if m = menu(); first(m) != "🎁 تست رایگان" || !strings.Contains(m.text, "رایگان امتحان") {
		t.Fatalf("with a trial on, it leads: %+v", m.kb.Rows)
	}
	_ = s.PutTrialPolicy(ctx, hubcore.TrialPolicy{Enabled: false, GB: 1, Hours: 24, MaxIPs: 1})

	// A running service: its card with a usage bar, and "my service" first.
	_ = s.PutProduct(ctx, hubcore.Principal{User: "o", Role: "owner"}, hubcore.Product{ID: "p40", Name: "🔹 ۴۰ گیگ · دو ماهه", Plan: "volume", GB: 40, Days: 60, Price: 400000, Status: "published"})
	id, err := s.CreateOrder(ctx, hubcore.Principal{User: "o", Role: "owner"}, "p40", hubcore.TgBuyer(cust), "", "")
	if err != nil {
		t.Fatal(err)
	}
	email, err := s.FulfillOrder(ctx, hubcore.Principal{User: "o", Role: "owner"}, id)
	if err != nil {
		t.Fatal(err)
	}
	m = menu()
	if !strings.Contains(m.text, "📦 ۴۰ گیگ · دو ماهه") || strings.Contains(m.text, "🔹") || !strings.Contains(m.text, "▱▱▱▱▱▱▱▱▱▱ ۰٪") || !strings.Contains(m.text, "حجم مانده") {
		t.Fatalf("service card: %q", m.text)
	}
	if strings.Contains(m.text, email) {
		t.Fatalf("one service needs no technical name: %q", m.text)
	}
	if first(m) != "📦 سرویس من" || hasButton(m.kb, "تمدید سرویس") {
		t.Fatalf("running service: %+v", m.kb.Rows)
	}
	// Used up: the card says so and renewing leads, straight to that service.
	if _, err := s.ApplyUsage(ctx, "n1", wire.UsageBatch{Seq: 1, ToUnix: time.Now().Unix(), Traffic: []wire.UserTraffic{{Email: email, Up: 40 << 30, Down: 1}}}); err != nil {
		t.Fatal(err)
	}
	m = menu()
	if first(m) != "🔄 تمدید سرویس" || m.kb.Rows[0][0].Data != "rnw:"+email || !strings.Contains(m.text, "تمام شد") {
		t.Fatalf("ended service: %q %+v", m.text, m.kb.Rows)
	}
	// With several services the menu lists one line each, renewals first,
	// instead of a wall of cards.
	for i := 0; i < 2; i++ {
		id, err := s.CreateOrder(ctx, hubcore.Principal{User: "o", Role: "owner"}, "p40", hubcore.TgBuyer(cust), "", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.FulfillOrder(ctx, hubcore.Principal{User: "o", Role: "owner"}, id); err != nil {
			t.Fatal(err)
		}
	}
	m = menu()
	if !strings.Contains(m.text, "۳ سرویس") || !strings.Contains(m.text, "۱ نیاز به تمدید") || strings.Contains(m.text, "▰") || strings.Contains(m.text, "<code>") {
		t.Fatalf("several services: %q", m.text)
	}
	if strings.Index(m.text, "🔴") > strings.Index(m.text, "🟢") && strings.Contains(m.text, "🟢") {
		t.Fatalf("ended services come first: %q", m.text)
	}
}

// The seller's look reorders, renames, recolours, hides and widens the
// menu's buttons; with nothing saved the built-in layout stays.
func TestLayoutMenuLook(t *testing.T) {
	list := []menuButton{
		{"trial", cbBtn("🎁 تست رایگان", "trial"), true},
		{"shop", cbBtn("🛍 خرید اشتراک", "shop"), true},
		{"guide", cbBtn("📚 آموزش اتصال", "guide"), false},
		{"status", cbBtn("🌐 وضعیت سرورها", "status"), false},
		{"me", cbBtn("👤 حساب من", "me"), false},
	}
	shape := func(rows [][]Button) string {
		var out []string
		for _, r := range rows {
			var x []string
			for _, b := range r {
				x = append(x, b.Data)
			}
			out = append(out, strings.Join(x, "+"))
		}
		return strings.Join(out, " | ")
	}
	if got := shape(layoutMenu(list, hubcore.BotLook{})); got != "trial | shop | guide+status | me" {
		t.Fatal(got)
	}
	look := hubcore.BotLook{Buttons: []hubcore.BotLookButton{
		{Key: "shop", Label: "⚡️ خرید سرویس", Style: "success", Wide: true},
		{Key: "me"},
		{Key: "renew", Wide: true}, // not offered to this customer: simply absent
		{Key: "trial", Style: "plain"},
		{Key: "status", Hidden: true},
	}}
	rows := layoutMenu(list, look)
	if got := shape(rows); got != "shop | me+trial | guide" {
		t.Fatal(got) // guide is not in the saved look: after the others
	}
	if b := rows[0][0]; b.Text != "⚡️ خرید سرویس" || b.Style != "success" {
		t.Fatalf("%+v", b)
	}
	if b := rows[1][1]; b.Text != "🎁 تست رایگان" || b.Style != "plain" {
		t.Fatalf("%+v", b)
	}
	// "plain" reaches Telegram as no colour at all, even for a word that
	// would otherwise be coloured.
	styled := styleKeyboard(&Keyboard{Rows: rows}, nil)
	if styled.Rows[1][1].Style != "" || styled.Rows[0][0].Style != "success" {
		t.Fatalf("%+v", styled.Rows)
	}
}
