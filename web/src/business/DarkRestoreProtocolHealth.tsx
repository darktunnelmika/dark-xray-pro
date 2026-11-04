import { useEffect, useRef, useState } from "react";
import { api } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import type { PromotionReceipt } from "../inboundPlanExecution";
import { initialHealthBinding, initialHealthPath, readInitialHealthPreparation, readInitialHealthRound, readInitialHealthState, sameInitialBinding, type InitialHealthBinding, type InitialHealthState } from "../inboundInitialApplyHealth";
import { subscriptionEventID } from "../labSubscriptions";

type Attempt = {action:"prepare";body:{event_id:string;promotion_event_id:string;confirmed:true}} | {action:"rounds";body:{id:string;initial_apply_sha:string;confirmed:true};binding:InitialHealthBinding};
type Checked={state:InitialHealthState;mono:number;wall:number};
function readAttempt(raw:string|null,p:PromotionReceipt):Attempt|null {
 if(!raw)return null;
 if(raw.length>4096)throw Error("oversized retry");
 const a=JSON.parse(raw),b=a?.body,hex=(v:unknown,n:number)=>typeof v==="string"&&new RegExp("^[a-f0-9]{"+n+"}$").test(v);
 if(!b||b.confirmed!==true)throw Error("invalid retry");
 if(a.action==="prepare"&&Object.keys(a).sort().join(",")==="action,body"&&Object.keys(b).sort().join(",")==="confirmed,event_id,promotion_event_id"&&hex(b.event_id,32)&&b.promotion_event_id===p.event_id)return a;
 if(a.action==="rounds"&&Object.keys(a).sort().join(",")==="action,binding,body"&&Object.keys(b).sort().join(",")==="confirmed,id,initial_apply_sha"&&hex(b.id,32)&&hex(b.initial_apply_sha,64)&&a.binding&&Object.keys(a.binding).sort().join(",")==="apply_command_id,epoch_id,initial_apply_sha,stage_id"&&initialHealthBinding(a.binding)&&b.initial_apply_sha===a.binding.initial_apply_sha)return a;
 throw Error("invalid retry");
}
export default function DarkRestoreProtocolHealth({model,promotion}:{model:BusinessModel;promotion:PromotionReceipt}) {
 const fa=model.language==="fa",base=initialHealthPath(promotion.inbound_id),key="dark-pro-import-health:"+model.session.user.id+":"+promotion.event_id;
 const [checked,setChecked]=useState<Checked|null>(null),[ready,setReady]=useState(false),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[storageError,setStorageError]=useState(false),[attempt,setAttempt]=useState<Attempt|null>(null),[refresh,setRefresh]=useState(0),[,tick]=useState(0);
 const life=useRef<AbortController|null>(null),sending=useRef(false),retained=useRef<Attempt|null>(null),current=useRef<Checked|null>(null);
 current.current=checked;
 function remember(a:Attempt|null){try{if(a)sessionStorage.setItem(key,JSON.stringify(a));else sessionStorage.removeItem(key);retained.current=a;setAttempt(a);setStorageError(false);return true;}catch{setStorageError(true);return false;}}
 useEffect(()=>{
  const c=new AbortController();life.current=c;
  try{const a=readAttempt(sessionStorage.getItem(key),promotion);retained.current=a;setAttempt(a);}catch{setStorageError(true);}
  const timer=setInterval(()=>tick(n=>n+1),250);
  return()=>{c.abort();clearInterval(timer);};
 },[key,model.session.csrf_token]);
 useEffect(()=>{
  const c=new AbortController();let timer:ReturnType<typeof setTimeout>;setReady(false);
  async function read(){
   const mono=performance.now(),wall=Date.now();
   try{
    const state=readInitialHealthState(await api<unknown>(base,{signal:c.signal}),promotion);
    if(c.signal.aborted)return;
    setChecked({state,mono,wall});setReady(true);
    const a=retained.current;
    if(state.enabled&&a?.action==="prepare"&&state.preparation?.event_id===a.body.event_id){readInitialHealthPreparation(state.preparation,promotion,a.body);remember(null);}
    if(state.enabled&&a?.action==="rounds"&&state.latest_round?.id===a.body.id){readInitialHealthRound(state.latest_round,promotion,{id:a.body.id,binding:a.binding});remember(null);}
   }catch{if(!c.signal.aborted){setChecked(null);setReady(false);setFailed(true);}}
   finally{if(!c.signal.aborted)timer=setTimeout(()=>void read(),2000);}
  }
  void read();return()=>{c.abort();clearTimeout(timer);};
 },[base,promotion.event_id,refresh,model.session.csrf_token]);
 function fresh(v:Checked|null){return !!v&&Math.max(0,performance.now()-v.mono,Date.now()-v.wall)<12000;}
 function now(v:Checked){return v.state.server_time_unix_milli+Math.max(0,performance.now()-v.mono,Date.now()-v.wall);}
 async function send(a:Attempt){
  const c=life.current;if(!c||c.signal.aborted||sending.current||storageError||!remember(a))return;
  sending.current=true;setBusy(true);setFailed(false);
  try{
   const raw=await api<unknown>(base+"/"+a.action,{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":model.session.csrf_token},body:JSON.stringify(a.body),signal:c.signal});
   if(a.action==="prepare")readInitialHealthPreparation(raw,promotion,a.body);else readInitialHealthRound(raw,promotion,{id:a.body.id,binding:a.binding});
   if(!c.signal.aborted){remember(null);setReady(false);setRefresh(n=>n+1);}
  }catch{if(!c.signal.aborted)setFailed(true);}
  finally{if(!c.signal.aborted){sending.current=false;setBusy(false);}}
 }
 function act(action:"prepare"|"rounds"){
  const snapshot=current.current,s=snapshot?.state;
  if(!ready||!fresh(snapshot)||!s?.enabled||!s.available||sending.current||retained.current||storageError)return;
  const binding=initialHealthBinding(s);
  if(action==="prepare"?!s.can_prepare:!s.eligible||!binding)return;
  if(!window.confirm(fa?(action==="prepare"?"هویت آزمون بدون کسر حجم، پیش از Stage آماده شود؟":"اتصال واقعی همین اجرای بررسی‌شده، بدون صدور اشتراک آزموده شود؟"):(action==="prepare"?"Prepare the nonbillable probe identity before Stage?":"Test this exact reviewed execution without issuing a subscription?")))return;
  if(!fresh(snapshot)||current.current!==snapshot)return;
  void send(action==="prepare"?{action,body:{event_id:subscriptionEventID(),promotion_event_id:promotion.event_id,confirmed:true}}:{action,body:{id:subscriptionEventID(),initial_apply_sha:binding!.initial_apply_sha,confirmed:true},binding:binding!});
 }
 const state=checked?.state,round=state?.enabled?state.latest_round:undefined,live=ready&&fresh(checked)&&state?.enabled&&state.available;
 const roundLive=!!(round&&checked&&round.expires_unix_nano/1e6>now(checked)&&!["expired","state_changed"].includes(round.state));
 const verified=!!(live&&state?.enabled&&state.eligible&&round&&round.current&&round.state==="complete"&&roundLive&&sameInitialBinding(round,state));
 return <section aria-label={fa?"سلامت اجرای مشتری واردشده":"Imported client execution health"}>
  <h4>{fa?"آماده‌سازی و آزمون سلامت":"Prepare and verify health"}</h4>
  <p>{fa?"ابتدا هویت آزمون را آماده کن، سپس پایین همین بخش Stage و Apply را انجام بده و بعد اتصال را بیازما. این آزمون توکن اشتراک نمی‌خواهد و مشتری را فعال نمی‌کند.":"Prepare the probe identity first, then Stage and Apply below, then test the connection. This test needs no subscription token and does not activate the client."}</p>
  <button type="button" disabled={busy} onClick={()=>{setFailed(false);setRefresh(n=>n+1);}}>{fa?"به‌روزرسانی سلامت اجرا":"Refresh execution health"}</button>
  {state&&!state.enabled&&<p role="status">{fa?"این قابلیت در راه‌اندازی HUB فعال نشده است.":"This capability is disabled at HUB startup."}</p>}
  {state?.enabled&&<>
   <button type="button" disabled={!live||busy||!!attempt||storageError||!state.can_prepare} onClick={()=>act("prepare")}>{fa?"آماده‌سازی آزمون بدون کسر حجم":"Prepare nonbillable probe"}</button>
   <button type="button" disabled={!live||busy||!!attempt||storageError||!state.eligible||roundLive} onClick={()=>act("rounds")}>{fa?"آزمون اتصال اجرای فعلی":"Test current execution"}</button>
   <p role="status">{verified?(fa?"اتصال همین اجرا در مهلت اثبات تأیید شد؛ فعال‌سازی هنوز انجام نشده است.":"This execution's connection is verified within the proof window; activation is still pending."):(fa?"سلامت فعلی تأیید نشده است.":"Current health is not verified.")}</p>
   {state.blockers.length>0&&<ul>{state.blockers.map((b,i)=><li key={i}><bdi>{b.field}: {b.code}</bdi></li>)}</ul>}
  </>}
  {attempt&&<p><button type="button" disabled={busy||storageError||!live} onClick={()=>{if(window.confirm(fa?"همان درخواست ذخیره‌شده تکرار شود؟":"Retry the exact retained request?"))void send(attempt);}}>{fa?"تکرار همان درخواست سلامت":"Retry exact health request"}</button></p>}
  {(attempt||storageError)&&<button type="button" disabled={busy} onClick={()=>{if(window.confirm(fa?"فقط درخواست محلی کنار گذاشته شود؟ عملیات ثبت‌شده در سرور لغو نمی‌شود.":"Discard only the local request? This does not undo a recorded server operation."))remember(null);}}>{fa?"کنارگذاشتن درخواست محلی":"Discard local request"}</button>}
  {(failed||storageError)&&<p role="alert">{storageError?(fa?"ذخیرهٔ درخواست ممکن نیست؛ درخواست جدیدی ارسال نشده است.":"The request could not be retained; no new write was sent."):(fa?"پاسخ قابل تأیید نبود؛ وضعیت را تازه کن یا همان درخواست را بازیابی کن.":"The response could not be verified. Refresh or recover the exact request.")}</p>}
 </section>;
}
