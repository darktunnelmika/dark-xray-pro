import { Plus, SlidersHorizontal, Trash2 } from "lucide-react";
import type { CreationPath } from "./InboundCreationModel";
import { creationHosts } from "./InboundCreationModel";
import { endpointAddresses, endpointDraft, type EndpointDraft, type EndpointProfileDefaults } from "./inboundEndpointOptions";
import { clientFingerprints } from "./inboundNetworkOptions";

const words = {
  en: {
    direct: "Direct", tunnel: "Tunnel", hosts: "Hosts", settings: "Endpoint settings", simple: "Use simple Hosts", add: "Add Host", remove: "Remove Host",
    address: "Address", port: "Public port", label: "Display name", enabled: "Host enabled", sni: "SNI", fingerprint: "Fingerprint", spider: "SpiderX",
    inherit: "Inherit", details: "Host settings", empty: "No Hosts configured.",
    portHint: "Blank or 0 inherits the listener port. A different public port requires your existing forwarding rule.",
    spiderHint: "Blank inherits the client default. Use /path and optional query; p, c, t, i and r crawler query keys are unsupported.",
    disabled: "Disabled · saved for later", unavailable: "This server cannot save endpoint settings. Existing rows are retained.",
    discard: "Use simple Hosts? Enabled addresses are kept. Per-Host ports, names, overrides and disabled Hosts will be removed.",
  },
  fa: {
    direct: "مستقیم", tunnel: "تونل", hosts: "هاست‌ها", settings: "تنظیمات نقطهٔ اتصال", simple: "استفاده از هاست‌های ساده", add: "افزودن Host", remove: "حذف Host",
    address: "آدرس", port: "پورت عمومی", label: "نام نمایشی", enabled: "Host فعال", sni: "SNI", fingerprint: "Fingerprint", spider: "SpiderX",
    inherit: "ارث‌بری", details: "تنظیمات Host", empty: "هیچ هاستی تنظیم نشده.",
    portHint: "خالی یا ۰ یعنی پورت شنود؛ پورت عمومی متفاوت به قانون انتقالی نیاز دارد که خودت تنظیم کرده‌ای.",
    spiderHint: "خالی یعنی پیش‌فرض کلاینت. مسیر /path و Query اختیاری مجاز است؛ کلیدهای خزندهٔ p، c، t، i و r پشتیبانی نمی‌شوند.",
    disabled: "خاموش · برای بعد ذخیره می‌شود", unavailable: "این سرور تنظیمات نقطهٔ اتصال را ذخیره نمی‌کند؛ ردیف‌های قبلی حفظ می‌شوند.",
    discard: "به هاست‌های ساده برگردی؟ آدرس‌های فعال حفظ می‌شوند؛ پورت، نام و تنظیمات اختصاصی هر Host و هاست‌های خاموش حذف می‌شوند.",
  },
};

export default function InboundCreationEndpoints({ language, path, nodeID, value, defaults, supported, disabled, maximum = 64, invalidFields = [], onChange }: {
  language: "en" | "fa"; path: "direct" | "tunnel"; nodeID: string; value: CreationPath; defaults: EndpointProfileDefaults;
  supported: boolean; disabled: boolean; maximum?: number; invalidFields?: string[]; onChange: (value: CreationPath) => void;
}) {
  const t = words[language], prefix = path + "Endpoints", inactive = disabled || !supported;
  const pathLabel = t[path], rows = value.endpoints;
  const hostsLabel = language === "fa" ? path === "direct" ? "هاست‌های مستقیم" : "هاست‌های تونل" : `${pathLabel} ${t.hosts}`;
  function update(index: number, field: keyof EndpointDraft, next: string | boolean) {
    onChange({ ...value, endpointsPresent: true, endpoints: rows?.map((row, position) => position === index ? { ...row, [field]: next } : row) });
  }
  function configure() {
    const hosts = creationHosts(value.hosts);
    onChange({ ...value, endpointsPresent: true, endpoints: hosts.length ? hosts.map(address => endpointDraft({ address, port: 0, enabled: true })) : [endpointDraft()] });
  }
  function simple() {
    const lossy = rows?.some(row => !row.enabled || row.port !== "" && row.port !== "0" || row.label || row.sni || row.fingerprint || row.spiderX);
    if (lossy && !window.confirm(t.discard)) return;
    onChange({ ...value, hosts: endpointAddresses(rows ?? []).join("\n"), endpoints: undefined, endpointsPresent: true });
  }
  if (rows === undefined) return <div className="creation-host-editor">
    <label className="creation-field"><span>{hostsLabel}<small>{creationHosts(value.hosts).length}</small></span><textarea aria-label={`${hostsLabel}: ${nodeID}`} data-creation-field={`${path}Hosts`} aria-invalid={invalidFields.includes(`${path}Hosts`)} dir="ltr" rows={2} spellCheck={false} value={value.hosts} disabled={disabled} onChange={event => onChange({ ...value, hosts: event.target.value })} placeholder="example.org"/></label>
    {supported && <button type="button" className="creation-endpoint-configure" data-creation-field={prefix} disabled={disabled} aria-label={`${t.settings}: ${pathLabel}: ${nodeID}`} onClick={configure}><SlidersHorizontal size={14}/>{t.settings}</button>}
  </div>;
  return <div className="creation-host-editor" data-endpoint-editor={path}>
    <header className="creation-endpoint-toolbar"><span>{pathLabel} {t.hosts} <bdi>{rows.length}/{maximum}</bdi></span><button type="button" disabled={inactive} onClick={simple}>{t.simple}</button></header>
    {!supported && <p className="creation-help" role="status">{t.unavailable}</p>}
    {!rows.length && <p className="creation-help">{t.empty}</p>}
    <div className="creation-endpoint-list">{rows.map((row, index) => {
      const field = (name: string) => `${prefix}.${index}.${name}`;
      const label = (name: string) => `${name}: ${pathLabel} ${index + 1}: ${nodeID}`;
      const overrideError = ["label", "sni", "fingerprint", "spiderX"].some(name => invalidFields.includes(field(name)));
      return <article className="creation-endpoint" key={row.key} data-endpoint-index={index} data-endpoint-enabled={row.enabled}>
        <header><strong>{row.label || `Host ${index + 1}`}</strong><label className="creation-endpoint-toggle"><input type="checkbox" aria-label={label(t.enabled)} data-creation-field={field("enabled")} disabled={inactive} checked={row.enabled} onChange={event => update(index, "enabled", event.target.checked)}/><span>{row.enabled ? t.enabled : t.disabled}</span></label><button type="button" className="creation-endpoint-remove" disabled={inactive} aria-label={label(t.remove)} onClick={() => onChange({ ...value, endpointsPresent: true, endpoints: rows.filter((_, position) => position !== index) })}><Trash2 size={15}/></button></header>
        <div className="creation-field-grid">
          <label className="creation-field"><span>{t.address}</span><input aria-label={label(t.address)} data-creation-field={field("address")} aria-invalid={invalidFields.includes(field("address"))} dir="ltr" disabled={inactive} value={row.address} onChange={event => update(index, "address", event.target.value)} placeholder="example.org" spellCheck={false} autoComplete="off"/></label>
          <label className="creation-field"><span>{t.port}</span><input aria-label={label(t.port)} data-creation-field={field("port")} aria-invalid={invalidFields.includes(field("port"))} dir="ltr" inputMode="numeric" disabled={inactive} value={row.port} onChange={event => update(index, "port", event.target.value)} placeholder={value.port || "0"} autoComplete="off"/><small>{t.portHint}</small></label>
        </div>
        <details className="creation-endpoint-details" open={overrideError || undefined}><summary>{t.details}</summary><div className="creation-field-grid">
          <label className="creation-field"><span>{t.label}</span><input aria-label={label(t.label)} data-creation-field={field("label")} aria-invalid={invalidFields.includes(field("label"))} disabled={inactive} value={row.label} onChange={event => update(index, "label", event.target.value)} autoComplete="off"/></label>
          <label className="creation-field"><span>{t.sni}</span><select aria-label={label(t.sni)} data-creation-field={field("sni")} aria-invalid={invalidFields.includes(field("sni"))} dir="ltr" disabled={inactive} value={row.sni} onChange={event => update(index, "sni", event.target.value)}><option value="">{t.inherit}{defaults.serverNames[0] ? ` (${defaults.serverNames[0]})` : ""}</option>{row.sni && !defaults.serverNames.includes(row.sni) && <option value={row.sni}>{row.sni}</option>}{defaults.serverNames.map((name, position) => <option key={name + position} value={name}>{name}</option>)}</select></label>
          <label className="creation-field"><span>{t.fingerprint}</span><select aria-label={label(t.fingerprint)} data-creation-field={field("fingerprint")} aria-invalid={invalidFields.includes(field("fingerprint"))} dir="ltr" disabled={inactive} value={row.fingerprint} onChange={event => update(index, "fingerprint", event.target.value)}><option value="">{t.inherit} ({defaults.fingerprint || "chrome"})</option>{clientFingerprints.map(fingerprint => <option key={fingerprint} value={fingerprint}>{fingerprint}</option>)}</select></label>
          <label className="creation-field"><span>{t.spider}</span><input aria-label={label(t.spider)} data-creation-field={field("spiderX")} aria-invalid={invalidFields.includes(field("spiderX"))} dir="ltr" disabled={inactive} value={row.spiderX} onChange={event => update(index, "spiderX", event.target.value)} placeholder={defaults.spiderX || "/"} spellCheck={false} autoComplete="off"/><small>{t.spiderHint}</small></label>
        </div></details>
      </article>;
    })}</div>
    <button type="button" className="creation-endpoint-add" data-creation-field={prefix} disabled={inactive || rows.length >= maximum} aria-label={`${t.add}: ${pathLabel}: ${nodeID}`} onClick={() => onChange({ ...value, endpointsPresent: true, endpoints: [...rows, endpointDraft()] })}><Plus size={14}/>{t.add}</button>
  </div>;
}
