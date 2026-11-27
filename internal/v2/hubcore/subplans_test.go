package hubcore

import (
	"context"
	"strings"
	"testing"
)

func TestSubSellerOwnPlans(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Name: "Ali", Password: "ali-password-1", CreditGB: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSeller(ctx, "ali", Rep{ID: "kid", Name: "Kid", Enabled: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	// The reseller's own cheapest plan sets the default floor: 3000 per GB.
	if err := s.PutProduct(ctx, Principal{Role: "owner"}, Product{ID: "a60", Seller: "ali", Name: "60", Plan: "volume", GB: 60, Days: 30, Price: 180000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if f := s.SubPlansOf(ctx, "ali"); !f.Allow || f.MinGB != 3000 || f.Saved {
		t.Fatalf("default %+v", f)
	}
	kid := Principal{User: "tg:5", Role: "rep", RepID: "kid"}
	err := s.PutProduct(ctx, kid, Product{ID: "k10", Name: "10", Plan: "volume", GB: 10, Days: 30, Price: 20000, Status: "published"})
	if err == nil || !strings.Contains(err.Error(), "lowest price is 30000") {
		t.Fatalf("under the floor: %v", err)
	}
	if msg, _ := miniFa(err); !strings.Contains(msg, "۳۰۰۰۰") {
		t.Fatalf("persian: %s", msg)
	}
	if err := s.PutProduct(ctx, kid, Product{ID: "k10", Name: "10", Plan: "volume", GB: 10, Days: 30, Price: 35000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	// Not someone else's products.
	if err := s.PutProduct(ctx, kid, Product{ID: "k11", Seller: "ali", Name: "x", Plan: "volume", GB: 10, Days: 30, Price: 99000, Status: "published"}); err == nil {
		t.Fatal("a sub-seller stocked its reseller")
	}
	// The reseller turns it off: its own plans are closed again.
	if err := s.PutSubPlans(ctx, "ali", SubPlans{Allow: false, MinGB: 2500}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProduct(ctx, kid, Product{ID: "k12", Name: "12", Plan: "volume", GB: 10, Days: 30, Price: 50000, Status: "published"}); err == nil || !strings.Contains(err.Error(), "your reseller sets your plans") {
		t.Fatalf("closed: %v", err)
	}
	if f := s.SubPlansOf(ctx, "ali"); f.Allow || f.MinGB != 2500 || !f.Saved {
		t.Fatalf("saved %+v", f)
	}
	// Ready-made plans for a sub-seller come from its reseller.
	if err := s.PutSubPlans(ctx, "ali", SubPlans{Allow: true, MinGB: 4000}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSeller(ctx, "ali", Rep{ID: "kid2", Name: "Kid2", Enabled: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	m := miniMgr{P: Principal{User: "tg:6", Role: "rep", RepID: "kid2"}, Seller: "kid2", Role: "owner", Chat: 6}
	if n, err := s.SuggestPlans(ctx, m); err != nil || n != 1 {
		t.Fatalf("suggested %d %v", n, err)
	}
	var price int64
	_ = s.ro.QueryRowContext(ctx, `SELECT price FROM products WHERE seller='kid2'`).Scan(&price)
	if price != 240000 {
		t.Fatalf("raised to the floor: %d", price)
	}
}
