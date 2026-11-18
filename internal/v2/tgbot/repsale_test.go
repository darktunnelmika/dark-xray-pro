package tgbot

import (
	"context"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func photoMsg(chat int64) Update {
	u := msg(chat, "")
	u.Message.Photo = append(u.Message.Photo, struct {
		FileID string `json:"file_id"`
	}{FileID: "receipt-file"})
	return u
}

func TestSellingResellerPanels(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://panel.example:2087", Logf: t.Logf}
	const cust = int64(6100)
	menuHas := func(chat int64, label string) bool {
		b.Handle(ctx, msg(chat, "/start"))
		for _, m := range api.take() {
			if hasButton(m.kb, label) {
				return true
			}
		}
		return false
	}

	// Off by default; turning it on needs a package and the payment card.
	if menuHas(cust, "پنل نمایندگی") {
		t.Fatal("the sale is off by default")
	}
	b.Handle(ctx, cb(ownerID, "a:settings"))
	if m := find(t, api.take(), ownerID, "تنظیمات ربات"); !hasButton(m.kb, "فروش پنل نمایندگی") {
		t.Fatal("bot settings must lead to the panel sale")
	}
	b.Handle(ctx, cb(ownerID, "P:cfg:on"))
	find(t, api.take(), ownerID, "اول یک بسته")
	b.Handle(ctx, cb(ownerID, "P:cfg:add"))
	api.take()
	b.Handle(ctx, msg(ownerID, "nonsense"))
	find(t, api.take(), ownerID, "دو عدد")
	b.Handle(ctx, msg(ownerID, "200 450,000"))
	find(t, api.take(), ownerID, "۲۰۰ گیگ")
	b.Handle(ctx, cb(ownerID, "P:cfg:on"))
	find(t, api.take(), ownerID, "شماره کارت")
	_ = s.PutCard(ctx, hubcore.CardInfo{Number: "6037000011112222", Holder: "Owner"})
	b.Handle(ctx, cb(ownerID, "P:cfg:on"))
	find(t, api.take(), ownerID, "🟢 روشن")
	pkg := s.RepSale(ctx).Packages[0].ID

	// Nobody but the owner reaches the settings or the approval.
	b.Handle(ctx, cb(cust, "P:cfg:off"))
	api.take()
	if !s.RepSale(ctx).Enabled {
		t.Fatal("a customer must not change the sale")
	}

	// A customer buys a panel: package, username, receipt.
	if !menuHas(cust, "خرید پنل نمایندگی") {
		t.Fatal("the customer's menu must offer the panel")
	}
	b.Handle(ctx, cb(cust, "P:buy"))
	find(t, api.take(), cust, "یک بسته را انتخاب")
	b.Handle(ctx, cb(cust, "P:pk:"+pkg))
	find(t, api.take(), cust, "نام کاربری")
	b.Handle(ctx, msg(cust, "A B"))
	find(t, api.take(), cust, "۳ تا ۲۴ حرف")
	b.Handle(ctx, msg(cust, "Reza-Shop"))
	find(t, api.take(), cust, "6037")
	b.Handle(ctx, msg(cust, "paid!"))
	find(t, api.take(), cust, "عکس رسید")
	b.Handle(ctx, photoMsg(cust))
	out := api.take()
	find(t, out, cust, "دریافت شد")
	card := find(t, out, ownerID, "reza-shop")
	if !card.photo || !hasButton(card.kb, "تأیید") {
		t.Fatalf("the owner gets the receipt with buttons: %+v", card)
	}
	if reps, _ := s.Reps(ctx); len(reps) != 0 {
		t.Fatal("nothing is created before the owner approves")
	}
	// The customer cannot approve their own order.
	b.Handle(ctx, cb(cust, "P:ok:1"))
	api.take()
	if reps, _ := s.Reps(ctx); len(reps) != 0 {
		t.Fatal("only the owner approves")
	}
	b.Handle(ctx, cb(ownerID, "P:ok:1"))
	out = api.take()
	find(t, out, ownerID, "تحویل شد")
	ready := find(t, out, cust, "پنل نمایندگی شما آماده است")
	pass := regexp.MustCompile(`رمز موقت: <code>([0-9a-f]{12})</code>`).FindStringSubmatch(ready.text)
	if pass == nil {
		t.Fatalf("the buyer gets a one-time password: %s", ready.text)
	}
	if _, err := s.Login(ctx, "reza-shop", pass[1]); err != nil {
		t.Fatalf("the new login must work: %v", err)
	}
	if s.TgRep(ctx, cust) != "reza-shop" || s.RepCredit(ctx, "reza-shop") != 200<<30 {
		t.Fatalf("panel, Telegram link and credit: %q %d", s.TgRep(ctx, cust), s.RepCredit(ctx, "reza-shop"))
	}
	// A second tap delivers nothing more.
	b.Handle(ctx, cb(ownerID, "P:ok:1"))
	find(t, api.take(), ownerID, "قبلاً بررسی")
	if s.RepCredit(ctx, "reza-shop") != 200<<30 {
		t.Fatal("an order is delivered once")
	}

	// The same person is now a reseller: the button charges the panel.
	b.Handle(ctx, cb(cust, "P:buy"))
	find(t, api.take(), cust, "اعتبار فعلی")
	b.Handle(ctx, cb(cust, "P:pk:"+pkg))
	find(t, api.take(), cust, "شارژ پنل نمایندگی #۲")
	b.Handle(ctx, photoMsg(cust))
	api.take()
	b.Handle(ctx, cb(ownerID, "P:no:2"))
	out = api.take()
	find(t, out, cust, "تأیید نشد")
	if s.RepCredit(ctx, "reza-shop") != 200<<30 {
		t.Fatal("a rejected top-up adds nothing")
	}
	b.Handle(ctx, cb(cust, "P:pk:"+pkg))
	api.take()
	b.Handle(ctx, photoMsg(cust))
	api.take()
	b.Handle(ctx, cb(ownerID, "P:ok:3"))
	find(t, api.take(), cust, "شارژ شد")
	if s.RepCredit(ctx, "reza-shop") != 400<<30 {
		t.Fatalf("top-up: %d", s.RepCredit(ctx, "reza-shop"))
	}
	// A taken username is refused for the next buyer.
	b.Handle(ctx, cb(cust+1, "P:pk:"+pkg))
	api.take()
	b.Handle(ctx, msg(cust+1, "reza-shop"))
	find(t, api.take(), cust+1, "گرفته شده")
}
