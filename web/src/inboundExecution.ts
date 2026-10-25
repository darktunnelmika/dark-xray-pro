import { api } from "./session";
import { assertPublicInboundData } from "./inboundTLS";

// A separate contract from temporary promotion/LAB receipts. Authoring remains
// paused; command admission and an observed running generation are distinct.
export const executionActions = ["activate", "apply", "stop", "restart", "archive"] as const;
export type ExecutionAction = typeof executionActions[number];
export type AccountingMode = "per_client" | "owner_aggregate";
export type ExecutionBlocker = { code: string; field?: string; node_id?: string; inbound_id?: number; client_id?: string };
export type ExecutionListener = {
  inbound_id: number; tag: string; engine: string; protocol: string; transport: string; security: string;
  kind: string; listen: string; port: number; network: string; resource_kind: string; accounting_mode: AccountingMode;
  dependency_node_id?: string;
  host_requirements?: Array<"tun" | "tun_system_dns" | "ipv6_alias">;
};
export type ExecutionHost = { version: 1; network_admin: boolean; child_network_admin: boolean; tun_device: boolean; ipv6: boolean; system_dns_control: boolean; tun_dns_activation: boolean };
export type ExecutionTarget = {
  node_id: string; installation_id: string; desired_generation: number; observed_generation: number; state: string;
  config_sha?: string; manifest_sha?: string; listeners: ExecutionListener[]; blockers: ExecutionBlocker[];
  reported_at_unix_nano?: number; lease_expires_unix_nano?: number; process_ready?: boolean;
  health_state?: string; accounting_state?: string;
  host?: ExecutionHost; host_reported_at_unix_nano?: number;
};
export type ExecutionOperation = {
  id: string; event_id: string; inbound_id: number; action: ExecutionAction; revision: number; state: string;
  created_unix_nano: number; updated_unix_nano: number; targets: ExecutionTarget[]; blockers: ExecutionBlocker[]; replayed: boolean;
};
export type ExecutionState = {
  version: 1; enabled: boolean; inbound_id: number; revision: number; plan_revision: number; inbound_revision: number;
  desired_revision: number; active_revision: number; desired_state: string; observed_state: string;
  accounting_mode: AccountingMode; client_ids: string[]; effective_client_ids: string[];
  targets: ExecutionTarget[]; allowed_actions: ExecutionAction[]; blockers: ExecutionBlocker[];
  latest_operation?: ExecutionOperation; server_time_unix_milli?: number; allowed_accounting_modes?: AccountingMode[];
  publication_state?: string; pending_changes?: boolean; active_plan_revision?: number; active_inbound_revision?: number;
  reconciliations?: ExecutionReconciliation[];
  owner_resume_required?: boolean;
};
export type ExecutionKnownUsage = { principal_id: string; inbound_id: number; client_id: string; role: string; upload: string; download: string; held: boolean };
export type ExecutionReconciliation = {
  epoch_id: string; node_id: string; installation_id: string; generation: number; revision: number; state: "uncertain";
  config_sha: string; manifest_sha: string; process_identity_sha: string; known_usage: ExecutionKnownUsage[];
  affected_client_ids: string[]; affected_inbound_ids: number[]; has_measured_usage: boolean;
  unknown_usage_policy: "unmeasured"; allowed_actions: Array<"accept_owner_loss">; blockers: ExecutionBlocker[];
};
export type ExecutionReconciliationInput = { event_id: string; epoch_id: string; expected_revision: number; action: "accept_owner_loss"; reason: string; review_sha?: string };
export type ExecutionReconciliationPreview = { valid: boolean; review_sha: string; state: ExecutionReconciliation; blockers: ExecutionBlocker[] };
export type ExecutionReconciliationReceipt = {
  event_id: string; epoch_id: string; inbound_id: number; node_id: string; installation_id: string; generation: number; actor_id: string;
  reason: string; revision: number; state: "accepted_owner_loss"; unknown_usage_policy: "owner_loss";
  known_usage: ExecutionKnownUsage[]; affected_client_ids: string[]; affected_inbound_ids: number[];
  has_measured_usage: boolean; created_unix_nano: number; replayed: boolean;
};
export type ExecutionInput = {
  event_id: string; action: ExecutionAction; expected_revision: number; expected_plan_revision: number;
  expected_inbound_revision: number; client_ids: string[]; accounting_mode: AccountingMode; reason: string;
};
export type ExecutionCommand = ExecutionInput & { review_sha: string };
export type ExecutionBindings = { event_id: string; expected_revision: number; client_ids: string[]; accounting_mode: AccountingMode; reason: string };
export type ExecutionPreview = { valid: boolean; review_sha: string; state: ExecutionState; action: ExecutionAction; targets: ExecutionTarget[]; blockers: ExecutionBlocker[] };
export type ExecutionAttempt = { inbound_id: number; kind: "command" | "clients"; body: string };
export type ExecutionClient = { id: string; name: string; archived: boolean; deployment_state: string; expires_unix: number };
export type ExecutionClientPage = { clients: ExecutionClient[]; offset: number; limit: number; has_more: boolean };
export const maxExecutionClients = 20000;

const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const natural = (v: unknown): v is number => Number.isSafeInteger(v) && Number(v) >= 0;
const positive = (v: unknown): v is number => natural(v) && v > 0;
const nano = (v: unknown): v is number => typeof v === "number" && Number.isInteger(v) && v >= 0 && v <= 9_223_372_036_854_775_000;
const identifier = (v: unknown): v is string => typeof v === "string" && /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/.test(v);
const eventID = (v: unknown): v is string => identifier(v) && v.length >= 16;
const code = (v: unknown): v is string => typeof v === "string" && /^[a-z][a-z_0-9.-]{0,99}$/.test(v);
const text = (v: unknown, max: number): v is string => typeof v === "string" && [...v].length <= max && !/[\u0000-\u001f\u007f-\u009f]/.test(v);
const sha = (v: unknown): v is string => typeof v === "string" && /^[a-f0-9]{64}$/.test(v);
const action = (v: unknown): v is ExecutionAction => executionActions.includes(v as ExecutionAction);
const accounting = (v: unknown): v is AccountingMode => v === "per_client" || v === "owner_aggregate";
const optional = (v: unknown, check: (v: unknown) => boolean) => v === undefined || check(v);
const unique = <T,>(values: T[]) => new Set(values).size === values.length;
export const sameExecutionClients = (a: string[], b: string[]) => {
  if (a.length !== b.length) return false;
  const left = [...a].sort(), right = [...b].sort();
  return left.every((id, i) => id === right[i]);
};
const clients = (v: unknown): v is string[] => Array.isArray(v) && v.length <= maxExecutionClients && v.every(identifier) && unique(v);
export const executionPath = (inboundID: number) => {
  if (!positive(inboundID)) throw Error("invalid_execution_identity");
  return `/api/v1/workspace/native-inbounds/${inboundID}/execution`;
};

/** Status, reviews and retained attempts never contain credentials or bundles. */
export function assertExecutionPublic(value: unknown): void {
  assertPublicInboundData(value);
  const visit = (v: unknown) => {
    if (Array.isArray(v)) { v.forEach(visit); return; }
    if (!object(v)) return;
    for (const [key, item] of Object.entries(v)) {
      if (["uuid", "password", "token", "secret", "credentials", "config", "nativeconfig", "components", "principals", "signature", "agenttoken"].includes(key.replace(/[_-]/g, "").toLowerCase())) throw Error("private_execution_response");
      visit(item);
    }
  };
  visit(value);
}
function readBlockers(v: unknown): ExecutionBlocker[] {
  if (!Array.isArray(v) || v.length > 4096 || v.some(b => !object(b) || !code(b.code) || !optional(b.field, v => typeof v === "string" && /^[A-Za-z_0-9.[\]-]{1,256}$/.test(v)) ||
    !optional(b.node_id, identifier) || !optional(b.client_id, identifier) || !optional(b.inbound_id, positive))) throw Error("invalid_execution_blockers");
  return v as ExecutionBlocker[];
}
function readTargets(v: unknown): ExecutionTarget[] {
  if (!Array.isArray(v) || v.length > 128) throw Error("invalid_execution_targets");
  for (const target of v) {
    if (!object(target) || !identifier(target.node_id) || typeof target.installation_id !== "string" || !/^[a-f0-9]{32}$/.test(target.installation_id) ||
      !natural(target.desired_generation) || !natural(target.observed_generation) || !code(target.state) || !optional(target.config_sha, v => v === "" || sha(v)) || !optional(target.manifest_sha, v => v === "" || sha(v)) ||
      !optional(target.reported_at_unix_nano, nano) || !optional(target.lease_expires_unix_nano, nano) || !optional(target.process_ready, v => typeof v === "boolean") ||
      !optional(target.health_state, v => v === "" || code(v)) || !optional(target.accounting_state, v => v === "" || code(v)) || !optional(target.host_reported_at_unix_nano, nano) || !Array.isArray(target.listeners) || target.listeners.length > 8192) throw Error("invalid_execution_target");
    const host = target.host;
    if (host !== undefined && (!object(host) || host.version !== 1 ||
      ["network_admin", "child_network_admin", "tun_device", "ipv6", "system_dns_control", "tun_dns_activation"].some(key => typeof host[key] !== "boolean") || !nano(target.host_reported_at_unix_nano) || target.host_reported_at_unix_nano <= 0)) throw Error("invalid_execution_host");
    readBlockers(target.blockers);
    for (const listener of target.listeners) {
      if (!object(listener) || !positive(listener.inbound_id) || !text(listener.tag, 256) || !text(listener.engine, 64) || !text(listener.protocol, 64) || !text(listener.transport, 64) || !text(listener.security, 64) ||
        !text(listener.kind, 64) || !text(listener.listen, 255) || !natural(listener.port) || listener.port > 65535 || !text(listener.network, 64) || !text(listener.resource_kind, 64) || !accounting(listener.accounting_mode) || !optional(listener.dependency_node_id, identifier) ||
        !optional(listener.host_requirements, values => Array.isArray(values) && values.length <= 3 && values.every(value => ["tun", "tun_system_dns", "ipv6_alias"].includes(value)) && unique(values))) throw Error("invalid_execution_listener");
    }
  }
  if (!unique(v.map(target => target.node_id))) throw Error("duplicate_execution_target");
  return v as ExecutionTarget[];
}
export function readExecutionOperation(v: unknown, inboundID: number, input?: ExecutionCommand): ExecutionOperation {
  assertExecutionPublic(v);
  if (!object(v) || v.inbound_id !== inboundID || !eventID(v.id) || !eventID(v.event_id) || v.id !== v.event_id || !action(v.action) || !positive(v.revision) || !code(v.state) ||
    !nano(v.created_unix_nano) || !nano(v.updated_unix_nano) || v.updated_unix_nano < v.created_unix_nano || typeof v.replayed !== "boolean" ||
    input && (v.event_id !== input.event_id || v.action !== input.action || v.revision !== input.expected_revision + 1)) throw Error("invalid_execution_operation");
  readTargets(v.targets); readBlockers(v.blockers);
  return v as ExecutionOperation;
}
export function readExecutionState(v: unknown, inboundID: number): ExecutionState {
  assertExecutionPublic(v);
  if (!object(v) || v.version !== 1 || typeof v.enabled !== "boolean" || v.inbound_id !== inboundID || !natural(v.revision) || !natural(v.plan_revision) || !natural(v.inbound_revision) ||
    !natural(v.desired_revision) || !natural(v.active_revision) || !code(v.desired_state) || !code(v.observed_state) || !accounting(v.accounting_mode) || !clients(v.client_ids) || !clients(v.effective_client_ids) ||
    !Array.isArray(v.allowed_actions) || !v.allowed_actions.every(action) || !unique(v.allowed_actions) || !optional(v.server_time_unix_milli, positive) ||
    !optional(v.allowed_accounting_modes, v => Array.isArray(v) && v.length <= 2 && v.every(accounting) && unique(v)) || !optional(v.publication_state, code) ||
    !optional(v.pending_changes, v => typeof v === "boolean") || !optional(v.owner_resume_required, v => typeof v === "boolean") || !optional(v.active_plan_revision, natural) || !optional(v.active_inbound_revision, natural)) throw Error("invalid_execution_state");
  readTargets(v.targets); readBlockers(v.blockers);
  if (v.latest_operation !== undefined) readExecutionOperation(v.latest_operation, inboundID);
  if (v.reconciliations !== undefined) {
    if (!Array.isArray(v.reconciliations) || v.reconciliations.length > 4096) throw Error("invalid_execution_reconciliations");
    v.reconciliations.forEach(item => readExecutionReconciliation(item, inboundID));
    if (!unique(v.reconciliations.map(item => item.epoch_id))) throw Error("duplicate_execution_reconciliation");
  }
  return v as ExecutionState;
}

const uint64 = (value: unknown): value is string => typeof value === "string" && /^(?:0|[1-9][0-9]{0,19})$/.test(value) && BigInt(value) <= 18446744073709551615n;
function readKnownUsage(v: Record<string, unknown>, inboundID: number): void {
  if (!Array.isArray(v.known_usage) || v.known_usage.length > 200000 || v.known_usage.some(row => !object(row) || !identifier(row.principal_id) || !positive(row.inbound_id) ||
    !(row.client_id === "" || identifier(row.client_id)) || !code(row.role) || !uint64(row.upload) || !uint64(row.download) || typeof row.held !== "boolean") ||
    !unique(v.known_usage.map(row => row.principal_id)) || !clients(v.affected_client_ids) || !Array.isArray(v.affected_inbound_ids) ||
    v.affected_inbound_ids.length > 20000 || !v.affected_inbound_ids.every(positive) || !v.affected_inbound_ids.includes(inboundID) || !unique(v.affected_inbound_ids) ||
    typeof v.has_measured_usage !== "boolean" || !v.has_measured_usage && v.known_usage.length !== 0) throw Error("invalid_reconciliation_usage");
  const inbounds = new Set(v.affected_inbound_ids), affected = new Set(v.affected_client_ids);
  if (v.known_usage.some(row => !inbounds.has(row.inbound_id) || row.held && (!row.client_id || !affected.has(row.client_id)))) throw Error("reconciliation_usage_scope");
}
export function readExecutionReconciliation(raw: unknown, inboundID: number): ExecutionReconciliation {
  assertExecutionPublic(raw);
  if (!object(raw) || !identifier(raw.epoch_id) || !identifier(raw.node_id) || typeof raw.installation_id !== "string" || !/^[a-f0-9]{32}$/.test(raw.installation_id) ||
    !positive(raw.generation) || !natural(raw.revision) || raw.state !== "uncertain" || raw.unknown_usage_policy !== "unmeasured" || !sha(raw.config_sha) || !sha(raw.manifest_sha) || !sha(raw.process_identity_sha) ||
    !Array.isArray(raw.allowed_actions) || raw.allowed_actions.some(value => value !== "accept_owner_loss") || !unique(raw.allowed_actions)) throw Error("invalid_execution_reconciliation");
  readKnownUsage(raw, inboundID); readBlockers(raw.blockers);
  return raw as ExecutionReconciliation;
}
export function readExecutionReconciliationInput(raw: unknown, reviewed = false): ExecutionReconciliationInput {
  assertExecutionPublic(raw);
  if (!object(raw) || !eventID(raw.event_id) || !identifier(raw.epoch_id) || !natural(raw.expected_revision) || raw.action !== "accept_owner_loss" ||
    !text(raw.reason, 512) || !raw.reason.trim() || (reviewed ? !sha(raw.review_sha) : !optional(raw.review_sha, sha)) ||
    Object.keys(raw).some(key => !["event_id", "epoch_id", "expected_revision", "action", "reason", "review_sha"].includes(key))) throw Error("invalid_reconciliation_input");
  return raw as ExecutionReconciliationInput;
}
export function readExecutionReconciliationPreview(raw: unknown, inboundID: number, input: ExecutionReconciliationInput): ExecutionReconciliationPreview {
  assertExecutionPublic(raw);
  if (!object(raw) || typeof raw.valid !== "boolean" || typeof raw.review_sha !== "string" || raw.valid && !sha(raw.review_sha)) throw Error("invalid_reconciliation_preview");
  const state = readExecutionReconciliation(raw.state, inboundID), blockers = readBlockers(raw.blockers);
  if (state.epoch_id !== input.epoch_id || state.revision !== input.expected_revision || raw.valid && (blockers.length > 0 || state.blockers.length > 0 || !state.allowed_actions.includes("accept_owner_loss"))) throw Error("changed_reconciliation_preview");
  return raw as ExecutionReconciliationPreview;
}
export function readExecutionReconciliationReceipt(raw: unknown, inboundID: number, actorID: string, input: ExecutionReconciliationInput): ExecutionReconciliationReceipt {
  assertExecutionPublic(raw);
  if (!object(raw) || raw.event_id !== input.event_id || raw.epoch_id !== input.epoch_id || raw.inbound_id !== inboundID || raw.actor_id !== actorID || raw.reason !== input.reason || raw.revision !== input.expected_revision ||
    raw.state !== "accepted_owner_loss" || raw.unknown_usage_policy !== "owner_loss" || !identifier(raw.node_id) || typeof raw.installation_id !== "string" || !/^[a-f0-9]{32}$/.test(raw.installation_id) ||
    !positive(raw.generation) || !nano(raw.created_unix_nano) || typeof raw.replayed !== "boolean") throw Error("invalid_reconciliation_receipt");
  readKnownUsage(raw, inboundID);
  return raw as ExecutionReconciliationReceipt;
}
export function readExecutionPreview(v: unknown, inboundID: number, input: ExecutionInput): ExecutionPreview {
  assertExecutionPublic(v);
  if (!object(v) || typeof v.valid !== "boolean" || v.action !== input.action || typeof v.review_sha !== "string" || v.valid && !sha(v.review_sha)) throw Error("invalid_execution_preview");
  const state = readExecutionState(v.state, inboundID);
  if (state.revision !== input.expected_revision || state.plan_revision !== input.expected_plan_revision || state.inbound_revision !== input.expected_inbound_revision ||
    !sameExecutionClients(state.client_ids, input.client_ids) || state.accounting_mode !== input.accounting_mode) throw Error("changed_execution_review");
  const blockers = readBlockers(v.blockers); readTargets(v.targets);
  if (v.valid && blockers.length || !v.valid && !blockers.length) throw Error("invalid_execution_review_blockers");
  return v as ExecutionPreview;
}
export function readExecutionAttempt(raw: string | null, inboundID: number): ExecutionAttempt | null {
  try {
    if (!raw || raw.length > 3_000_000) return null;
    const v = JSON.parse(raw); assertExecutionPublic(v);
    if (!object(v) || v.inbound_id !== inboundID || !["command", "clients"].includes(String(v.kind)) || typeof v.body !== "string") return null;
    const b = JSON.parse(v.body); assertExecutionPublic(b);
    if (!object(b) || !eventID(b.event_id) || !natural(b.expected_revision) || !clients(b.client_ids) || !accounting(b.accounting_mode) || !text(b.reason, 200) || !b.reason.trim()) return null;
    const fields = ["event_id", "expected_revision", "client_ids", "accounting_mode", "reason"];
    if (v.kind === "command") {
      if (!action(b.action) || !natural(b.expected_plan_revision) || !natural(b.expected_inbound_revision) || !sha(b.review_sha)) return null;
      fields.push("action", "expected_plan_revision", "expected_inbound_revision", "review_sha");
    }
    if (Object.keys(b).some(key => !fields.includes(key))) return null;
    return v as ExecutionAttempt;
  } catch { return null; }
}
export function readExecutionClientPage(v: unknown, offset: number): ExecutionClientPage {
  assertExecutionPublic(v);
  if (!object(v) || v.limit !== 25 || v.offset !== offset || typeof v.has_more !== "boolean" || !Array.isArray(v.clients) || v.clients.length > 25 ||
    v.clients.some(c => !object(c) || !identifier(c.id) || !text(c.name, 128) || typeof c.archived !== "boolean" || !text(c.deployment_state, 80) || !natural(c.expires_unix)) || !unique(v.clients.map(c => c.id))) throw Error("invalid_execution_clients");
  return v as ExecutionClientPage;
}
export function executionAPI<T>(path: string, signal: AbortSignal, body?: string, csrf?: string): Promise<T> {
  if (!/^\/api\/v1\/workspace\/native-inbounds\/\d+\/execution(?:\/|$)/.test(path)) throw Error("invalid_execution_path");
  return api<T>(path, { signal, method: body === undefined ? "GET" : "POST", ...(body === undefined ? {} : {
    body, headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf ?? "" },
  }) });
}
export function targetEvidence(target: ExecutionTarget, serverNow: number): "ready" | "running" | "pending" | "stale" | "stopped" | "failed" {
  if (["failed", "blocked"].includes(target.state)) return "failed";
  if (["stopped", "archived"].includes(target.state)) return "stopped";
  if (target.accounting_state === "stale") return "stale";
  const reported = (target.reported_at_unix_nano ?? 0) / 1e6, expires = (target.lease_expires_unix_nano ?? 0) / 1e6;
  if (reported > 0 && (reported > serverNow + 1000 || serverNow - reported > 15000) || expires > 0 && expires <= serverNow) return "stale";
  const executing = target.state === "active" && target.desired_generation > 0 && target.observed_generation === target.desired_generation && target.process_ready === true &&
    reported > 0 && expires > serverNow && ["admitted", "accepted"].includes(target.accounting_state ?? "") && target.blockers.length === 0;
  if (!executing) return "pending";
  if (["healthy", "verified"].includes(target.health_state ?? "")) return "ready";
  return target.health_state === "engine_ready" ? "running" : "pending";
}
export function executionDisplayState(state: ExecutionState, serverNow: number, stale: boolean): string {
  if (stale) return "stale";
  if (state.observed_state !== "active") return state.observed_state;
  const evidence = state.targets.map(target => targetEvidence(target, serverNow));
  if (evidence.some(item => item === "stale")) return "stale";
  if (evidence.some(item => item === "failed")) return "degraded";
  if (evidence.length > 0 && evidence.every(item => item === "ready")) return "active";
  return evidence.length > 0 && evidence.every(item => item === "ready" || item === "running") ? "running" : "verifying";
}
