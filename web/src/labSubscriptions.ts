// Native N18 wire validation. No subscription secret is stored here or in
// browser storage; only an explicit reveal response may reach component state.
import { readConsumerRef } from "./consumerManifests";
export type LabSnapshot = {
 id:string; event_id:string; client_id:string; handover_id:string; round_id:string; node_id:string; installation_id:string;
 revision:number; created_unix_nano:number; expires_unix_nano:number; route_count:number; current:boolean; replayed?:boolean;
 consumer_manifest_id?:string;consumer_manifest_revision?:number;consumer_manifest_sha?:string;
 scope?:"lab_single_handover";delivery_revision?:number;
};
export type InitialLabSnapshot = {
 id:string;event_id:string;client_id:string;handover_id:"";round_id:string;node_id:string;installation_id:string;
 scope:"lab_initial_apply";delivery_revision:number;inbound_id:number;promotion_event_id:string;initial_apply_sha:string;
 revision:number;created_unix_nano:number;expires_unix_nano:number;route_count:1;current:boolean;replayed?:boolean;server_time_unix_milli?:number;
 consumer_manifest_id?:never;consumer_manifest_revision?:never;consumer_manifest_sha?:never;
};
export type SubscriptionView = { enabled:false; scope:"lab_single_handover"|"lab_initial_apply"; client_id:string } | {
 enabled:true; scope:"lab_single_handover"|"lab_initial_apply"; client_id:string; generation:number; state:"absent"|"active"|"revoked";
 server_time_unix_milli:number;snapshot?:LabSnapshot|InitialLabSnapshot;
 delivery_revision?:number;delivery_scope?:""|"lab_single_handover"|"lab_initial_apply";initial_apply_revision?:number;handover_revision?:number;
};
export type TokenAction = "issue"|"rotate"|"revoke";
export type TokenAttempt = {action:TokenAction; body:{event_id:string;expected_generation:number}};
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==="object"&&!Array.isArray(v);
const natural=(v:unknown):v is number=>typeof v==="number"&&Number.isSafeInteger(v)&&v>=0;
const nano=(v:unknown):v is number=>typeof v==="number"&&Number.isInteger(v)&&v>0&&v<=9_223_372_036_854_775_000;
const hex=(v:unknown):v is string=>typeof v==="string"&&/^[a-f0-9]{32}$/.test(v);
export const clientIdentity=(v:unknown):v is string=>typeof v==="string"&&/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(v);
export const subscriptionPath=(id:string)=>"/api/v1/workspace/clients/"+encodeURIComponent(id)+"/subscription";
export const subscriptionEventID=()=>crypto.randomUUID().replaceAll("-","");
export function readTokenAttempt(raw:string|null):TokenAttempt|null {
 if(!raw)return null;
 try{const v=JSON.parse(raw);if(!object(v)||!["issue","rotate","revoke"].includes(String(v.action))||!object(v.body)||!hex(v.body.event_id)||!natural(v.body.expected_generation)||v.body.expected_generation>=Number.MAX_SAFE_INTEGER)return null;return {action:v.action as TokenAction,body:{event_id:v.body.event_id,expected_generation:v.body.expected_generation}};}catch{return null;}
}
export function readLabSnapshot(v:unknown,clientID:string):LabSnapshot {
 if(!object(v)||v.client_id!==clientID||![v.id,v.event_id,v.handover_id,v.round_id,v.installation_id].every(hex)||
  typeof v.node_id!=="string"||!/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(v.node_id)||
  !natural(v.revision)||v.revision<1||!natural(v.route_count)||v.route_count<1||v.route_count>256||
  !nano(v.created_unix_nano)||!nano(v.expires_unix_nano)||v.expires_unix_nano<=v.created_unix_nano||
  v.expires_unix_nano-v.created_unix_nano>8e9||typeof v.current!=="boolean"||
  (v.replayed!==undefined&&typeof v.replayed!=="boolean"))throw Error("invalid_lab_snapshot");
 if((v.scope!==undefined||v.delivery_revision!==undefined)&&(v.scope!=="lab_single_handover"||!(v.delivery_revision===undefined&&v.current===false&&v.replayed===true)&&(!natural(v.delivery_revision)||v.delivery_revision<1)))throw Error("invalid_handover_delivery_identity");
 if(v.inbound_id!==undefined||v.promotion_event_id!==undefined||v.initial_apply_sha!==undefined)throw Error("invalid_handover_snapshot_origin");
 readConsumerRef(v);
 return v as LabSnapshot;
}
export function readInitialLabSnapshot(v:unknown,clientID:string):InitialLabSnapshot {
 const keys=["id","event_id","client_id","handover_id","round_id","node_id","installation_id","scope","delivery_revision","inbound_id","promotion_event_id","initial_apply_sha","revision","created_unix_nano","expires_unix_nano","route_count","current","replayed","server_time_unix_milli"];
 if(!object(v)||Object.keys(v).some(k=>!keys.includes(k))||v.scope!=="lab_initial_apply"||v.client_id!==clientID||v.handover_id!==""||![v.id,v.event_id,v.round_id,v.installation_id].every(hex)||
  !clientIdentity(v.node_id)||!natural(v.inbound_id)||v.inbound_id<1||typeof v.promotion_event_id!=="string"||!/^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/.test(v.promotion_event_id)||typeof v.initial_apply_sha!=="string"||!/^[a-f0-9]{64}$/.test(v.initial_apply_sha)||
  !natural(v.delivery_revision)||v.delivery_revision<1||!natural(v.revision)||v.revision<1||v.route_count!==1||!nano(v.created_unix_nano)||!nano(v.expires_unix_nano)||v.expires_unix_nano<=v.created_unix_nano||v.expires_unix_nano-v.created_unix_nano>8e9||typeof v.current!=="boolean"||v.replayed!==undefined&&typeof v.replayed!=="boolean"||v.server_time_unix_milli!==undefined&&(!natural(v.server_time_unix_milli)||v.server_time_unix_milli===0||v.server_time_unix_milli>9_223_372_036_854))throw Error("invalid_initial_apply_snapshot");
 return v as InitialLabSnapshot;
}
export function readSubscription(v:unknown,clientID:string):SubscriptionView {
 if(!object(v)||v.client_id!==clientID||!["lab_single_handover","lab_initial_apply"].includes(String(v.scope))||typeof v.enabled!=="boolean")throw Error("invalid_subscription_scope");
 if(v.scope==="lab_initial_apply"&&Object.keys(v).some(k=>!["enabled","scope","client_id","generation","state","server_time_unix_milli","snapshot","delivery_revision","delivery_scope","initial_apply_revision","handover_revision"].includes(k)))throw Error("unexpected_initial_subscription_field");
 if(!v.enabled)return v as SubscriptionView;
 if(!natural(v.generation)||!["absent","active","revoked"].includes(String(v.state))||
  (v.state==="absent")!==(v.generation===0)||!natural(v.server_time_unix_milli)||v.server_time_unix_milli===0||v.server_time_unix_milli>9_223_372_036_854)throw Error("invalid_subscription_state");
 const deliveryFields=["delivery_revision","delivery_scope","initial_apply_revision","handover_revision"];
 const shared=v.scope==="lab_initial_apply"||deliveryFields.some(k=>v[k]!==undefined);
 if(shared&&(!natural(v.delivery_revision)||!natural(v.initial_apply_revision)||!natural(v.handover_revision)||!["","lab_single_handover","lab_initial_apply"].includes(String(v.delivery_scope))||(v.delivery_revision===0)!==(v.delivery_scope==="")))throw Error("invalid_subscription_delivery_authority");
 if(v.snapshot!==undefined){
  const snapshot=object(v.snapshot)&&v.snapshot.scope==="lab_initial_apply"?readInitialLabSnapshot(v.snapshot,clientID):readLabSnapshot(v.snapshot,clientID);
  if(snapshot.scope==="lab_initial_apply"&&(!shared||v.delivery_scope!==snapshot.scope||v.delivery_revision!==snapshot.delivery_revision||v.initial_apply_revision!==snapshot.revision))throw Error("mismatched_initial_delivery_authority");
  if(shared&&v.delivery_revision!==0&&(snapshot.scope!==v.delivery_scope||snapshot.delivery_revision!==v.delivery_revision||snapshot.revision!==(snapshot.scope==="lab_initial_apply"?v.initial_apply_revision:v.handover_revision)))throw Error("mismatched_subscription_delivery_authority");
  if(v.scope==="lab_initial_apply"&&v.delivery_revision===0)throw Error("missing_initial_delivery_authority");
 }
 if(shared&&v.delivery_revision!==0&&v.snapshot===undefined)throw Error("missing_subscription_delivery_snapshot");
 return v as SubscriptionView;
}
export function readTokenReceipt(v:unknown,clientID:string,a:TokenAttempt) {
 if(!object(v)||v.client_id!==clientID||v.event_id!==a.body.event_id||v.generation!==a.body.expected_generation+1||
  v.state!==(a.action==="revoke"?"revoked":"active")||typeof v.replayed!=="boolean"||typeof v.superseded!=="boolean"||
  "token" in v||"path" in v)throw Error("invalid_token_receipt");
 return v;
}
export function readSubscriptionSecret(v:unknown,clientID:string,generation:number):string {
 if(!object(v)||v.client_id!==clientID||v.generation!==generation||typeof v.token!=="string"||
  !/^[A-Za-z0-9_-]{43}$/.test(v.token)||v.path!=="/api/v1/subscriptions/"+v.token)throw Error("invalid_subscription_secret");
 const bytes=atob(v.token.replaceAll("-","+").replaceAll("_","/")+"=");
 if(bytes.length!==32||btoa(bytes).replaceAll("+","-").replaceAll("/","_").replace(/=+$/,"")!==v.token)throw Error("noncanonical_subscription_secret");
 return v.path as string;
}
