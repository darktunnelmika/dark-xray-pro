package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// The reseller's side of panel resale (hubcore/resale.go): its toman wallet
// at the main panel, its bills and their reminders. The wallet is charged in
// the main bot (the money goes to the owner's card); the reseller's own bot
// shows it and links there.

// repInScope reports whether a reseller is managed in this bot: any of them
// in the main bot; the reseller itself and its sub-sellers in its own bot.
func (b *Bot) repInScope(ctx context.Context, rep string) bool {
	return b.Seller == "" || rep == b.Seller || b.Store.IsSellerOf(ctx, b.Seller, rep)
}

// walletOwner is the reseller whose wallet this chat may see ("" = none).
func (b *Bot) walletOwner(ctx context.Context, chat int64) string {
	rep, role := b.repOf(ctx, chat)
	if rep == "" || role != "owner" || b.Store.IsSubSeller(ctx, rep) || !b.Store.ResaleOf(ctx, rep).Allowed {
		return ""
	}
	return rep
}

// walletLinkKB: the wallet button here, and in a reseller's bot also the
// way to the main bot, where the wallet is charged.
func (b *Bot) walletLinkKB(ctx context.Context) *Keyboard {
	kb := &Keyboard{Rows: [][]Button{row(cbBtn("💼 کیف پول و فاکتورها", "P:w"))}}
	if b.Seller != "" {
		if u := b.Store.BotUsernameFor(ctx, ""); u != "" {
			kb.Rows = append(kb.Rows, row(urlBtn("➕ شارژ کیف پول در ربات اصلی", "https://t.me/"+u+"?start=wallet")))
		}
	}
	return kb
}

func hoursLeft(d time.Duration) string {
	if d <= 0 {
		return "گذشته"
	}
	h := int64(d.Hours())
	if h < 1 {
		return fmt.Sprintf("%s دقیقه", faInt(int64(d.Minutes())+1))
	}
	return fmt.Sprintf("%s ساعت", faInt(h))
}

// repWallet is the reseller's wallet card.
func (b *Bot) repWallet(ctx context.Context, chat int64) {
	rep := b.walletOwner(ctx, chat)
	if rep == "" {
		b.send(ctx, chat, "کیف پول فروش پنل برای شما فعال نیست.", homeKB)
		return
	}
	bal := b.Store.RepWallet(ctx, rep)
	var sb strings.Builder
	fmt.Fprintf(&sb, "💼 <b>کیف پول پنل اصلی</b> — <code>%s</code>\n%s\n💰 موجودی: <b>%s تومان</b>\n🏷 قیمت عمدهٔ شما (هر ترابایت): <b>%s تومان</b>\n",
		esc(rep), line, faMoney(bal), faMoney(b.Store.WholesaleTB(ctx, rep)))
	if b.Store.ResaleOf(ctx, rep).Credit {
		sb.WriteString("🧾 اگر موجودی کافی نباشد، فروش با فاکتور ۲۴ ساعته انجام می‌شود.\n")
	} else {
		sb.WriteString("برای هر فروش، قیمت عمده باید در کیف پول باشد.\n")
	}
	kb := &Keyboard{}
	bills, _ := b.Store.RepBills(ctx, rep, true, 5)
	now := b.now()
	for _, bl := range bills {
		left := time.Unix(bl.DueUnix, 0).Sub(now)
		state := "مهلت: " + hoursLeft(left)
		if bl.Overdue {
			state = "⛔️ مهلت گذشته — فروش و ساخت کاربر قفل است"
		}
		fmt.Fprintf(&sb, "\n🧾 <b>فاکتور #%s</b>: %s تومان\n%s\n", faInt(bl.ID), faMoney(bl.Amount), state)
		if bal >= bl.Amount {
			kb.Rows = append(kb.Rows, row(cbBtn(fmt.Sprintf("✅ پرداخت فاکتور #%s از کیف پول", faInt(bl.ID)), "P:wp:"+strconv.FormatInt(bl.ID, 10))))
		}
	}
	if hist, _ := b.Store.RepWalletHistory(ctx, rep, 5); len(hist) > 0 {
		sb.WriteString("\n<b>آخرین تراکنش‌ها</b>\n")
		for _, e := range hist {
			sign := "➕"
			if e.Delta < 0 {
				sign = "➖"
			}
			fmt.Fprintf(&sb, "%s %s تومان · %s\n", sign, faMoney(abs64(e.Delta)), faDate(e.Unix))
		}
	}
	if b.Seller == "" {
		kb.Rows = append(kb.Rows, row(cbBtn("➕ شارژ کیف پول", "P:wt")))
	} else if u := b.Store.BotUsernameFor(ctx, ""); u != "" {
		kb.Rows = append(kb.Rows, row(urlBtn("➕ شارژ کیف پول در ربات اصلی", "https://t.me/"+u+"?start=wallet")))
	}
	kb.Rows = append(kb.Rows, row(repBtn("🏠 داشبورد", "home")))
	b.send(ctx, chat, sb.String(), kb)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// repWalletAsk asks how much to charge (main bot only).
func (b *Bot) repWalletAsk(ctx context.Context, chat int64) {
	rep := b.walletOwner(ctx, chat)
	if rep == "" || b.Seller != "" {
		return
	}
	hint := ""
	if bills, _ := b.Store.RepBills(ctx, rep, true, 1); len(bills) > 0 {
		need := bills[0].Amount - b.Store.RepWallet(ctx, rep)
		if need > 0 {
			hint = fmt.Sprintf("\nبرای پرداخت فاکتور #%s دست‌کم <b>%s تومان</b> لازم است؛ بعد از شارژ، فاکتور خودکار پرداخت می‌شود.", faInt(bills[0].ID), faMoney(need))
		}
	}
	b.setAwait(chat, "P:wamt")
	b.send(ctx, chat, "➕ مبلغ شارژ را به تومان بفرستید (مثلاً <code>2600000</code>)."+hint, &Keyboard{Rows: [][]Button{row(cbBtn("« بازگشت", "P:w"))}})
}

// repWalletOpen makes the top-up order and shows the owner's card.
func (b *Bot) repWalletOpen(ctx context.Context, chat int64, name, text string) {
	rep := b.walletOwner(ctx, chat)
	if rep == "" || b.Seller != "" {
		return
	}
	amount, ok := parseNum(text)
	if !ok {
		amount = 0
	}
	o, err := b.Store.CreateRepWalletOrder(ctx, chat, name, rep, amount)
	if err != nil {
		msg := "مبلغ معتبر نیست. یک عدد از ۱۰٬۰۰۰ تومان به بالا بفرستید:"
		if strings.Contains(err.Error(), "no card") {
			b.send(ctx, chat, "پرداخت کارت‌به‌کارت فعلاً تنظیم نشده است؛ با مدیر پنل در تماس باشید.", homeKB)
			return
		}
		b.setAwait(chat, "P:wamt")
		b.send(ctx, chat, msg, &Keyboard{Rows: [][]Button{row(cbBtn("« بازگشت", "P:w"))}})
		return
	}
	c := b.Store.Card(ctx)
	var sb strings.Builder
	fmt.Fprintf(&sb, "💼 <b>شارژ کیف پول #%s</b>\n%s\nپنل: <code>%s</code>\n💳 مبلغ: <b>%s تومان</b>\n\n", faInt(o.ID), line, esc(rep), faMoney(o.Price))
	fmt.Fprintf(&sb, "مبلغ را به این کارت واریز کنید (روی شماره بزنید تا کپی شود):\n<code>%s</code>\n", groupCard(c.Number))
	if c.Holder != "" || c.Bank != "" {
		fmt.Fprintf(&sb, "%s\n", esc(strings.TrimSpace(c.Holder+" "+c.Bank)))
	}
	sb.WriteString("\n📸 سپس <b>عکس رسید</b> را همین‌جا بفرستید. بعد از تأیید، کیف پول شارژ و فاکتور باز (اگر باشد) خودکار پرداخت می‌شود.")
	b.setAwait(chat, "P:receipt:"+strconv.FormatInt(o.ID, 10))
	b.send(ctx, chat, sb.String(), &Keyboard{Rows: [][]Button{row(cbBtn("« بازگشت", "P:w"))}})
}

// repBillPay pays an open bill from the wallet.
func (b *Bot) repBillPay(ctx context.Context, chat, id int64) {
	rep := b.walletOwner(ctx, chat)
	if rep == "" {
		return
	}
	if err := b.Store.PayRepBill(ctx, rep, id); err != nil {
		msg := "این فاکتور پرداخت‌شده یا پیدا نشد."
		if err == hubcore.ErrCredit {
			msg = "موجودی کیف پول برای این فاکتور کافی نیست."
		}
		b.send(ctx, chat, msg, b.walletLinkKB(ctx))
		return
	}
	b.send(ctx, chat, fmt.Sprintf("✅ فاکتور #%s پرداخت شد.", faInt(id)), nil)
	b.repWallet(ctx, chat)
}

// remindBills tells resellers about bills nearing or past their due time
// (from their own bot if it runs, else from the main bot) and, in the main
// bot, tells the owner when a bill becomes overdue.
func (b *Bot) remindBills(ctx context.Context) {
	for _, n := range b.Store.BillNotices(ctx) {
		bl := n.Bill
		if !b.handles(bl.RepID) || !b.Store.ClaimBillNotice(ctx, bl.ID, n.Stage) {
			continue
		}
		var msg string
		switch n.Stage {
		case 1, 2:
			left := time.Unix(bl.DueUnix, 0).Sub(b.now())
			msg = fmt.Sprintf("⏰ <b>یادآوری فاکتور #%s</b>\n%s\nمبلغ: <b>%s تومان</b>\nزمان باقی‌مانده: <b>%s</b>\n\nبعد از مهلت، فروش پنل و ساخت و تمدید کاربر تا پرداخت قفل می‌شود.", faInt(bl.ID), line, faMoney(bl.Amount), hoursLeft(left))
		default:
			msg = fmt.Sprintf("⛔️ <b>مهلت فاکتور #%s گذشت</b>\n%s\nمبلغ: <b>%s تومان</b>\n\nتا پرداخت، فروش پنل و ساخت و تمدید کاربر قفل است. کاربرهای فعلی شما قطع نمی‌شوند.", faInt(bl.ID), line, faMoney(bl.Amount))
		}
		for _, c := range b.Store.TgRepOwners(ctx, bl.RepID) {
			b.send(ctx, c, msg, b.walletLinkKB(ctx))
		}
	}
	if b.Seller != "" {
		return
	}
	for _, bl := range b.Store.OverdueToTell(ctx) {
		b.notify(ctx, "reps", fmt.Sprintf("⛔️ <b>فاکتور #%s نمایندهٔ %s پرداخت نشد</b>\nمبلغ: %s تومان\nفروش و ساخت کاربر این نماینده تا پرداخت قفل شد. از پنل می‌توانید فاکتور را «پرداخت‌شده» یا «بخشیده» ثبت کنید.",
			faInt(bl.ID), esc(bl.RepID), faMoney(bl.Amount)), nil)
	}
}
