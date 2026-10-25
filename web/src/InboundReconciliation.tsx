import { useEffect, useRef, useState } from "react";
import WorkspaceDialog from "./WorkspaceDialog";
import { useSession } from "./session";
import { creationNodeName } from "./InboundCreationModel";
import type { InboundNode } from "./inboundTypes";
import { executionAPI, executionPath, readExecutionReconciliationInput, readExecutionReconciliationPreview, readExecutionReconciliationReceipt,
  type ExecutionKnownUsage, type ExecutionReconciliation, type ExecutionReconciliationInput, type ExecutionReconciliationPreview, type ExecutionReconciliationReceipt } from "./inboundExecution";
import { executionBlockerLabel } from "./inboundExecutionWords";
import { subscriptionRequestRejected } from "./inboundSubscriptions";

const words = {
  en: {
    title: "Uncertain usage", help: "A final usage report is missing for this runtime. Measured usage is preserved; the unmeasured remainder has no reliable amount.",
    review: "Review recovery", reason: "Reason for accepting owner loss", reasonHelp: "Explain why the missing final report cannot be recovered (up to 512 characters).",
    preview: "Review owner loss", confirm: "Accept owner loss", cancel: "Cancel", measured: "Recorded usage", noMeasured: "No measured report was accepted. The unknown amount is not zero.",
    unknown: "Unmeasured usage", unmeasured: "Amount unknown", client: "Client / attribution", upload: "Upload (bytes)", download: "Download (bytes)", hold: "Hold at review", held: "Held", recorded: "Recorded",
    affected: "Affected clients", siblings: "Affected Inbounds", ack: "I accept the unmeasured remainder as owner loss. All measured bytes remain recorded and charged according to their existing attribution.",
    scope: "This decision applies to the complete interrupted runtime, including the listed clients and other Inbounds on this target.",
    result: "The owner-loss decision is recorded. Measured usage is retained. Starting the runtime requires a separate reviewed action.",
    changed: "The usage evidence changed. Review the current evidence again before confirming.",
    failed: "The review could not be verified. Refresh the execution state and review again.",
    rejected: "The request was rejected. Refresh and review the current evidence before another decision.",
    uncertain: "The response was interrupted. The decision may have been recorded. Check its receipt or retry the exact request.",
    check: "Check decision receipt", retry: "Retry exact decision", discard: "Discard rejected decision", reset: "Reset invalid local decision", storage: "The decision could not be retained for an exact retry. Check browser storage before continuing.",
    details: "Technical details", epoch: "Runtime epoch", revision: "Accepted report sequence", owner_aggregate: "Owner total", transit: "Transit (not billable)", probe: "Verification probe (not billable)", more: "More rows are available in technical details.",
  },
  fa: {
    title: "مصرف نامشخص", help: "گزارش نهایی مصرف این اجرا دریافت نشده است. مصرف اندازه‌گیری‌شده حفظ می‌شود؛ مقدار بخش اندازه‌گیری‌نشده قابل تأیید نیست.",
    review: "بازبینی بازیابی", reason: "دلیل پذیرش زیان مالک", reasonHelp: "توضیح دهید چرا گزارش نهایی قابل بازیابی نیست؛ حداکثر ۵۱۲ نویسه.",
    preview: "بازبینی زیان مالک", confirm: "پذیرش زیان مالک", cancel: "انصراف", measured: "مصرف ثبت‌شده", noMeasured: "هیچ گزارش اندازه‌گیری پذیرفته نشده است. مقدار نامشخص برابر صفر نیست.",
    unknown: "مصرف اندازه‌گیری‌نشده", unmeasured: "مقدار نامشخص", client: "کلاینت / نوع انتساب", upload: "آپلود (بایت)", download: "دانلود (بایت)", hold: "وضعیت هنگام بازبینی", held: "در انتظار تعیین تکلیف", recorded: "ثبت‌شده",
    affected: "کلاینت‌های مشمول", siblings: "اینباندهای مشمول", ack: "بخش اندازه‌گیری‌نشده را به‌عنوان زیان مالک می‌پذیرم. همهٔ بایت‌های اندازه‌گیری‌شده با انتساب قبلی ثبت و محاسبه می‌شوند.",
    scope: "این تصمیم کل اجرای متوقف‌شده، شامل کلاینت‌ها و اینباندهای فهرست‌شده روی این مقصد را پوشش می‌دهد.",
    result: "پذیرش زیان مالک ثبت شد. مصرف اندازه‌گیری‌شده حفظ شده است؛ شروع اجرا به اقدام جداگانه و بازبینی نیاز دارد.",
    changed: "شواهد مصرف تغییر کرده است. پیش از تأیید، وضعیت فعلی را دوباره بازبینی کنید.",
    failed: "نتیجهٔ بازبینی تأیید نشد. وضعیت اجرا را تازه و دوباره بازبینی کنید.",
    rejected: "درخواست رد شد. پیش از تصمیم تازه، وضعیت و شواهد فعلی را بازبینی کنید.",
    uncertain: "پاسخ قطع شد و ممکن است تصمیم ثبت شده باشد. رسید را بررسی یا همان درخواست را دوباره ارسال کنید.",
    check: "بررسی رسید تصمیم", retry: "ارسال دوبارهٔ همان تصمیم", discard: "کنارگذاشتن تصمیم ردشده", reset: "بازنشانی تصمیم محلی نامعتبر", storage: "امکان نگهداری درخواست برای تکرار دقیق وجود ندارد. پیش از ادامه، ذخیره‌سازی مرورگر را بررسی کنید.",
    details: "جزئیات فنی", epoch: "دورهٔ اجرا", revision: "شمارهٔ گزارش پذیرفته‌شده", owner_aggregate: "مجموع مالک", transit: "ترانزیت؛ بدون صورتحساب", probe: "آزمون اتصال؛ بدون صورتحساب", more: "ردیف‌های بیشتر در جزئیات فنی موجود است.",
  },
};
type Props = { inboundID: number; nodes: InboundNode[]; language: "fa" | "en"; entries: ExecutionReconciliation[]; disabled: boolean;
  onChanged: () => void; onBusyChange: (busy: boolean) => void; onDirtyChange: (dirty: boolean) => void };
const evidence = (entry: ExecutionReconciliation | undefined) => entry ? JSON.stringify(entry) : "";

export default function InboundReconciliation(props: Props) {
  const { inboundID, nodes, language, entries, disabled } = props, t = words[language], { session } = useSession();
  const key = `dark-pro-inbound-reconciliation:${session?.user.id ?? ""}:${inboundID}`, base = executionPath(inboundID) + "/reconciliations";
  const callbacks = useRef(props); callbacks.current = props;
  const latest = useRef(entries); latest.current = entries;
  const lifetime = useRef<AbortController | null>(null), sending = useRef(false);
  const [open, setOpen] = useState<string | null>(null), [reason, setReason] = useState(""), [ack, setAck] = useState(false), [busy, setBusy] = useState(false);
  const [review, setReview] = useState<ExecutionReconciliationPreview | null>(null), reviewed = useRef<ExecutionReconciliationInput | null>(null);
  const [attempt, setAttempt] = useState<string | null>(null), [rejected, setRejected] = useState(false), [notice, setNotice] = useState("");
  const [storageFailed, setStorageFailed] = useState(false), [receipt, setReceipt] = useState<ExecutionReconciliationReceipt | null>(null);
  const selected = entries.find(entry => entry.epoch_id === open);
  const validReason = !!reason.trim() && [...reason.trim()].length <= 512 && !/[\u0000-\u001f\u007f-\u009f]/.test(reason.trim());
  useEffect(() => {
    const c = new AbortController(); lifetime.current = c; setOpen(null); setReview(null); reviewed.current = null; setAck(false); setReceipt(null); setRejected(false); setNotice(""); sending.current = false; setBusy(false);
    try {
      const raw = sessionStorage.getItem(key);
      if (raw) {
        if (raw.length > 4096) throw Error("retained_decision");
        const retained = JSON.parse(raw);
        if (retained.inbound_id !== inboundID || typeof retained.body !== "string" || Object.keys(retained).length !== 2) throw Error("retained_scope");
        readExecutionReconciliationInput(JSON.parse(retained.body), true); setAttempt(retained.body);
      } else setAttempt(null);
      setStorageFailed(false);
    } catch { setStorageFailed(true); setAttempt(null); }
    return () => { c.abort(); if (lifetime.current === c) lifetime.current = null; callbacks.current.onBusyChange(false); callbacks.current.onDirtyChange(false); };
  }, [key, inboundID, session?.csrf_token, session?.user.role]);
  useEffect(() => { callbacks.current.onBusyChange(busy); }, [busy]);
  useEffect(() => { callbacks.current.onDirtyChange(!!open || !!attempt); }, [open, attempt]);
  useEffect(() => {
    if (review && evidence(selected) !== evidence(review.state)) { setReview(null); reviewed.current = null; setAck(false); setNotice(t.changed); }
  }, [evidence(selected)]);
  const authorized = (c: AbortController) => lifetime.current === c && !c.signal.aborted && session?.user.role === "owner";
  const remember = (body: string | null) => {
    try { if (body) sessionStorage.setItem(key, JSON.stringify({ inbound_id: inboundID, body })); else sessionStorage.removeItem(key); setAttempt(body); setStorageFailed(false); return true; }
    catch { setStorageFailed(true); setNotice(t.storage); return false; }
  };
  const finish = (result: ExecutionReconciliationReceipt) => {
    if (!remember(null)) return;
    setReceipt(result); setOpen(null); setReview(null); reviewed.current = null; setReason(""); setAck(false); setRejected(false); setNotice(t.result); callbacks.current.onChanged();
  };
  const lookup = async () => {
    const c = lifetime.current; if (!c || !authorized(c) || sending.current || !attempt) return;
    const input = readExecutionReconciliationInput(JSON.parse(attempt), true); sending.current = true; setBusy(true);
    try { const result = readExecutionReconciliationReceipt(await executionAPI<unknown>(base + "/" + encodeURIComponent(input.event_id), c.signal), inboundID, session!.user.id, input); if (authorized(c)) finish(result); }
    catch { if (authorized(c)) setNotice(t.uncertain); }
    finally { if (lifetime.current === c) { sending.current = false; setBusy(false); } }
  };
  const preview = async () => {
    const c = lifetime.current; if (!c || !authorized(c) || disabled || sending.current || attempt || !selected || !validReason || storageFailed) return;
    const input: ExecutionReconciliationInput = { event_id: "reconcile-" + crypto.randomUUID(), epoch_id: selected.epoch_id, expected_revision: selected.revision, action: "accept_owner_loss", reason: reason.trim() };
    sending.current = true; setBusy(true); setNotice(""); setReview(null); reviewed.current = null; setAck(false);
    try {
      const result = readExecutionReconciliationPreview(await executionAPI<unknown>(base + "/preview", c.signal, JSON.stringify(input), session!.csrf_token), inboundID, input);
      if (!authorized(c)) return;
      if (evidence(latest.current.find(entry => entry.epoch_id === input.epoch_id)) !== evidence(result.state)) throw Error("changed_evidence");
      setReview(result); reviewed.current = { ...input, review_sha: result.review_sha };
    } catch { if (authorized(c)) setNotice(t.failed); }
    finally { if (lifetime.current === c) { sending.current = false; setBusy(false); } }
  };
  const send = async (body: string) => {
    const c = lifetime.current; if (!c || !authorized(c) || sending.current || storageFailed) return;
    const input = readExecutionReconciliationInput(JSON.parse(body), true); sending.current = true; setBusy(true); setRejected(false); setNotice("");
    try {
      const result = readExecutionReconciliationReceipt(await executionAPI<unknown>(base, c.signal, body, session!.csrf_token), inboundID, session!.user.id, input);
      if (authorized(c)) finish(result);
    } catch (error) { if (authorized(c)) { setRejected(subscriptionRequestRejected(error)); setNotice(subscriptionRequestRejected(error) ? t.rejected : t.uncertain); setOpen(null); setReview(null); reviewed.current = null; setAck(false); callbacks.current.onChanged(); } }
    finally { if (lifetime.current === c) { sending.current = false; setBusy(false); } }
  };
  const confirm = () => {
    if (!selected || disabled || busy || !ack || !review?.valid || !reviewed.current || attempt || evidence(selected) !== evidence(review.state)) return;
    const body = JSON.stringify(readExecutionReconciliationInput(reviewed.current, true));
    if (remember(body)) void send(body);
  };
  const usage = (rows: ExecutionKnownUsage[], measured: boolean) => <div className="managed-reconciliation-usage">
    <h4>{t.measured}</h4>{!measured ? <p>{t.noMeasured}</p> : <div className="managed-table-scroll"><table><thead><tr><th>{t.client}</th><th>{t.upload}</th><th>{t.download}</th><th>{t.hold}</th></tr></thead><tbody>{rows.slice(0, 100).map(row => <tr key={row.principal_id}><td><bdi>{row.client_id || (row.role === "owner_aggregate" ? t.owner_aggregate : row.role === "transit" ? t.transit : row.role === "probe" ? t.probe : row.role)}</bdi><small>#{row.inbound_id}</small></td><td><bdi>{BigInt(row.upload).toLocaleString(language === "fa" ? "fa-IR" : "en-US")}</bdi></td><td><bdi>{BigInt(row.download).toLocaleString(language === "fa" ? "fa-IR" : "en-US")}</bdi></td><td>{row.held ? t.held : t.recorded}</td></tr>)}</tbody></table></div>}
    {rows.length > 100 && <details><summary>{t.more}</summary><pre dir="ltr">{JSON.stringify(rows, null, 2)}</pre></details>}
  </div>;
  if (session?.user.role !== "owner" || !entries.length && !attempt && !receipt && !storageFailed) return null;
  return <section className="managed-reconciliation" aria-label={t.title}>
    <h3>{t.title}</h3><p>{t.help}</p>
    {entries.map(entry => <article key={entry.epoch_id} data-reconciliation-epoch={entry.epoch_id}>
      <h4>{creationNodeName(nodes, entry.node_id)}</h4><p>{t.unknown}: <strong>{t.unmeasured}</strong></p>
      <p>{t.affected}: {entry.affected_client_ids.length} · {t.siblings}: {entry.affected_inbound_ids.map(id => `#${id}`).join(" · ")}</p>
      {entry.blockers.length > 0 && <ul>{entry.blockers.map((blocker, i) => <li key={i}>{executionBlockerLabel(blocker, language)}</li>)}</ul>}
      <button type="button" disabled={disabled || busy || !!attempt || storageFailed || !entry.allowed_actions.includes("accept_owner_loss") || !!entry.blockers.length} onClick={() => { setOpen(entry.epoch_id); setReason(""); setReview(null); reviewed.current = null; setAck(false); setNotice(""); }}>{t.review}</button>
      <details className="managed-diagnostics"><summary>{t.details}</summary><p>{t.epoch}: <bdi>{entry.epoch_id}</bdi></p><p>{t.revision}: <bdi>{entry.revision}</bdi></p><p>Configuration SHA: <bdi>{entry.config_sha}</bdi></p><p>Manifest SHA: <bdi>{entry.manifest_sha}</bdi></p><p>Process SHA: <bdi>{entry.process_identity_sha}</bdi></p></details>
    </article>)}
    {notice && <p role="status">{notice}</p>}
    {attempt && !notice && <p role="status">{t.uncertain}</p>}
    {storageFailed && <div role="alert"><p>{t.storage}</p>{!attempt && <button type="button" disabled={busy} onClick={() => { if (remember(null)) setNotice(""); }}>{t.reset}</button>}</div>}
    {attempt && <div className="native-actions">{rejected ? <button type="button" disabled={busy} onClick={() => { if (remember(null)) { setOpen(null); setReview(null); reviewed.current = null; setAck(false); setRejected(false); setNotice(""); callbacks.current.onChanged(); } }}>{t.discard}</button> : <><button type="button" disabled={busy} onClick={() => void lookup()}>{t.check}</button><button type="button" disabled={busy || storageFailed} onClick={() => void send(attempt)}>{t.retry}</button></>}</div>}
    {receipt && usage(receipt.known_usage, receipt.has_measured_usage)}
    {open && <WorkspaceDialog open title={t.review} closeLabel={t.cancel} busy={busy} onClose={() => { setOpen(null); setReview(null); reviewed.current = null; setAck(false); }}>
      <div className="managed-review managed-reconciliation-review"><p>{t.scope}</p>{selected ? <>
        <p>{creationNodeName(nodes, selected.node_id)} · {t.siblings}: {selected.affected_inbound_ids.map(id => `#${id}`).join(" · ")}</p>
        <p>{t.affected}: <bdi>{selected.affected_client_ids.join(" · ") || "—"}</bdi></p>
        {usage(selected.known_usage, selected.has_measured_usage)}<p>{t.unknown}: <strong>{t.unmeasured}</strong></p>
        <label className="managed-reason">{t.reason}<input aria-label={t.reason} aria-describedby={`managed-reason-help-${inboundID}`} data-initial-focus value={reason} maxLength={512} disabled={busy || !!attempt} onChange={event => { setReason(event.target.value); setReview(null); reviewed.current = null; setAck(false); }}/><small id={`managed-reason-help-${inboundID}`}>{t.reasonHelp}</small></label>
        <button type="button" disabled={disabled || busy || !validReason || !!attempt || storageFailed} onClick={() => void preview()}>{t.preview}</button>
        {review && <>{review.blockers.length > 0 && <ul>{review.blockers.map((blocker, i) => <li key={i}>{executionBlockerLabel(blocker, language)}</li>)}</ul>}<label className="managed-owner-loss-ack"><input type="checkbox" checked={ack} disabled={busy || !review.valid || !!attempt} onChange={event => setAck(event.target.checked)}/><span>{t.ack}</span></label></>}
      </> : <p role="alert">{t.changed}</p>}
      {notice && <p role="status">{notice}</p>}
      <div className="native-actions"><button type="button" className="managed-restrictive" disabled={disabled || busy || !ack || !review?.valid || !!attempt} onClick={confirm}>{t.confirm}</button><button type="button" disabled={busy} onClick={() => { setOpen(null); setReview(null); reviewed.current = null; setAck(false); }}>{t.cancel}</button></div></div>
    </WorkspaceDialog>}
  </section>;
}
