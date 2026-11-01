import { useEffect, useRef, useState } from "react";
import { AlertCircle, RotateCcw } from "lucide-react";
import { api, mutation, useSession, type Language } from "./session";
import { backupBase, backupID, newBackupEvent } from "./nativeBackups";

export type RestoreSource = { source_kind: "archive" | "upload"; source_id: string; sha256: string };
type Request = RestoreSource & { event_id: string; confirmation: "RESTORE" };
type Job = RestoreSource & {
  id: string; event_id: string; actor_id: string; state: "queued" | "running" | "succeeded" | "failed" | "recovery_required";
  phase: "queued" | "validating" | "stopping_hub" | "applying" | "rolling_back" | "starting_restored" | "starting_original" | "complete";
  created_unix: number; finished_unix: number; revision: number; error_code: string; table_count: number; data_restored: boolean;
};
type Status = { enabled: boolean; jobs: Job[] };
const hash = /^[a-f0-9]{64}$/;
const event = /^[A-Za-z0-9_-]{16,96}$/;
const states = ["queued", "running", "succeeded", "failed", "recovery_required"];
const phases = ["queued", "validating", "stopping_hub", "applying", "rolling_back", "starting_restored", "starting_original", "complete"];
const obj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const int = (v: unknown): v is number => Number.isSafeInteger(v) && (v as number) >= 0;
function validSource(v: unknown): v is RestoreSource & Record<string, unknown> {
  return obj(v) && (v.source_kind === "archive" || v.source_kind === "upload") && typeof v.source_id === "string" && backupID.test(v.source_id) && typeof v.sha256 === "string" && hash.test(v.sha256);
}
function validRequest(v: unknown): v is Request {
  return validSource(v) && obj(v) && typeof v.event_id === "string" && event.test(v.event_id) && v.confirmation === "RESTORE" && Object.keys(v).length === 5;
}
function validJob(v: unknown): v is Job {
  if (!validSource(v) || !obj(v) || typeof v.id !== "string" || !backupID.test(v.id) || typeof v.event_id !== "string" || !event.test(v.event_id) ||
    typeof v.actor_id !== "string" || typeof v.state !== "string" || !states.includes(v.state) || typeof v.phase !== "string" || !phases.includes(v.phase) ||
    !int(v.created_unix) || !int(v.finished_unix) || !int(v.revision) || !int(v.table_count) || v.table_count > 1024 || typeof v.error_code !== "string" || typeof v.data_restored !== "boolean") return false;
  return v.state !== "succeeded" || v.data_restored && v.phase === "complete" && v.table_count > 0 && v.finished_unix > 0;
}
function readStatus(v: unknown): Status {
  if (!obj(v) || typeof v.enabled !== "boolean" || !Array.isArray(v.jobs) || v.jobs.length > 128 || !v.jobs.every(validJob) || new Set(v.jobs.map(j => j.id)).size !== v.jobs.length) throw new Error("invalid_restore_status");
  return v as Status;
}
function matches(job: Job, q: Request, actor: string): boolean {
  return job.actor_id === actor && job.event_id === q.event_id && job.source_kind === q.source_kind && job.source_id === q.source_id && job.sha256 === q.sha256;
}
const active = (j: Job) => j.state !== "succeeded" && j.state !== "failed";
const storageKey = (actor: string) => "dark-pro-restore-request-v1:" + actor;
function load(actor: string): Request | null {
  try { const v: unknown = JSON.parse(sessionStorage.getItem(storageKey(actor)) ?? "null"); return validRequest(v) ? v : null; } catch { return null; }
}
const words = {
  en: {
    title: "Restore a backup", warning: "This replaces the panel data with the selected snapshot, including clients, recorded usage and balances. Changes made after that snapshot will not be kept. The panel briefly disconnects.",
    compatible: "Only a native backup from this installation with matching credentials and settings is accepted. The complete file is decrypted and checked before the panel stops.",
    login: "After restoration, sign in using the owner credentials stored in that backup.", type: "Type RESTORE to confirm replacement", submit: "Restore panel data", cancel: "Keep current data", close: "Close", request: "Restore request saved", retry: "Retry the same request",
    uncertain: "The result is not confirmed. The original request is saved; refresh does not start another restore.", stale: "Connection interrupted. This is the last confirmed restore state; reconnect or sign in again to check the result.",
    storage: "The browser could not save the request. Nothing was sent.", disabled: "Panel restoration is not configured on this installation.", success: "Panel data restored and service health confirmed.", failed: "Restore was not applied. Current data was retained or restored before restart.",
    attention: "Recovery needs operator attention. Do not start a second restore or replace the database manually.", tableCount: "Verified data tables", history: "Restore status", queued: "Queued", running: "In progress", succeeded: "Restored", failedState: "Not applied", recovery_required: "Needs attention",
    phases: { queued: "Waiting", validating: "Checking the complete backup", stopping_hub: "Stopping panel writers", applying: "Replacing the verified data", rolling_back: "Recovering the original data", starting_restored: "Starting the restored panel", starting_original: "Starting the original panel", complete: "Finished" },
  },
  fa: {
    title: "بازیابی پشتیبان", warning: "اطلاعات پنل، شامل کاربران، مصرف ثبت‌شده و اعتبارها، با نسخهٔ انتخاب‌شده جایگزین می‌شود. تغییرات بعد از زمان آن بکاپ حفظ نخواهند شد. ارتباط پنل موقتاً قطع می‌شود.",
    compatible: "فقط بکاپ بومی همین نصب با کلیدها و تنظیمات سازگار پذیرفته می‌شود. تمام فایل پیش از توقف پنل رمزگشایی و بررسی می‌شود.",
    login: "پس از بازیابی، با اطلاعات ورود مالک که داخل همان بکاپ بوده وارد شو.", type: "برای تأیید جایگزینی، RESTORE را بنویس", submit: "بازیابی اطلاعات پنل", cancel: "حفظ اطلاعات فعلی", close: "بستن", request: "درخواست بازیابی ذخیره شده", retry: "تکرار همان درخواست",
    uncertain: "نتیجه هنوز تأیید نشده است. درخواست اصلی ذخیره شده؛ تازه‌سازی صفحه بازیابی دیگری آغاز نمی‌کند.", stale: "ارتباط قطع شده است. این آخرین وضعیت تأییدشده است؛ پس از اتصال یا ورود دوباره، نتیجه را بررسی کن.",
    storage: "مرورگر نتوانست درخواست را ذخیره کند؛ چیزی ارسال نشد.", disabled: "بازیابی داخل پنل در این نصب هنوز تنظیم نشده است.", success: "اطلاعات پنل بازیابی شد و سلامت سرویس تأیید شد.", failed: "بازیابی اعمال نشد؛ اطلاعات قبلی حفظ شد یا پیش از راه‌اندازی دوباره برگردانده شد.",
    attention: "بازیابی به بررسی اپراتور نیاز دارد. بازیابی دوم آغاز نکن و دیتابیس را دستی جایگزین نکن.", tableCount: "جدول‌های بررسی‌شده", history: "وضعیت بازیابی", queued: "در صف", running: "در حال اجرا", succeeded: "بازیابی شد", failedState: "اعمال نشد", recovery_required: "نیازمند بررسی",
    phases: { queued: "در انتظار", validating: "بررسی کامل بکاپ", stopping_hub: "توقف نوشتن در پنل", applying: "جایگزینی دادهٔ بررسی‌شده", rolling_back: "بازگرداندن دادهٔ قبلی", starting_restored: "راه‌اندازی پنل بازیابی‌شده", starting_original: "راه‌اندازی پنل قبلی", complete: "پایان" },
  },
} as const;

export function NativeBackupRestore({ language, source, onClose }: { language: Language; source: RestoreSource | null; onClose: () => void }) {
  const { session } = useSession();
  const actor = session?.user.id ?? "";
  const t = words[language];
  const [status, setStatus] = useState<Status | null>(null);
  const [connected, setConnected] = useState(false);
  const [pending, setPending] = useState<Request | null>(() => load(actor));
  const pendingRef = useRef(pending);
  const [confirm, setConfirm] = useState("");
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const [notice, setNotice] = useState<"uncertain" | "storage" | "">("");
  const [refresh, setRefresh] = useState(0);
  const dialog = useRef<HTMLDialogElement>(null);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => {
    setConfirm("");
    if (source && !dialog.current?.open) dialog.current?.showModal();
    if (!source && dialog.current?.open) dialog.current?.close();
  }, [source]);
  useEffect(() => {
    if (!session || session.user.role !== "owner") return;
    let stopped = false, timer: number | undefined;
    let abort: AbortController | undefined;
    const poll = async () => {
      abort = new AbortController();
      try {
        const next = readStatus(await api<unknown>(backupBase + "/restores", { signal: abort.signal }));
        if (stopped) return;
        setStatus(next); setConnected(true);
        const saved = pendingRef.current;
        if (saved && next.jobs.some(j => matches(j, saved, actor))) {
          sessionStorage.removeItem(storageKey(actor)); pendingRef.current = null; setPending(null); setNotice("");
        }
      } catch { if (!stopped) setConnected(false); }
      if (!stopped) timer = window.setTimeout(() => { void poll(); }, 2000);
    };
    void poll();
    return () => { stopped = true; abort?.abort(); window.clearTimeout(timer); };
  }, [actor, session?.user.role, refresh]);
  async function send(q: Request) {
    if (!session || session.user.role !== "owner" || busyRef.current) return;
    busyRef.current = true; setBusy(true); setNotice("");
    try {
      sessionStorage.setItem(storageKey(actor), JSON.stringify(q));
      if (sessionStorage.getItem(storageKey(actor)) !== JSON.stringify(q)) throw new Error("storage");
    } catch { setNotice("storage"); busyRef.current = false; setBusy(false); return; }
    pendingRef.current = q; setPending(q);
    try {
      const v = await mutation<unknown>(backupBase + "/restores", q, session.csrf_token);
      if (!obj(v) || !validJob(v.job) || typeof v.replayed !== "boolean" || !matches(v.job, q, actor)) throw new Error("unconfirmed_restore");
      if (!alive.current) return;
      // Keep the same persisted ID until a confirmed status read reconciles it.
      setStatus(old => ({ enabled: old?.enabled ?? true, jobs: [v.job as Job, ...(old?.jobs ?? []).filter(j => j.id !== (v.job as Job).id)] }));
      onClose(); setRefresh(v => v + 1);
    } catch { if (alive.current) { setNotice("uncertain"); setRefresh(v => v + 1); } }
    finally { busyRef.current = false; if (alive.current) setBusy(false); }
  }
  if (!session || session.user.role !== "owner") return null;
  const latest = status?.jobs[0];
  const running = status?.jobs.some(active) ?? false;
  return <section className="backup-restore" aria-label={t.history}>
    {notice && <p className="backup-message backup-warning" role="status">{t[notice]}</p>}
    {pending && <div className="backup-pending"><div><strong>{t.request}</strong><p>{t.uncertain}</p><bdi>{pending.event_id}</bdi></div>
      <button type="button" className="backup-button backup-secondary" disabled={!connected || busy || !status?.enabled} onClick={() => { void send(pending); }}>{t.retry}</button></div>}
    {latest && <section className="backup-operation" aria-label={t.history}><header><h3>{t.phases[latest.phase]}</h3><span>{latest.state === "failed" ? t.failedState : t[latest.state]}</span></header>
      <p role="status">{!connected ? t.stale : latest.state === "succeeded" ? t.success : latest.state === "failed" ? t.failed : latest.state === "recovery_required" ? t.attention : t.phases[latest.phase]}</p>
      {latest.state === "succeeded" && <p>{t.tableCount}: {latest.table_count.toLocaleString(language === "fa" ? "fa-IR" : "en")}</p>}
      <p><bdi>{latest.event_id}</bdi></p></section>}
    <dialog ref={dialog} className="backup-restore-dialog" aria-label={t.title} onCancel={event => { event.preventDefault(); if (!busy) onClose(); }}>
      <h3><RotateCcw size={20} aria-hidden="true" /> {t.title}</h3>
      <p className="backup-warning"><AlertCircle size={18} aria-hidden="true" /> {t.warning}</p><p>{t.compatible}</p><p>{t.login}</p>
      {source && <dl><dt>SHA-256</dt><dd><bdi>{source.sha256}</bdi></dd></dl>}
      <label>{t.type}<input value={confirm} maxLength={7} autoComplete="off" dir="ltr" disabled={busy || !!pending} onChange={event => setConfirm(event.target.value)} /></label>
      {!status?.enabled && <p role="status">{t.disabled}</p>}
      <div className="backup-upload-actions"><button type="button" className="backup-button" disabled={confirm !== "RESTORE" || busy || !!pending || running || !connected || !status?.enabled || !source} onClick={() => { if (source) void send({ ...source, event_id: newBackupEvent(), confirmation: "RESTORE" }); }}>{t.submit}</button>
        <button type="button" className="backup-button backup-secondary" disabled={busy} onClick={onClose}>{pending ? t.close : t.cancel}</button></div>
    </dialog>
  </section>;
}
