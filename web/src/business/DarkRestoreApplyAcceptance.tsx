import { useEffect, useRef, useState } from "react";
import { api } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import type { PromotionReceipt } from "../inboundPlanExecution";
import type { InitialHealthRound } from "../inboundInitialApplyHealth";
import DarkRestorePostApplyDelivery, { type AcceptedApply } from "./DarkRestorePostApplyDelivery";
import { restoreAPI } from "./DarkRestoreImport";

const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==="object"&&!Array.isArray(v);
const sha=(v:unknown)=>typeof v==="string"&&/^[a-f0-9]{64}$/.test(v);
type Review={review_sha256:string;evidence_sha256:string;evidence:Record<string,unknown>};
export default function DarkRestoreApplyAcceptance({model,promotion,round,handoff,current}:{model:BusinessModel;promotion:PromotionReceipt;round:InitialHealthRound;handoff:string;current:boolean}){
 const fa=model.language==="fa",path=restoreAPI+"/handoff/apply-acceptance";
 const event="accept-apply-"+round.id;
 const request={handoff_sha256:handoff,event_id:event,inbound_id:promotion.inbound_id,round_id:round.id,review_sha256:"",confirmed:false};
 const [review,setReview]=useState<Review|null>(null),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[uncertain,setUncertain]=useState(false),[recorded,setRecorded]=useState<AcceptedApply|null>(null);
 const life=useRef<AbortController|null>(null),sending=useRef(false);
 useEffect(()=>{const c=new AbortController();life.current=c;return()=>c.abort();},[]);
 function evidence(v:unknown,recovered=false):asserts v is Record<string,unknown>{
  if(!object(v)||v.scope!=="darkrestore_native_apply_evidence_v1"||v.client_id!==promotion.client_ids[0]||promotion.client_ids.length!==1||v.inbound_id!==promotion.inbound_id||v.plan_revision!==promotion.plan_revision||v.prepared_inbound_revision!==promotion.source_inbound_revision||v.promoted_inbound_revision!==promotion.inbound_revision||v.promotion_event_id!==promotion.event_id||!recovered&&(v.health_round_id!==round.id||v.initial_apply_sha256!==round.initial_apply_sha||v.apply_command_id!==round.apply_command_id||v.stage_id!==round.stage_id))throw Error("foreign evidence");
  if(!sha(v.initial_apply_sha256)||[v.health_round_id,v.apply_command_id,v.stage_id].some(x=>typeof x!=="string"||!/^[a-f0-9]{32}$/.test(x)))throw Error("invalid execution identity");
  for(const k of ["transition_sha256","prepared_content_sha256","prepared_authority_sha256","promotion_receipt_sha256","health_receipt_sha256","import_receipt_sha256","native_baseline_sha256"])if(!sha(v[k]))throw Error("invalid evidence digest");
  const amounts=["current_finite_entitlement","settled_historical_bytes","native_post_migration_bytes","remaining_budget_bytes"].map(k=>{const x=v[k];if(typeof x!=="string"||!/^(0|[1-9][0-9]{0,18})$/.test(x))throw Error("invalid amount");return BigInt(x);});
  if(amounts[0]<=0n||amounts[0]-amounts[1]-amounts[2]!==amounts[3])throw Error("unbalanced evidence");
 }
 async function run(action:"preview"|"commit"|"recover"){
  const c=life.current;if(!c||c.signal.aborted||sending.current||action!=="recover"&&!current)return;
  if(action==="commit"&&(!review||!window.confirm(fa?"پذیرش همین Apply و نتیجهٔ سلامت ثبت شود؟ اشتراک هنوز فعال نمی‌شود.":"Record acceptance of this exact Apply and health proof? The subscription remains inactive.")))return;
  sending.current=true;setBusy(true);setFailed(false);if(action==="preview")setReview(null);
  try{
   const v=await api<unknown>(path+(action==="preview"?"/preview":""),action==="recover"?{signal:c.signal}:{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":model.session.csrf_token},body:JSON.stringify(action==="commit"?{...request,confirmed:true,review_sha256:review!.review_sha256}:request),signal:c.signal});
   if(c.signal.aborted)return;
   if(!object(v))throw Error("invalid response");
   evidence(v.evidence,action==="recover");
   if(action==="preview"){
    if(v.scope!=="darkrestore_owner_apply_review_v1"||v.event_id!==event||v.authorizes_mutation!==false||!sha(v.review_sha256)||!sha(v.evidence_sha256))throw Error("invalid review");
    setReview(v as unknown as Review);
   }else{
    if(v.scope!=="darkrestore_native_apply_acceptance_v1"||v.client_id!==promotion.client_ids[0]||v.inbound_id!==promotion.inbound_id||v.event_id!==(action==="recover"?"accept-apply-"+String(v.evidence.health_round_id):event)||v.native_apply_recorded!==true||v.subscription_delivered!==false||v.subscription_activated!==false||v.authorizes_further_mutation!==false||!sha(v.evidence_sha256)||v.transition_sha256!==v.evidence.transition_sha256||action==="commit"&&(v.evidence_sha256!==review!.evidence_sha256||JSON.stringify(v.evidence)!==JSON.stringify(review!.evidence)))throw Error("unmatched acceptance");
    if(typeof v.recorded_unix_nano!=="number"||!Number.isInteger(v.recorded_unix_nano)||v.recorded_unix_nano<=0)throw Error("invalid acceptance time");
    setRecorded({roundID:String(v.evidence.health_round_id),applySHA:String(v.evidence.initial_apply_sha256),recorded:v.recorded_unix_nano});setReview(null);setUncertain(false);
   }
  }catch{if(!c.signal.aborted){setFailed(true);if(action==="commit")setUncertain(true);}}
  finally{if(!c.signal.aborted){sending.current=false;setBusy(false);}}
 }
 return <section aria-label={fa?"پذیرش Apply و سلامت":"Accept Apply and health"}>
  {recorded?<p role="status">{fa?"پذیرش Apply و سلامت ثبت شد؛ تحویل اشتراک و فعال‌سازی هنوز انجام نشده‌اند.":"Apply and health acceptance recorded; subscription delivery and activation are still pending."}</p>:<>
   <button type="button" disabled={busy||!current||uncertain} onClick={()=>void run("preview")}>{fa?"پیش‌نمایش پذیرش اجرا":"Preview execution acceptance"}</button>
   {review&&<><p>{fa?"حجم باقی‌مانده (بایت)":"Remaining quota (bytes)"}: <bdi>{String(review.evidence.remaining_budget_bytes)}</bdi></p><button type="button" disabled={busy||!current} onClick={()=>void run("commit")}>{uncertain?(fa?"تکرار همان پذیرش":"Retry exact acceptance"):(fa?"تأیید پذیرش اجرا":"Confirm execution acceptance")}</button></>}
   <button type="button" disabled={busy} onClick={()=>void run("recover")}>{fa?"بازیابی رسید پذیرش اجرا":"Recover execution acceptance"}</button>
  </>}
  {recorded&&<DarkRestorePostApplyDelivery model={model} promotion={promotion} round={round} accepted={recorded} current={current}/>}
  {failed&&<p role="alert">{fa?"پاسخ با اجرای بررسی‌شده تطبیق نکرد؛ رسید را بازیابی کن.":"The response did not match the reviewed execution. Recover its receipt."}</p>}
 </section>;
}
