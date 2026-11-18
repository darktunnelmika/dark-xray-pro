package hubcore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTelegramCommerce(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	owner := Principal{User: "boss", Role: "owner"}
	if err := s.PutProduct(ctx, owner, Product{ID: "m30", Name: "30 GB", Plan: "volume", GB: 30, Days: 30, Price: 150000, PriceStars: 250, Status: "published", Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProduct(ctx, owner, Product{ID: "cash", Name: "cash only", Plan: "volume", GB: 10, Days: 30, Price: 50000, Status: "published", Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	const ali, bob = int64(111), int64(222)
	if err := s.PutReferralPolicy(ctx, ReferralPolicy{Enabled: true, Percent: 10, Fixed: 5, MaxReward: 100}); err != nil {
		t.Fatal(err)
	}
	code, err := s.RefCode(ctx, ali)
	if err != nil || len(code) != 7 {
		t.Fatal(code, err)
	}
	if again, _ := s.RefCode(ctx, ali); again != code {
		t.Fatal("code must be stable")
	}
	if _, err := s.AttributeReferral(ctx, ali, code); err == nil {
		t.Fatal("self invite")
	}
	if ref, err := s.AttributeReferral(ctx, bob, code); err != nil || ref != ali {
		t.Fatal(ref, err)
	}
	if _, err := s.AttributeReferral(ctx, bob, code); err == nil {
		t.Fatal("invited twice")
	}
	var notified int64
	SetRewardHook(func(r, stars int64) { notified = stars })
	defer SetRewardHook(nil)
	// Stars purchase by bob.
	if _, _, _, err := s.StarsCheckout(ctx, bob, "cash", ""); !errors.Is(err, ErrNotForStars) {
		t.Fatal("cash-only product sold for Stars")
	}
	id, stars, _, err := s.StarsCheckout(ctx, bob, "m30", "")
	if err != nil || stars != 250 {
		t.Fatal(stars, err)
	}
	if id2, _, _, _ := s.StarsCheckout(ctx, bob, "m30", ""); id2 != id {
		t.Fatal("a second tap must reuse the open invoice")
	}
	if err := s.StarsPrecheck(ctx, id, ali, 250); err == nil {
		t.Fatal("someone else's invoice")
	}
	if err := s.StarsPrecheck(ctx, id, bob, 249); err == nil {
		t.Fatal("wrong amount")
	}
	if err := s.StarsPrecheck(ctx, id, bob, 250); err != nil {
		t.Fatal(err)
	}
	email, err := s.StarsPaid(ctx, id, bob, 250, "charge-1")
	if err != nil || email == "" {
		t.Fatal(email, err)
	}
	if again, err := s.StarsPaid(ctx, id, bob, 250, "charge-1"); err != nil || again != email {
		t.Fatal("a repeated payment update must be harmless", err)
	}
	c, _ := s.ClientByEmail(ctx, email)
	if !c.Enabled || c.VolumeBytes != 30<<30 {
		t.Fatalf("%+v", c.Client)
	}
	// ali earned 5 + 10% of 250 = 30 Stars, once.
	if bal := s.WalletBalance(ctx, ali); bal != 30 || notified != 30 {
		t.Fatalf("reward %d (notified %d)", bal, notified)
	}
	if _, r := s.rewardReferral(ctx, id); r != 0 {
		t.Fatal("rewarded twice")
	}
	// Refund disables the client it created.
	if _, _, err := s.StarsRefundInfo(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.StarsRefunded(ctx, id); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.ClientByEmail(ctx, email); c.Enabled {
		t.Fatal("refunded client still enabled")
	}
	// Wallet: ali has 30, product costs 250.
	if _, _, err := s.WalletPay(ctx, ali, "m30", ""); err == nil {
		t.Fatal("paid without enough credit")
	}
	if _, err := s.WalletAdjust(ctx, ali, 300, "gift"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WalletAdjust(ctx, ali, -1000, ""); err == nil {
		t.Fatal("balance below zero")
	}
	em2, left, err := s.WalletPay(ctx, ali, "m30", "")
	if err != nil || left != 80 || em2 == "" {
		t.Fatal(em2, left, err)
	}
	hist, _ := s.WalletHistory(ctx, ali, 10)
	if len(hist) != 3 || hist[0].Kind != "spend" || hist[0].Delta != -250 {
		t.Fatalf("%+v", hist)
	}
	// Trial: once per account, needs inbounds.
	if s.TrialPolicy(ctx).Enabled {
		t.Fatal("the trial is off until its seller turns it on")
	}
	if _, err := s.ClaimTrial(ctx, "", bob); !errors.Is(err, ErrNotFound) {
		t.Fatal("trial while off")
	}
	// Turned on without a choice of inbounds, the trial uses the seller's inbounds.
	if err := s.PutTrialPolicy(ctx, TrialPolicy{Enabled: true, GB: 1, Hours: 24}); err != nil || len(s.TrialPolicy(ctx).Inbounds) == 0 {
		t.Fatal("trial without chosen inbounds", err)
	}
	if err := s.PutTrialPolicy(ctx, TrialPolicy{Enabled: true, GB: 1, Hours: 24, MaxIPs: 1, Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	te, err := s.ClaimTrial(ctx, "", bob)
	if err != nil || te != "trial-222" || !s.TrialUsed(ctx, "", bob) {
		t.Fatal(te, err)
	}
	if _, err := s.ClaimTrial(ctx, "", bob); !errors.Is(err, ErrTrialUsed) {
		t.Fatal("second trial")
	}
	mine, _ := s.TgClients(ctx, bob)
	found := false
	for _, e := range mine {
		found = found || e == "trial-222"
	}
	if !found {
		t.Fatal("trial must show under the customer's services")
	}
}

func TestRepPermissions(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	c := Client{Email: "r1", Enabled: true, VolumeBytes: 10 << 30, ExpiresUnix: now.Unix() + 30*86400, Inbounds: []string{"de-vless"}}
	pol := DefaultRepPolicy
	pol.DenyCreate = true
	if err := s.SetRepPolicy(ctx, "ali", pol); err != nil {
		t.Fatal(err)
	}
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); !errors.Is(err, ErrPolicy) {
		t.Fatalf("create while denied: %v", err)
	}
	pol.DenyCreate, pol.DenyRenew = false, true
	_ = s.SetRepPolicy(ctx, "ali", pol)
	if err := s.RepPutClient(ctx, "ali", "ali", c, "volume"); err != nil {
		t.Fatal(err)
	}
	more := c
	more.VolumeBytes = 20 << 30
	if err := s.RepPutClient(ctx, "ali", "ali", more, "volume"); !errors.Is(err, ErrPolicy) {
		t.Fatalf("renew while denied: %v", err)
	}
	pol.DenyRenew, pol.DenyEdit = false, true
	_ = s.SetRepPolicy(ctx, "ali", pol)
	ips := c
	ips.MaxIPs = 5
	if err := s.RepPutClient(ctx, "ali", "ali", ips, "volume"); !errors.Is(err, ErrPolicy) {
		t.Fatalf("edit while denied: %v", err)
	}
	if err := s.RepPutClient(ctx, "ali", "ali", more, "volume"); err != nil {
		t.Fatal("renew allowed", err)
	}
	got, _ := s.RepPolicyOf(ctx, "ali")
	if !got.DenyEdit || got.DenyRenew || got.DenyCreate {
		t.Fatalf("%+v", got)
	}
}

func TestAutoRenew(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	owner := Principal{User: "boss", Role: "owner"}
	if err := s.PutProduct(ctx, owner, Product{ID: "m30", Name: "30 GB", Plan: "volume", GB: 30, Days: 30, Price: 150000, PriceStars: 250, Status: "published", Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	const chat = int64(777)
	if err := s.PutClient(ctx, Client{Email: "carol", Enabled: true, VolumeBytes: 30 << 30, ExpiresUnix: now.Unix() + 3*86400, Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAutoRenew(ctx, chat, "carol", "m30"); err == nil {
		t.Fatal("a chat that does not own the service")
	}
	if err := s.TgLink(ctx, chat, "carol"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAutoRenew(ctx, chat, "carol", "m30"); err != nil {
		t.Fatal(err)
	}
	var told []string
	old := customerNotify
	customerNotify = func(_ context.Context, _ int64, text string) { told = append(told, text) }
	defer func() { customerNotify = old }()
	wait := func() { time.Sleep(60 * time.Millisecond) }
	if n := s.AutoRenewOnce(ctx); n != 0 {
		t.Fatal("three days left: not due yet")
	}
	*now = now.Add(2*24*time.Hour + 13*time.Hour) // under a day left
	if n := s.AutoRenewOnce(ctx); n != 0 {
		t.Fatal("no credit: nothing to renew")
	}
	wait()
	if len(told) != 1 || !strings.Contains(told[0], "کافی نیست") {
		t.Fatalf("one notice about credit: %v", told)
	}
	if _, err := s.WalletAdjust(ctx, chat, 300, "gift"); err != nil {
		t.Fatal(err)
	}
	if n := s.AutoRenewOnce(ctx); n != 0 {
		t.Fatal("tries at most every six hours")
	}
	*now = now.Add(7 * time.Hour)
	before, _ := s.ClientByEmail(ctx, "carol")
	if n := s.AutoRenewOnce(ctx); n != 1 {
		t.Fatal("should renew now")
	}
	wait()
	after, _ := s.ClientByEmail(ctx, "carol")
	if after.EffectiveUntil <= before.EffectiveUntil || s.WalletBalance(ctx, chat) != 50 {
		t.Fatalf("renewal %d -> %d, balance %d", before.EffectiveUntil, after.EffectiveUntil, s.WalletBalance(ctx, chat))
	}
	if len(told) != 2 || !strings.Contains(told[1], "تمدید شد") {
		t.Fatalf("renewal notice: %v", told)
	}
	if err := s.SetAutoRenew(ctx, chat, "carol", ""); err != nil || s.AutoRenew(ctx, "carol") != "" {
		t.Fatal("turn off", err)
	}
}
