package tgbot

import (
	"regexp"
	"strings"
)

// faErr turns the store's English error into something a Persian reader
// understands; anything not listed keeps its text.
var faErrs = []struct {
	re *regexp.Regexp
	fa string
}{
	{regexp.MustCompile(`old users cannot be renewed`), "کاربرهای قدیمی (منتقل‌شده از پنل قبلی) تمدید نمی‌شوند؛ برایشان کاربر جدید بسازید."},
	{regexp.MustCompile(`overdue bill is unpaid`), "یک فاکتور از مهلتش گذشته و پرداخت نشده؛ تا پرداخت آن فروش و ساخت کاربر قفل است."},
	{regexp.MustCompile(`no volume credit left|insufficient credit`), "اعتبار کافی نیست؛ اول اعتبار را شارژ کنید."},
	{regexp.MustCompile(`panel sale is (no longer )?not allowed|panel sale is no longer allowed`), "فروش پنل برای این نماینده مجاز نیست."},
	{regexp.MustCompile(`the client limit \((\d+)\) is reached`), "به سقف تعداد کاربر (${1}) رسیده‌اید."},
	{regexp.MustCompile(`the longest term allowed is (\d+) days`), "بیشترین مدت مجاز ${1} روز است."},
	{regexp.MustCompile(`the largest volume allowed is (\d+) GB`), "بیشترین حجم مجاز ${1} گیگ است."},
	{regexp.MustCompile(`renewing clients is not allowed`), "تمدید کاربر برای شما مجاز نیست."},
	{regexp.MustCompile(`creating clients is not allowed`), "ساخت کاربر برای شما مجاز نیست."},
	{regexp.MustCompile(`changing client settings is not allowed`), "تغییر تنظیمات کاربر برای شما مجاز نیست."},
	{regexp.MustCompile(`deleting clients is not allowed`), "حذف کاربر برای شما مجاز نیست."},
	{regexp.MustCompile(`this username is taken or not valid`), "این نام کاربری گرفته شده یا معتبر نیست."},
	{regexp.MustCompile(`this Telegram account already has a panel`), "این حساب تلگرام از قبل پنل دارد."},
	{regexp.MustCompile(`client already exists`), "کاربری با این نام از قبل هست."},
	{regexp.MustCompile(`already decided`), "این مورد قبلاً بررسی شده است."},
	{regexp.MustCompile(`the owner has no card set|set the payment card first`), "کارت پرداخت هنوز تنظیم نشده است."},
	{regexp.MustCompile(`the amount is out of range`), "مبلغ معتبر نیست."},
	{regexp.MustCompile(`^not found$`), "پیدا نشد."},
	{regexp.MustCompile(`^unauthorized$`), "دسترسی ندارید."},
}

func faErr(err error) string {
	if err == nil {
		return ""
	}
	m := err.Error()
	for _, f := range faErrs {
		if loc := f.re.FindStringSubmatchIndex(m); loc != nil {
			return string(f.re.ExpandString(nil, f.fa, m, loc))
		}
	}
	return strings.TrimSpace(m)
}
