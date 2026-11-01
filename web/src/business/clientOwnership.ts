import { api, mutation } from "../session";

export const ownershipBase = "/api/v1/workspace/clients/ownership";
export const ownershipID = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/;
const event = /^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/;
const hash = /^[a-f0-9]{64}$/;
const obj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const integer = (v: unknown, min = 0): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= min;
const optionalID = (v: unknown): v is string => typeof v === "string" && (v === "" || ownershipID.test(v));
const amount = (v: unknown): v is string => typeof v === "string" && /^(0|[1-9][0-9]{0,18})$/.test(v) && BigInt(v) <= 9223372036854775807n;

export type OwnershipIntent = { client_ids: string[]; source_group_id: string; representative_id: string; group_id: string; reason: string };
export type OwnershipRequest = OwnershipIntent & { event_id: string; quote_hash: string; quote_unix: number };
export type OwnershipItem = {
  client_id: string; name: string; previous_representative_id: string; representative_id: string;
  previous_group_id: string; group_id: string; revision: number; usage_present: boolean; usage_complete: boolean;
  uplink_bytes: string; downlink_bytes: string; reconciliation_holds: number;
};
export type OwnershipQuote = {
  intent: OwnershipIntent; quote_hash: string; quote_unix: number; count: number; clients: OwnershipItem[];
  refund_policy: "original_funder_only_mixed_funding_requires_review"; balance_changed: false; usage_reset: false;
};
export type OwnershipReceipt = {
  event_id: string; actor_user_id: string; representative_id: string; group_id: string; count: number;
  clients: OwnershipItem[]; created_unix: number; replayed: boolean;
  balance_changed: false; usage_reset: false; credentials_changed: false;
};
export type PendingOwnership = { version: 1; actor_id: string; request: OwnershipRequest; client_ids: string[] };

function validIDs(v: unknown, allowEmpty = false): v is string[] {
  return Array.isArray(v) && v.length <= 500 && (allowEmpty || v.length > 0) && v.every(id => typeof id === "string" && ownershipID.test(id)) && new Set(v).size === v.length;
}
export function validOwnershipIntent(v: unknown): v is OwnershipIntent & Record<string, unknown> {
  return obj(v) && validIDs(v.client_ids, true) && optionalID(v.source_group_id) &&
    ((v.client_ids.length > 0) !== (v.source_group_id !== "")) && typeof v.representative_id === "string" && ownershipID.test(v.representative_id) &&
    optionalID(v.group_id) && typeof v.reason === "string" && v.reason.trim() === v.reason && v.reason.length > 0 && [...v.reason].length <= 200 && !/[\u0000-\u001f\u007f]/.test(v.reason);
}
function validItem(v: unknown): v is OwnershipItem {
  return obj(v) && typeof v.client_id === "string" && ownershipID.test(v.client_id) && typeof v.name === "string" &&
    typeof v.previous_representative_id === "string" && ownershipID.test(v.previous_representative_id) &&
    typeof v.representative_id === "string" && ownershipID.test(v.representative_id) && v.previous_representative_id !== v.representative_id &&
    optionalID(v.previous_group_id) && optionalID(v.group_id) && integer(v.revision, 1) &&
    typeof v.usage_present === "boolean" && typeof v.usage_complete === "boolean" && amount(v.uplink_bytes) && amount(v.downlink_bytes) &&
    BigInt(v.uplink_bytes) + BigInt(v.downlink_bytes) <= 9223372036854775807n && integer(v.reconciliation_holds) &&
    (v.usage_present || !v.usage_complete && v.uplink_bytes === "0" && v.downlink_bytes === "0" && v.reconciliation_holds === 0);
}
function equalIntent(a: OwnershipIntent, b: OwnershipIntent): boolean {
  return [...a.client_ids].sort().join("\n") === [...b.client_ids].sort().join("\n") && a.source_group_id === b.source_group_id &&
    a.representative_id === b.representative_id && a.group_id === b.group_id && a.reason === b.reason;
}
function validMembers(v: unknown, count: unknown, intent: OwnershipIntent, expected?: string[]): v is OwnershipItem[] {
  if (!Array.isArray(v) || !integer(count, 1) || count > 500 || v.length !== count || !v.every(validItem) || new Set(v.map(c => c.client_id)).size !== count) return false;
  if (v.some(c => c.representative_id !== intent.representative_id || c.group_id !== intent.group_id || intent.source_group_id !== "" && c.previous_group_id !== intent.source_group_id)) return false;
  const ids = expected ?? (intent.client_ids.length ? intent.client_ids : null);
  return !ids || [...ids].sort().join("\n") === v.map(c => c.client_id).sort().join("\n");
}
export function readOwnershipQuote(v: unknown, intent: OwnershipIntent): OwnershipQuote {
  if (!obj(v) || !validOwnershipIntent(v.intent) || Object.keys(v.intent).length !== 5 || !equalIntent(v.intent, intent) || typeof v.quote_hash !== "string" || !hash.test(v.quote_hash) ||
    !integer(v.quote_unix, 1) || !validMembers(v.clients, v.count, intent) || v.balance_changed !== false || v.usage_reset !== false ||
    v.refund_policy !== "original_funder_only_mixed_funding_requires_review") throw new Error("invalid_ownership_quote");
  return v as OwnershipQuote;
}
export function validOwnershipRequest(v: unknown): v is OwnershipRequest {
  return validOwnershipIntent(v) && obj(v) && Object.keys(v).length === 8 && typeof v.event_id === "string" && event.test(v.event_id) &&
    typeof v.quote_hash === "string" && hash.test(v.quote_hash) && integer(v.quote_unix, 1);
}
export function readOwnershipReceipt(v: unknown, pending: PendingOwnership): OwnershipReceipt {
  const q = pending.request;
  if (!obj(v) || v.event_id !== q.event_id || v.actor_user_id !== pending.actor_id || v.representative_id !== q.representative_id || v.group_id !== q.group_id ||
    !validMembers(v.clients, v.count, q, pending.client_ids) || !integer(v.created_unix, 1) || typeof v.replayed !== "boolean" ||
    v.balance_changed !== false || v.usage_reset !== false || v.credentials_changed !== false) throw new Error("invalid_ownership_receipt");
  return v as OwnershipReceipt;
}
export const ownershipStorageKey = (actor: string): string => "dark-pro-client-ownership-v1:" + actor;
export function loadOwnershipPending(actor: string): PendingOwnership | null {
  try {
    const raw = sessionStorage.getItem(ownershipStorageKey(actor)); if (!raw || raw.length > 100_000) return null;
    const v: unknown = JSON.parse(raw);
    if (!obj(v) || Object.keys(v).length !== 4 || v.version !== 1 || v.actor_id !== actor || !ownershipID.test(actor) || !validOwnershipRequest(v.request) || !validIDs(v.client_ids)) return null;
    return v as PendingOwnership;
  } catch { return null; }
}
export function saveOwnershipPending(v: PendingOwnership): boolean {
  try { const raw = JSON.stringify(v); sessionStorage.setItem(ownershipStorageKey(v.actor_id), raw); return sessionStorage.getItem(ownershipStorageKey(v.actor_id)) === raw; } catch { return false; }
}
export function clearOwnershipPending(v: PendingOwnership): void {
  try { if (loadOwnershipPending(v.actor_id)?.request.event_id === v.request.event_id) sessionStorage.removeItem(ownershipStorageKey(v.actor_id)); } catch { /* Reconcile the same receipt next time. */ }
}
export async function quoteOwnership(intent: OwnershipIntent, csrf: string, signal?: AbortSignal): Promise<OwnershipQuote> {
  const v = await api<unknown>(ownershipBase + "/quote", { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(intent), signal });
  return readOwnershipQuote(v, intent);
}
export async function commitOwnership(pending: PendingOwnership, csrf: string): Promise<OwnershipReceipt> {
  return readOwnershipReceipt(await mutation<unknown>(ownershipBase + "/commit", pending.request, csrf), pending);
}
export async function findOwnershipReceipt(pending: PendingOwnership, signal?: AbortSignal): Promise<OwnershipReceipt> {
  return readOwnershipReceipt(await api<unknown>(ownershipBase + "/receipts/" + encodeURIComponent(pending.request.event_id), { signal }), pending);
}
