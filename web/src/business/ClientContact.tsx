import {useEffect,useRef,useState} from "react";
import {api,mutation,ApiError} from "../session";
import type {BusinessModel} from "./useBusinessWorkspace";
export type ContactInput={telegram_username:string;telegram_id:string;note:string};
export type Contact=ContactInput&{revision:number};
export const emptyContact:ContactInput={telegram_username:"",telegram_id:"",note:""};
const words={en:{title:"Client contact",username:"Telegram username",id:"Telegram numeric ID",note:"Internal note",help:"Optional private details for search. This does not link a bot account or send a message.",save:"Save contact",retry:"Retry same contact update",reload:"Reload saved details",saved:"Contact saved",failed:"Could not save. Retry the same update or reload saved details.",conflict:"Details changed elsewhere. Reload before editing.",copy:"Copy Telegram details",copied:"Copied",loading:"Loading contact…"},fa:{title:"اطلاعات تماس کلاینت",username:"نام کاربری تلگرام",id:"شناسه عددی تلگرام",note:"یادداشت داخلی",help:"اطلاعات اختیاری و خصوصی برای جست‌وجو؛ حساب ربات را متصل نمی‌کند و پیامی ارسال نمی‌شود.",save:"ذخیره اطلاعات تماس",retry:"تکرار همان تغییر",reload:"دریافت اطلاعات ذخیره‌شده",saved:"اطلاعات تماس ذخیره شد",failed:"ذخیره تأیید نشد؛ همان تغییر را تکرار کنید یا اطلاعات ذخیره‌شده را دریافت کنید.",conflict:"اطلاعات در جای دیگری تغییر کرده؛ ابتدا نسخه ذخیره‌شده را دریافت کنید.",copy:"کپی اطلاعات تلگرام",copied:"کپی شد",loading:"در حال دریافت اطلاعات تماس…"}};
export function normalizeContactDraft(v:ContactInput):ContactInput|null{
 const n={telegram_username:v.telegram_username.trim().replace(/^@/,"").toLowerCase(),telegram_id:v.telegram_id.trim(),note:v.note.replace(/\r\n/g,"\n").trim()};
 if(!/^(?:[a-z][a-z0-9_]{0,31})?$/.test(n.telegram_username)||!/^(?:[1-9][0-9]{0,15})?$/.test(n.telegram_id)||(n.telegram_id&&Number(n.telegram_id)>4503599627370495)||Array.from(n.note).length>500||/[\u0000-\u0008\u000b-\u001f\u007f]/.test(n.note))return null;
 return n;
}
export function validContact(x:unknown):x is Contact{
 const c=x as Contact;return !!c&&typeof c.telegram_username==="string"&&/^(?:[a-z][a-z0-9_]{0,31})?$/.test(c.telegram_username)&&typeof c.telegram_id==="string"&&/^(?:[1-9][0-9]{0,15})?$/.test(c.telegram_id)&&typeof c.note==="string"&&!!normalizeContactDraft(c)&&Number.isSafeInteger(c.revision)&&c.revision>=0;
}
export function ContactFields({value,onChange,language}:{value:ContactInput;onChange:(v:ContactInput)=>void;language:"en"|"fa"}){
 const t=words[language];return <div className="business-fields">
  <label>{t.username}<input dir="ltr" autoComplete="off" placeholder="@username" maxLength={33} value={value.telegram_username} onChange={e=>onChange({...value,telegram_username:e.target.value})}/></label>
  <label>{t.id}<input dir="ltr" inputMode="numeric" autoComplete="off" maxLength={16} value={value.telegram_id} onChange={e=>onChange({...value,telegram_id:e.target.value})}/></label>
  <label>{t.note}<textarea rows={2} maxLength={500} value={value.note} onChange={e=>onChange({...value,note:e.target.value})}/></label>
 </div>;
}
export function ClientContactEditor({model:m,clientID,canEdit}:{model:BusinessModel;clientID:string;canEdit:boolean}){
 const t=words[m.language];const [saved,setSaved]=useState<Contact|null>(null),[draft,setDraft]=useState<ContactInput>(emptyContact),[busy,setBusy]=useState(false),[notice,setNotice]=useState("");
 const [reload,setReload]=useState(0),[pending,setPending]=useState<(ContactInput&{event_id:string;expected_revision:number})|null>(null);
 const alive=useRef(true),sending=useRef(false);
 useEffect(()=>{let live=true;alive.current=true;setSaved(null);setNotice("");setPending(null);
  void api<{id:string;contact?:Contact}>("/api/v1/workspace/clients/"+encodeURIComponent(clientID)).then(c=>{const v=c.contact??{...emptyContact,revision:0};if(c.id!==clientID||!validContact(v))throw Error("invalid_contact");if(live){setSaved(v);setDraft(v);}}).catch(()=>{if(live)setNotice(t.failed);});
  return()=>{live=false;alive.current=false;};
 },[clientID,reload,m.session.user.id]);
 async function save(){if(!saved||!canEdit||sending.current)return;const normalized=normalizeContactDraft(draft);if(!normalized){setNotice(m.language==="fa"?"اطلاعات تلگرام یا یادداشت معتبر نیست.":"Invalid Telegram details or note.");return;}sending.current=true;setBusy(true);setNotice("");
  const input=pending??{...normalized,event_id:crypto.randomUUID(),expected_revision:saved.revision};setPending(input);
  try{const r=await mutation<{contact:Contact}>("/api/v1/workspace/clients/"+encodeURIComponent(clientID)+"/contact",input,m.session.csrf_token);if(!validContact(r.contact)||r.contact.revision!==input.expected_revision+1||r.contact.telegram_username!==input.telegram_username||r.contact.telegram_id!==input.telegram_id||r.contact.note!==input.note)throw Error("invalid_receipt");if(alive.current){setSaved(r.contact);setDraft(r.contact);setPending(null);setNotice(t.saved);m.setRefreshKey(x=>x+1);}}
  catch(e){if(alive.current)setNotice(e instanceof ApiError&&e.status===409?t.conflict:t.failed);}
  finally{sending.current=false;if(alive.current)setBusy(false);}
 }
 const canonical=normalizeContactDraft(draft);
 const changed=!!saved&&!!canonical&&(canonical.telegram_username!==saved.telegram_username||canonical.telegram_id!==saved.telegram_id||canonical.note!==saved.note);
 return <section className="business-panel" aria-label={t.title}><h3>{t.title}</h3><p>{t.help}</p>
 {saved?<><fieldset disabled={!canEdit||busy||!!pending}><ContactFields value={draft} onChange={setDraft} language={m.language}/></fieldset>
 {canEdit&&<button type="button" disabled={busy||(!pending&&!changed)} onClick={()=>void save()}>{pending?t.retry:t.save}</button>}
 {(saved.telegram_username||saved.telegram_id)&&<button type="button" onClick={()=>{void navigator.clipboard.writeText([saved.telegram_username?"@"+saved.telegram_username:"",saved.telegram_id].filter(Boolean).join(" · ")).then(()=>setNotice(t.copied)).catch(()=>setNotice(t.failed));}}>{t.copy}</button>}
 </>:<p>{t.loading}</p>}
 <button type="button" disabled={busy} onClick={()=>setReload(x=>x+1)}>{t.reload}</button>
 {notice&&<p role="status">{notice}</p>}</section>;
}
