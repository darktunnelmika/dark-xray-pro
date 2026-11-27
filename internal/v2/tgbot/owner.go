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

// ownerMenuKB shows the pending-order count on its button.
func (b *Bot) ownerMenuKB(pending int) *Keyboard {
	orders := "🛒 سفارش‌ها"
	if pending > 0 {
		orders = "🛒 سفارش‌ها (" + faInt(int64(pending)) + ")"
	}
	kb := &Keyboard{}
	if r := b.panelAppRow(); r != nil {
		kb.Rows = append(kb.Rows, r)
	}
	for _, r := range ownerMenu.Rows {
		nr := append([]Button(nil), r...)
		for i := range nr {
			if nr[i].Data == "a:orders" {
				nr[i].Text = orders
			}
		}
		kb.Rows = append(kb.Rows, nr)
	}
	return kb
}

var ownerMenu = &Keyboard{Rows: [][]Button{
	{{Text: "📊 وضعیت", Data: "a:status"}, {Text: "🧩 نودها", Data: "a:nodes"}},
	{{Text: "👥 کلاینت‌ها", Data: "a:clients"}, {Text: "🔎 جستجو", Data: "a:find"}},
	{{Text: "⚡ فروش سریع", Data: "a:quick"}, {Text: "🛒 سفارش‌ها", Data: "a:orders"}},
	{{Text: "🧾 محصولات فروشگاه", Data: "S:h"}},
	{{Text: "💼 نماینده‌ها", Data: "a:reps"}, {Text: "📋 گزارش امروز", Data: "a:report"}},
	{{Text: "📣 پیام همگانی", Data: "a:bc"}, {Text: "⏰ زمان‌بندی‌ها", Data: "a:schlist"}},
	{{Text: "⚙️ تنظیمات ربات", Data: "a:settings"}, {Text: "📦 بکاپ الان", Data: "a:backup"}},
	{{Text: "🛍 نمای مشتری", Data: "a:customer"}, {Text: "🔄 بروزرسانی", Data: "a:home"}},
}}

func (b *Bot) ownerHome(ctx context.Context, chat int64) {
	o, _ := b.Store.Overview(ctx)
	pending, _ := b.Store.Orders(ctx, b.owner(), "pending", 100)
	share := func(a, t int) float64 {
		if t == 0 {
			return 0
		}
		return float64(a) / float64(t)
	}
	alert := ""
	if o.NodesOnline < o.Nodes {
		alert = fmt.Sprintf("\n\n⚠️ <b>%s نود آفلاین است</b>", faInt(int64(o.Nodes-o.NodesOnline)))
	}
	orders := ""
	if len(pending) > 0 {
		orders = fmt.Sprintf("\n🛒 سفارش در انتظار: <b>%s</b>", faInt(int64(len(pending))))
	}
	if b.Store.TrialPolicyFor(ctx, b.Seller).Enabled {
		if st := b.Store.TrialStats(ctx, b.Seller); st.Taken > 0 {
			orders += fmt.Sprintf("\n🎁 تست رایگان: <b>%s</b> گرفته · %s وصل · %s خرید", faInt(int64(st.Taken)), faInt(int64(st.Connected)), faInt(int64(st.Bought)))
		}
	}
	b.send(ctx, chat, fmt.Sprintf("🛡 <b>پنل مدیریت Dark VPN</b>\n%s\n📡 نودها   <code>%s</code> %s/%s\n👥 فعال    <code>%s</code> %s/%s\n🟢 آنلاین الان: <b>%s</b>\n📶 ترافیک امروز: <b>%s</b>%s%s\n\n<i>%s</i>",
		line, bar(share(o.NodesOnline, o.Nodes)), faInt(int64(o.NodesOnline)), faInt(int64(o.Nodes)),
		bar(share(o.ClientsActive, o.Clients)), faInt(int64(o.ClientsActive)), faInt(int64(o.Clients)),
		faInt(int64(o.OnlineUsers)), size(o.TrafficToday), orders, alert, faTime(b.now().Unix())), b.ownerMenuKB(len(pending)))
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
		page, _ := strconv.Atoi(arg(2))
		b.clientsPage(ctx, chat, arg(1), page)
	case "find":
		b.setAwait(chat, "find")
		b.send(ctx, chat, "🔎 نام کلاینت یا بخشی از آن (یا یادداشتش) را بفرستید:", &Keyboard{Rows: [][]Button{back("a:home")}})
	case "c": // client card
		b.ownerClient(ctx, chat, arg(1))
	case "cfg":
		b.showConfigs(ctx, chat, arg(1))
	case "use":
		b.showUsage(ctx, chat, arg(1))
	case "reset":
		b.send(ctx, chat, "♻️ مصرف <b>"+esc(arg(1))+"</b> صفر شود؟ حجم کامل دوباره در دسترس قرار می‌گیرد.", &Keyboard{Rows: [][]Button{
			row(cbBtn("بله، صفر شود", "a:resetok:"+arg(1)), cbBtn("انصراف", "a:c:"+arg(1)))}})
	case "resetok":
		if err := b.Store.ResetUsage(ctx, arg(1), "telegram"); err != nil {
			b.send(ctx, chat, "انجام نشد: "+esc(faErr(err)), backTo("a:c:"+arg(1), "کلاینت"))
			return true
		}
		b.ownerClient(ctx, chat, arg(1))
	case "rot":
		b.send(ctx, chat, "🔁 لینک اشتراک و کلید اتصال <b>"+esc(arg(1))+"</b> عوض شود؟ لینک قبلی دیگر کار نمی‌کند و مشتری باید لینک تازه را وارد کند.", &Keyboard{Rows: [][]Button{
			row(cbBtn("بله، لینک تازه", "a:rotok:"+arg(1)), cbBtn("انصراف", "a:c:"+arg(1)))}})
	case "rotok":
		if err := b.Store.RotateClient(ctx, arg(1), true, true); err != nil {
			b.send(ctx, chat, "انجام نشد: "+esc(faErr(err)), backTo("a:c:"+arg(1), "کلاینت"))
			return true
		}
		b.ownerClient(ctx, chat, arg(1))
	case "node":
		b.nodeCard(ctx, chat, arg(1))
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
	case "qs": // quick sale: a:qs:<product> — ask for the client's name first
		b.setAwait(chat, "qsname:"+arg(1))
		b.send(ctx, chat, "✍️ نام کلاینت را بفرستید (حروف انگلیسی و عدد، مثل ali-30gb) یا «نام خودکار» را بزنید:", quickNameKB(arg(1)))
	case "qsgo": // a:qsgo:<product> — automatic name
		b.takeAwait(chat)
		b.quickSell(ctx, chat, arg(1), "")
	case "reps":
		b.repsView(ctx, chat)
	case "repnew":
		b.setAwait(chat, "repnew")
		b.send(ctx, chat, "➕ <b>نمایندهٔ جدید</b>\nنام کاربری پنل را بفرستید (حروف کوچک انگلیسی و عدد، مثل <code>arda</code>).\nرمز را خودم تصادفی می‌سازم.", &Keyboard{Rows: [][]Button{back("a:reps")}})
	case "repnewp": // a:repnewp:<id>:<package or ->
		b.repNewCreate(ctx, chat, arg(1), arg(2))
	case "rc": // a:rc:<rep>:<gb>:<units>
		g, _ := strconv.ParseFloat(arg(2), 64)
		u, _ := strconv.ParseInt(arg(3), 10, 64)
		if err := b.Store.AdjustCredit(ctx, "telegram", arg(1), int64(g*(1<<30)), u, "telegram"); err != nil {
			b.send(ctx, chat, "انجام نشد: "+esc(faErr(err)), nil)
			return true
		}
		b.repsView(ctx, chat)
	case "bc":
		b.setAwait(chat, "broadcast")
		b.send(ctx, chat, "📣 متن پیام همگانی را بفرستید (یا یک عکس با کپشن).\nبرای دکمهٔ لینک، آخرین خط را این‌طور بنویسید:\n<code>[متن دکمه](https://example.com)</code>\nبعد مخاطب را انتخاب می‌کنید و پیش‌نمایش می‌گیرید.", &Keyboard{Rows: [][]Button{back("a:home")}})
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
			{{Text: "✅ ارسال الان", Data: "a:bcok"}, {Text: "⏰ زمان‌بندی", Data: "a:bcsch"}},
			{{Text: "تغییر مخاطب", Data: "a:bcaud"}, {Text: "✖️ لغو", Data: "a:bcno"}}}})
	case "bcsch":
		b.askWhen(ctx, chat)
	case "bcat":
		at, ok := quickWhen(arg(1), b.now())
		if !ok {
			return true
		}
		b.takeAwait(chat)
		b.setWhen(ctx, chat, at)
	case "bcrep":
		b.saveScheduled(ctx, chat, arg(1))
	case "schlist":
		b.scheduledView(ctx, chat)
	case "schcancel":
		id, _ := strconv.ParseInt(arg(1), 10, 64)
		if err := b.Store.TgScheduledCancel(ctx, id); err != nil {
			b.send(ctx, chat, "پیدا نشد یا قبلاً ارسال شده.", nil)
			return true
		}
		b.send(ctx, chat, fmt.Sprintf("🗑 زمان‌بندی #%d لغو شد.", id), nil)
		b.scheduledView(ctx, chat)
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
		_ = b.sendBackup(ctx, chat)
	case "settings":
		pay := b.Store.Setting(ctx, "tg_pay_text")
		wel := b.Store.Setting(ctx, "tg_welcome")
		b.send(ctx, chat, fmt.Sprintf("⚙️ <b>تنظیمات ربات</b>\n\n💳 متن پرداخت:\n%s\n\n👋 متن خوش‌آمد:\n%s", orDash(esc(pay)), orDash(esc(wel))), &Keyboard{Rows: [][]Button{
			{{Text: "ویرایش متن پرداخت", Data: "a:set:pay"}, {Text: "ویرایش خوش‌آمد", Data: "a:set:welcome"}},
			{{Text: "ویرایش آموزش اتصال", Data: "a:set:guide"}, {Text: "🔐 رمز بکاپ", Data: "a:set:backup"}},
			{{Text: "🎁 تست رایگان", Data: "a:trial"}, {Text: "🤝 فروش پنل نمایندگی", Data: "P:cfg"}},
			{{Text: "📢 عضویت اجباری در کانال", Data: "a:join"}},
			back("a:home")}})
	case "join": // a:join[:on|off|add|del:<n>] — required channels of the main bot
		b.takeAwait(chat)
		b.joinCallback(ctx, chat, "", "a:join", "a:settings", arg(1), arg(2))
	case "trial": // a:trial[:on|off|gb|h] — the owner's free trial
		b.takeAwait(chat)
		b.trialCallback(ctx, chat, "", "a:trial", "a:settings", arg(1))
	case "set":
		b.setAwait(chat, "set:"+arg(1))
		prompt := map[string]string{
			"pay":     "💳 <b>متن پرداخت</b> را بفرستید (زیر فاکتور به مشتری نشان داده می‌شود، مثلاً شرایط یا ساعت پاسخ‌گویی):",
			"welcome": "👋 <b>متن خوش‌آمد</b> را بفرستید (بالای منوی مشتری‌های تازه):",
			"guide":   "📚 <b>متن آموزش اتصال</b> را بفرستید:",
			"backup":  "🔐 <b>رمز بکاپ</b> را بفرستید (حداقل ۸ کاراکتر). بکاپ‌ها با این رمز رمزگذاری می‌شوند؛ بدون آن باز نمی‌شوند، پس جایی نگهش دارید.",
		}[arg(1)]
		if prompt == "" {
			prompt = "متن جدید را بفرستید:"
		}
		b.send(ctx, chat, prompt, &Keyboard{Rows: [][]Button{back("a:settings")}})
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
	if b.trialInput(ctx, chat, what, text) {
		return true
	}
	if b.joinInput(ctx, chat, what, text) {
		return true
	}
	switch {
	case strings.HasPrefix(what, "qsname:"):
		b.quickSell(ctx, chat, strings.TrimPrefix(what, "qsname:"), strings.TrimSpace(text))
	case what == "repnew":
		b.repNewAsk(ctx, chat, text)
	case what == "find":
		b.findClients(ctx, chat, text)
	case what == "broadcast":
		b.prepareBroadcast(ctx, chat, text, "")
	case what == "schedtime":
		at, err := parseWhen(text, b.now())
		if err != nil {
			b.send(ctx, chat, "این زمان را نفهمیدم. مثال: <code>21:30</code> یا <code>+2h</code> یا <code>1405/08/25 21:30</code>", nil)
			b.setAwait(chat, "schedtime")
			return true
		}
		b.setWhen(ctx, chat, at)
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
	at          int64               // scheduled time (unix), while scheduling
	cb          string              // button prefix of who runs it: "a:" (owner) or "R:" (reseller)
	title       string              // report heading; default "پیام همگانی"
	done        func(result string) // scheduled runs record their outcome
}

var audienceName = map[string]string{"all": "همه", "active": "دارای سرویس فعال", "expiring": "رو به انقضا (۳ روز)", "expired": "منقضی یا حجم‌تمام", "none": "بدون سرویس", "reps": "نماینده‌ها"}

var buttonRE = regexp.MustCompile(`\[([^\]]{1,60})\]\((https?://[^\s)]+)\)\s*$`)

func (b *Bot) prepareBroadcast(ctx context.Context, chat int64, text, photo string) {
	b.prepareBroadcastFor(ctx, chat, text, photo, "a:")
}

// prepareBroadcastFor starts a broadcast draft for the owner ("a:") or for a
// reseller in their own bot ("R:").
func (b *Bot) prepareBroadcastFor(ctx context.Context, chat int64, text, photo, cb string) {
	d := &bcDraft{text: text, photo: photo, cb: cb}
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
	b.mu.Lock()
	d := b.draft[chat]
	b.mu.Unlock()
	cb := "a:"
	if d != nil && d.cb != "" {
		cb = d.cb
	}
	kinds := []string{"all", "active", "expiring", "expired", "none"}
	if cb == "a:" && b.Seller == "" {
		kinds = append(kinds, "reps") // the owner can also write to the resellers
	}
	kb := &Keyboard{}
	for _, k := range kinds {
		kb.Rows = append(kb.Rows, []Button{{Text: fmt.Sprintf("%s (%d)", audienceName[k], len(b.audience(ctx, k))), Data: cb + "bca:" + k}})
	}
	kb.Rows = append(kb.Rows, []Button{{Text: "✖️ لغو", Data: cb + "bcno"}})
	b.send(ctx, chat, "👥 پیام برای چه کسانی ارسال شود؟", kb)
}

// audience resolves a target group to bot chats that have not blocked the bot.
func (b *Bot) audience(ctx context.Context, kind string) []int64 {
	if kind == "reps" {
		if b.Seller != "" {
			return nil
		}
		return b.Store.TgAllRepChats(ctx)
	}
	// Only people who started this very bot can get a message from it.
	chats := b.Store.TgBotChats(ctx, b.Seller)
	if kind == "all" || kind == "" {
		return chats
	}
	links, _ := b.Store.TgLinkedChats(ctx)
	clients, _ := b.Store.Clients(ctx)
	now := b.now().Unix()
	state := map[int64]string{}
	rank := map[string]int{"expired": 1, "expiring": 2, "active": 3}
	for _, c := range clients {
		if b.Seller != "" && c.RepID != b.Seller {
			continue // a reseller's bot knows only that reseller's services
		}
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
			if isBlocked(err) && d.audience != "reps" {
				b.Store.TgSetBlockedBot(ctx, b.Seller, c)
			}
		}
		time.Sleep(40 * time.Millisecond) // under Telegram's 30 messages/second
	}
	title := d.title
	if title == "" {
		title = "📣 پیام همگانی"
	}
	result := fmt.Sprintf("%d ارسال، %d ناموفق", sent, failed)
	if stopped > 0 {
		result += fmt.Sprintf("، %d متوقف", stopped)
	}
	if d.done != nil {
		d.done(result)
	}
	msg := fmt.Sprintf("%s تمام شد: %d ارسال، %d ناموفق.", title, sent, failed)
	if stopped > 0 {
		msg += fmt.Sprintf(" %d نفر به‌خاطر توقف ارسال نشدند.", stopped)
	}
	home := "a:home"
	if d.cb == "R:" {
		home = "R:home"
	}
	b.send(ctx, owner, msg, &Keyboard{Rows: [][]Button{back(home)}})
}

func (b *Bot) nodesView(ctx context.Context, chat int64) {
	nodes, _ := b.Store.Nodes(ctx)
	now := b.now()
	var lines []string
	kb := &Keyboard{}
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
		kb.Rows = append(kb.Rows, row(cbBtn(dot+" "+name, "a:node:"+n.ID)))
	}
	if len(lines) == 0 {
		lines = []string{"نودی ثبت نشده."}
	}
	kb.Rows = append(kb.Rows, row(cbBtn("🔄 بروزرسانی", "a:nodes"), cbBtn("« پنل", "a:home")))
	b.send(ctx, chat, "🧩 <b>نودها</b>\n"+line+"\n"+strings.Join(lines, "\n"), kb)
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
	text := b.serviceCard(c)
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
		row(cbBtn("🔗 لینک و QR", "a:sub:"+e), cbBtn("🧩 کانفیگ‌ها", "a:cfg:"+e), cbBtn("📈 مصرف", "a:use:"+e)),
		row(cbBtn("➕ ۳۰ روز", "a:renew:"+e+":30:0"), cbBtn("➕ ۱۰ گیگ", "a:renew:"+e+":0:10"), cbBtn("➕ ۳۰ر و ۵۰گ", "a:renew:"+e+":30:50")),
		row(cbBtn("♻️ صفر کردن مصرف", "a:reset:"+e), cbBtn("🔁 لینک تازه", "a:rot:"+e)),
		row(cbBtn(toggle, "a:toggle:"+e), cbBtn("🗑 حذف", "a:del:"+e)),
		row(cbBtn("🔄", "a:c:"+e), cbBtn("« کلاینت‌ها", "a:clients")),
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
		b.send(ctx, chat, "انجام نشد: "+esc(faErr(err)), nil)
		return
	}
	b.ownerClient(ctx, chat, email)
}

func quickNameKB(product string) *Keyboard {
	return &Keyboard{Rows: [][]Button{{{Text: "🎲 نام خودکار", Data: "a:qsgo:" + product}}, back("a:quick")}}
}

// quickSell creates the client of a quick sale; name empty means automatic.
func (b *Bot) quickSell(ctx context.Context, chat int64, product, name string) {
	var id int64
	var err error
	if name == "" {
		id, err = b.Store.CreateOrder(ctx, b.owner(), product, "telegram:quick", "", "")
	} else {
		id, err = b.Store.CreateOrderNamed(ctx, b.owner(), product, "telegram:quick", name, "")
	}
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "already exists"):
			b.setAwait(chat, "qsname:"+product)
			b.send(ctx, chat, "این نام قبلاً استفاده شده؛ نام دیگری بفرستید:", quickNameKB(product))
		case strings.Contains(err.Error(), "the name may use"):
			b.setAwait(chat, "qsname:"+product)
			b.send(ctx, chat, "نام فقط حروف انگلیسی، عدد و . _ - باشد (بدون فاصله). دوباره بفرستید:", quickNameKB(product))
		default:
			b.send(ctx, chat, "ساخت ممکن نشد: "+esc(faErr(err)), nil)
		}
		return
	}
	email, err := b.Store.FulfillOrder(ctx, b.owner(), id)
	if err != nil {
		b.send(ctx, chat, "ساخت ممکن نشد: "+esc(faErr(err)), nil)
		return
	}
	b.Store.MarkOrderAnnounced(ctx, id, false)
	b.send(ctx, chat, "✅ کلاینت ساخته شد: <code>"+esc(email)+"</code>\nلینک و QR را برای مشتری بفرستید:", nil)
	b.sendSub(ctx, chat, email)
}

func (b *Bot) quickView(ctx context.Context, chat int64) {
	all, _ := b.Store.Products(ctx, b.owner(), true)
	kb := &Keyboard{}
	for _, p := range all {
		if p.Seller == "" {
			size := "نامحدود"
			if p.Plan == "volume" {
				size = faInt(p.GB) + " گیگ"
			}
			kb.Rows = append(kb.Rows, []Button{{Text: fmt.Sprintf("⚡ %s — %s · %s روز", p.Name, size, faInt(p.Days)), Data: "a:qs:" + p.ID}})
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
	newRep := []Button{{Text: "➕ نمایندهٔ جدید", Data: "a:repnew"}}
	if len(reps) == 0 {
		b.send(ctx, chat, "نماینده‌ای نیست.", &Keyboard{Rows: [][]Button{newRep, back("a:home")}})
		return
	}
	var lines []string
	kb := &Keyboard{}
	for _, r := range reps {
		state := "✅"
		if !r.Enabled {
			state = "⏸"
		}
		lines = append(lines, fmt.Sprintf("%s <b>%s</b> — اعتبار %s · %s واحد · %s کلاینت", state, esc(r.Name), gb(r.VolumeCredit), faInt(r.UnlimitedUnits), faInt(int64(r.Clients))))
		kb.Rows = append(kb.Rows, []Button{{Text: r.Name + " +۵۰گ", Data: "a:rc:" + r.ID + ":50:0"}, {Text: "+۱ واحد", Data: "a:rc:" + r.ID + ":0:1"}})
	}
	kb.Rows = append(kb.Rows, newRep, back("a:home"))
	b.send(ctx, chat, "💼 <b>نماینده‌ها</b>\n\n"+strings.Join(lines, "\n"), kb)
}

// repNewAsk shows the packages a new reseller can start with.
func (b *Bot) repNewAsk(ctx context.Context, chat int64, id string) {
	id = strings.ToLower(strings.TrimSpace(id))
	if len(id) > 32 || !b.Store.RepNameFree(ctx, id) {
		b.setAwait(chat, "repnew")
		b.send(ctx, chat, "این نام را نمی‌شود گذاشت: یا قبلاً گرفته شده، یا چیزی جز حروف کوچک انگلیسی، عدد، نقطه و خط تیره دارد (حداکثر ۳۲ نویسه).\nیک نام دیگر بفرستید:", &Keyboard{Rows: [][]Button{back("a:reps")}})
		return
	}
	kb := &Keyboard{}
	for _, p := range b.Store.RepSale(ctx).Packages {
		name := p.Name
		if name == "" {
			name = gbWord(p.GB)
		}
		kb.Rows = append(kb.Rows, []Button{{Text: "📦 " + name, Data: "a:repnewp:" + id + ":" + p.ID}})
	}
	kb.Rows = append(kb.Rows, []Button{{Text: "بدون بسته (اعتبار صفر، تنظیم پیش‌فرض)", Data: "a:repnewp:" + id + ":-"}}, back("a:reps"))
	b.send(ctx, chat, fmt.Sprintf("نمایندهٔ <code>%s</code> با کدام بسته ساخته شود؟\nاعتبار و محدودیت‌های بسته همان لحظه روی پنل می‌نشیند.", esc(id)), kb)
}

// repNewCreate makes the reseller with a random password and shows the
// message to pass on.
func (b *Bot) repNewCreate(ctx context.Context, chat int64, id, pkgID string) {
	in := hubcore.RepCreate{ID: id, Name: id, Password: hubcore.RandomPassword()}
	credit := "بدون اعتبار"
	if pkgID != "-" {
		found := false
		for _, p := range b.Store.RepSale(ctx).Packages {
			if p.ID == pkgID {
				pol := p.Policy()
				in.Policy, in.CreditGB, in.Units, found = &pol, float64(p.GB), p.Units, true
				credit = gbWord(p.GB)
			}
		}
		if !found {
			b.send(ctx, chat, "این بسته دیگر وجود ندارد.", &Keyboard{Rows: [][]Button{back("a:reps")}})
			return
		}
	}
	if err := b.Store.CreateRep(ctx, "telegram", in); err != nil {
		b.send(ctx, chat, "ساخته نشد: "+esc(faErr(err)), &Keyboard{Rows: [][]Button{back("a:reps")}})
		return
	}
	b.send(ctx, chat, fmt.Sprintf("✅ نمایندهٔ <code>%s</code> ساخته شد (%s).\nپیام زیر را برایش بفرستید؛ رمز دیگر نمایش داده نمی‌شود.", esc(id), credit), nil)
	b.send(ctx, chat, fmt.Sprintf("🎉 <b>پنل نمایندگی شما آماده است</b>\n\n🌐 آدرس: %s\n👤 نام کاربری: <code>%s</code>\n🔑 رمز: <code>%s</code>\n\nبعد از اولین ورود، رمز را از بخش تنظیمات عوض کنید.",
		esc(strings.TrimRight(b.SubBase, "/")), esc(id), esc(in.Password)), &Keyboard{Rows: [][]Button{back("a:reps")}})
}

// backupToGroup posts the backup in the group's backup topic (false = no group).
func (b *Bot) backupToGroup(ctx context.Context) bool {
	if _, _, ok := b.noticeTarget(ctx, "backups"); !ok {
		return false
	}
	pass := b.Store.Setting(ctx, "tg_backup_pass")
	if pass == "" {
		return false
	}
	data, err := b.Store.FullBackup(ctx, pass)
	if err != nil {
		data, err = b.Store.EncryptedBackup(ctx, pass)
	}
	if err != nil {
		return false
	}
	name := "dark-v2-" + b.now().UTC().Format("20060102-1504") + ".dkb"
	caption := fmt.Sprintf("📦 بکاپ کامل و رمزگذاری‌شدهٔ هاب (%s)\nبازیابی: پنل ← تنظیمات ← بازگردانی بکاپ", gb(int64(len(data))))
	return b.notifyDoc(ctx, "backups", name, data, caption)
}

// sendBackup sends an encrypted snapshot to one owner chat.
func (b *Bot) sendBackup(ctx context.Context, chat int64) error {
	pass := b.Store.Setting(ctx, "tg_backup_pass")
	if pass == "" {
		b.setAwait(chat, "set:backup")
		b.send(ctx, chat, "🔐 اول یک رمز بکاپ (حداقل ۸ کاراکتر) بفرستید. بکاپ‌ها با این رمز رمزگذاری می‌شوند؛ بدون آن باز نمی‌شوند، پس جایی نگهش دارید.", &Keyboard{Rows: [][]Button{back("a:home")}})
		return fmt.Errorf("no backup password")
	}
	// A full backup (database + the HUB's keys and settings) lets a new
	// server take over without reinstalling nodes.
	data, err := b.Store.FullBackup(ctx, pass)
	if err != nil {
		data, err = b.Store.EncryptedBackup(ctx, pass)
	}
	if err != nil {
		b.send(ctx, chat, "ساخت بکاپ ممکن نشد: "+esc(faErr(err)), nil)
		return err
	}
	name := "dark-v2-" + b.now().UTC().Format("20060102-1504") + ".dkb"
	caption := fmt.Sprintf("📦 بکاپ کامل و رمزگذاری‌شدهٔ هاب (%s)\nبازیابی روی همین سرور: پنل ← تنظیمات ← بازگردانی بکاپ\nروی سرور جدید: <code>dark-hub2 restore -file %s -new-ip IP_جدید</code> (رمز را می‌پرسد؛ نودها خودشان وصل می‌شوند)", gb(int64(len(data))), name)
	if err := b.API.SendDocument(ctx, chat, name, data, caption); err != nil {
		b.logf("tgbot backup: %v", err)
		return err
	}
	return nil
}

const clientsPerPage = 10

// clientsPage lists clients ten at a time with page buttons.
func (b *Bot) clientsPage(ctx context.Context, chat int64, filter string, page int) {
	all, err := b.Store.Clients(ctx)
	if err != nil {
		return
	}
	now := b.now().Unix()
	var list []hubcore.ClientView
	title := "👥 همهٔ کلاینت‌ها"
	for _, c := range all {
		ok := true
		switch filter {
		case "exp":
			title = "⏰ رو به انقضا (۳ روز)"
			ok = c.EffectiveUntil > now && c.EffectiveUntil-now <= 3*86400
		case "bad":
			title = "⛔ منقضی یا حجم‌تمام"
			ok = c.Remaining == 0 || c.EffectiveUntil > 0 && c.EffectiveUntil <= now
		case "on":
			title = "🟢 آنلاین الان"
			ok = c.Online
		case "off":
			title = "⏸ غیرفعال"
			ok = !c.Enabled
		}
		if ok {
			list = append(list, c)
		}
	}
	pages := (len(list) + clientsPerPage - 1) / clientsPerPage
	if page < 0 || page >= pages {
		page = 0
	}
	kb := &Keyboard{}
	from := page * clientsPerPage
	for _, c := range list[from:min(from+clientsPerPage, len(list))] {
		kb.Rows = append(kb.Rows, row(cbBtn(clientLabel(c, now), "a:c:"+c.Email)))
	}
	if pages > 1 {
		var nav []Button
		if page > 0 {
			nav = append(nav, cbBtn("◀️ قبلی", fmt.Sprintf("a:clients:%s:%d", filter, page-1)))
		}
		nav = append(nav, cbBtn(fmt.Sprintf("%s / %s", faInt(int64(page+1)), faInt(int64(pages))), fmt.Sprintf("a:clients:%s:%d", filter, page)))
		if page < pages-1 {
			nav = append(nav, cbBtn("بعدی ▶️", fmt.Sprintf("a:clients:%s:%d", filter, page+1)))
		}
		kb.Rows = append(kb.Rows, nav)
	}
	kb.Rows = append(kb.Rows,
		row(cbBtn("🟢 آنلاین", "a:clients:on"), cbBtn("⏰ رو به انقضا", "a:clients:exp"), cbBtn("⛔ منقضی", "a:clients:bad")),
		row(cbBtn("👥 همه", "a:clients"), cbBtn("⏸ غیرفعال", "a:clients:off"), cbBtn("🔎 جستجو", "a:find")),
		row(cbBtn("« پنل", "a:home")))
	msg := fmt.Sprintf("%s\n%s\n%s مورد", title, line, faInt(int64(len(list))))
	if len(list) == 0 {
		msg = title + "\n" + line + "\nموردی نیست ✨"
	}
	b.send(ctx, chat, msg, kb)
}

// nodeCard shows one node's health with bars.
func (b *Bot) nodeCard(ctx context.Context, chat int64, id string) {
	nodes, _ := b.Store.Nodes(ctx)
	now := b.now()
	for _, n := range nodes {
		if n.ID != id {
			continue
		}
		name := n.Name
		if name == "" {
			name = n.ID
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "🧩 <b>%s</b> <code>%s</code>\n%s\n", esc(name), esc(n.ID), line)
		if !n.Online(now) {
			seen := "هرگز"
			if n.LastSeen > 0 {
				seen = faTime(n.LastSeen)
			}
			fmt.Fprintf(&sb, "🔴 <b>آفلاین</b>\nآخرین تماس: %s\n", seen)
		} else {
			sb.WriteString("🟢 <b>آنلاین</b>")
			if !n.XrayRunning {
				sb.WriteString(" · ⚠️ Xray متوقف است")
			}
			sb.WriteString("\n")
			if h := n.Host; h != nil {
				ram := 0.0
				if h.RAMTotal > 0 {
					ram = float64(h.RAMUsed) / float64(h.RAMTotal)
				}
				fmt.Fprintf(&sb, "🧠 CPU  <code>%s</code> %s٪\n", bar(h.CPUPercent/100), faInt(int64(h.CPUPercent+0.5)))
				fmt.Fprintf(&sb, "💾 RAM  <code>%s</code> %s٪\n", bar(ram), faInt(int64(ram*100+0.5)))
				fmt.Fprintf(&sb, "👥 آنلاین: <b>%s</b> · ⬇️ %s/s · ⬆️ %s/s\n", faInt(int64(h.OnlineUsers)), size(int64(h.RXRate)), size(int64(h.TXRate)))
			}
		}
		fmt.Fprintf(&sb, "\n🔖 ایجنت %s · Xray %s", esc(orDash(n.AgentVersion)), esc(orDash(n.XrayVersion)))
		if n.PublicIP != "" {
			fmt.Fprintf(&sb, "\n🌍 <code>%s</code>", esc(n.PublicIP))
		}
		if n.UpdateAvailable {
			sb.WriteString("\n⬆️ نسخهٔ تازه‌تر ایجنت آماده است (از پنل وب).")
		}
		b.send(ctx, chat, sb.String(), &Keyboard{Rows: [][]Button{row(cbBtn("🔄 بروزرسانی", "a:node:"+id), cbBtn("« نودها", "a:nodes"))}})
		return
	}
	b.send(ctx, chat, "نود پیدا نشد.", backTo("a:nodes", "نودها"))
}
