package tgbot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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
		b.notify(ctx, "orders", fmt.Sprintf("⚠️ پرداخت %d⭐ سفارش #%d از %s انجام شد ولی تحویل نشد: %s", p.Total, id, esc(name), esc(faErr(err))),
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
		b.send(ctx, owner, "بازپرداخت ممکن نیست: "+esc(faErr(err)), nil)
		return
	}
	if err := api.RefundStars(ctx, chat, charge); err != nil {
		b.send(ctx, owner, "تلگرام بازپرداخت را رد کرد: "+esc(faErr(err)), nil)
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
	email, err := b.Store.ClaimTrial(ctx, b.Seller, chat)
	switch {
	case errors.Is(err, hubcore.ErrTrialUsed):
		b.send(ctx, chat, "تست رایگان قبلاً برای این حساب فعال شده است. برای ادامه یک پلن بخرید.", mainMenu)
		return
	case err != nil:
		b.send(ctx, chat, "تست رایگان فعلاً در دسترس نیست.", mainMenu)
		return
	}
	p := b.Store.TrialPolicyFor(ctx, b.Seller)
	size := trialSize(p)
	if size == "نامحدود" {
		size = "حجم نامحدود"
	}
	b.send(ctx, chat, fmt.Sprintf("🎁 تست رایگان شما فعال شد: %s برای %s.", size, trialSpan(p.Hours)), nil)
	b.sendSub(ctx, chat, email)
}

// remindTrials tells the owners of trials that end within a day, once, and
// offers the shop.
func (b *Bot) remindTrials(ctx context.Context) {
	for _, e := range b.Store.TrialsEnding(ctx, b.Seller) {
		b.Store.TrialReminded(ctx, b.Seller, e.Chat)
		left := (e.Expires - b.now().Unix() + 3599) / 3600
		b.send(ctx, e.Chat, fmt.Sprintf("⏳ <b>تست رایگان شما رو به پایان است</b>\n%s\nحدود <b>%s ساعت</b> دیگر تمام می‌شود. اگر از سرعت و کیفیت راضی بودید، با خرید اشتراک همین حالا بدون قطعی ادامه دهید.", line, faInt(left)),
			&Keyboard{Rows: [][]Button{row(cbBtn("🛍 خرید اشتراک", "shop")), row(cbBtn("📦 سرویس‌های من", "mine"), cbBtn("« منوی اصلی", "home"))}})
	}
}

func (b *Bot) invite(ctx context.Context, chat int64) {
	pol := b.Store.ReferralPolicy(ctx)
	if b.Seller != "" || !pol.Enabled {
		b.send(ctx, chat, "دعوت دوستان در این ربات فعال نیست.", homeKB)
		return
	}
	sum, err := b.Store.ReferralSummary(ctx, chat)
	if err != nil {
		return
	}
	user, _ := b.API.Me(ctx)
	link := fmt.Sprintf("https://t.me/%s?start=ref_%s", user, sum.Code)
	var you []string
	if pol.Percent > 0 {
		you = append(you, fmt.Sprintf("%s٪ مبلغ خرید او به کیف پولتان", faInt(pol.Percent)))
	}
	if pol.Fixed > 0 {
		you = append(you, fmt.Sprintf("%d⭐ برای هر خرید استارزی", pol.Fixed))
	}
	if pol.BonusGB > 0 {
		you = append(you, fmt.Sprintf("%s گیگ هدیه روی سرویس‌تان (با اولین خرید هر دوست)", faInt(pol.BonusGB)))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "👥 <b>دعوت دوستان</b>\n%s\n", line)
	if len(you) > 0 {
		first := ""
		if pol.FirstOnly && (pol.Percent > 0 || pol.Fixed > 0) {
			first = "\n(پاداش پولی فقط برای اولین خرید هر دوست)"
		}
		fmt.Fprintf(&sb, "🎁 <b>سهم شما</b> از هر دوستی که با لینک شما بیاید و خرید کند:\n• %s%s\n", strings.Join(you, "\n• "), first)
	}
	if pol.InviteePercent > 0 {
		fmt.Fprintf(&sb, "\n🎉 <b>سهم دوستتان:</b> %s٪ تخفیف روی اولین خرید\n", faInt(pol.InviteePercent))
	}
	fmt.Fprintf(&sb, "\nلینک شما (روی آن بزنید تا کپی شود):\n<code>%s</code>\n", esc(link))

	list, _ := b.Store.Invitees(ctx, chat, 500)
	bought := 0
	var bytes int64
	for _, v := range list {
		if v.Bought > 0 {
			bought++
		}
		bytes += v.Bytes
	}
	earned := faMoney(sum.EarnedToman) + " تومان"
	if sum.Earned > 0 {
		earned += fmt.Sprintf(" + %d⭐", sum.Earned)
	}
	if bytes > 0 {
		earned += " + " + faGB(bytes) + " گیگ"
	}
	fmt.Fprintf(&sb, "\n📊 دعوت‌شده‌ها: <b>%s نفر</b> · خرید کرده: <b>%s</b>\n💰 درآمد تا امروز: <b>%s</b>\n",
		faInt(int64(len(list))), faInt(int64(bought)), earned)
	if len(list) > 0 {
		sb.WriteString("\n<b>آخرین دعوت‌شده‌ها</b>\n")
		for i, v := range list {
			if i == 10 {
				fmt.Fprintf(&sb, "… و %s نفر دیگر\n", faInt(int64(len(list)-10)))
				break
			}
			name := v.Name
			if name == "" {
				name = "کاربر"
			}
			if r := []rune(name); len(r) > 18 {
				name = string(r[:18]) + "…"
			}
			state := "👋 عضو شد"
			switch {
			case v.Bought > 0:
				state = "✅ خرید کرد"
				var got []string
				if v.Toman > 0 {
					got = append(got, faMoney(v.Toman)+" ت")
				}
				if v.Stars > 0 {
					got = append(got, fmt.Sprintf("%d⭐", v.Stars))
				}
				if v.Bytes > 0 {
					got = append(got, faGB(v.Bytes)+" گیگ")
				}
				if len(got) > 0 {
					state += " · +" + strings.Join(got, " + ")
				}
			case v.Trial:
				state = "🧪 تست گرفت"
			}
			fmt.Fprintf(&sb, "• %s — %s\n", esc(name), state)
		}
	} else {
		sb.WriteString("\nهنوز کسی با لینک شما نیامده. لینک را برای دوستانتان بفرستید 👇\n")
	}
	pitch := "🚀 من از دارک وی‌پی‌ان استفاده می‌کنم: سریع، پایدار و با تحویل خودکار."
	if pol.InviteePercent > 0 {
		pitch += fmt.Sprintf(" با این لینک وارد شوی، روی اولین خریدت %s٪ تخفیف می‌گیری 🎁", faInt(pol.InviteePercent))
	}
	share := "https://t.me/share/url?url=" + url.QueryEscape(link) + "&text=" + url.QueryEscape(pitch)
	b.send(ctx, chat, strings.TrimRight(sb.String(), "\n"), &Keyboard{Rows: [][]Button{
		row(urlBtn("📤 ارسال لینک برای دوستان", share)),
		row(cbBtn("🔄 به‌روزرسانی", "invite"), cbBtn("« منوی اصلی", "home")),
	}})
}

// faGB writes bytes as gigabytes for people: "۵" or "۰٫۵".
func faGB(n int64) string {
	g := float64(n) / (1 << 30)
	if g == float64(int64(g)) {
		return faInt(int64(g))
	}
	return strings.ReplaceAll(faNum(strconv.FormatFloat(g, 'f', 1, 64)), ".", "٫")
}

// account is "my account": who the customer is here and what they have.
func (b *Bot) account(ctx context.Context, chat int64) {
	var sb strings.Builder
	name := b.Store.TgName(ctx, chat)
	if name == "" {
		name = "کاربر"
	}
	fmt.Fprintf(&sb, "👤 <b>حساب من</b>\n%s\nنام: <b>%s</b>\nشناسهٔ تلگرام: <code>%d</code>\n", line, esc(name), chat)
	emails, _ := b.clients(ctx, chat)
	active := 0
	for _, e := range emails {
		if c, err := b.Store.ClientByEmail(ctx, e); err == nil && b.state(c).active {
			active++
		}
	}
	fmt.Fprintf(&sb, "سرویس‌ها: <b>%s</b> (%s فعال)\n", faInt(int64(len(emails))), faInt(int64(active)))
	fmt.Fprintf(&sb, "کیف پول: <b>%s تومان</b>\n", faMoney(b.Store.TomanBalance(ctx, b.Seller, chat)))
	if b.Seller == "" {
		if st := b.Store.WalletBalance(ctx, chat); st > 0 {
			fmt.Fprintf(&sb, "اعتبار استارز: <b>%d⭐</b>\n", st)
		}
		if b.Store.ReferralPolicy(ctx).Enabled {
			if sum, err := b.Store.ReferralSummary(ctx, chat); err == nil {
				fmt.Fprintf(&sb, "دعوت‌شده‌ها: <b>%s نفر</b>\n", faInt(int64(sum.Invited)))
			}
		}
	}
	if orders := b.Store.TgOrdersOf(ctx, chat, b.Seller, 5); len(orders) > 0 {
		sb.WriteString("\n🧾 <b>سفارش‌های اخیر</b>\n")
		status := map[string]string{"pending": "⏳ در انتظار", "fulfilled": "✅ تحویل شد", "cancelled": "✖️ لغو شد"}
		for _, o := range orders {
			n := strings.TrimLeftFunc(o.Name, func(r rune) bool { return r > 0x2000 || r == ' ' || r == '️' })
			if n == "" {
				n = "سرویس"
			}
			kind := "خرید"
			if o.Renew {
				kind = "تمدید"
			}
			fmt.Fprintf(&sb, "#%s · %s %s · %s تومان · %s\n", faInt(o.ID), kind, esc(n), faMoney(o.Amount), status[o.Status])
		}
	} else {
		sb.WriteString("\nهنوز سفارشی ثبت نکرده‌اید.\n")
	}
	b.send(ctx, chat, strings.TrimRight(sb.String(), "\n"), &Keyboard{Rows: [][]Button{
		row(cbBtn("📦 سرویس‌های من", "mine"), cbBtn("👛 کیف پول", "wallet")), row(cbBtn("« منوی اصلی", "home"))}})
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
