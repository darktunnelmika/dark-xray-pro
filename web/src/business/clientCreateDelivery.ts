import { api, mutation } from "../session";
import { readExecutionState, readExecutionPreview, readExecutionOperation, executionPath, type ExecutionCommand, type ExecutionInput } from "../inboundExecution";

export type CreatePayload = {event_id:string;client_id:string;representative_id:string;name:string;duration_days:number;ip_count:number;volume_bytes:number;unlimited:boolean};
export type CreateAttempt = {version:1;owner:string;payload:CreatePayload;inbound_ids:number[];manual_uuid:boolean;creation_sent?:boolean;created:boolean;commands:Record<string,ExecutionCommand>;submitted:number[]};
export const createStorageKey=(owner:string)=>"darkpro:client-create:v1:"+owner;
export function readCreateAttempt(raw:string|null,owner:string):CreateAttempt|null {
 if (!raw) return null;
 const a=JSON.parse(raw) as CreateAttempt;
 if(a.version!==1 || a.owner!==owner || !a.payload || !/^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/.test(a.payload.event_id) ||
 !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(a.payload.client_id) || !Array.isArray(a.inbound_ids) ||
 a.inbound_ids.length>16 || a.inbound_ids.some(id=>!Number.isSafeInteger(id)||id<1) ||
 new Set(a.inbound_ids).size!==a.inbound_ids.length || typeof a.created!=="boolean" ||
 typeof a.manual_uuid!=="boolean" || !a.commands || !Array.isArray(a.submitted)) throw Error("invalid_creation_recovery");
 return a;
}
export async function preflightInbounds(ids:number[]) {
 for (const id of ids) {
  const s=readExecutionState(await api<unknown>(executionPath(id)),id);
  if(!s.enabled || s.owner_resume_required || s.pending_changes || !["active","stopped"].includes(s.desired_state) ||
    s.accounting_mode!=="per_client" || !s.allowed_actions.some(x=>x==="activate"||x==="apply"))
   throw Error("inbound_not_ready");
 }
}
export async function deliverCreatedClient(a:CreateAttempt, csrf:string, save:(a:CreateAttempt)=>void, alive:()=>boolean) {
 const check=()=>{if(!alive())throw Error("session_changed");};
 for(const id of a.inbound_ids) {
  check();
  const path=executionPath(id);
  let s=readExecutionState(await api<unknown>(path),id);
  check();
  const ready=s.observed_state==="active" && !s.pending_changes && s.effective_client_ids.includes(a.payload.client_id);
  if(ready)continue;
  if(!a.commands[id]) {
   if(s.owner_resume_required || s.pending_changes || !["active","stopped"].includes(s.desired_state) || s.accounting_mode!=="per_client")
    throw Error("inbound_not_ready");
   const input:ExecutionInput={event_id:crypto.randomUUID(),action:s.desired_state==="active"?"apply":"activate",
    expected_revision:s.revision,expected_plan_revision:s.plan_revision,expected_inbound_revision:s.inbound_revision,
    client_ids:[...new Set([...s.client_ids,a.payload.client_id])].sort(),accounting_mode:"per_client",reason:"Create client and deliver selected inbound"};
   const preview=readExecutionPreview(await mutation<unknown>(path+"/preview",input,csrf),id,input);
   check();
   if(!preview.valid)throw Error("inbound_activation_blocked");
   a={...a,commands:{...a.commands,[id]:{...input,review_sha:preview.review_sha}}};save(a);
  }
  if(!a.submitted.includes(id)) {
   const cmd=a.commands[id];
   readExecutionOperation(await mutation<unknown>(path+"/commands",cmd,csrf),id,cmd);
   check();a={...a,submitted:[...a.submitted,id]};save(a);
  }
 }
 for(let n=0;n<8;n++) {
  let all=true;
  for(const id of a.inbound_ids) {
   check();const s=readExecutionState(await api<unknown>(executionPath(id)),id);check();
   if(s.observed_state!=="active" || s.pending_changes || !s.effective_client_ids.includes(a.payload.client_id))all=false;
  }
  if(all)return true;
  if(n<7)await new Promise(r=>setTimeout(r,1500));
 }
 return false;
}
