import {useEffect,useRef,useState} from "react";
import {api,mutation} from "../session";
import type {BusinessModel} from "./useBusinessWorkspace";
type Grant={representative_id:string;inbound_id:number;revision:number;enabled:boolean};
type Choice={id:number;name:string;archived?:boolean};
type Request={event_id:string;expected_revision:number;enabled:boolean;reason:string};
export default function RepresentativeServiceGrants({model,representativeID}:{model:BusinessModel;representativeID:string}){
 const fa=model.language==="fa",[choices,setChoices]=useState<Choice[]>([]),[selected,setSelected]=useState(0),[grant,setGrant]=useState<Grant|null>(null),[reason,setReason]=useState(""),[busy,setBusy]=useState(false),[error,setError]=useState(false),[review,setReview]=useState<Request|null>(null),[version,setVersion]=useState(0);
 const pending=useRef<Request|null>(null),locked=useRef(false),alive=useRef(true);
 const path="/api/v1/workspace/representatives/"+encodeURIComponent(representativeID)+"/service-grants/"+selected;
 const valid=(g:Grant)=>g&&g.representative_id===representativeID&&g.inbound_id===selected&&Number.isSafeInteger(g.revision)&&g.revision>=0&&typeof g.enabled==="boolean";
 useEffect(()=>{alive.current=true;return()=>{alive.current=false}},[]);
 useEffect(()=>{
  const c=new AbortController();setChoices([]);setSelected(0);
  void api<{inbounds:Choice[]}>("/api/v1/workspace/native-inbounds-state",{signal:c.signal}).then(d=>{
   if(!Array.isArray(d.inbounds)||d.inbounds.length>256||d.inbounds.some(x=>!Number.isSafeInteger(x.id)||x.id<=0||typeof x.name!=="string"))throw Error();
   if(!c.signal.aborted)setChoices(d.inbounds.filter(x=>!x.archived));
  }).catch(()=>{if(!c.signal.aborted)setError(true)});return()=>c.abort();
 },[representativeID]);
 useEffect(()=>{
  const c=new AbortController();setGrant(null);setReview(null);pending.current=null;setReason("");setError(false);
  if(selected)void api<Grant>(path,{signal:c.signal}).then(g=>{if(!valid(g))throw Error();if(!c.signal.aborted)setGrant(g)}).catch(()=>{if(!c.signal.aborted)setError(true)});
  return()=>c.abort();
 },[path,version]);
 async function save(){
  if(!review||locked.current||model.actionBusy)return;
  locked.current=true;setBusy(true);model.setActionBusy(true);setError(false);pending.current=review;
  try{
   const g=await mutation<Grant>(path,review,model.session.csrf_token);
   if(!valid(g)||g.revision!==review.expected_revision+1||g.enabled!==review.enabled)throw Error();
   if(alive.current){pending.current=null;setReview(null);setVersion(v=>v+1)}
  }catch{if(alive.current)setError(true)}
  finally{locked.current=false;model.setActionBusy(false);if(alive.current)setBusy(false)}
 }
 return <section className="rep-admin-stat-panel" aria-label="Representative service grants">
  <h4>{fa?"اینباندهای مجاز نماینده":"Representative service grants"}</h4>
  <p>{fa?"این مجوز برای مدیریت عضویت مشتریان است؛ لغو آن، مشتریان فعلی را حذف نمی‌کند.":"This grants membership management. Revoking it does not remove existing customers."}</p>
  <div className="rep-admin-fields"><label>{fa?"اینباند":"Inbound"}<select aria-label="Grant inbound" value={selected} disabled={busy||model.actionBusy||!!review} onChange={e=>setSelected(Number(e.target.value))}><option value={0}>{fa?"انتخاب اینباند":"Choose inbound"}</option>{choices.map(c=><option key={c.id} value={c.id}>{c.name} · #{c.id}</option>)}</select></label>
  {grant&&<p>{grant.enabled?(fa?"مجاز":"Allowed"):(fa?"غیرمجاز":"Not allowed")}</p>}
  {grant&&!review&&<><label>{fa?"دلیل تغییر":"Change reason"}<input aria-label="Grant reason" value={reason} maxLength={200} disabled={busy||model.actionBusy} onChange={e=>setReason(e.target.value)}/></label><button type="button" disabled={!reason.trim()||busy||model.actionBusy} onClick={()=>setReview({event_id:crypto.randomUUID(),expected_revision:grant.revision,enabled:!grant.enabled,reason:reason.trim()})}>{grant.enabled?(fa?"بررسی لغو مجوز":"Review revocation"):(fa?"بررسی اعطای مجوز":"Review grant")}</button></>}
  {review&&<div><p>{review.enabled?(fa?"تأیید اعطای دسترسی":"Confirm access grant"):(fa?"تأیید لغو دسترسی":"Confirm access revocation")} · {review.reason}</p><button type="button" disabled={busy||model.actionBusy} onClick={()=>void save()}>{pending.current?(fa?"پیگیری همان درخواست":"Retry same request"):(fa?"تأیید":"Confirm")}</button>{!pending.current&&<button type="button" disabled={busy} onClick={()=>setReview(null)}>{fa?"انصراف":"Cancel"}</button>}</div>}
  </div>{error&&<p role="alert">{fa?"نتیجه تأیید نشد؛ در صورت ارسال، همان درخواست را پیگیری کنید.":"Result not verified. If submitted, retry the same request."}</p>}
 </section>;
}
