import { useEffect, useRef, useState } from "react";
import { AlertTriangle, CheckCircle2, PencilLine, ShieldCheck, Wallet } from "lucide-react";
import { api, mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { bytesFromGB, dateTime, uid, type Client } from "./shared";
import "./ClientProfileEditor.css";

type SavedClient=Client & {uuid:string;charged_volume_bytes:number;charged_unlimited_units:number};
type EditAudit={
  event_id:string;client_id:string;previous_name:string;new_name:string;
  previous_ip_count:number;new_ip_count:number;added_days:number;
  added_volume_bytes:string;volume_credit_debit_bytes:string;unlimited_credit_debit_units:string;
  reason:string;created_unix:number;
};
type EditIntent={
  client_id:string;name:string;plan_kind:string;ip_count:number;
  additional_days:number;additional_volume_bytes:number;reason:string;
};
type EditQuote={
  quote_hash:string;quote_unix:number;client_id:string;representative_id:string;plan_kind:string;
  previous_name:string;new_name:string;previous_ip_count:number;new_ip_count:number;
  previous_expires_unix:number;new_expires_unix:number;added_volume_bytes:string;
  volume_credit_debit_bytes:string;unlimited_credit_debit_units:string;
  available_volume_credit_bytes:string;available_unlimited_units:string;
  owner_exempt:boolean;sufficient:boolean;applied_to_xray:boolean;credit_refunded:boolean;
};
const words={
  en:{
    title:"Edit saved client safely", intro:"Edit name, IP/user slots and add purchased time or GB without changing the UUID.",
    pending:"Loading verified client and edit history…",invalid:"Unavailable or unverified client. Editing is blocked.",
    name:"Display name",slots:"User / IP slots",duration:"Add days",volume:"Add GB",reason:"Audit reason",
    kind:"Current plan type",noSwitch:"Changing between volume and unlimited is locked until real metering migration is certified.",
    maxSeats:"Decreasing paid unlimited user slots is deferred; never refund or strand a renewal.",
    unlimitedIP:"For volume plans, 0 means unlimited IPs. Changes do not cost volume credits.",
    daysHelp:"Expiry only extends; old usage and expiry history never reset.",
    review:"Review authoritative price",apply:"Confirm and save edit",quote:"Verified account quote",
    insufficient:"Representative credit is insufficient. Saving is disabled.",
    before:"Before",after:"After",newExpiry:"New expiry estimate",available:"Available",debit:"Debit",
    units:"unlimited units",gb:"GB credit",owner:"Owner account: no representative credit deducted",
    confirmation:"Confirm this recorded client edit and its credit debit? UUID, usage and disabled state remain intact.",
    success:"Client edit saved with an immutable audit receipt.",cancel:"No edit was applied.",
    failed:"Quote failed. Confirm the client, price and balance, then review again.",
    stale:"Edit failed or credit changed. Recheck the stored client and request a fresh quote.",
    audit:"Previous profile changes",none:"No profile edits recorded",at:"Recorded",
    immutable:"UUID, historical bytes and representative ownership are preserved. Xray/Subscription apply is NOT performed.",
    missing:"Provide valid terms, a change and a reason. New volume is unavailable for unlimited plans.",
  },
  fa:{
    title:"ویرایش امن مشخصات کلاینت",intro:"ویرایش نام، تعداد IP/کاربر و افزودن زمان یا حجم بدون تغییر UUID.",
    pending:"در حال بارگذاری اطلاعات معتبر و تاریخچهٔ ویرایش…",invalid:"اطلاعات کلاینت تأیید نشد؛ ویرایش مسدود است.",
    name:"نام کلاینت",slots:"تعداد کاربر / IP",duration:"افزودن روز",volume:"افزودن گیگابایت",reason:"دلیل حسابرسی",
    kind:"نوع پلن فعلی",noSwitch:"تغییر حجمی و نامحدود تا تأیید مهاجرت حسابداری واقعی مسدود است.",
    maxSeats:"کاهش کاربرانِ نامحدودِ از قبل پرداخت‌شده تا تعیین سیاست مالی جداگانه مجاز نیست.",
    unlimitedIP:"در پلن حجمی، صفر به معنی IP نامحدود است؛ تغییر IP هزینهٔ حجمی ندارد.",
    daysHelp:"فقط تمدید به جلو انجام می‌شود؛ مصرف و تاریخچهٔ انقضای قبلی پاک نمی‌شوند.",
    review:"بررسی هزینه در HUB",apply:"تأیید و ثبت ویرایش",quote:"پیش‌نمایش اعتبار تأییدشده",
    insufficient:"اعتبار نماینده کافی نیست و ثبت مجاز نیست.",
    before:"قبل",after:"بعد",newExpiry:"انقضای جدید تخمینی",available:"موجودی",debit:"کسر اعتبار",
    units:"واحد نامحدود",gb:"اعتبار گیگابایت",owner:"حساب مالک: اعتبار نماینده کسر نمی‌شود",
    confirmation:"این ویرایش و کسر اعتبار را تأیید می‌کنی؟ UUID، تاریخچه و وضعیت قطع دستی محفوظ می‌مانند.",
    success:"ویرایش همراه با حسابرسی تغییرناپذیر ثبت شد.",cancel:"ویرایشی انجام نشد.",
    failed:"پیش‌نمایش قابل‌تأیید نیست؛ اطلاعات کلاینت یا اعتبار را دوباره بررسی کن.",
    stale:"ویرایش ناموفق یا اعتبار تغییر کرده؛ اطلاعات را دوباره بارگذاری و قیمت را بررسی کن.",
    audit:"تاریخچهٔ ویرایش مشخصات",none:"هنوز ویرایش مشخصاتی ثبت نشده",at:"ثبت‌شده",
    immutable:"UUID، مصرف قبلی و مالکیت نماینده حفظ می‌شوند و هیچ Xray یا ساب جدیدی فعال نمی‌شود.",
    missing:"مقادیر، تغییر واقعی و دلیل را درست وارد کن؛ برای نامحدود افزایش حجم مجاز نیست.",
  },
} as const;
function quota(s:string):string {
  try{return BigInt(s).toLocaleString("en-US");}catch{return "—";}
}
function gbText(s:string):string{
  try{
    const n=BigInt(s),g=1024n**3n;
    return (n/g).toLocaleString("en-US")+"."+
      ((n%g)*100n/g).toString().padStart(2,"0")+" GB";
  }catch{return "—";}
}
function validSaved(c:SavedClient,id:string,model:BusinessModel):boolean{
 return !!c && c.id===id && typeof c.name==="string" &&
   typeof c.uuid==="string" && c.uuid.length>=8 &&
   (c.plan_kind==="volume"||c.plan_kind==="unlimited") &&
   Number.isSafeInteger(c.ip_count)&&c.ip_count>=0&&c.ip_count<=1000&&
   (c.plan_kind==="volume"||c.ip_count>=1) &&
   Number.isSafeInteger(c.duration_days)&&c.duration_days>=1&&c.duration_days<=3650&&
   Number.isFinite(c.volume_bytes)&&c.volume_bytes>=0 &&
   Number.isSafeInteger(c.expires_unix)&&typeof c.archived==="boolean" &&
   (model.session.user.role==="owner"||c.representative_id===model.session.user.representative_id);
}
function validQuote(q:EditQuote,intent:EditIntent,c:SavedClient):boolean{
 return !!q&&/^[a-f0-9]{64}$/.test(q.quote_hash)&&Number.isSafeInteger(q.quote_unix)&&q.quote_unix>0 &&
   q.client_id===c.id&&q.representative_id===c.representative_id&&
   q.plan_kind===c.plan_kind&&q.previous_name===c.name&&q.new_name===intent.name &&
   q.previous_ip_count===c.ip_count&&q.new_ip_count===intent.ip_count &&
   q.previous_expires_unix===c.expires_unix&&Number.isSafeInteger(q.new_expires_unix)&&
   typeof q.owner_exempt==="boolean"&&typeof q.sufficient==="boolean"&&
   q.applied_to_xray===false&&q.credit_refunded===false&&
   ["added_volume_bytes","volume_credit_debit_bytes","unlimited_credit_debit_units",
   "available_volume_credit_bytes","available_unlimited_units"].every(k=>
    /^\d+$/.test((q as unknown as Record<string,string>)[k]))&&
   q.added_volume_bytes===String(intent.additional_volume_bytes);
}
export default function ClientProfileEditor({model,clientID}:{
 model:BusinessModel;clientID:string;
}){
 const t=words[model.language];
 const [loaded,setLoaded]=useState<SavedClient|null>(null);
 const [history,setHistory]=useState<EditAudit[]|null>(null);
 const [name,setName]=useState("");
 const [slots,setSlots]=useState("");
 const [days,setDays]=useState("0");
 const [volume,setVolume]=useState("0");
 const [reason,setReason]=useState("");
 const [quote,setQuote]=useState<EditQuote|null>(null);
 const [quoteKey,setQuoteKey]=useState("");
 const [error,setError]=useState("");
 const [busy,setBusy]=useState(false);
 const [loadedKey,setLoadedKey]=useState("");
 const pending=useRef<{key:string;eventID:string}|null>(null);
 const fetchKey=model.session.user.id+":"+clientID+":"+model.refreshKey;
 useEffect(()=>{
  const controller=new AbortController();
  setLoaded(null);setHistory(null);setLoadedKey("");setQuote(null);setQuoteKey("");
  setError("");
  const root="/api/v1/workspace/clients/"+encodeURIComponent(clientID);
  void Promise.all([
    api<SavedClient>(root,{signal:controller.signal}),
    api<{edits:EditAudit[];limit:number;offset:number;audit:string}>(root+"/edits?limit=20&offset=0",{signal:controller.signal}),
  ]).then(([c,h])=>{
    if(controller.signal.aborted)return;
    if(!validSaved(c,clientID,model)||c.archived||
      !h||!Array.isArray(h.edits)||h.edits.length>20||h.limit!==20||h.offset!==0||
      h.audit!=="append_only"||h.edits.some(e=>e.client_id!==clientID||
       typeof e.new_name!=="string"||!/^\d+$/.test(e.unlimited_credit_debit_units)))
      throw Error("unverified_profile");
    setLoaded(c);setHistory(h.edits);setLoadedKey(fetchKey);
    setName(c.name);setSlots(String(c.ip_count));setDays("0");setVolume("0");setReason("");
    pending.current=null;
  }).catch(()=>{
    if(controller.signal.aborted)return;
    setLoaded(null);setHistory(null);setLoadedKey("");setError(t.invalid);
  });
  return ()=>controller.abort();
  // model is stable for the current session role; refreshKey controls reload.
  // eslint-disable-next-line react-hooks/exhaustive-deps
 },[fetchKey,clientID,model.session.user.id,model.session.user.role,model.session.user.representative_id]);
 const client=loadedKey===fetchKey?loaded:null;
 const newIP=Number(slots),addDays=Number(days);
 const addedVolume=volume.trim()==="0"?0:bytesFromGB(volume);
 const intent:EditIntent={client_id:clientID,name:name.trim(),plan_kind:client?.plan_kind||"",
   ip_count:newIP,additional_days:addDays,additional_volume_bytes:addedVolume??-1,reason:reason.trim()};
 const canEdit=!!client && !busy && !model.actionBusy &&
   Number.isSafeInteger(newIP)&&newIP>=0&&newIP<=1000&&
   (client.plan_kind==="volume"||newIP>=client.ip_count)&&
   (client.plan_kind==="volume"||newIP>=1)&&
   Number.isSafeInteger(addDays)&&addDays>=0&&addDays<=3650 &&
   client.duration_days+addDays<=3650&&addedVolume!==null&&addedVolume>=0&&
   (client.plan_kind==="volume"||addedVolume===0)&&
   intent.name.length>=1&&intent.name.length<=128&&intent.reason.length>=1&&intent.reason.length<=200&&
   (intent.name!==client.name||newIP!==client.ip_count||addDays>0||addedVolume>0);
 const key=JSON.stringify([model.session.user.id,intent]);
 const reviewedQuote=canEdit&&quoteKey===key?quote:null;
 useEffect(()=>{
   setQuote(null);setQuoteKey("");setError("");
   if(pending.current?.key!==key)pending.current=null;
 },[key]);
 async function review(){
  if(!canEdit){setError(t.missing);return}
  setBusy(true);setError("");setQuote(null);setQuoteKey("");
  try{
   const q=await mutation<EditQuote>("/api/v1/workspace/clients/"+encodeURIComponent(clientID)+"/edit/quote",
    intent,model.session.csrf_token);
   if(!validQuote(q,intent,client!))throw Error("invalid_quote");
   setQuote(q);setQuoteKey(key);
  }catch{
   setError(t.failed);
  }finally{setBusy(false)}
 }
 async function save(){
  if(!reviewedQuote||!reviewedQuote.sufficient||!canEdit)return;
  if(!window.confirm(t.confirmation)){setError(t.cancel);return}
  const id=pending.current?.key===key?pending.current.eventID:uid();
  pending.current={key,eventID:id};
  setBusy(true);model.setActionBusy(true);setError("");
  try{
   const receipt=await mutation<{client:SavedClient;replayed:boolean;applied_to_xray:boolean;
    credit_refunded:boolean;customer_traffic_changed:boolean;client_or_usage_history_deleted:boolean}>(
    "/api/v1/workspace/clients/"+encodeURIComponent(clientID)+"/edit/commit",
    {...intent,event_id:id,quote_hash:reviewedQuote.quote_hash,quote_unix:reviewedQuote.quote_unix},
    model.session.csrf_token);
   if(!validSaved(receipt.client,clientID,model)||receipt.applied_to_xray!==false||
      receipt.credit_refunded!==false||receipt.customer_traffic_changed!==false||
      receipt.client_or_usage_history_deleted!==false)throw Error("unverified_edit_receipt");
   pending.current=null;setQuote(null);setQuoteKey("");
   model.setNotice(t.success);model.reload();
  }catch{setError(t.stale)}
  finally{setBusy(false);model.setActionBusy(false)}
 }
 return <section className="business-panel profile-edit" aria-label="Client profile editor">
   <header><h3><PencilLine size={17}/>{t.title}</h3><p>{t.intro}</p></header>
   {!client ? <p role="status">{error||t.pending}</p> : <>
    <div className="profile-edit-summary">
     <strong>{client.name}</strong><code dir="ltr">{client.id}</code>
     <span>{t.kind}: <b>{client.plan_kind==="volume"?"Volume":"Unlimited"}</b></span>
     <span>{t.daysHelp} {dateTime(client.expires_unix,model.language)}</span>
    </div>
    <div className="profile-edit-fields">
     <label>{t.name}<input aria-label="Edit client name" maxLength={128} value={name}
       disabled={busy||model.actionBusy} onChange={e=>setName(e.target.value)}/></label>
     <label>{t.slots}<input aria-label="Edit client IP slots" type="number" min={client.plan_kind==="volume"?0:client.ip_count}
       max={1000} step={1} value={slots} disabled={busy||model.actionBusy} onChange={e=>setSlots(e.target.value)}/></label>
     <label>{t.duration}<input aria-label="Edit client additional days" type="number" min={0} max={3650-client.duration_days}
       step={1} value={days} disabled={busy||model.actionBusy} onChange={e=>setDays(e.target.value)}/></label>
     <label>{t.volume}<input aria-label="Edit client additional GB" type="number" min={0} step="any" value={volume}
       disabled={busy||model.actionBusy||client.plan_kind!=="volume"} onChange={e=>setVolume(e.target.value)}/></label>
     <label className="profile-edit-reason">{t.reason}<input aria-label="Edit client audit reason"
       maxLength={200} value={reason} disabled={busy||model.actionBusy} onChange={e=>setReason(e.target.value)}/></label>
    </div>
    <p className="profile-edit-hint">{client.plan_kind==="volume"?t.unlimitedIP:t.maxSeats}</p>
    <p className="profile-edit-hint">{t.noSwitch}</p>
    <button type="button" className="profile-edit-review" disabled={!canEdit}
      onClick={()=>void review()}><Wallet size={15}/>{t.review}</button>
    {reviewedQuote&&<div role="region" aria-label="Verified client edit quote" className="profile-edit-quote">
      <strong><ShieldCheck size={15}/>{t.quote}</strong>
      <div><span>{t.name}: {t.before}</span><b>{reviewedQuote.previous_name}</b>
        <span>{t.after}</span><b>{reviewedQuote.new_name}</b></div>
      <div><span>{t.slots}</span><b>{reviewedQuote.previous_ip_count} → {reviewedQuote.new_ip_count}</b></div>
      <div><span>{t.newExpiry}</span><b>{dateTime(reviewedQuote.new_expires_unix,model.language)}</b></div>
      {reviewedQuote.owner_exempt?<p>{t.owner}</p>:<>
       <div><span>{t.gb} ({t.debit} / {t.available})</span>
         <b>{gbText(reviewedQuote.volume_credit_debit_bytes)} / {gbText(reviewedQuote.available_volume_credit_bytes)}</b></div>
       <div><span>{t.units} ({t.debit} / {t.available})</span>
         <b>{quota(reviewedQuote.unlimited_credit_debit_units)} / {quota(reviewedQuote.available_unlimited_units)}</b></div>
      </>}
      {!reviewedQuote.sufficient&&<p className="profile-edit-danger"><AlertTriangle size={15}/>{t.insufficient}</p>}
      <button type="button" className="profile-edit-commit"
       onClick={()=>void save()} disabled={!canEdit||!reviewedQuote.sufficient}>
       <CheckCircle2 size={15}/>{t.apply}</button>
    </div>}
    {error&&<p role="alert" className="profile-edit-danger">{error}</p>}
    <p className="profile-edit-hint"><ShieldCheck size={14}/>{t.immutable}</p>
    <div className="profile-edit-history">
      <h4>{t.audit}</h4>
      {history?.length?history.map(e=><div key={e.event_id}>
       <strong>{dateTime(e.created_unix,model.language)} — {e.reason}</strong>
       <span>{e.previous_name} → {e.new_name}</span>
       <span>IP {e.previous_ip_count} → {e.new_ip_count} · +{e.added_days}d</span>
       <small>{t.debit}: {gbText(e.volume_credit_debit_bytes)} / {quota(e.unlimited_credit_debit_units)} {t.units}</small>
      </div>):<p>{t.none}</p>}
    </div>
   </>}
 </section>;
}
