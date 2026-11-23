package tgbot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// TestCustomerTranscript writes what a new customer sees, step by step, to
// DARK_TRANSCRIPT (a review aid; skipped without it).
func TestCustomerTranscript(t *testing.T) {
	out := os.Getenv("DARK_TRANSCRIPT")
	if out == "" {
		t.Skip()
	}
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	owner := hubcore.Principal{User: "o", Role: "owner"}
	_ = s.PutProduct(ctx, owner, hubcore.Product{ID: "p30", Name: "۳۰ گیگ یک‌ماهه", Plan: "volume", GB: 30, Days: 30, Inbounds: []string{"in1"}, Price: 200000, Status: "published"})
	_ = s.PutProduct(ctx, owner, hubcore.Product{ID: "p60", Name: "۶۰ گیگ یک‌ماهه", Plan: "volume", GB: 60, Days: 30, Inbounds: []string{"in1"}, Price: 350000, Status: "published"})
	_ = s.PutCard(ctx, hubcore.CardInfo{Number: "6037-9911-2233-4455", Holder: "Ali", Bank: "Melli"})
	_ = s.PutTrialPolicy(ctx, hubcore.TrialPolicy{Enabled: true, GB: 1, Hours: 24, Inbounds: []string{"in1"}})
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://sub.example", Now: time.Now, Logf: t.Logf}
	b.Tick(ctx)
	var sb strings.Builder
	const me = int64(5555)
	var last []sent
	dump := func(step string) {
		last = api.take()
		fmt.Fprintf(&sb, "\n===== %s\n", step)
		for _, m := range last {
			who := "CUSTOMER"
			if m.chat == ownerID {
				who = "OWNER"
			} else if m.chat != me {
				who = fmt.Sprint(m.chat)
			}
			fmt.Fprintf(&sb, "--> %s%s:\n%s\n", who, map[bool]string{true: " [photo]"}[m.photo], m.text)
			if m.kb != nil {
				for _, r := range m.kb.Rows {
					var bs []string
					for _, x := range r {
						bs = append(bs, fmt.Sprintf("[%s|%s]", x.Text, x.Data+x.URL))
					}
					fmt.Fprintf(&sb, "    %s\n", strings.Join(bs, " "))
				}
			}
		}
	}
	press := func(chat int64, label string) {
		for i := len(last) - 1; i >= 0; i-- {
			m := last[i]
			if m.chat != chat || m.kb == nil {
				continue
			}
			for _, r := range m.kb.Rows {
				for _, x := range r {
					if strings.Contains(x.Text, label) && x.Data != "" {
						b.Handle(ctx, cb(chat, x.Data))
						dump(fmt.Sprintf("%d presses «%s»", chat, x.Text))
						return
					}
				}
			}
		}
		fmt.Fprintf(&sb, "\n!!!!! no button «%s» for %d\n", label, chat)
	}
	say := func(chat int64, text string) {
		b.Handle(ctx, msg(chat, text))
		dump(fmt.Sprintf("%d says %q", chat, text))
	}
	photo := func(chat int64) {
		p := msg(chat, "")
		p.Message.Photo = []struct {
			FileID string `json:"file_id"`
		}{{FileID: "big"}}
		b.Handle(ctx, p)
		dump(fmt.Sprintf("%d sends a photo", chat))
	}
	say(me, "/start")
	press(me, "تست")
	say(me, "/start")
	press(me, "خرید")
	press(me, "۳۰ گیگ")
	press(me, "خرید ۳۰")
	photo(me)
	press(ownerID, "تأیید")
	say(me, "/start")
	press(me, "سرویس")
	press(me, "تمدید")
	press(me, "۶۰ گیگ")
	say(me, "/start")
	press(me, "کیف")
	press(me, "شارژ")
	say(me, "500000")
	photo(me)
	press(ownerID, "تأیید")
	say(me, "/start")
	press(me, "حساب من")
	say(me, "/start")
	press(me, "آموزش")
	_ = os.WriteFile(out, []byte(sb.String()), 0o644)
}

// TestResellerTranscript opens every button of a reseller's home in its own
// bot and writes what each shows (review aid; DARK_TRANSCRIPT2).
func TestResellerTranscript(t *testing.T) {
	out := os.Getenv("DARK_TRANSCRIPT2")
	if out == "" {
		t.Skip()
	}
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	_ = s.PutRep(ctx, hubcore.Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "ali", 500<<30, 0, "")
	const aliChat = int64(7001)
	_ = s.SetTgRepOwners(ctx, "ali", []int64{aliChat})
	_ = s.PutCardFor(ctx, "ali", hubcore.CardInfo{Number: "6037000033334444", Holder: "Ali"})
	_ = s.PutRepSale(ctx, hubcore.RepSale{Packages: []hubcore.RepPackage{{ID: "tb", Name: "1 TB", GB: 1024, Price: 2_500_000, Inbounds: []string{"in1"}}}, WholesaleTB: 1_300_000})
	_ = s.PutResaleOwner(ctx, "ali", true, true, 0)
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{}, Seller: "ali", SubBase: "https://panel.example", Now: time.Now, Logf: t.Logf}
	var sb strings.Builder
	dump := func(step string) []sent {
		l := api.take()
		fmt.Fprintf(&sb, "\n===== %s\n", step)
		for _, m := range l {
			fmt.Fprintf(&sb, "--> %d%s:\n%s\n", m.chat, map[bool]string{true: " [photo]"}[m.photo], m.text)
			if m.kb != nil {
				for _, r := range m.kb.Rows {
					var bs []string
					for _, x := range r {
						bs = append(bs, fmt.Sprintf("[%s|%s]", x.Text, x.Data+x.URL))
					}
					fmt.Fprintf(&sb, "    %s\n", strings.Join(bs, " "))
				}
			}
		}
		return l
	}
	b.Handle(ctx, msg(aliChat, "/start"))
	home := dump("reseller /start")
	var btns []Button
	for _, m := range home {
		if m.kb != nil {
			for _, r := range m.kb.Rows {
				btns = append(btns, r...)
			}
		}
	}
	for _, x := range btns {
		if x.Data == "" {
			continue
		}
		b.Handle(ctx, cb(aliChat, x.Data))
		dump("reseller presses «" + x.Text + "»")
	}
	b.Handle(ctx, msg(8001, "/start"))
	dump("a customer of the reseller /start")
	_ = os.WriteFile(out, []byte(sb.String()), 0o644)
}
