import { useEffect, useRef, useState } from "react";
import { Plus, RotateCcw, Trash2 } from "lucide-react";
import { profileOptionsDraft, type ProfileOptionsDraft } from "./inboundProfileOptions";
import { maxFallbacks, sniffingProtocols, trafficLines, type FallbackDraft, type SniffingDraft } from "./inboundTrafficOptions";
import "./InboundTrafficOptions.css";

const words = {
  en: {
    sniffing: "Sniffing", enabled: "Enable Sniffing", hint: "Detect a connection’s protocol and domain from its traffic.", settings: "Sniffing settings",
    overrides: "Destination overrides", routeOnly: "Use the detected domain for routing only", routeHint: "The original destination is retained except for FakeDNS resolution.", metadataOnly: "Read metadata only",
    metadataHint: "Skips traffic payload inspection; FakeDNS metadata can still be used.", fakeDNSHint: "FakeDNS requires separate DNS setup. Selecting it here does not enable a FakeDNS service.", exclusions: "Exclusions", domains: "Excluded domains", ips: "Excluded IPs or CIDRs",
    domainHint: "One rule per line: plain text, full:, domain:, keyword: or regexp:. The server checks regular expressions using RE2.",
    ipHint: "One literal IP or CIDR per line. External GeoIP files are not supported.", limits: "Up to 256 rules in each list; 512 bytes per rule.",
    reset: "Reset Sniffing", retained: "Sniffing is off. Its configured settings are retained.", unavailable: "This server does not support these settings in saved setup plans.",
    fallbacks: "TCP fallbacks", fallbackHint: "Configure VLESS/TCP fallback destinations in this paused setup. These settings cannot be activated or published yet.",
    add: "Add fallback", row: "Fallback", remove: "Remove fallback", destination: "Destination", destinationHint: "Port 1–65535 or host:port, for example 8080 or 127.0.0.1:8080.",
    path: "Path", pathHint: "Optional /path; up to 55 bytes, without spaces, ? or #.", alpnUnavailable: "ALPN matching is unavailable for REALITY fallbacks.", clearALPN: "Clear unsupported ALPN",
    xver: "PROXY protocol", none: "Disabled (0)", noRows: "No fallbacks configured.", tcp: "Fallbacks require TCP. Switch to TCP in Network or remove every fallback. Your rows are retained.",
    limit: "Maximum 32 fallbacks. Each Path must be unique; only one default fallback may have an empty Path.",
  },
  fa: {
    sniffing: "Sniffing", enabled: "فعال‌کردن Sniffing", hint: "تشخیص پروتکل و دامنهٔ اتصال از ترافیک آن.", settings: "تنظیمات Sniffing",
    overrides: "پروتکل‌های تشخیص مقصد", routeOnly: "استفاده از دامنهٔ تشخیص‌داده‌شده فقط برای مسیریابی", routeHint: "مقصد اصلی به‌جز موارد مربوط به FakeDNS حفظ می‌شود.", metadataOnly: "فقط خواندن فراداده",
    metadataHint: "محتوای ترافیک بررسی نمی‌شود؛ فرادادهٔ FakeDNS همچنان قابل استفاده است.", fakeDNSHint: "FakeDNS به راه‌اندازی جداگانهٔ DNS نیاز دارد؛ انتخاب این گزینه سرویس FakeDNS را فعال نمی‌کند.", exclusions: "استثناها", domains: "دامنه‌های مستثنا", ips: "IPها یا CIDRهای مستثنا",
    domainHint: "هر قانون در یک خط: متن ساده، full:، domain:، keyword: یا regexp:. عبارت‌های منظم با RE2 روی سرور بررسی می‌شوند.",
    ipHint: "هر IP یا CIDR در یک خط؛ فایل‌های خارجی GeoIP پشتیبانی نمی‌شوند.", limits: "هر فهرست حداکثر ۲۵۶ قانون؛ هر قانون حداکثر ۵۱۲ بایت.",
    reset: "پاک‌کردن تنظیمات Sniffing", retained: "Sniffing خاموش است؛ تنظیمات واردشده حفظ می‌شوند.", unavailable: "این سرور این تنظیمات را در طرح ذخیره‌شده پشتیبانی نمی‌کند.",
    fallbacks: "Fallbackهای TCP", fallbackHint: "مقصدهای Fallback در VLESS/TCP را در این طرح خاموش تنظیم کن. فعال‌سازی و انتشار این تنظیمات هنوز ممکن نیست.",
    add: "افزودن Fallback", row: "Fallback", remove: "حذف Fallback", destination: "مقصد", destinationHint: "پورت ۱ تا ۶۵۵۳۵ یا host:port؛ مثلاً 8080 یا 127.0.0.1:8080.",
    path: "مسیر", pathHint: "اختیاری با شروع /؛ حداکثر ۵۵ بایت و بدون فاصله، ? یا #.", alpnUnavailable: "تطبیق ALPN برای Fallback در REALITY در دسترس نیست.", clearALPN: "پاک‌کردن ALPN پشتیبانی‌نشده",
    xver: "پروتکل PROXY", none: "غیرفعال (۰)", noRows: "هیچ Fallbackی تنظیم نشده.", tcp: "Fallback به TCP نیاز دارد. در تب شبکه TCP را انتخاب کن یا همهٔ Fallbackها را حذف کن. ردیف‌ها حفظ شده‌اند.",
    limit: "حداکثر ۳۲ Fallback؛ مسیر هر ردیف باید یکتا باشد. فقط یک Fallback پیش‌فرض می‌تواند مسیر خالی داشته باشد.",
  },
};

export default function InboundTrafficOptions({ language, value, transport, onChange, sniffingSupported, fallbacksSupported, disabled, invalidFields = [] }: {
  language: "en" | "fa"; value?: ProfileOptionsDraft; transport: string; onChange: (value: ProfileOptionsDraft) => void;
  sniffingSupported: boolean; fallbacksSupported: boolean; disabled: boolean; invalidFields?: string[];
}) {
  const t = words[language], current = value ?? profileOptionsDraft();
  const surface = useRef<HTMLDivElement>(null), sniffingSettings = useRef<HTMLDetailsElement>(null), addButton = useRef<HTMLButtonElement>(null), fallbackHeading = useRef<HTMLHeadingElement>(null);
  const [focusKey, setFocusKey] = useState<string | null>(null);
  useEffect(() => {
    if (current.sniffing.enabled && sniffingSettings.current) sniffingSettings.current.open = true;
  }, [current.sniffing.enabled]);
  useEffect(() => {
    if (!focusKey) return;
    if (focusKey === "add") { if (addButton.current?.disabled) fallbackHeading.current?.focus(); else addButton.current?.focus(); }
    else {
      const row = surface.current?.querySelector<HTMLDetailsElement>(`[data-fallback-key="${CSS.escape(focusKey)}"]`);
      if (row) { row.open = true; const input = row.querySelector<HTMLInputElement>("input"); input?.focus({ preventScroll: true }); input?.scrollIntoView({ block: "nearest" }); }
    }
    setFocusKey(null);
  }, [focusKey]);
  function sniffing<K extends keyof SniffingDraft>(key: K, next: SniffingDraft[K]) {
    onChange({ ...current, sniffingPresent: true, sniffing: { ...current.sniffing, [key]: next } });
  }
  function resetSniffing() {
    onChange({ ...current, sniffingPresent: true, sniffing: { enabled: false, destOverride: [], metadataOnly: false, routeOnly: false, domainsExcluded: "", ipsExcluded: "" } });
  }
  function fallback(index: number, key: keyof Omit<FallbackDraft, "key">, next: string) {
    onChange({ ...current, fallbacksPresent: true, fallbacks: current.fallbacks.map((row, i) => i === index ? { ...row, [key]: next } : row) });
  }
  function addFallback() {
    const key = crypto.randomUUID();
    onChange({ ...current, fallbacksPresent: true, fallbacks: [...current.fallbacks, { key, path: "", alpn: "", dest: "", xver: "0" }] });
    setFocusKey(key);
  }
  function removeFallback(index: number) {
    const rows = current.fallbacks.filter((_, i) => i !== index);
    onChange({ ...current, fallbacksPresent: true, fallbacks: rows });
    setFocusKey(rows[Math.min(index, rows.length - 1)]?.key ?? "add");
  }
  const sniffingInactive = disabled || !sniffingSupported, fallbackInactive = disabled || !fallbacksSupported;
  const configured = current.sniffingPresent && (current.sniffing.destOverride.length > 0 || current.sniffing.metadataOnly || current.sniffing.routeOnly || current.sniffing.domainsExcluded !== "" || current.sniffing.ipsExcluded !== "");
  return <div ref={surface} className="inbound-traffic-options">
    <section className="inbound-traffic-card" aria-label={t.sniffing}>
      <header><div><h4>{t.sniffing}</h4><p>{t.hint}</p></div></header>
      {!sniffingSupported && <p className="inbound-traffic-notice" role="status">{t.unavailable}</p>}
      <label className="inbound-traffic-check inbound-traffic-enable"><input type="checkbox" data-creation-field="sniffing.enabled" checked={current.sniffing.enabled} disabled={sniffingInactive} onChange={event => sniffing("enabled", event.target.checked)}/><span>{t.enabled}</span></label>
      {!current.sniffing.enabled && configured && <p className="inbound-traffic-note">{t.retained}</p>}
      <details ref={sniffingSettings} className="inbound-traffic-disclosure">
        <summary>{t.settings}</summary>
        <div className="inbound-traffic-settings">
          <fieldset className="inbound-traffic-protocols"><legend>{t.overrides}</legend><div>{sniffingProtocols.map(protocol => <label key={protocol} className="inbound-traffic-check"><input type="checkbox" data-creation-field={protocol === "http" ? "sniffing.destOverride" : undefined} disabled={sniffingInactive} checked={current.sniffing.destOverride.includes(protocol)} onChange={event => sniffing("destOverride", event.target.checked ? [...current.sniffing.destOverride, protocol] : current.sniffing.destOverride.filter(item => item !== protocol))}/><span dir="ltr">{protocol === "fakedns" ? "FakeDNS" : protocol.toUpperCase()}</span></label>)}</div></fieldset>
          {current.sniffing.destOverride.includes("fakedns") && <p className="inbound-traffic-note">{t.fakeDNSHint}</p>}
          <div><label className="inbound-traffic-check"><input type="checkbox" disabled={sniffingInactive} checked={current.sniffing.routeOnly} onChange={event => sniffing("routeOnly", event.target.checked)}/><span>{t.routeOnly}</span></label><p className="inbound-traffic-note">{t.routeHint}</p></div>
          <div><label className="inbound-traffic-check"><input type="checkbox" disabled={sniffingInactive} checked={current.sniffing.metadataOnly} onChange={event => sniffing("metadataOnly", event.target.checked)}/><span>{t.metadataOnly}</span></label><p className="inbound-traffic-note">{t.metadataHint}</p></div>
          <details className="inbound-traffic-disclosure inbound-traffic-exclusions"><summary>{t.exclusions}<bdi>{trafficLines(current.sniffing.domainsExcluded).length + trafficLines(current.sniffing.ipsExcluded).length}</bdi></summary><div className="inbound-traffic-settings">
            <label className="inbound-traffic-field"><span>{t.domains}</span><textarea rows={3} dir="ltr" aria-label={t.domains} data-creation-field="sniffing.domainsExcluded" aria-invalid={invalidFields.includes("sniffing.domainsExcluded")} disabled={sniffingInactive} value={current.sniffing.domainsExcluded} onChange={event => sniffing("domainsExcluded", event.target.value)} autoComplete="off" spellCheck={false}/><small>{t.domainHint}</small></label>
            <label className="inbound-traffic-field"><span>{t.ips}</span><textarea rows={3} dir="ltr" aria-label={t.ips} data-creation-field="sniffing.ipsExcluded" aria-invalid={invalidFields.includes("sniffing.ipsExcluded")} disabled={sniffingInactive} value={current.sniffing.ipsExcluded} onChange={event => sniffing("ipsExcluded", event.target.value)} autoComplete="off" spellCheck={false}/><small>{t.ipHint}</small></label>
            <p className="inbound-traffic-note">{t.limits}</p>
          </div></details>
          <button type="button" className="inbound-traffic-reset" disabled={sniffingInactive} onClick={resetSniffing}><RotateCcw size={15}/>{t.reset}</button>
        </div>
      </details>
    </section>
    <section className="inbound-traffic-card" aria-label={t.fallbacks}>
      <header><div><h4 ref={fallbackHeading} tabIndex={-1}>{t.fallbacks}<bdi>{current.fallbacks.length}</bdi></h4><p>{t.fallbackHint}</p></div></header>
      <p className="inbound-traffic-note">{t.alpnUnavailable}</p>
      {!fallbacksSupported && <p className="inbound-traffic-notice" role="status">{t.unavailable}</p>}
      {transport !== "tcp" && current.fallbacks.length > 0 && <p className="inbound-traffic-notice" role="alert">{t.tcp}</p>}
      {!current.fallbacks.length && <p className="inbound-traffic-note">{t.noRows}</p>}
      <div className="inbound-fallback-list">{current.fallbacks.map((row, index) => <details key={row.key} data-fallback-key={row.key} className="inbound-traffic-disclosure inbound-fallback-row">
        <summary><span className="inbound-fallback-label" dir="ltr">{t.row} <bdi>{index + 1}</bdi></span><bdi className="inbound-fallback-destination" dir="ltr">{row.dest || "—"}</bdi></summary>
        <div className="inbound-fallback-body">
          <div className="inbound-fallback-fields">
            {(["dest", "path"] as const).map(field => <label key={field} className={"inbound-traffic-field" + (field === "dest" ? " inbound-fallback-wide" : "")}><span>{field === "dest" ? t.destination : t.path}</span><input aria-label={`${t.row} ${index + 1} ${field === "dest" ? t.destination : t.path}`} data-creation-field={`fallbacks.${index}.${field}`} aria-invalid={invalidFields.includes(`fallbacks.${index}.${field}`)} dir="ltr" disabled={fallbackInactive} value={row[field]} onChange={event => fallback(index, field, event.target.value)} autoComplete="off" spellCheck={false}/><small>{field === "dest" ? t.destinationHint : t.pathHint}</small></label>)}
            <label className="inbound-traffic-field"><span>{t.xver}</span><select aria-label={`${t.row} ${index + 1} ${t.xver}`} data-creation-field={`fallbacks.${index}.xver`} dir="ltr" disabled={fallbackInactive} value={row.xver} onChange={event => fallback(index, "xver", event.target.value)}><option value="0">{t.none}</option><option value="1">PROXY v1 (1)</option><option value="2">PROXY v2 (2)</option></select></label>
          </div>
          {row.alpn && <div className="inbound-traffic-notice"><p>{t.alpnUnavailable} <bdi dir="ltr">{row.alpn}</bdi></p><button type="button" data-creation-field={`fallbacks.${index}.alpn`} disabled={fallbackInactive} onClick={() => { fallback(index, "alpn", ""); setFocusKey(row.key); }}>{t.clearALPN}</button></div>}
          <button type="button" className="inbound-fallback-remove" aria-label={`${t.remove} ${index + 1}`} disabled={fallbackInactive} onClick={() => removeFallback(index)}><Trash2 size={15}/>{t.remove}</button>
        </div>
      </details>)}</div>
      <button ref={addButton} type="button" data-creation-field="fallbacks" className="inbound-fallback-add" disabled={fallbackInactive || transport !== "tcp" || current.fallbacks.length >= maxFallbacks} onClick={addFallback}><Plus size={16}/>{t.add}</button>
      <p className="inbound-traffic-note">{t.limit}</p>
    </section>
  </div>;
}
