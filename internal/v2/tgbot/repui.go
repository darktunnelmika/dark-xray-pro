package tgbot

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Reseller management with buttons. A reseller (or one of their bot
// admins) does the daily work without typing commands: a dashboard card,
// creating a client step by step, finding clients and renewing, sending
// the link, switching off or deleting them, approving orders, credit and
// bot admins. Callbacks start with "R:"; long client names travel as short
// references kept in memory.

// repDraft is a client being created or renewed.
type repDraft struct {
	mode     string // "new" or "renew"
	target   string // renew: the client
	name     string // new: the name
	gb       float64
	days     int64
	ips      int
	firstUse bool
	plan     string // "volume" or "unlimited"
	inbounds []string
	product  string // name of the ready plan, if one was picked
}

var repNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@+-]{0,63}$`)

// repCan says which buttons a bot admin role gets.
func repCan(role, what string) bool {
	switch role {
	case "owner":
		return true
	case "manager":
		return what != "admins" // includes "shop"
	case "sales":
		return what == "new" || what == "renew" || what == "orders" || what == "credit" || what == "view" || what == "link"
	case "support":
		return what == "view" || what == "link"
	}
	return false
}

func (b *Bot) ref(email string) string {
	h := sha1.Sum([]byte(email))
	r := hex.EncodeToString(h[:5])
	b.mu.Lock()
	if b.refs == nil {
		b.refs = map[string]string{}
	}
	b.refs[r] = email
	b.mu.Unlock()
	return r
}

func (b *Bot) deref(r string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.refs[r]
}

func (b *Bot) draftOf(chat int64) *repDraft {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rdraft == nil {
		b.rdraft = map[int64]*repDraft{}
	}
	return b.rdraft[chat]
}

func (b *Bot) setDraft(chat int64, d *repDraft) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rdraft == nil {
		b.rdraft = map[int64]*repDraft{}
	}
	if d == nil {
		delete(b.rdraft, chat)
		return
	}
	b.rdraft[chat] = d
}

// repOf is the reseller this chat manages in this bot ("" = none).
func (b *Bot) repOf(ctx context.Context, chat int64) (rep, role string) {
	rep, role = b.Store.TgRepRole(ctx, chat)
	if rep == "" || !b.repInScope(ctx, rep) {
		return "", ""
	}
	return rep, role
}

func repBtn(text, data string) Button { return Button{Text: text, Data: "R:" + data} }

var repBackKB = &Keyboard{Rows: [][]Button{{repBtn("🏠 داشبورد", "home")}}}

// repHome is the reseller's dashboard card with the main buttons.
func (b *Bot) repHome(ctx context.Context, chat int64, rep, role string) {
	name, credit, units := rep, int64(0), int64(0)
	if reps, err := b.Store.Reps(ctx); err == nil {
		for _, r := range reps {
			if r.ID == rep {
				name, credit, units = r.Name, r.VolumeCredit, r.UnlimitedUnits
			}
		}
	}
	all, _ := b.Store.Clients(ctx)
	now := b.now().Unix()
	var total, active, soon, online int
	for _, c := range all {
		if c.RepID != rep {
			continue
		}
		total++
		if c.Online {
			online++
		}
		if !c.Enabled || (c.EffectiveUntil > 0 && c.EffectiveUntil <= now) || c.Remaining == 0 {
			continue
		}
		active++
		used := c.Usage.Up + c.Usage.Down
		if c.EffectiveUntil > now && c.EffectiveUntil-now <= 3*86400 || c.VolumeBytes > 0 && used*10 >= c.VolumeBytes*9 {
			soon++
		}
	}
	p := hubcore.Principal{User: "tg", Role: "rep", RepID: rep}
	pending, _ := b.Store.Orders(ctx, p, "pending", 50)
	text := fmt.Sprintf("🧑‍💼 <b>%s</b> — %s\n%s\n💳 اعتبار: <b>%s</b>", esc(name), roleName(role), line, gb(credit))
	if units > 0 {
		text += fmt.Sprintf(" · %d واحد نامحدود", units)
	}
	text += fmt.Sprintf("\n👥 کاربرها: <b>%d</b> (فعال %d · آنلاین %d)\n⏳ نیازمند تمدید: <b>%d</b>\n🛒 سفارش منتظر: <b>%d</b>", total, active, online, soon, len(pending))
	kb := &Keyboard{}
	r1 := []Button{}
	if repCan(role, "new") {
		r1 = append(r1, repBtn("➕ ساخت کاربر", "new"))
	}
	r1 = append(r1, repBtn("👥 کاربران", "cl"))
	kb.Rows = append(kb.Rows, r1)
	r2 := []Button{repBtn(fmt.Sprintf("⏳ نیازمند تمدید (%d)", soon), "ct")}
	if repCan(role, "orders") {
		r2 = append(r2, repBtn(fmt.Sprintf("🛒 سفارش‌ها (%d)", len(pending)), "ord"))
	}
	kb.Rows = append(kb.Rows, r2)
	r3 := []Button{}
	if repCan(role, "credit") {
		r3 = append(r3, repBtn("💳 اعتبار و گزارش", "cr"))
	}
	if repCan(role, "shop") && !b.Store.IsSubSeller(ctx, rep) {
		r3 = append(r3, Button{Text: "🧾 محصولات", Data: "S:h"})
	}
	if repCan(role, "admins") {
		r3 = append(r3, repBtn("⚙️ ادمین‌های ربات", "adm"))
	}
	if len(r3) > 0 {
		kb.Rows = append(kb.Rows, r3)
	}
	if repCan(role, "shop") && !b.Store.IsSubSeller(ctx, rep) {
		kb.Rows = append(kb.Rows, []Button{repBtn("🎁 تست رایگان", "trial")})
	}
	if btn, ok := b.repSaleButton(ctx, chat); ok {
		kb.Rows = append(kb.Rows, []Button{btn})
	}
	if role == "owner" && b.Store.ResaleOf(ctx, rep).Allowed && !b.Store.IsSubSeller(ctx, rep) {
		kb.Rows = append(kb.Rows, []Button{cbBtn("💼 کیف پول و فاکتورها", "P:w")})
	}
	if repCan(role, "shop") && b.Seller == rep {
		// In the reseller's own bot: a message to everyone who started it,
		// and the channels its customers must join.
		kb.Rows = append(kb.Rows, []Button{repBtn("📣 پیام همگانی", "bc"), repBtn("📢 عضویت اجباری", "join")})
	}
	kb.Rows = append(kb.Rows, []Button{repBtn("🛍 حالت مشتری", "cust")})
	b.send(ctx, chat, text, kb)
}

// repCallback handles every "R:" button. It reports whether it handled one.
func (b *Bot) repCallback(ctx context.Context, chat int64, data string) bool {
	if !strings.HasPrefix(data, "R:") {
		return false
	}
	rep, role := b.repOf(ctx, chat)
	if rep == "" {
		b.send(ctx, chat, "این بخش فقط برای نماینده‌ها و ادمین‌های ربات است.", nil)
		return true
	}
	parts := strings.Split(strings.TrimPrefix(data, "R:"), ":")
	arg := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	deny := func(what string) bool {
		if repCan(role, what) {
			return false
		}
		b.send(ctx, chat, "این کار با نقش شما مجاز نیست.", repBackKB)
		return true
	}
	switch parts[0] {
	case "home":
		b.takeAwait(chat)
		b.setDraft(chat, nil)
		b.repHome(ctx, chat, rep, role)
	case "bc", "bca", "bcok", "bcno", "bcstop", "bcaud": // a broadcast in the reseller's own bot
		if deny("shop") {
			return true
		}
		if b.Seller != rep {
			b.send(ctx, chat, "پیام همگانی فقط از ربات اختصاصی خودتان ارسال می‌شود.", repBackKB)
			return true
		}
		b.repBroadcast(ctx, chat, parts[0], arg(1))
	case "join": // R:join[:on|off|add|del:<n>] — required channels of the reseller's own bot
		if deny("shop") {
			return true
		}
		if b.Seller != rep {
			b.send(ctx, chat, "عضویت اجباری فقط در ربات اختصاصی خودتان تنظیم می‌شود.", repBackKB)
			return true
		}
		b.takeAwait(chat)
		b.joinCallback(ctx, chat, rep, "R:join", "R:home", arg(1), arg(2))
	case "trial": // R:trial[:on|off|gb|h] — this reseller's free trial
		if deny("shop") {
			return true
		}
		b.takeAwait(chat)
		b.trialCallback(ctx, chat, rep, "R:trial", "R:home", arg(1))
	case "new":
		if deny("new") {
			return true
		}
		b.setDraft(chat, &repDraft{mode: "new", ips: 2, firstUse: true, plan: "volume"})
		b.repPickPlan(ctx, chat, rep, "new")
	case "p": // a ready plan
		d := b.draftOf(chat)
		if d == nil {
			b.repHome(ctx, chat, rep, role)
			return true
		}
		prods, _ := b.Store.Products(ctx, hubcore.Principal{Role: "rep", RepID: rep}, true)
		for _, p := range prods {
			if p.ID == arg(1) && p.Seller == rep {
				d.gb, d.days, d.ips, d.firstUse, d.plan, d.inbounds, d.product = float64(p.GB), p.Days, p.MaxIPs, p.FirstUse, p.Plan, p.Inbounds, p.Name
			}
		}
		b.repNext(ctx, chat, rep)
	case "c": // custom: volume
		d := b.draftOf(chat)
		if d == nil || b.Store.IsSubSeller(ctx, rep) {
			return true
		}
		d.product = ""
		kb := &Keyboard{}
		var r []Button
		for _, g := range []int{10, 20, 30, 50, 75, 100} {
			r = append(r, repBtn(fmt.Sprintf("%d گیگ", g), "g:"+strconv.Itoa(g)))
			if len(r) == 3 {
				kb.Rows, r = append(kb.Rows, r), nil
			}
		}
		kb.Rows = append(kb.Rows, []Button{repBtn("« انصراف", "home")})
		b.send(ctx, chat, "📦 حجم را انتخاب کنید:", kb)
	case "g":
		d := b.draftOf(chat)
		if d == nil {
			return true
		}
		d.gb, _ = strconv.ParseFloat(arg(1), 64)
		d.plan = "volume"
		kb := &Keyboard{Rows: [][]Button{
			{repBtn("۱ ماه", "d:30"), repBtn("۲ ماه", "d:60"), repBtn("۳ ماه", "d:90")},
			{repBtn("« انصراف", "home")}}}
		b.send(ctx, chat, fmt.Sprintf("📦 %s گیگ — مدت را انتخاب کنید:", fmtGB(d.gb)), kb)
	case "d":
		d := b.draftOf(chat)
		if d == nil {
			return true
		}
		d.days, _ = strconv.ParseInt(arg(1), 10, 64)
		if d.mode == "new" {
			kb := &Keyboard{Rows: [][]Button{
				{repBtn("۱ کاربره", "u:1"), repBtn("۲ کاربره", "u:2"), repBtn("۳ کاربره", "u:3")},
				{repBtn("« انصراف", "home")}}}
			b.send(ctx, chat, "👤 چند کاربره باشد؟", kb)
			return true
		}
		b.repNext(ctx, chat, rep)
	case "u":
		d := b.draftOf(chat)
		if d == nil {
			return true
		}
		d.ips, _ = strconv.Atoi(arg(1))
		b.repNext(ctx, chat, rep)
	case "rnd": // automatic name
		d := b.draftOf(chat)
		if d == nil {
			return true
		}
		b.takeAwait(chat)
		d.name = randomName()
		b.repConfirm(ctx, chat, rep)
	case "ok":
		b.repApply(ctx, chat, rep, role)
	case "cl":
		kb := &Keyboard{Rows: [][]Button{
			{repBtn("🔍 جستجوی نام", "cs"), repBtn("🆕 آخرین‌ها", "cr0")},
			{repBtn("⏳ نیازمند تمدید", "ct"), repBtn("🟢 آنلاین‌ها", "co")},
			{repBtn("🏠 داشبورد", "home")}}}
		b.send(ctx, chat, "👥 <b>کاربران</b>\nکدام را ببینید؟", kb)
	case "cs":
		b.setAwait(chat, "r:search")
		b.send(ctx, chat, "🔍 نام کاربر (یا بخشی از آن) را بفرستید:", repBackKB)
	case "cr0", "ct", "co":
		b.repList(ctx, chat, rep, parts[0], "")
	case "v": // one client's card
		b.repClient(ctx, chat, rep, role, b.deref(arg(1)))
	case "lk":
		if email := b.deref(arg(1)); b.repOwns(ctx, rep, email) && !deny("link") {
			b.sendSub(ctx, chat, email)
		}
	case "rn":
		email := b.deref(arg(1))
		if !b.repOwns(ctx, rep, email) || deny("renew") {
			return true
		}
		b.setDraft(chat, &repDraft{mode: "renew", target: email})
		b.repPickPlan(ctx, chat, rep, "renew")
	case "tg":
		email := b.deref(arg(1))
		if !b.repOwns(ctx, rep, email) || deny("edit") {
			return true
		}
		c, err := b.Store.ClientByEmail(ctx, email)
		if err != nil {
			return true
		}
		cl := c.Client
		cl.Enabled = !cl.Enabled
		plan := c.Plan
		if plan == "" {
			plan = "volume"
			if c.VolumeBytes < 0 {
				plan = "unlimited"
			}
		}
		if err := b.Store.RepPutClient(ctx, "tg:"+strconv.FormatInt(chat, 10), rep, cl, plan); err != nil {
			b.send(ctx, chat, "انجام نشد: "+esc(faErr(err)), repBackKB)
			return true
		}
		b.repClient(ctx, chat, rep, role, email)
	case "dl":
		email := b.deref(arg(1))
		if !b.repOwns(ctx, rep, email) || deny("delete") {
			return true
		}
		kb := &Keyboard{Rows: [][]Button{{repBtn("🗑 بله، حذف شود", "dy:"+arg(1)), repBtn("« نه", "v:"+arg(1))}}}
		b.send(ctx, chat, fmt.Sprintf("⚠️ کاربر <b>%s</b> برای همیشه حذف شود؟ حجم مصرف‌نشده طبق قوانین به اعتبارتان برمی‌گردد.", esc(email)), kb)
	case "dy":
		email := b.deref(arg(1))
		if !b.repOwns(ctx, rep, email) || deny("delete") {
			return true
		}
		bytes, units, err := b.Store.RepDeleteClient(ctx, "tg:"+strconv.FormatInt(chat, 10), rep, email)
		if err != nil {
			b.send(ctx, chat, "حذف نشد: "+esc(faErr(err)), repBackKB)
			return true
		}
		msg := "🗑 <b>" + esc(email) + "</b> حذف شد."
		if bytes > 0 || units > 0 {
			msg += fmt.Sprintf("\n↩️ برگشت به اعتبار: %s", gb(bytes))
			if units > 0 {
				msg += fmt.Sprintf(" · %d واحد", units)
			}
		}
		b.send(ctx, chat, msg, repBackKB)
	case "ord":
		if deny("orders") {
			return true
		}
		list, _ := b.Store.Orders(ctx, hubcore.Principal{User: "tg", Role: "rep", RepID: rep}, "pending", 20)
		if len(list) == 0 {
			b.send(ctx, chat, "✅ سفارش منتظری ندارید.", repBackKB)
			return true
		}
		for _, o := range list {
			b.send(ctx, chat, b.orderCard(ctx, o), orderButtons(o.ID))
		}
	case "cr":
		if deny("credit") {
			return true
		}
		b.repCredit(ctx, chat, rep)
	case "adm":
		if deny("admins") {
			return true
		}
		b.repAdmins(ctx, chat, rep)
	case "ai":
		if deny("admins") {
			return true
		}
		code, err := b.Store.NewTgAdminInvite(ctx, rep, arg(1))
		if err != nil {
			b.send(ctx, chat, "دعوت ساخته نشد.", repBackKB)
			return true
		}
		bot := b.Store.BotUsernameFor(ctx, rep)
		b.send(ctx, chat, fmt.Sprintf("🔗 لینک دعوت ادمین با نقش «%s»\n(یک‌بار مصرف، ۱ ساعت اعتبار) — این را برای آن شخص بفرستید:\n\nhttps://t.me/%s?start=adm_%s", roleName(arg(1)), esc(bot), code), repBackKB)
	case "cust":
		b.setRepMode(ctx, chat, false)
		b.menu(ctx, chat)
	case "mode":
		b.setRepMode(ctx, chat, true)
		b.repHome(ctx, chat, rep, role)
	default:
		b.repHome(ctx, chat, rep, role)
	}
	return true
}

// repPickPlan offers the seller's ready plans plus a custom choice.
func (b *Bot) repPickPlan(ctx context.Context, chat int64, rep, mode string) {
	prods, _ := b.Store.Products(ctx, hubcore.Principal{Role: "rep", RepID: rep}, true)
	kb := &Keyboard{}
	for _, p := range prods {
		if p.Seller != rep || mode == "renew" && p.NoRenew {
			continue
		}
		kb.Rows = append(kb.Rows, []Button{repBtn("📦 "+p.Name, "p:"+p.ID)})
		if len(kb.Rows) >= 12 {
			break
		}
	}
	sub := b.Store.IsSubSeller(ctx, rep)
	if !sub {
		kb.Rows = append(kb.Rows, []Button{repBtn("✍️ حجم و مدت دلخواه", "c")})
	} else if len(kb.Rows) == 0 {
		b.send(ctx, chat, "نماینده‌تان هنوز پلنی برای شما تعریف نکرده؛ از او بخواهید در فروشگاه برایتان محصول بسازد.", repBackKB)
		return
	}
	kb.Rows = append(kb.Rows, []Button{repBtn("« انصراف", "home")})
	title := "➕ <b>ساخت کاربر</b>\nیک پلن آماده انتخاب کنید یا حجم و مدت را خودتان بزنید:"
	if mode == "renew" {
		d := b.draftOf(chat)
		title = "🔄 <b>تمدید " + esc(d.target) + "</b>\nدورهٔ جدید از امروز شروع می‌شود و مصرف صفر می‌شود. بستهٔ جدید را انتخاب کنید:"
	}
	b.send(ctx, chat, title, kb)
}

// repNext asks for whatever the draft still lacks.
func (b *Bot) repNext(ctx context.Context, chat int64, rep string) {
	d := b.draftOf(chat)
	if d == nil {
		return
	}
	if d.mode == "new" && d.name == "" {
		b.setAwait(chat, "r:name")
		kb := &Keyboard{Rows: [][]Button{{repBtn("🎲 نام خودکار", "rnd")}, {repBtn("« انصراف", "home")}}}
		b.send(ctx, chat, "✍️ نام کاربر را بفرستید (حروف انگلیسی و عدد، مثل ali-30gb) یا «نام خودکار» را بزنید:", kb)
		return
	}
	b.repConfirm(ctx, chat, rep)
}

// repConfirm shows what will happen and what it costs.
func (b *Bot) repConfirm(ctx context.Context, chat int64, rep string) {
	d := b.draftOf(chat)
	if d == nil {
		return
	}
	credit := int64(0)
	if reps, err := b.Store.Reps(ctx); err == nil {
		for _, r := range reps {
			if r.ID == rep {
				credit = r.VolumeCredit
			}
		}
	}
	size := "نامحدود"
	cost := int64(0)
	if d.plan != "unlimited" {
		size = fmtGB(d.gb) + " گیگ"
		cost = int64(d.gb * (1 << 30))
	}
	refund := int64(0)
	if d.mode == "renew" {
		if c, err := b.Store.ClientByEmail(ctx, d.target); err == nil && c.VolumeBytes > 0 {
			if left := c.VolumeBytes - c.Usage.Up - c.Usage.Down; left > 0 {
				refund = left
			}
		}
	}
	who := d.name
	head := "➕ <b>ساخت کاربر</b>"
	if d.mode == "renew" {
		who, head = d.target, "🔄 <b>تمدید</b> (دورهٔ جدید از امروز، مصرف از صفر)"
	}
	text := fmt.Sprintf("%s\n%s\n👤 %s\n📦 %s · %d روز", head, line, esc(who), size, d.days)
	if d.mode == "new" {
		text += fmt.Sprintf(" · %d کاربره", d.ips)
		if d.firstUse {
			text += "\n⏱ زمان از اولین اتصال شروع می‌شود"
		}
	}
	if d.product != "" {
		text += "\n🏷 پلن: " + esc(d.product)
	}
	text += fmt.Sprintf("\n\n💳 از اعتبار: <b>%s</b>", gb(cost))
	if refund > 0 {
		text += fmt.Sprintf("\n↩️ برگشت حجم مصرف‌نشدهٔ قبلی: %s", gb(refund))
	}
	text += fmt.Sprintf("\n💰 مانده بعد از این: <b>%s</b>", gb(credit-cost+refund))
	if credit-cost+refund < 0 {
		b.send(ctx, chat, text+"\n\n⛔️ اعتبار کافی نیست.", repBackKB)
		return
	}
	kb := &Keyboard{Rows: [][]Button{{repBtn("✅ تأیید", "ok"), repBtn("« انصراف", "home")}}}
	b.send(ctx, chat, text, kb)
}

// repApply creates or renews.
func (b *Bot) repApply(ctx context.Context, chat int64, rep, role string) {
	d := b.draftOf(chat)
	if d == nil {
		b.repHome(ctx, chat, rep, role)
		return
	}
	b.setDraft(chat, nil)
	actor := "tg:" + strconv.FormatInt(chat, 10)
	if d.mode == "renew" {
		if !repCan(role, "renew") || !b.repOwns(ctx, rep, d.target) {
			return
		}
		res, err := b.Store.RenewClient(ctx, hubcore.Principal{User: actor, Role: "rep", RepID: rep}, d.target, d.plan, d.gb, d.days)
		if err != nil {
			b.send(ctx, chat, "تمدید نشد: "+esc(friendlyErr(err)), repBackKB)
			return
		}
		msg := fmt.Sprintf("✅ <b>%s</b> تمدید شد.\nاز اعتبار: %s", esc(d.target), gb(res.ChargedBytes))
		if res.RefundBytes > 0 {
			msg += " · برگشت: " + gb(res.RefundBytes)
		}
		b.send(ctx, chat, msg, &Keyboard{Rows: [][]Button{{repBtn("🔗 لینک و QR", "lk:"+b.ref(d.target)), repBtn("🏠 داشبورد", "home")}}})
		return
	}
	if !repCan(role, "new") {
		return
	}
	inb := d.inbounds
	if len(inb) == 0 {
		inb = b.repInbounds(ctx, rep)
	}
	c := hubcore.Client{Email: d.name, Enabled: true, MaxIPs: d.ips, Inbounds: inb, VolumeBytes: -1}
	if d.plan != "unlimited" {
		c.VolumeBytes = int64(d.gb * (1 << 30))
	}
	if d.firstUse {
		c.DurationSeconds = d.days * 86400
	} else {
		c.ExpiresUnix = b.now().Unix() + d.days*86400
	}
	if _, err := b.Store.ClientByEmail(ctx, d.name); err == nil {
		b.send(ctx, chat, "این نام قبلاً استفاده شده؛ نام دیگری بفرستید.", repBackKB)
		d.name = ""
		b.setDraft(chat, d)
		b.repNext(ctx, chat, rep)
		return
	}
	if err := b.Store.RepPutClient(ctx, actor, rep, c, d.plan); err != nil {
		b.send(ctx, chat, "ساخته نشد: "+esc(friendlyErr(err)), repBackKB)
		return
	}
	b.send(ctx, chat, "✅ کاربر <b>"+esc(d.name)+"</b> ساخته شد. لینک و QR را برای مشتری فوروارد کنید 👇", nil)
	b.sendSub(ctx, chat, d.name)
	b.send(ctx, chat, "کار بعدی؟", &Keyboard{Rows: [][]Button{{repBtn("➕ یکی دیگر", "new"), repBtn("🏠 داشبورد", "home")}}})
}

func friendlyErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "insufficient credit"), strings.Contains(s, "credit"):
		return "اعتبار کافی نیست"
	case strings.Contains(s, "longest term"):
		return "مدت بیشتر از سقف مجاز شماست"
	case strings.Contains(s, "largest volume"):
		return "حجم بیشتر از سقف مجاز شماست"
	case strings.Contains(s, "sub-sellers"):
		return "فقط پلن‌هایی که نماینده برایتان تعریف کرده مجاز است"
	case strings.Contains(s, "unlimited plans are not allowed"):
		return "فروش نامحدود برای شما فعال نیست"
	case strings.Contains(s, "not allowed"):
		return "این کار برای شما مجاز نیست"
	}
	return s
}

// repInbounds: the reseller's allowed inbounds (all, when unrestricted).
func (b *Bot) repInbounds(ctx context.Context, rep string) []string {
	return b.Store.SellerInbounds(ctx, rep)
}

func (b *Bot) repOwns(ctx context.Context, rep, email string) bool {
	if email == "" {
		return false
	}
	c, err := b.Store.ClientByEmail(ctx, email)
	return err == nil && c.RepID == rep
}

// repList shows up to 15 clients as buttons.
func (b *Bot) repList(ctx context.Context, chat int64, rep, kind, q string) {
	all, _ := b.Store.Clients(ctx)
	now := b.now().Unix()
	var list []hubcore.ClientView
	q = strings.ToLower(strings.TrimSpace(q))
	for _, c := range all {
		if c.RepID != rep {
			continue
		}
		used := c.Usage.Up + c.Usage.Down
		switch kind {
		case "ct":
			if !(c.Enabled && (c.EffectiveUntil > now && c.EffectiveUntil-now <= 3*86400 || c.VolumeBytes > 0 && used*10 >= c.VolumeBytes*9)) {
				continue
			}
		case "co":
			if !c.Online {
				continue
			}
		case "cs":
			if !strings.Contains(strings.ToLower(c.Email+" "+c.Note), q) {
				continue
			}
		}
		list = append(list, c)
	}
	if kind == "co" || kind == "cs" {
		sort.Slice(list, func(i, j int) bool { return list[i].Usage.LastSeen > list[j].Usage.LastSeen })
	}
	titles := map[string]string{"cr0": "🆕 آخرین کاربرها", "ct": "⏳ نیازمند تمدید (۳ روز یا ۹۰٪ حجم)", "co": "🟢 آنلاین‌ها", "cs": "🔍 نتیجهٔ جستجو"}
	if len(list) == 0 {
		b.send(ctx, chat, titles[kind]+"\nموردی نیست.", &Keyboard{Rows: [][]Button{{repBtn("👥 کاربران", "cl"), repBtn("🏠 داشبورد", "home")}}})
		return
	}
	kb := &Keyboard{}
	for i, c := range list {
		if i == 15 {
			break
		}
		kb.Rows = append(kb.Rows, []Button{repBtn(clientLabel(c, now), "v:"+b.ref(c.Email))})
	}
	more := ""
	if len(list) > 15 {
		more = fmt.Sprintf("\n(۱۵ مورد از %d؛ برای بقیه جستجو کنید)", len(list))
	}
	kb.Rows = append(kb.Rows, []Button{repBtn("🔍 جستجو", "cs"), repBtn("🏠 داشبورد", "home")})
	b.send(ctx, chat, titles[kind]+more, kb)
}

// repClient is one client's card with its buttons.
func (b *Bot) repClient(ctx context.Context, chat int64, rep, role, email string) {
	if !b.repOwns(ctx, rep, email) {
		b.send(ctx, chat, "کاربر پیدا نشد.", repBackKB)
		return
	}
	c, err := b.Store.ClientByEmail(ctx, email)
	if err != nil {
		return
	}
	r := b.ref(email)
	kb := &Keyboard{}
	row1 := []Button{}
	if repCan(role, "renew") {
		row1 = append(row1, repBtn("🔄 تمدید", "rn:"+r))
	}
	row1 = append(row1, repBtn("🔗 لینک و QR", "lk:"+r))
	kb.Rows = append(kb.Rows, row1)
	if repCan(role, "edit") {
		t := "⏸ غیرفعال کردن"
		if !c.Enabled {
			t = "▶️ فعال کردن"
		}
		row2 := []Button{repBtn(t, "tg:"+r)}
		if repCan(role, "delete") {
			row2 = append(row2, repBtn("🗑 حذف", "dl:"+r))
		}
		kb.Rows = append(kb.Rows, row2)
	}
	kb.Rows = append(kb.Rows, []Button{repBtn("👥 کاربران", "cl"), repBtn("🏠 داشبورد", "home")})
	b.send(ctx, chat, b.clientCard(c), kb)
}

func (b *Bot) repCredit(ctx context.Context, chat int64, rep string) {
	ov, _ := b.Store.RepOverviews(ctx, rep, false)
	if len(ov) == 0 {
		b.send(ctx, chat, "گزارشی نیست.", repBackKB)
		return
	}
	o := ov[0]
	text := fmt.Sprintf("💳 <b>اعتبار و گزارش</b>\n%s\nاعتبار فعلی: <b>%s</b>\n\n<b>این ماه</b>\n🛒 فروش: %s (%d سفارش)\n↩️ برگشت: %s\n➕ شارژ: %s\n📶 مصرف کل کاربرها: %s",
		line, gb(o.VolumeCredit), gb(o.MonthSold), o.MonthSales, gb(o.MonthBack), gb(o.MonthTopup), gb(o.UsedBytes))
	b.send(ctx, chat, text, repBackKB)
}

func (b *Bot) repAdmins(ctx context.Context, chat int64, rep string) {
	list, _ := b.Store.TgRepAdmins(ctx, rep)
	kb := &Keyboard{}
	var lines []string
	for _, a := range list {
		lines = append(lines, fmt.Sprintf("• %s — %s", esc(a.Name), roleName(a.Role)))
		kb.Rows = append(kb.Rows, []Button{{Text: "🗑 حذف " + a.Name, Data: "radm:" + strconv.FormatInt(a.ChatID, 10)}})
	}
	kb.Rows = append(kb.Rows,
		[]Button{repBtn("➕ مدیر", "ai:manager"), repBtn("➕ فروش", "ai:sales"), repBtn("➕ پشتیبانی", "ai:support")},
		[]Button{repBtn("🏠 داشبورد", "home")})
	text := "⚙️ <b>ادمین‌های ربات</b>\n" + line + "\n"
	if len(lines) == 0 {
		text += "هنوز ادمینی ندارید."
	} else {
		text += strings.Join(lines, "\n")
	}
	text += "\n\nبرای افزودن، نقش را انتخاب کنید تا لینک دعوت ساخته شود:\n• مدیر: همه‌کار به‌جز مدیریت ادمین‌ها\n• فروش: ساخت، تمدید، سفارش‌ها، اعتبار\n• پشتیبانی: دیدن کاربرها و فرستادن لینک"
	b.send(ctx, chat, text, kb)
}

// repInput takes typed text the reseller UI asked for.
func (b *Bot) repInput(ctx context.Context, chat int64, rep, role, what, text string) bool {
	if what == "r:broadcast" {
		if b.Seller != rep || !repCan(role, "shop") {
			return false
		}
		b.prepareBroadcastFor(ctx, chat, text, "", "R:")
		return true
	}
	if strings.HasPrefix(what, "r:joinadd:"+rep+"|") {
		return b.Seller == rep && repCan(role, "shop") && b.joinInput(ctx, chat, what, text)
	}
	// A trial prompt belongs to this reseller only.
	if strings.HasPrefix(what, "r:trialgb:"+rep+"|") || strings.HasPrefix(what, "r:trialh:"+rep+"|") {
		return repCan(role, "shop") && b.trialInput(ctx, chat, what, text)
	}
	switch what {
	case "r:name":
		d := b.draftOf(chat)
		if d == nil {
			return false
		}
		name := strings.TrimSpace(text)
		if !repNameRE.MatchString(name) {
			b.setAwait(chat, "r:name")
			b.send(ctx, chat, "نام فقط حروف انگلیسی، عدد و . _ - باشد (مثل ali-30gb). دوباره بفرستید یا «نام خودکار» را بزنید:",
				&Keyboard{Rows: [][]Button{{repBtn("🎲 نام خودکار", "rnd")}, {repBtn("« انصراف", "home")}}})
			return true
		}
		if _, err := b.Store.ClientByEmail(ctx, name); err == nil {
			b.setAwait(chat, "r:name")
			b.send(ctx, chat, "این نام قبلاً استفاده شده؛ نام دیگری بفرستید:", &Keyboard{Rows: [][]Button{{repBtn("🎲 نام خودکار", "rnd")}}})
			return true
		}
		d.name = name
		b.repConfirm(ctx, chat, rep)
		return true
	case "r:search":
		b.repList(ctx, chat, rep, "cs", text)
		return true
	}
	return false
}

func randomName() string {
	const a = "abcdefghjkmnpqrstuvwxyz23456789"
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	out := []byte("u-")
	for _, x := range buf {
		out = append(out, a[int(x)%len(a)])
	}
	return string(out)
}

func fmtGB(g float64) string { return strconv.FormatFloat(g, 'f', -1, 64) }

// ---------- bottom keyboard: reseller mode and customer mode ----------

var repRows = [][]string{
	{"➕ ساخت کاربر", "👥 کاربران"},
	{"🛒 سفارش‌ها", "🧾 محصولات"},
	{"💳 اعتبار", "🏠 داشبورد"},
	{"🛍 حالت مشتری"},
}

func (b *Bot) repModeKey(chat int64) string { return fmt.Sprintf("tg_repmode:%s:%d", b.Seller, chat) }

// repMode reports whether a reseller chat is in reseller mode (the default).
func (b *Bot) repMode(ctx context.Context, chat int64) bool {
	return b.Store.Setting(ctx, b.repModeKey(chat)) != "customer"
}

func (b *Bot) setRepMode(ctx context.Context, chat int64, rep bool) {
	v := "rep"
	rows := repRows
	text := "🧑‍💼 حالت نماینده — دکمه‌های مدیریت پایین صفحه‌اند."
	if !rep {
		v = "customer"
		rows = append(append([][]string{}, replyRows...), []string{"🧑‍💼 حالت نماینده"})
		text = "🛍 حالت مشتری — برای برگشت «حالت نماینده» را بزنید."
	}
	_ = b.Store.SetSetting(ctx, b.repModeKey(chat), v)
	if kb, ok := b.API.(interface {
		SendReplyKeyboard(ctx context.Context, chat int64, text string, rows [][]string) error
	}); ok {
		_ = kb.SendReplyKeyboard(ctx, chat, text, rows)
	}
}

// repKey runs a bottom-keyboard button of the reseller keyboard.
func (b *Bot) repKey(ctx context.Context, chat int64, rep, role, act string) {
	b.takeAwait(chat)
	switch act {
	case "rnew":
		b.repCallback(ctx, chat, "R:new")
	case "rclients":
		b.repCallback(ctx, chat, "R:cl")
	case "rorders":
		b.repCallback(ctx, chat, "R:ord")
	case "rcredit":
		b.repCallback(ctx, chat, "R:cr")
	case "rshop":
		if !repCan(role, "shop") {
			b.send(ctx, chat, "مدیریت محصولات با نقش شما مجاز نیست.", repBackKB)
			return
		}
		b.shopHome(ctx, chat)
	case "rcustomer":
		b.setRepMode(ctx, chat, false)
		b.menu(ctx, chat)
	case "repmode":
		b.setRepMode(ctx, chat, true)
		b.repHome(ctx, chat, rep, role)
	default:
		b.repHome(ctx, chat, rep, role)
	}
}

// ensureRepKeyboard puts the reseller keyboard on once (it replaces the
// customer keyboard and the old panel's).
func (b *Bot) ensureRepKeyboard(ctx context.Context, chat int64) {
	key := fmt.Sprintf("tg_kbrep2:%s:%d", b.Seller, chat)
	if b.Store.Setting(ctx, key) == "1" {
		return
	}
	b.setRepMode(ctx, chat, true)
	_ = b.Store.SetSetting(ctx, key, "1")
}

// repBroadcast runs a reseller's broadcast in their own bot: text or photo,
// a choice of audience among that bot's users, a preview, then sending.
func (b *Bot) repBroadcast(ctx context.Context, chat int64, act, arg string) {
	b.mu.Lock()
	d := b.draft[chat]
	b.mu.Unlock()
	switch act {
	case "bc":
		b.setAwait(chat, "r:broadcast")
		b.send(ctx, chat, "📣 متن پیام همگانی را بفرستید (یا یک عکس با کپشن).\nبرای دکمهٔ لینک، آخرین خط را این‌طور بنویسید:\n<code>[متن دکمه](https://example.com)</code>\n\nپیام برای کسانی می‌رود که همین ربات را استارت کرده‌اند.", repBackKB)
	case "bcaud":
		if d != nil {
			b.askAudience(ctx, chat)
		}
	case "bca":
		if d == nil || d.cb != "R:" {
			b.send(ctx, chat, "پیش‌نویسی نیست.", repBackKB)
			return
		}
		if _, ok := audienceName[arg]; !ok || arg == "reps" {
			return
		}
		d.audience = arg
		d.chats = b.audience(ctx, arg)
		_ = b.sendDraft(ctx, chat, d)
		b.send(ctx, chat, fmt.Sprintf("👆 پیش‌نمایش. برای «%s» یعنی %d نفر ارسال شود؟", audienceName[arg], len(d.chats)), &Keyboard{Rows: [][]Button{
			{{Text: "✅ ارسال", Data: "R:bcok"}, {Text: "تغییر مخاطب", Data: "R:bcaud"}},
			{{Text: "✖️ لغو", Data: "R:bcno"}}}})
	case "bcok":
		b.mu.Lock()
		delete(b.draft, chat)
		b.bcStop = false
		b.mu.Unlock()
		if d == nil || d.cb != "R:" || d.audience == "" {
			b.send(ctx, chat, "پیش‌نویسی نیست.", repBackKB)
			return
		}
		if len(d.chats) == 0 {
			b.send(ctx, chat, "کسی در این گروه نیست.", repBackKB)
			return
		}
		go b.broadcast(context.WithoutCancel(ctx), chat, d)
		b.send(ctx, chat, "📣 ارسال شروع شد… گزارش پس از پایان می‌آید.", &Keyboard{Rows: [][]Button{{{Text: "⏹ توقف ارسال", Data: "R:bcstop"}}}})
	case "bcstop":
		b.mu.Lock()
		b.bcStop = true
		b.mu.Unlock()
		b.send(ctx, chat, "⏹ ادامهٔ ارسال متوقف می‌شود.", repBackKB)
	case "bcno":
		b.mu.Lock()
		delete(b.draft, chat)
		b.mu.Unlock()
		b.send(ctx, chat, "لغو شد.", repBackKB)
	}
}
