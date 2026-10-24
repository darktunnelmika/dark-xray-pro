import { useEffect, useRef } from "react";
import { maxRealityTimeDiff, profileOptionsDraft, type ProfileOptionsDraft } from "./inboundProfileOptions";
import "./InboundProfileAdvanced.css";

const words = {
  en: {
    network: "Advanced gRPC settings", security: "Advanced REALITY settings", authority: "gRPC Authority", multiMode: "gRPC MultiMode",
    authorityHint: "Optional host or IP, with an optional port. Leave blank for the default authority.",
    xver: "REALITY Xver", none: "Disabled (0)", time: "Maximum time difference (ms)", timeHint: "0 disables the time-difference limit.",
    minVersion: "Minimum client version", maxVersion: "Maximum client version", versionHint: "Optional x.y.z, for example 1.8.0. Each component is 0–255.",
    show: "REALITY Show", unavailable: "These settings require an updated server. Existing values are retained.",
  },
  fa: {
    network: "تنظیمات پیشرفتهٔ gRPC", security: "تنظیمات پیشرفتهٔ REALITY", authority: "Authority در gRPC", multiMode: "MultiMode در gRPC",
    authorityHint: "هاست یا IP با پورت اختیاری؛ برای مقدار پیش‌فرض خالی بگذار.",
    xver: "Xver در REALITY", none: "غیرفعال (۰)", time: "حداکثر اختلاف زمان (میلی‌ثانیه)", timeHint: "مقدار ۰ محدودیت اختلاف زمان را غیرفعال می‌کند.",
    minVersion: "حداقل نسخهٔ کلاینت", maxVersion: "حداکثر نسخهٔ کلاینت", versionHint: "اختیاری با قالب x.y.z، مثلاً 1.8.0؛ هر بخش بین ۰ تا ۲۵۵ است.",
    show: "Show در REALITY", unavailable: "این تنظیمات به نسخهٔ جدید سرور نیاز دارند. مقادیر قبلی حفظ می‌شوند.",
  },
};

export default function InboundProfileAdvanced({ language, section, value, onChange, supported, disabled, invalidFields = [] }: {
  language: "fa" | "en"; section: "network" | "security"; value?: ProfileOptionsDraft;
  onChange: (value: ProfileOptionsDraft) => void; supported: boolean; disabled: boolean; invalidFields?: string[];
}) {
  const t = words[language], current = value ?? profileOptionsDraft(), disclosure = useRef<HTMLDetailsElement>(null);
  const fields = section === "network" ? ["grpcAuthority", "grpcMultiMode"] : ["realityXver", "realityMaxTimeDiff", "realityMinClientVer", "realityMaxClientVer", "realityShow"];
  const invalid = fields.some(field => invalidFields.includes(field));
  useEffect(() => { if (invalid && disclosure.current) disclosure.current.open = true; }, [invalid]);
  function change<K extends keyof ProfileOptionsDraft>(field: K, next: ProfileOptionsDraft[K]) { onChange({ ...current, [field]: next }); }
  const inactive = disabled || !supported;
  return <details ref={disclosure} className="inbound-profile-advanced">
    <summary>{t[section]}</summary>
    {!supported && <p className="inbound-option-hint" role="status">{t.unavailable}</p>}
    <div className="inbound-options-grid">
      {section === "network" ? <>
        <label className="inbound-option-field"><span>{t.authority}</span><input data-creation-field="grpcAuthority" aria-label={t.authority} aria-invalid={invalidFields.includes("grpcAuthority")} dir="ltr" maxLength={259} disabled={inactive} value={current.grpcAuthority} onChange={event => change("grpcAuthority", event.target.value)} autoComplete="off" spellCheck={false}/><small>{t.authorityHint}</small></label>
        <label className="inbound-option-check"><input type="checkbox" data-creation-field="grpcMultiMode" disabled={inactive} checked={current.grpcMultiMode} onChange={event => change("grpcMultiMode", event.target.checked)}/><span>{t.multiMode}</span></label>
      </> : <>
        <label className="inbound-option-field"><span>{t.xver}</span><select data-creation-field="realityXver" aria-label={t.xver} disabled={inactive} dir="ltr" value={current.realityXver} onChange={event => change("realityXver", event.target.value)}><option value="0">{t.none}</option><option value="1">PROXY v1 (1)</option><option value="2">PROXY v2 (2)</option></select></label>
        <label className="inbound-option-field"><span>{t.time}</span><input data-creation-field="realityMaxTimeDiff" aria-label={t.time} aria-invalid={invalidFields.includes("realityMaxTimeDiff")} type="text" inputMode="numeric" maxLength={13} data-max={maxRealityTimeDiff} dir="ltr" disabled={inactive} value={current.realityMaxTimeDiff} onChange={event => change("realityMaxTimeDiff", event.target.value)} autoComplete="off"/><small>{t.timeHint}</small></label>
        <label className="inbound-option-field"><span>{t.minVersion}</span><input data-creation-field="realityMinClientVer" aria-label={t.minVersion} aria-invalid={invalidFields.includes("realityMinClientVer")} dir="ltr" maxLength={11} disabled={inactive} value={current.realityMinClientVer} onChange={event => change("realityMinClientVer", event.target.value)} placeholder="1.8.0" autoComplete="off" spellCheck={false}/><small>{t.versionHint}</small></label>
        <label className="inbound-option-field"><span>{t.maxVersion}</span><input data-creation-field="realityMaxClientVer" aria-label={t.maxVersion} aria-invalid={invalidFields.includes("realityMaxClientVer")} dir="ltr" maxLength={11} disabled={inactive} value={current.realityMaxClientVer} onChange={event => change("realityMaxClientVer", event.target.value)} placeholder="" autoComplete="off" spellCheck={false}/><small>{t.versionHint}</small></label>
        <label className="inbound-option-check"><input type="checkbox" data-creation-field="realityShow" disabled={inactive} checked={current.realityShow} onChange={event => change("realityShow", event.target.checked)}/><span>{t.show}</span></label>
      </>}
    </div>
  </details>;
}
