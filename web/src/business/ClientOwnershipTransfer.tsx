import { useEffect, useRef, useState } from "react";
import { ArrowRightLeft, Check, LoaderCircle, Search, ShieldCheck } from "lucide-react";
import { api, ApiError, type Language } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { ownershipID, validOwnershipIntent, quoteOwnership, commitOwnership, findOwnershipReceipt, loadOwnershipPending, saveOwnershipPending, clearOwnershipPending,
  type OwnershipIntent, type OwnershipItem, type OwnershipQuote, type OwnershipReceipt, type PendingOwnership } from "./clientOwnership";
import "./ClientOwnershipTransfer.css";

const words = {
  en: {
    title: "Assign clients to a representative", intro: "Change who manages these clients, not who paid for their earlier orders.", selected: "Selected clients", group: "Entire source group", mode: "Selection", target: "Destination representative", search: "Search all representatives", chosen: "Selected destination", sourceGroup: "Source group", targetGroup: "Destination group", none: "No group", pick: "Choose a group", previous: "Previous", next: "Next", reason: "Enter the audit reason above.", preview: "Review assignment", confirm: "Confirm assignment", consent: "I reviewed the exact clients and destination. Move their management access.", preserve: "No credit is charged or returned. UUID, subscription credentials, expiry and recorded usage are preserved. New renewals use the destination account's credit.", funding: "Earlier payments remain with their original funder. When different representatives have funded one plan, automatic unused-volume refunds are held for review; no recipient is guessed.", maximum: "Choose clients above or one complete group (up to 500). No partial group is silently selected.", empty: "No results on this page", unavailable: "The account/group list could not be verified.", failed: "The preview was refused. Refresh the client data and check the destination, group and audit reason. Active clients of a disabled source account must be explicitly disabled first.", changed: "The preview is stale or the request was refused. Check the recorded result before preparing a new assignment.", uncertain: "The result is not confirmed. The saved request is checked automatically; refreshing never submits another transfer.", retry: "Retry the identical request", refresh: "Check recorded result", storage: "The browser could not save the request. No transfer was sent.", success: "Management assignment recorded", applied: "clients assigned", unknown: "Usage baseline unknown", counters: "Recorded bytes", pending: "Saved transfer request", close: "Close preview", disabled: "Ownership transfer is unavailable on this installation.", all: "Every displayed member will move together", noneChosen: "No representative selected", hold: "reconciliation hold", show: "Show all members", retained: "Keep this request until its outcome is confirmed.", reset: "Discard refused request and review again",
  },
  fa: {
    title: "واگذاری کاربران به نماینده", intro: "مدیر مشتری عوض می‌شود؛ پرداخت‌کنندهٔ سفارش‌های قبلی عوض نمی‌شود.", selected: "کاربران انتخاب‌شده", group: "تمام اعضای یک گروه", mode: "نوع انتخاب", target: "نمایندهٔ مقصد", search: "جست‌وجو در تمام نمایندگان", chosen: "مقصد انتخاب‌شده", sourceGroup: "گروه مبدأ", targetGroup: "گروه مقصد", none: "بدون گروه", pick: "یک گروه انتخاب کن", previous: "قبلی", next: "بعدی", reason: "دلیل حسابرسی را در کادر بالا وارد کن.", preview: "پیش‌نمایش واگذاری", confirm: "تأیید واگذاری", consent: "کاربران و مقصد دقیق را بررسی کردم؛ دسترسی مدیریت آن‌ها منتقل شود.", preserve: "از اعتبار هیچ‌کس کم یا به آن اضافه نمی‌شود. UUID، هویت اشتراک، انقضا و مصرف ثبت‌شده حفظ می‌شوند. تمدیدهای بعدی از اعتبار نمایندهٔ مقصد انجام می‌شوند.", funding: "پرداخت‌های قبلی متعلق به پرداخت‌کنندهٔ قبلی می‌مانند. اگر یک پلن از اعتبار چند نماینده تأمین شده باشد، برگشت خودکار حجم تا بررسی متوقف می‌شود؛ گیرندهٔ پول حدس زده نمی‌شود.", maximum: "از فهرست بالا کاربر انتخاب کن یا یک گروه کامل تا ۵۰۰ عضو را واگذار کن. بخشی از گروه پنهانی انتخاب نمی‌شود.", empty: "در این صفحه نتیجه‌ای نیست", unavailable: "فهرست نماینده یا گروه قابل تأیید نیست.", failed: "پیش‌نمایش پذیرفته نشد. اطلاعات کاربران، مقصد، گروه و دلیل را بررسی کن. کاربران فعالِ حساب مبدأ غیرفعال، ابتدا باید صریحاً غیرفعال شوند.", changed: "پیش‌نمایش قدیمی شده یا درخواست رد شده است. قبل از ساخت درخواست تازه، نتیجهٔ ثبت‌شده را بررسی کن.", uncertain: "نتیجه هنوز تأیید نشده است. همان درخواست ذخیره‌شده خودکار بررسی می‌شود؛ تازه‌سازی، واگذاری دیگری نمی‌فرستد.", retry: "تکرار دقیق همان درخواست", refresh: "بررسی نتیجهٔ ثبت‌شده", storage: "مرورگر نتوانست درخواست را نگه دارد؛ هیچ واگذاری ارسال نشد.", success: "واگذاری مدیریت ثبت شد", applied: "کاربر واگذار شد", unknown: "مبنای مصرف نامشخص", counters: "بایت ثبت‌شده", pending: "درخواست واگذاری ذخیره‌شده", close: "بستن پیش‌نمایش", disabled: "واگذاری کاربران در این نصب در دسترس نیست.", all: "تمام اعضای نمایش‌داده‌شده با هم منتقل می‌شوند", noneChosen: "نماینده انتخاب نشده", hold: "نیازمند تطبیق مصرف", show: "نمایش تمام اعضا", retained: "تا مشخص‌شدن نتیجه، همین درخواست را حفظ کن.", reset: "کنارگذاشتن درخواست ردشده و بررسی دوباره",
  },
} as const;
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
type Pick = { id: string; name: string };

function RepresentativePicker({ language, value, disabled, onChange }: { language: Language; value: Pick | null; disabled: boolean; onChange: (v: Pick) => void }) {
  const t = words[language]; const [draft, setDraft] = useState(""); const [query, setQuery] = useState(""); const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<{ rows: Pick[]; more: boolean } | null>(null); const [failed, setFailed] = useState(false);
  useEffect(() => { const timer = window.setTimeout(() => { setQuery(draft.trim()); setOffset(0); }, 250); return () => window.clearTimeout(timer); }, [draft]);
  useEffect(() => {
    const abort = new AbortController(); setPage(null); setFailed(false);
    const q = new URLSearchParams({ q: query, state: "enabled", sort: "name", limit: "50", offset: String(offset) });
    void api<unknown>("/api/v1/workspace/representatives?" + q, { signal: abort.signal }).then(v => {
      if (abort.signal.aborted) return;
      if (!object(v) || v.limit !== 50 || v.offset !== offset || v.q !== query || v.state !== "enabled" || v.sort !== "name" || !Array.isArray(v.representatives) || v.representatives.length > 50 || typeof v.has_more !== "boolean" ||
        !v.representatives.every(r => object(r) && typeof r.id === "string" && ownershipID.test(r.id) && typeof r.display_name === "string" && r.enabled === true && (r.kind === "representative" || r.kind === "owner"))) throw new Error("invalid_representative_page");
      setPage({ rows: v.representatives.map(r => ({ id: r.id as string, name: r.display_name as string })), more: v.has_more });
    }).catch(() => { if (!abort.signal.aborted) setFailed(true); });
    return () => abort.abort();
  }, [query, offset]);
  return <div className="ownership-picker">
    <label>{t.search}<span className="ownership-search"><Search size={15} /><input type="search" aria-label={t.search} value={draft} maxLength={128} disabled={disabled} onChange={e => setDraft(e.target.value)} /></span></label>
    <p className="ownership-chosen">{t.chosen}: <strong>{value ? `${value.name} (${value.id})` : t.noneChosen}</strong></p>
    {failed ? <p role="alert">{t.unavailable}</p> : page && query === draft.trim() ? <>
      <div className="ownership-picker-options">{page.rows.map(r => <button type="button" key={r.id} disabled={disabled} aria-pressed={value?.id === r.id} onClick={() => onChange(r)}><span>{r.name}</span><small dir="ltr">{r.id}</small>{value?.id === r.id && <Check size={14} />}</button>)}</div>
      {!page.rows.length && <p>{t.empty}</p>}
      <div className="ownership-pages"><button type="button" disabled={disabled || offset === 0} onClick={() => setOffset(n => Math.max(0, n - 50))}>{t.previous}</button><button type="button" disabled={disabled || !page.more || offset >= 999950} onClick={() => setOffset(n => n + 50)}>{t.next}</button></div>
    </> : <span role="status"><LoaderCircle size={16} /></span>}
  </div>;
}
function GroupPicker({ language, representative, label, value, disabled, optional, onChange }: { language: Language; representative?: string; label: string; value: string; disabled: boolean; optional: boolean; onChange: (id: string) => void }) {
  const t = words[language]; const [offset, setOffset] = useState(0); const [rows, setRows] = useState<Pick[] | null>(null); const [failed, setFailed] = useState(false);
  useEffect(() => {
    const abort = new AbortController(); setRows(null); setFailed(false);
    const q = new URLSearchParams({ limit: "50", offset: String(offset) }); if (representative) q.set("representative_id", representative);
    void api<unknown>("/api/v1/workspace/groups?" + q, { signal: abort.signal }).then(v => {
      if (abort.signal.aborted) return;
      if (!object(v) || v.limit !== 50 || v.offset !== offset || !Array.isArray(v.groups) || v.groups.length > 50 ||
        !v.groups.every(g => object(g) && typeof g.id === "string" && ownershipID.test(g.id) && typeof g.name === "string" && (!representative || g.representative_id === representative))) throw new Error("invalid_group_page");
      setRows(v.groups.map(g => ({ id: g.id as string, name: g.name as string })));
    }).catch(() => { if (!abort.signal.aborted) setFailed(true); });
    return () => abort.abort();
  }, [representative, offset]);
  return <div className="ownership-picker"><label>{label}<select aria-label={label} value={value} disabled={disabled || !rows || failed} onChange={e => onChange(e.target.value)}>
    <option value="">{optional ? t.none : t.pick}</option>
    {value && !rows?.some(r => r.id === value) && <option value={value}>{value}</option>}
    {rows?.map(r => <option key={r.id} value={r.id}>{r.name} ({r.id})</option>)}
  </select></label>{failed && <p role="alert">{t.unavailable}</p>}
    <div className="ownership-pages"><button type="button" disabled={disabled || !rows || offset === 0} onClick={() => setOffset(n => Math.max(0, n - 50))}>{t.previous}</button><button type="button" disabled={disabled || !rows || rows.length < 50 || offset >= 999950} onClick={() => setOffset(n => n + 50)}>{t.next}</button></div>
  </div>;
}
function Counter({ item, language }: { item: OwnershipItem; language: Language }) {
  const t = words[language];
  if (!item.usage_present || !item.usage_complete) return <span>{t.unknown}</span>;
  return <span>{t.counters}: <bdi>{(BigInt(item.uplink_bytes) + BigInt(item.downlink_bytes)).toLocaleString(language === "fa" ? "fa-IR" : "en-US")}</bdi>{item.reconciliation_holds > 0 ? ` · ${t.hold}` : ""}</span>;
}
export default function ClientOwnershipTransfer({ model, selected, reason, onCommitted }: { model: BusinessModel; selected: string[]; reason: string; onCommitted: () => void }) {
  const { language, session } = model; const t = words[language]; const actor = session.user.id;
  const enabled = model.me?.capabilities?.client_ownership_v1 === true && session.user.role === "owner";
  const [mode, setMode] = useState<"selected" | "group">("selected"); const [sourceGroup, setSourceGroup] = useState(""); const [target, setTarget] = useState<Pick | null>(null); const [group, setGroup] = useState("");
  const [quote, setQuote] = useState<OwnershipQuote | null>(null); const [consent, setConsent] = useState(false); const [busy, setBusy] = useState(false); const busyRef = useRef(false);
  const [pending, setPending] = useState<PendingOwnership | null>(() => loadOwnershipPending(actor)); const pendingRef = useRef(pending);
  const [receipt, setReceipt] = useState<OwnershipReceipt | null>(null); const [notice, setNotice] = useState<"failed" | "changed" | "uncertain" | "storage" | "">("");
  const [refresh, setRefresh] = useState(0); const [refused, setRefused] = useState(false); const [absent, setAbsent] = useState(false); const [showAll, setShowAll] = useState(false);
  const alive = useRef(true); const intentGeneration = useRef(0); const completed = useRef("");
  const intent: OwnershipIntent = { client_ids: mode === "selected" ? [...selected].sort() : [], source_group_id: mode === "group" ? sourceGroup : "", representative_id: target?.id ?? "", group_id: group, reason: reason.trim() };
  const intentKey = JSON.stringify(intent);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => { intentGeneration.current++; setQuote(null); setConsent(false); setShowAll(false); }, [intentKey]);
  const accept = useRef<(v: OwnershipReceipt, request: PendingOwnership) => void>(() => {});
  accept.current = (v, request) => {
    if (!alive.current || pendingRef.current?.request.event_id !== request.request.event_id || completed.current === v.event_id) return;
    completed.current = v.event_id; clearOwnershipPending(request); pendingRef.current = null; setPending(null); setReceipt(v); setNotice(""); setRefused(false); setAbsent(false); setQuote(null); setConsent(false); onCommitted(); model.reload();
  };
  useEffect(() => {
    if (!pending || session.user.role !== "owner" || !enabled) return;
    const abort = new AbortController(); let timer: number | undefined;
    const check = async () => {
      try { const v = await findOwnershipReceipt(pending, abort.signal); if (!abort.signal.aborted) accept.current(v, pending); }
      catch (e) { if (abort.signal.aborted) return; setAbsent(e instanceof ApiError && e.status === 404); setNotice("uncertain"); timer = window.setTimeout(() => { void check(); }, 3000); }
    };
    void check(); return () => { abort.abort(); window.clearTimeout(timer); };
  }, [pending, enabled, refresh, session.user.role]);
  async function preview() {
    if (busyRef.current || pendingRef.current || !enabled || !validOwnershipIntent(intent)) return;
    const generation = intentGeneration.current; busyRef.current = true; setBusy(true); setNotice(""); setReceipt(null);
    try { const v = await quoteOwnership(intent, session.csrf_token); if (alive.current && generation === intentGeneration.current) { setQuote(v); setConsent(false); } }
    catch { if (alive.current && generation === intentGeneration.current) setNotice("failed"); }
    finally { if (alive.current) { busyRef.current = false; setBusy(false); } }
  }
  async function send(saved?: PendingOwnership) {
    if (busyRef.current || !enabled || (!saved && (!quote || !consent))) return;
    const q = saved ?? { version: 1 as const, actor_id: actor, request: { ...quote!.intent, event_id: "ownership-" + crypto.randomUUID(), quote_hash: quote!.quote_hash, quote_unix: quote!.quote_unix }, client_ids: quote!.clients.map(c => c.client_id) };
    if (!saveOwnershipPending(q)) { setNotice("storage"); return; }
    pendingRef.current = q; setPending(q); busyRef.current = true; setBusy(true); setNotice(""); setAbsent(false); setRefused(false);
    try { const v = await commitOwnership(q, session.csrf_token); accept.current(v, q); }
    catch (e) { if (alive.current && completed.current !== q.request.event_id) { setNotice(e instanceof ApiError && [400,409,422].includes(e.status) ? "changed" : "uncertain"); setRefused(e instanceof ApiError && [400,409,422].includes(e.status)); setRefresh(n => n + 1); } }
    finally { if (alive.current) { busyRef.current = false; setBusy(false); } }
  }
  function discardRefused() {
    if (!pending || !refused || !absent || busyRef.current) return;
    clearOwnershipPending(pending); pendingRef.current = null; setPending(null); setQuote(null); setConsent(false); setRefused(false); setAbsent(false); setNotice("");
  }
  if (session.user.role !== "owner") return null;
  const locked = busy || !!pending;
  return <section className="ownership-transfer" aria-label={t.title}>
    <header><ArrowRightLeft size={19} /><div><h4>{t.title}</h4><p>{t.intro}</p></div></header>
    <p className="ownership-safe"><ShieldCheck size={16} />{t.preserve}</p>
    <details className="ownership-funding"><summary>{language === "fa" ? "قاعدهٔ پرداخت و برگشت اعتبار" : "Funding and refund policy"}</summary><p>{t.funding}</p></details>
    {!enabled && <p role="status">{t.disabled}</p>}
    {notice && <p className="ownership-warning" role="status">{t[notice]}</p>}
    {receipt && <section className="ownership-receipt" role="status"><strong><Check size={17} />{t.success}</strong><p>{receipt.count} {t.applied} · <bdi>{receipt.representative_id}</bdi></p><code dir="ltr">{receipt.event_id}</code></section>}
    {pending ? <section className="ownership-pending" aria-label={t.pending}><h4>{t.pending}</h4><code dir="ltr">{pending.request.event_id}</code><p>{pending.client_ids.length} → <bdi>{pending.request.representative_id}</bdi></p><p>{t.retained}</p>
      <div className="ownership-actions"><button type="button" disabled={!enabled || busy} onClick={() => setRefresh(n => n + 1)}>{t.refresh}</button><button type="button" disabled={!enabled || busy} onClick={() => { void send(pending); }}>{t.retry}</button>
        {refused && absent && <button type="button" disabled={busy} onClick={discardRefused}>{t.reset}</button>}</div></section> : <>
      <div className="ownership-fields"><label>{t.mode}<select aria-label={t.mode} value={mode} disabled={locked || !enabled} onChange={e => setMode(e.target.value as "selected" | "group")}><option value="selected">{t.selected} ({selected.length})</option><option value="group">{t.group}</option></select></label>
        {mode === "group" && <GroupPicker language={language} label={t.sourceGroup} value={sourceGroup} disabled={locked || !enabled} optional={false} onChange={setSourceGroup} />}
      </div>
      {enabled && <RepresentativePicker language={language} value={target} disabled={locked} onChange={v => { setTarget(v); setGroup(""); }} />}
      {target && enabled && <GroupPicker key={target.id} language={language} representative={target.id} label={t.targetGroup} value={group} disabled={locked} optional={true} onChange={setGroup} />}
      <p className="ownership-hint">{t.maximum}</p>{!reason.trim() && <p>{t.reason}</p>}
      <button type="button" className="ownership-primary" disabled={!enabled || locked || !validOwnershipIntent(intent)} onClick={() => { void preview(); }}>{busy ? <LoaderCircle size={16} /> : <ArrowRightLeft size={16} />}{t.preview}</button>
      {quote && <section className="ownership-preview" aria-label={t.preview}><h4>{quote.count} → <bdi>{quote.intent.representative_id}</bdi></h4><p>{t.all}</p>
        <div className="ownership-members">{quote.clients.slice(0, showAll ? 500 : 20).map(item => <article key={item.client_id}><strong>{item.name}</strong><small><bdi>{item.client_id}</bdi></small><span><bdi>{item.previous_representative_id}</bdi> → <bdi>{item.representative_id}</bdi></span><Counter item={item} language={language} /></article>)}</div>
        {!showAll && quote.count > 20 && <button type="button" onClick={() => setShowAll(true)}>{t.show} ({quote.count})</button>}
        <label className="ownership-consent"><input type="checkbox" checked={consent} disabled={locked} onChange={e => setConsent(e.target.checked)} />{t.consent}</label>
        <div className="ownership-actions"><button type="button" className="ownership-primary" disabled={locked || !consent || !enabled} onClick={() => { void send(); }}>{t.confirm}</button><button type="button" disabled={locked} onClick={() => { setQuote(null); setConsent(false); }}>{t.close}</button></div>
      </section>}
    </>}
  </section>;
}
