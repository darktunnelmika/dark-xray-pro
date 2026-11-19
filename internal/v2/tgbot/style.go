package tgbot

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// The bot's look: animated (premium) custom emoji, coloured buttons,
// message effects and streamed drafts.
//
// Custom emoji need no changes to the texts: the owner sends the bot the
// premium emoji they like (/emoji), each one is stored under the plain emoji
// it stands for (Telegram tells us), and every plain emoji with a match in a
// message or at the start of a button is shown animated. Telegram only
// allows this when the bot's owner has Premium; when it refuses, the bot
// sends plain emoji and tries again a few hours later.

// Message effects (private chats only).
const (
	EffectParty = "5046509860389126442" // 🎉
	EffectHeart = "5159385139981059251" // ❤️
	EffectFire  = "5104841245755180586" // 🔥
	EffectLike  = "5107584321108051014" // 👍
)

type effectKey struct{}

// WithEffect asks the next message sent with ctx to play an effect.
func WithEffect(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, effectKey{}, id)
}

func effectOf(ctx context.Context) string {
	v, _ := ctx.Value(effectKey{}).(string)
	return v
}

// emojiKey drops variation selectors so "❤️" and "❤" match.
func emojiKey(s string) string { return strings.ReplaceAll(s, "️", "") }

type emojiState struct {
	mu     sync.Mutex
	offTil time.Time // Telegram refused custom emoji: plain until then
}

func (a *HTTPAPI) emojiTheme() map[string]string {
	if a.Emoji == nil {
		return nil
	}
	a.es.mu.Lock()
	off := time.Now().Before(a.es.offTil)
	a.es.mu.Unlock()
	if off {
		return nil
	}
	return a.Emoji()
}

func (a *HTTPAPI) emojiRefused() {
	a.es.mu.Lock()
	a.es.offTil = time.Now().Add(6 * time.Hour)
	a.es.mu.Unlock()
}

// emojiError reports a refusal caused by custom emoji or button icons.
func emojiError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToUpper(err.Error())
	return strings.Contains(s, "EMOJI") || strings.Contains(s, "PREMIUM")
}

// decorate swaps plain emoji for their animated versions, outside <code>,
// <pre> and tags.
func decorate(text string, theme map[string]string) string {
	if len(theme) == 0 || text == "" {
		return text
	}
	var b strings.Builder
	inCode := 0
	rs := []rune(text)
	for i := 0; i < len(rs); {
		if rs[i] == '<' {
			j := i
			for j < len(rs) && rs[j] != '>' {
				j++
			}
			tag := strings.ToLower(string(rs[i:min(j+1, len(rs))]))
			switch {
			case strings.HasPrefix(tag, "<code"), strings.HasPrefix(tag, "<pre"), strings.HasPrefix(tag, "<tg-emoji"), strings.HasPrefix(tag, "<a "):
				inCode++
			case strings.HasPrefix(tag, "</code"), strings.HasPrefix(tag, "</pre"), strings.HasPrefix(tag, "</tg-emoji"), strings.HasPrefix(tag, "</a>"):
				inCode = max(inCode-1, 0)
			}
			b.WriteString(string(rs[i:min(j+1, len(rs))]))
			i = j + 1
			continue
		}
		if inCode == 0 {
			if id, n := matchEmoji(rs[i:], theme); n > 0 {
				b.WriteString(`<tg-emoji emoji-id="` + id + `">` + string(rs[i:i+n]) + `</tg-emoji>`)
				i += n
				continue
			}
		}
		b.WriteRune(rs[i])
		i++
	}
	return b.String()
}

// matchEmoji finds the longest themed emoji at the start of rs (up to 8
// runes: flags, ZWJ sequences and variation selectors).
func matchEmoji(rs []rune, theme map[string]string) (string, int) {
	if rs[0] < 0x2000 {
		return "", 0
	}
	for n := min(8, len(rs)); n >= 1; n-- {
		if id, ok := theme[emojiKey(string(rs[:n]))]; ok {
			// Take a trailing variation selector along.
			if n < len(rs) && rs[n] == '️' {
				n++
			}
			return id, n
		}
	}
	return "", 0
}

// Button colours by meaning.
var (
	dangerWords  = []string{"لغو", "حذف", "رد ", "❌", "انصراف", "توقف", "خاموش", "Cancel", "Delete", "Reject"}
	successWords = []string{"پرداخت", "تأیید", "تایید", "خرید", "تمدید", "شارژ", "ثبت", "ارسال رسید", "Pay", "Buy", "Renew", "Confirm", "Approve"}
	primaryWords = []string{"سرویس‌های من", "فروشگاه", "مینی", "باز کردن", "My services", "Shop", "Open"}
)

func buttonStyle(text, data string) string {
	switch {
	case strings.HasPrefix(data, "tno:") || strings.HasPrefix(data, "no:") || strings.HasPrefix(data, "x:"):
		return "danger"
	case strings.HasPrefix(data, "tok:") || strings.HasPrefix(data, "ok:"):
		return "success"
	}
	for _, w := range dangerWords {
		if strings.Contains(text, w) {
			return "danger"
		}
	}
	for _, w := range successWords {
		if strings.Contains(text, w) {
			return "success"
		}
	}
	for _, w := range primaryWords {
		if strings.Contains(text, w) {
			return "primary"
		}
	}
	return ""
}

// styleKeyboard colours buttons and, with a theme, moves a leading emoji
// into the button's animated icon. It returns a copy.
func styleKeyboard(kb *Keyboard, theme map[string]string) *Keyboard {
	if kb == nil {
		return nil
	}
	out := &Keyboard{Rows: make([][]Button, len(kb.Rows))}
	for i, row := range kb.Rows {
		out.Rows[i] = make([]Button, len(row))
		for j, b := range row {
			if b.Style == "" {
				b.Style = buttonStyle(b.Text, b.Data)
			}
			if b.Icon == "" && len(theme) > 0 {
				rs := []rune(b.Text)
				if len(rs) > 0 {
					if id, n := matchEmoji(rs, theme); n > 0 {
						rest := strings.TrimSpace(string(rs[n:]))
						if rest != "" {
							b.Icon, b.Text = id, rest
						}
					}
				}
			}
			out.Rows[i][j] = b
		}
	}
	return out
}

// plainKeyboard drops icons (when Telegram refuses them).
func plainKeyboard(kb *Keyboard) *Keyboard {
	if kb == nil {
		return nil
	}
	out := &Keyboard{Rows: make([][]Button, len(kb.Rows))}
	for i, row := range kb.Rows {
		out.Rows[i] = make([]Button, len(row))
		for j, b := range row {
			b.Icon = ""
			out.Rows[i][j] = b
		}
	}
	return out
}

// CustomEmoji lists the premium emoji in a message: plain emoji → id.
func (m *Message) CustomEmoji() map[string]string {
	out := map[string]string{}
	text, ents := m.Text, m.Entities
	if text == "" {
		text, ents = m.Caption, m.CaptionEntities
	}
	u := utf16.Encode([]rune(text))
	for _, e := range ents {
		if e.Type != "custom_emoji" || e.CustomEmojiID == "" || e.Offset < 0 || e.Offset+e.Length > len(u) {
			continue
		}
		alt := string(utf16.Decode(u[e.Offset : e.Offset+e.Length]))
		if k := emojiKey(alt); k != "" {
			out[k] = e.CustomEmojiID
		}
	}
	return out
}

// Draft streams text into the chat as it is "typed" (private chats), for
// a moment before the real message arrives. Best effort.
func (a *HTTPAPI) Draft(ctx context.Context, chat int64, text string) {
	id := rand.Int64N(1<<31-1) + 1
	rs := []rune(text)
	steps := 4
	for k := 1; k <= steps; k++ {
		part := string(rs[:len(rs)*k/steps])
		if strings.TrimSpace(part) == "" {
			continue
		}
		if err := a.callJSON(ctx, "sendMessageDraft", map[string]any{"chat_id": chat, "draft_id": id, "text": part}, nil); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(220 * time.Millisecond):
		}
	}
}

// drafter is implemented by the real API (tests leave it out).
type drafter interface {
	Draft(ctx context.Context, chat int64, text string)
}

func (b *Bot) typing(ctx context.Context, chat int64, text string) {
	if d, ok := b.API.(drafter); ok {
		d.Draft(ctx, chat, text)
	}
}

// celebrate sends a new message (never a redraw) with a full-screen effect.
func (b *Bot) celebrate(ctx context.Context, chat int64, text string, kb *Keyboard, effect string) {
	_, err := b.API.Send(WithEffect(ctx, effect), chat, text, kb, 0)
	if errors.Is(err, ErrBlocked) {
		b.Store.TgSetBlocked(ctx, chat)
	} else if err != nil {
		b.logf("tgbot send: %v", err)
	}
}

// The bottom keyboard: one small row that is always there. "منوی اصلی" brings
// the menu back from anywhere, so nobody has to type /start; everything else
// lives in that menu, once. (The labels of the older, larger keyboard are
// still understood by keyboardAction.)
var replyRows = [][]string{
	{"🏠 منوی اصلی", "💬 پشتیبانی"},
}

// keyboardAction maps a bottom-keyboard label (ours or the old panel's) to
// an action.
func keyboardAction(text string) string {
	t := strings.TrimSpace(text)
	// Drop a leading emoji and spaces, compare the words.
	words := strings.TrimLeftFunc(t, func(r rune) bool { return r > 0x2000 || r == ' ' || r == '️' })
	switch words {
	case "خرید اشتراک", "خرید سرویس":
		return "shop"
	case "سرویس‌های من", "سرویس های من":
		return "mine"
	case "کیف پول":
		return "wallet"
	case "پشتیبانی":
		return "support"
	case "آموزش اتصال", "آموزش":
		return "guide"
	case "منوی اصلی", "بیشتر", "منو":
		return "home"
	case "داشبورد", "فروش", "گزارش‌ها", "مدیریت", "نمایندگان", "اعلان‌ها":
		return "admin"
	case "کاربران":
		return "rclients"
	case "ساخت کاربر":
		return "rnew"
	case "سفارش‌ها", "سفارش ها":
		return "rorders"
	case "اعتبار":
		return "rcredit"
	case "محصولات", "محصولات فروشگاه":
		return "rshop"
	case "حالت مشتری":
		return "rcustomer"
	case "حالت نماینده":
		return "repmode"
	}
	return ""
}

func (b *Bot) keyAction(ctx context.Context, chat int64, act string) {
	b.takeAwait(chat) // a button press ends whatever the bot was waiting for
	switch act {
	case "shop", "mine", "wallet", "support", "guide", "home":
		// A press on the older, larger keyboard swaps it for the current one.
		defer b.ensureKeyboard(ctx, chat)
	}
	switch act {
	case "shop":
		b.shop(ctx, chat)
	case "mine":
		b.mine(ctx, chat)
	case "wallet":
		b.wallet(ctx, chat)
	case "support":
		b.setAwait(chat, "support")
		b.send(ctx, chat, "💬 <b>پشتیبانی</b>\n"+line+"\nپیام خود را بنویسید و بفرستید؛ مستقیم به پشتیبانی می‌رسد و پاسخ همین‌جا می‌آید.", homeKB)
	case "guide":
		b.showGuide(ctx, chat, "")
	case "admin":
		if b.Owners[chat] {
			b.ownerHome(ctx, chat)
			return
		}
		if rep, role := b.repOf(ctx, chat); rep != "" {
			b.repHome(ctx, chat, rep, role)
			return
		}
		b.menu(ctx, chat)
	case "rclients", "rnew", "rorders", "rcredit", "rshop", "rcustomer", "repmode":
		if b.Owners[chat] {
			if act == "rshop" && b.Seller == "" {
				b.shopHome(ctx, chat)
				return
			}
			b.ownerHome(ctx, chat)
			return
		}
		if rep, role := b.repOf(ctx, chat); rep != "" {
			b.repKey(ctx, chat, rep, role, act)
			return
		}
		b.menu(ctx, chat)
	default:
		b.menu(ctx, chat)
	}
}

// ensureKeyboard puts our bottom keyboard on a chat once (it also replaces
// the old panel's keyboard).
func (b *Bot) ensureKeyboard(ctx context.Context, chat int64) {
	kb, ok := b.API.(interface {
		SendReplyKeyboard(ctx context.Context, chat int64, text string, rows [][]string) error
	})
	if !ok {
		return
	}
	if rep, _ := b.repOf(ctx, chat); rep != "" && b.repMode(ctx, chat) {
		b.ensureRepKeyboard(ctx, chat)
		return
	}
	key := fmt.Sprintf("tg_kb2:%s:%d", b.Seller, chat) // kb2: the one-row keyboard
	if b.Store.Setting(ctx, key) == "1" {
		return
	}
	if err := kb.SendReplyKeyboard(ctx, chat, "⌨️ دکمهٔ «منوی اصلی» همیشه پایین صفحه است.", replyRows); err == nil {
		_ = b.Store.SetSetting(ctx, key, "1")
	}
}

// SendReplyKeyboard sends a message carrying a persistent bottom keyboard.
func (a *HTTPAPI) SendReplyKeyboard(ctx context.Context, chat int64, text string, rows [][]string) error {
	kb := make([][]map[string]string, len(rows))
	for i, r := range rows {
		for _, t := range r {
			kb[i] = append(kb[i], map[string]string{"text": t})
		}
	}
	return a.callJSON(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text,
		"reply_markup": map[string]any{"keyboard": kb, "resize_keyboard": true, "is_persistent": true}}, nil)
}
