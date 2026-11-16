package tgbot

import (
	"context"
	"fmt"
)

// deliverReminders sends the renewal reminders staff asked for in the
// panel's analytics page. A reseller's own bot sends its customers'; the
// main bot sends the owner's and those of resellers without a running bot.
func (b *Bot) deliverReminders(ctx context.Context) {
	for _, r := range b.Store.PendingReminders(ctx) {
		mine := r.Seller == b.Seller
		if !mine && b.Seller == "" && r.Seller != "" {
			mine = !b.Store.RepBotOf(ctx, r.Seller).Running
		}
		if !mine {
			continue
		}
		text := b.reminderText(ctx, r.Email)
		kb := &Keyboard{Rows: [][]Button{{{Text: "♻️ تمدید سرویس", Data: "rnw:" + r.Email}}, {{Text: "📦 سرویس‌های من", Data: "svc"}}}}
		for _, chat := range r.Chats {
			b.send(ctx, chat, text, kb)
		}
		b.Store.ReminderSent(ctx, r.ID)
	}
}

func (b *Bot) reminderText(ctx context.Context, email string) string {
	c, err := b.Store.ClientByEmail(ctx, email)
	if err != nil {
		return fmt.Sprintf("⏳ سرویس <b>%s</b> رو به اتمام است. برای ادامهٔ اتصال، تمدیدش کنید.", esc(email))
	}
	parts := ""
	if until := c.EffectiveUntil; until > 0 {
		days := (until - b.now().Unix() + 86399) / 86400
		if days < 0 {
			days = 0
		}
		parts += fmt.Sprintf("\n⏳ زمان باقی‌مانده: <b>%s روز</b> (تا %s)", faInt(days), faDate(until))
	}
	if c.VolumeBytes > 0 && c.Remaining >= 0 {
		parts += fmt.Sprintf("\n📦 حجم باقی‌مانده: <b>%s</b>", gb(c.Remaining))
	}
	return fmt.Sprintf("🔔 <b>یادآوری تمدید</b>\nسرویس <b>%s</b> رو به اتمام است.%s\n\nبرای اینکه اتصالتان قطع نشود، همین حالا تمدید کنید.", esc(email), parts)
}
