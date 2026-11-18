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
		return cbBtn("💳 شارژ پنل نمایندگی", "P:buy"), true
	}
	return cbBtn("🤝 خرید پنل نمایندگی", "P:buy"), true
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
		b.repSaleConfig(ctx, chat, arg(1), arg(2))
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

func (b *Bot) repSaleConfig(ctx context.Context, chat int64, act, id string) {
	sale := b.Store.RepSale(ctx)
	save := func() bool {
		if err := b.Store.PutRepSale(ctx, sale); err != nil {
			msg := esc(err.Error())
			switch {
			case strings.Contains(msg, "define a package first"):
				msg = "اول یک بسته تعریف کنید."
			case strings.Contains(msg, "payment card"):
				msg = "اول شماره کارت پرداخت را در پنل (فروشگاه تلگرام) ثبت کنید."
			}
			b.send(ctx, chat, "ذخیره نشد: "+msg, &Keyboard{Rows: [][]Button{back("P:cfg")}})
			return false
		}
		return true
	}
	switch act {
	case "on", "off":
		sale.Enabled = act == "on"
		if !save() {
			return
		}
	case "add":
		b.setAwait(chat, "P:pkg")
		b.send(ctx, chat, "بستهٔ جدید را در یک خط بفرستید: <b>حجم به گیگ</b> و <b>قیمت به تومان</b>.\nمثال: <code>500 1200000</code>", &Keyboard{Rows: [][]Button{back("P:cfg")}})
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
		if !save() {
			return
		}
	}
	sale = b.Store.RepSale(ctx)
	state, toggle := "🔴 خاموش", cbBtn("🟢 روشن کردن", "P:cfg:on")
	if sale.Enabled {
		state, toggle = "🟢 روشن", cbBtn("🔴 خاموش کردن", "P:cfg:off")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "🤝 <b>فروش پنل نمایندگی</b>\n\nوضعیت: <b>%s</b>\n\nمشتری از منوی ربات پنل می‌خرد و نماینده‌ها پنلشان را شارژ می‌کنند؛ هر دو با رسید کارت‌به‌کارت و تأیید شما.\n\n<b>بسته‌ها</b>\n", state)
	kb := &Keyboard{Rows: [][]Button{{toggle}}}
	if len(sale.Packages) == 0 {
		sb.WriteString("هنوز بسته‌ای تعریف نشده است.\n")
	}
	for _, p := range sale.Packages {
		fmt.Fprintf(&sb, "• %s\n", pkgLine(p))
		kb.Rows = append(kb.Rows, row(cbBtn("🗑 حذف "+gbWord(p.GB), "P:cfg:del:"+p.ID)))
	}
	kb.Rows = append(kb.Rows, row(cbBtn("➕ بستهٔ جدید", "P:cfg:add")), back("a:settings"))
	b.send(ctx, chat, sb.String(), kb)
}

func (b *Bot) repSaleAddPackage(ctx context.Context, chat int64, text string) {
	f := strings.Fields(strings.NewReplacer(",", "", "٬", "", "،", " ").Replace(text))
	var gbN, price int64
	ok := len(f) == 2
	if ok {
		var a, c bool
		gbN, a = parseNum(f[0])
		price, c = parseNum(f[1])
		ok = a && c && gbN > 0
	}
	if !ok {
		b.setAwait(chat, "P:pkg")
		b.send(ctx, chat, "دو عدد بفرستید: حجم به گیگ و قیمت به تومان. مثال: <code>500 1200000</code>", &Keyboard{Rows: [][]Button{back("P:cfg")}})
		return
	}
	sale := b.Store.RepSale(ctx)
	sale.Packages = append(sale.Packages, hubcore.RepPackage{GB: gbN, Price: price})
	if err := b.Store.PutRepSale(ctx, sale); err != nil {
		b.send(ctx, chat, "ذخیره نشد: "+esc(err.Error()), &Keyboard{Rows: [][]Button{back("P:cfg")}})
		return
	}
	b.repSaleConfig(ctx, chat, "", "")
}
