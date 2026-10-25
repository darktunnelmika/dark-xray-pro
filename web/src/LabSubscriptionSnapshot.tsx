import { useEffect, useRef, useState } from "react";
import { api, useSession, type Language } from "./session";
import type { Handover, HealthRound } from "./meteredHandovers";
import { clientIdentity, readLabSnapshot, readSubscription, subscriptionEventID, subscriptionPath, type LabSnapshot, type SubscriptionView } from "./labSubscriptions";
import "./LabSubscriptions.css";
import { useServerClock } from "./useServerClock";

const words={
 en:{title:"Prepare LAB subscription output",scope:"Record one client's complete local routes from this transfer. This replaces that client's previous LAB snapshot and preserves the original health deadline.",search:"Find a synthetic client",find:"Search clients",client:"Subscription client",previous:"Previous clients",next:"More clients",prepare:"Record LAB subscription snapshot",loading:"Checking clients and subscription revision…",failed:"The client, revision or retained request could not be verified.",needRound:"A complete, current health round for every destination Inbound is required.",noToken:"Issue a subscription token in the client's subscription section before recording output.",confirm:"Record this client's local LAB output from the current complete health round? It replaces the previous experimental snapshot and keeps the original deadline.",saved:"LAB snapshot recorded. Its original health deadline still applies.",retry:"Retry exact snapshot request",unknown:"The snapshot response was not confirmed. The exact request can be retried without extending its deadline.",discard:"Discard local retry",current:"Current LAB delivery verified at recording",historical:"Snapshot recorded; current delivery not confirmed",deadline:"Original deadline (Tehran)"},
 fa:{title:"آماده‌سازی خروجی اشتراک LAB",scope:"مسیرهای کامل محلی یک کلاینت را از این انتقال ثبت کنید. نسخهٔ آزمایشی قبلی همان کلاینت جایگزین می‌شود و مهلت اولیهٔ سلامت حفظ می‌شود.",search:"جست‌وجوی کلاینت مصنوعی",find:"جست‌وجوی کلاینت",client:"کلاینت اشتراک",previous:"کلاینت‌های قبلی",next:"کلاینت‌های بیشتر",prepare:"ثبت نسخهٔ اشتراک LAB",loading:"در حال بررسی کلاینت‌ها و نسخهٔ اشتراک…",failed:"کلاینت، نسخه یا درخواست ذخیره‌شده تأیید نشد.",needRound:"یک دورهٔ کامل و معتبر سلامت برای همهٔ اینباندهای مقصد لازم است.",noToken:"پیش از ثبت خروجی، از بخش اشتراک کلاینت یک توکن صادر کنید.",confirm:"خروجی محلی LAB این کلاینت از دورهٔ سلامت کامل فعلی ثبت شود؟ نسخهٔ آزمایشی قبلی جایگزین می‌شود و مهلت اولیه حفظ می‌شود.",saved:"نسخهٔ اشتراک LAB ثبت شد؛ همان مهلت اولیهٔ سلامت برقرار است.",retry:"تکرار همان درخواست نسخه",unknown:"پاسخ ثبت نسخه تأیید نشد؛ همان درخواست بدون تمدید مهلت قابل تکرار است.",discard:"حذف تکرار محلی",current:"دریافت آزمایشی در زمان ثبت تأیید شد",historical:"نسخه ثبت شده؛ دریافت فعلی تأیید نیست",deadline:"پایان اعتبار اولیه (تهران)"}
};
type Client={id:string;name:string;archived:boolean};
type Attempt={event_id:string;client_id:string;round_id:string;expected_revision:number;expected_delivery_revision?:number;consumer_manifest_id?:string;consumer_manifest_revision?:number};
const id=(v:unknown)=>typeof v==="string"&&/^[a-f0-9]{32}$/.test(v);
function retained(value:string|null):Attempt|null {
 if(!value)return null;
 try{const v=JSON.parse(value);if(!v||!id(v.event_id)||!clientIdentity(v.client_id)||!id(v.round_id)||!Number.isSafeInteger(v.expected_revision)||v.expected_revision<0)return null;const a:Attempt={event_id:v.event_id,client_id:v.client_id,round_id:v.round_id,expected_revision:v.expected_revision};if(v.expected_delivery_revision!==undefined){if(!Number.isSafeInteger(v.expected_delivery_revision)||v.expected_delivery_revision<0||v.expected_delivery_revision>=Number.MAX_SAFE_INTEGER)return null;a.expected_delivery_revision=v.expected_delivery_revision;}if(v.consumer_manifest_id!==undefined||v.consumer_manifest_revision!==undefined){if(!id(v.consumer_manifest_id)||!Number.isSafeInteger(v.consumer_manifest_revision)||v.consumer_manifest_revision<=0)return null;a.consumer_manifest_id=v.consumer_manifest_id;a.consumer_manifest_revision=v.consumer_manifest_revision;}return a;}catch{return null;}
}
export default function LabSubscriptionSnapshot({language,operation,round,ready,retryReady,serverNow,consumerMode=false}:{language:Language;operation:Handover;round?:HealthRound;ready:boolean;retryReady:boolean;serverNow:number;consumerMode?:boolean}) {
 const t={...words[language],...(consumerMode?(language==="fa"?{scope:"مسیرهای مجاز مصرف‌کنندهٔ این کلاینت از همان نگاشت و اثبات کامل ثبت می‌شوند. مهلت اولیهٔ سلامت حفظ می‌شود.",confirm:"خروجی LAB این کلاینت از همین نگاشت مصرف‌کننده و اثبات کامل ثبت شود؟ نسخهٔ قبلی جایگزین می‌شود و مهلت اولیه حفظ می‌شود."}:{scope:"Record this client's approved consumer routes from the exact mapping and complete proof. The original health deadline is preserved.",confirm:"Record this client's LAB output from this exact consumer mapping and complete proof? The prior snapshot is replaced and the original deadline is preserved."}):{})},{session}=useSession(),storageKey="dark-pro-lab-snapshot-attempt:"+session?.user.id+":"+operation.id;
 const [query,setQuery]=useState(""),[search,setSearch]=useState(""),[offset,setOffset]=useState(0),[clients,setClients]=useState<Client[]|null>(null),[more,setMore]=useState(false),[clientID,setClientID]=useState("");
 const [view,setView]=useState<SubscriptionView|null>(null),[refresh,setRefresh]=useState(0),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[result,setResult]=useState<LabSnapshot|null>(null);
 const [attempt,setAttempt]=useState<Attempt|null>(null),[storageFailed,setStorageFailed]=useState(false);
 const lifetime=useRef<AbortController|null>(null),sending=useRef(false);
 const latest=useRef({ready,round});latest.current={ready,round};
 const now=useServerClock(serverNow);
 function remember(a:Attempt|null){try{if(a)sessionStorage.setItem(storageKey,JSON.stringify(a));else sessionStorage.removeItem(storageKey);setAttempt(a);setStorageFailed(false);return true;}catch{setStorageFailed(true);return false;}}
 useEffect(()=>{const c=new AbortController();lifetime.current=c;try{const a=retained(sessionStorage.getItem(storageKey));setAttempt(a);if(a)setClientID(a.client_id);}catch{setStorageFailed(true);}return()=>c.abort();},[storageKey]);
 useEffect(()=>{
  const c=new AbortController();setClients(null);setFailed(false);
  const q=new URLSearchParams({q:search,limit:"25",offset:String(offset),state:"all",plan:"all",sort:"name"});
  void api<unknown>("/api/v1/workspace/clients?"+q,{signal:c.signal}).then(raw=>{
   const v=raw as {clients:Client[];limit:number;offset:number;has_more:boolean};
   if(!Array.isArray(v?.clients)||v.clients.length>25||v.limit!==25||v.offset!==offset||typeof v.has_more!=="boolean"||
    !v.clients.every(row=>clientIdentity(row.id)&&typeof row.name==="string"&&row.name.length<=128&&typeof row.archived==="boolean")||new Set(v.clients.map(row=>row.id)).size!==v.clients.length)throw Error("invalid_client_page");
   if(!c.signal.aborted){setClients(v.clients);setMore(v.has_more);}
  }).catch(()=>{if(!c.signal.aborted)setFailed(true);});return()=>c.abort();
 },[search,offset]);
 useEffect(()=>{
  const c=new AbortController();setView(null);if(!clientID)return()=>c.abort();
  void api<unknown>(subscriptionPath(clientID),{signal:c.signal}).then(raw=>{const v=readSubscription(raw,clientID);if(!c.signal.aborted){setView(v);setFailed(false);}}).catch(()=>{if(!c.signal.aborted)setFailed(true);});return()=>c.abort();
 },[clientID,refresh]);
 const validRound=!!round&&round.current&&round.state==="complete"&&round.handover_id===operation.id&&round.node_id===operation.target_node&&round.installation_id===operation.target_installation&&round.expires_unix_nano/1e6>now()&&(consumerMode?!!round.consumer_manifest_id&&!(!round.consumer_manifest_revision||!round.consumer_manifest_sha):!round.consumer_manifest_id);
 const available=ready&&validRound&&!failed&&!storageFailed&&!busy&&!attempt&&view?.enabled===true&&view.state==="active";
 async function send(a:Attempt){
  const c=lifetime.current;if(sending.current||!session?.csrf_token||!c||c.signal.aborted||!remember(a))return;
  sending.current=true;setBusy(true);setResult(null);
  try{
   const raw=await api<unknown>("/api/v1/workspace/metered-handovers/"+operation.id+"/lab-subscription-snapshot",{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":session.csrf_token},body:JSON.stringify(a),signal:c.signal});
   const v=readLabSnapshot(raw,a.client_id);
   const proof=operation.health_rounds?.find(r=>r.id===a.round_id);
   if(v.event_id!==a.event_id||v.round_id!==a.round_id||v.handover_id!==operation.id||v.node_id!==operation.target_node||v.installation_id!==operation.target_installation||v.revision!==a.expected_revision+1||!proof||v.created_unix_nano<proof.created_unix_nano||v.expires_unix_nano>proof.expires_unix_nano||v.route_count>proof.members.length||v.consumer_manifest_id!==a.consumer_manifest_id||v.consumer_manifest_revision!==a.consumer_manifest_revision||v.consumer_manifest_id!==proof.consumer_manifest_id||v.consumer_manifest_revision!==proof.consumer_manifest_revision||v.consumer_manifest_sha!==proof.consumer_manifest_sha)throw Error("mismatched_snapshot_receipt");
   if(v.delivery_revision!==undefined&&v.delivery_revision!==(a.expected_delivery_revision??0)+1||a.expected_delivery_revision!==undefined&&a.expected_delivery_revision>0&&v.delivery_revision===undefined)throw Error("mismatched_snapshot_delivery_revision");
   const current=latest.current.round;
   const stillCurrent=latest.current.ready&&current?.current&&current.id===v.round_id&&current.consumer_manifest_id===v.consumer_manifest_id&&current.consumer_manifest_revision===v.consumer_manifest_revision&&current.consumer_manifest_sha===v.consumer_manifest_sha&&v.expires_unix_nano/1e6>now();
   if(!c.signal.aborted){setResult({...v,current:v.current&&!!stillCurrent});remember(null);setRefresh(n=>n+1);}
  }catch{/* An unconfirmed reply retains the exact nonsecret operation. */}
  finally{sending.current=false;if(!c.signal.aborted)setBusy(false);}
 }
 function prepare(){if(!available||!round||!view?.enabled||!window.confirm(t.confirm)||!latest.current.ready||latest.current.round?.id!==round.id||latest.current.round.consumer_manifest_id!==round.consumer_manifest_id||latest.current.round.consumer_manifest_revision!==round.consumer_manifest_revision||latest.current.round.consumer_manifest_sha!==round.consumer_manifest_sha||!latest.current.round.current||latest.current.round.expires_unix_nano/1e6<=now())return;const a:Attempt={event_id:subscriptionEventID(),client_id:clientID,round_id:round.id,expected_revision:view.handover_revision??(view.snapshot?.scope!=="lab_initial_apply"?view.snapshot?.revision??0:0)};if(view.delivery_revision!==undefined)a.expected_delivery_revision=view.delivery_revision;if(consumerMode){a.consumer_manifest_id=round.consumer_manifest_id;a.consumer_manifest_revision=round.consumer_manifest_revision;}void send(a);}
 return <section className="lab-subscription handover-operation" aria-label="LAB subscription snapshot"><h4>{t.title}</h4><p>{t.scope}</p>
  <form className="lab-subscription-search" onSubmit={e=>{e.preventDefault();setSearch(query);setOffset(0);}}><label>{t.search}<input value={query} maxLength={128} disabled={busy||!!attempt} onChange={e=>setQuery(e.target.value)}/></label><button disabled={busy||!!attempt} type="submit">{t.find}</button></form>
  <label>{t.client}<select aria-label="Subscription client" value={clientID} disabled={busy||!!attempt||!clients} onChange={e=>{setClientID(e.target.value);setResult(null);}}><option value="">—</option>{clientID&&!clients?.some(c=>c.id===clientID)&&<option value={clientID}>{clientID}</option>}{clients?.map(c=><option key={c.id} value={c.id} disabled={c.archived}>{c.name} ({c.id})</option>)}</select></label>
  <div className="lab-subscription-actions"><button type="button" disabled={offset===0||busy||!!attempt} onClick={()=>setOffset(n=>Math.max(0,n-25))}>{t.previous}</button><button type="button" disabled={!more||busy||!!attempt||!clients} onClick={()=>setOffset(n=>n+25)}>{t.next}</button></div>
  {(failed||storageFailed)&&<p role="alert">{t.failed}</p>}{!clients&&!failed&&<p>{t.loading}</p>}
  {clientID&&view?.enabled&&view.state!=="active"&&<p>{t.noToken}</p>}
  {!validRound&&<p>{t.needRound}</p>}<button type="button" disabled={!available} onClick={prepare}>{t.prepare}</button>
  {attempt&&<div role="status"><p>{t.unknown}</p><div className="lab-subscription-actions"><button type="button" disabled={busy||!session?.csrf_token||!retryReady} onClick={()=>void send(attempt)}>{t.retry}</button><button type="button" disabled={busy} onClick={()=>remember(null)}>{t.discard}</button></div></div>}
  {result&&<div role="status"><p>{t.saved}</p><p>{result.current&&ready&&validRound&&result.round_id===round?.id&&result.consumer_manifest_id===round.consumer_manifest_id&&result.consumer_manifest_revision===round.consumer_manifest_revision&&result.consumer_manifest_sha===round.consumer_manifest_sha&&result.expires_unix_nano/1e6>now()?t.current:t.historical}</p><p>{t.deadline}: <bdi>{new Intl.DateTimeFormat(language==="fa"?"fa-IR":"en-GB",{timeZone:"Asia/Tehran",dateStyle:"short",timeStyle:"medium"}).format(result.expires_unix_nano/1e6)}</bdi></p></div>}
 </section>;
}
