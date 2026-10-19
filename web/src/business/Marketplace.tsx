import { useEffect, useRef, useState } from "react";
import { ShoppingBag, ShieldCheck } from "lucide-react";
import { api, mutation } from "../session";
import { uid, gb, bytesFromGB } from "./shared";
import { useAccountPageOffset } from "./useAccountPageOffset";
import type { BusinessModel } from "./useBusinessWorkspace";
import "./Marketplace.css";
import MarketplacePolicyV10b,{validPolicy,type CatalogPolicy} from "./MarketplacePolicyV10b";

type Spec={id:string;representative_id:string;name:string;plan_kind:"volume"|"unlimited";
 duration_days:number;ip_count:number;volume_bytes:number;price_toman:number};
type Product=Spec&{status:"draft"|"published"|"archived";revision:number;created_unix:number;updated_unix:number};
type Page={products:Product[];total:number;limit:number;offset:number;
 has_more:boolean;q:string;state:string;representative_id:string};
type Form={id:string;rep:string;name:string;kind:"volume"|"unlimited";
 days:string;ip:string;gb:string;price:string;status:Product["status"];reason:string};
const initial=():Form=>({id:"",rep:"",name:"",kind:"volume",days:"30",ip:"0",gb:"10",price:"0",status:"draft",reason:""});
const content={
 en:{title:"Representative marketplace",note:"Catalog only. No checkout, payment, client creation or credit deduction is enabled.",
  create:"Create draft",edit:"Save product",cancel:"Cancel",id:"Product ID",rep:"Representative ID",
  name:"Product name",kind:"Plan type",unlimited:"Unlimited",duration:"Duration (days)",ip:"User / IP seats",volume:"Volume (GB)",
  price:"Price (toman)",status:"Status",reason:"Audit reason",search:"Search entire catalog",
  select:"Manage",all:"All statuses",draft:"Draft",published:"Published",archived:"Archived",
  prev:"Previous",next:"Next",empty:"No verified products",loading:"Loading product catalog…",
  invalid:"Saved catalog unavailable or invalid. Actions blocked.",
  success:"Product saved; accounting and Xray are unchanged.",
  error:"Unable to save product. Check status and retry.",
  confirm:"Save this catalog change? It will not create or charge a customer.",
  buy:"Purchase is disabled pending real Xray and accounting acceptance."},
 fa:{title:"فروشگاه پلن نمایندگان",note:"فعلاً فقط کاتالوگ؛ خرید، پرداخت، ساخت مشتری و کسر اعتبار فعال نیست.",
  create:"ساخت پیش‌نویس",edit:"ذخیره محصول",cancel:"انصراف",id:"شناسه محصول",rep:"شناسه نماینده",
  name:"نام محصول",kind:"نوع پلن",unlimited:"نامحدود",duration:"مدت (روز)",ip:"تعداد کاربر / IP",volume:"حجم (گیگابایت)",
  price:"قیمت (تومان)",status:"وضعیت",reason:"دلیل حسابرسی",search:"جست‌وجوی سراسری",
  select:"مدیریت",all:"تمام وضعیت‌ها",draft:"پیش‌نویس",published:"منتشرشده",archived:"آرشیوشده",
  prev:"قبلی",next:"بعدی",empty:"محصول معتبری ثبت نشده",loading:"در حال دریافت محصولات…",
  invalid:"اطلاعات کاتالوگ معتبر نیست؛ عملیات مسدود است.",
  success:"محصول ثبت شد؛ حسابداری و Xray بدون تغییر ماند.",
  error:"ذخیره ناموفق بود؛ وضعیت را دوباره بررسی کن.",
  confirm:"تغییر محصول ثبت شود؟ مشتری ساخته نشده و اعتباری کسر نمی‌شود.",
  buy:"خرید تا تأیید حسابداری و Xray واقعی غیرفعال است."},
} as const;
function validProduct(p:Product,owner:boolean,rep:string,canManage=false):boolean{
 return !!p&&typeof p.id==="string"&&typeof p.name==="string"&&
  (p.plan_kind==="volume"||p.plan_kind==="unlimited")&&
  ["draft","published","archived"].includes(p.status)&&
  Number.isSafeInteger(p.revision)&&p.revision>=1&&
  Number.isSafeInteger(p.volume_bytes)&&p.volume_bytes>=0&&
  Number.isSafeInteger(p.price_toman)&&p.price_toman>=0&&
  (owner||(p.representative_id===rep&&(canManage||p.status==="published")));
}
export default function Marketplace({model}:{model:BusinessModel}){
 const owner=model.session.user.role==="owner",rep=model.session.user.representative_id;
 const t=content[model.language];
 const [repPolicy,setRepPolicy]=useState<CatalogPolicy|null>(null);
 useEffect(()=>{
  if(owner)return;
  const ctrl=new AbortController();
  setRepPolicy(null);
  void api<CatalogPolicy>("/api/v1/workspace/products/policy",{signal:ctrl.signal}).then(result=>{
   if(!ctrl.signal.aborted&&validPolicy(result,rep)){
    setRepPolicy(result);
    if(result.manage_products)setState("all");
   }
  }).catch(()=>{if(!ctrl.signal.aborted)setRepPolicy(null)});
  return ()=>ctrl.abort();
 },[owner,rep,model.refreshKey]);
 const canManage=owner||!!repPolicy?.manage_products;
 const [q,setQ]=useState("");
 const [state,setState]=useState(owner?"all":"published");
 const [offset,setOffset]=useAccountPageOffset(model.session.user.id,"products",50);
 const [page,setPage]=useState<Page|null>(null);
 const [loading,setLoading]=useState(true);
 const [error,setError]=useState("");
 const [editing,setEditing]=useState<Product|null>(null);
 const [form,setForm]=useState<Form>(initial);
 const [busy,setBusy]=useState(false);
 const [version,setVersion]=useState(0);
 const nonce=useRef<{key:string;eventID:string}|null>(null);
 useEffect(()=>{
  const controller=new AbortController();setLoading(true);setPage(null);
  const stateFilter=canManage?state:"published";
  const scope=owner?"":rep;
  const opts=new URLSearchParams({limit:"50",offset:String(offset),q,state:stateFilter,representative_id:scope});
  const uri=!owner&&canManage?"/api/v1/workspace/products/manage":"/api/v1/workspace/products";
  void api<Page>(uri+"?"+opts.toString(),{signal:controller.signal}).then(p=>{
   if(controller.signal.aborted)return;
   if(!p||!Array.isArray(p.products)||p.products.length>50||p.limit!==50||
    p.offset!==offset||p.q!==q||p.state!==stateFilter||p.representative_id!==scope||
    !Number.isSafeInteger(p.total)||p.total<0||typeof p.has_more!=="boolean"||
    p.has_more!==(offset+p.products.length<p.total)||
    p.products.some(x=>!validProduct(x,owner,rep,canManage)))throw Error("unverified_catalog");
   setPage(p);
  }).catch(()=>{if(!controller.signal.aborted)setError(t.invalid)})
   .finally(()=>{if(!controller.signal.aborted)setLoading(false)});
  return ()=>controller.abort();
 },[q,state,offset,version,model.refreshKey,owner,rep,canManage,t.invalid]);
 const kind=form.kind,volume=kind==="volume"?bytesFromGB(form.gb):0;
 const days=Number(form.days),ip=Number(form.ip),price=Number(form.price);
 const spec:Spec={id:form.id.trim(),representative_id:owner?form.rep.trim():rep,name:form.name.trim(),
  plan_kind:kind,duration_days:days,ip_count:ip,volume_bytes:volume??0,price_toman:price};
 const withinOwnerLimits=owner||!!repPolicy&&
  ((!repPolicy.manage_products)?false:
   (spec.plan_kind==="volume"?repPolicy.allow_volume&&spec.volume_bytes<=repPolicy.max_volume_bytes:
    repPolicy.allow_unlimited))&&
  spec.duration_days<=repPolicy.max_duration_days&&spec.ip_count<=repPolicy.max_ip_count&&
  spec.price_toman>=repPolicy.min_price_toman&&spec.price_toman<=repPolicy.max_price_toman&&
  (form.status!=="published"||repPolicy.can_publish);
 const valid=canManage&&withinOwnerLimits&&!busy&&!model.actionBusy&&/^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(spec.id)&&
  /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(spec.representative_id)&&
  spec.representative_id!=="owner"&&spec.name.length>0&&spec.name.length<=128&&
  Number.isSafeInteger(days)&&days>=1&&days<=3650&&
  Number.isSafeInteger(ip)&&ip>=0&&ip<=1000&&(kind==="volume"||ip>0)&&
  volume!==null&&(kind==="unlimited"||volume>0)&&
  Number.isSafeInteger(price)&&price>=0&&price<=1e12&&
  form.reason.trim().length>0&&form.reason.trim().length<=200&&
  (!editing||editing.status!=="archived")&&(form.status!=="published"||price>0);
 function choose(p:Product){
  setEditing(p);setForm({id:p.id,rep:p.representative_id,name:p.name,kind:p.plan_kind,
   days:String(p.duration_days),ip:String(p.ip_count),gb:String(p.volume_bytes/(1024**3)),
   price:String(p.price_toman),status:p.status,reason:""});setError("");nonce.current=null;
 }
 function clear(){setEditing(null);setForm(initial());nonce.current=null;}
 async function save(){
  if(!valid||!window.confirm(t.confirm))return;
  const payload=editing?{...spec,status:form.status,expected_revision:editing.revision,
    reason:form.reason.trim()}:{...spec,reason:form.reason.trim()};
  const identity=JSON.stringify([model.session.user.id,editing?.id||"",payload]);
  const eventID=nonce.current?.key===identity?nonce.current.eventID:uid();
  nonce.current={key:identity,eventID};
  setBusy(true);model.setActionBusy(true);setError("");
  try{
   const uri=editing?"/api/v1/workspace/products/"+encodeURIComponent(editing.id)+"/update":
     "/api/v1/workspace/products";
   const receipt=await mutation<{product:Product;checkout_enabled:boolean;
    applied_to_xray:boolean;billing_changed:boolean}>(
    uri,{...payload,event_id:eventID},model.session.csrf_token);
   if(!receipt||receipt.checkout_enabled!==false||receipt.applied_to_xray!==false||
      receipt.billing_changed!==false||!validProduct(receipt.product,owner,rep,canManage))
    throw Error("unverified_receipt");
   nonce.current=null;clear();model.setNotice(t.success);
   setVersion(v=>v+1);model.reload();
  }catch{setError(t.error)}
  finally{setBusy(false);model.setActionBusy(false)}
 }
 return <div className="catalog-layout">
  <section className="business-panel catalog-banner">
   <h3><ShoppingBag size={18}/>{t.title}</h3><p>{t.note}</p>
   <p><ShieldCheck size={14}/>{t.buy}</p>
  </section>
  {owner&&<MarketplacePolicyV10b model={model}/>}
  {!owner&&canManage&&<section className="business-panel catalog-banner" aria-label="Seller policy summary">
   <p>{model.language==="fa"?"مدیریت محصولات فقط در سقف مجاز مالک فعال است؛ سفارش و پرداخت غیرفعال‌اند.":
    "Product management is bounded by owner rules; checkout and payment remain disabled."}</p>
   <p>{model.language==="fa"?"کف قیمت":"Minimum price"}: {repPolicy?.min_price_toman.toLocaleString("en-US")} تومان</p>
  </section>}
  {canManage&&<section className="business-panel catalog-editor" aria-label="Marketplace product editor">
   <header><h3>{editing?t.edit:t.create}</h3>
    {editing&&<button type="button" onClick={clear} disabled={busy}>{t.cancel}</button>}</header>
   <div className="catalog-fields">
    <label>{t.id}<input aria-label="Catalog ID" value={form.id} disabled={busy||!!editing}
     onChange={e=>setForm({...form,id:e.target.value})}/></label>
    <label>{t.rep}<input aria-label="Catalog representative" list="catalog-reps" value={owner?form.rep:rep}
     disabled={busy||!!editing||!owner} onChange={e=>setForm({...form,rep:e.target.value})}/>
     <datalist id="catalog-reps">{model.reps?.filter(r=>r.kind==="representative").map(r=>
      <option key={r.id} value={r.id}>{r.display_name}</option>)}</datalist></label>
    <label>{t.name}<input aria-label="Catalog name" value={form.name} maxLength={128}
     onChange={e=>setForm({...form,name:e.target.value})}/></label>
    <label>{t.kind}<select aria-label="Catalog kind" value={form.kind}
      onChange={e=>setForm({...form,kind:e.target.value as Form["kind"]})}>
     <option value="volume">{t.volume}</option><option value="unlimited">{t.unlimited}</option></select></label>
    <label>{t.duration}<input aria-label="Catalog days" type="number" min={1} max={3650} value={form.days}
     onChange={e=>setForm({...form,days:e.target.value})}/></label>
    <label>{t.ip}<input aria-label="Catalog seats" type="number" min={kind==="volume"?0:1} max={1000} value={form.ip}
     onChange={e=>setForm({...form,ip:e.target.value})}/></label>
    {kind==="volume"&&<label>{t.volume}<input aria-label="Catalog GB" type="number" step="any" min="0.01" value={form.gb}
     onChange={e=>setForm({...form,gb:e.target.value})}/></label>}
    <label>{t.price}<input aria-label="Catalog price" type="number" min={0} max={1e12} value={form.price}
     onChange={e=>setForm({...form,price:e.target.value})}/></label>
    {editing&&<label>{t.status}<select aria-label="Catalog status" value={form.status}
     onChange={e=>setForm({...form,status:e.target.value as Product["status"]})}>
     <option value="draft">{t.draft}</option><option value="published">{t.published}</option>
     <option value="archived">{t.archived}</option></select></label>}
    <label className="catalog-reason">{t.reason}<input aria-label="Catalog reason" value={form.reason} maxLength={200}
     onChange={e=>setForm({...form,reason:e.target.value})}/></label>
   </div>
   <button type="button" className="catalog-primary" onClick={()=>void save()} disabled={!valid}>{editing?t.edit:t.create}</button>
  </section>}
  <section className="business-panel catalog-directory" aria-label="Marketplace directory">
   <header><h3>{t.title}</h3><span>{page?.total.toLocaleString("en-US")??"—"}</span></header>
   <div className="catalog-filters">
    <label>{t.search}<input aria-label="Search marketplace" value={q} onChange={e=>{setQ(e.target.value);setOffset(0)}}/></label>
    {canManage&&<label>{t.status}<select aria-label="Filter product status" value={state} onChange={e=>{setState(e.target.value);setOffset(0)}}>
     <option value="all">{t.all}</option><option value="draft">{t.draft}</option><option value="published">{t.published}</option>
     <option value="archived">{t.archived}</option></select></label>}
   </div>
   {loading?<p role="status">{t.loading}</p>:!page?<p role="alert">{error||t.invalid}</p>:
    page.products.length===0?<p>{t.empty}</p>:
    <div className="catalog-products">{page.products.map(p=><article key={p.id} className="catalog-product">
     <strong>{p.name}</strong><code dir="ltr">{p.id}</code><small>{p.representative_id}</small>
     <span>{p.plan_kind==="volume"?t.volume+" "+gb(p.volume_bytes):t.unlimited+" · "+p.ip_count+" "+t.ip} · {p.duration_days} {t.duration}</span>
     <b>{p.price_toman.toLocaleString("en-US")} تومان</b>
     <small>{p.status==="draft"?t.draft:p.status==="published"?t.published:t.archived}</small>
     {canManage&&p.status!=="archived"?<button type="button" onClick={()=>choose(p)} disabled={busy}>{t.select}</button>:
      <small>{t.buy}</small>}
    </article>)}</div>}
   <nav className="business-pagination">
    <button type="button" disabled={!page||offset===0} onClick={()=>setOffset(v=>Math.max(0,v-50))}>{t.prev}</button>
    <span>{page?String(Math.min(offset+1,page.total))+"–"+String(Math.min(offset+page.products.length,page.total))+" / "+page.total:"—"}</span>
    <button type="button" disabled={!page||!page.has_more} onClick={()=>setOffset(v=>v+50)}>{t.next}</button>
   </nav>
  </section>
 </div>;
}
