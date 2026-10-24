import { useEffect, useRef, useState } from "react";
import { api, ApiError, type Language, type Session } from "../session";
import { readSubscription, readSubscriptionSecret, readTokenReceipt, readTokenAttempt, subscriptionEventID, subscriptionPath, type SubscriptionView, type TokenAction, type TokenAttempt } from "../labSubscriptions";
import "../LabSubscriptions.css";

const words={
 en:{title:"LAB subscription",refresh:"Refresh subscription",loading:"Checking subscription access…",disabled:"LAB subscription delivery is disabled on this HUB.",failed:"Subscription state could not be verified. Refresh before another action.",denied:"Access to this client's subscription is no longer authorized.",absent:"No subscription token issued",active:"Subscription token active",revoked:"Subscription token revoked",issue:"Issue subscription token",reveal:"Show subscription URL",hide:"Hide URL",rotate:"Replace subscription token",revoke:"Revoke subscription token",confirmRotate:"Replace the subscription token? The previous URL will stop working. Already imported connections keep their existing UUID.",confirmRevoke:"Revoke subscription fetch access? Already imported connections and their UUID are unchanged.",saved:"Token operation recorded.",unknown:"The operation response was not confirmed. Retry the same request or refresh its current state.",retry:"Retry exact token operation",discard:"Discard local retry",url:"Subscription URL",format:"Subscription format",copy:"Copy URL",copied:"URL copied",noSnapshot:"No LAB snapshot has been recorded for this client.",current:"LAB snapshot is within its checked validity window",historical:"Retained LAB snapshot · current delivery is not confirmed",until:"Original deadline (Tehran)",routes:"Routes",scope:"This stage delivers the verified local LAB routes for one transfer. Every fetch rechecks eligibility and the original health deadline. Public subscription delivery is not enabled.",boundary:"Changing or revoking this token controls subscription downloads. It does not change the connection UUID or disconnect an already imported configuration.",secret:"The URL grants subscription access. It is hidden when this view closes, the tab is hidden, or after one minute."},
 fa:{title:"اشتراک آزمایشی LAB",refresh:"به‌روزرسانی اشتراک",loading:"در حال بررسی دسترسی اشتراک…",disabled:"دریافت اشتراک آزمایشی در این HUB فعال نیست.",failed:"وضعیت اشتراک تأیید نشد؛ پیش از اقدام بعدی آن را به‌روزرسانی کنید.",denied:"مجوز دسترسی به اشتراک این کلاینت دیگر معتبر نیست.",absent:"هنوز توکن اشتراک صادر نشده",active:"توکن دریافت اشتراک فعال است",revoked:"توکن دریافت اشتراک لغو شده",issue:"صدور توکن اشتراک",reveal:"نمایش لینک اشتراک",hide:"پنهان‌کردن لینک",rotate:"تعویض توکن اشتراک",revoke:"لغو توکن اشتراک",confirmRotate:"توکن اشتراک تعویض شود؟ لینک قبلی از کار می‌افتد. اتصال‌های قبلاً واردشده همان UUID را حفظ می‌کنند.",confirmRevoke:"دسترسی دریافت اشتراک لغو شود؟ اتصال‌های قبلاً واردشده و UUID آن‌ها تغییری نمی‌کنند.",saved:"عملیات توکن ثبت شد.",unknown:"پاسخ عملیات تأیید نشد؛ همان درخواست را دوباره ارسال کنید یا وضعیت فعلی را به‌روزرسانی کنید.",retry:"تکرار همان عملیات توکن",discard:"حذف تکرار محلی",url:"لینک اشتراک",format:"فرمت اشتراک",copy:"کپی لینک",copied:"لینک کپی شد",noSnapshot:"هنوز نسخهٔ اشتراک آزمایشی برای این کلاینت ثبت نشده است.",current:"نسخهٔ آزمایشی در بازهٔ اعتبار بررسی‌شده قرار دارد",historical:"نسخهٔ آزمایشی ثبت‌شده؛ دریافت فعلی تأیید نیست",until:"پایان اعتبار اولیه (تهران)",routes:"مسیرها",scope:"این مرحله مسیرهای محلی تأییدشدهٔ LAB را برای یک انتقال تحویل می‌دهد. هر دریافت، مجاز بودن کلاینت و مهلت اولیهٔ سلامت را دوباره بررسی می‌کند. اشتراک عمومی فعال نشده است.",boundary:"تعویض یا لغو این توکن، دریافت اشتراک را کنترل می‌کند؛ UUID اتصال را تغییر نمی‌دهد و کانفیگ قبلاً واردشده را قطع نمی‌کند.",secret:"این لینک مجوز دریافت اشتراک است؛ با بستن این بخش، پنهان‌شدن تب یا پس از یک دقیقه پنهان می‌شود."}
};

export default function ClientSubscription({clientID,language,session,canManage,refresh}:{clientID:string;language:Language;session:Session;canManage:boolean;refresh:number}) {
 const t=words[language],base=subscriptionPath(clientID);
 const storageKey="dark-pro-subscription-attempt:"+session.user.id+":"+clientID;
 const [view,setView]=useState<SubscriptionView|null>(null),[status,setStatus]=useState("loading"),[reload,setReload]=useState(0);
 const [busy,setBusy]=useState(false),[attempt,setAttempt]=useState<TokenAttempt|null>(null),[secret,setSecret]=useState("");
 const [format,setFormat]=useState("base64"),[notice,setNotice]=useState(""),[copied,setCopied]=useState(false),[,tick]=useState(0);
 const [storageFailed,setStorageFailed]=useState(false);
 const lifetime=useRef<AbortController|null>(null),reading=useRef<AbortController|null>(null),sending=useRef(false),checked=useRef(0);
 const secretEpoch=useRef(0);
 function remember(a:TokenAttempt|null){try{if(a)sessionStorage.setItem(storageKey,JSON.stringify(a));else sessionStorage.removeItem(storageKey);setAttempt(a);setStorageFailed(false);return true;}catch{setStorageFailed(true);setNotice(t.failed);return false;}}
 useEffect(()=>{const c=new AbortController();lifetime.current=c;secretEpoch.current++;setSecret("");setBusy(false);sending.current=false;try{setAttempt(readTokenAttempt(sessionStorage.getItem(storageKey)));}catch{setStorageFailed(true);}return()=>c.abort();},[storageKey,session.csrf_token,canManage]);
 useEffect(()=>{
  const c=new AbortController();reading.current=c;secretEpoch.current++;setView(null);setSecret("");setCopied(false);setStatus("loading");const started=performance.now();
  void api<unknown>(base,{signal:c.signal}).then(raw=>{const v=readSubscription(raw,clientID);if(!c.signal.aborted){checked.current=started;setView(v);setStatus("ready");}})
   .catch(e=>{if(!c.signal.aborted){setView(null);setStatus(e instanceof ApiError&&(e.status===401||e.status===403)?"denied":"failed");}});
  return()=>c.abort();
 },[base,clientID,refresh,reload,session.csrf_token,canManage]);
 useEffect(()=>{const timer=setInterval(()=>tick(n=>n+1),500);return()=>clearInterval(timer);},[]);
 useEffect(()=>{const hide=()=>{if(document.hidden){secretEpoch.current++;setSecret("");setCopied(false);}};document.addEventListener("visibilitychange",hide);return()=>document.removeEventListener("visibilitychange",hide);},[]);
 useEffect(()=>{if(!secret)return;const timer=setTimeout(()=>setSecret(""),60000);return()=>clearTimeout(timer);},[secret]);
 const allowed=canManage&&status==="ready"&&view?.enabled===true&&!busy&&!storageFailed;
 async function send(a:TokenAttempt) {
  const c=lifetime.current;if(!allowed||sending.current||!c||c.signal.aborted)return;
  if(!remember(a))return;
  sending.current=true;setBusy(true);setSecret("");setCopied(false);setNotice("");reading.current?.abort();
  try {
   const raw=await api<unknown>(base+"/"+a.action,{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":session.csrf_token},body:JSON.stringify(a.body),signal:c.signal});
   readTokenReceipt(raw,clientID,a);if(!c.signal.aborted){remember(null);setNotice(t.saved);}
  } catch { if(!c.signal.aborted)setNotice(t.unknown); }
  finally {if(c===lifetime.current){sending.current=false;if(!c.signal.aborted){setBusy(false);setReload(n=>n+1);}}}
 }
 function change(action:TokenAction) {
  if(!allowed||!view?.enabled||attempt)return;
  if(action!=="issue"&&!window.confirm(action==="rotate"?t.confirmRotate:t.confirmRevoke))return;
  void send({action,body:{event_id:subscriptionEventID(),expected_generation:view.generation}});
 }
 async function reveal() {
  const c=lifetime.current;if(!allowed||attempt||!view?.enabled||view.state!=="active"||sending.current||!c||c.signal.aborted)return;
  const epoch=++secretEpoch.current;
  sending.current=true;setBusy(true);setSecret("");setCopied(false);setNotice("");
  try {
   const raw=await api<unknown>(base+"/reveal",{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":session.csrf_token},body:JSON.stringify({expected_generation:view.generation}),signal:c.signal});
   const path=readSubscriptionSecret(raw,clientID,view.generation);if(!c.signal.aborted&&epoch===secretEpoch.current&&!document.hidden)setSecret(path);
  }catch{if(!c.signal.aborted&&epoch===secretEpoch.current){setNotice(t.failed);setReload(n=>n+1);}}
  finally{if(c===lifetime.current){sending.current=false;if(!c.signal.aborted)setBusy(false);}}
 }
 const snapshot=view?.enabled?view.snapshot:undefined;
 const scopeText=snapshot?.scope==="lab_initial_apply"?(language==="fa"?"این نسخه خروجی محلی همان اولین اجرای بررسی‌شدهٔ اینباند آماده‌شده را در LAB تحویل می‌دهد. هر دریافت، اثبات سلامت، اجرای فعلی، مجاز بودن کلاینت و مهلت اولیه را دوباره بررسی می‌کند.":"This snapshot delivers local LAB output from the verified first Apply of the prepared Inbound. Every fetch rechecks its health proof, current execution, client eligibility and original deadline."):snapshot?.consumer_manifest_id?(language==="fa"?"این نسخه مسیرهای مصرف‌کنندهٔ یک نگاشت مجاز در LAB ایزوله را تحویل می‌دهد. هر دریافت، همان نگاشت، مجاز بودن کلاینت و مهلت اولیهٔ سلامت را دوباره بررسی می‌کند. دسترسی عمومی تأیید نشده است.":"This snapshot delivers the consumer routes of one approved mapping in the isolated LAB. Every fetch rechecks that mapping, client eligibility and the original health deadline. Public availability remains unverified."):view?.scope==="lab_initial_apply"?(language==="fa"?"خروجی LAB فقط پس از ثبت نسخه با اثبات سلامت معتبر قابل دریافت است. فعال‌بودن توکن به‌تنهایی به معنی آماده‌بودن خروجی نیست.":"LAB output is available only after a snapshot is recorded with current health proof. An active token alone does not mean output is ready."):t.scope;
 const current=!!snapshot?.current&&view?.enabled===true&&view.server_time_unix_milli+Math.max(0,performance.now()-checked.current)<snapshot.expires_unix_nano/1e6;
 const url=secret?new URL(secret+(format==="raw"?"?format=raw":""),window.location.origin).href:"";
 return <section className="business-panel lab-subscription" aria-label="Client LAB subscription">
  <div className="lab-subscription-heading"><h3>{t.title}</h3><button type="button" disabled={busy||status==="loading"} onClick={()=>{secretEpoch.current++;setSecret("");setReload(n=>n+1);}}>{t.refresh}</button></div>
  {status!=="ready"&&<p role="status">{status==="loading"?t.loading:status==="denied"?t.denied:t.failed}</p>}
  {status==="ready"&&view&&!view.enabled&&<p role="status">{t.disabled}</p>}
  {status==="ready"&&view?.enabled&&<>
   <p role="status">{t[view.state]}</p>
   {snapshot?<div className="lab-subscription-snapshot"><p>{current?t.current:t.historical}</p><p>{t.routes}: {snapshot.route_count}</p><p>{t.until}: <bdi>{new Intl.DateTimeFormat(language==="fa"?"fa-IR":"en-GB",{timeZone:"Asia/Tehran",dateStyle:"short",timeStyle:"medium"}).format(snapshot.expires_unix_nano/1e6)}</bdi></p></div>:<p>{t.noSnapshot}</p>}
   {canManage&&<div className="lab-subscription-actions">
    {view.state==="absent"?<button type="button" disabled={!allowed||!!attempt} onClick={()=>change("issue")}>{t.issue}</button>:<>
     {view.state==="active"&&<button type="button" disabled={!allowed||!!attempt} onClick={()=>void reveal()}>{t.reveal}</button>}
     <button type="button" disabled={!allowed||!!attempt} onClick={()=>change("rotate")}>{t.rotate}</button>
     {view.state==="active"&&<button type="button" disabled={!allowed||!!attempt} onClick={()=>change("revoke")}>{t.revoke}</button>}
    </>}
   </div>}
  </>}
  {attempt&&<div role="status"><p>{t.unknown}</p><div className="lab-subscription-actions"><button type="button" disabled={!allowed} onClick={()=>void send(attempt)}>{t.retry}</button><button type="button" disabled={busy} onClick={()=>{if(remember(null)){setNotice("");setReload(n=>n+1);}}}>{t.discard}</button></div></div>}
  {notice&&!attempt&&<p role="status">{notice}</p>}
  {secret&&canManage&&<div className="lab-subscription-secret"><p>{t.secret}</p><label>{t.format}<select value={format} onChange={e=>{setFormat(e.target.value);setCopied(false);}}><option value="base64">Base64</option><option value="raw">VLESS URI</option></select></label><label>{t.url}<input aria-label="Subscription URL" value={url} readOnly autoComplete="off" spellCheck={false} dir="ltr"/></label><div className="lab-subscription-actions"><button type="button" onClick={()=>{void navigator.clipboard.writeText(url).then(()=>{if(!lifetime.current?.signal.aborted)setCopied(true);}).catch(()=>setNotice(t.failed));}}>{copied?t.copied:t.copy}</button><button type="button" onClick={()=>{setSecret("");setCopied(false);}}>{t.hide}</button></div></div>}
  <p className="business-warning">{scopeText}</p><p>{t.boundary}</p>
 </section>;
}
