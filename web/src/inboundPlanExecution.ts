import { assertPublicInboundData } from "./inboundTLS";

export const promotionScope = "single_direct_tcp_reality_loopback" as const;
export const maxPromotionClients = 500;
export type PromotionIssue = { node_id?: string; field: string; code: string };
export type PromotionInput = { event_id: string; expected_plan_revision: number; expected_inbound_revision: number; client_ids: string[]; reason: string };
export type PromotionRequest = PromotionInput & { review_sha256: string; confirmed: true };
export type PromotionPreview = {
  eligible: boolean; scope: typeof promotionScope; inbound_id: number; plan_revision: number; inbound_revision: number;
  node_id: string; installation_id: string; direct_port: number; direct_hosts: string[]; client_ids: string[];
  review_sha256: string; blockers: PromotionIssue[]; runtime_changed: false; subscription_published: false;
};
export type PromotionReceipt = {
  event_id: string; inbound_id: number; plan_revision: number; source_inbound_revision: number; inbound_revision: number;
  node_id: string; installation_id: string; draft_revision: number; client_ids: string[]; review_sha256: string;
  promoted: true; runtime_changed: false; subscription_published: false; created_unix: number; replayed: boolean;
};
export type PromotionDiscovery = { available: boolean; scope: typeof promotionScope; promoted: boolean; blockers: PromotionIssue[]; receipt?: PromotionReceipt };
export type PromotionAttempt = { inbound_id: number; body: string; review: PromotionPreview };
export type PromotionClient = { id: string; name: string; archived: boolean; deployment_state: string; expires_unix: number };
export type PromotionClientPage = { clients: PromotionClient[]; offset: number; limit: number; has_more: boolean };

const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === "object" && !Array.isArray(value);
const positive = (value: unknown): value is number => Number.isSafeInteger(value) && Number(value) > 0;
const hex = (value: unknown, length: number) => typeof value === "string" && new RegExp(`^[a-f0-9]{${length}}$`).test(value);
const identity = (value: unknown): value is string => typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(value);
const eventIdentity = (value: unknown): value is string => typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/.test(value);
const cleanText = (value: unknown, limit: number): value is string => typeof value === "string" && [...value].length <= limit && !/[\u0000-\u001f\u007f-\u009f]/.test(value);
const clientIDs = (value: unknown, allowEmpty = false): value is string[] => Array.isArray(value) && (allowEmpty || value.length > 0) && value.length <= maxPromotionClients && value.every(identity) && new Set(value).size === value.length && value.every((id, index) => index === 0 || value[index - 1] < id);
const same = (left: string[], right: string[]) => left.length === right.length && left.every((item, index) => item === right[index]);
export const promotionPath = (inboundID: number) => `/api/v1/workspace/native-inbound-plans/${inboundID}/promotion`;

export function readPromotionIssues(value: unknown): PromotionIssue[] {
  if (!Array.isArray(value) || value.length > 1024 || value.some(issue => !object(issue) || typeof issue.field !== "string" || !/^[a-z_0-9.]{1,80}$/.test(issue.field) || typeof issue.code !== "string" || !/^[a-z_]{3,80}$/.test(issue.code) || issue.node_id !== undefined && !identity(issue.node_id))) throw Error("invalid_promotion_issues");
  return value as PromotionIssue[];
}
export function readPromotionReceipt(value: unknown, inboundID: number, expected?: PromotionAttempt): PromotionReceipt {
  assertPublicInboundData(value);
  if (!object(value) || value.inbound_id !== inboundID || !positive(inboundID) || !eventIdentity(value.event_id) || !positive(value.plan_revision) || !positive(value.source_inbound_revision) || value.inbound_revision !== value.source_inbound_revision + 1 || value.draft_revision !== 1 ||
    !identity(value.node_id) || !hex(value.installation_id, 32) || !clientIDs(value.client_ids) || !hex(value.review_sha256, 64) || value.promoted !== true || value.runtime_changed !== false || value.subscription_published !== false || !positive(value.created_unix) || value.created_unix > 253402300799 || typeof value.replayed !== "boolean") throw Error("invalid_promotion_receipt");
  const receipt = value as PromotionReceipt;
  if (expected) {
    const body = JSON.parse(expected.body) as PromotionRequest, review = expected.review;
    if (expected.inbound_id !== inboundID || receipt.event_id !== body.event_id || receipt.plan_revision !== body.expected_plan_revision || receipt.source_inbound_revision !== body.expected_inbound_revision || receipt.node_id !== review.node_id || receipt.installation_id !== review.installation_id || receipt.review_sha256 !== body.review_sha256 || !same(receipt.client_ids, body.client_ids)) throw Error("mismatched_promotion_receipt");
  }
  return receipt;
}
export function readPromotionDiscovery(value: unknown, inboundID: number): PromotionDiscovery {
  assertPublicInboundData(value);
  if (!object(value) || value.scope !== promotionScope || typeof value.available !== "boolean" || typeof value.promoted !== "boolean") throw Error("invalid_promotion_discovery");
  const blockers = readPromotionIssues(value.blockers);
  const receipt = value.receipt === undefined ? undefined : readPromotionReceipt(value.receipt, inboundID);
  if (value.promoted !== !!receipt || !value.available && !value.promoted && blockers.length === 0) throw Error("invalid_promotion_discovery");
  return { available: value.available, scope: promotionScope, promoted: value.promoted, blockers, receipt };
}
export function readPromotionPreview(value: unknown, inboundID: number, intent: PromotionInput): PromotionPreview {
  assertPublicInboundData(value);
  if (!object(value) || value.scope !== promotionScope || value.inbound_id !== inboundID || value.plan_revision !== intent.expected_plan_revision || value.inbound_revision !== intent.expected_inbound_revision || typeof value.eligible !== "boolean" || value.runtime_changed !== false || value.subscription_published !== false || !clientIDs(value.client_ids) || !same(value.client_ids, intent.client_ids)) throw Error("invalid_promotion_preview");
  const blockers = readPromotionIssues(value.blockers);
  if (value.eligible && (blockers.length > 0 || !identity(value.node_id) || !hex(value.installation_id, 32) || !Number.isSafeInteger(value.direct_port) || Number(value.direct_port) < 1024 || Number(value.direct_port) > 65535 || [2087, 8443, 24443].includes(Number(value.direct_port)) || !Array.isArray(value.direct_hosts) || value.direct_hosts.length < 1 || value.direct_hosts.length > 64 || value.direct_hosts.some(host => !cleanText(host, 253) || !host) || !hex(value.review_sha256, 64))) throw Error("invalid_promotion_preview");
  if (!value.eligible && blockers.length === 0) throw Error("invalid_promotion_preview");
  return value as PromotionPreview;
}
export function readPromotionAttempt(raw: string | null, inboundID: number): PromotionAttempt | null {
  if (!raw) return null;
  try {
    const value = JSON.parse(raw);
    assertPublicInboundData(value);
    if (!object(value) || value.inbound_id !== inboundID || typeof value.body !== "string" || value.body.length > 100000) return null;
    const body = JSON.parse(value.body);
    if (!object(body) || !eventIdentity(body.event_id) || !positive(body.expected_plan_revision) || !positive(body.expected_inbound_revision) || !clientIDs(body.client_ids) || !cleanText(body.reason, 200) || !body.reason.trim() || body.reason !== body.reason.trim() || body.confirmed !== true || !hex(body.review_sha256, 64) || Object.keys(body).sort().join(",") !== "client_ids,confirmed,event_id,expected_inbound_revision,expected_plan_revision,reason,review_sha256") return null;
    const review = readPromotionPreview(value.review, inboundID, body as PromotionInput);
    if (!review.eligible || review.review_sha256 !== body.review_sha256) return null;
    return { inbound_id: inboundID, body: value.body, review };
  } catch { return null; }
}
export function readPromotionClientPage(value: unknown, offset: number): PromotionClientPage {
  assertPublicInboundData(value);
  if (!object(value) || value.limit !== 25 || value.offset !== offset || typeof value.has_more !== "boolean" || !Array.isArray(value.clients) || value.clients.length > 25 || value.clients.some(client => !object(client) || !identity(client.id) || !cleanText(client.name, 128) || typeof client.archived !== "boolean" || !cleanText(client.deployment_state, 80) || !positive(client.expires_unix)) || new Set(value.clients.map(client => client.id)).size !== value.clients.length) throw Error("invalid_promotion_clients");
  return value as PromotionClientPage;
}
