import { useState } from "react";
import { api, ApiError, mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import type { RestoreReview } from "./darkrestoreReview";

export const restoreAPI = "/api/v1/workspace/dark-restore";
const words = {
 en:{preview:"Preview import",confirm:"Confirm import",check:"Check recorded receipt",none:"No receipt was found. Review a fresh preview before retrying.",error:"The request could not be verified.",uncertain:"The result is pending. Check the receipt before retrying.",done:"Import recorded",ready:"Clients to create",archived:"Archived in this batch",skip:"Previously promoted",blocked:"Blocked",hold:"Imported clients remain disabled until usage at cutover is verified. The import charges no credit.",why:"Review required",retry:"Prepare another preview"},
 fa:{preview:"پیش‌نمایش واردسازی",confirm:"تأیید واردسازی",check:"بررسی رسید ثبت‌شده",none:"رسیدی پیدا نشد. پیش از تلاش دوباره، پیش‌نمایش تازه را بررسی کن.",error:"درخواست قابل تأیید نبود.",uncertain:"نتیجه هنوز مشخص نیست. پیش از تلاش دوباره رسید را بررسی کن.",done:"واردسازی ثبت شد",ready:"کاربران قابل ایجاد",archived:"آرشیوی در این دسته",skip:"قبلاً ارتقایافته",blocked:"مسدود",hold:"کاربران تا تأیید مصرف هنگام انتقال غیرفعال می‌مانند. واردسازی از اعتبار نماینده کم نمی‌کند.",why:"نیازمند بررسی",retry:"ساخت پیش‌نمایش تازه"}
};
type Quote = {review_sha256:string;source_sha256:string;quote_hash:string;quote_unix:number;importable:number;archived:number;skipped_promoted:number;blocked:number;total:number;credit_changed:boolean;usage_reset:boolean;xray_changed:boolean;blocked_reasons:{reason:string;count:number}[]};
export type Receipt = {legacy_event_count:number;event_id:string; actor_user_id:string; review_sha256:string;source_sha256:string;count:number;credit_changed:boolean;usage_reset:boolean;xray_changed:boolean};
const count=(n:unknown)=>typeof n==="number"&&Number.isSafeInteger(n)&&n>=0;
type LegacyEvent={id?:number;restore_id:string; event:string; detail?:string; at:number};
export function EventHistory({receipt,language}:{receipt:Receipt;language:"en"|"fa"}) {
 const [page,setPage]=useState<{offset:number;events:LegacyEvent[]}|null>(null);
 const [busy,setBusy]=useState(false),[failed,setFailed]=useState(false);
 const fa=language==="fa";
 async function show(offset:number){
  setBusy(true);setFailed(false);
  try{
   const v=await api<{offset:number;total:number;events:LegacyEvent[]}>(restoreAPI+"/receipts/"+encodeURIComponent(receipt.event_id)+"/history/"+offset);
   if(v.offset!==offset||v.total!==receipt.legacy_event_count||!Array.isArray(v.events)||v.events.length!==Math.min(20,v.total-offset)||
      !v.events.every(e=>typeof e.restore_id==="string"&&typeof e.event==="string"&&Number.isFinite(e.at)&&
      (e.detail===undefined||typeof e.detail==="string")))throw Error("history");
   setPage({offset,events:v.events});
  }catch{setFailed(true);}finally{setBusy(false);}
 }
 if(!receipt.legacy_event_count)return null;
 return <div><button type="button" disabled={busy} onClick={()=>void show(0)}>{fa?"نمایش سابقهٔ قدیمی":"Show legacy history"} ({receipt.legacy_event_count})</button>
  {failed&&<p role="alert">{fa?"سابقه قابل تأیید نبود.":"History could not be verified."}</p>}
  {page&&<><ol start={page.offset+1}>{page.events.map((e,i)=><li key={page.offset+i}>
   <span>{e.restore_id} · {e.event} · {new Date(e.at*1000).toLocaleString(language)}</span>
   {e.detail&&<details><summary>{fa?"جزئیات":"Details"}</summary><pre>{e.detail}</pre></details>}
  </li>)}</ol>
  {page.offset>0&&<button disabled={busy} type="button" onClick={()=>void show(page.offset-20)}>{fa?"قبلی":"Previous"}</button>}
  {page.offset+page.events.length<receipt.legacy_event_count&&<button disabled={busy} type="button" onClick={()=>void show(page.offset+20)}>{fa?"بعدی":"Next"}</button>}
  </>}
 </div>;
}
type Props = {model:BusinessModel; review:RestoreReview;digest:string; mapping:Record<string,string>};
export default function DarkRestoreImport({model,review,digest,mapping}:Props) {
 const {session,language,reload}=model,t=words[language];
 const storageKey="dark-pro-restore-event:"+session.user.id+":"+digest;
 const [pending,setPending]=useState(()=>{try{return sessionStorage.getItem(storageKey)||"";}catch{return "";}} );
 const [quote,setQuote]=useState<Quote|null>(null),[result,setResult]=useState<Receipt|null>(null);
 const [busy,setBusy]=useState(false),[message,setMessage]=useState(""),[missing,setMissing]=useState(false);
 const mappings=review.groups.map(g=>({source_group_id:g.source_group_id,representative_id:mapping[g.source_group_id]}));
 const intent={review_sha256:digest,mappings};
 function accept(receipt:Receipt,id:string) {
  if(receipt.event_id!==id||receipt.actor_user_id!==session.user.id||receipt.source_sha256!==review.source_sha256||receipt.review_sha256!==digest||
    !count(receipt.count)||!count(receipt.legacy_event_count)||receipt.credit_changed!==false||receipt.usage_reset!==false||receipt.xray_changed!==false)throw Error("receipt");
  setResult(receipt);setQuote(null);setMessage("");reload();
 }
 async function preview(){
  setBusy(true);setMessage("");setQuote(null);
  try{const q=await mutation<Quote>(restoreAPI+"/quote",intent,session.csrf_token);
   if(q.review_sha256!==digest||q.source_sha256!==review.source_sha256||!/^[a-f0-9]{64}$/.test(q.quote_hash)||!count(q.quote_unix)||
    ![q.importable,q.archived,q.skipped_promoted,q.blocked].every(count)||q.archived>q.importable||q.total!==review.clients.length||
    q.importable+q.skipped_promoted+q.blocked!==q.total||q.credit_changed!==false||q.usage_reset!==false||q.xray_changed!==false||
    !Array.isArray(q.blocked_reasons)||!q.blocked_reasons.every(x=>typeof x.reason==="string"&&count(x.count)))throw Error("quote");
   setQuote(q);
  }catch{setMessage(t.error);}finally{setBusy(false);}
 }
 async function commit(){
  if(!quote||quote.blocked||pending)return;
  setBusy(true);setMessage("");let saved=false;
  try{const id="restore-"+crypto.randomUUID();
   sessionStorage.setItem(storageKey,id);if(sessionStorage.getItem(storageKey)!==id)throw Error("storage");
   saved=true;setPending(id);setMissing(false);
   const receipt=await mutation<Receipt>(restoreAPI+"/commit",{...intent,event_id:id,quote_hash:quote.quote_hash,quote_unix:quote.quote_unix},session.csrf_token,185000);
   accept(receipt,id);
  }catch{setMessage(saved?t.uncertain:t.error);}finally{setBusy(false);}
 }
 async function reconcile(){
  setBusy(true);setMissing(false);
  try{if(!/^restore-[a-f0-9-]{36}$/.test(pending))throw Error("event");
   accept(await api<Receipt>(restoreAPI+"/receipts/"+encodeURIComponent(pending)),pending);
  }catch(e){const absent=e instanceof ApiError&&e.status===404;setMissing(absent);setMessage(absent?t.none:t.uncertain);}
  finally{setBusy(false);}
 }
 return <section className="darkrestore-commit" aria-live="polite"><p>{t.hold}</p>
  {result?<><p role="status"><strong>{t.done}</strong> · {result.count.toLocaleString(language)} <code>{result.event_id}</code></p><EventHistory receipt={result} language={language}/></>:
   pending?<><button type="button" disabled={busy} onClick={()=>void reconcile()}>{t.check}</button>
   {missing&&<button type="button" onClick={()=>{sessionStorage.removeItem(storageKey);setPending("");setMissing(false);setQuote(null);setMessage("");}}>{t.retry}</button>}</>:
   <><button type="button" disabled={busy||mappings.some(m=>!m.representative_id)} onClick={()=>void preview()}>{t.preview}</button>
    {quote&&<><dl>{([[t.ready,quote.importable],[t.archived,quote.archived],[t.skip,quote.skipped_promoted],[t.blocked,quote.blocked]] as const).map(([label,n])=><div key={label}><dt>{label}</dt><dd>{n.toLocaleString(language)}</dd></div>)}</dl>
     {quote.blocked_reasons.map(x=><p key={x.reason}>{t.why} · {x.reason} ({x.count})</p>)}
     <button type="button" disabled={busy||quote.blocked>0} onClick={()=>void commit()}>{t.confirm}</button></>}
   </>}
  {message&&<p role="status">{message}</p>}
 </section>;
}
