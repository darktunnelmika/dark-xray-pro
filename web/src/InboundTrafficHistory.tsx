import { useEffect, useState } from "react";
import { api } from "./session";
import { creationNodeName } from "./InboundCreationModel";
import type { InboundNode } from "./inboundTypes";
type Bucket={node_id:string;principal_id:string;role:string;at_unix:number;upload:string;download:string};
type History={inbound_id:number;window:string;from_unix:number;until_unix:number;bucket_seconds:number;coverage:string;buckets:Bucket[]};
export default function InboundTrafficHistory({inboundID,nodes,fa}:{inboundID:number;nodes:InboundNode[];fa:boolean}){
 const [window,setWindow]=useState("live"),[node,setNode]=useState("all"),[data,setData]=useState<History|null>(null),[failed,setFailed]=useState(false),[loading,setLoading]=useState(false);
 const t=(en:string,other:string)=>fa?other:en;
 useEffect(()=>{if(window==="live")return;const c=new AbortController();setLoading(true);setFailed(false);setData(null);
 void api<History>(`/api/v1/workspace/native-inbounds/${inboundID}/execution/traffic/history?window=${window}`,{signal:c.signal}).then(x=>{
  if(x.inbound_id!==inboundID||x.window!==window||!Array.isArray(x.buckets)||x.buckets.length>32768||!Number.isFinite(x.from_unix)||!Number.isFinite(x.until_unix)||x.until_unix<=x.from_unix)throw Error("invalid_history");
  for(const b of x.buckets){if(!Number.isFinite(b.at_unix)||typeof b.node_id!=="string"||!["owner_aggregate","transit"].includes(b.role)||![b.upload,b.download].every(v=>typeof v==="string"&&/^\d{1,19}$/.test(v)))throw Error("invalid_bucket")}
  if(!c.signal.aborted)setData(x);
 }).catch(()=>{if(!c.signal.aborted)setFailed(true)}).finally(()=>{if(!c.signal.aborted)setLoading(false)});return()=>c.abort();},[inboundID,window]);
 const totals=new Map<number,number>();for(const b of data?.buckets??[]){if(b.role==="transit"||node!=="all"&&b.node_id!==node)continue;totals.set(b.at_unix,(totals.get(b.at_unix)??0)+Number(BigInt(b.upload)+BigInt(b.download)))}
 const series=[...totals].sort((a,b)=>a[0]-b[0]);const max=Math.max(1,...series.map(x=>x[1]));
 return <div className="inbound-traffic-history"><div className="traffic-history-controls" role="group" aria-label={t("Traffic period","بازهٔ مصرف")}>
 {[['live',t('Live','زنده')],['1h',t('1 hour','۱ ساعت')],['24h',t('24 hours','۲۴ ساعت')]].map(([v,label])=><button key={v} type="button" aria-pressed={window===v} onClick={()=>setWindow(v)}>{label}</button>)}
 {window!=="live"&&<select aria-label={t("History node","نود نمودار")} value={node} onChange={e=>setNode(e.target.value)}><option value="all">{t("All nodes","همهٔ نودها")}</option>{nodes.map(n=><option key={n.node_id} value={n.node_id}>{creationNodeName(nodes,n.node_id)}</option>)}</select>}</div>
 {window!=="live"&&<div aria-live="polite">{loading?<p>{t("Loading recorded traffic…","در حال دریافت مصرف ثبت‌شده…")}</p>:failed?<p>{t("History unavailable. No values have been estimated.","تاریخچه در دسترس نیست؛ عدد تخمینی نمایش داده نمی‌شود.")}</p>:!series.length?<p>{t("No measured path samples in this period.","در این بازه نمونهٔ اندازه‌گیری‌شده‌ای برای مسیر وجود ندارد.")}</p>:data&&<figure>
 <svg viewBox="0 0 640 160" role="img" aria-label={t("Recorded inbound traffic; transit excluded","مصرف ثبت‌شدهٔ اینباند؛ بدون جمع دوبارهٔ ترافیک عبوری")}><line x1="0" y1="145" x2="640" y2="145" stroke="currentColor" opacity=".3"/>{series.map(([at,n])=><rect key={at} x={Math.max(0,(at-data.from_unix)/(data.until_unix-data.from_unix)*630)} y={145-n/max*125} width="8" height={Math.max(1,n/max*125)} rx="2" fill="currentColor"><title>{new Date(at*1000).toLocaleTimeString(fa?"fa-IR":"en-US")} · {(n/1048576).toFixed(2)} MiB</title></rect>)}</svg>
 <figcaption>{t("Recorded ingress bytes per interval; Swap transit is excluded from this total. Earlier unmeasured periods are not reconstructed.","مصرف ورودی ثبت‌شده در هر بازه؛ بخش عبوری Swap دوباره جمع نمی‌شود. بازه‌های قدیمیِ بدون اندازه‌گیری بازسازی نمی‌شوند.")}</figcaption>
 </figure>}</div>}
 </div>;
}
