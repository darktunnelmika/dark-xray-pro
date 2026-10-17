import { useEffect, useState } from "react";
import { Check, CheckSquare, Search, ShieldCheck, UserMinus, Users, UserPlus } from "lucide-react";
import { api, mutation, type Language, type Session } from "../session";
import { useAccountPageOffset } from "./useAccountPageOffset";
import type { ClientGroup } from "./shared";
import "./GroupMemberManager.css";

type ListedClient = {
 id: string; name: string; representative_id: string;
 archived: boolean; deployment_state: string;
};
type View = { groupID: string; clientOffset: number; memberOffset: number;
 clients: ListedClient[]; members: string[] };
const words = {
 en: {
  title: "Group members", subtitle: "Select real clients from this representative, without typing customer IDs.",
  pending: "Loading verified members", failed: "Membership data unavailable. No change can be submitted.",
  search: "Search this page by name or ID", choose: "Select", selected: "selected",
  bulk: "Move selected clients to this group", moveConfirm: "Move the selected clients to this group? Clients already in another group will be moved; no financial history will change.",
  members: "Current members", none: "No clients on this page", empty: "No members on this page",
  previous: "Previous", next: "Next", selectAll: "Select visible", clear: "Clear selection",
  remove: "Remove membership", removeConfirm: "Remove this client from this group? The client, UUID and all usage remain intact.",
  saved: "Membership saved. Clients and historical traffic are retained.",
  removed: "Membership removed. Client and billing history are untouched.",
  guard: "Group ownership is enforced by the HUB; this page never edits credits, subscriptions or usage.",
  archived: "Archived client",
 },
 fa: {
  title: "مدیریت اعضای گروه", subtitle: "کلاینت‌های همان نماینده را انتخاب کن؛ نیازی به تایپ شناسه نیست.",
  pending: "در حال دریافت اعضای معتبر", failed: "دریافت اطلاعات عضویت ناموفق بود؛ اعمال تغییر مسدود است.",
  search: "جست‌وجو در این صفحه با نام یا شناسه", choose: "انتخاب", selected: "انتخاب‌شده",
  bulk: "انتقال انتخاب‌شده‌ها به گروه", moveConfirm: "کلاینت‌های انتخابی به این گروه منتقل شوند؟ اگر عضو گروه دیگری باشند منتقل می‌شوند؛ تاریخچهٔ مالی تغییر نمی‌کند.",
  members: "اعضای فعلی گروه", none: "در این صفحه کلاینتی نیست", empty: "در این صفحه عضوی نیست",
  previous: "قبلی", next: "بعدی", selectAll: "انتخاب موارد نمایان", clear: "پاک‌کردن انتخاب‌ها",
  remove: "جداسازی عضو", removeConfirm: "این کلاینت از گروه جدا شود؟ خود کلاینت، UUID و تمام مصرف او باقی می‌ماند.",
  saved: "عضویت ذخیره شد؛ کلاینت‌ها و ترافیک تاریخی محفوظ‌اند.",
  removed: "عضویت حذف شد؛ سوابق مصرف و اطلاعات مشتری دست‌نخورده‌اند.",
  guard: "مالکیت گروه در HUB بررسی می‌شود؛ این صفحه به اعتبار، اشتراک یا مصرف دست نمی‌زند.",
  archived: "کلاینت آرشیوشده",
 },
} as const;

export default function GroupMemberManager({ group, language, session, refresh }: {
 group: ClientGroup; language: Language; session: Session; refresh: () => void;
}) {
 const t=words[language];
 const [clientOffset,setClientOffset]=useAccountPageOffset(session.user.id,"group-picker:"+group.id);
 const [memberOffset,setMemberOffset]=useAccountPageOffset(session.user.id,"group-members:"+group.id);
 const [view,setView]=useState<View|null>(null);
 const [loading,setLoading]=useState(true);
 const [failure,setFailure]=useState(false);
 const [saving,setSaving]=useState(false);
 const [selected,setSelected]=useState<string[]>([]);
 const [query,setQuery]=useState("");
 const [notice,setNotice]=useState("");
 const [generation,setGeneration]=useState(0);
 const path="/api/v1/workspace/groups/"+encodeURIComponent(group.id);
 useEffect(()=>{
  const controller=new AbortController();
  setLoading(true);
  setFailure(false);
  setSelected([]);
  setView(null);
  // The server scopes group membership and candidate clients independently.
  // Owners request the group representative explicitly; representatives can
  // only retrieve their own accounts on both endpoints.
  const candidatePath="/api/v1/workspace/clients?limit=50&offset="+clientOffset+
    "&representative_id="+encodeURIComponent(group.representative_id);
  void Promise.all([
   api<{clients:ListedClient[]}>(candidatePath,{signal:controller.signal}),
   api<{client_ids:string[]}>(path+"/members?limit=50&offset="+memberOffset,{signal:controller.signal}),
  ]).then(([a,b])=>{
   if(controller.signal.aborted)return;
   if(!Array.isArray(a.clients)||a.clients.length>50||!Array.isArray(b.client_ids)||
      b.client_ids.length>50||a.clients.some(c=>typeof c.id!=="string"||
        typeof c.name!=="string"||c.representative_id!==group.representative_id||
        typeof c.archived!=="boolean")||
      b.client_ids.some(x=>typeof x!=="string"||!/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(x))){
     throw new Error("invalid_group_membership_response");
   }
   setView({groupID:group.id,clientOffset,memberOffset,clients:a.clients,members:b.client_ids});
   setLoading(false);
  }).catch(()=>{
   if(controller.signal.aborted)return;
   setView(null);
   setFailure(true);
   setLoading(false);
  });
  return ()=>controller.abort();
 },[group.id,group.representative_id,clientOffset,memberOffset,generation,path]);
 const validView=!failure&&!loading&&view?.groupID===group.id &&
   view.clientOffset===clientOffset && view.memberOffset===memberOffset;
 const ready=validView&&!saving;
 const visible=view?.clients.filter(c=>(
   c.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())||
   c.id.toLowerCase().includes(query.trim().toLowerCase())
 )) ?? [];
 const chosen=selected.filter(id=>validView&&view?.clients.some(c=>c.id===id && !c.archived));
 async function move(){
  if(!ready||!chosen.length||chosen.length>50||!window.confirm(t.moveConfirm))return;
  setSaving(true);setNotice("");
  try{
   await mutation(path+"/members",{client_ids:chosen},session.csrf_token);
   setSelected([]);setGeneration(x=>x+1);setNotice(t.saved);
   refresh(); // refresh only the group row count; no financial mutation
  }catch{setView(null);setFailure(true);setNotice(t.failed);}
  finally{setSaving(false);}
 }
 async function remove(id:string){
  if(!ready||!view?.members.includes(id)||!window.confirm(t.removeConfirm))return;
  setSaving(true);setNotice("");
  try{
   await api(path+"/members/"+encodeURIComponent(id),{
    method:"DELETE",headers:{"Content-Type":"application/json","X-CSRF-Token":session.csrf_token},
    body:"{}",
   });
   setGeneration(x=>x+1);setNotice(t.removed);refresh();
  }catch{setView(null);setFailure(true);setNotice(t.failed);}
  finally{setSaving(false);}
 }
 return <section className="group-members-manager" aria-label="Group membership manager">
  <header className="group-members-header">
   <span><Users size={19}/></span>
   <div><h3>{t.title}: {group.name}</h3><p>{t.subtitle}</p></div>
   <strong>{group.member_count.toLocaleString()} {language==="fa"?"عضو":"members"}</strong>
  </header>
  <p className="group-members-security"><ShieldCheck size={14}/>{t.guard}</p>
  {loading&&<p role="status">{t.pending}</p>}
  {failure&&<p role="alert" className="group-members-warning">{t.failed}</p>}
  <div className="group-members-columns">
   <div className="group-members-column">
    <h4><UserPlus size={16}/>{t.choose}</h4>
    <label>{t.search}<div className="group-members-search"><Search size={15}/>
     <input type="search" value={query} onChange={e=>setQuery(e.target.value)}
      disabled={!validView||saving} placeholder={t.search}/>
    </div></label>
    <div className="group-members-choices">
     {validView&&visible.map(c=><label key={c.id} className="group-members-choice">
      <input type="checkbox" checked={chosen.includes(c.id)}
       disabled={!ready||c.archived}
       onChange={e=>setSelected(current=>e.target.checked
        ?[...current.filter(id=>id!==c.id),c.id]:current.filter(id=>id!==c.id))}/>
      <span><strong>{c.name}</strong><small dir="auto">{c.id}{c.archived?" · "+t.archived:""}</small></span>
     </label>)}
     {validView&&!visible.length&&<p className="group-members-empty">{t.none}</p>}
    </div>
    <div className="group-members-controls">
     <button type="button" disabled={!ready||!visible.some(c=>!c.archived)}
      onClick={()=>setSelected(visible.filter(c=>!c.archived).map(c=>c.id))}><CheckSquare size={14}/>{t.selectAll}</button>
     <button type="button" disabled={!ready||!chosen.length}
      onClick={()=>setSelected([])}>{t.clear}</button>
     <span>{chosen.length} {t.selected}</span>
    </div>
    <button type="button" className="group-members-primary" disabled={!ready||!chosen.length}
     onClick={()=>void move()}><Check size={15}/>{t.bulk}</button>
    <div className="group-members-pages">
     <button type="button" disabled={!ready||clientOffset===0}
      onClick={()=>setClientOffset(x=>Math.max(0,x-50))}>{t.previous}</button>
     <span>{Math.floor(clientOffset/50)+1}</span>
     <button type="button" disabled={!ready||(view?.clients.length??0)<50}
      onClick={()=>setClientOffset(x=>x+50)}>{t.next}</button>
    </div>
   </div>
   <div className="group-members-column">
    <h4><UserMinus size={16}/>{t.members}</h4>
    <div className="group-members-existing">
     {validView&&view.members.map(id=><div className="group-members-existing-row" key={id}>
       <span dir="auto">{id}</span>
       <button type="button" disabled={!ready} onClick={()=>void remove(id)}>{t.remove}</button>
     </div>)}
     {validView&&view.members.length===0&&<p className="group-members-empty">{t.empty}</p>}
    </div>
    <div className="group-members-pages">
     <button type="button" disabled={!ready||memberOffset===0}
      onClick={()=>setMemberOffset(x=>Math.max(0,x-50))}>{t.previous}</button>
     <span>{Math.floor(memberOffset/50)+1}</span>
     <button type="button" disabled={!ready||(view?.members.length??0)<50}
      onClick={()=>setMemberOffset(x=>x+50)}>{t.next}</button>
    </div>
   </div>
  </div>
  {notice&&<p className="group-members-notice" role="status">{notice}</p>}
 </section>;
}
