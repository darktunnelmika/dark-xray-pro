package tgbot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// payAPI is the part of the Bot API used for Telegram Stars payments
// (HTTPAPI implements it; test fakes may not).
type payAPI interface {
	SendInvoice(ctx context.Context, chat int64, title, desc, payload string, stars int64) error
	AnswerPreCheckout(ctx context.Context, id string, ok bool, msg string) error
	RefundStars(ctx context.Context, user int64, chargeID string) error
}

// buyButtons lists how a product can be bought: Stars (paid and delivered
// at once), wallet credit, or a manual order the owner confirms.
func (b *Bot) buyButtons(ctx context.Context, chat int64, p hubcore.Product, renew string) [][]Button {
	if d, ok := b.Store.HeldDiscount(ctx, hubcore.TgBuyer(chat), p.ID); ok && d.Percent > 0 && p.PriceStars > 0 {
		p.PriceStars = max(1, p.PriceStars*(100-d.Percent)/100)
	}
	suffix := ""
	label := "خرید "
	if renew != "" {
		suffix, label = ":"+renew, "تمدید با "
	}
	var rows [][]Button
	if p.Price > 0 && chat != 0 && b.Store.TomanBalance(ctx, b.Seller, chat) >= p.Price {
		rows = append(rows, []Button{{Text: "💰 " + label + p.Name + " از کیف پول", Data: "tpay:" + p.ID + suffix}})
	}
	if p.PriceStars > 0 && b.Seller == "" {
		if _, ok := b.API.(payAPI); ok {
			rows = append(rows, []Button{{Text: fmt.Sprintf("⭐ %s%s — %d استارز", label, p.Name, p.PriceStars), Data: "pay:" + p.ID + suffix}})
		}
		if chat != 0 && b.Store.WalletBalance(ctx, chat) >= p.PriceStars {
			rows = append(rows, []Button{{Text: "👛 " + label + p.Name + " از کیف پول", Data: "wpay:" + p.ID + suffix}})
		}
	}
	if p.Price > 0 || p.PriceStars == 0 || b.Seller != "" {
		data := "buy:" + p.ID
		if renew != "" {
			data = "renew:" + p.ID + ":" + renew
		}
		rows = append(rows, []Button{{Text: label + p.Name, Data: data}})
	}
	return rows
}

func (b *Bot) payStars(ctx context.Context, chat int64, productID, renew string) {
	api, ok := b.API.(payAPI)
	if !ok {
		return
	}
	id, stars, title, err := b.Store.StarsCheckout(ctx, chat, productID, renew)
	if err != nil {
		b.send(ctx, chat, "این محصول الان با استارز فروخته نمی‌شود.", mainMenu)
		return
	}
	desc := "پرداخت با استارز تلگرام؛ سرویس بلافاصله بعد از پرداخت فعال می‌شود."
	if renew != "" {
		desc = "تمدید " + renew + " — بلافاصله بعد از پرداخت اعمال می‌شود."
	}
	if err := api.SendInvoice(ctx, chat, title, desc, "order:"+strconv.FormatInt(id, 10), stars); err != nil {
		b.logf("tgbot invoice: %v", err)
		b.send(ctx, chat, "ساخت فاکتور ممکن نشد؛ کمی بعد دوباره امتحان کنید.", mainMenu)
	}
}

func orderOfPayload(p string) int64 {
	n, _ := strconv.ParseInt(strings.TrimPrefix(p, "order:"), 10, 64)
	if !strings.HasPrefix(p, "order:") {
		return 0
	}
	return n
}

func (b *Bot) onPreCheckout(ctx context.Context, q *PreCheckoutQuery) {
	api, ok := b.API.(payAPI)
	if !ok {
		return
	}
	err := errors.New("invalid invoice")
	if q.Currency == "XTR" {
		if id := orderOfPayload(q.Payload); id > 0 {
			err = b.Store.StarsPrecheck(ctx, id, q.From.ID, q.Total)
		}
	}
	if err != nil {
		_ = api.AnswerPreCheckout(ctx, q.ID, false, "این فاکتور دیگر معتبر نیست؛ دوباره از فروشگاه خرید کنید.")
		return
	}
	_ = api.AnswerPreCheckout(ctx, q.ID, true, "")
}

func (b *Bot) onPaid(ctx context.Context, chat int64, name string, p *SuccessfulPayment) {
	id := orderOfPayload(p.Payload)
	email, err := b.Store.StarsPaid(ctx, id, chat, p.Total, p.ChargeID)
	if err != nil {
		// Charged but not delivered: tell the owners with a refund button.
		b.logf("tgbot stars delivery %d: %v", id, err)
		b.send(ctx, chat, "✅ پرداخت انجام شد، ولی فعال‌سازی با خطا روبه‌رو شد. پشتیبانی همین الان پیگیری می‌کند.", nil)
		b.notify(ctx, "orders", fmt.Sprintf("⚠️ پرداخت %d⭐ سفارش #%d از %s انجام شد ولی تحویل نشد: %s", p.Total, id, esc(name), esc(err.Error())),
			&Keyboard{Rows: [][]Button{{{Text: "↩️ بازپرداخت استارز", Data: "srf:" + strconv.FormatInt(id, 10)}}}})
		return
	}
	b.announceOrders(ctx) // sends the link and QR right away
	b.notify(ctx, "orders", fmt.Sprintf("⭐ فروش با استارز: سفارش #%d، %d⭐ — %s ← <code>%s</code>", id, p.Total, esc(name), esc(email)),
		&Keyboard{Rows: [][]Button{{{Text: "↩️ بازپرداخت", Data: "srf:" + strconv.FormatInt(id, 10)}}}})
}

func (b *Bot) refundStars(ctx context.Context, owner, id int64) {
	api, ok := b.API.(payAPI)
	if !ok {
		return
	}
	chat, charge, err := b.Store.StarsRefundInfo(ctx, id)
	if err != nil {
		b.send(ctx, owner, "بازپرداخت ممکن نیست: "+esc(err.Error()), nil)
		return
	}
	if err := api.RefundStars(ctx, chat, charge); err != nil {
		b.send(ctx, owner, "تلگرام بازپرداخت را رد کرد: "+esc(err.Error()), nil)
		return
	}
	_ = b.Store.StarsRefunded(ctx, id)
	b.send(ctx, owner, fmt.Sprintf("↩️ استارزهای سفارش #%d برگردانده شد.", id), nil)
	b.send(ctx, chat, fmt.Sprintf("↩️ مبلغ سفارش #%d به حساب استارز شما برگشت.", id), nil)
}

func (b *Bot) payWallet(ctx context.Context, chat int64, productID, renew string) {
	email, left, err := b.Store.WalletPay(ctx, chat, productID, renew)
	if err != nil {
		b.send(ctx, chat, fmt.Sprintf("پرداخت از کیف پول ممکن نشد (موجودی: %d⭐).", left), mainMenu)
		return
	}
	b.announceOrders(ctx)
	b.celebrate(ctx, chat, fmt.Sprintf("✅ از کیف پول پرداخت شد: <code>%s</code>\nموجودی باقی‌مانده: %d⭐", esc(email), left), nil, EffectLike)
}

func (b *Bot) trial(ctx context.Context, chat int64) {
	email, err := b.Store.ClaimTrial(ctx, chat)
	switch {
	case errors.Is(err, hubcore.ErrTrialUsed):
		b.send(ctx, chat, "تست رایگان قبلاً برای این حساب فعال شده است. برای ادامه یک پلن بخرید.", mainMenu)
		return
	case err != nil:
		b.send(ctx, chat, "تست رایگان فعلاً در دسترس نیست.", mainMenu)
		return
	}
	p := b.Store.TrialPolicy(ctx)
	size := "حجم نامحدود"
	if p.GB > 0 {
		size = fmt.Sprintf("%d گیگ", p.GB)
	}
	b.send(ctx, chat, fmt.Sprintf("🎁 تست رایگان شما فعال شد: %s برای %d ساعت.", size, p.Hours), nil)
	b.sendSub(ctx, chat, email)
}

func (b *Bot) invite(ctx context.Context, chat int64) {
	sum, err := b.Store.ReferralSummary(ctx, chat)
	if err != nil {
		return
	}
	user, _ := b.API.Me(ctx)
	link := fmt.Sprintf("https://t.me/%s?start=ref_%s", user, sum.Code)
	pol := b.Store.ReferralPolicy(ctx)
	var how []string
	if pol.Percent > 0 {
		how = append(how, fmt.Sprintf("%d٪ مبلغ استارز هر خرید", pol.Percent))
	}
	if pol.Fixed > 0 {
		how = append(how, fmt.Sprintf("%d⭐ برای هر خرید", pol.Fixed))
	}
	first := ""
	if pol.FirstOnly {
		first = " (فقط اولین خرید هر دوست)"
	}
	b.send(ctx, chat, fmt.Sprintf("👥 <b>دعوت دوستان</b>\n\nهر دوستی که با لینک شما بیاید و خرید کند، %s%s به کیف پول شما اضافه می‌شود و می‌توانید با آن سرویس بخرید.\n\nلینک شما:\n<code>%s</code>\n\nدعوت‌شده‌ها: %d — کل اعتبار گرفته‌شده: %d⭐ — موجودی: %d⭐",
		strings.Join(how, " + "), first, esc(link), sum.Invited, sum.Earned, sum.Balance), nil)
}

func (b *Bot) wallet(ctx context.Context, chat int64) {
	toman := b.Store.TomanBalance(ctx, b.Seller, chat)
	if b.Seller != "" || b.cardOn(ctx) || toman > 0 {
		hist, _ := b.Store.TomanHistory(ctx, b.Seller, chat, 8)
		kinds := map[string]string{"topup": "شارژ", "spend": "خرید", "gift": "هدیه", "correction": "اصلاح", "refund": "برگشت", "import": "انتقال از پنل قبلی"}
		var lines []string
		for _, e := range hist {
			sign := "+"
			if e.Delta < 0 {
				sign = "−"
			}
			lines = append(lines, fmt.Sprintf("%s%s تومان — %s", sign, faMoney(abs64(e.Delta)), kinds[e.Kind]))
		}
		text := fmt.Sprintf("💰 <b>کیف پول</b>\nموجودی: <b>%s تومان</b>", faMoney(toman))
		if b.Seller == "" {
			if st := b.Store.WalletBalance(ctx, chat); st > 0 {
				text += fmt.Sprintf("\nاعتبار استارز: %d⭐", st)
			}
		}
		if len(lines) > 0 {
			text += "\n\n" + strings.Join(lines, "\n")
		}
		text += "\n\nبا موجودی کیف پول سرویس بخرید یا تمدید کنید؛ تمدید خودکار هم از همین موجودی انجام می‌شود."
		kb := &Keyboard{Rows: [][]Button{row(cbBtn("🛒 خرید سرویس", "shop"))}}
		if b.cardOn(ctx) {
			kb.Rows = append([][]Button{row(cbBtn("➕ شارژ کیف پول", "topup"))}, kb.Rows...)
		}
		b.send(ctx, chat, text, kb)
		return
	}
	bal := b.Store.WalletBalance(ctx, chat)
	hist, _ := b.Store.WalletHistory(ctx, chat, 8)
	kinds := map[string]string{"reward": "پاداش دعوت", "spend": "خرید", "gift": "هدیه", "correction": "اصلاح", "refund": "برگشت"}
	var lines []string
	for _, e := range hist {
		lines = append(lines, fmt.Sprintf("%+d⭐ — %s", e.Delta, kinds[e.Kind]))
	}
	text := fmt.Sprintf("👛 <b>کیف پول</b>\nموجودی: <b>%d⭐</b>", bal)
	if len(lines) > 0 {
		text += "\n\n" + strings.Join(lines, "\n")
	}
	text += "\n\nبا موجودی کیف پول می‌توانید از «خرید سرویس» پلن‌های استارزی را بخرید."
	b.send(ctx, chat, text, &Keyboard{Rows: [][]Button{{{Text: "🛒 خرید سرویس", Data: "shop"}}}})
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
