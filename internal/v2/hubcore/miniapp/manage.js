"use strict";
// Management inside the Mini App (resellers, their bot admins, the owner):
// dashboard, clients (create in one tap, renew, on/off, delete, link/QR,
// daily cap, bulk), orders and top-ups with receipts. Loaded only for
// managers; uses the helpers of app.js (h, icon, btn, sheet, api, …).

const M = { home: null, view: "dash", filter: "", q: "", list: [], count: 0, pick: null, inbox: [] };
const MIC = {
  home: "M3 11l9-8 9 8M5 10v10h14V10",
  users: "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75",
  inbox: "M22 12h-6l-2 3h-4l-2-3H2M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z",
  plus: "M12 5v14M5 12h14",
  power: "M18.36 6.64a9 9 0 1 1-12.73 0M12 2v10",
  trash: "M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6",
  share: "M4 12v8h16v-8M16 6l-4-4-4 4M12 2v13",
  panel: "M3 3h18v18H3zM3 9h18M9 21V9",
  check: "M20 6 9 17l-5-5",
  x: "M18 6 6 18M6 6l12 12",
  gauge: "M12 14l4-4M3.34 19a10 10 0 1 1 17.32 0",
  note: "M4 4h16v16H4zM8 9h8M8 13h6",
  grid: "M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z",
};
Object.assign(IC, MIC); // btn() takes icon names from IC
const mic = (k) => icon(IC[k]);

const daysLeft = (u) => Math.max(0, Math.ceil((u - Date.now() / 1000) / 86400));
const gbOf = (b) => (b < 0 ? "∞" : num(Math.round((b / 2 ** 30) * 10) / 10));
const stLabel = { active: ["good", "فعال"], soon: ["warn", "رو به اتمام"], ended: ["bad", "تمام شده"], off: ["", "خاموش"], waiting: ["", "منتظر اتصال"] };
const roleFa = { owner: "مالک", manager: "مدیر", sales: "فروش", support: "پشتیبانی" };
const can = (k) => !!(M.home && M.home.can && M.home.can[k]);

async function mLoad(view) {
  try {
    M.home = await api("GET", "m/home");
    if (view) M.view = view;
    await mRefresh();
  } catch (e) { toast(e.message); }
}

async function mRefresh() {
  if (M.view === "users") {
    const r = await api("GET", `m/clients?f=${encodeURIComponent(M.filter)}&q=${encodeURIComponent(M.q)}`);
    M.list = r.clients || []; M.count = r.count || 0;
  } else if (M.view === "inbox" && can("orders")) {
    M.inbox = await api("GET", "m/inbox");
  } else if (typeof m2Fetch === "function") {
    await m2Fetch(M.view);
  }
  render();
}

function mSet(view, filter) {
  M.view = view;
  if (filter !== undefined) M.filter = filter;
  M.pick = null;
  try { tg.HapticFeedback.selectionChanged(); } catch {}
  mRefresh().catch((e) => toast(e.message));
  window.scrollTo({ top: 0 });
}

// Manage is the tab's content (app.js calls it from render()).
function manageView() {
  if (!M.home) { mLoad(); return [h("div", { class: "boot" }, h("span", { class: "spinner" }))]; }
  const p = M.home.pending || {};
  const inboxN = (p.orders || 0) + (p.topups || 0);
  const more = typeof M2V === "object";
  const seg = h("div", { class: "seg", role: "tablist" },
    segBtn("dash", "داشبورد", "home"),
    segBtn("users", "کاربرها", "users"),
    can("orders") ? segBtn("inbox", inboxN ? `صندوق (${num(inboxN)})` : "صندوق", "inbox") : null,
    more ? segBtn("more", M.home.tickets ? `بیشتر (${num(M.home.tickets)})` : "بیشتر", "grid", !["dash", "users", "inbox"].includes(M.view)) : null);
  const body = M.view === "users" ? usersView() : M.view === "inbox" ? inboxView() : more && M2V[M.view] ? M2V[M.view]() : dashView();
  return [seg, ...body];
}
function segBtn(v, label, ic, on) {
  on = on || M.view === v;
  return h("button", { class: on ? "on" : "", role: "tab", "aria-selected": on ? "true" : "false", onclick: () => mSet(v) }, mic(ic), h("span", {}, label));
}

// ---------- dashboard ----------
function dashView() {
  const d = M.home, st = d.stats || {}, out = [];
  const f = d.forecast;
  const credit = d.owner ? null : h("div", { class: "card mcredit" },
    h("div", { class: "row between" }, h("div", {}, h("div", { class: "muted small" }, "اعتبار حجمی"), h("div", { class: "big" }, size(d.credit || 0))),
      d.units ? h("div", { class: "end" }, h("div", { class: "muted small" }, "واحد نامحدود"), h("div", { class: "big" }, num(d.units))) : null),
    f && f.days_left >= 0 ? h("div", { class: `fc ${f.days_left <= 3 ? "bad" : ""}` }, mic("gauge"),
      h("span", {}, `با سرعت فروش این هفته (${size(f.daily_bytes)} در روز) حدود `, h("b", {}, f.days_left < 1 ? "کمتر از یک روز" : `${num(Math.floor(f.days_left))} روز`), " کافی است.")) : null,
    f && f.package ? h("div", { class: "muted small" }, `پیشنهاد شارژ برای یک ماه: ${f.package.name} (${num(f.package.gb)} گیگ)`) : null);
  out.push(h("div", { class: "card mhead" },
    h("div", {}, h("h2", {}, d.name || ""), h("span", { class: "pill" }, roleFa[d.role] || d.role)),
    h("div", { class: "muted small" }, `فروش امروز: ${size((d.today || {}).sold || 0)} · ${num((d.today || {}).sales || 0)} مورد`)));
  if (credit) out.push(credit);
  const kpi = (label, n, filter, tone) => h("button", { class: `kpi ${tone || ""}`, onclick: () => mSet("users", filter) }, h("b", {}, num(n || 0)), h("span", {}, label));
  out.push(h("div", { class: "kpis" }, kpi("همه", st.total, ""), kpi("فعال", st.active, ""), kpi("آنلاین", st.online, "online", "good"),
    kpi("رو به اتمام", st.soon, "soon", "warn"), kpi("تمام‌شده", st.ended, "ended", "bad")));
  const pend = d.pending || {};
  if (can("orders") && (pend.orders || pend.topups)) {
    out.push(h("button", { class: "card alert", onclick: () => mSet("inbox") }, mic("inbox"),
      h("span", {}, `${num((pend.orders || 0) + (pend.topups || 0))} پرداخت منتظر تأیید شماست`), h("b", {}, "دیدن")));
  }
  if (d.tickets && typeof M2V === "object") {
    out.push(h("button", { class: "card alert", onclick: () => mSet("tickets") }, mic("chat"),
      h("span", {}, `${num(d.tickets)} تیکت مشتری منتظر جواب است`), h("b", {}, "دیدن")));
  }
  if (can("new")) {
    out.push(btn("ساخت کاربر", "primary block big", () => createSheet(), "plus"));
    const plans = (d.plans || []).slice(0, 6);
    if (plans.length) {
      out.push(h("div", { class: "section-title" }, "ساخت سریع با یک لمس"),
        h("div", { class: "qplans" }, plans.map((pl) => h("button", { class: "qplan", onclick: () => quickCreate(pl) },
          h("b", {}, pl.name), h("span", {}, `${pl.plan === "unlimited" ? "نامحدود" : num(pl.gb) + " گیگ"} · ${num(pl.days)} روز`)))));
    }
  }
  if (d.role === "owner" || d.role === "manager") {
    out.push(h("div", { class: "card manual" },
      h("div", {}, h("b", {}, "ورود دستی به پنل کامل"), h("p", { class: "muted small" }, "برای کارهایی که اینجا نیست (تنظیمات کامل، گزارش‌ها)، بدون رمز وارد پنل وب شوید.")),
      btn("پنل کامل", "", fullPanel, "panel")));
  }
  return out;
}

async function fullPanel() {
  try {
    const res = await fetch("/tgapp/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ init_data: initData }) });
    if (!res.ok) throw new Error((await res.text()).trim() || "ورود ممکن نشد");
    const j = await res.json();
    location.href = j.go || "/";
  } catch (e) { toast(e.message); }
}

// ---------- clients ----------
function usersView() {
  const out = [];
  const q = h("input", { class: "search", type: "search", placeholder: "جست‌وجوی نام یا یادداشت…", value: M.q, enterkeyhint: "search" });
  let t;
  q.addEventListener("input", () => { clearTimeout(t); t = setTimeout(() => { M.q = q.value; mRefresh().catch((e) => toast(e.message)); }, 300); });
  const chip = (f, label) => h("button", { class: `chip ${M.filter === f ? "on" : ""}`, onclick: () => mSet("users", f) }, label);
  out.push(h("div", { class: "searchbar" }, q, can("new") ? h("button", { class: "iconbtn primary", "aria-label": "ساخت کاربر", onclick: () => createSheet() }, mic("plus")) : null));
  out.push(h("div", { class: "chips" }, chip("", "همه"), chip("soon", "رو به اتمام"), chip("ended", "تمام‌شده"), chip("online", "آنلاین"), chip("off", "خاموش")));
  const picking = M.pick !== null;
  out.push(h("div", { class: "row between small muted listhead" },
    h("span", {}, `${num(M.count)} کاربر${M.count > M.list.length ? ` (${num(M.list.length)} تای اول)` : ""}`),
    M.list.length && (can("renew") || can("edit")) ? h("button", { class: "linkbtn", onclick: () => { M.pick = picking ? null : new Set(); render(); } }, picking ? "لغو انتخاب" : "انتخاب چندتایی") : null));
  if (!M.list.length) out.push(h("div", { class: "card empty" }, h("p", {}, M.q || M.filter ? "کاربری با این شرط نیست." : "هنوز کاربری نساخته‌اید.")));
  out.push(h("div", { class: "ulist" }, M.list.map(userRow)));
  if (picking && M.pick.size) out.push(bulkBar());
  return out;
}

function userRow(c) {
  const [tone, label] = stLabel[c.status] || ["", c.status];
  const pct = c.total > 0 ? Math.min(100, Math.round((c.used / c.total) * 100)) : 0;
  const until = c.until ? `${num(daysLeft(c.until))} روز` : c.start_days ? `${num(c.start_days)} روز از اتصال` : "بی‌زمان";
  const picking = M.pick !== null;
  const box = picking ? h("input", { type: "checkbox", class: "pickbox", checked: M.pick.has(c.name), "aria-label": c.name }) : null;
  const row = h("button", { class: `urow ${tone}`, onclick: () => {
    if (picking) { M.pick.has(c.name) ? M.pick.delete(c.name) : M.pick.add(c.name); render(); return; }
    userSheet(c.name);
  } },
    box,
    h("div", { class: "u-main" },
      h("div", { class: "u-top" }, c.online ? h("span", { class: "dot live" }) : null, h("b", { class: "ltr" }, c.name), h("span", { class: `pill ${tone}` }, label)),
      h("div", { class: "u-bar", "aria-hidden": "true" }, h("i", { style: `--w:${c.total < 0 ? 0 : pct}%` })),
      h("div", { class: "u-meta" }, h("span", {}, `${size(c.used)} از ${c.total < 0 ? "نامحدود" : size(c.total)}`), h("span", {}, until))));
  return row;
}

function bulkBar() {
  const names = [...M.pick];
  return h("div", { class: "bulkbar" }, h("b", {}, `${num(names.length)} انتخاب`),
    can("renew") ? btn("تمدید", "primary", () => renewSheet(names), "renew") : null,
    can("edit") ? btn("خاموش", "", () => setMany(names, "off"), "power") : null,
    can("edit") ? btn("روشن", "", () => setMany(names, "on"), "power") : null,
    can("delete") ? btn("حذف", "danger", () => confirmDelete(names), "trash") : null);
}

async function setMany(names, action, extra) {
  try {
    const r = await api("POST", "m/set", { emails: names, action, ...(extra || {}) });
    haptic("success"); toast(`${num(r.done)} مورد انجام شد${r.refund ? ` · برگشت ${size(r.refund)} به اعتبار` : ""}`);
    M.pick = null; await mLoad();
  } catch (e) { haptic("error"); toast(e.message); }
}

function confirmDelete(names) {
  const close = sheet(h("h2", {}, names.length > 1 ? `${num(names.length)} کاربر حذف شوند؟` : `«${names[0]}» حذف شود؟`),
    h("p", { class: "muted" }, "برای همیشه پاک می‌شود. حجم مصرف‌نشده طبق قوانین به اعتبارتان برمی‌گردد."),
    h("div", { class: "row" }, btn("بله، حذف", "danger", () => { close(); setMany(names, "delete"); }, "trash"), btn("انصراف", "", () => close())));
}

async function userSheet(name) {
  let r;
  try { r = await api("GET", `m/client?e=${encodeURIComponent(name)}`); } catch (e) { toast(e.message); return; }
  const c = r.client, [tone, label] = stLabel[c.status] || ["", c.status];
  const share = () => { try { tg.openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(r.sub)}`); } catch { copy(r.sub); } };
  const close = sheet(
    h("div", { class: "row between" }, h("h2", { class: "ltr" }, c.name), h("span", { class: `pill ${tone}` }, label)),
    r.qr ? h("img", { class: "qr", src: r.qr, alt: "QR" }) : null,
    h("div", { class: "sublink ltr", onclick: () => copy(r.sub) }, r.sub),
    h("div", { class: "row" }, btn("کپی لینک", "primary", () => copy(r.sub), "copy"), btn("ارسال", "", share, "share")),
    h("div", { class: "kv" }, h("span", {}, "مصرف"), h("b", {}, `${size(c.used)} از ${c.total < 0 ? "نامحدود" : size(c.total)}`)),
    h("div", { class: "kv" }, h("span", {}, "زمان"), h("b", {}, c.until ? `${num(daysLeft(c.until))} روز (تا ${date(c.until)})` : c.start_days ? `${num(c.start_days)} روز از اولین اتصال` : "بی‌زمان")),
    c.daily_cap ? h("div", { class: "kv" }, h("span", {}, "سقف روزانه"), h("b", {}, `${size(c.daily_used)} از ${size(c.daily_cap)} امروز`)) : null,
    c.note ? h("div", { class: "kv" }, h("span", {}, "یادداشت"), h("b", {}, c.note)) : null,
    h("div", { class: "actions" },
      can("renew") ? btn("تمدید", "primary", () => { close(); renewSheet([c.name]); }, "renew") : null,
      can("edit") ? btn(c.enabled ? "خاموش" : "روشن", "", () => { close(); setMany([c.name], c.enabled ? "off" : "on"); }, "power") : null,
      can("edit") ? btn("سقف روزانه", "", () => { close(); dailySheet(c); }, "gauge") : null,
      can("edit") ? btn("یادداشت", "", () => { close(); noteSheet(c); }, "note") : null,
      can("delete") ? btn("حذف", "danger", () => { close(); confirmDelete([c.name]); }, "trash") : null));
}

function dailySheet(c) {
  const v = h("input", { type: "number", min: "0", step: "0.5", inputmode: "decimal", value: c.daily_cap ? Math.round((c.daily_cap / 2 ** 30) * 10) / 10 : "" , placeholder: "۰ = بی‌سقف" });
  const close = sheet(h("h2", {}, "سقف مصرف روزانه"), h("p", { class: "muted small" }, "با رسیدن به سقف، کانفیگ تا شروع روز بعد (۳:۳۰ بامداد) قطع می‌شود."),
    h("label", { class: "field" }, h("span", {}, "گیگ در روز"), v),
    btn("ذخیره", "primary block", () => { close(); setMany([c.name], "daily", { daily_gb: Math.max(0, +v.value || 0) }); }, "check"));
}
function noteSheet(c) {
  const v = h("input", { value: c.note || "", maxlength: "200", placeholder: "مثلاً اسم یا شمارهٔ مشتری" });
  const close = sheet(h("h2", {}, "یادداشت"), h("label", { class: "field" }, v),
    btn("ذخیره", "primary block", () => { close(); setMany([c.name], "note", { note: v.value }); }, "check"));
}

// planPicker: ready plans and (when allowed) a custom volume and term.
function planPicker(forRenew) {
  const d = M.home, pol = d.policy || {};
  const plans = (d.plans || []).filter((p) => !(forRenew && p.no_renew));
  const st = { plan: plans.length ? plans[0].id : "", custom: !plans.length };
  const gb = h("input", { type: "number", min: "1", step: "1", inputmode: "numeric", value: "30" });
  const days = h("input", { type: "number", min: "1", step: "1", inputmode: "numeric", value: "30" });
  const ips = h("input", { type: "number", min: "0", step: "1", inputmode: "numeric", value: "0" });
  const unl = h("input", { type: "checkbox" }), first = h("input", { type: "checkbox" });
  const box = h("div", { class: "picker" });
  const draw = () => {
    box.replaceChildren(
      plans.length ? h("div", { class: "plist" }, plans.map((p) => h("button", { class: `popt ${!st.custom && st.plan === p.id ? "on" : ""}`, onclick: () => { st.plan = p.id; st.custom = false; draw(); } },
        h("b", {}, p.name), h("span", {}, `${p.plan === "unlimited" ? "نامحدود" : num(p.gb) + " گیگ"} · ${num(p.days)} روز${p.ips ? ` · ${num(p.ips)} کاربر` : ""}`)))) : null,
      d.custom ? h("button", { class: `popt ${st.custom ? "on" : ""}`, onclick: () => { st.custom = true; draw(); } }, h("b", {}, "حجم و زمان دلخواه")) : null,
      st.custom ? h("div", { class: "custom" },
        h("div", { class: "two" }, h("label", { class: "field" }, h("span", {}, "حجم (گیگ)"), gb), h("label", { class: "field" }, h("span", {}, "روز"), days)),
        !forRenew ? h("label", { class: "field" }, h("span", {}, "حداکثر کاربر هم‌زمان (۰ = بی‌سقف)"), ips) : null,
        pol.allow_unlimited !== false ? h("label", { class: "check" }, unl, h("span", {}, "حجم نامحدود")) : null,
        !forRenew ? h("label", { class: "check" }, first, h("span", {}, "زمان از اولین اتصال شروع شود")) : null) : null);
  };
  draw();
  const value = () => st.custom ? { gb: unl.checked ? 0 : +gb.value || 0, days: +days.value || 0, ips: +ips.value || 0, unlimited: unl.checked, first_use: first.checked } : { plan_id: st.plan };
  return { el: box, value };
}

function createSheet() {
  const pk = planPicker(false);
  const name = h("input", { class: "ltr", placeholder: "خالی = خودکار", autocapitalize: "off", autocomplete: "off", spellcheck: "false" });
  const count = h("input", { type: "number", min: "1", max: "50", value: "1", inputmode: "numeric" });
  const daily = h("input", { type: "number", min: "0", step: "0.5", inputmode: "decimal", placeholder: "۰ = بی‌سقف" });
  const note = h("input", { placeholder: "اختیاری" });
  const more = h("details", { class: "more" }, h("summary", {}, "تنظیمات بیشتر"),
    h("label", { class: "field" }, h("span", {}, "تعداد (ساخت گروهی، تا ۵۰)"), count),
    h("label", { class: "field" }, h("span", {}, "سقف مصرف روزانه (گیگ)"), daily),
    h("label", { class: "field" }, h("span", {}, "یادداشت"), note));
  const go = btn("بساز", "primary block big", async () => {
    go.disabled = true;
    try {
      const n = Math.max(1, Math.min(50, +count.value || 1));
      const r = await api("POST", "m/create", { ...pk.value(), name: name.value.trim(), count: n, daily_gb: +daily.value || 0, note: note.value });
      close(); haptic("success");
      if (r.names) { celebrate(`${num(r.names.length)} کاربر ساخته شد`); M.view = "users"; M.filter = ""; await mLoad(); return; }
      resultSheet(r); mLoad();
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "check");
  const close = sheet(h("h2", {}, "ساخت کاربر"), pk.el, h("label", { class: "field" }, h("span", {}, "نام کاربر"), name), more, go);
}

async function quickCreate(pl) {
  try {
    const r = await api("POST", "m/create", { plan_id: pl.id });
    haptic("success"); resultSheet(r); mLoad();
  } catch (e) { haptic("error"); toast(e.message); }
}

function resultSheet(r) {
  celebrate(`${r.name} ساخته شد`);
  const share = () => { try { tg.openTelegramLink(`https://t.me/share/url?url=${encodeURIComponent(r.sub)}`); } catch { copy(r.sub); } };
  sheet(h("h2", {}, "✅ کاربر آماده است"), h("p", { class: "muted ltr center" }, r.name),
    r.qr ? h("img", { class: "qr", src: r.qr, alt: "QR" }) : null,
    h("div", { class: "sublink ltr", onclick: () => copy(r.sub) }, r.sub),
    h("div", { class: "row" }, btn("کپی لینک", "primary", () => copy(r.sub), "copy"), btn("ارسال برای مشتری", "", share, "share")),
    can("new") ? btn("یکی دیگر بساز", "block", () => { document.querySelector(".sheet-back")?.remove(); createSheet(); }, "plus") : null);
}

function renewSheet(names) {
  const pk = planPicker(true);
  const go = btn(names.length > 1 ? `تمدید ${num(names.length)} کاربر` : "تمدید", "primary block big", async () => {
    go.disabled = true;
    try {
      const r = await api("POST", "m/renew", { ...pk.value(), emails: names });
      close(); haptic("success");
      celebrate(`${num(r.done)} کاربر تمدید شد · از اعتبار ${size(r.charged)}${r.refund ? ` · برگشت ${size(r.refund)}` : ""}`);
      M.pick = null; await mLoad();
    } catch (e) { go.disabled = false; haptic("error"); toast(e.message); }
  }, "renew");
  const close = sheet(h("h2", {}, "تمدید"), h("p", { class: "muted small" }, "دورهٔ تازه از امروز شروع می‌شود و مصرف صفر می‌شود؛ حجم مصرف‌نشدهٔ قبلی به اعتبار برمی‌گردد."), pk.el, go);
}

// ---------- inbox: orders and top-ups ----------
function inboxView() {
  if (!M.inbox.length) return [h("div", { class: "card empty" }, h("p", {}, "پرداخت منتظری نیست ✨"))];
  return [h("div", { class: "ulist" }, M.inbox.map((it) => h("div", { class: "card inbox" },
    h("div", { class: "row between" }, h("b", {}, `${it.kind === "topup" ? "➕ " : "🧾 "}${it.title} #${num(it.id)}`), h("b", {}, `${toman(it.amount)} تومان`)),
    h("div", { class: "muted small" }, `${it.buyer || ""} · ${date(it.unix)}${it.renew ? ` · تمدید ${it.renew}` : ""}`),
    it.card || it.last4 ? h("div", { class: "small" }, it.card ? `💳 به کارت …${it.card}` : "", it.last4 ? ` · ۴ رقم آخر واریزکننده: ${it.last4}` : "") : null,
    h("div", { class: "row" },
      it.receipt ? btn("رسید", "", () => showReceipt(it), "card") : h("span", { class: "muted small" }, "هنوز رسیدی نفرستاده"),
      btn("تأیید", "primary", () => decide(it, true), "check"), btn("رد", "danger", () => decide(it, false), "x")))))];
}

async function showReceipt(it) {
  try {
    const r = await api("GET", `m/receipt?k=${it.kind}&id=${it.id}`);
    sheet(h("h2", {}, `رسید #${num(it.id)}`), h("img", { class: "receipt", src: r.image, alt: "رسید" }),
      h("div", { class: "row" }, btn("تأیید", "primary", () => { document.querySelector(".sheet-back")?.remove(); decide(it, true); }, "check"),
        btn("رد", "danger", () => { document.querySelector(".sheet-back")?.remove(); decide(it, false); }, "x")));
  } catch (e) { toast(e.message); }
}

async function decide(it, ok) {
  try {
    await api("POST", "m/decide", { kind: it.kind, id: it.id, ok });
    haptic("success"); toast(ok ? "تأیید شد؛ برای مشتری فرستاده می‌شود" : "رد شد");
    await mLoad();
  } catch (e) { haptic("error"); toast(e.message); }
}
