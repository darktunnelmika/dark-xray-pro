package tgbot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Look and feel: one screen per chat. A button press redraws the message
// it sits on (editMessageText) instead of piling new messages up; replies
// to typed input, alerts and files are still new messages.

// editor is the optional part of the Bot API that redraws a message.
type editor interface {
	EditText(ctx context.Context, chat, msgID int64, text string, kb *Keyboard) error
}

type screenKey struct{}

type screen struct {
	chat, msgID int64
	used        bool
}

// onScreen marks ctx so the first send to this chat redraws msgID.
func onScreen(ctx context.Context, q *CallbackQuery) context.Context {
	if q.Message == nil || len(q.Message.Photo) > 0 || q.Message.Text == "" || q.Message.Chat.ID != q.From.ID {
		return ctx // a group's message is not this user's screen
	}
	return context.WithValue(ctx, screenKey{}, &screen{chat: q.From.ID, msgID: q.Message.ID})
}

// redraw edits the screen message when ctx carries one for chat; ok is
// false when a new message must be sent instead.
func (b *Bot) redraw(ctx context.Context, chat int64, text string, kb *Keyboard) (int64, bool) {
	sc, _ := ctx.Value(screenKey{}).(*screen)
	ed, isEd := b.API.(editor)
	if sc == nil || !isEd || sc.used || sc.chat != chat {
		return 0, false
	}
	sc.used = true
	err := ed.EditText(ctx, chat, sc.msgID, text, kb)
	if err == nil || errors.Is(err, errNotModified) {
		return sc.msgID, true
	}
	return 0, false
}

var errNotModified = errors.New("telegram: message is not modified")

// ---------- small visual helpers ----------

// bar draws a 10-step progress bar for a share between 0 and 1.
func bar(share float64) string {
	if share < 0 {
		share = 0
	}
	if share > 1 {
		share = 1
	}
	n := int(share*10 + 0.5)
	return strings.Repeat("▰", n) + strings.Repeat("▱", 10-n)
}

// size shows bytes in Persian: "۱٫۲۵ گیگ", "۸۴۰ مگ".
func size(n int64) string {
	switch {
	case n < 0:
		return "نامحدود"
	case n >= 1<<30:
		v := float64(n) / (1 << 30)
		if v >= 100 {
			return faNum(strconv.FormatFloat(v, 'f', 0, 64)) + " گیگ"
		}
		return strings.ReplaceAll(faNum(strconv.FormatFloat(v, 'f', 2, 64)), ".", "٫") + " گیگ"
	case n >= 1<<20:
		return faNum(strconv.FormatInt(n>>20, 10)) + " مگ"
	default:
		return faNum(strconv.FormatInt(n>>10, 10)) + " کیلوبایت"
	}
}

func faInt(n int64) string { return faNum(strconv.FormatInt(n, 10)) }

// faMoney: "۱۵۰٬۰۰۰".
func faMoney(n int64) string { return strings.ReplaceAll(faNum(money(n)), ",", "٬") }

// faDate: "۱۴۰۵/۰۹/۱۵" (Tehran).
func faDate(u int64) string {
	t := time.Unix(u, 0).In(tehran)
	y, m, d := gregorianToJalali(t.Year(), int(t.Month()), t.Day())
	return faNum(fmt.Sprintf("%d/%02d/%02d", y, m, d))
}

func row(btns ...Button) []Button    { return btns }
func cbBtn(text, data string) Button { return Button{Text: text, Data: data} }
func urlBtn(text, u string) Button   { return Button{Text: text, URL: u} }

const line = "┄┄┄┄┄┄┄┄┄┄┄┄┄┄"

// ---------- service card ----------

type svcState struct {
	icon, label string
	active      bool
}

func (b *Bot) state(c hubcore.ClientView) svcState {
	now := b.now().Unix()
	switch {
	case !c.Enabled:
		return svcState{"⛔", "غیرفعال", false}
	case c.Remaining == 0:
		return svcState{"🪫", "حجم تمام شده", false}
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
		return svcState{"⌛", "منقضی شده", false}
	case c.EffectiveUntil == 0 && c.DurationSeconds > 0 && c.Usage.FirstSeen == 0:
		return svcState{"🆕", "آماده؛ هنوز وصل نشده", true}
	case c.Online:
		return svcState{"🟢", "فعال · آنلاین", true}
	}
	return svcState{"✅", "فعال", true}
}

// serviceCard draws a client as a card with usage and time bars.
func (b *Bot) serviceCard(c hubcore.ClientView) string {
	now := b.now().Unix()
	st := b.state(c)
	used := c.Usage.Up + c.Usage.Down
	var sb strings.Builder
	fmt.Fprintf(&sb, "💠 <b>%s</b>\n%s\n", esc(c.Email), line)
	fmt.Fprintf(&sb, "%s وضعیت: <b>%s</b>\n\n", st.icon, st.label)
	if c.VolumeBytes > 0 {
		share := float64(used) / float64(c.VolumeBytes)
		left := c.VolumeBytes - used
		if left < 0 {
			left = 0
		}
		fmt.Fprintf(&sb, "📶 حجم  <code>%s</code> %s٪\n", bar(share), faInt(int64(share*100+0.5)))
		fmt.Fprintf(&sb, "      %s مصرف از %s · <b>%s مانده</b>\n\n", size(used), size(c.VolumeBytes), size(left))
	} else {
		fmt.Fprintf(&sb, "📶 حجم: <b>نامحدود</b> · %s مصرف شده\n\n", size(used))
	}
	switch {
	case c.EffectiveUntil > now:
		left := (c.EffectiveUntil - now + 86399) / 86400
		total := c.DurationSeconds / 86400
		if total <= 0 || total < left {
			total = left
		}
		share := 1 - float64(left)/float64(total)
		fmt.Fprintf(&sb, "⏳ زمان  <code>%s</code>\n", bar(share))
		fmt.Fprintf(&sb, "      <b>%s روز مانده</b> · تا %s\n", faInt(left), faDate(c.EffectiveUntil))
	case c.EffectiveUntil == 0 && c.DurationSeconds > 0:
		fmt.Fprintf(&sb, "⏳ زمان: <b>%s روز</b> از اولین اتصال\n", faInt(c.DurationSeconds/86400))
	case c.EffectiveUntil > 0:
		fmt.Fprintf(&sb, "⏳ زمان: <b>تمام شده</b> (%s)\n", faDate(c.EffectiveUntil))
	default:
		sb.WriteString("⏳ زمان: <b>بدون انقضا</b>\n")
	}
	if c.MaxIPs > 0 {
		fmt.Fprintf(&sb, "📱 دستگاه همزمان: %s\n", faInt(int64(c.MaxIPs)))
	}
	return sb.String()
}

// serviceKB is the customer's actions on one service.
func (b *Bot) serviceKB(ctx context.Context, chat int64, c hubcore.ClientView, many bool) *Keyboard {
	e := c.Email
	kb := &Keyboard{Rows: [][]Button{
		row(cbBtn("🔗 لینک اشتراک", "subl:"+e), cbBtn("📷 QR کد", "qr:"+e)),
		row(cbBtn("🧩 کانفیگ‌ها", "cfg:"+e), cbBtn("📈 مصرف ۷ روز", "use:"+e)),
	}}
	if len(b.renewProducts(ctx)) > 0 {
		kb.Rows = append(kb.Rows, row(cbBtn("♻️ تمدید / افزایش حجم", "rnw:"+e)))
	}
	if len(b.starsPlans(ctx)) > 0 {
		label := "🔁 تمدید خودکار: خاموش"
		if b.Store.AutoRenew(ctx, e) != "" {
			label = "🔁 تمدید خودکار: روشن ✅"
		}
		kb.Rows = append(kb.Rows, row(cbBtn(label, "ar:"+e)))
	}
	nav := row(cbBtn("🔄 بروزرسانی", "svc:"+e))
	if many {
		nav = append(nav, cbBtn("« سرویس‌ها", "mine"))
	} else {
		nav = append(nav, cbBtn("« منو", "home"))
	}
	kb.Rows = append(kb.Rows, nav)
	return kb
}

func (b *Bot) renewProducts(ctx context.Context) []hubcore.Product {
	all, _ := b.Store.Products(ctx, b.owner(), true)
	var out []hubcore.Product
	for _, p := range all {
		if b.sells(p) && !p.NoRenew {
			out = append(out, p)
		}
	}
	return out
}

func (b *Bot) showService(ctx context.Context, chat int64, email string) {
	c, err := b.Store.ClientByEmail(ctx, email)
	if err != nil {
		b.send(ctx, chat, "این سرویس پیدا نشد.", homeKB)
		return
	}
	list, _ := b.clients(ctx, chat)
	b.send(ctx, chat, b.serviceCard(c), b.serviceKB(ctx, chat, c, len(list) > 1))
}

var homeKB = &Keyboard{Rows: [][]Button{row(cbBtn("« منوی اصلی", "home"))}}

func backTo(data, text string) *Keyboard {
	return &Keyboard{Rows: [][]Button{row(cbBtn("« "+text, data))}}
}

// subURL is the client's subscription address.
func (b *Bot) subURL(c hubcore.ClientView) string {
	base := b.SubBase
	if c.RepID != "" {
		base = b.Store.SubBaseFor(context.Background(), c.Email, base) // the reseller's own address
	}
	return strings.TrimRight(base, "/") + "/sub/" + c.SubToken
}

func (b *Bot) showSubLink(ctx context.Context, chat int64, email string) {
	c, err := b.Store.ClientByEmail(ctx, email)
	if err != nil {
		return
	}
	u := b.subURL(c)
	text := fmt.Sprintf("🔗 <b>لینک اشتراک %s</b>\n%s\nروی لینک بزنید تا کپی شود، بعد در برنامه «افزودن از کلیپ‌بورد» را بزنید:\n\n<code>%s</code>\n\n💡 این لینک همهٔ سرورها را دارد و با «Update» در برنامه، سرورهای جدید هم اضافه می‌شوند.",
		esc(email), line, esc(u))
	kb := &Keyboard{Rows: [][]Button{
		row(urlBtn("🌐 صفحهٔ اشتراک", u), cbBtn("📷 QR کد", "qr:"+email)),
		row(cbBtn("📚 آموزش اتصال", "guide"), cbBtn("« سرویس", "svc:"+email)),
	}}
	b.send(ctx, chat, text, kb)
}

func (b *Bot) sendQR(ctx context.Context, chat int64, email string) {
	c, err := b.Store.ClientByEmail(ctx, email)
	if err != nil {
		return
	}
	u := b.subURL(c)
	png, err := qrcode.Encode(u, qrcode.Medium, 640)
	if err != nil {
		return
	}
	if err := b.API.SendPhoto(ctx, chat, png, "📷 <b>"+esc(email)+"</b>\nاین QR را در برنامه با «Scan QR» اسکن کنید."); err != nil {
		b.logf("tgbot photo: %v", err)
	}
}

// showConfigs lists each config of the service, ready to copy; AmneziaWG
// configs come as .conf files.
func (b *Bot) showConfigs(ctx context.Context, chat int64, email string) {
	links, err := b.Store.ClientLinks(ctx, email)
	if err != nil {
		return
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "🧩 <b>کانفیگ‌های %s</b>\n%s\nروی هر کانفیگ بزنید تا کپی شود:\n", esc(email), line)
	n, files := 0, 0
	for _, l := range links {
		if l.Kind == "awg" {
			files++
			name := strings.Map(func(r rune) rune {
				if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
					return r
				}
				return '-'
			}, l.Name)
			if err := b.API.SendDocument(ctx, chat, strings.Trim(name, "-")+".conf", []byte(l.Link), "🛡 AmneziaWG — "+esc(l.Name)+"\nفایل را در برنامهٔ AmneziaWG یا AmneziaVPN باز کنید."); err != nil {
				b.logf("tgbot conf: %v", err)
			}
			continue
		}
		if sb.Len() > 3300 {
			sb.WriteString("\n… بقیه در لینک اشتراک هست.")
			break
		}
		n++
		fmt.Fprintf(&sb, "\n%s <b>%s</b>\n<code>%s</code>\n", protoIcon(l.Kind), esc(l.Name), esc(l.Link))
	}
	if n == 0 && files == 0 {
		sb.WriteString("\nکانفیگی برای این سرویس نیست.")
	}
	if files > 0 {
		fmt.Fprintf(&sb, "\n🛡 %s فایل AmneziaWG جداگانه فرستاده شد.", faInt(int64(files)))
	}
	b.send(ctx, chat, sb.String(), backTo("svc:"+email, "سرویس"))
}

func protoIcon(kind string) string {
	switch kind {
	case "vless":
		return "🟣"
	case "vmess":
		return "🔵"
	case "trojan":
		return "🟠"
	case "ss":
		return "🟡"
	case "hysteria2", "tuic":
		return "⚡"
	case "wireguard":
		return "🛡"
	}
	return "▫️"
}

// showUsage draws the last 7 days as bars.
func (b *Bot) showUsage(ctx context.Context, chat int64, email string) {
	days, err := b.Store.ClientDailyUsage(ctx, email, 7)
	if err != nil {
		return
	}
	var peak, total int64
	for _, d := range days {
		total += d.Up + d.Down
		if d.Up+d.Down > peak {
			peak = d.Up + d.Down
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "📈 <b>مصرف ۷ روز اخیر — %s</b>\n%s\n", esc(email), line)
	for _, d := range days {
		t, _ := time.Parse("2006-01-02", d.Day)
		_, m, dd := gregorianToJalali(t.Year(), int(t.Month()), t.Day())
		v := d.Up + d.Down
		w := 0
		if peak > 0 {
			w = int(float64(v) / float64(peak) * 12)
		}
		if v > 0 && w == 0 {
			w = 1
		}
		fmt.Fprintf(&sb, "<code>%s %s</code> %s\n", faNum(fmt.Sprintf("%02d/%02d", m, dd)), strings.Repeat("█", w)+strings.Repeat("░", 12-w), size(v))
	}
	fmt.Fprintf(&sb, "\nجمع هفته: <b>%s</b>", size(total))
	b.send(ctx, chat, sb.String(), backTo("svc:"+email, "سرویس"))
}

// showRenew lists products for renewing one service.
func (b *Bot) showRenew(ctx context.Context, chat int64, email string) {
	kb := &Keyboard{Rows: b.renewButtons(ctx, chat, email)}
	kb.Rows = append(kb.Rows, row(cbBtn("« سرویس", "svc:"+email)))
	b.send(ctx, chat, "♻️ <b>تمدید "+esc(email)+"</b>\n"+line+"\nیکی از بسته‌ها را انتخاب کنید؛ حجم و زمان به سرویس فعلی اضافه می‌شود و لینک عوض نمی‌شود.", kb)
}

// ---------- shop ----------

func productLine(p hubcore.Product) string {
	sz := "نامحدود"
	if p.Plan == "volume" {
		sz = faInt(p.GB) + " گیگ"
	}
	return fmt.Sprintf("%s · %s روز", sz, faInt(p.Days))
}

func productPrice(p hubcore.Product) string {
	var parts []string
	if p.Price > 0 {
		parts = append(parts, faMoney(p.Price)+" تومان")
	}
	if p.PriceStars > 0 {
		parts = append(parts, faInt(p.PriceStars)+" ⭐")
	}
	if len(parts) == 0 {
		return "رایگان"
	}
	return strings.Join(parts, " یا ")
}

func (b *Bot) showProduct(ctx context.Context, chat int64, id string) {
	for _, p := range b.renewProducts(ctx) {
		if p.ID != id {
			continue
		}
		price := "<b>" + productPrice(p) + "</b>"
		if d, ok := b.Store.HeldDiscount(ctx, hubcore.TgBuyer(chat), p.ID); ok {
			np, ns := d.Discounted(p.Price, p.PriceStars)
			q := p
			q.Price, q.PriceStars = np, ns
			if d.Percent == 0 {
				q.PriceStars = p.PriceStars
			}
			price = "<s>" + productPrice(p) + "</s> ← <b>" + productPrice(q) + "</b> 🎟 " + esc(d.Code)
		}
		text := fmt.Sprintf("📦 <b>%s</b>\n%s\n📶 حجم: <b>%s</b>\n⏳ مدت: <b>%s روز</b>\n💳 قیمت: %s\n\n✅ همهٔ سرورها · ⚡ تحویل خودکار · 🔗 یک لینک برای همه",
			esc(p.Name), line, map[bool]string{true: faInt(p.GB) + " گیگ", false: "نامحدود"}[p.Plan == "volume"], faInt(p.Days), price)
		kb := &Keyboard{Rows: b.buyButtons(ctx, chat, p, "")}
		kb.Rows = append(kb.Rows, row(cbBtn("🎟 کد تخفیف دارم", "cpn:"+p.ID), cbBtn("« بسته‌ها", "shop")))
		b.send(ctx, chat, text, kb)
		return
	}
	b.send(ctx, chat, "این بسته دیگر فروخته نمی‌شود.", backTo("shop", "بسته‌ها"))
}

// ---------- guide ----------

type app struct{ name, url string }

var guides = map[string]struct {
	title string
	apps  []app
	steps string
}{
	"android": {"🤖 اندروید", []app{{"v2rayNG", "https://github.com/2dust/v2rayNG/releases/latest"}, {"Hiddify", "https://play.google.com/store/apps/details?id=app.hiddify.com"}},
		"۱. برنامه را نصب و باز کنید.\n۲. لینک اشتراک را از «سرویس‌های من» کپی کنید.\n۳. v2rayNG: دکمهٔ ➕ ← <i>Import config from Clipboard</i>\n   Hiddify: دکمهٔ ➕ ← <i>Add from clipboard</i>\n۴. یک سرور انتخاب و دکمهٔ اتصال را بزنید.\n\n💡 برای گرفتن سرورهای جدید: منو ← <i>Update subscription</i>"},
	"ios": {"🍎 آیفون و آیپد", []app{{"Streisand", "https://apps.apple.com/app/streisand/id6450534064"}, {"V2Box", "https://apps.apple.com/app/id6446814690"}},
		"۱. برنامه را از اپ‌استور نصب کنید.\n۲. لینک اشتراک را کپی کنید.\n۳. Streisand: ➕ ← <i>Add from clipboard</i>\n   V2Box: Configs ← ➕ ← <i>Import from clipboard</i>\n۴. اجازهٔ افزودن VPN را بدهید و وصل شوید."},
	"windows": {"🪟 ویندوز", []app{{"Hiddify", "https://github.com/hiddify/hiddify-app/releases/latest"}, {"v2rayN", "https://github.com/2dust/v2rayN/releases/latest"}},
		"۱. نسخهٔ ویندوز را دانلود و نصب کنید.\n۲. لینک اشتراک را کپی کنید.\n۳. Hiddify: ➕ ← <i>Add from clipboard</i>\n   v2rayN: Subscription ← Add، لینک را بگذارید و Update بزنید.\n۴. حالت <i>VPN/TUN</i> را روشن و وصل شوید."},
	"mac": {"💻 مک", []app{{"Hiddify", "https://github.com/hiddify/hiddify-app/releases/latest"}, {"Streisand (M1+)", "https://apps.apple.com/app/streisand/id6450534064"}},
		"۱. برنامه را نصب کنید.\n۲. لینک اشتراک را کپی کنید.\n۳. ➕ ← <i>Add from clipboard</i>\n۴. وصل شوید."},
}

var guideOrder = []string{"android", "ios", "windows", "mac"}

func (b *Bot) showGuide(ctx context.Context, chat int64, os string) {
	if custom := b.Store.Setting(ctx, "tg_guide"); custom != "" && os == "" {
		b.send(ctx, chat, esc(custom), homeKB)
		return
	}
	g, ok := guides[os]
	if !ok {
		kb := &Keyboard{Rows: [][]Button{
			row(cbBtn(guides["android"].title, "guide:android"), cbBtn(guides["ios"].title, "guide:ios")),
			row(cbBtn(guides["windows"].title, "guide:windows"), cbBtn(guides["mac"].title, "guide:mac")),
			row(cbBtn("« منوی اصلی", "home")),
		}}
		b.send(ctx, chat, "📚 <b>آموزش اتصال</b>\n"+line+"\nدستگاه خود را انتخاب کنید تا برنامهٔ مناسب و قدم‌های اتصال را ببینید:", kb)
		return
	}
	kb := &Keyboard{}
	var r []Button
	for _, a := range g.apps {
		r = append(r, urlBtn("⬇️ "+a.name, a.url))
	}
	kb.Rows = append(kb.Rows, r, row(cbBtn("📦 سرویس‌های من", "mine"), cbBtn("« دستگاه‌ها", "guide")))
	b.send(ctx, chat, fmt.Sprintf("%s\n%s\n%s", "<b>"+g.title+"</b>", line, g.steps), kb)
}

// ---------- server status (customers) ----------

func (b *Bot) showStatus(ctx context.Context, chat int64) {
	nodes, _ := b.Store.Nodes(ctx)
	now := b.now()
	var sb strings.Builder
	sb.WriteString("🌐 <b>وضعیت سرورها</b>\n" + line + "\n")
	up := 0
	for _, n := range nodes {
		name := n.Name
		if name == "" {
			name = n.ID
		}
		if n.Online(now) {
			up++
			load := ""
			if n.Host != nil {
				switch {
				case n.Host.CPUPercent >= 85:
					load = " · شلوغ"
				case n.Host.CPUPercent >= 55:
					load = " · متوسط"
				default:
					load = " · روان"
				}
			}
			fmt.Fprintf(&sb, "🟢 %s%s\n", esc(name), load)
		} else {
			fmt.Fprintf(&sb, "🔴 %s · در دسترس نیست\n", esc(name))
		}
	}
	if len(nodes) == 0 {
		sb.WriteString("سروری ثبت نشده.\n")
	}
	fmt.Fprintf(&sb, "\n%s از %s سرور فعال · به‌روز: %s", faInt(int64(up)), faInt(int64(len(nodes))), faNum(now.In(tehran).Format("15:04")))
	b.send(ctx, chat, sb.String(), &Keyboard{Rows: [][]Button{row(cbBtn("🔄 بروزرسانی", "status"), cbBtn("« منوی اصلی", "home"))}})
}

// starsPlans are the plans that can be paid from a wallet (toman, or
// Stars in the main bot).
func (b *Bot) starsPlans(ctx context.Context) []hubcore.Product {
	var out []hubcore.Product
	for _, p := range b.renewProducts(ctx) {
		if p.Price > 0 || (p.PriceStars > 0 && b.Seller == "") {
			out = append(out, p)
		}
	}
	return out
}

func walletPrice(p hubcore.Product) string {
	if p.Price > 0 {
		return faMoney(p.Price) + " تومان"
	}
	return fmt.Sprintf("%d⭐", p.PriceStars)
}

// showAutoRenew lets the customer pick the plan their service renews with
// from the wallet shortly before it ends, or turn it off.
func (b *Bot) showAutoRenew(ctx context.Context, chat int64, email string) {
	cur := b.Store.AutoRenew(ctx, email)
	text := "🔁 <b>تمدید خودکار</b> · " + esc(email) + "\n" + line + "\n" +
		"یک روز مانده به پایان (یا وقتی کمتر از ۵٪ حجم مانده) سرویس با پلن انتخابی از کیف پول تمدید می‌شود و همین‌جا خبر می‌دهیم.\n\n" +
		fmt.Sprintf("💰 موجودی کیف پول: <b>%s تومان</b>", faMoney(b.Store.TomanBalance(ctx, b.Seller, chat)))
	if b.Seller == "" {
		if st := b.Store.WalletBalance(ctx, chat); st > 0 {
			text += fmt.Sprintf(" · %d⭐", st)
		}
	}
	text += "\n"
	kb := &Keyboard{}
	for _, p := range b.starsPlans(ctx) {
		mark := ""
		if p.ID == cur {
			mark = "✅ "
		}
		kb.Rows = append(kb.Rows, row(cbBtn(fmt.Sprintf("%s%s · %s", mark, p.Name, walletPrice(p)), "arp:"+email+"|"+p.ID)))
	}
	if cur != "" {
		text += "وضعیت: <b>روشن</b>"
		kb.Rows = append(kb.Rows, row(cbBtn("⏹ خاموش کردن", "arx:"+email)))
	} else {
		text += "وضعیت: خاموش — یک پلن را انتخاب کنید تا روشن شود."
	}
	kb.Rows = append(kb.Rows, row(cbBtn("💰 کیف پول", "wallet"), cbBtn("« سرویس", "svc:"+email)))
	b.send(ctx, chat, text, kb)
}
