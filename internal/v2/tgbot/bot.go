package tgbot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
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
	Logf    func(string, ...any)
	Now     func() time.Time

	mu        sync.Mutex
	awaiting  map[int64]string // chat -> expected input: "support" | "link"
	nodeState map[string]bool
	lastDaily string
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
	}
}

// Run long-polls until ctx ends, and runs the background announcers.
func (b *Bot) Run(ctx context.Context) {
	b.init()
	if b.Store.Setting(ctx, "tg_started") == "" {
		b.Store.MarkAllOrdersAnnounced(ctx) // first start: do not replay history
		_ = b.Store.SetSetting(ctx, "tg_started", "1")
	}
	if name, err := b.API.Me(ctx); err == nil && name != "" {
		_ = b.Store.SetSetting(ctx, "tg_username", name)
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
	b.announceOrders(ctx)
	b.watchNodes(ctx)
	b.dailyReport(ctx)
}

func (b *Bot) send(ctx context.Context, chat int64, text string, kb *Keyboard) int64 {
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

// Handle processes one update.
func (b *Bot) Handle(ctx context.Context, u Update) {
	b.init()
	switch {
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
	text := strings.TrimSpace(m.Text)
	if b.Owners[chat] {
		if b.ownerMessage(ctx, m, text) {
			return
		}
	}
	if code, ok := strings.CutPrefix(text, "/start rep_"); ok {
		rep, err := b.Store.ClaimTgLinkCode(ctx, strings.TrimSpace(code), chat)
		if err != nil {
			b.send(ctx, chat, "این کد معتبر نیست یا منقضی شده. از پنل یک کد تازه بگیرید.", nil)
			return
		}
		b.send(ctx, chat, "✅ تلگرام شما به نمایندگی <b>"+esc(rep)+"</b> وصل شد.

"+repHelp, nil)
		return
	}
	if rep := b.Store.TgRep(ctx, chat); rep != "" {
		if b.repMessage(ctx, chat, rep, text) {
			return
		}
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
		return
	case "/buy":
		b.shop(ctx, chat)
		return
	case "/my":
		b.mine(ctx, chat)
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

func (b *Bot) takeAwait(chat int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	w := b.awaiting[chat]
	delete(b.awaiting, chat)
	return w
}

var mainMenu = &Keyboard{Rows: [][]Button{
	{{Text: "🛒 خرید سرویس", Data: "shop"}, {Text: "📊 سرویس‌های من", Data: "mine"}},
	{{Text: "🔗 اتصال سرویس موجود", Data: "link"}, {Text: "💬 پشتیبانی", Data: "support"}},
}}

func (b *Bot) menu(ctx context.Context, chat int64) {
	welcome := b.Store.Setting(ctx, "tg_welcome")
	if welcome == "" {
		welcome = "به <b>Dark VPN</b> خوش آمدید. یکی از گزینه‌ها را انتخاب کنید:"
	} else {
		welcome = esc(welcome)
	}
	b.send(ctx, chat, welcome, mainMenu)
}

func (b *Bot) shop(ctx context.Context, chat int64) {
	all, err := b.Store.Products(ctx, b.owner(), true)
	if err != nil {
		b.logf("tgbot products: %v", err)
		return
	}
	kb := &Keyboard{}
	var lines []string
	for _, p := range all {
		if p.Seller != "" {
			continue // the bot sells the owner's catalog
		}
		size := "نامحدود"
		if p.Plan == "volume" {
			size = fmt.Sprintf("%d گیگ", p.GB)
		}
		lines = append(lines, fmt.Sprintf("• <b>%s</b> — %s، %d روز — %s تومان", esc(p.Name), size, p.Days, money(p.Price)))
		kb.Rows = append(kb.Rows, []Button{{Text: "خرید " + p.Name, Data: "buy:" + p.ID}})
	}
	if len(lines) == 0 {
		b.send(ctx, chat, "فعلاً محصولی برای فروش نیست.", mainMenu)
		return
	}
	b.send(ctx, chat, "محصولات:\n\n"+strings.Join(lines, "\n"), kb)
}

func (b *Bot) buy(ctx context.Context, chat int64, productID, renew string) {
	note := ""
	id, err := b.Store.CreateOrder(ctx, b.owner(), productID, hubcore.TgBuyer(chat), renew, note)
	if err != nil {
		b.send(ctx, chat, "ثبت سفارش ممکن نشد. لطفاً دوباره تلاش کنید یا با پشتیبانی در تماس باشید.", mainMenu)
		return
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
	emails, err := b.Store.TgClients(ctx, chat)
	if err != nil || len(emails) == 0 {
		b.send(ctx, chat, "سرویسی به این حساب وصل نیست. اگر سرویس دارید، «اتصال سرویس موجود» را بزنید و لینک ساب را بفرستید.", mainMenu)
		return
	}
	for _, e := range emails {
		c, err := b.Store.ClientByEmail(ctx, e)
		if err != nil {
			continue
		}
		kb := &Keyboard{Rows: [][]Button{{{Text: "🔗 لینک و QR", Data: "sub:" + e}}}}
		if renewals := b.renewButtons(ctx, e); len(renewals) > 0 {
			kb.Rows = append(kb.Rows, renewals...)
		}
		b.send(ctx, chat, b.clientCard(c), kb)
	}
}

func (b *Bot) renewButtons(ctx context.Context, email string) [][]Button {
	all, _ := b.Store.Products(ctx, b.owner(), true)
	var rows [][]Button
	for _, p := range all {
		if p.Seller == "" {
			rows = append(rows, []Button{{Text: "تمدید با " + p.Name, Data: "renew:" + p.ID + ":" + email}})
		}
	}
	if len(rows) > 3 {
		rows = rows[:3]
	}
	return rows
}

func (b *Bot) clientCard(c hubcore.ClientView) string {
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
	url := strings.TrimRight(b.SubBase, "/") + "/sub/" + c.SubToken
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
	b.send(ctx, chat, "سرویس <b>"+esc(email)+"</b> به حساب شما وصل شد.", mainMenu)
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
	emails, _ := b.Store.TgClients(ctx, chat)
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
	verb, rest, _ := strings.Cut(q.Data, ":")
	switch verb {
	case "shop":
		b.shop(ctx, chat)
	case "mine":
		b.mine(ctx, chat)
	case "link":
		b.setAwait(chat, "link")
		b.send(ctx, chat, "لینک ساب سرویس خود را بفرستید.", nil)
	case "support":
		b.setAwait(chat, "support")
		b.send(ctx, chat, "پیام خود را بنویسید؛ به پشتیبانی ارسال می‌شود.", nil)
	case "buy":
		b.buy(ctx, chat, rest, "")
	case "renew":
		pid, email, _ := strings.Cut(rest, ":")
		if b.ownsClient(ctx, chat, email) {
			b.buy(ctx, chat, pid, email)
		}
	case "sub":
		if b.ownsClient(ctx, chat, rest) || b.Owners[chat] {
			b.sendSub(ctx, chat, rest)
		}
	case "ok", "no":
		by := b.owner()
		if !b.Owners[chat] {
			rep := b.Store.TgRep(ctx, chat)
			if rep == "" {
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
	list, _ := b.Store.TgClients(ctx, chat)
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
		chats, _ := b.Store.TgChats(ctx)
		sent := 0
		for _, c := range chats {
			if _, err := b.API.Send(ctx, c, esc(arg), nil, 0); err == nil {
				sent++
			} else if errors.Is(err, ErrBlocked) {
				b.Store.TgSetBlocked(ctx, c)
			}
			time.Sleep(40 * time.Millisecond) // stay under Telegram's 30 msg/s
		}
		b.send(ctx, m.Chat.ID, fmt.Sprintf("پیام همگانی برای %d نفر ارسال شد.", sent), nil)
	case "/paytext":
		_ = b.Store.SetSetting(ctx, "tg_pay_text", arg)
		b.send(ctx, m.Chat.ID, "متن پرداخت ذخیره شد.", nil)
	case "/welcome":
		_ = b.Store.SetSetting(ctx, "tg_welcome", arg)
		b.send(ctx, m.Chat.ID, "متن خوش‌آمد ذخیره شد.", nil)
	case "/report":
		b.send(ctx, m.Chat.ID, b.reportText(ctx), nil)
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
	return fmt.Sprintf("🛒 سفارش <b>#%d</b>\nمحصول: %s\nنوع: %s\nخریدار: %s\nمبلغ: %s تومان%s", o.ID, esc(o.ProductName), kind, buyer, money(o.Amount), seller)
}

func (b *Bot) announceOrders(ctx context.Context) {
	if news, err := b.Store.OrdersToAnnounce(ctx); err == nil {
		for _, o := range news {
			if o.Seller != "" {
				// A reseller's order goes to that reseller, who pays for it.
				if chat := b.Store.TgRepChat(ctx, o.Seller); chat != 0 {
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
			chat, ok := hubcore.TgChatOfBuyer(o.Buyer)
			if ok {
				if o.Status == "fulfilled" {
					b.send(ctx, chat, fmt.Sprintf("✅ سفارش #%d آماده شد.", o.ID), nil)
					b.sendSub(ctx, chat, o.ClientEmail)
				} else {
					b.send(ctx, chat, fmt.Sprintf("سفارش #%d لغو شد. برای پیگیری با پشتیبانی در تماس باشید.", o.ID), mainMenu)
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

const repHelp = `دستورهای نماینده:
/credit — اعتبار و تعداد کلاینت‌ها
/orders — سفارش‌های در انتظار شما
/client نام — اطلاعات یکی از کلاینت‌های شما`

func (b *Bot) repMessage(ctx context.Context, chat int64, rep, text string) bool {
	cmd, arg, _ := strings.Cut(text, " ")
	arg = strings.TrimSpace(arg)
	p := hubcore.Principal{User: "tg:" + strconv.FormatInt(chat, 10), Role: "rep", RepID: rep}
	switch strings.SplitN(cmd, "@", 2)[0] {
	case "/help":
		b.send(ctx, chat, repHelp, nil)
	case "/credit":
		reps, _ := b.Store.Reps(ctx)
		for _, r := range reps {
			if r.ID == rep {
				b.send(ctx, chat, fmt.Sprintf("💳 <b>%s</b>\nاعتبار حجمی: %s\nواحد نامحدود: %d\nکلاینت‌ها: %d", esc(r.Name), gb(r.VolumeCredit), r.UnlimitedUnits, r.Clients), nil)
			}
		}
	case "/orders":
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
	default:
		return false
	}
	return true
}
