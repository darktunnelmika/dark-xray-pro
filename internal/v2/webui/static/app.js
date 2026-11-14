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
const allRoutes = { dashboard: viewDashboard, clients: viewClients, groups: viewGroups, inbounds: viewInbounds, nodes: viewNodes, reps: viewReps, finance: viewFinance, activity: viewActivity, settings: viewSettings };
let me = null;
let routes = allRoutes;
function routesFor(m) {
  if (m && m.role === "rep") return { clients: viewClients, activity: viewActivity, settings: viewSettings };
  return allRoutes;
}

function shell(active, content) {
  const tabs = Object.keys(routes).map((r) => h("a", { href: `#/${r}`, "aria-current": r === active ? "page" : null }, t(r)));
  return h("div", { class: "shell" },
    h("header", { class: "top" }, h("span", { class: "brand" }, "Dark VPN"),
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
  return h("div", { class: "login" }, h("div", { class: "card" }, h("h1", {}, "Dark VPN"), form));
}

async function viewDashboard() {
  const [o, nodes] = await Promise.all([api("GET", "/api/v2/admin/overview"), api("GET", "/api/v2/admin/nodes")]);
  const stat = (k, v) => h("div", { class: "card stat" }, h("div", { class: "v" }, v), h("div", { class: "k" }, t(k)));
  return h("div", {},
    h("h1", {}, t("dashboard")),
    h("div", { class: "grid" },
      stat("nodesOnline", `${fmtNum(o.NodesOnline)} / ${fmtNum(o.Nodes)}`),
      stat("activeClients", `${fmtNum(o.ClientsActive)} / ${fmtNum(o.Clients)}`),
      stat("onlineUsers", fmtNum(o.OnlineUsers)),
      stat("trafficToday", bytes(o.TrafficToday)),
      stat("trafficTotal", bytes(o.TrafficTotal))),
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

function meter(label, pct, text) {
  const p = Math.max(0, Math.min(100, pct || 0));
  return h("div", { class: "meter" }, h("div", { class: "row small" }, h("span", {}, label), h("span", { class: "muted spacer" }, text)),
    h("div", { class: "bar" }, h("i", { class: p > 90 ? "bad" : p > 75 ? "warn" : "", style: `inline-size:${p}%` })));
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
      h("span", { class: "name" }, c.Email), h("span", { class: `badge ${cls}` }, status)),
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
  const [list, nodes, presets] = await Promise.all([api("GET", "/api/v2/admin/inbounds"), api("GET", "/api/v2/admin/nodes"), api("GET", "/api/v2/admin/presets")]);
  const cards = (list || []).map((ib) => {
    let meta = {};
    try { meta = typeof ib.meta === "string" ? JSON.parse(ib.meta) : ib.meta || {}; } catch {}
    return h("div", { class: "card item" },
      h("div", { class: "head" }, h("span", { class: `dot ${ib.enabled ? "on" : ""}` }), h("span", { class: "name" }, ib.tag), h("span", { class: "badge" }, `${ib.protocol} · ${ib.network} · ${ib.security}`),
        ib.enabled ? null : h("span", { class: "badge bad" }, t("disabled")),
        h("span", { class: "badge spacer" }, `${t("node")}: ${ib.node_id}`)),
      h("div", { class: "muted small" }, `${t("port")} ${ib.port} · ${t("clientsCount", { n: fmtNum(ib.clients) })} · ${t("trafficToday")}: ${bytes(ib.traffic_today || 0)} · ${t("trafficTotal")}: ${bytes(ib.traffic_total || 0)}`),
      h("div", { class: "small mono" }, (meta.endpoints || []).map((e) => `${e.name || ""} ${e.address}:${e.port}`).join("  ·  ")),
      h("div", { class: "row" },
        h("button", { onclick: async () => { await guarded(() => api("POST", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}/enabled`, { Enabled: !ib.enabled })); route(); } }, t(ib.enabled ? "disable" : "enable")),
        h("button", { class: "ghost", onclick: () => inboundEditor(ib.tag, nodes) }, t("advanced")),
        h("button", { class: "ghost danger spacer", onclick: async () => {
        if (await confirmAction(t("confirmDelete", { x: ib.tag }))) { await guarded(() => api("DELETE", `/api/v2/admin/inbounds/${encodeURIComponent(ib.tag)}`)); route(); }
      } }, t("delete"))));
  });
  return h("div", {},
    h("div", { class: "row" }, h("h1", {}, t("inbounds")), h("button", { class: "primary spacer", onclick: () => inboundForm(nodes, presets) }, t("add"))),
    h("div", { class: "list" }, cards.length ? cards : h("p", { class: "muted" }, t("empty"))));
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

function inboundForm(nodes, presets) {
  const node = h("select", { required: true }, (nodes || []).map((n) => h("option", { value: n.ID }, `${n.Name || n.ID} (${n.ID})`)));
  const preset = h("select", {}, (presets || []).map((p) => h("option", { value: p }, p)));
  const tag = h("input", { required: true, pattern: "[a-z0-9][a-z0-9._-]{0,62}", placeholder: "de-reality" });
  const port = h("input", { type: "number", min: "1", max: "65535", required: true, value: "443" });
  const sni = h("input", { value: "www.speedtest.net" });
  const path = h("input", {});
  const remark = h("input", { value: "Dark VPN" });
  const addr = h("input", { required: true, placeholder: "1.2.3.4" });
  const tunnel = h("input", { placeholder: "5.6.7.8:2400" });
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
        sni: sni.value.trim(), path: path.value.trim(), remark: remark.value.trim(), endpoints }));
      document.getElementById("modal").close("save"); route();
    } catch (x) { err.textContent = x.message; }
  } }, t("save"));
  openModal(`${t("add")} · ${t("inbounds")}`, h("div", { class: "form" },
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("node")), node), h("label", {}, h("span", {}, t("preset")), preset)),
    h("div", { class: "two" }, h("label", {}, h("span", {}, t("tag")), tag), h("label", {}, h("span", {}, t("port")), port)),
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

async function viewReps() {
  const [reps, inbounds] = await Promise.all([api("GET", "/api/v2/admin/reps"), api("GET", "/api/v2/admin/inbounds")]);
  const cards = (reps || []).map((r) => h("div", { class: "card item" },
    h("div", { class: "head" }, h("span", { class: "name" }, r.Name || r.ID), h("span", { class: "badge" }, r.ID),
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
      await guarded(() => api("PUT", `/api/v2/admin/reps/${encodeURIComponent(id.value)}`, { Name: name.value, Enabled: en.checked, Username: user.value, Password: pass.value }));
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
      await guarded(() => api("POST", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/credit`, { VolumeGB: +gb.value || 0, Units: +units.value || 0, Note: note.value }));
      document.getElementById("modal").close("save"); route();
    } catch {}
  } }, t("save"));
  openModal(`${t("addCredit")} · ${r.Name || r.ID}`, h("div", { class: "form" },
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
  const f = { mc: num(p.MaxClients), md: num(p.MaxDays), mg: num(p.MaxClientGB), au: box(p.AllowUnlimited), cd: box(p.CanDelete), ru: box(p.RefundUnused), pg: num(p.PriceGB), pu: num(p.PriceUnit) };
  const allowed = new Set(p.Inbounds || []);
  const boxes = (inbounds || []).map((ib) => h("label", { class: "check" }, h("input", { type: "checkbox", value: ib.tag, checked: allowed.has(ib.tag) }), `${ib.tag} · ${ib.node_id}`));
  const err = h("div", { class: "error" });
  const save = h("button", { class: "primary", value: "save", onclick: async (e) => {
    e.preventDefault();
    try {
      await guarded(() => api("PUT", `/api/v2/admin/reps/${encodeURIComponent(r.ID)}/policy`, {
        MaxClients: +f.mc.value || 0, MaxDays: +f.md.value || 0, MaxClientGB: +f.mg.value || 0,
        AllowUnlimited: f.au.checked, CanDelete: f.cd.checked, RefundUnused: f.ru.checked,
        Inbounds: boxes.map((b) => b.querySelector("input")).filter((i) => i.checked).map((i) => i.value),
        PriceGB: +f.pg.value || 0, PriceUnit: +f.pu.value || 0 }));
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
    h("fieldset", { class: "form", style: "border:0;padding:0;margin:0" }, h("legend", { class: "small muted" }, t("allowedInbounds")), boxes),
    h("div", { class: "two" }, lab("priceGB", f.pg), lab("priceUnit", f.pu)), err), [save]);
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
  const cls = { charge: "", refund: "good", adjust: "good", delete: "bad", payment: "good" };
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

// Daily usage as SVG bars (download stacked on upload), built with the DOM.
async function usageChart(c) {
  const days = await api("GET", `/api/v2/admin/clients/${encodeURIComponent(c.Email)}/daily?days=30`);
  const NS = "http://www.w3.org/2000/svg", W = 600, H = 180, P = 4;
  const el = (n, a) => { const x = document.createElementNS(NS, n); for (const [k, v] of Object.entries(a)) x.setAttribute(k, v); return x; };
  const svg = el("svg", { viewBox: `0 0 ${W} ${H + 18}`, class: "bars", role: "img", "aria-label": t("usageChart") });
  const top = Math.max(1, ...days.map((d) => d.Up + d.Down));
  const bw = W / days.length;
  days.forEach((d, i) => {
    const total = d.Up + d.Down, hd = (d.Down / top) * (H - P), hu = (d.Up / top) * (H - P);
    const g = el("g", {});
    const title = el("title", {}); title.textContent = `${d.Day}: ${bytes(total)} (↓ ${bytes(d.Down)} · ↑ ${bytes(d.Up)})`;
    g.append(title,
      el("rect", { x: i * bw + 1, y: H - hd, width: Math.max(1, bw - 2), height: hd, class: "bar-down", rx: 2 }),
      el("rect", { x: i * bw + 1, y: H - hd - hu, width: Math.max(1, bw - 2), height: hu, class: "bar-up", rx: 2 }));
    svg.append(g);
    if (i % 7 === days.length % 7 || i === days.length - 1) {
      const lab = el("text", { x: i * bw + bw / 2, y: H + 14, "text-anchor": "middle", class: "bar-label" }); lab.textContent = d.Day.slice(5); svg.append(lab);
    }
  });
  const sum = days.reduce((a, d) => a + d.Up + d.Down, 0);
  openModal(`${t("usageChart")} · ${c.Email}`, h("div", { class: "form" },
    h("div", { class: "row small" }, h("strong", {}, t("totalLast30", { v: bytes(sum) })),
      h("span", { class: "spacer legend" }, h("i", { class: "sw down" }), `↓ ${t("inRate")}`, h("i", { class: "sw up" }), `↑ ${t("outRate")}`)),
    svg));
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
  return h("div", { class: "form" }, h("h1", {}, t("settings")), pwForm, backupCard,
    h("div", { class: "card form" },
      h("label", {}, h("span", {}, t("language")), langSel),
      h("label", {}, h("span", {}, t("theme")), themeSel),
      h("button", { class: "danger", onclick: async () => { await api("POST", "/api/v2/auth/logout"); location.hash = "#/login"; } }, t("logout"))));
}

function applyLang() {
  document.documentElement.lang = lang;
  document.documentElement.dir = lang === "fa" ? "rtl" : "ltr";
}

async function route() {
  const app = document.getElementById("app");
  const name = (location.hash.replace(/^#\//, "") || "dashboard").split("?")[0];
  if (name === "login") { app.replaceChildren(viewLogin()); return; }
  try {
    me = await api("GET", "/api/v2/auth/me");
  } catch { return; }
  routes = routesFor(me);
  const first = Object.keys(routes)[0];
  const view = routes[name] || routes[first];
  try {
    const content = await view();
    app.replaceChildren(shell(routes[name] ? name : first, content));
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
