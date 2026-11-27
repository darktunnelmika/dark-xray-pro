package hubcore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSubSellerSellsOnlyDefinedPlans(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "ali", 500<<30, 0, "")
	if err := s.PutSeller(ctx, "ali", Rep{ID: "ali-sub", Name: "Sub", Enabled: true}, "alisub", "password-123456"); err != nil {
		t.Fatal(err)
	}
	if err := s.TransferCredit(ctx, "ali", "ali", "ali-sub", 100<<30, 0, ""); err != nil {
		t.Fatal(err)
	}
	ali := Principal{User: "ali", Role: "rep", RepID: "ali"}
	sub := Principal{User: "alisub", Role: "rep", RepID: "ali-sub"}
	// When its reseller closes its own plans (subplans.go), the sub-seller
	// makes no products, not even with the old grant.
	if err := s.PutSubPlans(ctx, "ali", SubPlans{Allow: false}); err != nil {
		t.Fatal(err)
	}
	_ = s.SetManageProducts(ctx, "ali-sub", true)
	if err := s.PutProduct(ctx, sub, Product{ID: "x1", Name: "mine", Plan: "volume", GB: 10, Days: 30, Status: "published"}); !errors.Is(err, ErrPolicy) {
		t.Fatal("sub-seller made a product", err)
	}
	// Its reseller defines one for it; another reseller cannot.
	if err := s.PutProduct(ctx, ali, Product{ID: "s20", Seller: "ali-sub", Name: "20GB", Plan: "volume", GB: 20, Days: 30, MaxIPs: 2, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	_ = s.PutRep(ctx, Rep{ID: "bob", Name: "Bob", Enabled: true})
	if err := s.PutProduct(ctx, Principal{Role: "rep", RepID: "bob"}, Product{ID: "s21", Seller: "ali-sub", Name: "x", Plan: "volume", GB: 5, Days: 30}); err == nil {
		t.Fatal("bob stocked ali's sub-seller")
	}
	// The reseller sees and edits it; the sub-seller only sees it.
	if p, err := s.ProductByID(ctx, ali, "s20"); err != nil || p.Seller != "ali-sub" {
		t.Fatal("reseller sees it", err)
	}
	if list, _ := s.Products(ctx, sub, true); len(list) != 1 {
		t.Fatal("sub-seller sees its product", list)
	}
	// Clients: only the defined plan, from the sub-seller's own credit.
	mk := func(email string, gb int64, days int64, ips int) error {
		return s.RepPutClient(ctx, "alisub", "ali-sub", Client{Email: email, Enabled: true, VolumeBytes: gb << 30, DurationSeconds: days * 86400, MaxIPs: ips, Inbounds: []string{"in1"}}, "volume")
	}
	if err := mk("c-bad", 50, 30, 2); !errors.Is(err, ErrPolicy) {
		t.Fatal("custom plan accepted", err)
	}
	if err := mk("c-ok", 20, 30, 2); err != nil {
		t.Fatal(err)
	}
	// No raising volume by editing; renew only with the defined plan.
	c, _ := s.ClientByEmail(ctx, "c-ok")
	cl := c.Client
	cl.VolumeBytes = 40 << 30
	if err := s.RepPutClient(ctx, "alisub", "ali-sub", cl, "volume"); !errors.Is(err, ErrPolicy) {
		t.Fatal("volume raised by edit", err)
	}
	if _, err := s.RenewClient(ctx, sub, "c-ok", "volume", 50, 30); !errors.Is(err, ErrPolicy) {
		t.Fatal("custom renew accepted", err)
	}
	if _, err := s.RenewClient(ctx, sub, "c-ok", "volume", 20, 30); err != nil {
		t.Fatal(err)
	}
	// The reseller itself still sells freely.
	if err := s.RepPutClient(ctx, "ali", "ali", Client{Email: "a-1", Enabled: true, VolumeBytes: 33 << 30, DurationSeconds: 45 * 86400, Inbounds: []string{"in1"}}, "volume"); err != nil {
		t.Fatal(err)
	}
}
