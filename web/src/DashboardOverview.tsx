import { useState } from "react";
import {
  Activity, ArrowDownLeft, ArrowUpRight, ChartNoAxesCombined, CheckCircle2,
  CircleAlert, Cpu, RadioTower, Server, ShieldCheck, Signal,
} from "lucide-react";
import type { FleetSnapshot } from "./api";
import NodeHistory from "./NodeHistory";

type Language = "en" | "fa";

const labels = {
  en: {
    title: "Fleet intelligence", explanation: "Live host telemetry, with an explicit boundary between physical traffic and customer usage.",
    observed: "Verified fleet snapshot", delayed: "Last verified snapshot · connection interrupted", waiting: "Waiting for authentic Node data",
    nodes: "Total Nodes", online: "Online", offline: "Offline", rx: "Physical RX", tx: "Physical TX",
    cpu: "Live average CPU", cpuNote: "Only Nodes with fresh metrics", linkNote: "Default-route interface · not billable customer usage",
    countNote: "Authenticated Node roster", stateNote: "Based on latest verified heartbeat",
    quality: "Availability", qualityLead: "Heartbeat status is separate from the freshness of CPU and RAM readings.",
    fresh: "Fresh metrics", outdated: "Delayed metrics", noNodes: "No connected Nodes",
    overview: "Fleet summary", coverage: "Online Nodes", missing: "A Node can remain Online when its resource sampling is delayed.",
    historyTitle: "Real Node history", historyLead: "Read the stored 1-hour or 24-hour chart for one verified PRO Node.",
    choose: "History Node", openHistory: "View stored history", hideHistory: "Hide history",
    noHistory: "History will appear after a new PRO Agent is paired and sends authenticated readings.",
    scope: "Physical host network only · never subscription or representative traffic.",
  },
  fa: {
    title: "نمای هوشمند زیرساخت", explanation: "اطلاعات واقعی سرورها؛ ترافیک فیزیکی و مصرف مشتری جدا از هم.",
    observed: "آخرین وضعیت تأییدشده", delayed: "اطلاعات تأییدشدهٔ قبلی · ارتباط مختل", waiting: "در انتظار اطلاعات معتبر نود",
    nodes: "کل نودها", online: "آنلاین", offline: "آفلاین", rx: "دریافت فیزیکی", tx: "ارسال فیزیکی",
    cpu: "میانگین CPU زنده", cpuNote: "فقط نودهایی که آمار تازه دارند", linkNote: "اینترفیس مسیر پیش‌فرض · مصرف قابل‌کسر مشتری نیست",
    countNote: "فهرست نودهای احرازشده", stateNote: "بر اساس آخرین ضربان معتبر",
    quality: "سلامت ناوگان", qualityLead: "وضعیت اتصال از تازه‌بودن اطلاعات CPU و RAM مستقل است.",
    fresh: "آمار به‌روز", outdated: "آمار با تأخیر", noNodes: "هنوز نودی متصل نیست",
    overview: "خلاصهٔ وضعیت", coverage: "نودهای آنلاین", missing: "نود ممکن است آنلاین باشد اما اطلاعات منابع آن با تأخیر دریافت شود.",
    historyTitle: "تاریخچهٔ واقعی نود", historyLead: "نمودار ذخیره‌شدهٔ یک‌ساعته یا ۲۴ساعته برای یک نود احرازشدهٔ PRO.",
    choose: "انتخاب نود", openHistory: "نمایش تاریخچه", hideHistory: "بستن تاریخچه",
    noHistory: "پس از اتصال Agent جدید PRO و دریافت دادهٔ تأییدشده، تاریخچه نمایش داده می‌شود.",
    scope: "فقط شبکهٔ فیزیکی سرور · نه ترافیک اشتراک یا نماینده.",
  },
} as const;

function rate(value: number | undefined): string {
  return value == null || !Number.isFinite(value) || value < 0 ? "—" :
    (value / 1_000_000).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " Mbps";
}
function Metric({ label, value, note, Icon, kind }: {
  label: string; value: string; note: string; Icon: typeof Server; kind?: "success" | "danger";
}) {
  return <article className="dash-metric-card">
    <div className="dash-metric-top"><span>{label}</span><span className={"dash-metric-icon " + (kind ?? "")}><Icon size={17}/></span></div>
    <strong className="dash-metric-value" dir="auto">{value}</strong>
    <small className="dash-metric-note">{note}</small>
  </article>;
}
export default function DashboardOverview({ fleet, language, stale }: {
  fleet: FleetSnapshot | null; language: Language; stale: boolean;
}) {
  const t = labels[language];
  const nodes = fleet?.nodes ?? [];
  const [selected, setSelected] = useState("");
  const [showHistory, setShowHistory] = useState(false);
  const selectedNode = nodes.find(node => node.node_id === selected) ?? nodes.find(node => node.status === "online") ?? nodes[0];
  const fresh = nodes.filter(node => node.status === "online" && node.metrics_current);
  const delayed = nodes.filter(node => node.status === "online" && !node.metrics_current);
  const avgCPU = fresh.length > 0
    ? (fresh.reduce((total, node) => total + node.cpu_percent, 0) / fresh.length).toLocaleString("en-US", { maximumFractionDigits: 1 }) + "%"
    : "—";
  const known = fleet !== null;
  // A zero physical rate can be real, but a missing sample / incomplete
  // fleet must remain unknown. Never merge these counters with billing.
  const networkReady = known && fleet.physical_rates_complete && fleet.online_nodes > 0;
  const percentage = known && fleet.total_nodes > 0 ? fleet.online_nodes / fleet.total_nodes * 100 : 0;

  return <section className="dashboard-v2" aria-label={t.title}>
    <div className="dash-intro">
      <div><span className="dash-eyebrow"><Signal size={15}/>{t.title}</span><p>{t.explanation}</p></div>
      <span className={"dash-sync-state " + (!known ? "is-pending" : stale ? "is-stale" : "is-verified")} role="status">
        {stale ? <CircleAlert size={15}/> : known ? <CheckCircle2 size={15}/> : <RadioTower size={15}/>}
        {stale ? t.delayed : known ? t.observed : t.waiting}
      </span>
    </div>

    <div className="dash-metric-grid">
      <Metric Icon={Server} label={t.nodes} value={known ? String(fleet.total_nodes) : "—"} note={t.countNote}/>
      <Metric Icon={Activity} kind="success" label={t.online} value={known ? String(fleet.online_nodes) : "—"} note={t.stateNote}/>
      <Metric Icon={CircleAlert} kind="danger" label={t.offline} value={known ? String(fleet.offline_nodes) : "—"} note={t.stateNote}/>
      <Metric Icon={ArrowDownLeft} label={t.rx} value={networkReady ? rate(fleet.interface_rx_bits_per_second) : "—"} note={t.linkNote}/>
      <Metric Icon={ArrowUpRight} label={t.tx} value={networkReady ? rate(fleet.interface_tx_bits_per_second) : "—"} note={t.linkNote}/>
      <Metric Icon={Cpu} label={t.cpu} value={known ? avgCPU : "—"} note={t.cpuNote}/>
    </div>

    <div className="dash-insight-grid">
      <article className="dash-insight">
        <div className="dash-insight-heading"><div><h2>{t.quality}</h2><p>{t.qualityLead}</p></div><ShieldCheck size={20}/></div>
        <div className="dash-coverage">
          <strong>{known ? fleet.online_nodes + " / " + fleet.total_nodes : "—"}</strong><span>{t.coverage}</span>
        </div>
        <div className="dash-coverage-track" role="progressbar" aria-label={t.coverage} aria-valuenow={known ? percentage : undefined} aria-valuemin={0} aria-valuemax={100}>
          <span style={{ width: percentage + "%" }}/>
        </div>
        <div className="dash-quality-line">
          <span><i className="dot-fresh"/>{t.fresh}<b>{known ? fresh.length : "—"}</b></span>
          <span><i className="dot-delayed"/>{t.outdated}<b>{known ? delayed.length : "—"}</b></span>
        </div>
        <p className="dash-quality-note">{t.missing}</p>
      </article>
      <article className="dash-insight dash-history-choice">
        <div className="dash-insight-heading"><div><h2>{t.historyTitle}</h2><p>{t.historyLead}</p></div><ChartNoAxesCombined size={20}/></div>
        {selectedNode ?
          <div className="dash-history-controls">
            {showHistory && <label>{t.choose}
              <select value={selectedNode.node_id} onChange={event => setSelected(event.target.value)}>
                {nodes.map(node => <option value={node.node_id} key={node.node_id}>{node.name}</option>)}
              </select>
            </label>}
            <button type="button" onClick={() => setShowHistory(v => !v)}>
              {showHistory ? t.hideHistory : t.openHistory}
            </button>
          </div> :
          <p className="dash-no-history">{t.noHistory}</p>}
        <small className="dash-history-scope"><ShieldCheck size={14}/>{t.scope}</small>
      </article>
    </div>
    {showHistory && selectedNode && <NodeHistory key={selectedNode.node_id} nodeID={selectedNode.node_id} language={language}/>}
  </section>;
}
