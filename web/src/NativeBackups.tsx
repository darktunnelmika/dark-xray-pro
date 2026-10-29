import { useEffect, useRef, useState, type FormEvent } from "react";
import { AlertCircle, Archive, CalendarClock, Check, Clock3, DatabaseBackup, Download, LoaderCircle, RefreshCw, ShieldCheck, Square } from "lucide-react";
import { ApiError, useSession, type Language } from "./session";
import {
  archiveDownloadURL, cancellable, clearPending, draftFrom, getBackupStatus, jobActive, loadPending, newBackupEvent,
  policyFrom, resolvedPending, savePending, sendBackupRequest,
  type BackupArchive, type BackupDraft, type BackupJob, type BackupSettings, type BackupStatus, type PendingBackupRequest,
} from "./nativeBackups";
import "./NativeBackups.css";

const words = {
  en: {
    label: "Native backups", eyebrow: "Your recovery copies", title: "Keep a recoverable copy.",
    intro: "Encrypted copies of your clients, balances and recorded history.",
    create: "Create backup", creating: "Recording request…", refresh: "Refresh status", loading: "Loading backup status…",
    unavailable: "Backup status is unavailable. Try refreshing in a moment.",
    stale: "Connection interrupted. The last confirmed status is shown; it will refresh automatically.",
    staleBackup: "The panel may be offline for this backup. This is the last confirmed status. Progress and cancellation become available when the panel reconnects.",
    offline: "Creating an offline copy temporarily disconnects this panel. Downtime depends on data size and disk speed; status refreshes automatically after reconnection.",
    disabled: "Panel backups are not configured yet.", disabledDetail: "The backup service and an encryption recipient must be configured before you can create or schedule copies.",
    ready: "Ready", busy: "Backup in progress", attention: "Recovery needs attention", runnerUnavailable: "Service unavailable",
    lastBackup: "Latest archive", noBackup: "No backup yet", nextBackup: "Next scheduled run", scheduleOff: "Schedule is off", nextUnknown: "Waiting for the next run time",
    count: "Stored archives", activity: "Latest operation", manual: "Manual", scheduled: "Scheduled",
    requestID: "Request ID", started: "Started", finished: "Finished", cancel: "Request cancellation", cancelling: "Cancellation requested",
    cancelNote: "Cancellation can be received while the panel is connected. The service finishes any necessary recovery before stopping the operation.",
    recovery: "The panel service needs recovery. A new backup cannot start until this is resolved.",
    failed: "The backup did not finish. Review the service status before trying again.",
    downloadBusy: "An archive download was still active, so this attempt ended before the panel went offline. After the download finishes, choose Create backup to start a new attempt.",
    publicationUnknown: "The service could not confirm the saved archive. A file may exist, but the outcome of this attempt is uncertain. Have this operation checked before relying on it.",
    retentionIncomplete: "The new archive was recorded, but removal of older copies did not finish. More copies may remain than your retention settings allow.",
    cancelledTooLate: "Cancellation arrived after the archive was created. The completed archive was kept.",
    recoveredOperation: "The interrupted operation was recovered and its archive was recorded.",
    warning: "The service recorded a follow-up item that needs attention.",
    pendingTitle: "A request is awaiting confirmation.", pending: "Its original request ID is saved in this tab. Refreshing this page does not start another operation.",
    retry: "Retry same request", pendingSettings: "These settings are awaiting a confirmed receipt. Retrying sends exactly the same changes.",
    uncertain: "A response was not confirmed. Check the current status or retry the saved request.",
    recorded: "Backup request recorded.", recovered: "The previous request was found in the recorded status.",
    cancelRecorded: "Cancellation request recorded.", alreadyFinished: "This operation has already finished.",
    saved: "Backup settings saved.", permission: "This action is no longer permitted for your account.",
    invalid: "Check the schedule and retention values before saving.",
    storage: "The browser could not save the recovery record. No request was sent.",
    conflict: "Settings changed elsewhere. Your edits are still here.",
    currentSettings: "Currently saved", saveEdits: "Save my edits", useSaved: "Use saved settings",
    scheduleTitle: "Automatic backups", scheduleIntro: "Choose when to create a copy. Times follow the selected time zone.",
    enabled: "Enable scheduled backups", frequency: "Repeat", daily: "Every day", weekly: "Every week", time: "Time",
    timezone: "Time zone", weekday: "Day", retentionTitle: "Keep useful history", retentionIntro: "Retention is applied after a new archive is successfully recorded.",
    keepLast: "Keep latest copies", maxAge: "Maximum age (days)", keepHint: "1–100 copies", ageHint: "1–365 days",
    protectedHint: "Protected archives are retained and may exceed the configured count.", save: "Save settings", saving: "Saving…", unsaved: "Unsaved changes",
    archivesTitle: "Your archives", archivesIntro: "Download an encrypted file for safekeeping.",
    empty: "Your first completed backup will appear here.", download: "Download archive", downloadUnavailable: "Download unavailable",
    encryptedSize: "Encrypted size", payload: "Contents", protected: "Protected", checksum: "Checksum verified",
    restoreVerified: "Restore verified", checksumDetail: "The stored file checksum is verified. Decryption and recovery compatibility have not been verified for this copy.",
    restoreDetail: "Recovery verification is recorded for this copy.", fingerprint: "Encryption recipient fingerprint",
    noActivation: "Uploading or restoring an archive is not available from this page yet.",
    details: "Archive details", sha: "SHA-256", source: "Source version", schema: "Schema",
    states: { queued: "Queued", running: "In progress", cancelling: "Cancelling safely", succeeded: "Completed", failed: "Failed", cancelled: "Cancelled", recovery_required: "Recovery required" },
    phases: { queued: "Waiting to start", preflight: "Checking readiness", stopping_hub: "Preparing the offline copy", creating_offline: "Creating the encrypted copy", restarting_hub: "Bringing the panel back", checking_archive: "Checking the stored archive", retention: "Applying retention", complete: "Finished" },
    days: ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"],
  },
  fa: {
    label: "پشتیبان‌های بومی", eyebrow: "نسخه‌های بازیابی اطلاعات", title: "یک نسخه برای بازیابی نگه دار.",
    intro: "نسخهٔ رمزگذاری‌شده از کلاینت‌ها، موجودی و سوابق ثبت‌شده.",
    create: "ساخت پشتیبان", creating: "در حال ثبت درخواست…", refresh: "به‌روزرسانی وضعیت", loading: "در حال دریافت وضعیت پشتیبان‌ها…",
    unavailable: "وضعیت پشتیبان‌ها در دسترس نیست. کمی بعد دوباره بررسی کن.",
    stale: "ارتباط قطع شده است. آخرین وضعیت تأییدشده نمایش داده می‌شود و خودکار به‌روز خواهد شد.",
    staleBackup: "ممکن است پنل برای ساخت این پشتیبان آفلاین شده باشد. این آخرین وضعیت تأییدشده است. دریافت پیشرفت و درخواست لغو پس از اتصال دوبارهٔ پنل ممکن می‌شود.",
    offline: "ساخت نسخهٔ آفلاین پنل را موقتاً قطع می‌کند. مدت آن به حجم داده و سرعت دیسک بستگی دارد؛ پس از اتصال دوباره، وضعیت خودکار به‌روز می‌شود.",
    disabled: "پشتیبان‌گیری پنل هنوز تنظیم نشده است.", disabledDetail: "برای ساخت یا زمان‌بندی نسخه‌ها، سرویس پشتیبان و گیرندهٔ رمزگذاری باید تنظیم شوند.",
    ready: "آماده", busy: "پشتیبان در حال ساخت", attention: "نیاز به بررسی بازیابی", runnerUnavailable: "سرویس در دسترس نیست",
    lastBackup: "آخرین آرشیو", noBackup: "هنوز پشتیبانی ساخته نشده", nextBackup: "اجرای زمان‌بندی‌شدهٔ بعدی", scheduleOff: "زمان‌بندی خاموش است", nextUnknown: "در انتظار زمان اجرای بعدی",
    count: "آرشیوهای موجود", activity: "آخرین عملیات", manual: "دستی", scheduled: "زمان‌بندی‌شده",
    requestID: "شناسهٔ درخواست", started: "شروع", finished: "پایان", cancel: "درخواست لغو", cancelling: "لغو درخواست شده",
    cancelNote: "درخواست لغو هنگام اتصال پنل دریافت می‌شود. سرویس پیش از توقف عملیات، بازیابی لازم را کامل می‌کند.",
    recovery: "سرویس پنل به بازیابی نیاز دارد. تا رفع این وضعیت، پشتیبان تازه‌ای آغاز نمی‌شود.",
    failed: "ساخت پشتیبان کامل نشد. پیش از تلاش دوباره، وضعیت سرویس را بررسی کن.",
    downloadBusy: "دریافت یک آرشیو هنوز ادامه داشت؛ این تلاش پیش از آفلاین‌شدن پنل پایان یافت. پس از پایان دریافت، با «ساخت پشتیبان» تلاش تازه‌ای آغاز کن.",
    publicationUnknown: "سرویس نتوانست ذخیرهٔ نهایی آرشیو را تأیید کند. ممکن است فایلی ساخته شده باشد، اما نتیجهٔ این تلاش نامشخص است. پیش از اتکا به آن، وضعیت عملیات باید بررسی شود.",
    retentionIncomplete: "آرشیو تازه ثبت شد، اما پاک‌سازی نسخه‌های قدیمی کامل نشد. ممکن است نسخه‌های بیشتری از حد تنظیم‌شده باقی مانده باشد.",
    cancelledTooLate: "درخواست لغو پس از ساخته‌شدن آرشیو رسید. آرشیو کامل‌شده نگه داشته شد.",
    recoveredOperation: "عملیات متوقف‌شده بازیابی شد و آرشیو آن ثبت شد.",
    warning: "سرویس یک مورد نیازمند بررسی تکمیلی ثبت کرده است.",
    pendingTitle: "یک درخواست منتظر تأیید نتیجه است.", pending: "شناسهٔ اصلی درخواست در این زبانه نگه داشته شده است. تازه‌سازی صفحه عملیات دیگری آغاز نمی‌کند.",
    retry: "تکرار همان درخواست", pendingSettings: "این تنظیمات منتظر رسید تأییدشده‌اند. تلاش دوباره دقیقاً همین تغییرات را می‌فرستد.",
    uncertain: "پاسخ تأیید نشد. وضعیت فعلی را بررسی کن یا همان درخواست ذخیره‌شده را تکرار کن.",
    recorded: "درخواست پشتیبان ثبت شد.", recovered: "درخواست قبلی در وضعیت ثبت‌شده پیدا شد.",
    cancelRecorded: "درخواست لغو ثبت شد.", alreadyFinished: "این عملیات قبلاً پایان یافته است.",
    saved: "تنظیمات پشتیبان ذخیره شد.", permission: "این عملیات دیگر برای حساب شما مجاز نیست.",
    invalid: "زمان‌بندی و تعداد یا مدت نگهداری نسخه‌ها را بررسی کن.",
    storage: "مرورگر نتوانست اطلاعات بازیابی درخواست را نگه دارد. درخواستی فرستاده نشد.",
    conflict: "تنظیمات در جای دیگری تغییر کرده است. ویرایش شما حفظ شده است.",
    currentSettings: "تنظیمات ثبت‌شدهٔ فعلی", saveEdits: "ذخیرهٔ ویرایش من", useSaved: "استفاده از تنظیمات ثبت‌شده",
    scheduleTitle: "پشتیبان خودکار", scheduleIntro: "زمان ساخت نسخه را انتخاب کن. ساعت بر اساس منطقهٔ زمانی انتخاب‌شده است.",
    enabled: "فعال‌کردن پشتیبان زمان‌بندی‌شده", frequency: "تکرار", daily: "هر روز", weekly: "هر هفته", time: "ساعت",
    timezone: "منطقهٔ زمانی", weekday: "روز", retentionTitle: "نگهداری سوابق", retentionIntro: "قواعد نگهداری پس از ثبت موفق یک آرشیو تازه اعمال می‌شوند.",
    keepLast: "تعداد نسخه‌های آخر", maxAge: "حداکثر عمر نسخه‌ها (روز)", keepHint: "از ۱ تا ۱۰۰ نسخه", ageHint: "از ۱ تا ۳۶۵ روز",
    protectedHint: "آرشیوهای محافظت‌شده حذف نمی‌شوند و ممکن است تعداد نسخه‌ها از حد تنظیم‌شده بیشتر شود.", save: "ذخیرهٔ تنظیمات", saving: "در حال ذخیره…", unsaved: "تغییرات ذخیره‌نشده",
    archivesTitle: "آرشیوهای شما", archivesIntro: "فایل رمزگذاری‌شده را برای نگهداری دریافت کن.",
    empty: "نخستین پشتیبان کامل‌شده در این بخش نمایش داده می‌شود.", download: "دریافت آرشیو", downloadUnavailable: "دریافت در دسترس نیست",
    encryptedSize: "حجم رمزگذاری‌شده", payload: "محتوا", protected: "محافظت‌شده", checksum: "صحت فایل بررسی شده",
    restoreVerified: "بازیابی بررسی شده", checksumDetail: "چک‌سام فایل ذخیره‌شده بررسی شده است. رمزگشایی و سازگاری بازیابی این نسخه هنوز بررسی نشده‌اند.",
    restoreDetail: "بررسی بازیابی برای این نسخه ثبت شده است.", fingerprint: "اثر انگشت گیرندهٔ رمزگذاری",
    noActivation: "بارگذاری یا بازیابی آرشیو از این صفحه هنوز در دسترس نیست.",
    details: "جزئیات آرشیو", sha: "SHA-256", source: "نسخهٔ منبع", schema: "ساختار داده",
    states: { queued: "در صف", running: "در حال اجرا", cancelling: "در حال لغو ایمن", succeeded: "کامل‌شده", failed: "ناموفق", cancelled: "لغوشده", recovery_required: "نیازمند بازیابی" },
    phases: { queued: "در انتظار شروع", preflight: "بررسی آمادگی", stopping_hub: "آماده‌سازی نسخهٔ آفلاین", creating_offline: "ساخت نسخهٔ رمزگذاری‌شده", restarting_hub: "بازگرداندن پنل", checking_archive: "بررسی آرشیو ذخیره‌شده", retention: "اعمال قواعد نگهداری", complete: "پایان‌یافته" },
    days: ["یکشنبه", "دوشنبه", "سه‌شنبه", "چهارشنبه", "پنجشنبه", "جمعه", "شنبه"],
  },
} as const;

const initialSettings: BackupSettings = {
  revision: 1, schedule: { enabled: false, frequency: "daily", time: "03:00", timezone: "Asia/Tehran", weekday: 0 },
  retention: { keep_last: 7, max_age_days: 30 },
};
function dateLabel(value: number, language: Language, timezone: string): string {
  if (!value) return "—";
  return new Intl.DateTimeFormat(language === "fa" ? "fa-IR" : "en-GB", {
    dateStyle: "medium", timeStyle: "short", timeZone: timezone,
  }).format(value * 1000);
}
function bytesLabel(value: number, language: Language): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let unit = 0, size = value;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return new Intl.NumberFormat(language === "fa" ? "fa-IR" : "en", { maximumFractionDigits: unit ? 1 : 0 }).format(size) + " " + units[unit];
}
function mergeJob(status: BackupStatus, job: BackupJob): BackupStatus {
  const existing = status.jobs.find(item => item.id === job.id);
  if (existing && existing.revision > job.revision) return status;
  return { ...status, jobs: [job, ...status.jobs.filter(item => item.id !== job.id)] };
}

export default function NativeBackups({ language }: { language: Language }) {
  const { session } = useSession();
  const actor = session?.user.id ?? "";
  const t = words[language];
  const [snapshot, setSnapshot] = useState<BackupStatus | null>(null);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<PendingBackupRequest | null>(() => loadPending(actor));
  const pendingRef = useRef(pending);
  const [draft, setDraft] = useState<BackupDraft>(() => pending?.kind === "settings"
    ? draftFrom({ revision: pending.body.expected_revision, ...pending.body }) : draftFrom(initialSettings));
  const [baseRevision, setBaseRevision] = useState(pending?.kind === "settings" ? pending.body.expected_revision : 0);
  const [dirty, setDirty] = useState(pending?.kind === "settings");
  const dirtyRef = useRef(dirty);
  const [conflict, setConflict] = useState(false);
  const [notice, setNotice] = useState<keyof Pick<typeof words.en, "recorded" | "recovered" | "cancelRecorded" | "alreadyFinished" | "saved" | "permission" | "invalid" | "storage" | "uncertain"> | "">("");
  const [mutationBusy, setMutationBusy] = useState(false);
  const mutationRef = useRef(false);
  const alive = useRef(true);
  const readGeneration = useRef(0);
  const [refresh, setRefresh] = useState(0);
  const retryRef = useRef<HTMLButtonElement>(null);

  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => {
    if (!session || session.user.role !== "owner") return;
    let active = true, timer: number | undefined, request: AbortController | undefined, failures = 0;
    const poll = async () => {
      if (!active || document.hidden || request) return;
      const controller = new AbortController();
      request = controller;
      const generation = ++readGeneration.current;
      let delay = 15000;
      try {
        const fresh = await getBackupStatus(controller.signal);
        if (!active || controller.signal.aborted || generation !== readGeneration.current) return;
        failures = 0;
        setSnapshot(before => {
          const previousJobs = new Map(before?.jobs.map(job => [job.id, job]));
          return {
            ...fresh,
            settings: before && before.settings.revision > fresh.settings.revision ? before.settings : fresh.settings,
            jobs: fresh.jobs.map(job => {
              const previous = previousJobs.get(job.id);
              return previous && previous.revision > job.revision ? previous : job;
            }),
          };
        });
        setConnected(true); setLoading(false);
        if (!dirtyRef.current && pendingRef.current?.kind !== "settings") {
          setDraft(draftFrom(fresh.settings)); setBaseRevision(fresh.settings.revision);
        }
        const saved = pendingRef.current;
        if (saved && resolvedPending(saved, fresh)) {
          clearPending(actor, saved.body.event_id);
          pendingRef.current = null; setPending(null); setNotice("recovered");
        }
        if (fresh.jobs.some(jobActive) || pendingRef.current) delay = 4000;
      } catch {
        if (!active || controller.signal.aborted) return;
        failures++;
        delay = Math.min(30000, 4000 * 2 ** Math.min(failures, 3));
        setConnected(false); setLoading(false);
      } finally {
        if (request === controller) request = undefined;
        if (active && !document.hidden) timer = window.setTimeout(() => { void poll(); }, delay);
      }
    };
    const wake = () => {
      window.clearTimeout(timer);
      if (document.hidden) { request?.abort(); return; }
      void poll();
    };
    void poll();
    document.addEventListener("visibilitychange", wake);
    window.addEventListener("online", wake);
    return () => {
      active = false; window.clearTimeout(timer); request?.abort();
      document.removeEventListener("visibilitychange", wake); window.removeEventListener("online", wake);
    };
  }, [actor, session?.user.role, refresh]);

  function update<K extends keyof BackupDraft>(key: K, value: BackupDraft[K]) {
    setDraft(before => ({ ...before, [key]: value }));
    dirtyRef.current = true; setDirty(true); setNotice("");
  }
  function forgetRequest(request: PendingBackupRequest) {
    clearPending(actor, request.body.event_id);
    if (pendingRef.current?.body.event_id === request.body.event_id) { pendingRef.current = null; setPending(null); }
  }
  async function perform(request: PendingBackupRequest) {
    if (!session || session.user.role !== "owner" || mutationRef.current ||
        (pendingRef.current && pendingRef.current.body.event_id !== request.body.event_id)) return;
    const replaying = pendingRef.current?.body.event_id === request.body.event_id;
    if (!savePending(actor, request)) { setNotice("storage"); return; }
    pendingRef.current = request; setPending(request);
    mutationRef.current = true; setMutationBusy(true); setNotice("");
    try {
      const result = await sendBackupRequest(request, session.csrf_token);
      if (!alive.current) return;
      readGeneration.current++;
      if (request.kind === "settings") {
        const settings = result as BackupSettings;
        setSnapshot(before => before ? { ...before, settings: before.settings.revision > settings.revision ? before.settings : settings } : before);
        setDraft(draftFrom(settings)); setBaseRevision(settings.revision);
        dirtyRef.current = false; setDirty(false); setConflict(false); setNotice("saved");
      } else {
        const job = result as BackupJob;
        setSnapshot(before => before ? mergeJob(before, job) : before);
        setNotice(request.kind === "create" ? "recorded" : job.cancel_requested ? "cancelRecorded" : "alreadyFinished");
      }
      forgetRequest(request);
      setRefresh(value => value + 1);
    } catch (error) {
      if (!alive.current) return;
      if (error instanceof ApiError && error.status === 409 && request.kind === "settings") {
        forgetRequest(request);
        setConflict(true); dirtyRef.current = true; setDirty(true);
        setRefresh(value => value + 1);
      } else if (!replaying && error instanceof ApiError && [400, 403, 404, 422].includes(error.status)) {
        forgetRequest(request); setNotice(error.status === 403 ? "permission" : "invalid");
        setRefresh(value => value + 1);
      } else {
        setNotice("uncertain");
        window.setTimeout(() => retryRef.current?.focus(), 0);
      }
    } finally {
      mutationRef.current = false;
      if (alive.current) setMutationBusy(false);
    }
  }
  const jobs = [...(snapshot?.jobs ?? [])].sort((a, b) => b.created_unix - a.created_unix);
  const activeJob = jobs.find(jobActive);
  const latestJob = activeJob ?? jobs[0];
  const recoveryRequired = snapshot?.runner_status === "recovery_required" || jobs.some(job => job.state === "recovery_required");
  const operationError = latestJob?.error_code === "publication_unknown" ? t.publicationUnknown : latestJob?.state === "failed"
    ? latestJob.error_code === "download_busy" ? t.downloadBusy : t.failed : "";
  const operationWarning = latestJob?.state === "succeeded" && latestJob.warning_code === "retention_incomplete" ? t.retentionIncomplete
    : latestJob?.state === "succeeded" && latestJob.warning_code === "cancel_requested_after_publication" ? t.cancelledTooLate
    : latestJob?.state === "succeeded" && latestJob.warning_code === "recovered_after_interruption" ? t.recoveredOperation : t.warning;
  const archives = [...(snapshot?.archives ?? [])].sort((a, b) => b.created_unix - a.created_unix);
  const zone = snapshot?.settings.schedule.timezone ?? "Asia/Tehran";
  const enabled = snapshot?.enabled === true;
  const canCreate = enabled && connected && snapshot.capabilities.create && snapshot.runner_status === "ready" &&
    !activeJob && !pending && !mutationBusy;
  const formLocked = mutationBusy || pending?.kind === "settings";
  const canSave = enabled && connected && !pending && !mutationBusy && dirty &&
    (snapshot.capabilities.schedules || snapshot.capabilities.retention) &&
    (!conflict || snapshot.settings.revision !== baseRevision);
  const runnerText = snapshot?.runner_status === "ready" ? t.ready : snapshot?.runner_status === "busy" ? t.busy :
    snapshot?.runner_status === "recovery_required" ? t.attention : t.runnerUnavailable;
  async function create() {
    if (!canCreate) return;
    await perform({ version: 1, actor_id: actor, kind: "create", body: { event_id: newBackupEvent(), action: "create" } });
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (!canSave || !snapshot) return;
    const policy = policyFrom(draft);
    if (!policy) { setNotice("invalid"); return; }
    await perform({ version: 1, actor_id: actor, kind: "settings",
      body: { event_id: newBackupEvent(), expected_revision: conflict ? snapshot.settings.revision : baseRevision, ...policy } });
  }
  function useSaved() {
    if (!snapshot || formLocked) return;
    setDraft(draftFrom(snapshot.settings)); setBaseRevision(snapshot.settings.revision);
    dirtyRef.current = false; setDirty(false); setConflict(false); setNotice("");
  }
  if (!session || session.user.role !== "owner") return null;
  return <section className="native-backups" aria-label={t.label}>
    <section className="backup-hero">
      <div className="backup-hero-copy">
        <span className="backup-hero-icon"><DatabaseBackup size={24} aria-hidden="true" /></span>
        <div><span className="backup-eyebrow">{t.eyebrow}</span><h2>{t.title}</h2><p>{t.intro}</p></div>
      </div>
      <div className="backup-primary-actions">
        <button type="button" className="backup-button" disabled={!canCreate} onClick={() => { void create(); }}>
          {mutationBusy && pending?.kind === "create" ? <LoaderCircle className="backup-spin" size={17} /> : <DatabaseBackup size={17} />}
          {mutationBusy && pending?.kind === "create" ? t.creating : t.create}
        </button>
        <button type="button" className="backup-button backup-secondary" onClick={() => setRefresh(value => value + 1)}>
          <RefreshCw size={15} aria-hidden="true" />{t.refresh}
        </button>
      </div>
      <p className="backup-offline-note"><Clock3 size={15} aria-hidden="true" />{t.offline}</p>
    </section>

    {loading && <p className="backup-message" role="status"><LoaderCircle className="backup-spin" size={17} />{t.loading}</p>}
    {!loading && !connected && <p className="backup-message backup-warning" role="status"><AlertCircle size={18} />{snapshot ? activeJob || pending?.kind === "create" ? t.staleBackup : t.stale : t.unavailable}</p>}
    {snapshot && !enabled && <section className="backup-disabled" role="status"><ShieldCheck size={22} /><div><h3>{t.disabled}</h3><p>{t.disabledDetail}</p></div></section>}
    {recoveryRequired && <p className="backup-message backup-warning" role="alert"><AlertCircle size={18} />{t.recovery}</p>}
    {notice && <p className={"backup-message" + (["uncertain", "storage", "invalid", "permission"].includes(notice) ? " backup-warning" : " backup-confirmed")} role="status">{t[notice]}</p>}

    {pending && <section className="backup-pending" aria-label={t.pendingTitle}>
      <div><h3>{t.pendingTitle}</h3><p>{pending.kind === "settings" ? t.pendingSettings : t.pending}</p>
        <small>{t.requestID}: <bdi>{pending.body.event_id}</bdi></small></div>
      <button ref={retryRef} type="button" className="backup-button backup-secondary" disabled={mutationBusy || !connected || !enabled}
        onClick={() => { void perform(pending); }}><RefreshCw size={16} aria-hidden="true" />{t.retry}</button>
    </section>}

    {snapshot && <>
      <div className="backup-summary">
        <div><span>{t.lastBackup}</span><strong>{archives[0] ? dateLabel(archives[0].created_unix, language, zone) : t.noBackup}</strong></div>
        <div><span>{t.nextBackup}</span><strong>{!snapshot.settings.schedule.enabled ? t.scheduleOff : snapshot.next_run_unix ? dateLabel(snapshot.next_run_unix, language, zone) : t.nextUnknown}</strong><small><bdi>{zone}</bdi></small></div>
        <div><span>{t.count}</span><strong>{archives.length.toLocaleString(language === "fa" ? "fa-IR" : "en")}</strong>
          {enabled && <small className={"backup-runner backup-runner-" + snapshot.runner_status}>{connected ? runnerText : t.runnerUnavailable}</small>}</div>
      </div>

      {latestJob && <section className="backup-operation" aria-label={t.activity}>
        <header><div><span className="backup-eyebrow">{t.activity}</span><h3>{t.phases[latestJob.phase]}</h3></div>
          <span className={"backup-badge backup-state-" + latestJob.state}>{t.states[latestJob.state]}</span></header>
        <div className="backup-operation-details">
          <span>{latestJob.trigger === "schedule" ? t.scheduled : t.manual}</span>
          <time dateTime={new Date(latestJob.created_unix * 1000).toISOString()}>{dateLabel(latestJob.created_unix, language, zone)}</time>
          {latestJob.finished_unix > 0 && <span>{t.finished}: {dateLabel(latestJob.finished_unix, language, zone)}</span>}
        </div>
        {operationError && <p className="backup-warning" role="status">{operationError}</p>}
        {latestJob.warning_code && <p className="backup-warning" role="status">{operationWarning}</p>}
        {latestJob.cancel_requested && jobActive(latestJob) && <p role="status">{t.cancelNote}</p>}
        {cancellable(latestJob) && snapshot.capabilities.cancel && <button type="button" className="backup-button backup-secondary"
          disabled={!connected || !!pending || mutationBusy} onClick={() => { void perform({ version: 1, actor_id: actor,
            kind: "cancel", job_id: latestJob.id, body: { event_id: newBackupEvent() } }); }}>
          <Square size={14} aria-hidden="true" />{t.cancel}
        </button>}
      </section>}

      <form className="backup-settings" onSubmit={event => { void save(event); }} aria-label={t.scheduleTitle}>
        <div className="backup-settings-columns">
          <section>
            <h3><CalendarClock size={19} aria-hidden="true" />{t.scheduleTitle}</h3><p>{t.scheduleIntro}</p>
            <label className="backup-toggle"><input type="checkbox" checked={draft.enabled}
              disabled={!enabled || !snapshot.capabilities.schedules || formLocked}
              onChange={event => update("enabled", event.target.checked)} /><span>{t.enabled}</span></label>
            <div className="backup-fields">
              <label>{t.frequency}<select value={draft.frequency} aria-label={t.frequency} disabled={!enabled || !snapshot.capabilities.schedules || formLocked}
                onChange={event => update("frequency", event.target.value as "daily" | "weekly")}>
                <option value="daily">{t.daily}</option><option value="weekly">{t.weekly}</option>
              </select></label>
              <label>{t.time}<input type="time" value={draft.time} required dir="ltr" disabled={!enabled || !snapshot.capabilities.schedules || formLocked}
                onChange={event => update("time", event.target.value)} /></label>
              {draft.frequency === "weekly" && <label>{t.weekday}<select value={draft.weekday} aria-label={t.weekday} disabled={!enabled || !snapshot.capabilities.schedules || formLocked}
                onChange={event => update("weekday", Number(event.target.value))}>{t.days.map((day, index) => <option key={index} value={index}>{day}</option>)}</select></label>}
              <label className="backup-zone">{t.timezone}<input value={draft.timezone} list="backup-timezones" dir="ltr" autoComplete="off" maxLength={64}
                disabled={!enabled || !snapshot.capabilities.schedules || formLocked} onChange={event => update("timezone", event.target.value)} /></label>
              <datalist id="backup-timezones"><option value="Asia/Tehran" /><option value="UTC" /></datalist>
            </div>
          </section>
          <section>
            <h3><Archive size={19} aria-hidden="true" />{t.retentionTitle}</h3><p>{t.retentionIntro}</p>
            <div className="backup-fields">
              <label>{t.keepLast}<input type="number" min={1} max={100} step={1} required value={draft.keepLast} inputMode="numeric" aria-label={t.keepLast} aria-describedby="backup-keep-hint"
                disabled={!enabled || !snapshot.capabilities.retention || formLocked} onChange={event => update("keepLast", event.target.value)} /><small id="backup-keep-hint">{t.keepHint}</small></label>
              <label>{t.maxAge}<input type="number" min={1} max={365} step={1} required value={draft.maxAgeDays} inputMode="numeric" aria-label={t.maxAge} aria-describedby="backup-age-hint"
                disabled={!enabled || !snapshot.capabilities.retention || formLocked} onChange={event => update("maxAgeDays", event.target.value)} /><small id="backup-age-hint">{t.ageHint}</small></label>
            </div>
            <p className="backup-retention-note"><ShieldCheck size={15} aria-hidden="true" />{t.protectedHint}</p>
          </section>
        </div>
        {conflict && <div className="backup-conflict" role="alert">
          <strong>{t.conflict}</strong><p>{t.currentSettings}: {snapshot.settings.schedule.enabled
            ? (snapshot.settings.schedule.frequency === "daily" ? t.daily : t.weekly) + " · " + snapshot.settings.schedule.time + " · " + snapshot.settings.schedule.timezone
            : t.scheduleOff} · {snapshot.settings.retention.keep_last} / {snapshot.settings.retention.max_age_days}</p>
          <button type="button" className="backup-text-button" disabled={formLocked} onClick={useSaved}>{t.useSaved}</button>
        </div>}
        <footer><span>{dirty ? t.unsaved : ""}</span><button className="backup-button" type="submit" disabled={!canSave}>
          {mutationBusy && pending?.kind === "settings" ? <LoaderCircle className="backup-spin" size={16} /> : <Check size={16} />}
          {mutationBusy && pending?.kind === "settings" ? t.saving : conflict ? t.saveEdits : t.save}
        </button></footer>
      </form>

      <section className="backup-archives" aria-label={t.archivesTitle}>
        <header><div><h3>{t.archivesTitle}</h3><p>{t.archivesIntro}</p></div><span className="backup-count">{archives.length.toLocaleString(language === "fa" ? "fa-IR" : "en")}</span></header>
        {archives.length === 0 ? <div className="backup-empty"><Archive size={30} aria-hidden="true" /><p>{t.empty}</p></div> :
          <div className="backup-archive-list">{archives.map(archive => <ArchiveCard key={archive.id} archive={archive} language={language} timezone={zone}
            downloadable={enabled && connected && snapshot.capabilities.download && archive.downloadable} />)}</div>}
      </section>
      <footer className="backup-footnote">
        {snapshot.recipient_fingerprint && <p>{t.fingerprint}: <bdi>{snapshot.recipient_fingerprint}</bdi></p>}
        <p>{t.noActivation}</p>
      </footer>
    </>}
  </section>;
}

function ArchiveCard({ archive, language, timezone, downloadable }: { archive: BackupArchive; language: Language; timezone: string; downloadable: boolean }) {
  const t = words[language];
  return <article className="backup-archive">
    <div className="backup-archive-heading"><div><strong>{dateLabel(archive.created_unix, language, timezone)}</strong>
      <span className="backup-archive-size"><bdi>{bytesLabel(archive.size_bytes, language)}</bdi></span></div>
      {downloadable ? <a className="backup-download" href={archiveDownloadURL(archive)} download referrerPolicy="no-referrer">
        <Download size={16} aria-hidden="true" />{t.download}</a> : <span className="backup-download backup-download-disabled">{t.downloadUnavailable}</span>}
    </div>
    <div className="backup-archive-badges"><span><ShieldCheck size={14} aria-hidden="true" />{archive.verification === "restore_verified" ? t.restoreVerified : t.checksum}</span>
      {archive.protected && <span>{t.protected}</span>}</div>
    <p className="backup-verification">{archive.verification === "restore_verified" ? t.restoreDetail : t.checksumDetail}</p>
    <details><summary>{t.details}</summary><dl>
      <div><dt>{t.payload}</dt><dd><bdi>{bytesLabel(archive.payload_bytes, language)}</bdi></dd></div>
      <div><dt>{t.schema}</dt><dd>{archive.schema_version}</dd></div>
      <div><dt>{t.source}</dt><dd><bdi>{archive.source_revision}</bdi></dd></div>
      <div><dt>{t.sha}</dt><dd><bdi>{archive.sha256}</bdi></dd></div>
    </dl></details>
  </article>;
}
