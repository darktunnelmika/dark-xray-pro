package hubcore

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestChatPanels(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutCard(ctx, CardInfo{Number: "6037991234567890", Holder: "Owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Enabled: true, WholesaleTB: 1_024_000, Packages: []RepPackage{{ID: "p1", Name: "100", GB: 100, Price: 300000}}}); err != nil {
		t.Fatal(err)
	}
	buy := func(chat int64, name string) {
		t.Helper()
		o, err := s.CreateRepOrder(ctx, "", chat, "B", "", name, "p1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.AttachRepReceipt(ctx, o.ID, chat, "photo"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ApproveRepOrder(ctx, "owner", "", o.ID); err != nil {
			t.Fatal(err)
		}
	}
	const chat = 900
	buy(chat, "first-shop")
	if rep, role := s.TgRepRole(ctx, chat); rep != "first-shop" || role != "owner" {
		t.Fatalf("first: %s %s", rep, role)
	}
	// The second panel stays with the account and is picked.
	buy(chat, "second-shop")
	if got := s.ChatRoles(ctx, chat); len(got) != 2 || got[0].Rep != "first-shop" || got[1].Rep != "second-shop" {
		t.Fatalf("roles %+v", got)
	}
	if rep := s.TgRep(ctx, chat); rep != "second-shop" {
		t.Fatalf("picked %q", rep)
	}
	if !slices.Contains(s.TgRepChats(ctx, "second-shop"), int64(chat)) || !slices.Contains(s.TgRepChats(ctx, "first-shop"), int64(chat)) {
		t.Fatal("order cards must reach the account for both panels")
	}
	if n := s.BuyerPanels(ctx, chat); n != 2 {
		t.Fatalf("count %d", n)
	}
	// Switching back and forth.
	if err := s.SetActivePanel(ctx, chat, "first-shop"); err != nil {
		t.Fatal(err)
	}
	if rep, _ := s.TgRepRole(ctx, chat); rep != "first-shop" {
		t.Fatalf("switched %q", rep)
	}
	if err := s.SetActivePanel(ctx, chat, "someone-else"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign panel: %v", err)
	}
	if err := s.SetActivePanel(ctx, 901, "first-shop"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other account: %v", err)
	}
	// A scope that only takes the second panel finds it whatever was picked.
	if rep, role := s.TgRepRoleIn(ctx, chat, "", func(r string) bool { return r == "second-shop" }); rep != "second-shop" || role != "owner" {
		t.Fatalf("scoped %s %s", rep, role)
	}
	if !s.ChatOwnsPanel(ctx, chat, "second-shop") || s.ChatOwnsPanel(ctx, 901, "second-shop") {
		t.Fatal("owns")
	}
	// A bot admin role on a third panel is listed too and can be picked.
	code, err := s.NewTgAdminInvite(ctx, "first-shop", "sales")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimTgAdminInvite(ctx, code, 777, "Helper"); err != nil {
		t.Fatal(err)
	}
	if rep, role := s.TgRepRole(ctx, 777); rep != "first-shop" || role != "sales" {
		t.Fatalf("admin %s %s", rep, role)
	}
	if s.TgRep(ctx, 777) != "" {
		t.Fatal("an admin owns nothing")
	}
	// A disabled panel drops out of the list and the pick falls back.
	if err := s.SetActivePanel(ctx, chat, "second-shop"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE reps SET enabled=0 WHERE id='second-shop'`); err != nil {
		t.Fatal(err)
	}
	if rep := s.TgRep(ctx, chat); rep != "first-shop" {
		t.Fatalf("fallback %q", rep)
	}
}

func TestPanelSetupAndLinks(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.PutRep(ctx, Rep{ID: "shop-a", Name: "A", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTgRepOwners(ctx, "shop-a", []int64{500}); err != nil {
		t.Fatal(err)
	}
	m := miniMgr{P: Principal{User: "tg:500", Role: "rep", RepID: "shop-a"}, Seller: "shop-a", Role: "owner", Chat: 500}
	steps := s.PanelSetup(ctx, m)
	keys := map[string]bool{}
	for _, st := range steps {
		keys[st.Key] = st.Done
	}
	if _, ok := keys["brand"]; !ok || keys["brand"] || keys["support"] || keys["plans"] {
		t.Fatalf("fresh panel %+v", steps)
	}
	if err := s.PutRepBrand(ctx, "shop-a", RepBrand{Title: "Ali Shop", Telegram: "@ali_support"}); err != nil {
		t.Fatal(err)
	}
	// The main store's plans to start from.
	if err := s.PutProduct(ctx, Principal{Role: "owner"}, Product{ID: "o60", Name: "60 GB", Plan: "volume", GB: 60, Days: 30, Price: 120000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.SuggestPlans(ctx, m); err != nil || n != 1 {
		t.Fatalf("suggested %d %v", n, err)
	}
	if _, err := s.SuggestPlans(ctx, m); !errors.Is(err, ErrConflict) {
		t.Fatalf("twice: %v", err)
	}
	for _, st := range s.PanelSetup(ctx, m) {
		if (st.Key == "brand" || st.Key == "support" || st.Key == "plans") && !st.Done {
			t.Fatalf("after: %+v", st)
		}
	}
	// A link to a second panel: the account keeps its main one and gets the
	// new one beside it, picked.
	if err := s.PutRep(ctx, Rep{ID: "shop-b", Name: "B", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	code, err := s.newTgLinkCode(ctx, "shop-b", 7*86400)
	if err != nil {
		t.Fatal(err)
	}
	if rep, err := s.ClaimTgLinkCode(ctx, code, 500); err != nil || rep != "shop-b" {
		t.Fatalf("claim %q %v", rep, err)
	}
	if s.tgRepMain(ctx, 500) != "shop-a" || s.TgRep(ctx, 500) != "shop-b" || len(s.ChatRoles(ctx, 500)) != 2 {
		t.Fatalf("main %q picked %q roles %+v", s.tgRepMain(ctx, 500), s.TgRep(ctx, 500), s.ChatRoles(ctx, 500))
	}
	if s.TgRepChat(ctx, "shop-a") != 500 {
		t.Fatal("the main panel keeps its account")
	}
	// A fresh account takes the panel as its main one, as before.
	code, _ = s.newTgLinkCode(ctx, "shop-b", 900)
	if _, err := s.ClaimTgLinkCode(ctx, code, 600); err != nil || s.tgRepMain(ctx, 600) != "shop-b" {
		t.Fatalf("fresh account: %v %q", err, s.tgRepMain(ctx, 600))
	}
}
