import { useState } from "react";
import { api, mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { restoreAPI } from "./DarkRestoreImport";

type Status = {
 enabled:boolean;scope?:string;client_id?:string;plan_sha256?:string;id?:string;
 state?:"not_requested"|"pending"|"issued"|"verified"|"expired_unknown";
 customer_connection_verified?:boolean;receipt_sha256?:string;completed_unix_nano?:number;
 authorizes_mutation?:boolean;
};
const digest=(s:string)=>/^[a-f0-9]{64}$/.test(s);
export default function DarkRestoreConnectionCheck({model,clientID,planSHA}:{model:BusinessModel;clientID:string;planSHA:string}) {
 const fa=model.language==="fa";
 const [status,setStatus]=useState<Status|null>(null),[consent,setConsent]=useState(false);
 const [busy,setBusy]=useState(false),[error,setError]=useState(false);
 function checked(s:Status):Status {
  if(s?.enabled===false)return s;
  if(s?.enabled!==true||s.scope!=="darkrestore_customer_connection_v1"||s.client_id!==clientID||
   !["not_requested","pending","issued","verified","expired_unknown"].includes(s.state??"")||
   (s.state!=="not_requested"&&(s.plan_sha256!==planSHA||!/^[a-f0-9]{32}$/.test(s.id??"")))||
   s.authorizes_mutation!==false||typeof s.customer_connection_verified!=="boolean"||
   (s.state==="verified")!==s.customer_connection_verified||
   (s.customer_connection_verified&&(!digest(s.receipt_sha256??"")||!Number.isFinite(s.completed_unix_nano)||s.completed_unix_nano!<=0)))
   throw Error("connection receipt mismatch");
  return s;
 }
 async function inspect(){setBusy(true);setError(false);setStatus(null);try{setStatus(checked(await api<Status>(restoreAPI+"/handoff/quarantine/connection-check")));}catch{setError(true);}finally{setBusy(false);}}
 async function start(){
  if(!consent||status?.enabled!==true||status.state!=="not_requested")return;
  setBusy(true);setError(false);
  try{setStatus(checked(await mutation<Status>(restoreAPI+"/handoff/quarantine/connection-check",
   {event_id:"connection-"+planSHA,plan_sha256:planSHA,acknowledge_usage:true},model.session.csrf_token)));setConsent(false);}
  catch{setError(true);}finally{setBusy(false);}
 }
 return <div aria-label={fa?"آزمون اتصال کاربر":"Customer connection test"}>
  <p>{fa?"این آزمون با همان لینک کاربر انجام می‌شود و ترافیک واقعی آن از حجم سرویس مصرف می‌کند. نتیجه فقط اتصال همان لحظه را ثبت می‌کند؛ پذیرش نهایی مهاجرت جداست.":"This test uses the customer's link and consumes service quota. It records a connection at that moment; final migration acceptance is separate."}</p>
  <button type="button" disabled={busy} onClick={()=>void inspect()}>{fa?"بررسی نتیجهٔ آزمون اتصال":"Check connection test result"}</button>
  {status?.enabled===false&&<p role="status">{fa?"مقصد آزمون هنوز توسط مدیر سرور تنظیم نشده است.":"The server administrator has not configured the verifier."}</p>}
  {status?.enabled&&<p role="status">{({
   not_requested:fa?"آزمون درخواست نشده است.":"No test requested.",
   pending:fa?"در انتظار Agent؛ درخواست ثبت شده است.":"Queued for the Agent.",
   issued:fa?"درخواست به Agent تحویل شده؛ نتیجه هنوز ثبت نشده است.":"Issued to the Agent; awaiting a recorded result.",
   expired_unknown:fa?"مهلت تمام شد؛ اتصال تأیید نشده و آزمون تکرار نمی‌شود.":"Expired; connection unverified. The test will not repeat.",
   verified:fa?"رسید اتصال این کاربر تأیید و ثبت شد.":"This customer's connection receipt was verified and recorded."
  })[status.state!]}</p>}
  {status?.enabled&&status.state==="not_requested"&&<>
   <label><input type="checkbox" checked={consent} disabled={busy} onChange={e=>setConsent(e.target.checked)}/>
    {fa?"مصرف حجم سرویس برای یک بار آزمون اتصال را تأیید می‌کنم.":"I approve service quota usage for one connection test."}</label>
   <button type="button" disabled={busy||!consent} onClick={()=>void start()}>{fa?"اجرای یک‌بارهٔ آزمون اتصال":"Run one connection test"}</button>
  </>}
  {error&&<p role="alert">{fa?"درخواست یا رسید قابل تأیید نبود؛ وضعیت را دوباره بررسی کن.":"The request or receipt could not be verified. Check the status again."}</p>}
 </div>;
}
