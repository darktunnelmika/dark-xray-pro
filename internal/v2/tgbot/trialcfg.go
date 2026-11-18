package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// The free trial's switch, volume and length, set from the bot: the owner
// under "bot settings" (buttons "a:trial…"), a reseller from their home
// screen ("R:trial…"). Each seller has its own trial; all start off.

// trialScreen shows a seller's trial settings. cb is the callback prefix
// ("a:trial" or "R:trial"), home where "back" leads.
func (b *Bot) trialScreen(ctx context.Context, chat int64, seller, cb, home string) {
	p := b.Store.TrialPolicyFor(ctx, seller)
	state, toggle := "🔴 خاموش", Button{Text: "🟢 روشن کردن", Data: cb + ":on"}
	if p.Enabled {
		state, toggle = "🟢 روشن", Button{Text: "🔴 خاموش کردن", Data: cb + ":off"}
	}
	size := trialSize(p)
	text := fmt.Sprintf("🎁 <b>تست رایگان</b>\n\nوضعیت: <b>%s</b>\nحجم: <b>%s</b>\nمدت: <b>%s</b>\nدستگاه هم‌زمان: <b>%d</b>\n\nهر حساب تلگرام فقط یک بار تست می‌گیرد. کانفیگ تست در پنل با برچسب «تست» دیده می‌شود و %d روز بعد از پایان خودکار حذف می‌شود.",
		state, size, trialSpan(p.Hours), p.MaxIPs, 3)
	if seller != "" {
		text += "\n\nحجم هر تست از اعتبار شما کم می‌شود."
	}
	b.send(ctx, chat, text, &Keyboard{Rows: [][]Button{
		{toggle},
		{{Text: "📦 تغییر حجم", Data: cb + ":gb"}, {Text: "⏱ تغییر مدت", Data: cb + ":h"}},
		back(home)}})
}

// trialSize words the trial's volume: "50 مگ", "2 گیگ" or "نامحدود".
func trialSize(p hubcore.TrialPolicy) string {
	switch {
	case p.MB > 0 && p.MB%1024 == 0:
		return fmt.Sprintf("%d گیگ", p.MB/1024)
	case p.MB > 0:
		return fmt.Sprintf("%d مگ", p.MB)
	case p.GB > 0:
		return fmt.Sprintf("%d گیگ", p.GB)
	}
	return "نامحدود"
}

// parseTrialVolume reads "50m", "۵۰ مگ", "0.5" or "2": megabytes when a unit
// says so, otherwise gigabytes (a fraction of one becomes megabytes).
func parseTrialVolume(text string) (gb, mb int64, ok bool) {
	var num strings.Builder
	dot := false
	for _, r := range strings.TrimSpace(text) {
		switch {
		case r >= '0' && r <= '9':
			num.WriteRune(r)
		case r >= '۰' && r <= '۹':
			num.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩':
			num.WriteRune('0' + (r - '٠'))
		case (r == '.' || r == '٫' || r == '/') && !dot && num.Len() > 0:
			dot = true
			num.WriteRune('.')
		}
	}
	v, err := strconv.ParseFloat(num.String(), 64)
	if err != nil || v < 0 {
		return 0, 0, false
	}
	low := strings.ToLower(text)
	isMB := strings.Contains(low, "m") || strings.Contains(text, "مگ") || strings.Contains(text, "مگا")
	switch {
	case isMB:
		if v < 1 {
			return 0, 0, false
		}
		return 0, int64(v + 0.5), true
	case v == float64(int64(v)):
		return int64(v), 0, true
	default:
		m := int64(v*1024 + 0.5)
		if m < 1 {
			return 0, 0, false
		}
		return 0, m, true
	}
}

func trialSpan(hours int64) string {
	if hours%24 == 0 {
		return fmt.Sprintf("%d روز", hours/24)
	}
	return fmt.Sprintf("%d ساعت", hours)
}

// trialCallback handles the screen's buttons; act is what follows the prefix
// ("", "on", "off", "gb", "h").
func (b *Bot) trialCallback(ctx context.Context, chat int64, seller, cb, home, act string) {
	switch act {
	case "on", "off":
		p := b.Store.TrialPolicyFor(ctx, seller)
		p.Enabled = act == "on"
		if err := b.Store.PutTrialPolicyFor(ctx, seller, p); err != nil {
			b.send(ctx, chat, "ذخیره نشد: "+trialErr(err), &Keyboard{Rows: [][]Button{back(cb)}})
			return
		}
	case "gb":
		b.setAwait(chat, trialAwait("trialgb", seller, cb, home))
		hint := "حجم تست را بفرستید: برای مگابایت با m (مثلاً 50m)، برای گیگ فقط عدد (مثلاً 1). برای نامحدود 0 بفرستید."
		if seller != "" {
			hint = "حجم تست را بفرستید: برای مگابایت با m (مثلاً 50m)، برای گیگ فقط عدد (مثلاً 1)."
		}
		b.send(ctx, chat, hint, &Keyboard{Rows: [][]Button{back(cb)}})
		return
	case "h":
		b.setAwait(chat, trialAwait("trialh", seller, cb, home))
		hint := "مدت تست را به ساعت بفرستید (مثلاً 24)."
		if seller != "" {
			hint = "مدت تست را به روز بفرستید (مثلاً 1)."
		}
		b.send(ctx, chat, hint, &Keyboard{Rows: [][]Button{back(cb)}})
		return
	}
	b.trialScreen(ctx, chat, seller, cb, home)
}

// trialAwait names the prompt. A reseller's prompts start with "r:" like all
// of the reseller UI's, so their typed answer is routed there.
func trialAwait(kind, seller, cb, home string) string {
	w := kind + ":" + seller + "|" + cb + "|" + home
	if seller != "" {
		w = "r:" + w
	}
	return w
}

// trialInput takes the typed volume or length. It reports whether what was
// one of its prompts.
func (b *Bot) trialInput(ctx context.Context, chat int64, what, text string) bool {
	kind, rest, ok := strings.Cut(strings.TrimPrefix(what, "r:"), ":")
	if !ok || (kind != "trialgb" && kind != "trialh") {
		return false
	}
	f := strings.SplitN(rest, "|", 3)
	if len(f) != 3 {
		return false
	}
	seller, cb, home := f[0], f[1], f[2]
	n, okNum := parseNum(text)
	p := b.Store.TrialPolicyFor(ctx, seller)
	if kind == "trialgb" {
		p.GB, p.MB, okNum = parseTrialVolume(text)
	} else if okNum {
		switch {
		case seller != "": // resellers sell whole days
			p.Hours = n * 24
		default:
			p.Hours = n
		}
	}
	if !okNum {
		b.setAwait(chat, what)
		b.send(ctx, chat, "فقط یک عدد بفرستید.", &Keyboard{Rows: [][]Button{back(cb)}})
		return true
	}
	if err := b.Store.PutTrialPolicyFor(ctx, seller, p); err != nil {
		b.setAwait(chat, what)
		b.send(ctx, chat, "ذخیره نشد: "+trialErr(err)+"\nدوباره بفرستید:", &Keyboard{Rows: [][]Button{back(cb)}})
		return true
	}
	b.trialScreen(ctx, chat, seller, cb, home)
	return true
}

func trialErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "0-1000 GB"):
		return "حجم باید بین ۰ تا ۱۰۰۰ گیگ و مدت بین ۱ ساعت تا ۳۰ روز باشد."
	case strings.Contains(s, "needs a volume"):
		return "تست نماینده باید حجم داشته باشد."
	case strings.Contains(s, "sub-sellers"):
		return "زیرفروشنده‌ها تست رایگان ندارند."
	case strings.Contains(s, "choose the inbounds"):
		return "هیچ اینباند فعالی برای تست پیدا نشد."
	}
	return esc(s)
}
