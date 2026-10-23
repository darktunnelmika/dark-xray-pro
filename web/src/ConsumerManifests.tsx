import { useEffect, useRef, useState } from "react";
import { api, useSession, type Language } from "./session";
import { readHealthRound, type Handover } from "./meteredHandovers";
import type { Channel } from "./meteredRouteTransactions";
import { subscriptionEventID } from "./labSubscriptions";
import { consumerEndpointKey, consumerPath, readConsumerAttempt, readConsumerManifest, readConsumerView, sameConsumerRound, type ConsumerAttempt, type ConsumerView } from "./consumerManifests";
import LabSubscriptionSnapshot from "./LabSubscriptionSnapshot";
import { useServerClock } from "./useServerClock";
import "./LabSubscriptions.css";

const words={
 en:{title:"Consumer routes in isolated LAB",scope:"Choose an approved address and port for every destination Inbound. Recording a mapping invalidates previous route proofs and subscription output. Public availability remains unverified.",loading:"Checking endpoint approvals and mapping revision…",failed:"The approved endpoints, current mapping or retained request could not be verified.",disabled:"Consumer endpoint proofs are disabled on this HUB.",origin:"Applied listener",endpoint:"Approved consumer endpoint",choose:"Choose an endpoint",save:"Record consumer mapping",revoke:"Revoke consumer mapping",reset:"Reload current endpoint choices",stale:"The mapping revision changed. Reload its choices before recording another version.",confirmSave:"Record this complete consumer mapping? Previous route proofs and subscription output become invalid. No listener or lease is changed.",confirmRevoke:"Revoke this consumer mapping and stop subscription downloads using it? Previously imported connections are not disconnected by this action.",confirmRound:"Test every endpoint in this exact mapping with system probe identities? The original bounded deadline applies; the runtime lease is not renewed.",test:"Test approved consumer routes",pending:"The response was not confirmed. Only the exact retained request can be retried.",retry:"Retry exact consumer request",discard:"Discard local consumer retry",saved:"Consumer mapping event recorded.",queued:"Consumer route proof requested. Wait for every accepted receipt.",active:"Recorded mapping matches the current destination",historical:"Recorded mapping is historical or no longer authorized",revoked:"Consumer mapping revoked",absent:"No consumer mapping recorded",revision:"Mapping revision",noApproval:"Every destination Inbound needs a separately approved endpoint.",needRuntime:"A fresh applied destination and settled source are required for a new mapping or proof.",proof:"A complete current proof must match this exact mapping before output can be recorded."},
 fa:{title:"مسیر مصرف‌کننده در LAB ایزوله",scope:"برای همهٔ اینباندهای مقصد، نشانی و پورت مجاز را انتخاب کنید. ثبت نگاشت، اثبات مسیر و خروجی اشتراک قبلی را نامعتبر می‌کند. دسترسی عمومی هنوز تأیید نشده است.",loading:"در حال بررسی مقصدهای مجاز و نسخهٔ نگاشت…",failed:"مقصدهای مجاز، نگاشت فعلی یا درخواست ذخیره‌شده تأیید نشد.",disabled:"اثبات مسیر مصرف‌کننده در این HUB غیرفعال است.",origin:"پورت اجراشده",endpoint:"مقصد مجاز مصرف‌کننده",choose:"یک مقصد انتخاب کنید",save:"ثبت نگاشت مصرف‌کننده",revoke:"لغو نگاشت مصرف‌کننده",reset:"بازخوانی انتخاب‌های فعلی",stale:"نسخهٔ نگاشت تغییر کرده است؛ پیش از ثبت نسخهٔ دیگر، انتخاب‌ها را بازخوانی کنید.",confirmSave:"همین نگاشت کامل مصرف‌کننده ثبت شود؟ اثبات مسیر و خروجی اشتراک قبلی نامعتبر می‌شوند. هیچ listener یا مجوز اجرایی تغییر نمی‌کند.",confirmRevoke:"این نگاشت لغو و دریافت اشتراک وابسته به آن متوقف شود؟ این اقدام اتصال‌های قبلاً واردشده را قطع نمی‌کند.",confirmRound:"همهٔ مقصدهای همین نگاشت با هویت سیستمی آزمایش شوند؟ همان مهلت محدود اولیه برقرار است و مجوز اجرای نود تمدید نمی‌شود.",test:"آزمون مسیرهای مجاز مصرف‌کننده",pending:"پاسخ تأیید نشد؛ فقط همان درخواست ذخیره‌شده قابل تکرار است.",retry:"تکرار همان درخواست مصرف‌کننده",discard:"حذف تکرار محلی مصرف‌کننده",saved:"رویداد نگاشت مصرف‌کننده ثبت شد.",queued:"اثبات مسیر مصرف‌کننده درخواست شد؛ رسید همهٔ مسیرها باید پذیرفته شود.",active:"نگاشت ثبت‌شده با مقصد فعلی مطابقت دارد",historical:"نگاشت ثبت‌شده تاریخی است یا دیگر مجاز نیست",revoked:"نگاشت مصرف‌کننده لغو شده است",absent:"نگاشت مصرف‌کننده ثبت نشده است",revision:"نسخهٔ نگاشت",noApproval:"هر اینباند مقصد به نشانی و پورت مجاز مستقل نیاز دارد.",needRuntime:"برای نگاشت یا اثبات تازه، مقصد اجراشده با مشاهدهٔ تازه و مبدأ تسویه‌شده لازم است.",proof:"پیش از ثبت خروجی، اثبات کامل و معتبر باید دقیقاً با همین نگاشت مطابقت داشته باشد."}
};

export default function ConsumerManifests({language,operation,review,ready,retryReady,serverNow,leaseExpires}:{language:Language;operation:Handover;review:Channel["review"];ready:boolean;retryReady:boolean;serverNow:number;leaseExpires:number}) {
 const t=words[language],{session}=useSession();
 const storageKey="dark-pro-consumer-attempt:"+session?.user.id+":"+operation.id;
 const [view,setView]=useState<ConsumerView|null>(null),[verified,setVerified]=useState(false),[failed,setFailed]=useState(false),[refresh,setRefresh]=useState(0);
 const [baseline,setBaseline]=useState<number|null>(null),[choices,setChoices]=useState<Record<string,string>>({});
 const [attempt,setAttempt]=useState<ConsumerAttempt|null>(null),[storageFailed,setStorageFailed]=useState(false),[busy,setBusy]=useState(false),[message,setMessage]=useState<"saved"|"queued"|null>(null);
 const lifetime=useRef<AbortController|null>(null),sending=useRef(false);
 const latest=useRef({view,ready,retryReady,verified,failed});latest.current={view,ready,retryReady,verified,failed};
 const now=useServerClock(serverNow);
 function remember(a:ConsumerAttempt|null){try{if(a)sessionStorage.setItem(storageKey,JSON.stringify(a));else sessionStorage.removeItem(storageKey);setAttempt(a);setStorageFailed(false);return true;}catch{setStorageFailed(true);return false;}}
 useEffect(()=>{
  const c=new AbortController();lifetime.current=c;
  try{const raw=sessionStorage.getItem(storageKey),a=readConsumerAttempt(raw,operation);setAttempt(a);if(raw&&!a)setStorageFailed(true);}catch{setStorageFailed(true);}
  return()=>c.abort();
 },[storageKey,operation.id,operation.target_node,operation.target_installation]);
 useEffect(()=>{
  const c=new AbortController();let timer:ReturnType<typeof setTimeout>;
  async function read(){
   setVerified(false);
   try{const v=readConsumerView(await api<unknown>(consumerPath(operation.id),{signal:c.signal}),operation);if(!c.signal.aborted){setView(v);setFailed(false);setVerified(true);}}
   catch{if(!c.signal.aborted){setFailed(true);setVerified(false);}}
   finally{if(!c.signal.aborted)timer=setTimeout(()=>void read(),3000);}
  }
  void read();return()=>{c.abort();clearTimeout(timer);};
 },[operation.id,operation.target_node,operation.target_installation,refresh]);
 function adopt(v:ConsumerView){if(!v.enabled)return;setBaseline(v.revision);setChoices(Object.fromEntries((v.manifest?.members??[]).map(m=>[m.inbound_id,consumerEndpointKey(m.consumer_address,m.consumer_port)])));}
 useEffect(()=>{if(baseline===null&&view?.enabled)adopt(view);},[view,baseline]);
 const current=view?.enabled?view:null,manifest=current?.manifest;
 const covers=!!current&&current.required_inbounds.length>0&&current.required_inbounds.length===review.length&&review.every(r=>r.tunnel_port===0&&current.required_inbounds.some(m=>m.inbound_id===r.inbound_id&&m.origin_port===r.direct_port));
 const authorized=verified&&!failed&&!!current&&retryReady;
 const idle=authorized&&!busy&&!attempt&&!storageFailed&&!!session?.csrf_token;
 const live=idle&&ready&&leaseExpires/1e6>now()&&covers;
 const sameRevision=!!current&&baseline===current.revision;
 const selections=current?.required_inbounds.map(r=>{
  const found=current.approved_endpoints.find(a=>a.inbound_id===r.inbound_id&&consumerEndpointKey(a.address,a.port)===choices[r.inbound_id]);
  return found?{inbound_id:r.inbound_id,consumer_address:found.address,consumer_port:found.port}:null;
 })??[];
 const selected=selections.length>0&&selections.every(Boolean)&&new Set(selections.filter(Boolean).map(m=>consumerEndpointKey(m!.consumer_address,m!.consumer_port))).size===selections.length;
 const mappingCurrent=authorized&&covers&&manifest?.state==="active"&&manifest.current;
 const roundPending=operation.health_rounds?.some(r=>r.expires_unix_nano/1e6>now()&&r.state!=="state_changed"&&r.state!=="expired")||operation.health_intents?.some(i=>i.expires_unix_nano/1e6>now());
 const canRound=live&&mappingCurrent&&!roundPending;
 const round=mappingCurrent?operation.health_rounds?.find(r=>r.current&&r.state==="complete"&&r.expires_unix_nano/1e6>now()&&sameConsumerRound(r,manifest!)):undefined;

 async function send(a:ConsumerAttempt){
  const c=lifetime.current;if(sending.current||!session?.csrf_token||!c||c.signal.aborted||!remember(a))return;
  sending.current=true;setBusy(true);setMessage(null);
  try{
   const path=a.action==="round"?consumerPath(operation.id).replace(/consumer-manifest$/,"consumer-health-rounds"):consumerPath(operation.id);
   const raw=await api<unknown>(path,{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":session.csrf_token},body:JSON.stringify(a.body),signal:c.signal});
   if(a.action==="round"){
    const r=readHealthRound(raw,operation.id,operation.target_node,operation.target_installation);
    if(r.id!==a.body.id||!sameConsumerRound(r,a.manifest))throw Error("mismatched_consumer_round_reply");
   }else{
    const m=readConsumerManifest(raw,operation);
    if(m.event_id!==a.body.event_id||m.revision!==a.body.expected_revision+1||m.state!==(a.action==="replace"?"active":"revoked")||
     (a.action==="replace"&&(m.members.length!==a.body.members.length||!a.body.members.every(x=>m.members.some(y=>y.inbound_id===x.inbound_id&&y.consumer_address===x.consumer_address&&y.consumer_port===x.consumer_port)))))throw Error("mismatched_consumer_mutation_reply");
   }
   if(!c.signal.aborted){remember(null);setMessage(a.action==="round"?"queued":"saved");setBaseline(null);setView(null);setVerified(false);setRefresh(n=>n+1);}
  }catch{/* Preserve the exact nonsecret request on every unconfirmed reply. */}
  finally{sending.current=false;if(!c.signal.aborted)setBusy(false);}
 }
 function sameLatest(){const v=latest.current.view;return v?.enabled&&current&&latest.current.verified&&!latest.current.failed&&latest.current.retryReady&&v.revision===current.revision&&v.manifest?.id===current.manifest?.id&&v.manifest?.consumer_manifest_sha===current.manifest?.consumer_manifest_sha;}
 function replace(){if(!live||!sameRevision||!selected||!current||!window.confirm(t.confirmSave)||!sameLatest()||!latest.current.ready||leaseExpires/1e6<=now())return;void send({action:"replace",body:{event_id:subscriptionEventID(),action:"replace",expected_revision:current.revision,members:selections.filter((m):m is NonNullable<typeof m>=>m!==null),confirmed:true}});}
 function revoke(){if(!idle||!current||current.state!=="active"||!window.confirm(t.confirmRevoke)||!sameLatest())return;void send({action:"revoke",body:{event_id:subscriptionEventID(),action:"revoke",expected_revision:current.revision,members:[],confirmed:true}});}
 function prove(){if(!canRound||!manifest||!window.confirm(t.confirmRound)||!sameLatest()||!latest.current.ready||!latest.current.view?.enabled||!latest.current.view.manifest?.current||leaseExpires/1e6<=now())return;void send({action:"round",body:{id:subscriptionEventID(),consumer_manifest_id:manifest.id,consumer_manifest_revision:manifest.revision,confirmed:true},manifest:structuredClone(manifest)});}

 return <section className="lab-subscription handover-operation" aria-label="Consumer endpoint manifest">
  <h4>{t.title}</h4><p>{t.scope}</p>
  {!view&&!failed&&<p>{t.loading}</p>}{view&&!view.enabled&&<p>{t.disabled}</p>}
  {(failed||storageFailed)&&<p role="alert">{t.failed}</p>}
  {current&&<>
   <p role="status">{current.state==="absent"?t.absent:current.state==="revoked"?t.revoked:mappingCurrent?t.active:t.historical}</p>
   <p>{t.revision}: <bdi>{current.revision}</bdi>{manifest&&<> · <code>{manifest.id}</code></>}</p>
   {current.required_inbounds.map(r=><label key={r.inbound_id}>{t.endpoint} #{r.inbound_id}<small>{t.origin}: <bdi>{r.origin_port}</bdi></small>
    <select aria-label={`Consumer endpoint ${r.inbound_id}`} value={choices[r.inbound_id]??""} disabled={!idle||!sameRevision} onChange={e=>setChoices(v=>({...v,[r.inbound_id]:e.target.value}))}><option value="">{t.choose}</option>{current.approved_endpoints.filter(a=>a.inbound_id===r.inbound_id).map(a=><option key={consumerEndpointKey(a.address,a.port)} value={consumerEndpointKey(a.address,a.port)}>{consumerEndpointKey(a.address,a.port)}</option>)}</select>
   </label>)}
   {!sameRevision&&<p role="status">{t.stale}</p>}
   {!covers||current.required_inbounds.some(r=>!current.approved_endpoints.some(a=>a.inbound_id===r.inbound_id))?<p>{t.noApproval}</p>:null}
   {!ready&&<p>{t.needRuntime}</p>}
   <div className="lab-subscription-actions"><button type="button" disabled={!live||!sameRevision||!selected} onClick={replace}>{t.save}</button><button type="button" disabled={!idle||current.state!=="active"} onClick={revoke}>{t.revoke}</button><button type="button" disabled={!idle} onClick={()=>adopt(current)}>{t.reset}</button></div>
   <button type="button" disabled={!canRound} onClick={prove}>{t.test}</button>
  </>}
  {(attempt||storageFailed)&&<div role="status"><p>{t.pending}</p><div className="lab-subscription-actions">{attempt&&<button type="button" disabled={busy||!authorized||!session?.csrf_token} onClick={()=>{if(attempt)void send(attempt);}}>{t.retry}</button>}<button type="button" disabled={busy} onClick={()=>remember(null)}>{t.discard}</button></div></div>}
  {message&&<p role="status">{t[message]}</p>}
  <p>{t.proof}</p>
  <LabSubscriptionSnapshot language={language} operation={operation} round={round} ready={!!(live&&mappingCurrent&&round)} retryReady={!!(authorized&&!busy&&!attempt&&!storageFailed)} serverNow={serverNow} consumerMode/>
 </section>;
}
