package tgbot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// The customer's bottom keyboard: the bot's whole menu beside the message
// box, two columns, the main actions coloured (buy green, trial and wheel
// blue) and animated emoji when the seller set a theme. It follows the chat: a free trial only while it
// can be taken, the wheel while it is on, the reseller panel while it is on
// sale for this chat. A changed layout is sent again (once).

// KeyButton is one bottom-keyboard button.
type KeyButton struct {
	Text   string
	Style  string // success | primary | danger | ""
	WebApp string // opens the Mini App instead of sending the text
}

func (b *Bot) customerKeys(ctx context.Context, chat int64) [][]KeyButton {
	emails, _ := b.clients(ctx, chat)
	trial := b.Store.TrialPolicyFor(ctx, b.Seller).Enabled && !b.Store.TrialUsed(ctx, b.Seller, chat)
	// The Mini App is the menu button beside the message box: a keyboard
	// button would open it without the signed init data it needs.
	var rows [][]KeyButton
	// Two by two, the main actions first; guide and support always the last pair.
	items := []KeyButton{{Text: "🛍 خرید اشتراک", Style: "success"}}
	if trial {
		items = append(items, KeyButton{Text: "🎁 تست رایگان", Style: "primary"})
	}
	if len(emails) > 0 {
		items = append(items, KeyButton{Text: "📦 سرویس‌های من"})
	}
	if b.Seller != "" || b.cardOn(ctx) || b.Store.WalletBalance(ctx, chat) > 0 || b.Store.TomanBalance(ctx, b.Seller, chat) > 0 {
		items = append(items, KeyButton{Text: "👛 کیف پول"})
	}
	if b.wheelOn(ctx) {
		items = append(items, KeyButton{Text: "🎡 گردونهٔ شانس", Style: "primary"})
	}
	items = append(items, KeyButton{Text: "👤 حساب من"})
	if len(items)%2 == 1 {
		items = append(items, KeyButton{Text: "🌐 وضعیت سرورها"})
	}
	for i := 0; i < len(items); i += 2 {
		rows = append(rows, items[i:i+2])
	}
	rows = append(rows, []KeyButton{{Text: "📚 آموزش اتصال"}, {Text: "💬 پشتیبانی"}})
	if _, ok := b.repSaleButton(ctx, chat); ok {
		rows = append(rows, []KeyButton{{Text: "🤝 پنل نمایندگی", Style: "success"}})
	}
	return rows
}

func keysSig(rows [][]KeyButton) string {
	raw, _ := json.Marshal(rows)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:6])
}

// SendKeyboard sends a message carrying a persistent bottom keyboard with
// colours, icons and Mini App buttons.
func (a *HTTPAPI) SendKeyboard(ctx context.Context, chat int64, text, placeholder string, rows [][]KeyButton) error {
	build := func(theme map[string]string) [][]map[string]any {
		kb := make([][]map[string]any, len(rows))
		for i, r := range rows {
			for _, k := range r {
				btn := map[string]any{"text": k.Text}
				if k.Style != "" {
					btn["style"] = k.Style
				}
				if k.WebApp != "" {
					btn["web_app"] = map[string]string{"url": k.WebApp}
				}
				if rs := []rune(k.Text); len(theme) > 0 && len(rs) > 0 {
					if id, n := matchEmoji(rs, theme); n > 0 {
						if rest := strings.TrimSpace(string(rs[n:])); rest != "" {
							btn["icon_custom_emoji_id"], btn["text"] = id, rest
						}
					}
				}
				kb[i] = append(kb[i], btn)
			}
		}
		return kb
	}
	send := func(theme map[string]string) error {
		return a.callJSON(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML",
			"reply_markup": map[string]any{"keyboard": build(theme), "resize_keyboard": true, "is_persistent": true, "input_field_placeholder": placeholder}}, nil)
	}
	theme := a.emojiTheme()
	err := send(theme)
	if err != nil && len(theme) > 0 && emojiError(err) {
		a.emojiRefused()
		err = send(nil)
	}
	if err != nil && strings.Contains(strings.ToUpper(err.Error()), "STYLE") {
		for i := range rows { // an older Telegram: no colours
			for j := range rows[i] {
				rows[i][j].Style = ""
			}
		}
		err = send(nil)
	}
	return err
}

// ensureCustomerKeys sends the customer's keyboard when its layout changed.
func (b *Bot) ensureCustomerKeys(ctx context.Context, chat int64) bool {
	kb, ok := b.API.(interface {
		SendKeyboard(ctx context.Context, chat int64, text, placeholder string, rows [][]KeyButton) error
	})
	if !ok {
		return false
	}
	rows := b.customerKeys(ctx, chat)
	key := fmt.Sprintf("tg_kb3:%s:%d", b.Seller, chat)
	sig := keysSig(rows)
	if b.Store.Setting(ctx, key) == sig {
		return true
	}
	if err := kb.SendKeyboard(ctx, chat, "👇 منوی ربات پایین صفحه است؛ هر وقت خواستید از همان‌جا انتخاب کنید.", "از منوی پایین انتخاب کنید 👇", rows); err == nil {
		_ = b.Store.SetSetting(ctx, key, sig)
	}
	return true
}
