import {useEffect,useState} from "react";
import {api} from "../session";
import type {BusinessModel} from "./useBusinessWorkspace";
type Choice={id:number;name:string;archived:boolean;clients:number};
export default function ClientInboundFilter({model:m}:{model:BusinessModel}){
 const [choices,setChoices]=useState<Choice[]|null>(null),[failed,setFailed]=useState(false),[retry,setRetry]=useState(0);
 const fa=m.language==="fa",scope=m.session.user.role==="owner"?"":m.session.user.representative_id;
 useEffect(()=>{const c=new AbortController();setChoices(null);setFailed(false);
 void api<{scope:string;representative_id:string;inbounds:Choice[]}>("/api/v1/workspace/client-inbound-options",{signal:c.signal}).then(v=>{
 if(v.scope!=="saved_client_assignments"||v.representative_id!==scope||!Array.isArray(v.inbounds)||v.inbounds.length>256||v.inbounds.some(x=>!Number.isSafeInteger(x.id)||x.id<=0||typeof x.name!=="string"||typeof x.archived!=="boolean"||!Number.isSafeInteger(x.clients)||x.clients<0||(scope!==""&&x.clients===0))||new Set(v.inbounds.map(x=>x.id)).size!==v.inbounds.length)throw Error("invalid_inbound_options");
 if(!c.signal.aborted)setChoices(v.inbounds);
 }).catch(()=>{if(!c.signal.aborted)setFailed(true)});return()=>c.abort();
 },[scope,m.session.user.id,m.refreshKey,retry]);
 return <div className="client-inbound-filter"><label>{fa?"اینباند مشتری":"Client inbound"}<select aria-label={fa?"اینباند مشتری":"Client inbound"} value={m.clientInboundFilter} disabled={!choices||m.actionBusy} onChange={e=>m.changeClientFilters("inbound",e.target.value)}>
 <option value="">{fa?"همه اینباندها":"All inbounds"}</option>
 {m.clientInboundFilter&&!choices?.some(c=>String(c.id)===m.clientInboundFilter)&&<option value={m.clientInboundFilter}>#{m.clientInboundFilter}</option>}
 {choices?.map(c=><option key={c.id} value={String(c.id)}>{c.name} · #{c.id}{c.archived?(fa?" (آرشیو)":" (archived)"):""}</option>)}
 </select></label><small>{failed?(fa?"فهرست سرویس‌ها دریافت نشد.":"Service list unavailable."):choices?(fa?"بر اساس عضویت ثبت‌شده؛ نه وضعیت آنلاین.":"Saved assignments, not online status."):(fa?"در حال دریافت سرویس‌ها…":"Loading services…")}</small>
 {failed&&<button type="button" onClick={()=>setRetry(x=>x+1)}>{fa?"تلاش دوباره":"Retry service list"}</button>}
 {m.clientInboundFilter&&<button type="button" disabled={m.actionBusy} onClick={()=>m.changeClientFilters("inbound","")}>{fa?"پاک‌کردن فیلتر اینباند":"Clear inbound filter"}</button>}
 </div>;
}
