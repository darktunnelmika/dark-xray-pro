package telegrambot

import (
	"html"
	"strconv"
	"strings"
)

func actionButton(text, style, name, resource, value string) Button {
	return Button{Text: text, Style: style, Action: &Action{Name: name, Resource: resource, Value: value}}
}
func homeButton(lang string) Button {
	return actionButton(tr(lang, "🏠 منوی اصلی", "🏠 Main menu"), "primary", "home", "", "")
}
func backButton(lang, name, resource, value string) Button {
	return actionButton(tr(lang, "↩️ بازگشت", "↩️ Back"), "primary", name, resource, value)
}
func oneScreen(text string, buttons [][]Button) Result {
	return Result{Screen: &Screen{Text: text, Buttons: buttons}}
}
func escaped(text string) string { return html.EscapeString(text) }
func clipped(text string, limit int) string {
	r := []rune(text)
	if len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return text
}

func homeScreen(a Access, lang string, support, campaigns, rewards bool) *Screen {
	name := a.BrandName
	if name == "" {
		name = "DARK"
	}
	text := "<b>" + escaped(name) + "</b>\n\n" + tr(lang, "خرید، تمدید و پیگیری سرویس از همین‌جا.", "Buy, renew and manage your service here.")
	rows := [][]Button{
		{actionButton(tr(lang, "🛒 خرید سرویس", "🛒 Buy service"), "success", "products", "", "")},
		{actionButton(tr(lang, "💎 سرویس‌های من", "💎 My services"), "", "services", "", ""), actionButton(tr(lang, "🔄 تمدید سرویس", "🔄 Renew service"), "", "renew_list", "", "")},
		{actionButton(tr(lang, "💳 کیف پول", "💳 Wallet"), "", "wallet", "", ""), actionButton(tr(lang, "📚 راهنما", "📚 Help"), "", "help", "", "")},
		{actionButton(tr(lang, "🧾 سفارش‌ها و پیگیری پرداخت", "🧾 Orders and payment status"), "", "orders", "", "")},
	}
	if support {
		rows = append(rows, []Button{actionButton(tr(lang, "💬 پشتیبانی", "💬 Support"), "", "tickets", "", "")})
	}
	if rewards {
		rows = append(rows, []Button{actionButton(tr(lang, "🎁 معرفی به دوستان", "🎁 Invite friends"), "", "referral", "", ""), actionButton(tr(lang, "🏅 امتیازهای من", "🏅 My rewards"), "", "rewards", "", "")})
	}
	if campaigns {
		rows = append(rows, []Button{actionButton(tr(lang, "🔔 تنظیم اعلان‌های تبلیغاتی", "🔔 Marketing preferences"), "", "consent", "", "")})
	}
	if a.Role != "customer" {
		rows = append(rows, []Button{actionButton(tr(lang, "⚙️ مدیریت فروشگاه", "⚙️ Store management"), "primary", "manage", "", "")})
	}
	rows = append(rows, []Button{actionButton("فارسی", "", "language", "", "fa"), actionButton("English", "", "language", "", "en")})
	return &Screen{Text: text, Buttons: rows}
}

func managementScreen(a Access, lang string, support, campaigns bool) *Screen {
	role := map[string]string{"owner": tr(lang, "مالک هاب", "HUB owner"), "representative": tr(lang, "نماینده", "Representative"), "admin": tr(lang, "ادمین فروشگاه", "Store administrator")}[a.Role]
	rows := [][]Button{}
	if a.Has("manage_store") {
		rows = append(rows, []Button{actionButton(tr(lang, "⚙️ تنظیمات فروشگاه", "⚙️ Store settings"), "", "settings", "", "")})
	}
	if a.Role == "owner" && a.RepresentativeID == "owner" {
		rows = append(rows, []Button{actionButton(tr(lang, "👥 نمایندگان و اعتبار", "👥 Representatives and credit"), "", "representatives", "", "")})
	}
	if a.Has("finance") {
		rows = append(rows, []Button{actionButton(tr(lang, "📊 اعتبار حجمی و نامحدود", "📊 Volume and unlimited credit"), "", "credits", "", "")})
	}
	if a.Has("manage_products") {
		rows = append(rows, []Button{actionButton(tr(lang, "🏷 محصولات و قیمت‌ها", "🏷 Products and prices"), "", "prices", "", "")})
	}
	if a.Has("manage_admins") {
		rows = append(rows, []Button{actionButton(tr(lang, "👤 ادمین‌های فروشگاه", "👤 Store administrators"), "", "admins", "", "")})
	}
	if support && a.Has("support") {
		rows = append(rows, []Button{actionButton(tr(lang, "🎫 صف پشتیبانی", "🎫 Support queue"), "", "support_queue", "", "")})
	}
	if campaigns && (a.Has("broadcast_draft") || a.Has("broadcast_send")) {
		rows = append(rows, []Button{actionButton(tr(lang, "📣 پیام همگانی", "📣 Broadcasts"), "", "campaigns", "", "")})
	}
	rows = append(rows, []Button{homeButton(lang)})
	return &Screen{Text: "<b>" + escaped(a.BrandName) + " · " + role + "</b>\n\n" + tr(lang, "هر عملیات با سطح دسترسی فعلی شما بررسی می‌شود.", "Actions use your current store permissions."), Buttons: rows}
}

func parsePage(value string) int {
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 10000 {
		return 0
	}
	return n
}
func pageButtons(lang, action, resource string, offset, total int) []Button {
	buttons := []Button{}
	if offset > 0 {
		buttons = append(buttons, actionButton(tr(lang, "قبلی", "Previous"), "", action, resource, strconv.Itoa(max(0, offset-8))))
	}
	if offset+8 < total {
		buttons = append(buttons, actionButton(tr(lang, "بعدی", "Next"), "", action, resource, strconv.Itoa(offset+8)))
	}
	return buttons
}
func command(text string) (string, []string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", nil
	}
	if strings.Contains(fields[0], "@") {
		return "", nil
	}
	return fields[0], fields[1:]
}
