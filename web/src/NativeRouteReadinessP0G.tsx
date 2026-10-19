import { useEffect, useState } from "react";
import { RefreshCw, ShieldAlert } from "lucide-react";
import { api } from "./session";
import "./NativeRouteReadinessP0G.css";

type Hop={
 node_id:string;pairing_matched:boolean;online:boolean;metrics_current:boolean;
 native_runtime_state:string;native_process_ready:boolean;
 native_reported_at?:string;
 native_desired_sequence:number;native_reported_sequence:number;
};
type Status={
 scope:string;as_of_utc:string;inbound_id:number;node_id:string;
 draft_revision:number;swap_enabled:boolean;plan_consistent:boolean;
 target:Hop;upstream?:Hop;blockers:string[];
 apply_allowed:boolean;route_signed_ack_verified:boolean;
 swap_forwarding_verified:boolean;customer_data_plane_verified:boolean;
 unique_billing_verified:boolean;automatic_rollback_verified:boolean;
 subscription_published:boolean;
};
const reasons:Record<string,[string,string]>={
 target_attachment_missing:["اتصال مقصد پیدا نشد","Target Inbound attachment missing"],
 target_attachment_changed:["تنظیمات مقصد پس از ذخیرهٔ طرح تغییر کرده","Target Inbound binding changed"],
 target_pairing_changed:["شناسهٔ نصب نود مقصد تغییر کرده","Target Node installation changed"],
 target_agent_offline:["Agent مقصد آفلاین است","Destination Agent offline"],
 target_metrics_stale:["آمار مقصد تازه نیست","Destination metrics stale"],
 target_native_runtime_not_ready:["اجرای Native مقصد هنوز تأیید نشده","Destination native runtime not ready"],
 upstream_attachment_missing:["اتصال تونل بالادستی پیدا نشد","Upstream binding missing"],
 upstream_pairing_changed:["شناسهٔ نصب تونل بالادستی تغییر کرده","Upstream installation changed"],
 upstream_agent_offline:["Agent تونل بالادستی آفلاین است","Upstream Agent offline"],
 upstream_metrics_stale:["آمار بالادستی تازه نیست","Upstream metrics stale"],
 upstream_native_runtime_not_ready:["اجرای Native بالادستی تأیید نشده","Upstream native runtime not ready"],
 plan_stale_or_conflicting:["طرح با وضعیت فعلی سازگار نیست","Saved route conflicts with current state"],
 plan_not_consistent:["طرح نیاز به بازبینی مجدد دارد","Route draft requires another review"],
 route_stage_receipt_missing:["رسید امضاشدهٔ Stage مسیر هنوز موجود نیست","Signed route-stage ACK missing"],
 swap_forwarding_unverified:["عبور واقعی ترافیک از تونل تأیید نشده","Actual Dark Swap forwarding not verified"],
 customer_data_plane_unverified:["اتصال واقعی REALITY مشتری آزمایش نشده","Real VLESS/REALITY handshake unverified"],
 unique_billing_unverified:["تفکیک مصرف قابل‌صورتحساب هنوز تأیید نشده","Unique billable-traffic attribution unverified"],
 rollback_unproven:["بازگشت عملیاتی آخرین تنظیمات سالم تست نشده","Real last-good rollback unproven"],
};
export default function NativeRouteReadinessP0G({
 inboundID,nodeID,draftRevision,language,
}:{
 inboundID:number;nodeID:string;draftRevision:number;language:"fa"|"en";
}){
 const isFa=language==="fa";
 const [data,setData]=useState<Status|null>(null);
 const [error,setError]=useState(false);
 const [busy,setBusy]=useState(false);
 const [retry,setRetry]=useState(0);
 useEffect(()=>{
  const abort=new AbortController();
  setData(null);setError(false);setBusy(true);
  if(!nodeID||draftRevision<1){setBusy(false);return()=>abort.abort()}
  const url="/api/v1/workspace/native-inbounds/"+inboundID+
   "/route-drafts/"+encodeURIComponent(nodeID)+"/readiness";
  void api<Status>(url,{signal:abort.signal}).then(result=>{
   if(abort.signal.aborted)return;
   if(result.scope!=="new_pro_lab_read_only"||
    result.inbound_id!==inboundID||result.node_id!==nodeID||
    result.draft_revision!==draftRevision||
    result.apply_allowed!==false||result.route_signed_ack_verified!==false||
    result.customer_data_plane_verified!==false||
    result.unique_billing_verified!==false||
    result.automatic_rollback_verified!==false||
    result.subscription_published!==false||!Array.isArray(result.blockers)){
    throw Error("untrusted_route_readiness");
   }
   setData(result);
  }).catch(()=>{if(!abort.signal.aborted)setError(true)})
    .finally(()=>{if(!abort.signal.aborted)setBusy(false)});
  return()=>abort.abort();
 },[inboundID,nodeID,draftRevision,retry]);
 const hop=(title:string,info:Hop)=>(
  <div className="route-hop-status" key={info.node_id}>
   <strong>{title} — {info.node_id}</strong>
   <span>{isFa?"Agent":"Agent"}: {info.online?(isFa?"آنلاین":"online"):(isFa?"آفلاین":"offline")}</span>
   <span>{isFa?"آمار":"Metrics"}: {info.metrics_current?(isFa?"تازه":"current"):(isFa?"قدیمی/ناموجود":"stale/missing")}</span>
   <span>Native Xray: {info.native_runtime_state||"unavailable"}</span>
  </div>
 );
 return <section className="route-readiness-p0g" role="region" aria-label="Native route readiness">
  <div className="route-readiness-head">
   <strong><ShieldAlert size={17}/>{isFa?"آمادگی واقعی دو نود":"Two-hop runtime readiness"}</strong>
   <button type="button" disabled={busy}
    onClick={()=>setRetry(x=>x+1)}><RefreshCw size={14}/>{isFa?"بررسی مجدد":"Recheck"}</button>
  </div>
  <p className="route-readiness-blocked">
   {isFa?"اعمال مسیر مسدود است — این گزارش فقط وضعیت لحظه‌ای را نشان می‌دهد.":
    "Route Apply blocked — this is an advisory snapshot only."}
  </p>
  {busy&&<p>{isFa?"در حال بررسی…":"Checking current status…"}</p>}
  {error&&<p role="alert">{isFa?"گزارش معتبر نیست؛ مسیر همچنان مسدود است.":
   "Readiness unavailable; route remains blocked."}</p>}
  {data&&<>
   <div className="route-readiness-hops">
    {hop(isFa?"نود مقصد":"Destination",data.target)}
    {data.upstream&&hop(isFa?"تونل بالادستی":"Upstream",data.upstream)}
   </div>
   <p className="route-readiness-consistency">{data.plan_consistent?
    (isFa?"ساختار طرح معتبر است؛ اما هنوز آمادهٔ اجرا نیست.":
     "Draft structure valid; runtime Apply remains blocked."):
    (isFa?"طرح ناسازگار یا منقضی شده؛ قبل از ادامه بازبینی لازم است.":
     "Draft stale or conflicting; owner review required.")}</p>
   <details>
    <summary>{isFa?"دلایل مسدودبودن":"Blocking conditions"} ({data.blockers.length})</summary>
    <ul>{data.blockers.map((code,i)=><li key={code+"-"+i}>
      {reasons[code]?.[isFa?0:1]||code}</li>)}</ul>
   </details>
   <small>{isFa?"زمان گزارش":"As of"}: {new Date(data.as_of_utc).toLocaleString(isFa?"fa-IR":"en-GB")}</small>
  </>}
 </section>;
}
