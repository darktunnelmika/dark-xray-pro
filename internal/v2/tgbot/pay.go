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

func (b *Bot) cardOn(ctx context.Context) bool { return b.Store.Card(ctx).Number != "" }

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
	c := b.Store.Card(ctx)
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
	targets := map[int64]bool{}
	if o.Seller != "" {
		for _, c := range b.Store.TgRepChats(ctx, o.Seller) {
			targets[c] = true
		}
	} else {
		for c := range b.Owners {
			targets[c] = true
		}
	}
	for c := range targets {
		if err := b.API.SendPhotoID(ctx, c, fileID, caption, receiptButtons(o.ID)); err != nil {
			b.logf("tgbot receipt: %v", err)
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
		b.send(ctx, chat, "❌ "+why, &Keyboard{Rows: [][]Button{row(cbBtn("🔁 کد دیگر", "cpn:"+productID), cbBtn("« بسته", "prod:"+productID))}})
		return
	}
	off := faInt(d.Percent) + "٪"
	if d.Percent == 0 {
		off = faMoney(d.Amount) + " تومان"
	}
	b.send(ctx, chat, "🎉 کد <b>"+esc(d.Code)+"</b> اعمال شد: "+off+" تخفیف روی خرید بعدی شما.", nil)
	b.showProduct(ctx, chat, productID)
}
