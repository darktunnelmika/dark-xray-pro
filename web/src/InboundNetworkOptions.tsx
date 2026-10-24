import { useEffect, useRef } from "react";
import { profileOptionsDraft, type ProfileOptionsDraft } from "./inboundProfileOptions";
import { clientFingerprints, xhttpModes } from "./inboundNetworkOptions";
import "./InboundProfileAdvanced.css";

const words = {
  en: {
    xhttp: "XHTTP settings", client: "Client connection defaults", path: "XHTTP Path", host: "XHTTP Host", mode: "XHTTP Mode", padding: "XHTTP Padding",
    pathHint: "Optional absolute path, for example /connect. Blank uses /. Queries and fragments are not supported.",
    hostHint: "Optional DNS name or IPv4, without a port. Blank uses SNI. IPv6 transport Host is unavailable.",
    paddingHint: "Optional bytes: N or N-M, from 1 to 65536. Blank uses the core default (100-1000).", auto: "Default (auto)",
    fingerprint: "Client Fingerprint", spider: "Client SpiderX", defaultFingerprint: "Default (Chrome)",
    clientHint: "Defaults for customer connection settings. Individual Hosts can override them.",
    spiderHint: "Optional /path with a query. Blank uses /. Crawler-control query keys p, c, t, i and r are not supported.",
    reset: "Reset these settings", unavailable: "This server cannot save these settings. Existing values are retained.",
  },
  fa: {
    xhttp: "تنظیمات XHTTP", client: "پیش‌فرض‌های اتصال کلاینت", path: "Path در XHTTP", host: "Host در XHTTP", mode: "Mode در XHTTP", padding: "Padding در XHTTP",
    pathHint: "مسیر اختیاری مثل /connect؛ مقدار خالی از / استفاده می‌کند. Query و Fragment پشتیبانی نمی‌شوند.",
    hostHint: "نام DNS یا IPv4 اختیاری، بدون پورت؛ خالی یعنی SNI. Host انتقال با IPv6 پشتیبانی نمی‌شود.",
    paddingHint: "تعداد بایت اختیاری با قالب N یا N-M بین ۱ تا ۶۵۵۳۶؛ مقدار خالی پیش‌فرض هسته (100-1000) است.", auto: "پیش‌فرض (auto)",
    fingerprint: "Fingerprint کلاینت", spider: "SpiderX کلاینت", defaultFingerprint: "پیش‌فرض (Chrome)",
    clientHint: "پیش‌فرض تنظیمات اتصال مشتری؛ هر Host می‌تواند مقدار مخصوص خودش را داشته باشد.",
    spiderHint: "مسیر اختیاری /path با Query؛ خالی یعنی /. کلیدهای کنترل خزندهٔ p، c، t، i و r پشتیبانی نمی‌شوند.",
    reset: "بازنشانی این تنظیمات", unavailable: "سرور این تنظیمات را ذخیره نمی‌کند؛ مقادیر قبلی حفظ می‌شوند.",
  },
};

export default function InboundNetworkOptions({ language, section, value, onChange, supported, disabled, invalidFields = [] }: {
  language: "fa" | "en"; section: "xhttp" | "client"; value?: ProfileOptionsDraft;
  onChange: (value: ProfileOptionsDraft) => void; supported: boolean; disabled: boolean; invalidFields?: string[];
}) {
  const t = words[language], current = value ?? profileOptionsDraft(), disclosure = useRef<HTMLDetailsElement>(null);
  const fields = section === "xhttp" ? ["xhttpPath", "xhttpHost", "xhttpMode", "xhttpPadding"] : ["clientFingerprint", "clientSpiderX"];
  const invalid = invalidFields.some(field => fields.includes(field));
  useEffect(() => { if (invalid && disclosure.current) disclosure.current.open = true; }, [invalid]);
  function change<K extends keyof ProfileOptionsDraft>(field: K, next: ProfileOptionsDraft[K]) { onChange({ ...current, [section === "xhttp" ? "xhttpPresent" : "clientPresent"]: true, [field]: next }); }
  const inactive = disabled || !supported;
  const content = <>
    {!supported && <p className="inbound-option-hint" role="status">{t.unavailable}</p>}
    {section === "client" && <p className="inbound-option-hint">{t.clientHint}</p>}
    <div className="inbound-options-grid">
      {section === "xhttp" ? <>
        <label className="inbound-option-field"><span>{t.path}</span><input data-creation-field="xhttpPath" aria-label={t.path} aria-invalid={invalidFields.includes("xhttpPath")} dir="ltr" disabled={inactive} value={current.xhttpPath} onChange={event => change("xhttpPath", event.target.value)} placeholder="/" spellCheck={false} autoComplete="off"/><small>{t.pathHint}</small></label>
        <label className="inbound-option-field"><span>{t.host}</span><input data-creation-field="xhttpHost" aria-label={t.host} aria-invalid={invalidFields.includes("xhttpHost")} dir="ltr" disabled={inactive} value={current.xhttpHost} onChange={event => change("xhttpHost", event.target.value)} maxLength={253} spellCheck={false} autoComplete="off"/><small>{t.hostHint}</small></label>
        <label className="inbound-option-field"><span>{t.mode}</span><select data-creation-field="xhttpMode" aria-label={t.mode} aria-invalid={invalidFields.includes("xhttpMode")} dir="ltr" disabled={inactive} value={current.xhttpMode} onChange={event => change("xhttpMode", event.target.value)}><option value="">{t.auto}</option>{xhttpModes.map(mode => <option key={mode} value={mode}>{mode}</option>)}</select></label>
        <label className="inbound-option-field"><span>{t.padding}</span><input data-creation-field="xhttpPadding" aria-label={t.padding} aria-invalid={invalidFields.includes("xhttpPadding")} dir="ltr" disabled={inactive} value={current.xhttpPadding} onChange={event => change("xhttpPadding", event.target.value)} placeholder="100-1000" autoComplete="off"/><small>{t.paddingHint}</small></label>
      </> : <>
        <label className="inbound-option-field"><span>{t.fingerprint}</span><select data-creation-field="clientFingerprint" aria-label={t.fingerprint} aria-invalid={invalidFields.includes("clientFingerprint")} dir="ltr" disabled={inactive} value={current.clientFingerprint} onChange={event => change("clientFingerprint", event.target.value)}><option value="">{t.defaultFingerprint}</option>{clientFingerprints.map(fingerprint => <option key={fingerprint} value={fingerprint}>{fingerprint}</option>)}</select></label>
        <label className="inbound-option-field"><span>{t.spider}</span><input data-creation-field="clientSpiderX" aria-label={t.spider} aria-invalid={invalidFields.includes("clientSpiderX")} dir="ltr" disabled={inactive} value={current.clientSpiderX} onChange={event => change("clientSpiderX", event.target.value)} placeholder="/" spellCheck={false} autoComplete="off"/><small>{t.spiderHint}</small></label>
      </>}
    </div>
    <div className="inbound-network-actions"><button type="button" disabled={inactive} onClick={() => onChange(section === "xhttp" ? { ...current, xhttpPresent: true, xhttpPath: "", xhttpHost: "", xhttpMode: "", xhttpPadding: "" } : { ...current, clientPresent: true, clientFingerprint: "", clientSpiderX: "" })}>{t.reset}</button></div>
  </>;
  return section === "client" ? <details ref={disclosure} className="inbound-profile-advanced" data-client-options><summary data-creation-field="clientOptions">{t.client}</summary>{content}</details> : <section className="inbound-network-options" aria-label={t.xhttp}>{content}</section>;
}
