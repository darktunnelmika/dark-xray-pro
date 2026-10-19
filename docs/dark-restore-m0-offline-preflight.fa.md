# DARK XRAY PRO — Dark Restore M0 / پیش‌بررسی آفلاین

این مرحله فقط کپی مجاز، مستقل و checkpoint شده از SQLite قدیمی را
به‌شکل کاملاً read-only بررسی می‌کند. هیچ Import، فروش، تنظیم Node/Xray،
تغییر اعتبار، انتقال Bot، DNS یا دخالت در HUB قدیمی انجام نمی‌دهد.

## منابع نگاشت قدیم به PRO

مبنای سورس قدیمی GitHub: ed66ded4ec09c9d023635f64d9b05eb592ce06a1

| جدول در سورس قدیمی | قرارداد حفظ داده |
|---|---|
| restore_subscriptions | restore ID، public token، URL/host/path/query، ایمیل، Node/Inbound، upload/download/total/expire، آرشیو و ارتقا |
| restore_groups | گروه و عضویت، حتی بعد از آرشیو |
| restore_usage | مصرف DARK از Scope محلی و نود؛ مستقل از baseline اشتراک قدیمی |
| restore_group_targets | نگاشت چند Node/Inbound |
| restore_safety | metadata تأییدشده، وضعیت قطع دستی و نیاز به بازبینی |
| restore_domains / restore_events | دامنه و تاریخچهٔ قبلی |
| core_clients | هویت Xray کاربران Restore فعال |

قرارداد مهم:
- legacy_total برابر صفر فقط در metadata معتبر به معنی حجم نامحدود است.
- legacy_expire به ثانیهٔ Unix ذخیره شده؛ old core expiryTime بر حسب میلی‌ثانیه است.
- کل مصرف تاریخی برابر است با legacy_upload + legacy_download + مجموع up/down در restore_usage.
- رکورد deleted_at غیرصفر آرشیوی است؛ آرشیو نباید کل مصرف تاریخی را کم کند.
- promoted_at غیرصفر نشان‌دهندهٔ انتقال قدیمی به native است؛ Import مجدد نباید کسر اعتبار تازه ایجاد کند.
- Token، URL، email و کلیدهای محرمانه در گزارش Preflight ظاهر نمی‌شوند.

## اجرای آفلاین پس از گرفتن بکاپ مستقلِ مورد تأیید

دیتابیس باید از بکاپ consistent و رمزگذاری‌شدهٔ مجاز، در محیط ایزوله
بازیابی شده باشد؛ فایل مستقل SQLite و بدون WAL/SHM/journal یا نویسندهٔ فعال.

فرمان نمونه پس از ساخت executable ابزار:

    darkrestore-preflight -offline-copy-verified \
      -source /secure/staging/old-hub-offline.sqlite \
      -as-of-unix 1780000000

خروجی JSON فقط اعداد، مجموع بایت‌ها به‌شکل رشتهٔ ده‌دهی و کدهای
نقص عمومی است. هیچ فایل دیگری ساخته و هیچ شبکه‌ای فراخوانی نمی‌شود.

- ready_for_manual_review: شکل و شمارش داده قابل بررسی است؛ مجوز Import یا cutover نیست.
- blocked: دادهٔ ناقص، منبع unverified، شمارندهٔ منفی، گروه بی‌مرجع یا ساختار ناسازگار.
- خطای source: فایل آفلاین یا مجوز خواندن معتبر نیست.

## پذیرش و باقی‌مانده

تست مصنوعی ۵۱۷ رکورد با چند scope مصرف، حجمی/نامحدود، آرشیو و promotion؛
دو بار بررسی یکسان و مقایسهٔ hash کپی؛ جلوگیری از نشت Token/URL؛
رد WAL، symlink، ساختار ناقص، شمارندهٔ منفی و گروه بدون مرجع.

M0a یعنی ابزار بازبینی سورس و fixture مصنوعی. برای M0b هنوز باید
schema بکاپ واقعی *آفلاین و مجاز* با fingerprint/row count مستقل
تطبیق داده شود. M1–M5 واردکنندهٔ واقعیِ چندگیگابایتی،
انتقال گروه/نماینده، آرشیو expired بدون تغییر مصرف، لینک‌های اشتراک،
حسابداری و Dry-run مستقل هستند و هنوز پیاده‌سازی نشده‌اند.

Dark Swap عملیاتی: Issue #3. Backup چندگیگابایتی: Issue #4.
Dark Restore کامل: Issue #60. گیت انتشار نهایی: Issue #22.
تا تأیید این بخش‌ها به HUB قدیمی، Xray آمریکا، Botها و کاربران واقعی
دست زده نمی‌شود.
