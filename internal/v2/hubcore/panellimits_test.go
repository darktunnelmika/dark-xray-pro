package hubcore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPanelCaps(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	if l := s.PanelLimitsOf(ctx); l.PerBuyer != 50 || l.PerSeller != 50 {
		t.Fatalf("defaults %+v", l)
	}
	if err := s.PutPanelLimits(ctx, PanelLimits{PerBuyer: 0, PerSeller: 5}); err == nil {
		t.Fatal("zero cap")
	}
	if err := s.PutPanelLimits(ctx, PanelLimits{PerBuyer: 2, PerSeller: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutCard(ctx, CardInfo{Number: "6037991234567890", Holder: "Owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Enabled: true, WholesaleTB: 1_024_000, Packages: []RepPackage{{ID: "p1", Name: "100", GB: 100, Price: 300000}}}); err != nil {
		t.Fatal(err)
	}
	buy := func(seller string, chat int64, name string) error {
		t.Helper()
		o, err := s.CreateRepOrder(ctx, seller, chat, "B", "", name, "p1")
		if err != nil {
			return err
		}
		if _, err := s.AttachRepReceipt(ctx, o.ID, chat, "photo"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ApproveRepOrder(ctx, "owner", seller, o.ID); err != nil {
			t.Fatal(err)
		}
		return nil
	}
	// One account: two panels at most; a top-up is always allowed.
	if err := buy("", 900, "first-shop"); err != nil {
		t.Fatal(err)
	}
	if err := buy("", 900, "second-shop"); err != nil {
		t.Fatal(err)
	}
	if n := s.BuyerPanels(ctx, 900); n != 2 {
		t.Fatalf("count %d", n)
	}
	err := buy("", 900, "third-shop")
	if !errors.Is(err, ErrPolicy) || !strings.Contains(err.Error(), "at most 2") {
		t.Fatalf("over the cap: %v", err)
	}
	if _, err := s.CreateRepOrder(ctx, "", 900, "B", "first-shop", "", "p1"); err != nil {
		t.Fatalf("top-up refused: %v", err)
	}
	// A special reseller: one panel of capacity, from its bot or by hand.
	if err := s.CreateRep(ctx, "owner", RepCreate{ID: "ali", Name: "Ali", Password: "ali-password-1", CreditGB: 100, TgOwners: []int64{600}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutResaleOwner(ctx, "ali", true, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPayCards(ctx, "ali", []PayCard{{Number: "6219861000000001", Holder: "Ali", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.RepWalletAdjust(ctx, "owner", "ali", 5_000_000, "test"); err != nil {
		t.Fatal(err)
	}
	if err := buy("ali", 950, "ali-sub-one"); err != nil {
		t.Fatal(err)
	}
	if n := s.SellerPanels(ctx, "ali"); n != 1 {
		t.Fatalf("sold %d", n)
	}
	if err := buy("ali", 951, "ali-sub-two"); err == nil || !strings.Contains(err.Error(), "capacity (1)") {
		t.Fatalf("over capacity: %v", err)
	}
	_, err = s.createRepOrder(ctx, "ali", 0, "x", "", "ali-hand", "p1", true)
	if err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("by hand over capacity: %v", err)
	}
	if msg, _ := miniFa(err); !strings.Contains(msg, "۱ پنل") {
		t.Fatalf("persian message %q", msg)
	}
}
