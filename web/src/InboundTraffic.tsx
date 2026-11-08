import { useEffect, useRef, useState } from "react";
import { api, ApiError, useSession } from "./session";
import { creationNodeName } from "./InboundCreationModel";
import type { InboundNode } from "./inboundTypes";
import type { ExecutionTarget } from "./inboundExecution";
import "./InboundTraffic.css";
import InboundTrafficHistory from "./InboundTrafficHistory";

type Path = { node_id: string; tag: string; kind: string; upload: string; download: string; measured: boolean; fresh: boolean; last_usage_unix_nano: number; generation: number; epoch_id: string };
type Sample = { inbound_id: number; sampled_at_unix_nano: number; coverage: string; paths: Path[] };
type Rate = { up: number; down: number };
function readSample(v: Sample, id: number): Sample {
 if (!v || v.inbound_id !== id || !Number.isFinite(v.sampled_at_unix_nano) || !Array.isArray(v.paths) || v.paths.length > 4096) throw Error("invalid_traffic");
 for (const p of v.paths) {
  if (!p || typeof p.node_id !== "string" || typeof p.tag !== "string" || typeof p.kind !== "string" || typeof p.measured !== "boolean" || typeof p.fresh !== "boolean" || !Number.isFinite(p.last_usage_unix_nano)) throw Error("invalid_path");
  for (const value of [p.upload,p.download]) if (typeof value !== "string" || !/^\d{1,19}$/.test(value) || BigInt(value) > 9223372036854775807n) throw Error("invalid_counter");
 }
 return v;
}
export default function InboundTraffic({ inboundID, nodes, targets, onEdit, language }: { language: "fa" | "en"; inboundID: number; nodes: InboundNode[]; targets: ExecutionTarget[]; onEdit: () => void }) {
 const { session, expire } = useSession();
 const fa = language === "fa";
 const [sample,setSample] = useState<Sample|null>(null), [rates,setRates] = useState<Record<string,Rate>>({}), [failed,setFailed] = useState(false), [now,setNow] = useState(Date.now());
 const previous = useRef<Sample|null>(null), received = useRef(0);
 const text = (en:string,faText:string) => fa ? faText : en;
 const key = (p:Path) => p.node_id+"/"+p.tag;
 const bytes = (n:number) => { const units=["B","KiB","MiB","GiB","TiB"];let i=0;while(n>=1024&&i<units.length-1){n/=1024;i++}return n.toLocaleString(fa?"fa-IR":"en-US",{maximumFractionDigits:2})+" "+units[i]; };
 useEffect(() => {
  if (!session || session.user.role !== "owner") return;
  const c=new AbortController();let timer:ReturnType<typeof setTimeout>;
  const clock=setInterval(()=>setNow(Date.now()),1000);
  async function poll() {
   if (!document.hidden) {
    try {
     const next=readSample(await api<Sample>(`/api/v1/workspace/native-inbounds/${inboundID}/execution/traffic`,{signal:c.signal}),inboundID);
     if(c.signal.aborted)return;
     const nextRates:Record<string,Rate>={};
     for(const p of next.paths){const old=previous.current?.paths.find(x=>key(x)===key(p));const dt=old?(p.last_usage_unix_nano-old.last_usage_unix_nano)/1e9:0;
      if(old?.fresh&&p.fresh&&old.measured&&p.measured&&p.epoch_id===old.epoch_id&&p.generation===old.generation&&dt>0&&dt<=15){const up=BigInt(p.upload)-BigInt(old.upload),down=BigInt(p.download)-BigInt(old.download);if(up>=0n&&down>=0n)nextRates[key(p)]={up:Number(up)/dt,down:Number(down)/dt};}
     }
     previous.current=next;received.current=Date.now();setNow(Date.now());setSample(next);setRates(nextRates);setFailed(false);
    }catch(e){if(!c.signal.aborted){setFailed(true);setRates({});if(e instanceof ApiError&&e.status===401)expire();}}
   }
   if(!c.signal.aborted)timer=setTimeout(poll,3000);
  }
  void poll();return()=>{c.abort();clearTimeout(timer);clearInterval(clock);previous.current=null};
 },[inboundID,session,expire]);
 const fresh=!failed&&now-received.current<10000;
 return <section className="inbound-traffic" aria-label={text("Live path traffic","مصرف زندهٔ مسیرها")}>
  <header><div><h3>{text("Live path traffic","مصرف زندهٔ مسیرها")}</h3><p>{text("Recorded network traffic since path metering began. Transit is not charged again to clients.","مصرف شبکه از زمان شروع اندازه‌گیری هر مسیر؛ ترافیک عبوری دوباره از سهمیهٔ مشتری کم نمی‌شود.")}</p></div><button type="button" onClick={onEdit}>{text("Edit nodes and paths","ویرایش نودها و مسیرها")}</button></header>
  <InboundTrafficHistory inboundID={inboundID} nodes={nodes} fa={fa}/>
  {failed&&<p role="status">{text("Live data unavailable; previous readings are stale.","دادهٔ زنده در دسترس نیست؛ مقادیر قبلی قدیمی‌اند.")}</p>}
  <div className="traffic-nodes">
   {targets.filter(target=>target.listeners.some(l=>l.inbound_id===inboundID)).map(target=><article className="traffic-node" key={target.node_id} aria-label={creationNodeName(nodes,target.node_id)}>
    <header><strong>{creationNodeName(nodes,target.node_id)}</strong><span>{target.listeners.filter(l=>l.inbound_id===inboundID).length} {text("paths","مسیر")}</span></header>
    <div className="traffic-paths">{target.listeners.filter(l=>l.inbound_id===inboundID).map(l=>{const p=sample?.paths.find(p=>p.node_id===target.node_id&&p.tag===l.tag),r=p?rates[key(p)]:undefined;const live=!!p?.fresh&&fresh&&target.process_ready===true;const kind=l.kind.includes("swap")?"Swap":l.kind==="tunnel"?"Tunnel":"Direct";
     return <div className="traffic-path" data-path-kind={kind} key={l.tag}>
      <div className="traffic-path-heading"><strong>{kind}{l.kind==="swap_relay"?text(" · transit"," · عبوری"):""}</strong><span className={live?"traffic-live":"traffic-stale"}>{!p?.measured?text("Not measured","اندازه‌گیری نشده"):live?text("Live","زنده"):text("Stale / stopped","قدیمی / متوقف")}</span></div>
      <div className="traffic-total"><span>{text("Total used","مصرف کل")}</span><strong dir="ltr">{p?.measured?bytes(Number(BigInt(p.upload)+BigInt(p.download))):"—"}</strong></div>
      <dl className="traffic-metrics"><div><dt>{text("Upload","آپلود")}</dt><dd dir="ltr">{p?.measured?bytes(Number(p.upload)):"—"}</dd></div><div><dt>{text("Download","دانلود")}</dt><dd dir="ltr">{p?.measured?bytes(Number(p.download)):"—"}</dd></div><div><dt>{text("Upload / s","ارسال / ثانیه")}</dt><dd dir="ltr">{live&&r?bytes(r.up)+"/s":"—"}</dd></div><div><dt>{text("Download / s","دریافت / ثانیه")}</dt><dd dir="ltr">{live&&r?bytes(r.down)+"/s":"—"}</dd></div></dl>
      <details className="traffic-path-details"><summary>{text("Address & last report","آدرس و آخرین گزارش")}</summary><bdi>{l.listen}{l.port?":"+l.port:""}</bdi><p>{text("Last usage report","آخرین گزارش مصرف")}: {p?.last_usage_unix_nano?new Date(p.last_usage_unix_nano/1e6).toLocaleTimeString(fa?"fa-IR":"en-US"):"—"}</p></details>
     </div>;
    })}</div>
   </article>)}
  </div>
  {!targets.some(t=>t.listeners.some(l=>l.inbound_id===inboundID))&&<p>{text("No deployed paths yet.","هنوز مسیری اجرا نشده است.")}</p>}
 </section>;
}
