"use strict";
// Dark VPN store inside Telegram. Every call carries Telegram's signed
// initData; the HUB checks it against the bot token.

const tg = window.Telegram && window.Telegram.WebApp;
const initData = (tg && tg.initData) || new URLSearchParams(location.hash.slice(1)).get("tgWebAppData") || "";
if (tg) {
  try { tg.ready(); tg.expand(); } catch {}
  if (tg.colorScheme === "light") document.documentElement.classList.add("light");
  try { tg.setHeaderColor(tg.colorScheme === "light" ? "#eef0fb" : "#060912"); tg.setBackgroundColor(tg.colorScheme === "light" ? "#eef0fb" : "#060912"); } catch {}
}

const app = document.getElementById("app");
const tabs = document.getElementById("tabs");
let state = null;
let tab = "services";

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "class") el.className = v;
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const c of kids.flat()) if (c != null && c !== false) el.append(c instanceof Node ? c : String(c));
  return el;
}
const SVG = "http://www.w3.org/2000/svg";
function s(tag, attrs) { const el = document.createElementNS(SVG, tag); for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v); return el; }

const fa = new Intl.NumberFormat("fa-IR", { maximumFractionDigits: 1 });
const num = (n) => fa.format(n);
const ltr = (t) => h("span", { class: "ltr" }, t);
function size(b) {
  if (b < 0) return "نامحدود";
  const u = ["بایت", "کیلوبایت", "مگابایت", "گیگابایت", "ترابایت"];
  let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return `${num(b)} ${u[i]}`;
}
const toman = (n) => new Intl.NumberFormat("fa-IR").format(n);
const date = (u) => new Intl.DateTimeFormat("fa-IR-u-ca-persian", { dateStyle: "medium" }).format(new Date(u * 1000));

function toast(msg) {
  const t = document.getElementById("toast");
  t.textContent = msg; t.hidden = false;
  clearTimeout(toast.t); toast.t = setTimeout(() => { t.hidden = true; }, 2400);
}
function haptic(kind) { try { tg && tg.HapticFeedback.notificationOccurred(kind); } catch {} }

async function api(method, path, body) {
  const res = await fetch("api/" + path, { method, headers: { "X-Init-Data": initData, "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
  const text = await res.text();
  if (!res.ok) throw Object.assign(new Error(text.trim() || res.statusText), { status: res.status });
  return text ? JSON.parse(text) : null;
}

async function copy(text) {
  try { await navigator.clipboard.writeText(text); toast("کپی شد"); haptic("success"); }
  catch { prompt("کپی کنید:", text); }
}

// ---------- pieces ----------
function ring(sv) {
  const r = 40, c = 2 * Math.PI * r;
  const unlimited = sv.total < 0;
  const frac = unlimited ? 1 : Math.min(1, sv.used / Math.max(1, sv.total));
  const svg = s("svg", { viewBox: "0 0 96 96", "aria-hidden": "true" });
  const defs = s("defs", {});
  const g = s("linearGradient", { id: "neon", x1: "0", y1: "0", x2: "1", y2: "1" });
  g.append(s("stop", { offset: "0", "stop-color": getComputedStyle(document.documentElement).getPropertyValue("--cyan").trim() }),
    s("stop", { offset: "1", "stop-color": getComputedStyle(document.documentElement).getPropertyValue("--violet").trim() }));
  defs.append(g);
  const val = s("circle", { class: "val", cx: "48", cy: "48", r: String(r), "stroke-dasharray": String(c), "stroke-dashoffset": String(c) });
  svg.append(defs, s("circle", { class: "track", cx: "48", cy: "48", r: String(r) }), val);
  // The ring shows what is LEFT: a full ring is an untouched plan.
  const left = unlimited ? null : Math.max(0, sv.total - sv.used);
  requestAnimationFrame(() => requestAnimationFrame(() => val.setAttribute("stroke-dashoffset", String(unlimited ? 0 : c * frac))));
  const bad = sv.status !== "active";
  return h("div", { class: "ring" + (bad ? " bad" : ""), role: "img", "aria-label": unlimited ? "حجم نامحدود" : `${size(left)} باقی مانده` }, svg,
    h("div", { class: "num" }, unlimited ? [h("b", {}, "∞"), h("small", {}, "نامحدود")] : [h("b", {}, num(Math.round((1 - frac) * 100)) + "٪"), h("small", {}, "باقی‌مانده")]));
}

function statusBadge(sv) {
  const map = { active: ["good", sv.online ? "آنلاین" : "فعال"], ended: ["bad", "حجم تمام شده"], expired: ["bad", "منقضی"], disabled: ["warn", "غیرفعال"] };
  const [cls, label] = map[sv.status] || ["", sv.status];
  return h("span", { class: "badge " + cls }, sv.status === "active" ? h("span", { class: "dot" }) : null, label);
}

function daysText(sv) {
  const now = Date.now() / 1000;
  if (sv.until > now) return `${num(Math.ceil((sv.until - now) / 86400))} روز مانده، تا ${date(sv.until)}`;
  if (sv.until === 0 && sv.days_on_use) return `${num(sv.days_on_use)} روز از اولین اتصال`;
  if (sv.until === 0) return "بدون محدودیت زمانی";
  return `پایان: ${date(sv.until)}`;
}

function sheet(...content) {
  const back = h("div", { class: "sheet-back", onclick: (e) => { if (e.target === back) close(); } });
  const close = () => { back.remove(); try { tg && tg.BackButton.hide(); } catch {} };
  back.append(h("div", { class: "sheet", role: "dialog", "aria-modal": "true" }, ...content));
  document.body.append(back);
  try { tg.BackButton.show(); tg.BackButton.onClick(close); } catch {}
  return close;
}

// ---------- views ----------
function services() {
  const out = [];
  if (!state.services.length) {
    const input = h("input", { placeholder: "https://…/sub/…", "aria-label": "لینک اشتراک" });
    out.push(h("div", { class: "card empty" },
      h("h2", {}, "هنوز سرویسی ندارید"),
      h("p", {}, "از بخش خرید یک سرویس بگیرید، یا اگر از قبل سرویس دارید لینک اشتراکش را اینجا بگذارید."),
      h("div", { class: "field" }, input, h("button", { class: "btn", onclick: () => link(input.value) }, "اتصال")),
      h("button", { class: "btn primary block", onclick: () => go("shop") }, "دیدن پلن‌ها")));
    return out;
  }
  for (const sv of state.services) {
    const total = sv.total < 0 ? "نامحدود" : size(sv.total);
    out.push(h("div", { class: "card svc" },
      ring(sv),
      h("div", {},
        h("h2", {}, ltr(sv.email)),
        h("div", { class: "meta" }, statusBadge(sv)),
        h("div", { class: "meta" }, `مصرف: ${size(sv.used)} از ${total}`),
        h("div", { class: "meta" }, daysText(sv))),
      h("div", { class: "actions" },
        h("button", { class: "btn primary", onclick: () => connect(sv) }, "اتصال و QR"),
        h("button", { class: "btn", onclick: () => copy(sv.sub_url) }, "کپی لینک"),
        state.products.length ? h("button", { class: "btn ghost", onclick: () => renew(sv) }, "تمدید") : null)));
  }
  const input = h("input", { placeholder: "https://…/sub/…", "aria-label": "لینک اشتراک دیگر" });
  out.push(h("div", { class: "section-title" }, "سرویس دیگری دارید؟"),
    h("div", { class: "card" }, h("div", { class: "field" }, input, h("button", { class: "btn", onclick: () => link(input.value) }, "اتصال"))));
  return out;
}

function connect(sv) {
  const close = sheet(
    h("h2", {}, "اتصال ", ltr(sv.email)),
    h("p", { class: "meta" }, "این QR را در v2rayNG، Hiddify، Streisand یا V2Box اسکن کنید، یا لینک را کپی و در برنامه وارد کنید."),
    h("img", { class: "qr", src: new URL(sv.sub_url).pathname + "?qr=1", alt: "QR", width: "220", height: "220" }),
    h("code", { class: "link" }, sv.sub_url),
    h("div", { class: "row" },
      h("button", { class: "btn primary", onclick: () => copy(sv.sub_url) }, "کپی لینک"),
      h("button", { class: "btn", onclick: () => { try { tg.openLink("hiddify://import/" + sv.sub_url); } catch { location.href = "hiddify://import/" + sv.sub_url; } } }, "باز کردن در Hiddify")),
    h("button", { class: "btn ghost block", onclick: () => close() }, "بستن"));
}

function productCard(p, renewEmail) {
  const spec = `${p.plan === "unlimited" ? "حجم نامحدود" : size(p.gb * 2 ** 30)}، ${num(p.days)} روز${p.max_ips ? `، ${num(p.max_ips)} دستگاه` : ""}${p.first_use ? "، شروع از اولین اتصال" : ""}`;
  return h("div", { class: "card product" },
    h("h3", {}, p.name), h("div", { class: "spec" }, spec),
    h("div", { class: "price" }, h("b", {}, p.price ? toman(p.price) : "رایگان"), p.price ? h("small", {}, "تومان") : null),
    h("button", { class: "btn primary block", onclick: () => buy(p, renewEmail) }, renewEmail ? "تمدید با این پلن" : "خرید"));
}

function shop() {
  if (!state.products.length) return [h("div", { class: "card empty" }, h("h2", {}, "فعلاً پلنی برای فروش نیست"), h("p", {}, "کمی بعد دوباره سر بزنید."))];
  return [h("div", { class: "section-title" }, "پلن‌ها"), ...state.products.map((p) => productCard(p, ""))];
}

function renew(sv) {
  const close = sheet(h("h2", {}, "تمدید ", ltr(sv.email)), h("p", { class: "meta" }, "پلن تمدید را انتخاب کنید:"),
    ...state.products.map((p) => productCard(p, sv.email)), h("button", { class: "btn ghost block", onclick: () => close() }, "انصراف"));
  renew.close = close;
}

async function buy(p, renewEmail) {
  if (renew.close) { renew.close(); renew.close = null; }
  const go = h("button", { class: "btn primary" }, "ثبت سفارش");
  const close = sheet(
    h("h2", {}, renewEmail ? "تمدید سرویس" : "خرید ", p.name),
    h("p", {}, `${p.plan === "unlimited" ? "نامحدود" : size(p.gb * 2 ** 30)}، ${num(p.days)} روز — `, h("b", {}, p.price ? `${toman(p.price)} تومان` : "رایگان")),
    renewEmail ? h("p", { class: "meta" }, "برای: ", ltr(renewEmail)) : null,
    h("div", { class: "row" }, go, h("button", { class: "btn", onclick: () => close() }, "انصراف")));
  go.addEventListener("click", async () => {
    go.disabled = true;
    try {
      const r = await api("POST", "order", { product: p.id, renew: renewEmail });
      haptic("success");
      close();
      await load(false);
      setTab("orders");
      sheet(h("h2", {}, `سفارش #${num(r.id)} ${r.existing ? "از قبل ثبت شده" : "ثبت شد"}`),
        h("div", { class: "pay" }, state.pay_text || "پشتیبانی برای پرداخت با شما تماس می‌گیرد."),
        h("p", { class: "meta" }, "بعد از تأیید پرداخت، سرویس خودکار فعال می‌شود و لینک اتصال در همین ربات و بخش «سرویس‌ها» می‌آید."),
        state.support ? h("button", { class: "btn block", onclick: () => { try { tg.openTelegramLink(state.support); } catch { location.href = state.support; } } }, "ارسال رسید به پشتیبانی") : null);
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  });
}

const orderStatus = { pending: ["warn", "در انتظار پرداخت"], fulfilled: ["good", "تحویل شد"], cancelled: ["", "لغو شد"] };
function orders() {
  if (!state.orders.length) return [h("div", { class: "card empty" }, h("h2", {}, "سفارشی ندارید"), h("button", { class: "btn primary block", onclick: () => go("shop") }, "خرید سرویس"))];
  return [h("div", { class: "section-title" }, "سفارش‌های شما"), ...state.orders.map((o) => {
    const [cls, label] = orderStatus[o.status] || ["", o.status];
    return h("div", { class: "card order" },
      h("div", {}, h("div", { class: "what" }, `#${num(o.id)}، ${o.product}`),
        h("div", { class: "when" }, date(o.created), o.amount ? `، ${toman(o.amount)} تومان` : "", o.renew ? ["، تمدید ", ltr(o.renew)] : "")),
      h("div", {}, h("span", { class: "badge " + cls }, label),
        o.status === "pending" ? h("button", { class: "btn ghost", onclick: async () => {
          try { await api("POST", "cancel/" + o.id); toast("لغو شد"); await load(false); } catch (e) { toast(e.message); }
        } }, "لغو") : null));
  })];
}

async function link(value) {
  value = (value || "").trim();
  if (!value) return;
  try { const r = await api("POST", "link", { link: value }); toast(`${r.email} اضافه شد`); haptic("success"); await load(false); setTab("services"); }
  catch (e) { haptic("error"); toast(e.status === 404 ? "این لینک پیدا نشد" : e.message); }
}

function render() {
  const name = (state.user && state.user.first_name) || "";
  const head = h("header", { class: "hello" },
    h("div", {}, h("h1", {}, state.title || "Dark VPN"), h("p", {}, name ? `سلام ${name}` : "")),
    h("div", { class: "mark", "aria-hidden": "true" }, (() => { const v = s("svg", { viewBox: "0 0 24 24" }); v.append(s("path", { d: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" })); return v; })()));
  const body = { services, shop, orders }[tab]();
  app.replaceChildren(head, state.announce ? h("div", { class: "announce" }, state.announce) : "", ...body);
  tabs.hidden = false;
  for (const b of tabs.querySelectorAll("button")) b.setAttribute("aria-current", b.dataset.tab === tab ? "page" : "false");
}

function setTab(t) { tab = t; render(); window.scrollTo({ top: 0 }); }
function go(t) { setTab(t); }
tabs.addEventListener("click", (e) => { const b = e.target.closest("button[data-tab]"); if (b) { try { tg.HapticFeedback.selectionChanged(); } catch {} setTab(b.dataset.tab); } });

async function load(first = true) {
  try { state = await api("GET", "me"); render(); }
  catch (e) {
    if (!first) { toast(e.message); return; }
    const msg = e.status === 401 ? "این صفحه را از داخل ربات تلگرام باز کنید." : e.status === 503 ? "فروشگاه فعلاً بسته است." : e.message;
    app.replaceChildren(h("div", { class: "error-page" }, h("h1", {}, "Dark VPN"), h("p", {}, msg)));
  }
}
load();
