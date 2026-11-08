import ClientDirectorySummary from "./ClientDirectorySummary";
import ClientInboundFilter from "./ClientInboundFilter";
import { useEffect, useRef, useState } from "react";
import { CalendarDays, Search, ShieldCheck, UserRoundCheck, Users, QrCode, CheckSquare, Archive, AlertTriangle } from "lucide-react";
import { mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import { dateTime, gb, uid, type Client } from "./shared";
import "./ClientSales.css";
import ClientBulkExtend from "./ClientBulkExtend";
import { businessGranted } from "./permissionsV9";
import ClientOwnershipTransfer, { GroupPicker } from "./ClientOwnershipTransfer";
import { loadOwnershipPending } from "./clientOwnership";

type Filter = "all" | "pending" | "disabled" | "expired" | "archived" | "other";
const words = {
  en: {
    title:"Client directory", subtitle:"Search, filter and sort across every client available to your account. Results are server-scoped and paged.",
    search:"Search by name, ID, Telegram or note", status:"Status", all:"All", pending:"Pending Node Apply",
    disabled:"Disabled", expired:"Expired", archived:"Archived", other:"Other / unknown",
    total:"Loaded", noResults:"No clients match these filters", empty:"No clients returned for this page",
    load:"Loading authenticated client records", page:"Page", previous:"Previous",next:"Next",
    manage:"Manage", volume:"Volume", unlimited:"Unlimited",
    slotUnlimited:"Unlimited IPs", slots:"user/IP slots", end:"Expires",
    note:"Exact status and server-side count reflect saved records, not a live Xray guarantee. Open the authenticated usage view for billable bytes.",
    selected:"Selected for management", rep:"Representative",
    plan:"Plan", allPlans:"All plans", sort:"Sort", newest:"Newest", oldest:"Oldest",
    byName:"Name", expirySoon:"Expiry first", expiryLate:"Expiry last",
    pageSize:"Per page", repFilter:"Representative ID (owner, optional)",
    matches:"Matches", loadingFilter:"Checking server filters…",
    moreFilters:"Group and origin",groupFilter:"Client group",allGroups:"All groups",
    origin:"Client origin",allOrigins:"All origins",nativeOrigin:"Created in this panel",legacyOrigin:"Historical import",
    previousGroups:"Previous groups",nextGroups:"Next groups",
    bulkTitle:"Bulk client action",selectClient:"Select client",selectVisible:"Select up to 50 visible",clear:"Clear",
    selectedCount:"selected",action:"Action",disableAction:"Disable",enableAction:"Enable",
    archiveAction:"Archive (keep history)",extendAction:"Renew / top up",transferAction:"Assign to representative",transferNote:"Assignment changes current management, not the original payer. Select up to 50 here, or a complete group of up to 500 in the assignment form.",
    reason:"Required audit reason",
    execute:"Apply to selected",confirm:"Apply this action to the selected clients? All records are checked together; financial usage is never deleted.",
    archiveConfirm:"Archive selected clients? Client IDs, UUIDs and ALL historical usage/credits stay intact. This does not remove any Xray process.",
    bulkNote:"At most 50 individually selected clients per transaction. All-or-none, immutable audit, no usage reset or direct Xray Apply.",
    missingReason:"Enter an audit reason and select permitted clients.",
    tooMany:"A maximum of 50 clients can be selected per operation.",
  },
  fa: {
    title:"فهرست کلاینت‌ها",subtitle:"جست‌وجو، فیلتر و مرتب‌سازی سراسری تمام کلاینت‌های مجاز حساب، با صفحه‌بندی واقعی سمت سرور.",
    search:"جست‌وجوی نام، شناسه، تلگرام یا یادداشت",status:"وضعیت",all:"همه",pending:"در انتظار اعمال نود",
    disabled:"غیرفعال",expired:"منقضی",archived:"آرشیوشده",other:"سایر / نامشخص",
    total:"بارگذاری‌شده",noResults:"کلاینتی مطابق این فیلترها پیدا نشد",empty:"در این صفحه کلاینتی ثبت نشده",
    load:"در حال دریافت کلاینت‌های معتبر",page:"صفحه",previous:"قبلی",next:"بعدی",
    manage:"مدیریت",volume:"حجمی",unlimited:"نامحدود",
    slotUnlimited:"IP نامحدود",slots:"کاربر / IP",end:"انقضا",
    note:"تعداد و فیلترها از دیتابیس واقعی خوانده می‌شوند؛ وضعیت آنلاین و مصرف Xray از روی فهرست حدس زده نمی‌شود.",
    selected:"انتخاب‌شده برای مدیریت",rep:"نماینده",
    plan:"نوع پلن",allPlans:"همهٔ پلن‌ها",sort:"مرتب‌سازی",newest:"جدیدترین",oldest:"قدیمی‌ترین",
    byName:"نام",expirySoon:"نزدیک‌ترین انقضا",expiryLate:"دورترین انقضا",
    pageSize:"در هر صفحه",repFilter:"شناسه نماینده (فقط مالک، اختیاری)",
    matches:"تعداد کل",loadingFilter:"در حال دریافت فیلترهای سرور…",
    moreFilters:"گروه و منبع",groupFilter:"گروه کلاینت",allGroups:"تمام گروه‌ها",
    origin:"منبع کلاینت",allOrigins:"تمام منابع",nativeOrigin:"ساخته‌شده در این پنل",legacyOrigin:"واردشدهٔ قدیمی",
    previousGroups:"گروه‌های قبلی",nextGroups:"گروه‌های بعدی",
    bulkTitle:"مدیریت گروهی کلاینت‌ها",selectClient:"انتخاب کلاینت",selectVisible:"انتخاب حداکثر ۵۰ مورد نمایان",clear:"پاک‌کردن انتخاب",
    selectedCount:"انتخاب‌شده",action:"عملیات",disableAction:"غیرفعال",enableAction:"فعال‌سازی منطقی",
    archiveAction:"آرشیو (با حفظ سوابق)",extendAction:"تمدید / افزایش حجم",transferAction:"واگذاری به نماینده",transferNote:"واگذاری، مدیر فعلی را تغییر می‌دهد نه پرداخت‌کنندهٔ قبلی را. اینجا تا ۵۰ کاربر انتخاب کن یا از فرم واگذاری، یک گروه کامل تا ۵۰۰ عضو را انتخاب کن.",
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

export default function ClientListExplorer({model,onCreate,onInspect}: {model:BusinessModel;onCreate?:()=>void;onInspect?:(id:string,panel:"overview"|"delivery"|"renew"|"edit")=>void}) {
  const { clients, clientOffset, setClientOffset, lifeClientID, setLifeClientID, language, t, session,
    clientLimit, changeClientLimit, clientTotal, clientHasMore, clientSearchDraft,setClientSearchDraft,
    clientSearch,clientStateFilter,clientPlanFilter,clientSort,clientRepFilter,clientGroupFilter,clientOriginFilter,clientInboundFilter,changeClientFilters,actionBusy,
    runAction,setNotice }=model;
  const copy=words[language];
  const canManageBulk=businessGranted(session.user,"manage_clients");
  const canEdit=businessGranted(session.user,"edit_clients");
  const canRenewBulk=businessGranted(session.user,"renew_clients");
  const canTransfer=session.user.role==="owner" && model.me?.capabilities?.client_ownership_v1===true;
  const [bulkMode,setBulkMode]=useState(()=>session.user.role==="owner"&&!!loadOwnershipPending(session.user.id));
  const [selected,setSelected]=useState<string[]>([]);
  const [filtersOpen,setFiltersOpen]=useState(false);
  const [extraFiltersOpen,setExtraFiltersOpen]=useState(false);
  const [bulkAction,setBulkAction]=useState<"disable"|"enable"|"archive"|"extend"|"transfer">(
    session.user.role==="owner"&&loadOwnershipPending(session.user.id)?"transfer":canManageBulk?"disable":"extend");
  const [bulkReason,setBulkReason]=useState("");
  const pendingBulk=useRef<{key:string;id:string}|null>(null);
  useEffect(()=>{
    setSelected([]); pendingBulk.current=null;
  },[session.user.id,clientOffset,clientLimit,clientSearch,clientStateFilter,clientPlanFilter,clientSort,clientRepFilter,clientGroupFilter,clientOriginFilter,clientInboundFilter]);
  const activeFilters=[clientSearchDraft.trim(),clientStateFilter!=="all",clientPlanFilter!=="all",clientRepFilter,clientGroupFilter,clientOriginFilter!=="all",clientInboundFilter].filter(Boolean).length;
  function resetFilters(){
    setClientSearchDraft("");
    changeClientFilters("state","all");changeClientFilters("plan","all");
    changeClientFilters("rep","");changeClientFilters("group","");
    changeClientFilters("origin","all");changeClientFilters("inbound","");
    setClientOffset(0);setLifeClientID("");
  }
  const [now,setNow]=useState(()=>Date.now()/1000);
  useEffect(()=>{
    const update=()=>setNow(Date.now()/1000);
    const timer=window.setInterval(update,30000);
    document.addEventListener("visibilitychange",update);
    return()=>{clearInterval(timer);document.removeEventListener("visibilitychange",update);};
  },[]);
  function remaining(expires:number){
    if(!Number.isFinite(expires)||expires<=0)return language==="fa"?"اعتبار نامشخص":"Expiry unknown";
    const seconds=expires-now;
    if(seconds<=0)return copy.expired;
    if(seconds<3600)return language==="fa"?"کمتر از یک ساعت":"Less than an hour";
    if(seconds<86400)return language==="fa"?`حدود ${Math.ceil(seconds/3600)} ساعت باقی`: `About ${Math.ceil(seconds/3600)} hours left`;
    return language==="fa"?`حدود ${Math.ceil(seconds/86400)} روز باقی`:`About ${Math.ceil(seconds/86400)} days left`;
  }
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
  const transferMode=bulkAction==="transfer" && canTransfer;
  const allowedSelected=selected.filter(id=>valid&&matches.some(c=>c.id===id&&(!c.archived||transferMode)));
  const canSelect=valid&&!actionBusy&&(canManageBulk||canRenewBulk);
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
    if(bulkAction==="extend"||bulkAction==="transfer"||!canSelect||allowedSelected.length<1||allowedSelected.length>50||
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
      {onCreate&&<button type="button" className="clients-create-button" aria-haspopup="dialog" onClick={onCreate}>{language==="fa"?"ایجاد کلاینت":"Create client"}</button>}
      <span className="clients-explorer-count">{copy.matches}: {total}</span>
    </header>
    {activeFilters>0&&<div className="clients-active-filters" role="status"><span>{language==="fa"?"جست‌وجو و فیلترهای فعال":"Active search and filters"}: {activeFilters}</span><button type="button" disabled={actionBusy} onClick={resetFilters}>{language==="fa"?"پاک‌کردن همهٔ فیلترها":"Reset all filters"}</button></div>}
    <div className="clients-explorer-filters">
      <label>{copy.search}<div className="clients-explorer-search">
        <Search size={15}/><input type="search" aria-label={copy.search} value={clientSearchDraft}
          onChange={e=>{setClientSearchDraft(e.target.value.slice(0,128));setClientOffset(0);setLifeClientID("");}}
          disabled={actionBusy} maxLength={128} placeholder={copy.search}/>
        {clientSearchDraft&&<button type="button" className="clients-search-clear" disabled={actionBusy} aria-label={language==="fa"?"پاک‌کردن جست‌وجو":"Clear search"} onClick={()=>{setClientSearchDraft("");setClientOffset(0);setLifeClientID("");}}>×</button>}</div></label>
      <label>{copy.status}<select aria-label={copy.status} value={clientStateFilter}
        disabled={actionBusy} onChange={e=>changeClientFilters("state",e.target.value)}>
        {(["all","pending","disabled","expired","archived","other"] as Filter[])
          .map(k=><option key={k} value={k}>{copy[k]}</option>)}
      </select></label>
    </div>
    <details className="clients-filter-options" onToggle={e=>setFiltersOpen(e.currentTarget.open)}><summary>{language==="fa"?"فیلترهای بیشتر":"More filters"}{clientInboundFilter?" •":""}</summary>
    <div className="clients-explorer-filters">
      {filtersOpen&&<ClientInboundFilter model={model}/>}
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
    </details>
    <details className="clients-explorer-extra-filters" onToggle={e=>setExtraFiltersOpen(e.currentTarget.open)}>
      <summary>{copy.moreFilters}{clientGroupFilter||clientOriginFilter!=="all"?" •":""}</summary>
      {extraFiltersOpen && <div className="clients-explorer-filters">
        <label>{copy.origin}<select aria-label={copy.origin} value={clientOriginFilter}
          disabled={actionBusy} onChange={e=>changeClientFilters("origin",e.target.value)}>
          <option value="all">{copy.allOrigins}</option><option value="native">{copy.nativeOrigin}</option>
          <option value="legacy">{copy.legacyOrigin}</option>
        </select></label>
        <GroupPicker key={session.user.role==="owner"?clientRepFilter:session.user.representative_id}
          language={language} representative={session.user.role==="owner"?clientRepFilter||undefined:session.user.representative_id}
          label={copy.groupFilter} value={clientGroupFilter} disabled={actionBusy} optional
          emptyLabel={copy.allGroups} previousLabel={copy.previousGroups} nextLabel={copy.nextGroups}
          onChange={id=>changeClientFilters("group",id)} />
      </div>}
    </details>
    {(canManageBulk||canRenewBulk) && <button type="button" className="clients-selection-toggle" aria-expanded={bulkMode} disabled={actionBusy} onClick={()=>{setBulkMode(x=>!x);}}>{bulkMode?(language==="fa"?"بستن انتخاب گروهی":"Close selection"):(language==="fa"?"انتخاب چند کلاینت":"Select multiple clients")}{selected.length>0?` · ${selected.length}`:""}</button>}
    {(canManageBulk||canRenewBulk) && <div hidden={!bulkMode} className="clients-bulk" aria-label="Bulk client actions">
      <div className="clients-bulk-header">
        <strong><CheckSquare size={16}/>{copy.bulkTitle}</strong>
        <span>{allowedSelected.length} {copy.selectedCount}</span>
      </div>
      {allowedSelected.length>=50 && <p role="status">{copy.tooMany}</p>}
      <div className="clients-bulk-quick">
        <button type="button" disabled={!canSelect||!matches.some(c=>!c.archived||transferMode)}
          onClick={()=>setSelected(matches.filter(c=>!c.archived||transferMode).slice(0,50).map(c=>c.id))}>
          <CheckSquare size={14}/>{copy.selectVisible}</button>
        <button type="button" disabled={!canSelect||allowedSelected.length===0}
          onClick={()=>setSelected([])}>{copy.clear}</button>
      </div>
      <div className="clients-bulk-fields">
        <label>{copy.action}<select value={bulkAction} disabled={!canSelect}
          onChange={e=>setBulkAction(e.target.value as typeof bulkAction)}>
          {canManageBulk&&<option value="disable">{copy.disableAction}</option>}
          {canManageBulk&&<option value="enable">{copy.enableAction}</option>}
          {canManageBulk&&<option value="archive">{copy.archiveAction}</option>}
          {canRenewBulk&&<option value="extend">{copy.extendAction}</option>}
          {canTransfer&&<option value="transfer">{copy.transferAction}</option>}
        </select></label>
        <label>{copy.reason}<input type="text" value={bulkReason} maxLength={200}
          disabled={!canSelect} onChange={e=>setBulkReason(e.target.value)}
          placeholder={copy.reason}/></label>
      </div>
      {bulkAction==="transfer"
        ? <ClientOwnershipTransfer key={session.user.id} model={model} selected={allowedSelected} reason={bulkReason} onCommitted={()=>{setSelected([]);setBulkReason("");}}/>
        : bulkAction==="extend"
        ? <ClientBulkExtend model={model} selected={allowedSelected} reason={bulkReason}
            onCommitted={()=>{setSelected([]);setBulkReason("");}}/>
        : <button type="button" className="clients-bulk-submit"
            disabled={!canSelect||!allowedSelected.length||!bulkReason.trim()}
            onClick={()=>void executeBulk()}>
            <Archive size={14}/>{copy.execute}</button>}
      <p><AlertTriangle size={14}/>{bulkAction==="transfer"?copy.transferNote:copy.bulkNote}</p>
    </div>}
    <div className="clients-explorer-list">
      {valid&&matches.map(c=>{
        const status=clientState(c,now);
        const rep=c.directory?.scope==="saved_membership_and_committed_usage"?c.directory.representative:undefined;
        const repName=rep?.id===c.representative_id&&typeof rep.display_name==="string"&&rep.display_name.trim()?rep.display_name:c.representative_id;
        const countLabel=c.ip_count===0 && c.plan_kind==="volume"
          ?copy.slotUnlimited:c.ip_count+" "+copy.slots;
        const planLabel=c.plan_kind==="unlimited"?copy.unlimited
          :c.plan_kind==="volume"?copy.volume:copy.other;
        return <article className={"clients-explorer-row"+(c.id===lifeClientID?" is-selected":"")} key={c.id}>
          <div className="clients-explorer-main">
            {bulkMode&&(canManageBulk||canRenewBulk)&&<label className="clients-bulk-checkbox">
              <input type="checkbox" aria-label={copy.selectClient+" "+c.id}
                checked={allowedSelected.includes(c.id)}
                disabled={!canSelect||(c.archived&&!transferMode)||
                  (allowedSelected.length>=50&&!allowedSelected.includes(c.id))}
                onChange={e=>toggle(c.id,e.target.checked)}/>
            </label>}
            <div className="clients-explorer-identity">
              <span className="clients-explorer-avatar"><UserRoundCheck size={16}/></span>
              <div><strong>{c.name}</strong><small dir="auto">{c.id}</small>{c.contact?.telegram_username&&<small dir="ltr">@{c.contact.telegram_username}</small>}{c.contact?.telegram_id&&<small dir="ltr">TG: {c.contact.telegram_id}</small>}</div>
            </div>
            <span className={"clients-explorer-status is-"+status}>{copy[status]}</span>
          </div>
          <div className="clients-explorer-details"><div className="clients-explorer-meta">
            <span><strong>{planLabel}</strong> {c.plan_kind==="volume"?gb(c.volume_bytes):""}</span>
            <span>{countLabel}</span>
            <span className="clients-expiry" title={dateTime(c.expires_unix,language)}><CalendarDays size={13}/><strong>{remaining(c.expires_unix)}</strong><span>{copy.end}: {dateTime(c.expires_unix,language)}</span></span>
            {session.user.role==="owner" && <span>{copy.rep}: <b title={c.representative_id}>{repName}</b></span>}
          </div>
          <ClientDirectorySummary client={c} language={language}/></div>
          <div className="clients-explorer-actions">
            <button type="button" aria-pressed={lifeClientID===c.id}
              disabled={actionBusy} onClick={()=>onInspect?onInspect(c.id,"overview"):setLifeClientID(c.id)}>{copy.manage}</button>
            {onInspect&&<button type="button" disabled={actionBusy} aria-label={(language==="fa"?"کانفیگ و QR ":"Configuration & QR ")+c.name} onClick={()=>onInspect(c.id,"delivery")}><QrCode size={16}/> QR</button>}
            {onInspect&&canRenewBulk&&<button type="button" disabled={actionBusy||c.archived} aria-label={(language==="fa"?"تمدید ":"Renew ")+c.name} onClick={()=>onInspect(c.id,"renew")}>{language==="fa"?"تمدید":"Renew"}</button>}
            {onInspect&&canEdit&&<button type="button" disabled={actionBusy||c.archived} aria-label={(language==="fa"?"ویرایش ":"Edit ")+c.name} onClick={()=>onInspect(c.id,"edit")}>{language==="fa"?"ویرایش":"Edit"}</button>}
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
