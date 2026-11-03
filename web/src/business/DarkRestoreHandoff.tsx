import { useState } from "react";
import DarkRestoreConnectionCheck from "./DarkRestoreConnectionCheck";
import { api, mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { restoreAPI } from "./DarkRestoreImport";

type Settlement = {
 scope: string; source_id: string; source_sha256: string;
 pre_epoch_bytes: string; epoch_uplink_bytes: string; epoch_downlink_bytes: string;
 settled_historical_bytes: string; finite_quota_remaining_bytes: string;
};
type Handoff = { handoff_sha256: string; source_settlement: Settlement };
type Attachment = {
 client_id: string; original_import_event_id: string; actor_user_id: string;
 source_settlement: Settlement; handoff_sha256: string;
 handoff_public_key_sha256: string; replayed: boolean;
};
type Readiness = {
 client_id:string; source_sha256:string; source_id:string; source_scope:string;
 source_epoch_settled:boolean; global_source_custody_verified:boolean;
 native_baseline_complete:boolean; subscription_delivered:boolean;
 client_archived:boolean; client_expired:boolean; finite_quota_remaining_bytes:string;
 activation_allowed:boolean; cutover_verified:boolean;
 activation_blockers:string[]; verification_blockers:string[];
};
type Execution = {
 scope:string;client_id:string;plan_sha256?:string;initial_zero_receipt_recorded:boolean;
 listener_apply_recorded:boolean;listener_apply_receipt_sha256?:string;
 native_apply_recorded:boolean;serving_health_recorded:boolean;authorizes_mutation:boolean;
};
const digest = (v: string) => /^[a-f0-9]{64}$/.test(v);
const bytes = (v: string) => /^(0|[1-9][0-9]{0,18})$/.test(v);
function validHandoff(v: Handoff): boolean {
 const s=v?.source_settlement;
 if (!v || !digest(v.handoff_sha256) || !s || s.scope!=="single_local_source_epoch" ||
   !s.source_id || !digest(s.source_sha256) ||
   ![s.pre_epoch_bytes,s.epoch_uplink_bytes,s.epoch_downlink_bytes,
     s.settled_historical_bytes,s.finite_quota_remaining_bytes].every(bytes)) return false;
 try { return BigInt(s.settled_historical_bytes) === BigInt(s.pre_epoch_bytes)+
     BigInt(s.epoch_uplink_bytes)+BigInt(s.epoch_downlink_bytes); }
 catch { return false; }
}

export default function DarkRestoreHandoff({model}:{model:BusinessModel}) {
 const fa=model.language==="fa";
 const [staged,setStaged]=useState<Handoff|null>(null);
 const [receipt,setReceipt]=useState<Attachment|null>(null);
 const [readiness,setReadiness]=useState<Readiness|null>(null);
 const [execution,setExecution]=useState<Execution|null>(null);
 const [executionError,setExecutionError]=useState(false);
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState(false);
 const fmt=(value:string)=>{try{return BigInt(value).toLocaleString(fa?"fa-IR":"en-US")+" B";}catch{return "—";}};
 async function inspect() {
  setBusy(true);setError(false);setStaged(null);setReceipt(null);setReadiness(null);setExecution(null);setExecutionError(false);
  try {
   const result=await api<Handoff>(restoreAPI+"/handoff");
   if (!validHandoff(result)) throw Error("unverified handoff");
   setStaged(result);
  } catch { setError(true); } finally { setBusy(false); }
 }
 async function attach() {
  if (!staged || !validHandoff(staged)) return;
  setBusy(true);setError(false);
  try {
   const result=await mutation<Attachment>(restoreAPI+"/handoff/attach",
    {handoff_sha256:staged.handoff_sha256},model.session.csrf_token);
   if (result.client_id==="" || result.actor_user_id!==model.session.user.id ||
    result.handoff_sha256!==staged.handoff_sha256 ||
    !digest(result.handoff_public_key_sha256) ||
    result.source_settlement?.source_id!==staged.source_settlement.source_id ||
    result.source_settlement?.settled_historical_bytes!==staged.source_settlement.settled_historical_bytes)
     throw Error("unverified attachment receipt");
   setReceipt(result);setReadiness(null);setExecution(null);setExecutionError(false);model.reload();
  } catch { setError(true); } finally { setBusy(false); }
 }
 async function inspectReadiness() {
  if(!receipt||!staged)return;
  setBusy(true);setError(false);setReadiness(null);setExecution(null);setExecutionError(false);
  try {
   const result=await api<Readiness>(restoreAPI+"/handoff/readiness");
   const lists=[result.activation_blockers,result.verification_blockers];
   if(!result||result.client_id!==receipt.client_id||
      result.source_sha256!==staged.source_settlement.source_sha256||
      result.source_id!==staged.source_settlement.source_id||
      !["single_local_source_epoch","pinned_inventory_plus_independent_coverage_assertion"].includes(result.source_scope)||
      ![result.source_epoch_settled,result.global_source_custody_verified,result.native_baseline_complete,
        result.subscription_delivered,result.client_archived,result.client_expired,
        result.activation_allowed,result.cutover_verified].every(x=>typeof x==="boolean")||
      !bytes(result.finite_quota_remaining_bytes)||
      lists.some(v=>!Array.isArray(v)||v.some(x=>typeof x!=="string"||!/^[a-z0-9_]+$/.test(x)))||
      result.cutover_verified&&(!result.activation_allowed||!result.native_baseline_complete||!result.subscription_delivered))
    throw Error("invalid readiness");
   setReadiness(result);
  } catch { setError(true); } finally { setBusy(false); }
 }
 async function inspectExecution() {
  if(!receipt)return;
  setBusy(true);setExecution(null);setExecutionError(false);
  try {
   const result=await api<Execution>(restoreAPI+"/handoff/quarantine/admission/evidence");
   if(result?.scope!=="darkrestore_native_admission_evidence_v1"||result.client_id!==receipt.client_id||
    result.initial_zero_receipt_recorded!==true||typeof result.listener_apply_recorded!=="boolean"||
    (result.listener_apply_recorded?!digest(result.listener_apply_receipt_sha256??""):!!result.listener_apply_receipt_sha256)||
    result.native_apply_recorded!==false||result.serving_health_recorded!==false||result.authorizes_mutation!==false)
     throw Error("execution evidence mismatch");
   setExecution(result);
  }catch{setExecutionError(true);}finally{setBusy(false);}
 }
 return <section className="darkrestore-commit" aria-label={fa?"تحویل امضاشدهٔ مصرف منبع":"Signed source usage handoff"}>
  <h4>{fa?"تحویل امضاشدهٔ مصرف منبع":"Signed source usage handoff"}</h4>
  <p>{fa?"فقط شواهد یک منبع بسته‌شده را به کاربر قبلاً واردشده پیوند می‌دهد. کلاینت همچنان غیرفعال می‌ماند و اعتباری کسر نمی‌شود.":"Attach one closed source's usage to an earlier imported client. The client remains disabled and no credit is charged."}</p>
  <button type="button" disabled={busy} onClick={()=>void inspect()}>
   {fa?"بررسی تحویل تأییدشدهٔ سرور":"Review server-approved handoff"}
  </button>
  {staged&&<div>
   <p>{fa?"شناسهٔ قدیمی":"Legacy ID"}: <code>{staged.source_settlement.source_id}</code></p>
   <p>{fa?"مصرف پیشین":"Earlier usage"}: {fmt(staged.source_settlement.pre_epoch_bytes)}
    {" · "}{fa?"آپلود منبع":"Source upload"}: {fmt(staged.source_settlement.epoch_uplink_bytes)}
    {" · "}{fa?"دانلود منبع":"Source download"}: {fmt(staged.source_settlement.epoch_downlink_bytes)}</p>
   <p>{fa?"کل مصرف تاریخی پس از اتصال":"Historical total after attachment"}: <strong>{fmt(staged.source_settlement.settled_historical_bytes)}</strong>
    {" · "}{fa?"باقی‌ماندهٔ حجم":"Finite quota remaining"}: {fmt(staged.source_settlement.finite_quota_remaining_bytes)}</p>
   <small>SHA-256: <code>{staged.handoff_sha256}</code></small>
   <button type="button" disabled={busy} onClick={()=>void attach()}>
    {fa?"ثبت یا بازیابی رسید اتصال":"Attach or recover receipt"}
   </button>
  </div>}
  {receipt&&<><p role="status">
   {receipt.replayed?(fa?"رسید پیشین بازیابی شد":"Existing receipt recovered"):(fa?"اتصال ثبت شد":"Attachment recorded")}
   {" · "}<code>{receipt.client_id}</code>{" · "}<code>{receipt.original_import_event_id}</code>
  </p>
  <button type="button" disabled={busy} onClick={()=>void inspectReadiness()}>
   {fa?"بررسی آمادگی فعال‌سازی":"Check activation readiness"}
  </button></>}
  {readiness&&<div aria-label={fa?"آمادگی مهاجرت":"Cutover readiness"}>
   <p>{fa?"تسویهٔ دورهٔ منبع":"Source epoch settled"}: <strong>{readiness.source_epoch_settled?(fa?"بله":"yes"):(fa?"خیر":"no")}</strong>
    {" · "}{fa?"انحصار همهٔ منابع":"All-source custody"}: <strong>{readiness.global_source_custody_verified?(fa?"تأیید":"verified"):(fa?"تأیید نشده":"not verified")}</strong></p>
   <p>{fa?"baseline مصرف جدید":"Native usage baseline"}: <strong>{readiness.native_baseline_complete?(fa?"کامل":"complete"):(fa?"ناقص":"incomplete")}</strong>
    {" · "}{fa?"تحویل اشتراک":"Subscription delivery"}: <strong>{readiness.subscription_delivered?(fa?"تأیید":"verified"):(fa?"انجام نشده":"not delivered")}</strong>
    {" · "}{fa?"حجم باقی‌مانده":"Remaining quota"}: {fmt(readiness.finite_quota_remaining_bytes)}</p>
   <p><strong>{readiness.activation_allowed?(fa?"آمادهٔ فعال‌سازی":"Activation ready"):(fa?"فعال‌سازی مسدود است":"Activation blocked")}</strong>
    {" · "}{readiness.cutover_verified?(fa?"مهاجرت تأیید شد":"Cutover verified"):(fa?"پذیرش نهایی نشده":"Cutover not verified")}</p>
   {receipt && readiness.native_baseline_complete && readiness.subscription_delivered && <div>
    <button type="button" disabled={busy} onClick={()=>void inspectExecution()}>
     {fa?"بررسی وضعیت اجرای سرویس":"Check service startup"}
    </button>
    {execution&&<div aria-label={fa?"وضعیت اجرای سرویس":"Service startup status"}>
     <p>{fa?"ثبت اولیهٔ حسابداری تأیید شده است.":"Initial accounting acknowledgment recorded."}</p>
     <p>{fa?"راه‌اندازی سرویس در نود":"Service startup on the node"}: <strong>
      {execution.listener_apply_recorded?(fa?"رسید Agent ثبت شده":"Agent receipt recorded"):(fa?"در انتظار رسید Agent":"Waiting for Agent receipt")}
     </strong></p>
     <p>{fa?"سلامت اتصال هنوز تأیید نشده است. ثبت راه‌اندازی به‌تنهایی به معنی پایان مهاجرت نیست.":"Connection health is awaiting verification. A startup receipt does not complete migration."}</p>
     {execution.listener_apply_recorded&&digest(execution.plan_sha256??"")&&<DarkRestoreConnectionCheck model={model} clientID={execution.client_id} planSHA={execution.plan_sha256!}/>}
    </div>}
    {executionError&&<p role="status">{fa?"رسید کامل و مطابق این کاربر در دسترس نیست؛ دوباره بررسی کن.":"A complete matching receipt is unavailable. Check again."}</p>}
   </div>}
   {staged && receipt && readiness.native_baseline_complete && <form onSubmit={async e=>{
    e.preventDefault();
    const form=new FormData(e.currentTarget);
    const ids=["inbound_id","plan_revision","inbound_revision"].map(k=>Number(form.get(k)));
    if(ids.some(v=>!Number.isSafeInteger(v)||v<1)) {setError(true);return;}
    setBusy(true);setError(false);
    try {
     const result=await mutation<{scope:string;client_id:string;content:string;applied:boolean;path_verified:boolean}>(restoreAPI+"/handoff/prepare",
      {handoff_sha256:staged.handoff_sha256,event_id:"prepare-"+staged.handoff_sha256,inbound_id:ids[0],plan_revision:ids[1],inbound_revision:ids[2]},model.session.csrf_token);
     if(result.scope!=="darkrestore_prepared_not_applied_v1"||result.client_id!==receipt.client_id||result.applied!==false||result.path_verified!==false||!result.content) throw Error("invalid preparation");
     const url=URL.createObjectURL(new Blob([result.content],{type:"text/plain"}));
     const a=document.createElement("a");a.href=url;a.download="prepared-subscription.txt";a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);
    } catch {setError(true);} finally {setBusy(false);}
   }}>
    <label>{fa?"شناسه اینباند":"Inbound ID"}<input required name="inbound_id" type="number" min="1"/></label>
    <label>{fa?"نسخه طرح":"Plan revision"}<input required name="plan_revision" type="number" min="1"/></label>
    <label>{fa?"نسخه اینباند":"Inbound revision"}<input required name="inbound_revision" type="number" min="1"/></label>
    <button disabled={busy}>{fa?"دانلود اشتراک پیش از فعال‌سازی":"Download prepared subscription"}</button>
   </form>}
   {[...readiness.activation_blockers,...readiness.verification_blockers].length>0&&<ul>
    {[...readiness.activation_blockers,...readiness.verification_blockers].map(x=><li key={x}><code>{x}</code></li>)}
   </ul>}
  </div>}
  {error&&<p role="alert">{fa?"شواهد یا رسید قابل تأیید نبود. پس از بررسی، دوباره رسید را بازیابی کن.":"Handoff or receipt could not be verified. Recheck and recover the receipt."}</p>}
 </section>;
}
