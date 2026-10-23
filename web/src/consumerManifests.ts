import type { Handover, HealthRound } from "./meteredHandovers";

export type ConsumerManifestRef = {consumer_manifest_id:string;consumer_manifest_revision:number;consumer_manifest_sha:string};
export type ConsumerMember = {inbound_id:number;origin_port:number;consumer_address:string;consumer_port:number};
export type ConsumerManifest = {
 id:string;event_id:string;handover_id:string;node_id:string;installation_id:string;revision:number;
 state:"active"|"revoked";consumer_manifest_sha:string;created_unix_nano:number;members:ConsumerMember[];current:boolean;replayed?:boolean;
};
export type ConsumerApproval = {node_id:string;installation_id:string;inbound_id:number;address:string;port:number};
export type ConsumerView = {enabled:false;scope:"consumer_endpoint_lab";handover_id:string} | {
 enabled:true;scope:"consumer_endpoint_lab";handover_id:string;revision:number;state:"absent"|"active"|"revoked";
 server_time_unix_milli:number;manifest?:ConsumerManifest;required_inbounds:{inbound_id:number;origin_port:number}[];approved_endpoints:ConsumerApproval[];
};
export type ConsumerMutation = {event_id:string;action:"replace"|"revoke";expected_revision:number;members:{inbound_id:number;consumer_address:string;consumer_port:number}[];confirmed:true};
export type ConsumerRoundRequest = {id:string;consumer_manifest_id:string;consumer_manifest_revision:number;confirmed:true};
export type ConsumerAttempt = {action:"replace"|"revoke";body:ConsumerMutation}|{action:"round";body:ConsumerRoundRequest;manifest:ConsumerManifest};
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==="object"&&!Array.isArray(v);
const natural=(v:unknown):v is number=>typeof v==="number"&&Number.isSafeInteger(v)&&v>=0&&v<Number.MAX_SAFE_INTEGER;
const positive=(v:unknown):v is number=>natural(v)&&v>0;
const port=(v:unknown):v is number=>positive(v)&&v>=1024&&v<=65535;
const hex=(v:unknown,n=32):v is string=>typeof v==="string"&&new RegExp(`^[a-f0-9]{${n}}$`).test(v);
const nano=(v:unknown):v is number=>typeof v==="number"&&Number.isInteger(v)&&v>0&&v<=9_223_372_036_854_775_000;
export const consumerPath=(id:string)=>"/api/v1/workspace/metered-handovers/"+id+"/consumer-manifest";
export const consumerEndpointKey=(address:string,port:number)=>address+":"+port;
// This is only wire validation. Independent HUB and Agent startup policies
// authorize the exact tuple and forbid special/private address ranges.
export function consumerAddress(v:unknown):v is string {
 if(typeof v!=="string"||!/^\d{1,3}(\.\d{1,3}){3}$/.test(v))return false;
 return v.split(".").every(part=>Number(part)<=255&&String(Number(part))===part);
}
export function readConsumerRef(v:Record<string,unknown>):ConsumerManifestRef|undefined {
 const keys=["consumer_manifest_id","consumer_manifest_revision","consumer_manifest_sha"];
 if(keys.every(k=>v[k]===undefined))return undefined;
 if(!hex(v.consumer_manifest_id)||!positive(v.consumer_manifest_revision)||!hex(v.consumer_manifest_sha,64))throw Error("invalid_consumer_manifest_binding");
 return {consumer_manifest_id:v.consumer_manifest_id,consumer_manifest_revision:v.consumer_manifest_revision,consumer_manifest_sha:v.consumer_manifest_sha};
}
export function readConsumerManifest(v:unknown,operation:Pick<Handover,"id"|"target_node"|"target_installation">):ConsumerManifest {
 if(!object(v)||!hex(v.id)||!hex(v.event_id)||v.handover_id!==operation.id||v.node_id!==operation.target_node||v.installation_id!==operation.target_installation||
  !positive(v.revision)||!["active","revoked"].includes(String(v.state))||!hex(v.consumer_manifest_sha,64)||!nano(v.created_unix_nano)||
  typeof v.current!=="boolean"||(v.state==="revoked"&&v.current)||(v.replayed!==undefined&&typeof v.replayed!=="boolean")||!Array.isArray(v.members)||v.members.length>256||
  (v.state==="active"&&v.members.length===0))throw Error("invalid_consumer_manifest");
 for(const member of v.members)if(!object(member)||!positive(member.inbound_id)||!port(member.origin_port)||!consumerAddress(member.consumer_address)||!port(member.consumer_port))throw Error("invalid_consumer_member");
 const members=v.members as ConsumerMember[];
 if(new Set(members.map(m=>m.inbound_id)).size!==members.length||new Set(members.map(m=>m.origin_port)).size!==members.length||new Set(members.map(m=>consumerEndpointKey(m.consumer_address,m.consumer_port))).size!==members.length)throw Error("duplicate_consumer_member");
 return v as ConsumerManifest;
}
export function readConsumerView(v:unknown,operation:Pick<Handover,"id"|"target_node"|"target_installation">):ConsumerView {
 if(!object(v)||v.scope!=="consumer_endpoint_lab"||v.handover_id!==operation.id||typeof v.enabled!=="boolean")throw Error("invalid_consumer_scope");
 if(!v.enabled)return v as ConsumerView;
 if(!natural(v.revision)||!["absent","active","revoked"].includes(String(v.state))||(v.state==="absent")!==(v.revision===0)||
  !positive(v.server_time_unix_milli)||v.server_time_unix_milli>9_223_372_036_854||!Array.isArray(v.required_inbounds)||v.required_inbounds.length>256||
  !Array.isArray(v.approved_endpoints)||v.approved_endpoints.length>256)throw Error("invalid_consumer_view");
 for(const row of v.required_inbounds)if(!object(row)||!positive(row.inbound_id)||!port(row.origin_port))throw Error("invalid_consumer_required_inbound");
 const required=v.required_inbounds as {inbound_id:number;origin_port:number}[];
 if(new Set(required.map(r=>r.inbound_id)).size!==required.length||new Set(required.map(r=>r.origin_port)).size!==required.length)throw Error("duplicate_consumer_required_inbound");
 for(const a of v.approved_endpoints)if(!object(a)||a.node_id!==operation.target_node||a.installation_id!==operation.target_installation||!positive(a.inbound_id)||!consumerAddress(a.address)||!port(a.port))throw Error("invalid_consumer_approval");
 const approved=v.approved_endpoints as ConsumerApproval[];
 if(new Set(approved.map(a=>consumerEndpointKey(a.address,a.port))).size!==approved.length)throw Error("duplicate_consumer_approval");
 if(v.manifest!==undefined){const m=readConsumerManifest(v.manifest,operation);if(m.revision!==v.revision||m.state!==v.state)throw Error("mismatched_consumer_head");
  if(m.current&&(m.members.length!==required.length||!required.every(r=>m.members.some(x=>x.inbound_id===r.inbound_id&&x.origin_port===r.origin_port))||!m.members.every(x=>approved.some(a=>a.inbound_id===x.inbound_id&&a.address===x.consumer_address&&a.port===x.consumer_port))))throw Error("unapproved_current_consumer_manifest");
 }else if(v.state!=="absent")throw Error("missing_consumer_head");
 if(v.state==="absent"&&v.manifest!==undefined)throw Error("unexpected_consumer_head");
 return v as ConsumerView;
}
export function sameConsumerRound(round:HealthRound,manifest:ConsumerManifest):boolean {
 return round.consumer_manifest_id===manifest.id&&round.consumer_manifest_revision===manifest.revision&&round.consumer_manifest_sha===manifest.consumer_manifest_sha&&
  round.members.length===manifest.members.length&&manifest.members.every(m=>round.members.some(r=>r.inbound_id===m.inbound_id&&r.port===m.origin_port&&r.consumer_address===m.consumer_address&&r.consumer_port===m.consumer_port));
}
export function readConsumerAttempt(raw:string|null,operation:Pick<Handover,"id"|"target_node"|"target_installation">):ConsumerAttempt|null {
 if(!raw||raw.length>65536)return null;
 try{
  const v:unknown=JSON.parse(raw);if(!object(v)||!object(v.body))return null;
  const b=v.body;
  if(v.action==="round"){
   if(!hex(b.id)||!hex(b.consumer_manifest_id)||!positive(b.consumer_manifest_revision)||b.confirmed!==true||Object.keys(b).length!==4)return null;
   const manifest=readConsumerManifest(v.manifest,operation);
   if(b.consumer_manifest_id!==manifest.id||b.consumer_manifest_revision!==manifest.revision||manifest.state!=="active")return null;
   return {action:"round",body:{id:b.id,consumer_manifest_id:b.consumer_manifest_id,consumer_manifest_revision:b.consumer_manifest_revision,confirmed:true},manifest};
  }
  if(!["replace","revoke"].includes(String(v.action))||b.action!==v.action||!hex(b.event_id)||!natural(b.expected_revision)||b.confirmed!==true||Object.keys(b).length!==5||!Array.isArray(b.members)||b.members.length>256||(v.action==="revoke"?b.members.length!==0:b.members.length===0))return null;
  const members:ConsumerMutation["members"]=[];
  for(const m of b.members){if(!object(m)||Object.keys(m).length!==3||!positive(m.inbound_id)||!consumerAddress(m.consumer_address)||!port(m.consumer_port))return null;members.push({inbound_id:m.inbound_id,consumer_address:m.consumer_address,consumer_port:m.consumer_port});}
  if(new Set(members.map(m=>m.inbound_id)).size!==members.length||new Set(members.map(m=>consumerEndpointKey(m.consumer_address,m.consumer_port))).size!==members.length)return null;
  return {action:v.action as "replace"|"revoke",body:{event_id:b.event_id,action:v.action as "replace"|"revoke",expected_revision:b.expected_revision,members,confirmed:true}};
 }catch{return null;}
}
