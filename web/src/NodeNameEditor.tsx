import { useRef, useState, useEffect, type FormEvent } from "react";
import { api, mutation, ApiError, useSession, type Language } from "./session";
import "./NodeNameEditor.css";

type Receipt = { event_id: string; previous_name: string; name: string; actor_id: string; revision: number; at: string };
type NameState = { node: { node_id: string; installation_id: string; name: string }; revision: number; audit_count: number; recent: Receipt[] };
type Intent = { event_id: string; installation_id: string; expected_revision: number; name: string };
const words = {
  en: { open: "Manage display name", label: "New display name", save: "Save name", retry: "Retry same request", refresh: "Read current name", close: "Close name editor", loading: "Loading…", scope: "Display metadata only. No deletion, replacement or activation.", conflict: "Revision or event conflict. Read current name before making a new change.", unknown: "Outcome is unknown. Retry the same request, or read current name and audit before making another change.", invalid: "Name was rejected. Read current name and review your input.", unavailable: "Current name could not be read. Editing is locked.", saved: "Receipt confirmed; current state refreshed.", revision: "Revision", audit: "Recent name changes", empty: "No name changes recorded." },
  fa: { open: "مدیریت نام نمایشی", label: "نام نمایشی جدید", save: "ذخیرهٔ نام", retry: "تکرار همان درخواست", refresh: "دریافت نام فعلی", close: "بستن ویرایش نام", loading: "در حال دریافت…", scope: "فقط نام نمایشی؛ بدون حذف، جایگزینی یا فعال‌سازی.", conflict: "نسخه یا شناسهٔ درخواست تعارض دارد؛ ابتدا نام فعلی را دریافت کن.", unknown: "نتیجه قطعی نیست؛ همان درخواست را تکرار کن یا پیش از تغییر بعدی، نام فعلی و سابقه را بخوان.", invalid: "نام پذیرفته نشد؛ نام فعلی را دریافت و ورودی را بررسی کن.", unavailable: "نام فعلی قابل دریافت نیست؛ ویرایش قفل است.", saved: "رسید تأیید و وضعیت فعلی بازخوانی شد.", revision: "نسخه", audit: "تغییرات اخیر نام", empty: "تغییر نامی ثبت نشده است." },
};

export default function NodeNameEditor({ nodeID, language, onSaved }: { nodeID: string; language: Language; onSaved: () => void }) {
  const { session } = useSession();
  const t = words[language];
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<NameState | null>(null);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [intent, setIntent] = useState<Intent | null>(null);
  const [notice, setNotice] = useState<keyof typeof words.en | null>(null);
  const [retryable, setRetryable] = useState(false);
  const alive = useRef(true);
  const inFlight = useRef(false);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  const path = "/api/v1/workspace/nodes/" + encodeURIComponent(nodeID);
  async function read() {
    const next = await api<NameState>(path + "/name");
    if (next.node?.node_id !== nodeID || !/^[a-f0-9]{32}$/.test(next.node.installation_id) || !Number.isSafeInteger(next.revision) || next.revision < 1 || typeof next.node.name !== "string" || !Array.isArray(next.recent)) throw new Error("invalid_name_state");
    if (!alive.current) return;
    setState(next); setName(next.node.name); setIntent(null); setRetryable(false);
  }
  async function refresh() {
    if (inFlight.current) return;
    inFlight.current = true; setBusy(true); setNotice(null); setState(null);
    try { await read(); }
    catch { if (alive.current) setNotice("unavailable"); }
    finally { inFlight.current = false; if (alive.current) setBusy(false); }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!session || inFlight.current || (!intent && !state)) return;
    const request = intent ?? { event_id: crypto.randomUUID().replaceAll("-", ""), installation_id: state!.node.installation_id, expected_revision: state!.revision, name: name.trim() };
    inFlight.current = true; setBusy(true); setIntent(request); setNotice(null);
    let acknowledged = false;
    try {
      const result = await mutation<{receipt: Receipt}>(path + "/rename", request, session.csrf_token);
      if (result.receipt?.event_id !== request.event_id || result.receipt.name !== request.name || result.receipt.revision !== request.expected_revision + 1) throw new Error("invalid_receipt");
      acknowledged = true;
      await read();
      if (alive.current) { setNotice("saved"); onSaved(); }
    } catch (error) {
      if (!alive.current) return;
      setState(null);
      const rejected = !acknowledged && error instanceof ApiError && [400, 401, 403, 404, 409, 422].includes(error.status);
      setRetryable(!rejected);
      setNotice(rejected ? (error.status === 409 ? "conflict" : "invalid") : "unknown");
    } finally { inFlight.current = false; if (alive.current) setBusy(false); }
  }
  if (!open) return <button type="button" onClick={() => { setOpen(true); void refresh(); }}>{t.open}</button>;
  return <div className="pro-node-name-editor">
    <p>{t.scope}</p>
    {busy && <p role="status">{t.loading}</p>}
    {notice && <p role="status">{t[notice]}</p>}
    <form onSubmit={save}>
      <label>{t.label}<input value={name} onChange={e => setName(e.target.value)} disabled={busy || !state || !!intent} required /></label>
      <button type="submit" disabled={busy || (intent ? !retryable : !state || !name.trim() || name.trim() === state.node.name)}>{intent && retryable ? t.retry : t.save}</button>
    </form>
    <button type="button" disabled={busy} onClick={() => void refresh()}>{t.refresh}</button>
    <button type="button" disabled={busy || !!intent} onClick={() => { setOpen(false); setState(null); setNotice(null); }}>{t.close}</button>
    {state && <><p>{t.revision}: {state.revision}</p><h4>{t.audit}</h4>
      {state.recent.length === 0 ? <p>{t.empty}</p> : <ul>{[...state.recent].reverse().map(row => <li key={row.event_id}><bdi>{row.previous_name}</bdi> → <bdi>{row.name}</bdi> · <bdi>{row.actor_id}</bdi> · <time>{row.at}</time></li>)}</ul>}
    </>}
  </div>;
}
