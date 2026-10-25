import { useRef, useState } from "react";
import { Code2, Plus } from "lucide-react";
import type { InboundNode } from "./inboundTypes";
import InboundSpecFields from "./InboundSpecFields";
import InboundSecurityTools from "./InboundSecurityTools";
import InboundFinalMask, { ECHSocketFields, sniffingFields } from "./InboundFinalMask";
import { allowedSpecSecurity, canonicalAuthoringSpec, clientFields, compatibleAuthoringFields, defaultSpec, generalTransportProtocols, inboundProtocols, inboundTransports, muxFields, networkNames, protocolClientFields, protocolFields, protocolNames, readSpecPath, realityAdvancedFields, realityFields, redactSpec, socketFields, specPayload, tlsAdvancedFields, tlsFields, transportGroups, validAuthoringSpec, writeSpecPath, type AuthoringSpec, type SpecField, type SpecValue } from "./inboundAuthoringSpec";
import "./InboundAuthoring.css";

export type AuthoringSectionProps = { section: "protocol" | "transport" | "security" | "advanced"; language: "fa" | "en"; spec: AuthoringSpec; secretPaths: string[]; nodes: InboundNode[]; csrf: string; disabled: boolean; invalidFields: string[]; onChange: (value: AuthoringSpec) => void; onInvalid: (path: string, invalid: boolean) => void; onUnauthorized: () => void };

function AdvancedJSON({ spec, secretPaths, disabled, language, onChange, onInvalid }: Pick<AuthoringSectionProps, "spec" | "secretPaths" | "disabled" | "language" | "onChange" | "onInvalid">) {
  const [draft, setDraft] = useState<string | null>(null), [error, setError] = useState(false);
  const publicValue = JSON.stringify(redactSpec(specPayload(spec, secretPaths), secretPaths), null, 2);
  function apply() {
    try {
      const next: unknown = JSON.parse(draft ?? publicValue);
      if (!validAuthoringSpec(next) || /"(?:__proto__|constructor|prototype)"\s*:/.test(draft ?? "")) throw new Error("invalid_spec");
      let merged = canonicalAuthoringSpec(next);
      // A public JSON edit cannot silently discard credentials entered in the
      // dedicated write-only fields. An explicit material field can replace it.
      function preserve(value: SpecValue, path = "") {
        if (Array.isArray(value)) { value.forEach((item, index) => preserve(item, `${path}.${index}`)); return; }
        if (value && typeof value === "object") for (const [key, item] of Object.entries(value)) {
          const full = path ? `${path}.${key}` : key;
          if (readSpecPath(redactSpec(spec, secretPaths), full) === undefined && readSpecPath(merged, full) === undefined && (typeof item !== "object" || Array.isArray(item))) merged = writeSpecPath(merged, full, item);
          else preserve(item, full);
        }
      }
      preserve(spec as unknown as SpecValue);
      onChange(merged); onInvalid("json", false); setDraft(null); setError(false);
    } catch { setError(true); onInvalid("json", true); }
  }
  return <details className="spec-disclosure"><summary><Code2 size={15}/>{language === "fa" ? "JSON پیشرفته" : "Advanced JSON"}</summary><div className="spec-group-body">
    <p className="creation-help">{language === "fa" ? "برای تنظیمات تکمیلی؛ فیلدهای بالا هم‌زمان به‌روز می‌شوند. کلید و گواهی ذخیره‌شده در این نمای عمومی نمایش داده نمی‌شوند." : "Additional configuration. Applying updates the fields above. Stored keys and certificates are omitted from this public view."}</p>
    <textarea className="spec-json" aria-label="Advanced specification JSON" data-creation-field="spec.json" dir="ltr" disabled={disabled} spellCheck={false} rows={12} value={draft ?? publicValue} onChange={event => { setDraft(event.target.value); onInvalid("json", true); }}/>
    <div className="spec-actions"><button type="button" disabled={disabled || draft === null} onClick={apply}>{language === "fa" ? "اعمال JSON" : "Apply JSON"}</button><button type="button" disabled={disabled || draft === null} onClick={() => { setDraft(null); setError(false); onInvalid("json", false); }}>{language === "fa" ? "بازگردانی" : "Reset"}</button></div>
    {error && <p role="alert" className="creation-issues">{language === "fa" ? "ساختار JSON یا نسخهٔ تنظیمات معتبر نیست." : "Invalid JSON or specification structure."}</p>}
  </div></details>;
}

export default function InboundAuthoringSection(props: AuthoringSectionProps) {
  const { section, language, spec, onChange, disabled, secretPaths } = props;
  const protocolDrafts = useRef<Record<string, AuthoringSpec>>({});
  const fields = (items: SpecField[]) => <InboundSpecFields {...props} fields={compatibleAuthoringFields(spec, items)}/>;
  const fa = language === "fa";
  if (section === "protocol") return <div className="spec-section" data-authoring-section="protocol">
    <label className="creation-field"><span>{fa ? "پروتکل" : "Protocol"}</span><select aria-label={fa ? "پروتکل" : "Protocol"} data-creation-field="spec.protocol" dir="ltr" disabled={disabled} value={spec.protocol} onChange={event => { protocolDrafts.current[spec.protocol] = structuredClone(spec); onChange(protocolDrafts.current[event.target.value] ?? defaultSpec(event.target.value)); }}>
      {inboundProtocols.map(protocol => <option key={protocol} value={protocol}>{protocolNames[protocol]}</option>)}
    </select></label>
    <p className="creation-help">{fa ? "شناسه‌ها و دسترسی مشتری‌ها از بخش مدیریت کلاینت تأمین می‌شوند." : "Customer identities and access are supplied by client management."}</p>
    {fields(protocolFields[spec.protocol] ?? [])}
    {["wireguard", "amneziawg"].includes(spec.protocol) && <InboundSecurityTools {...props} mode="protocol-key"/>}
  </div>;
  if (section === "transport") return <div className="spec-section" data-authoring-section="transport">
    {generalTransportProtocols.includes(spec.protocol) ? <label className="creation-field"><span>{fa ? "انتقال" : "Transport"}</span><select aria-label={fa ? "انتقال" : "Transport"} data-creation-field="spec.network" dir="ltr" disabled={disabled} value={spec.network} onChange={event => { const network = event.target.value, allowed = allowedSpecSecurity(spec.protocol, network); onChange({ ...spec, network, security: allowed.includes(spec.security) ? spec.security : allowed[0] }); }}>
      {inboundTransports.map(network => <option key={network} value={network}>{networkNames[network]}</option>)}
    </select></label> : <p className="spec-intrinsic"><bdi>{protocolNames[spec.protocol]} · {networkNames[spec.network] ?? spec.network}</bdi><span>{fa ? "انتقال اختصاصی این پروتکل" : "Protocol-specific transport"}</span></p>}
    {!["mtproto", "amneziawg", "tuic", "tun", "http", "mixed"].includes(spec.protocol) && <>{(transportGroups[spec.network] ?? []).map(group => group.advanced ? <details key={group.title} className="spec-disclosure"><summary>{fa ? group.fa : group.title}</summary><div className="spec-group-body">{fields(group.fields)}</div></details> : <section key={group.title} className="spec-group"><h5>{fa ? group.fa : group.title}</h5>{fields(group.fields)}</section>)}
    <details className="spec-disclosure"><summary>SOCKOPT</summary><div className="spec-group-body">{fields(socketFields)}</div></details></>}
  </div>;
  if (section === "security") {
    const allowed = allowedSpecSecurity(spec.protocol, spec.network);
    const visibleClient = [...clientFields.filter(field => {
      if (["client.spiderX", "client.publicKey", "client.shortId", "client.mldsa65Verify"].includes(field.path)) return spec.security === "reality";
      if (["client.echConfigList", "client.pinnedPeerCertSha256", "client.verifyPeerCertByName"].includes(field.path)) return spec.security === "tls";
      if (["client.flow", "client.encryption"].includes(field.path)) return spec.protocol === "vless";
      if (field.path === "client.fingerprint") return !["hysteria", "masque", "tuic"].includes(spec.protocol) && spec.security !== "none";
      return spec.security !== "none";
    }), ...protocolClientFields(spec)];
    return <div className="spec-section" data-authoring-section="security">
      <label className="creation-field"><span>{fa ? "نوع امنیت" : "Security"}</span><select aria-label={fa ? "نوع امنیت" : "Security"} data-creation-field="spec.security" disabled={disabled} dir="ltr" value={spec.security} onChange={event => onChange({ ...spec, security: event.target.value as AuthoringSpec["security"] })}>{(["none", "tls", "reality"] as const).map(security => <option key={security} value={security} disabled={!allowed.includes(security)}>{security === "none" ? "None" : security.toUpperCase()}{!allowed.includes(security) ? fa ? " — ناسازگار با انتخاب فعلی" : " — incompatible with this selection" : ""}</option>)}</select></label>
      {spec.security === "reality" && <>{fields(realityFields)}<div className="spec-actions"><button type="button" disabled={disabled || Array.isArray(spec.reality.shortIds) && spec.reality.shortIds.length >= 16} onClick={() => onChange(writeSpecPath(spec, "reality.shortIds", [...(Array.isArray(spec.reality.shortIds) ? spec.reality.shortIds : []), Array.from(crypto.getRandomValues(new Uint8Array(4)), byte => byte.toString(16).padStart(2, "0")).join("")]))}><Plus size={14}/>{fa ? "ساخت Short ID" : "Generate Short ID"}</button></div><details className="spec-disclosure"><summary>{fa ? "تنظیمات پیشرفتهٔ REALITY" : "Advanced REALITY settings"}</summary><div className="spec-group-body">{fields(realityAdvancedFields)}</div></details></>}
      {spec.security === "tls" && <>{fields(tlsFields)}<details className="spec-disclosure"><summary>{fa ? "تنظیمات پیشرفتهٔ TLS" : "Advanced TLS settings"}</summary><div className="spec-group-body">{fields(tlsAdvancedFields)}<details className="spec-disclosure"><summary>ECH SOCKOPT</summary><div className="spec-group-body"><ECHSocketFields {...props}/></div></details></div></details></>}
      {spec.security !== "none" && <InboundSecurityTools {...props} mode="security"/>}
      {visibleClient.length > 0 && <details className="spec-disclosure"><summary>{fa ? "تنظیمات اتصال کلاینت" : "Client connection settings"}</summary><div className="spec-group-body">{fields(visibleClient)}</div></details>}
      {secretPaths.length > 0 && <p className="creation-help">{fa ? "کلیدها و گواهی‌های ذخیره‌شده خوانده نمی‌شوند. خالی‌گذاشتن فیلد آن‌ها را حفظ می‌کند؛ واردکردن مقدار تازه جایگزینشان می‌کند." : "Saved keys and certificates are not read back. Blank fields keep them; entering new material replaces them."}</p>}
    </div>;
  }
  const muxAllowed = ["vmess", "vless", "trojan", "shadowsocks", "http", "mixed"].includes(spec.protocol) && spec.network !== "xhttp" && !spec.client.flow;
  return <div className="spec-section" data-authoring-section="advanced"><details className="spec-disclosure"><summary>{fa ? "چندگانه‌سازی اتصال کلاینت · MUX" : "Client multiplexing · MUX"}</summary><div className="spec-group-body">{muxAllowed ? fields(muxFields) : <p className="creation-help">{fa ? "MUX با پروتکل، انتقال یا Flow فعلی سازگار نیست." : "MUX is incompatible with this protocol, transport or flow."}</p>}</div></details>{!["mtproto", "amneziawg", "tuic"].includes(spec.protocol) && <details className="spec-disclosure"><summary>Sniffing</summary><div className="spec-group-body">{fields(sniffingFields())}</div></details>}<InboundFinalMask {...props}/><AdvancedJSON {...props}/></div>;
}

export function InboundSpecReview({ spec, secretPaths, language }: { spec: AuthoringSpec; secretPaths: string[]; language: "fa" | "en" }) {
  const clean = redactSpec(specPayload(spec, secretPaths), secretPaths);
  return <section className="creation-review-card" data-review-section="spec"><h4>{language === "fa" ? "تنظیمات اتصال" : "Connection settings"}</h4><dl className="creation-review-values"><div><dt>{language === "fa" ? "پروتکل" : "Protocol"}</dt><dd><bdi>{protocolNames[spec.protocol]}</bdi></dd></div><div><dt>{language === "fa" ? "انتقال" : "Transport"}</dt><dd><bdi>{networkNames[spec.network] ?? spec.network}</bdi></dd></div><div><dt>{language === "fa" ? "امنیت" : "Security"}</dt><dd><bdi>{spec.security.toUpperCase()}</bdi></dd></div></dl><details className="spec-disclosure"><summary>{language === "fa" ? "جزئیات عمومی پیکربندی" : "Public configuration details"}</summary><pre className="spec-public-json" dir="ltr">{JSON.stringify(clean, null, 2)}</pre></details></section>;
}
