import type { Language } from "./session";

const words = {
  en: { title: "Node health", transport: "Agent connection", metrics: "Host metrics",
    core: "Proxy engine", accounting: "Usage accounting", online: "Online",
    offline: "Offline", current: "Current", delayed: "Delayed", unknown: "Unknown",
    unobserved: "Not measured here", stale: "Stale observation",
    note: "Each channel uses only its own evidence; a stale value is not current health." },
  fa: { title: "سلامت نود", transport: "ارتباط Agent", metrics: "آمار سرور",
    core: "موتور پروکسی", accounting: "حسابداری مصرف", online: "آنلاین",
    offline: "آفلاین", current: "به‌روز", delayed: "با تأخیر", unknown: "نامعلوم",
    unobserved: "اینجا سنجیده نشده", stale: "گزارش قدیمی",
    note: "هر کانال فقط از مدرک مستقل خودش استفاده می‌کند؛ مقدار قدیمی، سلامت فعلی نیست." },
} as const;

function readableState(value: string | undefined, fallback: string): string {
  const clean = value?.trim();
  return clean ? clean.replaceAll("_", " ") : fallback;
}

export default function NodeHealth({ language, status, metricsCurrent, fresh,
  transportState, metricsState, coreState, accountingState,
  executionCurrent, executionReportedAt }: {
  language: Language;
  status: string;
  metricsCurrent: boolean;
  fresh: boolean;
  transportState?: string;
  metricsState?: string;
  coreState?: string;
  accountingState?: string;
  executionCurrent?: boolean;
  executionReportedAt?: string | null;
}) {
  const t = words[language];
  const connected = fresh && status === "online";
  const transportEvidence = transportState ?? status;
  const transport = !fresh ? t.unknown :
    transportEvidence === "online" ? t.online : transportEvidence === "offline" ? t.offline : t.unknown;
  const metrics = !connected ? t.unknown :
    metricsState === "current" ? t.current : metricsState === "delayed" ? t.delayed :
      metricsCurrent === true ? t.current : t.delayed;
  const hasExecutionEvidence = typeof executionReportedAt === "string" && executionReportedAt.length > 0;
  const staleExecution = hasExecutionEvidence && (!fresh || executionCurrent !== true);
  const executionValue = (value: string | undefined) => hasExecutionEvidence
    ? readableState(value, t.unknown) + (staleExecution ? ` · ${t.stale}` : "")
    : t.unobserved;

  return <section className="pro-node-health" aria-label={t.title}>
    <dl>
      <div data-node-health="transport"><dt>{t.transport}</dt><dd>{transport}</dd></div>
      <div data-node-health="metrics"><dt>{t.metrics}</dt><dd>{metrics}</dd></div>
      <div data-node-health="core"><dt>{t.core}</dt><dd>{executionValue(coreState)}</dd></div>
      <div data-node-health="accounting"><dt>{t.accounting}</dt><dd>{executionValue(accountingState)}</dd></div>
    </dl>
    <small>{t.note}</small>
  </section>;
}
