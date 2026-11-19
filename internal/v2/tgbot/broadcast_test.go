package tgbot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func TestBroadcastStaysInsideEachBot(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.PutRep(ctx, hubcore.Rep{ID: "ali", Name: "Ali", Enabled: true})
	const repChat = int64(3000)
	code, _ := s.NewTgLinkCode(ctx, "ali")

	mainAPI, repAPI := &fakeAPI{}, &fakeAPI{}
	main := &Bot{Store: s, API: mainAPI, Owners: map[int64]bool{ownerID: true}, SubBase: "https://x", Logf: t.Logf}
	rep := &Bot{Store: s, API: repAPI, SubBase: "https://x", Logf: t.Logf, Seller: "ali"}

	// Two people start the main bot, two others the reseller's bot.
	main.Handle(ctx, msg(501, "/start"))
	main.Handle(ctx, msg(502, "/start"))
	rep.Handle(ctx, msg(repChat, "/start rep_"+code))
	rep.Handle(ctx, msg(601, "/start"))
	rep.Handle(ctx, msg(602, "/start"))
	mainAPI.take()
	repAPI.take()

	wait := func(api *fakeAPI, chat int64, want string) []sent {
		t.Helper()
		var all []sent
		for i := 0; i < 100; i++ {
			all = append(all, api.take()...)
			for _, m := range all {
				if m.chat == chat && contains(m.text, want) {
					return all
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("no %q to %d in %+v", want, chat, all)
		return nil
	}
	got := func(all []sent, chat int64, text string) bool {
		for _, m := range all {
			if m.chat == chat && contains(m.text, text) {
				return true
			}
		}
		return false
	}

	// The reseller's home in their own bot offers the broadcast.
	rep.Handle(ctx, msg(repChat, "/start"))
	if m := find(t, repAPI.take(), repChat, "اعتبار"); !hasButton(m.kb, "پیام همگانی") {
		t.Fatal("a reseller broadcasts from their own bot")
	}
	rep.Handle(ctx, cb(repChat, "R:bc"))
	repAPI.take()
	rep.Handle(ctx, msg(repChat, "سلام مشتری‌های علی"))
	if m := find(t, repAPI.take(), repChat, "برای چه کسانی"); hasButton(m.kb, "نماینده‌ها") {
		t.Fatal("a reseller has no reseller audience")
	}
	rep.Handle(ctx, cb(repChat, "R:bca:all"))
	repAPI.take()
	rep.Handle(ctx, cb(repChat, "R:bcok"))
	all := wait(repAPI, repChat, "تمام شد")
	if !got(all, 601, "سلام مشتری‌های علی") || !got(all, 602, "سلام مشتری‌های علی") || got(all, 501, "سلام") {
		t.Fatalf("the reseller's message goes to their bot's users only: %+v", all)
	}
	if len(mainAPI.take()) != 0 {
		t.Fatal("nothing is sent through the main bot")
	}

	// The owner's broadcast reaches the main bot's users, not the reseller's.
	main.Handle(ctx, cb(ownerID, "a:bc"))
	mainAPI.take()
	main.Handle(ctx, msg(ownerID, "خبر از دارک"))
	if m := find(t, mainAPI.take(), ownerID, "برای چه کسانی"); !hasButton(m.kb, "نماینده‌ها") {
		t.Fatal("the owner can write to the resellers")
	}
	main.Handle(ctx, cb(ownerID, "a:bca:all"))
	mainAPI.take()
	main.Handle(ctx, cb(ownerID, "a:bcok"))
	all = wait(mainAPI, ownerID, "تمام شد")
	if !got(all, 501, "خبر از دارک") || !got(all, 502, "خبر از دارک") || got(all, 601, "خبر") {
		t.Fatalf("the owner's message goes to the main bot's users only: %+v", all)
	}
	// ... and to the resellers when that audience is chosen.
	main.Handle(ctx, cb(ownerID, "a:bc"))
	mainAPI.take()
	main.Handle(ctx, msg(ownerID, "نماینده‌های عزیز"))
	mainAPI.take()
	main.Handle(ctx, cb(ownerID, "a:bca:reps"))
	mainAPI.take()
	main.Handle(ctx, cb(ownerID, "a:bcok"))
	all = wait(mainAPI, ownerID, "تمام شد")
	if !got(all, repChat, "نماینده‌های عزیز") || got(all, 501, "نماینده‌های عزیز") {
		t.Fatalf("the reseller audience: %+v", all)
	}

	// The owner's bottom "main menu" opens management, not the customer menu.
	main.Handle(ctx, msg(ownerID, "🏠 منوی اصلی"))
	find(t, mainAPI.take(), ownerID, "پنل مدیریت")
}

func contains(s, sub string) bool { return len(sub) > 0 && len(s) >= len(sub) && (stringIndex(s, sub) >= 0) }

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
