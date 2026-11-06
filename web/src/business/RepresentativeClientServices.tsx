import {useEffect,useRef,useState} from "react";
import {api,mutation,ApiError,type Session} from "../session";
import "./ClientServices.css";
type Service={inbound_id:number;name:string;grant_revision:number;execution_revision:number;plan_revision:number;inbound_revision:number;assigned:boolean;applied:boolean;ready:boolean};
type Command={event_id:string;grant_revision:number;execution_revision:number;plan_revision:number;inbound_revision:number;add:boolean;reason:string;review_sha?:string};
type Attempt={inbound:number;body:Command};
type Result={client_id:string;inbound_id:number;valid:boolean;review_sha?:string;operation_id?:string;state?:string};
const integer=(n:unknown)=>Number.isSafeInteger(n)&&Number(n)>=0;
export default function RepresentativeClientServices({clientID,session,language}:{clientID:string;session:Session;language:"fa"|"en"}){
 const fa=language==="fa",base="/api/v1/workspace/clients/"+encodeURIComponent(clientID)+"/services",key="darkpro:rep-service:v1:"+session.user.id+":"+clientID;
 const [services,setServices]=useState<Service[]>([]),[selected,setSelected]=useState(0),[reason,setReason]=useState(""),[review,setReview]=useState<Attempt|null>(null),[pending,setPending]=useState<Attempt|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(false),[blocked,setBlocked]=useState(false),[notice,setNotice]=useState("");
 const controller=useRef<AbortController|null>(null),lock=useRef(false);
 async function load(){
  const d=await api<{client_id:string;services:Service[]}>(base,{signal:controller.current?.signal});
  if(d.client_id!==clientID||!Array.isArray(d.services)||d.services.length>256||new Set(d.services.map(s=>s.inbound_id)).size!==d.services.length||d.services.some(s=>!integer(s.inbound_id)||s.inbound_id<1||!integer(s.grant_revision)||s.grant_revision<1||![s.execution_revision,s.plan_revision,s.inbound_revision].every(integer)||typeof s.name!=="string"||![s.assigned,s.applied,s.ready].every(v=>typeof v==="boolean")))throw Error();
  if(!controller.current?.signal.aborted)setServices(d.services);
 }
 useEffect(()=>{
  const c=new AbortController();controller.current=c;
  try{const raw=sessionStorage.getItem(key);if(raw){if(raw.length>4096)throw Error();const p=JSON.parse(raw) as Attempt,b=p.body;if(!integer(p.inbound)||p.inbound<1||!b||typeof b.event_id!=="string"||!b.event_id||typeof b.reason!=="string"||typeof b.add!=="boolean"||!b.review_sha||!/^[a-f0-9]{64}$/.test(b.review_sha)||![b.grant_revision,b.execution_revision,b.plan_revision,b.inbound_revision].every(integer))throw Error();setPending(p);setSelected(p.inbound)}}catch{setBlocked(true)}
  void load().catch(()=>{if(!c.signal.aborted)setError(true)});return()=>c.abort();
 },[key]);
 const service=services.find(s=>s.inbound_id===selected);
 async function preview(){
  if(lock.current||!service?.ready||!reason.trim()||pending||blocked)return;
  lock.current=true;setBusy(true);setError(false);setNotice("");
  const body:Command={event_id:crypto.randomUUID(),grant_revision:service.grant_revision,execution_revision:service.execution_revision,plan_revision:service.plan_revision,inbound_revision:service.inbound_revision,add:!service.assigned,reason:reason.trim()};
  try{const r=await mutation<Result>(base+"/"+selected+"/preview",body,session.csrf_token);if(r.client_id!==clientID||r.inbound_id!==selected||!r.valid||!r.review_sha||!/^[a-f0-9]{64}$/.test(r.review_sha))throw Error();if(!controller.current?.signal.aborted)setReview({inbound:selected,body:{...body,review_sha:r.review_sha}})}catch{if(!controller.current?.signal.aborted)setError(true)}finally{lock.current=false;if(!controller.current?.signal.aborted)setBusy(false)}
 }
 async function send(p:Attempt){
  if(lock.current||blocked)return;lock.current=true;setBusy(true);setError(false);
  try{sessionStorage.setItem(key,JSON.stringify(p))}catch{setBlocked(true);setBusy(false);lock.current=false;return}
  setPending(p);setReview(null);
  try{const r=await mutation<Result>(base+"/"+p.inbound+"/commands",p.body,session.csrf_token);if(r.client_id!==clientID||r.inbound_id!==p.inbound||!r.valid||r.operation_id!==p.body.event_id)throw Error();sessionStorage.removeItem(key);if(controller.current?.signal.aborted)return;setPending(null);setReason("");setNotice(fa?"فرمان ثبت شد؛ وضعیت اعمال را تازه‌سازی کنید.":"Command recorded. Refresh to check runtime application.");await load()}catch(e){if(!controller.current?.signal.aborted){if(e instanceof ApiError&&e.status>=400&&e.status<500&&![408,429].includes(e.status)){try{sessionStorage.removeItem(key);setPending(null);setServices([]);await load()}catch{setBlocked(true)}}setError(true)}}finally{lock.current=false;if(!controller.current?.signal.aborted)setBusy(false)}
 }
 return <section className="business-panel client-services" aria-label="Representative client services">
 <h3>{fa?"سرویس‌های مجاز کلاینت":"Allowed client services"}</h3>
 <label>{fa?"سرویس":"Service"}<select aria-label="Allowed service" value={selected} disabled={busy||!!pending||!!review} onChange={e=>{setSelected(Number(e.target.value));setNotice("");setError(false)}}><option value={0}>{fa?"انتخاب سرویس":"Choose service"}</option>{services.map(s=><option key={s.inbound_id} value={s.inbound_id}>{s.name}</option>)}</select></label>
 <button type="button" disabled={busy||!!review} onClick={()=>{setError(false);void load().catch(()=>setError(true))}}>{fa?"تازه‌سازی وضعیت":"Refresh service status"}</button>
 {service&&<p>{service.assigned?(fa?"عضویت ثبت‌شده":"Membership saved"):(fa?"بدون عضویت":"Not assigned")} · {service.applied&&service.ready?(fa?"اعمال‌شده روی سرویس":"Applied to service"):(fa?"اعمال فعال تأیید نشده":"Active application not confirmed")}</p>}
 {!pending&&!review&&<><label>{fa?"دلیل تغییر":"Change reason"}<input aria-label="Service change reason" value={reason} maxLength={200} disabled={busy} onChange={e=>setReason(e.target.value)}/></label><button type="button" disabled={busy||blocked||!service?.ready||!reason.trim()} onClick={()=>void preview()}>{fa?"بررسی تغییر":"Review change"}</button></>}
 {review&&<div><p>{review.body.add?(fa?"افزودن به سرویس":"Add to service"):(fa?"حذف از سرویس":"Remove from service")} · {review.body.reason}</p><button type="button" disabled={busy} onClick={()=>void send(review)}>{fa?"تأیید تغییر":"Confirm change"}</button><button type="button" disabled={busy} onClick={()=>setReview(null)}>{fa?"انصراف":"Cancel"}</button></div>}
 {pending&&<button type="button" disabled={busy||blocked} onClick={()=>void send(pending)}>{fa?"پیگیری همان درخواست":"Retry same request"}</button>}
 {blocked&&<p role="alert">{fa?"ذخیره یا بازیابی درخواست ممکن نیست؛ تغییر مسدود است.":"Recovery storage unavailable. Changes blocked."}</p>}
 {error&&<p role="alert">{fa?"نتیجه تأیید نشد؛ وضعیت سرویس و مجوز را بررسی کنید.":"Result not verified. Check service state and permission."}</p>}{notice&&<p role="status">{notice}</p>}
 </section>;
}
