package tgbot

import (
	"context"
	"fmt"
	"strconv"
)

// repTrialPanels reminds trial reseller panels a day before their end and
// freezes them at the end (hubcore/reptrial.go). Main bot only.
func (b *Bot) repTrialPanels(ctx context.Context) {
	remind, freeze := b.Store.RepTrialsDue(ctx)
	charge := func(rep string) *Keyboard {
		return &Keyboard{Rows: [][]Button{row(cbBtn("💳 شارژ و دائمی کردن پنل", "P:tp:"+rep)), row(cbBtn("« منوی اصلی", "home"))}}
	}
	for _, rep := range remind {
		b.Store.RepTrialReminded(ctx, rep)
		st := b.Store.RepTrialStateOf(ctx, rep)
		hours := (st.End - b.now().Unix() + 3599) / 3600
		for _, chat := range b.Store.TgRepChats(ctx, rep) {
			b.send(ctx, chat, fmt.Sprintf("⏳ <b>پنل نمایندگی تستی شما رو به پایان است</b>\n%s\nحدود <b>%s ساعت</b> دیگر پنل <code>%s</code> تمام می‌شود و کاربرهایش خاموش می‌شوند.\n\nبا یک شارژ، همین پنل با همهٔ کاربرها و کانفیگ‌هایش دائمی می‌شود.",
				line, faInt(hours), esc(rep)), charge(rep))
		}
	}
	for _, rep := range freeze {
		if err := b.Store.FreezeRepTrial(ctx, rep); err != nil {
			b.logf("tgbot trial panel %s: %v", rep, err)
			continue
		}
		for _, chat := range b.Store.TgRepChats(ctx, rep) {
			b.send(ctx, chat, fmt.Sprintf("⛔️ <b>مهلت پنل تستی تمام شد</b>\n%s\nکاربرهای پنل <code>%s</code> خاموش شدند و ساخت و تمدید قفل است.\n\nهیچ‌چیز پاک نشده: با شارژ پنل، همه‌چیز همان لحظه برمی‌گردد و پنل دائمی می‌شود.", line, esc(rep)), charge(rep))
		}
		b.notify(ctx, "reps", "🎁 پنل تستی <code>"+esc(rep)+"</code> تمام شد و قفل شد ("+strconv.Itoa(len(b.Store.TgRepChats(ctx, rep)))+" مالک تلگرام خبر گرفت).", nil)
	}
}
