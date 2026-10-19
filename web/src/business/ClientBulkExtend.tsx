import { useEffect, useRef, useState } from "react";
import { AlertTriangle, CalendarClock, CheckCircle2, ShieldCheck, Wallet } from "lucide-react";
import { mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { bytesFromGB, dateTime, uid } from "./shared";
import "./ClientBulkExtend.css";

type PriceClient = {
  client_id: string; representative_id: string; plan_kind: string;
  expires_unix: number; estimated_expires_unix: number;
  volume_cost_bytes: string; unlimited_cost_units: string;
};
type PriceRep = {
  representative_id: string; owner_exempt: boolean;
  available_volume_bytes: string; available_unlimited_units: string;
  volume_debit_bytes: string; unlimited_debit_units: string; sufficient: boolean;
};
type ServerQuote = {
  quote_hash: string; quote_unix: number; count: number;
  clients: PriceClient[]; representatives: PriceRep[];
  sufficient: boolean; applied_to_xray: boolean;
};
type ExtensionIntent = {
  client_ids: string[]; additional_days: number; additional_volume_bytes: number;
  reason: string;
};

const words = {
  en: {
    title: "Renew / top up selected clients", intro: "Review the HUB-calculated price first; no charge is made until confirmation.",
    days: "Add days", volume: "Add volume (GB)", all:"Selected", quote:"Review exact charges",
    confirm:"Confirm and renew all", reason:"An audit reason is required.",
    estimate:"Estimated new expiry", client:"Client", account:"Representative",
    bytes:"Volume credit", units:"Unlimited units", available:"Available",
    cost:"Debit", owner:"Owner — no representative credit debit",
    noFunds:"Insufficient credit. None of these clients can be renewed in this batch.",
    safe:"One transaction for the entire selection. No reset, client deletion or Xray Apply.",
    expired:"Quote changed, expired or failed. Review again before applying.",
    confirmation:"Apply this audited renewal to ALL selected clients and debit only the confirmed representative credits? The operation is all-or-nothing.",
    failed:"Could not verify this quote or commit the renewal.", cancelled:"No changes were made.",
    finished:"The entire renewal batch was saved once.", loading:"Checking exact credit and expiry…",
    date:"Expiry", selectionChanged:"Selection or terms changed — review the charges again.",
  },
  fa: {
    title: "تمدید و شارژ گروهی انتخاب‌شده‌ها", intro: "اول هزینهٔ واقعی محاسبه‌شده در HUB را ببین؛ تا تأیید نهایی اعتباری کسر نمی‌شود.",
    days: "افزودن روز", volume: "افزودن حجم (گیگابایت)", all:"انتخاب‌شده",quote:"بررسی هزینهٔ دقیق",
    confirm:"تأیید و تمدید همه", reason:"دلیل حسابرسی الزامی است.",
    estimate:"انقضای جدید تخمینی", client:"کلاینت", account:"نماینده",
    bytes:"اعتبار حجمی",units:"واحد نامحدود",available:"موجودی",
    cost:"کسر اعتبار",owner:"مالک — بدون کسر اعتبار نماینده",
    noFunds:"اعتبار کافی نیست؛ هیچ‌کدام از این کلاینت‌ها تمدید نمی‌شوند.",
    safe:"یک تراکنش برای همه. بدون حذف کلاینت، صفرکردن مصرف یا فعال‌سازی Xray.",
    expired:"پیش‌نمایش تغییر کرده، منقضی شده یا نامعتبر است. دوباره بررسی کن.",
    confirmation:"تمدید همهٔ کلاینت‌های انتخاب‌شده با هزینهٔ بررسی‌شده تأیید شود؟ این عملیات همه یا هیچ است.",
    failed:"تأیید هزینه یا ثبت تمدید ناموفق بود.",cancelled:"هیچ تغییری اعمال نشد.",
    finished:"تمدید گروهی یک‌بار و کامل ثبت شد.",loading:"در حال بررسی اعتبار و انقضای واقعی…",
    date:"انقضا",selectionChanged:"انتخاب یا شرایط تغییر کرده؛ هزینه را دوباره بررسی کن.",
  },
} as const;

function units(s: string): string {
  try { return BigInt(s).toLocaleString("en-US"); } catch {return "—";}
}
function bytes(s: string): string {
  try {
    const n=BigInt(s), unit=1024n**3n;
    const whole=n/unit, fractional=(n%unit)*100n/unit;
    return whole.toLocaleString("en-US")+"."+fractional.toString().padStart(2,"0")+" GB";
  } catch {return "—";}
}
function validQuote(data: ServerQuote, ids: string[]): boolean {
  if (!data || typeof data.quote_hash!=="string" || !/^[0-9a-f]{64}$/.test(data.quote_hash) ||
      !Number.isSafeInteger(data.quote_unix) || data.quote_unix<=0 ||
      data.count!==ids.length || !Array.isArray(data.clients) ||
      data.clients.length!==ids.length || !Array.isArray(data.representatives) ||
      data.representatives.length<1 || data.representatives.length>ids.length ||
      typeof data.sufficient!=="boolean" || data.applied_to_xray!==false) return false;
  const seen=new Set<string>();
  for(const c of data.clients){
    if(!c || !ids.includes(c.client_id) || seen.has(c.client_id) ||
      typeof c.representative_id!=="string" ||
      (c.plan_kind!=="volume"&&c.plan_kind!=="unlimited") ||
      !Number.isSafeInteger(c.expires_unix)||!Number.isSafeInteger(c.estimated_expires_unix)||
      !/^\d+$/.test(c.volume_cost_bytes)||!/^\d+$/.test(c.unlimited_cost_units))return false;
    seen.add(c.client_id);
  }
  const reps=new Set<string>();
  let allSufficient=true;
  for(const r of data.representatives){
    if(!r || typeof r.representative_id!=="string" || reps.has(r.representative_id) ||
      typeof r.owner_exempt!=="boolean"||typeof r.sufficient!=="boolean"||
      !["available_volume_bytes","available_unlimited_units","volume_debit_bytes","unlimited_debit_units"]
      .every(k=>/^\d+$/.test((r as unknown as Record<string,string>)[k])))return false;
    reps.add(r.representative_id);
    allSufficient=allSufficient&&r.sufficient;
  }
  return seen.size===ids.length && data.clients.every(c=>reps.has(c.representative_id)) &&
    data.sufficient===allSufficient;
}

export default function ClientBulkExtend({model,selected,reason,onCommitted}: {
  model:BusinessModel;selected:string[];reason:string;onCommitted:()=>void;
}){
  const copy=words[model.language];
  const [days,setDays]=useState("30");
  const [volume,setVolume]=useState("0");
  const [quote,setQuote]=useState<ServerQuote|null>(null);
  const [quoteKey,setQuoteKey]=useState("");
  const [busy,setBusy]=useState(false);
  const [error,setError]=useState("");
  const pending=useRef<{key:string;eventID:string}|null>(null);
  const daysValue=Number(days);
  const volumeBytes=volume.trim()==="0"?0:bytesFromGB(volume);
  const validDays=days.trim()!==""&&Number.isSafeInteger(daysValue)&&daysValue>=0&&daysValue<=3650;
  const valid=selected.length>=1&&selected.length<=50&&validDays&&volumeBytes!==null&&
    volumeBytes>=0&&(daysValue>0||volumeBytes>0)&&reason.trim().length>0&&reason.trim().length<=200;
  const intent:ExtensionIntent={client_ids:[...selected].sort(),additional_days:daysValue,
    additional_volume_bytes:volumeBytes??0,reason:reason.trim()};
  const key=JSON.stringify([model.session.user.id,intent]);
  const verifiedQuote=valid && quoteKey===key ? quote : null;
  useEffect(()=>{
    setQuote(null);setQuoteKey("");setError("");
    // A new quote may be prepared, but a previously submitted event ID
    // only survives an identical intent when an HTTP response is ambiguous.
    if(pending.current?.key!==key)pending.current=null;
  },[key]);
  async function review(){
    if(!valid||busy||model.actionBusy){setError(copy.reason);return;}
    setBusy(true);setError("");setQuote(null);setQuoteKey("");
    try {
      const data=await mutation<ServerQuote>("/api/v1/workspace/clients/bulk/extend/quote",
        intent,model.session.csrf_token);
      if(!validQuote(data,intent.client_ids))throw Error("invalid_quote_response");
      setQuote(data);setQuoteKey(key);
    } catch {
      setError(copy.failed);setQuote(null);
    }finally{setBusy(false);}
  }
  async function commit(){
    if(!verifiedQuote||!verifiedQuote.sufficient||busy||model.actionBusy)return;
    const ok=window.confirm(copy.confirmation);
    if(!ok){setError(copy.cancelled);return;}
    const eventID=pending.current?.key===key?pending.current.eventID:uid();
    pending.current={key,eventID};
    setBusy(true);model.setActionBusy(true);setError("");
    try{
      const result=await mutation<{count:number;replayed:boolean;applied_to_xray:boolean;
        customer_traffic_changed:boolean;client_or_usage_history_deleted:boolean}>(
        "/api/v1/workspace/clients/bulk/extend/commit",
        {...intent,event_id:eventID,quote_hash:verifiedQuote.quote_hash,quote_unix:verifiedQuote.quote_unix},
        model.session.csrf_token);
      if(result.count!==selected.length||result.applied_to_xray!==false||
        result.customer_traffic_changed!==false||result.client_or_usage_history_deleted!==false)
        throw Error("unverified_commit_receipt");
      // A receipt with lost transport acknowledgement is recoverable with
      // the same ID; only confirmed successful submission clears pending.
      pending.current=null;setQuote(null);setQuoteKey("");
      model.setNotice(copy.finished);onCommitted();model.reload();
    }catch{
      setError(copy.expired);
    }finally{setBusy(false);model.setActionBusy(false);}
  }
  return <section className="bulk-renew" aria-label="Bulk client renewal">
    <header><h4><CalendarClock size={16}/>{copy.title}</h4><p>{copy.intro}</p></header>
    <div className="bulk-renew-inputs">
      <label>{copy.days}<input type="number" min={0} max={3650} step={1} value={days}
        disabled={busy||model.actionBusy} onChange={e=>setDays(e.target.value)}/></label>
      <label>{copy.volume}<input type="number" min={0} step="any" value={volume}
        disabled={busy||model.actionBusy} onChange={e=>setVolume(e.target.value)}/></label>
    </div>
    <button type="button" onClick={()=>void review()} disabled={!valid||busy||model.actionBusy}
      className="bulk-renew-review"><Wallet size={15}/>{busy?copy.loading:copy.quote}</button>
    {verifiedQuote && <div className="bulk-renew-quote" aria-label="Verified bulk renewal quote">
      <header><strong><ShieldCheck size={16}/>{verifiedQuote.count} {copy.all}</strong>
        <span>{copy.safe}</span></header>
      <div className="bulk-renew-accounts">
        {verifiedQuote.representatives.map(r=><div key={r.representative_id} className="bulk-renew-account">
          <strong>{copy.account}: {r.representative_id}</strong>
          {r.owner_exempt?<p>{copy.owner}</p>:<>
            <span>{copy.bytes}: {copy.cost} <b>{bytes(r.volume_debit_bytes)}</b> · {copy.available} {bytes(r.available_volume_bytes)}</span>
            <span>{copy.units}: {copy.cost} <b>{units(r.unlimited_debit_units)}</b> · {copy.available} {units(r.available_unlimited_units)}</span>
          </>}
          {!r.sufficient&&<p className="bulk-renew-alert"><AlertTriangle size={14}/>{copy.noFunds}</p>}
        </div>)}
      </div>
      <div className="bulk-renew-clients">
        {verifiedQuote.clients.map(c=><div key={c.client_id}>
          <strong dir="auto">{c.client_id}</strong>
          <span>{copy.date}: {dateTime(c.expires_unix,model.language)} → {copy.estimate}: {dateTime(c.estimated_expires_unix,model.language)}</span>
          <small>{copy.cost}: {c.plan_kind==="volume"?bytes(c.volume_cost_bytes):units(c.unlimited_cost_units)+" "+copy.units}</small>
        </div>)}
      </div>
      {!verifiedQuote.sufficient&&<p className="bulk-renew-alert"><AlertTriangle size={15}/>{copy.noFunds}</p>}
      <button type="button" className="bulk-renew-commit" onClick={()=>void commit()}
        disabled={!verifiedQuote.sufficient||busy||model.actionBusy}>
        <CheckCircle2 size={16}/>{copy.confirm}</button>
    </div>}
    {error&&<p className="bulk-renew-error" role="alert">{error}</p>}
    <p className="bulk-renew-safety"><ShieldCheck size={14}/>{copy.safe}</p>
  </section>;
}
