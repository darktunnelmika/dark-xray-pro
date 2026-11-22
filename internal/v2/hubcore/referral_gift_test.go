package hubcore

import (
	"context"
	"testing"
)

func TestReferralGiftAndBonus(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	owner := Principal{User: "owner", Role: "owner"}
	const inviter, friend, stranger = int64(100), int64(200), int64(300)
	if err := s.PutReferralPolicy(ctx, ReferralPolicy{Enabled: true, InviteePercent: 91}); err == nil {
		t.Fatal("a 91% gift must be refused")
	}
	if err := s.PutReferralPolicy(ctx, ReferralPolicy{Enabled: true, InviteePercent: 20, BonusGB: 5}); err != nil {
		t.Fatal(err)
	}
	code, _ := s.RefCode(ctx, inviter)
	if _, err := s.AttributeReferral(ctx, friend, code); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProduct(ctx, owner, Product{ID: "p20", Name: "20", Plan: "volume", GB: 20, Days: 30, Price: 100000, Status: "published", Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	if s.InviteGift(ctx, friend) != 20 || s.InviteGift(ctx, stranger) != 0 {
		t.Fatal("only the invited customer gets the gift")
	}
	var bonus struct {
		chat, bytes int64
		email       string
	}
	SetRefBonusHook(func(c, b int64, e string) { bonus.chat, bonus.bytes, bonus.email = c, b, e })
	defer SetRefBonusHook(nil)

	// First purchase: 20% off; the inviter has no service, so the bonus waits.
	id, err := s.CreateOrder(ctx, owner, "p20", TgBuyer(friend), "", "")
	if err != nil {
		t.Fatal(err)
	}
	o, _ := s.OrderByID(ctx, id)
	if o.Amount != 80000 || o.DiscountCode != InviteCode {
		t.Fatalf("first order %+v", o)
	}
	if _, err := s.FulfillOrder(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	if bonus.chat != inviter || bonus.bytes != 5<<30 || bonus.email != "" {
		t.Fatalf("bonus %+v", bonus)
	}
	// Second purchase: full price, and no second bonus.
	bonus.bytes = 0
	id2, _ := s.CreateOrder(ctx, owner, "p20", TgBuyer(friend), "", "")
	o2, _ := s.OrderByID(ctx, id2)
	if o2.Amount != 100000 || o2.DiscountCode != "" {
		t.Fatalf("second order %+v", o2)
	}
	if _, err := s.FulfillOrder(ctx, owner, id2); err != nil {
		t.Fatal(err)
	}
	if bonus.bytes != 0 {
		t.Fatal("one bonus per invited customer")
	}
	// The inviter buys: the waiting bonus lands on the new service.
	id3, _ := s.CreateOrder(ctx, owner, "p20", TgBuyer(inviter), "", "")
	email, err := s.FulfillOrder(ctx, owner, id3)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.ClientByEmail(ctx, email)
	if err != nil || c.VolumeBytes != 25<<30 {
		t.Fatalf("inviter's service %+v %v", c, err)
	}
	if bonus.chat != inviter || bonus.email != email {
		t.Fatalf("bonus applied %+v", bonus)
	}
	if s.applyPendingRefBonus(ctx, inviter, email) != 0 {
		t.Fatal("a bonus is used once")
	}
	list, err := s.Invitees(ctx, inviter, 10)
	if err != nil || len(list) != 1 || list[0].Bought != 2 || list[0].Bytes != 5<<30 {
		t.Fatalf("invitees %+v %v", list, err)
	}
	st, err := s.ReferralStatsAll(ctx, 10)
	if err != nil || st.Invited != 1 || st.Bought != 1 || st.Discounts != 20000 || st.Bytes != 5<<30 || len(st.Top) != 1 || st.Top[0].Chat != inviter {
		t.Fatalf("stats %+v %v", st, err)
	}
}
