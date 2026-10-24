import { assertPublicInboundData } from "./inboundTLS";
import type { PromotionReceipt } from "./inboundPlanExecution";
import { readInitialLabSnapshot, type InitialLabSnapshot } from "./labSubscriptions";

export const initialHealthScope = "lab_initial_apply" as const;
export type InitialHealthBlocker = { field: string; code: string };
export type InitialHealthPreparation = {
 event_id: string; promotion_event_id: string; inbound_id: number; node_id: string; installation_id: string;
 prepared: true; created_unix_nano: number; replayed: boolean; server_time_unix_milli?: number;
};
export type InitialHealthBinding = { initial_apply_sha: string; apply_command_id: string; stage_id: string; epoch_id: string };
export type InitialHealthMember = { inbound_id: number; intent_id: string; port: number; state: "pending" | "issued" | "accepted"; challenge_id?: string; accepted_unix_nano?: number };
export type InitialHealthRound = InitialHealthBinding & {
 id: string; scope: typeof initialHealthScope; inbound_id: number; promotion_event_id: string; node_id: string; installation_id: string;
 endpoint_sha: string; manifest_sha: string; created_unix_nano: number; expires_unix_nano: number;
 members: [InitialHealthMember]; state: "pending" | "complete" | "expired" | "state_changed"; current: boolean; replayed: boolean;
 server_time_unix_milli?: number;
};
export type InitialHealthState = { enabled: false; scope: typeof initialHealthScope; inbound_id: number; server_time_unix_milli: number; blockers: InitialHealthBlocker[] } | {
 enabled: true; available: boolean; scope: typeof initialHealthScope; inbound_id: number; promotion_event_id: string; node_id: string; installation_id: string;
 direct_port: number; inbound_revision: number; draft_revision: number;
 prepared: boolean; can_prepare: boolean; eligible: boolean; blockers: InitialHealthBlocker[]; preparation?: InitialHealthPreparation;
 initial_apply_sha?: string; apply_command_id?: string; stage_id?: string; epoch_id?: string; client_ids: string[];
 latest_round?: InitialHealthRound; server_time_unix_milli: number;
};
export type InitialHealthPrepareBody = { event_id: string; promotion_event_id: string; confirmed: true };
export type InitialHealthRoundBody = { id: string; initial_apply_sha: string; confirmed: true };
export type InitialHealthSnapshotBody = { event_id: string; client_id: string; round_id: string; initial_apply_sha: string; expected_revision: number; expected_delivery_revision: number; confirmed: true };
export type InitialHealthAttempt =
 { action: "prepare"; body: InitialHealthPrepareBody } |
 { action: "round"; body: InitialHealthRoundBody; binding: InitialHealthBinding; client_id: string; token_generation: number; expected_revision: number; expected_delivery_revision: number } |
 { action: "snapshot"; body: InitialHealthSnapshotBody; round: InitialHealthRound };

const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const positive = (v: unknown): v is number => Number.isSafeInteger(v) && Number(v) > 0;
const natural = (v: unknown): v is number => Number.isSafeInteger(v) && Number(v) >= 0 && Number(v) < Number.MAX_SAFE_INTEGER;
const hex = (v: unknown, length = 32): v is string => typeof v === "string" && new RegExp(`^[a-f0-9]{${length}}$`).test(v);
const nano = (v: unknown): v is number => typeof v === "number" && Number.isInteger(v) && v > 0 && v <= 9_223_372_036_854_775_000;
const milli = (v: unknown): v is number => positive(v) && v <= 9_223_372_036_854;
const eventID = (v: unknown): v is string => typeof v === "string" && /^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/.test(v);
const sameKeys = (v: Record<string, unknown>, keys: string[]) => Object.keys(v).every(k => keys.includes(k));
const identityKeys = ["inbound_id", "promotion_event_id", "node_id", "installation_id"];
const bindingKeys = ["initial_apply_sha", "apply_command_id", "stage_id", "epoch_id"];
const roundKeys = ["id", "scope", ...identityKeys, ...bindingKeys, "endpoint_sha", "manifest_sha", "created_unix_nano", "expires_unix_nano", "members", "state", "current", "replayed", "server_time_unix_milli"];
const preparationKeys = ["event_id", ...identityKeys, "prepared", "created_unix_nano", "replayed", "server_time_unix_milli"];

export const initialHealthPath = (id: number) => `/api/v1/workspace/native-inbound-plans/${id}/initial-health`;
export const sameInitialBinding = (a: Partial<InitialHealthBinding>, b: Partial<InitialHealthBinding>) => bindingKeys.every(k => a[k as keyof InitialHealthBinding] === b[k as keyof InitialHealthBinding]);
export function initialHealthBinding(v: Partial<InitialHealthBinding>): InitialHealthBinding | null {
 return hex(v.initial_apply_sha, 64) && hex(v.apply_command_id) && hex(v.stage_id) && hex(v.epoch_id)
  ? { initial_apply_sha: v.initial_apply_sha, apply_command_id: v.apply_command_id, stage_id: v.stage_id, epoch_id: v.epoch_id } : null;
}
function samePromotion(v: Record<string, unknown>, p: PromotionReceipt) {
 return v.inbound_id === p.inbound_id && v.promotion_event_id === p.event_id && v.node_id === p.node_id && v.installation_id === p.installation_id;
}
function blockers(v: unknown): InitialHealthBlocker[] {
 if (!Array.isArray(v) || v.length > 256 || v.some(b => !object(b) || !sameKeys(b, ["field", "code"]) || typeof b.field !== "string" || !/^[a-z_0-9.]{1,80}$/.test(b.field) || typeof b.code !== "string" || !/^[a-z_]{3,80}$/.test(b.code))) throw Error("invalid_initial_health_blockers");
 return v as InitialHealthBlocker[];
}
export function readInitialHealthPreparation(v: unknown, p: PromotionReceipt, expected?: InitialHealthPrepareBody): InitialHealthPreparation {
 assertPublicInboundData(v);
 if (!object(v) || !sameKeys(v, preparationKeys) || !samePromotion(v, p) || !hex(v.event_id) || v.prepared !== true || !nano(v.created_unix_nano) || typeof v.replayed !== "boolean" || v.server_time_unix_milli !== undefined && !milli(v.server_time_unix_milli) || expected && (v.event_id !== expected.event_id || v.promotion_event_id !== expected.promotion_event_id)) throw Error("invalid_initial_health_preparation");
 return v as InitialHealthPreparation;
}
export function readInitialHealthRound(v: unknown, p: PromotionReceipt, expected?: { id: string; binding: InitialHealthBinding }): InitialHealthRound {
 assertPublicInboundData(v);
 if (!object(v) || !sameKeys(v, roundKeys) || v.scope !== initialHealthScope || !samePromotion(v, p) || !hex(v.id) || !initialHealthBinding(v) || !hex(v.endpoint_sha, 64) || !hex(v.manifest_sha, 64) || !nano(v.created_unix_nano) || !nano(v.expires_unix_nano) || v.expires_unix_nano <= v.created_unix_nano || v.expires_unix_nano - v.created_unix_nano > 8e9 || !["pending", "complete", "expired", "state_changed"].includes(String(v.state)) || typeof v.current !== "boolean" || typeof v.replayed !== "boolean" || v.current && v.state !== "complete" || !Array.isArray(v.members) || v.members.length !== 1 || v.server_time_unix_milli !== undefined && !milli(v.server_time_unix_milli)) throw Error("invalid_initial_health_round");
 const m = v.members[0];
 if (!object(m) || !sameKeys(m, ["inbound_id", "intent_id", "port", "state", "challenge_id", "accepted_unix_nano"]) || m.inbound_id !== p.inbound_id || !hex(m.intent_id) || !positive(m.port) || m.port < 1024 || m.port > 65535 || [2087, 8443, 24443].includes(m.port) || !["pending", "issued", "accepted"].includes(String(m.state)) || m.challenge_id !== undefined && !hex(m.challenge_id) || m.state !== "accepted" && m.accepted_unix_nano !== undefined || m.state === "issued" && !hex(m.challenge_id) || m.state === "accepted" && (!hex(m.challenge_id) || !nano(m.accepted_unix_nano) || m.accepted_unix_nano < v.created_unix_nano || m.accepted_unix_nano >= v.expires_unix_nano) || v.state === "complete" && m.state !== "accepted" || v.state === "pending" && m.state === "accepted") throw Error("invalid_initial_health_member");
 const round = v as InitialHealthRound;
 if (expected && (round.id !== expected.id || !sameInitialBinding(round, expected.binding))) throw Error("mismatched_initial_health_round");
 return round;
}
export function readInitialHealthState(v: unknown, p: PromotionReceipt): InitialHealthState {
 assertPublicInboundData(v);
 if (!object(v) || v.scope !== initialHealthScope || v.inbound_id !== p.inbound_id || typeof v.enabled !== "boolean" || !milli(v.server_time_unix_milli)) throw Error("invalid_initial_health_scope");
 const issues = blockers(v.blockers);
 if (!v.enabled) {
  if (!sameKeys(v, ["enabled", "scope", "inbound_id", "server_time_unix_milli", "blockers"])) throw Error("invalid_initial_health_disabled");
  return v as InitialHealthState;
 }
 if (!sameKeys(v, ["enabled", "available", "scope", ...identityKeys, ...bindingKeys, "direct_port", "inbound_revision", "draft_revision", "prepared", "can_prepare", "eligible", "blockers", "preparation", "client_ids", "latest_round", "server_time_unix_milli"]) || !samePromotion(v, p) || v.inbound_revision !== p.inbound_revision || v.draft_revision !== p.draft_revision || !positive(v.direct_port) || v.direct_port < 1024 || v.direct_port > 65535 || [2087, 8443, 24443].includes(v.direct_port) || typeof v.available !== "boolean" || typeof v.prepared !== "boolean" || typeof v.can_prepare !== "boolean" || typeof v.eligible !== "boolean" || v.prepared !== (v.preparation !== undefined) || v.can_prepare && (v.prepared || !v.available) || v.eligible && (!v.prepared || !v.available || !initialHealthBinding(v) || issues.length !== 0) || !Array.isArray(v.client_ids) || v.client_ids.length !== p.client_ids.length || v.client_ids.some((id, i) => id !== p.client_ids[i])) throw Error("invalid_initial_health_state");
 if (bindingKeys.some(k => v[k] !== undefined) && !initialHealthBinding(v)) throw Error("incomplete_initial_health_binding");
 if (v.preparation !== undefined) readInitialHealthPreparation(v.preparation, p);
 if (v.latest_round !== undefined) {
  const round = readInitialHealthRound(v.latest_round, p);
  if (round.members[0].port !== v.direct_port || round.current && (!v.eligible || !sameInitialBinding(round, v))) throw Error("mismatched_current_initial_health");
 }
 return v as InitialHealthState;
}
export function readInitialHealthAttempt(raw: string | null, p: PromotionReceipt): InitialHealthAttempt | null {
 if (!raw) return null;
 if (raw.length > 20000) throw Error("invalid_retained_initial_health");
 const v: unknown = JSON.parse(raw);
 assertPublicInboundData(v);
 if (!object(v) || !object(v.body) || v.body.confirmed !== true) throw Error("invalid_retained_initial_health");
 const b = v.body;
 if (v.action === "prepare" && sameKeys(v, ["action", "body"]) && sameKeys(b, ["event_id", "promotion_event_id", "confirmed"]) && hex(b.event_id) && eventID(b.promotion_event_id) && b.promotion_event_id === p.event_id) return v as InitialHealthAttempt;
 if (v.action === "round" && sameKeys(v, ["action", "body", "binding", "client_id", "token_generation", "expected_revision", "expected_delivery_revision"]) && sameKeys(b, ["id", "initial_apply_sha", "confirmed"]) && hex(b.id) && hex(b.initial_apply_sha, 64) && object(v.binding) && sameKeys(v.binding, bindingKeys) && initialHealthBinding(v.binding) && v.binding.initial_apply_sha === b.initial_apply_sha && typeof v.client_id === "string" && p.client_ids.includes(v.client_id) && positive(v.token_generation) && natural(v.expected_revision) && natural(v.expected_delivery_revision)) return v as InitialHealthAttempt;
 if (v.action === "snapshot" && sameKeys(v, ["action", "body", "round"]) && sameKeys(b, ["event_id", "client_id", "round_id", "initial_apply_sha", "expected_revision", "expected_delivery_revision", "confirmed"]) && hex(b.event_id) && typeof b.client_id === "string" && p.client_ids.includes(b.client_id) && hex(b.round_id) && hex(b.initial_apply_sha, 64) && natural(b.expected_revision) && natural(b.expected_delivery_revision)) {
  const round = readInitialHealthRound(v.round, p);
  if (round.id === b.round_id && round.initial_apply_sha === b.initial_apply_sha && round.state === "complete" && round.current) return v as InitialHealthAttempt;
 }
 throw Error("invalid_retained_initial_health");
}
export function readInitialHealthSnapshot(v: unknown, p: PromotionReceipt, expected?: Extract<InitialHealthAttempt, { action: "snapshot" }>): InitialLabSnapshot {
 const snapshot = readInitialLabSnapshot(v, expected?.body.client_id ?? (object(v) && typeof v.client_id === "string" ? v.client_id : ""));
 if (snapshot.inbound_id !== p.inbound_id || snapshot.promotion_event_id !== p.event_id || snapshot.node_id !== p.node_id || snapshot.installation_id !== p.installation_id || !p.client_ids.includes(snapshot.client_id)) throw Error("mismatched_initial_snapshot_promotion");
 if (expected) {
  const { body, round } = expected;
  if (snapshot.event_id !== body.event_id || snapshot.round_id !== body.round_id || snapshot.initial_apply_sha !== body.initial_apply_sha || snapshot.revision !== body.expected_revision + 1 || snapshot.delivery_revision !== body.expected_delivery_revision + 1 || snapshot.created_unix_nano < round.created_unix_nano || snapshot.expires_unix_nano > round.expires_unix_nano || snapshot.round_id !== round.id || snapshot.initial_apply_sha !== round.initial_apply_sha) throw Error("mismatched_initial_snapshot_receipt");
 }
 return snapshot;
}
