import { CalendarDays, Search, ShieldCheck, UserRoundCheck, Users } from "lucide-react";
import type { BusinessModel } from "./useBusinessWorkspace";
import { dateTime, gb, type Client } from "./shared";
import "./ClientSales.css";

type Filter = "all" | "pending" | "disabled" | "expired" | "archived" | "other";
const words = {
  en: {
    title:"Client directory", subtitle:"Search, filter and sort across every client available to your account. Results are server-scoped and paged.",
    search:"Search all clients by name or ID", status:"Status", all:"All", pending:"Pending Node Apply",
    disabled:"Disabled", expired:"Expired", archived:"Archived", other:"Other / unknown",
    total:"Loaded", noResults:"No matches on this page", empty:"No clients returned for this page",
    load:"Loading authenticated client records", page:"Page", previous:"Previous",next:"Next",
    manage:"Manage", volume:"Volume", unlimited:"Unlimited",
    slotUnlimited:"Unlimited IPs", slots:"user/IP slots", end:"Expires",
    note:"Exact status and server-side count reflect saved records, not a live Xray guarantee. Open the authenticated usage view for billable bytes.",
    selected:"Selected for management", rep:"Representative",
    plan:"Plan", allPlans:"All plans", sort:"Sort", newest:"Newest", oldest:"Oldest",
    byName:"Name", expirySoon:"Expiry first", expiryLate:"Expiry last",
    pageSize:"Per page", repFilter:"Representative ID (owner, optional)",
    matches:"Matches", loadingFilter:"Checking server filters…",
  },
  fa: {
    title:"فهرست کلاینت‌ها",subtitle:"جست‌وجو، فیلتر و مرتب‌سازی سراسری تمام کلاینت‌های مجاز حساب، با صفحه‌بندی واقعی سمت سرور.",
    search:"جست‌وجوی سراسری نام یا شناسه",status:"وضعیت",all:"همه",pending:"در انتظار اعمال نود",
    disabled:"غیرفعال",expired:"منقضی",archived:"آرشیوشده",other:"سایر / نامشخص",
    total:"بارگذاری‌شده",noResults:"در این صفحه نتیجه‌ای وجود ندارد",empty:"در این صفحه کلاینتی ثبت نشده",
    load:"در حال دریافت کلاینت‌های معتبر",page:"صفحه",previous:"قبلی",next:"بعدی",
    manage:"مدیریت",volume:"حجمی",unlimited:"نامحدود",
    slotUnlimited:"IP نامحدود",slots:"کاربر / IP",end:"انقضا",
    note:"تعداد و فیلترها از دیتابیس واقعی خوانده می‌شوند؛ وضعیت آنلاین و مصرف Xray از روی فهرست حدس زده نمی‌شود.",
    selected:"انتخاب‌شده برای مدیریت",rep:"نماینده",
    plan:"نوع پلن",allPlans:"همهٔ پلن‌ها",sort:"مرتب‌سازی",newest:"جدیدترین",oldest:"قدیمی‌ترین",
    byName:"نام",expirySoon:"نزدیک‌ترین انقضا",expiryLate:"دورترین انقضا",
    pageSize:"در هر صفحه",repFilter:"شناسه نماینده (فقط مالک، اختیاری)",
    matches:"تعداد کل",loadingFilter:"در حال دریافت فیلترهای سرور…",
  },
} as const;

function clientState(client:Client,now:number): Filter {
  if(client.archived)return "archived";
  if(client.deployment_state==="disabled")return "disabled";
  if(Number.isFinite(client.expires_unix) && client.expires_unix>0 && client.expires_unix<now)return "expired";
  if(client.deployment_state==="pending_node_apply")return "pending";
  return "other";
}

export default function ClientListExplorer({model}: {model:BusinessModel}) {
  const { clients, clientOffset, setClientOffset, lifeClientID, setLifeClientID, language, t, session,
    clientLimit, changeClientLimit, clientTotal, clientHasMore, clientSearchDraft,setClientSearchDraft,
    clientSearch,clientStateFilter,clientPlanFilter,clientSort,clientRepFilter,changeClientFilters,actionBusy }=model;
  const copy=words[language];
  const now=Date.now()/1000;
  // The server owns the filtered result set; no client-side partial-page
  // pretending to offer a global search. Debounce hides old matches.
  const safe=clients===null?null:clients.filter(c=>
    typeof c.id==="string"&&typeof c.name==="string"&&typeof c.representative_id==="string"&&
    typeof c.archived==="boolean" && Number.isFinite(c.expires_unix));
  const valid=!!safe && safe.length===clients?.length && safe.length<=clientLimit &&
    clientSearchDraft.trim()===clientSearch;
  const matches=valid?safe:[];
  const more=clientHasMore===null?(valid&&matches.length===clientLimit):clientHasMore;
  const total=valid&&clientTotal!==null?clientTotal.toLocaleString("en-US"):"—";
  return <section className="clients-explorer business-panel" aria-label="Client directory">
    <header className="clients-explorer-heading">
      <span className="clients-explorer-icon"><Users size={19}/></span>
      <div><h3>{copy.title}</h3><p>{copy.subtitle}</p></div>
      <span className="clients-explorer-count">{copy.matches}: {total}</span>
    </header>
    <div className="clients-explorer-filters">
      <label>{copy.search}<div className="clients-explorer-search">
        <Search size={15}/><input type="search" value={clientSearchDraft}
          onChange={e=>{setClientSearchDraft(e.target.value.slice(0,128));setClientOffset(0);setLifeClientID("");}}
          disabled={actionBusy} maxLength={128} placeholder={copy.search}/></div></label>
      <label>{copy.status}<select aria-label={copy.status} value={clientStateFilter}
        disabled={actionBusy} onChange={e=>changeClientFilters("state",e.target.value)}>
        {(["all","pending","disabled","expired","archived","other"] as Filter[])
          .map(k=><option key={k} value={k}>{copy[k]}</option>)}
      </select></label>
      <label>{copy.plan}<select aria-label={copy.plan} value={clientPlanFilter}
        disabled={actionBusy} onChange={e=>changeClientFilters("plan",e.target.value)}>
        <option value="all">{copy.allPlans}</option>
        <option value="volume">{copy.volume}</option><option value="unlimited">{copy.unlimited}</option>
      </select></label>
      <label>{copy.sort}<select aria-label={copy.sort} value={clientSort}
        disabled={actionBusy} onChange={e=>changeClientFilters("sort",e.target.value)}>
        <option value="newest">{copy.newest}</option>
        <option value="oldest">{copy.oldest}</option>
        <option value="name">{copy.byName}</option>
        <option value="expiry_soon">{copy.expirySoon}</option>
        <option value="expiry_late">{copy.expiryLate}</option>
      </select></label>
      <label>{copy.pageSize}<select aria-label={copy.pageSize} value={clientLimit}
        disabled={actionBusy}
        onChange={e=>changeClientLimit(Number(e.target.value) as 25|50|100|200)}>
        {[25,50,100,200].map(n=><option key={n} value={n}>{n}</option>)}
      </select></label>
      {session.user.role==="owner" && <label>{copy.repFilter}
        <input type="text" maxLength={64} value={clientRepFilter}
          onChange={e=>changeClientFilters("rep",e.target.value.trim())}
          placeholder={copy.repFilter} disabled={actionBusy}/></label>}
    </div>
    <div className="clients-explorer-list">
      {valid&&matches.map(c=>{
        const status=clientState(c,now);
        const countLabel=c.ip_count===0 && c.plan_kind==="volume"
          ?copy.slotUnlimited:c.ip_count+" "+copy.slots;
        const planLabel=c.plan_kind==="unlimited"?copy.unlimited
          :c.plan_kind==="volume"?copy.volume:copy.other;
        return <article className={"clients-explorer-row"+(c.id===lifeClientID?" is-selected":"")} key={c.id}>
          <div className="clients-explorer-main">
            <div className="clients-explorer-identity">
              <span className="clients-explorer-avatar"><UserRoundCheck size={16}/></span>
              <div><strong>{c.name}</strong><small dir="auto">{c.id}</small></div>
            </div>
            <span className={"clients-explorer-status is-"+status}>{copy[status]}</span>
          </div>
          <div className="clients-explorer-meta">
            <span><strong>{planLabel}</strong> {c.plan_kind==="volume"?gb(c.volume_bytes):""}</span>
            <span>{countLabel}</span>
            <span><CalendarDays size={13}/>{copy.end}: {dateTime(c.expires_unix,language)}</span>
            {session.user.role==="owner" && <span>{copy.rep}: <b>{c.representative_id}</b></span>}
          </div>
          <div className="clients-explorer-actions">
            <button type="button" aria-pressed={lifeClientID===c.id}
              onClick={()=>setLifeClientID(c.id)}>{copy.manage}</button>
            {lifeClientID===c.id && <small><ShieldCheck size={12}/>{copy.selected}</small>}
          </div>
        </article>;
      })}
      {valid&&matches.length===0&&<p className="clients-explorer-empty">{clientTotal===0?copy.empty:copy.noResults}</p>}
      {!valid&&<p className="clients-explorer-empty">{clientSearchDraft.trim()!==clientSearch?copy.loadingFilter:clients===null?copy.load:t.unavailable}</p>}
    </div>
    <div className="clients-explorer-pager">
      <button type="button" disabled={!valid||clientOffset===0}
        onClick={()=>setClientOffset(x=>Math.max(0,x-clientLimit))}>{copy.previous}</button>
      <strong>{copy.page}: {Math.floor(clientOffset/clientLimit)+1}</strong>
      <button type="button" disabled={!valid||!more}
        onClick={()=>setClientOffset(x=>Math.min(1_000_000,x+clientLimit))}>{copy.next}</button>
    </div>
    <p className="clients-explorer-note"><ShieldCheck size={14}/>{copy.note}</p>
  </section>;
}
