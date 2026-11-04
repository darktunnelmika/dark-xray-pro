import { useEffect, useRef, useState } from "react";
import { api } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import NativeMeteredRouteTransactions from "../NativeMeteredRouteTransactions";
import { promotionPath, readPromotionDiscovery, type PromotionReceipt } from "../inboundPlanExecution";
import DarkRestoreProtocolHealth from "./DarkRestoreProtocolHealth";

export default function DarkRestoreExecution({model,client,inbound,planRevision,sourceRevision,promotionEvent}:{model:BusinessModel;client:string;inbound:number;planRevision:number;sourceRevision:number;promotionEvent:string}) {
 const fa=model.language==="fa", [promotion,setPromotion]=useState<PromotionReceipt|null>(null),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false);
 const life=useRef<AbortController|null>(null);
 useEffect(()=>{const c=new AbortController();life.current=c;return()=>c.abort();},[]);
 async function open() {
  const c=life.current;if(!c||c.signal.aborted||busy)return;
  setBusy(true);setFailed(false);setPromotion(null);
  try {
   const d=readPromotionDiscovery(await api<unknown>(promotionPath(inbound),{signal:c.signal}),inbound),p=d.receipt;
   if(!d.available||!d.promoted||d.blockers.length||!p||p.event_id!==promotionEvent||p.plan_revision!==planRevision||p.source_inbound_revision!==sourceRevision||p.client_ids.length!==1||p.client_ids[0]!==client)throw Error("unmatched execution");
   if(!c.signal.aborted)setPromotion(p);
  }catch{if(!c.signal.aborted)setFailed(true);}
  finally{if(!c.signal.aborted)setBusy(false);}
 }
 return <section aria-label={fa?"اجرای طرح انتقال‌یافته":"Execute transferred setup"}>
  <button type="button" disabled={busy} onClick={()=>void open()}>{fa?"بررسی و بازکردن کنترل‌های اجرا":"Review and open execution controls"}</button>
  {failed&&<p role="alert">{fa?"رسید اجرا با همین مشتری و طرح تطبیق نکرد؛ کنترل‌ها بسته ماندند.":"The execution receipt did not match this client and setup. Controls remain closed."}</p>}
  {promotion&&<>
   <DarkRestoreProtocolHealth model={model} promotion={promotion}/>
   <NativeMeteredRouteTransactions language={model.language} context={{nodeID:promotion.node_id,installationID:promotion.installation_id,inboundID:inbound,inboundRevision:promotion.inbound_revision,draftRevision:promotion.draft_revision}}/>
  </>}
 </section>;
}
