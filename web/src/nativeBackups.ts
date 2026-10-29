import { api, mutation } from "./session";

export const backupBase = "/api/v1/workspace/owner/backups";
export const backupID = /^[a-f0-9]{32}$/;
const eventID = /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/;
const actorID = /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$/;
const hash = /^[a-f0-9]{64}$/;
const states = ["queued", "running", "cancelling", "succeeded", "failed", "cancelled", "recovery_required"] as const;
const phases = ["queued", "preflight", "stopping_hub", "creating_offline", "restarting_hub", "checking_archive", "retention", "complete"] as const;
const runners = ["ready", "busy", "recovery_required", "unavailable", "disabled"] as const;

export type BackupSchedule = {
  enabled: boolean; frequency: "daily" | "weekly"; time: string; timezone: string; weekday: number;
};
export type BackupRetention = { keep_last: number; max_age_days: number };
export type BackupSettings = { revision: number; schedule: BackupSchedule; retention: BackupRetention };
export type BackupJob = {
  id: string; event_id: string; actor_id: string; action: "create"; trigger: "manual" | "schedule";
  state: typeof states[number]; phase: typeof phases[number]; created_unix: number; started_unix: number;
  finished_unix: number; archive_id: string; cancel_requested: boolean; error_code: string; warning_code: string;
  downtime_ms: number; revision: number;
};
export type BackupArchive = {
  id: string; job_id: string; created_unix: number; size_bytes: number; payload_bytes: number; sha256: string;
  source_revision: string; schema_version: number; verification: "checksum_verified" | "restore_verified";
  protected: boolean; downloadable: boolean;
};
export type BackupStatus = {
  enabled: boolean; runner_status: typeof runners[number]; recipient_fingerprint: string;
  capabilities: { create: boolean; download: boolean; cancel: boolean; schedules: boolean; retention: boolean; upload: false; restore: false };
  settings: BackupSettings; next_run_unix: number; jobs: BackupJob[]; archives: BackupArchive[];
};
export type SettingsBody = { event_id: string; expected_revision: number; schedule: BackupSchedule; retention: BackupRetention };
export type PendingBackupRequest =
  | { version: 1; actor_id: string; kind: "create"; body: { event_id: string; action: "create" } }
  | { version: 1; actor_id: string; kind: "cancel"; job_id: string; body: { event_id: string } }
  | { version: 1; actor_id: string; kind: "settings"; body: SettingsBody };
export type BackupDraft = {
  enabled: boolean; frequency: "daily" | "weekly"; time: string; timezone: string; weekday: number;
  keepLast: string; maxAgeDays: string;
};

const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const integer = (v: unknown, min = 0, max = Number.MAX_SAFE_INTEGER): v is number =>
  typeof v === "number" && Number.isSafeInteger(v) && v >= min && v <= max;
const timestamp = (v: unknown): v is number => integer(v, 0, 253402300799);
const code = (v: unknown): v is string => typeof v === "string" && (v === "" || /^[a-z][a-z0-9_]{0,95}$/.test(v));
const idOrEmpty = (v: unknown): v is string => typeof v === "string" && (v === "" || backupID.test(v));
function timezone(v: unknown): v is string {
  if (typeof v !== "string" || !/^[A-Za-z0-9_+\-/]{1,64}$/.test(v)) return false;
  try { new Intl.DateTimeFormat("en", { timeZone: v }).format(0); return true; } catch { return false; }
}
export function validSchedule(v: unknown): v is BackupSchedule {
  return object(v) && typeof v.enabled === "boolean" && (v.frequency === "daily" || v.frequency === "weekly") &&
    typeof v.time === "string" && /^(?:[01]\d|2[0-3]):[0-5]\d$/.test(v.time) &&
    timezone(v.timezone) && integer(v.weekday, 0, 6);
}
export function validRetention(v: unknown): v is BackupRetention {
  return object(v) && integer(v.keep_last, 1, 100) && integer(v.max_age_days, 1, 365);
}
export function validSettings(v: unknown): v is BackupSettings {
  return object(v) && integer(v.revision) && validSchedule(v.schedule) && validRetention(v.retention);
}
export function validBackupJob(v: unknown): v is BackupJob {
  return object(v) && typeof v.id === "string" && backupID.test(v.id) &&
    typeof v.event_id === "string" && eventID.test(v.event_id) &&
    typeof v.actor_id === "string" && actorID.test(v.actor_id) && v.action === "create" &&
    (v.trigger === "manual" || v.trigger === "schedule") && states.includes(v.state as BackupJob["state"]) &&
    phases.includes(v.phase as BackupJob["phase"]) && timestamp(v.created_unix) && v.created_unix > 0 &&
    timestamp(v.started_unix) && timestamp(v.finished_unix) && idOrEmpty(v.archive_id) &&
    typeof v.cancel_requested === "boolean" && code(v.error_code) && code(v.warning_code) &&
    integer(v.downtime_ms) && integer(v.revision);
}
export function validBackupArchive(v: unknown): v is BackupArchive {
  return object(v) && typeof v.id === "string" && backupID.test(v.id) &&
    typeof v.job_id === "string" && backupID.test(v.job_id) && timestamp(v.created_unix) && v.created_unix > 0 &&
    integer(v.size_bytes, 1) && integer(v.payload_bytes) && typeof v.sha256 === "string" && hash.test(v.sha256) &&
    typeof v.source_revision === "string" && /^[a-f0-9]{40}$/.test(v.source_revision) &&
    integer(v.schema_version) && (v.verification === "checksum_verified" || v.verification === "restore_verified") &&
    typeof v.protected === "boolean" && typeof v.downloadable === "boolean";
}
export function readBackupStatus(v: unknown): BackupStatus {
  if (!object(v) || typeof v.enabled !== "boolean" || !runners.includes(v.runner_status as BackupStatus["runner_status"]) ||
      typeof v.recipient_fingerprint !== "string" || (v.recipient_fingerprint !== "" && !hash.test(v.recipient_fingerprint)) ||
      !object(v.capabilities) || !["create", "download", "cancel", "schedules", "retention"].every(k => typeof (v.capabilities as Record<string, unknown>)[k] === "boolean") ||
      v.capabilities.upload !== false || v.capabilities.restore !== false || !validSettings(v.settings) ||
      !timestamp(v.next_run_unix) || !Array.isArray(v.jobs) || v.jobs.length > 4096 || !v.jobs.every(validBackupJob) ||
      !Array.isArray(v.archives) || v.archives.length > 4096 || !v.archives.every(validBackupArchive) ||
      new Set(v.jobs.map(j => j.id)).size !== v.jobs.length || new Set(v.archives.map(a => a.id)).size !== v.archives.length) {
    throw new Error("invalid_backup_status");
  }
  if (v.enabled && !hash.test(v.recipient_fingerprint)) throw new Error("invalid_backup_status");
  return v as BackupStatus;
}
export async function getBackupStatus(signal?: AbortSignal): Promise<BackupStatus> {
  return readBackupStatus(await api<unknown>(backupBase, { signal }));
}
export async function sendBackupRequest(request: PendingBackupRequest, csrf: string): Promise<BackupJob | BackupSettings> {
  if (request.kind === "settings") {
    const value = await api<unknown>(backupBase + "/settings", {
      method: "PUT", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(request.body),
    });
    if (!object(value) || !validSettings(value.settings) || typeof value.replayed !== "boolean" ||
        value.settings.revision !== request.body.expected_revision + 1 ||
        !samePolicy(value.settings, request.body)) throw new Error("unverified_backup_receipt");
    return value.settings;
  }
  const path = request.kind === "create" ? backupBase + "/jobs" : backupBase + "/jobs/" + request.job_id + "/cancel";
  const value = await mutation<unknown>(path, request.body, csrf);
  if (!object(value) || !validBackupJob(value.job) || typeof value.replayed !== "boolean" ||
      (request.kind === "create" && (value.job.event_id !== request.body.event_id ||
        value.job.actor_id !== request.actor_id || value.job.trigger !== "manual")) ||
      (request.kind === "cancel" && (value.job.id !== request.job_id ||
        (!value.job.cancel_requested && jobActive(value.job))))) throw new Error("unverified_backup_receipt");
  return value.job;
}
export function samePolicy(a: { schedule: BackupSchedule; retention: BackupRetention }, b: { schedule: BackupSchedule; retention: BackupRetention }): boolean {
  return a.schedule.enabled === b.schedule.enabled && a.schedule.frequency === b.schedule.frequency &&
    a.schedule.time === b.schedule.time && a.schedule.timezone === b.schedule.timezone &&
    a.schedule.weekday === b.schedule.weekday && a.retention.keep_last === b.retention.keep_last &&
    a.retention.max_age_days === b.retention.max_age_days;
}
export const jobActive = (job: BackupJob): boolean => !["succeeded", "failed", "cancelled"].includes(job.state);
export const cancellable = (job: BackupJob): boolean =>
  ["queued", "running"].includes(job.state) && !job.cancel_requested;
export function draftFrom(settings: BackupSettings): BackupDraft {
  return { ...settings.schedule, keepLast: String(settings.retention.keep_last), maxAgeDays: String(settings.retention.max_age_days) };
}
export function policyFrom(draft: BackupDraft): { schedule: BackupSchedule; retention: BackupRetention } | null {
  if (!/^\d{1,3}$/.test(draft.keepLast) || !/^\d{1,3}$/.test(draft.maxAgeDays)) return null;
  const schedule: BackupSchedule = { enabled: draft.enabled, frequency: draft.frequency, time: draft.time, timezone: draft.timezone, weekday: draft.weekday };
  const retention = { keep_last: Number(draft.keepLast), max_age_days: Number(draft.maxAgeDays) };
  return validSchedule(schedule) && validRetention(retention) ? { schedule, retention } : null;
}
export function newBackupEvent(): string { return "backup-" + crypto.randomUUID(); }
export function pendingStorageKey(actor: string): string { return "dark-pro-backup-request-v1:" + actor; }
function validPending(value: unknown, actor: string): value is PendingBackupRequest {
  if (!object(value) || value.version !== 1 || value.actor_id !== actor || !object(value.body) ||
      typeof value.body.event_id !== "string" || !eventID.test(value.body.event_id)) return false;
  if (value.kind === "create") return value.body.action === "create" && Object.keys(value.body).length === 2;
  if (value.kind === "cancel") return typeof value.job_id === "string" && backupID.test(value.job_id) && Object.keys(value.body).length === 1;
  return value.kind === "settings" && Object.keys(value.body).length === 4 && integer(value.body.expected_revision) &&
    validSchedule(value.body.schedule) && validRetention(value.body.retention);
}
export function loadPending(actor: string): PendingBackupRequest | null {
  try {
    const raw = sessionStorage.getItem(pendingStorageKey(actor));
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    return validPending(parsed, actor) ? parsed : null;
  } catch { return null; }
}
export function savePending(actor: string, value: PendingBackupRequest): boolean {
  if (!validPending(value, actor)) return false;
  try {
    const raw = JSON.stringify(value);
    sessionStorage.setItem(pendingStorageKey(actor), raw);
    return sessionStorage.getItem(pendingStorageKey(actor)) === raw;
  } catch { return false; }
}
export function clearPending(actor: string, event: string): void {
  try {
    const stored = loadPending(actor);
    if (stored?.body.event_id === event) sessionStorage.removeItem(pendingStorageKey(actor));
  } catch { /* A stale receipt is resolved again through status, never sent automatically. */ }
}
export function resolvedPending(request: PendingBackupRequest, status: BackupStatus): BackupJob | null {
  if (request.kind === "create") return status.jobs.find(j => j.event_id === request.body.event_id &&
    j.actor_id === request.actor_id && j.trigger === "manual") ?? null;
  if (request.kind === "cancel") return status.jobs.find(j => j.id === request.job_id &&
    (j.cancel_requested || !jobActive(j))) ?? null;
  // Equal policy is not a receipt for this event: settings replay must return its original revision.
  return null;
}
export function archiveDownloadURL(archive: BackupArchive): string {
  if (!validBackupArchive(archive) || !archive.downloadable) throw new Error("invalid_backup_download");
  return backupBase + "/archives/" + archive.id + "/download";
}
