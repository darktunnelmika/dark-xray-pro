package hubcore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestResaleWalletBillsAndLock(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(1_800_000_000, 0)
	s.SetClock(func() time.Time { return now })
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	_ = s.PutCard(ctx, CardInfo{Number: "6037000011112222"})
	if err := s.PutRepSale(ctx, RepSale{Packages: []RepPackage{{ID: "tb", GB: 1024, Price: 2_500_000, Inbounds: []string{"in1"}, CanDelete: true}, {ID: "half", GB: 500, Price: 1_300_000}}, WholesaleTB: 1_300_000}); err != nil {
		t.Fatal(err)
	}
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "ali", 100<<30, 0, "")
	_ = s.PutCardFor(ctx, "ali", CardInfo{Number: "6037000033334444"})
	if s.ResaleOn(ctx, "ali") {
		t.Fatal("resale is off until the owner allows it")
	}
	if err := s.PutResaleOwner(ctx, "ali", true, false, 0); err != nil {
		t.Fatal(err)
	}
	if !s.ResaleOn(ctx, "ali") {
		t.Fatal("allowed, priced and with a card: on")
	}
	tb := s.RepSale(ctx).Packages[0]
	if c := s.Wholesale(ctx, "ali", tb); c != 1_300_000 {
		t.Fatal("wholesale of 1 TB", c)
	}
	if c := s.Wholesale(ctx, "ali", s.RepSale(ctx).Packages[1]); c != 635_000 {
		t.Fatal("wholesale of 500 GB is per TB, rounded up to 1000", c)
	}
	if r := s.Retail(ctx, "ali", tb); r != 2_500_000 {
		t.Fatal("no own price: the owner's", r)
	}
	if err := s.PutResaleRep(ctx, "ali", true, map[string]int64{"tb": 1_000_000}); err == nil {
		t.Fatal("a price below wholesale is refused")
	}
	if err := s.PutResaleRep(ctx, "ali", true, map[string]int64{"tb": 2_200_000}); err != nil || s.Retail(ctx, "ali", tb) != 2_200_000 {
		t.Fatal("own price", err)
	}
	// A per-reseller wholesale price wins over the general one.
	_ = s.PutResaleOwner(ctx, "ali", true, false, 1_200_000)
	if c := s.Wholesale(ctx, "ali", tb); c != 1_200_000 {
		t.Fatal("own wholesale", c)
	}
	_ = s.PutResaleOwner(ctx, "ali", true, false, 0)

	order := func(chat int64, name string) RepOrder {
		o, err := s.CreateRepOrder(ctx, "ali", chat, "C", "", name, "tb")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.AttachRepReceipt(ctx, o.ID, chat, "f"); err != nil {
			t.Fatal(err)
		}
		return o
	}
	o := order(900, "cust-one")
	if o.Price != 2_200_000 || o.Cost != 1_300_000 || o.Seller != "ali" {
		t.Fatalf("order: %+v", o)
	}
	if _, err := s.ApproveRepOrder(ctx, "t", "", o.ID); err == nil {
		t.Fatal("the main bot cannot approve a reseller's order")
	}
	if _, err := s.ApproveRepOrder(ctx, "t", "ali", o.ID); !errors.Is(err, ErrFunds) {
		t.Fatal("empty wallet, no credit", err)
	}
	// Credit allowed: one bill, the panel under ali with the owner's credit.
	_ = s.PutResaleOwner(ctx, "ali", true, true, 0)
	res, err := s.ApproveRepOrder(ctx, "t", "ali", o.ID)
	if err != nil || res.Bill == 0 {
		t.Fatal("billed", res, err)
	}
	if !s.IsSellerOf(ctx, "ali", "cust-one") || s.RepCredit(ctx, "cust-one") != 1024<<30 || s.RepCredit(ctx, "ali") != 100<<30 {
		t.Fatal("the sold panel is ali's sub-seller with the package credit; ali's own credit is untouched")
	}
	// A second sale while a bill is open needs the wallet.
	o2 := order(902, "cust-two")
	if _, err := s.ApproveRepOrder(ctx, "t", "ali", o2.ID); !errors.Is(err, ErrFunds) {
		t.Fatal("one open bill at most", err)
	}
	// Overdue: ali cannot spend credit; its clients and sub-sellers are left alone.
	mk := func(email string) error {
		return s.RepPutClient(ctx, "ali", "ali", Client{Email: email, Enabled: true, VolumeBytes: 1 << 30, DurationSeconds: 86400, Inbounds: []string{"in1"}}, "volume")
	}
	if err := mk("c1"); err != nil {
		t.Fatal(err)
	}
	if len(s.BillNotices(ctx)) != 0 {
		t.Fatal("no reminder a day before")
	}
	now = now.Add(13 * time.Hour)
	if n := s.BillNotices(ctx); len(n) != 1 || n[0].Stage != 1 || !s.ClaimBillNotice(ctx, n[0].Bill.ID, 1) || s.ClaimBillNotice(ctx, n[0].Bill.ID, 1) {
		t.Fatal("12 hours left: one reminder, claimed once", n)
	}
	now = now.Add(12 * time.Hour)
	if !s.RepLocked(ctx, "ali") {
		t.Fatal("overdue")
	}
	if err := mk("c2"); !errors.Is(err, ErrPolicy) {
		t.Fatal("locked reseller made a client", err)
	}
	if l := s.OverdueToTell(ctx); len(l) != 1 || len(s.OverdueToTell(ctx)) != 0 {
		t.Fatal("the owner is told once")
	}
	if _, err := s.ApproveRepOrder(ctx, "t", "ali", o2.ID); err == nil {
		t.Fatal("locked reseller sold a panel")
	}
	// A top-up pays the bill and unlocks.
	w, err := s.CreateRepWalletOrder(ctx, 901, "Ali", "ali", 1_500_000)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.AttachRepReceipt(ctx, w.ID, 901, "f")
	wr, err := s.ApproveRepOrder(ctx, "owner", "", w.ID)
	if err != nil || wr.Wallet != 200_000 || len(wr.Settled) != 1 {
		t.Fatal("top-up settles the bill", wr, err)
	}
	if s.RepLocked(ctx, "ali") || mk("c3") != nil {
		t.Fatal("paid: unlocked")
	}
	// The owner can close a bill by hand.
	_, _ = s.RepWalletAdjust(ctx, "owner", "ali", -200_000, "")
	_, _ = s.ApproveRepOrder(ctx, "t", "ali", o2.ID)
	open, _ := s.RepBills(ctx, "ali", true, 5)
	if len(open) != 1 || s.CloseRepBill(ctx, "owner", open[0].ID, false) != nil || s.RepLocked(ctx, "ali") {
		t.Fatal("forgiven bill", open)
	}
}
