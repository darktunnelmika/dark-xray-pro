package tgbot

import (
	"strings"
	"testing"
)

func TestDecorateEmoji(t *testing.T) {
	theme := map[string]string{"🔥": "111", "❤": "222", "🇩🇪": "333"}
	got := decorate("🔥 hot <code>🔥</code> ❤️ love 🇩🇪 <b>x</b>", theme)
	want := `<tg-emoji emoji-id="111">🔥</tg-emoji> hot <code>🔥</code> <tg-emoji emoji-id="222">❤️</tg-emoji> love <tg-emoji emoji-id="333">🇩🇪</tg-emoji> <b>x</b>`
	if got != want {
		t.Fatalf("\n%s\n%s", got, want)
	}
	if decorate("plain", nil) != "plain" {
		t.Fatal("no theme, no change")
	}
}

func TestStyleKeyboard(t *testing.T) {
	kb := &Keyboard{Rows: [][]Button{{{Text: "🔥 خرید سرویس", Data: "shop"}, {Text: "❌ لغو", Data: "home"}, {Text: "« منو", Data: "home"}}}}
	out := styleKeyboard(kb, map[string]string{"🔥": "111"})
	b := out.Rows[0]
	if b[0].Style != "success" || b[0].Icon != "111" || b[0].Text != "خرید سرویس" {
		t.Fatalf("%+v", b[0])
	}
	if b[1].Style != "danger" || b[1].Icon != "" || b[2].Style != "" {
		t.Fatalf("%+v %+v", b[1], b[2])
	}
	if kb.Rows[0][0].Icon != "" {
		t.Fatal("the original keyboard is left alone")
	}
	if p := plainKeyboard(out); p.Rows[0][0].Icon != "" {
		t.Fatal(p)
	}
}

func TestCustomEmojiFromMessage(t *testing.T) {
	// "a🔥b❤️" : 🔥 is two UTF-16 units at offset 1, ❤️ two at offset 4.
	m := &Message{Text: "a🔥b❤️", Entities: []MessageEntity{{Type: "custom_emoji", Offset: 1, Length: 2, CustomEmojiID: "111"}, {Type: "custom_emoji", Offset: 4, Length: 2, CustomEmojiID: "222"}, {Type: "bold", Offset: 0, Length: 1}}}
	ce := m.CustomEmoji()
	if ce["🔥"] != "111" || ce["❤"] != "222" || len(ce) != 2 {
		t.Fatal(ce)
	}
	if !strings.Contains(decorate("❤️", ce), `emoji-id="222"`) {
		t.Fatal("a learned emoji decorates")
	}
}

func TestKeyboardAction(t *testing.T) {
	for in, want := range map[string]string{"⚡ خرید سرویس": "shop", "🛍 خرید اشتراک": "shop", "📦 سرویس‌های من": "mine", "💳 کیف پول": "wallet",
		"🎫 پشتیبانی": "support", "☰ بیشتر": "home", "🏠 داشبورد": "admin", "سلام": "", "/start": ""} {
		if got := keyboardAction(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
