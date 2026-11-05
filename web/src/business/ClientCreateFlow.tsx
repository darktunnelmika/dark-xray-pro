import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, mutation, ApiError } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { bytesFromGB, uid } from "./shared";
import ClientPlanWorkbench from "./ClientPlanWorkbench";
import ClientInboundSubscription from "./ClientInboundSubscription";
import { createStorageKey, readCreateAttempt, preflightInbounds, deliverCreatedClient, type CreateAttempt } from "./clientCreateDelivery";
import "./ClientCreateFlow.css";

type Choice={id:number;name:string;archived?:boolean;spec?:{protocol?:string;network?:string;security?:string}};
const words={
 en:{title:"Create client",manual:"Manual",preset:"Quick presets",name:"Client name",random:"Random name",identity:"Advanced identity",id:"Client ID (optional)",uuid:"UUID (optional)",uuidHelp:"Leave blank to generate securely. A custom UUID must be unique. It is never saved in browser storage.",rep:"Account owner",plan:"Plan",volume:"Volume (GiB)",days:"Duration (days)",ips:"IP limit",unlimited:"Unlimited",volumetric:"Volume",targets:"Select inbounds",search:"Search inbounds",reload:"Refresh inbounds",empty:"No selectable inbounds loaded.",loadError:"Inbound list unavailable. Refresh to continue.",deliver:"Create and activate",draft:"Save account only",draftNote:"This saves the account and charges its credit. An owner must activate it before configuration delivery.",repNote:"Inbound activation currently requires the owner. Your account can be saved for owner activation.",save:"Create client",retry:"Resume same request",busy:"Processing…",created:"Account saved. Activating selected inbounds…",pending:"Account saved; activation is not yet confirmed. Resume without another credit charge.",ready:"Client applied to all selected inbounds. Subscription delivery is available below.",saved:"Account saved; awaiting owner activation.",unknown:"Result not confirmed. Resume the same request; do not create another account.",invalid:"Check the name, duration, volume, IP limit and UUID.",storage:"Recovery storage unavailable. No new request was sent.",newClient:"Create another client",recovery:"Unfinished creation recovered",credential:"Re-enter the same custom UUID to retry creation",start:"Duration starts when the account is created.",selected:"selected",inspect:"View saved client"},
 fa:{title:"ایجاد کلاینت",manual:"دستی",preset:"مقادیر آماده",name:"نام کلاینت",random:"نام تصادفی",identity:"شناسه پیشرفته",id:"شناسه کلاینت (اختیاری)",uuid:"UUID (اختیاری)",uuidHelp:"برای تولید خودکار خالی بگذارید. UUID دستی باید یکتا باشد و در حافظهٔ مرورگر ذخیره نمی‌شود.",rep:"مالک حساب",plan:"نوع سرویس",volume:"حجم (گیگابایت)",days:"مدت (روز)",ips:"محدودیت IP",unlimited:"نامحدود",volumetric:"حجمی",targets:"انتخاب اینباندها",search:"جست‌وجوی اینباند",reload:"تازه‌سازی اینباندها",empty:"اینباند قابل انتخابی دریافت نشده است.",loadError:"فهرست اینباندها دریافت نشد؛ دوباره بررسی کنید.",deliver:"ساخت و فعال‌سازی",draft:"فقط ذخیره حساب",draftNote:"حساب ثبت و اعتبار آن کسر می‌شود؛ تحویل کانفیگ به فعال‌سازی توسط مالک نیاز دارد.",repNote:"فعال‌سازی اینباند فعلاً به مالک نیاز دارد. می‌توانید حساب را برای فعال‌سازی مالک ذخیره کنید.",save:"ساخت کلاینت",retry:"ادامه همان درخواست",busy:"در حال انجام…",created:"حساب ثبت شد؛ در حال فعال‌سازی اینباندهای انتخابی…",pending:"حساب ثبت شده؛ فعال‌سازی هنوز تأیید نشده است. ادامهٔ درخواست اعتبار دوباره کم نمی‌کند.",ready:"کلاینت روی همه اینباندهای انتخابی اعمال شد؛ دریافت اشتراک در پایین آماده است.",saved:"حساب ثبت شد؛ منتظر فعال‌سازی مالک است.",unknown:"نتیجه تأیید نشد؛ همان درخواست را ادامه دهید و حساب تازه نسازید.",invalid:"نام، مدت، حجم، محدودیت IP و UUID را بررسی کنید.",storage:"ذخیرهٔ درخواست برای بازیابی ممکن نیست؛ درخواست تازه‌ای ارسال نشد.",newClient:"ایجاد کلاینت دیگر",recovery:"درخواست ناتمام بازیابی شد",credential:"برای تکرار ساخت، همان UUID دستی را دوباره وارد کنید",start:"مدت سرویس از زمان ساخت حساب شروع می‌شود.",selected:"انتخاب شده",inspect:"مشاهده کلاینت ثبت‌شده"}
} as const;
export default function ClientCreateFlow({model:m}:{model:BusinessModel}) {
 const t=words[m.language],owner=m.session.user.role==="owner",key=createStorageKey(m.session.user.id);
 const [mode,setMode]=useState<"manual"|"preset">("manual"),[activate,setActivate]=useState(owner);
 const [choices,setChoices]=useState<Choice[]>([]),[selected,setSelected]=useState<number[]>([]),[query,setQuery]=useState("");
 const [loadError,setLoadError]=useState(false),[loading,setLoading]=useState(false);
 const [uuid,setUUID]=useState(""),[attempt,setAttempt]=useState<CreateAttempt|null>(null),[storageError,setStorageError]=useState(false);
 const [busy,setBusy]=useState(false),[notice,setNotice]=useState(""),[done,setDone]=useState(false);
 const live=useRef(true),serial=useRef(0),sending=useRef(false);
 useEffect(()=>{
  live.current=true;
  try {setAttempt(readCreateAttempt(sessionStorage.getItem(key),m.session.user.id));}
  catch {setStorageError(true);}
  return()=>{live.current=false;serial.current++;};
 },[key,m.session.user.id]);
 const remember=(a:CreateAttempt)=>{
  sessionStorage.setItem(key,JSON.stringify(a));
  if(live.current)setAttempt(a);
 };
 async function load() {
  if(!owner)return;
  const n=++serial.current;setLoading(true);setLoadError(false);
  try {
   const d=await api<{inbounds:Choice[]}>("/api/v1/workspace/native-inbounds-state");
   if(!Array.isArray(d.inbounds))throw Error("invalid_inbounds");
   if(live.current&&n===serial.current)setChoices(d.inbounds.filter(x=>Number.isSafeInteger(x.id)&&x.id>0&&typeof x.name==="string"&&!x.archived));
  } catch {if(live.current&&n===serial.current)setLoadError(true);}
  finally{if(live.current&&n===serial.current)setLoading(false);}
 }
 useEffect(()=>{void load();},[owner,key]);
 async function send(a:CreateAttempt) {
  if(sending.current||storageError)return;
  sending.current=true;setBusy(true);setNotice("");
  try {
   if(!a.created) {
    if(a.manual_uuid&&!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(uuid))throw Error("manual_uuid_required");
    if(a.inbound_ids.length)await preflightInbounds(a.inbound_ids);
    if(!live.current)return;
    a={...a,creation_sent:true};remember(a);
    const c=await mutation<{id:string}>("/api/v1/workspace/clients",{...a.payload,...(a.manual_uuid?{uuid}: {})},m.session.csrf_token);
    if(!live.current)return;
    if(c.id!==a.payload.client_id)throw Error("invalid_creation_receipt");
    a={...a,created:true};remember(a);setUUID("");setNotice(t.created);m.setRefreshKey(x=>x+1);
   }
   if(a.inbound_ids.length) {
    if(!owner)throw Error("owner_required");
    const ready=await deliverCreatedClient(a,m.session.csrf_token,remember,()=>live.current);
    if(!live.current)return;
    setDone(ready);setNotice(ready?t.ready:t.pending);
   } else {setDone(true);setNotice(t.saved);}
  } catch(e) {
   if(live.current)setNotice(e instanceof ApiError ? t.unknown+" ("+e.message+")" : e instanceof Error&&e.message==="manual_uuid_required"?t.credential:t.unknown);
  } finally {
   sending.current=false;
   if(live.current){setBusy(false);m.setRefreshKey(x=>x+1);}
  }
 }
 function submit(e:FormEvent) {
  e.preventDefault();if(attempt||busy||storageError)return;
  const days=Number(m.clientDuration),ips=Number(m.clientIPs),volume=m.clientKind==="volume"?bytesFromGB(m.clientVolume):0;
  if(!m.clientName.trim()||!Number.isSafeInteger(days)||days<1||days>3650||!Number.isSafeInteger(ips)||ips<0||ips>1000||
   (m.clientKind==="unlimited"&&ips<1)||volume===null||(m.clientKind==="volume"&&volume<=0)||
   (uuid&&!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(uuid))||
   (activate&&(!selected.length||loadError||loading))) {setNotice(t.invalid);return;}
  const a:CreateAttempt={version:1,owner:m.session.user.id,payload:{event_id:crypto.randomUUID(),client_id:m.clientId.trim()||uid(),
   name:m.clientName.trim(),representative_id:owner?m.clientRep:m.session.user.representative_id,duration_days:days,ip_count:ips,
   volume_bytes:volume,unlimited:m.clientKind==="unlimited"},inbound_ids:activate?[...selected].sort((a,b)=>a-b):[],
   manual_uuid:!!uuid,created:false,commands:{},submitted:[]};
  try{remember(a);}catch{setStorageError(true);setNotice(t.storage);return;}
  void send(a);
 }
 function editUnsent() {
  if(!attempt||attempt.created||attempt.creation_sent||busy)return;
  try{sessionStorage.removeItem(key);}catch{setStorageError(true);return;}
  m.setClientId(attempt.payload.client_id);m.setClientName(attempt.payload.name);m.setClientRep(attempt.payload.representative_id);m.setClientDuration(String(attempt.payload.duration_days));m.setClientIPs(String(attempt.payload.ip_count));m.setClientVolume(String(attempt.payload.volume_bytes/1073741824));m.setClientKind(attempt.payload.unlimited?"unlimited":"volume");setSelected(attempt.inbound_ids);setActivate(attempt.inbound_ids.length>0);setAttempt(null);setNotice("");
 }
 function reset() {
  if(!done||busy)return;
  try{sessionStorage.removeItem(key);}catch{setStorageError(true);return;}
  setAttempt(null);setDone(false);setNotice("");setUUID("");m.setClientId("");m.setClientName("");
 }
 return <section className="business-panel business-client-form client-create-flow" aria-label={t.title}>
  <h3>{t.title}</h3>
  {!attempt?<form onSubmit={submit}>
   <fieldset disabled={busy||storageError}>
    <div className="client-create-tabs">
     <button type="button" aria-pressed={mode==="manual"} onClick={()=>setMode("manual")}>{t.manual}</button>
     <button type="button" aria-pressed={mode==="preset"} onClick={()=>setMode("preset")}>{t.preset}</button>
    </div>
    <div className="business-fields">
     <label>{t.name}<div className="business-inline"><input aria-label={t.name} required maxLength={128} value={m.clientName} onChange={e=>m.setClientName(e.target.value)}/><button type="button" onClick={()=>m.setClientName("client-"+crypto.randomUUID().slice(0,8))}>{t.random}</button></div></label>
     {owner&&<label>{t.rep}<select aria-label={t.rep} value={m.clientRep} onChange={e=>m.setClientRep(e.target.value)}><option value="owner">Owner</option>{m.reps?.filter(r=>r.kind==="representative"&&r.enabled).map(r=><option key={r.id} value={r.id}>{r.display_name}</option>)}</select></label>}
     <label>{t.plan}<select aria-label={t.plan} value={m.clientKind} onChange={e=>{m.setClientKind(e.target.value as "volume"|"unlimited");if(e.target.value==="unlimited"&&m.clientIPs==="0")m.setClientIPs("1");}}><option value="volume">{t.volumetric}</option><option value="unlimited">{t.unlimited}</option></select></label>
     <label>{t.days}<input type="number" min={1} max={3650} step={1} required value={m.clientDuration} onChange={e=>m.setClientDuration(e.target.value)}/></label>
     <label>{t.ips}<input aria-label={t.ips} type="number" min={m.clientKind==="volume"?0:1} max={1000} step={1} required value={m.clientIPs} onChange={e=>m.setClientIPs(e.target.value)}/>{m.clientKind==="volume"&&<button type="button" onClick={()=>m.setClientIPs("0")}>{t.unlimited}</button>}</label>
     {m.clientKind==="volume"&&<label>{t.volume}<input type="number" min="0.01" step="any" required value={m.clientVolume} onChange={e=>m.setClientVolume(e.target.value)}/></label>}
    </div>
    <p>{t.start}</p>
    {mode==="preset"&&<ClientPlanWorkbench model={m}/>}
    <details><summary>{t.identity}</summary><div className="business-fields">
     <label>{t.id}<input maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,63}" value={m.clientId} onChange={e=>m.setClientId(e.target.value)}/></label>
     <label>{t.uuid}<input autoComplete="off" spellCheck={false} value={uuid} onChange={e=>setUUID(e.target.value.toLowerCase())}/></label>
    </div><p>{t.uuidHelp}</p></details>
    {owner?<div className="client-create-targets">
     <label><input type="checkbox" checked={activate} onChange={e=>setActivate(e.target.checked)}/>{activate?t.deliver:t.draft}</label>
     {activate&&<><h4>{t.targets} · {selected.length} {t.selected}</h4><input aria-label={t.search} placeholder={t.search} value={query} onChange={e=>setQuery(e.target.value)}/>
      <button type="button" disabled={loading} onClick={()=>void load()}>{t.reload}</button>
      {loadError?<p role="alert">{t.loadError}</p>:<div className="client-create-options">{choices.filter(x=>(x.name+" "+x.id).toLowerCase().includes(query.toLowerCase())).map(x=><label key={x.id}><input type="checkbox" checked={selected.includes(x.id)} disabled={!selected.includes(x.id)&&selected.length>=16} onChange={e=>setSelected(old=>e.target.checked?[...old,x.id]:old.filter(id=>id!==x.id))}/><span>{x.name}<small>#{x.id} · {[x.spec?.protocol,x.spec?.network,x.spec?.security].filter(Boolean).join(" / ")}</small></span></label>)}{!choices.length&&<p>{t.empty}</p>}</div>}</>}
    </div>:<p>{t.repNote}</p>}
    {!activate&&<p>{t.draftNote}</p>}
    <button type="submit" disabled={activate&&(!selected.length||loading||loadError)}>{activate?t.deliver:t.save}</button>
   </fieldset>
  </form>:<div className="client-create-recovery">
   <strong>{attempt.payload.name}</strong><p><bdi>{attempt.payload.client_id}</bdi> · {attempt.inbound_ids.join(", ")}</p>
   {!notice&&<p>{t.recovery}</p>}
   {!attempt.created&&attempt.manual_uuid&&<label>{t.credential}<input autoComplete="off" value={uuid} onChange={e=>setUUID(e.target.value.toLowerCase())}/></label>}
   {!done&&<button type="button" disabled={busy||storageError} onClick={()=>void send(attempt)}>{busy?t.busy:t.retry}</button>}
   {!attempt.created&&!attempt.creation_sent&&<button type="button" disabled={busy} onClick={editUnsent}>{m.language==="fa"?"ویرایش درخواست ارسال‌نشده":"Edit unsent request"}</button>}
   {attempt.created&&<button type="button" onClick={()=>m.setLifeClientID(attempt.payload.client_id)}>{t.inspect}</button>}
   {done&&<button type="button" onClick={reset}>{t.newClient}</button>}
  </div>}
  {storageError&&<p role="alert">{t.storage}</p>}
  {notice&&<p role="status">{notice}</p>}
  {attempt?.created&&attempt.inbound_ids.length>0&&<ClientInboundSubscription clientID={attempt.payload.client_id} language={m.language} session={m.session} canManage={true} refresh={m.refreshKey}/>}
 </section>;
}
