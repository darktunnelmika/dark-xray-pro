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
		b.toOwners(ctx, fmt.Sprintf("⚠️ اعتبار نمایندهٔ <b>%s</b> کم شده: <b>%s</b> مانده (حد هشدار %s).",
			esc(firstNonEmpty(l.Name, l.RepID)), hubcore.GBText(l.Credit), hubcore.GBText(l.Limit)), nil)
	}
}

func (b *Bot) repAlertsFor(ctx context.Context, rep string) {
	chats := b.Store.TgRepChats(ctx, rep)
	if len(chats) == 0 {
		return
	}
	for _, l := range b.Store.DueLowCredit(ctx, "rep", rep) {
		msg := fmt.Sprintf("⚠️ اعتبار حجمی شما رو به اتمام است: <b>%s</b> مانده.\nبرای ادامهٔ فروش و تمدید، اعتبار را شارژ کنید.", hubcore.GBText(l.Credit))
		for _, c := range chats {
			b.send(ctx, c, msg, nil)
		}
	}
	if s, ok := b.Store.DueRepSummary(ctx, rep); ok {
		msg := fmt.Sprintf("📋 خلاصهٔ ۲۴ ساعت گذشته\n\n🛒 فروش: <b>%s</b> (%s مورد)\n♻️ برگشتی به اعتبار: <b>%s</b>\n💾 اعتبار فعلی: <b>%s</b>\n\n👥 کلاینت فعال: %s\n⏳ رو به اتمام: <b>%s</b>\n⛔ تمام‌شده: %s",
			hubcore.GBText(s.Sold), faInt(int64(s.Sales)), hubcore.GBText(s.Back), hubcore.GBText(s.Credit), faInt(int64(s.Active)), faInt(int64(s.Expiring)), faInt(int64(s.Ended)))
		for _, c := range chats {
			b.send(ctx, c, msg, nil)
		}
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
