import { useEffect, useRef, useState, type FormEvent } from "react";
import { ArrowDownLeft, ArrowUpRight, Check, Plus, RefreshCw, ShieldCheck, Users, X } from "lucide-react";
import { api, ApiError, mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { uid } from "./shared";
import { exactText, gibText, quotaInput, sellerID, validQuotaReceipt, validSeller, validSellerPage,
  validSellerPermission, type Seller, type SellerPage, type SellerPermission, type SellerQuotaReceipt } from "./sellerManagement";
import SellerClientOrdersV1 from "./SellerClientOrdersV1";
import { sellerOrderAccess, sellerOrderPath, validSellerOrderRequest, validSellerOrderReceipt,
  type SellerOrderAccess, type SellerOrderKind, type SellerOrderReceipt } from "./sellerOrdersV1";
import "./SellersV2.css";

const copy = {
  en: {
    title: "Sellers", lead: "Manage seller access and reserve credit from this representative's balance.",
    refresh: "Refresh", loading: "Loading seller access and balances…", unavailable: "Seller information could not be verified. Refresh before making a change.",
    off: "Seller management has not been enabled on this HUB yet.", denied: "This account does not have permission to manage sellers.",
    permission: "Representative access", grant: "Allow this representative to manage sellers", savePermission: "Save seller permission",
    permissionNote: "Changing this permission signs the representative out. They can sign in again with the updated access.",
    reason: "Reason", saved: "Change recorded.", failed: "The change was rejected. Refresh the current information and try again.",
    uncertain: "The result could not be confirmed. Retry the same request to recover its recorded result without repeating the transfer.", retry: "Retry same request",
    add: "Add seller", close: "Close", id: "Seller ID", name: "Seller name", telegram: "Telegram user ID", create: "Create seller",
    telegramNote: "A numeric Telegram ID identifies this seller; bot sign-in is configured separately.",
    empty: "No sellers have been created for this representative.", active: "Enabled", disabled: "Disabled",
    balance: "Reserved credit", volume: "Volume", unlimited: "Unlimited units", bytes: "bytes", approx: "approx.",
    allocate: "Allocate credit", reclaim: "Reclaim unused credit", transfer: "Record transfer", unit: "Volume unit",
    transferNote: "Allocation moves free representative credit into the seller's reserve. Reclaim returns only unused reserve.",
    disable: "Disable seller", enable: "Enable seller", state: "Seller access", previous: "Previous", next: "Next", page: "Page",
    invalid: "Enter a valid ID, name, whole-number amount and reason.", back: "Back to sellers", selected: "Seller details",
  },
  fa: {
    title: "فروشندگان", lead: "مدیریت دسترسی فروشندگان و تخصیص سهمیه از اعتبار همین نماینده",
    refresh: "تازه‌سازی", loading: "در حال دریافت دسترسی و سهمیه فروشندگان…", unavailable: "اطلاعات فروشندگان تأیید نشد؛ پیش از تغییر، دوباره دریافت کنید.",
    off: "مدیریت فروشندگان هنوز روی این هاب فعال نشده است.", denied: "این حساب مجوز مدیریت فروشندگان را ندارد.",
    permission: "دسترسی نماینده", grant: "نماینده اجازه مدیریت فروشندگان داشته باشد", savePermission: "ذخیره مجوز فروشندگان",
    permissionNote: "با تغییر این مجوز، نماینده از نشست‌های قبلی خارج می‌شود و می‌تواند با دسترسی جدید وارد شود.",
    reason: "دلیل", saved: "تغییر ثبت شد.", failed: "تغییر پذیرفته نشد؛ اطلاعات فعلی را تازه‌سازی کنید و دوباره تلاش کنید.",
    uncertain: "نتیجه درخواست تأیید نشد. همان درخواست را دوباره بفرستید تا نتیجه ثبت‌شده بدون انتقال تکراری بازیابی شود.", retry: "تلاش مجدد همان درخواست",
    add: "افزودن فروشنده", close: "بستن", id: "شناسه فروشنده", name: "نام فروشنده", telegram: "شناسه عددی تلگرام", create: "ثبت فروشنده",
    telegramNote: "شناسه عددی، فروشنده را مشخص می‌کند؛ ورود از طریق ربات جداگانه تنظیم می‌شود.",
    empty: "هنوز فروشنده‌ای برای این نماینده ثبت نشده است.", active: "فعال", disabled: "غیرفعال",
    balance: "سهمیه رزروشده", volume: "حجم", unlimited: "واحد نامحدود", bytes: "بایت", approx: "تقریبی",
    allocate: "تخصیص اعتبار", reclaim: "بازپس‌گیری سهمیه مصرف‌نشده", transfer: "ثبت انتقال", unit: "واحد حجم",
    transferNote: "تخصیص، اعتبار آزاد نماینده را به سهمیه فروشنده منتقل می‌کند. بازپس‌گیری فقط از مانده مصرف‌نشده انجام می‌شود.",
    disable: "غیرفعال‌کردن فروشنده", enable: "فعال‌کردن فروشنده", state: "دسترسی فروشنده", previous: "قبلی", next: "بعدی", page: "صفحه",
    invalid: "شناسه، نام، مقدار صحیح و دلیل معتبر وارد کنید.", back: "بازگشت به فروشندگان", selected: "جزئیات فروشنده",
  },
} as const;
type Pending = { path: string; body: Record<string, unknown>; kind: "permission" | "create" | "quota" | "state" | SellerOrderKind };
function savedRequest(value: unknown, base: string, representative: string): Pending | null {
  if (!value || typeof value !== "object") return null;
  const request = value as Pending, body = request.body;
  if (!body || typeof body !== "object" || body.representative_id !== representative ||
      typeof body.event_id !== "string" || !/^pro-[a-f0-9-]{18}$/.test(body.event_id)) return null;
  const reason = typeof body.reason === "string" && body.reason.trim().length > 0 && [...body.reason].length <= 200;
  const revision = Number.isSafeInteger(body.expected_revision) && Number(body.expected_revision) >= 0;
  if (request.kind === "permission") return request.path === base + "/seller-permission" &&
    typeof body.manage_sellers === "boolean" && reason && revision ? request : null;
  if (typeof body.seller_id !== "string" || !sellerID.test(body.seller_id)) return null;
  if (request.kind === "client_create" || request.kind === "client_extend") return (
    validSellerOrderRequest(body, request.kind, representative) &&
    request.path === sellerOrderPath(representative, body.seller_id, request.kind === "client_extend" ? String(body.client_id) : undefined) ? request : null);
  if (request.kind === "create") return request.path === base + "/sellers" &&
    typeof body.name === "string" && [...body.name].length > 0 && [...body.name].length <= 128 &&
    Number.isSafeInteger(body.telegram_user_id) && Number(body.telegram_user_id) > 0 ? request : null;
  const sellerPath = base + "/sellers/" + encodeURIComponent(body.seller_id);
  if (request.kind === "state") return request.path === sellerPath + "/state" &&
    typeof body.enabled === "boolean" && reason && revision ? request : null;
  if (request.kind === "quota") return request.path === sellerPath + "/quota" &&
    (body.action === "allocate" || body.action === "reclaim") && reason &&
    quotaInput(String(body.bytes), "bytes", String(body.units)) ? request : null;
  return null;
}

export default function SellersV2({ model, representativeID }: { model: BusinessModel; representativeID: string }) {
  const t = copy[model.language], owner = model.session.user.role === "owner";
  const base = "/api/v1/workspace/representatives/" + encodeURIComponent(representativeID);
  const [permission, setPermission] = useState<SellerPermission | null>(null);
  const [sellers, setSellers] = useState<Seller[] | null>(null);
  const [status, setStatus] = useState<"loading" | "ready" | "off" | "denied" | "unavailable">("loading");
  const [offset, setOffset] = useState(0), [refresh, setRefresh] = useState(0);
  const [selected, setSelected] = useState(""), [creating, setCreating] = useState(false);
  const [permissionValue, setPermissionValue] = useState(false), [permissionReason, setPermissionReason] = useState("");
  const [newID, setNewID] = useState(""), [newName, setNewName] = useState(""), [telegram, setTelegram] = useState("");
  const [action, setAction] = useState<"allocate" | "reclaim">("allocate");
  const [volume, setVolume] = useState("0"), [volumeUnit, setVolumeUnit] = useState("GiB"), [units, setUnits] = useState("0");
  const [reason, setReason] = useState(""), [stateReason, setStateReason] = useState("");
  const [busy, setBusy] = useState(false), [notice, setNotice] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const [orderAccess, setOrderAccess] = useState<SellerOrderAccess | null>(null);
  const [orderReceipt, setOrderReceipt] = useState<SellerOrderReceipt | null>(null);
  const pending = useRef<Pending | null>(null);
  const inFlight = useRef(false);
  const identity = model.session.user.id + ":" + representativeID;
  const currentIdentity = useRef(identity);
  currentIdentity.current = identity;
  const pendingKey = "dark-pro-seller-request-v2:" + identity;
  const [loadedIdentity, setLoadedIdentity] = useState("");
  const locked = busy || uncertain || model.actionBusy;
  const ready = status === "ready" && loadedIdentity === identity;
  const chosen = ready ? sellers?.find(s => s.id === selected) : undefined;
  const pendingOrder = pending.current?.kind === "client_create" || pending.current?.kind === "client_extend";
  const orderRetryAllowed = !pendingOrder || ready && (pending.current?.kind === "client_create" ? orderAccess?.create : orderAccess?.renew) === true;

  useEffect(() => {
    setSelected(""); setCreating(false); setOffset(0); setNotice("");
    pending.current = null; setUncertain(false); setOrderReceipt(null);
    // The journal contains no cookie, CSRF or bot token. Preserve the exact
    // request nonce across a reload after a lost response; never mint a new
    // transfer nonce merely because this component was remounted.
    try {
      const raw = sessionStorage.getItem(pendingKey);
      const recovered = raw && raw.length <= 6000 ? savedRequest(JSON.parse(raw), base, representativeID) : null;
      if (recovered) {
        pending.current = recovered; setUncertain(true); setNotice(t.uncertain);
        if (typeof recovered.body.seller_id === "string") setSelected(recovered.body.seller_id);
      } else if (raw) sessionStorage.removeItem(pendingKey);
    } catch { /* No credentials or fabricated receipts are restored. */ }
  }, [identity]);
  useEffect(() => {
    const controller = new AbortController();
    let accessVerified = false;
    setStatus("loading"); setPermission(null); setSellers(null); setLoadedIdentity(""); setOrderAccess(null);
    void (async () => {
      const access = await api<SellerPermission>(base + "/seller-permission", { signal: controller.signal });
      if (!validSellerPermission(access, representativeID)) throw new Error("unverified_seller_permission");
      accessVerified = true;
      if (controller.signal.aborted) return;
      setPermission(access); setPermissionValue(access.manage_sellers); setPermissionReason("");
      if (!owner && !access.manage_sellers) { setStatus("denied"); return; }
      const [page, account] = await Promise.all([
        api<SellerPage>(base + "/sellers?limit=25&offset=" + offset, { signal: controller.signal }),
        api<unknown>("/api/v1/workspace/me", { signal: controller.signal }).catch(() => null),
      ]);
      if (!validSellerPage(page, representativeID, offset)) throw new Error("unverified_seller_page");
      if (controller.signal.aborted) return;
      setSellers(page.sellers); setLoadedIdentity(identity); setStatus("ready");
      setOrderAccess(sellerOrderAccess(account, model.session.user, representativeID, access.manage_sellers));
    })().catch((error: unknown) => {
      if (controller.signal.aborted) return;
      setPermission(null); setSellers(null); setLoadedIdentity(""); setOrderAccess(null);
      setStatus(error instanceof ApiError && error.status === 404 && !accessVerified ? "off" : error instanceof ApiError && error.status === 403 ? "denied" : "unavailable");
    });
    return () => controller.abort();
  }, [base, representativeID, owner, identity, offset, refresh, model.refreshKey]);

  async function execute(request: Pending) {
    if (inFlight.current || busy || model.actionBusy) return;
    const alreadyUncertain = uncertain || pending.current !== null;
    const requestIdentity = identity;
    const order = request.kind === "client_create" || request.kind === "client_extend";
    if (order && (!ready || (request.kind === "client_create" ? orderAccess?.create : orderAccess?.renew) !== true)) return;
    try {
      const serialized = JSON.stringify(request);
      sessionStorage.setItem(pendingKey, serialized);
      if (sessionStorage.getItem(pendingKey) !== serialized) throw new Error("journal_unavailable");
    } catch {
      // Never start a financial write without its recoverable request nonce.
      // An already-sent request may still be retried with its original nonce.
      if (!alreadyUncertain) {
        setNotice(model.language === "fa"
          ? "ذخیرهٔ رسید پیگیری در مرورگر ممکن نیست؛ درخواست ارسال نشد. پس از آزادکردن فضای ذخیره‌سازی مرورگر دوباره تلاش کنید."
          : "The browser could not save the recovery receipt; no request was sent. Restore browser storage and retry.");
        return;
      }
    }
    inFlight.current = true;
    setBusy(true); model.setActionBusy(true); setNotice(""); pending.current = request;
    if (order) setOrderReceipt(null);
    const clearJournal = () => {
      try {
        const raw = sessionStorage.getItem(pendingKey);
        if (raw && JSON.parse(raw)?.body?.event_id === request.body.event_id) sessionStorage.removeItem(pendingKey);
      } catch { /* A late response must not clear another request's nonce. */ }
    };
    try {
      const value = await mutation<unknown>(request.path, request.body, model.session.csrf_token);
      if (currentIdentity.current !== requestIdentity) return;
      if (request.kind === "permission") {
        const v = value as SellerPermission & { replayed: boolean; sessions_revoked: boolean };
        if (!validSellerPermission(v, representativeID) || v.manage_sellers !== request.body.manage_sellers ||
            typeof v.replayed !== "boolean" || typeof v.sessions_revoked !== "boolean") throw new Error("unverified_receipt");
      } else if (request.kind === "create") {
        const v = value as Seller;
        if (!validSeller(v, representativeID) || v.id !== request.body.seller_id || v.telegram_user_id !== String(request.body.telegram_user_id)) throw new Error("unverified_receipt");
        setCreating(false); setNewID(""); setNewName(""); setTelegram(""); setOffset(0);
      } else if (request.kind === "quota") {
        if (!validQuotaReceipt(value as SellerQuotaReceipt, representativeID, String(request.body.seller_id), String(request.body.event_id), String(request.body.action))) throw new Error("unverified_receipt");
        setVolume("0"); setUnits("0"); setReason("");
      } else if (request.kind === "client_create" || request.kind === "client_extend") {
        if (!validSellerOrderReceipt(value, request.body, request.kind, model.session.user.id)) throw new Error("unverified_receipt");
        setOrderReceipt(value);
      } else {
        const v = value as { seller: Seller; replayed: boolean };
        if (!v || !validSeller(v.seller, representativeID) || v.seller.id !== request.body.seller_id ||
            v.seller.enabled !== request.body.enabled || typeof v.replayed !== "boolean") throw new Error("unverified_receipt");
        setStateReason("");
      }
      clearJournal(); pending.current = null; setUncertain(false); setNotice(t.saved);
      setRefresh(n => n + 1); model.reload();
    } catch (error: unknown) {
      if (currentIdentity.current !== requestIdentity) return;
      // A rejected retry says nothing about whether the original request
      // committed before its response was lost, including after revocation.
      const rejected = !alreadyUncertain && error instanceof ApiError && error.status >= 400 && error.status < 500;
      if (rejected) { clearJournal(); pending.current = null; setUncertain(false); setNotice(t.failed); setRefresh(n => n + 1); }
      else { setUncertain(true); setNotice(t.uncertain); }
    } finally { inFlight.current = false; setBusy(false); model.setActionBusy(false); }
  }
  function submit(kind: Pending["kind"], path: string, body: Record<string, unknown>) {
    if (locked || !ready) return;
    void execute({ kind, path, body: { ...body, event_id: uid(), representative_id: representativeID } });
  }
  function create(event: FormEvent) {
    event.preventDefault();
    const numericID = Number(telegram);
    if (!sellerID.test(newID) || !newName.trim() || [...newName.trim()].length > 128 ||
        !/^[1-9][0-9]{0,15}$/.test(telegram) || !Number.isSafeInteger(numericID)) { setNotice(t.invalid); return; }
    submit("create", base + "/sellers", { seller_id: newID, name: newName.trim(), telegram_user_id: numericID });
  }
  function transfer(event: FormEvent) {
    event.preventDefault();
    const amount = quotaInput(volume, volumeUnit, units);
    if (!chosen || !amount || !reason.trim()) { setNotice(t.invalid); return; }
    submit("quota", base + "/sellers/" + encodeURIComponent(chosen.id) + "/quota", {
      seller_id: chosen.id, action, ...amount, reason: reason.trim(),
    });
  }
  return <section className="business-panel sellers-v2" aria-label="Seller management">
    <header className="sellers-heading"><div><h3><Users size={18}/>{t.title}</h3><p>{t.lead}</p></div>
      <button type="button" onClick={() => setRefresh(n => n + 1)} disabled={busy}><RefreshCw size={15}/>{t.refresh}</button></header>
    {notice && <div role={uncertain ? "alert" : "status"} className="sellers-notice">{notice}
      {uncertain && <button type="button" disabled={busy || !orderRetryAllowed} onClick={() => pending.current && void execute(pending.current)}>{t.retry}</button>}</div>}
    {!ready ? <p role="status">{t[status === "ready" ? "loading" : status]}</p> : <>
      {owner && permission && <form className="seller-permission" onSubmit={e => {
        e.preventDefault();
        if (permissionValue === permission.manage_sellers || !permissionReason.trim()) return;
        submit("permission", base + "/seller-permission", { expected_revision: permission.revision, manage_sellers: permissionValue, reason: permissionReason.trim() });
      }}>
        <h4><ShieldCheck size={16}/>{t.permission}</h4>
        <label className="seller-checkbox"><input type="checkbox" aria-label="Allow seller management" checked={permissionValue} disabled={locked}
          onChange={e => setPermissionValue(e.target.checked)}/><span>{t.grant}</span></label>
        <p>{t.permissionNote}</p>
        {permissionValue !== permission.manage_sellers && <div className="seller-form-row">
          <label>{t.reason}<input aria-label="Seller permission reason" required maxLength={200} value={permissionReason} disabled={locked} onChange={e => setPermissionReason(e.target.value)}/></label>
          <button type="submit" disabled={locked || !permissionReason.trim()}><Check size={15}/>{t.savePermission}</button>
        </div>}
      </form>}
      {!chosen ? <>
        <div className="sellers-toolbar"><span><strong>{representativeID}</strong></span>
          <button type="button" disabled={locked} onClick={() => setCreating(v => !v)}>{creating ? <X size={16}/> : <Plus size={16}/>}{creating ? t.close : t.add}</button></div>
        {creating && <form className="seller-create" aria-label="Create seller" onSubmit={create}>
          <div className="seller-form-grid">
            <label>{t.id}<input aria-label="Seller ID" autoComplete="off" maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,63}" required value={newID} disabled={locked} onChange={e => setNewID(e.target.value)}/></label>
            <label>{t.name}<input aria-label="Seller name" maxLength={128} required value={newName} disabled={locked} onChange={e => setNewName(e.target.value)}/></label>
            <label>{t.telegram}<input aria-label="Telegram user ID" inputMode="numeric" pattern="[1-9][0-9]{0,15}" maxLength={16} required value={telegram} disabled={locked} onChange={e => setTelegram(e.target.value)}/></label>
          </div><p>{t.telegramNote}</p><button type="submit" disabled={locked}><Plus size={15}/>{t.create}</button>
        </form>}
        {sellers?.length === 0 ? <p className="seller-empty">{t.empty}</p> : <div className="seller-list">
          {sellers?.map(seller => <button className="seller-row" type="button" key={seller.id} disabled={locked}
            onClick={() => { setSelected(seller.id); setReason(""); setStateReason(""); setVolume("0"); setUnits("0"); }}>
            <span><strong>{seller.name}</strong><small dir="ltr">{seller.id} · {seller.telegram_user_id}</small></span>
            <span className="seller-row-balance"><strong dir="ltr">≈ {gibText(seller.reserved_bytes)} GiB</strong><small><bdi>{exactText(seller.reserved_bytes)}</bdi> {t.bytes}</small><small>{exactText(seller.reserved_units)} {t.unlimited}</small></span>
            <span className={seller.enabled ? "seller-enabled" : "seller-disabled"}>{seller.enabled ? t.active : t.disabled}</span>
          </button>)}
        </div>}
        <footer className="seller-pagination"><button type="button" disabled={locked || offset === 0} onClick={() => setOffset(n => Math.max(0, n - 25))}>{t.previous}</button>
          <span>{t.page} {offset / 25 + 1}</span><button type="button" disabled={locked || sellers?.length !== 25} onClick={() => setOffset(n => n + 25)}>{t.next}</button></footer>
      </> : <>
        <button className="seller-back" type="button" disabled={locked} onClick={() => setSelected("")}>{t.back}</button>
        <section className="seller-detail" aria-label="Seller details"><header><div><h4>{chosen.name}</h4><small dir="ltr">{chosen.id} · {chosen.telegram_user_id}</small></div><span>{chosen.enabled ? t.active : t.disabled}</span></header>
          <div className="seller-balances"><div><span>{t.balance} · {t.volume}</span><strong dir="ltr">{gibText(chosen.reserved_bytes)} GiB</strong>
            <small>{t.approx} · <bdi>{exactText(chosen.reserved_bytes)}</bdi> {t.bytes}</small></div><div><span>{t.unlimited}</span><strong>{exactText(chosen.reserved_units)}</strong></div></div>
          <SellerClientOrdersV1 key={identity + ":" + chosen.id} seller={chosen} access={orderAccess} blocked={locked} language={model.language}
            receipt={orderReceipt?.seller_id === chosen.id ? orderReceipt : null} onSubmit={(kind, body) => {
              if (!chosen.enabled || (kind === "client_create" ? orderAccess?.create : orderAccess?.renew) !== true) return;
              submit(kind, sellerOrderPath(representativeID, chosen.id, kind === "client_extend" ? String(body.client_id) : undefined), body);
            }}/>
          <form className="seller-transfer" onSubmit={transfer}><h4>{action === "allocate" ? <ArrowDownLeft size={16}/> : <ArrowUpRight size={16}/>} {t.balance}</h4>
            <label><select aria-label="Quota action" value={action} disabled={locked} onChange={e => setAction(e.target.value as "allocate" | "reclaim")}>
              <option value="allocate" disabled={!chosen.enabled}>{t.allocate}</option><option value="reclaim">{t.reclaim}</option></select></label>
            <div className="seller-form-grid">
              <label>{t.volume}<input aria-label="Quota volume" inputMode="numeric" pattern="(0|[1-9][0-9]*)" required value={volume} disabled={locked} onChange={e => setVolume(e.target.value)}/></label>
              <label>{t.unit}<select aria-label="Quota volume unit" value={volumeUnit} disabled={locked} onChange={e => setVolumeUnit(e.target.value)}><option>GiB</option><option>MiB</option><option value="bytes">{t.bytes}</option></select></label>
              <label>{t.unlimited}<input aria-label="Quota unlimited units" inputMode="numeric" pattern="(0|[1-9][0-9]*)" required value={units} disabled={locked} onChange={e => setUnits(e.target.value)}/></label>
            </div><label>{t.reason}<input aria-label="Quota transfer reason" maxLength={200} required value={reason} disabled={locked} onChange={e => setReason(e.target.value)}/></label>
            <p>{t.transferNote}</p><button type="submit" disabled={locked || !quotaInput(volume, volumeUnit, units) || !reason.trim() || (action === "allocate" && !chosen.enabled)}>{t.transfer}</button>
          </form>
          <form className="seller-state" onSubmit={e => { e.preventDefault(); if (stateReason.trim()) submit("state", base + "/sellers/" + encodeURIComponent(chosen.id) + "/state", { seller_id: chosen.id, expected_revision: chosen.revision, enabled: !chosen.enabled, reason: stateReason.trim() }); }}>
            <h4>{t.state}</h4><label>{t.reason}<input aria-label="Seller state reason" maxLength={200} required value={stateReason} disabled={locked} onChange={e => setStateReason(e.target.value)}/></label>
            <button type="submit" disabled={locked || !stateReason.trim()}>{chosen.enabled ? t.disable : t.enable}</button>
          </form>
        </section>
      </>}
    </>}
  </section>;
}
