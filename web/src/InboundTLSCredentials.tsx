import { KeyRound, ShieldCheck } from "lucide-react";
import { maxTLSCertificateBytes, maxTLSPrivateKeyBytes, type TLSCredentials, type TLSMetadata } from "./inboundTLS";

const words = {
  en: {
    title: "TLS certificate", certificate: "TLS certificate (PEM)", privateKey: "TLS private key (PEM)",
    hint: "Paste the certificate chain with its leaf first, and the matching unencrypted private key. The server verifies the pair before saving.",
    certificateHint: "PEM certificate chain: up to 8 certificates, 64 KiB total.", keyHint: "Unencrypted RSA, EC or PKCS#8 PEM key; up to 16 KiB. The key is never returned or shown in saved settings.",
    retained: "Saved certificate retained", replace: "Replace certificate and key", keep: "Keep saved certificate",
    replacement: "Replace both fields together. The saved certificate stays unchanged until this setup is saved.",
    copy: "A copied TLS setup needs its own certificate and key. Private credentials are not copied.",
    names: "Certificate DNS names", validFrom: "Valid from", expires: "Expires", type: "Public key type", hash: "Certificate SHA-256", publicHash: "Public key SHA-256",
    missing: "Certificate has not been checked yet.",
  },
  fa: {
    title: "گواهی TLS", certificate: "گواهی TLS با قالب PEM", privateKey: "کلید خصوصی TLS با قالب PEM",
    hint: "زنجیرهٔ گواهی را با گواهی سرور در ابتدا و کلید خصوصی متناظرِ بدون رمز وارد کن. سرور پیش از ذخیره، تطابق آن‌ها را بررسی می‌کند.",
    certificateHint: "زنجیرهٔ PEM: حداکثر ۸ گواهی و در مجموع ۶۴ کیبی‌بایت.", keyHint: "کلید بدون رمز با قالب RSA، EC یا PKCS#8 PEM؛ حداکثر ۱۶ کیبی‌بایت. کلید در پاسخ سرور یا تنظیمات ذخیره‌شده نمایش داده نمی‌شود.",
    retained: "گواهی ذخیره‌شده حفظ می‌شود", replace: "جایگزینی گواهی و کلید", keep: "حفظ گواهی ذخیره‌شده",
    replacement: "هر دو فیلد را با هم جایگزین کن؛ تا ذخیرهٔ تنظیمات، گواهی قبلی تغییر نمی‌کند.",
    copy: "کپی تنظیمات TLS به گواهی و کلید خودش نیاز دارد؛ اطلاعات خصوصی کپی نمی‌شود.",
    names: "نام‌های DNS گواهی", validFrom: "شروع اعتبار", expires: "پایان اعتبار", type: "نوع کلید عمومی", hash: "SHA-256 گواهی", publicHash: "SHA-256 کلید عمومی",
    missing: "گواهی هنوز بررسی نشده است.",
  },
};

export function TLSCertificateSummary({ metadata, language }: { metadata: TLSMetadata; language: "en" | "fa" }) {
  const t = words[language];
  const date = (seconds: number) => new Intl.DateTimeFormat(language === "fa" ? "fa-IR" : "en-GB", { dateStyle: "medium", timeStyle: "short", timeZone: "Asia/Tehran" }).format(seconds * 1000);
  return <dl className="creation-tls-metadata" data-tls-metadata>
    <div><dt>{t.names}</dt><dd><bdi dir="ltr">{metadata.dns_names.join(", ")}</bdi></dd></div>
    <div><dt>{t.validFrom}</dt><dd>{date(metadata.not_before_unix)}</dd></div><div><dt>{t.expires}</dt><dd>{date(metadata.not_after_unix)} · {language === "fa" ? "تهران" : "Tehran"}</dd></div>
    <div><dt>{t.type}</dt><dd><bdi dir="ltr">{metadata.key_type}</bdi></dd></div>
    <div><dt>{t.hash}</dt><dd><bdi dir="ltr">{metadata.sha256}</bdi></dd></div>
    <div><dt>{t.publicHash}</dt><dd><bdi dir="ltr">{metadata.public_key_sha256}</bdi></dd></div>
  </dl>;
}

export default function InboundTLSCredentials({ language, metadata, credentials, replacing, copied, disabled, invalidFields, onChange, onReplace }: {
  language: "en" | "fa"; metadata?: TLSMetadata; credentials: TLSCredentials; replacing: boolean; copied: boolean; disabled: boolean;
  invalidFields: string[]; onChange: (value: TLSCredentials) => void; onReplace: (value: boolean) => void;
}) {
  const t = words[language];
  return <section className="creation-tls" aria-label={t.title}>
    <h4><ShieldCheck size={16}/>{t.title}</h4>
    {metadata && <><p className="creation-tls-retained" role="status">{replacing ? t.replacement : t.retained}</p><TLSCertificateSummary metadata={metadata} language={language}/></>}
    {copied && <p className="creation-help">{t.copy}</p>}
    {metadata && <button type="button" disabled={disabled} data-creation-field="tlsCredentials" onClick={() => onReplace(!replacing)}>{replacing ? t.keep : t.replace}</button>}
    {(replacing || !metadata) && <div className="creation-tls-inputs"><p>{t.hint}</p>
      <label className="creation-field"><span>{t.certificate}</span><textarea data-creation-field="tlsCertificate" aria-label={t.certificate} aria-invalid={invalidFields.includes("tlsCertificate")} dir="ltr" rows={5} maxLength={maxTLSCertificateBytes} autoComplete="off" spellCheck={false} disabled={disabled} value={credentials.certificate_pem} onChange={event => onChange({ ...credentials, certificate_pem: event.target.value })}/><small>{t.certificateHint}</small></label>
      <label className="creation-field"><span><KeyRound size={14}/>{t.privateKey}</span><textarea data-creation-field="tlsPrivateKey" data-private-field aria-label={t.privateKey} aria-invalid={invalidFields.includes("tlsPrivateKey")} dir="ltr" rows={5} maxLength={maxTLSPrivateKeyBytes} autoComplete="off" spellCheck={false} disabled={disabled} value={credentials.private_key_pem} onChange={event => onChange({ ...credentials, private_key_pem: event.target.value })}/><small>{t.keyHint}</small></label>
    </div>}
  </section>;
}
