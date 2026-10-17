import { useEffect, useState } from "react";
import { api, ApiError } from "./session";
import "./NodeHistory.css";

type Window = "1h" | "24h";
type Point = {
  ts_unix: number;
  cpu_percent: number;
  ram_used_bytes: number;
  ram_total_bytes: number;
  disk_used_bytes: number | null;
  disk_total_bytes: number | null;
  load_1: number | null;
  tcp_inuse: number | null;
  rx_bits_per_second: number | null;
  tx_bits_per_second: number | null;
  observations: number;
};
type Result = {
  node_id: string;
  window: Window;
  bucket_seconds: number;
  scope: "host_interface_not_billable";
  points: Point[];
};
type Language = "fa" | "en";

const copy = {
  en: {
    title: "Real Node history", hour: "1 hour", day: "24 hours",
    cpu: "CPU usage", traffic: "Physical interface speed",
    download: "RX", upload: "TX", noSamples: "No authenticated measurements in this period.",
    note: "History is sampled once per minute (24h shown in five-minute buckets). Gaps mean missing data, not zero. Network speed is NOT customer-billable traffic.",
    loading: "Reading stored Node history…", unavailable: "Private Node history is not enabled yet.",
    failed: "History service unavailable. Previous verified points remain visible.",
    last: "Last verified sample", points: "observed buckets",
  },
  fa: {
    title: "تاریخچهٔ واقعی نود", hour: "۱ ساعت", day: "۲۴ ساعت",
    cpu: "مصرف پردازنده", traffic: "سرعت کارت شبکه",
    download: "دریافت", upload: "ارسال", noSamples: "در این بازه نمونهٔ معتبر ثبت نشده است.",
    note: "نمونهٔ تاریخچه هر دقیقه ثبت می‌شود (در نمای ۲۴ساعته، بازه‌های پنج‌دقیقه‌ای). فاصلهٔ خالی یعنی نبود داده، نه مصرف صفر. سرعت کارت شبکه مصرف قابل‌کسر مشتری نیست.",
    loading: "در حال خواندن تاریخچهٔ نود…", unavailable: "تاریخچهٔ خصوصی نود هنوز فعال نشده است.",
    failed: "سرویس تاریخچه در دسترس نیست؛ آخرین دادهٔ معتبر حفظ شده است.",
    last: "آخرین نمونهٔ معتبر", points: "بازهٔ دارای داده",
  },
} as const;

function formatTime(ts: number, language: Language): string {
  const d = new Date(ts * 1000);
  return Number.isFinite(d.getTime()) ? d.toLocaleTimeString(language === "fa" ? "fa-IR" : "en-GB", { hour: "2-digit", minute: "2-digit" }) : "—";
}
function physicalRate(value: number): string {
  return (value / 1_000_000).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " Mbps";
}
type SeriesKey = "cpu_percent" | "rx_bits_per_second" | "tx_bits_per_second";
function Graph({ points, selected, window, bucket, variant }: {
  points: Point[];selected: SeriesKey;window: Window;bucket:number;variant: "cpu"|"rx"|"tx";
}) {
  const valid = points.filter(p => Number.isFinite(p.ts_unix) &&
    typeof p[selected] === "number" && Number.isFinite(p[selected]));
  if (valid.length === 0) return <div className="pro-history-empty">—</div>;
  const maxObserved = Math.max(1, ...valid.map(p => p[selected] as number));
  const maximum = selected === "cpu_percent" ? 100 : maxObserved;
  const end = Math.floor(Date.now() / 1000);
  const start = end - (window === "1h" ? 3600 : 86400);
  const x = (s:number) => Math.min(620, Math.max(10, 10 + (s - start) / (end - start) * 610));
  const y = (v:number) => Math.max(8, Math.min(100, 100 - (v / maximum * 88)));
  const segments: string[] = [];
  let current: string[] = [];
  let previous: Point | null = null;
  for (const p of valid) {
    if (previous && p.ts_unix - previous.ts_unix > bucket * 1.6) {
      if (current.length > 1) segments.push(current.join(" "));
      current = [];
    }
    current.push(x(p.ts_unix).toFixed(2) + "," + y(p[selected] as number).toFixed(2));
    previous = p;
  }
  if (current.length > 1) segments.push(current.join(" "));
  return <svg className={"pro-history-chart pro-history-" + variant} viewBox="0 0 630 120" role="img"
    aria-label={selected === "cpu_percent" ? "CPU % historical values" : selected.toUpperCase()+" host physical network"}>
    <line x1="10" y1="100" x2="620" y2="100" stroke="currentColor" strokeOpacity=".2" />
    <line x1="10" y1="56" x2="620" y2="56" stroke="currentColor" strokeOpacity=".1" />
    {segments.map((p,i) => <polyline key={i} points={p} fill="none" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />)}
    {valid.map((p,i) => <circle key={p.ts_unix + "-" + i} cx={x(p.ts_unix)} cy={y(p[selected] as number)} r="2.8" fill="currentColor">
      <title>{formatTime(p.ts_unix, "en")} · {selected === "cpu_percent" ? (p.cpu_percent.toFixed(1)+"%") : physicalRate(p[selected] as number)}</title>
    </circle>)}
  </svg>;
}

export default function NodeHistory({ nodeID, language }: { nodeID: string;language:Language }) {
  const [windowValue,setWindow] = useState<Window>("1h");
  const [data,setData] = useState<Result | null>(null);
  const [state,setState] = useState<"loading"|"ready"|"disabled"|"failed">("loading");
  const t=copy[language];

  useEffect(() => {
    const controller=new AbortController();
    setData(null);setState("loading");
    const poll=async () => {
      if (document.hidden) return;
      try {
        const result = await api<Result>("/api/v1/workspace/nodes/"+encodeURIComponent(nodeID)+"/history?window="+windowValue,{signal:controller.signal});
        if (controller.signal.aborted) return;
        if (result.node_id!==nodeID||result.window!==windowValue||result.scope!=="host_interface_not_billable"||
          result.bucket_seconds!==(windowValue==="1h"?60:300)||!Array.isArray(result.points)||
          result.points.length>290||result.points.some(p=>!Number.isFinite(p.ts_unix)||!Number.isFinite(p.cpu_percent))) {
          throw new Error("invalid_history_response");
        }
        setData(result);setState("ready");
      } catch (error) {
        if(controller.signal.aborted)return;
        setState(error instanceof ApiError&&error.status===503?"disabled":"failed");
      }
    };
    void poll();
    const timer=window.setInterval(()=>{void poll();},30000);
    return ()=>{controller.abort();window.clearInterval(timer);};
  },[nodeID,windowValue]);
  const points=data?.points??[];
  const last=points.at(-1)?.ts_unix;
  return <section className="pro-node-history" role="region" aria-label={t.title}>
    <header className="pro-node-history-heading">
      <div><h3>{t.title}</h3><p>{t.note}</p></div>
      <div className="pro-node-history-switch" role="group" aria-label={t.title}>
        {(["1h","24h"] as Window[]).map(w=><button type="button" key={w} aria-pressed={windowValue===w}
            onClick={()=>setWindow(w)}>{w==="1h"?t.hour:t.day}</button>)}
      </div>
    </header>
    {state==="loading" && <p className="pro-node-history-message">{t.loading}</p>}
    {state==="disabled" && <p className="pro-node-history-message" role="status">{t.unavailable}</p>}
    {state==="failed" && <p className="pro-node-history-message" role="alert">{t.failed}</p>}
    {state==="ready" && points.length===0 && <p className="pro-node-history-message">{t.noSamples}</p>}
    {data && points.length>0 && <>
      <p className="pro-node-history-meta">{points.length} {t.points} · {t.last}: {last===undefined?"—":formatTime(last,language)}</p>
      <div className="pro-node-history-graph">
        <h4>{t.cpu} · %</h4>
        <Graph points={points} selected="cpu_percent" window={windowValue} bucket={data.bucket_seconds} variant="cpu"/>
        <h4>{t.traffic} · Mbps</h4>
        <div className="pro-node-history-network">
          <div><span>{t.download}</span><Graph points={points} selected="rx_bits_per_second" window={windowValue} bucket={data.bucket_seconds} variant="rx"/></div>
          <div><span>{t.upload}</span><Graph points={points} selected="tx_bits_per_second" window={windowValue} bucket={data.bucket_seconds} variant="tx"/></div>
        </div>
      </div>
    </>}
  </section>;
}
