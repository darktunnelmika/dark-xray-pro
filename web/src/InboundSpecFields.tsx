import { useEffect, useId, useState } from "react";
import { Plus, RotateCcw, Trash2 } from "lucide-react";
import { readSpecPath, writeSpecPath, type AuthoringSpec, type SpecField, type SpecObject, type SpecValue } from "./inboundAuthoringSpec";

type Props = { language: "en" | "fa"; spec: AuthoringSpec; fields: SpecField[]; disabled?: boolean; prefix?: string; secretPaths?: string[]; invalidFields?: string[]; onChange: (spec: AuthoringSpec) => void; onInvalid: (path: string, invalid: boolean) => void };
function randomSecret(hex: boolean, length = 32): string {
  const bytes = crypto.getRandomValues(new Uint8Array(hex ? 16 : length));
  return hex ? Array.from(bytes, value => value.toString(16).padStart(2, "0")).join("") : btoa(String.fromCharCode(...bytes));
}
function lines(value: SpecValue | undefined): string { return Array.isArray(value) ? value.map(item => String(item)).join("\n") : typeof value === "string" ? value : ""; }
function pairsText(value: SpecValue | undefined): string {
  if (!value || typeof value !== "object" || Array.isArray(value)) return "";
  return Object.entries(value).flatMap(([key, item]) => (Array.isArray(item) ? item : [item]).map(child => `${key} = ${String(child)}`)).join("\n");
}
function PacketInput({ value, path, disabled, onChange, onInvalid }: { value: SpecValue | undefined; path: string; disabled?: boolean; onChange: (value: SpecValue | undefined) => void; onInvalid: (path: string, invalid: boolean) => void }) {
  const [draft, setDraft] = useState<string | null>(null), [error, setError] = useState(false);
  const text = Array.isArray(value) ? JSON.stringify(value) : typeof value === "string" ? value : "";
  function apply(raw: string) {
    try {
      let next: SpecValue | undefined = raw || undefined;
      if (raw.trim().startsWith("[")) { const list: unknown = JSON.parse(raw); if (!Array.isArray(list) || list.some(item => !Number.isInteger(item) || item < 0 || item > 255)) throw new Error("invalid_bytes"); next = list; }
      onChange(next); onInvalid(path, false); setError(false); setDraft(null);
    } catch { onInvalid(path, true); setError(true); }
  }
  return <textarea aria-label={path} data-creation-field={`spec.${path}`} aria-invalid={error} dir="ltr" rows={2} disabled={disabled} value={draft ?? text} onChange={event => { setDraft(event.target.value); onInvalid(path, true); }} onBlur={() => { if (draft !== null) apply(draft); }} placeholder="text, hex, base64 or [0, 1, 255]"/>;
}
function DraftObject({ value, path, pairs, disabled, onChange, onInvalid, language }: { value: SpecValue | undefined; path: string; pairs: boolean; disabled?: boolean; onChange: (value: SpecValue | undefined) => void; onInvalid: (path: string, invalid: boolean) => void; language: "fa" | "en" }) {
  const serialized = pairs ? pairsText(value) : value === undefined ? "" : JSON.stringify(value, null, 2);
  const [text, setText] = useState(serialized), [dirty, setDirty] = useState(false), [error, setError] = useState(false);
  useEffect(() => { if (!dirty) setText(serialized); }, [serialized, dirty]);
  function apply() {
    try {
      let next: SpecValue | undefined;
      if (text.trim()) {
        if (pairs) {
          const object: SpecObject = {};
          for (const line of text.split(/\r?\n/).filter(line => line.trim())) {
            const match = line.match(/^\s*([^=:]+?)\s*[=:]\s*(.*?)\s*$/);
            if (!match || ["__proto__", "constructor", "prototype"].includes(match[1])) throw new Error("invalid_map");
            const key = match[1], previous = object[key];
            if (path.endsWith("portMap")) { const port = Number(key); if (!/^\d+$/.test(key) || !Number.isInteger(port) || port < 1 || port > 65535 || !match[2]) throw new Error("invalid_port"); object[key] = match[2]; }
            else if (path.includes("tcpSettings.header")) object[key] = [...(Array.isArray(previous) ? previous : []), match[2]];
            else object[key] = match[2];
          }
          next = object;
        } else {
          next = JSON.parse(text) as SpecValue;
          if (!next || typeof next !== "object" || /"(?:__proto__|constructor|prototype)"\s*:/.test(text)) throw new Error("invalid_json");
        }
      }
      onChange(next); onInvalid(path, false); setDirty(false); setError(false);
    } catch { setError(true); onInvalid(path, true); }
  }
  return <div className="spec-object-draft"><textarea aria-label={path} data-creation-field={`spec.${path}`} aria-invalid={error} disabled={disabled} dir="ltr" spellCheck={false} autoComplete="off" rows={pairs ? 3 : 5} value={text} onChange={event => { setText(event.target.value); setDirty(true); onInvalid(path, true); }} onBlur={apply}/>
    {dirty && <button type="button" disabled={disabled} onClick={apply}>{language === "fa" ? "اعمال این فیلد" : "Apply field"}</button>}
    {error && <small role="alert">{language === "fa" ? pairs ? "در هر خط نام = مقدار وارد کن." : "JSON باید یک شیء یا آرایهٔ معتبر باشد." : pairs ? "Enter name = value on each line." : "Enter a valid JSON object or array."}</small>}
  </div>;
}

export default function InboundSpecFields(props: Props) {
  const { language, spec, fields, disabled, prefix = "", secretPaths = [], invalidFields = [], onChange, onInvalid } = props;
  const id = useId();
  return <div className="spec-fields">
    {fields.map(field => {
      const path = prefix + field.path, value = readSpecPath(spec, path), retained = secretPaths.some(saved => saved === path || path.startsWith(saved + ".")), label = language === "fa" ? field.fa ?? field.label : field.label;
      const invalid = invalidFields.includes(`spec.${path}`), inputID = `${id}-${path}`;
      const set = (next: SpecValue | undefined) => { onChange(writeSpecPath(spec, path, next)); onInvalid(path, false); };
      const common = { id: inputID, "data-creation-field": `spec.${path}`, "aria-label": label, "aria-invalid": invalid, disabled: disabled || field.disabled && value !== true, dir: "ltr" as const, autoComplete: "off", spellCheck: false, maxLength: field.maxLength };
      const hint = language === "fa" ? field.faHint ?? field.hint : field.hint;
      if (field.kind === "rows") {
        const rows = Array.isArray(value) ? value : [];
        return <section className="spec-repeater" key={path} data-spec-rows={path}>
          <header><h5>{label}<bdi>{rows.length}</bdi></h5><button type="button" disabled={disabled || rows.length >= 64} onClick={() => set([...rows, Object.fromEntries((field.fields ?? []).filter(child => child.initial !== undefined).map(child => [child.path, child.initial!]))])}><Plus size={14}/>{language === "fa" ? "افزودن" : "Add"}</button></header>
          {!rows.length && <p className="creation-help">{language === "fa" ? "موردی تنظیم نشده است." : "No entries configured."}</p>}
          {rows.map((_, index) => <details key={`${path}-${index}`} className="spec-row" open data-spec-row={index}><summary>{label} <bdi>{index + 1}</bdi></summary><div className="spec-row-body"><InboundSpecFields {...props} prefix={`${path}.${index}.`} fields={field.fields ?? []}/><button className="spec-remove" type="button" disabled={disabled} aria-label={`${language === "fa" ? "حذف" : "Remove"} ${label} ${index + 1}`} onClick={() => set(rows.filter((_, at) => at !== index))}><Trash2 size={14}/>{language === "fa" ? "حذف" : "Remove"}</button></div></details>)}
        </section>;
      }
      if (field.kind === "boolean") return <div key={path}><label className="spec-check" htmlFor={inputID}><input {...common} type="checkbox" checked={value === true} onChange={event => set(event.target.checked)}/><span>{label}</span></label>{hint && <small className="creation-help">{hint}</small>}</div>;
      return <div className={`creation-field spec-field${field.wide || ["pairs", "json", "pem", "pemText"].includes(field.kind ?? "") ? " spec-wide" : ""}`} key={path}>
        <span><label htmlFor={inputID}>{label}{field.required && <span className="spec-required" aria-label={language === "fa" ? "الزامی" : "required"}> *</span>}</label>{retained && <small className="spec-retained">{language === "fa" ? "ذخیره‌شده" : "Saved"}</small>}</span>
        {field.kind === "packet" ? <PacketInput value={value} path={path} disabled={disabled} onChange={set} onInvalid={onInvalid}/> : field.kind === "pairs" || field.kind === "json" ? <DraftObject value={value} path={path} pairs={field.kind === "pairs"} disabled={disabled} language={language} onChange={set} onInvalid={onInvalid}/> : field.kind === "select" ? <select {...common} value={typeof value === "string" ? value : ""} onChange={event => set(event.target.value || undefined)}><option value="">{language === "fa" ? "پیش‌فرض" : "Default"}</option>{field.options?.map(option => <option key={option} value={option}>{option}</option>)}</select> : ["lines", "numbers", "pem", "pemText"].includes(field.kind ?? "") ? <textarea {...common} data-private-field={field.secret || undefined} rows={field.kind?.startsWith("pem") ? 4 : 3} value={lines(value)} placeholder={retained ? language === "fa" ? "برای حفظ مقدار ذخیره‌شده خالی بگذار" : "Blank keeps saved material" : field.placeholder} onChange={event => { const raw = event.target.value; set(!raw ? undefined : field.kind === "pemText" ? raw : raw.split(field.kind === "numbers" ? /[\s,]+/ : /\r?\n/).map(line => field.kind?.startsWith("pem") ? line : line.trim()).filter(line => field.kind === "pem" || !!line).map(line => field.kind === "numbers" && /^-?\d+$/.test(line) ? Number(line) : line)); }}/> : <input {...common} data-private-field={field.secret || undefined} type={field.secret ? "password" : "text"} inputMode={field.kind === "number" ? "numeric" : undefined} value={typeof value === "string" || typeof value === "number" ? value : ""} placeholder={retained ? language === "fa" ? "خالی = حفظ مقدار" : "Blank = keep saved" : field.placeholder} onChange={event => { const raw = event.target.value; set(raw === "" ? undefined : field.kind === "number" && /^-?\d+$/.test(raw) ? Number(raw) : raw); }}/>}
        {hint && <small>{hint}</small>}
        {retained && <button className="spec-generate" type="button" disabled={disabled} onClick={() => set(value === "" || Array.isArray(value) && value.length === 0 ? undefined : ["pem", "lines", "numbers"].includes(field.kind ?? "") ? [] : field.kind === "pairs" || field.kind === "json" ? {} : "")}><Trash2 size={13}/>{value === "" || Array.isArray(value) && value.length === 0 ? language === "fa" ? "لغو حذف؛ حفظ مقدار ذخیره‌شده" : "Undo clear; keep saved value" : language === "fa" ? "حذف صریح مقدار ذخیره‌شده" : "Clear saved value"}</button>}
        {field.generate && <button className="spec-generate" type="button" disabled={disabled} onClick={() => set(randomSecret(field.generate === "hex", spec.protocol === "shadowsocks" && spec.settings.method === "2022-blake3-aes-128-gcm" ? 16 : 32))}><RotateCcw size={13}/>{language === "fa" ? "تولید مقدار تازه" : "Generate value"}</button>}
      </div>;
    })}
  </div>;
}
