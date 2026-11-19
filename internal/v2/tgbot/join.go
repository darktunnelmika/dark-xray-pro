package tgbot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Required channel membership. A customer who is not in the seller's
// channels can still see their own services and write to support; buying,
// renewing, the trial and the rest of the menu wait until they join.
// Owners, resellers and bot admins are never asked. If Telegram cannot be
// asked (network, the bot is not an admin there), the customer is let in:
// locking everyone out over an error would be worse.

// memberChecker is the part of the API that can ask about membership.
type memberChecker interface {
	ChatMember(ctx context.Context, channel string, user int64) (string, error)
}

const joinMemo = 10 * time.Minute // how long a confirmed membership is trusted

// missingChannels lists the required channels the chat has not joined.
func (b *Bot) missingChannels(ctx context.Context, chat int64) []string {
	pol := b.Store.JoinPolicy(ctx, b.Seller)
	if !pol.Enabled || len(pol.Channels) == 0 || b.Owners[chat] {
		return nil
	}
	if rep, _ := b.Store.TgRepRole(ctx, chat); rep != "" {
		return nil
	}
	mc, ok := b.API.(memberChecker)
	if !ok {
		return nil
	}
	now := b.now()
	b.mu.Lock()
	until, fresh := b.joined[chat]
	b.mu.Unlock()
	if fresh && now.Before(until) {
		return nil
	}
	var missing []string
	for _, ch := range pol.Channels {
		st, err := mc.ChatMember(ctx, ch, chat)
		if err != nil {
			b.logf("tgbot join check %s: %v", ch, err)
			continue // cannot ask: do not hold the customer back
		}
		if st == "left" || st == "kicked" {
			missing = append(missing, ch)
		}
	}
	if len(missing) == 0 {
		b.mu.Lock()
		if b.joined == nil {
			b.joined = map[int64]time.Time{}
		}
		b.joined[chat] = now.Add(joinMemo)
		b.mu.Unlock()
	}
	return missing
}

// joinPrompt asks the chat to join; false means nothing is missing.
func (b *Bot) joinPrompt(ctx context.Context, chat int64) bool {
	missing := b.missingChannels(ctx, chat)
	if len(missing) == 0 {
		return false
	}
	kb := &Keyboard{}
	for _, ch := range missing {
		kb.Rows = append(kb.Rows, []Button{{Text: "📢 عضویت در " + ch, URL: "https://t.me/" + strings.TrimPrefix(ch, "@")}})
	}
	kb.Rows = append(kb.Rows, row(cbBtn("✅ عضو شدم", "join:ok")), row(cbBtn("📦 سرویس‌های من", "mine"), cbBtn("💬 پشتیبانی", "support")))
	what := "کانال ما"
	if len(missing) > 1 {
		what = "کانال‌های ما"
	}
	b.send(ctx, chat, "📢 <b>عضویت در کانال</b>\n"+line+"\nبرای استفاده از ربات، اول در "+what+" عضو شوید و بعد «عضو شدم» را بزنید.\nخبرها، تخفیف‌ها و اطلاعیه‌های قطعی همان‌جا اعلام می‌شود.", kb)
	return true
}

// joinFree reports whether a customer action stays open without membership:
// their own services and support.
func joinFree(verb string) bool {
	switch verb {
	case "join", "mine", "svc", "subl", "qr", "cfg", "use", "support":
		return true
	}
	return false
}

// ---- settings (owner: "a:join…", reseller: "R:join…") ----

func (b *Bot) joinScreen(ctx context.Context, chat int64, seller, cb, home string) {
	p := b.Store.JoinPolicy(ctx, seller)
	state, toggle := "🔴 خاموش", Button{Text: "🟢 روشن کردن", Data: cb + ":on"}
	if p.Enabled {
		state, toggle = "🟢 روشن", Button{Text: "🔴 خاموش کردن", Data: cb + ":off"}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "📢 <b>عضویت اجباری در کانال</b>\n\nوضعیت: <b>%s</b>\n\nمشتری تا عضو نشود نمی‌تواند خرید، تمدید یا تست کند؛ دیدن سرویس‌های خودش و پشتیبانی همیشه باز است.\n\n<b>کانال‌ها</b>\n", state)
	kb := &Keyboard{Rows: [][]Button{{toggle}}}
	if len(p.Channels) == 0 {
		sb.WriteString("هنوز کانالی ثبت نشده است.\n")
	}
	for i, ch := range p.Channels {
		fmt.Fprintf(&sb, "• %s\n", esc(ch))
		kb.Rows = append(kb.Rows, []Button{{Text: "🗑 حذف " + ch, Data: fmt.Sprintf("%s:del:%d", cb, i)}})
	}
	if len(p.Channels) < 3 {
		kb.Rows = append(kb.Rows, []Button{{Text: "➕ افزودن کانال", Data: cb + ":add"}})
	}
	sb.WriteString("\nربات باید <b>ادمین</b> هر کانال باشد تا بتواند عضویت را بررسی کند.")
	kb.Rows = append(kb.Rows, back(home))
	b.send(ctx, chat, sb.String(), kb)
}

func joinAwait(seller, cb, home string) string {
	w := "joinadd:" + seller + "|" + cb + "|" + home
	if seller != "" {
		w = "r:" + w
	}
	return w
}

func joinErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "add a channel first"):
		return "اول یک کانال اضافه کنید."
	case strings.Contains(s, "public username"):
		return "آیدی کانال عمومی را بفرستید، مثل <code>@darkvpn</code>."
	case strings.Contains(s, "at most three"):
		return "حداکثر سه کانال."
	}
	return esc(s)
}

func (b *Bot) joinCallback(ctx context.Context, chat int64, seller, cb, home, act, arg string) {
	p := b.Store.JoinPolicy(ctx, seller)
	switch act {
	case "on", "off":
		p.Enabled = act == "on"
	case "del":
		var n int
		if _, err := fmt.Sscanf(arg, "%d", &n); err == nil && n >= 0 && n < len(p.Channels) {
			p.Channels = append(p.Channels[:n:n], p.Channels[n+1:]...)
			if len(p.Channels) == 0 {
				p.Enabled = false
			}
		}
	case "add":
		b.setAwait(chat, joinAwait(seller, cb, home))
		b.send(ctx, chat, "آیدی کانال را بفرستید (مثل <code>@darkvpn</code>).\nقبلش ربات را در همان کانال <b>ادمین</b> کنید.", &Keyboard{Rows: [][]Button{back(cb)}})
		return
	default:
		b.joinScreen(ctx, chat, seller, cb, home)
		return
	}
	if err := b.Store.PutJoinPolicy(ctx, seller, p); err != nil {
		b.send(ctx, chat, "ذخیره نشد: "+joinErr(err), &Keyboard{Rows: [][]Button{back(cb)}})
		return
	}
	b.forgetJoined()
	b.joinScreen(ctx, chat, seller, cb, home)
}

// forgetJoined drops remembered memberships after the rules change.
func (b *Bot) forgetJoined() {
	b.mu.Lock()
	b.joined = nil
	b.mu.Unlock()
}

// joinInput takes the typed channel. It reports whether what was its prompt.
func (b *Bot) joinInput(ctx context.Context, chat int64, what, text string) bool {
	rest, ok := strings.CutPrefix(strings.TrimPrefix(what, "r:"), "joinadd:")
	if !ok {
		return false
	}
	f := strings.SplitN(rest, "|", 3)
	if len(f) != 3 {
		return false
	}
	seller, cb, home := f[0], f[1], f[2]
	again := func(msg string) bool {
		b.setAwait(chat, what)
		b.send(ctx, chat, msg, &Keyboard{Rows: [][]Button{back(cb)}})
		return true
	}
	ch, valid := hubcore.NormalChannel(text)
	if !valid {
		return again("آیدی کانال عمومی را بفرستید، مثل <code>@darkvpn</code>.")
	}
	// Can this bot see who is in that channel? Only an admin can.
	if mc, ok := b.API.(memberChecker); ok {
		if _, err := mc.ChatMember(ctx, ch, chat); err != nil {
			return again("ربات نتوانست عضویت را در " + esc(ch) + " بررسی کند. مطمئن شوید آیدی درست است و ربات <b>ادمین</b> آن کانال است، بعد دوباره بفرستید.")
		}
	}
	p := b.Store.JoinPolicy(ctx, seller)
	p.Channels = append(p.Channels, ch)
	if err := b.Store.PutJoinPolicy(ctx, seller, p); err != nil {
		return again("ذخیره نشد: " + joinErr(err))
	}
	b.forgetJoined()
	b.joinScreen(ctx, chat, seller, cb, home)
	return true
}
