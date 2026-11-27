"use strict";
// Dark VPN store inside Telegram. Every call carries Telegram's signed
// initData; the HUB checks it against the bot token.

const tg = window.Telegram && window.Telegram.WebApp;
const initData = (tg && tg.initData) || new URLSearchParams(location.hash.slice(1)).get("tgWebAppData") || "";
if (tg) {
  try { tg.ready(); tg.expand(); } catch {}
  if (tg.colorScheme === "light") document.documentElement.classList.add("light");
  try {
    const bg = tg.colorScheme === "light" ? "#eef1f8" : "#070a12";
    tg.setHeaderColor(bg); tg.setBackgroundColor(bg);
    if (tg.setBottomBarColor) tg.setBottomBarColor(bg);
  } catch {}
  try { tg.disableVerticalSwipes && tg.disableVerticalSwipes(); } catch {}
}

const app = document.getElementById("app");
const tabs = document.getElementById("tabs");
let state = null;
let tab = "services";
let role = { manage: false, store: true }; // GET role: management (manage.js) and whether the store is open

function h(tag, attrs, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "class") el.className = v;
    else if (k === "style") for (const d of String(v).split(";")) { const i = d.indexOf(":"); if (i > 0) el.style.setProperty(d.slice(0, i).trim(), d.slice(i + 1).trim()); } // CSSOM: allowed by the strict CSP
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const c of kids.flat()) if (c != null && c !== false) el.append(c instanceof Node ? c : String(c));
  return el;
}
const SVG = "http://www.w3.org/2000/svg";
function s(tag, attrs) { const el = document.createElementNS(SVG, tag); for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v); return el; }
function icon(d) { const v = s("svg", { viewBox: "0 0 24 24", "aria-hidden": "true" }); v.append(s("path", { d })); return v; }
const IC = {
  shield: "M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z",
  bolt: "M13 2 3 14h9l-1 8 10-12h-9z",
  copy: "M8 8h12v12H8zM4 16V4h12",
  qr: "M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h3v3h-3zM20 14v3M14 20h3M20 20h1",
  renew: "M21 12a9 9 0 1 1-3-6.7L21 8M21 3v5h-5",
  wallet: "M3 7h18v13H3zM3 7l3-4h12l3 4M16 13.5h2",
  gift: "M20 12v10H4V12M2 7h20v5H2zM12 22V7M12 7H7.5a2.5 2.5 0 1 1 0-5C11 2 12 7 12 7zM12 7h4.5a2.5 2.5 0 1 0 0-5C13 2 12 7 12 7z",
  users: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75",
  chat: "M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z",
  card: "M2 6h20v12H2zM2 10h20M6 15h4",
  link: "M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.5 1.5M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.5-1.5",
};

const nf = new Intl.NumberFormat("fa-IR", { maximumFractionDigits: 1 });
const num = (n) => nf.format(n);
const ltr = (t) => h("span", { class: "ltr" }, t);
function size(b) {
  if (b < 0) return "نامحدود";
  const u = ["بایت", "کیلوبایت", "مگابایت", "گیگ", "ترابایت"];
  let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return `${num(b)} ${u[i]}`;
}
const toman = (n) => new Intl.NumberFormat("fa-IR").format(n);
const date = (u) => new Intl.DateTimeFormat("fa-IR-u-ca-persian", { dateStyle: "medium" }).format(new Date(u * 1000));

function toast(msg) {
  const t = document.getElementById("toast");
  t.textContent = msg; t.hidden = false;
  clearTimeout(toast.t); toast.t = setTimeout(() => { t.hidden = true; }, 2600);
}
function haptic(kind) { try { tg && tg.HapticFeedback.notificationOccurred(kind); } catch {} }
function tap() { try { tg && tg.HapticFeedback.impactOccurred("light"); } catch {} }

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

function openBot() {
  // Back to the chat, where the receipt photo goes.
  try { if (state.bot) tg.openTelegramLink(`https://t.me/${state.bot}`); tg.close(); } catch { if (state.bot) location.href = `https://t.me/${state.bot}`; }
}

// ---------- pieces ----------
// A ring: share is what is LEFT (a full ring = untouched).
function ring(share, big, small, tone) {
  const r = 34, c = 2 * Math.PI * r;
  const svg = s("svg", { viewBox: "0 0 80 80", "aria-hidden": "true" });
  const val = s("circle", { class: "val", cx: "40", cy: "40", r: String(r), "stroke-dasharray": String(c), "stroke-dashoffset": String(c) });
  svg.append(s("circle", { class: "track", cx: "40", cy: "40", r: String(r) }), val);
  requestAnimationFrame(() => requestAnimationFrame(() => val.setAttribute("stroke-dashoffset", String(c * (1 - Math.max(0, Math.min(1, share)))))));
  return h("div", { class: `ring ${tone || ""}` }, svg, h("div", { class: "num" }, h("b", {}, big), h("small", {}, small)));
}

function spark(days) {
  if (!days || !days.length) return null;
  const top = Math.max(1, ...days);
  const total = days.reduce((a, b) => a + b, 0);
  return h("div", { class: "spark" },
    h("div", { class: "bars", "aria-hidden": "true" }, days.map((d, i) => h("i", { style: `--h:${Math.max(4, Math.round((d / top) * 100))}%;--d:${i * 40}ms`, title: size(d) }))),
    h("div", { class: "spark-k" }, `مصرف ۷ روز: ${size(total)}`));
}

function statusPill(sv) {
  const map = { active: ["good", sv.online ? "آنلاین" : "فعال"], ended: ["bad", "حجم تمام شد"], expired: ["bad", "منقضی"], disabled: ["warn", "غیرفعال"] };
  const [cls, label] = map[sv.status] || ["", sv.status];
  return h("span", { class: "pill " + cls }, sv.status === "active" ? h("span", { class: sv.online ? "dot live" : "dot" }) : null, label);
}

function sheet(...content) {
  const back = h("div", { class: "sheet-back", onclick: (e) => { if (e.target === back) close(); } });
  const close = () => { back.classList.add("out"); setTimeout(() => back.remove(), 180); try { tg && tg.BackButton.hide(); } catch {} };
  back.append(h("div", { class: "sheet", role: "dialog", "aria-modal": "true" }, h("div", { class: "grip", "aria-hidden": "true" }), ...content));
  document.body.append(back);
  try { tg.BackButton.show(); tg.BackButton.onClick(close); } catch {}
  return close;
}

function btn(label, cls, onclick, ic) {
  return h("button", { class: `btn ${cls || ""}`, onclick: (e) => { tap(); onclick(e); } }, ic ? icon(IC[ic]) : null, h("span", {}, label));
}

// ---------- views ----------
function serviceCard(sv) {
  const now = Date.now() / 1000;
  const unl = sv.total < 0;
  const leftB = unl ? -1 : Math.max(0, sv.total - sv.used);
  const volShare = unl ? 1 : leftB / Math.max(1, sv.total);
  let daysBig = "∞", daysSmall = "بدون انقضا", dayShare = 1;
  if (sv.until > now) {
    const d = Math.ceil((sv.until - now) / 86400);
    daysBig = num(d); daysSmall = "روز مانده"; dayShare = Math.min(1, d / 30);
  } else if (sv.until === 0 && sv.days_on_use) {
    daysBig = num(sv.days_on_use); daysSmall = "روز از اولین اتصال";
  } else if (sv.until > 0) {
    daysBig = "۰"; daysSmall = "تمام شده"; dayShare = 0;
  }
  const tone = (x) => (x < 0.1 ? "bad" : x < 0.25 ? "warn" : "");
  return h("div", { class: "card svc" },
    h("div", { class: "svc-head" }, h("div", { class: "svc-name" }, icon(IC.shield), ltr(sv.email)), statusPill(sv)),
    h("div", { class: "rings" },
      ring(volShare, unl ? "∞" : size(leftB), unl ? "حجم نامحدود" : `مانده از ${size(sv.total)}`, unl ? "" : tone(volShare)),
      ring(dayShare, daysBig, daysSmall, sv.until > 0 ? tone(dayShare) : "alt")),
    sv.until > now ? h("div", { class: "muted small center" }, `تا ${date(sv.until)}`) : null,
    spark(sv.days),
    h("div", { class: "actions" },
      btn("اتصال و QR", "primary", () => connect(sv), "qr"),
      btn("کپی لینک", "", () => copy(sv.sub_url), "copy"),
      state.products.length ? btn("تمدید", "", () => renew(sv), "renew") : null),
    btn("آموزش اتصال / وصل نمی‌شوم", "ghost block", () => helpSheet(sv), "chat"));
}

// ---------- connection help ----------
const GUIDE = {
  android: { name: "اندروید", apps: [
    { name: "Hiddify", link: (u) => "hiddify://import/" + u, store: "https://play.google.com/store/apps/details?id=app.hiddify.com" },
    { name: "v2rayNG", link: (u) => "v2rayng://install-config?url=" + encodeURIComponent(u), store: "https://play.google.com/store/apps/details?id=com.v2ray.ang" }],
    steps: ["برنامه را نصب کنید.", "دکمهٔ «افزودن به برنامه» را بزنید (یا لینک را کپی و در برنامه از کلیپ‌بورد وارد کنید).", "در برنامه «به‌روزرسانی اشتراک» را بزنید.", "یکی از سرورها را انتخاب و وصل شوید."] },
  ios: { name: "آیفون", apps: [
    { name: "Streisand", link: (u) => "streisand://import/" + u, store: "https://apps.apple.com/app/streisand/id6450534064" },
    { name: "V2Box", link: (u) => "v2box://install-sub?url=" + encodeURIComponent(u) + "&name=VPN", store: "https://apps.apple.com/app/v2box-v2ray-client/id6446814690" },
    { name: "Hiddify", link: (u) => "hiddify://import/" + u, store: "https://apps.apple.com/app/hiddify-proxy-vpn/id6596777532" }],
    steps: ["برنامه را از اپ‌استور نصب کنید.", "دکمهٔ «افزودن به برنامه» را بزنید یا لینک را کپی کنید و در برنامه با + وارد کنید.", "اجازهٔ افزودن VPN را بدهید.", "یکی از سرورها را انتخاب و وصل شوید."] },
  windows: { name: "ویندوز", apps: [
    { name: "Hiddify", link: (u) => "hiddify://import/" + u, store: "https://github.com/hiddify/hiddify-app/releases/latest" },
    { name: "v2rayN", store: "https://github.com/2dust/v2rayN/releases/latest" }],
    steps: ["برنامه را نصب و باز کنید.", "لینک را کپی کنید و در برنامه «افزودن از کلیپ‌بورد» را بزنید.", "اشتراک را به‌روز کنید.", "حالت VPN یا System Proxy را روشن کنید و وصل شوید."] },
  mac: { name: "مک", apps: [
    { name: "Hiddify", link: (u) => "hiddify://import/" + u, store: "https://github.com/hiddify/hiddify-app/releases/latest" },
    { name: "V2Box", link: (u) => "v2box://install-sub?url=" + encodeURIComponent(u) + "&name=VPN", store: "https://apps.apple.com/app/v2box-v2ray-client/id6446814690" }],
    steps: ["برنامه را نصب کنید.", "دکمهٔ «افزودن به برنامه» را بزنید یا لینک را کپی و در برنامه وارد کنید.", "اشتراک را به‌روز کنید.", "یکی از سرورها را انتخاب و وصل شوید."] },
};
const TIPS = ["اشتراک را در برنامه به‌روز کنید؛ سرورها گاهی عوض می‌شوند.", "یک سرور دیگر از فهرست را امتحان کنید.", "تاریخ و ساعت گوشی روی «خودکار» باشد.", "یک بار حالت هواپیما را روشن و خاموش کنید.", "اگر حجم یا روزتان تمام شده، اول تمدید کنید."];
function guessDevice() {
  const ua = navigator.userAgent || "";
  return /iPhone|iPad|iPod/i.test(ua) ? "ios" : /Android/i.test(ua) ? "android" : /Mac/i.test(ua) ? "mac" : /Win/i.test(ua) ? "windows" : "android";
}
function helpSheet(sv) {
  let dev = guessDevice(), app = "";
  const box = h("div", {});
  const open = (url) => { try { tg.openLink(url); } catch { location.href = url; } };
  const draw = () => {
    const g = GUIDE[dev];
    box.replaceChildren(
      h("div", { class: "chips" }, Object.entries(GUIDE).map(([k, v]) => h("button", { class: `chip ${k === dev ? "on" : ""}`, onclick: () => { dev = k; app = ""; draw(); } }, v.name))),
      h("div", { class: "section-title" }, "۱. یکی از این برنامه‌ها"),
      ...g.apps.map((a) => h("div", { class: "card guide-app" }, h("b", {}, a.name),
        h("div", { class: "row" }, a.link ? btn("افزودن به برنامه", "primary", () => { app = a.name; open(a.link(sv.sub_url)); }, "link") : btn("کپی لینک", "primary", () => { app = a.name; copy(sv.sub_url); }, "copy"),
          btn("دانلود", "", () => open(a.store))))),
      h("div", { class: "section-title" }, "۲. قدم‌ها"),
      h("ol", { class: "steps" }, g.steps.map((t) => h("li", {}, t))),
      btn("کپی لینک اشتراک", "block ghost", () => copy(sv.sub_url), "copy"),
      h("details", { class: "more" }, h("summary", {}, "وصل نمی‌شوم 😕"),
        h("ol", { class: "steps" }, TIPS.map((t) => h("li", {}, t))),
        (() => {
          const note = h("textarea", { rows: "3", maxlength: "1000", placeholder: "اگر خطایی می‌بینید یا توضیحی دارید بنویسید (اختیاری)" });
          const go = btn("باز هم وصل نمی‌شوم — به پشتیبانی بگو", "primary block", async () => {
            go.disabled = true;
            try {
              const r = await api("POST", "help", { service: sv.email, device: g.name, app, note: note.value });
              haptic("success"); celebrate(`تیکت #${num(r.ticket)} ثبت شد؛ جواب در ربات می‌آید`);
            } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
          }, "chat");
          return h("div", {}, h("p", { class: "muted small" }, "وضعیت سرویس، دستگاه و برنامهٔ شما خودکار برای پشتیبانی فرستاده می‌شود."), note, go);
        })()));
  };
  draw();
  const close = sheet(h("h2", {}, "آموزش اتصال"), h("p", { class: "muted small ltr" }, sv.email), box, btn("بستن", "ghost block", () => close()));
}

function serviceRow(sv) {
  const now = Date.now() / 1000, unl = sv.total < 0;
  const left = unl ? -1 : Math.max(0, sv.total - sv.used);
  const pct = unl ? 100 : Math.round((left / Math.max(1, sv.total)) * 100);
  const days = sv.until > now ? `${num(Math.ceil((sv.until - now) / 86400))} روز` : sv.until === 0 && sv.days_on_use ? `${num(sv.days_on_use)} روز از اتصال` : sv.until > 0 ? "تمام شده" : "بی‌زمان";
  const tone = pct < 10 ? "bad" : pct < 25 ? "warn" : "";
  return h("div", { class: `svc-row ${tone}`, role: "button", tabindex: "0", onclick: () => { const close = sheet(serviceCard(sv), btn("بستن", "ghost block", () => close())); } },
    h("div", { class: "sr-main" },
      h("div", { class: "sr-top" }, ltr(sv.email), statusPill(sv)),
      h("div", { class: "sr-bar", "aria-hidden": "true" }, h("i", { style: `--w:${pct}%` })),
      h("div", { class: "sr-meta" }, h("span", {}, unl ? "حجم نامحدود" : `${size(left)} مانده از ${size(sv.total)}`), h("span", {}, days))),
    h("button", { class: "sr-copy", "aria-label": "کپی لینک", onclick: (e) => { e.stopPropagation(); copy(sv.sub_url); } }, icon(IC.copy)));
}

function services() {
  const out = [];
  if (!state.services.length) {
    const input = h("input", { placeholder: "https://…/sub/…", "aria-label": "لینک اشتراک", dir: "ltr" });
    out.push(h("div", { class: "card empty hero-empty" },
      h("div", { class: "glow-mark", "aria-hidden": "true" }, icon(IC.shield)),
      h("h2", {}, "هنوز سرویسی ندارید"),
      h("p", {}, "یک پلن بخرید یا اگر سرویس دارید، لینک اشتراکش را اینجا بگذارید."),
      btn("دیدن پلن‌ها", "primary block", () => setTab("shop"), "bolt"),
      h("div", { class: "field" }, input, btn("اتصال", "", () => link(input.value), "link"))));
    out.push(...ordersPart(true), ...ordersPart(false));
    out.push(...extras());
    return out;
  }
  // A short list; the rings, usage and actions open with a tap.
  out.push(...ordersPart(true));
  out.push(h("div", { class: "section-title" }, "سرویس‌های من"), h("div", { class: "svc-list" }, state.services.map(serviceRow)));
  out.push(...ordersPart(false));
  out.push(...extras());
  const input = h("input", { placeholder: "https://…/sub/…", "aria-label": "لینک اشتراک دیگر", dir: "ltr" });
  out.push(h("div", { class: "section-title" }, "سرویس دیگری دارید؟"),
    h("div", { class: "card" }, h("div", { class: "field" }, input, btn("اتصال", "", () => link(input.value), "link"))));
  return out;
}

function connect(sv) {
  const close = sheet(
    h("h2", {}, "اتصال ", ltr(sv.email)),
    h("p", { class: "muted" }, "QR را در v2rayNG، Hiddify، Streisand یا V2Box اسکن کنید، یا لینک را کپی کنید."),
    h("div", { class: "qr-wrap" }, h("img", { class: "qr", src: new URL(sv.sub_url).pathname + "?qr=1", alt: "QR", width: "220", height: "220" })),
    h("code", { class: "link" }, sv.sub_url),
    h("div", { class: "row" },
      btn("کپی لینک", "primary", () => copy(sv.sub_url), "copy"),
      btn("باز کردن در Hiddify", "", () => { try { tg.openLink("hiddify://import/" + sv.sub_url); } catch { location.href = "hiddify://import/" + sv.sub_url; } })),
    btn("بستن", "ghost block", () => close()));
}

function chips(p) {
  return h("div", { class: "chips" },
    h("span", { class: "chip" }, p.plan === "unlimited" ? "♾ نامحدود" : `📶 ${size(p.gb * 2 ** 30)}`),
    h("span", { class: "chip" }, `⏳ ${num(p.days)} روز`),
    p.max_ips ? h("span", { class: "chip" }, `📱 ${num(p.max_ips)} دستگاه`) : null,
    p.first_use ? h("span", { class: "chip" }, "شروع از اولین اتصال") : null);
}

function productCard(p, renewEmail, featured) {
  const stars = p.price_stars > 0 && state.stars;
  const price = p.price ? [h("b", {}, toman(p.price)), h("small", {}, "تومان")] : stars ? [h("b", {}, `${num(p.price_stars)} ⭐`), h("small", {}, "استارز")] : [h("b", {}, "رایگان")];
  const buttons = [];
  if (p.price > 0 && state.toman >= p.price) buttons.push(btn(`پرداخت از کیف پول (${toman(state.toman)} تومان)`, "primary block", () => payToman(p, renewEmail), "wallet"));
  if (p.price > 0 && state.card) buttons.push(btn(renewEmail ? "تمدید با کارت به کارت" : "خرید با کارت به کارت", buttons.length ? "block" : "primary block", () => buy(p, renewEmail), "card"));
  if (stars) buttons.push(btn(`${renewEmail ? "تمدید" : "خرید"} با ${num(p.price_stars)} استارز`, buttons.length ? "ghost block" : "primary block", () => payStars(p, renewEmail)));
  if (p.price_stars > 0 && state.wallet >= p.price_stars) buttons.push(btn(`از کیف پول استارز (${num(state.wallet)}⭐)`, "ghost block", () => payWallet(p, renewEmail)));
  if (!buttons.length) buttons.push(btn(renewEmail ? "تمدید" : "خرید", "primary block", () => buy(p, renewEmail)));
  return h("div", { class: `card product ${featured ? "featured" : ""}` },
    featured ? h("span", { class: "ribbon" }, "پیشنهاد ویژه") : null,
    p.category ? h("div", { class: "cat" }, p.category) : null,
    h("h3", {}, p.name), chips(p),
    h("div", { class: "price" }, ...price), ...buttons);
}

async function payStars(p, renewEmail) {
  closeRenew();
  try {
    const r = await api("POST", "pay", { product: p.id, renew: renewEmail, method: "stars" });
    const done = async (status) => {
      if (status === "paid") { haptic("success"); celebrate("پرداخت شد — سرویس فعال است"); await load(false); setTab("services"); }
      else if (status === "failed") { haptic("error"); toast("پرداخت انجام نشد"); }
    };
    if (tg && tg.openInvoice) tg.openInvoice(r.invoice, done); else location.href = r.invoice;
  } catch (e) { haptic("error"); toast(e.message); }
}

async function payWallet(p, renewEmail) {
  closeRenew();
  try {
    const r = await api("POST", "pay", { product: p.id, renew: renewEmail, method: "wallet" });
    haptic("success"); celebrate(`پرداخت شد — موجودی ${num(r.wallet)}⭐`);
    await load(false); setTab("services");
  } catch (e) { haptic("error"); toast(e.message); }
}

async function payToman(p, renewEmail) {
  closeRenew();
  const go = btn(`پرداخت ${toman(p.price)} تومان`, "primary", async () => {
    go.disabled = true;
    try {
      const r = await api("POST", "pay", { product: p.id, renew: renewEmail, method: "toman" });
      close(); haptic("success"); celebrate(`پرداخت شد — موجودی ${toman(r.toman)} تومان`);
      await load(false); setTab("services");
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "wallet");
  const close = sheet(h("h2", {}, renewEmail ? "تمدید از کیف پول" : "خرید از کیف پول"),
    h("div", { class: "summary" }, h("div", {}, p.name), chips(p), renewEmail ? h("div", { class: "muted small" }, "برای: ", ltr(renewEmail)) : null),
    h("div", { class: "kv" }, h("span", {}, "موجودی فعلی"), h("b", {}, `${toman(state.toman)} تومان`)),
    h("div", { class: "kv" }, h("span", {}, "بعد از پرداخت"), h("b", {}, `${toman(state.toman - p.price)} تومان`)),
    h("div", { class: "row" }, go, btn("انصراف", "", () => close())));
}

function cardBox(amount, card) {
  const c = card || state.card;
  const digits = (c.number || "").replace(/\D/g, "");
  const pretty = digits.replace(/(\d{4})(?=\d)/g, "$1 ");
  return h("div", { class: "bank-card" },
    h("div", { class: "bc-top" }, h("span", {}, c.bank || "کارت به کارت"), icon(IC.card)),
    h("div", { class: "bc-num ltr" }, pretty),
    h("div", { class: "bc-bottom" }, h("span", {}, c.holder || ""), amount ? h("b", {}, `${toman(amount)} تومان`) : null),
    h("div", { class: "row" }, btn("کپی شماره کارت", "", () => copy(digits), "copy"), amount ? btn("کپی مبلغ", "ghost", () => copy(String(amount))) : null));
}

function receiptSteps() {
  return h("ol", { class: "steps" },
    h("li", {}, "مبلغ را به کارت بالا واریز کنید."),
    h("li", {}, "از رسید پرداخت عکس بگیرید (یا اسکرین‌شات)."),
    h("li", {}, "با دکمهٔ پایین عکس را همین‌جا بفرستید؛ بعد از تأیید، خودکار انجام می‌شود."));
}

// pickImage asks for a photo and returns it as a JPEG data URL, at most
// 1600 px on its long side (receipts stay readable and small).
function pickImage() {
  return new Promise((res, rej) => {
    const inp = h("input", { type: "file", accept: "image/*", style: "position:fixed;inset-inline-start:-999px" });
    document.body.append(inp);
    inp.addEventListener("change", () => {
      const f = inp.files && inp.files[0];
      inp.remove();
      if (!f) { rej(new Error("عکسی انتخاب نشد")); return; }
      const fr = new FileReader();
      fr.onerror = () => rej(new Error("خواندن عکس ممکن نشد"));
      fr.onload = () => {
        const img = new Image();
        img.onerror = () => res(fr.result);
        img.onload = () => {
          try {
            const k = Math.min(1, 1600 / Math.max(img.width, img.height));
            const c = document.createElement("canvas");
            c.width = Math.max(1, Math.round(img.width * k)); c.height = Math.max(1, Math.round(img.height * k));
            c.getContext("2d").drawImage(img, 0, 0, c.width, c.height);
            res(c.toDataURL("image/jpeg", 0.85));
          } catch { res(fr.result); }
        };
        img.src = fr.result;
      };
      fr.readAsDataURL(f);
    });
    inp.click();
  });
}

// receiptButton uploads the receipt photo of a payment from the app (kind:
// order | topup | panel); the bot takes it as if it were sent in the chat.
function receiptButton(kind, id, done, cls) {
  const label = "فرستادن عکس رسید";
  const b = btn(label, cls || "primary block big", async () => {
    let data;
    try { data = await pickImage(); } catch { return; }
    b.disabled = true; b.querySelector("span").textContent = "در حال فرستادن…";
    try {
      await api("POST", "receipt", { kind, id, image: data });
      haptic("success"); celebrate("رسید فرستاده شد ✅ بعد از بررسی خبرتان می‌کنیم");
      if (done) done();
    } catch (e) { haptic("error"); toast(e.message); b.disabled = false; b.querySelector("span").textContent = label; }
  }, "card");
  return b;
}

// showPay shows how to pay an order: its own card (several cards: each
// order keeps one), or the phone check first, or that no card is free today.
function showPay(p, renewEmail, r) {
  const paid = state.card && p.price;
  const title = h("h2", {}, `سفارش #${num(r.id)} ${r.existing ? "از قبل ثبت شده" : "ثبت شد"}`);
  if (paid && r.phone_needed) {
    const close = sheet(title,
      h("div", { class: "pay" }, "برای امنیت پرداخت، یک بار شمارهٔ موبایل این حساب تلگرام را تأیید کنید تا شمارهٔ کارت نمایش داده شود."),
      btn("تأیید شمارهٔ من", "primary block", () => {
        try {
          tg.requestContact(async (ok) => {
            if (!ok) return;
            close();
            await new Promise((res) => setTimeout(res, 1500)); // the bot keeps the number first
            try { showPay(p, renewEmail, await api("POST", "order", { product: p.id, renew: renewEmail })); } catch (e) { toast(e.message); }
          });
        } catch { openBot(); }
      }, "chat"));
    return;
  }
  if (paid && r.no_card) {
    sheet(title, h("div", { class: "pay" }, "امروز امکان پرداخت کارت‌به‌کارت نیست؛ کمی بعد دوباره امتحان کنید یا به پشتیبانی پیام دهید."), btn("رفتن به ربات", "primary block", openBot, "chat"));
    return;
  }
  const close = sheet(title,
    paid ? cardBox(p.price, r.card) : h("div", { class: "pay" }, state.pay_text || "پشتیبانی برای پرداخت با شما تماس می‌گیرد."),
    paid ? receiptSteps() : null,
    paid ? receiptButton("order", r.id, () => { close(); load(false); }) : null,
    btn("یا در ربات بفرستید", paid ? "ghost block" : "primary block", openBot, "chat"));
}

async function buy(p, renewEmail) {
  closeRenew();
  const go = btn("ثبت سفارش", "primary", async () => {
    go.disabled = true;
    try {
      const r = await api("POST", "order", { product: p.id, renew: renewEmail });
      haptic("success"); close(); await load(false); setTab("services");
      showPay(p, renewEmail, r);
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "card");
  const close = sheet(
    h("h2", {}, renewEmail ? "تمدید سرویس" : "خرید ", p.name),
    h("div", { class: "summary" }, chips(p), renewEmail ? h("div", { class: "muted small" }, "برای: ", ltr(renewEmail)) : null),
    h("div", { class: "kv" }, h("span", {}, "مبلغ"), h("b", {}, p.price ? `${toman(p.price)} تومان` : "رایگان")),
    h("div", { class: "row" }, go, btn("انصراف", "", () => close())));
}

function extras() {
  const out = [];
  if (state.trial) out.push(h("div", { class: "card promo" },
    h("div", { class: "promo-ic" }, icon(IC.gift)),
    h("div", {}, h("h3", {}, "تست رایگان"), h("p", { class: "muted" }, `${state.trial.mb ? size(state.trial.mb * 2 ** 20) : state.trial.gb ? size(state.trial.gb * 2 ** 30) : "حجم نامحدود"} برای ${num(state.trial.hours)} ساعت — یک بار برای هر حساب.`)),
    btn("فعال کردن", "primary", async (e) => {
      e.currentTarget.disabled = true;
      try { await api("POST", "trial"); haptic("success"); celebrate("تست رایگان فعال شد"); await load(false); }
      catch (x) { toast(x.status === 409 ? "تست رایگان قبلاً استفاده شده" : x.message); }
    })));
  return out;
}

// Reseller panels on sale in this bot (GET panels); loaded once.
let panelSale;
function panelSection() {
  if (panelSale === undefined) {
    panelSale = null;
    api("GET", "panels").then((r) => { panelSale = r; if (tab === "shop") render(); }).catch(() => { panelSale = { on: false }; });
    return [];
  }
  if (!panelSale || !panelSale.on) return [];
  return [h("div", { class: "section-title" }, "🤝 پنل نمایندگی"),
    h("p", { class: "muted small" }, "پنل فروش خودتان را بگیرید: کاربر بسازید، بفروشید و سود کنید. همه‌چیز از داخل همین ربات و مینی‌اپ."),
    h("div", { class: "grid" }, panelSale.packages.map((p) => h("div", { class: "card product" },
      h("h3", {}, p.name),
      h("div", { class: "chips" }, h("span", { class: "chip" }, `📶 ${num(p.gb)} گیگ اعتبار`), p.unlimited ? h("span", { class: "chip" }, "♾ فروش نامحدود") : null,
        p.max_clients ? h("span", { class: "chip" }, `حداکثر ${num(p.max_clients)} کاربر`) : null),
      h("div", { class: "price" }, h("b", {}, toman(p.price)), h("small", {}, "تومان")),
      btn("خرید پنل", "primary block", () => buyPanel(p), "card"))))];
}

function buyPanel(p) {
  const name = h("input", { dir: "ltr", placeholder: "مثلاً ali-shop", autocapitalize: "off", autocomplete: "off", spellcheck: "false", "aria-label": "نام کاربری پنل" });
  const go = btn("ثبت سفارش", "primary", async () => {
    go.disabled = true;
    try {
      const r = await api("POST", "panel", { package: p.id, name: name.value.trim().toLowerCase() });
      close(); haptic("success");
      const done = sheet(h("h2", {}, `سفارش پنل #${num(r.id)} ثبت شد`), h("p", { class: "muted small" }, "نام کاربری پنل: ", ltr(r.username)),
        r.card && r.card.number ? cardBox(r.price, r.card) : h("div", { class: "pay" }, "پشتیبانی برای پرداخت با شما تماس می‌گیرد."),
        receiptSteps(), receiptButton("panel", r.id, () => done()), btn("یا در ربات بفرستید", "ghost block", openBot, "chat"));
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message.includes("گرفته شده") ? "این نام کاربری گرفته شده؛ نام دیگری بنویسید." : e.message); }
  }, "card");
  const close = sheet(h("h2", {}, "خرید پنل نمایندگی"),
    h("div", { class: "summary" }, h("div", {}, p.name), h("div", { class: "muted small" }, `${num(p.gb)} گیگ اعتبار · ${toman(p.price)} تومان`)),
    h("p", { class: "muted small" }, "یک نام کاربری انگلیسی برای پنل بنویسید (حروف کوچک و عدد، ۳ تا ۲۴ حرف)."),
    h("div", { class: "field" }, name), h("div", { class: "row" }, go, btn("انصراف", "", () => close())));
}

function shop() {
  if (!state.products.length) {
    const ps = panelSection();
    return ps.length ? ps : [h("div", { class: "card empty" }, h("h2", {}, "فعلاً پلنی برای فروش نیست"), h("p", {}, "کمی بعد دوباره سر بزنید."))];
  }
  const cats = [...new Set(state.products.map((p) => p.category || ""))];
  const out = [h("div", { class: "section-title" }, "پلن‌ها")];
  if (state.shop_text) out.push(h("div", { class: "card shop-text" }, state.shop_text));
  // The middle-priced plan is the suggested one.
  const priced = state.products.filter((p) => p.price > 0).sort((a, b) => a.price - b.price);
  const feat = priced.length >= 3 ? priced[Math.floor(priced.length / 2)].id : "";
  for (const c of cats) {
    if (cats.length > 1) out.push(h("div", { class: "section-title sub" }, c || "سایر"));
    out.push(h("div", { class: "grid" }, state.products.filter((p) => (p.category || "") === c).map((p) => productCard(p, "", p.id === feat))));
  }
  out.push(...panelSection());
  return out;
}

function closeRenew() { if (renew.close) { renew.close(); renew.close = null; } }
function renew(sv) {
  const list = state.products.filter((p) => !p.no_renew);
  const close = sheet(h("h2", {}, "تمدید ", ltr(sv.email)), h("p", { class: "muted" }, "پلن تمدید را انتخاب کنید:"),
    ...(list.length ? list : state.products).map((p) => productCard(p, sv.email)), btn("انصراف", "ghost block", () => close()));
  renew.close = close;
}

function wallet() {
  const out = [];
  out.push(h("div", { class: "card wallet-hero" },
    h("div", { class: "muted" }, "موجودی کیف پول"),
    h("div", { class: "big" }, toman(state.toman || 0), h("small", {}, " تومان")),
    state.wallet > 0 ? h("div", { class: "muted small" }, `استارز: ${num(state.wallet)}⭐`) : null,
    h("p", { class: "muted small" }, "با موجودی کیف پول، خرید و تمدید فوری و بدون انتظار انجام می‌شود.")));
  if (state.card) {
    const amount = h("input", { type: "number", inputmode: "numeric", min: "1000", step: "1000", placeholder: "مبلغ دلخواه (تومان)", dir: "ltr" });
    const presets = [100000, 200000, 500000, 1000000];
    out.push(h("div", { class: "section-title" }, "شارژ کیف پول"),
      h("div", { class: "card" },
        h("div", { class: "chips pick" }, presets.map((v) => h("button", { class: "chip", onclick: () => { tap(); amount.value = v; } }, `${toman(v)} تومان`))),
        h("div", { class: "field" }, amount, btn("شارژ", "primary", () => topup(+amount.value), "wallet"))));
  }
  if (state.referral) {
    const rf = state.referral;
    const reward = [rf.percent ? `${num(rf.percent)}٪ مبلغ` : "", rf.fixed ? `${num(rf.fixed)}⭐` : ""].filter(Boolean).join(" + ");
    const url = state.bot ? `https://t.me/${state.bot}?start=ref_${rf.code}` : `/start ref_${rf.code}`;
    out.push(h("div", { class: "section-title" }, "دعوت دوستان"),
      h("div", { class: "card promo" },
        h("div", { class: "promo-ic" }, icon(IC.users)),
        h("div", {}, h("h3", {}, `${num(rf.invited)} دعوت · ${num(rf.earned)}⭐ پاداش`),
          h("p", { class: "muted" }, `برای هر خرید دوستانتان ${reward}${rf.first_only ? " (فقط اولین خرید)" : ""} به کیف پول شما اضافه می‌شود.`)),
        h("div", { class: "row" },
          btn("ارسال لینک دعوت", "primary", () => { try { tg.openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(url)}&text=${encodeURIComponent("با این لینک اینترنت آزاد و پرسرعت بگیر 🚀")}`); } catch { copy(url); } }, "users"),
          btn("کپی", "", () => copy(url), "copy"))));
  }
  if (state.support) out.push(h("div", { class: "card promo" },
    h("div", { class: "promo-ic" }, icon(IC.chat)),
    h("div", {}, h("h3", {}, "پشتیبانی"), h("p", { class: "muted" }, "سؤال یا مشکلی دارید؟ همین حالا بپرسید.")),
    btn("گفتگو با پشتیبانی", "", () => { try { tg.openTelegramLink(state.support); } catch { location.href = state.support; } }, "chat")));
  return out;
}

async function topup(amount) {
  if (!(amount >= 1000)) { toast("مبلغ را وارد کنید (حداقل ۱٬۰۰۰ تومان)"); return; }
  try {
    const r = await api("POST", "topup", { amount });
    haptic("success");
    const close = sheet(h("h2", {}, `شارژ #${num(r.id)} ثبت شد`), cardBox(amount, r.card), receiptSteps(),
      receiptButton("topup", r.id, () => close()), btn("یا در ربات بفرستید", "ghost block", openBot, "chat"));
  } catch (e) { haptic("error"); toast(e.message); }
}

const orderStatus = { pending: ["warn", "در انتظار پرداخت"], fulfilled: ["good", "تحویل شد"], cancelled: ["", "لغو شد"] };
// ordersPart: open orders (waiting for payment) go above the services;
// past ones fold away below them.
function ordersPart(open) {
  const list = (state.orders || []).filter((o) => (o.status === "pending") === open);
  if (!list.length) return [];
  const cards = list.map(orderCard);
  if (open) return [h("div", { class: "section-title" }, "⏳ سفارش‌های در انتظار پرداخت"), ...cards];
  return [h("details", { class: "more past-orders" }, h("summary", {}, `سفارش‌های قبلی (${num(list.length)})`), ...cards)];
}
function orderCard(o) {
  {
    const [cls, label] = orderStatus[o.status] || ["", o.status];
    return h("div", { class: "card order" },
      h("div", {}, h("div", { class: "what" }, `#${num(o.id)} · ${o.product}`),
        h("div", { class: "muted small" }, date(o.created), o.amount ? ` · ${toman(o.amount)} تومان` : "", o.renew ? [" · تمدید ", ltr(o.renew)] : "")),
      h("div", { class: "order-side" }, h("span", { class: "pill " + cls }, label),
        o.needs_receipt ? receiptButton("order", o.id, () => load(false), "small") : null,
        o.status === "pending" ? btn("لغو", "ghost small", async () => {
          try { await api("POST", "cancel/" + o.id); toast("لغو شد"); await load(false); } catch (e) { toast(e.message); }
        }) : null));
  }
}

async function link(value) {
  value = (value || "").trim();
  if (!value) return;
  try { const r = await api("POST", "link", { link: value }); celebrate(`${r.email} اضافه شد`); haptic("success"); await load(false); setTab("services"); }
  catch (e) { haptic("error"); toast(e.status === 404 ? "این لینک پیدا نشد" : e.message); }
}

// celebrate: a short confetti burst with the message.
function celebrate(msg) {
  toast(msg);
  if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const box = h("div", { class: "confetti", "aria-hidden": "true" });
  for (let i = 0; i < 26; i++) box.append(h("i", { style: `--x:${Math.random() * 100}%;--r:${Math.random() * 360}deg;--d:${Math.random() * 300}ms;--c:${["var(--ion)", "var(--ember)", "var(--good)", "#c084fc"][i % 4]}` }));
  document.body.append(box);
  setTimeout(() => box.remove(), 1600);
}

function render() {
  if (!state) { // management only (the store is closed)
    app.replaceChildren(h("header", { class: "hello" }, h("div", { class: "av" }, (role.first_name || "D").slice(0, 1)),
      h("div", { class: "hello-t" }, h("p", {}, role.first_name ? `سلام ${role.first_name} 👋` : "سلام 👋"), h("h1", {}, "مدیریت"))),
      h("div", { class: "view" }, ...manageView()));
    tabs.hidden = true;
    return;
  }
  const u = state.user || {};
  const name = u.first_name || "";
  const initials = (name || "D").slice(0, 1);
  // A reseller's store wears its brand: accent colour and logo.
  if (state.accent && /^#[0-9a-f]{6}$/i.test(state.accent)) { document.documentElement.style.setProperty("--ion", state.accent); document.documentElement.style.setProperty("--ion-2", state.accent); document.documentElement.style.setProperty("--on-ion", "#fff"); }
  const head = h("header", { class: "hello" },
    state.logo && /^data:image\//.test(state.logo) ? h("img", { class: "av logo", src: state.logo, alt: "" }) : h("div", { class: "av" }, initials),
    h("div", { class: "hello-t" }, h("p", {}, name ? `سلام ${name} 👋` : "سلام 👋"), h("h1", {}, state.title || "Dark VPN")),
    h("button", { class: "wallet-chip", onclick: () => { tap(); setTab("wallet"); } }, icon(IC.wallet), h("span", {}, `${toman(state.toman || 0)}`)));
  const body = tab === "manage" && role.manage && typeof manageView === "function" ? manageView() : ({ services, shop, wallet }[tab] || services)();
  app.replaceChildren(head, state.announce ? h("div", { class: "announce" }, state.announce) : "", h("div", { class: "view" }, ...body));
  tabs.hidden = false;
  for (const b of tabs.querySelectorAll("button")) b.setAttribute("aria-current", b.dataset.tab === tab ? "page" : "false");
}

function setTab(t) { tab = t; render(); window.scrollTo({ top: 0 }); }
tabs.addEventListener("click", (e) => { const b = e.target.closest("button[data-tab]"); if (b) { try { tg.HapticFeedback.selectionChanged(); } catch {} setTab(b.dataset.tab); } });

// loadManage brings in the management screens (managers only).
function loadManage() {
  return new Promise((res) => {
    const css = document.createElement("link"); css.rel = "stylesheet"; css.href = "manage.css"; document.head.append(css);
    const add = (src, next) => { const js = document.createElement("script"); js.src = src; js.onload = next; js.onerror = next; document.head.append(js); };
    add("manage.js", () => add("manage2.js", res)); // manage2.js: credit, sellers, bot, cards, plans, tickets, reports
  });
}

async function load(first = true) {
  if (first) {
    try { role = await api("GET", "role"); } catch { role = { manage: false, store: true }; }
    if (role.manage) {
      await loadManage();
      const b = h("button", { "data-tab": "manage" }, icon("M3 3h7v9H3zM14 3h7v5h-7zM14 12h7v9h-7zM3 16h7v5H3z"), h("span", {}, "مدیریت"));
      tabs.prepend(b);
      tabs.classList.add("five");
      if (new URLSearchParams(location.search).get("m") === "1" || !role.store) tab = "manage";
      if (!role.store) { state = null; render(); return; }
    }
  }
  try { state = await api("GET", "me"); render(); }
  catch (e) {
    if (!first) { toast(e.message); return; }
    if (role.manage) { state = null; tab = "manage"; render(); return; }
    const msg = e.status === 401 ? "این صفحه را از داخل ربات تلگرام باز کنید." : e.status === 503 ? "فروشگاه فعلاً بسته است." : e.message;
    app.replaceChildren(h("div", { class: "error-page" }, h("div", { class: "glow-mark" }, icon(IC.shield)), h("h1", {}, "Dark VPN"), h("p", {}, msg)));
  }
}
load();
