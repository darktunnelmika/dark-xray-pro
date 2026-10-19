import { useEffect, useRef, useState } from "react";
import { Activity, Ban, CheckCircle2, LockKeyhole, ShieldCheck, UserCog, X } from "lucide-react";
import { api, mutation } from "../session";
import { uid } from "./shared";
import type { BusinessModel } from "./useBusinessWorkspace";
import "./RepresentativeControlsV9.css";

type PermissionKey = "view_clients" | "create_clients" | "renew_clients" | "edit_clients" |
  "manage_clients" | "manage_groups" | "view_finance";
type Permissions = Record<PermissionKey,boolean>;
type Control = {
  representative: {id:string;display_name:string;kind:string;enabled:boolean;
    volume_credit_bytes:number;unlimited_credit_units:number};
  permissions: Permissions;login_suspended:boolean;revision:number;active_sessions:number;
};
type ActivitySummary = {
  representative_id:string;saved_clients:number;archived_clients:number;
  recorded_client_creations:number;recorded_client_renewals:number;
  recorded_profile_edits:number;recorded_credit_adjustments:number;
  available_volume_bytes:string;available_unlimited_units:string;metric_scope:string;
};
type ControlEvent = {
  event_id:string;representative_id:string;previous_name:string;new_name:string;
  previous_login_suspended:boolean;new_login_suspended:boolean;
  reason:string;created_unix:number;
};
const permissionKeys:PermissionKey[]=[
  "view_clients","create_clients","renew_clients","edit_clients",
  "manage_clients","manage_groups","view_finance",
];
const dict={
  en:{
    title:"Representative account & permissions",subtitle:"Only the owner can change a representative's login and capabilities.",
    name:"Display name",status:"Suspend representative LOGIN (clients and credits remain untouched)",
    reason:"Required audit reason",save:"Review and save account changes",cancel:"Close account manager",
    active:"Login available",suspended:"Login suspended",noData:"Could not verify account settings. Actions are blocked.",
    wait:"Loading verified account, history and real activity…",
    invalid:"Name, reason and an actual change are required.",
    ok:"Settings saved with immutable audit. Existing sessions revoked.",
    fail:"Update failed or changed concurrently. Check the current account before retry.",
    confirmation:"Apply these audited account settings? All existing representative sessions will be revoked. Customer records, credits and Xray remain unchanged.",
    rights:"Permission switches",balance:"Available credit",units:"units",
    summary:"Verified saved activity (NOT online traffic)",
    saved:"Saved clients",archived:"Archived clients",created:"Recorded sales",
    renewed:"Recorded client renewals",profiles:"Recorded client edits",credit:"Credit adjustments",
    sessions:"Active login sessions",audits:"Account control history",none:"No control changes recorded",
    old:"Previous",next:"New",restricted:"This suspends panel login only, never removes customers or changes Xray.",
    permissionLabels:{
      view_clients:"View clients & group lists",create_clients:"Create new clients",
      renew_clients:"Renew / top up",edit_clients:"Edit client profiles",
      manage_clients:"Enable, disable and archive clients",
      manage_groups:"Edit groups and membership",
      view_finance:"View private financial events and activity",
    },
  },
  fa:{
    title:"مدیریت حساب و دسترسی نماینده",subtitle:"فقط مالک اجازهٔ تغییر وضعیت ورود و مجوزهای نماینده را دارد.",
    name:"نام نماینده",status:"مسدودسازی ورود نماینده (بدون دست‌زدن به مشتریان و اعتبار)",
    reason:"دلیل ضروری برای حسابرسی",save:"بررسی و ذخیرهٔ تنظیمات",cancel:"بستن مدیریت حساب",
    active:"ورود مجاز",suspended:"ورود مسدود",
    noData:"تنظیمات حساب تأیید نشد؛ ویرایش مجاز نیست.",
    wait:"در حال دریافت حساب، تاریخچه و آمار واقعی…",
    invalid:"نام، دلیل حسابرسی و یک تغییر واقعی لازم است.",
    ok:"تنظیمات با تاریخچهٔ دائمی ثبت و نشست‌های قبلی باطل شدند.",
    fail:"ثبت ناموفق بود یا وضعیت هم‌زمان تغییر کرد؛ حساب را دوباره بررسی کن.",
    confirmation:"این تغییرات ثبت و تمام نشست‌های قبلی نماینده باطل شوند؟ کلاینت‌ها، اعتبار و Xray بدون تغییر می‌مانند.",
    rights:"مجوزهای اختصاصی",balance:"اعتبار موجود",units:"واحد",
    summary:"آمار ثبت‌شدهٔ واقعی (نه ترافیک آنلاین)",
    saved:"کلاینت‌های ثبت‌شده",archived:"کلاینت‌های آرشیوشده",created:"فروش‌های ثبت‌شده",
    renewed:"تمدیدهای ثبت‌شده",profiles:"ویرایش‌های کلاینت",credit:"تراکنش‌های تغییر اعتبار",
    sessions:"نشست‌های ورود فعال",audits:"تاریخچهٔ تغییر دسترسی",none:"هیچ تغییری ثبت نشده",
    old:"قبلی",next:"جدید",restricted:"مسدودسازی فقط ورود پنل را قطع می‌کند؛ مشتری و Xray دست‌کاری نمی‌شوند.",
    permissionLabels:{
      view_clients:"مشاهدهٔ کلاینت‌ها و گروه‌ها",create_clients:"ساخت کلاینت",
      renew_clients:"تمدید و افزودن حجم",edit_clients:"ویرایش مشخصات کلاینت",
      manage_clients:"فعال‌سازی، غیرفعال‌سازی و آرشیو",
      manage_groups:"ویرایش گروه و اعضا",view_finance:"مشاهدهٔ حسابداری و گزارش‌های اختصاصی",
    },
  },
} as const;
function positive(n:unknown):n is number{return Number.isSafeInteger(n)&&Number(n)>=0;}
function validControl(value:Control,id:string):boolean{
 const c=value;
 if(!c||!c.representative||c.representative.id!==id||
    c.representative.kind!=="representative"||typeof c.representative.enabled!=="boolean"||
    typeof c.representative.display_name!=="string"||
    !positive(c.revision)||!positive(c.active_sessions)||
    !(Number.isFinite(c.representative.volume_credit_bytes)&&c.representative.volume_credit_bytes>=0)||!positive(c.representative.unlimited_credit_units)||
    typeof c.login_suspended!=="boolean"||!c.permissions)return false;
 return permissionKeys.every(p=>typeof c.permissions[p]==="boolean");
}
function validActivity(a:ActivitySummary,id:string):boolean{
 if(!a||a.representative_id!==id||
    a.metric_scope!=="saved_records_and_immutable_events_not_live_xray_traffic"||
    !/^\d+$/.test(a.available_volume_bytes)||!/^\d+$/.test(a.available_unlimited_units))return false;
 return ["saved_clients","archived_clients","recorded_client_creations",
   "recorded_client_renewals","recorded_profile_edits","recorded_credit_adjustments"]
  .every(k=>positive((a as unknown as Record<string,unknown>)[k])) &&
  a.archived_clients<=a.saved_clients;
}
function integerText(v:string):string{
 try{return BigInt(v).toLocaleString("en-US");}catch{return "—";}
}
function volumeText(v:string):string{
 try{
  const b=BigInt(v),g=1024n**3n;
  return (b/g).toLocaleString("en-US")+"."+
   ((b%g)*100n/g).toString().padStart(2,"0")+" GB";
 }catch{return "—";}
}
export default function RepresentativeControlsV9({model,representativeID,onClose}:{
 model:BusinessModel;representativeID:string;onClose:()=>void;
}){
 const t=dict[model.language];
 const [control,setControl]=useState<Control|null>(null);
 const [activity,setActivity]=useState<ActivitySummary|null>(null);
 const [events,setEvents]=useState<ControlEvent[]|null>(null);
 const [name,setName]=useState("");
 const [suspended,setSuspended]=useState(false);
 const [rights,setRights]=useState<Permissions|null>(null);
 const [reason,setReason]=useState("");
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState("");
 const [loadedKey,setLoadedKey]=useState("");
 const [loadVersion,setLoadVersion]=useState(0);
 const pending=useRef<{key:string;eventID:string}|null>(null);
 const fetchKey=model.session.user.id+":"+representativeID+":"+model.refreshKey+":"+loadVersion;
 useEffect(()=>{
  const ctrl=new AbortController();
  setControl(null);setActivity(null);setEvents(null);setLoadedKey("");setError("");
  const base="/api/v1/workspace/representatives/"+encodeURIComponent(representativeID);
  void Promise.all([
   api<Control>(base+"/control",{signal:ctrl.signal}),
   api<ActivitySummary>(base+"/activity",{signal:ctrl.signal}),
   api<{events:ControlEvent[];limit:number;offset:number;audit:string}>(
    base+"/control/events?limit=25&offset=0",{signal:ctrl.signal}),
  ]).then(([current,report,audit])=>{
    if(ctrl.signal.aborted)return;
    if(!validControl(current,representativeID)||!validActivity(report,representativeID)||
       !audit||!Array.isArray(audit.events)||audit.events.length>25||
       audit.limit!==25||audit.offset!==0||audit.audit!=="append_only"||
       audit.events.some(x=>x.representative_id!==representativeID||
         typeof x.reason!=="string"||!positive(x.created_unix)))
      throw Error("unverified_representative_control");
    setControl(current);setActivity(report);setEvents(audit.events);
    setName(current.representative.display_name);
    setSuspended(current.login_suspended);
    setRights({...current.permissions});setReason("");
    setLoadedKey(fetchKey);pending.current=null;
  }).catch(()=>{
    if(ctrl.signal.aborted)return;
    setControl(null);setActivity(null);setEvents(null);setLoadedKey("");setError(t.noData);
  });
  return ()=>ctrl.abort();
 },[fetchKey,representativeID,t.noData]);
 const current=loadedKey===fetchKey?control:null;
 const payload=current&&rights?{
  representative_id:representativeID,expected_revision:current.revision,
  display_name:name.trim(),login_suspended:suspended,permissions:rights,reason:reason.trim(),
 }:null;
 const key=JSON.stringify(payload);
 const changed=!!current&&!!rights&&(
  current.representative.display_name!==name.trim()||current.login_suspended!==suspended||
  permissionKeys.some(p=>current.permissions[p]!==rights[p]));
 const valid=changed && name.trim().length>0&&name.trim().length<=128&&
  reason.trim().length>0&&reason.trim().length<=200 &&
  !busy&&!model.actionBusy;
 useEffect(()=>{
  if(pending.current?.key!==key)pending.current=null;
 },[key]);
 async function apply(){
  if(!valid||!payload){setError(t.invalid);return}
  if(!window.confirm(t.confirmation))return;
  const eventID=pending.current?.key===key?pending.current.eventID:uid();
  pending.current={key,eventID};
  setBusy(true);model.setActionBusy(true);setError("");
  try{
   const receipt=await mutation<{control:Control;replayed:boolean;sessions_revoked:boolean;
    balance_changed:boolean;clients_deleted:boolean;customer_traffic_changed:boolean}>(
    "/api/v1/workspace/representatives/"+encodeURIComponent(representativeID)+"/control",
    {...payload,event_id:eventID},model.session.csrf_token);
   if(!validControl(receipt.control,representativeID)||
     receipt.balance_changed!==false||receipt.clients_deleted!==false||
     receipt.customer_traffic_changed!==false)throw Error("unverified_control_receipt");
   pending.current=null;
   model.setNotice(t.ok);
   model.reload();
   setLoadVersion(v=>v+1);
  }catch{setError(t.fail)}
  finally{setBusy(false);model.setActionBusy(false);}
 }
 return <section className="business-panel rep-admin-v9" aria-label="Representative account manager">
  <header className="rep-admin-heading">
   <div><h3><UserCog size={19}/>{t.title}</h3><p>{t.subtitle}</p></div>
   <button type="button" onClick={onClose} disabled={busy} aria-label={t.cancel}>
    <X size={17}/>{t.cancel}</button>
  </header>
  {!current||!rights||!activity||!events?<p role="status">{error||t.wait}</p>:<>
   <div className="rep-admin-identity">
    <strong>{current.representative.display_name}</strong>
    <code dir="ltr">{representativeID}</code>
    <span>{suspended?<><Ban size={13}/>{t.suspended}</>:<><ShieldCheck size={13}/>{t.active}</>}</span>
    <span>{t.sessions}: {current.active_sessions}</span>
   </div>
   <div className="rep-admin-fields">
    <label>{t.name}<input aria-label="Representative display name" maxLength={128} value={name}
      disabled={busy||model.actionBusy} onChange={e=>setName(e.target.value)}/></label>
    <label className="rep-admin-suspension">
     <input type="checkbox" aria-label="Suspend representative login" checked={suspended}
      disabled={busy||model.actionBusy} onChange={e=>setSuspended(e.target.checked)}/>
     <span><LockKeyhole size={15}/>{t.status}</span>
    </label>
   </div>
   <fieldset className="rep-admin-permissions">
    <legend><ShieldCheck size={16}/>{t.rights}</legend>
    <div>
     {permissionKeys.map(k=><label key={k}>
       <input type="checkbox" aria-label={"Permission "+k} checked={rights[k]}
        disabled={busy||model.actionBusy}
        onChange={e=>setRights(prev=>prev?{...prev,[k]:e.target.checked}:prev)}/>
       <span>{t.permissionLabels[k]}</span>
     </label>)}
    </div>
   </fieldset>
   <label className="rep-admin-reason">{t.reason}
    <input aria-label="Representative audit reason" maxLength={200} value={reason}
      disabled={busy||model.actionBusy} onChange={e=>setReason(e.target.value)}/>
   </label>
   <p className="rep-admin-safety"><ShieldCheck size={14}/>{t.restricted}</p>
   <button type="button" className="rep-admin-save" disabled={!valid} onClick={()=>void apply()}>
    <CheckCircle2 size={15}/>{t.save}</button>
   {error&&<p role="alert" className="rep-admin-error">{error}</p>}
   <div className="rep-admin-stat-panel">
    <header><h4><Activity size={16}/>{t.summary}</h4></header>
    <div className="rep-admin-metrics">
     {([
      [t.saved,activity.saved_clients],[t.archived,activity.archived_clients],
      [t.created,activity.recorded_client_creations],
      [t.renewed,activity.recorded_client_renewals],
      [t.profiles,activity.recorded_profile_edits],
      [t.credit,activity.recorded_credit_adjustments],
     ] as [string,number][]).map(([label,value])=>
      <div key={label}><span>{label}</span><strong>{value.toLocaleString("en-US")}</strong></div>)}
    </div>
    <p>{t.balance}: {volumeText(activity.available_volume_bytes)} · {integerText(activity.available_unlimited_units)} {t.units}</p>
   </div>
   <div className="rep-admin-history">
    <h4>{t.audits}</h4>
    {events.length?events.map(event=><div key={event.event_id}>
      <strong>{event.reason}</strong>
      <span>{t.old}: {event.previous_name} → {t.next}: {event.new_name}</span>
      <span>{new Date(event.created_unix*1000).toLocaleDateString(model.language==="fa"?"fa-IR":"en-GB")}</span>
     </div>):<p>{t.none}</p>}
   </div>
  </>}
 </section>;
}
