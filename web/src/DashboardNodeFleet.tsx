import {
  ArrowDownLeft, ArrowRight, ArrowUpRight, Activity, Box, Cpu, Gauge,
  Layers3, Radio, Server, ShieldCheck, Wifi,
} from "lucide-react";
import type { FleetSnapshot, NodeSnapshot } from "./api";
import "./DashboardNodeFleet.css";

type Language = "en" | "fa";
const copy = {
  en: {
    eyebrow: "NODE FLEET  /  LIVE OPERATIONS",
    title: "Every Node. One clear view.",
    lead: "Verified PRO Agents · independent of the HUB's own resources.",
    synced: "Last verified snapshot", syncing: "Loading authenticated fleet",
    delayed: "Snapshot delayed · connection interrupted",
    total: "Registered", online: "Online", offline: "Offline", fresh: "Fresh metrics",
    noNodes: "No Nodes connected yet",
    noNodesHint: "Pair a fresh PRO Agent to start displaying authentic live host readings.",
    cpu: "Processor", ram: "Memory", disk: "Root disk", cores: "CPU cores",
    load: "Load / 1m", tcp: "Host TCP sockets", rx: "Physical RX", tx: "Physical TX",
    onlineState: "Online", offlineState: "Offline", lastOnline: "Last verified status",
    lateMetrics: "Metrics delayed", lastSeen: "Last heartbeat",
    inspect: "Inspect Node", noTrend: "Awaiting real traffic samples",
    trend: "Recent real interface samples", scope: "Host interface only · not customer-billable traffic",
  },
  fa: {
    eyebrow: "NODE FLEET  /  پایش زنده",
    title: "تمام نودها؛ در یک قاب.",
    lead: "Agentهای احرازشدهٔ PRO؛ مستقل از منابع سرور HUB.",
    synced: "آخرین وضعیت تأییدشده", syncing: "در حال دریافت اطلاعات معتبر نودها",
    delayed: "ارتباط مختل · آخرین اطلاعات معتبر",
    total: "نودهای ثبت‌شده", online: "آنلاین", offline: "آفلاین", fresh: "آمار به‌روز",
    noNodes: "هنوز نودی متصل نشده",
    noNodesHint: "با اتصال یک Agent جدید PRO، اطلاعات واقعی سرور در این قسمت نمایش داده می‌شود.",
    cpu: "پردازنده", ram: "حافظه", disk: "دیسک ریشه", cores: "هسته‌های CPU",
    load: "لود / یک دقیقه", tcp: "سوکت‌های TCP سرور", rx: "دریافت فیزیکی", tx: "ارسال فیزیکی",
    onlineState: "آنلاین", offlineState: "آفلاین", lastOnline: "آخرین وضعیت تأییدشده",
    lateMetrics: "دریافت آمار با تأخیر", lastSeen: "آخرین ضربان",
    inspect: "جزئیات نود", noTrend: "در انتظار نمونه‌های واقعی ترافیک",
    trend: "نمونه‌های واقعی اخیر اینترفیس", scope: "فقط شبکهٔ فیزیکی سرور · نه حجم قابل‌کسر مشتری",
  },
} as const;

function bytes(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n) || n < 0) return "—";
  return (n / (1024 ** 3)).toLocaleString("en-US", { maximumFractionDigits: 1 }) + " GiB";
}
function rate(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n) || n < 0) return "—";
  return (n / 1_000_000).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " Mbps";
}
function percentage(used: number | null | undefined, total: number | null | undefined): number | null {
  return used != null && total != null && Number.isFinite(used) && total > 0
    ? Math.max(0, Math.min(100, used / total * 100)) : null;
}
function since(iso: string | null, language: Language): string {
  if (!iso) return "—";
  const date = new Date(iso);
  return Number.isFinite(date.getTime()) ? date.toLocaleTimeString(language === "fa" ? "fa-IR" : "en-GB",
    { hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "—";
}
function Progress({ label, value, detail }: {
  label: string; value: number | null; detail: string;
}) {
  return <div className="node-deck-progress">
    <div className="node-deck-progress-top"><span>{label}</span><strong dir="auto">{detail}</strong></div>
    <div className="node-deck-progress-track" role="progressbar" aria-label={label}
      aria-valuemin={0} aria-valuemax={100} aria-valuenow={value ?? undefined}>
      {value !== null && <span style={{ width: value + "%" }}/>}
    </div>
  </div>;
}
function CPUIndicator({ cpu, language }: { cpu: number | null; language: Language }) {
  const value = cpu == null ? null : Math.max(0, Math.min(100, cpu));
  const circumference = 238.76;
  return <div className="node-deck-cpu">
    <svg viewBox="0 0 100 100" role="img"
      aria-label={copy[language].cpu + ": " + (value === null ? "—" : value.toFixed(1) + "%")}>
      <circle cx="50" cy="50" r="38" className="node-deck-cpu-track"/>
      {value !== null && <circle cx="50" cy="50" r="38" className="node-deck-cpu-value"
        strokeDasharray={circumference} strokeDashoffset={circumference * (1 - value / 100)}/>}
    </svg>
    <div className="node-deck-cpu-text">
      <strong dir="ltr">{value === null ? "—" : value.toFixed(1) + "%"}</strong>
      <small>CPU</small>
    </div>
  </div>;
}
function Trend({ samples, language }: { samples: number[]; language: Language }) {
  const valid = samples.length >= 2 && samples.every(x => Number.isFinite(x) && x >= 0);
  if (!valid) return <span className="node-deck-trend-pending">{copy[language].noTrend}</span>;
  const max = Math.max(1, ...samples);
  const points = samples.map((n, i) =>
    (8 + i * 176 / (samples.length - 1)).toFixed(1) + "," + (47 - n / max * 36).toFixed(1)).join(" ");
  return <svg className="node-deck-trend" viewBox="0 0 192 54" role="img" aria-label={copy[language].trend}>
    <line x1="8" y1="46" x2="184" y2="46" stroke="currentColor" strokeOpacity=".2"/>
    <polyline points={points} fill="none" stroke="currentColor" strokeWidth="2.6"
      strokeLinecap="round" strokeLinejoin="round"/>
  </svg>;
}
function NodeCard({ node, samples, language, stale, onInspect }: {
  node: NodeSnapshot; samples: number[]; language: Language; stale: boolean;
  onInspect: (id: string) => void;
}) {
  const t = copy[language];
  // The previous successful heartbeat is never a present-time assertion
  // when the fleet API is interrupted. Unknown is not zero.
  const connected = node.status === "online";
  const current = !stale && connected && node.metrics_current;
  const cpu = current ? node.cpu_percent : null;
  const ramUsed = current ? node.ram_used_bytes : null;
  const ramTotal = current ? node.ram_total_bytes : null;
  const diskUsed = current ? node.disk_used_bytes : null;
  const diskTotal = current ? node.disk_total_bytes : null;
  const stateLabel = stale ? t.lastOnline + ": " + (connected ? t.onlineState : t.offlineState)
    : connected ? t.onlineState : t.offlineState;

  return <article className={"node-deck-card" + (!connected || stale ? " is-muted" : "")}>
    <header className="node-deck-card-heading">
      <span className="node-deck-card-symbol"><Server size={20}/></span>
      <div className="node-deck-card-identity">
        <strong>{node.name}</strong>
        <small dir="ltr">{node.node_id}</small>
      </div>
      <span className={"node-deck-state " + (stale ? "is-stale" : connected ? "is-online" : "is-offline")}>
        <i/>{stateLabel}
      </span>
    </header>

    <div className="node-deck-card-body">
      <CPUIndicator cpu={cpu} language={language}/>
      <div className="node-deck-card-usage">
        <Progress label={t.ram} value={percentage(ramUsed, ramTotal)}
          detail={ramUsed === null ? "—" : bytes(ramUsed) + " / " + bytes(ramTotal)}/>
        <Progress label={t.disk} value={percentage(diskUsed, diskTotal)}
          detail={diskUsed == null ? "—" : bytes(diskUsed) + " / " + bytes(diskTotal)}/>
        <div className="node-deck-card-minor">
          <span><Cpu size={13}/>{t.cores}: <b>{current && node.cpu_cores > 0 ? node.cpu_cores : "—"}</b></span>
          <span><Gauge size={13}/>{t.load}: <b>{current && node.load_1 != null ? node.load_1.toFixed(2) : "—"}</b></span>
          <span><Layers3 size={13}/>{t.tcp}: <b>{current && node.tcp_inuse != null ? node.tcp_inuse : "—"}</b></span>
        </div>
      </div>
    </div>

    <div className="node-deck-card-network">
      <div className="node-deck-network-speeds">
        <div><ArrowDownLeft size={16}/><span>{t.rx}</span>
          <strong dir="ltr">{rate(current ? node.rx_bits_per_second : null)}</strong></div>
        <div><ArrowUpRight size={16}/><span>{t.tx}</span>
          <strong dir="ltr">{rate(current ? node.tx_bits_per_second : null)}</strong></div>
      </div>
      <Trend samples={current ? samples : []} language={language}/>
    </div>
    <footer className="node-deck-card-foot">
      <span><Radio size={13}/>{t.lastSeen}: {since(node.last_seen,language)}
        {connected && !current && !stale && <em> · {t.lateMetrics}</em>}</span>
      <button type="button" onClick={() => onInspect(node.node_id)}>
        {t.inspect}<ArrowRight size={15}/>
      </button>
    </footer>
  </article>;
}

export default function DashboardNodeFleet({ fleet, history, language, stale, onInspect }: {
  fleet: FleetSnapshot | null; history: Record<string, number[]>;
  language: Language; stale: boolean; onInspect: (nodeID: string) => void;
}) {
  const t = copy[language];
  const known = fleet !== null;
  const fresh = !stale && known ? fleet.nodes.filter(n => n.status === "online" && n.metrics_current).length : null;
  return <section className="node-deck" aria-label="Node fleet monitoring">
    <div className="node-deck-decor" aria-hidden="true"/>
    <header className="node-deck-header">
      <div className="node-deck-header-title">
        <span className="node-deck-kicker"><Activity size={14}/>{t.eyebrow}</span>
        <h2><Box size={21}/>{t.title}</h2>
        <p>{t.lead}</p>
      </div>
      <span className={"node-deck-sync " + (stale ? "is-stale" : !known ? "is-pending" : "")} role="status">
        <i/>{stale ? t.delayed : known ? t.synced : t.syncing}
      </span>
    </header>
    <div className="node-deck-stats" aria-label={language === "fa" ? "خلاصه نودها" : "Node summary"}>
      <div><Server size={16}/><span>{t.total}</span><strong>{known ? fleet.total_nodes : "—"}</strong></div>
      <div><Wifi size={16}/><span>{t.online}</span><strong>{known ? fleet.online_nodes : "—"}</strong></div>
      <div><Radio size={16}/><span>{t.offline}</span><strong>{known ? fleet.offline_nodes : "—"}</strong></div>
      <div><ShieldCheck size={16}/><span>{t.fresh}</span><strong>{fresh === null ? "—" : fresh}</strong></div>
    </div>
    {known && fleet.nodes.length > 0 ? <div className="node-deck-cards">
      {fleet.nodes.map(node => <NodeCard key={node.node_id} node={node}
        samples={history[node.node_id] ?? []} stale={stale}
        language={language} onInspect={onInspect}/>)}
    </div> : <div className="node-deck-empty">
      <span className="node-deck-empty-icon"><Server size={23}/></span>
      <h3>{known ? t.noNodes : t.syncing}</h3>
      <p>{t.noNodesHint}</p>
    </div>}
    <p className="node-deck-scope"><ShieldCheck size={14}/>{t.scope}</p>
  </section>;
}
