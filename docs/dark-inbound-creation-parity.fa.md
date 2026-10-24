# مقایسهٔ عملکرد ساخت اینباند با DARK HUB اصلی

تاریخ ممیزی: ۲۳ اکتبر ۲۰۲۶.

این سند snapshot ممیزی است. تغییرات بعدی PRO در XHTTP، گزینه‌های مشتری
و Host مستقل در [سند شبکه و Host](inbound-network-endpoints.fa.md) آمده‌اند.

این سند، رفتار قابل‌ردیابی در سورس را از ظاهر فرم، ذخیرهٔ پیش‌نویس، اجرای
listener و انتشار لینک جدا می‌کند. مرجع، مخزن **darktunnelmika/dark-xray**
است؛ شاخهٔ `main` هنگام ممیزی روی commit
`ed66ded4ec09c9d023635f64d9b05eb592ce06a1` بود. بررسی با خواندن مخزن انجام
شد؛ به سرور قدیمی، ترافیک مشتری یا تنظیمات عملیاتی آن دسترسی داده نشد.

دامنهٔ مقایسه، ساخت و ویرایش اینباند، انتخاب مقصد، Direct، Tunnel، Dark Swap
و Hosts است. **WARP و ضدتبلیغ مطابق تصمیم مالک خارج از محصول PRO هستند.**
ظاهر مشابه، به‌تنهایی برابری قابلیت محسوب نمی‌شود.

## نتیجهٔ اصلی

DARK اصلی در تنظیم پروتکل، انتقال و امنیت از پروفایل فعلی PRO گسترده‌تر است.
Direct و Tunnel آن listener واقعی می‌سازند و Dark Swap نیز پیاده‌سازی واقعی
واسط TCP و حسابداری مقصد دارد. بااین‌حال، فرم قدیمی یک عملیات یکپارچه برای
ساخت اینباند و Swap نبود: ابتدا اینباند و تخصیص مقصد باید ذخیره می‌شد؛ سپس
تنظیم Swap فرم اینباند را می‌بست و صفحهٔ دیگری باز می‌کرد.

در سورس مرجع، فهرست واسط Swap از **نودهای ثبت‌شده** می‌آید؛ فهرست مستقل
«تونل‌های ایرانِ ازپیش‌ثبت‌شده» در این جریان وجود ندارد. آدرس و پورت ورودی
ایران دستی‌اند و رساندن آن ورودی به پورت واسط، بیرون از این قابلیت تنظیم
می‌شود. بنابراین ساخت یکپارچه در PRO باید همان مسیر واقعی را مدل کند و
وجود کاتالوگ یا سلامت تونلی را که در backend وجود ندارد، ادعا نکند.

منابع: [فرم Inbound، خطوط ۵۰ تا ۷۴ و ۱۱۵ تا ۱۲۱][old-inbound]،
[جریان Swap، خطوط ۲۸ تا ۵۲][old-swap-ui] و
[مدل اجرایی Swap، خطوط ۱۵۷ تا ۲۶۸][old-swap-core].

## ۱. مدل و رفتار واقعی فرم قدیمی

| بخش | کنترل و مقدار ذخیره‌شده | رفتار و محدودیت واقعی |
| --- | --- | --- |
| مشخصات | `remark`, `protocol`, `listen`, `port`, `enable`, `tag` | نام پیش‌فرض `DARK INBOUND`؛ VLESS، `0.0.0.0`، فعال، TCP و امنیت none. پورت از اولین مقدار آزاد از ۴۴۳ انتخاب می‌شود. Tag خالی در سرور ساخته می‌شود. |
| پروتکل | VLESS، VMess، Trojan، Shadowsocks، SOCKS، HTTP | هر شش مورد در فرم هستند. Core علاوه بر آن‌ها پروتکل‌های داخلی `dokodemo-door` و `tunnel` را می‌پذیرد؛ آن‌ها گزینهٔ معمول ساخت مشتری نیستند. |
| حساب مشتری | در API جداگانهٔ Clients | ساخت اینباند مشتری نمی‌سازد. سرور `settings.clients` و `settings.accounts` را در JSON اینباند نمی‌پذیرد؛ اعتبارهای مشتری هنگام ساخت کانفیگ از bindingهای جدا خوانده می‌شوند. |
| TCP | `tcpSettings.header.type` برابر none یا http | هدر در کانفیگ Core قرار می‌گیرد؛ همهٔ جزئیات camouflage در لینک اشتراک سریال نمی‌شوند. |
| WebSocket | `wsSettings.path`, `headers.Host` | پیش‌فرض path برابر `/`؛ Host اختیاری. |
| gRPC | `serviceName`, `authority`, `multiMode` | در کانفیگ Core مصرف می‌شوند؛ لینک معمول فقط serviceName را منتقل می‌کند. |
| HTTP Upgrade | `httpupgradeSettings.path`, `host` | تنظیمات واقعی transport؛ پوشش خروجی همهٔ قالب‌های مشتری یکسان نیست. |
| XHTTP | `path`, `host`, `mode`, `xPaddingBytes` | modeهای auto، packet-up، stream-up و stream-one؛ padding اختیاری. لینک path/host/mode دارد، ولی padding را حمل نمی‌کند. |
| mKCP | `kcpSettings.mtu`, `tti` | پیش‌فرض ۱۳۵۰ و ۵۰. این مسیر UDP است و در Swap اختصاصی TCP پشتیبانی نمی‌شود. |
| IP واقعی | off، Cloudflare، PROXY Protocol | به `sockopt.trustedXForwardedFor` و `acceptProxyProtocol` تبدیل می‌شود؛ CDN یا تونل خارجی نصب یا تأیید نمی‌شود. |
| TLS | `serverName`, `alpn`, `certificateFile`, `keyFile` | فرم هر دو مسیر فایل گواهی و کلید را لازم می‌داند. فایل‌های مقصد نود با قرارداد managed files منتقل و بازنویسی می‌شوند. |
| REALITY | `target`, `serverNames`, `privateKey`, `shortIds` | فقط VLESS/Trojan روی TCP/RAW، gRPC یا XHTTP. mKCP فقط امنیت none دارد. کلید خصوصی معتبر ۳۲بایتی و Short ID هگز لازم است. |
| REALITY سمت مشتری | fingerprint، SpiderX، publicKey | metadata زیر `panelMeta.reality` نگهداری می‌شود؛ کلید عمومی لینک از private key واقعی مشتق می‌شود. |
| REALITY پیشرفته | `xver`, `maxTimeDiff`, `minClientVer`, `maxClientVer`, `show` | مقادیر واقعی `realitySettings` هستند، نه صرفاً وضعیت نمایشی. |
| Sniffing | enabled، destOverride، metadataOnly، routeOnly، ipsExcluded، domainsExcluded | destOverride شامل http/tls/quic/fakedns؛ پیش‌فرض مدل جدید http و tls با routeOnly است. |
| Fallback | ردیف‌های path، alpn، dest، xver | فقط VLESS/Trojan با TCP/RAW و TLS/REALITY. این Fallback تنظیم Xray است و با Node failover یا Swap یکی نیست. |
| JSON پیشرفته | `settings`, `streamSettings`, `sniffing` | فیلدهای راهنمای فرم روی کلیدهای متناظر JSON غلبه می‌کنند. کلیدهای ناشناختهٔ تو‌در‌تو عموماً حفظ و تنظیمات شناخته‌شدهٔ نامرتبط حذف می‌شوند؛ import آزاد کل شیء اینباند نیست. |

منابع: [فرم و تبدیل به بدنهٔ درخواست، خطوط ۷۶ تا ۱۱۳][old-inbound]،
[مدل پیش‌فرض، خطوط ۲۷ تا ۳۳ و ۵۶ تا ۶۷][old-models]،
[اعتبارسنجی و ساخت کانفیگ Core، خطوط ۶۶۲ تا ۷۶۸ و ۹۸۲ تا ۱۰۳۵][old-core].

### قالب، کپی و بررسی فرم

در `inbounds-models.js` چهار helper برای VLESS REALITY، VLESS WS TLS،
Trojan gRPC REALITY و Shadowsocks TCP وجود دارد؛ اما مسیر فعال «New» در
ویرایشگر V3 فقط `template('custom')` را فراخوانی می‌کند. وجود helper به معنی
وجود انتخاب‌گر فعال قالب در فرم نهایی نیست.

کپی، شناسه و applied را حذف، Tag را خالی، نام را با `COPY` و پورت را با
مقدار آزاد جایگزین می‌کند. تنظیمات تو‌در‌تو، از جمله کلید REALITY، حفظ می‌شوند.
ساخت کلید تازه هنگام کپی، رفتار قدیمی نیست.

«Check Form» فقط `buildBody` را اجرا می‌کند؛ اعتبارسنجی واقعی Xray و بررسی
پورت‌های استقرار در آن انجام نمی‌شود. `POST /api/keys/x25519` واقعاً کلید
و Short ID می‌سازد. «Ping» و «Find» تارگت REALITY، probe TCP/TLS از سرور
هستند؛ گواهی، SAN، ALPN، TLS 1.3 و تأخیر handshake بررسی می‌شود. این‌ها ICMP
یا اثبات موفقیت اتصال کامل مشتری نیستند و `x25519Verified` در نتیجه null است.

منابع: [رویدادهای ویرایشگر، خطوط ۱۲۴ تا ۱۳۹][old-inbound]،
[API کلید و تارگت، خطوط ۲۷۵۷ تا ۲۷۷۹][old-server]،
[probe واقعی TLS، خطوط ۱۳۹ تا ۱۷۵][old-reality-scan].

## ۲. قرارداد API قدیمی و چرخهٔ ذخیره

| API | بدنه یا نتیجهٔ مرتبط | اثر |
| --- | --- | --- |
| `POST /api/inbounds` | مشخصات، settings، streamSettings، sniffing، panelMeta | اینباند منطقی را ذخیره می‌کند؛ خود درخواست مشتری یا لینک جدید نمی‌سازد. |
| `PUT /api/inbounds/{id}` | همان مدل اینباند | تنظیمات را ذخیره و چرخهٔ apply را پیگیری می‌کند؛ تغییر مقصد Swap فعال محدود است. |
| `GET /api/inbounds/{id}/deployments` | local، nodeIds، targets، tunnelPorts، tunnelRoutes، swapRoutes | وضعیت تخصیص و تطابق Host؛ نه تست شبکهٔ سرتاسری. |
| `PUT /api/inbounds/{id}/deployments` | `local`, `nodeIds`, `tunnelPorts` | تخصیص محلی/نود، listener تونلی و desired revision را ثبت می‌کند. |
| `GET/PUT /api/settings/hosts` | PUT با `{value:[...]}` | کل مجموعهٔ Hosts خوانده یا جایگزین می‌شود؛ API مستقل برای هر Host وجود ندارد. |
| `GET /api/swap` | nodes، inbounds، routes، legacyRoutes | منبع فهرست واسط و مقصد، همان NodeRegistry است. |
| `POST /api/swap` / `PUT /api/swap/{id}` | مدل هشت‌فیلدی مسیر در بخش بعد | ایجاد/ویرایش مسیرِ خاموش؛ تغییر topology مسیر فعال مجاز نیست. |
| `POST /api/swap/{id}/state` | `{enabled:boolean}` | اعمال فوری با ترتیب ACK مقصد و واسط؛ مستقل از Save Inbound. |
| `PATCH /api/swap/{id}/name` | `{name}` | تغییر نام بدون restart مسیر فعال. |
| `DELETE /api/swap/{id}` | بدون بدنه | نیازمند خاموشی تأییدشده و ACK پاک‌سازی مقصد. |
| `POST /api/swap/{id}/diagnostics` | نتیجهٔ probe | آمادگی listener واسط و اینترنت مقصد جداگانه؛ `endToEndVerified=false`. |

جریان ذخیرهٔ فرم قدیمی **اتمیک نیست**:

1. `buildBody` اجرا و اینباند POST/PUT می‌شود.
2. پورت‌های Tunnel فرم بررسی و درخواست deployment فرستاده می‌شود.
3. Core validate اجرا می‌شود؛ خطای مرحلهٔ آخر با پیام «ذخیره شد، ولی…» گزارش می‌شود.

بنابراین خطای دیرهنگام می‌تواند اینباند ذخیره‌شده و تنظیمات استقرار ناقص
بگذارد. همچنین Core، پورت Direct را به‌صورت محافظه‌کارانه بین همهٔ اینباندها
یکتا می‌خواهد؛ اختصاص نودهای متفاوت این محدودیت را حذف نمی‌کند.

منابع: [saveEditor، خط ۱۲۱][old-inbound]،
[handlerها، خطوط ۱۵۹۷ تا ۱۶۵۸ و ۱۸۰۶ تا ۱۸۸۳ و ۲۶۶۵ تا ۲۷۳۱][old-server]،
[بررسی پورت، خطوط ۶۶۷ تا ۷۳۳][old-core].

## ۳. استقرار چند نود و Tunnel

`local` پیش‌فرض true است. `nodeIds` تا ۲۵۶ نود و `tunnelPorts` تا ۲۵۷
جفت runtime/port دارد. runtime برابر `local` یا `node:<id>` است. پورت
Tunnel باید متعلق به همان مقصد انتخاب‌شده، بین ۱ و ۶۵۵۳۵، متفاوت از Direct
و آزاد از پورت‌های مدیریت و listenerهای متعارض باشد.

در Core، Tunnel کپی واقعی listener اصلی با همان اعتبارهای مشتری، پروتکل،
امنیت و تنظیمات است؛ Tag و پورتش جداست. قوانین روتینگ مرتبط به shadow نیز
گسترش می‌یابند. روی نود، پورت `node:<id>` به کلید `local` همان runtime
تبدیل می‌شود. کلید و اعتبارهای یک اینباند منطقی با مشتری‌های وابسته به مقصد
می‌رسند. TLS به فایل‌های managed نیاز دارد؛ نوشتن مسیر یک فایل روی HUB به
معنی وجود آن روی Node نیست.

وقتی نود آفلاین است، desired revision ذخیره می‌شود و اعمال بعدی منتظر
ارتباط می‌ماند. نتیجهٔ «ذخیره شد» معادل ACK، listener آماده یا انتشار لینک
نیست. خود status مربوط به Tunnel نیز محدودتر است:

| وضعیت | شرط دقیق |
| --- | --- |
| off | Tunnel Port تعیین نشده است. |
| waiting_host | پورت تعیین شده، ولی Host تونلی فعال برای همان اینباند و runtime نیست. |
| port_mismatch | Host تونلی هست، ولی پورتش با listener یکی نیست. |
| active | حداقل یک Host فعال با همان اینباند، runtime و پورت پیدا شده است. |

`active` در این API اثبات آماده‌بودن Backhaul، CDN، فایروال یا مسیر ایران نیست.

منابع: [deployment، خطوط ۱۸۰۶ تا ۱۸۸۳][old-server]،
[bundle و فایل TLS، خطوط ۱۰۷۶ تا ۱۱۲۹][old-server]،
[listener اصلی و shadow، خطوط ۹۸۲ تا ۱۰۳۵][old-core]،
[اعتبارسنجی Node runtime، خطوط ۳۶۲ تا ۴۳۲][old-node-runtime].

## ۴. مدل دقیق Dark Swap؛ سه نقطهٔ متفاوت

| نقطه | فیلدهای قدیمی | معنی |
| --- | --- | --- |
| ورودی عمومی ایران | `entryAddress`, `entryPort` | مقصدی که در لینک مشتری می‌آید؛ دستی و مستقل از پورت واسط. |
| نود واسط | `sourceNodeId`, `sourcePort` | listener جدید و اختصاصی TCP روی واسط؛ پورت Tunnel عادی آن نود نیست. |
| نود مقصد | `exitNodeId`, `inboundId`, `exitPort` | همان اینباند منطقی روی نود مقصد؛ auth و حسابداری اینجا انجام می‌شوند. |
| نام | `name` | label دقیق مسیر؛ پیش‌فرض `DARK SWAP`، حداکثر ۱۶۰ نویسه. |

پورت‌های پیش‌فرض فرم، source و entry برابر ۶۸۰۱ و exit برابر ۶۸۰۲ هستند؛
برابری دو پورت نخست اجباری نیست. **اگر مقصد Tunnel Port معمولی دارد، exitPort
باید دقیقاً همان باشد.** اگر ندارد، shadow جداگانه آماده می‌شود. پورت Tunnel
معمولی **واسط** نباید مصرف مجدد یا جایگزین شود. Direct مقصد و مسیرهای قبلی
واسط مستقل می‌مانند.

واسط و مقصد باید دو نود فعال متفاوت باشند. مقصد باید اینباند انتخاب‌شده را
داشته باشد و `data_address` معتبر بدهد؛ source به داشتن همان اینباند نیاز
ندارد. پروتکل مشتری مقصد باید VLESS، VMess، Trojan یا Shadowsocks و انتقال
آن TCP/RAW، gRPC، WS، HTTP Upgrade یا XHTTP باشد. mKCP/UDP پذیرفته نمی‌شود.
پورت‌ها، نودها و namespace خروجی رزرو می‌شوند تا listener دیگری با مسیر
تداخل نکند؛ هر جفت مقصد/اینباند یک Swap دارد.

ذخیره، مسیر را خاموش نگه می‌دارد. در desired payload مقصد، shadow آماده
می‌شود؛ فعال‌سازی ابتدا ACK مقصد و سپس ACK واسط را می‌خواهد. واسط یک
`dokodemo-door` اختصاصی با `clients:[]` دارد که TCP را به `data_address`
مقصد و exitPort می‌رساند. مشتری و quota جدید روی واسط ساخته نمی‌شود.
خرابی مقصد، خروج اینترنت واسط را جایگزین نمی‌کند.

لینک Swap فقط وقتی منتشر می‌شود که مسیر enabled و phase نیز enabled باشد،
هر دو نود سالم و آنلاین باشند، maintenance/error یا desired pending وجود
نداشته باشد و runtime مقصد واجد شرایط باشد. این شروط هنوز سلامت کامل تونل
ایران را ثابت نمی‌کنند. diagnostic خود API نیز این مرز را صریح برمی‌گرداند.

در فرم قدیمی، Configure روی اینباند بدون شناسه خطا می‌دهد. روی اینباند ذخیره‌شده،
پس از تأیید کنارگذاشتن ویرایش‌های ذخیره‌نشده، صفحهٔ Swap باز می‌شود. toggle
مسیر موجود همان لحظه اعمال می‌شود؛ ذخیرهٔ فرم Inbound آن را کنترل نمی‌کند.

منابع: [فرم و انتخاب نود، خطوط ۲۸ تا ۵۲][old-swap-ui]،
[اعتبارسنجی و چرخهٔ Swap، خطوط ۶۶ تا ۱۳۰ و ۱۵۷ تا ۲۶۸][old-swap-core]،
[API عیب‌یابی، خطوط ۱۶۴۵ تا ۱۶۵۷][old-server].

## ۵. Hosts و خروجی اشتراک

بدنهٔ واقعی هر Host شامل این فیلدهاست:

`inboundId`, `runtime`, `endpointType`, `address`, `addresses?`, `port`,
`remark`, `enable`, `security`, `sni`, `overrideSniFromAddress`,
`keepSniBlank`, `host`, `path`, `alpn`, `fingerprint`, `allowInsecure`,
`finalMask`, `mihomoIpVersion`, `excludeFromSubTypes`.

سه حالت راهنمای فرم Direct، Tunnel/CDN و Advanced هستند؛ مقدار ذخیره‌شدهٔ
endpointType فقط direct یا tunnel است. Advanced راهی برای override سمت
مشتری است، نه پروتکل جدید listener. SNI می‌تواند از اینباند ارث ببرد، دستی،
برابر address یا خالی باشد. Host/Path برای WS/HTTPUpgrade/XHTTP معنی دارند.
security فقط same/tls/none است؛ FinalMask رشتهٔ JSON شیء غیرخالی است.

| رفتار خروجی | نتیجهٔ واقعی |
| --- | --- |
| ۱ تا ۶۴ آدرس مشترک | آدرس‌ها یکتا و روی یک پورت ذخیره می‌شوند. Tunnel هر آدرس را به لینک جدا با شمارهٔ نام تبدیل می‌کند. |
| Direct چندآدرسی | Core عمداً یک Direct برای هر جفت اینباند/runtime نگه می‌دارد؛ نمایش ۶۴ آدرس به معنی خروجی ۶۴ لینک Direct نیست. |
| Tunnel روی Node | فرم، در نبود Direct همان runtime، یک Host مستقیم با data_address نود و پورت اصلی اینباند اضافه می‌کند. |
| Tunnel محلی | اگر Direct صریح واجد شرایط وجود نداشته باشد، Direct محلی ضمنی حفظ می‌شود. |
| تطابق پورت | Host تونلی بدون پورت متناظر یا با پورت اشتباه، warning تولید می‌کند؛ subscription به‌جای خروجی ناقص HTTP 422 می‌دهد. |
| حذف از فرمت | raw/json/clash قابل انتخاب‌اند؛ base64 از سیاست raw استفاده می‌کند. حذف همهٔ Hostها از یک فرمت، به‌طور عام مجوز ساخت fallback مخفی نیست. |
| نام محلی | از قالب عمومی `{remark} | {email}` با متغیرهای remark/email/protocol ساخته می‌شود. |
| نام remote | Direct/Tunnel/Swap نام تمیز Host یا مسیر را می‌گیرند؛ email مشتری به آن‌ها افزوده نمی‌شود. |
| نام Tunnel | اگر marker تانل ندارد، `🚇 TUNNEL` اضافه می‌شود؛ آدرس‌های متعدد شماره می‌گیرند. |
| نام Swap | دقیقاً نام سفارشی مسیر است. آزمون موجود، نبود پسوند اجباری انگلیسی را تأیید می‌کند؛ متن قدیمی مستندات که پسوند را وعده می‌دهد با کد فعلی یکسان نیست. |

VLESS، VMess، Trojan و Shadowsocks کلاسیک روی TCP/none، generator لینک
دارند. SOCKS/HTTP، listener حساب‌دار واقعی‌اند ولی generator subscription
ندارند. Shadowsocks 2022، یا Shadowsocks با انتقال/امنیت دیگر، warning
می‌دهد. presence یک کنترل تنظیماتی، تضمین پشتیبانی کامل URI و Clash نیست:
authority/multiMode، padding، camouflage و tuningهای mKCP همگی به لینک
همراه منتقل نمی‌شوند و converterهای قالب‌ها محدودیت متفاوت دارند.

Node failover تنها Direct را از نودهای تأییدشده تولید می‌کند؛ Tunnel یا Swap
را با تعویض address به Direct تبدیل نمی‌کند. این رفتار با Xray Fallbacks
و با منع خروج جایگزین در Swap تفاوت دارد.

منابع: [نرمال‌سازی و ذخیرهٔ Host، خطوط ۱۰ تا ۷۴ و ۱۲۷ تا ۱۴۵][old-host-ui]،
[اعتبارسنجی Host و لینک، خطوط ۵۸۳ تا ۶۴۵ و ۱۴۵۲ تا ۱۶۹۶][old-core]،
[Node failover، خطوط ۹۵۰ تا ۹۹۴][old-server]،
[آزمون چندآدرسی و یکتایی Direct][old-host-tests]،
[آزمون نام دقیق Swap، خطوط ۱۸۴ تا ۲۰۴][old-swap-tests].

## ۶. وضعیت PRO و امکان گسترش پروفایل

مبنای بررسی PRO، HEAD برابر `43832f1b4a12f09d756c047844777af32f2d12e8`
پیش از تغییر جاری ساخت یکپارچه است. هدف این بخش ثبت محدودیت‌های واقعی است؛
اضافه‌شدن فرم plan این محدودیت‌ها را خودکار برطرف نمی‌کند.

| لایهٔ PRO | وضعیت سورس | نتیجه برای افزودن قابلیت |
| --- | --- | --- |
| مدل پروفایل | [Profile و validProfile](../internal/xrayconfig/render.go): تنها transport، gRPC و REALITY با Users دارای UUID/email؛ protocol/security انتخابی ندارد. | افزایش فیلد به‌تنهایی کافی نیست؛ پیش‌فرض خواندن پروفایل‌های رمزگذاری‌شدهٔ موجود باید VLESS/REALITY بماند. |
| ساخت/ویرایش | [CreateInbound](../internal/business/inbounds.go) و [MutateInbound](../internal/business/inbound_lifecycle.go) پروفایل را با RenderAssigned بررسی می‌کنند. | مدل API، خواندن، ویرایش، replay دقیق و hashing باید هم‌زمان نرمال شوند؛ فقط اضافه‌کردن گزینه به select نادرست است. |
| دو renderer | [Render](../internal/xrayconfig/render.go) و [RenderAssigned](../internal/xrayconfig/assigned.go) VLESS+REALITY می‌سازند؛ TCP همیشه Vision دارد. | none/TLS و transportهای جدید، انتخاب flow صحیح و serialization مشخص می‌خواهند. |
| پذیرش metering | [UsageBootstrap](../internal/xrayconfig/usage_admission.go) فقط VLESS REALITY TCP/gRPC را می‌پذیرد. | نباید با حذف allowlist، قابلیت ظاهری را به runtime تحمیل کرد. |
| مجوز process | [process validation](../internal/managedxray/process_linux.go) فقط VLESS را قبول و socketهای TCP/TCP6 را بررسی می‌کند. | VMess/Trojan validator مستقل می‌خواهند؛ mKCP به readiness واقعی UDP نیاز دارد. |
| انتساب حسابداری | [metered route authority](../internal/business/metered_route_authority.go)، [usage](../internal/business/usage.go) و [observer](../internal/managedxray/usage_observer_linux.go) VLESS و id مبتنی بر UUID را انتظار دارند. | اتصال واقعی protocol جدید باید بدون double-charge یا گم‌شدن probe و بدون تغییر هویت ledger به حساب درست متصل شود. |
| هویت مشتری | [clients و schema](../internal/business/store.go) UUID یکتا نگه می‌دارند؛ [probeها](../internal/usageledger/probes.go) نیز UUID مستقل دارند. | VMess امکان استفاده از همان UUID را دارد، ولی renderer/admission/export جدا لازم است. Trojan مدل credential مبتنی بر password و اتصال آن به client/probe لازم دارد. |
| تسویهٔ نهایی | [Final TCP](../internal/managedxray/final_tcp_linux.go) فقط VLESS TCP REALITY را پذیرفته است. | اضافه‌شدن core config مساوی اثبات drain و ثبت آخرین بایت نیست. |
| اثبات پروتکل | [protocol health client](../internal/managedxray/protocol_health_linux.go) Direct، TCP، REALITY، Vision و credential probe را بررسی می‌کند. | probe مشتری باید واقعاً با protocol/transport/security جدید اجرا شود؛ ACK یا بازبودن پورت جای آن را نمی‌گیرد. |
| serializer اشتراک | [codec](../internal/subscriptioncodec/codec.go) VLESS REALITY TCP/gRPC، raw و base64 دارد. | WS/HTTPUpgrade/XHTTP/none/TLS و قالب‌های VMess/Trojan باید صریح و قابل‌آزمون اضافه شوند. |
| تحویل اشتراک | [LAB adapter](../internal/xrayconfig/subscription_lab.go) و proofهای LAB از serializer محدودتر و فقط Direct TCP REALITY Vision هستند. | پشتیبانی gRPC در codec به معنی قابل‌انتشاربودن آن در جریان LAB فعلی نیست. |
| فایل TLS | renderer فعلی مدل certificate/keyfile یا بستهٔ managed TLS اینباند ندارد. | چرخهٔ هویت، ذخیره، انتقال، permission و تعویض گواهی باید تعریف شود؛ مسیر محلی HUB کافی نیست. |
| دستور زبان Host | [canonicalRouteHost](../internal/business/native_route_drafts.go) دامنه و IPv4 را می‌پذیرد و `:` را رد می‌کند. | Hosts و ورودی عمومی Swap در این مرحله IPv6 ندارند؛ پشتیبانی از IPv6 در bind نود یا تارگت REALITY این محدودیت را رفع نمی‌کند. |

### نتیجهٔ امکان‌سنجی

- **VLESS با انتقال‌های TCPمحور** از نظر UUID و email حسابداری امکان استفاده
  از همان binding را دارد؛ اما transport، امنیت، flow، admission، probe،
  تسویه و خروجی باید به‌عنوان یک قابلیت کامل توسعه یابند. WS/HTTPUpgrade با
  REALITY ترکیب مجاز legacy نبودند و نباید کورکورانه مجاز شوند.
- **TLS** علاوه بر config به قرارداد واقعی فایل/گواهی روی هر نصب نیاز دارد.
  **none** نیز نیازمند حذف درست Vision/پارامترهای REALITY از client و server است.
- **VMess** در مدل هویت به UUID نزدیک است، ولی hardcodeهای VLESS و export
  را نمی‌توان نادیده گرفت. **Trojan** علاوه بر آن به credential صریح password
  و تطبیق محفوظ آن با حساب مشتری و هویت probe احتیاج دارد.
- **mKCP** توسعهٔ کوچک همین مسیر TCP نیست: readiness UDP، اثبات انتقال،
  حسابداری و تسویه، و منع استفاده در Swap TCP باید بررسی شوند.

نگهداری یک پیش‌نویس پروفایل با schema توسعه‌یافته شدنی است؛ معرفی آن به‌عنوان
اتصال عملیاتی بدون تکمیل مسیر بالا درست نیست. در تغییر ساخت یکپارچه، هیچ‌یک
از این گیت‌ها برای بازشدن گزینهٔ ظاهری ضعیف نمی‌شوند.

## ۷. معیار پذیرش ساخت یکپارچهٔ PRO

این معیارها برای تغییر جاری‌اند و به‌تنهایی ادعای انجام‌شدن یا استقرار نیستند:

1. فرم، پروفایل و همهٔ ردیف‌های نود را در یک plan نگه دارد. هر ردیف Direct،
   Tunnel و Swap مستقل داشته باشد؛ نود واسط لازم نیست مقصد عادی اینباند باشد.
2. Swap فیلدهای مستقل sourceNode/relayPort، مقصد/exitPort و
   entryAddress/entryPort داشته باشد. پورت Tunnel عادی source حفظ شود.
   در PRO، بر خلاف اجبار قدیمی به reuse، مقصد می‌تواند listener اختصاصی
   Swap در کنار Tunnel عادی داشته باشد؛ یا با انتخاب همان پورت، یک receiver
   مشترک داشته باشد. در حالت مشترک، تفکیک مصرف مسیرها نباید ادعا شود؛ در حالت
   اختصاصی، compiler باید هر دو listener را بدون تغییر Tunnel قبلی حفظ کند.
3. تمام plan پیش از ثبت، در backend بررسی شود: نصب فعلی نود، تکرار ردیف،
   پورت رزروشده/متعارض، چرخهٔ Swap، Host، revision و وابستگی مقصدها.
4. پروفایل خاموش و plan شامل همهٔ مقصدها، Hostها و تنظیمات مسیر در یک
   تراکنش ثبت شوند. خطای ردیف آخر نباید اینباند یا ردیف قبلی را باقی بگذارد.
   شناسهٔ تلاش برای retry نامطمئن ثابت و replay وابسته به همان payload کامل
   باشد. جدول plan با جدول target عملیاتی یکی نیست؛ ثبت plan نباید به‌عنوان
   materialize یا اعمال target در runtime گزارش شود.
5. نتیجهٔ API فقط واقعیت ثبت‌شده را گزارش کند. ثبت plan، حالت configured
   یا saved draft است؛ اجرای runtime، سلامت و انتشار به قراردادهای مستقل
   موجود وابسته می‌مانند.
6. UI هیچ source tunnel یا entryAddress خودکار از listenIP نسازد. اگر backend
   مرجع ثبت‌شدهٔ تونل ندارد، ورود دستی روشن و پیوند مستقل نود نشان داده شود.
7. تست backend باید rollback کامل، replay بدون duplicate، conflict تلاش
   متفاوت، port collision، source متفاوت و reuse مقصد را بسنجد. تست مرورگر
   باید حفظ فرم پس از خطا، بدون POST در ورودی نامعتبر، payload کامل، انتخاب
   واسط مستقل، retry همان هویت و کاربردپذیری فارسی در ۳۲۰px را بررسی کند.

این دامنه، مشکل چندمرحله‌ای و ناقص‌ماندن ساخت را حل می‌کند. برابری کامل
پروتکل‌ها، امنیت‌ها، خروجی‌ها و اجرای مسیرهای جدید، معیارهای مستقل بخش شش
را نیز لازم دارد.

## ۸. قرارداد پیاده‌سازی جاری و موارد باقی‌مانده

تغییر جاری، سه تب «پروفایل»، «نودها و مسیرها» و «بازبینی» را در یک فرم
نگه می‌دارد. هر مقصد، IP شنود، Direct و Tunnel مستقل با Hostهای خودش، و
Swap اختیاری دارد. فهرست نود مبدأ شامل نودهای متصل خارج از مقصدهای انتخاب‌شده
هم هست. هر plan حداکثر ۶۴ مقصد دارد؛ سقف قدیمی ۲۵۶ نود بود. ورودی عمومی
از IP شنود حدس زده نمی‌شود. فرم پورت بیرونی ۱ تا ۶۵۵۳۵
و پورت listener از ۱۰۲۴ تا ۶۵۵۳۵ را می‌پذیرد؛ ۲۰۸۷، ۸۴۴۳ و ۲۴۴۴۳ رزرواند.

| عملیات PRO | قرارداد و اثر |
| --- | --- |
| `GET /api/v1/workspace/native-inbound-plans/capabilities` | نسخه، قابلیت‌های واقعی VLESS/REALITY/TCP/gRPC و فهرست planهای ذخیره‌شده؛ `runtime_apply:false`. |
| `POST /api/v1/workspace/native-inbound-plans/preview` | بررسی کل پروفایل، نودها، IPها، پورت‌ها، Hostها، چرخه‌ها، revision و ساخت topology آفلاین؛ هیچ ثبت یا اعمالی انجام نمی‌دهد. |
| `POST /api/v1/workspace/native-inbound-plans` | پروفایل جدید غیرفعال، plan کامل و رسید immutable در یک تراکنش رمزگذاری‌شده ذخیره می‌شوند. |
| `GET /api/v1/workspace/native-inbound-plans/{id}` | envelope شامل `plan` و `references_current`؛ تغییر نصب نود، پیش‌نویس را پاک نمی‌کند. |
| `PUT /api/v1/workspace/native-inbound-plans/{id}` | CAS روی revision پروفایل و plan؛ کلید خصوصی و هویت اینباند حفظ می‌شوند. تغییر صرف مسیر، revision پروفایل را بالا نمی‌برد. |
| retry همان `event_id` | همان payload نرمال‌شده، رسید ذخیره‌شده را برمی‌گرداند؛ تغییر payload با همان شناسه تعارض است. حتی پس از تعویض نصب نود، بازیابی رسید قبلی مجاز است و عملیات تازه‌ای اجرا نمی‌شود. |

آدرس‌های Direct و Tunnel در هر فهرست باید یکتا باشند و اجتماع آن‌ها حداکثر
۶۴ Host یکتا دارد؛ استفاده از همان Host روی دو پورت جدا مجاز است. اگر یک
مسیر فعال در plan، Host نداشته باشد یا ورودی Swap خالی باشد، وضعیت
`WAITING_HOST` می‌گیرد؛ در غیر این صورت `SAVED_DISABLED`. هر دو وضعیت
غیرفعال‌اند. یک ورودی عمومی یکسان با پورت یکسان نمی‌تواند به دو رلهٔ متفاوت
نگاشت شود. رزرو listener نودهای مبدأِ صرفاً واسط نیز در تعارض پورت لحاظ می‌شود.

preview یک topology واقعی از listenerهای مشتری و رلهٔ TCP می‌سازد، اما
config خام یا کلید خصوصی را به مرورگر نمی‌فرستد. کلید اعتبارسنجی ساخت جدید
فقط برای preview است و با کلید تازهٔ زمان ذخیره یکی نیست. رله، credential
مشتری ندارد و برای انتساب مصرف، `transit_not_billable` است. reuse پورت مقصد
با `shared_tunnel_swap` مشخص می‌شود تا تفکیک دو مسیر ادعا نشود. این metadata
به معنی تکمیل اتصال به موتور حسابداری یا فعال‌سازی نیست.

منابع پیاده‌سازی جاری: [فرم](../web/src/InboundCreation.tsx)،
[نودها و مسیرها](../web/src/InboundCreationNodes.tsx)،
[مدل فرم](../web/src/InboundCreationModel.ts)،
[API](../internal/api/native_inbound_plans.go)،
[تراکنش و اعتبارسنجی](../internal/business/native_inbound_plans.go)،
[compiler آفلاین](../internal/xrayconfig/creation_topology.go).

### برابری‌هایی که هنوز کامل نشده‌اند

| قابلیت legacy | وضعیت پس از این تغییر |
| --- | --- |
| ساخت یکپارچهٔ پروفایل و چند مقصد | ذخیره و بازکردن مجدد plan کامل فراهم است؛ ثبت target عملیاتی و فعال‌سازی جداگانه هنوز مسیر دیگری می‌خواهد. |
| Direct، Tunnel و Swap واقعی روی نود | توپولوژی و config آفلاین قابل‌ساخت است؛ اتصال این plan به desired runtime، ترتیب ACK، readiness، probe مشتری، settlement و انتشار اشتراک هنوز انجام نشده است. |
| پروتکل‌ها، transportها و امنیت‌های گستردهٔ legacy | همان محدودهٔ VLESS/REALITY با TCP یا gRPC باقی است؛ گزینه‌های دیگر هنوز اضافه نشده‌اند. |
| REALITY پیشرفته، Sniffing، Fallback، Proxy Protocol و JSON | مدل و فرم این تنظیمات هنوز هم‌سطح legacy نیستند. |
| تنظیمات Host پیشرفته و نام دقیق مسیر | override امنیت/SNI/ALPN/fingerprint/path، exclusion از فرمت‌ها، FinalMask، Mihomo IP version و نام مستقل هر Swap هنوز مدل کامل ندارند. |
| کلید و پروب REALITY | کلید تازه در ساخت و حفظ کلید در ویرایش انجام می‌شود؛ Ping/Find تارگت و کنترل پیشرفتهٔ کلید legacy در این فرم نیست. |
| چند runtime محلی و نودی | فقط نودهای متصل در plan هستند؛ local HUB به‌عنوان مقصد مستقل legacy اضافه نشده است. |
| چرخهٔ کامل Swap | مرحلهٔ ذخیرهٔ غیرفعال مدل شده؛ toggle عملیاتی، rename بدون restart، diagnostics و حذف با ACK پاک‌سازی در این جریان هنوز کامل نیست. |
| WARP و ضدتبلیغ | عمداً خارج از محصول؛ جزو کمبودهایی که باید اضافه شوند نیستند. |

این جدول باید مبنای ادعای قابلیت باشد: مرتب‌شدن فرم و وجود plan پایدار،
معادل آماده‌بودن مسیر برای ترافیک مشتری یا برابری کامل با DARK HUB نیست.

### آزمون مرورگر تغییر جاری

مجموعهٔ مستقل [inbound-creation-plan.spec.ts](../web/tests/inbound-creation-plan.spec.ts)
روی خروجی build با APIهای شبیه‌سازی‌شده اجرا شد. اجرای کامل رابط شامل ۲۴۵
سناریو موفق بود. پس از افزودن آرشیو پیکربندی، اجرای هدفمند هر ۳۹ سناریوی
دو فایل ساخت یکپارچه و مدیریت اینباند نیز موفق بود. مجموعهٔ ساخت اکنون این
۱۲ سناریو را دارد:

1. ثبت یک plan کامل با چند مقصد، هر سه مسیر مستقل و نود صرفاً واسط.
2. reuse اختیاری Tunnel مقصد، با حفظ Tunnel عادی و پورت مستقل رلهٔ مبدأ.
3. رفتن به تب و فیلد نامعتبر، بدون فرستادن POST ناقص؛ اگر هیچ مقصدی انتخاب نشده باشد، focus روی checkbox قابل‌دیدنِ انتخاب نود قرار می‌گیرد.
4. نتیجهٔ نامطمئن ذخیره و retry دستی با همان بدنه، هویت و event.
5. رد preview که نگاشت مسیرش با درخواست فرق دارد، حتی با تعداد و شناسه‌های صحیح.
6. ثبت `WAITING_HOST` بدون ساختن آدرس عمومی از IP نود.
7. بازکردن envelope ذخیره‌شده، ویرایش CAS مسیر با حفظ کلید و هویت، و حالت فقط‌خواندنی.
8. ناوبری صفحه‌کلید RTL، باقی‌ماندن footer و نبود سرریز افقی در عرض ۳۲۰ پیکسل فارسی.
9. پذیرش IPv6 نگاشت‌شده به IPv4 و تطبیق آن با مقدار canonical برگشتی سرور، هم در preview و هم در رسید ذخیره.
10. هدایت خطای تعارض پورت سرور به فیلد همان نود، بدون پیشنهاد بارگذاری نسخه‌ای که هنوز ذخیره نشده است.
11. بازشدن مستقیم جزئیات یک plan ذخیره‌شده با پاسخ دیرهنگام قابلیت‌ها: ویرایش پروفایل، فعال‌سازی، آرشیو، افزودن target و اتصال مشتری تا مشخص‌شدن مالکیت plan مسدود می‌مانند؛ سپس کنترل‌های قدیمی با بازکردن تنظیمات ذخیره‌شده جایگزین می‌شوند.
12. آرشیو صریح پیکربندی غیرفعال با دلیل و تأیید مالک؛ تنها عملیات archive ارسال می‌شود و دکمهٔ فعال‌سازی ناقص یا ویرایش جداگانهٔ پروفایل نمایش داده نمی‌شود.

مرز ذخیرهٔ غیرفعال در Store و triggerهای پایدار SQLite نیز اعمال می‌شود:
مسیرهای عمومیِ enable/edit، افزودن target و اتصال کلاینت نمی‌توانند plan
را خارج از ویرایشگر کامل تغییر یا عملیاتی کنند. triggerهای enable، target
و binding پس از بازگشت به باینری قدیمی هم باقی می‌مانند. آرشیو منطقی مجاز
است و رزرو پورت و ورودی بیرونی plan را آزاد می‌کند؛ رسیدها و تاریخچه حذف
نمی‌شوند. تست‌های API، Store و شکل SQL باینری قدیمی این مرز را بررسی می‌کنند.

این آزمون‌ها دادهٔ عملیاتی یا سرور واقعی را استفاده نمی‌کنند؛ اثبات رفتار
فرم‌اند، نه تست اتصال مشتری یا سلامت تونل بیرونی. آزمون‌های تراکنش و compiler
به‌ترتیب در [business](../internal/business/native_inbound_plans_test.go)،
[API](../internal/api/native_inbound_plans_test.go) و
[xrayconfig](../internal/xrayconfig/creation_topology_test.go) نگهداری می‌شوند.

آزمون مستقل `TestRealBrowserInboundCreationPlan` نیز با رابط buildشده، ورود
واقعی مالک، API واقعی و SQLite موقت موفق شد: دو نود مقصد با Direct و Tunnel
مستقل و یک نود صرفاً واسط برای Dark Swap ساخته، ذخیره و پس از reload باز شدند.
ویرایش TCP به gRPC، Short IDها، Host و پورت ورودی عمومی دوباره preview، ذخیره
و پس از reload کنترل شد. هویت و کلید REALITY ثابت ماندند و revisionها دقیقاً
به ۲ رسیدند. snapshot مستقل Go نشان داد targetها، مشتریان، حساب‌ها، اعتبار،
bindingها و بایت‌ها، hash و generation پیکربندی قبلی تغییر نکرده‌اند.
این آزمون هیچ پاسخ API شبیه‌سازی‌شده، نوشتن مستقیم از driver، اجرای Xray یا
فعال‌سازی نود ندارد. کد در [harness](../internal/api/inbound_creation_browser_linux_test.go)
و [driver](../web/tests/live-inbound-creation.mjs) و اجرای اجباریِ بدون skip
در [CI](../.github/workflows/native-usage.yml) نگهداری می‌شود.

## منابع ثابت

[old-inbound]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/web/inbounds-v3.js
[old-models]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/web/inbounds-models.js
[old-swap-ui]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/web/dark-swap.js
[old-host-ui]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/web/hosts-v2.js
[old-core]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/backend/core.py
[old-server]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/backend/server.py
[old-swap-core]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/backend/node_port_swap.py
[old-node-runtime]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/backend/node_runtime.py
[old-reality-scan]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/backend/reality_scan.py
[old-host-tests]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/tests/test_hosts_v3.py
[old-swap-tests]: https://github.com/darktunnelmika/dark-xray/blob/ed66ded4ec09c9d023635f64d9b05eb592ce06a1/tests/test_node_port_swap.py
