"use strict";
// Management, stage 2: credit and top-up, sub-sellers, the reseller's own
// bot, payment cards, plans and prices, tickets and reports. Loaded after
// manage.js and uses its helpers (M, mSet, mic, can, …) and app.js's.

Object.assign(IC, {
  bot: "M12 8V4H8M4 8h16v12H4zM2 14h2M20 14h2M9 13v2M15 13v2",
  tag: "M20.6 13.4 13.4 20.6a2 2 0 0 1-2.8 0L3 13V3h10l7.6 7.6a2 2 0 0 1 0 2.8zM7.5 7.5h.01",
  chart: "M3 3v18h18M7 15l4-4 3 3 5-6",
  send: "M22 2 11 13M22 2l-7 20-4-9-9-4z",
  back: "M9 18l6-6-6-6",
  down: "M12 3v12M6 11l6 6 6-6M4 21h16",
  shop: "M3 9l1.5-5h15L21 9M3 9h18v11H3zM9 13h6",
  gear: "M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z",
});

const M2 = { wallet: null, ledger: [], ledgerEnd: false, sellers: null, bot: null, cards: null, plans: null, tickets: [], tStatus: "active", report: null, days: 7 };

const kindFa = { charge: "فروش", refund: "برگشت", adjust: "تنظیم مدیر", delete: "حذف کاربر", payment: "پرداخت", transfer: "انتقال اعتبار", topup: "شارژ" };
const gbIn = (v, ph) => h("input", { type: "number", step: "any", inputmode: "decimal", value: v ?? "", placeholder: ph || "" });
const field = (label, input, hint) => h("label", { class: "field" }, h("span", {}, label), input, hint ? h("small", { class: "muted" }, hint) : null);
const check = (label, on) => { const i = h("input", { type: "checkbox" }); i.checked = !!on; return [h("label", { class: "check" }, i, h("span", {}, label)), i]; };
const closeSheets = () => document.querySelectorAll(".sheet-back").forEach((e) => e.remove());
const signed = (b) => (b > 0 ? "+" : b < 0 ? "−" : "") + size(Math.abs(b));

function backHead(title, extra) {
  return h("div", { class: "m2head" },
    h("button", { class: "iconbtn", "aria-label": "بازگشت", onclick: () => mSet("more") }, mic("back")),
    h("h2", {}, title), extra || h("span"));
}

async function m2Fetch(view) {
  switch (view) {
    case "wallet":
      M2.wallet = await api("GET", "m/wallet"); M2.ledger = M2.wallet.ledger || []; M2.ledgerEnd = M2.ledger.length < 50; break;
    case "sellers": M2.sellers = await api("GET", "m/sellers"); break;
    case "bot": M2.bot = await api("GET", "m/bot"); break;
    case "cards": M2.cards = await api("GET", "m/cards"); break;
    case "plans": M2.plans = await api("GET", "m/plans"); M2.custom = M2.plans.edit ? await api("GET", "m/custom").catch(() => null) : null; break;
    case "tickets": M2.tickets = await api("GET", `m/tickets?s=${M2.tStatus}`); break;
    case "report": M2.report = await api("GET", `m/report?days=${M2.days}`); break;
    case "resale": M2.resale = await api("GET", "m/resale"); break;
    case "settings": M2.settings = await api("GET", "m/settings"); break;
    case "discounts": M2.discounts = await api("GET", "m/discounts"); break;
    case "broadcast": M2.bc = await api("GET", "m/broadcast"); break;
    case "customers": M2.cust = await api("GET", `m/customers?q=${encodeURIComponent(M2.cq || "")}`); break;
    case "admins": M2.admins = await api("GET", "m/admins"); break;
    case "sale": M2.sale = await api("GET", "m/sale"); break;
    case "wheel": M2.wheel = await api("GET", "m/wheel"); break;
  }
}
const m2Reload = () => m2Fetch(M.view).then(render).catch((e) => toast(e.message));

// ---------- the "more" grid ----------
function moreView() {
  const d = M.home, c = d.can || {};
  const tile = (v, label, ic, sub, badge) => h("button", { class: "tile", onclick: () => (v === "guide" ? tour("manage", true) : mSet(v)) }, mic(ic),
    h("b", {}, label), sub ? h("small", { class: "muted" }, sub) : null, badge ? h("i", { class: "badge" }, num(badge)) : null);
  const tiles = [
    !d.owner && c.credit ? tile("wallet", "اعتبار و شارژ", "wallet", d.credit !== undefined ? size(d.credit) : "") : null,
    c.sellers && !d.sub_seller ? tile("sellers", d.owner ? "نماینده‌ها" : "زیرمجموعه‌ها", "users", "ساخت، اعتبار، دعوت") : null,
    d.resale ? tile("resale", "فروش پنل نمایندگی", "shop", d.owner ? "بسته‌ها و سفارش‌ها" : "پنل بفروش، سود ببر", (d.pending || {}).panels) : null,
    c.bot ? tile("bot", "ربات من", "bot", "توکن، روشن و خاموش") : null,
    c.cards ? tile("cards", "کارت‌های پرداخت", "card", "چند کارت، سقف روزانه") : null,
    c.plans ? tile("plans", "پلن‌ها و قیمت‌ها", "tag", "فروشگاه ربات") : null,
    c.tickets ? tile("tickets", "تیکت‌ها", "chat", "پشتیبانی مشتری", d.tickets) : null,
    c.reports ? tile("report", "گزارش فروش", "chart", "نمودار و اکسل") : null,
    c.settings ? tile("settings", "تنظیمات ربات", "gear", "خوش‌آمد، تست، کانال، ظاهر") : null,
    c.settings && !d.sub_seller ? tile("discounts", "کد تخفیف", "tag", "درصد یا مبلغ، با سقف") : null,
    c.settings ? tile("broadcast", "پیام همگانی", "send", "به همه یا یک گروه") : null,
    c.settings ? tile("customers", "مشتری‌ها", "users", "پیام، کیف پول، مسدود") : null,
    c.settings ? tile("sale", "تنظیمات فروش", "gauge", "لینک ساب، پاک‌سازی، پاسخ آماده") : null,
    c.settings && !d.sub_seller ? tile("wheel", "گردونهٔ شانس", "gift", "جایزهٔ روزانه برای مشتری") : null,
    d.role === "owner" && !d.owner ? tile("admins", "ادمین‌های ربات", "bot", "دعوت، نقش، حذف") : null,
  ].filter(Boolean);
  tiles.push(tile("guide", "راهنما", "chat", "معرفی بخش‌ها"));
  const out = [h("div", { class: "tiles" }, tiles)];
  if (d.role === "owner" || d.role === "manager") {
    out.push(h("div", { class: "card manual" },
      h("div", {}, h("b", {}, "ورود دستی به پنل کامل"), h("p", { class: "muted small" }, "برای کارهایی که اینجا نیست، بدون رمز وارد پنل وب شوید.")),
      btn("پنل کامل", "", fullPanel, "panel")));
  }
  return out;
}

// ---------- credit ----------
function walletView() {
  const w = M2.wallet;
  if (!w) return [backHead("اعتبار و شارژ"), spin()];
  const f = w.forecast || {};
  const out = [backHead("اعتبار و شارژ"),
    h("div", { class: "card mcredit" },
      h("div", { class: "row between" }, h("div", {}, h("div", { class: "muted small" }, "اعتبار حجمی"), h("div", { class: "big" }, size(w.credit || 0))),
        w.units ? h("div", { class: "end" }, h("div", { class: "muted small" }, "واحد نامحدود"), h("div", { class: "big" }, num(w.units))) : null),
      f.days_left >= 0 ? h("div", { class: `fc ${f.days_left <= 3 ? "bad" : ""}` }, mic("gauge"),
        h("span", {}, "با سرعت فروش این هفته حدود ", h("b", {}, f.days_left < 1 ? "کمتر از یک روز" : `${num(Math.floor(f.days_left))} روز`), " کافی است.")) : null)];
  const pk = w.packages || [];
  if (pk.length) {
    out.push(h("div", { class: "section-title" }, "شارژ اعتبار"),
      h("div", { class: "qplans" }, pk.map((p) => h("button", { class: `qplan ${f.package && f.package.id === p.id ? "hot" : ""}`, onclick: () => buyCredit(p) },
        h("b", {}, p.name), h("span", {}, `${num(p.gb)} گیگ${p.units ? ` + ${num(p.units)} واحد` : ""}`), h("span", { class: "price" }, `${toman(p.price)} تومان`)))));
  } else if (w.sub_seller) {
    out.push(h("div", { class: "card muted small" }, "اعتبار شما را نمایندهٔ بالادستتان شارژ می‌کند."));
  }
  if (pk.length) {
    const per = Math.min(...pk.map((p) => p.price / Math.max(1, p.gb)));
    const sell = h("input", { type: "number", inputmode: "numeric", placeholder: "هر گیگ را چند می‌فروشید؟ (تومان)", value: M2.sellGB || "" });
    const res = h("div", { class: "small" });
    const calc = () => {
      M2.sellGB = +sell.value || 0;
      if (!M2.sellGB) { res.replaceChildren(h("span", { class: "muted" }, `خرید شما: هر گیگ حدود ${toman(Math.round(per))} تومان (ارزان‌ترین بسته).`)); return; }
      const p = M2.sellGB - per;
      res.replaceChildren(h("div", { class: p < 0 ? "bad" : "" }, `سود هر گیگ: ${toman(Math.round(p))} تومان${p < 0 ? " (ضرر!)" : ""}`),
        h("div", { class: "muted" }, `۵۰ گیگ: ${toman(Math.round(p * 50))} · ۱۰۰ گیگ: ${toman(Math.round(p * 100))} تومان سود`));
    };
    sell.addEventListener("input", calc); calc();
    out.push(h("div", { class: "section-title" }, "ماشین‌حساب سود"), h("div", { class: "card" }, sell, res));
  }
  out.push(h("div", { class: "section-title" }, "تاریخچه"));
  if (!M2.ledger.length) out.push(h("div", { class: "card empty" }, h("p", {}, "هنوز تراکنشی نیست.")));
  out.push(h("div", { class: "ulist" }, M2.ledger.map((e) => h("div", { class: "lrow" },
    h("div", {}, h("b", {}, kindFa[e.kind] || e.kind), e.client ? h("span", { class: "ltr muted" }, ` ${e.client}`) : null,
      h("div", { class: "muted small" }, date(e.unix), e.note ? ` · ${e.note}` : "")),
    h("div", { class: "end" }, e.volume ? h("b", { class: e.volume > 0 ? "plus" : "minus" }, signed(e.volume)) : null,
      e.units ? h("div", { class: "small" }, `${e.units > 0 ? "+" : ""}${num(e.units)} واحد`) : null,
      e.amount ? h("div", { class: "small muted" }, `${toman(e.amount)} تومان`) : null)))));
  if (!M2.ledgerEnd && M2.ledger.length) {
    out.push(btn("قدیمی‌ترها", "block ghost", async () => {
      try {
        const r = await api("GET", `m/wallet?before=${M2.ledger[M2.ledger.length - 1].id}`);
        M2.ledger.push(...(r.ledger || [])); M2.ledgerEnd = (r.ledger || []).length < 50; render();
      } catch (e) { toast(e.message); }
    }));
  }
  return out;
}

async function buyCredit(p) {
  try {
    const r = await api("POST", "m/topup", { package: p.id, buyer: (tg && tg.initDataUnsafe && tg.initDataUnsafe.user && tg.initDataUnsafe.user.first_name) || "" });
    haptic("success");
    const goBot = () => { try { tg.openTelegramLink(`https://t.me/${r.bot}`); tg.close(); } catch { location.href = `https://t.me/${r.bot}`; } };
    const close = sheet(h("h2", {}, `شارژ ${num(r.gb)} گیگ · سفارش #${num(r.id)}`),
      r.card && r.card.number ? cardBox(r.price, r.card) : h("p", { class: "muted" }, "کارت پرداخت هنوز تعریف نشده؛ با مدیر پنل تماس بگیرید."),
      h("ol", { class: "steps" }, h("li", {}, "مبلغ را به کارت بالا واریز کنید."), h("li", {}, "عکس رسید را با دکمهٔ پایین همین‌جا بفرستید."),
        h("li", {}, "بعد از تأیید، اعتبار خودکار به پنل شما اضافه می‌شود.")),
      receiptButton("panel", r.id, () => close()),
      r.bot ? btn(`یا در ربات @${r.bot} بفرستید`, "ghost block", goBot, "send") : null);
  } catch (e) { haptic("error"); toast(e.message); }
}

// ---------- sub-sellers ----------
function sellersView() {
  const d = M2.sellers, owner = M.home.owner;
  const title = owner ? "نماینده‌ها" : "زیرمجموعه‌ها";
  if (!d) return [backHead(title), spin()];
  const out = [backHead(title, btnIcon("plus", "نمایندهٔ جدید", () => newSellerSheet()))];
  if (!owner) {
    out.push(h("div", { class: "card row between" }, h("div", {}, h("div", { class: "muted small" }, "اعتبار شما برای دادن"), h("b", {}, size(d.credit || 0), d.units ? ` · ${num(d.units)} واحد` : "")),
      btn("لینک دعوت", "", inviteLink, "link")));
    const [reqRow, reqBox] = check("دکمهٔ «درخواست نمایندگی» در ربات من", d.requests);
    reqBox.addEventListener("change", async () => {
      try { await api("POST", "m/sellers/requests", { on: reqBox.checked }); toast(reqBox.checked ? "روشن شد" : "خاموش شد"); } catch (e) { reqBox.checked = !reqBox.checked; toast(e.message); }
    });
    out.push(h("div", { class: "card" }, reqRow, h("p", { class: "muted small" }, "مشتری‌ها از ربات شما درخواست می‌دهند و شما با یک لمس تأیید می‌کنید.")));
  }
  if (!d.sellers.length) out.push(h("div", { class: "card empty" }, h("p", {}, owner ? "هنوز نماینده‌ای نیست." : "هنوز زیرمجموعه‌ای ندارید. با «لینک دعوت» یا دکمهٔ + بسازید.")));
  out.push(h("div", { class: "ulist" }, d.sellers.map((x) => h("button", { class: `urow ${x.enabled ? "" : "off"}`, onclick: () => sellerSheet(x) },
    h("div", { class: "u-main" },
      h("div", { class: "u-top" }, h("b", {}, x.name || x.id), h("span", { class: "ltr muted small" }, x.id), x.enabled ? null : h("span", { class: "pill" }, "خاموش")),
      h("div", { class: "u-meta" }, h("span", {}, `اعتبار: ${size(x.credit)}${x.units ? ` · ${num(x.units)} واحد` : ""}`), h("span", {}, `${num(x.clients)} کاربر`)))))));
  return out;
}

function btnIcon(ic, label, fn) { return h("button", { class: "iconbtn primary", "aria-label": label, onclick: fn }, mic(ic)); }

async function inviteLink() {
  try {
    const r = await api("POST", "m/sellers/invite", {});
    const share = () => { try { tg.openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(r.link)}&text=${encodeURIComponent("با این لینک نمایندهٔ من شوید و پنل خودتان را بگیرید")}`); } catch { copy(r.link); } };
    sheet(h("h2", {}, "🔗 لینک دعوت نماینده"), h("p", { class: "muted small" }, "تا ۷ روز معتبر است. هر کس با آن ربات را باز کند، همان لحظه زیرمجموعهٔ شما می‌شود؛ اعتبار ندارد تا شما بدهید."),
      h("div", { class: "sublink ltr", onclick: () => copy(r.link) }, r.link),
      h("div", { class: "row" }, btn("کپی", "primary", () => copy(r.link), "copy"), btn("ارسال", "", share, "share")));
  } catch (e) { toast(e.message); }
}

function newSellerSheet() {
  const owner = M.home.owner;
  const id = h("input", { class: "ltr", placeholder: "مثلاً ali-shop", autocapitalize: "off", autocomplete: "off", spellcheck: "false" });
  const name = h("input", { placeholder: "نام نمایشی" });
  const gb = gbIn("", "۰"), units = gbIn("", "۰");
  const tgid = h("input", { class: "ltr", type: "number", inputmode: "numeric", placeholder: "اختیاری" });
  const go = btn("بساز", "primary block big", async () => {
    go.disabled = true;
    try {
      const r = await api("POST", "m/sellers/new", { id: id.value.trim().toLowerCase(), name: name.value.trim(), credit_gb: +gb.value || 0, units: +units.value || 0, tg: +tgid.value || 0 });
      close(); haptic("success");
      const txt = `نام کاربری: ${r.username}\nرمز: ${r.password}`;
      sheet(h("h2", {}, "✅ نماینده ساخته شد"), h("p", { class: "muted small" }, "این رمز فقط همین یک بار نشان داده می‌شود. اگر آیدی تلگرام داده باشید، او بدون رمز از ربات هم مدیریت می‌کند."),
        h("div", { class: "kv" }, h("span", {}, "نام کاربری"), h("b", { class: "ltr" }, r.username)),
        h("div", { class: "kv" }, h("span", {}, "رمز"), h("b", { class: "ltr" }, r.password)),
        btn("کپی هر دو", "primary block", () => copy(txt), "copy"));
      m2Reload();
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, owner ? "نمایندهٔ جدید" : "زیرمجموعهٔ جدید"),
    field("نام کاربری (انگلیسی)", id), field("نام", name),
    h("div", { class: "two" }, field(owner ? "اعتبار (گیگ)" : "اعتبار از خودم (گیگ)", gb), field("واحد نامحدود", units)),
    field("آیدی عددی تلگرامِ او", tgid, "تا پنلش را از ربات و مینی‌اپ مدیریت کند"), go);
}

function sellerSheet(x) {
  const owner = M.home.owner;
  const gb = gbIn("", "گیگ"), units = gbIn("", "واحد"), note = h("input", { placeholder: "یادداشت (اختیاری)" });
  const move = async (sign) => {
    const v = (+gb.value || 0) * sign, u = Math.trunc(+units.value || 0) * sign;
    if (!v && !u) { toast("مقدار را بنویسید"); return; }
    try {
      await api("POST", "m/sellers/credit", { id: x.id, gb: v, units: u, note: note.value });
      close(); haptic("success"); toast(sign > 0 ? "اعتبار داده شد" : "اعتبار پس گرفته شد"); m2Reload(); mLoad();
    } catch (e) { haptic("error"); toast(e.message); }
  };
  const close = sheet(h("div", { class: "row between" }, h("h2", {}, x.name || x.id), h("span", { class: "ltr muted" }, x.id)),
    h("div", { class: "kv" }, h("span", {}, "اعتبار"), h("b", {}, size(x.credit), x.units ? ` · ${num(x.units)} واحد` : "")),
    h("div", { class: "kv" }, h("span", {}, "کاربرها"), h("b", {}, num(x.clients))),
    h("div", { class: "kv" }, h("span", {}, "مصرف"), h("b", {}, size(x.used || 0))),
    h("div", { class: "section-title" }, owner ? "تغییر اعتبار" : "انتقال اعتبار"),
    h("div", { class: "two" }, field("حجم (گیگ)", gb), field("واحد نامحدود", units)), note,
    h("div", { class: "row" }, btn(owner ? "افزودن" : "بده", "primary", () => move(1), "plus"), btn(owner ? "کم کردن" : "پس بگیر", "", () => move(-1), "renew")),
    h("div", { class: "row" }, btn("محدودیت‌ها", "", () => { close(); policySheet(x); }, "gauge"), btn("پیام", "", () => { close(); sellerMessageSheet(x); }, "send")),
    btn(x.enabled ? "خاموش کردن پنل" : "روشن کردن پنل", x.enabled ? "danger block" : "block", async () => {
      try { await api("POST", "m/sellers/enable", { id: x.id, on: !x.enabled }); close(); toast(x.enabled ? "خاموش شد" : "روشن شد"); m2Reload(); } catch (e) { toast(e.message); }
    }, "power"));
}

// ---------- the reseller's bot ----------
function botView() {
  const d = M2.bot;
  if (!d) return [backHead("ربات من"), spin()];
  const b = d.bot || {};
  const state = b.running ? ["good", `فعال${b.username ? " · @" + b.username.replace(/^@/, "") : ""}`] : b.enabled ? ["warn", "در حال اتصال…"] : b.has_token ? ["", "خاموش"] : ["", "ثبت نشده"];
  const tok = h("input", { class: "ltr", placeholder: b.has_token ? "برای عوض کردن، توکن تازه را بگذارید" : "123456:ABC… از @BotFather", autocomplete: "off", spellcheck: "false" });
  const [onRow, onBox] = check("ربات روشن باشد", b.enabled || !b.has_token);
  const [appRow, appBox] = check("دکمهٔ فروشگاه (مینی‌اپ) در ربات", b.miniapp !== false);
  const save = btn("ذخیره", "primary block big", async () => {
    save.disabled = true;
    try {
      M2.bot = Object.assign(M2.bot, await api("POST", "m/bot", { token: tok.value.trim(), enabled: onBox.checked, miniapp: appBox.checked }));
      haptic("success"); toast("ذخیره شد"); render();
      setTimeout(() => { if (M.view === "bot") m2Reload(); }, 4000);
    } catch (e) { save.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  const out = [backHead("ربات من"),
    h("div", { class: "card" }, h("div", { class: "row between" }, h("b", {}, "وضعیت"), h("span", { class: `pill ${state[0]}` }, state[1])),
      b.error ? h("p", { class: "small bad" }, b.error) : null,
      b.running && b.username ? btn("باز کردن ربات", "block ghost", () => { try { tg.openTelegramLink(`https://t.me/${b.username.replace(/^@/, "")}`); } catch {} }, "send") : null)];
  if (d.mirza) out.push(h("div", { class: "card small" }, "⚠️ فروش شما الان با ربات میرزا انجام می‌شود؛ تا وقتی آن وصل است این ربات خاموش می‌ماند."));
  out.push(h("div", { class: "card" }, field("توکن ربات", tok, "در @BotFather دستور /newbot را بزنید و توکنی را که می‌دهد اینجا بگذارید."), onRow, appRow, save));
  if (b.has_token) {
    out.push(btn("حذف ربات", "danger block ghost", () => {
      const close = sheet(h("h2", {}, "ربات حذف شود؟"), h("p", { class: "muted" }, "مشتری‌ها دیگر از این ربات خرید نمی‌کنند تا دوباره ثبتش کنید."),
        h("div", { class: "row" }, btn("بله، حذف", "danger", async () => {
          close();
          try { await api("POST", "m/bot", { token: "-", enabled: false }); toast("حذف شد"); m2Reload(); } catch (e) { toast(e.message); }
        }, "trash"), btn("انصراف", "", () => close())));
    }, "trash"));
  }
  return out;
}

// ---------- payment cards ----------
function cardsView() {
  const d = M2.cards;
  if (!d) return [backHead("کارت‌های پرداخت"), spin()];
  const rows = (d.cards || []).map((c) => ({ ...c }));
  const box = h("div", { class: "ulist" });
  const draw = () => box.replaceChildren(...rows.map((c, i) => {
    const num_ = h("input", { class: "ltr", inputmode: "numeric", value: c.number || "", placeholder: "شمارهٔ ۱۶ رقمی کارت", oninput: (e) => { c.number = e.target.value; } });
    const holder = h("input", { value: c.holder || "", placeholder: "به نام", oninput: (e) => { c.holder = e.target.value; } });
    const bank = h("input", { value: c.bank || "", placeholder: "بانک", oninput: (e) => { c.bank = e.target.value; } });
    const cap = h("input", { type: "number", inputmode: "numeric", value: c.daily_cap || "", placeholder: "۰ = بی‌سقف", oninput: (e) => { c.daily_cap = +e.target.value || 0; } });
    const [onRow, onBox] = check("فعال", c.enabled !== false);
    onBox.addEventListener("change", () => { c.enabled = onBox.checked; });
    return h("div", { class: "card ccard" },
      h("div", { class: "row between" }, h("b", {}, `کارت ${num(i + 1)}`), c.used_today ? h("small", { class: "muted" }, `امروز ${toman(c.used_today)} تومان`) : null,
        h("button", { class: "linkbtn bad", onclick: () => { rows.splice(i, 1); draw(); } }, "حذف")),
      num_, h("div", { class: "two" }, holder, bank), field("سقف دریافت روزانه (تومان)", cap), onRow);
  }));
  draw();
  const sf = d.safety || {};
  const [phRow, phBox] = check("اول شمارهٔ تلگرام مشتری گرفته شود", sf.require_phone);
  const [irRow, irBox] = check("فقط شماره‌های ایران (+98)", sf.iran_only);
  const [l4Row, l4Box] = check("۴ رقم آخر کارت واریزکننده پرسیده شود", sf.ask_last4);
  const save = btn("ذخیرهٔ کارت‌ها", "primary block big", async () => {
    save.disabled = true;
    try {
      await api("POST", "m/cards", { cards: rows.filter((c) => (c.number || "").replace(/\D/g, "")), safety: { require_phone: phBox.checked, iran_only: irBox.checked, ask_last4: l4Box.checked } });
      haptic("success"); toast("ذخیره شد"); m2Reload();
    } catch (e) { save.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  return [backHead("کارت‌های پرداخت"),
    h("p", { class: "muted small" }, "با چند کارت، هر سفارش یک کارت می‌گیرد و وقتی سقف روزانهٔ کارتی پر شد، کارت بعدی نشان داده می‌شود."),
    box,
    rows.length < (d.max || 5) ? btn("افزودن کارت", "block ghost", () => { rows.push({ enabled: true }); draw(); }, "plus") : null,
    h("div", { class: "section-title" }, "امنیت پرداخت"),
    h("div", { class: "card" }, phRow, irRow, l4Row),
    save];
}

// ---------- plans and prices ----------
function plansView() {
  const d = M2.plans;
  if (!d) return [backHead("پلن‌ها و قیمت‌ها"), spin()];
  const out = [backHead("پلن‌ها و قیمت‌ها", d.edit ? btnIcon("plus", "پلن جدید", () => planSheet({})) : null)];
  if (!d.edit) out.push(h("div", { class: "card small muted" }, "ساخت و ویرایش پلن برای شما فعال نیست؛ پلن‌ها را نمایندهٔ بالادست یا مدیر پنل تعیین می‌کند."));
  if (!d.plans.length) out.push(h("div", { class: "card empty" }, h("p", {}, "هنوز پلنی ندارید. پلن‌ها در فروشگاه ربات و ساخت سریع نشان داده می‌شوند.")));
  if (M2.custom) out.push(customPlanCard(M2.custom));
  out.push(h("div", { class: "ulist" }, d.plans.map((p) => h("button", { class: `urow ${p.status === "published" ? "" : "off"}`, onclick: () => d.edit && planSheet(p) },
    h("div", { class: "u-main" },
      h("div", { class: "u-top" }, h("b", {}, p.name), h("span", { class: `pill ${p.status === "published" ? "good" : ""}` }, p.status === "published" ? "در فروشگاه" : "پیش‌نویس")),
      h("div", { class: "u-meta" }, h("span", {}, `${p.plan === "unlimited" ? "نامحدود" : num(p.gb) + " گیگ"} · ${num(p.days)} روز${p.max_ips ? ` · ${num(p.max_ips)} کاربر` : ""}`),
        h("b", {}, p.price ? `${toman(p.price)} تومان` : "بدون قیمت")))))));
  return out;
}

function customPlanCard(c) {
  const n = (v, ph) => h("input", { type: "number", inputmode: "numeric", value: v || "", placeholder: ph || "" });
  const [onRow, on] = check("مشتری حجم و روز را خودش انتخاب کند (نوار کشویی در فروشگاه)", c.enabled);
  const pg = n(c.price_gb, "تومان"), p10 = n(c.price_10d, "تومان"), g1 = n(c.min_gb), g2 = n(c.max_gb), d1 = n(c.min_days), d2 = n(c.max_days), ips = n(c.ips, "۰ = بی‌سقف");
  const ex = h("div", { class: "muted small" });
  const draw = () => { ex.textContent = `مثال: ۵۰ گیگ ۳۰ روزه = ${toman(50 * (+pg.value || 0) + 3 * (+p10.value || 0))} تومان`; };
  [pg, p10].forEach((i) => i.addEventListener("input", draw)); draw();
  const save = btn("ذخیره", "primary block", async () => {
    try {
      M2.custom = await api("POST", "m/custom", { enabled: on.checked, price_gb: Math.round(+pg.value || 0), price_10d: Math.round(+p10.value || 0), min_gb: Math.round(+g1.value || 0),
        max_gb: Math.round(+g2.value || 0), min_days: Math.round(+d1.value || 0), max_days: Math.round(+d2.value || 0), ips: Math.round(+ips.value || 0) });
      haptic("success"); toast("ذخیره شد"); render();
    } catch (e) { haptic("error"); toast(e.message); }
  }, "check");
  return h("div", {}, h("div", { class: "section-title" }, "🧩 حجم و زمان دلخواه"),
    h("div", { class: "card" }, onRow,
      h("div", { class: "two" }, field("قیمت هر گیگ", pg), field("قیمت هر ۱۰ روز", p10)),
      h("div", { class: "two" }, field("کمترین گیگ", g1), field("بیشترین گیگ", g2)),
      h("div", { class: "two" }, field("کمترین روز", d1), field("بیشترین روز", d2)),
      field("کاربر هم‌زمان", ips), ex, save));
}

function planSheet(p) {
  const pol = M2.plans.policy || {};
  const name = h("input", { value: p.name || "", placeholder: "مثلاً ۳۰ گیگ یک‌ماهه" });
  const gb = gbIn(p.gb || "", "گیگ"), days = gbIn(p.days || 30), ips = gbIn(p.max_ips || 0), price = gbIn(p.price || "", "تومان");
  const [unlRow, unl] = check("حجم نامحدود", p.plan === "unlimited");
  const [fuRow, fu] = check("زمان از اولین اتصال شروع شود", p.first_use);
  const [nrRow, nr] = check("فقط خرید تازه (نه تمدید)", p.no_renew);
  const [pubRow, pub] = check("در فروشگاه نشان داده شود", p.status ? p.status === "published" : true);
  const go = btn("ذخیره", "primary block big", async () => {
    go.disabled = true;
    try {
      await api("POST", "m/plan", { id: p.id || "", name: name.value.trim(), plan: unl.checked ? "unlimited" : "volume", gb: unl.checked ? 0 : Math.round(+gb.value || 0),
        days: Math.round(+days.value || 0), max_ips: Math.round(+ips.value || 0), price: Math.round(+price.value || 0), first_use: fu.checked, no_renew: nr.checked, status: pub.checked ? "published" : "draft" });
      close(); haptic("success"); toast("ذخیره شد"); m2Reload(); mLoad();
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, p.id ? "ویرایش پلن" : "پلن جدید"), field("نام", name),
    h("div", { class: "two" }, field("حجم (گیگ)", gb), field("روز", days)),
    h("div", { class: "two" }, field("کاربر هم‌زمان (۰ = بی‌سقف)", ips), field("قیمت (تومان)", price)),
    pol.allow_unlimited !== false ? unlRow : null, fuRow, nrRow, pubRow,
    pol.max_days || pol.max_client_gb ? h("p", { class: "muted small" }, `سقف شما: ${pol.max_days ? num(pol.max_days) + " روز" : ""}${pol.max_days && pol.max_client_gb ? " · " : ""}${pol.max_client_gb ? num(pol.max_client_gb) + " گیگ" : ""}`) : null,
    go,
    p.id ? btn("حذف پلن", "danger block ghost", async () => {
      try { await api("POST", "m/plan/delete", { id: p.id }); close(); toast("حذف شد"); m2Reload(); mLoad(); } catch (e) { toast(e.message); }
    }, "trash") : null);
}

// ---------- tickets ----------
const tSt = { open: ["warn", "منتظر جواب"], answered: ["good", "جواب داده شد"], closed: ["", "بسته"] };
function ticketsView() {
  const chip = (s, label) => h("button", { class: `chip ${M2.tStatus === s ? "on" : ""}`, onclick: () => { M2.tStatus = s; m2Reload(); } }, label);
  const out = [backHead("تیکت‌ها"), h("div", { class: "chips" }, chip("active", "باز"), chip("closed", "بسته"))];
  if (!M2.tickets.length) out.push(h("div", { class: "card empty" }, h("p", {}, M2.tStatus === "active" ? "تیکت بازی نیست ✨" : "تیکت بسته‌ای نیست.")));
  out.push(h("div", { class: "ulist" }, M2.tickets.map((t) => {
    const [tone, label] = tSt[t.status] || ["", t.status];
    return h("button", { class: `urow ${t.status === "open" ? "warn" : ""}`, onclick: () => ticketSheet(t.id) },
      h("div", { class: "u-main" },
        h("div", { class: "u-top" }, h("b", {}, t.name || `#${t.chat}`), h("span", { class: `pill ${tone}` }, label)),
        h("div", { class: "muted small clamp" }, t.last || ""),
        h("div", { class: "u-meta" }, h("span", {}, `#${num(t.id)} · ${num(t.count)} پیام`), h("span", {}, date(t.updated)))));
  })));
  return out;
}

async function ticketSheet(id) {
  let t;
  try { t = await api("GET", `m/ticket?id=${id}`); } catch (e) { toast(e.message); return; }
  const txt = h("textarea", { rows: "3", placeholder: "جواب شما…", maxlength: "3000" });
  if (!M2.quick) { try { M2.quick = await api("GET", "m/quick"); } catch { M2.quick = []; } }
  const quick = M2.quick.length ? h("div", { class: "chips quick" }, M2.quick.map((q) => h("button", { class: "chip", onclick: () => { txt.value = txt.value ? txt.value + "\n" + q : q; } }, q.length > 28 ? q.slice(0, 28) + "…" : q))) : null;
  const send = async (closeIt) => {
    if (!txt.value.trim() && !closeIt) { toast("متن جواب را بنویسید"); return; }
    try {
      await api("POST", "m/ticket", { id, text: txt.value, close: closeIt });
      close(); haptic("success"); toast(closeIt ? "تیکت بسته شد" : "فرستاده شد"); m2Reload(); mLoad();
    } catch (e) { haptic("error"); toast(e.message); }
  };
  const close = sheet(h("div", { class: "row between" }, h("h2", {}, t.name || `#${t.chat}`), h("span", { class: "muted small" }, `#${num(t.id)}`)),
    t.clients && t.clients.length ? h("div", { class: "muted small ltr" }, t.clients.join(" · ")) : null,
    h("div", { class: "thread" }, (t.msgs || []).map((m) => h("div", { class: `bubble ${m.staff ? "me" : ""}`, onclick: () => copy(m.text) },
      h("div", {}, m.text),
      m.photo ? h("button", { class: "linkbtn", onclick: async (e) => {
        e.stopPropagation();
        try { const r = await api("GET", `m/ticket/photo?msg=${m.id}`); sheet(h("img", { class: "receipt", src: r.image, alt: "عکس" })); } catch (x) { toast(x.message); }
      } }, "📷 نمایش عکس") : null,
      h("small", {}, `${m.staff ? m.author || "پشتیبانی" : ""}${m.staff ? " · " : ""}${date(m.unix)}`)))),
    t.status !== "closed" ? quick : null,
    t.status !== "closed" ? txt : null,
    t.status !== "closed" ? h("div", { class: "row" }, btn("ارسال", "primary", () => send(false), "send"), btn("ارسال و بستن", "", () => send(true), "check")) : h("p", { class: "muted small" }, "این تیکت بسته است."));
}

// ---------- reports ----------
function reportView() {
  const r = M2.report;
  const chip = (n, label) => h("button", { class: `chip ${M2.days === n ? "on" : ""}`, onclick: () => { M2.days = n; m2Reload(); } }, label);
  const out = [backHead("گزارش فروش"), h("div", { class: "chips" }, chip(7, "۷ روز"), chip(30, "۳۰ روز"), chip(90, "۹۰ روز"))];
  if (!r) { out.push(spin()); return out; }
  const t = r.total || {};
  const kpi = (label, v) => h("div", { class: "kpi" }, h("b", {}, v), h("span", {}, label));
  out.push(h("div", { class: "kpis four" }, kpi("فروش", num(t.sales || 0)), kpi("حجم فروخته", size(t.sold || 0)), kpi("درآمد کارت و کیف پول", `${toman(t.toman || 0)}`), kpi("برگشتی", size(t.back || 0))));
  const days = r.days || [];
  const max = Math.max(1, ...days.map((d) => d.sold));
  out.push(h("div", { class: "card" }, h("div", { class: "muted small" }, "حجم فروخته در هر روز"),
    h("div", { class: "rbars", style: `--n:${days.length}` }, days.map((d) => h("i", { style: `--h:${Math.round((d.sold / max) * 100)}%`, title: `${d.day}: ${size(d.sold)} · ${num(d.sales)} فروش` }))),
    h("div", { class: "row between muted small" }, h("span", {}, days.length ? days[0].day : ""), h("span", {}, days.length ? days[days.length - 1].day : ""))));
  const recent = days.slice().reverse().filter((d) => d.sales || d.toman || d.back).slice(0, 14);
  if (recent.length) {
    out.push(h("div", { class: "ulist" }, recent.map((d) => h("div", { class: "lrow" },
      h("div", {}, h("b", {}, d.day), h("div", { class: "muted small" }, `${num(d.sales)} فروش${d.orders ? ` · ${num(d.orders)} سفارش` : ""}`)),
      h("div", { class: "end" }, h("b", {}, size(d.sold)), d.toman ? h("div", { class: "small muted" }, `${toman(d.toman)} تومان`) : null)))));
  }
  out.push(btn("دانلود گزارش اکسل", "primary block big", downloadXLSX, "down"));
  return out;
}

async function downloadXLSX() {
  try {
    const r = await api("POST", "m/report.link", { days: M2.days === 7 ? 7 : M2.days === 90 ? 90 : 30 });
    try {
      if (tg && tg.isVersionAtLeast && tg.isVersionAtLeast("8.0") && tg.downloadFile) { tg.downloadFile({ url: r.url, file_name: r.name }); return; }
      tg.openLink(r.url);
    } catch { location.href = r.url; }
  } catch (e) { toast(e.message); }
}

function spin() { return h("div", { class: "boot" }, h("span", { class: "spinner" })); }

// ---------- panel sale ----------
function resaleView() {
  const d = M2.resale;
  if (!d) return [backHead("فروش پنل نمایندگی"), spin()];
  if (d.owner) return ownerSaleView(d);
  if (!d.allowed) return [backHead("فروش پنل نمایندگی"), h("div", { class: "card" }, h("p", {}, "فروش پنل برای شما فعال نیست. اگر می‌خواهید نمایندهٔ ویژه شوید و پنل بفروشید، از مدیر پنل بخواهید."))];
  const v = d.view, st = d.stats || {}, out = [backHead("فروش پنل نمایندگی", btnIcon("plus", "فروش پنل", () => sellPanelSheet()))];
  if (v.locked) out.push(h("div", { class: "card alert bad" }, "⛔️ یک صورتحساب از مهلتش گذشته؛ تا پرداخت آن، فروش پنل و ساخت کاربر قفل است."));
  const capBox = h("div", {});
  api("GET", "m/panellimits").then((c) => {
    const max = c.limits.per_seller, sold = c.sold || 0, pct = Math.min(100, Math.round((sold / max) * 100));
    capBox.replaceChildren(h("div", { class: `capbar ${sold >= max ? "bad" : pct >= 80 ? "warn" : ""}` },
      h("div", { class: "row between" }, h("b", {}, "ظرفیت فروش پنل"), h("span", {}, `${num(sold)} از ${num(max)}`)),
      h("div", { class: "capbar-bar" }, h("i", { style: `--w:${pct}%` })),
      h("p", { class: "muted small" }, sold >= max ? "ظرفیت پر است؛ پنل تازه فروخته نمی‌شود (شارژ پنل‌های فروخته‌شده آزاد است)." : `هنوز ${num(max - sold)} پنل دیگر می‌توانید بفروشید. سفارش‌های در انتظار هم حساب می‌شوند.`)));
  }).catch(() => {});
  out.push(capBox);
  out.push(h("div", { class: "kpis four" },
    h("div", { class: "kpi" }, h("b", {}, num(st.panels || 0)), h("span", {}, "پنل فروخته")),
    h("div", { class: "kpi" }, h("b", {}, num(st.month_sales || 0)), h("span", {}, "فروش این ماه")),
    h("div", { class: "kpi" }, h("b", {}, toman(st.month_paid || 0)), h("span", {}, "درآمد ماه (تومان)")),
    h("div", { class: "kpi" }, h("b", {}, toman((st.month_paid || 0) - (st.month_cost || 0))), h("span", {}, "سود ماه (تومان)"))));
  const bills = (v.bills || []).filter((b) => b.status === "open");
  out.push(h("div", { class: "card" },
    h("div", { class: "row between" }, h("div", {}, h("div", { class: "muted small" }, "کیف پول نزد مدیر پنل"), h("b", { class: "big" }, `${toman(v.wallet || 0)} تومان`)),
      btn("شارژ", "primary", resaleWalletSheet, "wallet")),
    h("p", { class: "muted small" }, `هر فروش، قیمت عمده (هر ترابایت ${toman(v.wholesale_tb || 0)} تومان) را از این کیف پول کم می‌کند${v.resale && v.resale.credit ? "؛ اگر کافی نبود، صورتحساب ۲۴ ساعته صادر می‌شود" : ""}.`),
    bills.map((b) => h("div", { class: "lrow" }, h("div", {}, h("b", {}, `صورتحساب #${num(b.id)}`), h("div", { class: `small ${b.overdue ? "bad" : "muted"}` }, b.overdue ? "سررسید گذشته" : `مهلت: ${date(b.due_unix)}`)),
      h("div", { class: "end" }, h("b", {}, `${toman(b.amount)} تومان`), btn("پرداخت", "", async () => {
        try { await api("POST", "m/resale/bill", { id: b.id }); toast("پرداخت شد"); m2Reload(); } catch (e) { toast(e.message); }
      }))))));
  // Selling in my bot and my prices.
  const [onRow, onBox] = check("فروش پنل در ربات من (دکمهٔ «پنل نمایندگی» برای مشتری‌ها)", v.resale ? v.resale.enabled : true);
  const prices = {};
  const pk = (v.packages || []).map((p) => {
    const inp = h("input", { type: "number", inputmode: "numeric", value: p.own || "", placeholder: toman(p.owner) });
    prices[p.id] = inp;
    return h("div", { class: "lrow" }, h("div", {}, h("b", {}, p.name), h("div", { class: "muted small" }, `${num(p.gb)} گیگ · عمده ${toman(p.cost)} تومان`)), h("div", { class: "pricein" }, inp));
  });
  out.push(btn("🎉 بنر و تخفیف فروش پنل", "block", promoSheet));
  out.push(h("div", { class: "section-title" }, "قیمت فروش من"),
    h("div", { class: "card" }, onRow, h("p", { class: "muted small" }, "خالی = همان قیمت مدیر پنل. سود هر فروش = قیمت شما منهای قیمت عمده."), pk,
      btn("ذخیره", "primary block", async () => {
        const out_ = {};
        for (const [id, inp] of Object.entries(prices)) if (+inp.value > 0) out_[id] = Math.round(+inp.value);
        try { await api("POST", "m/resale/prices", { enabled: onBox.checked, prices: out_ }); haptic("success"); toast("ذخیره شد"); m2Reload(); } catch (e) { toast(e.message); }
      }, "check")));
  out.push(h("div", { class: "section-title" }, "پنل‌های فروخته"));
  if (!d.panels.length) out.push(h("div", { class: "card empty" }, h("p", {}, "هنوز پنلی نفروخته‌اید. با دکمهٔ + بفروشید یا بگذارید مشتری‌ها از ربات‌تان بخرند.")));
  out.push(h("div", { class: "ulist" }, d.panels.map((x) => h("button", { class: `urow ${x.enabled ? "" : "off"}`, onclick: () => soldPanelSheet(x) },
    h("div", { class: "u-main" },
      h("div", { class: "u-top" }, h("b", { class: "ltr" }, x.login || x.id), x.enabled ? null : h("span", { class: "pill" }, "خاموش")),
      h("div", { class: "u-meta" }, h("span", {}, `اعتبار ${size(x.credit)} · ${num(x.clients)} کاربر`), h("span", {}, `${toman(x.paid)} تومان`)))))));
  return out;
}

function pkgSelect(pkgs) {
  const sel = h("select", {}, pkgs.map((p) => h("option", { value: p.id }, `${p.name} · ${num(p.gb)} گیگ · ${toman(p.price)} تومان`)));
  return sel;
}

function sellPanelSheet(target) {
  const v = M2.resale.view;
  const pk = pkgSelect(v.packages || []);
  const name = h("input", { class: "ltr", placeholder: "مثلاً reza-shop", autocapitalize: "off", autocomplete: "off", spellcheck: "false" });
  const buyer = h("input", { placeholder: "اسم خریدار (اختیاری)" });
  const go = btn(target ? "شارژ پنل" : "بفروش", "primary block big", async () => {
    go.disabled = true;
    try {
      const r = await api("POST", "m/resale/sell", { target: target || "", name: name.value.trim().toLowerCase(), buyer: buyer.value, package: pk.value });
      close(); haptic("success");
      if (target) { celebrate("پنل شارژ شد"); m2Reload(); return; }
      const txt = `🌐 ${r.address}\n👤 ${r.username}\n🔑 ${r.password}`;
      sheet(h("h2", {}, "✅ پنل ساخته شد"),
        r.bill ? h("p", { class: "small bad" }, `کیف پول کافی نبود؛ صورتحساب #${num(r.bill)} صادر شد و تا ۲۴ ساعت باید پرداخت شود.`) : h("p", { class: "muted small" }, `قیمت عمده (${toman(r.cost)} تومان) از کیف پول کم شد.`),
        h("div", { class: "kv" }, h("span", {}, "آدرس"), h("b", { class: "ltr" }, r.address)),
        h("div", { class: "kv" }, h("span", {}, "نام کاربری"), h("b", { class: "ltr" }, r.username)),
        h("div", { class: "kv" }, h("span", {}, "رمز"), h("b", { class: "ltr" }, r.password)),
        btn("کپی برای خریدار", "primary block", () => copy(txt), "copy"));
      m2Reload();
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, target ? `شارژ پنل ${target}` : "فروش پنل نمایندگی"), field("بسته", pk),
    target ? null : field("نام کاربری پنل (انگلیسی)", name), target ? null : field("خریدار", buyer), go);
}

function soldPanelSheet(x) {
  const close = sheet(h("div", { class: "row between" }, h("h2", { class: "ltr" }, x.login || x.id), x.enabled ? h("span", { class: "pill good" }, "فعال") : h("span", { class: "pill" }, "خاموش")),
    h("div", { class: "kv" }, h("span", {}, "اعتبار"), h("b", {}, size(x.credit))),
    h("div", { class: "kv" }, h("span", {}, "کاربرها"), h("b", {}, `${num(x.clients)} (${num(x.active)} فعال)`)),
    h("div", { class: "kv" }, h("span", {}, "فروخته در"), h("b", {}, x.sold ? date(x.sold) : "—")),
    h("div", { class: "kv" }, h("span", {}, "پرداختی خریدار / عمدهٔ شما"), h("b", {}, `${toman(x.paid)} / ${toman(x.cost)}`)),
    h("div", { class: "actions" },
      btn("شارژ پنل", "primary", () => { close(); sellPanelSheet(x.id); }, "plus"),
      btn(x.enabled ? "خاموش" : "روشن", "", async () => {
        try { await api("POST", "m/resale/panel", { id: x.id, on: !x.enabled }); close(); toast("انجام شد"); m2Reload(); } catch (e) { toast(e.message); }
      }, "power"),
      btn("رمز تازه", "", async () => {
        try {
          const r = await api("POST", "m/resale/panel", { id: x.id, password: true });
          close(); sheet(h("h2", {}, "🔑 رمز تازه"), h("div", { class: "kv" }, h("span", {}, "نام کاربری"), h("b", { class: "ltr" }, r.username)),
            h("div", { class: "kv" }, h("span", {}, "رمز"), h("b", { class: "ltr" }, r.password)), btn("کپی", "primary block", () => copy(`${r.username}\n${r.password}`), "copy"));
        } catch (e) { toast(e.message); }
      }, "renew")));
}

function resaleWalletSheet() {
  const amt = h("input", { type: "number", inputmode: "numeric", placeholder: "مثلاً ۵۰۰۰۰۰۰" });
  const go = btn("ادامه", "primary block big", async () => {
    go.disabled = true;
    try {
      const r = await api("POST", "m/resale/wallet", { amount: Math.round(+amt.value || 0) });
      close(); payToOwnerSheet(`شارژ کیف پول · سفارش #${num(r.id)}`, r);
    } catch (e) { go.disabled = false; toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, "شارژ کیف پول فروش پنل"), field("مبلغ (تومان)", amt), go);
}

function payToOwnerSheet(title, r) {
  const goBot = () => { try { tg.openTelegramLink(`https://t.me/${r.bot}`); tg.close(); } catch { location.href = `https://t.me/${r.bot}`; } };
  const close = sheet(h("h2", {}, title), r.card && r.card.number ? cardBox(r.price, r.card) : h("p", { class: "muted" }, "کارت پرداخت هنوز تعریف نشده."),
    h("ol", { class: "steps" }, h("li", {}, "مبلغ را به کارت بالا واریز کنید."), h("li", {}, "عکس رسید را با دکمهٔ پایین همین‌جا بفرستید."), h("li", {}, "بعد از تأیید، خودکار انجام می‌شود.")),
    receiptButton("panel", r.id, () => close()),
    r.bot ? btn(`یا در ربات @${r.bot} بفرستید`, "ghost block", goBot, "send") : null);
}

function ownerSaleView(d) {
  const rows = (d.packages || []).map((p) => ({ id: p.id, name: p.name, gb: p.gb, price: p.price }));
  const [onRow, onBox] = check("فروش پنل نمایندگی در ربات اصلی و مینی‌اپ", d.enabled);
  const box = h("div", { class: "ulist" });
  const draw = () => box.replaceChildren(...rows.map((p, i) => h("div", { class: "card ccard" },
    h("div", { class: "row between" }, h("b", {}, `بسته ${num(i + 1)}`), h("button", { class: "linkbtn bad", onclick: () => { rows.splice(i, 1); draw(); } }, "حذف")),
    h("input", { value: p.name || "", placeholder: "نام بسته", oninput: (e) => { p.name = e.target.value; } }),
    h("div", { class: "two" }, field("حجم (گیگ)", h("input", { type: "number", inputmode: "numeric", value: p.gb || "", oninput: (e) => { p.gb = Math.round(+e.target.value || 0); } })),
      field("قیمت (تومان)", h("input", { type: "number", inputmode: "numeric", value: p.price || "", oninput: (e) => { p.price = Math.round(+e.target.value || 0); } }))))));
  draw();
  const capsBox = h("div", {});
  api("GET", "m/panellimits").then((c) => {
    const pb = h("input", { type: "number", inputmode: "numeric", value: c.limits.per_buyer });
    const ps = h("input", { type: "number", inputmode: "numeric", value: c.limits.per_seller });
    capsBox.replaceChildren(h("div", { class: "section-title" }, "سقف پنل‌ها"),
      h("div", { class: "card" }, h("div", { class: "two" }, field("حداکثر پنل هر خریدار", pb), field("ظرفیت فروش هر نمایندهٔ ویژه", ps)),
        h("p", { class: "muted small" }, "هر حساب تلگرام بیشتر از این تعداد پنل نمی‌تواند بخرد؛ هر نمایندهٔ ویژه هم بیشتر از ظرفیتش پنل نمی‌فروشد. سفارش‌های در انتظار هم حساب می‌شوند. شارژ پنل‌های موجود همیشه آزاد است."),
        btn("ذخیرهٔ سقف‌ها", "block", async () => {
          try { await api("POST", "m/panellimits", { per_buyer: Math.round(+pb.value || 0), per_seller: Math.round(+ps.value || 0) }); haptic("success"); toast("ذخیره شد"); }
          catch (e) { haptic("error"); toast(e.message); }
        }, "check")));
  }).catch(() => {});
  const trialBox = h("div", {});
  api("GET", "m/reptrial").then((t) => {
    const p = t.policy, st = t.stats || {};
    const [tOn, tBox] = check("هر حساب تلگرام یک پنل تستی رایگان بگیرد (ربات اصلی و مینی‌اپ)", p.on);
    const gb = h("input", { type: "number", inputmode: "decimal", value: p.gb || 5 });
    const days = h("input", { type: "number", inputmode: "numeric", value: p.days || "", placeholder: "۰ = بی‌پایان" });
    const mc = h("input", { type: "number", inputmode: "numeric", value: p.max_clients || "", placeholder: "۰ = بی‌سقف" });
    trialBox.replaceChildren(h("div", { class: "section-title" }, "🎁 پنل نمایندگی تستی"),
      h("div", { class: "card" }, tOn, h("div", { class: "two" }, field("اعتبار رایگان (گیگ)", gb), field("مدت (روز)", days)), field("سقف کاربر", mc),
        h("p", { class: "muted small" }, `در حال اجرا: ${num(st.running || 0)} · تمام‌شده و قفل: ${num(st.frozen || 0)}. یک روز قبل از پایان به صاحب پنل خبر می‌دهیم؛ سر مهلت کاربرهایش خاموش و پنل قفل می‌شود و با اولین شارژ پولی دائمی می‌شود.`),
        btn("ذخیرهٔ پنل تستی", "block", async () => {
          try { await api("POST", "m/reptrial", { on: tBox.checked, gb: +gb.value || 0, days: Math.round(+days.value || 0), max_clients: Math.round(+mc.value || 0) }); haptic("success"); toast("ذخیره شد"); }
          catch (e) { haptic("error"); toast(e.message); }
        }, "check")));
  }).catch(() => {});
  return [backHead("فروش پنل نمایندگی"),
    d.pending ? h("button", { class: "card alert", onclick: () => mSet("inbox") }, mic("inbox"), h("span", {}, `${num(d.pending)} سفارش پنل منتظر تأیید`), h("b", {}, "دیدن")) : null,
    !d.has_card ? h("div", { class: "card small bad" }, "اول کارت پرداخت را در «کارت‌ها» تعریف کنید.") : null,
    btn("🎉 بنر و تخفیف فروش پنل", "block", promoSheet),
    h("div", { class: "card" }, onRow, h("p", { class: "muted small" }, "تنظیمات دقیق هر بسته (سقف‌ها، نامحدود، اینباندها) از پنل وب است؛ اینجا نام، حجم و قیمت را عوض کنید.")),
    box,
    rows.length < 20 ? btn("افزودن بسته", "block ghost", () => { rows.push({ id: "", name: "", gb: 100, price: 0 }); draw(); }, "plus") : null,
    btn("ذخیره", "primary block big", async () => {
      try { await api("POST", "m/resale/sale", { enabled: onBox.checked, packages: rows }); haptic("success"); toast("ذخیره شد"); m2Reload(); } catch (e) { haptic("error"); toast(e.message); }
    }, "check"), capsBox, trialBox];
}

// ---------- bot settings ----------
function settingsView() {
  const d = M2.settings;
  if (!d) return [backHead("تنظیمات ربات"), spin()];
  const ta = (v, ph) => h("textarea", { rows: "4", maxlength: "1000", placeholder: ph }, v || "");
  const welcome = ta(d.welcome, "خالی = متن پیش‌فرض ربات");
  const shopText = ta(d.shop_text, "مثلاً: همهٔ پلن‌ها با پشتیبانی ۲۴ ساعته · تحویل فوری");
  const out = [backHead("تنظیمات ربات"),
    h("div", { class: "section-title" }, "متن‌ها"),
    h("div", { class: "card" }, field("خوش‌آمد (برای مشتری تازه)", welcome), field("متن بالای پلن‌ها (ربات و فروشگاه)", shopText))];
  const THEMES = [["", "پیش‌فرض", "#2de2e6"], ["teal", "فیروزه‌ای", "#22d3ee"], ["emerald", "زمردی", "#34d399"], ["rose", "صورتی", "#fb7185"], ["coral", "نارنجی", "#fb923c"]];
  M2.theme = M2.theme ?? d.theme ?? "";
  const themeRow = h("div", { class: "chips themes" }, THEMES.map(([k, label, c]) => h("button", { class: `chip ${M2.theme === k ? "on" : ""}`, style: `--sw:${c}`, onclick: (e) => { M2.theme = k; themeRow.querySelectorAll(".chip").forEach((c) => c.classList.remove("on")); e.currentTarget.classList.add("on"); } }, h("i", { class: "swatch" }), label)));
  out.push(h("div", { class: "section-title" }, "تم فروشگاه"), h("div", { class: "card" }, themeRow, h("p", { class: "muted small" }, "رنگ فروشگاه و دکمه‌ها برای مشتری‌های شما.")));
  let brand = null;
  if (d.brand) {
    const b = d.brand;
    brand = { title: h("input", { value: b.title || "", maxlength: "80", placeholder: "اسم فروشگاه" }),
      accent: h("input", { type: "color", value: /^#[0-9a-f]{6}$/i.test(b.accent || "") ? b.accent : "#2de2e6" }),
      announce: h("textarea", { rows: "2", maxlength: "1000", placeholder: "یک خبر کوتاه بالای فروشگاه (اختیاری)" }, b.announce || ""),
      telegram: h("input", { class: "ltr", value: b.telegram ? "@" + b.telegram : "", placeholder: "@support_id" }) };
    M2.logo = M2.logo ?? (b.logo || "");
    brand.logoBox = h("div", { class: "row logo-row" },
      M2.logo ? h("img", { class: "brand-logo big", src: M2.logo, alt: "" }) : h("span", { class: "muted small" }, "لوگویی نگذاشته‌اید"),
      btn(M2.logo ? "عوض کردن لوگو" : "گذاشتن لوگو", "", async () => {
        try { const data = await pickImage(256); if (data.length > 190000) { toast("عکس خیلی بزرگ است"); return; } M2.logo = data; const im = brand.logoBox.firstChild; im.replaceWith(h("img", { class: "brand-logo big", src: data, alt: "" })); toast("بعد از «ذخیرهٔ همه» اعمال می‌شود"); } catch {}
      }, "card"),
      M2.logo ? btn("حذف", "ghost", () => { M2.logo = ""; brand.logoBox.firstChild.replaceWith(h("span", { class: "muted small" }, "بعد از ذخیره حذف می‌شود")); }) : null);
    out.push(h("div", { class: "section-title" }, "ظاهر فروشگاه"),
      h("div", { class: "card" }, field("اسم فروشگاه", brand.title), brand.logoBox, field("رنگ اصلی (وقتی تم پیش‌فرض است)", brand.accent), field("خبر بالای فروشگاه", brand.announce), field("آیدی پشتیبانی", brand.telegram)));
  }
  let trial = null;
  if (d.trial) {
    const t = d.trial;
    const [row, on] = check("تست رایگان روشن باشد (هر کاربر یک بار)", t.enabled);
    const mb = h("input", { type: "number", inputmode: "numeric", value: t.mb || (t.gb ? t.gb * 1024 : 200) });
    const hours = h("input", { type: "number", inputmode: "numeric", value: t.hours || 24 });
    const ips = h("input", { type: "number", inputmode: "numeric", value: t.max_ips ?? 1 });
    trial = { on, mb, hours, ips };
    out.push(h("div", { class: "section-title" }, "تست رایگان"),
      h("div", { class: "card" }, row, h("div", { class: "two" }, field("حجم (مگابایت)", mb), field("مدت (ساعت)", hours)), field("کاربر هم‌زمان", ips),
        d.owner ? null : h("p", { class: "muted small" }, "حجم تست از اعتبار شما کم می‌شود.")));
  }
  const j = d.join || {};
  const [jrow, jon] = check("قبل از خرید و تست، عضویت در کانال لازم باشد", j.enabled);
  const chans = [0, 1, 2].map((i) => h("input", { class: "ltr", value: (j.channels || [])[i] || "", placeholder: i ? "کانال دوم یا سوم (اختیاری)" : "@mychannel" }));
  out.push(h("div", { class: "section-title" }, "عضویت اجباری"),
    h("div", { class: "card" }, jrow, chans, h("p", { class: "muted small" }, "ربات باید ادمین کانال باشد تا بتواند عضویت را بررسی کند.")));
  const save = btn("ذخیرهٔ همه", "primary block big", async () => {
    save.disabled = true;
    const body = { theme: M2.theme || "", welcome: welcome.value, shop_text: shopText.value, join: { enabled: jon.checked, channels: chans.map((c) => c.value.trim()).filter(Boolean) } };
    if (trial) {
      const mbv = Math.max(0, Math.round(+trial.mb.value || 0));
      body.trial = { enabled: trial.on.checked, mb: mbv % 1024 === 0 ? 0 : mbv, gb: mbv % 1024 === 0 ? mbv / 1024 : 0, hours: Math.round(+trial.hours.value || 0), max_ips: Math.round(+trial.ips.value || 0) };
    }
    if (brand) body.brand = { title: brand.title.value, accent: brand.accent.value, announce: brand.announce.value, telegram: brand.telegram.value, support_url: d.brand.support_url || "", logo: M2.logo || "" };
    try { await api("POST", "m/settings", body); M2.theme = undefined; M2.logo = undefined; haptic("success"); toast("ذخیره شد"); m2Reload(); }
    catch (e) { save.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  out.push(save);
  return out;
}

// ---------- discount codes ----------
function discountsView() {
  const list = M2.discounts;
  if (!list) return [backHead("کد تخفیف"), spin()];
  const now = Date.now() / 1000;
  const out = [backHead("کد تخفیف", btnIcon("plus", "کد تازه", () => discountSheet({ enabled: true, once_per_buyer: true })))];
  if (!list.length) out.push(h("div", { class: "card empty" }, h("p", {}, "هنوز کدی نساخته‌اید. مشتری کد را در ربات وارد می‌کند و روی خریدش اعمال می‌شود.")));
  out.push(h("div", { class: "ulist" }, list.map((d) => {
    const dead = !d.enabled || (d.expires_unix && d.expires_unix <= now) || (d.max_uses && d.used >= d.max_uses);
    return h("button", { class: `urow ${dead ? "off" : ""}`, onclick: () => discountSheet(d) },
      h("div", { class: "u-main" },
        h("div", { class: "u-top" }, h("b", { class: "ltr" }, d.code), h("span", { class: `pill ${dead ? "" : "good"}` }, d.percent ? `${num(d.percent)}٪` : `${toman(d.amount)} تومان`)),
        h("div", { class: "u-meta" }, h("span", {}, `${num(d.used)}${d.max_uses ? " از " + num(d.max_uses) : ""} استفاده`), h("span", {}, d.expires_unix ? `تا ${date(d.expires_unix)}` : "بی‌مهلت"))));
  })));
  return out;
}

function discountSheet(d) {
  const isNew = !d.code;
  const code = h("input", { class: "ltr", value: d.code || "", placeholder: "مثلاً YALDA30", autocapitalize: "characters", disabled: isNew ? null : "" });
  const pct = h("input", { type: "number", inputmode: "numeric", value: d.percent || "", placeholder: "درصد" });
  const amt = h("input", { type: "number", inputmode: "numeric", value: d.amount || "", placeholder: "یا مبلغ (تومان)" });
  const uses = h("input", { type: "number", inputmode: "numeric", value: d.max_uses || "", placeholder: "۰ = بی‌سقف" });
  const days = h("input", { type: "number", inputmode: "numeric", value: d.expires_unix ? Math.max(1, Math.ceil((d.expires_unix - Date.now() / 1000) / 86400)) : "", placeholder: "۰ = بی‌مهلت" });
  const [onceRow, once] = check("هر مشتری فقط یک بار", d.once_per_buyer);
  const [onRow, on] = check("فعال", d.enabled);
  const go = btn("ذخیره", "primary block big", async () => {
    go.disabled = true;
    const dd = Math.round(+days.value || 0);
    try {
      await api("POST", "m/discount", { code: code.value.trim().toUpperCase(), percent: Math.round(+pct.value || 0), amount: +pct.value ? 0 : Math.round(+amt.value || 0),
        max_uses: Math.round(+uses.value || 0), expires_unix: dd ? Math.floor(Date.now() / 1000) + dd * 86400 : 0, once_per_buyer: once.checked, enabled: on.checked, note: d.note || "" });
      close(); haptic("success"); toast("ذخیره شد"); m2Reload();
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, isNew ? "کد تخفیف تازه" : `کد ${d.code}`), field("کد", code),
    h("div", { class: "two" }, field("درصد تخفیف", pct), field("یا مبلغ تخفیف", amt)),
    h("div", { class: "two" }, field("سقف استفاده", uses), field("مهلت (روز)", days)), onceRow, onRow, go,
    isNew ? null : btn("حذف کد", "danger block ghost", async () => {
      try { await api("POST", "m/discount/delete", { code: d.code }); close(); toast("حذف شد"); m2Reload(); } catch (e) { toast(e.message); }
    }, "trash"));
}

// ---------- a message to every customer ----------
const audFa = { all: "همه", active: "سرویس فعال", expiring: "رو به اتمام", expired: "تمام‌شده", none: "بدون سرویس" };
function broadcastView() {
  const d = M2.bc;
  if (!d) return [backHead("پیام همگانی"), spin()];
  if (!d.on) return [backHead("پیام همگانی"), h("div", { class: "card" }, h("p", {}, "ربات شما روشن نیست. اول در «ربات من» روشنش کنید."))];
  M2.aud = M2.aud || "all";
  const txt = h("textarea", { rows: "6", maxlength: "3500", placeholder: "متن پیام…\nبرای دکمهٔ لینک، خط آخر: [متن دکمه](https://…)" }, M2.bcText || "");
  txt.addEventListener("input", () => { M2.bcText = txt.value; });
  const chip = (k) => h("button", { class: `chip ${M2.aud === k ? "on" : ""}`, onclick: () => { M2.aud = k; render(); } }, `${audFa[k]} (${num((d.audience || {})[k] || 0)})`);
  const send = async (aud) => {
    if (!txt.value.trim()) { toast("متن پیام را بنویسید"); return; }
    try {
      const r = await api("POST", "m/broadcast", { text: txt.value, audience: aud });
      haptic("success");
      if (aud === "test") toast("پیام آزمایشی در چت ربات برایتان رفت");
      else { M2.bcText = ""; celebrate(`ارسال برای ${num(r.count)} نفر شروع شد؛ گزارشش در ربات می‌آید`); render(); }
    } catch (e) { haptic("error"); toast(e.message); }
  };
  return [backHead("پیام همگانی"),
    h("div", { class: "card" }, txt),
    h("div", { class: "section-title" }, "گیرنده‌ها"),
    h("div", { class: "chips" }, ["all", "active", "expiring", "expired", "none"].map(chip)),
    h("div", { class: "row" }, btn("ارسال آزمایشی به خودم", "", () => send("test"), "send"),
      btn("ارسال", "primary", () => {
        const n = (d.audience || {})[M2.aud] || 0;
        if (!n) { toast("کسی در این گروه نیست"); return; }
        const close = sheet(h("h2", {}, `ارسال برای ${num(n)} نفر؟`), h("p", { class: "muted" }, "بعد از شروع، فقط از ربات می‌شود متوقفش کرد."),
          h("div", { class: "row" }, btn("بله، بفرست", "primary", () => { close(); send(M2.aud); }, "send"), btn("انصراف", "", () => close())));
      }, "check"))];
}

async function policySheet(x) {
  let p;
  try { p = await api("GET", `m/sellers/policy?id=${encodeURIComponent(x.id)}`); } catch (e) { toast(e.message); return; }
  const n = (v) => h("input", { type: "number", inputmode: "numeric", value: v || "", placeholder: "۰ = بی‌سقف" });
  const mc = n(p.max_clients), md = n(p.max_days), mg = n(p.max_client_gb);
  const [uRow, u] = check("فروش نامحدود مجاز است", p.allow_unlimited);
  const [dRow, dl] = check("حذف کاربر مجاز است", p.can_delete);
  const [cRow, cr] = check("ساخت کاربر تازه بسته باشد", p.deny_create);
  const [rRow, rn] = check("تمدید و افزودن حجم بسته باشد", p.deny_renew);
  const go = btn("ذخیره", "primary block big", async () => {
    go.disabled = true;
    try {
      await api("POST", "m/sellers/policy", { id: x.id, max_clients: Math.round(+mc.value || 0), max_days: Math.round(+md.value || 0), max_client_gb: Math.round(+mg.value || 0),
        allow_unlimited: u.checked, can_delete: dl.checked, deny_create: cr.checked, deny_renew: rn.checked });
      close(); haptic("success"); toast("ذخیره شد");
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, `محدودیت‌های ${x.name || x.id}`), field("حداکثر تعداد کاربر", mc),
    h("div", { class: "two" }, field("حداکثر روز هر کاربر", md), field("حداکثر گیگ هر کاربر", mg)), uRow, dRow, cRow, rRow,
    M.home.owner ? null : h("p", { class: "muted small" }, "از سقف‌های خودتان بیشتر نمی‌شود."), go);
}

function sellerMessageSheet(x) {
  const t = h("textarea", { rows: "5", maxlength: "3000", placeholder: "متن پیام…" });
  const go = btn("ارسال", "primary block big", async () => {
    go.disabled = true;
    try { await api("POST", "m/sellers/message", { id: x.id, text: t.value }); close(); haptic("success"); toast("فرستاده شد"); }
    catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "send");
  const close = sheet(h("h2", {}, `پیام به ${x.name || x.id}`), t, go);
}

// ---------- customers ----------
function customersView() {
  const d = M2.cust;
  const q = h("input", { class: "search", type: "search", placeholder: "اسم، آیدی یا شمارهٔ تلگرام…", value: M2.cq || "", enterkeyhint: "search" });
  let tmr;
  q.addEventListener("input", () => { clearTimeout(tmr); tmr = setTimeout(() => { M2.cq = q.value; m2Reload(); }, 350); });
  const out = [backHead("مشتری‌ها"), h("div", { class: "searchbar" }, q)];
  if (!d) { out.push(spin()); return out; }
  out.push(h("div", { class: "small muted listhead" }, `${num(d.total)} نفر ربات را استارت کرده‌اند${d.total > d.customers.length ? ` (${num(d.customers.length)} تای آخر)` : ""}`));
  if (!d.customers.length) out.push(h("div", { class: "card empty" }, h("p", {}, "کسی پیدا نشد.")));
  out.push(h("div", { class: "ulist" }, d.customers.map((c) => h("button", { class: `urow ${c.banned ? "bad" : ""} ${c.left ? "off" : ""}`, onclick: () => customerSheet(c) },
    h("div", { class: "u-main" },
      h("div", { class: "u-top" }, h("b", {}, c.name || `#${c.chat}`), c.username ? h("span", { class: "ltr muted small" }, "@" + c.username) : null,
        c.banned ? h("span", { class: "pill bad" }, "مسدود") : c.left ? h("span", { class: "pill" }, "ربات را بسته") : null),
      h("div", { class: "u-meta" }, h("span", {}, `${num(c.services)} سرویس${c.wallet ? ` · کیف پول ${toman(c.wallet)}` : ""}`), h("span", {}, date(c.last))))))));
  return out;
}

function customerSheet(c) {
  const msg = h("textarea", { rows: "3", maxlength: "3000", placeholder: "پیام مستقیم از ربات…" });
  const amt = h("input", { type: "number", inputmode: "numeric", placeholder: "مبلغ (تومان)" });
  const act = async (body, done) => {
    try { const r = await api("POST", "m/customer", { chat: c.chat, ...body }); haptic("success"); toast(done(r)); close(); m2Reload(); }
    catch (e) { haptic("error"); toast(e.message); }
  };
  const close = sheet(h("div", { class: "row between" }, h("h2", {}, c.name || `#${c.chat}`), c.username ? h("span", { class: "ltr muted" }, "@" + c.username) : null),
    h("div", { class: "kv" }, h("span", {}, "شمارهٔ تلگرام"), h("b", { class: "ltr" }, String(c.chat))),
    h("div", { class: "kv" }, h("span", {}, "سرویس‌ها"), h("b", {}, num(c.services))),
    h("div", { class: "kv" }, h("span", {}, "کیف پول"), h("b", {}, `${toman(c.wallet)} تومان`)),
    h("div", { class: "kv" }, h("span", {}, "اولین / آخرین بار"), h("b", {}, `${date(c.first)} / ${date(c.last)}`)),
    c.username ? btn("باز کردن چت", "block ghost", () => { try { tg.openTelegramLink(`https://t.me/${c.username}`); } catch {} }, "chat") : null,
    h("div", { class: "section-title" }, "پیام از ربات"), msg,
    btn("ارسال پیام", "primary block", () => act({ action: "message", text: msg.value }, () => "فرستاده شد"), "send"),
    h("div", { class: "section-title" }, "کیف پول"), amt,
    h("div", { class: "row" }, btn("افزودن", "", () => act({ action: "wallet", delta: Math.round(+amt.value || 0), text: "از مینی‌اپ" }, (r) => `موجودی: ${toman(r.balance)} تومان`), "plus"),
      btn("کم کردن", "", () => act({ action: "wallet", delta: -Math.round(+amt.value || 0), text: "از مینی‌اپ" }, (r) => `موجودی: ${toman(r.balance)} تومان`), "renew")),
    btn(c.banned ? "رفع مسدودی" : "مسدود کردن", c.banned ? "block" : "danger block", () => act({ action: c.banned ? "unban" : "ban" }, () => (c.banned ? "رفع مسدودی شد" : "مسدود شد")), "power"));
}

// ---------- the bot's admins ----------
const roleDesc = { manager: "مدیر: همهٔ کارها", sales: "فروش: ساخت، تمدید، سفارش‌ها، تیکت", support: "پشتیبانی: دیدن کاربرها و تیکت" };
function adminsView() {
  const d = M2.admins;
  if (!d) return [backHead("ادمین‌های ربات"), spin()];
  const invite = async (role) => {
    try {
      const r = await api("POST", "m/admins/invite", { role });
      const share = () => { try { tg.openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(r.link)}`); } catch { copy(r.link); } };
      sheet(h("h2", {}, `دعوت ${roleFa[role]}`), h("p", { class: "muted small" }, "یک‌بار مصرف، تا یک ساعت. برای آن شخص بفرستید؛ با باز کردنش ادمین ربات شما می‌شود."),
        h("div", { class: "sublink ltr", onclick: () => copy(r.link) }, r.link), h("div", { class: "row" }, btn("کپی", "primary", () => copy(r.link), "copy"), btn("ارسال", "", share, "share")));
    } catch (e) { toast(e.message); }
  };
  const out = [backHead("ادمین‌های ربات"),
    h("div", { class: "card" }, h("b", {}, "دعوت ادمین تازه"), h("div", { class: "cfgs" }, ["manager", "sales", "support"].map((r) => h("button", { class: "cfg", onclick: () => invite(r) },
      h("span", { class: "pill" }, roleFa[r]), h("span", {}, roleDesc[r]), mic("plus")))))];
  if (!d.admins.length) out.push(h("div", { class: "card empty" }, h("p", {}, "هنوز ادمینی ندارید.")));
  out.push(h("div", { class: "ulist" }, d.admins.map((a) => {
    const sel = h("select", { "aria-label": "نقش" }, ["manager", "sales", "support"].map((r) => { const o = h("option", { value: r }, roleFa[r]); if (r === a.role) o.selected = true; return o; }));
    sel.addEventListener("change", async () => { try { await api("POST", "m/admins/set", { chat: a.chat, role: sel.value }); toast("نقش عوض شد"); } catch (e) { toast(e.message); } });
    return h("div", { class: "lrow" }, h("div", {}, h("b", {}, a.name || `#${a.chat}`), a.username ? h("div", { class: "ltr muted small" }, "@" + a.username) : null,
        a.perms && a.perms.length ? h("div", { class: "small muted" }, `دسترسی سفارشی: ${num(a.perms.length)} مورد`) : null),
      h("div", { class: "row" }, sel, h("button", { class: "iconbtn mini", "aria-label": "دسترسی‌ها", onclick: () => permsSheet(a, d.all_perms || []) }, mic("gauge")), h("button", { class: "iconbtn mini danger", "aria-label": "حذف", onclick: async () => {
        try { await api("POST", "m/admins/set", { chat: a.chat, role: "" }); toast("حذف شد"); m2Reload(); } catch (e) { toast(e.message); }
      } }, mic("trash"))));
  })));
  return out;
}

// ---------- sale settings ----------
function saleView() {
  const d = M2.sale;
  if (!d) return [backHead("تنظیمات فروش"), spin()];
  const out = [backHead("تنظیمات فروش")];
  let dom = null;
  if (d.domains) {
    dom = h("select", {}, h("option", { value: "" }, `پیش‌فرض (${(d.default_domain || "").replace(/^https?:\/\//, "")})`),
      d.domains.map((x) => { const o = h("option", { value: x.url }, `${x.label || x.url.replace(/^https?:\/\//, "")}`); if (x.url === d.domain) o.selected = true; return o; }));
    out.push(h("div", { class: "section-title" }, "آدرس لینک ساب مشتری‌ها"),
      h("div", { class: "card" }, dom, h("p", { class: "muted small" }, "با عوض کردن، لینک همهٔ کاربرهای شما همان لحظه با آدرس تازه ساخته می‌شود؛ لینک‌های قبلی هم کار می‌کنند.")));
  }
  const ac = d.autoclean || {};
  const [acRow, acOn] = check("کانفیگ‌های تمام‌شده و بی‌مصرف خودکار پاک شوند", ac.on);
  const acDays = h("input", { type: "number", inputmode: "numeric", value: ac.days || 30 });
  out.push(h("div", { class: "section-title" }, "پاک‌سازی خودکار"),
    h("div", { class: "card" }, acRow, field("بعد از چند روز بی‌مصرفی (۷ تا ۳۶۵)", acDays),
      h("p", { class: "muted small" }, `الان ${num(d.dead || 0)} کانفیگ این شرط را دارد. کانفیگ فعال هیچ‌وقت پاک نمی‌شود.`)));
  const quick = h("textarea", { rows: "6", placeholder: "هر خط یک پاسخ آماده\nمثلاً: لطفاً اشتراک را در برنامه آپدیت کنید." }, (d.quick || []).join("\n"));
  out.push(h("div", { class: "section-title" }, "پاسخ‌های آماده تیکت"), h("div", { class: "card" }, quick));
  const save = btn("ذخیره", "primary block big", async () => {
    save.disabled = true;
    const body = { autoclean: { on: acOn.checked, days: Math.round(+acDays.value || 30) }, quick: quick.value.split("\n").map((x) => x.trim()).filter(Boolean) };
    if (dom) body.domain = dom.value;
    try { await api("POST", "m/sale", body); M2.quick = null; haptic("success"); toast("ذخیره شد"); m2Reload(); }
    catch (e) { save.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  out.push(save);
  return out;
}

const permFa = { view: "دیدن کاربرها", link: "لینک و QR", new: "ساخت کاربر", renew: "تمدید و افزودن حجم", edit: "روشن/خاموش و ویرایش", delete: "حذف کاربر",
  orders: "تأیید پرداخت‌ها", credit: "اعتبار و شارژ", tickets: "تیکت‌ها", plans: "پلن‌ها و قیمت‌ها", cards: "کارت‌های پرداخت", settings: "تنظیمات ربات، مشتری‌ها، پیام همگانی",
  reports: "گزارش فروش", sellers: "زیرمجموعه‌ها و فروش پنل" };
function permsSheet(a, all) {
  const boxes = all.map((p) => { const [row, box] = check(permFa[p] || p, (a.perms || []).includes(p)); box.dataset.p = p; return { row, box }; });
  const go = btn("ذخیره", "primary block big", async () => {
    try {
      await api("POST", "m/admins/perms", { chat: a.chat, perms: boxes.filter((x) => x.box.checked).map((x) => x.box.dataset.p) });
      close(); haptic("success"); toast("ذخیره شد"); m2Reload();
    } catch (e) { toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, `دسترسی‌های ${a.name || a.chat}`),
    h("p", { class: "muted small" }, "هر چه تیک بزنید، همان را دارد (در ربات و مینی‌اپ). هیچ‌کدام = همان دسترسی‌های نقشش."),
    h("div", { class: "card" }, boxes.map((x) => x.row)), go);
}

const M2V = { wheel: wheelView, sale: saleView, customers: customersView, admins: adminsView, settings: settingsView, discounts: discountsView, broadcast: broadcastView, more: moreView, resale: resaleView, wallet: walletView, sellers: sellersView, bot: botView, cards: cardsView, plans: plansView, tickets: ticketsView, report: reportView };

// ---------- the panel-sale banner and discount ----------
async function promoSheet() {
  let d;
  try { d = await api("GET", "m/panelpromo"); } catch (e) { toast(e.message); return; }
  const [onRow, onBox] = check("بنر روی صفحهٔ «نمایندگی» نشان داده شود", d.on);
  const title = h("input", { value: d.title || "", maxlength: "80", placeholder: "مثلاً: جشنوارهٔ پاییزه 🍂" });
  const text = h("textarea", { rows: "3", maxlength: "300", placeholder: "مثلاً: فقط تا آخر هفته، همهٔ پنل‌ها با تخفیف ویژه" }, d.text || "");
  const badge = h("input", { value: d.badge || "", maxlength: "24", placeholder: "مثلاً: فقط امروز" });
  const pct = h("input", { type: "number", inputmode: "numeric", min: "0", max: "90", value: d.percent || "", placeholder: "۰ = بدون تخفیف" });
  const days = h("input", { type: "number", inputmode: "numeric", min: "0", value: d.until ? Math.max(0, Math.ceil((d.until - Date.now() / 1000) / 3600)) : "", placeholder: "خالی = بدون پایان" });
  let color = d.color || "violet", image = d.image || "";
  const COLORS = [["violet", "بنفش", "#8b5cf6,#ec4899"], ["gold", "طلایی", "#f59e0b,#ef4444"], ["green", "سبز", "#10b981,#0ea5e9"], ["rose", "صورتی", "#f43f5e,#a855f7"], ["blue", "آبی", "#3b82f6,#06b6d4"]];
  const sw = h("div", { class: "chips themes" });
  const preview = h("div", {});
  const drawPreview = () => {
    const pr = { title: title.value, text: text.value, badge: badge.value, color, image, percent: Math.round(+pct.value || 0), until: +days.value > 0 ? Math.floor(Date.now() / 1000) + Math.round(+days.value) * 3600 : 0 };
    preview.replaceChildren(promoBanner(pr));
  };
  const drawSw = () => sw.replaceChildren(...COLORS.map(([k, n, g]) => h("button", { class: `chip ${k === color ? "on" : ""}`, onclick: () => { color = k; drawSw(); drawPreview(); } },
    h("i", { class: "swatch", style: `background:linear-gradient(135deg,${g})` }), n)));
  drawSw();
  const imgRow = h("div", { class: "row" });
  const drawImg = () => imgRow.replaceChildren(
    btn(image ? "عوض کردن عکس" : "گذاشتن عکس بنر", "", async () => {
      try { const data = await pickImage(800); if (data.length > 195000) { toast("عکس خیلی بزرگ است؛ عکس کوچک‌تری بگذارید"); return; } image = data; drawImg(); drawPreview(); } catch {}
    }, "gift"),
    image ? btn("حذف عکس", "ghost", () => { image = ""; drawImg(); drawPreview(); }) : null);
  drawImg();
  for (const el of [title, text, badge, pct, days]) el.addEventListener("input", drawPreview);
  drawPreview();
  const go = btn("ذخیره", "primary block big", async () => {
    go.disabled = true;
    const hours = Math.round(+days.value || 0);
    try {
      await api("POST", "m/panelpromo", { on: onBox.checked, title: title.value, text: text.value, badge: badge.value, color, image,
        percent: Math.round(+pct.value || 0), until: hours > 0 ? Math.floor(Date.now() / 1000) + hours * 3600 : 0 });
      haptic("success"); toast("ذخیره شد"); close(); panelSale = undefined; loadPanels();
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, "بنر و تخفیف فروش پنل"),
    h("p", { class: "muted small" }, "این بنر بالای صفحهٔ «نمایندگی» و در بخش خرید مینی‌اپ دیده می‌شود. اگر درصد تخفیف بگذارید، واقعاً روی قیمت همهٔ بسته‌ها اعمال می‌شود (در ربات هم)."),
    h("div", { class: "section-title sub" }, "پیش‌نمایش"), preview,
    onRow, field("عنوان", title), field("متن", text), h("div", { class: "two" }, field("برچسب کوچک", badge), field("درصد تخفیف", pct)),
    field("مدت (ساعت)", days, "مثلاً ۴۸ = دو روز؛ بعدش بنر و تخفیف خودکار برداشته می‌شود"),
    h("div", { class: "section-title sub" }, "رنگ"), sw, h("div", { class: "section-title sub" }, "عکس (اختیاری)"), imgRow, go);
}

// ---------- the wheel of fortune ----------
function wheelView() {
  const d = M2.wheel;
  if (!d) return [backHead("گردونهٔ شانس"), spin()];
  const cfg = d.config, t = d.today || {};
  const rows = cfg.slices.map((x) => ({ ...x }));
  const [onRow, onBox] = check("گردونه روشن باشد (در مینی‌اپ و منوی ربات)", cfg.on);
  const [needRow, needBox] = check("فقط کسانی که سرویس دارند بچرخانند", cfg.need_service);
  const cap = h("input", { type: "number", inputmode: "numeric", min: "0", value: cfg.daily_max || "", placeholder: "خالی = بی‌سقف" });
  const KINDS = [["gb", "حجم (گیگ)"], ["days", "روز اضافه"], ["wallet", "کیف پول (تومان)"], ["discount", "کد تخفیف (٪)"], ["none", "پوچ"]];
  const box = h("div", { class: "ulist" });
  const total = () => rows.reduce((a, x) => a + (+x.weight || 0), 0) || 1;
  const draw = () => box.replaceChildren(...rows.map((x, i) => {
    const kind = h("select", { onchange: (e) => { x.kind = e.target.value; if (x.kind === "none") x.value = 0; draw(); } }, KINDS.map(([k, n]) => { const o = h("option", { value: k }, n); o.selected = k === x.kind; return o; }));
    const val = h("input", { type: "number", inputmode: "numeric", value: x.kind === "none" ? "" : x.value, disabled: x.kind === "none", oninput: (e) => { x.value = Math.round(+e.target.value || 0); } });
    const wt = h("input", { type: "number", inputmode: "numeric", value: x.weight, oninput: (e) => { x.weight = Math.round(+e.target.value || 0); pct.textContent = `شانس ${num(Math.round(((+x.weight || 0) / total()) * 100))}٪`; } });
    const pct = h("small", { class: "muted" }, `شانس ${num(Math.round(((+x.weight || 0) / total()) * 100))}٪`);
    return h("div", { class: "card ccard" },
      h("div", { class: "row between" }, h("b", {}, h("i", { class: "swatch", style: `background:${WHEEL_C[i % WHEEL_C.length]}` }), ` جایزهٔ ${num(i + 1)}`), pct,
        rows.length > 2 ? h("button", { class: "linkbtn bad", onclick: () => { rows.splice(i, 1); draw(); } }, "حذف") : null),
      field("نوع", kind), h("div", { class: "two" }, field("مقدار", val), field("وزن شانس", wt)));
  }));
  draw();
  return [backHead("گردونهٔ شانس"),
    h("div", { class: "kpis four" },
      h("div", { class: "kpi" }, h("b", {}, num(t.spins || 0)), h("span", {}, "چرخش امروز")),
      h("div", { class: "kpi" }, h("b", {}, `${num(t.gb || 0)} گیگ`), h("span", {}, "حجم داده‌شده")),
      h("div", { class: "kpi" }, h("b", {}, toman(t.wallet || 0)), h("span", {}, "کیف پول (تومان)")),
      h("div", { class: "kpi" }, h("b", {}, num(t.discount || 0)), h("span", {}, "کد تخفیف"))),
    h("div", { class: "card" }, onRow, needRow, field("سقف چرخش در روز (برای کل ربات)", cap),
      h("p", { class: "muted small" }, "هر مشتری روزی یک بار (به وقت تهران) می‌چرخاند. حجم و روز به سرویس همان ربات اضافه می‌شود" + (M.home.owner ? "." : " و از اعتبار پنل شما کم می‌شود."))),
    h("div", { class: "section-title" }, "جایزه‌ها"), box,
    rows.length < 12 ? btn("افزودن جایزه", "block ghost", () => { rows.push({ kind: "gb", value: 1, weight: 10 }); draw(); }, "plus") : null,
    btn("ذخیره", "primary block big", async () => {
      try {
        await api("POST", "m/wheel", { on: onBox.checked, need_service: needBox.checked, daily_max: Math.round(+cap.value || 0), slices: rows.map((x) => ({ kind: x.kind, value: x.kind === "none" ? 0 : Math.round(+x.value || 0), weight: Math.round(+x.weight || 0) })) });
        haptic("success"); toast("ذخیره شد"); m2Reload(); wheel = undefined; loadWheel();
      } catch (e) { haptic("error"); toast(e.message); }
    }, "check")];
}
