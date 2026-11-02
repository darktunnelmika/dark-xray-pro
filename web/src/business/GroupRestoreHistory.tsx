import { useEffect, useState } from "react";
import { api, type Language } from "../session";
import type { ClientGroup } from "./shared";

type Row = { source_sha256:string; source_group_id:string; source_name:string;
 source_count:number; archived_count:number; promoted_count:number;
 snapshot_bytes:string; closed_epoch_bytes:string; historical_bytes:string };
const bytes = (v:unknown): v is string => typeof v === "string" && /^(0|[1-9][0-9]*)$/.test(v);
function valid(v:Row):boolean {
 if(!v || !/^[a-f0-9]{64}$/.test(v.source_sha256) || !v.source_group_id || !v.source_name ||
    ![v.source_count,v.archived_count,v.promoted_count].every(x=>Number.isSafeInteger(x)&&x>=0) ||
    !bytes(v.snapshot_bytes) || !bytes(v.closed_epoch_bytes) || !bytes(v.historical_bytes))return false;
 return BigInt(v.snapshot_bytes)+BigInt(v.closed_epoch_bytes)===BigInt(v.historical_bytes);
}
export default function GroupRestoreHistory({group,language}:{group:ClientGroup;language:Language}) {
 const [rows,setRows]=useState<Row[]|null>(null);
 const [failed,setFailed]=useState(false);
 useEffect(()=>{
  const controller=new AbortController();setRows(null);setFailed(false);
  void api<{histories:Row[];scope:string}>("/api/v1/workspace/groups/"+encodeURIComponent(group.id)+"/history",{signal:controller.signal})
   .then(v=>{if(controller.signal.aborted)return;
    if(v.scope!=="immutable_source_group" || !Array.isArray(v.histories) || v.histories.some(x=>!valid(x)))throw Error("history");
    setRows(v.histories);
   }).catch(()=>{if(!controller.signal.aborted)setFailed(true)});
  return ()=>controller.abort();
 },[group.id]);
 const fa=language==="fa";
 return <section className="business-panel" aria-label={fa?"تاریخچهٔ گروه مبدا":"Source group history"}>
  <h3>{fa?"مصرف تاریخی گروه مبدا":"Source group historical usage"}</h3>
  <p>{fa?"تاریخچه به گروه مبدا وصل است و با تغییر نام، آرشیو یا جابه‌جایی اعضا کم نمی‌شود. مصرف پس از مهاجرت در جزئیات هر کلاینت جداست.":"History belongs to the original source group and does not shrink when it is renamed or members move. Post-migration usage remains separate on each client."}</p>
  {failed?<p role="alert">{fa?"تاریخچه قابل تأیید نیست.":"History could not be verified."}</p>:
   rows===null?<p>{fa?"در حال دریافت تاریخچه":"Loading history"}</p>:
   rows.length===0?<p>{fa?"برای این گروه Import ثبت نشده است.":"No import recorded for this group."}</p>:
   <div className="business-records">{rows.map(r=><article className="business-group-row" key={r.source_sha256+":"+r.source_group_id}>
    <strong>{r.source_name} · {r.source_group_id}</strong>
    <span>{fa?"کل کاربران":"Source users"}: {r.source_count} · {fa?"آرشیوی":"Archived"}: {r.archived_count} · {fa?"ارتقایافته":"Promoted"}: {r.promoted_count}</span>
    <span>{fa?"تا زمان snapshot":"At snapshot"}: {BigInt(r.snapshot_bytes).toLocaleString(language)} B</span>
    <span>{fa?"دورهٔ بسته‌شدهٔ منبع":"Closed source epoch"}: {BigInt(r.closed_epoch_bytes).toLocaleString(language)} B</span>
    <strong>{fa?"کل مصرف تاریخی":"Historical total"}: {BigInt(r.historical_bytes).toLocaleString(language)} B</strong>
   </article>)}</div>}
 </section>;
}
