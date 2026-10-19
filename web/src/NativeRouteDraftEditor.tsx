import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { AlertTriangle, CheckCircle2, RefreshCw, Route } from "lucide-react";
import { api, ApiError } from "./session";
import "./NativeRouteDraftEditor.css";

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
type Saved=RouteDraft & {
 draft_revision:number;listen_ip:string;native_direct_port:number;
 native_swap_port:number;billing_source_node_id:string;
 apply_allowed:boolean;agent_ack_verified:boolean;
 data_plane_verified:boolean;billing_verified:boolean;
 subscription_published:boolean;
};
const words={
 fa:{title:"مسیریابی جدید اینباند — پیش‌نویس امن",
  description:"این فرم فقط طرح مسیر را ذخیره می‌کند؛ روی Xray، نود یا اشتراک هیچ Apply انجام نمی‌شود.",
  select:"انتخاب نود مقصد",direct:"پورت مستقیم",directHosts:"Hostهای مستقیم (حداکثر ۶۴)",
  tunnel:"پورت تونل معمولی",tunnelHosts:"Hostهای تونل",
  swap:"فعال‌کردن Dark Swap در پیش‌نویس",
  upstream:"نود تونل بالادستی",host:"آدرس تونل",tunnelPort:"پورت مقصد تونل",
  nodePort:"پورت نود مقصد",confirm:"تغییر مسیر قبلی بررسی شده و فقط برای پیش‌نویس تأیید می‌کنم.",
  reason:"دلیل ویرایش برای حسابرسی",preview:"بررسی تداخل و Revision",
  save:"ذخیرهٔ امن پیش‌نویس",nothing:"هیچ پیش‌نویسی برای این نود ثبت نشده.",
  saved:"طرح ذخیره شد؛ منتظر Apply و آزمون مستقل Agent/Xray است.",
  checked:"پیش‌نمایش معتبر است؛ هنوز Apply مجاز نیست.",
  error:"دسترسی یا وضعیت سرور تأیید نشد. عملیات مسدود است.",
  expected:"Revision اینباند",draftRev:"Revision پیش‌نویس",
  legacy:"پورت Swap قدیمی به‌تنهایی نشان‌دهندهٔ تونل فعال نیست.",
  noApply:"Agent ACK: تأییدنشده · Xray: تأییدنشده · حسابداری: تأییدنشده",
  },
 en:{title:"Inbound routing — protected draft only",
  description:"This saves a route draft only. No Xray, Agent, client, or subscription Apply.",
  select:"Destination Node",direct:"Direct port",directHosts:"Direct hosts (64 maximum)",
  tunnel:"Ordinary Tunnel port",tunnelHosts:"Tunnel hosts",
  swap:"Plan Dark Swap (not activation)",
  upstream:"Upstream tunnel Node",host:"Tunnel address",tunnelPort:"Upstream tunnel port",
  nodePort:"Destination Node port",confirm:"I reviewed existing bindings; save draft only.",
  reason:"Required audit reason",preview:"Check ports and revision",
  save:"Save protected draft",nothing:"No saved route draft for this Node.",
  saved:"Route draft saved; Agent/Xray acceptance still required.",
  checked:"Preview valid; runtime Apply is still forbidden.",
  error:"Server state or account access could not be verified. Actions blocked.",
  expected:"Inbound revision",draftRev:"Draft revision",
  legacy:"Old native swap_port is not proof of an active tunnel.",
  noApply:"Agent ACK: unverified · Xray: unverified · billing: unverified",
  }
} as const;
const comma=(value:string)=>value.split(",").map(x=>x.trim()).filter(Boolean);
function blank(id:number,rev:number,node:NativeTarget):RouteDraft{
 return {event_id:"",inbound_id:id,node_id:node.node_id,
  installation_id:node.installation_id,expected_inbound_revision:rev,
  expected_draft_revision:0,direct_port:node.direct_port,direct_hosts:[],
  tunnel_port:0,tunnel_hosts:[],swap_enabled:false,
  swap_upstream_node_id:"",swap_tunnel_host:"",swap_tunnel_port:0,swap_node_port:0,
  confirm_existing_route_disable:false,reason:""};
}
export default function NativeRouteDraftEditor({inboundID,inboundRevision,nodes,targets,language,csrf,blocked}:{
 inboundID:number;inboundRevision:number;nodes:NodeInfo[];
 targets:NativeTarget[];language:"fa"|"en";csrf:string;blocked:boolean;
}){
 const t=words[language];
 const [nodeID,setNodeID]=useState("");
 const [form,setForm]=useState<RouteDraft|null>(null);
 const [loaded,setLoaded]=useState(false);
 const [preview,setPreview]=useState(false);
 const [message,setMessage]=useState("");
 const [busy,setBusy]=useState(false);
 const pendingID=useRef<string|null>(null);
 const available=targets.filter(x=>x.inbound_id===inboundID&&
  nodes.some(n=>n.node_id===x.node_id&&n.installation_id===x.installation_id));
 const selected=available.find(x=>x.node_id===nodeID);
 useEffect(()=>{setNodeID("");setForm(null);setLoaded(false);setPreview(false)},[inboundID]);
 useEffect(()=>{
  const ctrl=new AbortController();setLoaded(false);setPreview(false);setMessage("");
  pendingID.current=null;
  if(!selected){setForm(null);return()=>ctrl.abort()}
  setForm(blank(inboundID,inboundRevision,selected));
  const uri="/api/v1/workspace/native-inbounds/"+inboundID+"/route-drafts/"+encodeURIComponent(nodeID);
  void api<Saved>(uri,{signal:ctrl.signal}).then(existing=>{
   if(ctrl.signal.aborted)return;
   if(existing.inbound_id!==inboundID||existing.node_id!==nodeID||
    existing.installation_id!==selected.installation_id||existing.apply_allowed!==false)
    throw Error("unverified_saved_route");
   setForm({...existing,expected_inbound_revision:inboundRevision,
    expected_draft_revision:existing.draft_revision,
    event_id:"",reason:""});
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
 function update(patch:Partial<RouteDraft>){
  setForm(before=>before?{...before,...patch}:null);
  setPreview(false);pendingID.current=null;setMessage("");
 }
 function number(e:ChangeEvent<HTMLInputElement>){
  const value=Number(e.target.value);
  return Number.isSafeInteger(value)&&value>=0&&value<=65535?value:0;
 }
 const canAct=!!form&&loaded&&!blocked&&!busy&&csrf.length>0&&
  form.reason.trim().length>0&&form.reason.trim().length<=200;
 async function action(save:boolean){
  if(!form||!canAct||save&&!preview)return;
  const id=pendingID.current||crypto.randomUUID();
  pendingID.current=id;setBusy(true);setMessage("");
  const uri="/api/v1/workspace/native-inbounds/"+inboundID+"/route-drafts"+(save?"":"/preview");
  const body={...form,event_id:id};
  try{
   const result=await api<any>(uri,{method:"POST",headers:{
    "Content-Type":"application/json","X-CSRF-Token":csrf},body:JSON.stringify(body)});
   if(save){
    if(!result||result.draft?.apply_allowed!==false||
     result.clients_deleted!==false||result.credit_changed!==false||
     result.runtime_changed!==false)throw Error("invalid_draft_receipt");
    pendingID.current=null;setForm(v=>v?{...v,expected_draft_revision:result.draft.draft_revision,reason:""}:v);setMessage(t.saved);setPreview(false);
   }else{
    if(!result||result.valid!==true||result.apply_allowed!==false)throw Error("invalid_preview");
    setMessage(t.checked);setPreview(true);
   }
  }catch{setMessage(t.error);setPreview(false)}
  finally{setBusy(false)}
 }
 if(!nodes.length)return null;
 return <section className="native-card inbound-route-editor" aria-label="Protected Inbound routing draft">
  <h3><Route size={18}/>{t.title}</h3>
  <p>{t.description}</p>
  <label>{t.select}<select aria-label="Route destination Node" value={nodeID}
    onChange={e=>setNodeID(e.target.value)}>
    <option value="">—</option>
    {available.map(x=><option key={x.node_id} value={x.node_id}>
      {nodes.find(n=>n.node_id===x.node_id)?.name||x.node_id} ({x.node_id})</option>)}
  </select></label>
  {selected&&form&&loaded&&<>
   <div className="route-revision"><span>{t.expected}: {inboundRevision}</span>
    <span>{t.draftRev}: {form.expected_draft_revision}</span></div>
   <div className="native-fields">
    <label>{t.direct}<input aria-label="Route Direct port" type="number" min="0" max="65535"
      value={form.direct_port} onChange={e=>update({direct_port:number(e)})}/></label>
    <label>{t.directHosts}<input aria-label="Route Direct hosts" value={form.direct_hosts.join(", ")}
      onChange={e=>update({direct_hosts:comma(e.target.value)})}/></label>
    <label>{t.tunnel}<input aria-label="Route Tunnel port" type="number" min="0" max="65535"
      value={form.tunnel_port} onChange={e=>update({tunnel_port:number(e)})}/></label>
    <label>{t.tunnelHosts}<input aria-label="Route Tunnel hosts" value={form.tunnel_hosts.join(", ")}
      onChange={e=>update({tunnel_hosts:comma(e.target.value)})}/></label>
   </div>
   <label className="route-boolean"><input aria-label="Route Dark Swap" type="checkbox"
      checked={form.swap_enabled} onChange={e=>update({
       swap_enabled:e.target.checked,
       ...(!e.target.checked?{swap_upstream_node_id:"",swap_tunnel_host:"",
        swap_tunnel_port:0,swap_node_port:0}:{})})}/>
      {t.swap}</label>
   {form.swap_enabled&&<div className="native-fields">
    <label>{t.upstream}<select aria-label="Route upstream Node" value={form.swap_upstream_node_id}
      onChange={e=>update({swap_upstream_node_id:e.target.value})}><option value="">—</option>
      {available.filter(x=>x.node_id!==nodeID).map(x=><option key={x.node_id}
       value={x.node_id}>{x.node_id}</option>)}</select></label>
    <label>{t.host}<input aria-label="Route Swap host" value={form.swap_tunnel_host}
      onChange={e=>update({swap_tunnel_host:e.target.value})}/></label>
    <label>{t.tunnelPort}<input aria-label="Route Swap tunnel port" type="number" min="0" max="65535"
      value={form.swap_tunnel_port} onChange={e=>update({swap_tunnel_port:number(e)})}/></label>
    <label>{t.nodePort}<input aria-label="Route Swap destination port" type="number" min="0" max="65535"
      value={form.swap_node_port} onChange={e=>update({swap_node_port:number(e)})}/></label>
   </div>}
   <p className="native-warning"><AlertTriangle size={14}/>{t.legacy} {t.noApply}</p>
   <label className="route-boolean"><input aria-label="Route confirm disabled bindings" type="checkbox"
    checked={form.confirm_existing_route_disable}
    onChange={e=>update({confirm_existing_route_disable:e.target.checked})}/>{t.confirm}</label>
   <label>{t.reason}<input aria-label="Route audit reason" maxLength={200} value={form.reason}
    onChange={e=>update({reason:e.target.value})}/></label>
   <div className="native-actions">
    <button type="button" disabled={!canAct} onClick={()=>void action(false)}>
      <RefreshCw size={15}/>{t.preview}</button>
    <button type="button" disabled={!canAct||!preview} onClick={()=>void action(true)}>
      <CheckCircle2 size={15}/>{t.save}</button>
   </div>
  </>}
  {message&&<p role="status" className="native-notice">{message}</p>}
 </section>;
}
