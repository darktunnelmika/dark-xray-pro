package tgbot

import "strings"

// panelAppURL opens the Mini App on its management tab (hubcore/miniadmin.go;
// the full web panel is a button inside it). Telegram opens Mini Apps only
// over HTTPS, so without an HTTPS address there is no button ("").
func (b *Bot) panelAppURL() string {
	base := strings.TrimRight(b.SubBase, "/")
	if !strings.HasPrefix(base, "https://") {
		return ""
	}
	return base + "/app/?m=1"
}

// panelAppRow is the "management panel" button row, or nil.
func (b *Bot) panelAppRow() []Button {
	u := b.panelAppURL()
	if u == "" {
		return nil
	}
	return []Button{{Text: "📱 پنل مدیریت", WebApp: &WebApp{URL: u}}}
}
