# DARK XRAY PRO — Inbound Workspace V3

## Scope of this increment (24 October 2026)

- Added directly reachable Network, Security, Dark Swap, Hosts and Advanced stages while preserving existing Profile, Nodes & routes and Review.
- Every supported Network/Security/Hosts control edits the **same creation plan state**, including gRPC service, REALITY target/SNI/Short IDs and each Node's Direct/Tunnel host list.
- Dark Swap receives a dedicated stage *inside* Inbound creation/editing. Each selected destination's relay source, listener ports and external entry remain editable through the existing validated single-plan API, with original per-Node controls still available.
- A clear unavailable-feature catalog shows legacy transport/security options that cannot yet be applied, without allowing unsupported saves or claiming runtime success.
- Stage tabs scroll horizontally on 320 px screens, support keyboard navigation and preserve the footer.
- Only front-end source, browser tests and documentation change. No registry, node, customer, tunnel, ledger or live Xray state is touched.

## Important non-completions

This is **not** full functional legacy parity. The current backend supports only VLESS/REALITY with TCP/Vision or gRPC; existing plan saves are disabled and do not start listeners. VMess/Trojan/Shadowsocks, WS/XHTTP/HTTPUpgrade, None/TLS, Sniffing/Fallback/advanced JSON, advanced per-Host metadata, target scanning, local HUB execution, and live Dark Swap apply/ack/diagnostics must be developed end-to-end (validator, renderer, metering, health, subscription and regression tests) before enabling their controls.

## ادامهٔ اجرایی — ۲۴ اکتبر ۲۰۲۶

این مرحله تنظیمات پیشرفتهٔ زیر را به فرم، API، پروفایل رمزگذاری‌شده و
تولید کانفیگ متصل می‌کند:

| بخش | گزینه‌ها | رفتار |
| --- | --- | --- |
| شبکهٔ gRPC | Authority و MultiMode | داخل تنظیمات پیشرفتهٔ شبکه؛ فقط در gRPC ارسال می‌شوند. |
| REALITY | Xver، MaxTimeDiff، Min/MaxClientVer و Show | داخل تنظیمات پیشرفتهٔ امنیت؛ در listenerهای مقصد Direct، Tunnel و Dark Swap قرار می‌گیرند. |
| ایجاد، ویرایش و کپی | همان گزینه‌های بالا | با بازکردن مجدد حفظ می‌شوند؛ پاسخ ذخیره باید دقیقاً همان گزینه‌ها را برگرداند. |
| مرورگر قدیمی | جلوگیری از حذف ناخواستهٔ گزینه‌ها | حذف فیلد `options` هنگام ویرایش پروفایل پیشرفته با خطای تعارض رد می‌شود؛ `{}` حذف عمدی تنظیمات پیشرفته است. |
| موبایل | تب فعال و حالت خالی Swap | تب فعال پس از «ادامه»، «قبلی» یا خطای فرم در دید می‌ماند؛ Swap بدون مقصد دکمهٔ انتخاب نود دارد. |

تنظیمات مقصد به listener واسط Swap یا مشتری‌های جدید روی واسط کپی نمی‌شوند.
کلید خصوصی فقط در سرور می‌ماند و ویرایش تنظیمات آن را عوض نمی‌کند.
`Show` مقدار تنظیمات Xray را کنترل می‌کند؛ این مرحله نمایشگر لاگ زنده اضافه
نمی‌کند. وجود Authority/MultiMode در خروجی gRPC به معنی تکمیل اشتراک، سلامت
پروتکل یا تسویهٔ نهایی ترافیک gRPC نیست؛ مرز اجرایی قبلی TCP حفظ شده است.

### اعتبارسنجی و سازگاری

- Xver فقط ۰، ۱ یا ۲ است. MaxTimeDiff بر حسب میلی‌ثانیه و بین ۰ و
  ۹۲۲۳۳۷۲۰۳۶۸۵۴ است تا تبدیل داخلی Core سرریز نکند.
- نسخه‌ها سه جزء عددی ۰ تا ۲۵۵ دارند؛ نسخهٔ حداکثر از حداقل کمتر نمی‌شود.
- گزینهٔ خالی به حالت قبلی تبدیل می‌شود. آزمون‌های digest ثابت می‌کنند
  خروجی کانفیگ و manifest پروفایل‌های بدون تنظیمات جدید، بایت‌به‌بایت حفظ شده است.
- پروفایل دارای گزینهٔ پیشرفته از قالب رمزگذاری‌شدهٔ نسخهٔ ۲ استفاده می‌کند.
  باینری قدیمی نمی‌تواند آن را بخواند و گزینه‌ها را بی‌صدا حذف کند.
  پس از اولین ذخیرهٔ چنین پروفایلی، برگشت به باینری پیش از این قابلیت،
  برگشت سازگار با داده نیست. برگشت اولیهٔ نصب فقط پیش از این نوشته‌ها ممکن است؛
  برای برگشت بعدی باید از نسخه‌ای با همین پشتیبانی استفاده کرد.
- پاک‌کردن صریح گزینه‌ها پروفایل را با همان کلید و هویت به قالب معمول نسخهٔ ۱
  برمی‌گرداند. این کار نیازمند ویرایش عمدی مالک است.

### شواهد این مرحله

- آزمون واقعی مرورگر، UI کامپایل‌شده را به API واقعی و SQLite موقت متصل می‌کند؛
  ایجاد، بازکردن مجدد و ویرایش گزینه‌ها و ثابت‌ماندن داده‌های مشتری/اجرایی بررسی می‌شود.
- آزمون topology روی Core ثابت، ۲۴ کانفیگ TCP/gRPC معمولی و پیشرفته را با
  مقصد Swap مشترک یا اختصاصی بررسی می‌کند.
- آزمون loopback واقعی با Xray رسمی `v26.9.9`، payload مشخص را با محدودیت
  زمان/نسخهٔ مجاز عبور می‌دهد و شمارندهٔ واقعی مشتری را می‌خواند؛ نسخهٔ نامجاز
  به مقصد HTTP نمی‌رسد. CI وجود هر دو نتیجه را الزامی می‌کند و skip را نمی‌پذیرد.
- تست‌های رمزگذاری، ویرایش مرورگر قدیمی، بازپخش درخواست و پاک‌کردن صریح گزینه‌ها
  حفظ هویت و جلوگیری از حذف ناخواسته را بررسی می‌کنند.

این شواهد به معنی تکمیل همهٔ پروتکل‌ها یا فعال‌سازی عملیاتی Dark Swap نیست.
فهرست کمبودهای بالا و مرز ذخیرهٔ غیرفعال همچنان معتبرند.

مرجع قالب‌های Core: [REALITY](https://xtls.github.io/config/transports/reality.html)،
[gRPC](https://xtls.github.io/config/transports/grpc.html)، و سورس ثابت
[تنظیمات امنیت](https://github.com/XTLS/Xray-core/blob/52a412d9e2f5c2a5142b1b4e2ab3771dacb8b120/infra/conf/transport_security.go).

## Acceptance

- TypeScript and Vite production build must pass.
- Full browser creation suite must pass, including cross-stage state retention, saved-plan CAS, event replay and 320px RTL.
- Test new supported fields in browser fixtures without writing to operational DBs.
- Deploy only reviewed artifacts to isolated PRO LAB after CI passes. Old HUB and customer traffic remain strictly out of scope.
