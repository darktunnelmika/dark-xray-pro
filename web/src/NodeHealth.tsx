import type { Language } from "./session";

export type NodeHealthEvidence = {
  status: string;
  metrics_current: boolean;
  transport_state?: string;
  metrics_state?: string;
  core_state?: string;
  accounting_state?: string;
  execution_current?: boolean;
  execution_reported_at?: string | null;
  execution_fresh_for_ms?: number;
};

const words = {
  en: { title: "Node health", transport: "Agent connection", metrics: "Host metrics",
    core: "Proxy engine", accounting: "Usage accounting", online: "Online",
    offline: "Offline", current: "Current", delayed: "Delayed", unknown: "Unknown",
    unobserved: "Not measured here", stale: "Stale", notConfigured: "Not configured",
    healthy: "Health verified", engineReady: "Engine ready", stopped: "Stopped",
    accepted: "Accepted", pending: "Pending receipt", settled: "Settled",
    uncertain: "Needs reconciliation",
    note: "Each status uses independent evidence. Agent connectivity does not confirm service delivery or accounting health." },
  fa: { title: "سلامت نود", transport: "ارتباط Agent", metrics: "آمار سرور",
    core: "موتور پروکسی", accounting: "حسابداری مصرف", online: "آنلاین",
    offline: "آفلاین", current: "به‌روز", delayed: "با تأخیر", unknown: "نامعلوم",
    unobserved: "اینجا سنجیده نشده", stale: "گزارش قدیمی", notConfigured: "تنظیم نشده",
    healthy: "سلامت تأییدشده", engineReady: "موتور آماده", stopped: "متوقف",
    accepted: "پذیرفته‌شده", pending: "در انتظار رسید", settled: "تسویه‌شده",
    uncertain: "نیازمند تطبیق",
    note: "هر وضعیت مدرک مستقل دارد. ارتباط Agent، تحویل سرویس یا سلامت حسابداری را تأیید نمی‌کند." },
} as const;

export default function NodeHealth({ language, node, fresh, elapsedMs }: {
  language: Language; node: NodeHealthEvidence; fresh: boolean; elapsedMs: number;
}) {
  const t = words[language];
  const transportKnown = node.transport_state === undefined ||
    (node.status === "online" && node.transport_state === "connected") ||
    (node.status === "offline" && node.transport_state === "offline");
  const connected = fresh && transportKnown && node.status === "online";
  const transport = !fresh || !transportKnown ? t.unknown :
    node.status === "online" ? t.online : node.status === "offline" ? t.offline : t.unknown;
  const metricsKnown = node.metrics_state === undefined ||
    (node.metrics_current === true && node.metrics_state === "current") ||
    (node.metrics_current === false && node.metrics_state === "stale");
  const validReport = typeof node.execution_reported_at === "string" &&
    Number.isFinite(Date.parse(node.execution_reported_at));
  // The HUB supplies a bounded remaining lifetime. Count from request start,
  // conservatively including transport delay, without trusting the browser's
  // wall clock or renewing evidence merely because the roster was fetched again.
  const lifetime = node.execution_fresh_for_ms;
  const currentExecution = fresh && node.execution_current === true && validReport &&
    typeof lifetime === "number" && Number.isInteger(lifetime) && lifetime > 0 &&
    lifetime <= 15_000 && Number.isFinite(elapsedMs) && elapsedMs >= 0 && elapsedMs < lifetime;
  const coreLabels: Record<string, string> = {
    healthy: t.healthy, engine_ready: t.engineReady, stopped: t.stopped, unknown: t.unknown,
  };
  const accountingLabels: Record<string, string> = {
    accepted: t.accepted, pending: t.pending, settled: t.settled,
    uncertain: t.uncertain, stale: t.stale, unknown: t.unknown,
  };
  function observedLabel(state: string | undefined, labels: Record<string, string>): string {
    if (!fresh) return t.unknown;
    if (state === undefined) return t.unobserved;
    if (state === "not_configured" && !node.execution_reported_at) return t.notConfigured;
    if (!validReport) return t.unknown;
    if (!currentExecution) return t.stale;
    return labels[state] ?? t.unknown;
  }
  return <section className="pro-node-health" aria-label={t.title}>
    <dl>
      <div data-node-health="transport"><dt>{t.transport}</dt><dd>{transport}</dd></div>
      <div data-node-health="metrics"><dt>{t.metrics}</dt><dd>{!connected || !metricsKnown ? t.unknown : node.metrics_current === true ? t.current : t.delayed}</dd></div>
      <div data-node-health="core"><dt>{t.core}</dt><dd>{observedLabel(node.core_state, coreLabels)}</dd></div>
      <div data-node-health="accounting"><dt>{t.accounting}</dt><dd>{observedLabel(node.accounting_state, accountingLabels)}</dd></div>
    </dl>
    <small>{t.note}</small>
  </section>;
}
