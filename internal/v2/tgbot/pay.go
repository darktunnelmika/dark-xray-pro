package tgbot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Card-to-card payment with a receipt, and discount codes.
//
// With a card set in the panel, a bot order becomes an invoice: the amount,
// the card number to copy, and "send the receipt photo here". The photo goes
// to the owners with Approve / Refuse; approving delivers at once. A held
// discount code lowers the price of the next order.

func (b *Bot) cardOn(ctx context.Context) bool { return b.Store.CardFor(ctx, b.Seller).Number != "" }

func groupCard(n string) string {
	if len(n) == 16 {
		return n[0:4] + "-" + n[4:8] + "-" + n[8:12] + "-" + n[12:16]
	}
	return n
}

// invoice shows how to pay one open order.
func (b *Bot) invoice(ctx context.Context, chat, id int64) {
	o, err := b.Store.OrderByID(ctx, id)
	if err != nil {
		return
	}
	c := b.Store.CardFor(ctx, b.Seller)
	var sb strings.Builder
	fmt.Fprintf(&sb, "🧾 <b>فاکتور #%s</b>\n%s\n📦 %s\n", faInt(o.ID), line, esc(o.ProductName))
	if o.Renew {
		fmt.Fprintf(&sb, "♻️ تمدید <b>%s</b>\n", esc(o.ClientEmail))
	}
	if o.Discount > 0 {
		fmt.Fprintf(&sb, "🎟 تخفیف (%s): <s>%s</s> − %s تومان\n", esc(o.DiscountCode), faMoney(o.Amount+o.Discount), faMoney(o.Discount))
	}
	fmt.Fprintf(&sb, "💳 مبلغ قابل پرداخت: <b>%s تومان</b>\n\n", faMoney(o.Amount))
	fmt.Fprintf(&sb, "مبلغ را به این کارت واریز کنید (روی شماره بزنید تا کپی شود):\n<code>%s</code>\n", groupCard(c.Number))
	if c.Holder != "" || c.Bank != "" {
		fmt.Fprintf(&sb, "👤 %s %s\n", esc(c.Holder), map[bool]string{true: "· " + esc(c.Bank), false: ""}[c.Bank != ""])
	}
	sb.WriteString("\n📸 بعد از واریز، <b>عکس رسید</b> را همین‌جا بفرستید. بعد از تأیید، سرویس خودکار برایتان ارسال می‌شود.")
	if extra := b.Store.Setting(ctx, "tg_pay_text"); extra != "" {
		sb.WriteString("\n\nℹ️ " + esc(extra))
	}
	s := strconv.FormatInt(id, 10)
	b.setAwait(chat, "receipt:"+s)
	b.send(ctx, chat, sb.String(), &Keyboard{Rows: [][]Button{
		row(cbBtn("📸 ارسال رسید", "rcpt:"+s), cbBtn("✖️ لغو سفارش", "ocx:"+s)),
		row(cbBtn("💬 پشتیبانی", "support"), cbBtn("« منو", "home")),
	}})
}

// onReceipt takes a photo from a buyer with an open card order.
func (b *Bot) onReceipt(ctx context.Context, chat int64, name, fileID string, await string) bool {
	if b.onTopupReceipt(ctx, chat, name, fileID, await) {
		return true
	}
	buyer := hubcore.TgBuyer(chat)
	var id int64
	if v, ok := strings.CutPrefix(await, "receipt:"); ok {
		id, _ = strconv.ParseInt(v, 10, 64)
	}
	if id == 0 {
		var err error
		if id, err = b.Store.AwaitingReceipt(ctx, buyer); err != nil {
			return false
		}
	}
	o, err := b.Store.AttachReceipt(ctx, id, buyer, fileID)
	if err != nil {
		return false
	}
	b.takeAwait(chat)
	b.send(ctx, chat, fmt.Sprintf("✅ رسید سفارش <b>#%s</b> دریافت شد.\nبعد از بررسی (معمولاً چند دقیقه) سرویس همین‌جا برایتان ارسال می‌شود.", faInt(o.ID)), homeKB)
	caption := b.orderCard(ctx, o) + "\n\n🧾 <b>رسید پرداخت</b> از " + esc(name) + "\nمبلغ را با حساب خود تطبیق دهید، بعد تأیید یا رد کنید."
	if o.Seller == b.Seller {
		b.notifyPhoto(ctx, "orders", fileID, caption, receiptButtons(o.ID))
	} else {
		for _, c := range b.Store.TgRepChats(ctx, o.Seller) {
			if err := b.API.SendPhotoID(ctx, c, fileID, caption, receiptButtons(o.ID)); err != nil {
				b.logf("tgbot receipt: %v", err)
			}
		}
	}
	b.Store.MarkOrderAnnounced(ctx, o.ID, false)
	return true
}

func receiptButtons(id int64) *Keyboard {
	s := strconv.FormatInt(id, 10)
	return &Keyboard{Rows: [][]Button{row(cbBtn("✅ تأیید و تحویل", "ok:"+s), cbBtn("❌ رد رسید", "no:"+s))}}
}

// askCoupon waits for a discount code for a product.
func (b *Bot) askCoupon(ctx context.Context, chat int64, productID string) {
	b.setAwait(chat, "coupon:"+productID)
	if productID == "" { // from the main menu: the code is kept for the next purchase
		b.send(ctx, chat, "🎟 <b>کد تخفیف</b>\n"+line+"\nکد تخفیف خود را بفرستید؛ روی خرید بعدی شما اعمال می‌شود.", homeKB)
		return
	}
	b.send(ctx, chat, "🎟 <b>کد تخفیف</b>\n"+line+"\nکد تخفیف خود را بفرستید:", backTo("prod:"+productID, "بسته"))
}

var couponWhy = map[error]string{
	hubcore.ErrCodeUnknown:  "این کد وجود ندارد. املای آن را بررسی کنید.",
	hubcore.ErrCodeExpired:  "مهلت این کد تمام شده است.",
	hubcore.ErrCodeUsedUp:   "ظرفیت این کد تمام شده است.",
	hubcore.ErrCodeUsed:     "شما قبلاً از این کد استفاده کرده‌اید.",
	hubcore.ErrCodeProduct:  "این کد برای بستهٔ دیگری است.",
	hubcore.ErrCodeDisabled: "این کد فعال نیست.",
}

func (b *Bot) takeCoupon(ctx context.Context, chat int64, productID, code string) {
	d, err := b.Store.HoldDiscount(ctx, hubcore.TgBuyer(chat), code, productID)
	if err != nil {
		why := "این کد پذیرفته نشد."
		for e, t := range couponWhy {
			if errors.Is(err, e) {
				why = t
			}
		}
		if productID == "" {
			b.send(ctx, chat, "❌ "+why, &Keyboard{Rows: [][]Button{row(cbBtn("🔁 کد دیگر", "cpn:"), cbBtn("« منوی اصلی", "home"))}})
			return
		}
		b.send(ctx, chat, "❌ "+why, &Keyboard{Rows: [][]Button{row(cbBtn("🔁 کد دیگر", "cpn:"+productID), cbBtn("« بسته", "prod:"+productID))}})
		return
	}
	off := faInt(d.Percent) + "٪"
	if d.Percent == 0 {
		off = faMoney(d.Amount) + " تومان"
	}
	if productID == "" {
		b.send(ctx, chat, "🎉 کد <b>"+esc(d.Code)+"</b> ثبت شد: "+off+" تخفیف روی خرید بعدی شما (تا یک ساعت).", &Keyboard{Rows: [][]Button{row(cbBtn("🛍 خرید اشتراک", "shop")), row(cbBtn("« منوی اصلی", "home"))}})
		return
	}
	b.send(ctx, chat, "🎉 کد <b>"+esc(d.Code)+"</b> اعمال شد: "+off+" تخفیف روی خرید بعدی شما.", nil)
	b.showProduct(ctx, chat, productID)
}

// ---- toman wallet: top-up by receipt ----

func parseToman(s string) int64 {
	var n int64
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			n = n*10 + int64(r-'0')
		case r >= '۰' && r <= '۹':
			n = n*10 + int64(r-'۰')
		case r == ',' || r == '٬' || r == ' ' || r == '،':
		default:
			return 0
		}
		if n > 1e12 {
			return 0
		}
	}
	return n
}

// askTopup asks how much to add to the toman wallet.
func (b *Bot) askTopup(ctx context.Context, chat int64) {
	if !b.cardOn(ctx) {
		b.send(ctx, chat, "شارژ کیف پول فعلاً فعال نیست.", homeKB)
		return
	}
	b.setAwait(chat, "topamt")
	b.send(ctx, chat, "➕ <b>شارژ کیف پول</b>\n"+line+"\nمبلغ شارژ را به <b>تومان</b> بفرستید (مثلاً <code>200000</code>):",
		&Keyboard{Rows: [][]Button{row(cbBtn("100,000", "tamt:100000"), cbBtn("200,000", "tamt:200000"), cbBtn("500,000", "tamt:500000")), row(cbBtn("« کیف پول", "wallet"))}})
}

// startTopup opens a top-up and shows the card to pay to.
func (b *Bot) startTopup(ctx context.Context, chat int64, text string) {
	amount := parseToman(text)
	id, err := b.Store.CreateTopup(ctx, b.Seller, chat, amount)
	if err != nil {
		b.send(ctx, chat, "❌ مبلغ معتبر نیست. یک عدد بین ۱٬۰۰۰ تا ۲۰۰٬۰۰۰٬۰۰۰ تومان بفرستید.", &Keyboard{Rows: [][]Button{row(cbBtn("🔁 دوباره", "topup"), cbBtn("« منو", "home"))}})
		return
	}
	c := b.Store.CardFor(ctx, b.Seller)
	var sb strings.Builder
	fmt.Fprintf(&sb, "➕ <b>شارژ کیف پول #%s</b>\n%s\n💳 مبلغ: <b>%s تومان</b>\n\n", faInt(id), line, faMoney(amount))
	fmt.Fprintf(&sb, "مبلغ را به این کارت واریز کنید (روی شماره بزنید تا کپی شود):\n<code>%s</code>\n", groupCard(c.Number))
	if c.Holder != "" || c.Bank != "" {
		fmt.Fprintf(&sb, "👤 %s %s\n", esc(c.Holder), map[bool]string{true: "· " + esc(c.Bank), false: ""}[c.Bank != ""])
	}
	sb.WriteString("\n📸 بعد از واریز، <b>عکس رسید</b> را همین‌جا بفرستید. بعد از تأیید، موجودی کیف پول شما زیاد می‌شود.")
	b.setAwait(chat, "topup:"+strconv.FormatInt(id, 10))
	b.send(ctx, chat, sb.String(), &Keyboard{Rows: [][]Button{row(cbBtn("💬 پشتیبانی", "support"), cbBtn("« منو", "home"))}})
}

// onTopupReceipt takes a receipt photo for an open top-up.
func (b *Bot) onTopupReceipt(ctx context.Context, chat int64, name, fileID, await string) bool {
	var id int64
	if v, ok := strings.CutPrefix(await, "topup:"); ok {
		id, _ = strconv.ParseInt(v, 10, 64)
	} else if strings.HasPrefix(await, "receipt:") {
		return false // an order is waiting for its receipt
	} else if _, err := b.Store.AwaitingReceipt(ctx, hubcore.TgBuyer(chat)); err == nil {
		return false
	} else if id, err = b.Store.AwaitingTopup(ctx, b.Seller, chat); err != nil {
		return false
	}
	t, err := b.Store.AttachTopupReceipt(ctx, id, chat, fileID)
	if err != nil || t.Seller != b.Seller {
		return false
	}
	b.takeAwait(chat)
	b.send(ctx, chat, fmt.Sprintf("✅ رسید شارژ <b>#%s</b> دریافت شد. بعد از بررسی، موجودی کیف پول شما زیاد می‌شود.", faInt(t.ID)), homeKB)
	caption := fmt.Sprintf("➕ <b>شارژ کیف پول #%s</b>\n👤 %s (<code>%d</code>)\n💳 مبلغ: <b>%s تومان</b>\nمبلغ را با حساب خود تطبیق دهید.", faInt(t.ID), esc(name), chat, faMoney(t.Amount))
	kb := &Keyboard{Rows: [][]Button{row(cbBtn("✅ تأیید شارژ", "tok:"+strconv.FormatInt(t.ID, 10)), cbBtn("❌ رد", "tno:"+strconv.FormatInt(t.ID, 10)))}}
	b.notifyPhoto(ctx, "orders", fileID, caption, kb)
	return true
}

// sellerAdmins are the chats that approve this bot's payments.
func (b *Bot) sellerAdmins(ctx context.Context) map[int64]bool {
	out := map[int64]bool{}
	if b.Seller == "" {
		for c := range b.Owners {
			out[c] = true
		}
		return out
	}
	for _, c := range b.Store.TgRepChats(ctx, b.Seller) {
		out[c] = true
	}
	return out
}

// decideTopup is the approve/reject button on a top-up receipt.
func (b *Bot) decideTopup(ctx context.Context, chat int64, idText string, approve bool) {
	if !b.sellerAdmins(ctx)[chat] {
		if rep, role := b.Store.TgRepRole(ctx, chat); rep == "" || rep != b.Seller || role == "support" {
			return
		}
	}
	id, _ := strconv.ParseInt(idText, 10, 64)
	t, err := b.Store.DecideTopup(ctx, b.Seller, id, approve, "tg:"+strconv.FormatInt(chat, 10))
	if err != nil {
		b.send(ctx, chat, "انجام نشد: "+esc(err.Error()), nil)
		return
	}
	if approve {
		b.send(ctx, chat, fmt.Sprintf("✅ شارژ #%s تأیید شد (%s تومان).", faInt(t.ID), faMoney(t.Amount)), nil)
		b.celebrate(ctx, t.Chat, fmt.Sprintf("✅ کیف پول شما %s تومان شارژ شد.\n💰 موجودی: <b>%s تومان</b>", faMoney(t.Amount), faMoney(t.Balance)), &Keyboard{Rows: [][]Button{row(cbBtn("🛒 خرید سرویس", "shop"), cbBtn("💰 کیف پول", "wallet"))}}, EffectParty)
	} else {
		b.send(ctx, chat, fmt.Sprintf("شارژ #%s رد شد.", faInt(t.ID)), nil)
		b.send(ctx, t.Chat, "❌ رسید شارژ کیف پول تأیید نشد. اگر مبلغ را واریز کرده‌اید، با پشتیبانی در تماس باشید.", &Keyboard{Rows: [][]Button{row(cbBtn("💬 پشتیبانی", "support"), cbBtn("« منو", "home"))}})
	}
}

// payToman pays a product from the toman wallet.
func (b *Bot) payToman(ctx context.Context, chat int64, productID, renew string) {
	email, left, err := b.Store.TomanPay(ctx, chat, productID, renew)
	if err != nil {
		b.send(ctx, chat, fmt.Sprintf("پرداخت از کیف پول ممکن نشد (موجودی: %s تومان).", faMoney(left)), &Keyboard{Rows: [][]Button{row(cbBtn("➕ شارژ کیف پول", "topup"), cbBtn("« منو", "home"))}})
		return
	}
	b.announceOrders(ctx)
	b.celebrate(ctx, chat, fmt.Sprintf("✅ از کیف پول پرداخت شد: <code>%s</code>\nموجودی باقی‌مانده: %s تومان", esc(email), faMoney(left)), nil, EffectLike)
}
