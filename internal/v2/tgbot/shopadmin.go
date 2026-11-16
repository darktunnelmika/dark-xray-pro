package tgbot

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Shop management with buttons, for the owner (main bot) and for resellers
// (their own bot, owner/manager roles): list products by category, switch
// one on or off, change name, price, volume, days, users and category,
// reorder, delete, and make a new product in a few steps. Callbacks start
// with "S:"; typed answers wait under "S:..." keys.

// shopWho is whose shop this chat manages ("ok" false = none).
func (b *Bot) shopWho(ctx context.Context, chat int64) (p hubcore.Principal, seller, home string, ok bool) {
	if b.Owners[chat] && b.Seller == "" {
		return hubcore.Principal{User: "tg:" + strconv.FormatInt(chat, 10), Role: "owner"}, "", "a:home", true
	}
	if rep, role := b.repOf(ctx, chat); rep != "" && repCan(role, "shop") {
		return hubcore.Principal{User: "tg:" + strconv.FormatInt(chat, 10), Role: "rep", RepID: rep}, rep, "R:home", true
	}
	return hubcore.Principal{}, "", "", false
}

func shopBtn(text, data string) Button { return Button{Text: text, Data: "S:" + data} }

// shopProducts: the seller's products in shop order (removed ones last).
func (b *Bot) shopProducts(ctx context.Context, p hubcore.Principal, seller string, withArchived bool) []hubcore.Product {
	all, _ := b.Store.Products(ctx, p, false)
	out := []hubcore.Product{}
	for _, x := range all {
		if x.Seller == seller && (withArchived || x.Status != "archived") {
			out = append(out, x)
		}
	}
	return out
}

func (b *Bot) shopProduct(ctx context.Context, p hubcore.Principal, seller, id string) (hubcore.Product, bool) {
	x, err := b.Store.ProductByID(ctx, p, id)
	if err != nil || x.Seller != seller {
		return hubcore.Product{}, false
	}
	return x, true
}

func productSize(x hubcore.Product) string {
	if x.Plan == "unlimited" {
		return "نامحدود"
	}
	return fmt.Sprintf("%s گیگ", faInt(x.GB))
}

func productStatus(x hubcore.Product) string {
	switch x.Status {
	case "published":
		return "🟢"
	case "archived":
		return "🗃"
	}
	return "⚪"
}

// shopErr is a save error in plain Persian.
func shopErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "managing products is not allowed"):
		return "مدیریت محصولات برای شما فعال نیست؛ از مدیر پنل بخواهید فعالش کند."
	case strings.Contains(s, "unknown inbound"):
		return "یکی از اینباندهای این محصول دیگر وجود ندارد؛ از پنل اینباندهای محصول را درست کنید."
	case strings.Contains(s, "category names"):
		return "نام دسته تکراری یا خیلی بلند است (حداکثر ۴۰ حرف)."
	case strings.Contains(s, "invalid"):
		return "مقدار واردشده درست نیست."
	}
	return friendlyErr(err)
}

// shopHome: summary and the categories (or the products when there are none).
func (b *Bot) shopHome(ctx context.Context, chat int64) {
	p, seller, home, ok := b.shopWho(ctx, chat)
	if !ok {
		return
	}
	list := b.shopProducts(ctx, p, seller, false)
	on := 0
	for _, x := range list {
		if x.Status == "published" {
			on++
		}
	}
	text := fmt.Sprintf("🧾 <b>محصولات فروشگاه</b>\n%s\n📦 محصول: <b>%s</b> · 🟢 در فروش: <b>%s</b>\n🟢 = در ربات دیده می‌شود · ⚪ = خاموش", line, faInt(int64(len(list))), faInt(int64(on)))
	cats := b.Store.ShopCategories(ctx, seller)
	kb := &Keyboard{}
	if len(cats) > 1 || (len(cats) == 1 && cats[0].Count < len(list)) {
		var r []Button
		for i, c := range cats {
			e := c.Emoji
			if e == "" {
				e = "📂"
			}
			r = append(r, shopBtn(fmt.Sprintf("%s %s (%s)", e, c.Name, faInt(int64(c.Count))), "k:"+strconv.Itoa(i)))
			if len(r) == 2 {
				kb.Rows, r = append(kb.Rows, r), nil
			}
		}
		if len(r) > 0 {
			kb.Rows = append(kb.Rows, r)
		}
		kb.Rows = append(kb.Rows, []Button{shopBtn("📋 همه", "k:*"), shopBtn("📦 بدون دسته", "k:-")})
		text += "\n\nیک دسته را انتخاب کنید:"
	} else {
		b.shopRows(kb, list)
		if len(list) == 0 {
			text += "\n\nهنوز محصولی ندارید. با «محصول جدید» اولین بسته را بسازید."
		} else {
			text += "\n\nروی هر محصول بزنید تا ویرایشش کنید:"
		}
	}
	kb.Rows = append(kb.Rows, []Button{shopBtn("➕ محصول جدید", "n")}, []Button{{Text: "🏠 بازگشت", Data: home}})
	b.send(ctx, chat, text, kb)
}

func (b *Bot) shopRows(kb *Keyboard, list []hubcore.Product) {
	for i, x := range list {
		if i == 40 {
			break
		}
		price := "رایگان"
		if x.Price > 0 {
			price = money(x.Price)
		}
		kb.Rows = append(kb.Rows, []Button{shopBtn(fmt.Sprintf("%s %s — %s", productStatus(x), x.Name, price), "p:"+x.ID)})
	}
}

// shopCategory lists one category ("*" all, "-" none).
func (b *Bot) shopCategory(ctx context.Context, chat int64, key string) {
	p, seller, _, ok := b.shopWho(ctx, chat)
	if !ok {
		return
	}
	name, title := "", "📋 همهٔ محصولات"
	if key == "-" {
		title = "📦 بدون دسته"
	} else if key != "*" {
		cats := b.Store.ShopCategories(ctx, seller)
		i, _ := strconv.Atoi(key)
		if i < 0 || i >= len(cats) {
			b.shopHome(ctx, chat)
			return
		}
		name, title = cats[i].Name, "📂 "+cats[i].Name
	}
	var list []hubcore.Product
	for _, x := range b.shopProducts(ctx, p, seller, false) {
		if key == "*" || (key == "-" && x.Category == "") || (key != "-" && x.Category == name && name != "") {
			list = append(list, x)
		}
	}
	kb := &Keyboard{}
	b.shopRows(kb, list)
	kb.Rows = append(kb.Rows, []Button{shopBtn("➕ محصول جدید", "n"), shopBtn("« دسته‌ها", "h")})
	msg := fmt.Sprintf("<b>%s</b> (%s)\nروی هر محصول بزنید تا ویرایشش کنید:", esc(title), faInt(int64(len(list))))
	if len(list) == 0 {
		msg = fmt.Sprintf("<b>%s</b>\nمحصولی در این دسته نیست.", esc(title))
	}
	b.send(ctx, chat, msg, kb)
}

// shopCard shows one product with its edit buttons.
func (b *Bot) shopCard(ctx context.Context, chat int64, id string) {
	p, seller, _, ok := b.shopWho(ctx, chat)
	if !ok {
		return
	}
	x, ok := b.shopProduct(ctx, p, seller, id)
	if !ok {
		b.send(ctx, chat, "این محصول پیدا نشد.", &Keyboard{Rows: [][]Button{{shopBtn("« محصولات", "h")}}})
		return
	}
	st := map[string]string{"published": "🟢 در حال فروش", "draft": "⚪ خاموش (در ربات دیده نمی‌شود)", "archived": "🗃 حذف‌شده از فروشگاه"}[x.Status]
	cat := x.Category
	if cat == "" {
		cat = "بدون دسته"
	}
	price := "رایگان"
	if x.Price > 0 {
		price = money(x.Price) + " تومان"
	}
	users := "بدون محدودیت"
	if x.MaxIPs > 0 {
		users = faInt(int64(x.MaxIPs)) + " کاربره"
	}
	start := "از همان لحظهٔ خرید"
	if x.FirstUse {
		start = "از اولین اتصال"
	}
	text := fmt.Sprintf("🧾 <b>%s</b>\n%s\n📂 دسته: %s\n📦 حجم: <b>%s</b>\n⏳ مدت: <b>%s روز</b> (%s)\n👤 %s\n💰 قیمت: <b>%s</b>\n\nوضعیت: %s",
		esc(x.Name), line, esc(cat), productSize(x), faInt(x.Days), start, users, price, st)
	kb := &Keyboard{}
	if x.Status == "archived" {
		kb.Rows = append(kb.Rows, []Button{shopBtn("♻️ بازگرداندن", "t:"+x.ID)})
	} else {
		toggle := "⚪ خاموش کن"
		if x.Status != "published" {
			toggle = "🟢 روشن کن (فروش)"
		}
		kb.Rows = append(kb.Rows,
			[]Button{shopBtn(toggle, "t:"+x.ID)},
			[]Button{shopBtn("💰 قیمت", "e:"+x.ID+":price"), shopBtn("✏️ نام", "e:"+x.ID+":name")},
			[]Button{shopBtn("📦 حجم", "e:"+x.ID+":gb"), shopBtn("⏳ روز", "e:"+x.ID+":days"), shopBtn("👤 کاربر", "e:"+x.ID+":ips")},
			[]Button{shopBtn("📂 دسته", "c:"+x.ID), shopBtn("⬆️", "u:"+x.ID), shopBtn("⬇️", "w:"+x.ID)},
			[]Button{shopBtn("🗑 حذف", "x:"+x.ID)})
	}
	kb.Rows = append(kb.Rows, []Button{shopBtn("« محصولات", "h")})
	b.send(ctx, chat, text, kb)
}

// shopSave writes a product and shows its card (or the error).
func (b *Bot) shopSave(ctx context.Context, chat int64, p hubcore.Principal, x hubcore.Product) bool {
	if err := b.Store.PutProduct(ctx, p, x); err != nil {
		b.send(ctx, chat, "⚠️ ذخیره نشد: "+esc(shopErr(err)), &Keyboard{Rows: [][]Button{{shopBtn("« محصول", "p:"+x.ID)}}})
		return false
	}
	return true
}

// shopCallback handles every "S:" button.
func (b *Bot) shopCallback(ctx context.Context, chat int64, data string) bool {
	if !strings.HasPrefix(data, "S:") {
		return false
	}
	p, seller, _, ok := b.shopWho(ctx, chat)
	if !ok {
		b.send(ctx, chat, "مدیریت فروشگاه با نقش شما مجاز نیست.", nil)
		return true
	}
	parts := strings.Split(strings.TrimPrefix(data, "S:"), ":")
	arg := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	id := arg(1)
	switch parts[0] {
	case "h":
		b.takeAwait(chat)
		b.shopHome(ctx, chat)
	case "k":
		b.shopCategory(ctx, chat, id)
	case "p":
		b.takeAwait(chat)
		b.shopCard(ctx, chat, id)
	case "t":
		x, ok := b.shopProduct(ctx, p, seller, id)
		if !ok {
			return true
		}
		if x.Status == "published" {
			x.Status = "draft"
		} else if x.Status == "archived" {
			x.Status = "draft"
		} else {
			x.Status = "published"
		}
		if b.shopSave(ctx, chat, p, x) {
			b.shopCard(ctx, chat, id)
		}
	case "e":
		x, ok := b.shopProduct(ctx, p, seller, id)
		if !ok {
			return true
		}
		ask := map[string]string{
			"price": fmt.Sprintf("💰 قیمت تازهٔ «%s» را به تومان بفرستید (مثلاً 150000). ۰ یعنی رایگان.", esc(x.Name)),
			"name":  fmt.Sprintf("✏️ نام تازهٔ «%s» را بفرستید:", esc(x.Name)),
			"gb":    "📦 حجم را به گیگ بفرستید (مثلاً 30). برای نامحدود بنویسید: نامحدود",
			"days":  "⏳ مدت را به روز بفرستید (مثلاً 30):",
			"ips":   "👤 تعداد کاربر هم‌زمان را بفرستید (مثلاً 2). ۰ یعنی بدون محدودیت.",
		}[arg(2)]
		if ask == "" {
			return true
		}
		b.setAwait(chat, "S:e:"+id+":"+arg(2))
		b.send(ctx, chat, ask, &Keyboard{Rows: [][]Button{{shopBtn("« انصراف", "p:"+id)}}})
	case "c":
		if _, ok := b.shopProduct(ctx, p, seller, id); !ok {
			return true
		}
		kb := &Keyboard{}
		for i, c := range b.Store.ShopCategories(ctx, seller) {
			e := c.Emoji
			if e == "" {
				e = "📂"
			}
			kb.Rows = append(kb.Rows, []Button{shopBtn(e+" "+c.Name, "q:"+id+":"+strconv.Itoa(i))})
		}
		kb.Rows = append(kb.Rows, []Button{shopBtn("➕ دستهٔ جدید", "q:"+id+":+"), shopBtn("📦 بدون دسته", "q:"+id+":-")}, []Button{shopBtn("« انصراف", "p:"+id)})
		b.send(ctx, chat, "📂 دستهٔ این محصول را انتخاب کنید:", kb)
	case "q":
		x, ok := b.shopProduct(ctx, p, seller, id)
		if !ok {
			return true
		}
		switch arg(2) {
		case "+":
			b.setAwait(chat, "S:e:"+id+":cat")
			b.send(ctx, chat, "📂 نام دستهٔ جدید را بفرستید (می‌توانید اول یک ایموجی بگذارید):", &Keyboard{Rows: [][]Button{{shopBtn("« انصراف", "p:"+id)}}})
			return true
		case "-":
			x.Category = ""
		default:
			cats := b.Store.ShopCategories(ctx, seller)
			i, _ := strconv.Atoi(arg(2))
			if i < 0 || i >= len(cats) {
				return true
			}
			x.Category = cats[i].Name
		}
		if b.shopSave(ctx, chat, p, x) {
			b.shopCard(ctx, chat, id)
		}
	case "u", "w":
		list := b.shopProducts(ctx, p, seller, false)
		for i, x := range list {
			if x.ID != id {
				continue
			}
			j := i - 1
			if parts[0] == "w" {
				j = i + 1
			}
			if j >= 0 && j < len(list) {
				list[i], list[j] = list[j], list[i]
				ids := make([]string, len(list))
				for k, y := range list {
					ids[k] = y.ID
				}
				if err := b.Store.ReorderProducts(ctx, p, seller, ids); err != nil {
					b.send(ctx, chat, "⚠️ "+esc(shopErr(err)), nil)
					return true
				}
			}
			break
		}
		b.shopCard(ctx, chat, id)
	case "x":
		x, ok := b.shopProduct(ctx, p, seller, id)
		if !ok {
			return true
		}
		kb := &Keyboard{Rows: [][]Button{{shopBtn("🗑 بله، حذف شود", "y:"+id), shopBtn("« نه", "p:"+id)}}}
		b.send(ctx, chat, fmt.Sprintf("⚠️ محصول «%s» حذف شود؟\nاگر قبلاً فروخته شده باشد، برای حفظ تاریخچهٔ سفارش‌ها فقط از فروشگاه برداشته می‌شود و در پنل قابل بازگرداندن است.", esc(x.Name)), kb)
	case "y":
		gone, err := b.Store.DeleteProduct(ctx, p, id)
		if err != nil {
			b.send(ctx, chat, "⚠️ حذف نشد: "+esc(shopErr(err)), nil)
			return true
		}
		msg := "🗑 محصول حذف شد."
		if !gone {
			msg = "🗑 محصول از فروشگاه برداشته شد (سفارش‌های قبلی آن حفظ شد)."
		}
		b.send(ctx, chat, msg, &Keyboard{Rows: [][]Button{{shopBtn("« محصولات", "h")}}})
	case "n": // new product: name first
		b.setPDraft(chat, &hubcore.Product{Plan: "volume", MaxIPs: 2, FirstUse: true, Seller: seller})
		b.setAwait(chat, "S:n:name")
		b.send(ctx, chat, "➕ <b>محصول جدید</b>\nنام محصول را بفرستید (همان چیزی که مشتری در ربات می‌بیند، مثلاً «۳۰ گیگ یک‌ماهه»):", &Keyboard{Rows: [][]Button{{shopBtn("« انصراف", "h")}}})
	case "ng":
		d := b.pdraftOf(chat)
		if d == nil {
			return true
		}
		g, _ := strconv.ParseInt(id, 10, 64)
		if g == 0 {
			d.Plan, d.GB = "unlimited", 0
		} else {
			d.Plan, d.GB = "volume", g
		}
		kb := &Keyboard{Rows: [][]Button{
			{shopBtn("۱ ماه", "nd:30"), shopBtn("۲ ماه", "nd:60"), shopBtn("۳ ماه", "nd:90")},
			{shopBtn("۶ ماه", "nd:180"), shopBtn("۱ سال", "nd:365")},
			{shopBtn("✍️ عدد دلخواه", "nt:days"), shopBtn("« انصراف", "h")}}}
		b.send(ctx, chat, "⏳ مدت:", kb)
	case "nd":
		d := b.pdraftOf(chat)
		if d == nil {
			return true
		}
		d.Days, _ = strconv.ParseInt(id, 10, 64)
		b.shopAskUsers(ctx, chat)
	case "ni":
		d := b.pdraftOf(chat)
		if d == nil {
			return true
		}
		d.MaxIPs, _ = strconv.Atoi(id)
		b.setAwait(chat, "S:n:price")
		b.send(ctx, chat, "💰 قیمت را به تومان بفرستید (مثلاً 150000):", &Keyboard{Rows: [][]Button{{shopBtn("« انصراف", "h")}}})
	case "nt": // typed volume or days
		if b.pdraftOf(chat) == nil {
			return true
		}
		b.setAwait(chat, "S:n:"+id)
		ask := "📦 حجم را به گیگ بفرستید (مثلاً 25):"
		if id == "days" {
			ask = "⏳ مدت را به روز بفرستید (مثلاً 45):"
		}
		b.send(ctx, chat, ask, &Keyboard{Rows: [][]Button{{shopBtn("« انصراف", "h")}}})
	case "nk":
		d := b.pdraftOf(chat)
		if d == nil {
			return true
		}
		if id == "+" {
			b.setAwait(chat, "S:n:cat")
			b.send(ctx, chat, "📂 نام دستهٔ جدید را بفرستید:", &Keyboard{Rows: [][]Button{{shopBtn("« انصراف", "h")}}})
			return true
		}
		d.Category = ""
		if id != "-" {
			cats := b.Store.ShopCategories(ctx, seller)
			if i, err := strconv.Atoi(id); err == nil && i >= 0 && i < len(cats) {
				d.Category = cats[i].Name
			}
		}
		b.shopConfirm(ctx, chat)
	case "nm":
		d := b.pdraftOf(chat)
		if d == nil {
			return true
		}
		x := *d
		x.ID = newProductID()
		x.Status = "draft"
		if id == "pub" {
			x.Status = "published"
		}
		if b.shopSave(ctx, chat, p, x) {
			b.setPDraft(chat, nil)
			b.send(ctx, chat, "✅ محصول ساخته شد.", nil)
			b.shopCard(ctx, chat, x.ID)
		}
	default:
		b.shopHome(ctx, chat)
	}
	return true
}

func (b *Bot) shopAskVolume(ctx context.Context, chat int64) {
	_, seller, _, _ := b.shopWho(ctx, chat)
	kb := &Keyboard{}
	var r []Button
	for _, g := range []int{10, 20, 30, 50, 100, 200} {
		r = append(r, shopBtn(fmt.Sprintf("%s گیگ", faInt(int64(g))), "ng:"+strconv.Itoa(g)))
		if len(r) == 3 {
			kb.Rows, r = append(kb.Rows, r), nil
		}
	}
	last := []Button{shopBtn("✍️ عدد دلخواه", "nt:gb")}
	if seller == "" {
		last = append(last, shopBtn("♾ نامحدود", "ng:0"))
	} else if pol, err := b.Store.RepPolicyOf(ctx, seller); err == nil && pol.AllowUnlimited {
		last = append(last, shopBtn("♾ نامحدود", "ng:0"))
	}
	kb.Rows = append(kb.Rows, last, []Button{shopBtn("« انصراف", "h")})
	b.send(ctx, chat, "📦 حجم:", kb)
}

func (b *Bot) shopAskUsers(ctx context.Context, chat int64) {
	kb := &Keyboard{Rows: [][]Button{
		{shopBtn("۱ کاربره", "ni:1"), shopBtn("۲ کاربره", "ni:2"), shopBtn("۳ کاربره", "ni:3")},
		{shopBtn("۵ کاربره", "ni:5"), shopBtn("بدون محدودیت", "ni:0")},
		{shopBtn("« انصراف", "h")}}}
	b.send(ctx, chat, "👤 چند کاربره (دستگاه هم‌زمان)؟", kb)
}

func (b *Bot) shopAskCategory(ctx context.Context, chat int64) {
	_, seller, _, _ := b.shopWho(ctx, chat)
	cats := b.Store.ShopCategories(ctx, seller)
	if len(cats) == 0 {
		b.shopConfirm(ctx, chat)
		return
	}
	kb := &Keyboard{}
	var r []Button
	for i, c := range cats {
		e := c.Emoji
		if e == "" {
			e = "📂"
		}
		r = append(r, shopBtn(e+" "+c.Name, "nk:"+strconv.Itoa(i)))
		if len(r) == 2 {
			kb.Rows, r = append(kb.Rows, r), nil
		}
	}
	if len(r) > 0 {
		kb.Rows = append(kb.Rows, r)
	}
	kb.Rows = append(kb.Rows, []Button{shopBtn("➕ دستهٔ جدید", "nk:+"), shopBtn("📦 بدون دسته", "nk:-")})
	b.send(ctx, chat, "📂 در کدام دسته باشد؟", kb)
}

func (b *Bot) shopConfirm(ctx context.Context, chat int64) {
	d := b.pdraftOf(chat)
	if d == nil {
		return
	}
	cat := d.Category
	if cat == "" {
		cat = "بدون دسته"
	}
	users := "بدون محدودیت"
	if d.MaxIPs > 0 {
		users = faInt(int64(d.MaxIPs)) + " کاربره"
	}
	price := "رایگان"
	if d.Price > 0 {
		price = money(d.Price) + " تومان"
	}
	text := fmt.Sprintf("🧾 <b>%s</b>\n%s\n📂 %s\n📦 %s · ⏳ %s روز · 👤 %s\n💰 %s\n⏱ زمان از اولین اتصال مشتری شروع می‌شود.\n\nدر ربات این‌طور دیده می‌شود:\n<code>📦 %s — %s</code>",
		esc(d.Name), line, esc(cat), productSize(*d), faInt(d.Days), users, price, esc(d.Name), price)
	kb := &Keyboard{Rows: [][]Button{
		{shopBtn("✅ ساخت و شروع فروش", "nm:pub")},
		{shopBtn("💾 فقط ذخیره (خاموش)", "nm:draft"), shopBtn("« انصراف", "h")}}}
	b.send(ctx, chat, text, kb)
}

// shopInput takes a typed answer for a shop question.
func (b *Bot) shopInput(ctx context.Context, chat int64, what, text string) bool {
	p, seller, _, ok := b.shopWho(ctx, chat)
	if !ok {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(what, "S:"), ":")
	text = strings.TrimSpace(text)
	again := func(msg, back string) bool {
		b.setAwait(chat, what)
		b.send(ctx, chat, msg, &Keyboard{Rows: [][]Button{{shopBtn("« انصراف", back)}}})
		return true
	}
	if parts[0] == "n" && len(parts) == 2 {
		d := b.pdraftOf(chat)
		if d == nil {
			return false
		}
		switch parts[1] {
		case "name":
			if text == "" || len([]rune(text)) > 60 {
				return again("نام باید بین ۱ تا ۶۰ حرف باشد؛ دوباره بفرستید:", "h")
			}
			d.Name = text
			b.shopAskVolume(ctx, chat)
		case "gb":
			n, ok := parseNum(text)
			if !ok || n <= 0 || n > 100000 {
				return again("یک عدد درست بفرستید (مثلاً 25):", "h")
			}
			d.Plan, d.GB = "volume", n
			b.shopCallback(ctx, chat, "S:ng:"+strconv.FormatInt(n, 10))
		case "days":
			n, ok := parseNum(text)
			if !ok || n <= 0 || n > 3650 {
				return again("مدت باید بین ۱ تا ۳۶۵۰ روز باشد؛ دوباره بفرستید:", "h")
			}
			d.Days = n
			b.shopAskUsers(ctx, chat)
		case "price":
			n, ok := parseNum(text)
			if !ok || n < 0 {
				return again("قیمت را فقط با عدد بفرستید (مثلاً 150000):", "h")
			}
			d.Price = n
			b.shopAskCategory(ctx, chat)
		case "cat":
			if text == "" || len(text) > 40 {
				return again("نام دسته کوتاه‌تر باشد (حداکثر ۴۰ حرف):", "h")
			}
			d.Category = text
			b.shopConfirm(ctx, chat)
		default:
			return false
		}
		return true
	}
	if parts[0] != "e" || len(parts) != 3 {
		return false
	}
	id, field := parts[1], parts[2]
	x, ok := b.shopProduct(ctx, p, seller, id)
	if !ok {
		return false
	}
	back := "p:" + id
	switch field {
	case "name":
		if text == "" || len([]rune(text)) > 60 {
			return again("نام باید بین ۱ تا ۶۰ حرف باشد؛ دوباره بفرستید:", back)
		}
		x.Name = text
	case "price":
		n, ok := parseNum(text)
		if !ok || n < 0 {
			return again("قیمت را فقط با عدد بفرستید (مثلاً 150000):", back)
		}
		x.Price = n
	case "gb":
		if strings.Contains(text, "نامحدود") || strings.EqualFold(text, "unlimited") {
			x.Plan, x.GB = "unlimited", 0
		} else {
			n, ok := parseNum(text)
			if !ok || n <= 0 || n > 100000 {
				return again("یک عدد درست بفرستید (مثلاً 30) یا «نامحدود»:", back)
			}
			x.Plan, x.GB = "volume", n
		}
	case "days":
		n, ok := parseNum(text)
		if !ok || n <= 0 || n > 3650 {
			return again("مدت باید بین ۱ تا ۳۶۵۰ روز باشد:", back)
		}
		x.Days = n
	case "ips":
		n, ok := parseNum(text)
		if !ok || n < 0 || n > 100 {
			return again("یک عدد بین ۰ تا ۱۰۰ بفرستید:", back)
		}
		x.MaxIPs = int(n)
	case "cat":
		if text == "" || len(text) > 40 {
			return again("نام دسته کوتاه‌تر باشد (حداکثر ۴۰ حرف):", back)
		}
		x.Category = text
	default:
		return false
	}
	if b.shopSave(ctx, chat, p, x) {
		b.send(ctx, chat, "✅ ذخیره شد.", nil)
		b.shopCard(ctx, chat, id)
	}
	return true
}

// parseNum reads a whole number typed with Persian or Latin digits,
// separators and an optional "تومان".
func parseNum(s string) (int64, bool) {
	var buf strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			buf.WriteRune(r)
		case r >= '۰' && r <= '۹':
			buf.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩':
			buf.WriteRune('0' + (r - '٠'))
		case r == ',' || r == '٬' || r == '،' || r == ' ' || r == '_' || r == '.':
		default:
			if !strings.ContainsRune("تومانهزارمیلیونگیگروزکاربره", r) {
				return 0, false
			}
		}
	}
	if buf.Len() == 0 || buf.Len() > 12 {
		return 0, false
	}
	n, err := strconv.ParseInt(buf.String(), 10, 64)
	if strings.Contains(s, "میلیون") {
		n *= 1000000
	} else if strings.Contains(s, "هزار") {
		n *= 1000
	}
	return n, err == nil
}

func newProductID() string {
	var x [2]byte
	_, _ = rand.Read(x[:])
	return "p" + strconv.FormatInt(time.Now().UnixMilli(), 36) + strconv.FormatInt(int64(x[0])<<8|int64(x[1]), 36)
}

func (b *Bot) pdraftOf(chat int64) *hubcore.Product {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pdraft[chat]
}

func (b *Bot) setPDraft(chat int64, d *hubcore.Product) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pdraft == nil {
		b.pdraft = map[int64]*hubcore.Product{}
	}
	if d == nil {
		delete(b.pdraft, chat)
		return
	}
	b.pdraft[chat] = d
}
