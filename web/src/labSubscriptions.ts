// Native N18 wire validation. No subscription secret is stored here or in
// browser storage; only an explicit reveal response may reach component state.
import { readConsumerRef } from "./consumerManifests";
export type LabSnapshot = {
 id:string; event_id:string; client_id:string; handover_id:string; round_id:string; node_id:string; installation_id:string;
 revision:number; created_unix_nano:number; expires_unix_nano:number; route_count:number; current:boolean; replayed?:boolean;
 consumer_manifest_id?:string;consumer_manifest_revision?:number;consumer_manifest_sha?:string;
};
export type SubscriptionView = { enabled:false; scope:"lab_single_handover"; client_id:string } | {
 enabled:true; scope:"lab_single_handover"; client_id:string; generation:number; state:"absent"|"active"|"revoked";
 server_time_unix_milli:number; snapshot?:LabSnapshot;
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
 readConsumerRef(v);
 return v as LabSnapshot;
}
export function readSubscription(v:unknown,clientID:string):SubscriptionView {
 if(!object(v)||v.client_id!==clientID||v.scope!=="lab_single_handover"||typeof v.enabled!=="boolean")throw Error("invalid_subscription_scope");
 if(!v.enabled)return v as SubscriptionView;
 if(!natural(v.generation)||!["absent","active","revoked"].includes(String(v.state))||
  (v.state==="absent")!==(v.generation===0)||!natural(v.server_time_unix_milli)||v.server_time_unix_milli===0||v.server_time_unix_milli>9_223_372_036_854)throw Error("invalid_subscription_state");
 if(v.snapshot!==undefined)readLabSnapshot(v.snapshot,clientID);
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
