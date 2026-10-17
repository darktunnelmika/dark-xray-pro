import { useState } from "react";
import { Beaker, Check, LockKeyhole, ShieldCheck, Server, ArrowRight } from "lucide-react";
import "./NativeCanary.css";

type Language = "en" | "fa";
const copy = {
 en: {
  eyebrow: "NEW PRO ONLY / PRIVATE QA", title: "USA loopback canary",
  intro: "Create ONE disposable test account and REALITY Inbound through your authenticated owner session. Xray remains OFF; no real customer is imported.",
  account: "One-day test · 1 MiB limit", node: "USA PRO · usa-pro-01",
  port: "127.0.0.1:19666 · unreachable from the public Internet",
  agreement: "I understand this stages a dummy account and a loopback-only target. It does not start customer Xray or publish a subscription.",
  stage: "Stage private test", staged: "Canary staged — Xray not started",
  unavailable: "Requires a fresh authenticated USA PRO Node, enabled private ledger and owner session.",
  caution: "Activation remains a separate reviewed step after the initial metering receipt. Previous data and Nodes are not modified.",
 },
 fa: {
  eyebrow: "فقط PRO جدید / آزمایش خصوصی", title: "تست کنترل‌شدهٔ آمریکا",
  intro: "یک اکانت آزمایشی یک‌روزه و یک Inbound REALITY فقط از طریق نشست امن مالک ساخته می‌شود. Xray خاموش می‌ماند و هیچ کاربر قدیمی منتقل نمی‌شود.",
  account: "اشتراک یک‌روزه · سقف ۱ مگابایت",
  node: "USA PRO · usa-pro-01",
  port: "127.0.0.1:19666 · بدون دسترسی از اینترنت",
  agreement: "تأیید می‌کنم فقط اکانت تست و مقصد لوپ‌بک ساخته می‌شود؛ هیچ Xray مشتری فعال یا اشتراکی منتشر نمی‌شود.",
  stage: "آماده‌سازی تست خصوصی", staged: "تست آماده شد؛ Xray هنوز خاموش است",
  unavailable: "اتصال تازه و معتبر آمریکا، دفتر حسابداری PRO و نشست مالک لازم است.",
  caution: "فعال‌سازی فقط بعد از بررسی رسید اولیهٔ حسابداری و مرحلهٔ مستقل بعدی انجام می‌شود. اطلاعات پنل قبلی تغییر نمی‌کند.",
 },
} as const;
export default function NativeCanary({
 language, hasUSA, staged, unavailable, busy, onStage,
}: {
 language: Language; hasUSA: boolean; staged: boolean;
 unavailable: boolean; busy: boolean; onStage: () => void;
}) {
 const t = copy[language];
 const [accepted,setAccepted] = useState(false);
 const canStage = hasUSA && !staged && !unavailable && !busy && accepted;
 return <section className="native-canary" aria-label="USA PRO loopback canary">
   <div className="native-canary-heading">
    <span className="native-canary-icon"><Beaker size={19}/></span>
    <div><span className="native-canary-kicker"><ShieldCheck size={13}/>{t.eyebrow}</span>
     <h3>{t.title}</h3><p>{t.intro}</p></div>
   </div>
   <div className="native-canary-facts">
    <span><Server size={14}/>{t.node}</span>
    <span><LockKeyhole size={14}/>{t.port}</span>
    <span><Check size={14}/>{t.account}</span>
   </div>
   {!staged && <label className="native-canary-accept">
    <input type="checkbox" checked={accepted} disabled={unavailable || busy || !hasUSA}
      onChange={e => setAccepted(e.target.checked)}/>
    <span>{t.agreement}</span>
   </label>}
   {!hasUSA && <p role="status" className="native-canary-note">{t.unavailable}</p>}
   <div className="native-canary-actions">
     <button type="button" disabled={!canStage}
       onClick={() => { setAccepted(false); onStage(); }}>
       {staged ? <Check size={16}/> : <Beaker size={16}/>}
       {staged ? t.staged : t.stage}
       {!staged && <ArrowRight size={15}/>}
     </button>
     <p>{t.caution}</p>
   </div>
 </section>;
}
