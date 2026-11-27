package tgbot

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func hasBtn(kb *Keyboard, data string) bool {
	if kb == nil {
		return false
	}
	for _, r := range kb.Rows {
		for _, b := range r {
			if b.Data == data {
				return true
			}
		}
	}
	return false
}

func TestCustomPlanInBot(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "n1", "N"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true); err != nil {
		t.Fatal(err)
	}
	owner := hubcore.Principal{User: "o", Role: "owner"}
	if err := s.PutProduct(ctx, owner, hubcore.Product{ID: "p30", Name: "30GB", Plan: "volume", GB: 30, Days: 30, Inbounds: []string{"in1"}, Price: 120000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://sub.example", Now: time.Now, Logf: t.Logf}

	// Off: no button.
	b.Handle(ctx, cb(buyerID, "shop"))
	if m := find(t, api.take(), buyerID, "بسته‌ها"); hasBtn(m.kb, "cx:") {
		t.Fatal("custom button while off")
	}
	if err := s.PutCustomPlanFor(ctx, "", hubcore.CustomPlan{Enabled: true, PriceGB: 3000, Price10: 10000, MinGB: 10, MaxGB: 100, MinDays: 10, MaxDays: 60}); err != nil {
		t.Fatal(err)
	}
	b.Handle(ctx, cb(buyerID, "shop"))
	if m := find(t, api.take(), buyerID, "بسته‌ها"); !hasBtn(m.kb, "cx:") {
		t.Fatalf("no custom button: %+v", m.kb)
	}
	b.Handle(ctx, cb(buyerID, "cx:"))
	m := find(t, api.take(), buyerID, "قدم ۱")
	if !hasBtn(m.kb, "cxg:10:") || !hasBtn(m.kb, "cxg:100:") || !hasBtn(m.kb, "cxg:50:") || hasBtn(m.kb, "cxg:150:") {
		t.Fatalf("volume steps: %+v", m.kb)
	}
	b.Handle(ctx, cb(buyerID, "cxg:50:"))
	m = find(t, api.take(), buyerID, "قدم ۲")
	if !hasBtn(m.kb, "cxd:50:30:") || hasBtn(m.kb, "cxd:50:90:") {
		t.Fatalf("day steps: %+v", m.kb)
	}
	// 50 GB for 30 days: 50×3000 + 3×10000 = 180,000.
	b.Handle(ctx, cb(buyerID, "cxd:50:30:"))
	m = find(t, api.take(), buyerID, "۱۸۰٬۰۰۰")
	if !hasBtn(m.kb, "buy:cx-50g-30d") || !hasBtn(m.kb, "cpn:cx-50g-30d") {
		t.Fatalf("card buttons: %+v", m.kb)
	}
	// The hidden product still opens from its id (back from the coupon screen).
	b.Handle(ctx, cb(buyerID, "prod:cx-50g-30d"))
	find(t, api.take(), buyerID, "۱۸۰٬۰۰۰")

	// Typed numbers, Persian digits too; out of range is refused.
	b.Handle(ctx, cb(buyerID, "cxt:"))
	find(t, api.take(), buyerID, "با فاصله")
	b.Handle(ctx, msg(buyerID, "5 30"))
	find(t, api.take(), buyerID, "مجاز نیست")
	b.Handle(ctx, cb(buyerID, "cxt:"))
	api.take()
	b.Handle(ctx, msg(buyerID, "۲۰ ۱۵"))
	m = find(t, api.take(), buyerID, "۸۰٬۰۰۰") // 20×3000 + 2×10000
	if !hasBtn(m.kb, "buy:cx-20g-15d") {
		t.Fatalf("typed card: %+v", m.kb)
	}

	b.Handle(ctx, cb(buyerID, "buy:cx-50g-30d"))
	find(t, api.take(), buyerID, "#1")
	o, err := s.OrderByID(ctx, 1)
	if err != nil || o.ProductID != "cx-50g-30d" || o.Amount != 180000 {
		t.Fatalf("order %+v %v", o, err)
	}
	// The hidden products never show in the list.
	b.Handle(ctx, cb(buyerID, "shop"))
	if m := find(t, api.take(), buyerID, "بسته‌ها"); hasBtn(m.kb, "prod:cx-50g-30d") {
		t.Fatal("custom product listed")
	}
	// Off again: the card is gone.
	_ = s.PutCustomPlanFor(ctx, "", hubcore.CustomPlan{PriceGB: 3000, MinGB: 10, MaxGB: 100, MinDays: 10, MaxDays: 60})
	b.Handle(ctx, cb(buyerID, "prod:cx-50g-30d"))
	find(t, api.take(), buyerID, "دیگر فروخته نمی‌شود")
}
