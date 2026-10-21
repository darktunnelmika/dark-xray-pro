import { useEffect, useRef, useState } from "react";
import { api, mutation, useSession, type Language } from "./session";
import "./NodeReplacements.css";

type Operation = {
  id:string; old_node_id:string; new_node_id:string; phase:string;
  revision:number; created_at:string; can_activate:false;
  preview:{manifest_sha256:string;deployments:unknown[]};
};
type Listing = {node_id:string;operations:Operation[];can_activate:false};
const copy = {
  en:{open:"Replacement preflights",title:"Replacement preflight journal",close:"Close",refresh:"Refresh",empty:"No replacement preflights for this node.",failed:"Journal could not be verified; outcome unknown.",boundary:"Preflight only. TCP reachability is not real Xray validation or safe traffic cutover. No activation is available.",cancel:"Cancel preflight",confirm:"Cancel this preflight? This releases the reserved nodes but preserves history. No traffic changes.",cancelled:"Cancellation recorded. No traffic or subscriptions changed.",conflict:"Revision changed; reload the journal before deciding again.",revision:"Revision",manifest:"Manifest",routes:"Route entries",pending:"Pending TCP check",checked:"TCP checked (not activated)",failedProbe:"TCP check failed",terminal:"Cancelled"},
  fa:{open:"پیش‌بررسی‌های جایگزینی",title:"دفتر پیش‌بررسی جایگزینی",close:"بستن",refresh:"بازخوانی",empty:"برای این نود پیش‌بررسی جایگزینی ثبت نشده است.",failed:"اصالت دفتر قابل تأیید نیست؛ نتیجه نامعلوم است.",boundary:"فقط پیش‌بررسی؛ دسترسی TCP به‌معنی تأیید واقعی Xray یا انتقال ایمن ترافیک نیست. فعال‌سازی وجود ندارد.",cancel:"لغو پیش‌بررسی",confirm:"این پیش‌بررسی لغو شود؟ رزرو نودها آزاد می‌شود ولی سابقه باقی می‌ماند؛ ترافیک تغییر نمی‌کند.",cancelled:"لغو ثبت شد؛ ترافیک و اشتراک‌ها تغییر نکردند.",conflict:"Revision تغییر کرده؛ قبل از تصمیم تازه، دفتر را بازخوانی کن.",revision:"نسخه",manifest:"هش برنامه",routes:"مسیرها",pending:"در انتظار بررسی TCP",checked:"TCP بررسی شده؛ فعال نیست",failedProbe:"بررسی TCP ناموفق",terminal:"لغوشده"},
} as const;

function valid(data:Listing,id:string):Operation[] {
  if(data?.node_id!==id || data.can_activate!==false || !Array.isArray(data.operations) || data.operations.length>1000)throw Error("invalid_journal");
  for(const op of data.operations){
    if(!/^[a-f0-9]{32}$/.test(op.id) || !Number.isSafeInteger(op.revision) || op.revision<1 ||
      !["pending_tcp_preflight","tcp_preflight_failed","tcp_preflight_passed_not_activated","cancelled"].includes(op.phase) ||
      op.can_activate!==false || !/^[a-f0-9]{64}$/.test(op.preview?.manifest_sha256) ||
      !Array.isArray(op.preview.deployments) || ![op.old_node_id,op.new_node_id].includes(id))throw Error("invalid_operation");
  }
  return data.operations;
}

export default function NodeReplacements({nodeID,language}:{nodeID:string;language:Language}) {
  const t=copy[language],{session}=useSession();
  const [open,setOpen]=useState(false),[busy,setBusy]=useState(false),[rows,setRows]=useState<Operation[]|null>(null),[notice,setNotice]=useState(""),[error,setError]=useState("");
  const active=useRef<AbortController|null>(null);
  useEffect(()=>()=>{active.current?.abort();active.current=null;},[]);
  const path="/api/v1/workspace/nodes/"+encodeURIComponent(nodeID)+"/replacements";
  async function read() {
    active.current?.abort();const controller=new AbortController();active.current=controller;
    setBusy(true);setError("");setNotice("");setRows(null);
    try { const result=valid(await api<Listing>(path,{signal:controller.signal}),nodeID);if(active.current===controller)setRows(result); }
    catch {if(active.current===controller)setError(t.failed);}
    finally {if(active.current===controller){active.current=null;setBusy(false);}}
  }
  async function cancel(op:Operation) {
    if(!session || busy || op.phase==="cancelled" || !window.confirm(t.confirm))return;
    setBusy(true);setError("");setNotice("");
    try {
      const result=await mutation<Operation>(path+"/"+op.id+"/cancel",{expected_revision:op.revision},session.csrf_token);
      if(result.id!==op.id || result.phase!=="cancelled" || result.can_activate!==false)throw Error("invalid_cancel_receipt");
      setRows(null);await read();setNotice(t.cancelled);
    } catch {setRows(null);setError(t.conflict);}
    finally {setBusy(false);}
  }
  if(!open)return <button type="button" onClick={()=>{setOpen(true);void read();}}>{t.open}</button>;
  return <section className="pro-node-replacements" aria-label={t.title}>
    <h4>{t.title}</h4><p>{t.boundary}</p>
    <div className="pro-node-replacements-actions"><button type="button" disabled={busy} onClick={()=>void read()}>{t.refresh}</button><button type="button" onClick={()=>{active.current?.abort();active.current=null;setOpen(false);setRows(null);setError("");setNotice("");}}>{t.close}</button></div>
    {error&&<p role="alert">{error}</p>}{notice&&<p role="status">{notice}</p>}
    {rows?.length===0&&<p>{t.empty}</p>}
    {rows?.map(op=><article key={op.id}>
      <strong>{op.old_node_id} → {op.new_node_id}</strong>
      <small>{({pending_tcp_preflight:t.pending,tcp_preflight_failed:t.failedProbe,tcp_preflight_passed_not_activated:t.checked,cancelled:t.terminal} as Record<string,string>)[op.phase]}</small>
      <small>{t.revision}: {op.revision} · {t.routes}: {op.preview.deployments.length}</small>
      <small>{t.manifest}: <code dir="ltr">{op.preview.manifest_sha256}</code></small>
      {op.phase!=="cancelled"&&<button type="button" disabled={busy} onClick={()=>void cancel(op)}>{t.cancel}</button>}
    </article>)}
  </section>;
}
