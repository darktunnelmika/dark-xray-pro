// Dark VPN owner panel (v2). No framework, no build step.
const I18N = {
  en: {
    dashboard: "Dashboard", clients: "Clients", inbounds: "Inbounds", nodes: "Nodes", settings: "Settings",
    login: "Sign in", username: "Username", password: "Password", badLogin: "Invalid username or password",
    nodesOnline: "Nodes online", activeClients: "Active clients", onlineUsers: "Online now", trafficToday: "Traffic today", trafficTotal: "Total traffic",
    add: "Add", edit: "Edit", delete: "Delete", save: "Save", cancel: "Cancel", close: "Close", copy: "Copy", copied: "Copied",
    search: "Search", name: "Name", volumeGB: "Volume (GB, 0 = unlimited)", days: "Days (0 = no expiry)", firstUse: "Start on first connection",
    maxIPs: "Max IPs (0 = no limit)", enabled: "Enabled", assignInbounds: "Inbounds", unlimited: "Unlimited", noExpiry: "No expiry",
    notStarted: "Starts on first use", daysLeft: "{n} days left", expired: "Expired", disabled: "Disabled", exhausted: "Volume used up",
    subscription: "Subscription", subHint: "Import this link or scan the QR code in v2rayNG, Hiddify or Streisand.",
    confirmDelete: "Delete {x}? Usage history is kept.", node: "Node", preset: "Preset", tag: "Tag", port: "Port", sni: "REALITY target (SNI)",
    path: "Path / service name (optional)", remark: "Remark", directAddr: "Public address", tunnelAddr: "Tunnel entry (optional, address:port)",
    addNode: "Add node", nodeId: "Node ID (e.g. de-1)", nodeToken: "Node token — shown once, keep it safe", online: "Online", offline: "Offline",
    restarts: "Xray starts", spooled: "Waiting batches", language: "Language", theme: "Theme", system: "System", light: "Light", dark: "Dark",
    logout: "Sign out", empty: "Nothing here yet.", saved: "Saved", failed: "Failed: {e}", clientsCount: "{n} clients", lastError: "Last error",
    reps: "Resellers", changePassword: "Change password", oldPassword: "Current password", newPassword: "New password (10+ chars)",
    credit: "Credit", volumeCredit: "Volume credit", units: "Unlimited units", addCredit: "Add credit", unitsHint: "1 unit = 1 user × 30 days",
    plan: "Plan", planVolume: "Volume", planUnlimited: "Unlimited", repId: "Reseller ID", loginUser: "Login username", loginPass: "Login password",
    installCmd: "Run this on the new server as root (link works once, for 1 hour):", note: "Note", reinstall: "New install link",
    rename: "Rename", groups: "Groups", members: "Members", bulkRenew: "Bulk renew", addDays: "Add days", addGB: "Add GB",
    selected: "{n} selected", changed: "{n} clients updated", all: "All", filterOnline: "Online", filterExpiring: "Expiring (≤3 days)",
    filterExpired: "Expired / used up", filterDisabled: "Disabled", group: "Group", contact: "Contact / note", transfer: "Move to reseller",
    ownerSelf: "Owner (me)", confirmNodeDelete: "Delete node {x} and its inbounds? Clients and usage stay.", selectAll: "Select all",
    backup: "Backup", backupHint: "Download a full snapshot (nodes, inbounds and keys, clients, resellers, credit, usage). The HUB also keeps 14 daily snapshots.", download: "Download backup",
    finance: "Finance", activity: "Activity", policy: "Limits", usageChart: "Daily usage", payment: "Record payment", amount: "Amount",
    maxClients: "Max clients (0 = no cap)", maxDays: "Longest term, days (0 = no cap)", maxClientGB: "Largest client volume, GB (0 = no cap)",
    allowUnlimited: "May sell unlimited plans", canDelete: "May delete clients", refundUnused: "Return unused volume on delete",
    allowedInbounds: "Allowed inbounds (none ticked = all)", priceGB: "Price per GB", priceUnit: "Price per unlimited unit",
    period: "Period", last30: "Last 30 days", last7: "Last 7 days", allTime: "All time", sold: "Sold", refunded: "Returned", granted: "Credit given",
    paid: "Paid", revenue: "Revenue", debt: "Balance due", balance: "Remaining credit", refundDone: "Returned {v} to your credit",
    kind_adjust: "Credit change", kind_charge: "Sale", kind_refund: "Return", kind_delete: "Deleted", kind_payment: "Payment",
    advanced: "Advanced editor", template: "Xray inbound (JSON, without clients)", metaJson: "Subscription settings (JSON)", invalidJson: "Invalid JSON in {x}",
    enable: "Enable", disable: "Disable", advancedHint: "Clients are injected by the HUB. Nodes pick up changes within a few seconds without dropping other inbounds.",
    swap: "Dark Swap", swapHint: "Relay this inbound through another node: the relay listens on its own port and forwards TCP to this inbound's Direct address. Users and accounting stay on the inbound's node. Point your entry server (e.g. in Iran) at relay address:relay port.",
    relayNode: "Relay node", relayPort: "Relay port", entryAddr: "Entry address (in the link)", entryPort: "Entry port", forwardsTo: "forwards to",
    traffic14: "Traffic, last 14 days", topToday: "Top users today", recentEvents: "Recent blocks", noEvents: "No blocks — every limit is quiet.",
    certs: "TLS certificates", addCert: "Add certificate", certPem: "Certificate (PEM, full chain)", keyPem: "Private key (PEM)",
    certFor: "Certificate (TLS presets)", noCert: "— none —", usedBy: "used by", certHint: "Paste the full chain and key (e.g. from certbot or Cloudflare origin). Uploading again with the same ID renews it on every inbound and node.",
    sellers: "Sub-sellers", transferCredit: "Transfer credit", subOf: "under", kind_transfer: "Transfer",
    sellersHint: "Open sellers under you and give them part of your credit. Their limits can only be tighter than yours.",
    transferHint: "Moves credit from your balance to this seller. Negative numbers take unused credit back.",
    shop: "Shop", products: "Products", orders: "Orders", newOrder: "New order", product: "Product", price: "Price", buyer: "Buyer (name, @telegram, phone)",
    renewClient: "Renew an existing client (optional)", fulfill: "Deliver", cancelOrder: "Cancel", status_pending: "Pending", status_fulfilled: "Delivered",
    status_cancelled: "Cancelled", status_draft: "Draft", status_published: "Published", status_archived: "Archived", statusLabel: "Status", seller: "Seller",
    delivered: "Delivered to {x}", manageProducts: "May create own shop products", shopHint: "Orders are recorded here; delivering one creates (or renews) the client in one step. A reseller's orders are paid from that reseller's credit.",
    newClient: "New client", firstUseShort: "starts on first use",
    telegram: "Telegram", telegramHint: "Connect your Telegram to get your order cards with Deliver/Cancel, your credit and your clients in the bot.",
    connectTelegram: "Connect Telegram", openBot: "Open the bot", codeExpires: "The link works once, for 15 minutes.",
    stepProfile: "Profile", stepNodes: "Nodes & routes", stepSwap: "Dark Swap", stepReview: "Review", next: "Next", back: "Back",
    newInbound: "New inbound", editInbound: "Nodes & routes", nodesRoutes: "Nodes & routes", pickNode: "Select at least one node.",
    badTag: "Tag: lowercase letters, digits, dot, dash or underscore.", needCert: "TLS presets need a certificate (add one under TLS certificates).",
    profileHint: "One profile — protocol, transport, security and keys — shared by every node you place it on. Clients assigned to it work on all of them.",
    nodesHint: "Pick the nodes. Each gets its own listen IP, a Direct port and, if you want, a separate Tunnel port so their traffic is counted apart.",
    hostsHint: "Hosts are the addresses clients receive. Add as many as you like per path; ⚙ opens per-host SNI, Host, Path, ALPN, fingerprint and format filters.",
    direct: "Direct", tunnel: "Tunnel", route: "Path", address: "Address", listenIP: "Listen IP", pathOverride: "Path / service", hideIn: "Hide in:",
    destNode: "Destination node", targetAddr: "Relay target (optional)", targetAuto: "auto: node's public IP", noHosts: "no hosts",
    used: "Used", of: "of", last30Traffic: "Last 30 days", timeLeft: "Time left", details: "Details", firstSeen: "First connection", lastSeen: "Last seen",
    when: "When", who: "By", what: "What", more: "Load more", totalLast30: "{v} in the last 30 days",
    health: "Health (24h)", cpu: "CPU", ram: "RAM", disk: "Disk", net: "Network", uptime: "Uptime", inRate: "in", outRate: "out", noData: "No samples yet",
  },
  fa: {
    dashboard: "داشبورد", clients: "کلاینت‌ها", inbounds: "اینباندها", nodes: "نودها", settings: "تنظیمات",
    login: "ورود", username: "نام کاربری", password: "رمز عبور", badLogin: "نام کاربری یا رمز اشتباه است",
    nodesOnline: "نود آنلاین", activeClients: "کلاینت فعال", onlineUsers: "آنلاین الان", trafficToday: "ترافیک امروز", trafficTotal: "کل ترافیک",
    add: "افزودن", edit: "ویرایش", delete: "حذف", save: "ذخیره", cancel: "انصراف", close: "بستن", copy: "کپی", copied: "کپی شد",
    search: "جستجو", name: "نام", volumeGB: "حجم (گیگ، ۰ = نامحدود)", days: "روز (۰ = بدون انقضا)", firstUse: "شروع از اولین اتصال",
    maxIPs: "حداکثر IP (۰ = بدون محدودیت)", enabled: "فعال", assignInbounds: "اینباندها", unlimited: "نامحدود", noExpiry: "بدون انقضا",
    notStarted: "از اولین اتصال شروع می‌شود", daysLeft: "{n} روز مانده", expired: "منقضی", disabled: "غیرفعال", exhausted: "حجم تمام شد",
    subscription: "لینک ساب", subHint: "این لینک را در v2rayNG، Hiddify یا Streisand وارد کنید یا QR را اسکن کنید.",
    confirmDelete: "{x} حذف شود؟ سابقهٔ مصرف حفظ می‌شود.", node: "نود", preset: "نوع", tag: "شناسه", port: "پورت", sni: "مقصد REALITY (SNI)",
    path: "مسیر / نام سرویس (اختیاری)", remark: "نام نمایشی", directAddr: "آدرس عمومی", tunnelAddr: "ورودی تونل (اختیاری، آدرس:پورت)",
    addNode: "افزودن نود", nodeId: "شناسه نود (مثلاً de-1)", nodeToken: "توکن نود — فقط یک بار نشان داده می‌شود", online: "آنلاین", offline: "آفلاین",
    restarts: "دفعات اجرای Xray", spooled: "بستهٔ در انتظار", language: "زبان", theme: "پوسته", system: "سیستم", light: "روشن", dark: "تیره",
    logout: "خروج", empty: "هنوز چیزی نیست.", saved: "ذخیره شد", failed: "خطا: {e}", clientsCount: "{n} کلاینت", lastError: "آخرین خطا",
    reps: "نماینده‌ها", changePassword: "تغییر رمز", oldPassword: "رمز فعلی", newPassword: "رمز جدید (حداقل ۱۰ کاراکتر)",
    credit: "اعتبار", volumeCredit: "اعتبار حجمی", units: "واحد نامحدود", addCredit: "افزودن اعتبار", unitsHint: "هر واحد = ۱ کاربر × ۳۰ روز",
    plan: "نوع پلن", planVolume: "حجمی", planUnlimited: "نامحدود", repId: "شناسه نماینده", loginUser: "نام کاربری ورود", loginPass: "رمز ورود",
    installCmd: "این دستور را روی سرور جدید با root اجرا کنید (یک‌بار مصرف، ۱ ساعت):", note: "توضیح", reinstall: "لینک نصب جدید",
    rename: "تغییر نام", groups: "گروه‌ها", members: "اعضا", bulkRenew: "تمدید گروهی", addDays: "افزودن روز", addGB: "افزودن گیگ",
    selected: "{n} انتخاب شده", changed: "{n} کلاینت به‌روز شد", all: "همه", filterOnline: "آنلاین", filterExpiring: "رو به انقضا (≤۳ روز)",
    filterExpired: "منقضی / حجم تمام", filterDisabled: "غیرفعال", group: "گروه", contact: "تماس / یادداشت", transfer: "انتقال به نماینده",
    ownerSelf: "مالک (خودم)", confirmNodeDelete: "نود {x} و اینباندهایش حذف شود؟ کلاینت‌ها و مصرف حفظ می‌شوند.", selectAll: "انتخاب همه",
    backup: "بکاپ", backupHint: "دانلود نسخهٔ کامل (نودها، اینباندها و کلیدها، کلاینت‌ها، نماینده‌ها، اعتبار، مصرف). هاب ۱۴ بکاپ روزانه هم نگه می‌دارد.", download: "دانلود بکاپ",
    finance: "مالی", activity: "فعالیت", policy: "محدودیت‌ها", usageChart: "مصرف روزانه", payment: "ثبت پرداخت", amount: "مبلغ",
    maxClients: "حداکثر کلاینت (۰ = بدون سقف)", maxDays: "بیشترین مدت، روز (۰ = بدون سقف)", maxClientGB: "بیشترین حجم هر کلاینت، گیگ (۰ = بدون سقف)",
    allowUnlimited: "اجازهٔ فروش نامحدود", canDelete: "اجازهٔ حذف کلاینت", refundUnused: "برگشت حجم مصرف‌نشده هنگام حذف",
    allowedInbounds: "اینباندهای مجاز (هیچ‌کدام = همه)", priceGB: "قیمت هر گیگ", priceUnit: "قیمت هر واحد نامحدود",
    period: "بازه", last30: "۳۰ روز اخیر", last7: "۷ روز اخیر", allTime: "همهٔ زمان‌ها", sold: "فروش", refunded: "برگشتی", granted: "اعتبار داده‌شده",
    paid: "پرداختی", revenue: "درآمد", debt: "بدهی", balance: "اعتبار باقی‌مانده", refundDone: "{v} به اعتبار شما برگشت",
    kind_adjust: "تغییر اعتبار", kind_charge: "فروش", kind_refund: "برگشت", kind_delete: "حذف", kind_payment: "پرداخت",
    advanced: "ویرایشگر پیشرفته", template: "اینباند Xray (JSON، بدون کلاینت)", metaJson: "تنظیمات ساب (JSON)", invalidJson: "JSON نامعتبر در {x}",
    enable: "فعال کردن", disable: "غیرفعال کردن", advancedHint: "کلاینت‌ها را هاب اضافه می‌کند. نودها تغییر را ظرف چند ثانیه می‌گیرند و بقیهٔ اینباندها قطع نمی‌شوند.",
    swap: "Dark Swap", swapHint: "این اینباند را از یک نود دیگر رله کن: نود واسط روی پورت خودش گوش می‌دهد و TCP را به آدرس Direct این اینباند می‌فرستد. کاربر و حسابداری روی نود اصلی می‌ماند. سرور ورودی (مثلاً ایران) را به آدرس:پورت واسط بفرست.",
    relayNode: "نود واسط", relayPort: "پورت واسط", entryAddr: "آدرس ورودی (داخل لینک)", entryPort: "پورت ورودی", forwardsTo: "ارسال به",
    traffic14: "ترافیک ۱۴ روز اخیر", topToday: "پرمصرف‌های امروز", recentEvents: "آخرین قطع‌ها", noEvents: "قطعی نداشته‌ایم؛ همهٔ محدودیت‌ها آرام‌اند.",
    certs: "گواهی‌های TLS", addCert: "افزودن گواهی", certPem: "گواهی (PEM، زنجیرهٔ کامل)", keyPem: "کلید خصوصی (PEM)",
    certFor: "گواهی (برای الگوهای TLS)", noCert: "— هیچ —", usedBy: "استفاده در", certHint: "زنجیرهٔ کامل و کلید را وارد کنید (مثلاً از certbot یا Cloudflare Origin). آپلود دوباره با همان شناسه، گواهی را روی همهٔ اینباندها و نودها تمدید می‌کند.",
    sellers: "زیرفروشنده‌ها", transferCredit: "انتقال اعتبار", subOf: "زیرمجموعهٔ", kind_transfer: "انتقال",
    sellersHint: "برای خودتان زیرفروشنده بسازید و بخشی از اعتبارتان را به او بدهید. محدودیت‌های او فقط می‌تواند سخت‌تر از شما باشد.",
    transferHint: "اعتبار را از موجودی شما به این فروشنده منتقل می‌کند. عدد منفی اعتبار مصرف‌نشده را برمی‌گرداند.",
    shop: "فروشگاه", products: "محصولات", orders: "سفارش‌ها", newOrder: "سفارش جدید", product: "محصول", price: "قیمت", buyer: "خریدار (نام، آیدی تلگرام، تلفن)",
    renewClient: "تمدید کلاینت موجود (اختیاری)", fulfill: "تحویل", cancelOrder: "لغو", status_pending: "در انتظار", status_fulfilled: "تحویل شد",
    status_cancelled: "لغو شد", status_draft: "پیش‌نویس", status_published: "منتشر", status_archived: "بایگانی", statusLabel: "وضعیت", seller: "فروشنده",
    delivered: "تحویل شد: {x}", manageProducts: "اجازهٔ ساخت محصول فروشگاه", shopHint: "سفارش‌ها اینجا ثبت می‌شوند؛ با «تحویل» کلاینت یک‌جا ساخته (یا تمدید) می‌شود. سفارش‌های نماینده از اعتبار همان نماینده پرداخت می‌شود.",
    newClient: "کلاینت جدید", firstUseShort: "شروع از اولین اتصال",
    telegram: "تلگرام", telegramHint: "تلگرامتان را وصل کنید تا سفارش‌ها با دکمهٔ تحویل/لغو، اعتبار و کلاینت‌هایتان در ربات بیاید.",
    connectTelegram: "اتصال تلگرام", openBot: "باز کردن ربات", codeExpires: "این لینک یک‌بار مصرف است و ۱۵ دقیقه اعتبار دارد.",
    stepProfile: "پروفایل", stepNodes: "نودها و مسیرها", stepSwap: "دارک سواپ", stepReview: "بازبینی", next: "بعدی", back: "قبلی",
    newInbound: "اینباند جدید", editInbound: "نودها و مسیرها", nodesRoutes: "نودها و مسیرها", pickNode: "حداقل یک نود انتخاب کنید.",
    badTag: "شناسه: حروف کوچک انگلیسی، عدد، نقطه، خط تیره یا زیرخط.", needCert: "الگوهای TLS گواهی لازم دارند (از بخش گواهی‌های TLS اضافه کنید).",
    profileHint: "یک پروفایل — پروتکل، شبکه، امنیت و کلیدها — که روی همهٔ نودهای انتخابی یکسان است. کلاینتِ این اینباند روی همهٔ آن‌ها کار می‌کند.",
    nodesHint: "نودها را انتخاب کنید. هر نود IP گوش‌دادن، پورت Direct و در صورت نیاز پورت جدای Tunnel دارد تا مصرف هر مسیر جدا شمرده شود.",
    hostsHint: "هاست‌ها آدرس‌هایی هستند که در لینک کاربر می‌آیند. برای هر مسیر هر چند تا خواستید اضافه کنید؛ ⚙ تنظیمات SNI، Host، Path، ALPN، Fingerprint و فیلتر فرمت هر هاست را باز می‌کند.",
    direct: "Direct", tunnel: "Tunnel", route: "مسیر", address: "آدرس", listenIP: "IP گوش‌دادن", pathOverride: "Path / سرویس", hideIn: "مخفی در:",
    destNode: "نود مقصد", targetAddr: "مقصد رله (اختیاری)", targetAuto: "خودکار: IP عمومی نود", noHosts: "بدون هاست",
    used: "مصرف", of: "از", last30Traffic: "۳۰ روز اخیر", timeLeft: "زمان باقی‌مانده", details: "جزئیات", firstSeen: "اولین اتصال", lastSeen: "آخرین فعالیت",
    when: "زمان", who: "توسط", what: "شرح", more: "بیشتر", totalLast30: "{v} در ۳۰ روز اخیر",
    health: "سلامت (۲۴ ساعت)", cpu: "پردازنده", ram: "رم", disk: "دیسک", net: "شبکه", uptime: "روشن بودن", inRate: "ورودی", outRate: "خروجی", noData: "هنوز نمونه‌ای نیست",
  },
};
let lang = safeGet("lang") === "fa" ? "fa" : "en";
const t = (k, vars = {}) => (I18N[lang][k] ?? I18N.en[k] ?? k).replace(/\{(\w+)\}/g, (_, v) => vars[v] ?? "");
const fmtNum = (n) => new Intl.NumberFormat(lang === "fa" ? "fa-IR" : "en-US", { maximumFractionDigits: 1 }).format(n);

function safeGet(k) { try { return localStorage.getItem(k); } catch { return null; } }
function safeSet(k, v) { try { v == null ? localStorage.removeItem(k) : localStorage.setItem(k, v); } catch {} }

// Tiny element builder: never uses innerHTML, so names from the API can't inject markup.
function h(tag, attrs = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "class") el.className = v;
    else if (k === "style") el.style.cssText = v; // CSSOM: allowed under a strict style-src CSP
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const kid of kids.flat()) if (kid != null && kid !== false) el.append(kid instanceof Node ? kid : String(kid));
  return el;
}

function bytes(n) {
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${fmtNum(n)} ${u[i]}`;
}

async function api(method, path, body) {
  const res = await fetch(path, {
    method, credentials: "same-origin",
    headers: { "Content-Type": "application/json", "X-Dark-Request": "1" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401 && !path.startsWith("/api/v2/auth/login")) { location.hash = "#/login"; throw new Error("unauthorized"); }
  const text = await res.text();
  if (!res.ok) throw new Error(text.trim() || res.statusText);
  return text ? JSON.parse(text) : null;
}

function toast(msg) {
  const el = document.getElementById("toast");
  el.textContent = msg;
  try { el.showPopover(); } catch {}
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => { try { el.hidePopover(); } catch {} }, 2200);
}

async function copy(text) {
  try { await navigator.clipboard.writeText(text); toast(t("copied")); } catch { prompt(t("copy"), text); }
}

// One shared native <dialog>; content is rebuilt per use.
function openModal(title, body, actions = []) {
  const d = document.getElementById("modal");
  d.replaceChildren(
    h("h2", { id: "modal-title" }, title), body,
    h("form", { method: "dialog", class: "actions" },
      ...actions, h("button", { value: "cancel", autofocus: actions.length === 0 }, actions.length ? t("cancel") : t("close"))),
  );
  d.returnValue = "";
  d.showModal();
  return d;
}

function confirmAction(text) {
  return new Promise((resolve) => {
    const d = openModal(text, h("p", { class: "muted" }, ""), [h("button", { value: "ok", class: "danger" }, t("delete"))]);
    d.addEventListener("close", () => { resolve(d.returnValue === "ok"); d.returnValue = ""; }, { once: true });
  });
}

// ---------- Views ----------
const allRoutes = { dashboard: viewDashboard, clients: viewClients, groups: viewGroups, inbounds: viewInbounds, nodes: viewNodes, reps: viewReps, shop: viewShop, finance: viewFinance, activity: viewActivity, settings: viewSettings };
let me = null;
let routes = allRoutes;
function routesFor(m) {
  if (m && m.role === "rep") {
    const r = { clients: viewClients };
    if (m.can_sell) r.sellers = viewSellers;
    r.shop = viewShop; r.activity = viewActivity; r.settings = viewSettings;
    return r;
  }
  return allRoutes;
}

// Brand mark and icons are built with the DOM (strict CSP, no image files).
const SVGNS = "http://www.w3.org/2000/svg";
function svgEl(n, a) { const x = document.createElementNS(SVGNS, n); for (const [k, v] of Object.entries(a)) x.setAttribute(k, v); return x; }
let markSeq = 0;
function mark() {
  const id = `g${++markSeq}`;
  const svg = svgEl("svg", { viewBox: "0 0 32 32", "aria-hidden": "true" });
  const defs = svgEl("defs", {});
  const g = svgEl("linearGradient", { id, x1: "0", y1: "0", x2: "1", y2: "1" });
  g.append(svgEl("stop", { offset: "0", "stop-color": "#2de2e6" }), svgEl("stop", { offset: "1", "stop-color": "#9b5cff" }));
  defs.append(g);
  svg.append(defs,
    svgEl("path", { d: "M16 2.5 28 9.25v13.5L16 29.5 4 22.75V9.25z", fill: "none", stroke: `url(#${id})`, "stroke-width": 2.2, "stroke-linejoin": "round" }),
    svgEl("circle", { cx: 12.5, cy: 19.5, r: 2.2, fill: `url(#${id})` }),
    svgEl("path", { d: "M12.5 14.5a5 5 0 0 1 5 5", stroke: `url(#${id})`, "stroke-width": 2.2, fill: "none", "stroke-linecap": "round" }),
    svgEl("path", { d: "M12.5 10a9.5 9.5 0 0 1 9.5 9.5", stroke: `url(#${id})`, "stroke-width": 2.2, fill: "none", "stroke-linecap": "round" }));
  return svg;
}
const ICONS = {
  dashboard: "M3 13h8V3H3zM13 21h8V11h-8zM3 21h8v-6H3zM13 3v6h8V3z",
  clients: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75",
  groups: "M12 2 2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5",
  inbounds: "M6 3v6M18 3v6M4 9h16v3a8 8 0 0 1-16 0zM12 20v2",
  nodes: "M3 4h18v6H3zM3 14h18v6H3zM7 7h.01M7 17h.01",
  reps: "M3 7h18v13H3zM8 7V5a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M3 13h18",
  sellers: "M15 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M8.5 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM20 8v6M23 11h-6",
  shop: "M6 2 3 6v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6l-3-4zM3 6h18M16 10a4 4 0 0 1-8 0",
  finance: "M3 3v18h18M7 15l4-4 3 3 6-6",
  activity: "M22 12h-4l-3 9L9 3l-3 9H2",
  settings: "M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6",
};
function icon(name) {
  const svg = svgEl("svg", { viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", "stroke-width": 1.8, "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true" });
  svg.append(svgEl("path", { d: ICONS[name] || ICONS.dashboard }));
  return svg;
}

function shell(active, content) {
  const tabs = Object.keys(routes).map((r) => h("a", { href: `#/${r}`, "aria-current": r === active ? "page" : null }, icon(r), h("span", {}, t(r))));
  return h("div", { class: "shell" },
    h("header", { class: "top" }, h("span", { class: "brand" }, mark(), "Dark VPN"),
      me && me.role === "rep" ? h("span", { class: "badge" }, `${t("volumeCredit")}: ${bytes(me.volume_credit || 0)} · ${t("units")}: ${fmtNum(me.unlimited_units || 0)}`) : null,
      h("span", { class: "muted small" }, me ? me.user : "")),
    h("nav", { class: "tabs", "aria-label": "Main" }, tabs),
    h("main", {}, content));
}

function viewLogin() {
  const err = h("div", { class: "error", role: "alert" });
  const user = h("input", { name: "u", autocomplete: "username", required: true, autofocus: true });
  const pass = h("input", { name: "p", type: "password", autocomplete: "current-password", required: true });
  const btn = h("button", { class: "primary", type: "submit" }, t("login"));
  const form = h("form", { class: "form", onsubmit: async (e) => {
    e.preventDefault(); btn.disabled = true; err.textContent = "";
    try { await api("POST", "/api/v2/auth/login", { Username: user.value, Password: pass.value }); location.hash = "#/dashboard"; }
    catch { err.textContent = t("badLogin"); } finally { btn.disabled = false; }
  } }, h("label", {}, h("span", {}, t("username")), user), h("label", {}, h("span", {}, t("password")), pass), err, btn);
  return h("div", { class: "login" }, h("div", { class: "card" }, h("h1", {}, mark(), "Dark VPN"), form));
}

async function viewDashboard() {
  const [o, nodes, ins] = await Promise.all([api("GET", "/api/v2/admin/overview"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/insights?days=14")]);
  const stat = (k, v) => h("div", { class: "card stat" }, h("div", { class: "v" }, v), h("div", { class: "k" }, t(k)));
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const topMax = Math.max(1, ...ins.top.map((u) => u.bytes));
  return h("div", {},
    h("h1", {}, t("dashboard")),
    h("div", { class: "grid" },
      stat("nodesOnline", `${fmtNum(o.NodesOnline)} / ${fmtNum(o.Nodes)}`),
      stat("activeClients", `${fmtNum(o.ClientsActive)} / ${fmtNum(o.Clients)}`),
      stat("onlineUsers", fmtNum(o.OnlineUsers)),
      stat("trafficToday", bytes(o.TrafficToday)),
      stat("trafficTotal", bytes(o.TrafficTotal))),
    h("div", { class: "dash" },
      h("div", { class: "card form wide-col" }, h("div", { class: "row" }, h("h2", {}, t("traffic14")), legend()), barsSvg(ins.daily, 1100)),
      h("div", { class: "card form" }, h("h2", {}, t("topToday")),
        ins.top.length ? h("div", { class: "meters" }, ins.top.map((u) => meter(u.email, (100 * u.bytes) / topMax, bytes(u.bytes), true))) : h("p", { class: "muted small" }, t("noData"))),
      h("div", { class: "card form" }, h("h2", {}, t("recentEvents")),
        ins.events.length ? h("div", { class: "form" }, ins.events.map((e) => h("div", { class: "row small" },
          h("span", { class: "badge bad" }, e.kind.replace("blocked:", "")), h("span", {}, e.email), h("span", { class: "muted spacer" }, `${e.node_id} · ${fmtTime(e.unix)}`))))
          : h("p", { class: "muted small" }, t("noEvents")))),
    h("div", { class: "list" }, (nodes || []).map((n) => nodeCard(n))));
}

function rate(bps) { return `${bytes(bps)}/s`; }
function dur(sec) { const d = Math.floor(sec / 86400), hh = Math.floor((sec % 86400) / 3600); return d ? `${fmtNum(d)}d ${fmtNum(hh)}h` : `${fmtNum(hh)}h`; }

// Sparkline as inline SVG (built with the DOM, never markup strings).
function spark(values, max) {
  const NS = "http://www.w3.org/2000/svg", W = 300, H = 48;
  const svg = document.createElementNS(NS, "svg");
  svg.setAttribute("viewBox", `0 0 ${W} ${H}`); svg.setAttribute("class", "spark"); svg.setAttribute("preserveAspectRatio", "none");
  if (values.length < 2) return svg;
  const top = max || Math.max(1, ...values);
  const pts = values.map((v, i) => `${(i / (values.length - 1)) * W},${H - 2 - (Math.min(v, top) / top) * (H - 4)}`).join(" ");
  const area = document.createElementNS(NS, "polygon");
  area.setAttribute("points", `0,${H} ${pts} ${W},${H}`); area.setAttribute("class", "spark-area");
  const line = document.createElementNS(NS, "polyline");
  line.setAttribute("points", pts); line.setAttribute("class", "spark-line");
  svg.append(area, line);
  return svg;
}

async function nodeHealth(n) {
  const pts = await api("GET", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/history?hours=24`);
  const block = (label, vals, max, fmt) => h("div", { class: "form" },
    h("div", { class: "row small" }, h("strong", {}, label), h("span", { class: "muted spacer" }, vals.length ? fmt(vals.at(-1)) : "—")), spark(vals, max));
  openModal(`${t("health")} · ${n.Name || n.ID}`, pts.length ? h("div", { class: "form" },
    block(t("cpu"), pts.map((p) => p.cpu), 100, (v) => `${fmtNum(v)}%`),
    block(t("ram"), pts.map((p) => p.ram), 100, (v) => `${fmtNum(v)}%`),
    block(`${t("net")} · ${t("inRate")}`, pts.map((p) => p.rx), 0, rate),
    block(`${t("net")} · ${t("outRate")}`, pts.map((p) => p.tx), 0, rate),
    block(t("onlineUsers"), pts.map((p) => p.online), 0, (v) => fmtNum(v))) : h("p", { class: "muted" }, t("noData")));
}

function nodeCard(n, actions = false) {
  const host = n.Host;
  return h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: `dot ${n.online ? "on" : ""}` }), h("span", { class: "name" }, `${n.Name || n.ID}`),
      h("span", { class: "badge" }, n.ID), h("span", { class: `badge ${n.online ? "good" : "bad"} spacer` }, t(n.online ? "online" : "offline"))),
    h("div", { class: "muted small" }, `${t("restarts")}: ${fmtNum(n.XrayRestarts)} · ${t("spooled")}: ${fmtNum(n.Spooled)} · ${n.AgentVersion || ""}`),
    host ? h("div", { class: "meters" },
      meter(t("cpu"), host.cpu_percent, `${fmtNum(host.cpu_percent)}%`),
      meter(t("ram"), host.ram_total ? (100 * host.ram_used) / host.ram_total : 0, `${bytes(host.ram_used)} / ${bytes(host.ram_total)}`),
      host.disk_total ? meter(t("disk"), (100 * host.disk_used) / host.disk_total, `${bytes(host.disk_used)} / ${bytes(host.disk_total)}`) : null,
      h("div", { class: "small muted" }, `↓ ${rate(host.rx_rate)} · ↑ ${rate(host.tx_rate)} · ${t("onlineUsers")}: ${fmtNum(host.online_users)} · ${t("uptime")}: ${dur(host.uptime_sec)}`)) : null,
    n.LastError ? h("div", { class: "small", style: "color:var(--bad)" }, `${t("lastError")}: ${n.LastError}`) : null,
    actions ? h("div", { class: "row" },
      h("button", { onclick: () => nodeHealth(n) }, t("health")),
      h("button", { class: "ghost", onclick: () => renameNode(n) }, t("rename")),
      h("button", { class: "ghost", onclick: async () => {
        try { const r = await guarded(() => api("POST", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}/install`));
          openModal(t("reinstall"), h("div", { class: "form" }, h("p", { class: "small" }, t("installCmd")), h("code", {}, r.install), h("button", { type: "button", onclick: () => copy(r.install) }, t("copy"))));
        } catch {}
      } }, t("reinstall")),
      h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmNodeDelete", { x: n.ID }))) { await guarded(() => api("DELETE", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}`)); route(); }
      } }, t("delete"))) : null);
}

function meter(label, pct, text, neutral = false) {
  const p = Math.max(0, Math.min(100, pct || 0));
  return h("div", { class: "meter" }, h("div", { class: "row small" }, h("span", {}, label), h("span", { class: "muted spacer" }, text)),
    h("div", { class: "bar" }, h("i", { class: neutral ? "" : p > 90 ? "bad" : p > 75 ? "warn" : "", style: `inline-size:${p}%` })));
}

function renameNode(n) {
  const name = h("input", { value: n.Name || "", autofocus: true });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try { await guarded(() => api("PUT", `/api/v2/admin/nodes/${encodeURIComponent(n.ID)}`, { Name: name.value })); document.getElementById("modal").close("save"); route(); } catch {}
  } }, t("save"));
  openModal(`${t("rename")} · ${n.ID}`, h("label", {}, h("span", {}, t("name")), name), [save]);
}

function clientStatus(c) {
  const now = Date.now() / 1000;
  if (!c.Enabled) return ["bad", t("disabled")];
  if (c.Remaining === 0) return ["bad", t("exhausted")];
  if (c.EffectiveUntil && c.EffectiveUntil <= now) return ["bad", t("expired")];
  if (!c.EffectiveUntil && c.DurationSeconds) return ["", t("notStarted")];
  if (!c.EffectiveUntil) return ["", t("noExpiry")];
  const d = Math.ceil((c.EffectiveUntil - now) / 86400);
  return [d <= 3 ? "warn" : "good", t("daysLeft", { n: fmtNum(d) })];
}

async function viewClients() {
  const owner = me.role === "owner";
  const [list, inbounds, groups, reps] = await Promise.all([
    api("GET", "/api/v2/admin/clients"),
    owner ? api("GET", "/api/v2/admin/inbounds") : api("GET", "/api/v2/rep/inbounds").catch(() => []),
    owner ? api("GET", "/api/v2/admin/groups") : Promise.resolve([]),
    owner ? api("GET", "/api/v2/admin/reps") : Promise.resolve([]),
  ]);
  const ctx = { inbounds, groups: groups || [], reps: reps || [] };
  const selected = new Set();
  const box = h("div", { class: "list" });
  const count = h("span", { class: "muted small" });
  const q = h("input", { class: "search", type: "search", placeholder: t("search"), "aria-label": t("search") });
  const filter = h("select", { "aria-label": t("all") }, [["", t("all")], ["online", t("filterOnline")], ["expiring", t("filterExpiring")], ["bad", t("filterExpired")], ["disabled", t("filterDisabled")]]
    .map(([v, l]) => h("option", { value: v }, l)),
    ctx.groups.map((g) => h("option", { value: `g:${g.ID}` }, `${t("group")}: ${g.Name || g.ID}`)),
    (inbounds || []).map((ib) => h("option", { value: `i:${ib.tag}` }, `${t("inbounds")}: ${ib.tag}`)));
  const match = (c) => {
    const f = filter.value, now = Date.now() / 1000;
    if (q.value && !(c.Email + " " + (c.Note || "")).toLowerCase().includes(q.value.toLowerCase())) return false;
    if (f === "online") return c.Online;
    if (f === "disabled") return !c.Enabled;
    if (f === "bad") return c.Remaining === 0 || (c.EffectiveUntil && c.EffectiveUntil <= now);
    if (f === "expiring") return c.EffectiveUntil && c.EffectiveUntil > now && c.EffectiveUntil - now <= 3 * 86400;
    if (f.startsWith("g:")) return (ctx.groups.find((g) => g.ID === f.slice(2))?.Members || []).includes(c.Email);
    if (f.startsWith("i:")) return (c.Inbounds || []).includes(f.slice(2));
    return true;
  };
  const bulkBtn = h("button", { onclick: () => bulkForm([...selected]), disabled: true }, t("bulkRenew"));
  const sync = () => { count.textContent = selected.size ? t("selected", { n: fmtNum(selected.size) }) : t("clientsCount", { n: fmtNum((list || []).length) }); bulkBtn.disabled = selected.size === 0; };
  const render = () => {
    const rows = (list || []).filter(match);
    box.replaceChildren(...(rows.length ? rows.map((c) => clientCard(c, ctx, owner ? selected : null, sync)) : [h("p", { class: "muted" }, t("empty"))]));
    sync();
  };
  q.addEventListener("input", render);
  filter.addEventListener("change", render);
  render();
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("clients")), count,
      h("span", { class: "spacer" }), owner ? bulkBtn : null,
      h("button", { class: "primary", onclick: () => clientForm(null, inbounds) }, t("add"))),
    h("div", { class: "row" }, q, filter),
    box);
}

function bulkForm(emails) {
  const days = h("input", { type: "number", min: "0", value: "30" });
  const gb = h("input", { type: "number", min: "0", step: "0.5", value: "0" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try {
      const r = await api("POST", "/api/v2/admin/clients/bulk", { Emails: emails, AddDays: +days.value || 0, AddGB: +gb.value || 0 });
      toast(t("changed", { n: fmtNum(r.changed) })); document.getElementById("modal").close("save"); route();
    } catch (x) { toast(t("failed", { e: x.message })); }
  } }, t("save"));
  openModal(`${t("bulkRenew")} · ${t("selected", { n: fmtNum(emails.length) })}`, h("div", { class: "form two" },
    h("label", {}, h("span", {}, t("addDays")), days), h("label", {}, h("span", {}, t("addGB")), gb)), [save]);
}

async function viewGroups() {
  const [groups, clients] = await Promise.all([api("GET", "/api/v2/admin/groups"), api("GET", "/api/v2/admin/clients")]);
  const cards = (groups || []).map((g) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, g.Name || g.ID), h("span", { class: "badge" }, g.ID),
      h("span", { class: "badge spacer" }, t("clientsCount", { n: fmtNum(g.Members.length) }))),
    h("div", { class: "small muted" }, g.Members.join(", ")),
    h("div", { class: "row" },
      h("button", { onclick: () => bulkGroup(g) }, t("bulkRenew")),
      h("button", { class: "ghost", onclick: () => groupForm(g, clients) }, t("edit")),
      h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: g.Name || g.ID }))) { await guarded(() => api("DELETE", `/api/v2/admin/groups/${encodeURIComponent(g.ID)}`)); route(); }
      } }, t("delete")))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("groups")), h("button", { class: "primary spacer", onclick: () => groupForm(null, clients) }, t("add"))),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))));
}

function bulkGroup(g) {
  const days = h("input", { type: "number", min: "0", value: "30" });
  const gb = h("input", { type: "number", min: "0", step: "0.5", value: "0" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try { const r = await api("POST", "/api/v2/admin/clients/bulk", { Group: g.ID, AddDays: +days.value || 0, AddGB: +gb.value || 0 });
      toast(t("changed", { n: fmtNum(r.changed) })); document.getElementById("modal").close("save"); route(); } catch (x) { toast(t("failed", { e: x.message })); }
  } }, t("save"));
  openModal(`${t("bulkRenew")} · ${g.Name || g.ID}`, h("div", { class: "form two" },
    h("label", {}, h("span", {}, t("addDays")), days), h("label", {}, h("span", {}, t("addGB")), gb)), [save]);
}

function groupForm(g, clients) {
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: g ? g.ID : "", disabled: !!g, autofocus: !g });
  const name = h("input", { value: g ? g.Name : "" });
  const boxes = (clients || []).map((c) => h("label", { class: "check" },
    h("input", { type: "checkbox", value: c.Email, checked: !!g && g.Members.includes(c.Email) }), c.Email));
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!id.reportValidity()) return;
    const members = boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value);
    try { await guarded(() => api("PUT", `/api/v2/admin/groups/${encodeURIComponent(id.value)}`, { Name: name.value, Members: members }));
      document.getElementById("modal").close("save"); route(); } catch {}
  } }, t("save"));
  openModal(g ? `${t("edit")} · ${g.Name || g.ID}` : `${t("add")} · ${t("groups")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), id), h("label", {}, h("span", {}, t("name")), name)),
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("members")), boxes)), [save]);
}

function clientCard(c, ctx, selected, onSelect) {
  const inbounds = ctx.inbounds;
  const used = c.Usage.Up + c.Usage.Down;
  const pct = c.VolumeBytes > 0 ? Math.min(100, (used / c.VolumeBytes) * 100) : 0;
  const [cls, status] = clientStatus(c);
  return h("div", { class: "card item" },
    h("div", { class: "head" },
      selected ? h("input", { type: "checkbox", "aria-label": c.Email, checked: selected.has(c.Email), style: "inline-size:auto",
        onchange: (e) => { e.target.checked ? selected.add(c.Email) : selected.delete(c.Email); onSelect(); } }) : null,
      h("span", { class: `dot ${c.Online ? "on" : ""}`, title: c.Online ? t("online") : t("offline") }),
      h("a", { class: "name", href: `#/client?e=${encodeURIComponent(c.Email)}` }, c.Email), h("span", { class: `badge ${cls}` }, status)),
    h("div", { class: "row small muted" },
      h("span", {}, `${bytes(used)} / ${c.VolumeBytes < 0 ? t("unlimited") : bytes(c.VolumeBytes)}`),
      c.MaxIPs ? h("span", {}, `· IP ≤ ${fmtNum(c.MaxIPs)}`) : null,
      h("span", {}, `· ${(c.Inbounds || []).join(", ")}`)),
    c.Note ? h("div", { class: "small muted" }, c.Note) : null,
    c.VolumeBytes > 0 ? h("div", { class: "bar" }, h("i", { class: pct > 90 ? "bad" : pct > 75 ? "warn" : "", style: `inline-size:${pct}%` })) : null,
    h("div", { class: "row" },
      h("button", { onclick: () => showSub(c) }, t("subscription")),
      h("button", { class: "ghost", onclick: () => clientForm(c, inbounds, ctx) }, t("edit")),
      h("button", { class: "ghost", onclick: () => usageChart(c) }, t("usageChart")),
      me.role === "rep" && me.policy && !me.policy.CanDelete ? null : h("button", { class: "ghost danger spacer", onclick: async () => {
        if (!(await confirmAction(t("confirmDelete", { x: c.Email })))) return;
        const r = await guarded(() => api("DELETE", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}`));
        if (r && (r.refund_bytes || r.refund_units)) toast(t("refundDone", { v: [r.refund_bytes ? bytes(r.refund_bytes) : "", r.refund_units ? `${fmtNum(r.refund_units)} ${t("units")}` : ""].filter(Boolean).join(" + ") }));
        route();
      } }, t("delete"))));
}

// Client detail page: #/client?e=<name>
async function viewClient() {
  const email = new URLSearchParams(location.hash.split("?")[1] || "").get("e") || "";
  const owner = me.role === "owner";
  const [list, inbounds, days, reps] = await Promise.all([
    api("GET", "/api/v2/admin/clients"),
    owner ? api("GET", "/api/v2/admin/inbounds") : api("GET", "/api/v2/rep/inbounds").catch(() => []),
    api("GET", `/api/v2/admin/clients/${encodeURIComponent(email)}/daily?days=30`).catch(() => []),
    owner ? api("GET", "/api/v2/admin/reps") : Promise.resolve([])]);
  const c = (list || []).find((x) => x.Email === email);
  if (!c) return h("div", {}, h("h1", {}, t("clients")), h("p", { class: "muted" }, t("empty")));
  const ctx = { inbounds, groups: [], reps: reps || [] };
  const [cls, status] = clientStatus(c);
  const used = c.Usage.Up + c.Usage.Down;
  const now = Date.now() / 1000;
  const left = c.EffectiveUntil ? Math.max(0, Math.ceil((c.EffectiveUntil - now) / 86400)) : null;
  const stat = (k, v, sub) => h("div", { class: "card stat" }, h("div", { class: "v" }, v), h("div", { class: "k" }, k), sub ? h("div", { class: "small muted" }, sub) : null);
  const url = c.SubURL || "";
  const fmt = (label, u) => h("div", { class: "row" }, h("span", { class: "badge" }, label), h("code", { style: "flex:1" }, u), h("button", { type: "button", class: "ghost", onclick: () => copy(u) }, t("copy")));
  const sum30 = days.reduce((a, d) => a + d.Up + d.Down, 0);
  return h("div", {},
    h("div", { class: "row" }, h("a", { class: "btn ghost", href: "#/clients" }, "‹"), h("span", { class: `dot ${c.Online ? "on" : ""}` }), h("h1", {}, c.Email),
      h("span", { class: `badge ${cls}` }, status),
      h("span", { class: "spacer" }),
      h("button", { onclick: () => clientForm(c, inbounds, ctx) }, t("edit")),
      me.role === "rep" && me.policy && !me.policy.CanDelete ? null : h("button", { class: "ghost danger", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: c.Email }))) { await guarded(() => api("DELETE", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}`)); location.hash = "#/clients"; }
      } }, t("delete"))),
    h("div", { class: "grid" },
      stat(t("used"), bytes(used), c.VolumeBytes > 0 ? `${t("of")} ${bytes(c.VolumeBytes)} · ${fmtNum(Math.min(100, (used / c.VolumeBytes) * 100))}%` : t("unlimited")),
      stat(t("timeLeft"), left == null ? (c.DurationSeconds ? t("notStarted") : t("noExpiry")) : t("daysLeft", { n: fmtNum(left) })),
      stat(t("maxIPs").split(" (")[0], c.MaxIPs ? fmtNum(c.MaxIPs) : "∞"),
      stat(t("last30Traffic"), bytes(sum30))),
    h("div", { class: "dash" },
      h("div", { class: "card form wide-col" }, h("div", { class: "row" }, h("h2", {}, t("usageChart")), legend()), barsSvg(days, 1100)),
      h("div", { class: "card form" }, h("h2", {}, t("subscription")),
        h("img", { class: "qr", alt: "QR", src: `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/qr.png` }),
        fmt("Auto", url), fmt("sing-box", `${url}?format=singbox`), fmt("Clash", `${url}?format=clash`)),
      h("div", { class: "card form" }, h("h2", {}, t("details")),
        h("div", { class: "row" }, h("span", { class: "muted" }, t("assignInbounds")), ...(c.Inbounds || []).map((x) => h("span", { class: "badge" }, x))),
        c.RepID ? h("div", { class: "row" }, h("span", { class: "muted" }, t("seller")), h("span", { class: "badge" }, c.RepID)) : null,
        c.Plan ? h("div", { class: "row" }, h("span", { class: "muted" }, t("plan")), h("span", {}, c.Plan === "unlimited" ? t("planUnlimited") : t("planVolume"))) : null,
        c.Note ? h("div", { class: "row" }, h("span", { class: "muted" }, t("note")), h("span", {}, c.Note)) : null,
        c.Usage.FirstSeen ? h("div", { class: "row small" }, h("span", { class: "muted" }, t("firstSeen")), h("span", {}, new Date(c.Usage.FirstSeen * 1000).toLocaleString(lang === "fa" ? "fa-IR" : "en-GB"))) : null,
        c.Usage.LastSeen ? h("div", { class: "row small" }, h("span", { class: "muted" }, t("lastSeen")), h("span", {}, new Date(c.Usage.LastSeen * 1000).toLocaleString(lang === "fa" ? "fa-IR" : "en-GB"))) : null)));
}

function showSub(c) {
  const url = c.SubURL || "";
  openModal(`${t("subscription")} · ${c.Email}`, h("div", { class: "form" },
    h("p", { class: "muted small" }, t("subHint")),
    h("img", { class: "qr", alt: "QR", src: `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/qr.png` }),
    h("code", {}, url),
    h("button", { type: "button", onclick: () => copy(url) }, t("copy"))));
}

async function guarded(fn) {
  try { const r = await fn(); toast(t("saved")); return r; } catch (e) { toast(t("failed", { e: e.message })); throw e; }
}

function clientForm(c, inbounds, ctx = { reps: [] }) {
  const isNew = !c;
  const v = c || { Email: "", Enabled: true, VolumeBytes: -1, ExpiresUnix: 0, DurationSeconds: 30 * 86400, MaxIPs: 2, Inbounds: [] };
  const daysLeft = v.ExpiresUnix ? Math.max(0, Math.round((v.ExpiresUnix - Date.now() / 1000) / 86400)) : Math.round((v.DurationSeconds || 0) / 86400);
  const name = h("input", { required: true, pattern: "[A-Za-z0-9][A-Za-z0-9._@+-]{0,127}", value: v.Email, disabled: !isNew, autofocus: isNew });
  const vol = h("input", { type: "number", min: "0", step: "0.5", value: v.VolumeBytes < 0 ? 0 : +(v.VolumeBytes / 2 ** 30).toFixed(2) });
  const days = h("input", { type: "number", min: "0", value: daysLeft });
  const first = h("input", { type: "checkbox", checked: isNew ? true : !v.ExpiresUnix && v.DurationSeconds > 0 });
  const ips = h("input", { type: "number", min: "0", max: "1000", value: v.MaxIPs });
  const enabled = h("input", { type: "checkbox", checked: v.Enabled });
  const note = h("input", { value: v.Note || "", maxlength: "2000" });
  const owner = me.role === "owner";
  const repSel = owner && !isNew && ctx.reps.length ? h("select", {}, h("option", { value: "" }, t("ownerSelf")),
    ctx.reps.map((r) => h("option", { value: r.ID, selected: v.RepID === r.ID }, r.Name || r.ID))) : null;
  const plan = h("select", {}, h("option", { value: "volume", selected: v.Plan !== "unlimited" }, t("planVolume")),
    me.role === "rep" && me.policy && !me.policy.AllowUnlimited ? null : h("option", { value: "unlimited", selected: v.Plan === "unlimited" }, t("planUnlimited")));
  const boxes = (inbounds || []).map((ib) => h("label", { class: "check" },
    h("input", { type: "checkbox", value: ib.tag, checked: isNew ? (inbounds.length === 1) : (v.Inbounds || []).includes(ib.tag) }),
    `${ib.tag} · ${ib.node_id} · ${ib.network}/${ib.security}`));
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!name.reportValidity()) return;
    const d = +days.value || 0;
    const body = {
      Enabled: enabled.checked, MaxIPs: +ips.value || 0,
      VolumeBytes: +vol.value > 0 ? Math.round(+vol.value * 2 ** 30) : -1,
      ExpiresUnix: d && !first.checked ? Math.floor(Date.now() / 1000) + d * 86400 : 0,
      DurationSeconds: d && first.checked ? d * 86400 : 0,
      Inbounds: boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value),
      Plan: plan.value,
    };
    if (plan.value === "unlimited") body.VolumeBytes = -1;
    if (!isNew && !first.checked && v.ExpiresUnix && d === daysLeft) body.ExpiresUnix = v.ExpiresUnix;
    try {
      const em = encodeURIComponent(name.value);
      await api("PUT", `/api/v2/admin/clients/${em}`, body);
      if ((note.value || "") !== (v.Note || "")) await api("PUT", `/api/v2/admin/clients/${em}/note`, { Note: note.value });
      if (repSel && repSel.value !== (v.RepID || "")) await api("POST", `/api/v2/admin/clients/${em}/transfer`, { RepID: repSel.value });
      toast(t("saved")); document.getElementById("modal").close("save"); route();
    }
    catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(isNew ? t("add") : `${t("edit")} · ${v.Email}`, h("div", { class: "form" },
    h("label", {}, h("span", {}, t("name")), name),
    me.role === "rep" ? h("label", {}, h("span", {}, t("plan")), plan, h("span", { class: "small" }, t("unitsHint"))) : null,
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("volumeGB")), vol), h("label", {}, h("span", {}, t("days")), days)),
    h("label", { class: "check" }, first, t("firstUse")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("maxIPs")), ips), h("label", { class: "check" }, enabled, t("enabled"))),
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("assignInbounds")), boxes),
    h("label", {}, h("span", {}, t("contact")), note),
    repSel ? h("label", {}, h("span", {}, t("transfer")), repSel) : null,
    err), [save]);
}

async function viewInbounds() {
  const [list, nodes, presets, certs] = await Promise.all([api("GET", "/api/v2/admin/inbounds"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/presets"), api("GET", "/api/v2/admin/certs")]);
  const cards = (list || []).map((ib) => {
    let meta = {};
    try { meta = typeof ib.meta === "string" ? JSON.parse(ib.meta) : ib.meta || {}; } catch {}
    return h("div", { class: "card item" },
      h("div", { class: "head" }, h("span", { class: `dot ${ib.enabled ? "on" : ""}` }), h("span", { class: "name" }, ib.tag), h("span", { class: "badge" }, `${ib.protocol} · ${ib.network} · ${ib.security}`),
        ib.enabled ? null : h("span", { class: "badge bad" }, t("disabled")),
        h("span", { class: "spacer" })),
      h("div", { class: "row" }, (ib.nodes || []).map((n) => h("span", { class: "badge" }, n))),
      h("div", { class: "muted small" }, `${t("clientsCount", { n: fmtNum(ib.clients) })} · ${t("trafficToday")}: ${bytes(ib.traffic_today || 0)} · ${t("trafficTotal")}: ${bytes(ib.traffic_total || 0)}`),
      h("div", { class: "row" },
        h("button", { class: "primary", onclick: () => inboundStudio(ib.tag, { nodes, presets, certs }) }, t("nodesRoutes")),
        h("button", { onclick: async () => { await guarded(() => api("POST", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}/enabled`, { Enabled: !ib.enabled })); route(); } }, t(ib.enabled ? "disable" : "enable")),
        h("button", { class: "ghost", onclick: () => inboundEditor(ib.tag, nodes) }, t("advanced")),
        h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: ib.tag }))) { await guarded(() => api("DELETE", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}`)); route(); }
      } }, t("delete"))));
  });
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("inbounds")), h("button", { class: "primary spacer", onclick: () => inboundStudio(null, { nodes, presets, certs }) }, t("newInbound"))),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))),
    h("div", { class: "row section" }, h("h2", {}, t("certs")), h("button", { class: "spacer", onclick: () => certForm() }, t("addCert"))),
    h("div", { class: "list" }, certs.length ? certs.map((c) => h("div", { class: "card item" },
      h("div", { class: "head" }, h("span", { class: "name" }, c.id), h("span", { class: "badge" }, c.domains.join(", ")),
        h("span", { class: `badge spacer ${c.days_left < 7 ? "bad" : c.days_left < 21 ? "warn" : "good"}` }, t("daysLeft", { n: fmtNum(c.days_left) }))),
      c.used_by.length ? h("div", { class: "small muted" }, `${t("usedBy")}: ${c.used_by.join(", ")}`) : null,
      h("div", { class: "row" }, h("button", { class: "ghost", onclick: () => certForm(c.id) }, t("edit")),
        h("button", { class: "ghost danger spacer", onclick: async () => {
          if (await confirmAction(t("confirmDelete", { x: c.id }))) { await guarded(() => api("DELETE", `/api/v2/admin/certs/${encodeURIComponent(c.id)}`)); route(); }
        } }, t("delete"))))) : h("p", { class: "muted small" }, t("empty"))));
}

// ---------- Inbound studio: one profile, many nodes, Direct/Tunnel/Swap per node ----------
async function inboundStudio(tag, ctx) {
  const isNew = !tag;
  const nodes = ctx.nodes || [];
  const nodeName = (id) => { const n = nodes.find((x) => x.ID === id); return n ? n.Name || n.ID : id; };
  const st = {
    step: 0,
    profile: { tag: "", preset: (ctx.presets || [])[0] || "vless-reality-tcp", sni: "www.speedtest.net", path: "", remark: "Dark VPN", cert: (ctx.certs || [])[0]?.id || "" },
    routes: {}, // nodeID -> {listen_ip, direct_port, tunnel_port, hosts}
    swaps: [], removedSwaps: [],
  };
  const newHost = (path, node) => ({ path, enabled: true, remark: "", address: path === "direct" ? (node?.PublicIP || "") : "", port: 443,
    sni: "", host: "", path_override: "", alpn: "", fingerprint: "", allow_insecure: false, exclude: [], _adv: false });
  const newRoute = (node) => ({ listen_ip: "", direct_port: 443, tunnel_port: 0, hosts: [newHost("direct", node)] });
  if (!isNew) {
    const [routes, swaps] = await Promise.all([api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}/routes`), api("GET", `/api/v2/admin/swaps?inbound=${encodeURIComponent(tag)}`)]);
    for (const r of routes) st.routes[r.node_id] = { ...r, hosts: r.hosts.map((x) => ({ ...x, exclude: x.exclude || [], _adv: !!(x.sni || x.host || x.path_override || x.alpn || x.fingerprint || x.allow_insecure || x.exclude?.length) })) };
    st.swaps = swaps.map((w) => ({ ...w }));
    st.step = 1;
  }
  const steps = [["stepProfile"], ["stepNodes"], ["stepSwap"], ["stepReview"]];
  const body = h("div", { class: "form" });
  const err = h("div", { class: "error", role: "alert" });
  const d = openModal(isNew ? t("newInbound") : `${t("editInbound")} · ${tag}`, h("div", {}, h("div", { class: "steps", role: "tablist" }), body, err), [
    h("button", { class: "ghost", type: "button", onclick: (e) => { e.preventDefault(); go(st.step - 1); } }, t("back")),
    h("button", { class: "primary", value: "save", onclick: async (e) => { e.preventDefault(); if (st.step < 3) go(st.step + 1); else await save(); } }, t("next")),
  ]);
  d.classList.add("wide");
  d.addEventListener("close", () => d.classList.remove("wide"), { once: true });
  const stepBar = d.querySelector(".steps");
  const nextBtn = d.querySelector(".actions .primary");
  const backBtn = d.querySelector(".actions .ghost");

  function go(i) {
    if (i < (isNew ? 0 : 1) || i > 3) return;
    if (i > st.step && st.step === 0 && !validProfile()) return;
    if (i > 1 && st.step <= 1 && !Object.keys(st.routes).length) { err.textContent = t("pickNode"); return; }
    err.textContent = ""; st.step = i; draw();
  }
  function validProfile() {
    if (!/^[a-z0-9][a-z0-9._-]{0,62}$/.test(st.profile.tag)) { err.textContent = t("badTag"); return false; }
    if (st.profile.preset.includes("-tls-") && !st.profile.cert) { err.textContent = t("needCert"); return false; }
    return true;
  }
  const field = (label, el) => h("label", {}, h("span", {}, label), el);
  const bind = (obj, key, attrs = {}, num = false) => {
    const el = h("input", { ...attrs, value: obj[key] ?? "" });
    el.addEventListener("input", () => { obj[key] = num ? (+el.value || 0) : el.value; });
    return el;
  };

  function draw() {
    stepBar.replaceChildren(...steps.map(([k], i) => h("button", { type: "button", role: "tab", "aria-current": i === st.step ? "step" : null,
      disabled: isNew ? false : i === 0, onclick: () => go(i) }, `${i + 1}. ${t(k)}`)));
    backBtn.hidden = st.step === (isNew ? 0 : 1);
    nextBtn.textContent = st.step === 3 ? t("save") : t("next");
    body.replaceChildren(...[drawProfile, drawNodes, drawSwaps, drawReview][st.step]());
  }

  function drawProfile() {
    const p = st.profile;
    const preset = h("select", {}, (ctx.presets || []).map((x) => h("option", { value: x, selected: x === p.preset }, x)));
    const certSel = h("select", {}, (ctx.certs || []).map((c) => h("option", { value: c.id, selected: c.id === p.cert }, `${c.id} · ${c.domains.join(", ")}`)));
    const certRow = field(t("certFor"), certSel);
    const sync = () => { certRow.hidden = !p.preset.includes("-tls-"); };
    preset.addEventListener("change", () => { p.preset = preset.value; sync(); });
    certSel.addEventListener("change", () => { p.cert = certSel.value; const c = (ctx.certs || []).find((x) => x.id === p.cert); if (c?.domains[0]) { p.sni = c.domains[0]; draw(); } });
    sync();
    return [
      h("p", { class: "small muted" }, t("profileHint")),
      h("div", { class: "two" }, field(t("tag"), bind(p, "tag", { required: true, placeholder: "multi-reality", autofocus: true })), field(t("preset"), preset)),
      certRow,
      h("div", { class: "two" }, field(t("sni"), bind(p, "sni")), field(t("path"), bind(p, "path"))),
      field(t("remark"), bind(p, "remark")),
    ];
  }

  function hostRow(r, hst, idx, node) {
    const pathSel = h("select", {}, [["direct", t("direct")], ["tunnel", t("tunnel")]].map(([v, l]) => h("option", { value: v, selected: hst.path === v }, l)));
    pathSel.addEventListener("change", () => { hst.path = pathSel.value; });
    const en = h("input", { type: "checkbox", checked: hst.enabled, "aria-label": t("enabled") });
    en.addEventListener("change", () => { hst.enabled = en.checked; });
    const adv = h("div", { class: "adv", hidden: !hst._adv },
      field("SNI", bind(hst, "sni")), field("Host", bind(hst, "host")), field(t("pathOverride"), bind(hst, "path_override")),
      field("ALPN", bind(hst, "alpn", { placeholder: "h2,http/1.1" })), field("Fingerprint", bind(hst, "fingerprint", { placeholder: "chrome" })),
      h("div", { class: "form" },
        h("label", { class: "check" }, (() => { const c = h("input", { type: "checkbox", checked: hst.allow_insecure }); c.addEventListener("change", () => { hst.allow_insecure = c.checked; }); return c; })(), "allowInsecure"),
        h("div", { class: "row small" }, t("hideIn"), ...["links", "singbox", "clash"].map((f) => h("label", { class: "check" },
          (() => { const c = h("input", { type: "checkbox", checked: hst.exclude.includes(f) }); c.addEventListener("change", () => { hst.exclude = c.checked ? [...hst.exclude, f] : hst.exclude.filter((x) => x !== f); }); return c; })(), f)))));
    return h("div", { class: "hostrow" },
      field(t("route"), pathSel), field(t("address"), bind(hst, "address", { placeholder: "1.2.3.4 / cdn.example.com" })),
      field(t("port"), bind(hst, "port", { type: "number", min: "1", max: "65535" }, true)), field(t("remark"), bind(hst, "remark", { placeholder: node })),
      h("label", { class: "check" }, en, t("enabled")),
      h("div", { class: "row" },
        h("button", { type: "button", class: "ghost", onclick: () => { hst._adv = !hst._adv; adv.hidden = !hst._adv; } }, "⚙"),
        h("button", { type: "button", class: "ghost danger", "aria-label": t("delete"), onclick: () => { r.hosts.splice(idx, 1); draw(); } }, "✕")),
      adv);
  }

  function drawNodes() {
    const pick = h("div", { class: "nodepick" }, nodes.map((n) => {
      const c = h("input", { type: "checkbox", checked: !!st.routes[n.ID] });
      c.addEventListener("change", () => { if (c.checked) st.routes[n.ID] = st.routes[n.ID] || newRoute(n); else delete st.routes[n.ID]; draw(); });
      return h("label", {}, c, h("span", { class: `dot ${n.online ? "on" : ""}` }), `${n.Name || n.ID}`, h("span", { class: "muted small" }, n.ID));
    }));
    const cards = Object.keys(st.routes).sort().map((id) => {
      const r = st.routes[id];
      const node = nodes.find((x) => x.ID === id);
      const tunnelOn = h("input", { type: "checkbox", checked: r.tunnel_port > 0 });
      const tport = bind(r, "tunnel_port", { type: "number", min: "1", max: "65535", disabled: !(r.tunnel_port > 0) }, true);
      tunnelOn.addEventListener("change", () => { r.tunnel_port = tunnelOn.checked ? (r.direct_port === 8443 ? 2083 : 8443) : 0; draw(); });
      return h("div", { class: "route" },
        h("div", { class: "head" }, h("span", { class: `dot ${node?.online ? "on" : ""}` }), h("strong", {}, nodeName(id)), h("span", { class: "badge" }, id),
          node?.PublicIP ? h("span", { class: "badge" }, node.PublicIP) : null,
          !isNew && r.direct_total != null ? h("span", { class: "small muted spacer" }, `${t("direct")} ${bytes(r.direct_total || 0)} · ${t("tunnel")} ${bytes(r.tunnel_total || 0)}`) : null),
        h("div", { class: "three" },
          field(t("listenIP"), bind(r, "listen_ip", { placeholder: "0.0.0.0" })),
          field(`${t("direct")} · ${t("port")}`, bind(r, "direct_port", { type: "number", min: "1", max: "65535", required: true }, true)),
          h("label", { class: "tunnel-label" }, h("span", {}, tunnelOn, `${t("tunnel")} · ${t("port")}`), tport)),
        ...r.hosts.map((x, i) => hostRow(r, x, i, id)),
        h("div", { class: "row" },
          h("button", { type: "button", onclick: () => { r.hosts.push(newHost("direct", node)); draw(); } }, `+ ${t("direct")}`),
          h("button", { type: "button", onclick: () => { r.hosts.push({ ...newHost("tunnel", node), port: r.tunnel_port || r.direct_port }); draw(); } }, `+ ${t("tunnel")}`)));
    });
    return [h("p", { class: "small muted" }, t("nodesHint")), pick, cards.length ? h("p", { class: "small muted" }, t("hostsHint")) : null, ...cards];
  }

  function drawSwaps() {
    const placed = Object.keys(st.routes).sort();
    const rows = st.swaps.map((w, i) => {
      const dest = h("select", {}, placed.map((id) => h("option", { value: id, selected: id === w.dest_node }, nodeName(id))));
      dest.addEventListener("change", () => { w.dest_node = dest.value; });
      const src = h("select", {}, nodes.map((n) => h("option", { value: n.ID, selected: n.ID === w.source_node }, n.Name || n.ID)));
      src.addEventListener("change", () => { w.source_node = src.value; });
      const en = h("input", { type: "checkbox", checked: w.enabled });
      en.addEventListener("change", () => { w.enabled = en.checked; });
      return h("div", { class: "route" },
        h("div", { class: "head" }, h("span", { class: "pathtag swap" }, "Dark Swap"), h("strong", {}, w.name || "DARK SWAP"), w.target ? h("span", { class: "small muted mono" }, `→ ${w.target}`) : null,
          h("button", { type: "button", class: "ghost danger spacer", onclick: () => { if (!w._new) st.removedSwaps.push(w.id); st.swaps.splice(i, 1); draw(); } }, t("delete"))),
        h("div", { class: "three" }, field(t("name"), bind(w, "name")), field(t("destNode"), dest), field(t("relayNode"), src)),
        h("div", { class: "three" }, field(t("relayPort"), bind(w, "source_port", { type: "number", min: "1", max: "65535" }, true)),
          field(t("entryAddr"), bind(w, "entry_address", { placeholder: "ir.example.com" })), field(t("entryPort"), bind(w, "entry_port", { type: "number", min: "1", max: "65535" }, true))),
        h("div", { class: "two" }, field(t("targetAddr"), bind(w, "target_address", { placeholder: t("targetAuto") })), h("label", { class: "check" }, en, t("enabled"))));
    });
    return [h("p", { class: "small muted" }, t("swapHint")), ...rows,
      h("div", { class: "row" }, h("button", { type: "button", disabled: !placed.length || nodes.length < 2, onclick: () => {
        const dest = placed[0];
        const src = nodes.find((n) => n.ID !== dest)?.ID || "";
        st.swaps.push({ _new: true, id: `${st.profile.tag || tag}-swap${st.swaps.length + 1}`, name: "DARK SWAP", dest_node: dest, source_node: src, source_port: 6801, entry_address: "", entry_port: 6801, target_address: "", enabled: true });
        draw();
      } }, `+ ${t("swap")}`))];
  }

  function drawReview() {
    const lines = [];
    for (const id of Object.keys(st.routes).sort()) {
      const r = st.routes[id];
      lines.push(h("div", { class: "line" }, h("strong", {}, nodeName(id)), h("span", { class: "pathtag direct" }, `${t("direct")} :${r.direct_port}`),
        r.tunnel_port ? h("span", { class: "pathtag tunnel" }, `${t("tunnel")} :${r.tunnel_port}`) : null,
        h("span", { class: "small muted" }, r.hosts.filter((x) => x.enabled).map((x) => `${x.address}:${x.port}`).join("  ") || t("noHosts"))));
    }
    for (const w of st.swaps) lines.push(h("div", { class: "line" }, h("span", { class: "pathtag swap" }, "Dark Swap"),
      h("span", { class: "mono small" }, `${w.entry_address || "?"}:${w.entry_port} → ${nodeName(w.source_node)}:${w.source_port} → ${nodeName(w.dest_node)}`)));
    return [isNew ? h("p", {}, h("strong", {}, st.profile.tag), h("span", { class: "muted" }, ` · ${st.profile.preset}`)) : null, h("div", { class: "review" }, lines)];
  }

  async function save() {
    err.textContent = "";
    const routes = Object.entries(st.routes).map(([node_id, r]) => ({ node_id, listen_ip: (r.listen_ip || "").trim(), direct_port: +r.direct_port, tunnel_port: +r.tunnel_port || 0,
      hosts: r.hosts.map(({ _adv, ...x }) => ({ ...x, port: +x.port })) }));
    const theTag = isNew ? st.profile.tag : tag;
    try {
      if (isNew) {
        const p = st.profile;
        await api("POST", "/api/v2/admin/inbounds/compose", { tag: p.tag, preset: p.preset, sni: p.sni.trim(), path: p.path.trim(), remark: p.remark.trim(), cert: p.preset.includes("-tls-") ? p.cert : "", routes });
      } else {
        await api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(theTag)}/routes`, routes);
      }
      for (const id of st.removedSwaps) await api("DELETE", `/api/v2/admin/swaps/${encodeURIComponent(id)}`);
      for (const w of st.swaps) {
        const { _new, target, ...body } = w;
        await api("PUT", `/api/v2/admin/swaps/${encodeURIComponent(w.id)}`, { ...body, inbound_tag: theTag, source_port: +w.source_port, entry_port: +w.entry_port });
      }
      toast(t("saved")); d.close("save"); route();
    } catch (x) { err.textContent = x.message; }
  }
  draw();
}

async function swapDialog(ib, nodes) {
  const list = await api("GET", `/api/v2/admin/swaps?inbound=${encodeURIComponent(ib.tag)}`);
  const others = (nodes || []).filter((n) => n.ID !== ib.node_id);
  const rows = list.map((w) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: `dot ${w.enabled ? "on" : ""}` }), h("span", { class: "name" }, w.name), h("span", { class: "badge" }, w.id)),
    h("div", { class: "small mono" }, `${w.entry_address}:${w.entry_port} → ${w.source_node}:${w.source_port} → ${w.target || "?"}`),
    h("div", { class: "row" },
      h("button", { type: "button", onclick: async () => { await guarded(() => api("PUT", `/api/v2/admin/swaps/${encodeURIComponent(w.id)}`, { ...w, enabled: !w.enabled })); swapDialog(ib, nodes); } }, t(w.enabled ? "disable" : "enable")),
      h("button", { type: "button", class: "ghost danger spacer", onclick: async () => { await guarded(() => api("DELETE", `/api/v2/admin/swaps/${encodeURIComponent(w.id)}`)); swapDialog(ib, nodes); } }, t("delete")))));
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: `${ib.tag}-swap${list.length ? list.length + 1 : ""}` });
  const name = h("input", { value: "DARK SWAP", maxlength: "160" });
  const node = h("select", { required: true }, others.map((n) => h("option", { value: n.ID }, `${n.Name || n.ID} (${n.ID})`)));
  const sport = h("input", { type: "number", min: "1", max: "65535", required: true, value: "6801" });
  const eaddr = h("input", { required: true, placeholder: "ir.example.com" });
  const eport = h("input", { type: "number", min: "1", max: "65535", required: true, value: "6801" });
  const err = h("div", { class: "error" });
  const add = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    for (const f of [id, node, sport, eaddr, eport]) if (!f.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `/api/v2/admin/swaps/${encodeURIComponent(id.value)}`, { name: name.value, inbound_tag: ib.tag, source_node: node.value,
        source_port: +sport.value, entry_address: eaddr.value.trim(), entry_port: +eport.value, enabled: true }));
      swapDialog(ib, nodes);
    } catch (x) { err.textContent = x.message; }
  } }, t("add"));
  openModal(`${t("swap")} · ${ib.tag}`, h("div", { class: "form" },
    h("p", { class: "small muted" }, t("swapHint")),
    rows.length ? h("div", { class: "list" }, rows) : null,
    others.length ? h("div", { class: "form" },
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), id), h("label", {}, h("span", {}, t("name")), name)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("relayNode")), node), h("label", {}, h("span", {}, t("relayPort")), sport)),
      h("div", { class: "two" }, h("label", {}, h("span", {}, t("entryAddr")), eaddr), h("label", {}, h("span", {}, t("entryPort")), eport)), err)
      : h("p", { class: "muted" }, t("empty"))), others.length ? [add] : []);
}

async function inboundEditor(tag, nodes) {
  const raw = await api("GET", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}`);
  const pretty = (v) => { try { return JSON.stringify(typeof v === "string" ? JSON.parse(v) : v, null, 2); } catch { return String(v || ""); } };
  const node = h("select", {}, (nodes || []).map((n) => h("option", { value: n.ID, selected: n.ID === raw.node_id }, `${n.Name || n.ID} (${n.ID})`)));
  const tpl = h("textarea", { class: "code", rows: "16", spellcheck: "false", dir: "ltr" }, pretty(raw.template));
  const meta = h("textarea", { class: "code", rows: "8", spellcheck: "false", dir: "ltr" }, pretty(raw.meta));
  const en = h("input", { type: "checkbox", checked: raw.enabled });
  const err = h("div", { class: "error" });
  const parse = (el, name) => { try { return JSON.parse(el.value); } catch (x) { throw new Error(t("invalidJson", { x: name }) + `: ${x.message}`); } };
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    try {
      const body = { node_id: node.value, template: parse(tpl, "template"), meta: parse(meta, "meta"), enabled: en.checked };
      await guarded(() => api("PUT", `/api/v2/admin/inbounds/${encodeURIComponent(tag)}`, body));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  const d = openModal(`${t("advanced")} · ${tag}`, h("div", { class: "form" },
    h("p", { class: "small muted" }, t("advancedHint")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("node")), node), h("label", { class: "check" }, en, t("enabled"))),
    h("label", {}, h("span", {}, t("template")), tpl),
    h("label", {}, h("span", {}, t("metaJson")), meta), err), [save]);
  d.classList.add("wide");
  d.addEventListener("close", () => d.classList.remove("wide"), { once: true });
}

function certForm(id) {
  const idf = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: id || "", disabled: !!id, autofocus: !id });
  const cert = h("textarea", { class: "code", rows: "8", required: true, spellcheck: "false", dir: "ltr", placeholder: "-----BEGIN CERTIFICATE-----" });
  const key = h("textarea", { class: "code", rows: "6", required: true, spellcheck: "false", dir: "ltr", placeholder: "-----BEGIN PRIVATE KEY-----" });
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    for (const f of [idf, cert, key]) if (!f.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `/api/v2/admin/certs/${encodeURIComponent(idf.value)}`, { Cert: cert.value, Key: key.value }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(id ? `${t("edit")} · ${id}` : t("addCert"), h("div", { class: "form" },
    h("p", { class: "small muted" }, t("certHint")),
    h("label", {}, h("span", {}, t("tag")), idf), h("label", {}, h("span", {}, t("certPem")), cert), h("label", {}, h("span", {}, t("keyPem")), key), err), [save]);
}

function inboundForm(nodes, presets, certs = []) {
  const node = h("select", { required: true }, (nodes || []).map((n) => h("option", { value: n.ID }, `${n.Name || n.ID} (${n.ID})`)));
  const preset = h("select", {}, (presets || []).map((p) => h("option", { value: p }, p)));
  const tag = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", placeholder: "de-reality" });
  const port = h("input", { type: "number", min: "1", max: "65535", required: true, value: "443" });
  const sni = h("input", { value: "www.speedtest.net" });
  const path = h("input", {});
  const remark = h("input", { value: "Dark VPN" });
  const addr = h("input", { required: true, placeholder: "1.2.3.4" });
  const tunnel = h("input", { placeholder: "5.6.7.8:2400" });
  const cert = h("select", {}, h("option", { value: "" }, t("noCert")), certs.map((c) => h("option", { value: c.id }, `${c.id} · ${c.domains.join(", ")}`)));
  const certRow = h("label", { hidden: true }, h("span", {}, t("certFor")), cert);
  const syncPreset = () => {
    const tls = preset.value.includes("-tls-");
    certRow.hidden = !tls;
    if (tls && cert.value === "" && certs.length) { cert.value = certs[0].id; sni.value = certs[0].domains[0] || sni.value; }
  };
  preset.addEventListener("change", syncPreset);
  cert.addEventListener("change", () => { const c = certs.find((x) => x.id === cert.value); if (c && c.domains[0]) sni.value = c.domains[0]; });
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    for (const f of [tag, port, addr]) if (!f.reportValidity()) return;
    const endpoints = [{ name: "Direct", address: addr.value.trim(), port: +port.value }];
    if (tunnel.value.trim()) {
      const [a, p] = tunnel.value.trim().split(/:(?=\d+$)/);
      endpoints.push({ name: "Tunnel", address: a, port: +p || +port.value });
    }
    try {
      await guarded(() => api("POST", "/api/v2/admin/inbounds/preset", { tag: tag.value, node_id: node.value, preset: preset.value, port: +port.value,
        sni: sni.value.trim(), path: path.value.trim(), remark: remark.value.trim(), endpoints, cert: preset.value.includes("-tls-") ? cert.value : "" }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(`${t("add")} · ${t("inbounds")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("node")), node), h("label", {}, h("span", {}, t("preset")), preset)),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), tag), h("label", {}, h("span", {}, t("port")), port)),
    certRow,
    h("label", {}, h("span", {}, t("sni")), sni), h("label", {}, h("span", {}, t("path")), path),
    h("label", {}, h("span", {}, t("remark")), remark),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("directAddr")), addr), h("label", {}, h("span", {}, t("tunnelAddr")), tunnel)),
    err), [save]);
}

async function viewNodes() {
  const nodes = await api("GET", "/api/v2/admin/nodes");
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("nodes")), h("button", { class: "primary spacer", onclick: nodeForm }, t("addNode"))),
    h("div", { class: "list" }, (nodes || []).length ? nodes.map((n) => nodeCard(n, true)) : h("p", { class: "muted" }, t("empty"))));
}

function nodeForm() {
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", autofocus: true });
  const name = h("input", {});
  const out = h("div", {});
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!id.reportValidity()) return;
    try {
      const r = await guarded(() => api("POST", "/api/v2/admin/nodes", { ID: id.value, Name: name.value }));
      out.replaceChildren(h("p", { class: "small" }, t("installCmd")), h("code", {}, r.install), h("button", { type: "button", onclick: () => copy(r.install) }, t("copy")));
      save.disabled = true;
    } catch {}
  } }, t("save"));
  openModal(t("addNode"), h("div", { class: "form" },
    h("label", {}, h("span", {}, t("nodeId")), id), h("label", {}, h("span", {}, t("name")), name), out), [save]);
}

// Owners manage resellers at /api/v2/admin/reps; a reseller manages its own
// sub-sellers at /api/v2/rep/sellers with the same forms.
const repBase = () => (me && me.role === "rep" ? "/api/v2/rep/sellers" : "/api/v2/admin/reps");

async function viewSellers() {
  const [reps, inbounds] = await Promise.all([api("GET", "/api/v2/rep/sellers"), api("GET", "/api/v2/rep/inbounds")]);
  const cards = (reps || []).map((r) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, r.Name || r.ID), h("span", { class: "badge" }, r.ID),
      h("span", { class: `badge ${r.Enabled ? "good" : "bad"} spacer` }, t(r.Enabled ? "enabled" : "disabled"))),
    h("div", { class: "muted small" }, `${t("volumeCredit")}: ${bytes(r.VolumeCredit)} · ${t("units")}: ${fmtNum(r.UnlimitedUnits)} · ${t("clientsCount", { n: fmtNum(r.Clients) })}`),
    h("div", { class: "muted small" }, policySummary(r.Policy)),
    h("div", { class: "row" }, h("button", { onclick: () => creditForm(r) }, t("transferCredit")),
      h("button", { class: "ghost", onclick: () => policyForm(r, inbounds) }, t("policy")),
      h("a", { class: "btn ghost", href: `#/activity?rep=${encodeURIComponent(r.ID)}` }, t("activity")),
      h("button", { class: "ghost", onclick: () => repForm(r) }, t("edit")))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("sellers")), h("button", { class: "primary spacer", onclick: () => repForm(null) }, t("add"))),
    h("p", { class: "small muted" }, t("sellersHint")),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))));
}

async function viewReps() {
  const [reps, inbounds] = await Promise.all([api("GET", "/api/v2/admin/reps"), api("GET", "/api/v2/admin/inbounds")]);
  const cards = (reps || []).map((r) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, r.Name || r.ID), h("span", { class: "badge" }, r.ID),
      r.ParentID ? h("span", { class: "badge" }, `${t("subOf")} ${r.ParentID}`) : null,
      h("span", { class: `badge ${r.Enabled ? "good" : "bad"} spacer` }, t(r.Enabled ? "enabled" : "disabled"))),
    h("div", { class: "muted small" }, `${t("volumeCredit")}: ${bytes(r.VolumeCredit)} · ${t("units")}: ${fmtNum(r.UnlimitedUnits)} · ${t("clientsCount", { n: fmtNum(r.Clients) })}`),
    h("div", { class: "muted small" }, policySummary(r.Policy)),
    h("div", { class: "row" }, h("button", { onclick: () => creditForm(r) }, t("addCredit")),
      h("button", { class: "ghost", onclick: () => policyForm(r, inbounds) }, t("policy")),
      h("button", { class: "ghost", onclick: () => paymentForm(r) }, t("payment")),
      h("a", { class: "btn ghost", href: `#/activity?rep=${encodeURIComponent(r.ID)}` }, t("activity")),
      h("button", { class: "ghost", onclick: () => repForm(r) }, t("edit")))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("reps")), h("button", { class: "primary spacer", onclick: () => repForm(null) }, t("add"))),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))));
}

function repForm(r) {
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: r ? r.ID : "", disabled: !!r });
  const name = h("input", { value: r ? r.Name : "" });
  const en = h("input", { type: "checkbox", checked: r ? r.Enabled : true });
  const user = h("input", { autocomplete: "off", pattern: "[a-z0-9][a-z0-9._-]{0,62}" });
  const pass = h("input", { type: "password", autocomplete: "new-password", minlength: "10" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!id.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `${repBase()}/${encodeURIComponent(id.value)}`, { Name: name.value, Enabled: en.checked, Username: user.value, Password: pass.value }));
      document.getElementById("modal").close("save"); route();
    } catch {}
  } }, t("save"));
  openModal(r ? `${t("edit")} · ${r.ID}` : `${t("add")} · ${t("reps")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("repId")), id), h("label", {}, h("span", {}, t("name")), name)),
    h("label", { class: "check" }, en, t("enabled")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("loginUser")), user), h("label", {}, h("span", {}, t("loginPass")), pass))), [save]);
}

function creditForm(r) {
  const gb = h("input", { type: "number", step: "1", value: "0" });
  const units = h("input", { type: "number", step: "1", value: "0" });
  const note = h("input", {});
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try {
      await guarded(() => api("POST", `${repBase()}/${encodeURIComponent(r.ID)}/credit`, { VolumeGB: +gb.value || 0, Units: +units.value || 0, Note: note.value }));
      document.getElementById("modal").close("save"); route();
    } catch {}
  } }, t("save"));
  openModal(`${t(me.role === "rep" ? "transferCredit" : "addCredit")} · ${r.Name || r.ID}`, h("div", { class: "form" },
    me.role === "rep" ? h("p", { class: "small muted" }, t("transferHint")) : null,
    h("div", { class: "two" }, h("label", {}, h("span", {}, `${t("volumeCredit")} (GB)`), gb), h("label", {}, h("span", {}, t("units")), units)),
    h("p", { class: "small muted" }, t("unitsHint")), h("label", {}, h("span", {}, t("note")), note)), [save]);
}

function policySummary(p) {
  if (!p) return "";
  const parts = [];
  if (p.MaxClients) parts.push(`≤ ${fmtNum(p.MaxClients)} ${t("clients")}`);
  if (p.MaxDays) parts.push(`≤ ${fmtNum(p.MaxDays)}d`);
  if (p.MaxClientGB) parts.push(`≤ ${fmtNum(p.MaxClientGB)} GB`);
  if (!p.AllowUnlimited) parts.push(`✕ ${t("planUnlimited")}`);
  if (!p.CanDelete) parts.push(`✕ ${t("delete")}`);
  if (p.RefundUnused) parts.push(`↺ ${t("refunded")}`);
  if ((p.Inbounds || []).length) parts.push(p.Inbounds.join(", "));
  return parts.length ? `${t("policy")}: ${parts.join(" · ")}` : "";
}

function policyForm(r, inbounds) {
  const p = r.Policy || {};
  const num = (v) => h("input", { type: "number", min: "0", step: "1", value: v || 0 });
  const box = (v) => h("input", { type: "checkbox", checked: !!v });
  const f = { mc: num(p.MaxClients), md: num(p.MaxDays), mg: num(p.MaxClientGB), au: box(p.AllowUnlimited), cd: box(p.CanDelete), ru: box(p.RefundUnused), pg: num(p.PriceGB), pu: num(p.PriceUnit), mp: box(r.ManageProducts) };
  const allowed = new Set(p.Inbounds || []);
  const boxes = (inbounds || []).map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag, checked: allowed.has(ib.tag) }), `${ib.tag} · ${ib.node_id}`));
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try {
      await guarded(() => api("PUT", `${repBase()}/${encodeURIComponent(r.ID)}/policy`, {
        MaxClients: +f.mc.value || 0, MaxDays: +f.md.value || 0, MaxClientGB: +f.mg.value || 0,
        AllowUnlimited: f.au.checked, CanDelete: f.cd.checked, RefundUnused: f.ru.checked,
        Inbounds: boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value),
        PriceGB: +f.pg.value || 0, PriceUnit: +f.pu.value || 0 }));
      if (me.role === "owner" && f.mp.checked !== !!r.ManageProducts) await api("PUT", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/products`, { Enabled: f.mp.checked });
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  const lab = (k, el) => h("label", {}, h("span", {}, t(k)), el);
  openModal(`${t("policy")} · ${r.Name || r.ID}`, h("div", { class: "form" },
    h("div", { class: "two" }, lab("maxClients", f.mc), lab("maxDays", f.md)),
    lab("maxClientGB", f.mg),
    h("label", { class: "check" }, f.au, t("allowUnlimited")),
    h("label", { class: "check" }, f.cd, t("canDelete")),
    h("label", { class: "check" }, f.ru, t("refundUnused")),
    me.role === "owner" ? h("label", { class: "check" }, f.mp, t("manageProducts")) : null,
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("allowedInbounds")), boxes),
    me.role === "rep" ? null : h("div", { class: "two" }, lab("priceGB", f.pg), lab("priceUnit", f.pu)), err), [save]);
}

function paymentForm(r) {
  const amount = h("input", { type: "number", step: "1", required: true, autofocus: true });
  const note = h("input", {});
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    if (!amount.reportValidity()) return;
    try {
      await guarded(() => api("POST", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/payment`, { Amount: Math.round(+amount.value), Note: note.value }));
      document.getElementById("modal").close("save"); route();
    } catch {}
  } }, t("save"));
  openModal(`${t("payment")} · ${r.Name || r.ID}`, h("div", { class: "form" },
    h("label", {}, h("span", {}, t("amount")), amount), h("label", {}, h("span", {}, t("note")), note)), [save]);
}

function money(n) { return fmtNum(n); }

async function viewShop() {
  const owner = me.role === "owner";
  const [products, orders, inbounds, reps] = await Promise.all([
    api("GET", "/api/v2/shop/products"), api("GET", "/api/v2/shop/orders"),
    owner ? api("GET", "/api/v2/admin/inbounds") : api("GET", "/api/v2/rep/inbounds"),
    owner ? api("GET", "/api/v2/admin/reps") : Promise.resolve([])]);
  const canEdit = owner || me.manage_products;
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const planText = (p) => `${p.plan === "unlimited" ? t("planUnlimited") : `${fmtNum(p.gb)} GB`} · ${fmtNum(p.days)}d${p.max_ips ? ` · IP ≤ ${fmtNum(p.max_ips)}` : ""}`;
  const stCls = { published: "good", draft: "", archived: "bad", pending: "warn", fulfilled: "good", cancelled: "bad" };
  const cards = products.map((p) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, p.name), h("span", { class: "badge" }, p.id),
      owner && p.seller ? h("span", { class: "badge" }, `${t("seller")}: ${p.seller}`) : null,
      h("span", { class: `badge spacer ${stCls[p.status]}` }, t(`status_${p.status}`))),
    h("div", { class: "row small muted" }, h("span", {}, planText(p)), h("strong", { class: "spacer" }, money(p.price))),
    h("div", { class: "row" },
      p.status === "published" ? h("button", { onclick: () => orderForm(products, p.id) }, t("newOrder")) : null,
      canEdit && p.status !== "archived" ? h("button", { class: "ghost", onclick: () => productForm(p, inbounds, reps) }, t("edit")) : null)));
  const body = h("tbody", {}, orders.map((o) => h("tr", {},
    h("td", { class: "small" }, `#${o.id}`, h("div", { class: "muted" }, fmtTime(o.created_unix))),
    h("td", {}, o.product_name || o.product_id, owner && o.seller ? h("div", { class: "small muted" }, o.seller) : null),
    h("td", { class: "small" }, o.buyer || "—", o.note ? h("div", { class: "muted" }, o.note) : null),
    h("td", { class: "small mono" }, o.client_email ? `${o.renew ? "↻ " : ""}${o.client_email}` : t("newClient")),
    h("td", {}, money(o.amount)),
    h("td", {}, h("span", { class: `badge ${stCls[o.status]}` }, t(`status_${o.status}`))),
    h("td", {}, o.status === "pending" ? h("div", { class: "row" },
      h("button", { class: "primary", onclick: async () => {
        const r = await guarded(() => api("POST", `/api/v2/shop/orders/${o.id}/fulfill`));
        toast(t("delivered", { x: r.client_email })); route();
      } }, t("fulfill")),
      h("button", { class: "ghost danger", onclick: async () => { await guarded(() => api("POST", `/api/v2/shop/orders/${o.id}/cancel`)); route(); } }, t("cancelOrder"))) : null))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("shop")),
      h("span", { class: "spacer" }), canEdit ? h("button", { onclick: () => productForm(null, inbounds, reps) }, `${t("add")} · ${t("product")}`) : null,
      products.some((p) => p.status === "published") ? h("button", { class: "primary", onclick: () => orderForm(products) }, t("newOrder")) : null),
    h("p", { class: "small muted" }, t("shopHint")),
    h("h2", {}, t("products")),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted small" }, t("empty"))),
    h("h2", { class: "section" }, t("orders")),
    orders.length ? h("div", { class: "table-wrap" }, h("table", { class: "table" },
      h("thead", {}, h("tr", {}, ["#", t("product"), t("buyer").split(" (")[0], t("clients"), t("price"), t("statusLabel"), ""].map((x) => h("th", {}, x)))), body))
      : h("p", { class: "muted small" }, t("empty")));
}

function productForm(p, inbounds, reps) {
  const owner = me.role === "owner";
  const v = p || { id: "", name: "", plan: "volume", gb: 50, days: 30, max_ips: 2, inbounds: [], price: 0, status: "published", first_use: true, seller: "" };
  const id = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", value: v.id, disabled: !!p });
  const name = h("input", { required: true, maxlength: "120", value: v.name });
  const plan = h("select", {}, h("option", { value: "volume", selected: v.plan === "volume" }, t("planVolume")), h("option", { value: "unlimited", selected: v.plan === "unlimited" }, t("planUnlimited")));
  const gb = h("input", { type: "number", min: "1", value: v.gb || 50 });
  const days = h("input", { type: "number", min: "1", max: "3650", required: true, value: v.days });
  const ips = h("input", { type: "number", min: "0", value: v.max_ips });
  const price = h("input", { type: "number", min: "0", step: "1", value: v.price });
  const first = h("input", { type: "checkbox", checked: v.first_use });
  const status = h("select", {}, ["draft", "published", "archived"].map((x) => h("option", { value: x, selected: v.status === x }, t(`status_${x}`))));
  const seller = owner ? h("select", {}, h("option", { value: "" }, t("ownerSelf")), (reps || []).map((r) => h("option", { value: r.ID, selected: v.seller === r.ID }, r.Name || r.ID))) : null;
  const boxes = (inbounds || []).map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag, checked: (v.inbounds || []).includes(ib.tag) || (!p && inbounds.length === 1) }), `${ib.tag} · ${ib.node_id}`));
  const gbRow = h("label", {}, h("span", {}, "GB"), gb);
  const sync = () => { gbRow.hidden = plan.value === "unlimited"; };
  plan.addEventListener("change", sync); sync();
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    for (const f of [id, name, days]) if (!f.reportValidity()) return;
    try {
      await guarded(() => api("PUT", `/api/v2/shop/products/${encodeURIComponent(id.value)}`, {
        name: name.value, plan: plan.value, gb: +gb.value || 0, days: +days.value, max_ips: +ips.value || 0, price: Math.round(+price.value || 0),
        status: status.value, first_use: first.checked, seller: seller ? seller.value : "",
        inbounds: boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value) }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(p ? `${t("edit")} · ${p.name}` : `${t("add")} · ${t("product")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), id), h("label", {}, h("span", {}, t("name")), name)),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("plan")), plan), gbRow),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("days").split(" (")[0]), days), h("label", {}, h("span", {}, t("maxIPs")), ips)),
    h("label", { class: "check" }, first, t("firstUse")),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("price")), price), h("label", {}, h("span", {}, t("statusLabel")), status)),
    seller ? h("label", {}, h("span", {}, t("seller")), seller) : null,
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("assignInbounds")), boxes), err), [save]);
}

async function orderForm(products, preselect) {
  const list = products.filter((p) => p.status === "published");
  const clients = await api("GET", "/api/v2/admin/clients");
  const prod = h("select", {}, list.map((p) => h("option", { value: p.id, selected: p.id === preselect }, `${p.name} · ${money(p.price)}`)));
  const buyer = h("input", { maxlength: "200", autofocus: true });
  const renew = h("select", {}, h("option", { value: "" }, t("newClient")), clients.map((c) => h("option", { value: c.Email }, c.Email)));
  const note = h("input", { maxlength: "1000" });
  const err = h("div", { class: "error" });
  const mk = (deliver) => h("button", { class: deliver ? "primary" : "", value: "save", onclick: async (e) => {
    e.preventDefault(); err.textContent = "";
    try {
      const r = await api("POST", "/api/v2/shop/orders", { product_id: prod.value, buyer: buyer.value, client_email: renew.value, note: note.value });
      if (deliver) { const d = await api("POST", `/api/v2/shop/orders/${r.id}/fulfill`); toast(t("delivered", { x: d.client_email })); } else toast(t("saved"));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, deliver ? `${t("save")} + ${t("fulfill")}` : t("save"));
  openModal(t("newOrder"), h("div", { class: "form" },
    h("label", {}, h("span", {}, t("product")), prod), h("label", {}, h("span", {}, t("buyer")), buyer),
    h("label", {}, h("span", {}, t("renewClient")), renew), h("label", {}, h("span", {}, t("note")), note), err), [mk(false), mk(true)]);
}


async function viewFinance() {
  const days = new URLSearchParams(location.hash.split("?")[1] || "").get("days") ?? "30";
  const rows = await api("GET", `/api/v2/admin/finance?days=${encodeURIComponent(days)}`);
  const period = h("select", { onchange: (e) => { location.hash = `#/finance?days=${e.target.value}`; } },
    [["7", t("last7")], ["30", t("last30")], ["0", t("allTime")]].map(([v, l]) => h("option", { value: v, selected: v === days }, l)));
  const sum = (k) => rows.reduce((a, r) => a + r[k], 0);
  const stat = (k, v, cls = "") => h("div", { class: `card stat ${cls}` }, h("div", { class: "v" }, v), h("div", { class: "k" }, t(k)));
  const head = ["reps", "sold", "refunded", "granted", "revenue", "paid", "debt", "balance"];
  const table = h("div", { class: "table-wrap" }, h("table", { class: "table" },
    h("thead", {}, h("tr", {}, head.map((k) => h("th", { scope: "col" }, t(k))))),
    h("tbody", {}, rows.map((r) => h("tr", {},
      h("th", { scope: "row" }, h("a", { href: `#/activity?rep=${encodeURIComponent(r.RepID)}` }, r.Name || r.RepID), h("div", { class: "small muted" }, t("clientsCount", { n: fmtNum(r.Clients) }))),
      h("td", {}, bytes(r.SoldBytes), r.SoldUnits ? h("div", { class: "small muted" }, `${fmtNum(r.SoldUnits)} ${t("units")}`) : null),
      h("td", {}, bytes(r.RefundBytes), r.RefundUnits ? h("div", { class: "small muted" }, `${fmtNum(r.RefundUnits)} ${t("units")}`) : null),
      h("td", {}, bytes(r.GrantedBytes), r.GrantedUnits ? h("div", { class: "small muted" }, `${fmtNum(r.GrantedUnits)} ${t("units")}`) : null),
      h("td", {}, money(r.Revenue)), h("td", {}, money(r.Paid)),
      h("td", { class: r.Debt > 0 ? "warn-text" : "" }, money(r.Debt)),
      h("td", {}, bytes(r.VolumeCredit), h("div", { class: "small muted" }, `${fmtNum(r.Units)} ${t("units")}`)))))));
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("finance")), h("label", { class: "spacer inline" }, h("span", { class: "small muted" }, t("period")), period)),
    h("div", { class: "grid" }, stat("sold", bytes(sum("SoldBytes"))), stat("revenue", money(sum("Revenue"))), stat("paid", money(sum("Paid"))),
      stat("debt", money(sum("Debt")), sum("Debt") > 0 ? "warn" : "")),
    rows.length ? table : h("p", { class: "muted" }, t("empty")));
}

async function viewActivity() {
  const q = new URLSearchParams(location.hash.split("?")[1] || "");
  const rep = q.get("rep") || "";
  const owner = me.role === "owner";
  const [first, reps] = await Promise.all([
    api("GET", `/api/v2/rep/ledger?rep=${encodeURIComponent(rep)}`),
    owner ? api("GET", "/api/v2/admin/reps") : Promise.resolve([]),
  ]);
  const fmtTime = (u) => new Intl.DateTimeFormat(lang === "fa" ? "fa-IR" : "en-GB", { dateStyle: "short", timeStyle: "short" }).format(new Date(u * 1000));
  const delta = (e) => [
    e.VolumeDelta ? `${e.VolumeDelta > 0 ? "+" : "−"}${bytes(Math.abs(e.VolumeDelta))}` : "",
    e.UnitsDelta ? `${e.UnitsDelta > 0 ? "+" : "−"}${fmtNum(Math.abs(e.UnitsDelta))} ${t("units")}` : "",
    e.Amount ? `${t("amount")} ${money(e.Amount)}` : ""].filter(Boolean).join(" · ");
  const cls = { charge: "", refund: "good", adjust: "good", delete: "bad", payment: "good", transfer: "warn" };
  const body = h("tbody", {});
  const add = (list) => body.append(...list.map((e) => h("tr", {},
    h("td", { class: "small" }, fmtTime(e.Unix)),
    owner && !rep ? h("td", {}, e.RepID) : null,
    h("td", {}, h("span", { class: `badge ${cls[e.Kind] || ""}` }, t(`kind_${e.Kind}`)), e.ClientEmail ? ` ${e.ClientEmail}` : ""),
    h("td", { class: "mono small" }, delta(e)),
    h("td", { class: "small muted" }, [e.Actor, e.Note].filter(Boolean).join(" — ")))));
  add(first);
  let last = first.at(-1);
  const more = h("button", { class: "ghost", hidden: first.length < 100, onclick: async () => {
    const next = await api("GET", `/api/v2/rep/ledger?rep=${encodeURIComponent(rep)}&before=${last.ID}`);
    add(next); last = next.at(-1) || last; more.hidden = next.length < 100;
  } }, t("more"));
  const repSel = owner ? h("select", { onchange: (e) => { location.hash = `#/activity${e.target.value ? `?rep=${encodeURIComponent(e.target.value)}` : ""}`; } },
    h("option", { value: "" }, t("all")), reps.map((r) => h("option", { value: r.ID, selected: r.ID === rep }, r.Name || r.ID))) : null;
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("activity")), repSel ? h("span", { class: "spacer" }, repSel) : null),
    first.length ? h("div", { class: "table-wrap" }, h("table", { class: "table" },
      h("thead", {}, h("tr", {}, h("th", {}, t("when")), owner && !rep ? h("th", {}, t("reps")) : null, h("th", {}, t("what")), h("th", {}, t("credit")), h("th", {}, t("who")))),
      body)) : h("p", { class: "muted" }, t("empty")), more);
}

// Daily traffic as a neon line chart: grid, glowing lines, soft area fill.
function barsSvg(days, W = 600) {
  const NS = "http://www.w3.org/2000/svg", H = 190, P = 8, L = 0;
  const el = (n, a) => { const x = document.createElementNS(NS, n); for (const [k, v] of Object.entries(a)) x.setAttribute(k, v); return x; };
  const svg = el("svg", { viewBox: `0 0 ${W} ${H + 20}`, class: "bars", role: "img", "aria-label": t("usageChart") });
  const id = `c${++markSeq}`;
  const defs = el("defs", {});
  const glow = el("filter", { id: `${id}g`, x: "-10%", y: "-30%", width: "120%", height: "160%" });
  glow.append(el("feGaussianBlur", { stdDeviation: "3.5", result: "b" }));
  const merge = el("feMerge", {}); merge.append(el("feMergeNode", { in: "b" }), el("feMergeNode", { in: "SourceGraphic" })); glow.append(merge);
  const area = (col, key) => { const g = el("linearGradient", { id: `${id}${key}`, x1: 0, y1: 0, x2: 0, y2: 1 });
    g.append(el("stop", { offset: "0", "stop-color": col, "stop-opacity": ".35" }), el("stop", { offset: "1", "stop-color": col, "stop-opacity": "0" })); return g; };
  defs.append(glow, area("#2de2e6", "d"), area("#9b5cff", "u"));
  svg.append(defs);
  if (!days.length) return svg;
  const top = Math.max(1, ...days.map((d) => Math.max(d.Down, d.Up))) * 1.12;
  const x = (i) => L + (days.length === 1 ? W / 2 : (i / (days.length - 1)) * (W - L - 2 * P) + P);
  const y = (v) => H - (v / top) * (H - P);
  for (let k = 0; k <= 4; k++) {
    const yy = P + ((H - P) * k) / 4;
    svg.append(el("line", { x1: 0, x2: W, y1: yy, y2: yy, class: "grid-line" }));
  }
  const series = (key, col) => {
    const pts = days.map((d, i) => [x(i), y(d[key])]);
    const line = pts.map(([a, b]) => `${a.toFixed(1)},${b.toFixed(1)}`).join(" ");
    svg.append(el("polygon", { points: `${pts[0][0]},${H} ${line} ${pts.at(-1)[0]},${H}`, fill: `url(#${id}${key === "Down" ? "d" : "u"})` }));
    svg.append(el("polyline", { points: line, fill: "none", stroke: col, "stroke-width": 2.2, "stroke-linejoin": "round", "stroke-linecap": "round", filter: `url(#${id}g)`, "vector-effect": "non-scaling-stroke" }));
    const [lx, ly] = pts.at(-1);
    svg.append(el("circle", { cx: lx, cy: ly, r: 4, fill: col, filter: `url(#${id}g)` }));
  };
  series("Up", "#9b5cff");
  series("Down", "#2de2e6");
  const step = Math.max(1, Math.round(days.length / 6));
  days.forEach((d, i) => {
    const hit = el("rect", { x: x(i) - W / days.length / 2, y: 0, width: W / days.length, height: H, class: "bar-hit" });
    const title = el("title", {}); title.textContent = `${d.Day}: ↓ ${bytes(d.Down)} · ↑ ${bytes(d.Up)}`;
    hit.append(title); svg.append(hit);
    if ((days.length - 1 - i) % step === 0) {
      const lab = el("text", { x: x(i), y: H + 15, "text-anchor": "middle", class: "bar-label" }); lab.textContent = d.Day.slice(5); svg.append(lab);
    }
  });
  return svg;
}

function legend() {
  return h("span", { class: "spacer legend" }, h("i", { class: "sw down" }), `↓ ${t("inRate")}`, h("i", { class: "sw up" }), `↑ ${t("outRate")}`);
}

async function usageChart(c) {
  const days = await api("GET", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/daily?days=30`);
  const sum = days.reduce((a, d) => a + d.Up + d.Down, 0);
  openModal(`${t("usageChart")} · ${c.Email}`, h("div", { class: "form" },
    h("div", { class: "row small" }, h("strong", {}, t("totalLast30", { v: bytes(sum) })), legend()), barsSvg(days)));
}

function viewSettings() {
  const langSel = h("select", { onchange: (e) => { lang = e.target.value; safeSet("lang", lang); applyLang(); route(); } },
    h("option", { value: "en", selected: lang === "en" }, "English"), h("option", { value: "fa", selected: lang === "fa" }, "فارسی"));
  const cur = safeGet("color-scheme") || "light dark";
  const themeSel = h("select", { onchange: (e) => {
    const v = e.target.value;
    safeSet("color-scheme", v === "light dark" ? null : v);
    document.querySelector('meta[name="color-scheme"]').content = v;
  } }, [["light dark", t("system")], ["light", t("light")], ["dark", t("dark")]].map(([v, l]) => h("option", { value: v, selected: v === cur }, l)));
  const oldP = h("input", { type: "password", autocomplete: "current-password", required: true });
  const newP = h("input", { type: "password", autocomplete: "new-password", required: true, minlength: "10" });
  const pwErr = h("div", { class: "error" });
  const pwForm = h("form", { class: "card form", onsubmit: async (e) => {
    e.preventDefault(); pwErr.textContent = "";
    try { await api("POST", "/api/v2/auth/password", { Old: oldP.value, New: newP.value }); toast(t("saved")); location.hash = "#/login"; }
    catch (x) { pwErr.textContent = x.message; }
  } }, h("h2", {}, t("changePassword")), h("label", {}, h("span", {}, t("oldPassword")), oldP), h("label", {}, h("span", {}, t("newPassword")), newP), pwErr,
    h("button", { class: "primary", type: "submit" }, t("save")));
  const backupCard = me.role === "owner" ? h("div", { class: "card form" }, h("h2", {}, t("backup")), h("p", { class: "small muted" }, t("backupHint")),
    h("a", { class: "btn", href: "/api/v2/admin/backup", download: "" }, t("download"))) : null;
  const tgOut = h("div", { class: "form" });
  const tgCard = me.role === "rep" ? h("div", { class: "card form" }, h("h2", {}, t("telegram")), h("p", { class: "small muted" }, t("telegramHint")),
    tgOut, h("button", { onclick: async () => {
      const r = await guarded(() => api("POST", "/api/v2/rep/telegram"));
      tgOut.replaceChildren(r.link ? h("a", { class: "btn", href: r.link, target: "_blank", rel: "noopener" }, t("openBot")) : null,
        h("code", {}, `/start rep_${r.code}`), h("p", { class: "small muted" }, t("codeExpires")));
    } }, t("connectTelegram"))) : null;
  return h("div", { class: "form" }, h("h1", {}, t("settings")), pwForm, tgCard, backupCard,
    h("div", { class: "card form" },
      h("label", {}, h("span", {}, t("language")), langSel),
      h("label", {}, h("span", {}, t("theme")), themeSel),
      h("button", { class: "danger", onclick: async () => { await api("POST", "/api/v2/auth/logout"); location.hash = "#/login"; } }, t("logout"))));
}

function applyLang() {
  document.documentElement.lang = lang;
  document.documentElement.dir = lang === "fa" ? "rtl" : "ltr";
}

let routeSeq = 0;
async function route() {
  const seq = ++routeSeq; // a slower, older navigation must not overwrite a newer one
  const app = document.getElementById("app");
  const name = (location.hash.replace(/^#\//, "") || "dashboard").split("?")[0];
  if (name === "login") { app.replaceChildren(viewLogin()); return; }
  try {
    me = await api("GET", "/api/v2/auth/me");
  } catch { return; }
  routes = routesFor(me);
  const first = Object.keys(routes)[0];
  const hidden = { client: [viewClient, "clients"] };
  const view = hidden[name] ? hidden[name][0] : routes[name] || routes[first];
  try {
    const content = await view();
    if (seq !== routeSeq) return;
    app.replaceChildren(shell(hidden[name] ? hidden[name][1] : routes[name] ? name : first, content));
  } catch (e) {
    if (e.message !== "unauthorized") app.replaceChildren(shell(name, h("p", { class: "error" }, t("failed", { e: e.message }))));
  }
}

window.addEventListener("hashchange", route);
window.addEventListener("storage", (e) => {
  if (e.key === "color-scheme") document.querySelector('meta[name="color-scheme"]').content = e.newValue ?? "light dark";
});
applyLang();
route();
setInterval(() => { if (!document.hidden && !document.getElementById("modal").open && /dashboard|clients|nodes/.test(location.hash || "#/dashboard")) route(); }, 15000);
