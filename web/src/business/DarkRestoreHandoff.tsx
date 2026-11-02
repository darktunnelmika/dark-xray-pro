import { useState } from "react";
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
 const [busy,setBusy]=useState(false);
 const [error,setError]=useState(false);
 const fmt=(value:string)=>{try{return BigInt(value).toLocaleString(fa?"fa-IR":"en-US")+" B";}catch{return "—";}};
 async function inspect() {
  setBusy(true);setError(false);setStaged(null);setReceipt(null);
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
   setReceipt(result);model.reload();
  } catch { setError(true); } finally { setBusy(false); }
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
  {receipt&&<p role="status">
   {receipt.replayed?(fa?"رسید پیشین بازیابی شد":"Existing receipt recovered"):(fa?"اتصال ثبت شد":"Attachment recorded")}
   {" · "}<code>{receipt.client_id}</code>{" · "}<code>{receipt.original_import_event_id}</code>
  </p>}
  {error&&<p role="alert">{fa?"شواهد یا رسید قابل تأیید نبود. پس از بررسی، دوباره رسید را بازیابی کن.":"Handoff or receipt could not be verified. Recheck and recover the receipt."}</p>}
 </section>;
}
