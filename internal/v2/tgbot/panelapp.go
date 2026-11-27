package tgbot

import "strings"

// panelAppURL is the panel as a Telegram Mini App (hubcore/tgpanel.go):
// the panel's address + /tgapp/. Telegram opens Mini Apps only over HTTPS,
// so without an HTTPS address there is no button ("").
func (b *Bot) panelAppURL() string {
	base := strings.TrimRight(b.SubBase, "/")
	if !strings.HasPrefix(base, "https://") {
		return ""
	}
	return base + "/tgapp/"
}

// panelAppRow is the "management panel" button row, or nil.
func (b *Bot) panelAppRow() []Button {
	u := b.panelAppURL()
	if u == "" {
		return nil
	}
	return []Button{{Text: "📱 پنل مدیریت (مینی‌اپ)", WebApp: &WebApp{URL: u}}}
}
