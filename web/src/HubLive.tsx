import { useEffect, useState } from "react";
import {
  Activity, ArrowDownLeft, ArrowUpRight, Cpu, Gauge,
  HardDrive, MemoryStick, Radio, Server, ShieldCheck, Wifi, Workflow,
} from "lucide-react";
import { api } from "./session";
import "./HubLive.css";

type Language = "en" | "fa";
type Series = "cpu_percent" | "rx_bits_per_second" | "tx_bits_per_second";
type Point = { ts_unix: number; cpu_percent: number | null;
  rx_bits_per_second: number | null; tx_bits_per_second: number | null };
type HubSnapshot = {
  scope: "hub_host_physical_not_billable";
  source: "local_linux";
  sampled_at: string | null;
  metrics_current: boolean;
  cpu_cores: number;
  cpu_percent: number | null;
  ram_total_bytes: number | null;
  ram_used_bytes: number | null;
  disk_total_bytes: number | null;
  disk_used_bytes: number | null;
  load_1: number | null;
  load_5: number | null;
  load_15: number | null;
  host_uptime_seconds: number | null;
  tcp_inuse: number | null;
  interface: string;
  rx_bits_per_second: number | null;
  tx_bits_per_second: number | null;
  hub_process_uptime_seconds: number;
  hub_process_rss_bytes: number | null;
  history: Point[];
};
const words = {
  en: {
    eyebrow: "DARK XRAY PRO  /  CONTROL PLANE",
    heading: "The heart of your infrastructure.",
    subtitle: "Actual resources of this HUB server — independent of every Node.",
    online: "HUB API responsive", unavailable: "HUB telemetry unavailable", stale: "HUB stats delayed",
    cpu: "CPU", ram: "Memory", disk: "Root disk", load: "Load 1m", process: "HUB process RAM",
    hostUptime: "Host uptime", hubUptime: "HUB service uptime", sockets: "Host TCP sockets",
    rx: "Physical download", tx: "Physical upload", scope: "HUB host interface · NOT billable customer traffic",
    history: "LIVE HOST TELEMETRY", trend: "Actual samples · recent three minutes", noSeries: "Waiting for the next verified measurements",
    last: "Last HUB sample", status: "Local Linux sampler", cores: "cores", iface: "NIC",
    graphCPU: "CPU %", graphRX: "RX Mbps", graphTX: "TX Mbps", retry: "Retrying automatically",
    host: "HUB HOST", connecting: "Connecting to HUB monitoring…",
    memoryUsed: "used", unknown: "No verified reading",
  },
  fa: {
    eyebrow: "DARK XRAY PRO  /  مرکز کنترل",
    heading: "قلب زیرساخت، زیر نظر تو.",
    subtitle: "منابع واقعی سرور اصلی HUB؛ مستقل از تمام نودها.",
    online: "ارتباط API هاب برقرار", unavailable: "مانیتورینگ هاب در دسترس نیست", stale: "اطلاعات منابع هاب با تأخیر",
    cpu: "پردازنده", ram: "حافظه", disk: "دیسک ریشه", load: "لود ۱ دقیقه", process: "رم فرایند HUB",
    hostUptime: "آپتایم سرور", hubUptime: "آپتایم سرویس HUB", sockets: "سوکت‌های TCP سرور",
    rx: "دریافت فیزیکی", tx: "ارسال فیزیکی", scope: "شبکهٔ فیزیکی HUB · مصرف قابل‌کسر مشتری نیست",
    history: "نمودار لحظه‌ای سرور", trend: "نمونه‌های واقعی · سه دقیقهٔ اخیر", noSeries: "در انتظار نمونه‌های معتبر بعدی",
    last: "آخرین نمونهٔ HUB", status: "مانیتور محلی لینوکس", cores: "هسته", iface: "کارت شبکه",
    graphCPU: "CPU ٪", graphRX: "RX Mbps", graphTX: "TX Mbps", retry: "تلاش مجدد خودکار",
    host: "سرور HUB", connecting: "در حال اتصال به مانیتورینگ HUB…",
    memoryUsed: "مصرف‌شده", unknown: "هنوز دادهٔ معتبر نداریم",
  },
} as const;

function finite(n: unknown, limit = Number.MAX_SAFE_INTEGER): n is number {
  return typeof n === "number" && Number.isFinite(n) && n >= 0 && n <= limit;
}
function maybe(n: unknown, limit?: number): boolean { return n === null || finite(n, limit); }
function validate(value: unknown): HubSnapshot {
  if (!value || typeof value !== "object") throw new Error("invalid_hub_metrics");
  const s = value as Partial<HubSnapshot>;
  if (s.scope !== "hub_host_physical_not_billable" || s.source !== "local_linux" ||
      typeof s.metrics_current !== "boolean" || !finite(s.cpu_cores, 1024) ||
      !finite(s.hub_process_uptime_seconds, 1e12) ||
      (s.sampled_at !== null && (typeof s.sampled_at !== "string" || !Number.isFinite(Date.parse(s.sampled_at)))) ||
      !maybe(s.cpu_percent, 100) || !maybe(s.ram_used_bytes) || !maybe(s.ram_total_bytes) ||
      !maybe(s.disk_used_bytes) || !maybe(s.disk_total_bytes) ||
      !maybe(s.load_1, 1e6) || !maybe(s.load_5, 1e6) || !maybe(s.load_15, 1e6) ||
      !maybe(s.host_uptime_seconds, 1e12) || !maybe(s.tcp_inuse, 1e8) ||
      !maybe(s.rx_bits_per_second, 1e15) || !maybe(s.tx_bits_per_second, 1e15) ||
      !maybe(s.hub_process_rss_bytes) || typeof s.interface !== "string" || s.interface.length > 32 ||
      !Array.isArray(s.history) || s.history.length > 90 ||
      s.history.some(point => !Number.isSafeInteger(point.ts_unix) ||
        !maybe(point.cpu_percent, 100) || !maybe(point.rx_bits_per_second, 1e15) ||
        !maybe(point.tx_bits_per_second, 1e15))) {
    throw new Error("invalid_hub_metrics");
  }
  if (s.ram_total_bytes !== null && s.ram_used_bytes !== null &&
      s.ram_total_bytes !== undefined && s.ram_used_bytes !== undefined &&
      s.ram_used_bytes > s.ram_total_bytes) throw new Error("invalid_hub_ram");
  if (s.disk_total_bytes !== null && s.disk_used_bytes !== null &&
      s.disk_total_bytes !== undefined && s.disk_used_bytes !== undefined &&
      s.disk_used_bytes > s.disk_total_bytes) throw new Error("invalid_hub_disk");
  if (s.history.some((point, i) => i > 0 && s.history![i - 1].ts_unix > point.ts_unix)) {
    throw new Error("unordered_hub_history");
  }
  return s as HubSnapshot;
}
function bytes(value: number | null | undefined): string {
  if (value == null) return "—";
  const divisor = value >= 1024 ** 3 ? 1024 ** 3 : 1024 ** 2;
  return (value / divisor).toLocaleString("en-US", { maximumFractionDigits: 1 }) +
    (divisor === 1024 ** 3 ? " GiB" : " MiB");
}
function mbps(value: number | null | undefined): string {
  return value == null ? "—" :
    (value / 1_000_000).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " Mbps";
}
function elapsed(value: number | null | undefined, lang: Language): string {
  if (value == null) return "—";
  const days = Math.floor(value / 86400);
  const hours = Math.floor(value % 86400 / 3600);
  const mins = Math.floor(value % 3600 / 60);
  return days > 0 ? `${days}${lang === "fa" ? " روز" : "d"}  ${hours}h` :
    `${hours}h ${mins}m`;
}
function percent(a: number | null | undefined, b: number | null | undefined): number | null {
  return a != null && b != null && b > 0 ? Math.max(0, Math.min(100, a / b * 100)) : null;
}
function Meter({ label, value, text }: { label: string; value: number | null; text: string }) {
  return <div className="hub-meter">
    <div className="hub-meter-title"><span>{label}</span><strong>{text}</strong></div>
    <div className="hub-meter-track" aria-label={label} role="progressbar"
      aria-valuemin={0} aria-valuemax={100} aria-valuenow={value === null ? undefined : value}>
      {value !== null && <span style={{ width: value + "%" }}/>}
    </div>
  </div>;
}
function LiveChart({ points, series, language }: {points: Point[]; series: Series; language: Language }) {
  const rows=points.filter(p => finite(p[series]));
  if (rows.length === 0) return <div className="hub-chart-empty">{words[language].noSeries}</div>;
  const latest=points.at(-1)?.ts_unix ?? rows.at(-1)!.ts_unix;
  const start=latest-180;
  const max=series==="cpu_percent" ? 100 : Math.max(1, ...rows.map(p => p[series] ?? 0));
  const x=(time:number)=>Math.max(8,Math.min(692,8+(time-start)/180*684));
  const y=(value:number)=>Math.max(14,Math.min(136,136-value/max*115));
  let segments:string[][]=[],current:string[]=[];
  let previous:number|undefined;
  for(const point of points){
    const v=point[series];
    if(v===null || !finite(v)){
      if(current.length>=2)segments.push(current);
      current=[];previous=undefined;continue;
    }
    if(previous!==undefined && point.ts_unix-previous>5){
      if(current.length>=2)segments.push(current);
      current=[];
    }
    current.push(x(point.ts_unix).toFixed(1)+","+y(v).toFixed(1));
    previous=point.ts_unix;
  }
  if(current.length>=2)segments.push(current);
  return <svg className="hub-live-chart" viewBox="0 0 700 160" role="img"
    aria-label={series==="cpu_percent"?"HUB CPU measured history":"HUB physical interface measured history"}>
    <line x1="8" y1="136" x2="692" y2="136" className="hub-chart-grid"/>
    <line x1="8" y1="75" x2="692" y2="75" className="hub-chart-grid"/>
    {segments.map((segment,index)=><polyline key={index} points={segment.join(" ")}
      fill="none" stroke="currentColor" strokeWidth="2.6" strokeLinejoin="round" strokeLinecap="round"/>)}
    {rows.map((point,index)=><circle key={point.ts_unix+"-"+index}
      cx={x(point.ts_unix)} cy={y(point[series]!)} r="2.5" fill="currentColor"/>)}
  </svg>;
}

export default function HubLive({ language }: {language: Language}) {
  const t=words[language];
  const [data,setData]=useState<HubSnapshot|null>(null);
  const [unavailable,setUnavailable]=useState(false);
  const [graph,setGraph]=useState<Series>("cpu_percent");
  useEffect(()=>{
    let active=true;
    let controller:AbortController|null=null;
    let timer:number|undefined;
    async function poll(){
      if(!active||document.hidden||controller)return;
      const request=new AbortController();
      controller=request;
      const timeout=window.setTimeout(()=>request.abort(),3500);
      try{
        const result=validate(await api<unknown>("/api/v1/workspace/hub/metrics",{signal:request.signal}));
        if(active && !request.signal.aborted){setData(result);setUnavailable(false);}
      }catch{
        if(active && !document.hidden)setUnavailable(true);
      }finally{
        window.clearTimeout(timeout);controller=null;
        if(active)timer=window.setTimeout(()=>{void poll();},2500);
      }
    }
    const resume=()=>{
      if(!document.hidden&&!controller&&active){window.clearTimeout(timer);void poll();}
    };
    void poll();
    document.addEventListener("visibilitychange",resume);
    window.addEventListener("online",resume);
    return()=>{
      active=false;window.clearTimeout(timer);controller?.abort();
      document.removeEventListener("visibilitychange",resume);
      window.removeEventListener("online",resume);
    };
  },[]);
  const fresh=!!data?.metrics_current && !unavailable;
  const cpu=fresh?data!.cpu_percent:null;
  const ramUsed=fresh?data!.ram_used_bytes:null;
  const ramTotal=fresh?data!.ram_total_bytes:null;
  const diskUsed=fresh?data!.disk_used_bytes:null;
  const diskTotal=fresh?data!.disk_total_bytes:null;
  const lastSample=data?.sampled_at ? new Date(data.sampled_at).toLocaleTimeString(
    language==="fa"?"fa-IR":"en-GB",{hour:"2-digit",minute:"2-digit",second:"2-digit"}) : "—";
  return <section className="hub-live" role="region" aria-label="HUB live monitor">
    <div className="hub-live-decor" aria-hidden="true"><i/><i/><i/></div>
    <div className="hub-live-header">
      <div className="hub-live-heading">
        <span className="hub-live-eyebrow"><Workflow size={15}/>{t.eyebrow}</span>
        <div className="hub-live-title-row">
          <span className="hub-live-symbol"><Server size={24}/></span>
          <div><h2>{t.heading}</h2><p>{t.subtitle}</p></div>
        </div>
      </div>
      <span className={"hub-live-status "+(unavailable?"hub-live-failed":!fresh?"hub-live-pending":"hub-live-connected")}
        role="status"><i/>{unavailable?t.unavailable:fresh?t.online:data?t.stale:t.connecting}</span>
    </div>

    <div className="hub-live-body">
      <div className="hub-live-side">
        <div className="hub-live-resource-title"><span><Radio size={16}/>{t.host}</span>
          <small>{t.status}</small></div>
        <div className="hub-live-meters">
          <Meter label={t.cpu} value={cpu} text={cpu==null?"—":cpu.toFixed(1)+"%"}/>
          <Meter label={t.ram} value={percent(ramUsed,ramTotal)}
            text={ramUsed==null?"—":bytes(ramUsed)+" / "+bytes(ramTotal)}/>
          <Meter label={t.disk} value={percent(diskUsed,diskTotal)}
            text={diskUsed==null?"—":bytes(diskUsed)+" / "+bytes(diskTotal)}/>
        </div>
        <div className="hub-live-rates">
          <div><ArrowDownLeft size={17}/><span>{t.rx}</span>
            <strong>{mbps(fresh?data!.rx_bits_per_second:null)}</strong></div>
          <div><ArrowUpRight size={17}/><span>{t.tx}</span>
            <strong>{mbps(fresh?data!.tx_bits_per_second:null)}</strong></div>
        </div>
      </div>
      <div className="hub-live-trends">
        <div className="hub-live-trend-head">
          <div><h3><Activity size={15}/>{t.history}</h3><small>{t.trend}</small></div>
          <div className="hub-live-switch" role="group" aria-label="HUB chart metric">
            {(["cpu_percent","rx_bits_per_second","tx_bits_per_second"] as Series[]).map(item=>
              <button key={item} type="button" aria-pressed={graph===item}
                onClick={()=>setGraph(item)}>
                {item==="cpu_percent"?t.graphCPU:item==="rx_bits_per_second"?t.graphRX:t.graphTX}
              </button>)}
          </div>
        </div>
        <div className="hub-live-canvas">
          <LiveChart points={data?.history??[]} series={graph} language={language}/>
        </div>
        <div className="hub-live-chart-caption"><span><i/>{t.last}: <b>{lastSample}</b></span>
          <span>{t.iface}: <b dir="ltr">{fresh?data!.interface||"—":"—"}</b></span></div>
      </div>
    </div>

    <div className="hub-live-footer">
      {[
        {icon:Cpu,label:t.cores,value:fresh&&data!.cpu_cores>0?String(data!.cpu_cores):"—"},
        {icon:MemoryStick,label:t.process,value:bytes(fresh?data!.hub_process_rss_bytes:null)},
        {icon:Gauge,label:t.load,value:fresh&&data!.load_1!=null?data!.load_1.toFixed(2):"—"},
        {icon:HardDrive,label:t.sockets,value:fresh&&data!.tcp_inuse!=null?String(data!.tcp_inuse):"—"},
        {icon:Wifi,label:t.hostUptime,value:elapsed(fresh?data!.host_uptime_seconds:null,language)},
        {icon:ShieldCheck,label:t.hubUptime,value:elapsed(unavailable?null:data?.hub_process_uptime_seconds,language)},
      ].map(({icon:Icon,label,value})=><div className="hub-live-footer-item" key={label}>
        <span><Icon size={14}/>{label}</span><strong dir="auto">{value}</strong>
      </div>)}
    </div>
    <p className="hub-live-scope"><ShieldCheck size={14}/>{t.scope}</p>
  </section>;
}
