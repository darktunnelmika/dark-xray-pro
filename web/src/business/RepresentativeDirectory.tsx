import { useEffect, useState } from "react";
import { Search, Users, CreditCard, KeyRound, UserCog } from "lucide-react";
import { api } from "../session";
import { useAccountPageOffset } from "./useAccountPageOffset";
import { gb, type Representative } from "./shared";
import type { BusinessModel } from "./useBusinessWorkspace";
import "./RepresentativeDirectory.css";

type Row = Representative & { client_count: number; archived_count: number };
type Page = {
  representatives: Row[]; total: number; limit: number; offset: number;
  has_more: boolean; q: string; state: string; sort: string;
};
const terms = {
  en: {
    title:"Representative directory", hint:"Search ALL representatives — not only the loaded page.",
    search:"Search name or account ID", all:"All accounts", active:"Enabled", disabled:"Disabled",
    newest:"Newest", id:"Account ID", name:"Name", volume:"Most volume credit",
    units:"Most unlimited credit", records:"Saved clients", archived:"Archived",
    balance:"Available credit", selectCredit:"Select for credit", selectLogin:"Select for login",
    manage:"Manage account", noData:"No matching accounts", unavailable:"Directory is unavailable or unverified; actions are blocked.",
    previous:"Previous", next:"Next", page:"Results", owner:"Primary owner",
  },
  fa: {
    title:"فهرست حرفه‌ای نمایندگان", hint:"جست‌وجو در تمام نمایندگان؛ نه فقط صفحه فعلی.",
    search:"جست‌وجوی نام یا شناسه نماینده", all:"همه حساب‌ها", active:"فعال", disabled:"غیرفعال",
    newest:"جدیدترین", id:"شناسه حساب", name:"نام", volume:"بیشترین اعتبار حجمی",
    units:"بیشترین اعتبار نامحدود", records:"کلاینت ثبت‌شده", archived:"آرشیوشده",
    balance:"اعتبار موجود", selectCredit:"انتخاب برای اعتبار", selectLogin:"انتخاب برای ورود",
    manage:"مدیریت حساب", noData:"نماینده‌ای پیدا نشد", unavailable:"فهرست نمایندگان قابل تأیید نیست؛ عملیات غیرفعال است.",
    previous:"قبلی", next:"بعدی", page:"نتایج", owner:"مالک اصلی",
  },
} as const;

function validPage(raw: Page, q: string, state: string, sort: string, offset: number): boolean {
  if (!raw || !Array.isArray(raw.representatives) || raw.representatives.length>50 ||
    raw.limit!==50 || raw.offset!==offset || raw.q!==q || raw.state!==state || raw.sort!==sort ||
    !Number.isSafeInteger(raw.total) || raw.total<raw.representatives.length ||
    raw.total<0 || typeof raw.has_more!=="boolean" ||
    raw.has_more !== (offset+raw.representatives.length<raw.total)) return false;
  return raw.representatives.every(r =>
    typeof r.id==="string" && /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(r.id) &&
    typeof r.display_name==="string" && (r.kind==="owner" || r.kind==="representative") &&
    typeof r.enabled==="boolean" &&
    Number.isSafeInteger(r.client_count) && r.client_count>=0 &&
    Number.isSafeInteger(r.archived_count) && r.archived_count>=0 &&
    r.archived_count<=r.client_count &&
    Number.isFinite(r.volume_credit_bytes) && r.volume_credit_bytes>=0 &&
    Number.isFinite(r.unlimited_credit_units) && r.unlimited_credit_units>=0);
}

export default function RepresentativeDirectory({model,onManage}: {model:BusinessModel;onManage?:(id:string)=>void}) {
  const { language, session, actionBusy, refreshKey, setCreditRep, setLoginRep } = model;
  const t=terms[language];
  const [draft,setDraft]=useState("");
  const [query,setQuery]=useState("");
  const [state,setState]=useState("all");
  const [sort,setSort]=useState("id");
  const [offset,setOffset]=useAccountPageOffset(session.user.id,"rep-directory",50);
  const [page,setPage]=useState<Page|null>(null);
  const [loadedKey,setLoadedKey]=useState("");
  const [failed,setFailed]=useState(false);
  useEffect(()=>{
    const timer=window.setTimeout(()=>setQuery(draft.trim()),300);
    return ()=>window.clearTimeout(timer);
  },[draft]);
  const key=JSON.stringify([session.user.id,offset,query,state,sort]);
  const view=loadedKey===key && query===draft.trim() && !failed ? page : null;
  useEffect(()=>{
    const controller=new AbortController();
    setPage(null);setFailed(false);setLoadedKey("");
    const p=new URLSearchParams({limit:"50",offset:String(offset),q:query,state,sort});
    void api<Page>("/api/v1/workspace/representatives?"+p.toString(),
      {signal:controller.signal}).then(data=>{
        if(controller.signal.aborted)return;
        if(!validPage(data,query,state,sort,offset))throw new Error("invalid_scoped_representative_page");
        setPage(data);setLoadedKey(key);
      }).catch(()=>{
        if(controller.signal.aborted)return;
        setPage(null);setLoadedKey("");setFailed(true);
      });
    return ()=>controller.abort();
  },[key,offset,query,state,sort,refreshKey]);
  return <section className="business-panel rep-directory" aria-label="Representative directory">
    <header className="rep-dir-heading">
      <div><h3><Users size={17}/>{t.title}</h3><p>{t.hint}</p></div>
      <span className="rep-dir-total">{view ? `${t.page}: ${view.total.toLocaleString()}` : "—"}</span>
    </header>
    <div className="rep-dir-filters">
      <label className="rep-dir-search"><Search size={16}/>
        <input aria-label={t.search} placeholder={t.search} value={draft} maxLength={128}
          onChange={e=>{setDraft(e.target.value);setOffset(0);}}/>
      </label>
      <label><span>{language==="fa"?"وضعیت":"Status"}</span>
        <select aria-label="Representative status" value={state} onChange={e=>{setState(e.target.value);setOffset(0);}}>
          <option value="all">{t.all}</option><option value="enabled">{t.active}</option>
          <option value="disabled">{t.disabled}</option>
        </select>
      </label>
      <label><span>{language==="fa"?"مرتب‌سازی":"Sort"}</span>
        <select aria-label="Representative sort" value={sort} onChange={e=>{setSort(e.target.value);setOffset(0);}}>
          <option value="id">{t.id}</option><option value="newest">{t.newest}</option>
          <option value="name">{t.name}</option><option value="volume">{t.volume}</option>
          <option value="unlimited">{t.units}</option>
        </select>
      </label>
    </div>
    {failed ? <p role="alert">{t.unavailable}</p> :
      !view ? <p role="status">{model.t.loading}</p> :
      view.representatives.length===0 ? <p>{t.noData}</p> :
      <div className="rep-dir-results">
        {view.representatives.map(r=><article className="business-rep-row rep-dir-row" key={r.id}>
          <div className="rep-dir-identity"><strong>{r.display_name}</strong><code dir="ltr">{r.id}</code>
            <span>{r.kind==="owner"?t.owner:r.enabled?t.active:t.disabled}</span>
          </div>
          <div className="rep-dir-stats">
            <span>{t.records}: <b>{r.client_count.toLocaleString()}</b></span>
            <span>{t.archived}: <b>{r.archived_count.toLocaleString()}</b></span>
            <span>{t.balance}: <b>{gb(r.volume_credit_bytes)}</b> · <b>{r.unlimited_credit_units.toLocaleString()}</b> {t.units}</span>
          </div>
          {r.kind==="representative" && <div className="business-rep-select">
            <button type="button" disabled={actionBusy} onClick={()=>setCreditRep(r.id)}><CreditCard size={14}/>{t.selectCredit}</button>
            <button type="button" disabled={actionBusy} onClick={()=>setLoginRep(r.id)}><KeyRound size={14}/>{t.selectLogin}</button>
            {onManage && <button type="button" disabled={actionBusy} onClick={()=>onManage(r.id)}><UserCog size={14}/>{t.manage}</button>}
          </div>}
        </article>)}
      </div>}
    <nav className="business-pagination rep-dir-pagination" aria-label="Representative pagination">
      <button type="button" disabled={!view||offset===0}
        onClick={()=>setOffset(n=>Math.max(0,n-50))}>{t.previous}</button>
      <span>{view ? `${Math.min(offset+1,view.total)}–${Math.min(offset+view.representatives.length,view.total)} / ${view.total}` : "—"}</span>
      <button type="button" disabled={!view || !view.has_more || offset>=999950}
        onClick={()=>setOffset(n=>Math.min(1_000_000,n+50))}>{t.next}</button>
    </nav>
  </section>;
}
