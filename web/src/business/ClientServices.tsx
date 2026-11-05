import {useEffect,useRef,useState} from "react";
import {api,ApiError,type Session} from "../session";
import {executionAPI,executionPath,readExecutionAttempt,readExecutionState,readExecutionPreview,readExecutionOperation,type ExecutionState,type ExecutionCommand,type ExecutionInput} from "../inboundExecution";
import "./ClientServices.css";
type Choice={id:number;name:string;archived?:boolean};
type Pending={owner:string;client:string;inbound:number;adding:boolean;command:ExecutionCommand};
const ready=(s:ExecutionState)=>s.enabled&&!s.owner_resume_required&&!s.pending_changes&&s.desired_state==="active"&&s.observed_state==="active"&&s.accounting_mode==="per_client";
export default function ClientServices({clientID,session,language,onChanged}:{clientID:string;session:Session;language:"fa"|"en";onChanged:()=>void}) {
 const fa=language==="fa",key="darkpro:client-service:v1:"+session.user.id+":"+clientID;
 const [choices,setChoices]=useState<Choice[]>([]),[selected,setSelected]=useState(0),[state,setState]=useState<ExecutionState|null>(null);
 const [reason,setReason]=useState(""),[review,setReview]=useState<Pending|null>(null),[pending,setPending]=useState<Pending|null>(null);
 const [busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[storageFailed,setStorageFailed]=useState(false),[notice,setNotice]=useState("");
 const lifetime=useRef<AbortController|null>(null),locked=useRef(false),generation=useRef(0),changed=useRef(onChanged);changed.current=onChanged;
 const get=async(id:number)=>readExecutionState(await api<unknown>(executionPath(id),{signal:lifetime.current?.signal}),id);
 const remember=(p:Pending|null)=>{if(p)sessionStorage.setItem(key,JSON.stringify(p));else sessionStorage.removeItem(key);setPending(p);};
 useEffect(()=>{
  const controller=new AbortController();lifetime.current=controller;
  if(session.user.role!=="owner")return()=>controller.abort();
  let recovered:Pending|null=null;
  try{
   const raw=sessionStorage.getItem(key);
   if(raw){if(raw.length>3_000_000)throw Error();const p=JSON.parse(raw) as Pending;
    const valid=readExecutionAttempt(JSON.stringify({inbound_id:p.inbound,kind:"command",body:JSON.stringify(p.command)}),p.inbound);
    if(!valid||p.owner!==session.user.id||p.client!==clientID||!Number.isSafeInteger(p.inbound)||p.inbound<1||typeof p.adding!=="boolean"||p.command.action!=="apply"||p.command.accounting_mode!=="per_client"||p.command.client_ids.includes(clientID)!==p.adding)throw Error();
    recovered=p;setPending(p);setSelected(p.inbound);
   }
  }catch{setStorageFailed(true);}
  void api<{inbounds:Choice[]}>("/api/v1/workspace/native-inbounds-state",{signal:controller.signal}).then(d=>{
   if(!Array.isArray(d.inbounds)||d.inbounds.length>256||d.inbounds.some(x=>!Number.isSafeInteger(x.id)||x.id<1||typeof x.name!=="string")||new Set(d.inbounds.map(x=>x.id)).size!==d.inbounds.length)throw Error();
   if(!controller.signal.aborted){const items=d.inbounds.filter(x=>!x.archived);setChoices(items);if(!recovered)setSelected(items[0]?.id??0);}
  }).catch(()=>{if(!controller.signal.aborted)setFailed(true);});
  return()=>{controller.abort();generation.current++;};
 },[key,session.user.role,session.user.id,clientID]);
 async function refresh(){
  if(!selected||session.user.role!=="owner")return;
  const n=++generation.current;setFailed(false);setState(null);setReview(null);
  try{const s=await get(selected);if(!lifetime.current?.signal.aborted&&n===generation.current)setState(s);}
  catch{if(!lifetime.current?.signal.aborted&&n===generation.current)setFailed(true);}
 }
 useEffect(()=>{void refresh();},[selected]);
 async function preview(){
  if(locked.current||pending||storageFailed||!selected||!reason.trim()||session.user.role!=="owner")return;
  locked.current=true;setBusy(true);setReview(null);setNotice("");
  try{
   const s=await get(selected);if(lifetime.current?.signal.aborted)return;setState(s);
   if(!ready(s)||!s.allowed_actions.includes("apply"))throw Error();
   const adding=!s.client_ids.includes(clientID);
   const ids=adding?[...s.client_ids,clientID].sort():s.client_ids.filter(id=>id!==clientID);
   const input:ExecutionInput={event_id:crypto.randomUUID(),action:"apply",expected_revision:s.revision,expected_plan_revision:s.plan_revision,expected_inbound_revision:s.inbound_revision,client_ids:ids,accounting_mode:"per_client",reason:reason.trim()};
   const p=readExecutionPreview(await executionAPI<unknown>(executionPath(selected)+"/preview",lifetime.current!.signal,JSON.stringify(input),session.csrf_token),selected,input);
   if(lifetime.current?.signal.aborted)return;
   if(!p.valid)throw Error();
   setReview({owner:session.user.id,client:clientID,inbound:selected,adding,command:{...input,review_sha:p.review_sha}});
  }catch{if(!lifetime.current?.signal.aborted)setNotice(fa?"تغییر قابل اعمال نیست یا وضعیت سرویس عوض شده؛ اطلاعات را تازه کنید.":"Change unavailable or service state changed. Refresh and review again.");}
  finally{locked.current=false;if(!lifetime.current?.signal.aborted)setBusy(false);}
 }
 async function send(p:Pending){
  if(locked.current||storageFailed||session.user.role!=="owner")return;
  locked.current=true;setBusy(true);setNotice("");
  let stored=false,accepted=false;
  try{
   remember(p);stored=true;setReview(null);
   const path=executionPath(p.inbound),signal=lifetime.current!.signal;
   // Lookup before replay handles a lost reply without generating a new event.
   try{readExecutionOperation(await executionAPI<unknown>(path+"/operations/"+encodeURIComponent(p.command.event_id),signal),p.inbound,p.command);accepted=true;}
   catch(e){if(!(e instanceof ApiError&&e.status===404))throw e;}
   if(!accepted){readExecutionOperation(await executionAPI<unknown>(path+"/commands",signal,JSON.stringify(p.command),session.csrf_token),p.inbound,p.command);accepted=true;}
   if(signal.aborted)return;
   setNotice(fa?"درخواست ثبت شد؛ منتظر تأیید اعمال روی نود…":"Request recorded; awaiting runtime confirmation…");
   for(let n=0;n<8;n++){
    const s=await get(p.inbound);if(signal.aborted)return;setState(s);
    if(ready(s)&&s.revision>=p.command.expected_revision+1&&s.client_ids.includes(clientID)===p.adding&&s.effective_client_ids.includes(clientID)===p.adding){
     remember(null);setReason("");setNotice(fa?"تغییر سرویس روی نود تأیید شد.":"Service change confirmed on the runtime.");changed.current();return;
    }
    if(n<7)await new Promise(r=>setTimeout(r,1200));
   }
   setNotice(fa?"اعمال هنوز تأیید نشده است؛ نتیجه را دوباره بررسی کنید.":"Runtime application is not yet confirmed. Check the same request again.");
  }catch(e){
   if(lifetime.current?.signal.aborted)return;
   if(!stored){setStorageFailed(true);setNotice(fa?"ذخیرهٔ امن درخواست ممکن نیست؛ فرمانی ارسال نشد.":"Recovery storage unavailable; no command was sent.");}
   else if(!accepted&&e instanceof ApiError&&e.status>=400&&e.status<500&&![408,429].includes(e.status)){
    try{remember(null);}catch{setStorageFailed(true);}
    setNotice(fa?"سرور درخواست را نپذیرفت؛ وضعیت را تازه و دوباره بازبینی کنید.":"Server rejected the request. Refresh and review again.");setState(null);
   }else setNotice(fa?"نتیجه نامشخص است؛ همان درخواست را دوباره بررسی کنید.":"Result unknown. Check or retry the same request.");
  }finally{locked.current=false;if(!lifetime.current?.signal.aborted)setBusy(false);}
 }
 if(session.user.role!=="owner")return null;
 const assigned=state?.client_ids.includes(clientID),applied=state?.effective_client_ids.includes(clientID);
 return <section className="business-panel client-services" aria-label="Client services">
  <h3>{fa?"سرویس‌های کلاینت":"Client services"}</h3>
  <p>{fa?"هر تغییر فقط عضویت این کلاینت در سرویس انتخاب‌شده را عوض می‌کند و پس از تأیید نود نهایی می‌شود.":"Each change updates this client's membership in one selected service and waits for runtime confirmation."}</p>
  <div className="business-fields"><label>{fa?"اینباند":"Service inbound"}<select value={selected} disabled={busy||!!pending||!!review} onChange={e=>{setSelected(Number(e.target.value));setNotice("");}}><option value={0}>{fa?"انتخاب سرویس":"Select service"}</option>{choices.map(x=><option key={x.id} value={x.id}>{x.name} · #{x.id}</option>)}</select></label>
  <button type="button" disabled={busy||!!pending||!selected} onClick={()=>void refresh()}>{fa?"تازه‌سازی سرویس":"Refresh service"}</button></div>
  {failed&&<p role="alert">{fa?"وضعیت سرویس دریافت نشد.":"Service state unavailable."}</p>}
  {storageFailed&&<p role="alert">{fa?"بازیابی درخواست ممکن نیست؛ تغییر مسدود است.":"Recovery storage unavailable or invalid; changes are blocked."}</p>}
  {state&&<p>{fa?"عضویت ثبت‌شده: ":"Saved membership: "}{assigned?(fa?"دارد":"Assigned"):(fa?"ندارد":"Not assigned")} · {ready(state)&&applied?(fa?"اعمال‌شده روی سرویس":"Applied to service"):(fa?"اتصال فعال تأیید نشده":"Active delivery not confirmed")}</p>}
  {state&&!ready(state)&&<p>{fa?"ابتدا وضعیت اینباند را در بخش اینباندها اصلاح کنید.":"Resolve this service in Inbounds before changing membership."}</p>}
  {!pending&&!review&&<><label>{fa?"دلیل تغییر":"Change reason"}<input value={reason} maxLength={200} disabled={busy} onChange={e=>setReason(e.target.value)}/></label><button type="button" disabled={busy||storageFailed||!state||!ready(state)||!state.allowed_actions.includes("apply")||!reason.trim()} onClick={()=>void preview()}>{assigned?(fa?"بررسی حذف از سرویس":"Review removal"):(fa?"بررسی افزودن به سرویس":"Review addition")}</button></>}
  {review&&<div className="client-service-review"><strong>{review.adding?(fa?"افزودن کلاینت به سرویس":"Add client to service"):(fa?"حذف کلاینت از سرویس":"Remove client from service")}</strong><p>{choices.find(x=>x.id===review.inbound)?.name} · {clientID}</p><p>{fa?"دلیل: ":"Reason: "}{review.command.reason}</p><button type="button" disabled={busy} onClick={()=>void send(review)}>{fa?"تأیید تغییر سرویس":"Confirm service change"}</button><button type="button" disabled={busy} onClick={()=>setReview(null)}>{fa?"انصراف":"Cancel review"}</button></div>}
  {pending&&<button type="button" disabled={busy||storageFailed} onClick={()=>void send(pending)}>{fa?"بررسی / ادامه همان درخواست":"Check / resume same request"}</button>}
  {notice&&<p role="status">{notice}</p>}
 </section>;
}
