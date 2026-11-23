package tgbot

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func TestResellerSellsPanelsInItsBot(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	_ = s.PutCard(ctx, hubcore.CardInfo{Number: "6037000011112222", Holder: "Owner"})
	_ = s.PutRepSale(ctx, hubcore.RepSale{Packages: []hubcore.RepPackage{{ID: "tb", GB: 1024, Price: 2_500_000, Inbounds: []string{"in1"}}}, WholesaleTB: 1_300_000})
	_ = s.PutRep(ctx, hubcore.Rep{ID: "ali", Name: "Ali", Enabled: true})
	const aliChat, cust = int64(7001), int64(7100)
	_ = s.SetTgRepOwners(ctx, "ali", []int64{aliChat})
	_ = s.PutCardFor(ctx, "ali", hubcore.CardInfo{Number: "6037000033334444", Holder: "Ali"})
	api, mainAPI := &fakeAPI{}, &fakeAPI{}
	rb := &Bot{Store: s, API: api, Owners: map[int64]bool{}, Seller: "ali", SubBase: "https://panel.example", Logf: t.Logf}
	mb := &Bot{Store: s, API: mainAPI, Owners: map[int64]bool{ownerID: true}, SubBase: "https://panel.example", Logf: t.Logf}

	rb.Handle(ctx, msg(cust, "/start"))
	for _, m := range api.take() {
		if hasButton(m.kb, "پنل نمایندگی") {
			t.Fatal("not before the owner allows it")
		}
	}
	_ = s.PutResaleOwner(ctx, "ali", true, false, 0)
	rb.Handle(ctx, cb(cust, "P:buy"))
	find(t, api.take(), cust, "۲٬۵۰۰٬۰۰۰")
	rb.Handle(ctx, cb(cust, "P:pk:tb"))
	api.take()
	rb.Handle(ctx, msg(cust, "shop-one"))
	find(t, api.take(), cust, "6037")
	rb.Handle(ctx, photoMsg(cust))
	out := api.take()
	find(t, out, cust, "دریافت شد")
	rc := find(t, out, aliChat, "سود")
	if !rc.photo || !hasButton(rc.kb, "تأیید") {
		t.Fatal("the reseller approves its own receipt")
	}
	// The main bot does not approve a reseller's panel order.
	mb.Handle(ctx, cb(ownerID, "P:ok:1"))
	mainAPI.take()
	rb.Handle(ctx, cb(aliChat, "P:ok:1"))
	if m := find(t, api.take(), aliChat, "کافی نیست"); !hasButton(m.kb, "کیف پول") {
		t.Fatal("no funds: points to the wallet")
	}
	// The reseller charges its wallet in the main bot; the owner approves.
	mb.Handle(ctx, cb(aliChat, "P:wt"))
	mainAPI.take()
	mb.Handle(ctx, msg(aliChat, "1,500,000"))
	find(t, mainAPI.take(), aliChat, "6037-0000-1111-2222")
	mb.Handle(ctx, photoMsg(aliChat))
	find(t, mainAPI.take(), ownerID, "کیف پول نماینده")
	mb.Handle(ctx, cb(ownerID, "P:ok:2"))
	find(t, mainAPI.take(), aliChat, "۱٬۵۰۰٬۰۰۰")
	rb.Handle(ctx, cb(aliChat, "P:ok:1"))
	out = api.take()
	find(t, out, aliChat, "از کیف پول شما کم شد")
	find(t, out, cust, "shop-one")
	if s.RepWallet(ctx, "ali") != 200_000 || !s.IsSellerOf(ctx, "ali", "shop-one") {
		t.Fatal("paid from the wallet, the panel is ali's")
	}
	// The wallet card in the reseller's bot links to the main bot.
	rb.Handle(ctx, cb(aliChat, "P:w"))
	find(t, api.take(), aliChat, "۲۰۰٬۰۰۰")
}
