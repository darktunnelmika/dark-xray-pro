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

// leftBar shows what is left in coloured squares: green, then yellow under
// a quarter, red under a tenth.
func leftBar(left float64) string {
	if left < 0 {
		left = 0
	}
	if left > 1 {
		left = 1
	}
	n := int(left*8 + 0.5)
	if n == 0 && left > 0 {
		n = 1
	}
	sq := "🟩"
	switch {
	case left < 0.1:
		sq = "🟥"
	case left < 0.25:
		sq = "🟨"
	}
	return strings.Repeat(sq, n) + strings.Repeat("⬜", 8-n)
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
	st := b.state(c)
	fmt.Fprintf(&sb, "%s <b>%s</b>\n", st.icon, esc(b.planTitle(ctx, c)))
	if showName { // several services: the technical name tells them apart, on its own line
		fmt.Fprintf(&sb, "<code>%s</code>\n", esc(shortName(c.Email)))
	}
	used := c.Usage.Up + c.Usage.Down
	switch {
	case !c.Enabled:
		sb.WriteString("این سرویس غیرفعال است.")
		return sb.String(), false
	case c.Remaining == 0:
		sb.WriteString(leftBar(0) + "\n🔴 حجم تمام شد؛ با تمدید، همین لینک دوباره فعال می‌شود.")
		return sb.String(), true
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
		sb.WriteString("🔴 زمان تمام شد؛ با تمدید، همین لینک دوباره فعال می‌شود.")
		return sb.String(), true
	}
	vol := "♾ <b>نامحدود</b>"
	if c.VolumeBytes > 0 {
		left := max(c.VolumeBytes-used, 0)
		share := float64(left) / float64(c.VolumeBytes)
		fmt.Fprintf(&sb, "%s  %s٪\n", leftBar(share), faInt(int64(share*100+0.5)))
		vol = fmt.Sprintf("💾 <b>%s</b> از %s", sizeShort(left), sizeShort(c.VolumeBytes))
	}
	var when string
	switch {
	case c.EffectiveUntil > now:
		left := c.EffectiveUntil - now
		if left >= 86400 {
			when = "⏳ <b>" + faInt((left+86399)/86400) + " روز</b>"
		} else {
			when = "⏳ <b>" + faInt((left+3599)/3600) + " ساعت</b>"
		}
	case c.DurationSeconds > 0 && c.Usage.FirstSeen == 0:
		when = "🆕 " + faInt(c.DurationSeconds/86400) + " روز از اولین اتصال"
	default:
		when = "⏳ بی‌زمان"
	}
	sb.WriteString(vol + "  ·  " + when)
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
	if brand := b.brandTitle(ctx); brand != "" {
		sb.WriteString("✨ <b>" + esc(brand) + "</b>\n")
	}
	emails, _ := b.clients(ctx, chat)
	var views []hubcore.ClientView
	for _, e := range emails {
		if c, err := b.Store.ClientByEmail(ctx, e); err == nil {
			views = append(views, c)
		}
	}
	hello := "سلام"
	if name := b.Store.TgName(ctx, chat); name != "" {
		hello += " <b>" + esc(name) + "</b>"
	}
	hello += " 👋"
	if len(views) == 0 {
		hello += " خوش آمدید!"
	}
	sb.WriteString(hello + "\n")
	if len(views) > 0 {
		sb.WriteString(line + "\n" + b.menuSummary(views) + "\n")
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
			sb.WriteString(line + "\n" + esc(w) + "\n")
		} else {
			sb.WriteString(line + "\n🚀 اینترنت آزاد و پرسرعت" + b.countriesPhrase(ctx) + "، با یک لینک برای همهٔ دستگاه‌ها.\n")
			sb.WriteString("⚡️ تحویل خودکار  ·  🔄 تمدید بدون عوض شدن لینک  ·  💬 پشتیبانی\n")
			if b.Store.TrialPolicyFor(ctx, b.Seller).Enabled && !b.Store.TrialUsed(ctx, b.Seller, chat) {
				sb.WriteString("\n🎁 <b>اول رایگان امتحان کنید</b>، بعد تصمیم بگیرید.\n")
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
	styled := func(btn Button, st string) Button { btn.Style = st; return btn }
	look := b.Store.BotLook(ctx, b.Seller)
	custom := len(look.Buttons) > 0 // the seller arranged the menu itself: every button stays where it put it
	trial := b.Store.TrialPolicyFor(ctx, b.Seller).Enabled && !b.Store.TrialUsed(ctx, b.Seller, chat)
	// The Mini App first, wide: the whole store and the services in one tap.
	if u := b.miniURL(ctx); strings.HasPrefix(u, "https://") {
		add("app", Button{Text: "📱 باز کردن اپ", WebApp: &WebApp{URL: u}, Style: "success"}, true)
	}
	switch {
	case services == 0:
		if trial {
			add("trial", styled(cbBtn("🎁 تست رایگان", "trial"), "primary"), true)
		}
		add("shop", styled(cbBtn("🛍 خرید اشتراک", "shop"), "success"), true)
	default:
		if len(ended) == 1 && len("rnw:"+ended[0]) <= 64 {
			add("renew", styled(cbBtn("🔄 تمدید سرویس", "rnw:"+ended[0]), "success"), true)
		} else if len(ended) > 0 {
			add("renew", styled(cbBtn("🔄 تمدید سرویس‌ها", "mine"), "success"), true)
		}
		mine := "📦 سرویس من"
		if services > 1 {
			mine = "📦 سرویس‌های من"
		}
		add("mine", styled(cbBtn(mine, "mine"), "primary"), false)
		add("shop", styled(cbBtn("🛍 خرید اشتراک", "shop"), "success"), false)
		if trial {
			add("trial", styled(cbBtn("🎁 تست رایگان", "trial"), "primary"), false)
		}
	}
	// The wallet is always there when it can be used (to top it up), not
	// only once it holds something.
	if b.Seller != "" || b.cardOn(ctx) || b.Store.WalletBalance(ctx, chat) > 0 || b.Store.TomanBalance(ctx, b.Seller, chat) > 0 {
		add("wallet", cbBtn("👛 کیف پول", "wallet"), false)
	}
	if b.wheelOn(ctx) {
		add("wheel", styled(cbBtn("🎡 گردونهٔ شانس", "spin"), "primary"), false)
	}
	add("guide", cbBtn("📚 آموزش اتصال", "guide"), false)
	add("support", cbBtn("💬 پشتیبانی", "support"), false)
	add("me", cbBtn("👤 حساب من", "me"), false)
	if btn, ok := b.repSaleButton(ctx, chat); ok {
		add("panel", btn, false)
	}
	list = append(list, b.growButtons(ctx, chat)...) // trial panel, reseller request (repgrow.go)
	if custom {
		add("status", cbBtn("🌐 وضعیت سرورها", "status"), false)
		if b.Seller == "" && b.Store.ReferralPolicy(ctx).Enabled {
			add("invite", cbBtn("👥 دعوت دوستان", "invite"), false)
		}
		add("coupon", cbBtn("🎟 کد تخفیف", "cpn:"), false)
		add("link", cbBtn("🔗 افزودن با لینک", "link"), false)
	} else {
		// The less used ones wait behind one button (moreMenu).
		add("more", cbBtn("➕ بیشتر", "more"), false)
	}
	rows := layoutMenu(list, look)
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

// brandTitle is the store's name over the menu: the reseller's brand in its
// bot (nothing when it set none), else the panel's, else Dark VPN.
func (b *Bot) brandTitle(ctx context.Context) string {
	ss := b.Store.SubSettings(ctx)
	if b.Seller != "" {
		return b.Store.RepBrand(ctx, b.Seller).Title
	}
	if ss.Title != "" {
		return ss.Title
	}
	return "Dark VPN"
}

// menuSummary is one line over the cards: how many services, what is left
// in them and the nearest end.
func (b *Bot) menuSummary(views []hubcore.ClientView) string {
	now := b.now().Unix()
	live, unl := 0, false
	var left int64
	soon := int64(-1)
	for _, c := range views {
		if !c.Enabled || c.Remaining == 0 || (c.EffectiveUntil > 0 && c.EffectiveUntil <= now) {
			continue
		}
		live++
		if c.VolumeBytes < 0 {
			unl = true
		} else if c.VolumeBytes > 0 {
			left += max(c.VolumeBytes-c.Usage.Up-c.Usage.Down, 0)
		}
		if c.EffectiveUntil > now {
			if d := (c.EffectiveUntil - now + 86399) / 86400; soon < 0 || d < soon {
				soon = d
			}
		}
	}
	if live == 0 {
		return "📊 سرویس فعالی ندارید؛ برای ادامه، تمدید یا خرید کنید."
	}
	parts := []string{fmt.Sprintf("📊 <b>%s</b> سرویس فعال", faInt(int64(live)))}
	switch {
	case unl:
		parts = append(parts, "♾ حجم نامحدود")
	case left > 0:
		parts = append(parts, "<b>"+sizeShort(left)+"</b> مانده")
	}
	if soon >= 0 {
		w := faInt(soon) + " روز"
		if soon <= 3 {
			w = "⚠️ " + w
		}
		parts = append(parts, "نزدیک‌ترین پایان: "+w)
	}
	return strings.Join(parts, "  ·  ")
}

// moreMenu holds the menu's less used buttons.
func (b *Bot) moreMenu(ctx context.Context, chat int64) {
	kb := &Keyboard{}
	kb.Rows = append(kb.Rows, row(cbBtn("🌐 وضعیت سرورها", "status"), cbBtn("🎟 کد تخفیف", "cpn:")))
	r := []Button{cbBtn("🔗 افزودن با لینک", "link")}
	if b.Seller == "" && b.Store.ReferralPolicy(ctx).Enabled {
		r = append(r, cbBtn("👥 دعوت دوستان", "invite"))
	}
	kb.Rows = append(kb.Rows, r, row(cbBtn("« منوی اصلی", "home")))
	b.send(ctx, chat, "➕ <b>بیشتر</b>\n"+line+"\n🌐 وضعیت سرورها: کدام کشورها الان آنلاین‌اند\n🎟 کد تخفیف: اگر کد دارید، اینجا بزنید\n🔗 افزودن با لینک: سرویسی که از جای دیگر گرفته‌اید را به این حساب وصل کنید", kb)
}
