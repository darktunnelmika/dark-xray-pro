import { useEffect, useState } from "react";
import { CalendarDays, Search, ShieldCheck, UserRoundCheck, Users } from "lucide-react";
import type { BusinessModel } from "./useBusinessWorkspace";
import { dateTime, gb, type Client } from "./shared";
import "./ClientSales.css";

type Filter = "all" | "pending" | "disabled" | "expired" | "archived" | "other";
const words = {
  en: {
    title:"Client directory", subtitle:"Only the current authenticated page (up to 50). Search and status filters are NOT fleet-wide.",
    search:"Search name or ID on this page", status:"Filter page", all:"All", pending:"Pending Node Apply",
    disabled:"Disabled", expired:"Expired", archived:"Archived", other:"Other / unknown",
    total:"Loaded", noResults:"No matches on this page", empty:"No clients returned for this page",
    load:"Loading authenticated client records", page:"Page", previous:"Previous",next:"Next",
    manage:"Manage", volume:"Volume", unlimited:"Unlimited",
    slotUnlimited:"Unlimited IPs", slots:"user/IP slots", end:"Expires",
    note:"No online status or billed traffic inferred here. Open a client to read authoritative usage. Selecting Manage does not activate Xray.",
    selected:"Selected for management", rep:"Representative",
  },
  fa: {
    title:"فهرست کلاینت‌ها",subtitle:"فقط همین صفحهٔ احرازشده (حداکثر ۵۰ نفر). جست‌وجو و فیلتر وضعیت، کل سرور را پوشش نمی‌دهد.",
    search:"جست‌وجوی نام یا شناسه در همین صفحه",status:"فیلتر صفحه",all:"همه",pending:"در انتظار اعمال نود",
    disabled:"غیرفعال",expired:"منقضی",archived:"آرشیوشده",other:"سایر / نامشخص",
    total:"بارگذاری‌شده",noResults:"در این صفحه نتیجه‌ای وجود ندارد",empty:"در این صفحه کلاینتی ثبت نشده",
    load:"در حال دریافت کلاینت‌های معتبر",page:"صفحه",previous:"قبلی",next:"بعدی",
    manage:"مدیریت",volume:"حجمی",unlimited:"نامحدود",
    slotUnlimited:"IP نامحدود",slots:"کاربر / IP",end:"انقضا",
    note:"وضعیت آنلاین یا مصرف مالی از این لیست حدس زده نمی‌شود. برای مصرف معتبر، کلاینت را باز کن. «مدیریت» Xray را فعال نمی‌کند.",
    selected:"انتخاب‌شده برای مدیریت",rep:"نماینده",
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
  const { clients, clientOffset, setClientOffset, lifeClientID, setLifeClientID, language, t, session }=model;
  const copy=words[language];
  const [query,setQuery]=useState("");
  const [filter,setFilter]=useState<Filter>("all");
  useEffect(()=>{setQuery("");setFilter("all");},[clientOffset,session.user.id]);
  const safe=clients===null?null:clients.filter(c=>
    typeof c.id==="string"&&typeof c.name==="string"&&typeof c.representative_id==="string"&&
    typeof c.archived==="boolean" && Number.isFinite(c.expires_unix));
  // An incomplete/malformed API response must never be treated as a full
  // verified page; the mutation-capable lifecycle is independently guarded.
  const valid=!!safe && safe.length===clients?.length && safe.length<=50;
  const now=Date.now()/1000;
  const needle=query.trim().toLocaleLowerCase();
  const matches=(valid?safe:[]).filter(c=>
    (filter==="all"||clientState(c,now)===filter) &&
    (!needle||c.id.toLocaleLowerCase().includes(needle)||c.name.toLocaleLowerCase().includes(needle)));
  return <section className="clients-explorer business-panel" aria-label="Client directory">
    <header className="clients-explorer-heading">
      <span className="clients-explorer-icon"><Users size={19}/></span>
      <div><h3>{copy.title}</h3><p>{copy.subtitle}</p></div>
      <span className="clients-explorer-count">{copy.total}: {valid?clients!.length:"—"}</span>
    </header>
    <div className="clients-explorer-filters">
      <label>{copy.search}<div className="clients-explorer-search">
        <Search size={15}/><input type="search" value={query}
          onChange={e=>setQuery(e.target.value)} disabled={!valid}
          placeholder={copy.search}/></div></label>
      <label>{copy.status}<select aria-label={copy.status} value={filter}
        disabled={!valid} onChange={e=>setFilter(e.target.value as Filter)}>
        {(["all","pending","disabled","expired","archived","other"] as Filter[])
          .map(k=><option key={k} value={k}>{copy[k]}</option>)}
      </select></label>
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
      {valid&&matches.length===0&&<p className="clients-explorer-empty">{clients!.length===0?copy.empty:copy.noResults}</p>}
      {!valid&&<p className="clients-explorer-empty">{clients===null?copy.load:t.unavailable}</p>}
    </div>
    <div className="clients-explorer-pager">
      <button type="button" disabled={!valid||clientOffset===0}
        onClick={()=>setClientOffset(x=>Math.max(0,x-50))}>{copy.previous}</button>
      <strong>{copy.page}: {Math.floor(clientOffset/50)+1}</strong>
      <button type="button" disabled={!valid||clients!.length<50}
        onClick={()=>setClientOffset(x=>x+50)}>{copy.next}</button>
    </div>
    <p className="clients-explorer-note"><ShieldCheck size={14}/>{copy.note}</p>
  </section>;
}
