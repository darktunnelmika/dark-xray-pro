package tgbot

import (
	"context"
	"fmt"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// repAlerts sends resellers their low-credit warning and daily summary
// (through their own bot when it runs, otherwise through the main bot) and
// tells the owners when a reseller runs low.
func (b *Bot) repAlerts(ctx context.Context) {
	if b.Seller != "" {
		b.repAlertsFor(ctx, b.Seller)
		return
	}
	reps, err := b.Store.Reps(ctx)
	if err != nil {
		return
	}
	for _, r := range reps {
		if r.Enabled && !hubcore.RepBotRunning(r.ID) {
			b.repAlertsFor(ctx, r.ID)
		}
	}
	for _, l := range b.Store.DueLowCredit(ctx, "owner", "") {
		b.notify(ctx, "reps", fmt.Sprintf("⚠️ اعتبار نمایندهٔ <b>%s</b> کم شده: <b>%s</b> مانده (حد هشدار %s).",
			esc(firstNonEmpty(l.Name, l.RepID)), hubcore.GBText(l.Credit), hubcore.GBText(l.Limit)), nil)
	}
}

func (b *Bot) repAlertsFor(ctx context.Context, rep string) {
	chats := b.Store.TgRepChats(ctx, rep)
	if len(chats) == 0 {
		return
	}
	warned := false
	for _, l := range b.Store.DueLowCredit(ctx, "rep", rep) {
		msg := fmt.Sprintf("⚠️ اعتبار حجمی شما رو به اتمام است: <b>%s</b> مانده.\nبرای ادامهٔ فروش و تمدید، اعتبار را شارژ کنید.", hubcore.GBText(l.Credit))
		msg += forecastText(b.Store.RepForecast(ctx, rep, l.Credit))
		b.repNotice(ctx, rep, chats, msg)
		warned = true
	}
	// Above the GB threshold but spending fast: warn by days left too.
	if o, ok := b.Store.DueRunningOut(ctx, rep); ok && !warned {
		msg := fmt.Sprintf("⏳ با سرعت فروش فعلی، اعتبار شما (<b>%s</b>) فقط برای حدود <b>%s روز</b> دیگر کافی است.", hubcore.GBText(o.Credit), faDays(o.Forecast.DaysLeft))
		msg += forecastText(o.Forecast)
		b.repNotice(ctx, rep, chats, msg)
	}
	if s, ok := b.Store.DueRepSummary(ctx, rep); ok {
		msg := fmt.Sprintf("📋 خلاصهٔ ۲۴ ساعت گذشته\n\n🛒 فروش: <b>%s</b> (%s مورد)\n♻️ برگشتی به اعتبار: <b>%s</b>\n💾 اعتبار فعلی: <b>%s</b>\n\n👥 کلاینت فعال: %s\n⏳ رو به اتمام: <b>%s</b>\n⛔ تمام‌شده: %s",
			hubcore.GBText(s.Sold), faInt(int64(s.Sales)), hubcore.GBText(s.Back), hubcore.GBText(s.Credit), faInt(int64(s.Active)), faInt(int64(s.Expiring)), faInt(int64(s.Ended)))
		msg += forecastText(b.Store.RepForecast(ctx, rep, s.Credit))
		b.repNotice(ctx, rep, chats, msg)
	}
}

// faDays is a day count for messages: "کمتر از یک" under one day.
func faDays(d float64) string {
	if d < 1 {
		return "کمتر از یک"
	}
	return faInt(int64(d))
}

// forecastText is the pace line (and the package that covers a month) for
// the reseller's messages; "" when it is not selling.
func forecastText(f hubcore.CreditForecast) string {
	if f.DaysLeft < 0 || f.DailyBytes <= 0 {
		return ""
	}
	t := fmt.Sprintf("\n\n📈 میانگین فروش ۷ روز اخیر: <b>%s</b> در روز؛ اعتبار فعلی حدود <b>%s روز</b> کافی است.", hubcore.GBText(f.DailyBytes), faDays(f.DaysLeft))
	if f.Package != nil {
		name := f.Package.Name
		if name == "" {
			name = hubcore.GBText(f.Package.GB << 30)
		}
		t += fmt.Sprintf("\n💡 بستهٔ پیشنهادی برای حدود یک ماه: <b>%s</b> (%s)", esc(name), hubcore.GBText(f.Package.GB<<30))
		if f.Package.Price > 0 {
			t += fmt.Sprintf(" — %s تومان", faMoney(f.Package.Price))
		}
	}
	return t
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// repNotice: in the reseller's own bot, its group topic when it has one;
// otherwise its chats.
func (b *Bot) repNotice(ctx context.Context, rep string, chats []int64, msg string) {
	if b.Seller == rep {
		b.notify(ctx, "reports", msg, nil)
		return
	}
	for _, c := range chats {
		b.send(ctx, c, msg, nil)
	}
}
