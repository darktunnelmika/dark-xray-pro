package telegrambot

func (b *BusinessBackend) help(r Request, name string) (Result, error) {
	lang := r.Language
	back := backButton(lang, "help", "", "")
	switch name {
	case "help":
		rows := [][]Button{
			{actionButton(tr(lang, "📱 اندروید", "📱 Android"), "", "help_device", "android", ""), actionButton(tr(lang, "🍎 آیفون و آیپد", "🍎 iPhone and iPad"), "", "help_device", "ios", "")},
			{actionButton(tr(lang, "💻 کامپیوتر", "💻 Computer"), "", "help_device", "desktop", "")},
			{actionButton(tr(lang, "🧾 پیگیری پرداخت و تحویل", "🧾 Payment and delivery help"), "", "help_payment", "", "")},
			{actionButton(tr(lang, "🧪 زمان تست و مصرف سرویس", "🧪 Trial time and usage"), "", "help_trial", "", "")},
			{homeButton(lang)},
		}
		return oneScreen(tr(lang, "📚 <b>راهنمای استفاده</b>\nاز «خرید سرویس» محصول را انتخاب و مبلغ نهایی فاکتور را بررسی کنید. برای تمدید، سرویس فعلی را از «سرویس‌های من» انتخاب کنید.\n\nراهنمای موردنیاز را از پایین باز کنید. دستور /start همیشه منوی اصلی را باز می‌کند و /cancel ورودی فعلی را لغو می‌کند.", "📚 <b>Getting started</b>\nChoose a product under Buy service and review the final invoice. To renew, select your existing service under My services.\n\nOpen the guide you need below. /start opens the main menu; /cancel cancels the current input."), rows), nil
	case "help_device":
		var title, body string
		switch r.Action.Resource {
		case "android":
			title = tr(lang, "📱 اتصال در اندروید", "📱 Connect on Android")
			body = tr(lang, "۱. «سرویس‌های من» را باز و سرویس را انتخاب کنید.\n۲. «دریافت تنظیمات اتصال» را بزنید و کانفیگ را کپی کنید.\n۳. در برنامهٔ اتصال سازگار، افزودن کانفیگ از کلیپ‌بورد (Import from clipboard) را انتخاب کنید.\n۴. پروفایل افزوده‌شده را انتخاب و اتصال را روشن کنید؛ درخواست ساخت VPN سیستم را تأیید کنید.", "1. Open My services and select your service.\n2. Choose Get connection settings and copy the configuration.\n3. In a compatible connection app, choose Import from clipboard.\n4. Select the imported profile and connect; accept the system VPN request.")
		case "ios":
			title = tr(lang, "🍎 اتصال در آیفون و آیپد", "🍎 Connect on iPhone and iPad")
			body = tr(lang, "۱. از صفحهٔ سرویس، تنظیمات اتصال را دریافت و کپی کنید.\n۲. در برنامهٔ اتصال سازگار، افزودن از کلیپ‌بورد (Import from clipboard) را انتخاب کنید.\n۳. پروفایل افزوده‌شده را انتخاب و اتصال را روشن کنید.\n۴. اگر سیستم اجازهٔ افزودن VPN خواست، آن را تأیید کنید. نام گزینه‌ها به برنامهٔ نصب‌شده بستگی دارد.", "1. Get and copy the connection settings from your service page.\n2. In a compatible connection app, choose Import from clipboard.\n3. Select the imported profile and connect.\n4. If the system asks to add a VPN configuration, approve it. Option names depend on the installed app.")
		case "desktop":
			title = tr(lang, "💻 اتصال در کامپیوتر", "💻 Connect on a computer")
			body = tr(lang, "۱. در تلگرام دسکتاپ یا وب، صفحهٔ سرویس را باز کنید.\n۲. تنظیمات اتصال را کپی کنید؛ اگر فایل دریافت کردید، آن را ذخیره کنید.\n۳. در برنامهٔ اتصال سازگار، افزودن از کلیپ‌بورد یا واردکردن فایل را انتخاب کنید.\n۴. پروفایل را انتخاب و اتصال را روشن کنید؛ حالت اتصال را مطابق راهنمای همان برنامه تنظیم کنید.", "1. Open the service page in Telegram Desktop or Web.\n2. Copy the connection settings, or save the configuration file if supplied.\n3. In a compatible connection app, import from clipboard or import the file.\n4. Select the profile, connect, and follow that app's guide for its connection mode.")
		default:
			return Result{}, ErrProtocol
		}
		body += "\n\n" + tr(lang, "اگر پیام «در انتظار اعمال روی نود» دیدید، وضعیت را از صفحهٔ سرویس پیگیری کنید. برای اتصال ناموفق، از دکمهٔ «پشتیبانی این سرویس» استفاده کنید.", "If the service is awaiting node application, check its service page for progress. For a failed connection, use Service support from that page.")
		return oneScreen("<b>"+title+"</b>\n\n"+body, [][]Button{{actionButton(tr(lang, "💎 سرویس‌های من", "💎 My services"), "primary", "services", "", "")}, {back}}), nil
	case "help_payment":
		rows := [][]Button{{actionButton(tr(lang, "🧾 سفارش‌ها و پیگیری پرداخت", "🧾 Orders and payment status"), "primary", "orders", "", "")}}
		if b.SupportEnabled {
			rows = append(rows, []Button{actionButton(tr(lang, "💬 پشتیبانی پرداخت", "💬 Payment support"), "", "tickets", "", "")})
		}
		rows = append(rows, []Button{back})
		return oneScreen(tr(lang, "🧾 <b>پرداخت و تحویل</b>\nپرداخت در این ربات با Telegram Stars است.\n\n• اگر پرداخت ثبت شده ولی سفارش تکمیل نشده، همان سفارش را باز و «تلاش مجدد برای تکمیل» را انتخاب کنید. دوباره پرداخت نکنید.\n• پس از ثبت سرویس، آماده‌بودن اتصال جداگانه بررسی می‌شود. وضعیت «در انتظار اعمال روی نود» را از صفحهٔ سرویس پیگیری کنید.\n• اگر نتیجهٔ پرداخت روشن نیست، شناسهٔ سفارش را همراه توضیح مشکل در پشتیبانی بفرستید.", "🧾 <b>Payment and delivery</b>\nPayments in this bot use Telegram Stars.\n\n• If payment is recorded but fulfillment is pending, open that order and choose Retry fulfillment. Do not pay again.\n• Once the service is registered, connection readiness is checked separately. Track Awaiting node application on the service page.\n• If the payment result is unclear, include the order ID when contacting support."), rows), nil
	case "help_trial":
		return oneScreen(tr(lang, "🧪 <b>تست و مصرف</b>\nاگر تست در فروشگاه فعال باشد، هر مشتری یک بار می‌تواند آن را دریافت کند. مدت تست ۲۴ ساعت از زمان صدور است و زمان انتظار برای آماده‌شدن اتصال هم جزو آن حساب می‌شود.\n\nمصرف تنها وقتی نمایش داده می‌شود که اطلاعات معتبر در دسترس باشد. «مصرف نامشخص» یعنی هنوز عدد قابل‌تأییدی نداریم. تمدید سرویس، مصرف قبلی ثبت‌شده را پاک نمی‌کند.", "🧪 <b>Trials and usage</b>\nWhen the store enables trials, each customer can claim one trial. Its 24-hour duration starts at issuance, including time spent waiting for connection readiness.\n\nUsage appears only when authoritative data is available. Unknown usage means a verified value is not currently available. Renewal does not erase recorded previous usage."), [][]Button{{actionButton(tr(lang, "💎 سرویس‌های من", "💎 My services"), "", "services", "", "")}, {back}}), nil
	}
	return Result{}, ErrProtocol
}
