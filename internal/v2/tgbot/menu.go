package tgbot

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// The main menu: a banner, a greeting by name, one short card per service
// (usage bar, what is left) and buttons that lead with what this customer
// most likely wants next: renew an ended service, open the running one, or
// try and buy when there is none.

//go:embed banner.jpg
var bannerJPG []byte

// bannerKey names the stored Telegram file id of the banner; change the
// suffix when banner.jpg changes.
const bannerKey = "tg_banner_v1"

// photoUploader is the part of the API that can upload a picture and report
// its file id (the real API can; test doubles need not).
type photoUploader interface {
	UploadPhoto(ctx context.Context, chat int64, name string, img []byte, caption string, kb *Keyboard) (string, error)
}

// sizeShort is size without needless decimals: "۴۰ گیگ", not "۴۰٫۰۰ گیگ".
func sizeShort(n int64) string {
	return strings.NewReplacer("٫۰۰", "", ".00", "").Replace(size(n))
}

func usageBar(share float64) string {
	if share < 0 {
		share = 0
	}
	if share > 1 {
		share = 1
	}
	n := int(share*10 + 0.5)
	return strings.Repeat("▰", n) + strings.Repeat("▱", 10-n)
}

// planTitle is a readable name for a service: the product it was bought
// with, or its size and length.
func (b *Bot) planTitle(ctx context.Context, c hubcore.ClientView) string {
	name := b.Store.ClientPlanName(ctx, c.Email)
	// Drop a leading emoji of the product's name: the card has its own icon.
	name = strings.TrimLeftFunc(name, func(r rune) bool { return r > 0x2000 || r == ' ' || r == '️' })
	if name != "" {
		return name
	}
	var parts []string
	switch {
	case c.VolumeBytes > 0:
		parts = append(parts, sizeShort(c.VolumeBytes))
	case c.VolumeBytes < 0:
		parts = append(parts, "نامحدود")
	}
	if c.DurationSeconds >= 86400 {
		parts = append(parts, faInt(c.DurationSeconds/86400)+" روزه")
	}
	if len(parts) == 0 {
		return c.Email
	}
	return strings.Join(parts, " · ")
}

// menuCard draws one service for the menu and reports whether it has ended
// (so the menu can lead with renewing it).
func (b *Bot) menuCard(ctx context.Context, c hubcore.ClientView, showName bool) (card string, ended bool) {
	now := b.now().Unix()
	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>📦 %s</b>\n", esc(b.planTitle(ctx, c)))
	if showName { // several services: the technical name tells them apart, on its own line
		fmt.Fprintf(&sb, "<code>%s</code>\n", esc(shortName(c.Email)))
	}
	used := c.Usage.Up + c.Usage.Down
	switch {
	case !c.Enabled:
		sb.WriteString("⛔ این سرویس غیرفعال است.")
		return sb.String(), false
	case c.Remaining == 0:
		sb.WriteString(usageBar(1) + " تمام شد\nحجم این سرویس تمام شده است. با تمدید، همین لینک دوباره فعال می‌شود.")
		return sb.String(), true
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
		sb.WriteString("⌛ زمان این سرویس تمام شده است. با تمدید، همین لینک دوباره فعال می‌شود.")
		return sb.String(), true
	}
	if c.VolumeBytes > 0 {
		share := float64(used) / float64(c.VolumeBytes)
		left := c.VolumeBytes - used
		if left < 0 {
			left = 0
		}
		fmt.Fprintf(&sb, "%s %s٪\nحجم مانده: <b>%s</b> از %s\n", usageBar(share), faInt(int64(share*100+0.5)), sizeShort(left), sizeShort(c.VolumeBytes))
	} else {
		sb.WriteString("حجم: <b>نامحدود</b>\n")
	}
	switch {
	case c.EffectiveUntil > now:
		left := c.EffectiveUntil - now
		if left >= 86400 {
			fmt.Fprintf(&sb, "زمان مانده: <b>%s روز</b>", faInt((left+86399)/86400))
		} else {
			fmt.Fprintf(&sb, "زمان مانده: <b>%s ساعت</b>", faInt((left+3599)/3600))
		}
	case c.DurationSeconds > 0 && c.Usage.FirstSeen == 0:
		fmt.Fprintf(&sb, "🆕 آماده؛ %s روز از اولین اتصال", faInt(c.DurationSeconds/86400))
	default:
		sb.WriteString("زمان: <b>بدون محدودیت</b>")
	}
	return sb.String(), false
}

// shortName keeps a long technical service name on one line.
func shortName(email string) string {
	r := []rune(email)
	if len(r) <= 26 {
		return email
	}
	return string(r[:25]) + "…"
}

// menuLine is one service in a single line, for customers with several:
// state, plan, what is left.
func (b *Bot) menuLine(ctx context.Context, c hubcore.ClientView) (line string, ended bool) {
	now := b.now().Unix()
	title := esc(b.planTitle(ctx, c))
	switch {
	case !c.Enabled:
		return "⛔ " + title + " · غیرفعال", false
	case c.Remaining == 0:
		return "🔴 " + title + " · حجم تمام شد", true
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
		return "🔴 " + title + " · زمان تمام شد", true
	}
	parts := []string{title}
	if c.VolumeBytes > 0 {
		left := c.VolumeBytes - c.Usage.Up - c.Usage.Down
		if left < 0 {
			left = 0
		}
		parts = append(parts, "<b>"+sizeShort(left)+"</b> مانده")
	}
	icon := "🟢"
	switch {
	case c.EffectiveUntil > now:
		if left := c.EffectiveUntil - now; left >= 86400 {
			parts = append(parts, faInt((left+86399)/86400)+" روز")
		} else {
			parts = append(parts, faInt((left+3599)/3600)+" ساعت")
		}
	case c.DurationSeconds > 0 && c.Usage.FirstSeen == 0:
		icon = "🆕"
		parts = append(parts, "هنوز وصل نشده")
	}
	return icon + " " + strings.Join(parts, " · "), false
}

const menuLines = 6 // services listed one per line before the rest are counted

func (b *Bot) menu(ctx context.Context, chat int64) {
	var sb strings.Builder
	if name := b.Store.TgName(ctx, chat); name != "" {
		fmt.Fprintf(&sb, "<b>سلام %s 👋</b>\n", esc(name))
	} else {
		sb.WriteString("<b>سلام 👋</b>\n")
	}
	emails, _ := b.clients(ctx, chat)
	var views []hubcore.ClientView
	for _, e := range emails {
		if c, err := b.Store.ClientByEmail(ctx, e); err == nil {
			views = append(views, c)
		}
	}
	var ended []string
	if len(views) <= 2 {
		// One or two services: a full card each.
		for _, c := range views {
			card, end := b.menuCard(ctx, c, len(views) > 1)
			if end {
				ended = append(ended, c.Email)
			}
			sb.WriteString("<blockquote>" + card + "</blockquote>\n")
		}
	} else {
		// Several: one line each, the ones that need renewing first.
		var done, live []string
		for _, c := range views {
			l, end := b.menuLine(ctx, c)
			if end {
				ended = append(ended, c.Email)
				done = append(done, l)
			} else {
				live = append(live, l)
			}
		}
		lines := append(done, live...)
		more := 0
		if len(lines) > menuLines {
			more = len(lines) - menuLines
			lines = lines[:menuLines]
		}
		fmt.Fprintf(&sb, "📦 <b>%s سرویس</b> (%s نیاز به تمدید)\n<blockquote>%s</blockquote>\n", faInt(int64(len(views))), faInt(int64(len(ended))), strings.Join(lines, "\n"))
		if more > 0 {
			fmt.Fprintf(&sb, "و %s سرویس دیگر در «سرویس‌های من».\n", faInt(int64(more)))
		}
	}
	if len(views) == 0 {
		if w := b.Store.Welcome(ctx, b.Seller); w != "" {
			sb.WriteString(esc(w) + "\n")
		} else {
			brand := "به <b>Dark VPN</b> خوش آمدید."
			if b.Seller != "" {
				brand = "خوش آمدید."
			}
			sb.WriteString(brand + "\nاینترنت بدون قطعی" + b.countriesPhrase(ctx) + "، با یک لینک برای همهٔ دستگاه‌ها.\n")
			if b.Store.TrialPolicyFor(ctx, b.Seller).Enabled && !b.Store.TrialUsed(ctx, b.Seller, chat) {
				sb.WriteString("اول رایگان امتحان کنید، بعد تصمیم بگیرید.\n")
			}
		}
	}
	if bal := b.Store.TomanBalance(ctx, b.Seller, chat); bal > 0 {
		sb.WriteString("👛 کیف پول: <b>" + faMoney(bal) + " تومان</b>\n")
	}
	b.sendMenu(ctx, chat, strings.TrimRight(sb.String(), "\n"), b.menuKB(ctx, chat, len(views), ended))
}

// countriesPhrase is " روی N کشور" when several servers are up.
func (b *Bot) countriesPhrase(ctx context.Context) string {
	nodes, err := b.Store.Nodes(ctx)
	if err != nil {
		return ""
	}
	n, now := 0, b.now().Unix()
	for _, x := range nodes {
		if now-x.LastSeen < 180 {
			n++
		}
	}
	if n < 2 {
		return ""
	}
	return " روی <b>" + faInt(int64(n)) + " کشور</b>"
}

// sendMenu sends the menu under its banner: the seller's own picture when
// one was chosen, the built-in one in the main bot, none in a reseller's
// bot or when the seller turned it off. Anything a caption cannot carry
// goes out as plain text.
func (b *Bot) sendMenu(ctx context.Context, chat int64, text string, kb *Keyboard) {
	up, ok := b.API.(photoUploader)
	mode := b.Store.BotLook(ctx, b.Seller).Banner
	if !ok || mode == "none" || utf8.RuneCountInString(text) > 900 {
		b.send(ctx, chat, text, kb)
		return
	}
	img, name := bannerJPG, "dark-vpn.jpg"
	id := ""
	save := func(id string) { _ = b.Store.SetSetting(ctx, bannerKey, id) }
	if mode == "custom" {
		var has bool
		if img, id, has = b.Store.BotBanner(ctx, b.Seller); !has {
			b.send(ctx, chat, text, kb)
			return
		}
		name = "banner.jpg"
		save = func(id string) { b.Store.SetBotBannerFileID(ctx, b.Seller, id) }
	} else {
		if b.Seller != "" || len(bannerJPG) == 0 {
			b.send(ctx, chat, text, kb)
			return
		}
		id = b.Store.Setting(ctx, bannerKey)
	}
	if id != "" {
		if err := b.API.SendPhotoID(ctx, chat, id, text, kb); err == nil {
			return
		} else if isBlocked(err) {
			return
		}
		save("") // the stored id went stale: upload again
	}
	id, err := up.UploadPhoto(ctx, chat, name, img, text, kb)
	if err != nil {
		b.logf("tgbot banner: %v", err)
		b.send(ctx, chat, text, kb)
		return
	}
	if id != "" {
		save(id)
	}
}

// menuButton is a button of the main menu with the key its seller can
// restyle it under.
type menuButton struct {
	key  string
	btn  Button
	wide bool
}

// layoutMenu applies the seller's look (order, names, colours, hidden,
// width) to the buttons this customer gets, and lays them out: a wide button
// takes a row, the others go two to a row.
func layoutMenu(list []menuButton, look hubcore.BotLook) [][]Button {
	if len(look.Buttons) > 0 {
		at := map[string]int{}
		for i, lb := range look.Buttons {
			at[lb.Key] = i
		}
		var first, rest []menuButton
		slots := make([]*menuButton, len(look.Buttons))
		for i := range list {
			m := list[i]
			n, ok := at[m.key]
			if !ok {
				rest = append(rest, m) // a button newer than the saved look: after the others
				continue
			}
			lb := look.Buttons[n]
			if lb.Hidden {
				continue
			}
			if lb.Label != "" {
				m.btn.Text = lb.Label
			}
			m.btn.Style, m.wide = lb.Style, lb.Wide
			mm := m
			slots[n] = &mm
		}
		for _, m := range slots {
			if m != nil {
				first = append(first, *m)
			}
		}
		list = append(first, rest...)
	}
	var rows [][]Button
	var pending []Button
	flush := func() {
		if len(pending) > 0 {
			rows = append(rows, pending)
			pending = nil
		}
	}
	for _, m := range list {
		if m.wide {
			flush()
			rows = append(rows, row(m.btn))
			continue
		}
		pending = append(pending, m.btn)
		if len(pending) == 2 {
			flush()
		}
	}
	flush()
	return rows
}

// menuKB is the main menu's buttons. services is how many services the chat
// has, ended the ones that ran out. The Mini App store is not repeated here
// (its button sits beside the message box) and support lives on the bottom
// keyboard.
func (b *Bot) menuKB(ctx context.Context, chat int64, services int, ended []string) *Keyboard {
	var list []menuButton
	add := func(key string, btn Button, wide bool) { list = append(list, menuButton{key, btn, wide}) }
	trial := b.Store.TrialPolicyFor(ctx, b.Seller).Enabled && !b.Store.TrialUsed(ctx, b.Seller, chat)
	switch {
	case services == 0:
		if trial {
			add("trial", cbBtn("🎁 تست رایگان", "trial"), true)
		}
		add("shop", cbBtn("🛍 خرید اشتراک", "shop"), true)
	default:
		if len(ended) == 1 && len("rnw:"+ended[0]) <= 64 {
			add("renew", cbBtn("🔄 تمدید سرویس", "rnw:"+ended[0]), true)
		} else if len(ended) > 0 {
			add("renew", cbBtn("🔄 تمدید سرویس‌ها", "mine"), true)
		}
		mine := "📦 سرویس من"
		if services > 1 {
			mine = "📦 سرویس‌های من"
		}
		add("mine", cbBtn(mine, "mine"), false)
		add("shop", cbBtn("🛍 خرید اشتراک", "shop"), false)
		if trial {
			add("trial", cbBtn("🎁 تست رایگان", "trial"), false)
		}
	}
	// The wallet is always there when it can be used (to top it up), not
	// only once it holds something.
	if b.Seller != "" || b.cardOn(ctx) || b.Store.WalletBalance(ctx, chat) > 0 || b.Store.TomanBalance(ctx, b.Seller, chat) > 0 {
		add("wallet", cbBtn("👛 کیف پول", "wallet"), false)
	}
	add("guide", cbBtn("📚 آموزش اتصال", "guide"), false)
	add("status", cbBtn("🌐 وضعیت سرورها", "status"), false)
	// Inviting friends is the owner's shop's; resellers' bots do not have it.
	if b.Seller == "" && b.Store.ReferralPolicy(ctx).Enabled {
		add("invite", cbBtn("👥 دعوت دوستان", "invite"), false)
	}
	add("coupon", cbBtn("🎟 کد تخفیف", "cpn:"), false)
	if b.wheelOn(ctx) {
		add("wheel", cbBtn("🎡 گردونهٔ شانس", "spin"), false)
	}
	add("me", cbBtn("👤 حساب من", "me"), false)
	if btn, ok := b.repSaleButton(ctx, chat); ok {
		add("panel", btn, false)
	}
	list = append(list, b.growButtons(ctx, chat)...) // trial panel, reseller request (repgrow.go)
	add("link", cbBtn("🔗 افزودن با لینک", "link"), false)
	rows := layoutMenu(list, b.Store.BotLook(ctx, b.Seller))
	if b.Owners[chat] {
		// The owner is only looking at the customer's view: one tap back.
		rows = append(rows, row(cbBtn("🛡 بازگشت به پنل مدیریت", "a:home")))
	} else if rep, _ := b.repOf(ctx, chat); rep != "" {
		// A reseller in customer mode: the way back never depends on the
		// bottom keyboard, which Telegram drops when the chat is cleared.
		rows = append(rows, row(repBtn("🧑‍💼 بازگشت به حالت نماینده", "mode")))
	}
	return &Keyboard{Rows: rows}
}
