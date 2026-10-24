// Owner-only public metadata. Never deserialize/re-sign an Agent command here:
// its nanosecond fields and HMAC belong to the HUB/Agent transport exclusively.
export type Condition = { highwater: number; saved_sequence: number; saved_sha: string; active_sequence: number; active_sha: string; process_ready: boolean };
export type Observation = Condition & { phase: string; lease_expires_unix_nano: number; data_plane_verified: false; subscription_published: false; no_listeners: boolean };
export type Action = "stage" | "apply" | "rollback" | "renew" | "drain";
export type DraftRef = { inbound_id: number; inbound_revision: number; draft_revision: number };
export type Seal = { epoch_id: string; sequence: string; sha256: string };
export type MeteredCondition = { epoch_id?: string; epoch_sha?: string; admission_sha?: string; settlement: Seal };
export type MeteredEvidence = { after: MeteredCondition; prior_settlement: Seal };
export type CommandRequest = { usage: MeteredCondition; id: string; action: Action; stage_id: string; runtime_instance: string; expected: Condition; drafts: DraftRef[] };
export type Command = {
 registered_epoch_id?: string;
 id: string; node_id: string; installation_id: string; serial: number; action: Action; stage_id: string;
 state: "queued" | "delivered" | "acknowledged" | "failed" | "acknowledged_stale" | "abandoned_unknown";
 created_unix_nano: number;
 receipt?: { version: 2; metered: MeteredEvidence; command_id: string; serial: number; action: Action; stage_id: string; node_id: string; installation_id: string; runtime_instance: string; outcome: string; observed_unix_nano: number; observed: Observation; signature: string };
};
export type Scope = { enabled: boolean; health_enabled?: boolean; health_rounds_enabled?: boolean; initial_apply_health_enabled?: boolean; lab_subscriptions_enabled?: boolean; consumer_manifests_enabled?: boolean; scope: "metered_tcp_loopback_lab"; node_id?: string; installation_id?: string; nodes?: {node_id:string;installation_id:string}[] };
export type Target = { stage_id: string; ports: number[]; expires_unix_milli?: number };
export type Channel = {

 handover_id?: string;
 node_id: string; installation_id: string; binary_sha: string; reported_at_unix_nano: number; observation_fresh: boolean;
 server_time_unix_milli: number; drafts_valid: boolean;
 status: { usage?: MeteredCondition; instance: string; serial: number; staged_id?: string; current_stage_id?: string; previous_stage_id?: string; interrupted_command_sha?: string; observed: Observation };
 review: (DraftRef & { name: string; listen_ip: string; direct_port: number; tunnel_port: number })[];
 controls: { stage: boolean; apply?: Target; rollback?: Target; renew?: Target; drain?: Target };
 commands: Command[];
};
export type Attempt = { node_id: string; installation_id: string; action: Action | "abandon"; id: string; body: string };
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const hex = (v: unknown, length: number): v is string => typeof v === "string" && new RegExp(`^[a-f0-9]{${length}}$`).test(v);
const natural = (v: unknown): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= 0;
const nano = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v) && v >= 0;
const sha = (v: unknown) => v === "" || hex(v, 64);
const port = (v: unknown): v is number => natural(v) && v <= 65535;
const actions: string[] = ["stage", "apply", "rollback", "renew", "drain"];
function condition(v: unknown): v is Condition {
 return object(v) && natural(v.highwater) && natural(v.saved_sequence) && natural(v.active_sequence) && sha(v.saved_sha) && sha(v.active_sha) && typeof v.process_ready === "boolean";
}
function observation(v: unknown): v is Observation {
 return object(v) && typeof v.phase === "string" && v.phase.length <= 80 && nano(v.lease_expires_unix_nano) &&
  v.data_plane_verified === false && v.subscription_published === false && typeof v.no_listeners === "boolean" && condition(v);
}
function refs(v: unknown): v is DraftRef[] {
 return Array.isArray(v) && v.length <= 256 && v.every(r => object(r) && natural(r.inbound_id) && r.inbound_id > 0 && natural(r.inbound_revision) && r.inbound_revision > 0 && natural(r.draft_revision)) && new Set(v.map(r => r.inbound_id)).size === v.length;
}
export function readScope(v: unknown): Scope {
 if(object(v)&&((v.consumer_manifests_enabled!==undefined&&typeof v.consumer_manifests_enabled!=="boolean")||(v.consumer_manifests_enabled===true&&v.lab_subscriptions_enabled!==true)))throw Error("invalid_consumer_manifests_scope");
 if(object(v)&&((v.lab_subscriptions_enabled!==undefined&&typeof v.lab_subscriptions_enabled!=="boolean")||(v.lab_subscriptions_enabled===true&&v.health_rounds_enabled!==true&&v.initial_apply_health_enabled!==true)))throw Error("invalid_lab_subscriptions_scope");
 if(object(v)&&((v.initial_apply_health_enabled!==undefined&&typeof v.initial_apply_health_enabled!=="boolean")||(v.initial_apply_health_enabled===true&&v.health_enabled!==true)))throw Error("invalid_initial_apply_health_scope");
 if(object(v)&&v.health_enabled!==undefined&&typeof v.health_enabled!=="boolean")throw Error("invalid_health_scope");
 if(object(v)&&((v.health_rounds_enabled!==undefined&&typeof v.health_rounds_enabled!=="boolean")||(v.health_rounds_enabled===true&&v.health_enabled!==true)))throw Error("invalid_health_rounds_scope");
 if (!object(v) || typeof v.enabled !== "boolean" || v.scope !== "metered_tcp_loopback_lab" || (v.enabled &&
  (typeof v.node_id !== "string" || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(v.node_id) || !hex(v.installation_id, 32)))) throw Error("invalid_route_scope");
 if(v.nodes!==undefined){
  if(!Array.isArray(v.nodes)||v.nodes.length>64||v.enabled!==(v.nodes.length>0)||
   !v.nodes.every(n=>object(n)&&typeof n.node_id==="string"&&/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(n.node_id)&&hex(n.installation_id,32))||
   new Set(v.nodes.map(n=>n.node_id)).size!==v.nodes.length||
   (v.enabled&&!v.nodes.some(n=>n.node_id===v.node_id&&n.installation_id===v.installation_id)))throw Error("invalid_route_scopes");
 }
 if(v.initial_apply_health_enabled===true&&(!v.enabled||(Array.isArray(v.nodes)?v.nodes.length:1)!==1||v.health_rounds_enabled===true||v.consumer_manifests_enabled===true))throw Error("invalid_initial_apply_node_scope");
 return v as Scope;
}
export function readCommand(v: unknown, scope: Scope): Command {
 if (object(v) && v.registered_epoch_id !== undefined && !hex(v.registered_epoch_id,32)) throw Error("invalid_registered_epoch");
 if (!object(v) || !hex(v.id, 32) || v.node_id !== scope.node_id || v.installation_id !== scope.installation_id || !natural(v.serial) || v.serial === 0 ||
  !actions.includes(String(v.action)) || !hex(v.stage_id, 32) || !["queued", "delivered", "acknowledged", "failed", "acknowledged_stale", "abandoned_unknown"].includes(String(v.state)) || !nano(v.created_unix_nano)) throw Error("invalid_route_command");
 if (v.receipt !== undefined) {
  const a = v.receipt;
  if (!object(a) || a.version !== 2 || !object(a.metered) || !meteredCondition(a.metered.after) || !seal(a.metered.prior_settlement) || a.command_id !== v.id || a.serial !== v.serial || a.action !== v.action || a.stage_id !== v.stage_id || a.node_id !== v.node_id || a.installation_id !== v.installation_id || !hex(a.runtime_instance, 32) || !hex(a.signature, 64) || !nano(a.observed_unix_nano) || !observation(a.observed) ||
   !["staged", "applied", "rolled_back", "renewed", "drained", "failed_stopped", "failed_previous_retained"].includes(String(a.outcome))) throw Error("invalid_route_receipt");
 }
 if (["acknowledged", "failed", "acknowledged_stale"].includes(String(v.state)) && !v.receipt) throw Error("missing_route_receipt");
 return v as Command;
}
export function readChannel(v: unknown, scope: Scope): Channel {
 if (!object(v) || v.node_id !== scope.node_id || v.installation_id !== scope.installation_id || !hex(v.binary_sha, 64) || !nano(v.reported_at_unix_nano) ||
  !natural(v.server_time_unix_milli) || v.server_time_unix_milli === 0 || typeof v.observation_fresh !== "boolean" || typeof v.drafts_valid !== "boolean" ||
  !object(v.status) || !natural(v.status.serial) || !(v.status.instance === "" && v.reported_at_unix_nano === 0 || hex(v.status.instance, 32)) || !observation(v.status.observed) ||
  !refs(v.review) || !Array.isArray(v.commands) || v.commands.length > 50 || !object(v.controls) || typeof v.controls.stage !== "boolean") throw Error("invalid_route_channel");
 if (v.reported_at_unix_nano !== 0 && !meteredCondition(v.status.usage)) throw Error("missing_metered_condition");
 for (const id of [v.status.staged_id, v.status.current_stage_id, v.status.previous_stage_id]) if (id !== undefined && !hex(id, 32)) throw Error("invalid_route_stage");
 if (v.status.interrupted_command_sha !== undefined && !hex(v.status.interrupted_command_sha, 64)) throw Error("invalid_route_recovery");
 for (const row of v.review) {
  const r = row as unknown as Record<string, unknown>;
  if (typeof r.name !== "string" || r.name.length > 128 || !["127.0.0.1", "::1"].includes(String(r.listen_ip)) || !port(r.direct_port) || !port(r.tunnel_port)) throw Error("invalid_route_review");
 }
 if (v.controls.stage && (!v.drafts_valid || !v.review.length || v.review.some(r => r.draft_revision === 0))) throw Error("incomplete_route_review");
 for (const action of ["apply", "rollback", "renew", "drain"]) {
  const t = v.controls[action];
  if (t !== undefined && (!object(t) || !hex(t.stage_id, 32) || !Array.isArray(t.ports) || t.ports.length > 512 || !t.ports.every(p => port(p) && p > 0) || (action === "apply" && !natural(t.expires_unix_milli)))) throw Error("invalid_route_control");
 }
 v.commands.forEach(c => readCommand(c, scope));
 if (new Set(v.commands.map(c => c.id)).size !== v.commands.length) throw Error("duplicate_route_command");
 return v as Channel;
}
export function expected(v: Observation): Condition {
 return { highwater: v.highwater, saved_sequence: v.saved_sequence, saved_sha: v.saved_sha, active_sequence: v.active_sequence, active_sha: v.active_sha, process_ready: v.process_ready };
}
// Retry identity survives page reload in this tab. No credentials, private
// config or signed Agent packet is stored; retries always require another click.
export function readAttempt(raw: string | null, scope: Scope): Attempt | null {
 if (!raw || raw.length > 65536) return null;
 try {
  const a: unknown = JSON.parse(raw);
  if (!object(a) || a.node_id !== scope.node_id || a.installation_id !== scope.installation_id || !hex(a.id, 32) || typeof a.body !== "string") return null;
  const b: unknown = JSON.parse(a.body);
  if (!object(b)) return null;
  if (a.action === "abandon") {
   if (b.confirm_execution_may_have_occurred !== true || Object.keys(b).length !== 1) return null;
  } else if (!actions.includes(String(a.action)) || b.id !== a.id || b.action !== a.action || !hex(b.runtime_instance, 32) || !meteredCondition(b.usage) || !condition(b.expected) || !refs(b.drafts) || (a.action === "stage" ? b.stage_id !== "" || !b.drafts.length || b.drafts.some(r => r.draft_revision === 0) : !hex(b.stage_id, 32) || b.drafts.length !== 0)) return null;
  return a as Attempt;
 } catch { return null; }
}

function seal(v: unknown): v is Seal {
 if (!object(v) || typeof v.sequence !== "string" || !/^(0|[1-9][0-9]{0,19})$/.test(v.sequence)) return false;
 if (v.sequence === "0") return v.epoch_id === "" && v.sha256 === "";
 return BigInt(v.sequence) <= 18446744073709551615n && hex(v.epoch_id,32) && hex(v.sha256,64);
}
export function meteredCondition(v: unknown): v is MeteredCondition {
 if (!object(v) || !seal(v.settlement) || (v.admission_sha !== undefined && !hex(v.admission_sha,64))) return false;
 if (v.epoch_id === undefined) return v.epoch_sha === undefined && v.settlement.sequence === "0";
 return hex(v.epoch_id,32) && hex(v.epoch_sha,64) && hex(v.admission_sha,64) &&
  (v.settlement.sequence === "0" || v.settlement.epoch_id === v.epoch_id);
}
