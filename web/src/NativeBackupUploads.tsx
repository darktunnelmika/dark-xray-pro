import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { AlertCircle, Check, Download, FileUp, LoaderCircle, RefreshCw, ShieldCheck, Square, Trash2 } from "lucide-react";
import { ApiError, useSession, type Language } from "./session";
import { clearPendingUpload, controlUpload, createUpload, getUploadStatus, hashUploadFile, loadPendingUpload,
  pendingFromUpload, retryableUpload, savePendingUpload, sendUploadContent, uploadDownloadURL, uploadMatches,
  type BackupUpload, type BackupUploadStatus, type PendingUpload } from "./nativeBackupUploads";
import "./NativeBackupUploads.css";

const words = {
  en: {
    title: "Upload an encrypted copy", intro: "Keep a previously downloaded encrypted file in this panel’s upload inbox.",
    boundary: "A received file has a verified ciphertext checksum. Decryption, native-data compatibility and recovery still require an independent check. Uploading does not restore or activate services.",
    choose: "Encrypted backup file", select: "Choose a file", upload: "Upload encrypted file", retryUpload: "Upload selected file from the beginning",
    selected: "Selected file", hash: "SHA-256", size: "Size", preparing: "Calculating the file checksum", stopHash: "Cancel checksum calculation",
    hashHint: "The file is read in small pieces on your device before it is sent.", hashing: "Reading file", hashReady: "File checksum ready.",
    transferring: "Browser bytes transferred", transferHint: "Transferred bytes do not yet confirm that the server saved the file.", waiting: "Waiting for the server’s durable receipt…",
    cancel: "Cancel upload", cancelPending: "Cancel this upload request", cancelHint: "Cancellation is confirmed only after the server records it.",
    refresh: "Refresh uploaded copies", loading: "Loading uploaded copies…", unavailable: "The upload inbox is unavailable. Refresh to check its recorded state.",
    stale: "Connection interrupted. The last confirmed upload status is shown.", disabled: "Uploads are currently unavailable.",
    received: "The encrypted file was received and its ciphertext checksum was verified.", unknown: "The response was not confirmed. The saved request will be checked; no new upload is started automatically.",
    storage: "The browser could not save the request record. No new request was sent.", wrongFile: "This file’s size or checksum differs from the saved request. Select the original file to retry.",
    capacity: "The inbox could not reserve this file. Review stored copies and available space before a new attempt.", requestRejected: "The server rejected this upload request before allocating a copy. Choose the file again.",
    invalidSize: "Choose a non-empty file within the displayed upload limit.", hashFailed: "The file checksum could not be calculated. Select the file again.",
    hashCancelled: "Checksum calculation cancelled. No file was sent.", cancelled: "The server confirmed this upload was cancelled.", removed: "The uploaded copy was removed.",
    lateCancel: "The file completed before cancellation. Its received copy was kept.", busy: "The server is still processing this request. Check its status before retrying.",
    request: "Saved upload request", requestHint: "The original request is retained across refresh. Select the same file to continue an interrupted upload.",
    requestID: "Request ID", retryRequest: "Retry saved request", retryCancel: "Retry cancellation", retryRemove: "Retry confirmed removal",
    sameFile: "Select the original file. Its complete checksum will be checked before another transfer.", remembered: "The saved request was found. Select the original file when a full upload can be retried.",
    receivedTitle: "Uploaded copies", empty: "Received and interrupted uploads will appear here.", download: "Download uploaded copy", retry: "Select original file to retry",
    remove: "Remove uploaded copy", confirmRemove: "Remove this exact uploaded copy?", removeHint: "This removes the encrypted inbox copy shown below. Your running panel data is not part of this action.",
    confirm: "Confirm removal", keep: "Keep this copy", bytes: "Server-recorded bytes", attempts: "Transfer attempts", details: "Upload details",
    noNativeProof: "Native contents have not been decrypted or validated.", limit: "Maximum file size", reserved: "Reserved inbox space", more: "Show more uploaded copies",
    publicationUnknown: "File publication is uncertain. Preserve this request for an operator check; do not start a replacement upload for it.",
    states: { awaiting_upload: "Ready to receive", receiving: "Receiving file", checking: "Checking saved bytes", received: "Ciphertext received", interrupted: "Transfer interrupted", rejected: "File rejected", cancelled: "Cancelled", deleting: "Removal in progress", deleted: "Removed", publication_unknown: "Publication uncertain" },
  },
  fa: {
    title: "بارگذاری نسخهٔ رمزگذاری‌شده", intro: "فایل رمزگذاری‌شده‌ای را که قبلاً دانلود کرده‌ای در صندوق بارگذاری این پنل نگه دار.",
    boundary: "برای فایل دریافت‌شده، چک‌سام فایل رمزگذاری‌شده تأیید می‌شود. رمزگشایی، سازگاری داده‌های بومی و بازیابی همچنان به بررسی مستقل نیاز دارند. بارگذاری، سرویس‌ها را بازیابی یا فعال نمی‌کند.",
    choose: "فایل پشتیبان رمزگذاری‌شده", select: "انتخاب فایل", upload: "بارگذاری فایل رمزگذاری‌شده", retryUpload: "بارگذاری فایل انتخاب‌شده از ابتدا",
    selected: "فایل انتخاب‌شده", hash: "SHA-256", size: "حجم", preparing: "محاسبهٔ چک‌سام فایل", stopHash: "لغو محاسبهٔ چک‌سام",
    hashHint: "فایل پیش از ارسال، به‌صورت بخش‌های کوچک روی دستگاه تو خوانده می‌شود.", hashing: "خواندن فایل", hashReady: "چک‌سام فایل آماده است.",
    transferring: "بایت‌های ارسال‌شده توسط مرورگر", transferHint: "تعداد بایت‌های ارسال‌شده، ذخیره‌شدن فایل روی سرور را تأیید نمی‌کند.", waiting: "در انتظار رسید ذخیرهٔ پایدار از سرور…",
    cancel: "لغو بارگذاری", cancelPending: "لغو این درخواست بارگذاری", cancelHint: "لغو فقط پس از ثبت نتیجه در سرور تأیید می‌شود.",
    refresh: "به‌روزرسانی فایل‌های بارگذاری‌شده", loading: "دریافت فایل‌های بارگذاری‌شده…", unavailable: "صندوق بارگذاری در دسترس نیست. برای بررسی وضعیت ثبت‌شده، به‌روزرسانی کن.",
    stale: "ارتباط قطع شده است. آخرین وضعیت تأییدشدهٔ بارگذاری نمایش داده می‌شود.", disabled: "بارگذاری در حال حاضر در دسترس نیست.",
    received: "فایل رمزگذاری‌شده دریافت شد و چک‌سام آن تأیید شد.", unknown: "پاسخ تأیید نشده است. درخواست ذخیره‌شده بررسی می‌شود؛ بارگذاری تازه‌ای خودکار آغاز نمی‌شود.",
    storage: "مرورگر نتوانست رسید درخواست را ذخیره کند. درخواست تازه‌ای ارسال نشد.", wrongFile: "حجم یا چک‌سام این فایل با درخواست ذخیره‌شده یکسان نیست. برای تلاش دوباره، همان فایل اصلی را انتخاب کن.",
    capacity: "صندوق نتوانست برای این فایل ظرفیت رزرو کند. پیش از تلاش تازه، نسخه‌های موجود و فضای آزاد را بررسی کن.", requestRejected: "سرور این درخواست را پیش از رزرو نسخه رد کرد. فایل را دوباره انتخاب کن.",
    invalidSize: "فایلی غیرخالی و کوچک‌تر از سقف نمایش‌داده‌شده انتخاب کن.", hashFailed: "محاسبهٔ چک‌سام فایل انجام نشد. فایل را دوباره انتخاب کن.",
    hashCancelled: "محاسبهٔ چک‌سام لغو شد. فایلی ارسال نشد.", cancelled: "سرور لغوشدن این بارگذاری را تأیید کرد.", removed: "نسخهٔ بارگذاری‌شده حذف شد.",
    lateCancel: "فایل پیش از لغو کامل شد. نسخهٔ دریافت‌شده حفظ شد.", busy: "سرور هنوز این درخواست را پردازش می‌کند. پیش از تلاش دوباره، وضعیت را بررسی کن.",
    request: "درخواست بارگذاری ذخیره‌شده", requestHint: "درخواست اصلی پس از به‌روزرسانی صفحه حفظ می‌شود. برای ادامهٔ بارگذاری قطع‌شده، همان فایل را انتخاب کن.",
    requestID: "شناسهٔ درخواست", retryRequest: "تکرار درخواست ذخیره‌شده", retryCancel: "تکرار درخواست لغو", retryRemove: "تکرار حذف تأییدشده",
    sameFile: "فایل اصلی را انتخاب کن. پیش از ارسال دوباره، چک‌سام کامل آن بررسی خواهد شد.", remembered: "درخواست ذخیره‌شده پیدا شد. وقتی تلاش دوباره مجاز شد، فایل اصلی را انتخاب کن.",
    receivedTitle: "نسخه‌های بارگذاری‌شده", empty: "فایل‌های دریافت‌شده و بارگذاری‌های قطع‌شده اینجا نمایش داده می‌شوند.", download: "دانلود نسخهٔ بارگذاری‌شده", retry: "انتخاب فایل اصلی برای تلاش دوباره",
    remove: "حذف نسخهٔ بارگذاری‌شده", confirmRemove: "همین نسخهٔ بارگذاری‌شده حذف شود؟", removeHint: "این کار فقط نسخهٔ رمزگذاری‌شدهٔ زیر را از صندوق بارگذاری حذف می‌کند. داده‌های پنل فعال جزو این عملیات نیستند.",
    confirm: "تأیید حذف", keep: "نگه‌داشتن این نسخه", bytes: "بایت‌های ثبت‌شده در سرور", attempts: "تعداد تلاش‌های ارسال", details: "جزئیات بارگذاری",
    noNativeProof: "محتوای بومی رمزگشایی یا اعتبارسنجی نشده است.", limit: "حداکثر حجم فایل", reserved: "فضای رزروشدهٔ صندوق", more: "نمایش نسخه‌های بیشتر",
    publicationUnknown: "انتشار فایل قطعی نیست. این درخواست را برای بررسی مسئول سرور حفظ کن و برای آن بارگذاری جایگزین آغاز نکن.",
    states: { awaiting_upload: "آمادهٔ دریافت", receiving: "در حال دریافت فایل", checking: "بررسی بایت‌های ذخیره‌شده", received: "فایل رمزگذاری‌شده دریافت شد", interrupted: "ارسال قطع شد", rejected: "فایل رد شد", cancelled: "لغوشده", deleting: "در حال حذف", deleted: "حذف‌شده", publication_unknown: "انتشار نامشخص" },
  },
};
type Notice = Exclude<keyof typeof words.en, "states">;
type LocalProgress = { phase: "hashing" | "sending" | "waiting"; bytes: number; size: number };
function bytes(value: number, language: Language): string {
  const unit = value >= 1024 ** 3 ? 1024 ** 3 : value >= 1024 ** 2 ? 1024 ** 2 : value >= 1024 ? 1024 : 1;
  const label = unit === 1024 ** 3 ? "GiB" : unit === 1024 ** 2 ? "MiB" : unit === 1024 ? "KiB" : "B";
  return new Intl.NumberFormat(language === "fa" ? "fa-IR" : "en", { maximumFractionDigits: 2 }).format(value / unit) + " " + label;
}

export function NativeBackupUploads({ language, timezone }: { language: Language; timezone: string }) {
  const { session } = useSession();
  const actor = session?.user.id ?? "";
  const t = words[language];
  const [snapshot, setSnapshot] = useState<BackupUploadStatus | null>(null);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<PendingUpload | null>(null);
  const pendingRef = useRef<PendingUpload | null>(null);
  const [file, setFile] = useState<File | null>(null);
  const [fileHash, setFileHash] = useState("");
  const [progress, setProgress] = useState<LocalProgress | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<Notice | "">("");
  const [refresh, setRefresh] = useState(0);
  const [showCount, setShowCount] = useState(12);
  const [removeCandidate, setRemoveCandidate] = useState<BackupUpload | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const alive = useRef(false);
  const generation = useRef(0);
  const readGeneration = useRef(0);
  const pollingBusy = useRef(false);
  const active = useRef<AbortController | null>(null);
  const busyRef = useRef(false);

  function remember(value: PendingUpload): boolean {
    if (!savePendingUpload(value)) { setNotice("storage"); return false; }
    pendingRef.current = value; setPending(value); return true;
  }
  function forget(value: PendingUpload) {
    clearPendingUpload(value);
    if (pendingRef.current?.event_id === value.event_id && pendingRef.current.intent === value.intent) {
      pendingRef.current = null; setPending(null); setFile(null); setFileHash("");
      if (fileInput.current) fileInput.current.value = "";
    }
  }
  function reconcile(upload: BackupUpload, request: PendingUpload) {
    if (!uploadMatches(upload, request)) return;
    if (request.intent === "remove" && upload.state === "deleted") { forget(request); setNotice("removed"); }
    else if (request.intent === "cancel" && ["cancelled", "received", "deleted"].includes(upload.state)) {
      forget(request); setNotice(upload.state === "received" ? "lateCancel" : "cancelled");
    } else if (request.intent === "upload" && ["received", "cancelled", "deleted"].includes(upload.state)) {
      forget(request); setNotice(upload.state === "received" ? "received" : upload.state === "deleted" ? "removed" : "cancelled");
    } else if (!request.upload_id) remember({ ...request, upload_id: upload.id });
  }
  function record(upload: BackupUpload, request: PendingUpload) {
    readGeneration.current++;
    setSnapshot(before => before ? { ...before, uploads: [upload, ...before.uploads.filter(item => item.id !== upload.id)] } : before);
    reconcile(upload, request);
    setRefresh(value => value + 1);
  }
  function rejectedAllocation(request: PendingUpload, error: unknown): boolean {
    // These responses prove no allocation was made for this exact event. A
    // conflict, authentication change or transport error is still uncertain.
    if (request.upload_id || pendingRef.current?.event_id === request.event_id && pendingRef.current.upload_id || !(error instanceof ApiError) ||
        !(error.status === 400 || error.status === 409 && error.message === "backup_capacity")) return false;
    forget(request); setNotice(error.status === 409 ? "capacity" : "requestRejected");
    setRefresh(value => value + 1); return true;
  }
  useEffect(() => {
    alive.current = true;
    const saved = loadPendingUpload(actor);
    pendingRef.current = saved; setPending(saved);
    return () => { alive.current = false; generation.current++; active.current?.abort(); active.current = null; };
  }, [actor]);
  useEffect(() => {
    if (!actor || session?.user.role !== "owner") return;
    let stopped = false;
    let timer: number | undefined;
    let controller: AbortController | undefined;
    async function poll() {
      if (document.visibilityState !== "visible") return;
      window.clearTimeout(timer); controller?.abort(); controller = new AbortController();
      const currentRead = ++readGeneration.current;
      try {
        const value = await getUploadStatus(controller.signal);
        if (stopped || currentRead !== readGeneration.current) return;
        setSnapshot(value); setConnected(true);
        pollingBusy.current = value.uploads.some(upload => ["receiving", "checking", "deleting"].includes(upload.state));
        const saved = pendingRef.current;
        const found = saved ? value.uploads.find(upload => uploadMatches(upload, saved)) : undefined;
        if (saved && found) reconcile(found, saved);
      } catch {
        if (!stopped && currentRead === readGeneration.current) setConnected(false);
      } finally {
        if (!stopped && document.visibilityState === "visible") {
          setLoading(false);
          timer = window.setTimeout(() => { void poll(); }, pendingRef.current || pollingBusy.current ? 2500 : 15000);
        }
      }
    }
    const wake = () => {
      if (document.visibilityState === "visible") void poll();
      else { window.clearTimeout(timer); readGeneration.current++; controller?.abort(); }
    };
    void poll(); document.addEventListener("visibilitychange", wake); window.addEventListener("online", wake);
    return () => { stopped = true; window.clearTimeout(timer); controller?.abort(); document.removeEventListener("visibilitychange", wake); window.removeEventListener("online", wake); };
  }, [actor, session?.user.role, refresh]);

  const current = pending ? snapshot?.uploads.find(upload => uploadMatches(upload, pending)) : undefined;
  const available = connected && snapshot?.enabled === true;
  const canSend = available && !busy && !!file && !!fileHash && (!pending || pending.intent === "upload" && (!pending.upload_id || !!current && retryableUpload(current)));
  const visible = [...(snapshot?.uploads ?? [])].filter(upload => upload.state !== "deleted").sort((a, b) => b.created_unix - a.created_unix || b.id.localeCompare(a.id));

  async function chooseFile(event: ChangeEvent<HTMLInputElement>) {
    const selected = event.target.files?.[0];
    if (!selected || busyRef.current || !snapshot) return;
    setFile(null); setFileHash(""); setNotice("");
    if (selected.size < 1 || selected.size > snapshot.max_upload_bytes) { setNotice("invalidSize"); event.target.value = ""; return; }
    if (pendingRef.current && selected.size !== pendingRef.current.size_bytes) { setNotice("wrongFile"); event.target.value = ""; return; }
    const controller = new AbortController(); active.current = controller;
    const operation = ++generation.current;
    busyRef.current = true; setBusy(true); setProgress({ phase: "hashing", bytes: 0, size: selected.size });
    try {
      const hash = await hashUploadFile(selected, controller.signal, count => {
        if (alive.current && operation === generation.current) setProgress({ phase: "hashing", bytes: count, size: selected.size });
      });
      if (!alive.current || operation !== generation.current) return;
      if (pendingRef.current && hash !== pendingRef.current.sha256) { setNotice("wrongFile"); if (fileInput.current) fileInput.current.value = ""; return; }
      setFile(selected); setFileHash(hash); setNotice("hashReady");
    } catch (error) {
      if (alive.current && operation === generation.current) {
        setNotice(error instanceof DOMException && error.name === "AbortError" ? "hashCancelled" : "hashFailed");
        if (fileInput.current) fileInput.current.value = "";
      }
    } finally {
      if (alive.current && operation === generation.current) { active.current = null; busyRef.current = false; setBusy(false); setProgress(null); }
    }
  }
  async function upload() {
    if (!canSend || !session || !file || busyRef.current) return;
    let request = pendingRef.current;
    if (request && (request.intent !== "upload" || request.sha256 !== fileHash || request.size_bytes !== file.size)) { setNotice("wrongFile"); return; }
    if (!request) request = { version: 1, actor_id: actor, event_id: "upload-" + crypto.randomUUID(), size_bytes: file.size, sha256: fileHash, upload_id: "", intent: "upload" };
    if (!remember(request)) return;
    const operation = ++generation.current;
    busyRef.current = true; setBusy(true); setNotice("");
    try {
      if (!request.upload_id) {
        const receipt = await createUpload(request, session.csrf_token);
        if (!alive.current || operation !== generation.current) return;
        record(receipt, request);
        if (!retryableUpload(receipt)) { if (receipt.state !== "received") setNotice("busy"); return; }
        request = { ...request, upload_id: receipt.id };
        if (!remember(request)) return;
      }
      const controller = new AbortController(); active.current = controller;
      setProgress({ phase: "sending", bytes: 0, size: file.size });
      const receipt = await sendUploadContent(file, request, session.csrf_token, controller.signal,
        count => { if (alive.current && operation === generation.current) setProgress({ phase: "sending", bytes: count, size: file.size }); },
        () => { if (alive.current && operation === generation.current) setProgress({ phase: "waiting", bytes: file.size, size: file.size }); });
      if (alive.current && operation === generation.current) record(receipt, request);
    } catch (error) {
      if (alive.current && operation === generation.current && pendingRef.current?.event_id === request.event_id && !rejectedAllocation(request, error)) {
        setNotice("unknown"); setRefresh(value => value + 1);
      }
    } finally {
      if (alive.current && operation === generation.current) { active.current = null; busyRef.current = false; setBusy(false); setProgress(null); }
    }
  }
  async function replayCreate() {
    const request = pendingRef.current;
    if (!request || request.upload_id || !available || !session || busyRef.current) return;
    if (!remember(request)) return;
    const operation = ++generation.current; busyRef.current = true; setBusy(true);
    try {
      const receipt = await createUpload(request, session.csrf_token);
      if (alive.current && operation === generation.current) { record(receipt, request); if (retryableUpload(receipt)) setNotice("remembered"); }
    } catch (error) {
      if (alive.current && operation === generation.current && pendingRef.current?.event_id === request.event_id && !rejectedAllocation(request, error)) setNotice("unknown");
    }
    finally { if (alive.current && operation === generation.current) { busyRef.current = false; setBusy(false); } }
  }
  async function control(request: PendingUpload) {
    if (!session || request.actor_id !== actor || (!available && request.intent !== "cancel") || !request.upload_id || request.intent === "upload" ||
        (pendingRef.current && pendingRef.current.event_id !== request.event_id)) return;
    if (!remember(request)) return;
    const operation = ++generation.current;
    active.current?.abort(); active.current = null;
    setRemoveCandidate(null); busyRef.current = true; setBusy(true); setProgress(null); setNotice("");
    try {
      if (!available) { setNotice("unknown"); return; }
      const receipt = await controlUpload(request, session.csrf_token);
      if (alive.current && operation === generation.current) record(receipt, request);
    } catch {
      if (alive.current && operation === generation.current && pendingRef.current?.event_id === request.event_id) {
        setNotice("unknown"); setRefresh(value => value + 1);
      }
    } finally { if (alive.current && operation === generation.current) { busyRef.current = false; setBusy(false); } }
  }
  function adopt(upload: BackupUpload) {
    if (pendingRef.current || busyRef.current || upload.actor_id !== actor || !retryableUpload(upload)) return;
    if (!remember(pendingFromUpload(upload))) return;
    setFile(null); setFileHash(""); setNotice("sameFile");
    if (fileInput.current) { fileInput.current.value = ""; fileInput.current.focus(); }
  }

  if (!session || session.user.role !== "owner") return null;
  return <section className="backup-uploads" aria-label={t.title}>
    <header className="backup-upload-header"><div><h3><FileUp size={20} aria-hidden="true" />{t.title}</h3><p>{t.intro}</p></div>
      <button type="button" className="backup-button backup-secondary" onClick={() => setRefresh(value => value + 1)}><RefreshCw size={15} />{t.refresh}</button></header>
    <p className="backup-upload-boundary"><ShieldCheck size={17} aria-hidden="true" />{t.boundary}</p>
    {!snapshot && <p className="backup-message" role="status">{loading ? t.loading : t.unavailable}</p>}
    {snapshot && !connected && <p className="backup-message backup-warning" role="status"><AlertCircle size={17} />{t.stale}</p>}
    {snapshot && !snapshot.enabled && <p className="backup-message" role="status">{t.disabled}</p>}
    {notice && <p className={"backup-message " + (["received", "hashReady", "cancelled", "removed"].includes(notice) ? "backup-confirmed" : "backup-warning")} role="status">{t[notice]}</p>}
    {pending && <section className="backup-upload-pending" aria-label={t.request}>
      <h4>{t.request}</h4><p>{t.requestHint}</p>
      <dl><div><dt>{t.requestID}</dt><dd><bdi>{pending.event_id}</bdi></dd></div><div><dt>{t.size}</dt><dd><bdi>{bytes(pending.size_bytes, language)}</bdi></dd></div>
        <div><dt>{t.hash}</dt><dd><bdi>{pending.sha256}</bdi></dd></div></dl>
      {current && <p className="backup-upload-server-state">{t.states[current.state]}</p>}
      {current?.state === "publication_unknown" && <p className="backup-warning">{t.publicationUnknown}</p>}
      <div className="backup-upload-actions">
        {!pending.upload_id && <button className="backup-button backup-secondary" disabled={!available || busy} onClick={() => { void replayCreate(); }}>{t.retryRequest}</button>}
        {pending.upload_id && pending.intent === "upload" && current && !["received", "cancelled", "deleted", "deleting", "publication_unknown"].includes(current.state) &&
          <button className="backup-button backup-secondary" disabled={busy && !["sending", "waiting"].includes(progress?.phase ?? "")} onClick={() => { void control({ ...pending, intent: "cancel" }); }}><Square size={14} />{progress ? t.cancel : t.cancelPending}</button>}
        {pending.intent !== "upload" && <button className="backup-button backup-secondary" disabled={!available || busy} onClick={() => { void control(pending); }}>{pending.intent === "cancel" ? t.retryCancel : t.retryRemove}</button>}
      </div>
      {pending.intent === "cancel" && <p>{t.cancelHint}</p>}
    </section>}
    <div className="backup-upload-picker">
      <label htmlFor="backup-upload-file">{t.choose}</label>
      <input id="backup-upload-file" ref={fileInput} type="file" accept=".backup,application/octet-stream" disabled={!available || busy || !!pending && pending.intent !== "upload" || !!current && !retryableUpload(current)}
        onChange={event => { void chooseFile(event); }} />
      <p>{t.hashHint}</p>
      {snapshot && <div className="backup-upload-limits"><span>{t.limit}: <bdi>{bytes(snapshot.max_upload_bytes, language)}</bdi></span>
        <span>{t.reserved}: <bdi>{bytes(snapshot.reserved_bytes, language)} / {bytes(snapshot.max_inbox_bytes, language)}</bdi></span></div>}
      {file && fileHash && <dl className="backup-upload-selection"><div><dt>{t.selected}</dt><dd>{file.name}</dd></div><div><dt>{t.size}</dt><dd><bdi>{bytes(file.size, language)}</bdi></dd></div><div><dt>{t.hash}</dt><dd><bdi>{fileHash}</bdi></dd></div></dl>}
      {progress && <div className="backup-upload-progress" role="status">
        <strong>{progress.phase === "hashing" ? t.preparing : progress.phase === "waiting" ? t.waiting : t.transferring}</strong>
        <progress aria-label={progress.phase === "hashing" ? t.hashing : t.transferring} value={progress.bytes} max={progress.size} />
        <span><bdi>{bytes(progress.bytes, language)} / {bytes(progress.size, language)}</bdi></span>
        {progress.phase !== "hashing" && <p>{t.transferHint}</p>}
        {progress.phase === "hashing" && <button type="button" className="backup-button backup-secondary" onClick={() => active.current?.abort()}><Square size={14} />{t.stopHash}</button>}
      </div>}
      <button type="button" className="backup-button backup-upload-start" disabled={!canSend} onClick={() => { void upload(); }}>
        {busy ? <LoaderCircle size={17} className="backup-spin" /> : <FileUp size={17} />}{pending ? t.retryUpload : t.upload}</button>
    </div>
    <section className="backup-upload-list" aria-label={t.receivedTitle}>
      <h4>{t.receivedTitle} <span>{visible.length.toLocaleString(language === "fa" ? "fa-IR" : "en")}</span></h4>
      {visible.length === 0 && <p className="backup-empty">{t.empty}</p>}
      {visible.slice(0, showCount).map(upload => <article key={upload.id} className="backup-upload-card" data-upload-id={upload.id}>
        <header><div><strong>{t.states[upload.state]}</strong><time dateTime={new Date(upload.created_unix * 1000).toISOString()}>{new Intl.DateTimeFormat(language === "fa" ? "fa-IR" : "en", { dateStyle: "medium", timeStyle: "short", timeZone: timezone }).format(upload.created_unix * 1000)}</time></div>
          <span><bdi>{bytes(upload.size_bytes, language)}</bdi></span></header>
        {upload.state === "received" ? <p className="backup-upload-received"><Check size={15} aria-hidden="true" />{t.received}</p> : upload.state === "publication_unknown" && <p className="backup-warning">{t.publicationUnknown}</p>}
        <p className="backup-upload-validation">{t.noNativeProof}</p>
        <details><summary>{t.details}</summary><dl><div><dt>{t.requestID}</dt><dd><bdi>{upload.event_id}</bdi></dd></div><div><dt>{t.hash}</dt><dd><bdi>{upload.sha256}</bdi></dd></div>
          <div><dt>{t.bytes}</dt><dd><bdi>{bytes(upload.received_bytes, language)}</bdi></dd></div><div><dt>{t.attempts}</dt><dd>{upload.attempts.toLocaleString(language === "fa" ? "fa-IR" : "en")}</dd></div></dl></details>
        <div className="backup-upload-actions">
          {upload.downloadable && available && <a className="backup-download" href={uploadDownloadURL(upload)} download referrerPolicy="no-referrer"><Download size={15} />{t.download}</a>}
          {upload.actor_id === actor && retryableUpload(upload) && <button type="button" className="backup-button backup-secondary" disabled={!available || busy || !!pending} onClick={() => adopt(upload)}>{t.retry}</button>}
          {upload.actor_id === actor && ["awaiting_upload", "receiving", "checking", "interrupted", "rejected"].includes(upload.state) && <button type="button" className="backup-button backup-secondary" disabled={!available || busy || !!pending}
            onClick={() => { void control(pendingFromUpload(upload, "cancel")); }}><Square size={14} />{t.cancelPending}</button>}
          {upload.actor_id === actor && ["received", "interrupted", "rejected", "cancelled"].includes(upload.state) && <button type="button" className="backup-button backup-secondary" disabled={!available || busy || !!pending}
            onClick={() => setRemoveCandidate(upload)}><Trash2 size={14} />{t.remove}</button>}
        </div>
        {removeCandidate?.id === upload.id && <section className="backup-upload-remove" role="alertdialog" aria-modal="false" aria-label={t.confirmRemove}>
          <h4>{t.confirmRemove}</h4><p>{t.removeHint}</p><dl><div><dt>{t.size}</dt><dd><bdi>{bytes(upload.size_bytes, language)}</bdi></dd></div><div><dt>{t.hash}</dt><dd><bdi>{upload.sha256}</bdi></dd></div></dl>
          <div className="backup-upload-actions"><button className="backup-button" disabled={!available || busy || !!pending} onClick={() => { void control(pendingFromUpload(upload, "remove")); }}>{t.confirm}</button>
            <button className="backup-button backup-secondary" onClick={() => setRemoveCandidate(null)}>{t.keep}</button></div>
        </section>}
      </article>)}
      {visible.length > showCount && <button className="backup-button backup-secondary" onClick={() => setShowCount(value => value + 12)}>{t.more}</button>}
    </section>
  </section>;
}
