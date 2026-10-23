import { forwardRef, useEffect, useId, useImperativeHandle, useRef, useState, type FormEvent, type KeyboardEvent, type ReactNode } from "react";
import { KeyRound, Network, ShieldCheck, SlidersHorizontal } from "lucide-react";
import "./InboundProfileForm.css";

export type InboundProfileValue = {
 name: string;
 transport: string;
 grpcService: string;
 realityTarget: string;
 serverNames: string;
};
export type InboundProfileFormHandle = {
 /** Validates the profile, reveals the first invalid field and focuses it. */
 validate: () => boolean;
 focusName: () => void;
};
export type InboundProfileFormProps = {
 language: "fa" | "en";
 value: InboundProfileValue;
 onChange: (value: InboundProfileValue) => void;
 disabled?: boolean;
 mode?: "create" | "edit";
 /** Optional assignment summary or other parent-owned creation context. */
 children?: ReactNode;
};

type Tab = "basics" | "network" | "security";
type Field = keyof InboundProfileValue;
type Control = HTMLInputElement | HTMLSelectElement;
const tabOrder: Tab[] = ["basics", "network", "security"];
const fieldTabs: Record<Field, Tab> = { name: "basics", transport: "network", grpcService: "network", realityTarget: "security", serverNames: "security" };
const fieldOrder: Field[] = ["name", "transport", "grpcService", "realityTarget", "serverNames"];
const icons = { basics: SlidersHorizontal, network: Network, security: ShieldCheck };
const words = {
 en: {
  sections: "Inbound profile sections", basics: "Basics", network: "Network", security: "Security",
  basicsTitle: "Name and connection type", name: "Inbound name", editName: "Name", namePlaceholder: "e.g. Germany Direct",
  protocol: "Protocol", protection: "Security", automatic: "The Inbound ID, REALITY keys and Short ID are generated automatically.",
  preserved: "The existing Inbound ID and REALITY keys are preserved when you save.",
  networkTitle: "Connection settings", networkHelp: "Choose a transport. Its relevant settings appear below.",
  transport: "Transport", service: "gRPC service", serviceHint: "1–64 letters, numbers, underscores or hyphens.",
  tcpHint: "TCP uses Vision. No service name is needed.",
  securityTitle: "REALITY settings", securityHelp: "Set the target and server names for this Inbound.",
  target: "REALITY target (host:port)", editTarget: "REALITY target", targetHint: "Enter the target address and port, for example example.org:443.",
  serverNames: "Server names (comma-separated)", editServerNames: "Server names", serverNamesHint: "Separate multiple server names with commas.",
  nameError: "Enter an Inbound name.", serviceError: "Use 1–64 letters, numbers, underscores or hyphens for the gRPC service.",
  targetError: "Enter a REALITY target including its port.", serverNamesError: "Enter at least one server name.",
 },
 fa: {
  sections: "بخش‌های پروفایل اینباند", basics: "پایه", network: "شبکه", security: "امنیت",
  basicsTitle: "نام و نوع اتصال", name: "نام اینباند", editName: "نام", namePlaceholder: "مثلاً آلمان مستقیم",
  protocol: "پروتکل", protection: "امنیت", automatic: "شناسهٔ اینباند، کلیدهای REALITY و Short ID خودکار ساخته می‌شوند.",
  preserved: "با ذخیرهٔ تغییرات، شناسهٔ اینباند و کلیدهای REALITY فعلی حفظ می‌شوند.",
  networkTitle: "تنظیمات اتصال", networkHelp: "روش انتقال را انتخاب کن؛ تنظیمات مربوط به همان روش نمایش داده می‌شود.",
  transport: "ترنسپورت", service: "سرویس gRPC", serviceHint: "۱ تا ۶۴ حرف انگلیسی، عدد، زیرخط یا خط تیره.",
  tcpHint: "TCP از Vision استفاده می‌کند و به نام سرویس نیاز ندارد.",
  securityTitle: "تنظیمات REALITY", securityHelp: "مقصد و نام‌های سرور را برای این اینباند مشخص کن.",
  target: "مقصد REALITY (هاست:پورت)", editTarget: "مقصد REALITY", targetHint: "آدرس و پورت مقصد را وارد کن؛ مثلاً example.org:443.",
  serverNames: "نام‌های سرور (با کاما)", editServerNames: "نام‌های سرور", serverNamesHint: "نام‌های سرور را با کاما از هم جدا کن.",
  nameError: "نام اینباند را وارد کن.", serviceError: "برای سرویس gRPC از ۱ تا ۶۴ حرف انگلیسی، عدد، زیرخط یا خط تیره استفاده کن.",
  targetError: "مقصد REALITY را همراه پورت وارد کن.", serverNamesError: "حداقل یک نام سرور وارد کن.",
 },
};

/** Profile fields only. The parent owns the form, submission and persistent actions. */
const InboundProfileForm = forwardRef<InboundProfileFormHandle, InboundProfileFormProps>(function InboundProfileForm({ language, value, onChange, disabled = false, mode = "create", children }, ref) {
 const t = words[language];
 const id = useId();
 const [activeTab, setActiveTab] = useState<Tab>("basics");
 const [errors, setErrors] = useState<Partial<Record<Field, string>>>({});
 const [focusRequest, setFocusRequest] = useState(0);
 const controls = useRef<Partial<Record<Field, Control | null>>>({});
 const tabButtons = useRef<Partial<Record<Tab, HTMLButtonElement | null>>>({});
 const pendingFocus = useRef<{ field: Field; report: boolean } | null>(null);
 const reportingInvalid = useRef(false);

 function showField(field: Field, report = false) {
  pendingFocus.current = { field, report };
  setActiveTab(fieldTabs[field]);
  setFocusRequest(n => n + 1);
 }
 useEffect(() => {
  const pending = pendingFocus.current;
  if (!pending) return;
  pendingFocus.current = null;
  const control = controls.current[pending.field];
  if (!control || control.disabled || !control.isConnected) return;
  control.focus({ preventScroll: true });
  control.scrollIntoView({ block: "nearest", inline: "nearest" });
  if (pending.report) {
   reportingInvalid.current = true;
   control.reportValidity();
   reportingInvalid.current = false;
  }
 }, [focusRequest]);

 function validate(): boolean {
  if (disabled) return false;
  const next: Partial<Record<Field, string>> = {};
  if (!value.name.trim()) next.name = t.nameError;
  if (value.transport === "grpc" && !/^[A-Za-z0-9_-]{1,64}$/.test(value.grpcService)) next.grpcService = t.serviceError;
  const target = value.realityTarget.match(/^(?:\[[0-9a-fA-F:.]+\]|[^:\s/\\@%\[\]]+):([0-9]+)$/);
  if (!target || Number(target[1]) < 1 || Number(target[1]) > 65535) next.realityTarget = t.targetError;
  if (!value.serverNames.split(/[\s,]+/).filter(Boolean).length) next.serverNames = t.serverNamesError;
  for (const field of fieldOrder) controls.current[field]?.setCustomValidity(next[field] ?? "");
  const invalid = fieldOrder.find(field => {
   const control = controls.current[field];
   return control && !control.disabled && !control.validity.valid;
  });
  setErrors(next);
  if (invalid) showField(invalid, true);
  return !invalid;
 }
 useImperativeHandle(ref, () => ({ validate, focusName: () => showField("name") }));

 function update(field: Field, nextValue: string) {
  controls.current[field]?.setCustomValidity("");
  setErrors(previous => { const next = { ...previous }; delete next[field]; return next; });
  onChange({ ...value, [field]: nextValue });
 }
 function nativeInvalid(event: FormEvent<HTMLDivElement>) {
  if (reportingInvalid.current) return;
  // Parent forms use noValidate + validate(). This also covers native submissions.
  if (!(event.target instanceof HTMLInputElement || event.target instanceof HTMLSelectElement) || !event.target.dataset.profileField) return;
  event.preventDefault();
  validate();
 }
 function tabKey(event: KeyboardEvent<HTMLButtonElement>, tab: Tab) {
  const position = tabOrder.indexOf(tab);
  const forward = language === "fa" ? -1 : 1;
  const offset = event.key === "ArrowRight" ? forward : event.key === "ArrowLeft" ? -forward : 0;
  const next = event.key === "Home" ? tabOrder[0] : event.key === "End" ? tabOrder[tabOrder.length - 1] : offset ? tabOrder[(position + offset + tabOrder.length) % tabOrder.length] : undefined;
  if (!next) return;
  event.preventDefault();
  setActiveTab(next);
  tabButtons.current[next]?.focus();
 }
 const describe = (field: Field, hint?: string) => [hint, errors[field] ? `${id}-${field}-error` : ""].filter(Boolean).join(" ") || undefined;
 const error = (field: Field) => errors[field] ? <span className="inbound-profile-error" id={`${id}-${field}-error`} role="alert">{errors[field]}</span> : null;
 const nameLabel = mode === "edit" ? t.editName : t.name;
 const targetLabel = mode === "edit" ? t.editTarget : t.target;
 const serverNamesLabel = mode === "edit" ? t.editServerNames : t.serverNames;

 return <div className="inbound-profile-form" dir={language === "fa" ? "rtl" : "ltr"} onInvalidCapture={nativeInvalid}>
  <div className="inbound-profile-tabs" role="tablist" aria-label={t.sections}>
   {tabOrder.map(tab => { const Icon = icons[tab]; return <button key={tab} ref={node => { tabButtons.current[tab] = node; }} id={`${id}-tab-${tab}`} type="button" role="tab" aria-selected={activeTab === tab} aria-controls={`${id}-panel-${tab}`} tabIndex={activeTab === tab ? 0 : -1} disabled={disabled} onClick={() => setActiveTab(tab)} onKeyDown={event => tabKey(event, tab)}><Icon size={17} aria-hidden="true"/><span>{t[tab]}</span></button>; })}
  </div>

  <section className="inbound-profile-panel" role="tabpanel" id={`${id}-panel-basics`} aria-labelledby={`${id}-tab-basics`} hidden={activeTab !== "basics"}>
   <header className="inbound-profile-section-heading"><h3>{t.basicsTitle}</h3></header>
   <label className="inbound-profile-field"><span>{nameLabel}</span><input ref={node => { controls.current.name = node; }} data-profile-field="name" data-initial-focus aria-label={nameLabel} aria-invalid={!!errors.name} aria-describedby={describe("name")} value={value.name} onChange={event => update("name", event.target.value)} maxLength={128} required disabled={disabled} placeholder={t.namePlaceholder} autoComplete="off"/>{error("name")}</label>
   <dl className="inbound-profile-identity"><div><dt>{t.protocol}</dt><dd><bdi>VLESS</bdi></dd></div><div><dt>{t.protection}</dt><dd><ShieldCheck size={14} aria-hidden="true"/><bdi>REALITY</bdi></dd></div></dl>
   <p className="inbound-profile-note"><KeyRound size={16} aria-hidden="true"/><span>{mode === "edit" ? t.preserved : t.automatic}</span></p>
   {children && <div className="inbound-profile-context">{children}</div>}
  </section>

  <section className="inbound-profile-panel" role="tabpanel" id={`${id}-panel-network`} aria-labelledby={`${id}-tab-network`} hidden={activeTab !== "network"}>
   <header className="inbound-profile-section-heading"><h3>{t.networkTitle}</h3><p>{t.networkHelp}</p></header>
   <div className="inbound-profile-field-grid">
    <label className="inbound-profile-field"><span>{t.transport}</span><select ref={node => { controls.current.transport = node; }} data-profile-field="transport" aria-label={t.transport} value={value.transport} onChange={event => update("transport", event.target.value)} required disabled={disabled} dir="ltr"><option value="tcp">TCP / REALITY</option><option value="grpc">gRPC / REALITY</option></select></label>
    {value.transport === "grpc" && <label className="inbound-profile-field"><span>{t.service}</span><input ref={node => { controls.current.grpcService = node; }} data-profile-field="grpcService" aria-label={t.service} aria-invalid={!!errors.grpcService} aria-describedby={describe("grpcService", `${id}-service-hint`)} value={value.grpcService} onChange={event => update("grpcService", event.target.value)} pattern={"[A-Za-z0-9_\\-]{1,64}"} maxLength={64} required disabled={disabled} dir="ltr" spellCheck={false} autoCapitalize="none" autoComplete="off" placeholder="dark-pro"/><small id={`${id}-service-hint`}>{t.serviceHint}</small>{error("grpcService")}</label>}
   </div>
   {value.transport === "tcp" && <p className="inbound-profile-note"><Network size={16} aria-hidden="true"/><span>{t.tcpHint}</span></p>}
  </section>

  <section className="inbound-profile-panel" role="tabpanel" id={`${id}-panel-security`} aria-labelledby={`${id}-tab-security`} hidden={activeTab !== "security"}>
   <header className="inbound-profile-section-heading"><h3>{t.securityTitle}</h3><p>{t.securityHelp}</p></header>
   <div className="inbound-profile-field-grid">
    <label className="inbound-profile-field"><span>{targetLabel}</span><input ref={node => { controls.current.realityTarget = node; }} data-profile-field="realityTarget" aria-label={targetLabel} aria-invalid={!!errors.realityTarget} aria-describedby={describe("realityTarget", `${id}-target-hint`)} value={value.realityTarget} onChange={event => update("realityTarget", event.target.value)} required disabled={disabled} dir="ltr" spellCheck={false} autoCapitalize="none" autoComplete="off" placeholder="example.org:443"/><small id={`${id}-target-hint`}>{t.targetHint}</small>{error("realityTarget")}</label>
    <label className="inbound-profile-field"><span>{serverNamesLabel}</span><input ref={node => { controls.current.serverNames = node; }} data-profile-field="serverNames" aria-label={serverNamesLabel} aria-invalid={!!errors.serverNames} aria-describedby={describe("serverNames", `${id}-names-hint`)} value={value.serverNames} onChange={event => update("serverNames", event.target.value)} required disabled={disabled} dir="ltr" spellCheck={false} autoCapitalize="none" autoComplete="off" placeholder="example.org"/><small id={`${id}-names-hint`}>{t.serverNamesHint}</small>{error("serverNames")}</label>
   </div>
  </section>
 </div>;
});

export default InboundProfileForm;
