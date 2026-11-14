package tgbot

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// The owner's console: a button-driven admin menu so day-to-day work needs
// no typing — status, orders, clients (search, expiring, renew, top up,
// enable/disable, delete), quick sale from a product, nodes, resellers with
// credit top-ups, broadcast with preview and confirmation, bot texts.

var ownerMenu = &Keyboard{Rows: [][]Button{
	{{Text: "📊 وضعیت", Data: "a:status"}, {Text: "🧩 نودها", Data: "a:nodes"}},
	{{Text: "👥 کلاینت‌ها", Data: "a:clients"}, {Text: "🔎 جستجو", Data: "a:find"}},
	{{Text: "⚡ فروش سریع", Data: "a:quick"}, {Text: "🛒 سفارش‌ها", Data: "a:orders"}},
	{{Text: "💼 نماینده‌ها", Data: "a:reps"}, {Text: "📋 گزارش امروز", Data: "a:report"}},
	{{Text: "📣 پیام همگانی", Data: "a:bc"}, {Text: "📦 بکاپ الان", Data: "a:backup"}},
	{{Text: "⚙️ تنظیمات ربات", Data: "a:settings"}},
	{{Text: "🛍 نمای مشتری", Data: "a:customer"}},
}}

func (b *Bot) ownerHome(ctx context.Context, chat int64) {
	o, _ := b.Store.Overview(ctx)
	pending, _ := b.Store.Orders(ctx, b.owner(), "pending", 100)
	b.send(ctx, chat, fmt.Sprintf("🛡 <b>پنل مدیریت Dark VPN</b>\n\n📡 نود آنلاین: %d از %d\n👥 کلاینت فعال: %d از %d — آنلاین الان: %d\n📶 ترافیک امروز: %s\n⏳ سفارش در انتظار: %d",
		o.NodesOnline, o.Nodes, o.ClientsActive, o.Clients, o.OnlineUsers, gb(o.TrafficToday), len(pending)), ownerMenu)
}

func back(data string) []Button { return []Button{{Text: "« بازگشت", Data: data}} }

// ownerCallback handles "a:" buttons; false means not an owner action.
func (b *Bot) ownerCallback(ctx context.Context, chat int64, data string) bool {
	if !b.Owners[chat] || !strings.HasPrefix(data, "a:") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(data, "a:"), ":")
	arg := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	switch parts[0] {
	case "home":
		b.ownerHome(ctx, chat)
	case "customer":
		b.menu(ctx, chat)
	case "status":
		b.send(ctx, chat, b.statusText(ctx), &Keyboard{Rows: [][]Button{back("a:home")}})
	case "report":
		b.send(ctx, chat, b.reportText(ctx), &Keyboard{Rows: [][]Button{back("a:home")}})
	case "nodes":
		b.nodesView(ctx, chat)
	case "orders":
		list, _ := b.Store.Orders(ctx, b.owner(), "pending", 20)
		if len(list) == 0 {
			b.send(ctx, chat, "سفارش در انتظاری نیست ✅", &Keyboard{Rows: [][]Button{back("a:home")}})
		}
		for _, o := range list {
			b.send(ctx, chat, b.orderCard(ctx, o), orderButtons(o.ID))
		}
	case "clients":
		b.clientsView(ctx, chat, arg(1))
	case "find":
		b.setAwait(chat, "find")
		b.send(ctx, chat, "🔎 نام کلاینت یا بخشی از آن (یا یادداشتش) را بفرستید:", nil)
	case "c": // client card
		b.ownerClient(ctx, chat, arg(1))
	case "sub":
		b.sendSub(ctx, chat, arg(1))
	case "renew": // a:renew:<email>:<days>:<gb>
		days, _ := strconv.ParseInt(arg(2), 10, 64)
		g, _ := strconv.ParseFloat(arg(3), 64)
		on := true
		n, err := b.Store.BulkExtend(ctx, hubcore.BulkRequest{Emails: []string{arg(1)}, AddDays: days, AddBytes: int64(g * (1 << 30)), Enable: &on})
		if err != nil || n == 0 {
			b.send(ctx, chat, "انجام نشد.", nil)
			return true
		}
		b.ownerClient(ctx, chat, arg(1))
	case "toggle":
		b.toggleClient(ctx, chat, arg(1))
	case "del":
		b.send(ctx, chat, "⚠️ کلاینت <b>"+esc(arg(1))+"</b> حذف شود؟ سابقهٔ مصرف حفظ می‌شود.", &Keyboard{Rows: [][]Button{
			{{Text: "بله، حذف شود", Data: "a:delok:" + arg(1)}, {Text: "انصراف", Data: "a:c:" + arg(1)}}}})
	case "delok":
		if err := b.Store.DeleteClient(ctx, arg(1)); err != nil {
			b.send(ctx, chat, "حذف نشد.", nil)
			return true
		}
		b.send(ctx, chat, "🗑 حذف شد.", &Keyboard{Rows: [][]Button{back("a:clients")}})
	case "quick":
		b.quickView(ctx, chat)
	case "qs": // quick sale: a:qs:<product>
		id, err := b.Store.CreateOrder(ctx, b.owner(), arg(1), "telegram:quick", "", "")
		if err == nil {
			var email string
			if email, err = b.Store.FulfillOrder(ctx, b.owner(), id); err == nil {
				b.Store.MarkOrderAnnounced(ctx, id, false)
				b.send(ctx, chat, "✅ کلاینت ساخته شد: <code>"+esc(email)+"</code>\nلینک و QR را برای مشتری بفرستید:", nil)
				b.sendSub(ctx, chat, email)
				return true
			}
		}
		b.send(ctx, chat, "ساخت ممکن نشد: "+esc(fmt.Sprint(err)), nil)
	case "reps":
		b.repsView(ctx, chat)
	case "rc": // a:rc:<rep>:<gb>:<units>
		g, _ := strconv.ParseFloat(arg(2), 64)
		u, _ := strconv.ParseInt(arg(3), 10, 64)
		if err := b.Store.AdjustCredit(ctx, "telegram", arg(1), int64(g*(1<<30)), u, "telegram"); err != nil {
			b.send(ctx, chat, "انجام نشد: "+esc(err.Error()), nil)
			return true
		}
		b.repsView(ctx, chat)
	case "bc":
		b.setAwait(chat, "broadcast")
		b.send(ctx, chat, "📣 متن پیام همگانی را بفرستید (یا یک عکس با کپشن).\nبرای دکمهٔ لینک، آخرین خط را این‌طور بنویسید:\n<code>[متن دکمه](https://example.com)</code>\nبعد مخاطب را انتخاب می‌کنید و پیش‌نمایش می‌گیرید.", nil)
	case "bca": // audience chosen
		b.mu.Lock()
		d := b.draft[chat]
		b.mu.Unlock()
		if d == nil {
			b.send(ctx, chat, "پیش‌نویسی نیست.", nil)
			return true
		}
		d.audience = arg(1)
		d.chats = b.audience(ctx, d.audience)
		_ = b.sendDraft(ctx, chat, d)
		b.send(ctx, chat, fmt.Sprintf("👆 پیش‌نمایش. برای «%s» یعنی %d نفر ارسال شود؟", audienceName[d.audience], len(d.chats)), &Keyboard{Rows: [][]Button{
			{{Text: "✅ ارسال", Data: "a:bcok"}, {Text: "✖️ لغو", Data: "a:bcno"}}, {{Text: "تغییر مخاطب", Data: "a:bcaud"}}}})
	case "bcaud":
		b.askAudience(ctx, chat)
	case "bcok":
		b.mu.Lock()
		d := b.draft[chat]
		delete(b.draft, chat)
		b.bcStop = false
		b.mu.Unlock()
		if d == nil || d.audience == "" {
			b.send(ctx, chat, "پیش‌نویسی نیست.", nil)
			return true
		}
		go b.broadcast(context.WithoutCancel(ctx), chat, d)
		b.send(ctx, chat, "📣 ارسال شروع شد… گزارش پس از پایان می‌آید.", &Keyboard{Rows: [][]Button{{{Text: "⏹ توقف ارسال", Data: "a:bcstop"}}}})
	case "bcstop":
		b.mu.Lock()
		b.bcStop = true
		b.mu.Unlock()
		b.send(ctx, chat, "⏹ ادامهٔ ارسال متوقف می‌شود (پیام‌های ارسال‌شده پس گرفته نمی‌شوند).", nil)
	case "bcno":
		b.mu.Lock()
		delete(b.draft, chat)
		b.mu.Unlock()
		b.send(ctx, chat, "لغو شد.", &Keyboard{Rows: [][]Button{back("a:home")}})
	case "backup":
		b.sendBackup(ctx, chat)
	case "settings":
		pay := b.Store.Setting(ctx, "tg_pay_text")
		wel := b.Store.Setting(ctx, "tg_welcome")
		b.send(ctx, chat, fmt.Sprintf("⚙️ <b>تنظیمات ربات</b>\n\n💳 متن پرداخت:\n%s\n\n👋 متن خوش‌آمد:\n%s", orDash(esc(pay)), orDash(esc(wel))), &Keyboard{Rows: [][]Button{
			{{Text: "ویرایش متن پرداخت", Data: "a:set:pay"}, {Text: "ویرایش خوش‌آمد", Data: "a:set:welcome"}},
			{{Text: "ویرایش آموزش اتصال", Data: "a:set:guide"}, {Text: "🔐 رمز بکاپ", Data: "a:set:backup"}},
			back("a:home")}})
	case "set":
		b.setAwait(chat, "set:"+arg(1))
		b.send(ctx, chat, "متن جدید را بفرستید:", nil)
	default:
		return false
	}
	return true
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ownerInput handles a typed answer the owner was asked for.
func (b *Bot) ownerInput(ctx context.Context, chat int64, what, text string) bool {
	switch {
	case what == "find":
		b.findClients(ctx, chat, text)
	case what == "broadcast":
		b.prepareBroadcast(ctx, chat, text, "")
	case strings.HasPrefix(what, "set:"):
		if what == "set:backup" {
			if len(text) < 8 {
				b.send(ctx, chat, "رمز باید حداقل ۸ کاراکتر باشد.", nil)
				return true
			}
			_ = b.Store.SetSetting(ctx, "tg_backup_pass", text)
			b.send(ctx, chat, "🔐 رمز بکاپ ذخیره شد. این رمز را جایی امن یادداشت کنید؛ بدون آن بکاپ باز نمی‌شود.\nاز این به بعد هر روز یک بکاپ رمزگذاری‌شده همین‌جا می‌آید.", &Keyboard{Rows: [][]Button{{{Text: "📦 بکاپ الان", Data: "a:backup"}}}})
			return true
		}
		key := map[string]string{"pay": "tg_pay_text", "welcome": "tg_welcome", "guide": "tg_guide"}[strings.TrimPrefix(what, "set:")]
		if key == "" {
			return false
		}
		_ = b.Store.SetSetting(ctx, key, text)
		b.send(ctx, chat, "ذخیره شد ✅", &Keyboard{Rows: [][]Button{back("a:settings")}})
	default:
		return false
	}
	return true
}

type bcDraft struct {
	text, photo string
	button      *Button
	audience    string
	chats       []int64
}

var audienceName = map[string]string{"all": "همه", "active": "دارای سرویس فعال", "expiring": "رو به انقضا (۳ روز)", "expired": "منقضی یا حجم‌تمام", "none": "بدون سرویس"}

var buttonRE = regexp.MustCompile(`\[([^\]]{1,60})\]\((https?://[^\s)]+)\)\s*$`)

func (b *Bot) prepareBroadcast(ctx context.Context, chat int64, text, photo string) {
	d := &bcDraft{text: text, photo: photo}
	if m := buttonRE.FindStringSubmatch(text); m != nil {
		d.button = &Button{Text: m[1], URL: m[2]}
		d.text = strings.TrimSpace(strings.TrimSuffix(text, m[0]))
	}
	if d.text == "" && d.photo == "" {
		b.send(ctx, chat, "پیام خالی است.", nil)
		return
	}
	b.mu.Lock()
	if b.draft == nil {
		b.draft = map[int64]*bcDraft{}
	}
	b.draft[chat] = d
	b.mu.Unlock()
	b.askAudience(ctx, chat)
}

func (b *Bot) askAudience(ctx context.Context, chat int64) {
	kb := &Keyboard{}
	for _, k := range []string{"all", "active", "expiring", "expired", "none"} {
		kb.Rows = append(kb.Rows, []Button{{Text: fmt.Sprintf("%s (%d)", audienceName[k], len(b.audience(ctx, k))), Data: "a:bca:" + k}})
	}
	kb.Rows = append(kb.Rows, []Button{{Text: "✖️ لغو", Data: "a:bcno"}})
	b.send(ctx, chat, "👥 پیام برای چه کسانی ارسال شود؟", kb)
}

// audience resolves a target group to bot chats that have not blocked the bot.
func (b *Bot) audience(ctx context.Context, kind string) []int64 {
	chats, _ := b.Store.TgChats(ctx)
	if kind == "all" || kind == "" {
		return chats
	}
	links, _ := b.Store.TgLinkedChats(ctx)
	clients, _ := b.Store.Clients(ctx)
	now := b.now().Unix()
	state := map[int64]string{}
	rank := map[string]int{"expired": 1, "expiring": 2, "active": 3}
	for _, c := range clients {
		st := "active"
		switch {
		case !c.Enabled || c.Remaining == 0 || c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
			st = "expired"
		case c.EffectiveUntil > 0 && c.EffectiveUntil-now <= 3*86400:
			st = "expiring"
		}
		for _, ch := range links[c.Email] {
			if rank[st] > rank[state[ch]] {
				state[ch] = st
			}
		}
	}
	var out []int64
	for _, ch := range chats {
		st := state[ch]
		switch kind {
		case "none":
			if st == "" {
				out = append(out, ch)
			}
		case "active":
			if st == "active" || st == "expiring" {
				out = append(out, ch)
			}
		default:
			if st == kind {
				out = append(out, ch)
			}
		}
	}
	return out
}

func (b *Bot) sendDraft(ctx context.Context, chat int64, d *bcDraft) error {
	var kb *Keyboard
	if d.button != nil {
		kb = &Keyboard{Rows: [][]Button{{*d.button}}}
	}
	if d.photo != "" {
		return b.API.SendPhotoID(ctx, chat, d.photo, esc(d.text), kb)
	}
	_, err := b.API.Send(ctx, chat, esc(d.text), kb, 0)
	return err
}

func (b *Bot) broadcast(ctx context.Context, owner int64, d *bcDraft) {
	sent, failed, stopped := 0, 0, 0
	for i, c := range d.chats {
		b.mu.Lock()
		stop := b.bcStop
		b.mu.Unlock()
		if stop {
			stopped = len(d.chats) - i
			break
		}
		if err := b.sendDraft(ctx, c, d); err == nil {
			sent++
		} else {
			failed++
			if isBlocked(err) {
				b.Store.TgSetBlocked(ctx, c)
			}
		}
		time.Sleep(40 * time.Millisecond) // under Telegram's 30 messages/second
	}
	msg := fmt.Sprintf("📣 پیام همگانی تمام شد: %d ارسال، %d ناموفق.", sent, failed)
	if stopped > 0 {
		msg += fmt.Sprintf(" %d نفر به‌خاطر توقف ارسال نشدند.", stopped)
	}
	b.send(ctx, owner, msg, &Keyboard{Rows: [][]Button{back("a:home")}})
}

func (b *Bot) nodesView(ctx context.Context, chat int64) {
	nodes, _ := b.Store.Nodes(ctx)
	now := b.now()
	var lines []string
	for _, n := range nodes {
		dot, extra := "🔴", " — آفلاین"
		if n.Online(now) {
			dot, extra = "🟢", ""
			if n.Host != nil {
				ram := 0.0
				if n.Host.RAMTotal > 0 {
					ram = 100 * float64(n.Host.RAMUsed) / float64(n.Host.RAMTotal)
				}
				extra = fmt.Sprintf("\n    CPU %.0f%% · RAM %.0f%% · آنلاین %d · ↓%s/s", n.Host.CPUPercent, ram, n.Host.OnlineUsers, gb(int64(n.Host.RXRate)))
			}
		}
		name := n.Name
		if name == "" {
			name = n.ID
		}
		lines = append(lines, fmt.Sprintf("%s <b>%s</b> (%s)%s", dot, esc(name), esc(n.ID), extra))
	}
	if len(lines) == 0 {
		lines = []string{"نودی ثبت نشده."}
	}
	b.send(ctx, chat, "🧩 <b>نودها</b>\n\n"+strings.Join(lines, "\n"), &Keyboard{Rows: [][]Button{back("a:home")}})
}

func (b *Bot) clientsView(ctx context.Context, chat int64, filter string) {
	all, err := b.Store.Clients(ctx)
	if err != nil {
		return
	}
	now := b.now().Unix()
	var list []hubcore.ClientView
	title := "🕘 آخرین کلاینت‌ها"
	switch filter {
	case "exp":
		title = "⏰ رو به انقضا (۳ روز)"
		for _, c := range all {
			if c.EffectiveUntil > now && c.EffectiveUntil-now <= 3*86400 {
				list = append(list, c)
			}
		}
	case "bad":
		title = "⛔ منقضی یا حجم‌تمام"
		for _, c := range all {
			if c.Remaining == 0 || c.EffectiveUntil > 0 && c.EffectiveUntil <= now {
				list = append(list, c)
			}
		}
	case "on":
		title = "🟢 آنلاین الان"
		for _, c := range all {
			if c.Online {
				list = append(list, c)
			}
		}
	default:
		list = all
	}
	if len(list) > 12 {
		list = list[:12]
	}
	kb := &Keyboard{}
	for _, c := range list {
		kb.Rows = append(kb.Rows, []Button{{Text: clientLabel(c, now), Data: "a:c:" + c.Email}})
	}
	kb.Rows = append(kb.Rows,
		[]Button{{Text: "🟢 آنلاین", Data: "a:clients:on"}, {Text: "⏰ رو به انقضا", Data: "a:clients:exp"}, {Text: "⛔ منقضی", Data: "a:clients:bad"}},
		[]Button{{Text: "🔎 جستجو", Data: "a:find"}, {Text: "« بازگشت", Data: "a:home"}})
	msg := fmt.Sprintf("%s — %d مورد", title, len(list))
	if len(list) == 0 {
		msg = title + "\nموردی نیست."
	}
	b.send(ctx, chat, msg, kb)
}

func clientLabel(c hubcore.ClientView, now int64) string {
	dot := "⚪"
	switch {
	case !c.Enabled || c.Remaining == 0 || c.EffectiveUntil > 0 && c.EffectiveUntil <= now:
		dot = "⛔"
	case c.Online:
		dot = "🟢"
	}
	left := ""
	if c.EffectiveUntil > now {
		left = fmt.Sprintf(" · %dر", (c.EffectiveUntil-now+86399)/86400)
	}
	return dot + " " + c.Email + left
}

func (b *Bot) findClients(ctx context.Context, chat int64, q string) {
	all, _ := b.Store.Clients(ctx)
	q = strings.ToLower(strings.TrimSpace(q))
	var hits []hubcore.ClientView
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Email+" "+c.Note), q) {
			hits = append(hits, c)
		}
	}
	if len(hits) == 1 {
		b.ownerClient(ctx, chat, hits[0].Email)
		return
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Email < hits[j].Email })
	if len(hits) > 15 {
		hits = hits[:15]
	}
	kb := &Keyboard{}
	now := b.now().Unix()
	for _, c := range hits {
		kb.Rows = append(kb.Rows, []Button{{Text: clientLabel(c, now), Data: "a:c:" + c.Email}})
	}
	kb.Rows = append(kb.Rows, back("a:home"))
	b.send(ctx, chat, fmt.Sprintf("🔎 %d نتیجه برای «%s»", len(hits), esc(q)), kb)
}

func (b *Bot) ownerClient(ctx context.Context, chat int64, email string) {
	c, err := b.Store.ClientByEmail(ctx, email)
	if err != nil {
		b.send(ctx, chat, "کلاینت پیدا نشد.", &Keyboard{Rows: [][]Button{back("a:clients")}})
		return
	}
	text := b.clientCard(c)
	if c.Online {
		text += "\n🟢 الان آنلاین"
	}
	if c.RepID != "" {
		text += "\n💼 نماینده: " + esc(c.RepID)
	}
	if c.Note != "" {
		text += "\n📝 " + esc(c.Note)
	}
	if len(c.Inbounds) > 0 {
		text += "\n🔌 " + esc(strings.Join(c.Inbounds, "، "))
	}
	toggle := "⏸ غیرفعال"
	if !c.Enabled {
		toggle = "▶️ فعال"
	}
	e := c.Email
	b.send(ctx, chat, text, &Keyboard{Rows: [][]Button{
		{{Text: "🔗 لینک و QR", Data: "a:sub:" + e}},
		{{Text: "+۳۰ روز", Data: "a:renew:" + e + ":30:0"}, {Text: "+۱۰ گیگ", Data: "a:renew:" + e + ":0:10"}, {Text: "+۳۰ر و ۵۰گ", Data: "a:renew:" + e + ":30:50"}},
		{{Text: toggle, Data: "a:toggle:" + e}, {Text: "🗑 حذف", Data: "a:del:" + e}},
		back("a:clients"),
	}})
}

func (b *Bot) toggleClient(ctx context.Context, chat int64, email string) {
	c, err := b.Store.ClientByEmail(ctx, email)
	if err != nil {
		return
	}
	cl := c.Client
	cl.Enabled = !cl.Enabled
	if err := b.Store.PutClient(ctx, cl); err != nil {
		b.send(ctx, chat, "انجام نشد: "+esc(err.Error()), nil)
		return
	}
	b.ownerClient(ctx, chat, email)
}

func (b *Bot) quickView(ctx context.Context, chat int64) {
	all, _ := b.Store.Products(ctx, b.owner(), true)
	kb := &Keyboard{}
	for _, p := range all {
		if p.Seller == "" {
			size := "نامحدود"
			if p.Plan == "volume" {
				size = fmt.Sprintf("%dگ", p.GB)
			}
			kb.Rows = append(kb.Rows, []Button{{Text: fmt.Sprintf("⚡ %s — %s · %dر", p.Name, size, p.Days), Data: "a:qs:" + p.ID}})
		}
	}
	kb.Rows = append(kb.Rows, back("a:home"))
	msg := "⚡ <b>فروش سریع</b>\nیک محصول را بزنید تا کلاینت فوراً ساخته شود و لینک و QR بیاید."
	if len(kb.Rows) == 1 {
		msg = "محصول منتشرشده‌ای نیست. از تب «فروشگاه» در پنل محصول بسازید."
	}
	b.send(ctx, chat, msg, kb)
}

func (b *Bot) repsView(ctx context.Context, chat int64) {
	reps, _ := b.Store.Reps(ctx)
	if len(reps) == 0 {
		b.send(ctx, chat, "نماینده‌ای نیست.", &Keyboard{Rows: [][]Button{back("a:home")}})
		return
	}
	var lines []string
	kb := &Keyboard{}
	for _, r := range reps {
		state := "✅"
		if !r.Enabled {
			state = "⏸"
		}
		lines = append(lines, fmt.Sprintf("%s <b>%s</b> — اعتبار %s · %d واحد · %d کلاینت", state, esc(r.Name), gb(r.VolumeCredit), r.UnlimitedUnits, r.Clients))
		kb.Rows = append(kb.Rows, []Button{{Text: r.Name + " +۵۰گ", Data: "a:rc:" + r.ID + ":50:0"}, {Text: "+۱ واحد", Data: "a:rc:" + r.ID + ":0:1"}})
	}
	kb.Rows = append(kb.Rows, back("a:home"))
	b.send(ctx, chat, "💼 <b>نماینده‌ها</b>\n\n"+strings.Join(lines, "\n"), kb)
}

// sendBackup sends an encrypted snapshot to one owner chat.
func (b *Bot) sendBackup(ctx context.Context, chat int64) {
	pass := b.Store.Setting(ctx, "tg_backup_pass")
	if pass == "" {
		b.setAwait(chat, "set:backup")
		b.send(ctx, chat, "🔐 اول یک رمز بکاپ (حداقل ۸ کاراکتر) بفرستید. بکاپ‌ها با این رمز رمزگذاری می‌شوند.", nil)
		return
	}
	data, err := b.Store.EncryptedBackup(ctx, pass)
	if err != nil {
		b.send(ctx, chat, "ساخت بکاپ ممکن نشد: "+esc(err.Error()), nil)
		return
	}
	name := "dark-v2-" + b.now().UTC().Format("20060102-1504") + ".dkb"
	caption := fmt.Sprintf("📦 بکاپ رمزگذاری‌شدهٔ هاب (%s)\nبازیابی: <code>dark-hub2 restore -file %s</code> (رمز را می‌پرسد)", gb(int64(len(data))), name)
	if err := b.API.SendDocument(ctx, chat, name, data, caption); err != nil {
		b.logf("tgbot backup: %v", err)
	}
}
