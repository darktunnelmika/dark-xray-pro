import DarkRestoreExecution from "./DarkRestoreExecution";
import { useEffect, useRef, useState } from "react";
import { api } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { restoreAPI } from "./DarkRestoreImport";

type Target={inbound_id:number;plan_revision:number;inbound_revision:number};
type Review={review_sha256:string;promotion_review_sha256:string;node:string;eligible:boolean};
const sha=(v:unknown):v is string=>typeof v==="string"&&/^[a-f0-9]{64}$/.test(v);
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==="object"&&!Array.isArray(v);
export default function DarkRestorePreparedPromotion({model,client,handoff,target,disabled,onBusyChange}:{model:BusinessModel;client:string;handoff:string;target:Target;disabled:boolean;onBusyChange:(busy:boolean)=>void}) {
 const fa=model.language==="fa", [busy,setBusy]=useState(false),[review,setReview]=useState<Review|null>(null),[recorded,setRecorded]=useState(false),[failed,setFailed]=useState(false),[uncertain,setUncertain]=useState(false);
 const lifetime=useRef<AbortController|null>(null);
 useEffect(()=>{const c=new AbortController();lifetime.current=c;return()=>{c.abort();};},[]);
 const suffix=`${handoff}-${target.inbound_id}-${target.plan_revision}-${target.inbound_revision}`;
 const request={handoff_sha256:handoff,event_id:`drp-${suffix}`,promotion_event_id:`drn-${suffix}`,...target,reason:"Owner-reviewed prepared Dark Restore promotion"};
 const path=restoreAPI+"/handoff/promotion";
 function receipt(v:unknown) {
  if(!object(v)||v.scope!=="darkrestore_prepared_native_promotion_v1"||v.client_id!==client||v.inbound_id!==target.inbound_id||v.event_id!==request.event_id||v.promotion_event_id!==request.promotion_event_id||v.plan_revision!==target.plan_revision||v.prepared_inbound_revision!==target.inbound_revision||v.native_promotion_recorded!==true||v.native_apply_recorded!==false||v.subscription_activated!==false||v.authorizes_further_mutation!==false||!sha(v.transition_sha256)||!sha(v.prepared_content_sha256))throw Error("unverified promotion receipt");
 }
 async function run(action:"preview"|"commit"|"recover") {
  const c=lifetime.current;if(!c||c.signal.aborted||busy||disabled)return;
  if(action==="commit"&&(!review?.eligible||!window.confirm(fa?"همین طرح بررسی‌شده برای این مشتری آمادهٔ اعمال شود؟ سرویس هنوز فعال نمی‌شود.":"Prepare this reviewed setup for this client? Service will not be activated.")))return;
  setBusy(true);onBusyChange(true);setFailed(false);
  if(action==="preview")setReview(null);
  try {
   const body=action==="commit"?{...request,confirmed:true,review_sha256:review!.review_sha256,promotion_review_sha256:review!.promotion_review_sha256}:request;
   const v=await api<unknown>(path+(action==="preview"?"/preview":""),action==="recover"?{signal:c.signal}:{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":model.session.csrf_token},body:JSON.stringify(body),signal:c.signal});
   if(c.signal.aborted)return;
   if(action==="preview") {
    if(!object(v)||!object(v.promotion)||v.scope!=="darkrestore_prepared_native_promotion_v1"||v.client_id!==client||v.inbound_id!==target.inbound_id||v.authorizes_mutation!==false||typeof v.eligible!=="boolean"||!Array.isArray(v.blockers)||v.eligible&&v.blockers.length!==0||!sha(v.review_sha256)||!sha(v.transition_sha256)||!sha(v.pre_state_sha256)||!sha(v.post_state_sha256))throw Error("invalid promotion preview");
    const p=v.promotion;
    if(p.inbound_id!==target.inbound_id||p.plan_revision!==target.plan_revision||p.inbound_revision!==target.inbound_revision||p.eligible!==v.eligible||!Array.isArray(p.client_ids)||p.client_ids.length!==1||p.client_ids[0]!==client||p.runtime_changed!==false||p.subscription_published!==false||!sha(p.review_sha256)||typeof p.node_id!=="string")throw Error("foreign promotion preview");
    setReview({review_sha256:v.review_sha256,promotion_review_sha256:p.review_sha256,node:p.node_id,eligible:v.eligible});
   } else {receipt(v);setRecorded(true);setUncertain(false);setReview(null);}
  } catch {if(!c.signal.aborted){setFailed(true);if(action==="commit")setUncertain(true);}}
  finally {if(!c.signal.aborted){setBusy(false);onBusyChange(false);}}
 }
 return <section aria-label={fa?"آماده‌سازی انتقال مشتری":"Prepare imported client transfer"}>
  <h4>{fa?"آماده‌سازی انتقال مشتری":"Prepare imported client transfer"}</h4>
  {recorded?<p role="status">{fa?"رسید آماده‌سازی ثبت شد. اعمال تنظیمات، بررسی سلامت و فعال‌سازی هنوز مراحل جدا هستند.":"Preparation receipt recorded. Apply, health verification and activation are still separate steps."}</p>:<>
   <button type="button" disabled={busy||disabled||uncertain} onClick={()=>void run("preview")}>{fa?"پیش‌نمایش انتقال":"Preview transfer"}</button>
   {review&&<><p>{fa?"نود بررسی‌شده":"Reviewed node"}: <bdi>{review.node}</bdi></p><p>{review.eligible?(fa?"آمادهٔ تأیید انتقال به مرحلهٔ اعمال":"Ready to confirm preparation for Apply"):(fa?"پیش‌شرط‌های انتقال کامل نیست؛ طرح و وضعیت نود را بررسی کن.":"Transfer prerequisites are incomplete. Review the setup and node state.")}</p><button type="button" disabled={busy||disabled||!review.eligible} onClick={()=>void run("commit")}>{uncertain?(fa?"تکرار همان تأیید":"Retry exact confirmation"):(fa?"تأیید آماده‌سازی انتقال":"Confirm transfer preparation")}</button></>}
   <button type="button" disabled={busy||disabled} onClick={()=>void run("recover")}>{fa?"بازیابی رسید آماده‌سازی":"Recover preparation receipt"}</button>
  </>}
  {recorded&&<DarkRestoreExecution handoff={handoff} model={model} client={client} inbound={target.inbound_id} planRevision={target.plan_revision} sourceRevision={target.inbound_revision} promotionEvent={request.promotion_event_id}/>}
  {failed&&<p role="alert">{uncertain?(fa?"پاسخ تأیید نشد؛ رسید را بازیابی کن یا همان درخواست را تکرار کن.":"The response was not confirmed. Recover the receipt or retry the exact request."):(fa?"وضعیت مطابق این مشتری و طرح تأیید نشد.":"The state could not be verified for this client and setup.")}</p>}
 </section>;
}
