import { ArrowRight, ArrowRightLeft, ChevronDown, Globe2, Link2, Plus, Server } from "lucide-react";
import type { InboundNode, InboundTarget } from "./inboundTypes";
import { creationNodeName, creationPathHosts, creationPort, creationWaitingHosts, newCreationNode, type CreationIssue, type CreationNode, type CreationState } from "./InboundCreationModel";
import InboundCreationEndpoints from "./InboundCreationEndpoints";

type Language = "en" | "fa";
const messages: Record<Language, Record<string, string>> = {
  en: {
    security_invalid: "Choose a supported security mode.", security_immutable: "Security cannot change on a saved setup. Reload it or create a new setup.",
    profile_tls_unavailable: "This server cannot save TLS setup plans.", profile_websocket_unavailable: "This server cannot save WebSocket settings.", profile_http_upgrade_unavailable: "This server cannot save HTTPUpgrade settings.",
    tls_certificate_required: "Enter the TLS certificate chain.", tls_private_key_required: "Enter the matching TLS private key.",
    tls_certificate_invalid: "Use a valid PEM certificate chain, with the leaf certificate first, up to 64 KiB.", tls_certificate_chain_invalid: "Review the certificate chain order and signatures; use at most 8 certificates.",
    tls_certificate_usage_invalid: "Use a server certificate and key type supported for TLS connections.", tls_certificate_names_invalid: "Use a certificate with 1–1024 valid DNS names or DNS wildcards.", tls_private_key_invalid: "Use an unencrypted PEM private key up to 16 KiB.",
    tls_private_key_mismatch: "The private key does not match the leaf certificate.", tls_certificate_not_yet_valid: "This certificate is not valid yet. Supply a currently valid certificate.", tls_certificate_expired: "This certificate has expired. Replace the certificate and key before saving.",
    tls_server_name_uncovered: "Every server name must be covered by the certificate’s DNS names.", tls_reality_fields_invalid: "TLS does not use a REALITY target or Short IDs.", tls_reality_options_invalid: "REALITY settings cannot be saved with TLS.", tls_fallbacks_unsupported: "TLS fallbacks are not supported in this editor.", tls_spider_x_invalid: "SpiderX applies to REALITY and cannot be saved with TLS.",
    websocket_path_invalid: "Use an ASCII WebSocket /path up to 2048 bytes, without spaces, // prefix, %, ?, # or backslashes.", websocket_host_invalid: "Use a bare WebSocket DNS name or IPv4 without a port.",
    http_upgrade_path_invalid: "Use an ASCII HTTPUpgrade /path up to 2048 bytes, without spaces, // prefix, %, ?, # or backslashes.", http_upgrade_host_invalid: "Use a bare HTTPUpgrade DNS name or IPv4 without a port.",
    profile_websocket_required: "Reload the saved setup to preserve its WebSocket settings.", profile_http_upgrade_required: "Reload the saved setup to preserve its HTTPUpgrade settings.",
    name_required: "Enter an Inbound name.", grpc_service_invalid: "Use 1–64 letters, numbers, underscores or hyphens for the gRPC service.",
    name_invalid: "Use an Inbound name of up to 128 characters, without control characters.", transport_invalid: "Choose a supported transport.", server_names_invalid: "Use 1–16 valid DNS server names, without IP addresses, schemes or paths.",
    profile_xhttp_unavailable: "This server cannot save XHTTP in setup plans. Existing settings are retained.", profile_client_options_unavailable: "This server cannot save client connection defaults in setup plans. Existing settings are retained.",
    profile_xhttp_required: "Reload the saved setup to retain its XHTTP settings before editing.", profile_client_options_required: "Reload the saved setup to retain its client connection defaults before editing.",
    xhttp_path_invalid: "Use an optional /path of up to 2048 bytes, without spaces, a query, a fragment or invalid percent escapes.", xhttp_host_invalid: "Use an optional DNS name or IPv4 for XHTTP Host, without a port or brackets.",
    xhttp_mode_invalid: "Choose auto, packet-up, stream-up or stream-one.", xhttp_padding_invalid: "Use padding N or N-M with whole numbers from 1 to 65536 and N no greater than M.",
    client_fingerprint_invalid: "Choose a supported client fingerprint or inherit the default.", client_spider_x_invalid: "Use /path with an optional valid query, without spaces, a fragment or crawler query keys p, c, t, i and r.",
    endpoint_options_unavailable: "This server cannot save endpoint settings. Existing rows are retained.", endpoint_options_required: "Reload the saved setup to retain endpoint settings before editing.",
    endpoint_limit: "Use at most 64 endpoints per path.", endpoint_port_invalid: "Use a public port from 1 to 65535, or leave it blank / 0 to inherit the listener port.",
    endpoint_label_invalid: "Use a display name of up to 128 characters without control characters.", endpoint_sni_invalid: "Select an SNI from this Inbound’s server names, or inherit the first name.",
    endpoint_duplicate: "This address, effective port and client settings duplicate another Host in this path.", endpoint_hosts_mismatch: "The endpoint addresses changed unexpectedly. Reload the saved setup.", disabled_path_has_endpoints: "Disabled paths cannot save endpoint rows; their draft values are retained until the path is enabled.",
    profile_options_required: "Reload the saved profile to retain its advanced settings before editing.",
    profile_options_invalid: "Review the advanced REALITY and gRPC settings; the server rejected this combination.",
    profile_options_unavailable: "This server cannot save advanced profile settings. Update the server before saving this profile.",
    profile_sniffing_unavailable: "This server cannot save Sniffing in setup plans. Existing values are retained.", profile_fallbacks_unavailable: "This server cannot save fallbacks in setup plans. Existing rows are retained.",
    profile_sniffing_required: "Reload the saved setup to retain its Sniffing settings before editing.", profile_fallbacks_required: "Reload the saved setup to retain its fallback settings before editing.",
    sniffing_invalid: "Review the Sniffing settings and exclusions; the server rejected this combination.", fallbacks_invalid: "Review the fallback destinations, matches and TCP transport; the server rejected this combination.",
    sniffing_protocol_invalid: "Choose each supported Sniffing protocol at most once.", sniffing_domain_invalid: "Use up to 256 domain rules, each at most 512 bytes, with no surrounding spaces or control characters. Supported prefixes are full:, domain:, keyword: and regexp:.",
    sniffing_ip_invalid: "Use up to 256 valid IP addresses or CIDRs, each at most 512 bytes, with no surrounding spaces.",
    fallback_limit: "Use at most 32 fallbacks.", fallback_transport: "Fallbacks require TCP. Change the transport or remove the retained fallback rows.",
    fallback_path_invalid: "A fallback path must start with /, use at most 55 bytes and contain no spaces, ? or #.", fallback_alpn_unsupported: "ALPN matching is unavailable for REALITY fallbacks. Clear the unsupported ALPN explicitly before saving.",
    fallback_dest_invalid: "Enter a port from 1 to 65535 or host:port. Use brackets for IPv6 and no leading zeros in the port.",
    fallback_xver_invalid: "Choose fallback PROXY protocol 0, 1 or 2.", fallback_duplicate: "Each fallback must have a unique Path, including the empty default Path.",
    grpc_authority_invalid: "Enter a valid gRPC authority host or IP with an optional port.",
    reality_xver_invalid: "Choose REALITY Xver 0, 1 or 2.", reality_max_time_diff_invalid: "Enter a whole number of milliseconds from 0 to 9223372036854.",
    reality_version_invalid: "Use a client version x.y.z with each component between 0 and 255, without leading zeros.", reality_version_order: "The maximum client version must be at least the minimum version.",
    target_invalid: "Enter a REALITY target with a valid port.", server_names_required: "Enter at least one server name.",
    short_ids_invalid: "Use 1–16 unique Short IDs, each 2–16 lowercase hexadecimal characters with an even length.",
    node_required: "Select at least one destination Node.", pairing_changed: "This Node’s installation changed. Remove and select it again.",
    listen_ip_required: "Enter this destination Node’s listening IP.", path_required: "Enable at least one connection path.",
    port_invalid: "Listener ports must be whole numbers from 1024 to 65535.", entry_port_invalid: "The external entry port must be from 1 to 65535.",
    host_invalid: "Use a DNS name, IPv4 or IPv6 address without a scheme, path or port. Wildcard, multicast and link-local IPs are not supported.", host_duplicate: "Remove duplicate addresses from this path.",
    host_limit: "Use at most 64 unique addresses for each destination Node.", source_required: "Choose a different, currently paired source Node.",
    port_reserved: "Ports 2087, 8443 and 24443 are reserved.", port_collision: "Direct and Tunnel need separate ports; Swap may reuse this Node’s Tunnel port.",
    source_port_collision: "The source relay port conflicts with another listener in this plan.", route_cycle: "These Swap links form a loop. Change the source Node.",
    entry_mapping_conflict: "This public entry address and port already map to a different source relay. Use a distinct entry mapping.",
    reason_required: "Enter a change note of up to 200 characters.",
    source_listen_ip_required: "Enter the IP assigned to the source relay Node.", source_listen_ip_conflict: "The source relay IP must match this Node’s listening IP in the plan.",
    listen_ip_invalid: "Use a valid, assigned listening IP; wildcard, multicast and link-local IPs are not supported.",
    listener_port_invalid_or_reserved: "Use a listener port from 1024 to 65535, excluding 2087, 8443 and 24443.", swap_port_invalid_or_reserved: "Check the relay and destination listener ports and the external entry port.",
    existing_listener_port_collision: "This port already belongs to another Inbound or saved setup on the Node.", listener_port_collision: "Two listeners in this setup use the same port on the Node.",
    node_binding_conflict: "Use the same installation and listening IP for every role of this Node.", existing_node_binding_conflict: "This Node already has a different assigned listening IP or installation.",
    host_limit_or_invalid: "Check the Host format and the limit of 64 unique addresses per Node.", profile_invalid: "Check the profile name, REALITY target, server names and Short IDs.",
    profile_changed_or_materialized: "The saved profile changed or was assigned outside this editor; reload it to review.", plan_revision_conflict: "A newer version of this setup was saved. Reload it before continuing.",
    node_count_invalid: "Select between 1 and 64 destination Nodes.", node_identity_invalid: "Re-select the affected Node to use its current installation.",
    swap_source_invalid: "Choose a currently paired source Node different from the destination.", topology_invalid: "The complete connection topology could not be compiled. Review the listening IPs and ports.",
    inbound_id_exists: "This Inbound identity already exists. Reload to check the saved setup.", inbound_limit: "The server’s Inbound limit has been reached.", reason_invalid: "Enter a change note of up to 200 characters.",
  },
  fa: {
    security_invalid: "نوع امنیت پشتیبانی‌شده را انتخاب کن.", security_immutable: "نوع امنیت تنظیمات ذخیره‌شده قابل تغییر نیست؛ دوباره بارگذاری کن یا تنظیمات تازه بساز.",
    profile_tls_unavailable: "این سرور طرح TLS را ذخیره نمی‌کند.", profile_websocket_unavailable: "این سرور تنظیمات WebSocket را ذخیره نمی‌کند.", profile_http_upgrade_unavailable: "این سرور تنظیمات HTTPUpgrade را ذخیره نمی‌کند.",
    tls_certificate_required: "زنجیرهٔ گواهی TLS را وارد کن.", tls_private_key_required: "کلید خصوصی متناظر TLS را وارد کن.",
    tls_certificate_invalid: "زنجیرهٔ گواهی PEM معتبر، حداکثر ۶۴ کیبی‌بایت و با گواهی سرور در ابتدا وارد کن.", tls_certificate_chain_invalid: "ترتیب و امضای زنجیرهٔ گواهی را بررسی کن؛ حداکثر ۸ گواهی مجاز است.",
    tls_certificate_usage_invalid: "از گواهی سرور و نوع کلید پشتیبانی‌شده برای اتصال TLS استفاده کن.", tls_certificate_names_invalid: "گواهی باید بین ۱ تا ۱۰۲۴ نام DNS یا Wildcard معتبر داشته باشد.", tls_private_key_invalid: "کلید خصوصی PEM بدون رمز و حداکثر ۱۶ کیبی‌بایت وارد کن.",
    tls_private_key_mismatch: "کلید خصوصی با گواهی سرور تطابق ندارد.", tls_certificate_not_yet_valid: "اعتبار این گواهی هنوز شروع نشده؛ گواهی معتبر فعلی وارد کن.", tls_certificate_expired: "این گواهی منقضی شده؛ پیش از ذخیره، گواهی و کلید را جایگزین کن.",
    tls_server_name_uncovered: "گواهی باید همهٔ نام‌های DNS سرور را پوشش دهد.", tls_reality_fields_invalid: "TLS به مقصد REALITY یا Short ID نیاز ندارد.", tls_reality_options_invalid: "تنظیمات REALITY همراه TLS ذخیره نمی‌شوند.", tls_fallbacks_unsupported: "Fallback در TLS در این ویرایشگر پشتیبانی نمی‌شود.", tls_spider_x_invalid: "SpiderX مخصوص REALITY است و همراه TLS ذخیره نمی‌شود.",
    websocket_path_invalid: "مسیر ASCII با شروع / و حداکثر ۲۰۴۸ بایت وارد کن؛ فاصله، پیشوند //، %، ?، # و بک‌اسلش مجاز نیست.", websocket_host_invalid: "نام DNS یا IPv4 خالص برای WebSocket وارد کن؛ بدون پورت.",
    http_upgrade_path_invalid: "مسیر ASCII با شروع / و حداکثر ۲۰۴۸ بایت وارد کن؛ فاصله، پیشوند //، %، ?، # و بک‌اسلش مجاز نیست.", http_upgrade_host_invalid: "نام DNS یا IPv4 خالص برای HTTPUpgrade وارد کن؛ بدون پورت.",
    profile_websocket_required: "برای حفظ تنظیمات WebSocket، طرح ذخیره‌شده را دوباره بارگذاری کن.", profile_http_upgrade_required: "برای حفظ تنظیمات HTTPUpgrade، طرح ذخیره‌شده را دوباره بارگذاری کن.",
    name_invalid: "نام اینباند حداکثر ۱۲۸ نویسه و بدون نویسهٔ کنترلی باشد.", transport_invalid: "یک انتقال پشتیبانی‌شده را انتخاب کن.", server_names_invalid: "بین ۱ تا ۱۶ نام DNS معتبر وارد کن؛ IP، پیشوند URL و مسیر مجاز نیست.",
    profile_xhttp_unavailable: "سرور XHTTP را در طرح ذخیره‌شده پشتیبانی نمی‌کند؛ مقادیر قبلی حفظ می‌شوند.", profile_client_options_unavailable: "سرور پیش‌فرض‌های اتصال کلاینت را ذخیره نمی‌کند؛ مقادیر قبلی حفظ می‌شوند.",
    profile_xhttp_required: "برای حفظ تنظیمات XHTTP، نسخهٔ ذخیره‌شده را دوباره بارگذاری کن.", profile_client_options_required: "برای حفظ پیش‌فرض‌های کلاینت، نسخهٔ ذخیره‌شده را دوباره بارگذاری کن.",
    xhttp_path_invalid: "مسیر اختیاری /path تا ۲۰۴۸ بایت، بدون فاصله، Query، Fragment یا کد درصد نامعتبر وارد کن.", xhttp_host_invalid: "Host در XHTTP باید نام DNS یا IPv4 بدون پورت و کروشه باشد؛ خالی هم مجاز است.",
    xhttp_mode_invalid: "یکی از auto، packet-up، stream-up یا stream-one را انتخاب کن.", xhttp_padding_invalid: "Padding با قالب N یا N-M و عدد صحیح بین ۱ تا ۶۵۵۳۶ باشد؛ N از M بیشتر نباشد.",
    client_fingerprint_invalid: "یک Fingerprint پشتیبانی‌شده یا مقدار ارث‌بری را انتخاب کن.", client_spider_x_invalid: "مسیر /path با Query معتبر اختیاری وارد کن؛ فاصله، Fragment و کلیدهای خزندهٔ p، c، t، i و r مجاز نیست.",
    endpoint_options_unavailable: "سرور تنظیمات نقطهٔ اتصال را ذخیره نمی‌کند؛ ردیف‌های قبلی حفظ می‌شوند.", endpoint_options_required: "برای حفظ تنظیمات Host، نسخهٔ ذخیره‌شده را دوباره بارگذاری کن.",
    endpoint_limit: "هر مسیر حداکثر ۶۴ نقطهٔ اتصال دارد.", endpoint_port_invalid: "پورت عمومی بین ۱ تا ۶۵۵۳۵ باشد؛ خالی یا ۰ یعنی ارث‌بری از پورت شنود.",
    endpoint_label_invalid: "نام نمایشی حداکثر ۱۲۸ نویسه و بدون نویسهٔ کنترلی باشد.", endpoint_sni_invalid: "SNI را از نام‌های سرور همین اینباند انتخاب کن یا از نام اول ارث ببر.",
    endpoint_duplicate: "این آدرس، پورت مؤثر و تنظیمات کلاینت با Host دیگری در همین مسیر تکراری است.", endpoint_hosts_mismatch: "آدرس‌های نقطهٔ اتصال به‌طور غیرمنتظره تغییر کرده؛ نسخهٔ ذخیره‌شده را دوباره بارگذاری کن.", disabled_path_has_endpoints: "مسیر خاموش ردیف Host ذخیره نمی‌کند؛ مقادیر پیش‌نویس تا فعال‌کردن مسیر حفظ می‌شوند.",
    name_required: "نام اینباند را وارد کن.", grpc_service_invalid: "برای سرویس gRPC از ۱ تا ۶۴ حرف انگلیسی، عدد، زیرخط یا خط تیره استفاده کن.",
    profile_options_required: "برای حفظ تنظیمات پیشرفته، پروفایل ذخیره‌شده را پیش از ویرایش دوباره بارگذاری کن.",
    profile_options_invalid: "تنظیمات پیشرفتهٔ REALITY و gRPC را بررسی کن؛ سرور این ترکیب را نپذیرفت.",
    profile_options_unavailable: "این نسخهٔ سرور تنظیمات پیشرفته را ذخیره نمی‌کند. پیش از ذخیرهٔ این پروفایل، سرور باید به‌روزرسانی شود.",
    profile_sniffing_unavailable: "این سرور Sniffing را در طرح ذخیره نمی‌کند؛ مقادیر قبلی حفظ می‌شوند.", profile_fallbacks_unavailable: "این سرور Fallback را در طرح ذخیره نمی‌کند؛ ردیف‌های قبلی حفظ می‌شوند.",
    profile_sniffing_required: "برای حفظ تنظیمات Sniffing، طرح ذخیره‌شده را پیش از ویرایش دوباره بارگذاری کن.", profile_fallbacks_required: "برای حفظ تنظیمات Fallback، طرح ذخیره‌شده را پیش از ویرایش دوباره بارگذاری کن.",
    sniffing_invalid: "تنظیمات و استثناهای Sniffing را بررسی کن؛ سرور این ترکیب را نپذیرفت.", fallbacks_invalid: "مقصدها، شرط‌های Fallback و انتقال TCP را بررسی کن؛ سرور این ترکیب را نپذیرفت.",
    sniffing_protocol_invalid: "هر پروتکل Sniffing را حداکثر یک‌بار انتخاب کن.", sniffing_domain_invalid: "حداکثر ۲۵۶ قانون دامنه با سقف ۵۱۲ بایت برای هر قانون و بدون فاصلهٔ ابتدا و انتها یا نویسهٔ کنترلی وارد کن. پیشوندهای مجاز: full:، domain:، keyword: و regexp:.",
    sniffing_ip_invalid: "حداکثر ۲۵۶ IP یا CIDR معتبر با سقف ۵۱۲ بایت و بدون فاصلهٔ ابتدا و انتها وارد کن.",
    fallback_limit: "حداکثر ۳۲ Fallback مجاز است.", fallback_transport: "Fallback به TCP نیاز دارد؛ انتقال را تغییر بده یا ردیف‌های حفظ‌شده را حذف کن.",
    fallback_path_invalid: "مسیر Fallback باید با / شروع شود، حداکثر ۵۵ بایت باشد و فاصله، ? یا # نداشته باشد.", fallback_alpn_unsupported: "تطبیق ALPN در Fallback با REALITY در دسترس نیست؛ پیش از ذخیره، مقدار پشتیبانی‌نشده را صریحاً پاک کن.",
    fallback_dest_invalid: "پورت ۱ تا ۶۵۵۳۵ یا host:port وارد کن؛ IPv6 باید داخل براکت و پورت بدون صفر اضافه در ابتدا باشد.",
    fallback_xver_invalid: "برای پروتکل PROXY یکی از مقادیر ۰، ۱ یا ۲ را انتخاب کن.", fallback_duplicate: "مسیر هر Fallback، از جمله مسیر خالی پیش‌فرض، باید یکتا باشد.",
    grpc_authority_invalid: "برای Authority یک هاست یا IP معتبر با پورت اختیاری وارد کن.",
    reality_xver_invalid: "برای Xver یکی از مقادیر ۰، ۱ یا ۲ را انتخاب کن.", reality_max_time_diff_invalid: "تعداد میلی‌ثانیه را با عدد صحیح بین ۰ و 9223372036854 وارد کن.",
    reality_version_invalid: "نسخه را با قالب x.y.z وارد کن؛ هر بخش بین ۰ تا ۲۵۵ و بدون صفر اضافه در ابتدا باشد.", reality_version_order: "حداکثر نسخهٔ کلاینت نباید از حداقل نسخه کوچک‌تر باشد.",
    target_invalid: "مقصد REALITY را همراه یک پورت معتبر وارد کن.", server_names_required: "حداقل یک نام سرور وارد کن.",
    short_ids_invalid: "۱ تا ۱۶ شناسهٔ یکتا وارد کن؛ هر Short ID باید ۲ تا ۱۶ رقم هگز کوچک با طول زوج داشته باشد.",
    node_required: "حداقل یک نود مقصد را انتخاب کن.", pairing_changed: "نصب این نود تغییر کرده؛ نود را حذف و دوباره انتخاب کن.",
    listen_ip_required: "IP شنود این نود مقصد را وارد کن.", path_required: "حداقل یک مسیر اتصال را فعال کن.",
    port_invalid: "پورت شنود باید عدد صحیحی از ۱۰۲۴ تا ۶۵۵۳۵ باشد.", entry_port_invalid: "پورت ورودی بیرونی باید از ۱ تا ۶۵۵۳۵ باشد.",
    host_invalid: "نام DNS، آدرس IPv4 یا IPv6 را بدون پروتکل، مسیر و پورت وارد کن؛ IP عمومی شنود، چندپخشی و لینک‌لوکال مجاز نیست.", host_duplicate: "آدرس‌های تکراری این مسیر را حذف کن.",
    host_limit: "برای هر نود مقصد حداکثر ۶۴ آدرس یکتا وارد کن.", source_required: "یک نود مبدأ متصل و متفاوت از مقصد انتخاب کن.",
    port_reserved: "پورت‌های ۲۰۸۷، ۸۴۴۳ و ۲۴۴۴۳ رزرو هستند.", port_collision: "پورت مستقیم و تونل باید جدا باشند؛ دارک‌سواپ می‌تواند پورت تونل همین نود را استفاده کند.",
    source_port_collision: "پورت رلهٔ مبدأ با یک شنوندهٔ دیگر در این تنظیمات تداخل دارد.", route_cycle: "اتصال‌های دارک‌سواپ حلقه ساخته‌اند؛ نود مبدأ را تغییر بده.",
    entry_mapping_conflict: "این آدرس و پورت ورودی بیرونی به رلهٔ مبدأ دیگری متصل است؛ یک نگاشت ورودی جدا انتخاب کن.",
    reason_required: "یادداشت تغییر را با حداکثر ۲۰۰ نویسه وارد کن.",
    source_listen_ip_required: "IP اختصاص‌یافته به نود رلهٔ مبدأ را وارد کن.", source_listen_ip_conflict: "IP رلهٔ مبدأ باید با IP شنود همین نود در تنظیمات یکسان باشد.",
    listen_ip_invalid: "یک IP شنود معتبر و اختصاص‌یافته وارد کن؛ آدرس عمومی شنود، چندپخشی و لینک‌لوکال پشتیبانی نمی‌شود.",
    listener_port_invalid_or_reserved: "پورت شنود باید از ۱۰۲۴ تا ۶۵۵۳۵ و غیر از ۲۰۸۷، ۸۴۴۳ و ۲۴۴۴۳ باشد.", swap_port_invalid_or_reserved: "پورت‌های شنود مبدأ و مقصد و پورت ورودی بیرونی را بررسی کن.",
    existing_listener_port_collision: "این پورت روی نود به اینباند یا تنظیمات ذخیره‌شدهٔ دیگری تعلق دارد.", listener_port_collision: "دو شنونده در این تنظیمات از یک پورت روی همین نود استفاده می‌کنند.",
    node_binding_conflict: "برای تمام نقش‌های این نود، نصب و IP شنود یکسان انتخاب کن.", existing_node_binding_conflict: "این نود از قبل IP شنود یا نصب متفاوتی دارد.",
    host_limit_or_invalid: "قالب آدرس‌ها و سقف ۶۴ هاست یکتا برای هر نود را بررسی کن.", profile_invalid: "نام پروفایل، مقصد REALITY، نام‌های سرور و شناسه‌های کوتاه را بررسی کن.",
    profile_changed_or_materialized: "پروفایل ذخیره‌شده خارج از این ویرایشگر تغییر کرده یا اختصاص یافته؛ نسخهٔ تازه را بارگذاری کن.", plan_revision_conflict: "نسخهٔ تازه‌تری از این تنظیمات ذخیره شده؛ پیش از ادامه دوباره بارگذاری کن.",
    node_count_invalid: "بین ۱ تا ۶۴ نود مقصد انتخاب کن.", node_identity_invalid: "برای استفاده از نصب فعلی، نود مربوط را دوباره انتخاب کن.",
    swap_source_invalid: "یک نود مبدأ متصل و متفاوت از مقصد انتخاب کن.", topology_invalid: "ساخت پیکربندی کامل اتصال ممکن نشد؛ IPهای شنود و پورت‌ها را بررسی کن.",
    inbound_id_exists: "این شناسهٔ اینباند از قبل وجود دارد؛ تنظیمات ذخیره‌شده را بارگذاری و بررسی کن.", inbound_limit: "تعداد اینباندها به سقف مجاز سرور رسیده است.", reason_invalid: "یادداشت تغییر را با حداکثر ۲۰۰ نویسه وارد کن.",
  },
};
export function creationIssueMessage(issue: CreationIssue, language: Language): string { return messages[language][issue.code] ?? (language === "fa" ? "تنظیمات این بخش را بررسی کن." : "Review this section’s settings."); }

const words = {
  en: {
    title: "Destination Nodes", help: "Select destination Nodes, then configure Direct, Tunnel and optional Dark Swap here. Direct and Tunnel work independently with separate ports.", none: "No paired Nodes are available.",
    selected: "selected", select: "Select destination", configure: "Configure", waiting: "Waiting for Host", configured: "Configured", noPath: "No path selected",
    direct: "Direct", tunnel: "Tunnel", swap: "Dark Swap", listen: "Destination listening IP", listenHint: "Use an IP assigned to this Node.",
    directPort: "Direct port", directHosts: "Direct Hosts", tunnelPort: "Tunnel port", tunnelHosts: "Tunnel Hosts", hostsHint: "One address per line or separated with commas. Up to 64 unique Hosts per Node.",
    emptyHost: "You can save this path without a Host. It will wait for an address.", paths: "Connection paths", remove: "Remove this Node and its changes from the setup?",
    source: "Source Node", choose: "Choose a source Node", relay: "Source relay port", entryHost: "External entry address", entryPort: "External entry port", destinationPort: "Destination exit port",
    sourceIP: "Source relay listening IP",
    swapHint: "The external entry reaches a dedicated relay on the source Node, then the destination port.",
    sourceHint: "Select the paired Node that receives the external tunnel connection.", entryHint: "Enter the address and port of your configured external tunnel.",
    useTunnel: "Use this Node’s Tunnel port", reuse: "The destination uses this Node’s existing Tunnel port.", shadow: "A separate destination listener is configured.",
    relayLabel: "Source relay", entryLabel: "External entry", destinationLabel: "Destination", notSet: "Not set", limit: "Select up to 64 destination Nodes.",
  },
  fa: {
    title: "نودهای مقصد", help: "نودهای مقصد را انتخاب کن و Direct، Tunnel و دارک‌سواپ اختیاری را همین‌جا تنظیم کن. Direct و Tunnel مستقل‌اند و پورت جدا دارند.", none: "نود متصلی در دسترس نیست.",
    selected: "انتخاب‌شده", select: "انتخاب مقصد", configure: "تنظیم", waiting: "در انتظار هاست", configured: "تنظیم‌شده", noPath: "مسیری انتخاب نشده",
    direct: "مستقیم", tunnel: "تونل", swap: "دارک‌سواپ", listen: "IP شنود نود مقصد", listenHint: "یک IP اختصاص‌یافته به همین نود وارد کن.",
    directPort: "پورت مستقیم", directHosts: "هاست‌های مستقیم", tunnelPort: "پورت تونل", tunnelHosts: "هاست‌های تونل", hostsHint: "هر آدرس در یک خط یا با کاما؛ حداکثر ۶۴ هاست یکتا برای هر نود.",
    emptyHost: "می‌توانی این مسیر را بدون هاست ذخیره کنی؛ تا افزودن آدرس در انتظار می‌ماند.", paths: "مسیرهای اتصال", remove: "این نود و تغییراتش از تنظیمات حذف شود؟",
    source: "نود مبدأ", choose: "انتخاب نود مبدأ", relay: "پورت رلهٔ مبدأ", entryHost: "آدرس ورودی بیرونی", entryPort: "پورت ورودی بیرونی", destinationPort: "پورت خروجی مقصد",
    sourceIP: "IP شنود رلهٔ مبدأ",
    swapHint: "ورودی بیرونی به رلهٔ اختصاصی نود مبدأ و سپس به پورت نود مقصد متصل می‌شود.",
    sourceHint: "نود متصلی را انتخاب کن که اتصال تونل بیرونی را دریافت می‌کند.", entryHint: "آدرس و پورت تونل بیرونیِ تنظیم‌شدهٔ خودت را وارد کن.",
    useTunnel: "استفاده از پورت تونل همین نود", reuse: "مقصد از پورت تونل همین نود استفاده می‌کند.", shadow: "یک شنوندهٔ جدا برای مقصد تنظیم می‌شود.",
    relayLabel: "رلهٔ مبدأ", entryLabel: "ورودی بیرونی", destinationLabel: "مقصد", notSet: "تنظیم نشده", limit: "حداکثر ۶۴ نود مقصد انتخاب کن.",
  },
};

export default function InboundCreationNodes({ language, state, nodes, targets = [], disabled, expanded, onExpanded, onChange, issues = [], endpointOptionsSupported = false, maxEndpointsPerPath = 64, listenerPortMin = 1024, listenerPortMax = 65535, reservedListenerPorts = [2087, 8443, 24443] }: {
  language: Language; state: CreationState; nodes: InboundNode[]; disabled: boolean; expanded: string;
  targets?: InboundTarget[];
  onExpanded: (id: string) => void; onChange: (value: CreationState) => void; issues?: CreationIssue[];
  endpointOptionsSupported?: boolean; maxEndpointsPerPath?: number; listenerPortMin?: number; listenerPortMax?: number; reservedListenerPorts?: number[];
}) {
  const t = words[language];
  const update = (id: string, value: CreationNode) => onChange({ ...state, nodes: { ...state.nodes, [id]: value } });
  function select(node: InboundNode, checked: boolean) {
    if (!checked && !window.confirm(t.remove)) return;
    const existing = state.nodes[node.node_id];
    onChange({ ...state, selected: checked ? [...state.selected, node.node_id] : state.selected.filter(id => id !== node.node_id),
      nodes: { ...state.nodes, [node.node_id]: existing?.installationID === node.installation_id ? existing : newCreationNode(node) } });
    if (checked) onExpanded(node.node_id);
    else if (expanded === node.node_id) onExpanded(state.selected.find(id => id !== node.node_id) ?? "");
  }
  const visibleNodes: InboundNode[] = [...nodes, ...state.selected.filter(id => !nodes.some(node => node.node_id === id)).map(id => ({ node_id: id, name: id, installation_id: state.nodes[id].installationID }))];
  return <section className="creation-nodes" aria-label={t.title}>
    <header className="creation-section-heading"><div><h3>{t.title}</h3><p>{t.help}</p></div><span className="creation-count"><bdi>{state.selected.length}</bdi> {t.selected}</span></header>
    {!nodes.length && <p className="creation-empty"><Server size={22}/>{t.none}</p>}
    {state.selected.length >= 64 && <p className="creation-help">{t.limit}</p>}
    <div className="creation-node-list">
      {visibleNodes.map(node => {
        const id = node.node_id, selected = state.selected.includes(id), row = state.nodes[id];
        const open = selected && expanded === id;
        const nodeIssues = issues.filter(issue => issue.nodeID === id);
        return <article key={id} className="creation-node" data-selected={selected} data-open={open} data-node-id={id}>
          <div className="creation-node-heading">
            <label className="creation-node-select"><input aria-label={`${t.select}: ${creationNodeName(visibleNodes, id)}`} data-creation-field="node" type="checkbox" checked={selected}
              disabled={disabled || !selected && state.selected.length >= 64} onChange={event => select(node, event.target.checked)}/><Server size={17}/><span>{creationNodeName(visibleNodes, id)}</span>{node.kind === "hub" && <span className="creation-node-kind">{language === "fa" ? "HUB · همین سرور" : "HUB · this server"}</span>}</label>
            {selected && row && <><div className="creation-node-paths" aria-label={t.paths}>
              {row.direct.enabled && <span title={t.direct}><Globe2 size={12}/><bdi>{row.direct.port || "—"}</bdi></span>}
              {row.tunnel.enabled && <span title={t.tunnel}><Link2 size={12}/><bdi>{row.tunnel.port || "—"}</bdi></span>}
              {row.swap.enabled && <span title={t.swap}><ArrowRightLeft size={12}/><bdi>{row.swap.destinationPort || "—"}</bdi></span>}
            </div><button type="button" className="creation-expand" disabled={disabled} aria-label={`${t.configure}: ${creationNodeName(visibleNodes, id)}`} aria-expanded={open} onClick={() => onExpanded(open ? "" : id)}><ChevronDown size={17}/></button></>}
          </div>
          {selected && row && <div className="creation-row-state"><span className={creationWaitingHosts(row) ? "creation-waiting" : ""}>{nodeIssues.length ? `${nodeIssues.length} ${language === "fa" ? "مورد برای بررسی" : "items to review"}` : creationWaitingHosts(row) ? t.waiting : row.direct.enabled || row.tunnel.enabled || row.swap.enabled ? t.configured : t.noPath}</span></div>}
          {open && row && <div className="creation-node-body">
            <label className="creation-field"><span>{t.listen}</span><input aria-label={`${t.listen}: ${id}`} data-creation-field="listenIP" dir="ltr" value={row.listenIP} disabled={disabled} onChange={event => update(id, { ...row, listenIP: event.target.value })} placeholder="203.0.113.10" autoComplete="off"/><small>{t.listenHint}</small></label>
            <p className="creation-help" data-listener-port-limits>{language === "fa" ? "بازهٔ پورت شنود" : "Listener port range"}: <bdi>{listenerPortMin}–{listenerPortMax}</bdi>{reservedListenerPorts.length > 0 && <> · {language === "fa" ? "رزروشده" : "Reserved"}: <bdi>{reservedListenerPorts.join(", ")}</bdi></>}</p>
            <div className="creation-path-toggles" role="group" aria-label={`${t.paths}: ${id}`}>
              {(["direct", "tunnel", "swap"] as const).map(path => { const Icon = path === "direct" ? Globe2 : path === "tunnel" ? Link2 : ArrowRightLeft; return <label key={path} data-enabled={row[path].enabled}><span><Icon size={15}/>{t[path]}</span><input type="checkbox" data-creation-field={`${path}Enabled`} aria-label={`${t[path]}: ${id}`} checked={row[path].enabled} disabled={disabled} onChange={event => update(id, { ...row, [path]: { ...row[path], enabled: event.target.checked } })}/></label>; })}
            </div>
            {(["direct", "tunnel"] as const).map(path => row[path].enabled && <fieldset className="creation-path-fields" disabled={disabled} key={path}>
              <legend>{path === "direct" ? <Globe2 size={15}/> : <Link2 size={15}/>} {t[path]}</legend>
              <div className={"creation-port-hosts" + (row[path].endpoints !== undefined ? " creation-rich-hosts" : "")}><label className="creation-field"><span>{t[`${path}Port`]}</span><input aria-label={`${t[`${path}Port`]}: ${id}`} data-creation-field={`${path}Port`} dir="ltr" inputMode="numeric" value={row[path].port} onChange={event => update(id, { ...row, [path]: { ...row[path], port: event.target.value } })} placeholder={path === "direct" ? "10443" : "1185"}/></label>
                <InboundCreationEndpoints language={language} path={path} nodeID={id} value={row[path]} onChange={value => update(id, { ...row, [path]: value })} supported={endpointOptionsSupported} disabled={disabled} maximum={maxEndpointsPerPath} invalidFields={nodeIssues.map(issue => issue.field)} defaults={{ serverNames: state.profile.serverNames.split(/[\s,]+/).filter(Boolean), fingerprint: state.profile.options?.clientFingerprint ?? "", spiderX: state.profile.options?.clientSpiderX ?? "", security: state.profile.security }}/></div>
              {!creationPathHosts(row[path]).length && <p className="creation-help creation-waiting">{t.emptyHost}</p>}
            </fieldset>)}
            {(row.direct.enabled || row.tunnel.enabled) && <p className="creation-help">{t.hostsHint}</p>}
            {row.swap.enabled && <fieldset className="creation-path-fields creation-swap-fields" disabled={disabled}>
              <legend><ArrowRightLeft size={15}/> {t.swap}</legend><p className="creation-help">{t.swapHint}</p>
              <div className="creation-field-grid"><label className="creation-field"><span>{t.source}</span><select aria-label={`${t.source}: ${id}`} data-creation-field="sourceNode" value={row.swap.sourceNodeID} onChange={event => {
                const source = nodes.find(item => item.node_id === event.target.value);
                const selectedSource = source && state.selected.includes(source.node_id) ? state.nodes[source.node_id] : undefined;
                const savedIPs = new Set(targets.filter(target => target.node_id === source?.node_id && target.installation_id === source?.installation_id).map(target => target.listen_ip));
                const sourceListenIP = selectedSource?.listenIP || (savedIPs.size === 1 ? [...savedIPs][0] : "");
                update(id, { ...row, swap: { ...row.swap, sourceNodeID: source?.node_id ?? "", sourceInstallationID: source?.installation_id ?? "", sourceListenIP } });
              }}><option value="">{t.choose}</option>{nodes.filter(item => item.node_id !== id).map(item => <option key={item.node_id} value={item.node_id}>{creationNodeName(nodes, item.node_id)}</option>)}</select><small>{t.sourceHint}</small></label>
                <label className="creation-field"><span>{t.relay}</span><input aria-label={`${t.relay}: ${id}`} data-creation-field="sourcePort" dir="ltr" inputMode="numeric" value={row.swap.sourcePort} onChange={event => update(id, { ...row, swap: { ...row.swap, sourcePort: event.target.value } })} placeholder="16443"/></label></div>
              <label className="creation-field"><span>{t.sourceIP}</span><input aria-label={`${t.sourceIP}: ${id}`} data-creation-field="sourceListenIP" dir="ltr" value={row.swap.sourceListenIP} onChange={event => update(id, { ...row, swap: { ...row.swap, sourceListenIP: event.target.value } })} placeholder="203.0.113.20" autoComplete="off"/></label>
              <div className="creation-field-grid"><label className="creation-field"><span>{t.entryHost}</span><input aria-label={`${t.entryHost}: ${id}`} data-creation-field="entryHost" dir="ltr" value={row.swap.entryHost} onChange={event => update(id, { ...row, swap: { ...row.swap, entryHost: event.target.value } })} placeholder="entry.example.org" autoComplete="off"/><small>{t.entryHint}</small></label>
                <label className="creation-field"><span>{t.entryPort}</span><input aria-label={`${t.entryPort}: ${id}`} data-creation-field="entryPort" dir="ltr" inputMode="numeric" value={row.swap.entryPort} onChange={event => update(id, { ...row, swap: { ...row.swap, entryPort: event.target.value } })} placeholder="443"/></label></div>
              <div className="creation-field-grid"><label className="creation-field"><span>{t.destinationPort}</span><input aria-label={`${t.destinationPort}: ${id}`} data-creation-field="swapPort" dir="ltr" inputMode="numeric" value={row.swap.destinationPort} onChange={event => update(id, { ...row, swap: { ...row.swap, destinationPort: event.target.value } })} placeholder="18443"/></label>
                <div className="creation-reuse">{row.tunnel.enabled && creationPort(row.tunnel.port) > 0 && <button type="button" onClick={() => update(id, { ...row, swap: { ...row.swap, destinationPort: row.tunnel.port } })}><Plus size={14}/>{t.useTunnel}</button>}<small>{row.tunnel.enabled && creationPort(row.tunnel.port) && row.swap.destinationPort === row.tunnel.port ? t.reuse : t.shadow}</small></div></div>
              <div className="creation-relay-map"><div><span>{t.entryLabel}</span><bdi dir="ltr">{row.swap.entryHost || t.notSet}:{row.swap.entryPort || "—"}</bdi></div><ArrowRight size={14}/><div><span>{t.relayLabel}</span><bdi>{row.swap.sourceNodeID ? creationNodeName(nodes, row.swap.sourceNodeID) : t.notSet} : {row.swap.sourcePort || "—"}</bdi></div><ArrowRight size={14}/><div><span>{t.destinationLabel}</span><bdi>{creationNodeName(nodes, id)} : {row.swap.destinationPort || "—"}</bdi></div></div>
            </fieldset>}
            {nodeIssues.length > 0 && <ul className="creation-issues" role="alert">{nodeIssues.map((issue, index) => <li key={index}>{creationIssueMessage(issue, language)}</li>)}</ul>}
          </div>}
        </article>;
      })}
    </div>
  </section>;
}
