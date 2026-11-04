import { useEffect, useRef, useState } from "react";
import { api } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import type { PromotionReceipt } from "../inboundPlanExecution";
import { initialHealthPath, readInitialHealthAttempt, readInitialHealthSnapshot, type InitialHealthRound, type InitialHealthAttempt } from "../inboundInitialApplyHealth";
import { readSubscription, subscriptionPath, subscriptionEventID, type SubscriptionView, type InitialLabSnapshot } from "../labSubscriptions";

type Attempt=Extract<InitialHealthAttempt,{action:"snapshot"}>;
export type AcceptedApply={roundID:string;applySHA:string;recorded:number};
export default function DarkRestorePostApplyDelivery({model,promotion,round,accepted,current}:{model:BusinessModel;promotion:PromotionReceipt;round:InitialHealthRound;accepted:AcceptedApply;current:boolean}){
 const fa=model.language==="fa",client=promotion.client_ids[0];
 const key="darkrestore-post-apply:"+model.session.user.id+":"+client+":"+promotion.event_id+":"+accepted.roundID;
 const [view,setView]=useState<SubscriptionView|null>(null),[attempt,setAttempt]=useState<Attempt|null>(null),[snapshot,setSnapshot]=useState<InitialLabSnapshot|null>(null),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[storageFailed,setStorageFailed]=useState(false);
 const life=useRef<AbortController|null>(null),sending=useRef(false);
 function bound(s:InitialLabSnapshot){
  if(s.round_id!==accepted.roundID||s.initial_apply_sha!==accepted.applySHA||s.created_unix_nano<=accepted.recorded)throw Error("foreign accepted delivery");
  return s;
 }
 function retain(a:Attempt|null){
  try{if(a)sessionStorage.setItem(key,JSON.stringify(a));else sessionStorage.removeItem(key);setAttempt(a);return true;}catch{setStorageFailed(true);return false;}
 }
 async function refresh(c:AbortController){
  setFailed(false);setView(null);
  try{
   const v=readSubscription(await api<unknown>(subscriptionPath(client),{signal:c.signal}),client);
   if(c.signal.aborted)return;
   if(v.enabled&&v.scope!=="lab_initial_apply")throw Error("wrong delivery scope");
   if(v.enabled&&v.snapshot){const s=bound(readInitialHealthSnapshot(v.snapshot,promotion));setSnapshot(s);}
   setView(v);
  }catch{if(!c.signal.aborted)setFailed(true);}
 }
 useEffect(()=>{
  const c=new AbortController();life.current=c;sending.current=false;setBusy(false);setSnapshot(null);setAttempt(null);setStorageFailed(false);
  try{const a=readInitialHealthAttempt(sessionStorage.getItem(key),promotion);if(a&&(a.action!=="snapshot"||a.body.client_id!==client||a.body.round_id!==accepted.roundID||a.body.initial_apply_sha!==accepted.applySHA))throw Error("foreign retained delivery");setAttempt(a as Attempt|null);}catch{setStorageFailed(true);}
  void refresh(c);return()=>c.abort();
 },[key,model.session.csrf_token]);
 const matching=round.id===accepted.roundID&&round.initial_apply_sha===accepted.applySHA;
 const allowed=current&&matching&&view?.enabled===true&&view.scope==="lab_initial_apply"&&!attempt&&!snapshot&&!storageFailed&&!busy;
 async function send(a:Attempt){
  const c=life.current;if(!c||c.signal.aborted||sending.current||storageFailed||!retain(a))return;
  sending.current=true;setBusy(true);setFailed(false);
  try{
   const raw=await api<unknown>(initialHealthPath(promotion.inbound_id)+"/snapshot",{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":model.session.csrf_token},body:JSON.stringify(a.body),signal:c.signal});
   const s=bound(readInitialHealthSnapshot(raw,promotion,a));
   if(!c.signal.aborted){setSnapshot(s);retain(null);}
  }catch{if(!c.signal.aborted)setFailed(true);}
  finally{if(!c.signal.aborted){sending.current=false;setBusy(false);}}
 }
 function materialize(){
  if(!allowed||!view?.enabled||view.initial_apply_revision===undefined||view.delivery_revision===undefined)return;
  if(!window.confirm(fa?"نسخهٔ رمزگذاری‌شدهٔ اشتراک برای همین پذیرش ثبت شود؟ توکن صادر و اشتراک فعال نمی‌شود.":"Record the encrypted subscription snapshot for this acceptance? No token is issued or subscription activated."))return;
  void send({action:"snapshot",round,body:{event_id:subscriptionEventID(),client_id:client,round_id:accepted.roundID,initial_apply_sha:accepted.applySHA,expected_revision:view.initial_apply_revision,expected_delivery_revision:view.delivery_revision,confirmed:true}});
 }
 return <section aria-label={fa?"ثبت نسخهٔ اشتراک پس از Apply":"Post-Apply subscription snapshot"}>
  <p>{fa?"این مرحله فقط نسخهٔ رمزگذاری‌شده را ثبت می‌کند؛ لینک اشتراک و فعال‌سازی جداگانه‌اند.":"This step records an encrypted snapshot. Subscription access and activation are separate."}</p>
  {snapshot?<p>{fa?"رسید ثبت نسخه بازیابی شد؛ فعال‌سازی تأیید نشده است.":"Snapshot receipt verified; activation is not confirmed."}</p>:<button type="button" disabled={!allowed} onClick={materialize}>{fa?"ثبت نسخهٔ اشتراک":"Record subscription snapshot"}</button>}
  {(!current||!matching)&&<p>{fa?"سلامتِ همان پذیرش دیگر معتبر نیست؛ ثبت جدید مسدود است. آزمون جدید این رسید را تمدید نمی‌کند.":"The accepted round is no longer current; new materialization is blocked. A new round does not renew this receipt."}</p>}
  {view&&!view.enabled&&<p>{fa?"ثبت اشتراک در این LAB فعال نیست.":"Subscription materialization is disabled in this LAB."}</p>}
  {attempt&&<button type="button" disabled={busy||storageFailed} onClick={()=>void send(attempt)}>{fa?"تکرار همان ثبت نسخه":"Retry exact snapshot request"}</button>}
  <button type="button" disabled={busy} onClick={()=>{const c=life.current;if(c&&!c.signal.aborted)void refresh(c);}}>{fa?"بازیابی وضعیت نسخهٔ اشتراک":"Recover subscription snapshot"}</button>
  {(failed||storageFailed)&&<p role="alert">{fa?"ثبت نسخه تأیید نشد؛ وضعیت را بازیابی یا همان درخواست را تکرار کن.":"Snapshot recording is unconfirmed. Recover status or retry the same request."}</p>}
 </section>;
}
