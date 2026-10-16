import { useEffect, useState } from "react";
import { Database, RefreshCw, ShieldAlert } from "lucide-react";
import "./ClientUsage.css";

type Lang = "en" | "fa";
type Usage = {
 client_id: string; representative_id: string; uplink_bytes: string; downlink_bytes: string;
 total_bytes: string; limit_bytes: string; plan_kind: "volume" | "unlimited";
 known: boolean; reconciliation_holds: number; exhausted: boolean; eligible: boolean;
};
type Result = { usage: Usage; scope: string; live_collection_enabled: boolean; distributed_hard_limit_verified: boolean };
type State = "loading" | "ready" | "disabled" | "denied" | "failed";
const text = {
 en: {
  title: "Recorded client usage", refresh: "Refresh usage", loading: "Reading the committed usage ledger…",
  disabled: "The usage ledger has not been enabled for this HUB.", failed: "Usage could not be verified. No zero value or active status is assumed.",
  denied: "This account no longer has permission to view this client's usage.", unknown: "Historical baseline unknown — not authorized by the quota gate.",
  held: "Unobserved usage requires reconciliation. Previously recorded bytes are preserved.", exhausted: "Volume exhausted", eligible: "Eligible in the quota ledger only", blocked: "Not eligible in the quota ledger",
  total: "Total recorded", uplink: "Uplink", downlink: "Downlink", limit: "Volume limit", remaining: "Remaining", unlimited: "Unlimited volume",
  caveat: "Committed counters only. Automatic Agent admission and distributed hard-limit enforcement are not enabled by this view.",
  bytes: "bytes", knownPartial: "Known recorded portion; not a complete historical total.",
 },
 fa: {
  title: "مصرف ثبت‌شدهٔ کلاینت", refresh: "به‌روزرسانی مصرف", loading: "در حال خواندن دفتر مصرف ثبت‌شده…",
  disabled: "دفتر مصرف در این HUB هنوز فعال نشده است.", failed: "مصرف تأیید نشد؛ مقدار صفر یا وضعیت فعال فرض نمی‌شود.",
  denied: "این حساب دیگر مجوز مشاهدهٔ مصرف این کلاینت را ندارد.", unknown: "مصرف اولیهٔ تاریخی نامشخص است؛ مجوز سهمیه صادر نمی‌شود.",
  held: "بخشی از مصرف مشاهده نشده و نیازمند تطبیق است؛ بایت‌های ثبت‌شده محفوظ‌اند.", exhausted: "حجم تمام شده", eligible: "مجاز فقط در دفتر سهمیه", blocked: "غیرمجاز در دفتر سهمیه",
  total: "مجموع ثبت‌شده", uplink: "ارسال", downlink: "دریافت", limit: "سقف حجم", remaining: "باقی‌مانده", unlimited: "حجم نامحدود",
  caveat: "این بخش شمارنده‌های ثبت‌شده را نشان می‌دهد؛ پذیرش خودکار Agent و تضمین سقف حجم بین چند نود از این صفحه فعال نمی‌شود.",
  bytes: "بایت", knownPartial: "فقط بخش ثبت‌شده مشخص است؛ مجموع تاریخی کامل نیست.",
 },
};
const max = 9223372036854775807n;
function validBytes(v: unknown): v is string {
 return typeof v === "string" && /^(0|[1-9][0-9]{0,18})$/.test(v) && BigInt(v) <= max;
}
function validate(result: Result, id: string): boolean {
 const u = result?.usage;
 if (!u || u.client_id !== id || typeof u.representative_id !== "string" || result.scope !== "committed_user_counters" ||
  typeof result.live_collection_enabled !== "boolean" || typeof result.distributed_hard_limit_verified !== "boolean" ||
  ![u.uplink_bytes,u.downlink_bytes,u.total_bytes,u.limit_bytes].every(validBytes) ||
  ![u.known,u.exhausted,u.eligible].every(v=>typeof v === "boolean") ||
  !Number.isSafeInteger(u.reconciliation_holds) || u.reconciliation_holds < 0 ||
  (u.plan_kind !== "volume" && u.plan_kind !== "unlimited")) return false;
 if (BigInt(u.uplink_bytes)+BigInt(u.downlink_bytes)!==BigInt(u.total_bytes)) return false;
 if (u.eligible && (!u.known || u.exhausted || u.reconciliation_holds !== 0)) return false;
 return true;
}
function display(value: string, language: Lang, unit: string): string {
 return BigInt(value).toLocaleString(language === "fa" ? "fa-IR" : "en-US") + " " + unit;
}
// Mounted per selected identity. Never carry another client's private values
// across a switch or turn a failed/unknown response into a zero/green card.
export default function ClientUsage({ clientID, language, refresh }: { clientID: string; language: Lang; refresh: number }) {
 const t=text[language];const [result,setResult]=useState<Result|null>(null);
 const [state,setState]=useState<State>("loading");const [reload,setReload]=useState(0);
 useEffect(()=>{
  const c=new AbortController();let active=true;const timer=setTimeout(()=>c.abort(),8000);
  setState("loading");setResult(null);
  void fetch("/api/v1/workspace/clients/"+encodeURIComponent(clientID)+"/usage",{signal:c.signal,credentials:"same-origin",cache:"no-store",headers:{Accept:"application/json"}})
   .then(async response=>{
    if(!active)return;
    if(response.status===404){setState("disabled");return;}
    if(response.status===401||response.status===403){setState("denied");return;}
    if(!response.ok)throw new Error("usage unavailable");
    const body=await response.json() as Result;if(!validate(body,clientID))throw new Error("invalid ledger response");
    if(active&&!c.signal.aborted){setResult(body);setState("ready");}
   }).catch(()=>{if(active){setResult(null);setState("failed");}})
   .finally(()=>clearTimeout(timer));
  return ()=>{active=false;clearTimeout(timer);c.abort();};
 },[clientID,refresh,reload]);
 const usage=result?.usage;
 const known=usage?.known===true;
 let remaining="—";
 if(usage&&known){remaining=usage.plan_kind==="unlimited"?t.unlimited:display((BigInt(usage.total_bytes)<BigInt(usage.limit_bytes)?BigInt(usage.limit_bytes)-BigInt(usage.total_bytes):0n).toString(),language,t.bytes);}
 return <section className="business-panel client-usage" aria-label="Recorded client usage">
  <div className="client-usage-heading"><h3><Database size={17}/>{t.title}</h3><button type="button" disabled={state==="loading"} onClick={()=>setReload(n=>n+1)}><RefreshCw size={14}/>{t.refresh}</button></div>
  {state!=="ready"&&<p role="status">{state==="loading"?t.loading:state==="disabled"?t.disabled:state==="denied"?t.denied:t.failed}</p>}
  {state==="ready"&&usage&&<>
   <p className="client-usage-state" role="status">{!known?usage.reconciliation_holds>0?t.held:t.unknown:usage.exhausted?t.exhausted:usage.eligible?t.eligible:t.blocked}</p>
   {!known&&<p>{t.knownPartial}</p>}
   <dl className="client-usage-values">
    {[[t.total,display(usage.total_bytes,language,t.bytes)],[t.uplink,display(usage.uplink_bytes,language,t.bytes)],[t.downlink,display(usage.downlink_bytes,language,t.bytes)],[t.limit,usage.plan_kind==="unlimited"?t.unlimited:display(usage.limit_bytes,language,t.bytes)],[t.remaining,remaining]].map(([label,value])=><div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}
   </dl>
  </>}
  <p className="client-usage-boundary"><ShieldAlert size={16}/>{t.caveat}</p>
 </section>;
}
