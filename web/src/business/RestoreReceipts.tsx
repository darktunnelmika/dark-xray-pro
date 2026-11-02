import {useState} from "react";
import {api} from "../session";
import {restoreAPI, EventHistory, type Receipt} from "./DarkRestoreImport";
import type {BusinessModel} from "./useBusinessWorkspace";

type Summary={event_id:string;source_sha256:string;created_unix:number};
const validEvent=(id:string)=>/^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/.test(id);

export default function RestoreReceipts({model}:{model:BusinessModel}){
 const fa=model.language==="fa";
 const [list,setList]=useState<Summary[]|null>(null),[receipt,setReceipt]=useState<Receipt|null>(null);
 const [busy,setBusy]=useState(false),[failed,setFailed]=useState(false),[id,setID]=useState("");
 async function recent(){
  setBusy(true);setFailed(false);setReceipt(null);
  try{
   const data=await api<{imports:Summary[]}>(restoreAPI+"/recent");
   if(!Array.isArray(data.imports)||data.imports.length>20||!data.imports.every(x=>validEvent(x.event_id)&&Number.isSafeInteger(x.created_unix)&&/^[a-f0-9]{64}$/.test(x.source_sha256)))throw Error("receipt index");
   setList(data.imports);
  }catch{setFailed(true);}finally{setBusy(false);}
 }
 async function read(event:string){
  setBusy(true);setFailed(false);setReceipt(null);
  try{
   const r=await api<Receipt>(restoreAPI+"/receipts/"+encodeURIComponent(event));
   if(r.event_id!==event||r.credit_changed!==false||r.usage_reset!==false||r.xray_changed!==false||!Number.isSafeInteger(r.count)||r.count<0||!Number.isSafeInteger(r.legacy_event_count)||r.legacy_event_count<0)throw Error("receipt");
   setReceipt(r);
  }catch{setFailed(true);}finally{setBusy(false);}
 }
 return <section aria-label={fa?"رسیدهای واردسازی":"Import receipts"}><button type="button" disabled={busy} onClick={()=>void recent()}>{fa?"رسیدهای واردسازی":"Import receipts"}</button>
 {list&&<><ul>{list.map(v=><li key={v.event_id}><button type="button" disabled={busy} onClick={()=>void read(v.event_id)}>{new Date(v.created_unix*1000).toLocaleString(model.language)} · {v.event_id}</button></li>)}</ul>
 {!list.length&&<p>{fa?"هنوز واردسازی ثبت نشده است.":"No imports recorded yet."}</p>}
 <label>{fa?"شناسهٔ رسید":"Receipt ID"}<input value={id} maxLength={128} onChange={e=>setID(e.target.value)}/></label>
 <button type="button" disabled={busy||!validEvent(id)} onClick={()=>void read(id)}>{fa?"مشاهدهٔ رسید":"Open receipt"}</button></>}
 {receipt&&<div><p>{fa?"کاربران واردشده":"Clients imported"} · {receipt.count} · <code>{receipt.event_id}</code></p><EventHistory key={receipt.event_id} receipt={receipt} language={model.language}/></div>}
 {failed&&<p role="alert">{fa?"رسید قابل تأیید نبود.":"Receipt could not be verified."}</p>}
 </section>;
}
