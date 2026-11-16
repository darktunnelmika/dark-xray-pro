package hubcore

import (
	"context"
	"testing"
)

func TestShopCategories(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	rep := Principal{User: "ali", Role: "rep", RepID: "ali"}
	for i, c := range []string{"VIP", "Eco", "VIP"} {
		p := Product{ID: "p" + string(rune('a'+i)), Name: "x", Plan: "volume", GB: 10, Days: 30, Price: 1000, Status: "published", Category: c, Inbounds: []string{"de-vless"}}
		if err := s.PutProduct(ctx, rep, p); err != nil {
			t.Fatal("new resellers may manage products:", err)
		}
	}
	got := s.ShopCategories(ctx, "ali")
	if len(got) != 2 || got[0].Name != "VIP" || got[0].Count != 2 {
		t.Fatalf("categories from products: %+v", got)
	}
	// Reorder, rename VIP to Gold with an emoji, drop Eco.
	if err := s.PutShopCategories(ctx, rep, "", []ShopCategory{{Name: "Gold", Emoji: "💎", Old: "VIP"}}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.Products(ctx, rep, false)
	for _, p := range list {
		if (p.ID == "pa" || p.ID == "pc") && p.Category != "Gold" || p.ID == "pb" && p.Category != "" {
			t.Fatalf("rename and drop reach the products: %+v", p)
		}
	}
	got = s.ShopCategories(ctx, "ali")
	if len(got) != 1 || got[0].Emoji != "💎" {
		t.Fatalf("%+v", got)
	}
	// A reseller only touches their own shop.
	if err := s.PutShopCategories(ctx, rep, "", []ShopCategory{{Name: "A"}, {Name: "A"}}); err == nil {
		t.Fatal("duplicate names refused")
	}
}
