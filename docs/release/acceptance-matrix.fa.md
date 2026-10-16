# DARK XRAY PRO — ماتریس آزمون پذیرش نسخهٔ یکپارچه

این جدول برنامهٔ آزمون است، نه نتیجهٔ اجرای آزمون‌های محصول. مرجع GitHub برای S01 خوانده شد؛ تطبیق کامل checkout و وابستگی‌ها هنوز لازم است. هیچ‌یک از گیت‌های اجرا با تعداد آزمون‌های مستقل قدیمی پر نمی‌شوند.

| شناسه | حوزه | مدرک لازم | شرط عبور | وضعیت این نوبت |
|---|---|---|---|---|
| S01 | تطبیق سورس و وابستگی | `live_main` | همان Commit و go.mod/go.sum بدون جایگزین آزمایشی؛ ورودی تمیز یا تطبیق تمام blobها | مرجع خوانده شد؛ آزمون اجرا نشد |
| S02 | تطبیق نسخه و هش هسته | `real_core` | آرشیو و binary طبق CI همان Commit؛ نتیجهٔ v25 جای v26 حساب نشود | اجرا نشده |
| D01 | ایجاد اتمیک مشتری | `project_database` | ایجاد UUID، خرید، debit و baseline در یک تراکنش؛ تزریق شکست و retry | اجرا نشده |
| D02 | مصرف قدیمی نامعلوم | `project_database` | baseline مشتری موجود فقط از import معتبر؛ نبود داده معادل صفر نیست | اجرا نشده |
| D03 | ثبت دقیق و ضدتکرار | `project_database` | دو اتصال DB هم‌زمان، پاسخ گم‌شده پس از commit و replay پس از restart؛ total برابر | اجرا نشده |
| D04 | اعتبار و تمدید | `project_database` | کاربر×ماه و split renewal؛ مصرف حفظ شود و توقف دستی خودکار برداشته نشود | اجرا نشده |
| D05 | خرابی schema و flag | `project_database` | schema ناقص/ناشناخته رد شود؛ برداشتن flag quota ثبت‌شده را دور نزند | اجرا نشده |
| R01 | Registry واقعی | `actual_agent` | فقط Node و installation ثبت‌شده؛ token اشتباه/سابق و نقش‌دادن در body رد | اجرا نشده |
| R02 | شروع امن metering | `actual_agent` | قبل از Listener، epoch و صف و scope و authority حاضر؛ بدون پنجره ترافیک بی‌حساب | اجرا نشده |
| R03 | A دو اتصال، B یک اتصال | `real_core` | A با پایان حجم هر دو اتصال را از دست بدهد؛ همان اتصال B با همان IP مبدأ ادامه دهد | اجرا نشده |
| R04 | تمام مسیرهای موردنیاز | `real_core` | TCP/REALITY و Vision اجباری؛ gRPC/UDP/MUX/سایر transportهای inventory بدون bypass آزموده شوند | اجرا نشده |
| R05 | مجوز دوگانه و replay | `actual_agent` | اشتراک مجوز runtime و budget؛ درخواست تکراری حجم یا زمان تازه نسازد | اجرا نشده |
| R06 | قطع HUB و توقف امن | `actual_agent` | بدون پاسخ جدید authority منقضی شود؛ heartbeat مستقل؛ data ناشناخته صفر نشود | اجرا نشده |
| R07 | crash و بازیابی | `actual_agent` | SIGKILL قبل/بعد fsync/COMMIT؛ آزمون reboot مستقل؛ old epoch از صفر شروع نشود | اجرا نشده |
| R08 | پایان سالم دوره | `actual_agent` | جریان‌ها پایان‌یافته، final در صف پایدار، receipt ثبت‌شده؛ سپس release سهم | اجرا نشده |
| R09 | لغو و Rollback | `real_core` | نسخه شکست‌خورده مشتری/endpoint لغوشده را زنده نکند؛ ledger به عقب برنگردد | اجرا نشده |
| R10 | چند نود و Swap | `multi_node` | دو اجرای مستقل یک سهم را خرج نکنند؛ واسط دوباره billable نشود؛ بازتخصیص پس از تسویه | اجرا نشده |
| R11 | گزارش جعلی یا قدیمی | `actual_agent` | ACK اجرا، اتصال مشتری و صحت metering سه مدرک جدا؛ stale هیچ‌کدام را تازه نکند | اجرا نشده |
| U01 | مصرف در UI واقعی | `browser_live` | owner و دو rep؛ عدد صحیح، known/hold/exhausted/manual مستقل؛ Refresh و موبایل | اجرا نشده |
| U02 | Subscription واقعی | `browser_live` | لینک همان UUID و مسیر معتبر، expiry و state درست؛ نماینده اطلاعات دیگری نبیند | اجرا نشده |
| P01 | ظرفیت و هزینه پردازش | `performance` | مقایسه با baseline همان سخت‌افزار و dataset اعلام‌شده؛ latency/RAM/CPU/throughput و اثر قطع‌ها ثبت | اجرا نشده |
| M01 | پوشش schema و روابط | `migration_copy` | همهٔ entityها و native/Restore با namespace جدا؛ هیچ silent skip یا promotion ضمنی | اجرا نشده |
| M02 | تطبیق مالی و زمان | `migration_copy` | count/fingerprint/UUID/quota/expiry و balances/receipts بدون افت یا duplicate | اجرا نشده |
| M03 | Backup بزرگ و recovery | `migration_copy` | رمزگذاری، محدودیت size/RAM، قطع import، اجرای مجدد و خروجی خالی؛ بدون تغییر قدیمی | اجرا نشده |
| M04 | Telegram و فروش کامل | `migration_copy` | planned migration مستقل از DR؛ BotID/user/chat/order/payment محفوظ و فقط یک receiver فعال | اجرا نشده |
| M05 | قرارداد پورت و دامنه | `cutover` | پورت عمومی قدیمی با منبع واقعی تأیید، scheme/host/path/query و TLS/pin و تمدید قبول | اجرا نشده |
| M06 | بازگشت با write جدید | `cutover` | delta مصرف/پرداخت/update بعد cutover محفوظ؛ DNS-only rollback کافی نیست | اجرا نشده |
| M07 | تأیید نهایی مالک | `human_approval` | پس از تکمیل امکانات و پایداری؛ مهاجرت واقعی فقط با تأیید جداگانه و ثبت‌شده | اجرا نشده |

## حداقل بستهٔ شواهد هر اجرای معتبر

Commit و tree، hash فایل‌های وابستگی، نسخه و هش آرشیو/باینری Xray، driver واقعی پروژه، schemaهای مبنا و نتیجه، محیط سخت‌افزاری و capability inventory باید مشخص باشند. زمان شروع/پایان و لاگ خام هر آزمون، exit code، نام subtest، معیار اندازه‌گیری و نتیجهٔ موردانتظار/مشاهده‌شده ثبت شود.

`no tests to run`، Skip، مدل ساختگی به‌جای Registry، receipt ساختگی به‌جای commit، نسخهٔ متفاوت هسته و فقط build موفق، مدرک عبور گیت واقعی نیستند. فایل‌های خروجی قبلی به‌جای اجرای تازه استفاده نمی‌شوند. تست مثبت و منفی هر مسیر باید در یک نسخه قابل بازتولید باشند.

آزمون شرطی بر اساس transport تنها وقتی قابل حذف از اجرای مشخص است که inventory معتبر نشان دهد آن ترکیب لازم نیست؛ این مجوز حذف قابلیت توافق‌شده از محصول نیست.

## سه نوع وضعیت مستقل

۱. طراحی/قرارداد آماده؛ ۲. پیاده‌سازی در سورس با آزمون واقعی همان نسخه؛ ۳. نصب آزمایشی و سپس مجوز انتقال. هیچ‌کدام از دیگری استنتاج نمی‌شود. فایل `readiness.json` برای پیگیری است و خودکار هیچ فرمان نصب یا انتقالی ندارد.
