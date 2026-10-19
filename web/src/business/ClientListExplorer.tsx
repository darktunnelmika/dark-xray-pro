import { useEffect, useRef, useState } from "react";
import { CalendarDays, Search, ShieldCheck, UserRoundCheck, Users, CheckSquare, Archive, AlertTriangle } from "lucide-react";
import { mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { dateTime, gb, uid, type Client } from "./shared";
import "./ClientSales.css";
import ClientBulkExtend from "./ClientBulkExtend";

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
    bulkTitle:"Bulk client action",selectClient:"Select client",selectVisible:"Select up to 50 visible",clear:"Clear",
    selectedCount:"selected",action:"Action",disableAction:"Disable",enableAction:"Enable",
    archiveAction:"Archive (keep history)",extendAction:"Renew / top up",
    reason:"Required audit reason",
    execute:"Apply to selected",confirm:"Apply this action to the selected clients? All records are checked together; financial usage is never deleted.",
    archiveConfirm:"Archive selected clients? Client IDs, UUIDs and ALL historical usage/credits stay intact. This does not remove any Xray process.",
    bulkNote:"At most 50 individually selected clients per transaction. All-or-none, immutable audit, no usage reset or direct Xray Apply.",
    missingReason:"Enter an audit reason and select permitted clients.",
    tooMany:"A maximum of 50 clients can be selected per operation.",
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
    bulkTitle:"مدیریت گروهی کلاینت‌ها",selectClient:"انتخاب کلاینت",selectVisible:"انتخاب حداکثر ۵۰ مورد نمایان",clear:"پاک‌کردن انتخاب",
    selectedCount:"انتخاب‌شده",action:"عملیات",disableAction:"غیرفعال",enableAction:"فعال‌سازی منطقی",
    archiveAction:"آرشیو (با حفظ سوابق)",extendAction:"تمدید / افزایش حجم",
    reason:"دلیل ضروری برای حسابرسی",
    execute:"اعمال به انتخاب‌شده‌ها",confirm:"این عملیات روی کلاینت‌های انتخاب‌شده اعمال شود؟ همه هم‌زمان بررسی می‌شوند و هیچ مصرف مالی حذف نمی‌شود.",
    archiveConfirm:"کلاینت‌ها آرشیو شوند؟ شناسه‌ها، UUID و تمام مصرف و اعتبار تاریخی می‌مانند؛ هیچ پردازش Xray مستقیم حذف نمی‌شود.",
    bulkNote:"حداکثر ۵۰ کلاینتِ مشخص در هر تراکنش. همه یا هیچ، با حسابرسی تغییرناپذیر و بدون صفرکردن مصرف.",
    missingReason:"دلیل حسابرسی را وارد کن و کلاینت‌های معتبر انتخاب کن.",
    tooMany:"در هر درخواست حداکثر ۵۰ کلاینت مجاز است.",
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
    clientSearch,clientStateFilter,clientPlanFilter,clientSort,clientRepFilter,changeClientFilters,actionBusy,
    runAction,setNotice }=model;
  const copy=words[language];
  const [selected,setSelected]=useState<string[]>([]);
  const [bulkAction,setBulkAction]=useState<"disable"|"enable"|"archive"|"extend">("disable");
  const [bulkReason,setBulkReason]=useState("");
  const pendingBulk=useRef<{key:string;id:string}|null>(null);
  useEffect(()=>{
    setSelected([]); pendingBulk.current=null;
  },[session.user.id,clientOffset,clientLimit,clientSearch,clientStateFilter,clientPlanFilter,clientSort,clientRepFilter]);
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
  const allowedSelected=selected.filter(id=>valid&&matches.some(c=>c.id===id&&!c.archived));
  const canSelect=valid&&!actionBusy;
  function toggle(id:string,checked:boolean){
    if(!canSelect)return;
    setSelected(current=>{
      if(!checked)return current.filter(x=>x!==id);
      if(current.includes(id))return current;
      if(current.length>=50)return current;
      return [...current,id];
    });
  }
  async function executeBulk(){
    if(bulkAction==="extend"||!canSelect||allowedSelected.length<1||allowedSelected.length>50||
      bulkReason.trim().length<1||bulkReason.trim().length>200){
      setNotice(copy.missingReason);return;
    }
    const confirmed=window.confirm(bulkAction==="archive"?copy.archiveConfirm:copy.confirm);
    if(!confirmed)return;
    const payload={client_ids:[...allowedSelected].sort(),action:bulkAction,reason:bulkReason.trim()};
    const key=JSON.stringify(payload);
    const eventId=pendingBulk.current?.key===key?pendingBulk.current.id:uid();
    pendingBulk.current={key,id:eventId};
    await runAction(async()=>{
      await mutation("/api/v1/workspace/clients/bulk/state",{event_id:eventId,...payload},session.csrf_token);
      setSelected([]);setBulkReason("");pendingBulk.current=null;
    });
  }
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
    <div className="clients-bulk" aria-label="Bulk client actions">
      <div className="clients-bulk-header">
        <strong><CheckSquare size={16}/>{copy.bulkTitle}</strong>
        <span>{allowedSelected.length} {copy.selectedCount}</span>
      </div>
      {allowedSelected.length>=50 && <p role="status">{copy.tooMany}</p>}
      <div className="clients-bulk-quick">
        <button type="button" disabled={!canSelect||!matches.some(c=>!c.archived)}
          onClick={()=>setSelected(matches.filter(c=>!c.archived).slice(0,50).map(c=>c.id))}>
          <CheckSquare size={14}/>{copy.selectVisible}</button>
        <button type="button" disabled={!canSelect||allowedSelected.length===0}
          onClick={()=>setSelected([])}>{copy.clear}</button>
      </div>
      <div className="clients-bulk-fields">
        <label>{copy.action}<select value={bulkAction} disabled={!canSelect}
          onChange={e=>setBulkAction(e.target.value as typeof bulkAction)}>
          <option value="disable">{copy.disableAction}</option>
          <option value="enable">{copy.enableAction}</option>
          <option value="archive">{copy.archiveAction}</option>
          <option value="extend">{copy.extendAction}</option>
        </select></label>
        <label>{copy.reason}<input type="text" value={bulkReason} maxLength={200}
          disabled={!canSelect} onChange={e=>setBulkReason(e.target.value)}
          placeholder={copy.reason}/></label>
      </div>
      {bulkAction==="extend"
        ? <ClientBulkExtend model={model} selected={allowedSelected} reason={bulkReason}
            onCommitted={()=>{setSelected([]);setBulkReason("");}}/>
        : <button type="button" className="clients-bulk-submit"
            disabled={!canSelect||!allowedSelected.length||!bulkReason.trim()}
            onClick={()=>void executeBulk()}>
            <Archive size={14}/>{copy.execute}</button>}
      <p><AlertTriangle size={14}/>{copy.bulkNote}</p>
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
            <label className="clients-bulk-checkbox">
              <input type="checkbox" aria-label={copy.selectClient+" "+c.id}
                checked={allowedSelected.includes(c.id)}
                disabled={!canSelect||c.archived||
                  (allowedSelected.length>=50&&!allowedSelected.includes(c.id))}
                onChange={e=>toggle(c.id,e.target.checked)}/>
            </label>
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
