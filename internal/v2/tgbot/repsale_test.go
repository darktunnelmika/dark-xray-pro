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
	// The new package opens on its settings: strict by default, set here once.
	if m := find(t, api.take(), ownerID, "فقط فروش حجمی"); !hasButton(m.kb, "حداکثر کاربر") {
		t.Fatal("a new package shows its panel settings")
	}
	pkg := s.RepSale(ctx).Packages[0].ID
	b.Handle(ctx, cb(ownerID, "P:cfg:mc:"+pkg))
	api.take()
	b.Handle(ctx, msg(ownerID, "50"))
	find(t, api.take(), ownerID, "حداکثر کاربر: ۵۰")
	b.Handle(ctx, cb(ownerID, "P:cfg:ib:"+pkg))
	find(t, api.take(), ownerID, "اینباندهایی")
	b.Handle(ctx, cb(ownerID, "P:cfg:it:"+pkg+":0"))
	api.take()
	if got := s.RepSale(ctx).Packages[0]; len(got.Inbounds) != 1 || got.Inbounds[0] != "in1" || got.AllowUnlimited || !got.CanDelete {
		t.Fatalf("package settings: %+v", got)
	}
	b.Handle(ctx, cb(ownerID, "P:cfg:on"))
	find(t, api.take(), ownerID, "شماره کارت")
	_ = s.PutCard(ctx, hubcore.CardInfo{Number: "6037000011112222", Holder: "Owner"})
	b.Handle(ctx, cb(ownerID, "P:cfg:on"))
	find(t, api.take(), ownerID, "🟢 روشن")

	// Nobody but the owner reaches the settings or the approval.
	b.Handle(ctx, cb(cust, "P:cfg:off"))
	api.take()
	if !s.RepSale(ctx).Enabled {
		t.Fatal("a customer must not change the sale")
	}

	// A customer buys a panel: package, username, receipt.
	if !menuHas(cust, "پنل نمایندگی") {
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
	// The panel starts with the package's settings: nothing left to set by hand.
	if pol, err := s.RepPolicyOf(ctx, "reza-shop"); err != nil || pol.AllowUnlimited || pol.MaxClients != 50 || !pol.CanDelete || len(pol.Inbounds) != 1 || pol.Inbounds[0] != "in1" {
		t.Fatalf("the new panel's policy: %+v %v", pol, err)
	}
	// Later the owner loosens the package; a top-up must not loosen a panel.
	b.Handle(ctx, cb(ownerID, "P:cfg:tu:"+pkg))
	find(t, api.take(), ownerID, "فروش حجمی و نامحدود")
	b.Handle(ctx, cb(ownerID, "P:cfg:un:"+pkg))
	api.take()
	b.Handle(ctx, msg(ownerID, "3"))
	find(t, api.take(), ownerID, "۳ واحد نامحدود")
	// A second tap delivers nothing more.
	b.Handle(ctx, cb(ownerID, "P:ok:1"))
	find(t, api.take(), ownerID, "قبلاً بررسی")
	if s.RepCredit(ctx, "reza-shop") != 200<<30 {
		t.Fatal("an order is delivered once")
	}

	// The same person is now a reseller: the button lists the panel to
	// charge, beside buying another one.
	b.Handle(ctx, cb(cust, "P:buy"))
	find(t, api.take(), cust, "پنل‌های نمایندگی شما")
	b.Handle(ctx, cb(cust, "P:tp:reza-shop"))
	find(t, api.take(), cust, "اعتبار فعلی")
	b.Handle(ctx, cb(cust, "P:tk:reza-shop:"+pkg))
	find(t, api.take(), cust, "شارژ پنل نمایندگی #۲")
	b.Handle(ctx, photoMsg(cust))
	api.take()
	b.Handle(ctx, cb(ownerID, "P:no:2"))
	out = api.take()
	find(t, out, cust, "تأیید نشد")
	if s.RepCredit(ctx, "reza-shop") != 200<<30 {
		t.Fatal("a rejected top-up adds nothing")
	}
	b.Handle(ctx, cb(cust, "P:tk:reza-shop:"+pkg))
	api.take()
	b.Handle(ctx, photoMsg(cust))
	api.take()
	b.Handle(ctx, cb(ownerID, "P:ok:3"))
	find(t, api.take(), cust, "شارژ شد")
	if s.RepCredit(ctx, "reza-shop") != 400<<30 {
		t.Fatalf("top-up: %d", s.RepCredit(ctx, "reza-shop"))
	}
	if pol, _ := s.RepPolicyOf(ctx, "reza-shop"); pol.AllowUnlimited || pol.MaxClients != 50 {
		t.Fatalf("a top-up changes only the credit: %+v", pol)
	}
	for _, r := range func() []hubcore.Rep { l, _ := s.Reps(ctx); return l }() {
		if r.ID == "reza-shop" && r.UnlimitedUnits != 0 {
			t.Fatalf("a volume-only panel gets no unlimited units: %d", r.UnlimitedUnits)
		}
	}
	// Nobody tops up a panel that is not theirs.
	b.Handle(ctx, cb(cust+1, "P:tk:reza-shop:"+pkg))
	if out := api.take(); len(out) != 0 {
		t.Fatalf("a stranger's top-up must do nothing: %v", out)
	}
	// A second panel for the same account: the bot stays on the first.
	b.Handle(ctx, cb(cust, "P:new"))
	api.take()
	b.Handle(ctx, cb(cust, "P:pk:"+pkg))
	api.take()
	b.Handle(ctx, msg(cust, "reza-two"))
	find(t, api.take(), cust, "خرید پنل نمایندگی #۴")
	b.Handle(ctx, photoMsg(cust))
	api.take()
	b.Handle(ctx, cb(ownerID, "P:ok:4"))
	api.take()
	if s.RepCredit(ctx, "reza-two") != 200<<30 || s.TgRep(ctx, cust) != "reza-shop" {
		t.Fatalf("second panel: credit %d, bot on %q", s.RepCredit(ctx, "reza-two"), s.TgRep(ctx, cust))
	}
	if got := s.ChatPanels(ctx, cust); len(got) != 2 || got[0] != "reza-shop" || got[1] != "reza-two" {
		t.Fatalf("panels %v", got)
	}
	b.Handle(ctx, cb(cust, "P:buy"))
	out = api.take()
	find(t, out, cust, "reza-two")
	// A taken username is refused for the next buyer.
	b.Handle(ctx, cb(cust+1, "P:pk:"+pkg))
	api.take()
	b.Handle(ctx, msg(cust+1, "reza-shop"))
	find(t, api.take(), cust+1, "گرفته شده")
}
