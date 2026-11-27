package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// The seller's "your own volume and days" inside the bot: pick the GB, then
// the days, and the bot shows the plan card with the normal pay buttons. The
// picked size becomes the seller's hidden "cx-…" product (customplan.go), so
// paying, receipts and delivery are the usual ones. A renewal carries the
// service's email at the end of the callback data.

func (b *Bot) customPlan(ctx context.Context) (hubcore.CustomPlan, bool) {
	c := b.Store.CustomPlanFor(ctx, b.Seller)
	if !c.Enabled || c.PriceGB <= 0 || (b.Seller != "" && b.Store.IsSubSeller(ctx, b.Seller)) {
		return c, false
	}
	return c, true
}

// customFits reports whether a renewal's email still fits Telegram's 64-byte
// callback data, both in the picker and in the pay buttons of the card.
func (b *Bot) customFits(email string) bool {
	return len("renew:cx-"+strings.ToLower(b.Seller)+"-10000g-3650d:"+email) <= 64
}

// steps picks a handful of round values between min and max.
func customSteps(min, max int64, base []int64) []int64 {
	out := []int64{min}
	for _, v := range base {
		if v > min && v < max {
			out = append(out, v)
		}
	}
	if max > min {
		out = append(out, max)
	}
	if len(out) > 9 { // keep the ends, thin the middle
		keep := []int64{out[0]}
		step := float64(len(out)-1) / 8
		for i := 1; i < 8; i++ {
			keep = append(keep, out[int(float64(i)*step+0.5)])
		}
		out = append(keep, out[len(out)-1])
	}
	return out
}

func gridRows(btns []Button, per int) [][]Button {
	var rows [][]Button
	for i := 0; i < len(btns); i += per {
		j := i + per
		if j > len(btns) {
			j = len(btns)
		}
		rows = append(rows, btns[i:j])
	}
	return rows
}

func customBack(email string) Button {
	if email != "" {
		return cbBtn("« سرویس", "svc:"+email)
	}
	return cbBtn("« بسته‌ها", "shop")
}

// customGB is step one: the volume.
func (b *Bot) customGB(ctx context.Context, chat int64, email string) {
	c, ok := b.customPlan(ctx)
	if !ok || (email != "" && (!b.ownsClient(ctx, chat, email) || !b.customFits(email))) {
		b.send(ctx, chat, "این امکان الان فعال نیست.", backTo("shop", "بسته‌ها"))
		return
	}
	var btns []Button
	for _, gb := range customSteps(c.MinGB, c.MaxGB, []int64{20, 30, 50, 75, 100, 150, 200, 300, 500, 1000}) {
		btns = append(btns, cbBtn(faInt(gb)+" گیگ", fmt.Sprintf("cxg:%d:%s", gb, email)))
	}
	kb := &Keyboard{Rows: gridRows(btns, 3)}
	kb.Rows = append(kb.Rows, row(cbBtn("✍️ عدد دلخواه", "cxt:"+email)), row(customBack(email)))
	price := "هر گیگ " + faMoney(c.PriceGB) + " تومان"
	if c.Price10 > 0 {
		price += " + هر ۱۰ روز " + faMoney(c.Price10) + " تومان"
	}
	title := "🎛 <b>حجم و روز دلخواه</b>"
	if email != "" {
		title = "🎛 <b>تمدید دلخواه " + esc(email) + "</b>"
	}
	b.send(ctx, chat, fmt.Sprintf("%s\n%s\n💳 %s\n📶 حجم: %s تا %s گیگ · ⏳ مدت: %s تا %s روز\n\n<b>قدم ۱ از ۲:</b> حجم را انتخاب کنید:",
		title, line, price, faInt(c.MinGB), faInt(c.MaxGB), faInt(c.MinDays), faInt(c.MaxDays)), kb)
}

// customDays is step two: the days, each button with its total price.
func (b *Bot) customDays(ctx context.Context, chat int64, gb int64, email string) {
	c, ok := b.customPlan(ctx)
	if !ok || gb < c.MinGB || gb > c.MaxGB || (email != "" && !b.ownsClient(ctx, chat, email)) {
		b.customGB(ctx, chat, email)
		return
	}
	var btns []Button
	for _, d := range customSteps(c.MinDays, c.MaxDays, []int64{15, 30, 45, 60, 90, 120, 180, 365}) {
		btns = append(btns, cbBtn(fmt.Sprintf("%s روز · %s", faInt(d), faMoney(c.Price(gb, d))), fmt.Sprintf("cxd:%d:%d:%s", gb, d, email)))
	}
	kb := &Keyboard{Rows: gridRows(btns, 2)}
	kb.Rows = append(kb.Rows, row(cbBtn("« تغییر حجم", "cx:"+email)))
	b.send(ctx, chat, fmt.Sprintf("🎛 <b>%s گیگ</b>\n%s\n<b>قدم ۲ از ۲:</b> مدت را انتخاب کنید (قیمت کل به تومان کنار هر دکمه است):", faInt(gb), line), kb)
}

// customPick shows the chosen size as a plan card with the pay buttons.
func (b *Bot) customPick(ctx context.Context, chat int64, gb, days int64, email string) {
	if email != "" && !b.ownsClient(ctx, chat, email) {
		return
	}
	if _, ok := b.customPlan(ctx); !ok {
		b.send(ctx, chat, "این امکان الان فعال نیست.", backTo("shop", "بسته‌ها"))
		return
	}
	p, err := b.Store.CustomProduct(ctx, b.Seller, gb, days)
	if err != nil {
		b.send(ctx, chat, "❌ این حجم یا مدت مجاز نیست.", &Keyboard{Rows: [][]Button{row(cbBtn("🔁 دوباره", "cx:"+email), customBack(email))}})
		return
	}
	b.productCard(ctx, chat, p, email)
}

// customAsk lets the buyer type both numbers at once.
func (b *Bot) customAsk(ctx context.Context, chat int64, email string) {
	c, ok := b.customPlan(ctx)
	if !ok {
		return
	}
	b.setAwait(chat, "cx:"+email)
	b.send(ctx, chat, fmt.Sprintf("✍️ حجم (گیگ) و مدت (روز) را با فاصله بفرستید.\nمثال: <code>%d %d</code>\n\nحجم %s تا %s گیگ · مدت %s تا %s روز",
		c.MinGB, c.MinDays, faInt(c.MinGB), faInt(c.MaxGB), faInt(c.MinDays), faInt(c.MaxDays)), &Keyboard{Rows: [][]Button{row(cbBtn("« برگشت", "cx:"+email))}})
}

func (b *Bot) customTyped(ctx context.Context, chat int64, email, text string) {
	gb, days, ok := parseGBPrice(text)
	if !ok || days <= 0 {
		b.setAwait(chat, "cx:"+email)
		b.send(ctx, chat, "دو عدد بفرستید: حجم به گیگ و مدت به روز. مثال: <code>50 30</code>", &Keyboard{Rows: [][]Button{row(cbBtn("« برگشت", "cx:"+email))}})
		return
	}
	b.customPick(ctx, chat, gb, days, email)
}

// customCallback handles cx, cxg, cxd and cxt.
func (b *Bot) customCallback(ctx context.Context, chat int64, data string) bool {
	verb, rest, _ := strings.Cut(data, ":")
	switch verb {
	case "cx":
		b.customGB(ctx, chat, rest)
	case "cxt":
		b.customAsk(ctx, chat, rest)
	case "cxg":
		gbS, email, _ := strings.Cut(rest, ":")
		gb, _ := strconv.ParseInt(gbS, 10, 64)
		b.customDays(ctx, chat, gb, email)
	case "cxd":
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) < 2 {
			return true
		}
		gb, _ := strconv.ParseInt(parts[0], 10, 64)
		days, _ := strconv.ParseInt(parts[1], 10, 64)
		email := ""
		if len(parts) == 3 {
			email = parts[2]
		}
		b.customPick(ctx, chat, gb, days, email)
	default:
		return false
	}
	return true
}
