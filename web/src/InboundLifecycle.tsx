import { useEffect, useRef, useState, type FormEvent } from "react";
import { Archive, Edit3, History, PauseCircle, PlayCircle, ShieldCheck } from "lucide-react";
import { profileOptionsDraft, profileOptionsPayload, type InboundProfileOptions } from "./inboundProfileOptions";
import InboundProfileForm, { type InboundProfileFormHandle, type InboundProfileValue } from "./InboundProfileForm";

type Lang = "en" | "fa";
export type LifecycleInbound = { id: number; name: string; transport: string; grpc_service: string; reality_target: string; server_names: string[]; short_ids: string[]; public_key: string; revision: number; enabled: boolean; archived: boolean; options?: InboundProfileOptions; security?: string };
type Operation = { event_id: string; action: string; reason: string; from_revision: number; to_revision: number; created_unix: number };
type Profile = Pick<LifecycleInbound, "id" | "name" | "transport" | "grpc_service" | "reality_target" | "server_names" | "short_ids" | "options">;
type Intent = { action: "edit" | "enable" | "disable" | "archive"; reason: string; profile?: Profile };
const words = {
 en: {
  title: "Edit and protect this Inbound", edit: "Edit profile", name: "Name", transport: "Transport", service: "gRPC service", dest: "REALITY target", sni: "Server names", reason: "Reason for change", save: "Save profile changes", cancel: "Cancel editing", revision: "Profile revision", active: "Enabled", disabled: "Disabled", archived: "Archived", enable: "Enable Inbound", disable: "Disable Inbound", archive: "Delete safely (archive)",
  preserve: "Editing preserves the REALITY key. Disabling closes this Inbound's ports after Agent Apply. Safe deletion retains clients, UUIDs, bindings and financial history.",
  confirmEdit: "Update this Inbound? Its opted-in PRO Agent may restart Xray, interrupting connections. The private key will not rotate.",
  confirmDisable: "Disable this Inbound? Its listener will be removed on the next successful Agent Apply. Client records and credit history will remain.",
  confirmEnable: "Enable this Inbound? Eligible bound clients may regain access after Agent Apply.",
  confirmArchive: "Archive this Inbound permanently? It will stop accepting traffic after Agent Apply, without deleting clients or refunding credit. Reactivation is not available.",
  changed: "This profile changed after editing began. Cancel and reopen the editor to load the latest version.", conflict: "The profile changed or this operation conflicts. Refresh and review before retrying.", error: "The result could not be confirmed. Retrying the same change uses the same operation ID.", saved: "Change recorded; a fresh Agent report is still required.", history: "Inbound change history", empty: "No recorded changes.", previous: "Previous", next: "Next", historyError: "Change history is unavailable.",
  more: "More actions", keyHint: "The existing identity and REALITY key are preserved.",
  setupTitle: "Saved setup controls", archiveSetup: "Archive saved setup", setupPreserve: "Archiving cancels this paused setup and releases its planned ports. The saved configuration and history remain available.", confirmArchiveSetup: "Permanently archive this paused setup and release its planned ports? Its saved configuration and history will remain available.", setupArchived: "The saved setup was archived and its planned ports were released.",
 },
 fa: {
  title: "ویرایش و کنترل اینباند", edit: "ویرایش پروفایل", name: "نام", transport: "ترنسپورت", service: "سرویس gRPC", dest: "مقصد REALITY", sni: "نام‌های سرور", reason: "دلیل تغییر", save: "ذخیره تغییرات پروفایل", cancel: "انصراف از ویرایش", revision: "نسخهٔ پروفایل", active: "فعال", disabled: "غیرفعال", archived: "آرشیوشده", enable: "فعال‌کردن اینباند", disable: "غیرفعال‌کردن اینباند", archive: "حذف ایمن (آرشیو)",
  preserve: "ویرایش، کلید REALITY را حفظ می‌کند. غیرفعال‌سازی پس از Apply نود پورت‌های این اینباند را می‌بندد. حذف ایمن، کلاینت‌ها، UUID، اتصال‌ها و سوابق مالی را پاک نمی‌کند.",
  confirmEdit: "پروفایل تغییر کند؟ Agent فعال PRO ممکن است Xray را بازراه‌اندازی کند و اتصال‌ها قطع شوند. کلید خصوصی عوض نمی‌شود.",
  confirmDisable: "این اینباند غیرفعال شود؟ پس از Apply موفق نود، پورت آن بسته می‌شود؛ رکورد کلاینت‌ها و اعتبار محفوظ می‌ماند.",
  confirmEnable: "این اینباند فعال شود؟ کلاینت‌های مجاز متصل، پس از Apply می‌توانند دوباره دسترسی بگیرند.",
  confirmArchive: "این اینباند برای همیشه آرشیو شود؟ پس از Apply پذیرش ترافیک متوقف می‌شود، بدون حذف کلاینت یا بازگشت اعتبار. فعال‌سازی دوباره در دسترس نیست.",
  changed: "پروفایل پس از شروع ویرایش تغییر کرده است. برای بارگذاری نسخهٔ تازه، ویرایش را ببند و دوباره باز کن.", conflict: "نسخهٔ پروفایل تغییر کرده یا عملیات ناسازگار است. پیش از تلاش دوباره، به‌روزرسانی و بررسی کن.", error: "نتیجه تأیید نشد. تلاش دوباره برای همین تغییر از همان شناسهٔ عملیات استفاده می‌کند.", saved: "تغییر ثبت شد؛ هنوز گزارش تازهٔ Agent لازم است.", history: "تاریخچهٔ تغییرات اینباند", empty: "تغییری ثبت نشده است.", previous: "قبلی", next: "بعدی", historyError: "تاریخچهٔ تغییرات در دسترس نیست.",
  more: "عملیات بیشتر", keyHint: "شناسه و کلید REALITY فعلی حفظ می‌شوند.",
  setupTitle: "کنترل پیکربندی ذخیره‌شده", archiveSetup: "آرشیو پیکربندی", setupPreserve: "آرشیو، این پیکربندی غیرفعال را لغو و رزرو پورت‌هایش را آزاد می‌کند. تنظیمات و تاریخچه برای بازبینی حفظ می‌شوند.", confirmArchiveSetup: "این پیکربندی غیرفعال برای همیشه آرشیو و رزرو پورت‌هایش آزاد شود؟ تنظیمات و تاریخچه برای بازبینی باقی می‌مانند.", setupArchived: "پیکربندی آرشیو شد و رزرو پورت‌هایش آزاد شد.",
 },
};
export default function InboundLifecycle({ inbound, language, csrf, blocked, mode = "all", setupOnly = false, profileOptionsSupported = false, onChanged, onDirtyChange }: { inbound: LifecycleInbound; language: Lang; csrf: string; blocked: boolean; mode?: "all" | "controls" | "history"; setupOnly?: boolean; profileOptionsSupported?: boolean; onChanged: () => void; onDirtyChange?: (dirty: boolean) => void }) {
 setupOnly ||= inbound.security === "tls";
 const t=words[language];const [editing,setEditing]=useState(false);const [base,setBase]=useState(inbound.revision);
 const [name,setName]=useState(inbound.name);const [transport,setTransport]=useState(inbound.transport);const [service,setService]=useState(inbound.grpc_service);const [dest,setDest]=useState(inbound.reality_target);const [sni,setSni]=useState(inbound.server_names.join(", "));const [shortIDs,setShortIDs]=useState(inbound.short_ids);
 const [profileOptions,setProfileOptions]=useState(() => profileOptionsDraft(inbound.options));
 const [reason,setReason]=useState("");const [busy,setBusy]=useState(false);const [message,setMessage]=useState("");
 const [history,setHistory]=useState<Operation[]|null>(null);const [historyError,setHistoryError]=useState(false);const [offset,setOffset]=useState(0);const [refresh,setRefresh]=useState(0);
 const life=useRef<AbortController|null>(null);const pending=useRef<{ semantic: string; payload: Intent & {event_id:string;expected_revision:number} }|null>(null);
 const profileForm=useRef<InboundProfileFormHandle>(null);
 useEffect(()=>{const c=new AbortController();life.current=c;return ()=>c.abort();},[]);
 useEffect(()=>{onDirtyChange?.(editing || !!reason.trim());},[editing,reason,onDirtyChange]);
 useEffect(()=>()=>onDirtyChange?.(false),[onDirtyChange]);
 useEffect(()=>{
  if(mode==="controls")return;
  const c=new AbortController();const timer=setTimeout(()=>c.abort(),8000);
  void fetch(`/api/v1/workspace/native-inbounds/${inbound.id}/operations?limit=20&offset=${offset}`,{signal:c.signal,credentials:"same-origin",cache:"no-store"})
   .then(async r=>{if(!r.ok)throw new Error();const b=await r.json() as {operations:Operation[]};if(!Array.isArray(b.operations))throw new Error();if(!c.signal.aborted){setHistory(b.operations);setHistoryError(false);}})
   .catch(()=>{if(!c.signal.aborted)setHistoryError(true);}).finally(()=>clearTimeout(timer));
  return ()=>{clearTimeout(timer);c.abort();};
 },[inbound.id,inbound.revision,offset,refresh,mode]);
 function beginEdit(){pending.current=null;setBase(inbound.revision);setName(inbound.name);setTransport(inbound.transport);setService(inbound.grpc_service);setDest(inbound.reality_target);setSni(inbound.server_names.join(", "));setShortIDs([...inbound.short_ids]);setProfileOptions(profileOptionsDraft(inbound.options));setEditing(true);setMessage("");}
 async function change(intent:Intent,revision:number){
  if(busy||blocked||inbound.archived||!reason.trim()||!life.current||life.current.signal.aborted||(setupOnly&&intent.action!=="archive"))return;
  const confirmation=setupOnly?t.confirmArchiveSetup:({edit:t.confirmEdit,disable:t.confirmDisable,enable:t.confirmEnable,archive:t.confirmArchive})[intent.action];
  if(!window.confirm(confirmation))return;
  const semantic=JSON.stringify({id:inbound.id,...intent});
  if(pending.current?.semantic!==semantic)pending.current={semantic,payload:{...intent,event_id:"inbound-"+crypto.randomUUID(),expected_revision:revision}};
  const c=new AbortController();const abort=()=>c.abort();life.current.signal.addEventListener("abort",abort,{once:true});const timer=setTimeout(abort,10000);
  setBusy(true);setMessage("");
  try{
   const r=await fetch(`/api/v1/workspace/native-inbounds/${inbound.id}/lifecycle`,{method:"POST",signal:c.signal,credentials:"same-origin",cache:"no-store",headers:{"Content-Type":"application/json","X-CSRF-Token":csrf},body:JSON.stringify(pending.current.payload)});
   if(!r.ok){setMessage(r.status===409?t.conflict:t.error);if(r.status===409){pending.current=null;onChanged();}return;}
   const b=await r.json() as {inbound?:LifecycleInbound;clients_deleted?:boolean;ledger_modified?:boolean};
   if(b.inbound?.id!==inbound.id||b.clients_deleted!==false||b.ledger_modified!==false)throw new Error();
   if(!life.current.signal.aborted){pending.current=null;setReason("");setEditing(false);setMessage(setupOnly?t.setupArchived:t.saved);setRefresh(n=>n+1);onChanged();}
  }catch{if(!life.current.signal.aborted)setMessage(t.error);}
  finally{clearTimeout(timer);life.current.signal.removeEventListener("abort",abort);if(!life.current.signal.aborted)setBusy(false);}
 }
 function save(e:FormEvent){e.preventDefault();if(!profileForm.current?.validate())return;if(base!==inbound.revision){setMessage(t.changed);return;}
  const profile:Profile={id:inbound.id,name,transport,grpc_service:transport==="grpc"?service:"",reality_target:dest,server_names:sni.split(/[\s,]+/).filter(Boolean),short_ids:shortIDs,options:profileOptionsPayload(profileOptions,transport) ?? (inbound.options ? {} : undefined)};
  void change({action:"edit",reason:reason.trim(),profile},base);
 }
 function updateProfile(value:InboundProfileValue){setName(value.name);setTransport(value.transport);setService(value.grpcService);setDest(value.realityTarget);setSni(value.serverNames);setProfileOptions(value.options ?? profileOptionsDraft());}
 const disabled=blocked||busy||inbound.archived||!reason.trim();
 return <section className="native-card native-lifecycle" aria-label={mode==="history"?t.history:"Inbound lifecycle"}>
  {mode!=="history"&&<>
  <div className="inbound-card-heading"><div><h3><ShieldCheck size={19}/>{setupOnly?t.setupTitle:t.title}</h3><p className="inbound-lifecycle-state"><span className={"inbound-state " + (inbound.enabled&&!inbound.archived?"inbound-state-enabled":"")}>{inbound.archived?t.archived:inbound.enabled?t.active:t.disabled}</span><span>{t.revision}: {inbound.revision}</span></p></div>{!setupOnly&&!inbound.archived&&!editing&&<button type="button" disabled={busy||blocked} onClick={beginEdit}><Edit3 size={15}/>{t.edit}</button>}</div>
  {!inbound.archived&&<>
   {editing&&<form aria-label="Edit Inbound profile" onSubmit={save} noValidate className="inbound-inline-editor">
    {base!==inbound.revision&&<p className="native-warning" role="alert">{t.changed}</p>}
    <InboundProfileForm ref={profileForm} language={language} mode="edit" disabled={busy} value={{name,transport,grpcService:service,realityTarget:dest,serverNames:sni,options:profileOptions}} profileOptionsSupported={profileOptionsSupported} onChange={updateProfile}/>
    <label>{t.reason}<input value={reason} onChange={e=>setReason(e.target.value)} maxLength={200} disabled={busy} required/></label>
    <div className="inbound-inline-footer"><span>{t.keyHint}</span><div className="native-actions"><button type="button" disabled={busy} onClick={()=>{setEditing(false);setReason("");}}>{t.cancel}</button><button type="submit" disabled={disabled||base!==inbound.revision}>{t.save}</button></div></div>
   </form>}
   {!editing&&<details className="inbound-more-actions"><summary>{t.more}</summary><p>{setupOnly?t.setupPreserve:t.preserve}</p><label>{t.reason}<input value={reason} onChange={e=>setReason(e.target.value)} maxLength={200} disabled={busy}/></label><div className="native-actions">
    {!setupOnly&&<button type="button" disabled={disabled} onClick={()=>{void change({action:inbound.enabled?"disable":"enable",reason:reason.trim()},inbound.revision);}}>{inbound.enabled?<PauseCircle size={15}/>:<PlayCircle size={15}/>} {inbound.enabled?t.disable:t.enable}</button>}
    <button type="button" disabled={disabled} onClick={()=>{void change({action:"archive",reason:reason.trim()},inbound.revision);}}><Archive size={15}/>{setupOnly?t.archiveSetup:t.archive}</button>
   </div></details>}
  </>}
  {message&&<p className="native-notice" role="status">{message}</p>}
  </>}
  {mode!=="controls"&&<>
  <h4><History size={16}/>{t.history}</h4>{historyError&&<p className="native-warning">{t.historyError}</p>}
  <div className="native-inbound-history">{history?.map(o=><article key={o.event_id}><strong>{({edit:t.edit,disable:t.disable,enable:t.enable,archive:t.archive} as Record<string,string>)[o.action]??o.action}</strong><span>{o.from_revision} → {o.to_revision} · {new Date(o.created_unix*1000).toLocaleString(language==="fa"?"fa-IR":"en-GB")}</span><p>{o.reason}</p></article>)}</div>
  {history?.length===0&&<p>{t.empty}</p>}<div className="native-actions"><button type="button" disabled={offset===0} onClick={()=>setOffset(n=>Math.max(0,n-20))}>{t.previous}</button><button type="button" disabled={!history||history.length<20} onClick={()=>setOffset(n=>n+20)}>{t.next}</button></div>
  </>}
 </section>;
}
