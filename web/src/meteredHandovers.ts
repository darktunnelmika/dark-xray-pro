import { meteredCondition, readAttempt, readCommand, type Command, type MeteredCondition, type Scope } from "./meteredRouteTransactions";
import { consumerAddress, readConsumerRef } from "./consumerManifests";

export type Pair = { source_node: string; target_node: string; source_installation: string; target_installation: string };
export type Plan = Pair & { id: string; source_stage_id: string; target_stage_id: string; source_usage: MeteredCondition };
export type HealthIntent = {id:string;handover_id:string;node_id:string;installation_id:string;inbound_id:number;endpoint_sha:string;expires_unix_nano:number};
export type HealthEvidence = {challenge_id:string;handover_id:string;request_sha:string;receipt_sha:string;accepted_unix_nano:number;replayed:boolean};
export type HealthRoundMember = { inbound_id: number; intent_id: string; port: number; state: "pending" | "accepted"; challenge_id?: string; accepted_unix_nano?: number;consumer_address?:string;consumer_port?:number };
export type HealthRound = {
 id: string; handover_id: string; node_id: string; installation_id: string; endpoint_sha: string; manifest_sha: string;
 created_unix_nano: number; expires_unix_nano: number; members: HealthRoundMember[];
 state: "pending" | "complete" | "expired" | "state_changed"; current: boolean;
 consumer_manifest_id?:string;consumer_manifest_revision?:number;consumer_manifest_sha?:string;
};
export type Handover = Plan & { created_unix_nano: number; drain_command_id: string; apply_command_id: string; cancelled: boolean; can_publish: false;
 phase: "awaiting_source_drain" | "source_drain_pending" | "source_drained" | "target_apply_pending" | "target_applied_health_required" | "recovery_required" | "cancelled";
 drain_command?: Command; apply_command?: Command;health_intents?:HealthIntent[];historical_health_evidence?:HealthEvidence;health_rounds?:HealthRound[] };
export type Page = { operations: Handover[]; next_cursor: string };
export type HandoverAttempt = { pair: Pair; operation: string; action: "create" | "stage" | "drain" | "apply" | "cancel" | "health" | "health-round"; body: string };
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const hex = (v: unknown, n: number): v is string => typeof v === "string" && new RegExp(`^[a-f0-9]{${n}}$`).test(v);
const node = (v: unknown): v is string => typeof v === "string" && /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(v);
const phases = ["awaiting_source_drain", "source_drain_pending", "source_drained", "target_apply_pending", "target_applied_health_required", "recovery_required", "cancelled"];
export function readHealthIntent(v:unknown,op:string,target:string,install:string):HealthIntent{
 if(!object(v)||!hex(v.id,32)||v.handover_id!==op||v.node_id!==target||v.installation_id!==install||typeof v.inbound_id!=="number"||!Number.isSafeInteger(v.inbound_id)||v.inbound_id<=0||!hex(v.endpoint_sha,64)||typeof v.expires_unix_nano!=="number"||!Number.isFinite(v.expires_unix_nano)||v.expires_unix_nano<=0)throw Error("invalid_health_intent");return v as HealthIntent;
}
// These are audit facts, never credentials or a signed Agent packet. A partial
// or inconsistent response cannot be promoted to a complete current round.
export function readHealthRound(v: unknown, op: string, target: string, install: string): HealthRound {
 const nano = (n: unknown): n is number => typeof n === "number" && Number.isInteger(n) && n > 0 && n <= 9_223_372_036_854_775_000;
 if (!object(v) || !hex(v.id,32) || v.handover_id !== op || v.node_id !== target || v.installation_id !== install ||
  !hex(v.endpoint_sha,64) || !hex(v.manifest_sha,64) || !nano(v.created_unix_nano) || !nano(v.expires_unix_nano) || v.expires_unix_nano <= v.created_unix_nano || v.expires_unix_nano-v.created_unix_nano>8e9 ||
  !["pending","complete","expired","state_changed"].includes(String(v.state)) || typeof v.current !== "boolean" ||
  !Array.isArray(v.members) || v.members.length === 0 || v.members.length > 256) throw Error("invalid_health_round");
 const consumer=readConsumerRef(v);
 for (const m of v.members) {
  if (!object(m) || typeof m.inbound_id !== "number" || !Number.isSafeInteger(m.inbound_id) || m.inbound_id <= 0 || !hex(m.intent_id,32) ||
   typeof m.port !== "number" || !Number.isSafeInteger(m.port) || m.port <= 0 || m.port > 65535 || !["pending","accepted"].includes(String(m.state)) ||
   (m.challenge_id !== undefined && !hex(m.challenge_id,32)) ||
   (m.state === "accepted" ? !hex(m.challenge_id,32) || !nano(m.accepted_unix_nano) || m.accepted_unix_nano < v.created_unix_nano || m.accepted_unix_nano >= v.expires_unix_nano : m.accepted_unix_nano !== undefined)) throw Error("invalid_health_round_member");
  if(consumer? !consumerAddress(m.consumer_address)||typeof m.consumer_port!=="number"||!Number.isSafeInteger(m.consumer_port)||m.consumer_port<1024||m.consumer_port>65535 : m.consumer_address!==undefined||m.consumer_port!==undefined)throw Error("invalid_health_consumer_member");
 }
 const members = v.members as HealthRoundMember[];
 if (new Set(members.map(m=>m.inbound_id)).size !== members.length || new Set(members.map(m=>m.intent_id)).size !== members.length ||
  new Set(members.map(m=>m.port)).size !== members.length || new Set(members.filter(m=>m.challenge_id).map(m=>m.challenge_id)).size !== members.filter(m=>m.challenge_id).length) throw Error("duplicate_health_round_member");
 const complete = members.every(m=>m.state === "accepted");
 if(consumer&&new Set(members.map(m=>m.consumer_address+":"+m.consumer_port)).size!==members.length)throw Error("duplicate_health_consumer_endpoint");
 if ((v.state === "complete" && !complete) || (v.state === "pending" && complete) || (v.current && (v.state !== "complete" || !complete))) throw Error("invalid_health_round_coverage");
 return v as HealthRound;
}
function pair(v: unknown): v is Pair & Record<string, unknown> {
 return object(v) && node(v.source_node) && node(v.target_node) && v.source_node !== v.target_node && hex(v.source_installation,32) && hex(v.target_installation,32);
}
function plan(v: unknown): v is Plan & Record<string, unknown> {
 return pair(v) && object(v) && hex(v.id,32) && hex(v.source_stage_id,32) && hex(v.target_stage_id,32) && meteredCondition(v.source_usage) && !!v.source_usage.epoch_id && v.source_usage.settlement.sequence === "0";
}
export const commandScope = (p: Pair, source: boolean): Scope => ({ enabled: true, scope: "metered_tcp_loopback_lab", node_id: source ? p.source_node : p.target_node, installation_id: source ? p.source_installation : p.target_installation });
export function readHandover(v: unknown): Handover {
 if (!plan(v) || !object(v) || !phases.includes(String(v.phase)) || v.can_publish !== false || typeof v.cancelled !== "boolean" || v.cancelled !== (v.phase === "cancelled") || typeof v.created_unix_nano !== "number" || !Number.isFinite(v.created_unix_nano) || v.created_unix_nano <= 0 || (v.drain_command_id !== "" && !hex(v.drain_command_id,32)) || (v.apply_command_id !== "" && !hex(v.apply_command_id,32))) throw Error("invalid_handover");
 if(v.health_intents!==undefined){if(!Array.isArray(v.health_intents)||v.health_intents.length>50)throw Error("invalid_health_intents");v.health_intents.forEach(i=>readHealthIntent(i,v.id,v.target_node,v.target_installation));if(new Set(v.health_intents.map(i=>i.id)).size!==v.health_intents.length)throw Error("duplicate_health_intent");}
 if(v.health_rounds!==undefined){
  if(!Array.isArray(v.health_rounds)||v.health_rounds.length>50)throw Error("invalid_health_rounds");
  v.health_rounds.forEach(r=>readHealthRound(r,v.id,v.target_node,v.target_installation));
  if(new Set(v.health_rounds.map(r=>r.id)).size!==v.health_rounds.length||v.health_rounds.filter(r=>r.current).length>1)throw Error("duplicate_health_round");
 }
 if(v.historical_health_evidence!==undefined){const e=v.historical_health_evidence;if(!object(e)||!hex(e.challenge_id,32)||e.handover_id!==v.id||!hex(e.request_sha,64)||!hex(e.receipt_sha,64)||typeof e.replayed!=="boolean"||typeof e.accepted_unix_nano!=="number"||!Number.isFinite(e.accepted_unix_nano)||e.accepted_unix_nano<=0)throw Error("invalid_health_evidence");}
 for (const [field, id, source, action, stage] of [["drain_command", v.drain_command_id, true, "drain",v.source_stage_id],["apply_command",v.apply_command_id,false,"apply",v.target_stage_id]] as const) {
  if (v[field] === undefined) continue;
  const c = readCommand(v[field],commandScope(v,source));
  if (c.id !== id || c.action !== action || c.stage_id !== stage) throw Error("mismatched_handover_command");
 }
 return v as Handover;
}
export function readPage(v: unknown): Page {
 if (!object(v) || !Array.isArray(v.operations) || v.operations.length > 50 || (v.next_cursor !== "" && !hex(v.next_cursor,32))) throw Error("invalid_handover_page");
 const operations = v.operations.map(readHandover);
 if (new Set(operations.map(o=>o.id)).size !== operations.length || (v.next_cursor !== "" && v.next_cursor !== operations.at(-1)?.id)) throw Error("invalid_handover_cursor");
 return { operations, next_cursor: v.next_cursor as string };
}
export function readHandoverAttempt(raw: string | null): HandoverAttempt | null {
 if (!raw || raw.length > 32768) return null;
 try {
  const a: unknown = JSON.parse(raw);
  if (!object(a) || !pair(a.pair) || typeof a.body !== "string" || typeof a.operation !== "string" || !["create","stage","drain","apply","cancel","health","health-round"].includes(String(a.action))) return null;
  const b: unknown = JSON.parse(a.body);
  if (!object(b)) return null;
  if (a.action === "create") {
   if (!plan(b) || a.operation !== b.id || !Object.keys(a.pair).every(k=>b[k] === (a.pair as unknown as Record<string, unknown>)[k])) return null;
  } else if (a.action === "health") {
   if(!hex(a.operation,32)||!hex(b.id,32)||b.confirm_probe!==true||typeof b.inbound_id!=="number"||!Number.isSafeInteger(b.inbound_id)||b.inbound_id<=0||Object.keys(b).length!==3)return null;
  } else if (a.action === "health-round") {
   if(!hex(a.operation,32)||!hex(b.id,32)||b.confirmed!==true||Object.keys(b).length!==2)return null;
  } else if (a.action === "cancel") {
   if (!hex(a.operation,32) || b.confirm_before_drain !== true || Object.keys(b).length !== 1) return null;
  } else {
   const source = a.action === "drain", scope = commandScope(a.pair,source);
   if (a.action === "stage" ? a.operation !== "" : !hex(a.operation,32)) return null;
   const attempt = readAttempt(JSON.stringify({node_id:scope.node_id,installation_id:scope.installation_id,id:b.id,action:a.action,body:a.body}),scope);
   if (!attempt) return null;
  }
  return a as HandoverAttempt;
 } catch { return null; }
}
export function sameUsage(a: MeteredCondition, b: MeteredCondition) {
 return a.epoch_id === b.epoch_id && a.epoch_sha === b.epoch_sha && a.admission_sha === b.admission_sha && a.settlement.epoch_id === b.settlement.epoch_id && a.settlement.sequence === b.settlement.sequence && a.settlement.sha256 === b.settlement.sha256;
}
