package tgbot

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Selling reseller panels and panel top-ups from the owner's main bot.
// Buttons start with "P:"; typed answers are awaited under "P:…".
//
//	customer:  P:buy → P:pk:<pkg> → username → card + receipt photo
//	reseller:  P:buy → P:pk:<pkg> → card + receipt photo
//	owner:     P:ok:<id> / P:no:<id> on the receipt, P:cfg… for the settings

var panelNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,23}$`)

// repSaleOn reports whether this bot sells panels (only the main bot does).
func (b *Bot) repSaleOn(ctx context.Context) bool {
	return b.Seller == "" && b.Store.RepSale(ctx).Enabled
}

// repSaleBuyer is the reseller this chat buys a top-up for ("" = a customer
// buying a new panel). Bot admins of a reseller and sub-sellers buy nothing.
func (b *Bot) repSaleBuyer(ctx context.Context, chat int64) (rep string, ok bool) {
	rep = b.Store.TgRep(ctx, chat)
	if rep == "" {
		if r, _ := b.Store.TgRepRole(ctx, chat); r != "" {
			return "", false
		}
		return "", true
	}
	return rep, !b.Store.IsSubSeller(ctx, rep)
}

// repSaleButton is the menu button for this chat, if the sale is on.
func (b *Bot) repSaleButton(ctx context.Context, chat int64) (Button, bool) {
	if !b.repSaleOn(ctx) {
		return Button{}, false
	}
	rep, ok := b.repSaleBuyer(ctx, chat)
	if !ok {
		return Button{}, false
	}
	if rep != "" {
		return cbBtn("💳 شارژ پنل", "P:buy"), true
	}
	return cbBtn("🤝 پنل نمایندگی", "P:buy"), true
}

func (b *Bot) repSaleCallback(ctx context.Context, q *CallbackQuery) bool {
	if !strings.HasPrefix(q.Data, "P:") {
		return false
	}
	chat := q.From.ID
	parts := strings.Split(strings.TrimPrefix(q.Data, "P:"), ":")
	arg := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	switch parts[0] {
	case "cfg", "ok", "no":
		if !b.Owners[chat] || b.Seller != "" {
			return true
		}
	}
	switch parts[0] {
	case "buy":
		b.takeAwait(chat)
		b.repSaleList(ctx, chat)
	case "pk":
		b.repSalePick(ctx, chat, userName(&q.From), arg(1))
	case "ok", "no":
		id, _ := strconv.ParseInt(arg(1), 10, 64)
		b.repSaleDecide(ctx, chat, id, parts[0] == "ok")
	case "cfg":
		b.takeAwait(chat)
		b.repSaleConfig(ctx, chat, parts[1:])
	}
	return true
}

func userName(u *User) string {
	if u == nil {
		return ""
	}
	return strings.TrimSpace(u.FirstName)
}

func pkgLine(p hubcore.RepPackage) string {
	return fmt.Sprintf("%s — %s تومان", gbWord(p.GB), faMoney(p.Price))
}

func gbWord(n int64) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%s ترابایت", faInt(n/1024))
	}
	return fmt.Sprintf("%s گیگ", faInt(n))
}

func (b *Bot) repSaleList(ctx context.Context, chat int64) {
	if !b.repSaleOn(ctx) {
		b.send(ctx, chat, "فروش پنل نمایندگی فعلاً فعال نیست.", homeKB)
		return
	}
	rep, ok := b.repSaleBuyer(ctx, chat)
	if !ok {
		b.send(ctx, chat, "اعتبار شما را نمایندهٔ بالادستتان شارژ می‌کند.", homeKB)
		return
	}
	var text string
	if rep != "" {
		text = fmt.Sprintf("💳 <b>شارژ پنل نمایندگی</b>\n%s\nپنل: <code>%s</code>\nاعتبار فعلی: <b>%s</b>\n\nیک بسته را انتخاب کنید:", line, esc(rep), gb(b.Store.RepCredit(ctx, rep)))
	} else {
		text = "🤝 <b>خرید پنل نمایندگی</b>\n" + line + "\nبا پنل نمایندگی برای مشتری‌های خودتان سرویس می‌سازید و تمدید می‌کنید؛ هر سرویس از اعتبار حجمی پنل شما کم می‌شود.\n\nیک بسته را انتخاب کنید:"
	}
	kb := &Keyboard{}
	for _, p := range b.Store.RepSale(ctx).Packages {
		kb.Rows = append(kb.Rows, row(cbBtn(pkgLine(p), "P:pk:"+p.ID)))
	}
	kb.Rows = append(kb.Rows, row(cbBtn("« منوی اصلی", "home")))
	b.send(ctx, chat, text, kb)
}

func (b *Bot) repSalePick(ctx context.Context, chat int64, name, pkg string) {
	if !b.repSaleOn(ctx) {
		b.send(ctx, chat, "فروش پنل نمایندگی فعلاً فعال نیست.", homeKB)
		return
	}
	rep, ok := b.repSaleBuyer(ctx, chat)
	if !ok {
		return
	}
	if rep == "" {
		b.setAwait(chat, "P:name:"+pkg)
		b.send(ctx, chat, "✍️ یک <b>نام کاربری</b> برای پنل خود بفرستید.\nحروف کوچک انگلیسی و عدد، ۳ تا ۲۴ حرف (مثلاً <code>ali-shop</code>).", &Keyboard{Rows: [][]Button{row(cbBtn("« بازگشت", "P:buy"))}})
		return
	}
	b.repSaleOpen(ctx, chat, name, rep, "", pkg)
}

// repSaleOpen creates the order and shows the card to pay to.
func (b *Bot) repSaleOpen(ctx context.Context, chat int64, buyer, rep, name, pkg string) {
	o, err := b.Store.CreateRepOrder(ctx, chat, buyer, rep, name, pkg)
	if err != nil {
		msg := "ثبت سفارش ممکن نشد. دوباره تلاش کنید."
		if strings.Contains(err.Error(), "username is taken") {
			b.setAwait(chat, "P:name:"+pkg)
			msg = "این نام کاربری گرفته شده یا معتبر نیست. نام دیگری بفرستید:"
		} else if strings.Contains(err.Error(), "already has a panel") {
			msg = "این حساب تلگرام از قبل پنل دارد؛ از «شارژ پنل» استفاده کنید."
		}
		b.send(ctx, chat, msg, &Keyboard{Rows: [][]Button{row(cbBtn("« بازگشت", "P:buy"))}})
		return
	}
	c := b.Store.Card(ctx)
	var sb strings.Builder
	title := "🤝 <b>خرید پنل نمایندگی"
	if o.Kind == "topup" {
		title = "💳 <b>شارژ پنل نمایندگی"
	}
	fmt.Fprintf(&sb, "%s #%s</b>\n%s\nپنل: <code>%s</code>\nبسته: <b>%s</b>\n💳 مبلغ: <b>%s تومان</b>\n\n", title, faInt(o.ID), line, esc(o.RepID), gbWord(o.GB), faMoney(o.Price))
	fmt.Fprintf(&sb, "مبلغ را به این کارت واریز کنید (روی شماره بزنید تا کپی شود):\n<code>%s</code>\n", groupCard(c.Number))
	if c.Holder != "" || c.Bank != "" {
		fmt.Fprintf(&sb, "%s\n", esc(strings.TrimSpace(c.Holder+" "+c.Bank)))
	}
	if o.Kind == "new" {
		kind := "فقط فروش حجمی"
		if o.Terms.AllowUnlimited {
			kind = "فروش حجمی و نامحدود"
		}
		fmt.Fprintf(&sb, "\nنوع پنل: %s\n", kind)
	}
	sb.WriteString("\n📸 سپس <b>عکس رسید</b> را همین‌جا بفرستید. بعد از تأیید، پنل شما آماده می‌شود.")
	b.setAwait(chat, "P:receipt:"+strconv.FormatInt(o.ID, 10))
	b.send(ctx, chat, sb.String(), &Keyboard{Rows: [][]Button{row(cbBtn("« منوی اصلی", "home"))}})
}

// repSaleInput takes a typed answer awaited under "P:…". It reports whether
// it used the text.
func (b *Bot) repSaleInput(ctx context.Context, m *Message, what, text string) bool {
	chat := m.Chat.ID
	rest := strings.TrimPrefix(what, "P:")
	switch {
	case strings.HasPrefix(rest, "name:"):
		pkg := strings.TrimPrefix(rest, "name:")
		name := strings.ToLower(strings.TrimSpace(text))
		if !panelNameRE.MatchString(name) {
			b.setAwait(chat, what)
			b.send(ctx, chat, "نام کاربری باید ۳ تا ۲۴ حرف از حروف کوچک انگلیسی، عدد، خط تیره یا نقطه باشد. دوباره بفرستید:", &Keyboard{Rows: [][]Button{row(cbBtn("« بازگشت", "P:buy"))}})
			return true
		}
		b.repSaleOpen(ctx, chat, userName(m.From), "", name, pkg)
		return true
	case strings.HasPrefix(rest, "receipt:"):
		b.setAwait(chat, what)
		b.send(ctx, chat, "📸 لطفاً <b>عکس رسید</b> را بفرستید (نه متن).", nil)
		return true
	case rest == "pkg":
		if !b.Owners[chat] {
			return false
		}
		b.repSaleAddPackage(ctx, chat, text)
		return true
	case strings.HasPrefix(rest, "pv:"):
		if !b.Owners[chat] {
			return false
		}
		f := strings.SplitN(strings.TrimPrefix(rest, "pv:"), ":", 2)
		if len(f) != 2 {
			return false
		}
		b.repSaleSetValue(ctx, chat, what, f[0], f[1], text)
		return true
	}
	return false
}

// repSaleReceipt takes the receipt photo of a panel order.
func (b *Bot) repSaleReceipt(ctx context.Context, chat int64, name, fileID, await string) bool {
	if b.Seller != "" {
		return false
	}
	var id int64
	if v, ok := strings.CutPrefix(await, "P:receipt:"); ok {
		id, _ = strconv.ParseInt(v, 10, 64)
	} else if await != "" {
		return false // another flow is waiting for its own photo
	} else if id, _ = b.Store.AwaitingRepReceipt(ctx, chat); id == 0 {
		return false
	}
	o, err := b.Store.AttachRepReceipt(ctx, id, chat, fileID)
	if err != nil {
		return false
	}
	b.takeAwait(chat)
	if !b.receiptOnce("p:" + strconv.FormatInt(o.ID, 10)) {
		return true
	}
	b.send(ctx, chat, fmt.Sprintf("✅ رسید سفارش <b>#%s</b> دریافت شد.\nبعد از بررسی، نتیجه همین‌جا برایتان می‌آید.", faInt(o.ID)), homeKB)
	kind := "🤝 <b>خرید پنل نمایندگی"
	if o.Kind == "topup" {
		kind = "💳 <b>شارژ پنل نمایندگی"
	}
	caption := fmt.Sprintf("%s #%s</b>\n👤 %s (<code>%d</code>)\nپنل: <code>%s</code>\nبسته: <b>%s</b>\n💳 مبلغ: <b>%s تومان</b>\n\nمبلغ را با حساب خود تطبیق دهید، بعد تأیید یا رد کنید.",
		kind, faInt(o.ID), esc(name), chat, esc(o.RepID), gbWord(o.GB), faMoney(o.Price))
	sid := strconv.FormatInt(o.ID, 10)
	b.notifyPhoto(ctx, "reps", fileID, caption, &Keyboard{Rows: [][]Button{row(cbBtn("✅ تأیید و تحویل", "P:ok:"+sid), cbBtn("❌ رد", "P:no:"+sid))}})
	return true
}

func (b *Bot) repSaleDecide(ctx context.Context, chat, id int64, approve bool) {
	if !approve {
		o, err := b.Store.RejectRepOrder(ctx, "telegram", id)
		if err != nil {
			b.send(ctx, chat, "این سفارش قبلاً بررسی شده است.", nil)
			return
		}
		b.send(ctx, chat, fmt.Sprintf("❌ سفارش #%s رد شد.", faInt(o.ID)), nil)
		b.send(ctx, o.Chat, fmt.Sprintf("❌ رسید سفارش <b>#%s</b> تأیید نشد. اگر اشتباهی شده، با پشتیبانی در تماس باشید.", faInt(o.ID)), homeKB)
		return
	}
	res, err := b.Store.ApproveRepOrder(ctx, "telegram", id)
	if err != nil {
		msg := "تحویل نشد: " + esc(err.Error())
		if strings.Contains(err.Error(), "already decided") {
			msg = "این سفارش قبلاً بررسی شده است."
		} else if strings.Contains(err.Error(), "is taken now") {
			msg = "نام کاربری این سفارش در این فاصله گرفته شده است. سفارش را رد کنید تا خریدار دوباره با نام دیگری ثبت کند."
		}
		b.send(ctx, chat, msg, nil)
		return
	}
	o := res.Order
	if o.Kind == "topup" {
		b.send(ctx, chat, fmt.Sprintf("✅ سفارش #%s تحویل شد: %s به پنل <code>%s</code> اضافه شد.", faInt(o.ID), gbWord(o.GB), esc(o.RepID)), nil)
		b.send(ctx, o.Chat, fmt.Sprintf("✅ <b>پنل شما شارژ شد</b>\n%s\n%s اضافه شد.\nاعتبار فعلی: <b>%s</b>", line, gbWord(o.GB), gb(b.Store.RepCredit(ctx, o.RepID))), homeKB)
		return
	}
	b.send(ctx, chat, fmt.Sprintf("✅ سفارش #%s تحویل شد: پنل <code>%s</code> با %s اعتبار ساخته شد.", faInt(o.ID), esc(o.RepID), gbWord(o.GB)), nil)
	b.send(ctx, o.Chat, fmt.Sprintf("🎉 <b>پنل نمایندگی شما آماده است</b>\n%s\n🌐 آدرس: %s\n👤 نام کاربری: <code>%s</code>\n🔑 رمز موقت: <code>%s</code>\n💳 اعتبار: <b>%s</b>\n\nبعد از اولین ورود رمز را عوض کنید. همین ربات هم برای شما به حالت نمایندگی رفته است: /start را بزنید تا داشبورد، ساخت کاربر و تمدید را ببینید.",
		line, esc(strings.TrimRight(b.SubBase, "/")), esc(o.RepID), esc(res.Password), gbWord(o.GB)), nil)
}

// ---- the owner's settings ----

// pkgTerms words what a panel bought with the package may do.
func pkgTerms(p hubcore.RepPackage) string {
	var l []string
	if p.AllowUnlimited {
		kind := "فروش حجمی و نامحدود"
		if p.Units > 0 {
			kind += fmt.Sprintf(" (%s واحد نامحدود)", faInt(p.Units))
		}
		l = append(l, kind)
	} else {
		l = append(l, "فقط فروش حجمی")
	}
	if len(p.Inbounds) > 0 {
		l = append(l, "اینباندها: "+esc(strings.Join(p.Inbounds, "، ")))
	} else {
		l = append(l, "همهٔ اینباندها")
	}
	capOf := func(n int64, unit string) string {
		if n == 0 {
			return "بدون سقف"
		}
		return faInt(n) + unit
	}
	l = append(l, "حداکثر کاربر: "+capOf(p.MaxClients, ""), "حداکثر مدت هر کاربر: "+capOf(p.MaxDays, " روز"), "حداکثر حجم هر کاربر: "+capOf(p.MaxClientGB, " گیگ"))
	yes := func(v bool) string {
		if v {
			return "بله"
		}
		return "خیر"
	}
	l = append(l, "حذف کاربر: "+yes(p.CanDelete), "برگشت حجم مصرف‌نشده هنگام حذف: "+yes(p.RefundUnused))
	return "• " + strings.Join(l, "\n• ")
}

// repSaleConfig handles "P:cfg[:act[:id[:arg]]]".
func (b *Bot) repSaleConfig(ctx context.Context, chat int64, args []string) {
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	act, id := arg(0), arg(1)
	sale := b.Store.RepSale(ctx)
	var pk *hubcore.RepPackage
	for i := range sale.Packages {
		if sale.Packages[i].ID == id {
			pk = &sale.Packages[i]
		}
	}
	save := func(backTo string) bool {
		if err := b.Store.PutRepSale(ctx, sale); err != nil {
			msg := esc(err.Error())
			switch {
			case strings.Contains(msg, "define a package first"):
				msg = "اول یک بسته تعریف کنید."
			case strings.Contains(msg, "payment card"):
				msg = "اول شماره کارت پرداخت را در پنل (فروشگاه تلگرام) ثبت کنید."
			}
			b.send(ctx, chat, "ذخیره نشد: "+msg, &Keyboard{Rows: [][]Button{back(backTo)}})
			return false
		}
		return true
	}
	ask := func(field, prompt string) {
		b.setAwait(chat, "P:pv:"+field+":"+id)
		b.send(ctx, chat, prompt, &Keyboard{Rows: [][]Button{back("P:cfg:e:" + id)}})
	}
	switch act {
	case "on", "off":
		sale.Enabled = act == "on"
		if !save("P:cfg") {
			return
		}
	case "add":
		b.setAwait(chat, "P:pkg")
		b.send(ctx, chat, "بستهٔ جدید را در یک خط بفرستید: <b>حجم به گیگ</b> و <b>قیمت به تومان</b>.\nمثال: <code>500 1200000</code>\n\nبعد از آن تنظیمات پنل همین بسته را می‌بینید.", &Keyboard{Rows: [][]Button{back("P:cfg")}})
		return
	case "del":
		kept := sale.Packages[:0]
		for _, p := range sale.Packages {
			if p.ID != id {
				kept = append(kept, p)
			}
		}
		sale.Packages = kept
		if len(sale.Packages) == 0 {
			sale.Enabled = false
		}
		if !save("P:cfg") {
			return
		}
	case "e", "tu", "td", "tr", "ib", "it", "ia", "gp", "un", "mc", "md", "mg":
		if pk == nil {
			b.send(ctx, chat, "این بسته دیگر وجود ندارد.", &Keyboard{Rows: [][]Button{back("P:cfg")}})
			return
		}
		switch act {
		case "tu":
			pk.AllowUnlimited = !pk.AllowUnlimited
		case "td":
			pk.CanDelete = !pk.CanDelete
		case "tr":
			pk.RefundUnused = !pk.RefundUnused
		case "ia":
			pk.Inbounds = nil
		case "it":
			all := b.Store.SellerInbounds(ctx, "")
			if n, err := strconv.Atoi(arg(2)); err == nil && n >= 0 && n < len(all) {
				tag, kept, had := all[n], []string{}, false
				for _, t := range pk.Inbounds {
					if t == tag {
						had = true
					} else {
						kept = append(kept, t)
					}
				}
				if !had {
					kept = append(kept, tag)
				}
				pk.Inbounds = kept
			}
		case "gp":
			ask("gp", "حجم به گیگ و قیمت به تومان را در یک خط بفرستید. مثال: <code>500 1200000</code>")
			return
		case "un":
			ask("un", "تعداد واحد نامحدودی که همراه این بسته داده می‌شود را بفرستید (۰ = هیچ):")
			return
		case "mc":
			ask("mc", "حداکثر تعداد کاربر پنل را بفرستید (۰ = بدون سقف):")
			return
		case "md":
			ask("md", "حداکثر مدت هر کاربر به روز را بفرستید (۰ = بدون سقف):")
			return
		case "mg":
			ask("mg", "حداکثر حجم هر کاربر به گیگ را بفرستید (۰ = بدون سقف):")
			return
		}
		if act != "e" && act != "ib" && !save("P:cfg:e:"+id) {
			return
		}
		if act == "ib" || act == "it" || act == "ia" {
			b.repSaleInbounds(ctx, chat, *pk)
			return
		}
		b.repSalePackage(ctx, chat, *pk)
		return
	}
	sale = b.Store.RepSale(ctx)
	state, toggle := "🔴 خاموش", cbBtn("🟢 روشن کردن", "P:cfg:on")
	if sale.Enabled {
		state, toggle = "🟢 روشن", cbBtn("🔴 خاموش کردن", "P:cfg:off")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "🤝 <b>فروش پنل نمایندگی</b>\n\nوضعیت: <b>%s</b>\n\nمشتری از منوی ربات پنل می‌خرد و نماینده‌ها پنلشان را شارژ می‌کنند؛ هر دو با رسید کارت‌به‌کارت و تأیید شما. پنل جدید با تنظیمات همان بسته ساخته می‌شود.\n\n<b>بسته‌ها</b> (برای دیدن و تغییر تنظیمات روی بسته بزنید)\n", state)
	kb := &Keyboard{Rows: [][]Button{{toggle}}}
	if len(sale.Packages) == 0 {
		sb.WriteString("هنوز بسته‌ای تعریف نشده است.\n")
	}
	for _, p := range sale.Packages {
		kind := "فقط حجمی"
		if p.AllowUnlimited {
			kind = "حجمی + نامحدود"
		}
		fmt.Fprintf(&sb, "• %s — %s\n", pkgLine(p), kind)
		kb.Rows = append(kb.Rows, row(cbBtn("⚙️ "+gbWord(p.GB), "P:cfg:e:"+p.ID)))
	}
	kb.Rows = append(kb.Rows, row(cbBtn("➕ بستهٔ جدید", "P:cfg:add")), back("a:settings"))
	b.send(ctx, chat, sb.String(), kb)
}

// repSalePackage is one package's settings: what a panel bought with it gets.
func (b *Bot) repSalePackage(ctx context.Context, chat int64, p hubcore.RepPackage) {
	on := func(v bool, yes, no string) string {
		if v {
			return yes
		}
		return no
	}
	c := "P:cfg:"
	kb := &Keyboard{Rows: [][]Button{
		row(cbBtn("📦 حجم و قیمت", c+"gp:"+p.ID)),
		row(cbBtn(on(p.AllowUnlimited, "نوع فروش: حجمی + نامحدود", "نوع فروش: فقط حجمی"), c+"tu:"+p.ID)),
	}}
	if p.AllowUnlimited {
		kb.Rows = append(kb.Rows, row(cbBtn(fmt.Sprintf("واحد نامحدود همراه بسته: %s", faInt(p.Units)), c+"un:"+p.ID)))
	}
	kb.Rows = append(kb.Rows,
		row(cbBtn("🔌 اینباندها", c+"ib:"+p.ID)),
		row(cbBtn("👥 حداکثر کاربر", c+"mc:"+p.ID), cbBtn("📅 حداکثر روز", c+"md:"+p.ID)),
		row(cbBtn("📏 حداکثر حجم هر کاربر", c+"mg:"+p.ID)),
		row(cbBtn(on(p.CanDelete, "حذف کاربر: مجاز", "حذف کاربر: غیرمجاز"), c+"td:"+p.ID)),
		row(cbBtn(on(p.RefundUnused, "برگشت حجم هنگام حذف: بله", "برگشت حجم هنگام حذف: خیر"), c+"tr:"+p.ID)),
		row(cbBtn("🗑 حذف بسته", c+"del:"+p.ID)), back("P:cfg"))
	b.send(ctx, chat, fmt.Sprintf("⚙️ <b>بستهٔ %s</b> — %s تومان\n%s\nپنلی که با این بسته خریده شود با این تنظیمات ساخته می‌شود:\n\n%s\n\nشارژ پنل‌های موجود فقط اعتبار اضافه می‌کند و تنظیمات آن پنل را عوض نمی‌کند.",
		gbWord(p.GB), faMoney(p.Price), line, pkgTerms(p)), kb)
}

func (b *Bot) repSaleInbounds(ctx context.Context, chat int64, p hubcore.RepPackage) {
	has := map[string]bool{}
	for _, t := range p.Inbounds {
		has[t] = true
	}
	kb := &Keyboard{}
	mark := "⚪️ "
	if len(p.Inbounds) == 0 {
		mark = "✅ "
	}
	kb.Rows = append(kb.Rows, row(cbBtn(mark+"همهٔ اینباندها (بعدی‌ها هم)", "P:cfg:ia:"+p.ID)))
	for i, t := range b.Store.SellerInbounds(ctx, "") {
		m := "⚪️ "
		if has[t] {
			m = "✅ "
		}
		kb.Rows = append(kb.Rows, row(cbBtn(m+t, fmt.Sprintf("P:cfg:it:%s:%d", p.ID, i))))
	}
	kb.Rows = append(kb.Rows, back("P:cfg:e:"+p.ID))
	b.send(ctx, chat, "🔌 اینباندهایی که پنل این بسته می‌تواند بفروشد را انتخاب کنید:", kb)
}

// parseGBPrice reads "500 1200000".
func parseGBPrice(text string) (gbN, price int64, ok bool) {
	f := strings.Fields(strings.NewReplacer(",", "", "٬", "", "،", " ").Replace(text))
	if len(f) != 2 {
		return 0, 0, false
	}
	gbN, a := parseNum(f[0])
	price, c := parseNum(f[1])
	return gbN, price, a && c && gbN > 0
}

func (b *Bot) repSaleAddPackage(ctx context.Context, chat int64, text string) {
	gbN, price, ok := parseGBPrice(text)
	if !ok {
		b.setAwait(chat, "P:pkg")
		b.send(ctx, chat, "دو عدد بفرستید: حجم به گیگ و قیمت به تومان. مثال: <code>500 1200000</code>", &Keyboard{Rows: [][]Button{back("P:cfg")}})
		return
	}
	sale := b.Store.RepSale(ctx)
	// A new package starts strict: volume only, every inbound, no caps.
	sale.Packages = append(sale.Packages, hubcore.RepPackage{GB: gbN, Price: price, CanDelete: true})
	if err := b.Store.PutRepSale(ctx, sale); err != nil {
		b.send(ctx, chat, "ذخیره نشد: "+esc(err.Error()), &Keyboard{Rows: [][]Button{back("P:cfg")}})
		return
	}
	sale = b.Store.RepSale(ctx)
	b.repSalePackage(ctx, chat, sale.Packages[len(sale.Packages)-1])
}

// repSaleSetValue takes a typed value for one field of a package
// (awaited as "P:pv:<field>:<package>").
func (b *Bot) repSaleSetValue(ctx context.Context, chat int64, what, field, id, text string) {
	sale := b.Store.RepSale(ctx)
	var pk *hubcore.RepPackage
	for i := range sale.Packages {
		if sale.Packages[i].ID == id {
			pk = &sale.Packages[i]
		}
	}
	if pk == nil {
		b.send(ctx, chat, "این بسته دیگر وجود ندارد.", &Keyboard{Rows: [][]Button{back("P:cfg")}})
		return
	}
	again := func(msg string) {
		b.setAwait(chat, what)
		b.send(ctx, chat, msg, &Keyboard{Rows: [][]Button{back("P:cfg:e:" + id)}})
	}
	if field == "gp" {
		gbN, price, ok := parseGBPrice(text)
		if !ok {
			again("دو عدد بفرستید: حجم به گیگ و قیمت به تومان. مثال: <code>500 1200000</code>")
			return
		}
		pk.GB, pk.Price, pk.Name = gbN, price, ""
	} else {
		n, ok := parseNum(text)
		if !ok || n < 0 {
			again("فقط یک عدد بفرستید (۰ = بدون سقف).")
			return
		}
		switch field {
		case "un":
			pk.Units = n
		case "mc":
			pk.MaxClients = n
		case "md":
			pk.MaxDays = n
		case "mg":
			pk.MaxClientGB = n
		default:
			return
		}
	}
	if err := b.Store.PutRepSale(ctx, sale); err != nil {
		again("ذخیره نشد: " + esc(err.Error()))
		return
	}
	b.repSalePackage(ctx, chat, *pk)
}
