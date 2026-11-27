package hubcore

import (
	"context"
	"testing"
)

func TestClaimOrderAnnounceOnce(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	owner := Principal{User: "owner", Role: "owner"}
	if err := s.PutProduct(ctx, owner, Product{ID: "p1", Name: "1", Plan: "volume", GB: 1, Days: 30, Price: 1000, Status: "published", Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateOrder(ctx, owner, "p1", TgBuyer(5), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !s.ClaimOrderAnnounce(ctx, id, false) || s.ClaimOrderAnnounce(ctx, id, false) {
		t.Fatal("the admins' announcement is claimed exactly once")
	}
	if !s.ClaimOrderAnnounce(ctx, id, true) || s.ClaimOrderAnnounce(ctx, id, true) {
		t.Fatal("the buyer's announcement is claimed exactly once")
	}
}
