import { useEffect, useRef, useState } from "react";
import { LockKeyhole, ShieldCheck, UserRoundCog } from "lucide-react";
import { api, mutation } from "../session";
import { bytesFromGB, uid } from "./shared";
import type { BusinessModel } from "./useBusinessWorkspace";
import "./MarketplacePolicyV10b.css";

export type CatalogPolicy={
 representative_id:string;manage_products:boolean;can_publish:boolean;
 allow_volume:boolean;allow_unlimited:boolean;min_price_toman:number;
 max_price_toman:number;max_duration_days:number;max_ip_count:number;
 max_volume_bytes:number;revision:number;updated_unix:number;
};
type Audit={event_id:string;representative_id:string;reason:string;created_unix:number;
 previous:CatalogPolicy;current:CatalogPolicy};
export function validPolicy(p:CatalogPolicy,id:string):boolean{
 return !!p&&p.representative_id===id&&
  ["manage_products","can_publish","allow_volume","allow_unlimited"].every(k=>
   typeof (p as unknown as Record<string,unknown>)[k]==="boolean")&&
  ["min_price_toman","max_price_toman","max_duration_days","max_ip_count","max_volume_bytes",
    "revision","updated_unix"].every(k=>Number.isSafeInteger(
     (p as unknown as Record<string,number>)[k]))&&
  p.min_price_toman>=1&&p.max_price_toman>=p.min_price_toman&&p.max_price_toman<=1e12&&
  p.max_duration_days>=1&&p.max_duration_days<=3650&&
  p.max_ip_count>=1&&p.max_ip_count<=1000&&
  p.max_volume_bytes>=1&&p.max_volume_bytes<=9e15&&
  p.revision>=0&&p.updated_unix>=0&&(!p.can_publish||p.manage_products);
}
const dictionary={
 fa:{
  title:"مجوز و سقف فروش نمایندگان",
  subtitle:"مالک تعیین می‌کند هر نماینده چه محصولاتی با چه قیمت و محدودیتی مجاز است تعریف کند.",
  rep:"نماینده",grant:"اجازه مدیریت محصولات",publish:"اجازه انتشار محصول",
  volume:"امکان محصول حجمی",unlimited:"امکان محصول نامحدود",
  min:"حداقل قیمت (تومان)",max:"حداکثر قیمت (تومان)",
  days:"حداکثر روز",ip:"حداکثر کاربر / IP",gb:"حداکثر حجم (GB)",
  reason:"دلیل حسابرسی",save:"ثبت مجوز و محدودیت",disabled:"تا تأیید مالک، ایجاد محصول مجاز نیست.",
  confirm:"تغییر مجوز و سقف محصولات ثبت شود؟ نشست‌های قبلی نماینده باطل می‌شوند، اما مشتریان و اعتبارشان تغییر نمی‌کند.",
  success:"مجوز محصولات ثبت شد؛ نشست‌های قبلی نماینده باطل شدند.",
  error:"امکان تأیید این سیاست نیست. تغییرات را دوباره بررسی کن.",
  pending:"در حال خواندن سیاست واقعی از HUB…",history:"تاریخچهٔ تغییر مجوز",
 },
 en:{
  title:"Representative product permissions",
  subtitle:"The owner sets what each representative may create and the price and plan ceilings.",
  rep:"Representative",grant:"Allow product management",publish:"Allow publishing",
  volume:"Allow volume products",unlimited:"Allow unlimited products",
  min:"Minimum price (toman)",max:"Maximum price (toman)",
  days:"Maximum days",ip:"Maximum user/IP seats",gb:"Maximum volume (GB)",
  reason:"Audit reason",save:"Save permissions and limits",
  disabled:"No representative product management without explicit owner authorization.",
  confirm:"Save audited catalog policy? Existing representative sessions are revoked. Clients and credit remain untouched.",
  success:"Catalog permissions saved and old representative sessions revoked.",
  error:"Could not verify the current policy or update. Review again.",
  pending:"Reading verified account policy…",history:"Policy change history",
 },
} as const;
export default function MarketplacePolicyV10b({model}:{model:BusinessModel}){
 const t=dictionary[model.language];
 const [rep,setRep]=useState("");
 const [policy,setPolicy]=useState<CatalogPolicy|null>(null);
 const [updated,setUpdated]=useState<CatalogPolicy|null>(null);
 const [fields,setFields]=useState({manage_products:false,can_publish:false,
  allow_volume:true,allow_unlimited:false,min_price_toman:"1",max_price_toman:"1000000000000",
  max_duration_days:"3650",max_ip_count:"1000",max_volume_gb:"10"});
 const [reason,setReason]=useState("");
 const [audit,setAudit]=useState<Audit[]>([]);
 const [error,setError]=useState("");
 const [busy,setBusy]=useState(false);
 const [loaded,setLoaded]=useState(false);
 const [reload,setReload]=useState(0);
 const nonce=useRef<{key:string;eventID:string}|null>(null);
 useEffect(()=>{
  const ctl=new AbortController();
  setLoaded(false);setPolicy(null);setUpdated(null);setAudit([]);setError("");setReason("");
  if(!rep)return ()=>ctl.abort();
  const uri="/api/v1/workspace/representatives/"+encodeURIComponent(rep)+"/catalog-policy";
  void Promise.all([
   api<CatalogPolicy>(uri,{signal:ctl.signal}),
   api<{events:Audit[];limit:number;offset:number;audit:string}>(uri+"/events?limit=20&offset=0",{signal:ctl.signal}),
  ]).then(([value,logs])=>{
   if(ctl.signal.aborted)return;
   if(!validPolicy(value,rep)||!Array.isArray(logs?.events)||logs.events.length>20||
     logs.limit!==20||logs.offset!==0||logs.audit!=="append_only"||
     logs.events.some(x=>x.representative_id!==rep||!Number.isSafeInteger(x.created_unix)))
    throw Error("invalid_scoped_policy");
   setPolicy(value);setAudit(logs.events);
   setFields({
    manage_products:value.manage_products,can_publish:value.can_publish,
    allow_volume:value.allow_volume,allow_unlimited:value.allow_unlimited,
    min_price_toman:String(value.min_price_toman),max_price_toman:String(value.max_price_toman),
    max_duration_days:String(value.max_duration_days),max_ip_count:String(value.max_ip_count),
    max_volume_gb:String(value.max_volume_bytes/(1024**3)),
   });
   setLoaded(true);nonce.current=null;
  }).catch(()=>{if(!ctl.signal.aborted){setLoaded(false);setPolicy(null);setError(t.error)}});
  return ()=>ctl.abort();
 },[rep,model.refreshKey,reload,t.error]);
 const maxVolume=bytesFromGB(fields.max_volume_gb);
 const candidate=policy?{
  representative_id:rep,manage_products:fields.manage_products,can_publish:fields.can_publish,
  allow_volume:fields.allow_volume,allow_unlimited:fields.allow_unlimited,
  min_price_toman:Number(fields.min_price_toman),max_price_toman:Number(fields.max_price_toman),
  max_duration_days:Number(fields.max_duration_days),max_ip_count:Number(fields.max_ip_count),
  max_volume_bytes:fields.max_volume_gb===String(policy.max_volume_bytes/(1024**3))?
   policy.max_volume_bytes:(maxVolume??0),
 }:null;
 const valid=!!candidate&&!!policy&&!!loaded&&!busy&&!model.actionBusy&&
  candidate.min_price_toman>=1&&Number.isSafeInteger(candidate.min_price_toman)&&
  candidate.max_price_toman>=candidate.min_price_toman&&candidate.max_price_toman<=1e12&&
  Number.isSafeInteger(candidate.max_price_toman)&&
  Number.isSafeInteger(candidate.max_duration_days)&&candidate.max_duration_days>=1&&candidate.max_duration_days<=3650&&
  Number.isSafeInteger(candidate.max_ip_count)&&candidate.max_ip_count>=1&&candidate.max_ip_count<=1000&&
  Number.isSafeInteger(candidate.max_volume_bytes)&&candidate.max_volume_bytes>=1&&candidate.max_volume_bytes<=9e15&&
  (!candidate.can_publish||candidate.manage_products)&&
  (!candidate.manage_products||candidate.allow_volume||candidate.allow_unlimited)&&
  reason.trim().length>0&&reason.trim().length<=200&&
  ["manage_products","can_publish","allow_volume","allow_unlimited","min_price_toman","max_price_toman",
   "max_duration_days","max_ip_count","max_volume_bytes"].some(k=>
    (candidate as unknown as Record<string,unknown>)[k]!==(policy as unknown as Record<string,unknown>)[k]);
 const identity=JSON.stringify([model.session.user.id,policy?.revision,candidate,reason.trim()]);
 useEffect(()=>{if(nonce.current?.key!==identity)nonce.current=null},[identity]);
 async function save(){
  if(!valid||!candidate||!policy||!window.confirm(t.confirm))return;
  const eventID=nonce.current?.key===identity?nonce.current.eventID:uid();
  nonce.current={key:identity,eventID};setBusy(true);model.setActionBusy(true);setError("");
  try{
   const result=await mutation<{
    policy:CatalogPolicy;sessions_revoked:boolean;replayed:boolean;credit_changed:boolean;
    clients_changed:boolean;applied_to_xray:boolean;
   }>("/api/v1/workspace/representatives/"+encodeURIComponent(rep)+"/catalog-policy",
    {...candidate,expected_revision:policy.revision,event_id:eventID,reason:reason.trim()},
    model.session.csrf_token);
   if(!validPolicy(result.policy,rep)||result.credit_changed!==false||
     result.clients_changed!==false||result.applied_to_xray!==false)throw Error("invalid_receipt");
   nonce.current=null;setUpdated(result.policy);model.setNotice(t.success);
   setReload(v=>v+1);
  }catch{setError(t.error)}
  finally{setBusy(false);model.setActionBusy(false)}
 }
 function setBool(key:"manage_products"|"can_publish"|"allow_volume"|"allow_unlimited",v:boolean){
  setFields(old=>({...old,[key]:v,...(key==="manage_products"&&!v?{can_publish:false}:{})}));
 }
 return <section className="business-panel marketplace-policy" aria-label="Owner catalog policy">
  <header><h3><UserRoundCog size={17}/>{t.title}</h3><p>{t.subtitle}</p></header>
  <label>{t.rep}<select aria-label="Policy representative" value={rep} disabled={busy}
   onChange={e=>setRep(e.target.value)}>
   <option value="">—</option>{model.reps?.filter(r=>r.kind==="representative").map(r=>
    <option value={r.id} key={r.id}>{r.display_name} ({r.id})</option>)}
  </select></label>
  {!rep?<p>{t.disabled}</p>:!policy||!loaded?<p role="status">{error||t.pending}</p>:<>
   <div className="marketplace-policy-flags">
    {(["manage_products","can_publish","allow_volume","allow_unlimited"] as const).map((key,i)=>
     <label key={key}><input type="checkbox" aria-label={"Policy "+key} checked={fields[key]}
       disabled={busy||model.actionBusy} onChange={e=>setBool(key,e.target.checked)}/>
      {[t.grant,t.publish,t.volume,t.unlimited][i]}</label>)}
   </div>
   <div className="marketplace-policy-limits">
    {([
     ["min_price_toman",t.min],["max_price_toman",t.max],
     ["max_duration_days",t.days],["max_ip_count",t.ip],["max_volume_gb",t.gb],
    ] as const).map(([key,label])=><label key={key}>{label}<input type="number" aria-label={"Policy "+key}
     min={0} step={key==="max_volume_gb"?"any":1} value={fields[key]}
     disabled={busy||model.actionBusy}
     onChange={e=>setFields(old=>({...old,[key]:e.target.value}))}/></label>)}
   </div>
   <label>{t.reason}<input aria-label="Policy audit reason" value={reason}
     maxLength={200} onChange={e=>setReason(e.target.value)} disabled={busy||model.actionBusy}/></label>
   <button className="marketplace-policy-save" type="button" disabled={!valid} onClick={()=>void save()}>
    <ShieldCheck size={15}/>{t.save}</button>
   {error&&<p role="alert">{error}</p>}
   {updated&&<p>{t.success}</p>}
   <div className="marketplace-policy-audit"><h4><LockKeyhole size={14}/>{t.history}</h4>
    {audit.map(a=><p key={a.event_id}>{new Date(a.created_unix*1000).toLocaleDateString()} — {a.reason}</p>)}
   </div>
  </>}
 </section>;
}
