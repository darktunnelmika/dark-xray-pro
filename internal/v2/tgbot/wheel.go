package tgbot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// The daily wheel of fortune in the bot (the Mini App shows it turning; the
// prize is the same hubcore.SpinWheel).

func (b *Bot) wheelOn(ctx context.Context) bool {
	return b.Store.WheelFor(ctx, b.Seller).On && (b.Seller == "" || !b.Store.IsSubSeller(ctx, b.Seller))
}

func wheelPrize(kind string, v int64) string {
	switch kind {
	case "gb":
		return faInt(v) + " گیگ حجم هدیه"
	case "days":
		return faInt(v) + " روز اضافه"
	case "wallet":
		return faMoney(v) + " تومان در کیف پول"
	case "discount":
		return faInt(v) + "٪ کد تخفیف"
	}
	return "پوچ"
}

func (b *Bot) spin(ctx context.Context, chat int64) {
	back := &Keyboard{Rows: [][]Button{row(cbBtn("« منوی اصلی", "home"))}}
	sp, err := b.Store.SpinWheel(ctx, b.Seller, chat)
	switch {
	case errors.Is(err, hubcore.ErrSpun):
		b.send(ctx, chat, "🎡 امروز گردونه را چرخانده‌اید. فردا دوباره سر بزنید!", back)
		return
	case errors.Is(err, hubcore.ErrNotFound):
		b.send(ctx, chat, "🎡 گردونه فعلاً خاموش است.", back)
		return
	case err != nil && strings.Contains(err.Error(), "buy a service"):
		b.send(ctx, chat, "🎡 برای چرخاندن گردونه اول یک سرویس بخرید.", &Keyboard{Rows: [][]Button{row(cbBtn("🛍 خرید اشتراک", "shop"), cbBtn("« منو", "home"))}})
		return
	case err != nil && strings.Contains(err.Error(), "all given"):
		b.send(ctx, chat, "🎡 جایزه‌های امروز تمام شد؛ فردا دوباره سر بزنید.", back)
		return
	case err != nil:
		b.logf("tgbot wheel: %v", err)
		b.send(ctx, chat, "چرخاندن ممکن نشد؛ کمی بعد دوباره امتحان کنید.", back)
		return
	}
	text := "🎡 <b>گردونهٔ شانس</b>\n" + line + "\n"
	if sp.Kind == "none" {
		b.send(ctx, chat, text+"این بار پوچ بود 😅 فردا دوباره شانس‌تان را امتحان کنید!", back)
		return
	}
	text += "🎉 برنده شدید: <b>" + wheelPrize(sp.Kind, sp.Value) + "</b>\n\n"
	switch sp.Kind {
	case "gb", "days":
		text += fmt.Sprintf("به سرویس <code>%s</code> اضافه شد.", esc(sp.Target))
	case "wallet":
		text += "به کیف پول‌تان اضافه شد؛ برای خرید و تمدید استفاده کنید."
	case "discount":
		text += fmt.Sprintf("کد شما: <code>%s</code>\nسه روز وقت دارید و یک بار قابل استفاده است.", sp.Code)
	}
	b.send(ctx, chat, text+"\n\nفردا دوباره بچرخانید! 🍀", back)
}
