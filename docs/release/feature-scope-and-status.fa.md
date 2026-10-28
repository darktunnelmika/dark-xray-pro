# DARK XRAY PRO — دامنهٔ قابلیت‌ها و وضعیت واقعی پیاده‌سازی

افزودهٔ **۲۵ اکتبر ۲۰۲۶**: [ایجاد اینباند نسخهٔ ۴](../inbound-authoring-parity-v4.fa.md)
مقصد مستقل هاب، ۱۴ نوع پروتکل، هفت انتقال و ابزارهای کامل‌تر REALITY/TLS را در مسیر
authoring، ذخیرهٔ رمز‌شده و تولید کانفیگ اضافه می‌کند. وضعیت عملیاتی Apply، موتورهای
خارجی و انتشار اشتراک در سند همان تغییر صریحاً مشخص شده است.

تاریخ ممیزی: **۲۲ اکتبر ۲۰۲۶**. مبنای سورس:
`c740c48dea5d25ba61ebc7c55aa7ae2c2ec2c8b3`، درخت
`d7f5e3603fcf68934bb5b85881439d6954b80946`، پس از
[PR #96 / N17](https://github.com/darktunnelmika/dark-xray-pro/pull/96).
افزوده‌های بعدی: [N18 — توکن و خروجی اشتراک LAB](../native-lab-subscriptions-n18.fa.md)
در [PR #98](https://github.com/darktunnelmika/dark-xray-pro/pull/98)، و
[N19 — manifest و اثبات مسیر مصرف‌کننده](../consumer-endpoint-manifests-n19.fa.md)
در **۲۳ اکتبر ۲۰۲۶** بر مبنای `77136723985efd7c35e73c3c2cafdb88bfe12bd1`.
[پوسته و چیدمان مشترک](../neutral-ui-shell-v1.fa.md) نیز در **۲۳ اکتبر ۲۰۲۶**
بر مبنای `bc75759797da94b5e2018ed77c2f9b4c4ea89689` به F16 افزوده شده است.
برنامهٔ [سامان‌دهی مرحله‌ای پنل](../panel-organization-roadmap.fa.md) در همین تاریخ
به F16 افزوده شد؛ پایهٔ UX اینباندها با build موفق و **۲۲۹ تست موفق مرورگر**
پذیرش محدود رابط دارد. این تغییر، وضعیت عملکردی سایر حوزه‌ها را کامل اعلام نمی‌کند.
سایر ردیف‌ها همچنان از ممیزی مبنا هستند.

**نتیجه: PRO هنوز نسخهٔ کامل و آمادهٔ جایگزینی نیست.** بخش‌های اصلی کسب‌وکار،
هویت نود، حسابداری، Inbound، انتقال و تحویل اشتراک آزمایشگاهی پیاده شده‌اند؛ انتشار اشتراک عمومی،
مسیرهای چندنودی عملیاتی، فروش کامل، ربات، بکاپ و چند گیت امنیت و پایداری بازند.
این سند مرجع مشترک دامنه و وضعیت است تا یک مرحلهٔ موفق، به معنی پایان کل محصول
برداشت نشود و امکانات انجام‌شده نیز به‌علت متن قدیمی «ساخته‌نشده» گزارش نشوند.

این ممیزی بر کد، آزمون‌های موجود، قراردادهای ثبت‌شده و Issueهای
[#1](https://github.com/darktunnelmika/dark-xray-pro/issues/1)،
[#2](https://github.com/darktunnelmika/dark-xray-pro/issues/2)،
[#3](https://github.com/darktunnelmika/dark-xray-pro/issues/3)،
[#4](https://github.com/darktunnelmika/dark-xray-pro/issues/4)،
[#5](https://github.com/darktunnelmika/dark-xray-pro/issues/5)،
[#22](https://github.com/darktunnelmika/dark-xray-pro/issues/22) و
[#60](https://github.com/darktunnelmika/dark-xray-pro/issues/60) متکی است.
وضعیت نصب زنده از سورس استنتاج نشده و هیچ آزمون میدانی تازه در این ممیزی انجام نشده است.

## تصمیم قطعی دامنه: WARP و ضدتبلیغ حذف‌اند

طبق درخواست مالک، **WARP و مسدودسازی تبلیغات به‌کل جزو این پنل نیستند**:
نصب، ماژول، منو، تنظیم UI/API، افزونهٔ اختیاری، فاز بعدی یا انتقال قابلیت قدیمی
برای آن‌ها در دامنهٔ فعلی وجود ندارد. نبودشان قابلیت جاافتاده یا مانع انتشار نیست.

در سورس مبنا هیچ قابلیت فعال WARP/adblock پیدا نشد؛ اشاره‌های موجود، اعلام
خارج‌بودن از دامنه بودند. بنابراین این تغییر «حذف پیاده‌سازی موجود» نیست، بلکه
تثبیت تصمیم و رفع ابهام نقشهٔ راه است. `blackhole` مربوط به توقف امن/محدودسازی
probe در [تنظیمات بدون listener](../../internal/xrayconfig/stopped.go) و
[آزمون سلامت](../../internal/managedxray/protocol_health_linux.go) ضدتبلیغ نیست.
انصراف از پیام تبلیغاتی در قرارداد ربات نیز حق مخاطب پیام همگانی است و حفظ می‌شود.

## معنی وضعیت‌ها

- **پیاده‌شده در سورس:** API، ذخیره‌سازی یا UI واقعی دارد؛ این عبارت ادعای نصب فعلی یا پذیرش تولیدی نیست.
- **محدود/آزمایشگاهی:** مسیر واقعی در محدودهٔ مشخص پیاده یا آزموده شده؛ تعمیم به اینترنت، همهٔ transportها یا ناوگان مجاز نیست.
- **باقی‌مانده:** برای تکمیل قرارداد محصول، توسعه یا پذیرش مستقل لازم است.

## ماتریس قابلیت‌ها

| شناسه و حوزه | آنچه اکنون وجود دارد | آنچه هنوز باید تکمیل شود | شاهد سورس/قرارداد |
| --- | --- | --- | --- |
| F01 — مالک، نماینده و امنیت پایه | Argon2id، نشست قابل ابطال، cookie امن، CSRF/Origin، جداسازی نقش و tenant، محدودیت تلاش ورود؛ مجوزهای جزئی نماینده و audit واقعی؛ API محلی مالک با token زمان راه‌اندازی | TOTP/2FA، مدیریت API Key مالک با ایجاد/ابطال/گردش کلید، قفل ماندگار ورود و کنترل لبه، چرخهٔ rotation راز/اعتماد و ممیزی مستقل امنیت | [auth](../../internal/business/auth.go)، [browser auth](../../internal/api/browser_auth.go)، [owner API](../../internal/api/api.go)، [V9](../representative-account-controls-v9.md) |
| F02 — Node Manager و افزودن نود | Pair Code یک‌بارمصرف و لغو دعوت، هویت نصب مستقل، TLS/pin، installer نود تازه، نام‌گذاری با revision/audit و خطای قابل retry | چرخهٔ کامل Enable/Disable/Maintenance/Remove/Replace از UI؛ بازنشستگی اتمیک با حفظ مشتری و وابستگی‌ها؛ پذیرش روی چند ماشین مستقل | [Node API](../../internal/api/node_manager.go)، [lifecycle blocker](../../internal/api/node_dependencies.go)، [installer](../new-pro-node-installer-v2.md) |
| F03 — وضعیت و پایش نود | heartbeat مستقل از metrics، Online/Offline و Stats delayed، CPU/RAM/disk/load/uptime، ترافیک NIC و socketهای میزبان، تاریخچهٔ واقعی ۱h/۲۴h با فعال‌سازی صریح | وضعیت/اتصال واقعی مشتریان Xray، اصل IP و آمار مستقل هر مسیر؛ پذیرش فشار و قطعی ناوگان. تعداد CPU فعلی logical است؛ نباید هستهٔ فیزیکی نامیده شود | [host metrics](../node-host-metrics-history.md)، [Agent](../../cmd/node-agent/main.go)، [history](../../internal/nodehistory/history.go) |
| F04 — Inbounds و Hosts | Native VLESS/REALITY با rendererِ TCP/Vision و gRPC؛ اتصال مشتری، کلید رمزگذاری‌شده، ویرایش/توقف/آرشیو با حفظ UUID/حساب‌ها؛ draft چند Host و پورت مستقل | پذیرش runtime همهٔ پروتکل/transportهای موردنیاز، مدیریت عملیاتی کامل Hosts/چند IP و مسیر عمومی؛ rendererِ gRPC مدرک پشتیبانی metered final-TCP نیست | [lifecycle](../native-inbound-lifecycle.md)، [renderer](../../internal/xrayconfig/render.go)، [route drafts](../../internal/business/native_route_drafts.go) |
| F05 — انتقال نود و N17 | reservation، توقف و تسویهٔ مبدأ، Apply/ACK مقصد، receipt ضدتکرار و round سلامت همهٔ اینباندهای candidate؛ آزمون واقعی محدود در محیط ایزوله | تعویض مسیر مصرف‌کننده، publication اتمیک، ادامه/ابطال lease، بازیابی و rollback کل انتقال و آزادسازی صحیح رزروها | [handover](../../internal/business/metered_handovers.go)، [N17](../metered-health-rounds-n17.fa.md) |
| F06 — Direct/Tunnel/Dark Swap | انتخاب مستقل مسیرها، wizard سه‌فیلدی، draft رمزگذاری‌شده، بررسی برخورد پورت/چرخه و readiness مستقل دو سمت | forwarding واقعی چند hop، failover/last-good، حفظ upstream و سایر نودها، Apply/ACK هر مسیر، مصرف یکتای مقصد و Live/۱h/۲۴h قابل‌انتساب. builderهای فعلی Swap را رد می‌کنند | [route transaction](../../internal/business/route_transactions.go)، [metered route](../../internal/business/metered_routes.go)، [two-hop readiness](../inbounds-p0g-two-hop-readiness.md) |
| F07 — اشتراک، QR و تحویل سرویس | N18: توکن پایدار و snapshot رمزگذاری‌شده با CAS و دریافت raw/Base64؛ N19: manifest کامل و تغییرناپذیر نشانی/پورت با مجوز صریح هر اینباند، اثبات v3 و URI از همان tuple، ابطال پس از تعویض/لغو یا حذف مجوز؛ Direct TCP/Vision با اثبات از سمت Node و مهلت اولیهٔ حداکثر ۸ثانیه؛ `can_publish=false` | اثبات مستقل دسترسی بیرونی، DNS/IPv6 و publisher عمومی، تجمیع چند نود و تعویض اتمیک، lifecycle/renew/rollback پس از انتشار؛ QR، متادیتای مصرف/انقضا و خروجی‌های اپلیکیشن‌محور | [قرارداد N18](../native-lab-subscriptions-n18.fa.md)، [قرارداد N19](../consumer-endpoint-manifests-n19.fa.md)، [گیت U02](acceptance-matrix.fa.md) |
| F08 — Clients و تمدید | ساخت UUID، debit نماینده و event اتمیک، همراه baseline در همان تراکنش هنگام فعال‌بودن دفتر مصرف؛ نام دستی، حجمی/نامحدود، تمدید/افزایش حجم، ویرایش محدود، توقف/آرشیو و عملیات گروهی اتمیک؛ جست‌وجوی سراسری و pagination | Import/Export بومی مشتریان با حفظ هویت، روابط و مصرف و اعتبارسنجی پیش از اعمال؛ شروع مدت از اولین اتصال، تحویل سرویس و مصرف زندهٔ قابل اتکا؛ «ویرایش همه‌چیز» موجود نیست و تغییر نوع پلن/مالکیت/UUID یا refund صندلی جزو ویرایش فعلی نیست | [Store](../../internal/business/store.go)، [business API](../../internal/api/business.go)، [usage seed](../../internal/business/usage.go)، [lifecycle](../../internal/business/lifecycle.go)، [V8](../client-profile-editor-v8.md)، [search](../client-global-search-v4.md) |
| F09 — Groups و Representatives | گروه پایدار، انتقال عضویت بدون حذف مشتری/مصرف، مدیریت نماینده و اعتبار/مجوز/تعلیق/ابطال نشست، فهرست و تراکنش‌های واقعی | گزارش مصرف تاریخی گروه و تطبیق گروه/نماینده در Dark Restore؛ نقش نمایندهٔ پنل معادل ادمین ربات نیست | [Groups](../../internal/business/groups.go)، [V9 controls](../../internal/business/representative_controls_v9.go) |
| F10 — دفتر اعتبار و مصرف | اعتبار حجمی و نامحدود کاربر×ماه، تمدید بدون کسر تکراری، baseline صفر فقط برای مشتری تازه، شمارندهٔ صحیح، receipt و replay، ingest با Registry واقعی، outbox پایدار و settlement/hold | سقف سخت مشترک حجم میان نودهای هم‌زمان، قطع فقط مشتری تمام‌شده با حفظ اتصال مشتری دیگر در همهٔ مسیرهای لازم، رفع عملیاتی unknown-tail و پذیرش بازیابی/فشار | [ledger](../../internal/usageledger/ledger.go)، [usage API](../../internal/api/usage.go)، [metering](../distributed-native-metering-v1.md) |
| F11 — IP Limit، HWID و اولین اتصال | `IPCount` به‌عنوان سیاست ذخیره‌شده/عامل ظرفیت و قیمت وجود دارد؛ expiry مطلق از زمان ساخت محاسبه می‌شود | enforcement واقعی IP/HWID، اصل IP معتبر پشت tunnel و مشاهدهٔ کاربران آنلاین؛ آغاز مدت با اولین اتصال. ذخیرهٔ عدد IP محدودکنندهٔ شبکه نیست | [client edit](../../internal/business/client_profile_edit.go)، [config users](../../internal/business/inbounds.go)، [timing](../../internal/business/store.go)، [گیت‌های باقی‌مانده](../native-usage-product-acceptance.md) |
| F12 — Marketplace و فروش کامل | V10a/V10b: محصول، قیمت، draft/publish/archive کاتالوگ، سیاست نماینده و audit | V10c: checkout، سفارش/پرداخت، خرید/تمدید متصل به محصول و ساخت اتمیک مشتری، wallet و رفع شکست بدون debit تکراری. `checkout_enabled=false` است | [Catalog API](../../internal/api/catalog_v10.go)، [V10b](../marketplace-representative-policy-v10b.md) |
| F13 — Telegram و Mini App | قرارداد نیازها ثبت شده؛ runtime/receiver/worker و Mini App فروش پیاده نشده‌اند | جریان مالک/مشتری/نماینده، shop، خرید/پرداخت/تمدید، QR/اشتراک، پشتیبانی/تیکت، اعلان‌ها؛ ادمین ربات نماینده با scope، پیام همگانی با پیش‌نمایش/تأیید/صف/توقف و گزارش | [نیازهای ربات و Mini App](../telegram-representative-upgrade.fa.md)، [قرارداد حفظ کارکرد](migration-contract.fa.md) |
| F14 — Backup/Restore محصول | هستهٔ بومی آفلاین: فهرست منابع و قفل نویسنده‌ها، snapshot مستقل SQLite، آرشیو streaming رمز‌شده، بررسی کامل و restore به مقصد غایب و قرنطینه‌شده؛ پذیرش مالی و replay و آزمون codec بیش از ۳GiB در [نسخهٔ آفلاین ۱](../native-backup-v1.fa.md). snapshot نصب، جای این آرشیو را نمی‌گیرد | ساخت/دانلود/آپلود/بررسی/بازیابی از UI مالک، زمان‌بندی روزانه/هفتگی و retention، کپی اختیاری امن خارج از میزبان؛ آزمون عملیاتی دیتابیس بزرگ و دیسک/قطع برق، هماهنگی مصرف نودها، فعال‌سازی بازیابی‌شده و rollback؛ M03 کامل هنوز تأیید نشده است | [گیت بکاپ #4](https://github.com/darktunnelmika/dark-xray-pro/issues/4)، [M03](acceptance-matrix.fa.md) |
| F15 — Dark Restore مستقل | ابزار واقعی M0 برای بررسی فقط‌خواندنی schema، روابط و جمع مصرف تاریخی، با fixture مصنوعی و گزارش بدون token/URL؛ `ImportEnabled=false` | workspace و namespace مستقل Restore، importer پایدار/idempotent با دادهٔ مصنوعی، group/rep mapping، حفظ UUID/token/URL/baseline، آرشیو expired بدون کاهش مصرف و recovery/rollback | [M0](../../internal/darkrestore/preflight.go)، [مرزهای M0](../dark-restore-m0-offline-preflight.fa.md)، [#60](https://github.com/darktunnelmika/dark-xray-pro/issues/60) |
| F16 — داشبورد و UX | پوستهٔ خنثی مشترک، فونت محلی فارسی/لاتین، ورود و مجوز، فارسی/انگلیسی و Light/Dark/System با انتخاب تصویری، RTL موبایل ۳۲۰px، منوی کشویی با مدیریت فوکوس و نشست، loading/empty/unavailable متمایز، پایش واقعی HUB و نودها؛ ناوبری چهارگروهی، فهرست/فیلتر/صفحه‌بندی اینباند، پنجرهٔ ساخت/کپی/مقصد نود، تب‌های جزئیات با حفظ ویرایش و لینک مستقیم، مسیر مستقل پیش‌نویس Dark Swap؛ پذیرش ۲۲۹ تست مرورگر | UX ماژول‌های باز، گزارش/هشدار/لاگ عملیاتی و سنجش latency باقی‌اند. مقصدهای وضعیت Restore/Telegram/Backup فقط کارهای باقی‌مانده را مشخص می‌کنند و تکمیل ماژول محسوب نمی‌شوند. تاریخچهٔ HUB فعلاً حدود ۳دقیقه در RAM است؛ با تاریخچهٔ پایدار نود یکسان نیست | [برنامهٔ مرحله‌ای و معیار پایان](../panel-organization-roadmap.fa.md)، [پوسته و چیدمان](../neutral-ui-shell-v1.fa.md)، [shell](../../web/src/App.tsx)، [navigation](../../web/src/navigation.ts)، [HUB monitor](../../internal/hubmonitor/monitor.go)، [یک محصول](../one-product-architecture.md) |
| F17 — نصب، تنظیمات و نگهداری | bundle با UI توکار، checksum، نصب محافظت‌شدهٔ HUB و Agent آزمایشگاهی با systemd غیرroot، TLS/pin و nginx allowlist | نصب عمومی کامل، update/recovery خودسرویس، راه‌اندازی دامنه/TLS/پورت، تمدید گواهی و چرخهٔ اعتماد/rotation، پذیرش ماشین تازه و reboot | [LAB installer](../../scripts/install-lab.sh)، [Node installer](../new-pro-node-installer-v2.md)، [HTTPS](../node-manager-https-deployment.md) |
| F18 — پذیرش یکپارچه و پایداری | CI واقعی Go/race/vet، Chromium، هستهٔ پین‌شده و سناریوهای replay/crash/settlement/health؛ شواهد N17 در PR #96 | soak با ۷۵۰+ مشتری مصنوعی و تاریخچهٔ بزرگ، latency/RAM/CPU/throughput، چند میزبان مستقل، reboot/DR کامل، امنیت و همهٔ جریان‌های owner/rep/customer/bot روی یک نسخه | [Foundation CI](../../.github/workflows/ci.yml)، [Native CI](../../.github/workflows/native-usage.yml)، [ماتریس پذیرش](acceptance-matrix.fa.md) |

## نکاتی که نباید از قلم بیفتند

**سلامت N17 محدود است.** فقط Direct روی `127.0.0.1` با VLESS Reality / TCP /
Vision را اثبات می‌کند. تست تمام اینباندهای همان candidate، سلامت host/port
عمومی، CDN، Tunnel یا خروجی واقعی subscription نیست. تغییر یک bool به
`can_publish=true` جای publisher، manifest، proof مسیر مصرف‌کننده و تعویض
اتمیک را نمی‌گیرد. [دامنهٔ probe](../../internal/managedxray/protocol_health_linux.go)

**N18 خروجی واقعی محدود دارد.** token/access، snapshot و endpoint بومی اکنون
پیاده شده‌اند و خروجی به همان UUID و candidate اعمال‌شده متصل است. اعتبار
دریافت از مهلت اولیهٔ round/lease/کلاینت فراتر نمی‌رود و بعد از انقضا متوقف
می‌شود؛ ثابت‌بودن لینک به معنی تحویل پیوسته یا آمادگی مسیر عمومی نیست.
[مرز تحویل N18](../native-lab-subscriptions-n18.fa.md)

**N19 همان tuple مصرف‌کننده را از سمت Node اثبات می‌کند.** مجوز startup به
نود/نصب/اینباند/IP/پورت وابسته است و manifest کامل، درخواست v3 و خروجی URI
به یک هویت متصل‌اند. تعویض یا لغو manifest به خروجی loopback قبلی برنمی‌گردد.
این اثباتِ داخل LAB جای دسترسی مستقل از اینترنت، publisher عمومی یا چرخهٔ
lease پس از انتشار را نمی‌گیرد. [مرز N19](../consumer-endpoint-manifests-n19.fa.md)

**ledger معتبر با سقف سخت چندنودی یکی نیست.** receipt و تسویهٔ نهایی واقعی
وجود دارند؛ API هنوز `distributed_hard_limit_verified=false` اعلام می‌کند.
رزرو نود برای handover، سهمیهٔ بایتی هر مشتری میان چند نود نیست. حفظ hold در
خرابی، داده را محافظت می‌کند ولی workflow رفع ابهام و بازگشت سرویس را کامل
نمی‌کند. [usage API](../../internal/api/usage.go)، [راهنمای VolumeStatus](../../internal/usagecheckpoint/quota.go)

**دو معنای «publish» و «ساخت مشتری» جدا هستند.** publish محصول در Marketplace
فقط نمایش کاتالوگ است؛ CreateClient اپراتوری برای مشتری نماینده، اعتبار همان
نماینده را کسر و هویت را اتمیک می‌سازد؛ مالک از debit معاف است. این‌ها
checkout پرداخت‌شده، انتشار subscription یا تحویل
سرویس کاربر نیستند. [catalog](../../internal/api/catalog_v10.go)، [CreateClient](../../internal/business/store.go)

**Node و Backup دو گیت اصلی تجربهٔ روزمره‌اند.** افزودن یا تغییر نام نود و
لغو دعوت، حذف/تعویض امن نود جفت‌شده را کامل نمی‌کند. Dark Restore M0 نیز
ابزار بررسی است و جای UI بکاپ، بازیابی یا importer کامل را نمی‌گیرد.

**حجم fixture با تست فشار فرق دارد.** آزمون ۷۵۰ مشتری، حفظ UUID/binding/debit
در تغییر draft را می‌سنجد؛ مدرک ۷۵۰ اتصال هم‌زمان یا latency نیست. اهداف
ثبت‌شده هنوز نیازمند اندازه‌گیری روی سخت‌افزار مرجع‌اند: dashboard p95 زیر
۱٫۵ثانیه، Nodes زیر ۲ثانیه و navigation معمول زیر ۱ثانیه.
[آزمون حفظ هویت](../../internal/business/native_route_graph_p0f_test.go)،
[اهداف پذیرش](../architecture.md)

## ترتیب ادامهٔ توسعه

پایهٔ UX اینباندها با دامنهٔ بالا پذیرفته شد. ترتیب اجراییِ تکمیل قابلیت‌ها یکی‌یکی و معیار
پایان هر ماژول در [برنامهٔ سامان‌دهی پنل](../panel-organization-roadmap.fa.md) ثبت شده؛
کارهای فنی زیر پس از این پایه ادامه می‌یابند. افزودن منو یا صفحهٔ وضعیت به‌تنهایی
هیچ‌یک از گیت‌های عملکردی زیر را تکمیل نمی‌کند.

1. در امتداد N19، تکمیل اثبات مستقل دسترسی بیرونی و سیاست مسیر عمومی با fixture ایزوله؛
   اتصال آن به انتشار اتمیک، تجمیع، lease، invalidation و rollback انتقال.
2. تکمیل Node Manager عملیاتی و Direct/Tunnel/Dark Swap واقعی؛ حفظ upstream،
   IP واقعی، آمار مسیر و حسابداری یکتا همراه با آزمون قطع/تعویض/بازیابی.
3. پیشبرد بخش‌های مستقل Backup/Restore و Dark Restore با دادهٔ مصنوعی،
   V10c خرید/تمدید/پرداخت و تکمیل سیاست‌های مشتری/گروه/نماینده.
4. اتصال Bot و Mini App به همان backend و اعتبار/اشتراک، با هویت آزمایشی؛
   اجرای نیازهای ثبت‌شدهٔ ادمین نماینده، پیام همگانی، پشتیبانی و اعلان.
5. تکمیل امنیت، نصب/نگهداری، UX و پذیرش یکپارچهٔ فشار/قطعی/reboot/DR؛
   هیچ ماژول ناقص با تعداد تست‌های مستقل «تمام‌شده» اعلام نمی‌شود.

این ترتیب برنامهٔ توسعهٔ سورس و آزمون‌های مصنوعی است؛ نصب LAB، فعال‌سازی
runtime آزمایشی روی سرور، یا دست‌زدن به نود عملیاتی از آن نتیجه نمی‌شود.
تا پذیرش کامل محصول، هیچ انتقال یا برنامه‌ریزی عملیاتی دادهٔ واقعی قدیمی،
کاربر/نماینده/اعتبار، Bot/token، subscription یا دامنه/پورت شروع نمی‌شود.
HUB قدیمی، نودهای عملیاتی و Xray آمریکا طبق دستور مالک دست‌نخورده می‌مانند.

## استفاده از اسناد و شواهد

README مراحل قدیمی، `architecture.md` و گزارش‌های هر مرحله تاریخچهٔ طراحی
و دامنهٔ همان تغییرند. متن «باقی‌مانده» در یک مرحله ممکن است با مرحلهٔ بعد
تکمیل شده باشد؛ مبنای وضعیت جاری، جدول بالا و سورس ذکرشده است.

`readiness.json` و وضعیت‌های [ماتریس پذیرش](acceptance-matrix.fa.md)، snapshot
طراحی ۱۶ اکتبر روی commit `077ed6d` هستند. در این ممیزی تغییر نکرده‌اند.
شواهد CI جدید باید با همان commit/tree و محدودهٔ آزمون خوانده شوند؛ سبزشدن
CI، پذیرش کل محصول یا مجوز انتقال واقعی نیست. گزارش بعدی باید تاریخ و مبنای
تازه را صریح ثبت کند و وضعیت این snapshot را بدون مدرک بازنویسی نکند.
