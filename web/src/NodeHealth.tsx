import type { Language } from "./session";

const words = {
  en: { title: "Node health", transport: "Agent connection", metrics: "Host metrics",
    core: "Proxy engine", accounting: "Usage accounting", online: "Online",
    offline: "Offline", current: "Current", delayed: "Delayed", unknown: "Unknown",
    unobserved: "Not measured here", note: "Agent connectivity and host metrics do not confirm proxy or accounting health." },
  fa: { title: "سلامت نود", transport: "ارتباط Agent", metrics: "آمار سرور",
    core: "موتور پروکسی", accounting: "حسابداری مصرف", online: "آنلاین",
    offline: "آفلاین", current: "به‌روز", delayed: "با تأخیر", unknown: "نامعلوم",
    unobserved: "اینجا سنجیده نشده", note: "ارتباط Agent و آمار سرور، سلامت پروکسی یا حسابداری را تأیید نمی‌کنند." },
} as const;

export default function NodeHealth({ language, status, metricsCurrent, fresh }: {
  language: Language; status: string; metricsCurrent: boolean; fresh: boolean;
}) {
  const t = words[language];
  const connected = fresh && status === "online";
  const transport = !fresh ? t.unknown : status === "online" ? t.online : status === "offline" ? t.offline : t.unknown;
  return <section className="pro-node-health" aria-label={t.title}>
    <dl>
      <div data-node-health="transport"><dt>{t.transport}</dt><dd>{transport}</dd></div>
      <div data-node-health="metrics"><dt>{t.metrics}</dt><dd>{!connected ? t.unknown : metricsCurrent === true ? t.current : t.delayed}</dd></div>
      <div data-node-health="core"><dt>{t.core}</dt><dd>{t.unobserved}</dd></div>
      <div data-node-health="accounting"><dt>{t.accounting}</dt><dd>{t.unobserved}</dd></div>
    </dl>
    <small>{t.note}</small>
  </section>;
}
