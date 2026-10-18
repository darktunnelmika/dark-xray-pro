import { CalendarDays, ShieldCheck, Users, Wallet, Info } from "lucide-react";
import type { BusinessModel } from "./useBusinessWorkspace";
import { bytesFromGB, gb } from "./shared";
import "./ClientSales.css";

const words = {
  en: {
    heading: "Quick plan builder",
    intro: "Presets change draft fields only. No client is created and no credits are charged until you explicitly submit.",
    durations: "Duration presets", days: "days", slots: "User/IP slots",
    anyIp: "Unlimited IPs", preview: "Representative credit preview",
    estimated: "Estimated debit", units: "unlimited units", vol: "volume credit",
    owner: "Owner-created accounts do not debit representative credit.",
    available: "Last loaded balance", absent: "Balance unavailable for this representative in the current page.",
    low: "Last loaded credit is below this estimate. The HUB will check actual credit when saving.",
    note: "Estimate follows the HUB formula; billing is finalized atomically on save. A saved client is NOT an activated Xray connection.",
    invalid: "Enter valid duration, user limit and volume to calculate an estimate.",
  },
  fa: {
    heading: "ساخت سریع پلن",
    intro: "دکمه‌های آماده فقط مقادیر فرم را تغییر می‌دهند. تا ثبت نهایی، کلاینت و کسر اعتبار انجام نمی‌شود.",
    durations: "مدت‌های آماده", days: "روز", slots: "تعداد کاربر / IP",
    anyIp: "IP نامحدود", preview: "پیش‌نمایش کسر اعتبار نماینده",
    estimated: "کسر اعتبار تخمینی", units: "واحد نامحدود", vol: "اعتبار حجمی",
    owner: "ساخت مستقیم از حساب مالک، اعتبار نماینده را کم نمی‌کند.",
    available: "اعتبار آخرین بارگذاری", absent: "موجودی این نماینده در صفحهٔ فعلی دریافت نشده است.",
    low: "طبق آخرین موجودی دریافتی، اعتبار کافی نیست؛ HUB هنگام ثبت دوباره بررسی می‌کند.",
    note: "فرمول تخمین مطابق HUB است؛ مبلغ نهایی هنگام ثبت به‌صورت اتمیک کنترل می‌شود. ساخت کلاینت به‌معنی اتصال فعال Xray نیست.",
    invalid: "برای نمایش هزینه، مدت، کاربران و حجم معتبر وارد کن.",
  },
} as const;

export default function ClientPlanWorkbench({model}: {model:BusinessModel}) {
  const {language, clientKind, clientDuration, setClientDuration, clientIPs, setClientIPs,
    clientVolume, session, clientRep, reps, me, actionBusy} = model;
  const t=words[language];
  const days=Number(clientDuration), slots=Number(clientIPs);
  const validDays=clientDuration.trim()!=="" && Number.isSafeInteger(days) && days>=1 && days<=3650;
  const validSlots=clientIPs.trim()!=="" && Number.isSafeInteger(slots) &&
    slots>=0 && slots<=1000 && (clientKind==="volume" || slots>0);
  const volume=clientKind==="volume"?bytesFromGB(clientVolume):0;
  const validCost=validDays && validSlots && volume!==null && (clientKind!=="volume" || volume>0);
  // Mirrors the NEW PRO Store.CreateClient debit, not a renewal quote.
  const costUnits=validCost && clientKind==="unlimited" ? Math.ceil(days*slots/30):null;
  const costVolume=validCost && clientKind==="volume"?volume:null;
  const targetRep=session.user.role==="owner"?clientRep:session.user.representative_id;
  const ownerExempt=targetRep==="owner" && session.user.role==="owner";
  const account=me?.balance.id===targetRep?me.balance:reps?.find(r=>r.id===targetRep);
  const credit=clientKind==="unlimited"?account?.unlimited_credit_units:account?.volume_credit_bytes;
  const needed=clientKind==="unlimited"?costUnits:costVolume;
  const insufficient=!ownerExempt && account?.kind==="representative" &&
    needed!==null && typeof needed==="number" && typeof credit==="number" && credit<needed;
  const displayCost=!validCost?"—":clientKind==="unlimited"
    ?String(costUnits?.toLocaleString("en-US"))+" "+t.units
    :gb(costVolume??NaN)+" "+t.vol;
  return <section className="sales-workbench" aria-label="Plan builder and credit preview">
    <div className="sales-workbench-heading">
      <div><span className="sales-workbench-kicker"><ShieldCheck size={14}/>NEW PRO / OWNER & REPRESENTATIVE</span>
      <h4>{t.heading}</h4><p>{t.intro}</p></div>
      <span className="sales-workbench-plan"><Wallet size={16}/>{clientKind==="volume"?model.t.volumePlan:model.t.unlimitedPlan}</span>
    </div>
    <div className="sales-workbench-quick">
      <fieldset disabled={actionBusy} className="sales-workbench-choices">
        <legend><CalendarDays size={14}/>{t.durations}</legend>
        <div>{[30,60,90].map(n=><button type="button" key={n} aria-pressed={clientDuration===String(n)}
          onClick={()=>setClientDuration(String(n))}>{n} {t.days}</button>)}</div>
      </fieldset>
      <fieldset disabled={actionBusy} className="sales-workbench-choices">
        <legend><Users size={14}/>{t.slots}</legend>
        <div>{[1,2,3,4,5].map(n=><button type="button" key={n} aria-pressed={clientIPs===String(n)}
          onClick={()=>setClientIPs(String(n))}>{n}</button>)}
          {clientKind==="volume" && <button type="button" aria-pressed={clientIPs==="0"}
            onClick={()=>setClientIPs("0")}>{t.anyIp}</button>}</div>
      </fieldset>
    </div>
    <div className="sales-workbench-cost" aria-live="polite">
      <div className="sales-workbench-total">
        <span>{t.estimated}</span><strong dir="auto">{displayCost}</strong>
      </div>
      <div className="sales-workbench-balance">
        <span>{t.preview}</span>
        {ownerExempt?<strong>{t.owner}</strong>:account?.kind==="representative"
        ?<strong>{t.available}: {clientKind==="unlimited"
          ?String(credit?.toLocaleString("en-US"))+" "+t.units
          :gb(credit??NaN)+" "+t.vol}</strong>
        :<strong>{t.absent}</strong>}
      </div>
      <p className={insufficient?"sales-workbench-warning":""}>
        <Info size={14}/>{!validCost?t.invalid:insufficient?t.low:t.note}
      </p>
    </div>
  </section>;
}
