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
    case "plans": M2.plans = await api("GET", "m/plans"); break;
    case "tickets": M2.tickets = await api("GET", `m/tickets?s=${M2.tStatus}`); break;
    case "report": M2.report = await api("GET", `m/report?days=${M2.days}`); break;
    case "resale": M2.resale = await api("GET", "m/resale"); break;
    case "settings": M2.settings = await api("GET", "m/settings"); break;
    case "discounts": M2.discounts = await api("GET", "m/discounts"); break;
    case "broadcast": M2.bc = await api("GET", "m/broadcast"); break;
  }
}
const m2Reload = () => m2Fetch(M.view).then(render).catch((e) => toast(e.message));

// ---------- the "more" grid ----------
function moreView() {
  const d = M.home, c = d.can || {};
  const tile = (v, label, ic, sub, badge) => h("button", { class: "tile", onclick: () => mSet(v) }, mic(ic),
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
  ].filter(Boolean);
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
  out.push(h("div", { class: "ulist" }, d.plans.map((p) => h("button", { class: `urow ${p.status === "published" ? "" : "off"}`, onclick: () => d.edit && planSheet(p) },
    h("div", { class: "u-main" },
      h("div", { class: "u-top" }, h("b", {}, p.name), h("span", { class: `pill ${p.status === "published" ? "good" : ""}` }, p.status === "published" ? "در فروشگاه" : "پیش‌نویس")),
      h("div", { class: "u-meta" }, h("span", {}, `${p.plan === "unlimited" ? "نامحدود" : num(p.gb) + " گیگ"} · ${num(p.days)} روز${p.max_ips ? ` · ${num(p.max_ips)} کاربر` : ""}`),
        h("b", {}, p.price ? `${toman(p.price)} تومان` : "بدون قیمت")))))));
  return out;
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
  const send = async (closeIt) => {
    if (!txt.value.trim() && !closeIt) { toast("متن جواب را بنویسید"); return; }
    try {
      await api("POST", "m/ticket", { id, text: txt.value, close: closeIt });
      close(); haptic("success"); toast(closeIt ? "تیکت بسته شد" : "فرستاده شد"); m2Reload(); mLoad();
    } catch (e) { haptic("error"); toast(e.message); }
  };
  const close = sheet(h("div", { class: "row between" }, h("h2", {}, t.name || `#${t.chat}`), h("span", { class: "muted small" }, `#${num(t.id)}`)),
    t.clients && t.clients.length ? h("div", { class: "muted small ltr" }, t.clients.join(" · ")) : null,
    h("div", { class: "thread" }, (t.msgs || []).map((m) => h("div", { class: `bubble ${m.staff ? "me" : ""}` },
      h("div", {}, m.text), h("small", {}, `${m.staff ? m.author || "پشتیبانی" : ""}${m.staff ? " · " : ""}${date(m.unix)}`)))),
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
  return [backHead("فروش پنل نمایندگی"),
    d.pending ? h("button", { class: "card alert", onclick: () => mSet("inbox") }, mic("inbox"), h("span", {}, `${num(d.pending)} سفارش پنل منتظر تأیید`), h("b", {}, "دیدن")) : null,
    !d.has_card ? h("div", { class: "card small bad" }, "اول کارت پرداخت را در «کارت‌ها» تعریف کنید.") : null,
    h("div", { class: "card" }, onRow, h("p", { class: "muted small" }, "تنظیمات دقیق هر بسته (سقف‌ها، نامحدود، اینباندها) از پنل وب است؛ اینجا نام، حجم و قیمت را عوض کنید.")),
    box,
    rows.length < 20 ? btn("افزودن بسته", "block ghost", () => { rows.push({ id: "", name: "", gb: 100, price: 0 }); draw(); }, "plus") : null,
    btn("ذخیره", "primary block big", async () => {
      try { await api("POST", "m/resale/sale", { enabled: onBox.checked, packages: rows }); haptic("success"); toast("ذخیره شد"); m2Reload(); } catch (e) { haptic("error"); toast(e.message); }
    }, "check")];
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
  let brand = null;
  if (d.brand) {
    const b = d.brand;
    brand = { title: h("input", { value: b.title || "", maxlength: "80", placeholder: "اسم فروشگاه" }),
      accent: h("input", { type: "color", value: /^#[0-9a-f]{6}$/i.test(b.accent || "") ? b.accent : "#2de2e6" }),
      announce: h("textarea", { rows: "2", maxlength: "1000", placeholder: "یک خبر کوتاه بالای فروشگاه (اختیاری)" }, b.announce || ""),
      telegram: h("input", { class: "ltr", value: b.telegram ? "@" + b.telegram : "", placeholder: "@support_id" }) };
    out.push(h("div", { class: "section-title" }, "ظاهر فروشگاه"),
      h("div", { class: "card" }, field("اسم فروشگاه", brand.title), field("رنگ اصلی", brand.accent), field("خبر بالای فروشگاه", brand.announce), field("آیدی پشتیبانی", brand.telegram)));
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
    const body = { welcome: welcome.value, shop_text: shopText.value, join: { enabled: jon.checked, channels: chans.map((c) => c.value.trim()).filter(Boolean) } };
    if (trial) {
      const mbv = Math.max(0, Math.round(+trial.mb.value || 0));
      body.trial = { enabled: trial.on.checked, mb: mbv % 1024 === 0 ? 0 : mbv, gb: mbv % 1024 === 0 ? mbv / 1024 : 0, hours: Math.round(+trial.hours.value || 0), max_ips: Math.round(+trial.ips.value || 0) };
    }
    if (brand) body.brand = { title: brand.title.value, accent: brand.accent.value, announce: brand.announce.value, telegram: brand.telegram.value, support_url: d.brand.support_url || "" };
    try { await api("POST", "m/settings", body); haptic("success"); toast("ذخیره شد"); m2Reload(); }
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

const M2V = { settings: settingsView, discounts: discountsView, broadcast: broadcastView, more: moreView, resale: resaleView, wallet: walletView, sellers: sellersView, bot: botView, cards: cardsView, plans: plansView, tickets: ticketsView, report: reportView };
