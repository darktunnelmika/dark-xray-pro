package hubcore

import (
	"context"
	"testing"
)

func TestReferralRewardInToman(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	owner := Principal{User: "owner", Role: "owner"}
	const inviter, friend = int64(100), int64(200)
	if err := s.PutReferralPolicy(ctx, ReferralPolicy{Enabled: true, Percent: 10}); err != nil {
		t.Fatal(err)
	}
	code, err := s.RefCode(ctx, inviter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttributeReferral(ctx, friend, code); err != nil {
		t.Fatal(err)
	}
	var got [3]int64
	SetRewardHook(func(r, stars, toman int64) { got = [3]int64{r, stars, toman} })
	defer SetRewardHook(nil)
	if err := s.PutProduct(ctx, owner, Product{ID: "p20", Name: "20", Plan: "volume", GB: 20, Days: 30, Price: 100000, Status: "published", Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateOrder(ctx, owner, "p20", TgBuyer(friend), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FulfillOrder(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	// A card (toman) purchase pays the inviter 10% into the toman wallet, once.
	if got != [3]int64{inviter, 0, 10000} || s.TomanBalance(ctx, "", inviter) != 10000 {
		t.Fatalf("reward %v, balance %d", got, s.TomanBalance(ctx, "", inviter))
	}
	if r, n := s.rewardReferralToman(ctx, id); r != 0 || n != 0 || s.TomanBalance(ctx, "", inviter) != 10000 {
		t.Fatal("an order rewards once")
	}
	if sum, _ := s.ReferralSummary(ctx, inviter); sum.Invited != 1 || sum.EarnedToman != 10000 {
		t.Fatalf("%+v", sum)
	}
	// A reseller's sale rewards nobody.
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "owner", "ali", 100<<30, 0, "")
	rep := Principal{User: "ali", Role: "rep", RepID: "ali"}
	if err := s.PutProduct(ctx, rep, Product{ID: "r20", Name: "20", Plan: "volume", GB: 20, Days: 30, Price: 90000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	rid, err := s.CreateOrder(ctx, rep, "r20", TgBuyer(friend), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FulfillOrder(ctx, rep, rid); err != nil {
		t.Fatal(err)
	}
	if s.TomanBalance(ctx, "", inviter) != 10000 {
		t.Fatal("a reseller's sale must not reward the owner's inviter")
	}
}
