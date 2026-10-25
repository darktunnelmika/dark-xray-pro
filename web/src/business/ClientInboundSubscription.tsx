import { useEffect, useRef, useState } from "react";
import { api, ApiError, type Language, type Session } from "../session";
import { readInboundSubscriptionAttempt, readInboundSubscriptionReceipt, readInboundSubscriptionSecret, readInboundSubscriptionView, subscriptionRequestRejected, type InboundSubscriptionAttempt as Attempt, type InboundSubscriptionView as View } from "../inboundSubscriptions";
import ClientSubscription from "./ClientSubscription";
import "../LabSubscriptions.css";

type Props = { clientID: string; language: Language; session: Session; canManage: boolean; refresh: number };
type Revealed = { path: string; generation: number; scope: string; auth: string; serial: number };
const words = {
  fa: { title: "اشتراک اینباند", refresh: "به‌روزرسانی", loading: "در حال بررسی مسیرهای این کلاینت…", unavailable: "وضعیت اشتراک قابل تأیید نیست. دوباره بررسی کنید.", ready: "تنظیمات مسیرهای در حال اجرا آمادهٔ دریافت است.", withheld: "هنوز مسیر قابل دریافت و تأییدشده‌ای برای این کلاینت وجود ندارد.", route: "تعداد مسیرها", unverified: "آماده‌بودن موتور ثبت شده است؛ اتصال کامل مسیر هنوز آزموده نشده.", verified: "اتصال مسیرها نیز آزموده شده است.", issue: "ساخت لینک اشتراک", reveal: "نمایش لینک", hide: "پنهان‌کردن لینک", rotate: "تعویض لینک", revoke: "لغو لینک", download: "دریافت تنظیمات", copy: "کپی لینک", copied: "کپی شد", format: "فرمت", absent: "لینک هنوز ساخته نشده", active: "لینک فعال است", revoked: "لینک لغو شده", unknown: "پاسخ عملیات تأیید نشده است؛ همان درخواست را دوباره ارسال کنید.", retry: "تلاش مجدد همان عملیات", secret: "این لینک دسترسی به تنظیمات کلاینت می‌دهد و پس از یک دقیقه پنهان می‌شود.", boundary: "تعویض یا لغو لینک، دریافت اشتراک را کنترل می‌کند. برای قطع اتصال تنظیمات واردشده، کلاینت را غیرفعال کنید.", saved: "عملیات ثبت شد.", storage: "ثبت درخواست قابل اطمینان نیست؛ مرورگر را بررسی کنید.", rejected: "این درخواست رد شد. وضعیت را به‌روز کنید و سپس درخواست را کنار بگذارید تا عملیات تازه‌ای آغاز شود.", discard: "کنارگذاشتن درخواست ردشده", reset: "بازنشانی درخواست محلی", confirmRotate: "لینک فعلی لغو و یک لینک جدید ساخته شود؟", confirmRevoke: "دریافت تنظیمات با لینک فعلی لغو شود؟" },
  en: { title: "Inbound subscription", refresh: "Refresh", loading: "Checking this client's routes…", unavailable: "Subscription state could not be verified. Please refresh.", ready: "Configuration for the running routes is available.", withheld: "This client has no currently available delivery routes.", route: "Routes", unverified: "Engine readiness is recorded; the complete connection path has not been tested.", verified: "Connection paths have also been tested.", issue: "Create subscription link", reveal: "Show link", hide: "Hide link", rotate: "Replace link", revoke: "Revoke link", download: "Download configuration", copy: "Copy link", copied: "Copied", format: "Format", absent: "No link issued", active: "Link active", revoked: "Link revoked", unknown: "The operation response was not confirmed. Retry the same request.", retry: "Retry exact operation", secret: "This link grants access to the client's configuration. It is hidden after one minute.", boundary: "Replacing or revoking a link controls subscription downloads. Disable the client to stop connections using previously imported settings.", saved: "Operation recorded.", storage: "The request could not be retained safely. Check browser storage.", rejected: "This request was rejected. Refresh the state, then discard the rejected request before starting a new operation.", discard: "Discard rejected request", reset: "Reset local request", confirmRotate: "Revoke the current link and create a new one?", confirmRevoke: "Revoke configuration downloads through the current link?" }
};


export default function ClientInboundSubscription(props: Props) {
  const { clientID, language, session, canManage, refresh } = props, t = words[language];
  const base = "/api/v1/workspace/clients/" + encodeURIComponent(clientID) + "/inbound-subscription";
  const storageKey = "darkpro:managed-sub:" + session.user.id + ":" + clientID;
  const [view, setView] = useState<View | null>(null), [failed, setFailed] = useState(false), [busy, setBusy] = useState(false);
  const [format, setFormat] = useState("uri"), [secret, setSecret] = useState<Revealed | null>(null), [notice, setNotice] = useState("");
  const [copied, setCopied] = useState(false), [attempt, setAttempt] = useState<Attempt | null>(null), [rejected, setRejected] = useState(false);
  const [storageFailed, setStorageFailed] = useState(false), [clock, setClock] = useState(0);
  const lifetime = useRef<AbortController | null>(null), activity = useRef<{ id: number; kind: "mutation" | "reveal" } | null>(null);
  const requestSerial = useRef(0), secretSerial = useRef(0), metadataSerial = useRef(0), checked = useRef(0);
  const viewRef = useRef<View | null>(null), loadRef = useRef<() => Promise<void>>(async () => {});
  const access = useRef({ canManage, storageKey, csrf: session.csrf_token });
  access.current = { canManage, storageKey, csrf: session.csrf_token };
  const clearSecret = () => { secretSerial.current++; setSecret(null); setCopied(false); };

  useEffect(() => {
    const c = new AbortController(); lifetime.current = c; activity.current = null;
    viewRef.current = null; checked.current = 0; setView(null); setFailed(false); setBusy(false); setNotice(""); setRejected(false); clearSecret();
    try { setAttempt(readInboundSubscriptionAttempt(sessionStorage.getItem(storageKey))); setStorageFailed(false); }
    catch { setAttempt(null); setStorageFailed(true); }
    let seenManaged = false;
    const load = async () => {
      if (c.signal.aborted || activity.current?.kind === "mutation") return;
      const serial = ++metadataSerial.current;
      try {
        const next = readInboundSubscriptionView(await api<unknown>(base, { signal: c.signal }), clientID);
        if (c.signal.aborted || serial !== metadataSerial.current) return;
        const previous = viewRef.current;
        if (previous?.enabled && next.enabled && next.generation < previous.generation) throw Error("stale_subscription_state");
        if (!next.enabled || !previous?.enabled || next.generation !== previous.generation || next.state !== previous.state) clearSecret();
        seenManaged ||= next.enabled; viewRef.current = next; checked.current = performance.now(); setView(next); setFailed(false);
        if (next.enabled && next.formats.length) setFormat(old => next.formats.includes(old) ? old : next.formats[0]);
      } catch (error) {
        if (c.signal.aborted || serial !== metadataSerial.current) return;
        clearSecret();
        // Older installations have no managed endpoint. Once this installation
        // advertised it, a missing/denied response cannot downgrade its scope.
        if (!seenManaged && error instanceof ApiError && error.status === 404) {
          const legacy: View = { enabled: false, client_id: clientID }; viewRef.current = legacy; setView(legacy); setFailed(false);
        } else setFailed(true);
      }
    };
    loadRef.current = load; void load();
    const poll = window.setInterval(() => { if (!document.hidden) void load(); }, 5000);
    const tick = window.setInterval(() => setClock(performance.now()), 1000);
    const hidden = () => { if (document.hidden) clearSecret(); else void load(); };
    document.addEventListener("visibilitychange", hidden);
    return () => { c.abort(); metadataSerial.current++; secretSerial.current++; clearInterval(poll); clearInterval(tick); document.removeEventListener("visibilitychange", hidden); if (lifetime.current === c) lifetime.current = null; };
  }, [base, clientID, storageKey, session.csrf_token, canManage]);
  useEffect(() => { clearSecret(); void loadRef.current(); }, [refresh]);
  useEffect(() => { if (!secret) return; const timer = window.setTimeout(clearSecret, 60000); return () => clearTimeout(timer); }, [secret]);

  const remember = (value: Attempt | null) => {
    try {
      if (value) sessionStorage.setItem(storageKey, JSON.stringify(value)); else sessionStorage.removeItem(storageKey);
      setAttempt(value); setStorageFailed(false); return true;
    } catch { setStorageFailed(true); setNotice(t.storage); return false; }
  };
  const authorized = (c: AbortController) => lifetime.current === c && !c.signal.aborted && access.current.canManage && access.current.storageKey === storageKey && access.current.csrf === session.csrf_token;
  const send = async (a: Attempt) => {
    const c = lifetime.current; if (!c || !authorized(c) || activity.current || storageFailed) return;
    const id = ++requestSerial.current; activity.current = { id, kind: "mutation" }; metadataSerial.current++;
    setBusy(true); clearSecret(); setNotice(""); setRejected(false);
    try {
      const out = await api<unknown>(base + "/" + a.action, { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token }, body: JSON.stringify({ event_id: a.event_id, expected_generation: a.expected_generation }), signal: c.signal });
      readInboundSubscriptionReceipt(out, clientID, a);
      if (authorized(c) && remember(null)) setNotice(t.saved);
    } catch (error) {
      if (authorized(c)) { setRejected(subscriptionRequestRejected(error)); setNotice(subscriptionRequestRejected(error) ? t.rejected : t.unknown); }
    } finally {
      if (activity.current?.id === id) activity.current = null;
      if (lifetime.current === c && !c.signal.aborted) { setBusy(false); void loadRef.current(); }
    }
  };
  const change = (action: Attempt["action"]) => {
    if (!view?.enabled || view.generation >= Number.MAX_SAFE_INTEGER || attempt || busy || failed || !canManage || storageFailed) return;
    if (action !== "issue" && !window.confirm(action === "rotate" ? t.confirmRotate : t.confirmRevoke)) return;
    const next: Attempt = { action, event_id: "managed-sub-" + crypto.randomUUID(), expected_generation: view.generation };
    if (remember(next)) void send(next);
  };
  const reveal = async () => {
    const c = lifetime.current; if (!c || !authorized(c) || !view?.enabled || view.state !== "active" || attempt || busy || failed || activity.current) return;
    clearSecret(); const serial = secretSerial.current, generation = view.generation, id = ++requestSerial.current;
    activity.current = { id, kind: "reveal" }; setBusy(true); setNotice("");
    try {
      const out = await api<unknown>(base + "/reveal", { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token }, body: JSON.stringify({ expected_generation: generation }), signal: c.signal });
      const path = readInboundSubscriptionSecret(out, clientID, generation), latest = viewRef.current;
      if (authorized(c) && serial === secretSerial.current && !document.hidden && latest?.enabled && latest.state === "active" && latest.generation === generation) setSecret({ path, generation, scope: storageKey, auth: session.csrf_token, serial });
    } catch { if (authorized(c)) setNotice(t.unavailable); }
    finally { if (activity.current?.id === id) activity.current = null; if (lifetime.current === c && !c.signal.aborted) setBusy(false); }
  };
  const discard = () => { if (!busy && remember(null)) { setRejected(false); setNotice(""); clearSecret(); void loadRef.current(); } };
  const serverNow = view?.enabled ? view.server_time_unix_milli + Math.max(0, Math.max(clock, performance.now()) - checked.current) : 0;
  const current = !!view?.enabled && view.available && !failed && serverNow < view.expires_unix_nano / 1e6;
  const visibleSecret = canManage && !failed && view?.enabled && view.state === "active" && secret?.scope === storageKey && secret.auth === session.csrf_token && secret.generation === view.generation && secret.serial === secretSerial.current ? secret : null;
  const url = visibleSecret ? new URL(visibleSecret.path + "?format=" + encodeURIComponent(format), window.location.origin).href : "";
  if (view && !view.enabled) return <ClientSubscription {...props}/>;
  return <section className="business-panel lab-subscription" aria-label="Client inbound subscription">
    <div className="lab-subscription-heading"><h3>{t.title}</h3><button type="button" disabled={busy} onClick={() => { clearSecret(); void loadRef.current(); }}>{t.refresh}</button></div>
    {!view && <p role="status">{failed ? t.unavailable : t.loading}</p>}
    {view?.enabled && <>
      <p role="status">{failed ? t.unavailable : current ? t.ready : t.withheld}</p><p>{t[view.state]} · {t.route}: {view.route_count}</p>
      {current && <p>{view.path_verified ? t.verified : t.unverified}</p>}
      {view.formats.length > 0 && <label>{t.format}<select aria-label={t.format} value={format} onChange={event => { setFormat(event.target.value); setCopied(false); }}>{view.formats.map(value => <option key={value} value={value}>{value === "uri" ? "URI" : value === "xray" ? "Xray JSON" : value === "wireguard" ? "WireGuard / AmneziaWG" : value}</option>)}</select></label>}
      {canManage && <div className="lab-subscription-actions">
        {view.state === "absent" ? <button type="button" disabled={busy || failed || !!attempt || storageFailed} onClick={() => change("issue")}>{t.issue}</button> : <>
          {view.state === "active" && <button type="button" disabled={busy || failed || !!attempt} onClick={() => void reveal()}>{t.reveal}</button>}
          <button type="button" disabled={busy || failed || !!attempt || storageFailed} onClick={() => change("rotate")}>{t.rotate}</button>
          {view.state === "active" && <button type="button" disabled={busy || failed || !!attempt || storageFailed} onClick={() => change("revoke")}>{t.revoke}</button>}
        </>}
        {current && <a className="business-button" href={base + "/export?format=" + encodeURIComponent(format)} download={"inbound-" + clientID + (format === "uri" ? ".txt" : format === "wireguard" ? ".conf" : ".json")}>{t.download}</a>}
      </div>}
    </>}
    {attempt && <div role="status"><p>{rejected ? t.rejected : t.unknown}</p>{rejected ? <button type="button" disabled={busy} onClick={discard}>{t.discard}</button> : <button type="button" disabled={busy || storageFailed || !canManage} onClick={() => void send(attempt)}>{t.retry}</button>}</div>}
    {notice && !attempt && <p role="status">{notice}</p>}
    {storageFailed && <div role="status"><p>{t.storage}</p>{!attempt && <button type="button" disabled={busy} onClick={discard}>{t.reset}</button>}</div>}
    {visibleSecret && <div className="lab-subscription-secret"><p>{t.secret}</p><label>{t.reveal}<input aria-label="Inbound subscription URL" dir="ltr" readOnly autoComplete="off" value={url}/></label><div className="lab-subscription-actions"><button type="button" onClick={() => {
      const serial = secretSerial.current, c = lifetime.current;
      void navigator.clipboard.writeText(url).then(() => { if (c && authorized(c) && serial === secretSerial.current) setCopied(true); }).catch(() => { if (c && authorized(c)) setNotice(t.unavailable); });
    }}>{copied ? t.copied : t.copy}</button><button type="button" onClick={clearSecret}>{t.hide}</button></div></div>}
    <p>{t.boundary}</p>
  </section>;
}
