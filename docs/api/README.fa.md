<div dir="rtl">

# API بیرونی دارک ایکس‌ری پرو

[English](README.md) · [SDK برای PHP](../../sdk/php/DarkXrayPro.php) · [SDK برای Python](../../sdk/python/darkxraypro.py)

دو API برای برنامه‌های دیگر، کنار API خود پنل:

| | API پارتنر | API نماینده |
| --- | --- | --- |
| برای | سایت یا رباتی که **پنل نمایندگی می‌فروشد** | **ربات یا سایت خود نماینده** که به مشتری می‌فروشد |
| کلید | `dp_<id>_<secret>`، ساخت مالک (نماینده‌ها ← API فروش پنل) | `dr_<id>_<secret>`، ساخت خود نماینده (تنظیمات ← API فروش من)، مالک یا پارتنری که پنل را فروخته |
| آدرس پایه | `/api/v2/partner` | `/api/v2/reseller` |
| چه می‌بیند | فقط پنل‌هایی که خودش ساخته | فقط مشتری‌های همان نماینده |

هر دو دقیقاً قوانین خود پنل را دارند: کلید پارتنر هیچ‌وقت بیشتر از سقفش اعتبار نمی‌دهد و قوانین قالب یا پکیج را شل‌تر
نمی‌کند؛ کلید نماینده از اعتبار نماینده و طبق قوانینش خرج می‌کند، با همان برگشت اعتبار و همان دفتر حساب پنل. مشخصات
دقیق به صورت OpenAPI 3 در `GET /api/v2/openapi.json` است (برای نشست پنل یا هر کلید؛ `?api=partner` یا `?api=reseller`)
و مستقیم در Postman یا ابزارهای تولید کد باز می‌شود. همین راهنما با مثال داخل پنل هم هست (دکمهٔ «📖 راهنمای API»).

## قواعد کلی

**احراز هویت.** هدر `Authorization: Bearer <کلید>` (یا `X-API-Key: <کلید>`). مسیر مخفی پنل روی درخواست‌های API اثری ندارد.

**JSON.** درخواست و جواب JSON است. حجم‌ها گیگ (اعشاری، هر گیگ = ۲ به توان ۳۰ بایت)، زمان‌ها ثانیهٔ یونیکس، و حجم
نامحدود `-1` است.

**تکرار امن.** روی هر درخواست تغییری (POST، PATCH، DELETE) هدر `Idempotency-Key: <شناسهٔ سفارش شما>` بفرستید. همان کلید
با همان درخواست تا ۲۴ ساعت فقط یک بار اجرا می‌شود: اگر اتصال قطع شد و دوباره فرستادید، همان جواب قبلی با
`Idempotent-Replayed: true` برمی‌گردد؛ پنل دوتایی ساخته نمی‌شود و اعتبار دو بار کم نمی‌شود. همان کلید با درخواست دیگر رد
می‌شود (`idempotency_key_reused`) و درخواستی که هنوز در حال اجراست 409 می‌گیرد. فقط جواب‌های موفق نگه داشته می‌شوند؛
درخواست ردشده را می‌توانید اصلاح کنید و با همان کلید دوباره بفرستید. SDKها خودشان این هدر را می‌گذارند.

**خطاها.** همهٔ خطاها این شکل را دارند و `code` برای برنامه ثابت است:

```json
{"detail": "not enough credit", "code": "insufficient_credit", "request_id": "3f2a9c1b7d4e8a60"}
```

| HTTP | code | یعنی |
| --- | --- | --- |
| 400 | `invalid_json` | بدنه JSON نیست |
| 401 | `unauthorized`، `key_expired` | کلید اشتباه، خاموش یا منقضی |
| 402 | `insufficient_credit` | اعتبار نماینده (یا پنل) کافی نیست |
| 403 | `scope_missing`، `ip_not_allowed`، `policy_denied`، `credit_limit`، `account_disabled` | کلید این اجازه را ندارد، قوانین اجازه نمی‌دهد، سقف کلید پارتنر پر شده، نماینده خاموش است |
| 404 | `not_found` | نیست، یا مال شما نیست |
| 409 | `conflict` | نام یا `external_ref` تکراری است، پنل هنوز مشتری دارد |
| 422 | `validation_failed`، `idempotency_key_reused` | یک فیلد اشتباه است |
| 429 | `rate_limited` | خیلی سریع؛ به اندازهٔ `Retry-After` صبر کنید |
| 500 | `internal_error` | `request_id` را برای مالک بفرستید |

**سرعت.** پیش‌فرض ۱۲۰ درخواست در دقیقه برای هر کلید (مالک می‌تواند ۱ تا ۶۰۰۰ بگذارد). هدرهای `X-RateLimit-Limit` و
`X-RateLimit-Remaining` در هر جواب هست. آدرسی که در یک دقیقه ۲۰ کلید اشتباه بفرستد مدتی رد می‌شود.

**فهرست‌ها.** با `limit` و `offset`؛ جواب `{"items": [...], "total": n, "limit": l, "offset": o}` است. فقط `GET /reps`
پارتنر مثل قبل آرایهٔ ساده برمی‌گرداند و تعداد کل در هدر `X-Total-Count` است. تاریخچه‌ها با `before=<next_before>` و
رویدادها با `after=<next_after>` ورق می‌خورند.

## API پارتنر

دسترسی‌ها: `panels:read`، `panels:write`، `credit:add`، `events` (پیش‌فرض روشن)؛ `credit:revoke`، `panels:delete`،
`clients:read` (مالک برای هر کلید روشن می‌کند). مالک می‌تواند کلید را به چند IP محدود کند، تاریخ انقضا و سقف سرعت بدهد و
پکیج‌های قابل فروش را انتخاب کند.

| متد | مسیر | دسترسی | |
| --- | --- | --- | --- |
| GET | `/api/v2/partner` | | خود کلید: سقف‌ها، `remaining_gb` و `remaining_units` (‎-1 = بدون سقف)، دسترسی‌ها |
| GET | `/packages` | | پکیج‌های قابل فروش |
| GET | `/stats?days=30` | panels:read | آمار پنل‌ها، مشتری‌ها، اعتبار، فروش و برگشت در دوره |
| GET | `/ledger` | panels:read | اعتبار داده‌شده یا پس‌گرفته در همهٔ پنل‌ها |
| POST | `/reps` | panels:write | ساخت پنل |
| GET | `/reps?q=&enabled=&external_ref=` | panels:read | پنل‌ها، جدیدترین اول |
| GET | `/reps/{id}` | panels:read | یک پنل |
| PATCH | `/reps/{id}` | panels:write | نام، یادداشت، external_ref، روشن/خاموش، قوانین، آیدی تلگرام، هشدار کمبود اعتبار |
| DELETE | `/reps/{id}?clients=1` | panels:delete | حذف (با مشتری‌ها وقتی `clients=1`)؛ اعتبار استفاده‌نشده به سقف کلید برمی‌گردد |
| POST | `/reps/{id}/credit` | credit:add | افزودن اعتبار `{credit_gb, units, note}` |
| POST | `/reps/{id}/credit/revoke` | credit:revoke | پس گرفتن اعتبار استفاده‌نشده |
| GET | `/reps/{id}/ledger` | panels:read | دفتر حساب پنل |
| GET | `/reps/{id}/overview` | panels:read | داشبورد پنل |
| GET | `/reps/{id}/clients` | clients:read | مشتری‌های پنل (فقط خواندن) |
| POST | `/reps/{id}/enable`، `/disable` | panels:write | خاموش = ورود و فروش بسته؛ مشتری‌ها وصل می‌مانند |
| POST | `/reps/{id}/password` | panels:write | `{password}` یا خالی برای رمز تصادفی؛ جواب رمز و متن آمادهٔ ارسال دارد |
| POST | `/reps/{id}/sessions/revoke` | panels:write | خروج پنل از همهٔ دستگاه‌ها |
| POST | `/reps/{id}/login-link` | panels:write | لینک ورود یک‌بارمصرف ۱۰ دقیقه‌ای (برای ورود دومرحله‌ای ساخته نمی‌شود) |
| GET، POST | `/reps/{id}/apikeys` | panels:read، panels:write | کلیدهای API نماینده برای همان پنل؛ پنل را همراه API بفروشید |
| DELETE | `/reps/{id}/apikeys/{key}` | panels:write | |

ساخت پنل:

```json
POST /api/v2/partner/reps
Idempotency-Key: order-1001

{"name": "Shop A", "package": "gold", "external_ref": "order-1001", "telegram_ids": [123456789], "policy": {"max_clients": 100}}
```

`package` اعتبار و قوانین پکیج را می‌دهد؛ بدون آن `credit_gb` و `units` بدهید (قوانین از نمایندهٔ قالب کلید می‌آید).
`policy` فقط می‌تواند قوانین را سخت‌تر کند. جواب، پنل است با `password`، `panel_url` و `message` (متن آمادهٔ ارسال با آدرس و
نام کاربری و رمز). `external_ref` برای هر کلید یکتاست، پس با `GET /reps?external_ref=order-1001` دوباره پیدا می‌شود.

## API نماینده

دسترسی‌ها: `account:read`، `clients:read`، `clients:write`، `clients:delete`، `events` (پیش‌فرض روشن)؛ `sellers`.

| متد | مسیر | دسترسی | |
| --- | --- | --- | --- |
| GET | `/api/v2/reseller/me` | account:read | اعتبار، قوانین، کلید، `locked` (قبض سررسیدگذشته) |
| GET | `/inbounds`، `/plans` | account:read | اینباندهای مجاز، پلن‌های منتشرشده |
| POST | `/quote` | account:read | قیمت ساخت یا تمدید و اینکه انجام می‌شود یا نه؛ چیزی تغییر نمی‌کند |
| GET | `/overview`، `/ledger` | account:read | داشبورد، دفتر حساب |
| GET | `/clients?status=&q=` | clients:read | مشتری‌ها |
| POST | `/clients` | clients:write | ساخت مشتری |
| POST | `/clients/bulk` | clients:write | ساخت گروهی `prefix-1 … prefix-n`؛ اگر نامی تکراری باشد یا اعتبار نرسد هیچ چیزی ساخته نمی‌شود |
| GET | `/clients/{name}` | clients:read | یک مشتری با `sub_url` |
| PATCH | `/clients/{name}` | clients:write | روشن/خاموش، محدودیت IP، اینباندها، یادداشت، محدودیت دستگاه، آیدی تلگرام |
| DELETE | `/clients/{name}` | clients:delete | حذف؛ اعتبار استفاده‌نشده طبق قوانین برمی‌گردد |
| POST | `/clients/{name}/renew` | clients:write | دورهٔ جدید؛ حجم استفاده‌نشدهٔ دورهٔ قبل برمی‌گردد |
| POST | `/clients/{name}/reset` | clients:write | صفر کردن مصرف؛ در پلن حجمی مقدار مصرف‌شده کم می‌شود |
| POST | `/clients/{name}/revoke` | clients:write | لینک و مشخصات تازه |
| POST | `/clients/{name}/enable`، `/disable` | clients:write | |
| GET | `/clients/{name}/links`، `/qr.png`، `/usage`، `/devices`، `/ips` | clients:read | لینک‌ها، QR، مصرف روزانه، دستگاه‌ها، IPها |
| GET | `/online` | clients:read | مشتری‌های آنلاین |
| GET، POST، PATCH | `/sellers`، `/sellers/{id}` | sellers | زیرفروشنده‌ها (قوانین هیچ‌وقت شل‌تر از خود نماینده نیست) |
| POST | `/sellers/{id}/credit`، `/credit/revoke` | sellers | انتقال اعتبار یا پس گرفتن آن |

ساخت مشتری:

```json
POST /api/v2/reseller/clients
Idempotency-Key: order-1001

{"name": "ali", "gb": 30, "days": 30, "max_ips": 2}
```

`plan` یا `volume` است (پیش‌فرض، `gb` لازم دارد) یا `unlimited` (هر ۳۰ روز یک واحد). دوره از اولین اتصال شروع می‌شود،
مگر `start_on_first_use` را `false` بدهید. با `product` (شناسهٔ پلن از `/plans`) همه چیز از روی همان پلن پر می‌شود؛
زیرفروشنده فقط همین‌ها را می‌فروشد. جواب: مشتری با `sub_url`، مقدار کسرشده (`charge`) و اعتبار باقی‌مانده (`balance`).

بقیهٔ قوانین پنل هم اینجا برقرار است: نماینده‌ای که قبض سررسیدگذشته دارد (`locked` در `/me`) تا پرداخت نکند نمی‌تواند
بفروشد، تمدید یا ریست کند (`policy_denied`). کاربر قدیمیِ منتقل‌شده از پنل دیگر تمدید نمی‌شود؛ فقط روشن/خاموش و محدودیت IP
تغییر می‌کند و روشن کردنش اعتبار حجمی لازم دارد. نام‌ها بدون توجه به حروف بزرگ و کوچک یکتا هستند: اگر `ali` باشد، `Ali`
گرفته شده است.

## رویدادها و وب‌هوک

هر کلید پارتنر و هر نماینده فهرست رویداد دارد (۳۰ روز نگه داشته می‌شود): `GET …/events?after=<آخرین id>`. با
`PUT …/webhook {"url": "https://…"}` رویدادها برایتان POST هم می‌شوند و کلید امضا یک بار نشان داده می‌شود. امضا
`X-Dark-Signature: sha256=<HMAC-SHA256 از "<timestamp>.<body>">` است و `X-Dark-Timestamp` زمان ارسال است:

```php
$want = "sha256=" . hash_hmac("sha256", $_SERVER["HTTP_X_DARK_TIMESTAMP"] . "." . $body, $secret);
$ok = hash_equals($want, $_SERVER["HTTP_X_DARK_SIGNATURE"]) && abs(time() - (int)$_SERVER["HTTP_X_DARK_TIMESTAMP"]) < 300;
```

حداکثر ۵۰ رویداد در هر ارسال، به ترتیب؛ هر جواب 2xx یعنی رسید. وگرنه بعد از ۱۰، ۲۰، ۴۰ ثانیه … تا یک ساعت فاصله دوباره
فرستاده می‌شود. آدرس وب‌هوک باید عمومی باشد.

رویدادهای پارتنر: `panel.created`، `panel.updated`، `panel.enabled`، `panel.disabled`، `panel.password_reset`،
`panel.credit_added`، `panel.credit_revoked`، `panel.credit_low`، `panel.deleted`.

رویدادهای نماینده (برای هر مشتری، از هر راهی فروخته شده باشد: پنل، ربات، فروشگاه یا API): `client.created`،
`client.updated`، `client.enabled`، `client.disabled`، `client.revoked`، `client.deleted`، `client.renewed`،
`client.usage_reset`، `client.first_used`، `client.expired`، `client.volume_exhausted`، `client.expiring` (۳ روز یا کمتر
مانده)، `client.volume_low` (کمتر از ۱۰٪ مانده)، `credit.added`، `credit.removed`، `credit.low`. جزئیات فیلدهای هر رویداد
در [نسخهٔ انگلیسی](README.md#events-and-webhooks) است.

## SDK

هر کدام یک فایل و بدون وابستگی: [PHP](../../sdk/php/DarkXrayPro.php) و [Python](../../sdk/python/darkxraypro.py). هر دو
روی هر تغییر `Idempotency-Key` می‌گذارند، خطای شبکه و 429 و 5xx را با همان کلید دوباره می‌فرستند، خطا را با `status` و
`code` و `request_id` برمی‌گردانند و امضای وب‌هوک را بررسی می‌کنند.

```php
$partner = new DarkXrayPro\Partner("https://panel.example.com:2087", "dp_…");
$panel = $partner->createPanel(["name" => "Shop A", "package" => "gold", "external_ref" => "order-77"], "order-77");
echo $panel["message"];   // آدرس، نام کاربری و رمز، آمادهٔ ارسال برای مشتری
```

</div>
