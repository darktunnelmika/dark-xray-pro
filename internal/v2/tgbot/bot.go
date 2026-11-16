package tgbot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Bot serves customers and owners. Customers browse the owner's published
// products, order, receive their subscription after delivery, check usage
// and open support tickets. Owners get new-order cards with Deliver/Cancel,
// node up/down alerts, a daily report, /status, /client, /broadcast and
// answer tickets by replying to the forwarded message.
type Bot struct {
	Store   *hubcore.Store
	API     API
	Owners  map[int64]bool
	SubBase string // public base URL for /sub/<token>
	// Seller is "" for the panel's main bot; a reseller's own bot sells only
	// that reseller's catalog, to its card and toman wallet, and shows only
	// that reseller's clients. It has no owner menu (Owners is empty).
	Seller string
	Logf   func(string, ...any)
	Now    func() time.Time

	mu         sync.Mutex
	awaiting   map[int64]string   // chat -> expected input: "support" | "link" | owner prompts
	draft      map[int64]*bcDraft // owner chat -> broadcast being prepared
	bcStop     bool
	lastNotify time.Time
	nodeState  map[string]bool
	lastDaily  string
	schedBusy  bool
	menuURL    string // Mini App address the menu button points at
	menuSet    bool
	// SyncSchedule sends scheduled broadcasts inside Tick (tests).
	SyncSchedule bool
}

func (b *Bot) logf(f string, a ...any) {
	if b.Logf != nil {
		b.Logf(f, a...)
		return
	}
	log.Printf(f, a...)
}

func (b *Bot) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func esc(s string) string { return html.EscapeString(s) }

func gb(n int64) string {
	if n < 0 {
		return "نامحدود"
	}
	return strconv.FormatFloat(float64(n)/(1<<30), 'f', 2, 64) + " GB"
}

func money(n int64) string {
	s := strconv.FormatInt(n, 10)
	var out []byte
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

func (b *Bot) init() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.awaiting == nil {
		b.awaiting = map[int64]string{}
		hubcore.SetRewardHook(func(referrer, stars int64) {
			b.send(context.Background(), referrer, fmt.Sprintf("🎉 یکی از دوستانی که دعوت کردید خرید کرد: %d⭐ به کیف پول شما اضافه شد.", stars), nil)
		})
	}
}

// Run long-polls until ctx ends, and runs the background announcers.
func (b *Bot) Run(ctx context.Context) {
	b.init()
	if b.Seller == "" { // the panel's main bot keeps the shared state
		b.Store.TgFinishInterrupted(ctx)
		if b.Store.Setting(ctx, "tg_started") == "" {
			b.Store.MarkAllOrdersAnnounced(ctx) // first start: do not replay history
			_ = b.Store.SetSetting(ctx, "tg_started", "1")
		}
		if name, err := b.API.Me(ctx); err == nil && name != "" {
			_ = b.Store.SetSetting(ctx, "tg_username", name)
		}
	}
	_ = b.API.SetCommands(ctx, 0, [][2]string{{"start", "🏠 منوی اصلی"}, {"my", "📦 سرویس‌های من"}, {"buy", "🛍 خرید اشتراک"}, {"help", "📚 آموزش اتصال"}, {"status", "🌐 وضعیت سرورها"}, {"support", "💬 پشتیبانی"}})
	for id := range b.Owners {
		_ = b.API.SetCommands(ctx, id, [][2]string{{"admin", "پنل مدیریت"}, {"status", "وضعیت هاب و نودها"}, {"orders", "سفارش‌های در انتظار"},
			{"report", "گزارش امروز"}, {"client", "اطلاعات یک کلاینت"}, {"broadcast", "پیام همگانی"}, {"help", "راهنما"}})
	}
	go b.background(ctx)
	var offset int64
	for ctx.Err() == nil {
		ups, err := b.API.Updates(ctx, offset)
		if err != nil {
			if ctx.Err() == nil {
				b.logf("tgbot: %v", err)
				time.Sleep(5 * time.Second)
			}
			continue
		}
		for _, u := range ups {
			offset = u.ID + 1
			b.Handle(ctx, u)
		}
	}
}

func (b *Bot) background(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		b.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick announces new orders and deliveries, node changes and the daily report.
func (b *Bot) Tick(ctx context.Context) {
	b.init()
	if b.Seller != "" {
		// A reseller's own bot: its orders and its customers' notices only.
		b.syncMenuButton(ctx)
		b.announceOrders(ctx)
		b.notifyClients(ctx)
		b.repAlerts(ctx)
		return
	}
	b.syncMenuButton(ctx)
	b.Store.ExpireStarsInvoices(ctx)
	b.announceOrders(ctx)
	b.watchNodes(ctx)
	b.dailyReport(ctx)
	b.scheduledBackup(ctx)
	b.notifyClients(ctx)
	b.repAlerts(ctx)
	b.runScheduled(ctx)
}

func (b *Bot) send(ctx context.Context, chat int64, text string, kb *Keyboard) int64 {
	if id, ok := b.redraw(ctx, chat, text, kb); ok {
		return id
	}
	id, err := b.API.Send(ctx, chat, text, kb, 0)
	if errors.Is(err, ErrBlocked) {
		b.Store.TgSetBlocked(ctx, chat)
	} else if err != nil {
		b.logf("tgbot send: %v", err)
	}
	return id
}

func (b *Bot) toOwners(ctx context.Context, text string, kb *Keyboard) map[int64]int64 {
	out := map[int64]int64{}
	for id := range b.Owners {
		out[id] = b.send(ctx, id, text, kb)
	}
	return out
}

func (b *Bot) owner() hubcore.Principal { return hubcore.Principal{User: "telegram", Role: "owner"} }

// handles reports whether this bot speaks for a seller's orders: a
// reseller's own bot for its orders, the main bot for the owner's and for
// resellers without a running bot of their own.
func (b *Bot) handles(seller string) bool {
	if b.Seller != "" {
		return seller == b.Seller
	}
	return seller == "" || !hubcore.RepBotRunning(seller)
}

// sells reports whether this bot sells a product.
func (b *Bot) sells(p hubcore.Product) bool { return p.Seller == b.Seller }

// clients lists the services a chat sees in this bot.
func (b *Bot) clients(ctx context.Context, chat int64) ([]string, error) {
	if b.Seller == "" {
		return b.Store.TgClients(ctx, chat)
	}
	return b.Store.TgClientsOf(ctx, chat, b.Seller)
}

// Handle processes one update.
func (b *Bot) Handle(ctx context.Context, u Update) {
	b.init()
	switch {
	case u.PreCheckout != nil:
		b.onPreCheckout(ctx, u.PreCheckout)
	case u.Callback != nil:
		b.onCallback(ctx, u.Callback)
	case u.Message != nil && u.Message.Chat.Type == "private" && u.Message.From != nil:
		b.onMessage(ctx, u.Message)
	}
}

func (b *Bot) onMessage(ctx context.Context, m *Message) {
	chat := m.Chat.ID
	name := strings.TrimSpace(m.From.FirstName + " " + m.From.LastName)
	_ = b.Store.TgTouch(ctx, chat, name, m.From.Username)
	if m.Paid != nil {
		b.onPaid(ctx, chat, name, m.Paid)
		return
	}
	text := strings.TrimSpace(m.Text)
	if text == "" {
		text = strings.TrimSpace(m.Caption)
	}
	if b.Owners[chat] && len(m.Photo) > 0 {
		b.mu.Lock()
		what := b.awaiting[chat]
		b.mu.Unlock()
		if what == "broadcast" {
			b.takeAwait(chat)
			b.prepareBroadcast(ctx, chat, text, m.Photo[len(m.Photo)-1].FileID)
			return
		}
	}
	if len(m.Photo) > 0 && !(b.Owners[chat] && b.peekAwait(chat) == "broadcast") {
		if b.onReceipt(ctx, chat, name, m.Photo[len(m.Photo)-1].FileID, b.peekAwait(chat)) {
			return
		}
	}
	if b.Owners[chat] && b.peekAwait(chat) == "emoji" {
		if ce := m.CustomEmoji(); len(ce) > 0 {
			n, err := b.Store.AddEmoji(ctx, ce, false)
			if err != nil {
				b.send(ctx, chat, "ذخیره نشد: "+esc(err.Error()), nil)
				return
			}
			var keys []string
			for k := range ce {
				keys = append(keys, k)
			}
			b.send(ctx, chat, fmt.Sprintf("✨ %d ایموجی ثبت شد (جمعاً %d). از این به بعد این‌ها در پیام‌ها و دکمه‌ها متحرک‌اند:\n%s\n\nایموجی‌های بیشتری بفرستید یا /done بزنید.", len(ce), n, strings.Join(keys, " ")), nil)
			return
		}
	}
	if b.Owners[chat] {
		if text == "/start" || text == "/admin" || text == "/menu" {
			b.ownerHome(ctx, chat)
			return
		}
		if b.ownerMessage(ctx, m, text) {
			return
		}
		b.mu.Lock()
		what := b.awaiting[chat]
		b.mu.Unlock()
		if what != "" && what != "support" && what != "link" && !strings.HasPrefix(text, "/") {
			b.takeAwait(chat)
			if b.ownerInput(ctx, chat, what, text) {
				return
			}
		}
	}
	if code, ok := strings.CutPrefix(text, "/start ref_"); ok {
		if referrer, err := b.Store.AttributeReferral(ctx, chat, code); err == nil {
			b.send(ctx, referrer, "👥 "+esc(name)+" با لینک دعوت شما وارد شد. با اولین خریدش اعتبار می‌گیرید.", nil)
		}
		b.menu(ctx, chat)
		return
	}
	if code, ok := strings.CutPrefix(text, "/start rep_"); ok {
		rep, err := b.Store.ClaimTgLinkCode(ctx, strings.TrimSpace(code), chat)
		if err != nil {
			b.send(ctx, chat, "این کد معتبر نیست یا منقضی شده. از پنل یک کد تازه بگیرید.", nil)
			return
		}
		b.send(ctx, chat, "✅ تلگرام شما به نمایندگی <b>"+esc(rep)+"</b> وصل شد.\n\n"+repHelp, nil)
		return
	}
	if code, ok := strings.CutPrefix(text, "/start adm_"); ok {
		rep, role, err := b.Store.ClaimTgAdminInvite(ctx, strings.TrimSpace(code), chat, name)
		if err != nil {
			b.send(ctx, chat, "این دعوت معتبر نیست یا منقضی شده.", nil)
			return
		}
		b.send(ctx, chat, fmt.Sprintf("✅ شما با نقش «%s» ادمین ربات نمایندگی <b>%s</b> شدید.\n\n%s", roleName(role), esc(rep), repHelp), nil)
		if c := b.Store.TgRepChat(ctx, rep); c != 0 {
			b.send(ctx, c, fmt.Sprintf("👤 %s با نقش «%s» به ادمین‌های ربات شما اضافه شد.", esc(name), roleName(role)), nil)
		}
		return
	}
	if rep, role := b.Store.TgRepRole(ctx, chat); rep != "" && (b.Seller == "" || rep == b.Seller) {
		if b.repMessage(ctx, chat, rep, role, text) {
			return
		}
	}
	// Buttons of the bottom keyboard (ours, or the old panel's, still on
	// some phones) arrive as plain text.
	if act := keyboardAction(text); act != "" {
		b.keyAction(ctx, chat, act)
		return
	}
	cmd, arg, _ := strings.Cut(text, " ")
	cmd = strings.SplitN(cmd, "@", 2)[0]
	switch cmd {
	case "/start", "/menu":
		if arg != "" { // deep link: /start <sub token>
			b.link(ctx, chat, arg)
			return
		}
		b.menu(ctx, chat)
		b.ensureKeyboard(ctx, chat)
		return
	case "/buy":
		b.shop(ctx, chat)
		return
	case "/my":
		b.mine(ctx, chat)
		return
	case "/help", "/guide":
		b.showGuide(ctx, chat, "")
		return
	case "/status":
		b.showStatus(ctx, chat)
		return
	case "/link":
		if arg == "" {
			b.setAwait(chat, "link")
			b.send(ctx, chat, "لینک ساب سرویس خود را بفرستید تا به این حساب تلگرام وصل شود.", nil)
			return
		}
		b.link(ctx, chat, arg)
		return
	case "/support":
		if arg == "" {
			b.setAwait(chat, "support")
			b.send(ctx, chat, "پیام خود را بنویسید؛ به پشتیبانی ارسال می‌شود.", nil)
			return
		}
		b.ticket(ctx, chat, name, m.From.Username, arg)
		return
	}
	if what := b.peekAwait(chat); what == "topamt" && text != "" && !strings.HasPrefix(text, "/") {
		b.takeAwait(chat)
		b.startTopup(ctx, chat, text)
		return
	}
	if what := b.peekAwait(chat); strings.HasPrefix(what, "coupon:") && text != "" && !strings.HasPrefix(text, "/") {
		b.takeAwait(chat)
		b.takeCoupon(ctx, chat, strings.TrimPrefix(what, "coupon:"), text)
		return
	}
	switch b.takeAwait(chat) {
	case "link":
		b.link(ctx, chat, text)
	case "support":
		b.ticket(ctx, chat, name, m.From.Username, text)
	default:
		if strings.Contains(text, "/sub/") {
			b.link(ctx, chat, text)
			return
		}
		b.menu(ctx, chat)
	}
}

func (b *Bot) setAwait(chat int64, what string) {
	b.mu.Lock()
	b.awaiting[chat] = what
	b.mu.Unlock()
}

func (b *Bot) peekAwait(chat int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.awaiting[chat]
}

func (b *Bot) takeAwait(chat int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	w := b.awaiting[chat]
	delete(b.awaiting, chat)
	return w
}

var mainMenu = &Keyboard{Rows: [][]Button{
	{{Text: "🛍 خرید اشتراک", Data: "shop"}, {Text: "📦 سرویس‌های من", Data: "mine"}},
	{{Text: "« منوی اصلی", Data: "home"}},
}}

const defaultGuide = `📲 <b>آموزش اتصال</b>

۱. یکی از برنامه‌ها را نصب کنید:
• اندروید: v2rayNG یا Hiddify
• آیفون: Streisand یا V2Box
• ویندوز/مک: Hiddify یا v2rayN

۲. در «سرویس‌های من» دکمهٔ «لینک و QR» را بزنید.
۳. لینک را کپی کنید و در برنامه «Import from clipboard» را بزنید (یا QR را اسکن کنید).
۴. سرور را انتخاب و اتصال را بزنید. برای به‌روزرسانی سرورها، Update subscription را بزنید.`

func isBlocked(err error) bool { return errors.Is(err, ErrBlocked) }

func (b *Bot) menu(ctx context.Context, chat int64) {
	welcome := b.Store.Setting(ctx, "tg_welcome")
	if welcome == "" {
		welcome = "✨ <b>به Dark VPN خوش آمدید</b>\n" + line + "\nاینترنت آزاد، سریع و پایدار روی چند سرور.\nاز دکمه‌های زیر شروع کنید 👇"
	} else {
		welcome = esc(welcome)
	}
	if emails, _ := b.clients(ctx, chat); len(emails) > 0 {
		var lines []string
		for _, e := range emails {
			if c, err := b.Store.ClientByEmail(ctx, e); err == nil {
				st := b.state(c)
				left := ""
				if c.VolumeBytes > 0 {
					l := c.VolumeBytes - c.Usage.Up - c.Usage.Down
					if l < 0 {
						l = 0
					}
					left = " · " + size(l) + " مانده"
				}
				lines = append(lines, st.icon+" <b>"+esc(c.Email)+"</b>"+left)
			}
		}
		welcome += "\n\n📦 <b>سرویس‌های شما</b>\n<blockquote>" + strings.Join(lines, "\n") + "</blockquote>"
	}
	if bal := b.Store.TomanBalance(ctx, b.Seller, chat); bal > 0 {
		welcome += "\n👛 کیف پول: <b>" + faMoney(bal) + " تومان</b>"
	}
	b.send(ctx, chat, welcome, b.menuKB(ctx, chat))
}

// menuKB is the main menu, led by the Mini App store when it is on.
func (b *Bot) menuKB(ctx context.Context, chat int64) *Keyboard {
	var rows [][]Button
	if u := b.miniURL(ctx); u != "" {
		rows = append(rows, []Button{{Text: "🛍 فروشگاه و سرویس‌های من", WebApp: &WebApp{URL: u}}})
	}
	rows = append(rows, row(cbBtn("🛍 خرید اشتراک", "shop"), cbBtn("📦 سرویس‌های من", "mine")))
	var extra []Button
	if b.Store.TrialPolicy(ctx).Enabled && !b.Store.TrialUsed(ctx, chat) {
		extra = append(extra, cbBtn("🎁 تست رایگان", "trial"))
	}
	if b.Store.WalletBalance(ctx, chat) > 0 || b.Store.TomanBalance(ctx, b.Seller, chat) > 0 {
		extra = append(extra, cbBtn("👛 کیف پول", "wallet"))
	}
	if len(extra) > 0 {
		rows = append(rows, extra)
	}
	rows = append(rows, row(cbBtn("📚 آموزش اتصال", "guide"), cbBtn("🌐 وضعیت سرورها", "status")))
	social := row(cbBtn("💬 پشتیبانی", "support"))
	if b.Store.ReferralPolicy(ctx).Enabled {
		social = append([]Button{cbBtn("👥 دعوت دوستان", "invite")}, social...)
	}
	rows = append(rows, social, row(cbBtn("🔗 افزودن سرویس با لینک", "link")))
	return &Keyboard{Rows: rows}
}

// syncMenuButton keeps the chat menu button in step with the Mini App setting.
// miniURL is this bot's Mini App address ("" = off): a reseller's bot opens
// the same app, which shows that reseller's store.
func (b *Bot) miniURL(ctx context.Context) string {
	if b.Seller != "" {
		return b.Store.RepMiniAppURL(ctx, b.Seller, b.SubBase)
	}
	return b.Store.MiniAppURL(ctx, b.SubBase)
}

func (b *Bot) syncMenuButton(ctx context.Context) {
	setter, ok := b.API.(interface {
		SetMenuButton(ctx context.Context, url, text string) error
	})
	if !ok {
		return
	}
	u := b.miniURL(ctx)
	if u == b.menuURL && b.menuSet {
		return
	}
	if err := setter.SetMenuButton(ctx, u, "فروشگاه"); err != nil {
		b.logf("tgbot menu button: %v", err)
		return
	}
	b.menuURL, b.menuSet = u, true
}

func (b *Bot) shop(ctx context.Context, chat int64) { b.shopIn(ctx, chat, "") }

// shopIn lists the catalog; with categories, it first lists those.
func (b *Bot) shopIn(ctx context.Context, chat int64, cat string) {
	all, err := b.Store.Products(ctx, b.owner(), true)
	if err != nil {
		b.logf("tgbot products: %v", err)
		return
	}
	cats, seen, uncategorised := []string{}, map[string]bool{}, false
	for _, p := range all {
		if !b.sells(p) {
			continue
		}
		if p.Category == "" {
			uncategorised = true
		} else if !seen[p.Category] {
			seen[p.Category] = true
			cats = append(cats, p.Category)
		}
	}
	if cat == "" && len(cats) > 0 && (len(cats) > 1 || uncategorised) {
		kb := &Keyboard{}
		for _, c := range cats {
			kb.Rows = append(kb.Rows, row(cbBtn("📂 "+c, "cat:"+c)))
		}
		if uncategorised {
			kb.Rows = append(kb.Rows, row(cbBtn("📦 سایر بسته‌ها", "cat:-")))
		}
		kb.Rows = append(kb.Rows, row(cbBtn("« منوی اصلی", "home")))
		b.send(ctx, chat, "🛍 <b>خرید سرویس</b>\n"+line+"\nیک دسته را انتخاب کنید:", kb)
		return
	}
	kb := &Keyboard{}
	var lines []string
	for _, p := range all {
		if !b.sells(p) {
			continue // each bot sells its own seller's catalog
		}
		if (cat == "-" && p.Category != "") || (cat != "" && cat != "-" && p.Category != cat) {
			continue
		}
		size := "نامحدود"
		if p.Plan == "volume" {
			size = fmt.Sprintf("%d گیگ", p.GB)
		}
		price := []string{}
		if p.Price > 0 {
			price = append(price, money(p.Price)+" تومان")
		}
		if p.PriceStars > 0 {
			price = append(price, fmt.Sprintf("%d ⭐", p.PriceStars))
		}
		if len(price) == 0 {
			price = append(price, "رایگان")
		}
		_ = size
		lines = append(lines, p.ID)
		kb.Rows = append(kb.Rows, row(cbBtn("📦 "+p.Name+" — "+productPrice(p), "prod:"+p.ID)))
	}
	if len(lines) == 0 {
		b.send(ctx, chat, "🛍 فعلاً بسته‌ای برای فروش نیست. کمی بعد سر بزنید.", homeKB)
		return
	}
	kb.Rows = append(kb.Rows, row(cbBtn("« منوی اصلی", "home")))
	b.send(ctx, chat, "🛍 <b>بسته‌ها</b>\n"+line+"\nهمهٔ بسته‌ها روی همهٔ سرورها کار می‌کنند و با یک لینک تحویل می‌شوند.\nیک بسته را انتخاب کنید:", kb)
}

func (b *Bot) buy(ctx context.Context, chat int64, productID, renew string) {
	note := ""
	id, err := b.Store.CreateOrder(ctx, b.owner(), productID, hubcore.TgBuyer(chat), renew, note)
	if err != nil {
		b.send(ctx, chat, "ثبت سفارش ممکن نشد. لطفاً دوباره تلاش کنید یا با پشتیبانی در تماس باشید.", mainMenu)
		return
	}
	if o, err := b.Store.OrderByID(ctx, id); err == nil && o.Amount > 0 && b.cardOn(ctx) {
		b.invoice(ctx, chat, id)
		return
	} else if err == nil && o.Amount == 0 && o.Discount > 0 {
		// A 100% code: nothing to pay, deliver now.
		if _, err := b.Store.FulfillOrder(ctx, b.owner(), id); err == nil {
			b.announceOrders(ctx)
			return
		}
	}
	pay := b.Store.Setting(ctx, "tg_pay_text")
	if pay == "" {
		pay = "پشتیبانی برای پرداخت با شما تماس می‌گیرد."
	} else {
		pay = esc(pay)
	}
	b.send(ctx, chat, fmt.Sprintf("سفارش <b>#%d</b> ثبت شد.\n\n%s\n\nپس از تأیید، لینک اتصال همین‌جا برایتان ارسال می‌شود.", id, pay), nil)
	b.announceOrders(ctx)
}

func (b *Bot) mine(ctx context.Context, chat int64) {
	emails, err := b.clients(ctx, chat)
	if err != nil || len(emails) == 0 {
		b.send(ctx, chat, "📦 <b>سرویس‌های من</b>\n"+line+"\nهنوز سرویسی به این حساب وصل نیست.\n\n• برای خرید، «🛍 خرید اشتراک» را بزنید.\n• اگر سرویس دارید، «🔗 افزودن با لینک» را بزنید و لینک اشتراک را بفرستید.", &Keyboard{Rows: [][]Button{
			row(cbBtn("🛍 خرید اشتراک", "shop"), cbBtn("🔗 افزودن با لینک", "link")), row(cbBtn("« منوی اصلی", "home"))}})
		return
	}
	if len(emails) == 1 {
		b.showService(ctx, chat, emails[0])
		return
	}
	kb := &Keyboard{}
	for _, e := range emails {
		c, err := b.Store.ClientByEmail(ctx, e)
		if err != nil {
			continue
		}
		st := b.state(c)
		kb.Rows = append(kb.Rows, row(cbBtn(st.icon+" "+c.Email, "svc:"+e)))
	}
	kb.Rows = append(kb.Rows, row(cbBtn("« منوی اصلی", "home")))
	b.send(ctx, chat, "📦 <b>سرویس‌های شما</b>\n"+line+"\nیک سرویس را برای دیدن جزئیات انتخاب کنید:", kb)
}

func (b *Bot) renewButtons(ctx context.Context, chat int64, email string) [][]Button {
	all, _ := b.Store.Products(ctx, b.owner(), true)
	var rows [][]Button
	n := 0
	for _, p := range all {
		if b.sells(p) && n < 3 {
			rows = append(rows, b.buyButtons(ctx, chat, p, email)...)
			n++
		}
	}
	return rows
}

func (b *Bot) clientCard(c hubcore.ClientView) string { return b.serviceCard(c) }

func (b *Bot) clientCardOld(c hubcore.ClientView) string {
	used := c.Usage.Up + c.Usage.Down
	status := "فعال ✅"
	now := b.now().Unix()
	switch {
	case !c.Enabled:
		status = "غیرفعال ⛔"
	case c.Remaining == 0:
		status = "حجم تمام شده ⛔"
	case c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
		status = "منقضی ⛔"
	}
	days := "بدون انقضا"
	switch {
	case c.EffectiveUntil > now:
		days = fmt.Sprintf("%d روز", (c.EffectiveUntil-now+86399)/86400)
	case c.EffectiveUntil == 0 && c.DurationSeconds > 0:
		days = fmt.Sprintf("%d روز (از اولین اتصال)", c.DurationSeconds/86400)
	case c.EffectiveUntil > 0:
		days = "۰"
	}
	total := "نامحدود"
	if c.VolumeBytes >= 0 {
		total = gb(c.VolumeBytes)
	}
	return fmt.Sprintf("👤 <b>%s</b>\nوضعیت: %s\nمصرف: %s از %s\nزمان باقی‌مانده: %s", esc(c.Email), status, gb(used), total, days)
}

func (b *Bot) sendSub(ctx context.Context, chat int64, email string) {
	c, err := b.Store.ClientByEmail(ctx, email)
	if err != nil {
		return
	}
	url := strings.TrimRight(b.Store.SubBaseFor(ctx, c.Email, b.SubBase), "/") + "/sub/" + c.SubToken
	if png, err := qrcode.Encode(url, qrcode.Medium, 512); err == nil {
		if err := b.API.SendPhoto(ctx, chat, png, "<b>"+esc(email)+"</b>"); err != nil {
			b.logf("tgbot photo: %v", err)
		}
	}
	b.send(ctx, chat, "لینک اشتراک (در v2rayNG، Hiddify یا Streisand وارد کنید):\n<code>"+esc(url)+"</code>", nil)
}

func (b *Bot) link(ctx context.Context, chat int64, tokenOrURL string) {
	email, err := b.Store.TgLinkByToken(ctx, chat, tokenOrURL)
	if err != nil {
		b.send(ctx, chat, "این لینک پیدا نشد. لینک کامل ساب را بفرستید.", mainMenu)
		return
	}
	b.celebrate(ctx, chat, "✅ سرویس <b>"+esc(email)+"</b> به حساب شما وصل شد.\nاز این به بعد هشدار حجم و انقضا همین‌جا می‌آید.", &Keyboard{Rows: [][]Button{row(cbBtn("📦 مشاهدهٔ سرویس", "svc:"+email), cbBtn("« منو", "home"))}}, EffectHeart)
}

func (b *Bot) ticket(ctx context.Context, chat int64, name, username, text string) {
	if text == "" {
		return
	}
	id, err := b.Store.TgTicket(ctx, chat, text)
	if err != nil {
		b.logf("tgbot ticket: %v", err)
		return
	}
	who := esc(name)
	if username != "" {
		who += " (@" + esc(username) + ")"
	}
	emails, _ := b.clients(ctx, chat)
	msgs := b.toOwners(ctx, fmt.Sprintf("💬 تیکت #%d از %s\nسرویس‌ها: %s\n\n%s\n\n<i>برای پاسخ، روی همین پیام Reply کنید.</i>",
		id, who, esc(strings.Join(emails, ", ")), esc(text)), nil)
	for owner, mid := range msgs {
		if mid != 0 {
			b.Store.TgMapMessage(ctx, owner, mid, id)
		}
	}
	b.send(ctx, chat, fmt.Sprintf("پیام شما ثبت شد (تیکت #%d). پاسخ همین‌جا ارسال می‌شود.", id), nil)
}

func (b *Bot) onCallback(ctx context.Context, q *CallbackQuery) {
	chat := q.From.ID
	_ = b.API.Answer(ctx, q.ID, "")
	ctx = onScreen(ctx, q)
	if b.ownerCallback(ctx, chat, q.Data) {
		return
	}
	verb, rest, _ := strings.Cut(q.Data, ":")
	mine := func() bool { return b.ownsClient(ctx, chat, rest) || b.Owners[chat] }
	switch verb {
	case "home":
		b.menu(ctx, chat)
	case "svc":
		if mine() {
			b.showService(ctx, chat, rest)
		}
	case "subl":
		if mine() {
			b.showSubLink(ctx, chat, rest)
		}
	case "qr":
		if mine() {
			b.sendQR(ctx, chat, rest)
		}
	case "cfg":
		if mine() {
			b.showConfigs(ctx, chat, rest)
		}
	case "use":
		if mine() {
			b.showUsage(ctx, chat, rest)
		}
	case "rnw":
		if mine() {
			b.showRenew(ctx, chat, rest)
		}
	case "ar":
		if mine() {
			b.showAutoRenew(ctx, chat, rest)
		}
	case "arp", "arx":
		email, pid, _ := strings.Cut(rest, "|")
		if !b.ownsClient(ctx, chat, email) {
			return
		}
		if verb == "arx" {
			pid = ""
		}
		if err := b.Store.SetAutoRenew(ctx, chat, email, pid); err != nil {
			b.send(ctx, chat, "این تنظیم انجام نشد: "+esc(err.Error()), homeKB)
			return
		}
		b.showAutoRenew(ctx, chat, email)
	case "prod":
		b.showProduct(ctx, chat, rest)
	case "cpn":
		b.askCoupon(ctx, chat, rest)
	case "rcpt":
		b.setAwait(chat, "receipt:"+rest)
		b.send(ctx, chat, "📸 عکس رسید واریز را همین‌جا بفرستید (اسکرین‌شات یا عکس).", homeKB)
	case "ocx":
		id, _ := strconv.ParseInt(rest, 10, 64)
		if err := b.Store.CancelOwnOrder(ctx, hubcore.TgBuyer(chat), id); err != nil {
			b.send(ctx, chat, "این سفارش دیگر قابل لغو نیست.", homeKB)
			return
		}
		b.takeAwait(chat)
		b.send(ctx, chat, "سفارش لغو شد.", homeKB)
	case "status":
		b.showStatus(ctx, chat)
	case "shop":
		b.shop(ctx, chat)
	case "mine":
		b.mine(ctx, chat)
	case "link":
		b.setAwait(chat, "link")
		b.send(ctx, chat, "🔗 <b>افزودن سرویس</b>\n"+line+"\nلینک اشتراک سرویس خود را همین‌جا بفرستید تا به این حساب تلگرام وصل شود و هشدارهای حجم و انقضا را بگیرید.", homeKB)
	case "support":
		b.setAwait(chat, "support")
		b.send(ctx, chat, "💬 <b>پشتیبانی</b>\n"+line+"\nپیام خود را بنویسید و بفرستید؛ مستقیم به پشتیبانی می‌رسد و پاسخ همین‌جا می‌آید.", homeKB)
	case "guide":
		b.showGuide(ctx, chat, rest)
	case "buy":
		b.buy(ctx, chat, rest, "")
	case "pay", "wpay":
		pid, email, _ := strings.Cut(rest, ":")
		if email != "" && !b.ownsClient(ctx, chat, email) {
			return
		}
		if verb == "pay" {
			b.payStars(ctx, chat, pid, email)
		} else {
			b.payWallet(ctx, chat, pid, email)
		}
	case "trial":
		b.trial(ctx, chat)
	case "invite":
		b.invite(ctx, chat)
	case "wallet":
		b.wallet(ctx, chat)
	case "cat":
		b.shopIn(ctx, chat, rest)
	case "topup":
		b.askTopup(ctx, chat)
	case "tamt":
		b.takeAwait(chat)
		b.startTopup(ctx, chat, rest)
	case "tok", "tno":
		b.decideTopup(ctx, chat, rest, verb == "tok")
	case "tpay":
		pid, email, _ := strings.Cut(rest, ":")
		if email == "" || b.ownsClient(ctx, chat, email) {
			b.payToman(ctx, chat, pid, email)
		}
	case "srf":
		if b.Owners[chat] {
			id, _ := strconv.ParseInt(rest, 10, 64)
			b.refundStars(ctx, chat, id)
		}
	case "renew":
		pid, email, _ := strings.Cut(rest, ":")
		if b.ownsClient(ctx, chat, email) {
			b.buy(ctx, chat, pid, email)
		}
	case "sub":
		if mine() {
			b.showSubLink(ctx, chat, rest)
			b.sendQR(ctx, chat, rest)
		}
	case "radm":
		if rep, role := b.Store.TgRepRole(ctx, chat); rep != "" && role == "owner" {
			id, _ := strconv.ParseInt(rest, 10, 64)
			_ = b.Store.RemoveTgRepAdmin(ctx, rep, id)
			b.send(ctx, chat, "حذف شد.", nil)
			b.send(ctx, id, "دسترسی ادمین شما به ربات نمایندگی برداشته شد.", nil)
		}
	case "ok", "no":
		by := b.owner()
		if !b.Owners[chat] {
			rep, role := b.Store.TgRepRole(ctx, chat)
			if rep == "" || role == "support" {
				return
			}
			by = hubcore.Principal{User: "tg:" + strconv.FormatInt(chat, 10), Role: "rep", RepID: rep}
		}
		id, _ := strconv.ParseInt(rest, 10, 64)
		if verb == "ok" {
			email, err := b.Store.FulfillOrder(ctx, by, id)
			if err != nil {
				b.send(ctx, chat, fmt.Sprintf("تحویل سفارش #%d ممکن نشد: %s", id, esc(err.Error())), nil)
				return
			}
			b.send(ctx, chat, fmt.Sprintf("✅ سفارش #%d تحویل شد: <code>%s</code>", id, esc(email)), &Keyboard{Rows: [][]Button{{{Text: "لینک و QR", Data: "sub:" + email}}}})
		} else {
			if err := b.Store.CancelOrder(ctx, by, id); err != nil {
				b.send(ctx, chat, fmt.Sprintf("لغو سفارش #%d ممکن نشد: %s", id, esc(err.Error())), nil)
				return
			}
			b.send(ctx, chat, fmt.Sprintf("سفارش #%d لغو شد.", id), nil)
		}
		b.announceOrders(ctx)
	}
}

func (b *Bot) ownsClient(ctx context.Context, chat int64, email string) bool {
	list, _ := b.clients(ctx, chat)
	for _, e := range list {
		if e == email {
			return true
		}
	}
	return false
}

// ownerMessage handles owner commands and ticket replies; false means "not
// an owner action", so the owner can also use the customer menu.
func (b *Bot) ownerMessage(ctx context.Context, m *Message, text string) bool {
	if m.ReplyTo != nil && !strings.HasPrefix(text, "/") {
		_, chat, err := b.Store.TgTicketChat(ctx, m.Chat.ID, m.ReplyTo.ID)
		if err == nil {
			b.send(ctx, chat, "📩 پاسخ پشتیبانی:\n\n"+esc(text), nil)
			b.send(ctx, m.Chat.ID, "ارسال شد ✅", nil)
			return true
		}
	}
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	switch strings.SplitN(cmd, "@", 2)[0] {
	case "/help":
		b.send(ctx, m.Chat.ID, ownerHelp, nil)
	case "/status":
		b.send(ctx, m.Chat.ID, b.statusText(ctx), nil)
	case "/orders":
		list, _ := b.Store.Orders(ctx, b.owner(), "pending", 20)
		if len(list) == 0 {
			b.send(ctx, m.Chat.ID, "سفارش در انتظاری نیست.", nil)
		}
		for _, o := range list {
			b.send(ctx, m.Chat.ID, b.orderCard(ctx, o), orderButtons(o.ID))
		}
	case "/client":
		c, err := b.Store.ClientByEmail(ctx, arg)
		if err != nil {
			b.send(ctx, m.Chat.ID, "کلاینت پیدا نشد.", nil)
			return true
		}
		b.send(ctx, m.Chat.ID, b.clientCard(c), &Keyboard{Rows: [][]Button{{{Text: "لینک و QR", Data: "sub:" + c.Email}}}})
	case "/broadcast":
		if arg == "" {
			b.send(ctx, m.Chat.ID, "متن را بعد از /broadcast بنویسید.", nil)
			return true
		}
		b.prepareBroadcast(ctx, m.Chat.ID, arg, "")
	case "/wallet":
		// /wallet <chat id> <+/-stars> [note]
		f := strings.Fields(arg)
		chat, err1 := strconv.ParseInt(firstOr(f, 0), 10, 64)
		delta, err2 := strconv.ParseInt(firstOr(f, 1), 10, 64)
		if err1 != nil || err2 != nil {
			b.send(ctx, m.Chat.ID, "نمونه: <code>/wallet 123456789 +50 هدیه</code>", nil)
			return true
		}
		bal, err := b.Store.WalletAdjust(ctx, chat, delta, strings.Join(f[2:], " "))
		if err != nil {
			b.send(ctx, m.Chat.ID, "انجام نشد: "+esc(err.Error()), nil)
			return true
		}
		b.send(ctx, m.Chat.ID, fmt.Sprintf("کیف پول %d: %+d⭐ — موجودی %d⭐", chat, delta, bal), nil)
		if delta > 0 {
			b.send(ctx, chat, fmt.Sprintf("🎁 %d⭐ هدیه به کیف پول شما اضافه شد. موجودی: %d⭐", delta, bal), nil)
		}
	case "/paytext":
		_ = b.Store.SetSetting(ctx, "tg_pay_text", arg)
		b.send(ctx, m.Chat.ID, "متن پرداخت ذخیره شد.", nil)
	case "/welcome":
		_ = b.Store.SetSetting(ctx, "tg_welcome", arg)
		b.send(ctx, m.Chat.ID, "متن خوش‌آمد ذخیره شد.", nil)
	case "/report":
		b.send(ctx, m.Chat.ID, b.reportText(ctx), nil)
	case "/emoji":
		if arg == "reset" {
			_, _ = b.Store.AddEmoji(ctx, nil, true)
			b.send(ctx, m.Chat.ID, "ایموجی‌های متحرک پاک شدند؛ ربات دوباره ایموجی معمولی نشان می‌دهد.", nil)
			return true
		}
		b.setAwait(m.Chat.ID, "emoji")
		theme := b.Store.EmojiTheme(ctx)
		var keys []string
		for k := range theme {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		cur := "هنوز چیزی ثبت نشده."
		if len(keys) > 0 {
			cur = "ثبت‌شده: " + strings.Join(keys, " ")
		}
		b.send(ctx, m.Chat.ID, "✨ ایموجی متحرک\n\nایموجی‌های پرمیوم دلخواهتان را (از پک‌های متحرک) همین‌جا بفرستید. هر کدام جای ایموجی معمولی هم‌شکلش می‌نشیند — مثلاً یک 🔥 متحرک، همهٔ 🔥های ربات را متحرک می‌کند؛ روی دکمه‌ها هم.\n\nبرای ایموجی‌های رایج ربات: 🛒 📊 💳 👛 🎁 ✅ ❌ ⏳ 🔗 💬 🚀 📦 🌐 ⚡️ 🎉 ❤️\n\nشرط تلگرام: صاحب ربات باید Telegram Premium داشته باشد؛ وگرنه ایموجی معمولی نمایش داده می‌شود.\n\n"+cur+"\n\nپایان: /done — پاک کردن همه: <code>/emoji reset</code>", nil)
	case "/done":
		if b.peekAwait(m.Chat.ID) == "emoji" {
			b.takeAwait(m.Chat.ID)
			b.send(ctx, m.Chat.ID, "✅ تمام. نمونه: 🛒 خرید سرویس · 📊 سرویس‌های من · 👛 کیف پول · 🎉 مبارک باشد", &Keyboard{Rows: [][]Button{{{Text: "🛒 خرید سرویس", Data: "shop"}, {Text: "📊 سرویس‌های من", Data: "mine"}}}})
			return true
		}
		return false
	default:
		return false
	}
	return true
}

const ownerHelp = `دستورهای مدیر:
/status — وضعیت هاب و نودها
/orders — سفارش‌های در انتظار
/client نام — اطلاعات یک کلاینت
/report — گزارش امروز
/broadcast متن — پیام همگانی
/paytext متن — متن راهنمای پرداخت برای خریدار
/welcome متن — متن خوش‌آمد
/wallet شناسه ±استارز — هدیه یا اصلاح کیف پول مشتری
/emoji — ایموجی‌های متحرک پرمیوم برای ربات
برای پاسخ به تیکت، روی پیام تیکت Reply کنید.`

func orderButtons(id int64) *Keyboard {
	s := strconv.FormatInt(id, 10)
	return &Keyboard{Rows: [][]Button{{{Text: "✅ تحویل", Data: "ok:" + s}, {Text: "✖️ لغو", Data: "no:" + s}}}}
}

func (b *Bot) orderCard(ctx context.Context, o hubcore.Order) string {
	buyer := esc(o.Buyer)
	if chat, ok := hubcore.TgChatOfBuyer(o.Buyer); ok {
		buyer = fmt.Sprintf(`<a href="tg://user?id=%d">%d</a>`, chat, chat)
	}
	kind := "کلاینت جدید"
	if o.Renew {
		kind = "تمدید " + esc(o.ClientEmail)
	}
	seller := ""
	if o.Seller != "" {
		seller = "\nفروشنده: " + esc(o.Seller)
	}
	disc := ""
	if o.Discount > 0 {
		disc = fmt.Sprintf("\n🎟 تخفیف %s: %s تومان", esc(o.DiscountCode), faMoney(o.Discount))
	}
	return fmt.Sprintf("🛒 سفارش <b>#%s</b>\n📦 محصول: %s\n🔖 نوع: %s\n👤 خریدار: %s\n💳 مبلغ: <b>%s تومان</b>%s%s", faInt(o.ID), esc(o.ProductName), kind, buyer, faMoney(o.Amount), disc, seller)
}

func (b *Bot) announceOrders(ctx context.Context) {
	if news, err := b.Store.OrdersToAnnounce(ctx); err == nil {
		cardOn := b.cardOn(ctx)
		for _, o := range news {
			if !b.handles(o.Seller) {
				continue // another bot (the reseller's own) announces it
			}
			if cardOn && o.Seller == b.Seller && !o.HasReceipt && o.Amount > 0 && strings.HasPrefix(o.Buyer, "tg:") {
				continue // announced with the receipt photo
			}
			if o.Seller != "" {
				// A reseller's order goes to that reseller, who pays for it.
				for _, chat := range b.Store.TgRepChats(ctx, o.Seller) {
					b.send(ctx, chat, b.orderCard(ctx, o), orderButtons(o.ID))
				}
			} else {
				b.toOwners(ctx, b.orderCard(ctx, o), orderButtons(o.ID))
			}
			b.Store.MarkOrderAnnounced(ctx, o.ID, false)
		}
	}
	if done, err := b.Store.DeliveriesToAnnounce(ctx); err == nil {
		for _, o := range done {
			if !b.handles(o.Seller) {
				continue
			}
			chat, ok := hubcore.TgChatOfBuyer(o.Buyer)
			if ok {
				if o.Status == "fulfilled" {
					b.typing(ctx, chat, "⏳ در حال آماده‌سازی سرویس شما…")
					b.celebrate(ctx, chat, fmt.Sprintf("🎉 سفارش #%s آماده شد! سرویس شما فعال است.", faInt(o.ID)), nil, EffectParty)
					b.sendSub(ctx, chat, o.ClientEmail)
				} else {
					msg := fmt.Sprintf("❌ سفارش <b>#%s</b> لغو شد.", faInt(o.ID))
					if o.HasReceipt {
						msg = fmt.Sprintf("❌ رسید سفارش <b>#%s</b> تأیید نشد و سفارش لغو شد.\nاگر مبلغ را واریز کرده‌اید، با پشتیبانی در تماس باشید.", faInt(o.ID))
					}
					b.send(ctx, chat, msg, &Keyboard{Rows: [][]Button{row(cbBtn("💬 پشتیبانی", "support"), cbBtn("« منو", "home"))}})
				}
			}
			b.Store.MarkOrderAnnounced(ctx, o.ID, true)
		}
	}
}

func (b *Bot) watchNodes(ctx context.Context) {
	nodes, err := b.Store.Nodes(ctx)
	if err != nil {
		return
	}
	now := b.now()
	b.mu.Lock()
	first := b.nodeState == nil
	if first {
		b.nodeState = map[string]bool{}
	}
	var changes []string
	for _, n := range nodes {
		on := n.Online(now)
		if was, seen := b.nodeState[n.ID]; seen && was != on && n.LastSeen > 0 {
			if on {
				changes = append(changes, "🟢 نود <b>"+esc(n.ID)+"</b> دوباره آنلاین شد.")
			} else {
				changes = append(changes, "🔴 نود <b>"+esc(n.ID)+"</b> آفلاین شد.")
			}
		}
		b.nodeState[n.ID] = on
	}
	b.mu.Unlock()
	for _, c := range changes {
		b.toOwners(ctx, c, nil)
	}
}

func (b *Bot) statusText(ctx context.Context) string {
	o, err := b.Store.Overview(ctx)
	if err != nil {
		return "وضعیت در دسترس نیست."
	}
	nodes, _ := b.Store.Nodes(ctx)
	var lines []string
	now := b.now()
	for _, n := range nodes {
		dot := "🔴"
		if n.Online(now) {
			dot = "🟢"
		}
		extra := ""
		if n.Host != nil {
			extra = fmt.Sprintf(" — CPU %.0f%%، آنلاین %d", n.Host.CPUPercent, n.Host.OnlineUsers)
		}
		lines = append(lines, dot+" "+esc(n.ID)+extra)
	}
	return fmt.Sprintf("📡 نودها %d/%d آنلاین\n%s\n\n👥 کلاینت فعال %d از %d — آنلاین الان %d\n📶 ترافیک امروز %s",
		o.NodesOnline, o.Nodes, strings.Join(lines, "\n"), o.ClientsActive, o.Clients, o.OnlineUsers, gb(o.TrafficToday))
}

func (b *Bot) reportText(ctx context.Context) string {
	pending, _ := b.Store.Orders(ctx, b.owner(), "pending", 500)
	done, _ := b.Store.Orders(ctx, b.owner(), "fulfilled", 500)
	dayStart := b.now().Add(-24 * time.Hour).Unix()
	var sold, revenue int64
	for _, o := range done {
		if o.DoneUnix >= dayStart {
			sold++
			revenue += o.Amount
		}
	}
	return fmt.Sprintf("📋 گزارش ۲۴ ساعت اخیر\n\n%s\n\n🛒 تحویل‌شده: %d سفارش — %s تومان\n⏳ در انتظار: %d", b.statusText(ctx), sold, money(revenue), len(pending))
}

// dailyReport sends the report once a day at 09:00 Tehran time (05:30 UTC).
func (b *Bot) dailyReport(ctx context.Context) {
	// Every N hours when the owner picked a shorter interval (Settings).
	if h := b.Store.Alerts(ctx).ReportHours; h != 24 {
		last, _ := strconv.ParseInt(b.Store.Setting(ctx, "tg_report_last"), 10, 64)
		if b.now().Unix()-last < int64(h)*3600 {
			return
		}
		_ = b.Store.SetSetting(ctx, "tg_report_last", strconv.FormatInt(b.now().Unix(), 10))
		b.toOwners(ctx, b.reportText(ctx), nil)
		return
	}
	now := b.now().UTC()
	if now.Hour() < 5 || now.Hour() == 5 && now.Minute() < 30 {
		return
	}
	day := now.Format("2006-01-02")
	b.mu.Lock()
	if b.lastDaily == "" {
		b.lastDaily = b.Store.Setting(ctx, "tg_daily")
	}
	due := b.lastDaily != day
	if due {
		b.lastDaily = day
	}
	b.mu.Unlock()
	if !due {
		return
	}
	_ = b.Store.SetSetting(ctx, "tg_daily", day)
	b.toOwners(ctx, b.reportText(ctx), nil)
}

// scheduledBackup sends the encrypted backup every tg_backup_hours (set in
// the panel; default daily) once a backup password is set.
func (b *Bot) scheduledBackup(ctx context.Context) {
	hrs := b.Store.BackupHours(ctx)
	if hrs <= 0 || b.Store.Setting(ctx, "tg_backup_pass") == "" || len(b.Owners) == 0 {
		return
	}
	last, _ := strconv.ParseInt(b.Store.Setting(ctx, "tg_backup_last"), 10, 64)
	if b.now().Unix()-last < int64(hrs)*3600 {
		return
	}
	_ = b.BackupNow(ctx)
}

// BackupNow sends an encrypted backup to every owner and records the result.
func (b *Bot) BackupNow(ctx context.Context) error {
	var first error
	for id := range b.Owners {
		if err := b.sendBackup(ctx, id); err != nil && first == nil {
			first = err
		}
	}
	_ = b.Store.SetSetting(ctx, "tg_backup_last", strconv.FormatInt(b.now().Unix(), 10))
	msg := ""
	if first != nil {
		msg = first.Error()
	}
	_ = b.Store.SetSetting(ctx, "tg_backup_error", msg)
	return first
}

// Test sends a short check message to every owner.
func (b *Bot) Test(ctx context.Context) error {
	var first error
	for id := range b.Owners {
		if _, err := b.API.Send(ctx, id, "✅ اتصال ربات به پنل Dark VPN برقرار است.", nil, 0); err != nil && first == nil {
			first = fmt.Errorf("owner %d: %w", id, err)
		}
	}
	return first
}

// Alert tells every owner something urgent (used by the HUB, e.g. a
// locked-out address).
func (b *Bot) Alert(ctx context.Context, text string) { b.toOwners(ctx, text, nil) }

// Tell sends one customer a message (auto-renew notices and the like).
func (b *Bot) Tell(ctx context.Context, chat int64, text string) {
	_, _ = b.API.Send(ctx, chat, text, &Keyboard{Rows: [][]Button{row(cbBtn("📦 سرویس‌های من", "mine"), cbBtn("💰 کیف پول", "wallet"))}}, 0)
}

const repHelp = `دستورهای نماینده:
/credit — اعتبار و تعداد کلاینت‌ها
/orders — سفارش‌های در انتظار
/client نام — اطلاعات یک کلاینت
/today — کلاینت‌های نیازمند تمدید
/admins — ادمین‌های ربات (فقط خود نماینده)
/invite manager|sales|support — دعوت ادمین جدید`

func roleName(r string) string {
	return map[string]string{"owner": "نماینده", "manager": "مدیر", "sales": "فروش", "support": "پشتیبانی"}[r]
}

func (b *Bot) repMessage(ctx context.Context, chat int64, rep, role, text string) bool {
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	p := hubcore.Principal{User: "tg:" + strconv.FormatInt(chat, 10), Role: "rep", RepID: rep}
	can := func(roles ...string) bool {
		for _, r := range roles {
			if r == role {
				return true
			}
		}
		b.send(ctx, chat, "این کار با نقش شما مجاز نیست.", nil)
		return false
	}
	switch strings.SplitN(cmd, "@", 2)[0] {
	case "/help", "/start":
		b.send(ctx, chat, fmt.Sprintf("نقش شما: %s\n\n%s", roleName(role), repHelp), nil)
	case "/credit":
		if !can("owner", "manager", "sales") {
			return true
		}
		reps, _ := b.Store.Reps(ctx)
		for _, r := range reps {
			if r.ID == rep {
				b.send(ctx, chat, fmt.Sprintf("💳 <b>%s</b>\nاعتبار حجمی: %s\nواحد نامحدود: %d\nکلاینت‌ها: %d", esc(r.Name), gb(r.VolumeCredit), r.UnlimitedUnits, r.Clients), nil)
			}
		}
	case "/orders":
		if !can("owner", "manager", "sales") {
			return true
		}
		list, _ := b.Store.Orders(ctx, p, "pending", 20)
		if len(list) == 0 {
			b.send(ctx, chat, "سفارش در انتظاری نیست.", nil)
		}
		for _, o := range list {
			b.send(ctx, chat, b.orderCard(ctx, o), orderButtons(o.ID))
		}
	case "/client":
		c, err := b.Store.ClientByEmail(ctx, arg)
		if err != nil || c.RepID != rep {
			b.send(ctx, chat, "کلاینت پیدا نشد.", nil)
			return true
		}
		b.send(ctx, chat, b.clientCard(c), nil)
	case "/today":
		all, _ := b.Store.Clients(ctx)
		now := b.now().Unix()
		var lines []string
		for _, c := range all {
			if c.RepID != rep || !c.Enabled {
				continue
			}
			used := c.Usage.Up + c.Usage.Down
			if c.EffectiveUntil > now && c.EffectiveUntil-now <= 3*86400 || c.VolumeBytes > 0 && used*10 >= c.VolumeBytes*9 {
				lines = append(lines, "• "+esc(clientLabel(c, now))+" — "+gb(used))
			}
		}
		if len(lines) == 0 {
			b.send(ctx, chat, "✅ امروز کلاینتی نیازمند تمدید نیست.", nil)
			return true
		}
		b.send(ctx, chat, "📋 <b>کارهای امروز</b> — نیازمند تمدید (۳ روز یا ۹۰٪ حجم):\n"+strings.Join(lines, "\n"), nil)
	case "/invite":
		if !can("owner") {
			return true
		}
		code, err := b.Store.NewTgAdminInvite(ctx, rep, arg)
		if err != nil {
			b.send(ctx, chat, "نقش را بنویسید: /invite manager یا sales یا support", nil)
			return true
		}
		bot := b.Store.Setting(ctx, "tg_username")
		b.send(ctx, chat, fmt.Sprintf("لینک دعوت نقش «%s» (یک‌بار مصرف، ۱ ساعت):\nhttps://t.me/%s?start=adm_%s", roleName(arg), esc(bot), code), nil)
	case "/admins":
		if !can("owner") {
			return true
		}
		list, _ := b.Store.TgRepAdmins(ctx, rep)
		if len(list) == 0 {
			b.send(ctx, chat, "ادمینی ندارید. با /invite دعوت کنید.", nil)
			return true
		}
		kb := &Keyboard{}
		var lines []string
		for _, a := range list {
			lines = append(lines, fmt.Sprintf("• %s — %s", esc(a.Name), roleName(a.Role)))
			kb.Rows = append(kb.Rows, []Button{{Text: "حذف " + a.Name, Data: "radm:" + strconv.FormatInt(a.ChatID, 10)}})
		}
		b.send(ctx, chat, "👥 ادمین‌های ربات:\n"+strings.Join(lines, "\n"), kb)
	default:
		return false
	}
	return true
}

func firstOr(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}
