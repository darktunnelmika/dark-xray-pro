import { api, ApiError, mutation } from "./session";

export const uploadBase = "/api/v1/workspace/owner/backups/uploads";
export const maxUploadBytes = 66 * 1024 ** 3;
export const maxInboxBytes = 132 * 1024 ** 3;
const id = /^[a-f0-9]{32}$/;
const sha = /^[a-f0-9]{64}$/;
const event = /^[A-Za-z0-9_-]{16,96}$/;
const actor = /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$/;
const states = ["awaiting_upload", "receiving", "checking", "received", "interrupted", "rejected", "cancelled", "deleting", "deleted", "publication_unknown"] as const;
export type BackupUpload = {
  id: string; event_id: string; actor_id: string; size_bytes: number; sha256: string;
  state: typeof states[number]; created_unix: number; updated_unix: number; received_bytes: number;
  verification: "unverified" | "ciphertext_checksum_verified"; downloadable: boolean; error_code: string; attempts: number;
};
export type BackupUploadStatus = {
  enabled: boolean; max_upload_bytes: number; max_inbox_bytes: number; reserved_bytes: number;
  max_entries: number; max_active_entries: number; uploads: BackupUpload[];
};
export type PendingUpload = {
  version: 1; actor_id: string; event_id: string; size_bytes: number; sha256: string; upload_id: string;
  intent: "upload" | "cancel" | "remove";
};
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === "object" && !Array.isArray(value);
const integer = (value: unknown, min = 0, max = Number.MAX_SAFE_INTEGER): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value >= min && value <= max;
const timestamp = (value: unknown) => integer(value, 1, 253402300799);

export function validBackupUpload(value: unknown): value is BackupUpload {
  if (!object(value) || typeof value.id !== "string" || !id.test(value.id) ||
      typeof value.event_id !== "string" || !event.test(value.event_id) ||
      typeof value.actor_id !== "string" || !actor.test(value.actor_id) ||
      !integer(value.size_bytes, 1, maxUploadBytes) || typeof value.sha256 !== "string" || !sha.test(value.sha256) ||
      !states.includes(value.state as BackupUpload["state"]) || !timestamp(value.created_unix) || !timestamp(value.updated_unix) ||
      (value.updated_unix as number) < (value.created_unix as number) || !integer(value.received_bytes, 0, value.size_bytes) ||
      !["unverified", "ciphertext_checksum_verified"].includes(value.verification as string) ||
      typeof value.downloadable !== "boolean" || typeof value.error_code !== "string" ||
      (value.error_code !== "" && !/^[a-z][a-z0-9_]{0,95}$/.test(value.error_code)) || !integer(value.attempts, 0, 1_000_000)) return false;
  if (value.downloadable && (value.state !== "received" || value.verification !== "ciphertext_checksum_verified")) return false;
  if (value.state === "received" && (value.verification !== "ciphertext_checksum_verified" || value.received_bytes !== value.size_bytes ||
      value.downloadable !== true || value.attempts === 0 || value.error_code !== "")) return false;
  if (value.state === "awaiting_upload" && (value.attempts !== 0 || value.received_bytes !== 0)) return false;
  if (!["received", "deleting", "deleted"].includes(value.state as string) && value.verification !== "unverified") return false;
  return true;
}
export function readUploadStatus(value: unknown): BackupUploadStatus {
  if (!object(value) || typeof value.enabled !== "boolean" || !integer(value.max_upload_bytes, 1, maxUploadBytes) ||
      !integer(value.max_inbox_bytes, 1, maxInboxBytes) || !integer(value.reserved_bytes, 0, value.max_inbox_bytes) ||
      !integer(value.max_entries, 1, 4096) || !integer(value.max_active_entries, 1, 32) ||
      !Array.isArray(value.uploads) || value.uploads.length > value.max_entries || !value.uploads.every(validBackupUpload) ||
      new Set(value.uploads.map(upload => upload.id)).size !== value.uploads.length ||
      new Set(value.uploads.map(upload => upload.event_id)).size !== value.uploads.length) throw new Error("invalid_upload_status");
  return value as BackupUploadStatus;
}
export function uploadMatches(upload: BackupUpload, pending: PendingUpload): boolean {
  return upload.actor_id === pending.actor_id && upload.event_id === pending.event_id && upload.size_bytes === pending.size_bytes &&
    upload.sha256 === pending.sha256 && (pending.upload_id === "" || upload.id === pending.upload_id);
}
export function readUploadReceipt(value: unknown, pending: PendingUpload): BackupUpload {
  if (!object(value) || !validBackupUpload(value.upload) || typeof value.replayed !== "boolean" || !uploadMatches(value.upload, pending)) {
    throw new Error("unverified_upload_receipt");
  }
  return value.upload;
}
export async function getUploadStatus(signal?: AbortSignal): Promise<BackupUploadStatus> {
  return readUploadStatus(await api<unknown>(uploadBase, { signal }));
}
export async function createUpload(pending: PendingUpload, csrf: string): Promise<BackupUpload> {
  return readUploadReceipt(await mutation<unknown>(uploadBase,
    { event_id: pending.event_id, size_bytes: pending.size_bytes, sha256: pending.sha256 }, csrf), pending);
}
export async function controlUpload(pending: PendingUpload, csrf: string): Promise<BackupUpload> {
  if (!id.test(pending.upload_id) || pending.intent === "upload") throw new Error("invalid_upload_control");
  return readUploadReceipt(await mutation<unknown>(uploadBase + "/" + pending.upload_id + "/" + pending.intent,
    { action: pending.intent }, csrf), pending);
}
export function uploadPendingKey(actorID: string): string { return "dark-pro-backup-upload-v1:" + actorID; }
function validPending(value: unknown, actorID: string): value is PendingUpload {
  return object(value) && Object.keys(value).length === 7 && value.version === 1 && value.actor_id === actorID && actor.test(actorID) &&
    typeof value.event_id === "string" && event.test(value.event_id) && integer(value.size_bytes, 1, maxUploadBytes) &&
    typeof value.sha256 === "string" && sha.test(value.sha256) && typeof value.upload_id === "string" &&
    (value.upload_id === "" || id.test(value.upload_id)) && ["upload", "cancel", "remove"].includes(value.intent as string) &&
    (value.intent === "upload" || value.upload_id !== "");
}
export function loadPendingUpload(actorID: string): PendingUpload | null {
  try {
    const raw = sessionStorage.getItem(uploadPendingKey(actorID));
    if (!raw || raw.length > 4096) return null;
    const value: unknown = JSON.parse(raw);
    return validPending(value, actorID) ? value : null;
  } catch { return null; }
}
export function savePendingUpload(pending: PendingUpload): boolean {
  if (!validPending(pending, pending.actor_id)) return false;
  try {
    const raw = JSON.stringify(pending);
    sessionStorage.setItem(uploadPendingKey(pending.actor_id), raw);
    return sessionStorage.getItem(uploadPendingKey(pending.actor_id)) === raw;
  } catch { return false; }
}
export function clearPendingUpload(pending: PendingUpload): void {
  try {
    const current = loadPendingUpload(pending.actor_id);
    if (current?.event_id === pending.event_id && current.intent === pending.intent) sessionStorage.removeItem(uploadPendingKey(pending.actor_id));
  } catch { /* A retained receipt is reconciled again; it is never resent automatically. */ }
}
export function pendingFromUpload(upload: BackupUpload, intent: PendingUpload["intent"] = "upload"): PendingUpload {
  return { version: 1, actor_id: upload.actor_id, event_id: upload.event_id, size_bytes: upload.size_bytes,
    sha256: upload.sha256, upload_id: upload.id, intent };
}
export const retryableUpload = (upload: BackupUpload): boolean => ["awaiting_upload", "interrupted", "rejected"].includes(upload.state);
export function uploadDownloadURL(upload: BackupUpload): string {
  if (!validBackupUpload(upload) || !upload.downloadable) throw new Error("invalid_upload_download");
  return uploadBase + "/" + upload.id + "/download";
}

export function hashUploadFile(file: File, signal: AbortSignal, progress: (bytes: number) => void): Promise<string> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) { reject(new DOMException("Cancelled", "AbortError")); return; }
    const worker = new Worker(new URL("./nativeBackupUploadHash.worker.ts", import.meta.url), { type: "module" });
    let done = false;
    const finish = (hash?: string, error?: Error) => {
      if (done) return;
      done = true; worker.terminate(); signal.removeEventListener("abort", abort);
      if (hash) resolve(hash); else reject(error ?? new Error("file_hash_failed"));
    };
    const abort = () => finish(undefined, new DOMException("Cancelled", "AbortError"));
    signal.addEventListener("abort", abort, { once: true });
    worker.onerror = () => finish();
    worker.onmessageerror = () => finish();
    worker.onmessage = (event: MessageEvent<unknown>) => {
      const value = event.data;
      if (object(value) && value.type === "progress" && integer(value.bytes, 0, file.size)) progress(value.bytes);
      else if (object(value) && value.type === "complete" && typeof value.sha256 === "string" && sha.test(value.sha256)) finish(value.sha256);
      else finish();
    };
    try { worker.postMessage({ file }); } catch { finish(); }
  });
}

// XHR sends the File directly, giving bounded-memory browser transfer progress.
// These bytes are not a durable server acknowledgement; only its typed receipt
// can confirm publication. Metadata API's eight-second timeout is not reused.
export function sendUploadContent(file: File, pending: PendingUpload, csrf: string, signal: AbortSignal,
  progress: (bytes: number) => void, sent: () => void): Promise<BackupUpload> {
  return new Promise((resolve, reject) => {
    if (!id.test(pending.upload_id) || file.size !== pending.size_bytes || signal.aborted) {
      reject(new DOMException("Cancelled or invalid upload", "AbortError")); return;
    }
    const xhr = new XMLHttpRequest();
    let done = false;
    let idle: number | undefined;
    const finish = (upload?: BackupUpload, error?: Error) => {
      if (done) return;
      done = true; window.clearTimeout(idle); signal.removeEventListener("abort", abort);
      if (upload) resolve(upload); else reject(error ?? new ApiError(0, "upload_response_unconfirmed"));
    };
    const abort = () => { xhr.abort(); finish(undefined, new DOMException("Cancelled", "AbortError")); };
    const idleWatch = () => {
      window.clearTimeout(idle);
      idle = window.setTimeout(() => { xhr.abort(); finish(); }, 90_000);
    };
    xhr.open("PUT", uploadBase + "/" + pending.upload_id + "/content");
    xhr.setRequestHeader("Content-Type", "application/octet-stream");
    xhr.setRequestHeader("Accept", "application/json");
    xhr.setRequestHeader("X-CSRF-Token", csrf);
    xhr.setRequestHeader("X-Backup-SHA256", pending.sha256);
    // The server bounds a transfer to six hours; allow its terminal receipt
    // a short additional response window without implying unlimited waiting.
    xhr.timeout = 6 * 60 * 60 * 1000 + 30_000;
    xhr.upload.onprogress = event => { progress(Math.min(file.size, event.loaded)); idleWatch(); };
    xhr.upload.onload = () => { window.clearTimeout(idle); progress(file.size); sent(); };
    xhr.onprogress = event => { if (event.loaded > (64 << 10)) { xhr.abort(); finish(); } };
    xhr.onerror = xhr.ontimeout = () => finish();
    xhr.onabort = () => finish(undefined, new DOMException("Cancelled", "AbortError"));
    xhr.onload = () => {
      if (xhr.status === 401) window.dispatchEvent(new Event("dark-pro-session-revoked"));
      if (xhr.status !== 200) { finish(undefined, new ApiError(xhr.status, "upload_response_unconfirmed")); return; }
      try {
        if (xhr.responseText.length > (64 << 10)) throw new Error("invalid_upload_response");
        finish(readUploadReceipt(JSON.parse(xhr.responseText), pending));
      } catch { finish(); }
    };
    signal.addEventListener("abort", abort, { once: true });
    idleWatch();
    xhr.send(file);
  });
}
