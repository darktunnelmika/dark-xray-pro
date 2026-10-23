import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { ArrowRightLeft, CheckCircle2, ChevronDown, Globe2, Link2, RefreshCw, Route, Server, ShieldCheck } from "lucide-react";
import { api, ApiError } from "./session";
import "./NativeRouteDraftEditor.css";
import NativeRouteReadinessP0G from "./NativeRouteReadinessP0G";

type NodeInfo={node_id:string;installation_id:string;name:string};
type NativeTarget={inbound_id:number;node_id:string;installation_id:string;
 listen_ip:string;direct_port:number;swap_port:number};
type RouteDraft={
 event_id:string;inbound_id:number;node_id:string;installation_id:string;
 expected_inbound_revision:number;expected_draft_revision:number;
 direct_port:number;direct_hosts:string[];tunnel_port:number;tunnel_hosts:string[];
 swap_enabled:boolean;swap_upstream_node_id:string;swap_tunnel_host:string;
 swap_tunnel_port:number;swap_node_port:number;
 confirm_existing_route_disable:boolean;reason:string;
};
// Preserve delimiters while editing; normalize host lists only for API requests.
type RouteDraftForm=Omit<RouteDraft,"direct_hosts"|"tunnel_hosts"> & {
 direct_hosts:string;tunnel_hosts:string;
};
type Saved=RouteDraft & {
 draft_revision:number;listen_ip:string;native_direct_port:number;
 native_swap_port:number;billing_source_node_id:string;
 apply_allowed:boolean;agent_ack_verified:boolean;
 data_plane_verified:boolean;billing_verified:boolean;
 subscription_published:boolean;
};
const words={
 fa:{title:"مسیرهای اتصال",
  description:"مسیرهای هر نود را تنظیم کن. ذخیرهٔ پیش‌نویس، ترافیک را فعال نمی‌کند.",
  select:"نود مقصد",direct:"پورت مستقیم",directHosts:"آدرس‌های مستقیم",
  tunnel:"پورت تونل",tunnelHosts:"آدرس‌های تونل",
  swap:"دارک سواپ",
  upstream:"نود تونل بالادستی",host:"آدرس تونل",tunnelPort:"پورت مقصد تونل",
  nodePort:"پورت نود مقصد",confirm:"تغییر مسیرهای قبلی را بررسی کرده‌ام.",
  reason:"یادداشت تغییر",preview:"بررسی تنظیمات",
  save:"ذخیرهٔ مسیر",nothing:"مسیر ذخیره‌شده‌ای برای این نود وجود ندارد.",
  saved:"پیش‌نویس مسیر ذخیره شد. اجرای مسیر هنوز فعال نیست.",
  checked:"تنظیمات معتبر است و می‌توانی پیش‌نویس را ذخیره کنی.",
  error:"دسترسی یا وضعیت سرور تأیید نشد. عملیات مسدود است.",
  expected:"نسخهٔ اینباند",draftRev:"نسخهٔ پیش‌نویس",
  legacy:"پورت Swap قدیمی به‌تنهایی نشان‌دهندهٔ تونل فعال نیست.",
  noApply:"Agent ACK: تأییدنشده · Xray: تأییدنشده · حسابداری: تأییدنشده",
  draft:"پیش‌نویس",directName:"مستقیم",tunnelName:"تونل",paths:"انتخاب مسیرها",
  directHelp:"دسترسی مستقیم به نود مقصد",tunnelHelp:"نشانی‌های ورودی تونل معمولی",
  swapHelp:"عبور از نود تونل بالادستی تا نود مقصد",
  hostsHelp:"هر آدرس در یک خط یا با کاما؛ بدون http یا شمارهٔ پورت.",
  addressCount:"آدرس",hostLimit:"حداکثر ۶۴ آدرس در مجموع مسیرها",hostOverflow:"تعداد آدرس‌ها از ۶۴ بیشتر است.",
  choose:"انتخاب نود",noTargets:"برای تنظیم مسیر، ابتدا یک نود به این اینباند اضافه کن.",
  chooseHelp:"نود مقصد را انتخاب کن تا مسیرهایش نمایش داده شوند.",
  noPaths:"حداقل یک مسیر را انتخاب کن.",required:"الزامی",technical:"جزئیات فنی و وضعیت اجرا",
  nodeIdentity:"شناسهٔ نود",listen:"IP نود",nativeDirect:"پورت مستقیم نود",nativeSwap:"پورت مقصد نود",
  savedVersion:"نسخهٔ ذخیره‌شده",notSaved:"ذخیره‌نشده",
  discardNode:"تغییرات ذخیره‌نشدهٔ مسیر این نود کنار گذاشته شود؟",
  },
 en:{title:"Connection routes",
  description:"Configure each Node's routes. Saving a draft does not activate traffic.",
  select:"Destination Node",direct:"Direct port",directHosts:"Direct addresses",
  tunnel:"Tunnel port",tunnelHosts:"Tunnel addresses",
  swap:"Dark Swap",
  upstream:"Upstream tunnel Node",host:"Tunnel address",tunnelPort:"Upstream tunnel port",
  nodePort:"Destination Node port",confirm:"I reviewed the changes to existing routes.",
  reason:"Change note",preview:"Check settings",
  save:"Save route",nothing:"No saved routes for this Node yet.",
  saved:"Route draft saved. Route execution is not active yet.",
  checked:"Settings are valid. You can save the route draft.",
  error:"Server state or account access could not be verified. Actions blocked.",
  expected:"Inbound revision",draftRev:"Draft revision",
  legacy:"Old native swap_port is not proof of an active tunnel.",
  noApply:"Agent ACK: unverified · Xray: unverified · billing: unverified",
  draft:"Draft",directName:"Direct",tunnelName:"Tunnel",paths:"Connection paths",
  directHelp:"Connect directly to the destination Node",tunnelHelp:"Entry addresses for the ordinary tunnel",
  swapHelp:"Use an upstream tunnel Node to reach the destination",
  hostsHelp:"One address per line or separated by commas; no http or port.",
  addressCount:"addresses",hostLimit:"64 addresses maximum across all paths",hostOverflow:"The address count exceeds 64.",
  choose:"Choose a Node",noTargets:"Add a Node to this Inbound before configuring its routes.",
  chooseHelp:"Choose a destination Node to see its routes.",
  noPaths:"Select at least one connection path.",required:"Required",technical:"Technical details and execution status",
  nodeIdentity:"Node ID",listen:"Node IP",nativeDirect:"Native Direct port",nativeSwap:"Native destination port",
  savedVersion:"Saved revision",notSaved:"Not saved",
  discardNode:"Discard the unsaved route changes for this Node?",
  }
} as const;
const comma=(value:string)=>value.split(/[,\n\r]+/).map(x=>x.trim()).filter(Boolean);
function blank(id:number,rev:number,node:NativeTarget):RouteDraftForm{
 return {event_id:"",inbound_id:id,node_id:node.node_id,
  installation_id:node.installation_id,expected_inbound_revision:rev,
  expected_draft_revision:0,direct_port:node.direct_port,direct_hosts:"",
  tunnel_port:0,tunnel_hosts:"",swap_enabled:false,
  swap_upstream_node_id:"",swap_tunnel_host:"",swap_tunnel_port:0,swap_node_port:0,
  confirm_existing_route_disable:false,reason:""};
}
export default function NativeRouteDraftEditor({inboundID,inboundRevision,nodes,targets,language,csrf,blocked,onDirtyChange}:{
 inboundID:number;inboundRevision:number;nodes:NodeInfo[];
 targets:NativeTarget[];language:"fa"|"en";csrf:string;blocked:boolean;
 onDirtyChange?:(dirty:boolean)=>void;
}){
 const t=words[language];
 const [nodeID,setNodeID]=useState("");
 const [form,setForm]=useState<RouteDraftForm|null>(null);
 const [loaded,setLoaded]=useState(false);
 const [preview,setPreview]=useState(false);
 const [message,setMessage]=useState("");
 const [busy,setBusy]=useState(false);
 const [dirty,setDirty]=useState(false);
 const [directEnabled,setDirectEnabled]=useState(false);
 const [tunnelEnabled,setTunnelEnabled]=useState(false);
 const pendingID=useRef<string|null>(null);
 const available=targets.filter(x=>x.inbound_id===inboundID&&
  nodes.some(n=>n.node_id===x.node_id&&n.installation_id===x.installation_id));
 const selected=available.find(x=>x.node_id===nodeID);
 const nodeLabel=(id:string)=>{
  const name=nodes.find(n=>n.node_id===id)?.name||id;
  return nodes.filter(n=>n.name===name).length>1?`${name} (${id})`:name;
 };
 useEffect(()=>{onDirtyChange?.(dirty)},[dirty,onDirtyChange]);
 useEffect(()=>()=>onDirtyChange?.(false),[onDirtyChange]);
 useEffect(()=>{setNodeID("");setForm(null);setLoaded(false);setPreview(false)},[inboundID]);
 useEffect(()=>{
  const ctrl=new AbortController();setLoaded(false);setPreview(false);setMessage("");setDirty(false);
  pendingID.current=null;
  if(!selected){setForm(null);setDirectEnabled(false);setTunnelEnabled(false);return()=>ctrl.abort()}
  setForm(blank(inboundID,inboundRevision,selected));
  setDirectEnabled(selected.direct_port>0);setTunnelEnabled(false);
  const uri="/api/v1/workspace/native-inbounds/"+inboundID+"/route-drafts/"+encodeURIComponent(nodeID);
  void api<Saved>(uri,{signal:ctrl.signal}).then(existing=>{
   if(ctrl.signal.aborted)return;
   if(existing.inbound_id!==inboundID||existing.node_id!==nodeID||
    existing.installation_id!==selected.installation_id||existing.apply_allowed!==false)
    throw Error("unverified_saved_route");
   setForm({...existing,direct_hosts:existing.direct_hosts.join(", "),
    tunnel_hosts:existing.tunnel_hosts.join(", "),expected_inbound_revision:inboundRevision,
    expected_draft_revision:existing.draft_revision,
    event_id:"",reason:""});
   setDirectEnabled(existing.direct_port>0);setTunnelEnabled(existing.tunnel_port>0);
   setLoaded(true);
  }).catch(e=>{
   if(ctrl.signal.aborted)return;
   if(e instanceof ApiError&&e.status===404){
    setMessage(t.nothing);setLoaded(true);
   }else{
    setLoaded(false);setMessage(t.error);
   }
  });
  return()=>ctrl.abort();
 },[inboundID,inboundRevision,nodeID,selected?.installation_id]);
 function update(patch:Partial<RouteDraftForm>){
  setForm(before=>before?{...before,...patch}:null);
  setPreview(false);pendingID.current=null;setMessage("");setDirty(true);
 }
 function number(e:ChangeEvent<HTMLInputElement>){
  const value=Number(e.target.value);
  return Number.isSafeInteger(value)&&value>=0&&value<=65535?value:0;
 }
 function chooseNode(value:string){
  if(value===nodeID||dirty&&!window.confirm(t.discardNode))return;
  setNodeID(value);
 }
 const directHostCount=directEnabled&&form?comma(form.direct_hosts).length:0;
 const tunnelHostCount=tunnelEnabled&&form?comma(form.tunnel_hosts).length:0;
 const hostCount=directHostCount+tunnelHostCount;
 const hasPath=directEnabled||tunnelEnabled||!!form?.swap_enabled;
 const canAct=!!form&&loaded&&!blocked&&!busy&&csrf.length>0&&hasPath&&hostCount<=64&&
  (!directEnabled||form.direct_port>0)&&(!tunnelEnabled||form.tunnel_port>0)&&
  form.reason.trim().length>0&&form.reason.trim().length<=200;
 async function action(save:boolean){
  if(!form||!canAct||save&&!preview)return;
  const id=pendingID.current||crypto.randomUUID();
  pendingID.current=id;setBusy(true);setMessage("");
  const uri="/api/v1/workspace/native-inbounds/"+inboundID+"/route-drafts"+(save?"":"/preview");
  const body:RouteDraft={...form,event_id:id,
   direct_port:directEnabled?form.direct_port:0,direct_hosts:directEnabled?comma(form.direct_hosts):[],
   tunnel_port:tunnelEnabled?form.tunnel_port:0,tunnel_hosts:tunnelEnabled?comma(form.tunnel_hosts):[]};
  try{
   const result=await api<any>(uri,{method:"POST",headers:{
    "Content-Type":"application/json","X-CSRF-Token":csrf},body:JSON.stringify(body)});
   if(save){
    if(!result||result.draft?.apply_allowed!==false||
     result.clients_deleted!==false||result.credit_changed!==false||
     result.runtime_changed!==false)throw Error("invalid_draft_receipt");
    pendingID.current=null;setForm(v=>v?{...v,expected_draft_revision:result.draft.draft_revision,reason:""}:v);setMessage(t.saved);setPreview(false);setDirty(false);
   }else{
    if(!result||result.valid!==true||result.apply_allowed!==false)throw Error("invalid_preview");
    setMessage(t.checked);setPreview(true);
   }
  }catch{setMessage(t.error);setPreview(false)}
  finally{setBusy(false)}
 }
 if(!nodes.length)return null;
 return <section className="native-card inbound-route-editor" aria-label="Protected Inbound routing draft">
  <header className="route-editor-heading"><div><h3><Route size={18}/>{t.title}</h3><p>{t.description}</p></div><span className="route-draft-badge">{t.draft}</span></header>
  {!available.length?<p className="route-empty"><Server size={19}/>{t.noTargets}</p>:<div className="route-node-picker">
   <span className="route-node-icon" aria-hidden="true"><Server size={19}/></span>
   <label>{t.select}<select aria-label="Route destination Node" value={nodeID} disabled={busy}
     onChange={e=>chooseNode(e.target.value)}>
     <option value="">{t.choose}</option>
     {available.map(x=><option key={x.node_id} value={x.node_id}>
       {nodeLabel(x.node_id)}</option>)}
   </select></label>
   {selected&&form&&loaded&&<span className="route-node-version">{form.expected_draft_revision>0?t.savedVersion:t.notSaved}{form.expected_draft_revision>0&&<bdi>{form.expected_draft_revision}</bdi>}</span>}
  </div>}
  {available.length>0&&!selected&&<p className="route-empty">{t.chooseHelp}</p>}
  {selected&&form&&loaded&&<>
   <div className="route-path-switches" role="group" aria-label={t.paths}>
    <label className="route-toggle" data-enabled={directEnabled} data-path="direct"><span><Globe2 size={15}/>{t.directName}</span><input aria-label="Route Direct" type="checkbox" disabled={busy}
     checked={directEnabled} onChange={e=>{setDirectEnabled(e.target.checked);update({})}}/></label>
    <label className="route-toggle" data-enabled={tunnelEnabled} data-path="tunnel"><span><Link2 size={15}/>{t.tunnelName}</span><input aria-label="Route Tunnel" type="checkbox" disabled={busy}
     checked={tunnelEnabled} onChange={e=>{setTunnelEnabled(e.target.checked);update({})}}/></label>
    <label className="route-toggle" data-enabled={form.swap_enabled} data-path="swap"><span><ArrowRightLeft size={15}/>{t.swap}</span><input aria-label="Route Dark Swap" type="checkbox" disabled={busy}
     checked={form.swap_enabled} onChange={e=>update({swap_enabled:e.target.checked,
      ...(!e.target.checked?{swap_upstream_node_id:"",swap_tunnel_host:"",swap_tunnel_port:0,swap_node_port:0}:{})})}/></label>
   </div>
   {!hasPath&&<p className="route-inline-warning" role="status">{t.noPaths}</p>}
   {(directEnabled||tunnelEnabled)&&<div className="route-basic-paths">
   {directEnabled&&<fieldset className="route-path-section" disabled={busy}>
    <legend><Globe2 size={15}/>{t.directName}</legend><p>{t.directHelp}</p>
    <div className="route-host-fields"><label>{t.direct}<input dir="ltr" aria-label="Route Direct port" type="number" min="1024" max="65535" inputMode="numeric"
      value={form.direct_port||""} onChange={e=>update({direct_port:number(e)})} placeholder="10443"/></label>
     <label><span className="route-field-label">{t.directHosts}<small>{directHostCount} {t.addressCount}</small></span><textarea dir="ltr" aria-label="Route Direct hosts" rows={2} value={form.direct_hosts}
      onChange={e=>update({direct_hosts:e.target.value})} placeholder="node.example.org"/></label></div>
   </fieldset>}
   {tunnelEnabled&&<fieldset className="route-path-section" disabled={busy}>
    <legend><Link2 size={15}/>{t.tunnelName}</legend><p>{t.tunnelHelp}</p>
    <div className="route-host-fields"><label>{t.tunnel}<input dir="ltr" aria-label="Route Tunnel port" type="number" min="1024" max="65535" inputMode="numeric"
      value={form.tunnel_port||""} onChange={e=>update({tunnel_port:number(e)})} placeholder="1185"/></label>
     <label><span className="route-field-label">{t.tunnelHosts}<small>{tunnelHostCount} {t.addressCount}</small></span><textarea dir="ltr" aria-label="Route Tunnel hosts" rows={2} value={form.tunnel_hosts}
      onChange={e=>update({tunnel_hosts:e.target.value})} placeholder="tunnel.example.org"/></label></div>
   </fieldset>}
   </div>}
   {(directEnabled||tunnelEnabled)&&<div className="route-host-help"><span>{t.hostsHelp}</span><span className={hostCount>64?"route-count-exceeded":""}><bdi dir="ltr">{hostCount} / 64</bdi> · {t.hostLimit}</span></div>}
   {hostCount>64&&<p className="route-inline-warning" role="alert">{t.hostOverflow}</p>}
   {form.swap_enabled&&<fieldset className="route-path-section" disabled={busy}>
    <legend><ArrowRightLeft size={15}/>{t.swap}</legend><p>{t.swapHelp}</p>
    <div className="route-swap-fields">
     <label>{t.upstream}<select aria-label="Route upstream Node" value={form.swap_upstream_node_id}
       onChange={e=>update({swap_upstream_node_id:e.target.value})}><option value="">{t.choose}</option>
       {available.filter(x=>x.node_id!==nodeID).map(x=><option key={x.node_id}
        value={x.node_id}>{nodeLabel(x.node_id)}</option>)}</select></label>
     <label>{t.host}<input dir="ltr" aria-label="Route Swap host" value={form.swap_tunnel_host}
       onChange={e=>update({swap_tunnel_host:e.target.value})} placeholder="tunnel.example.org"/></label>
     <label>{t.tunnelPort}<input dir="ltr" aria-label="Route Swap tunnel port" type="number" min="1024" max="65535" inputMode="numeric"
       value={form.swap_tunnel_port||""} onChange={e=>update({swap_tunnel_port:number(e)})}/></label>
     <label>{t.nodePort}<input dir="ltr" aria-label="Route Swap destination port" type="number" min="1024" max="65535" inputMode="numeric"
       value={form.swap_node_port||""} onChange={e=>update({swap_node_port:number(e)})}/></label>
    </div>
   </fieldset>}
   <div className="route-save-section">
    <label><span className="route-field-label">{t.reason}<small>{t.required}</small></span><input aria-label="Route audit reason" maxLength={200} disabled={busy} value={form.reason}
     onChange={e=>update({reason:e.target.value})}/></label>
    <label className="route-boolean"><input aria-label="Route confirm disabled bindings" type="checkbox" disabled={busy}
    checked={form.confirm_existing_route_disable}
    onChange={e=>update({confirm_existing_route_disable:e.target.checked})}/>{t.confirm}</label>
    <div className="native-actions route-save-actions">
     <button type="button" disabled={!canAct} onClick={()=>void action(false)}>
       <RefreshCw size={15}/>{t.preview}</button>
     <button type="button" className="route-save-primary" disabled={!canAct||!preview} onClick={()=>void action(true)}>
       <CheckCircle2 size={15}/>{t.save}</button>
    </div>
   </div>
  </>}
  {message&&<p role="status" className="native-notice">{message}</p>}
  {selected&&form&&loaded&&<details className="route-technical">
   <summary><ShieldCheck size={16}/>{t.technical}<ChevronDown size={15}/></summary>
   <div className="route-technical-body"><dl>
    <div><dt>{t.nodeIdentity}</dt><dd><bdi dir="ltr">{nodeID}</bdi></dd></div>
    <div><dt>{t.listen}</dt><dd><bdi dir="ltr">{selected.listen_ip}</bdi></dd></div>
    <div><dt>{t.expected}</dt><dd>{inboundRevision}</dd></div>
    <div><dt>{t.draftRev}</dt><dd>{form.expected_draft_revision}</dd></div>
    <div><dt>{t.nativeDirect}</dt><dd>{selected.direct_port||"—"}</dd></div>
    <div><dt>{t.nativeSwap}</dt><dd>{selected.swap_port||"—"}</dd></div>
   </dl><p>{t.legacy}</p><p>{t.noApply}</p>
   {form.expected_draft_revision>0&&<NativeRouteReadinessP0G
     key={nodeID+":"+form.expected_draft_revision}
     inboundID={inboundID} nodeID={nodeID}
     draftRevision={form.expected_draft_revision}
     language={language}/>}</div>
  </details>}
 </section>;
}
