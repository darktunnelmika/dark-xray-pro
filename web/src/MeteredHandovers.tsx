import { useEffect, useRef, useState } from "react";
import { api, ApiError, useSession, type Language } from "./session";
import { expected, readChannel, readCommand, readScope, type Channel, type CommandRequest, type Scope } from "./meteredRouteTransactions";
import { commandScope, readHandover, readHandoverAttempt, readHealthIntent, readHealthRound, readPage, sameUsage, type Handover, type HandoverAttempt, type HealthRound, type Page, type Pair, type Plan } from "./meteredHandovers";
import "./MeteredHandovers.css";
import LabSubscriptionSnapshot from "./LabSubscriptionSnapshot";

const words = {
 en: {
  healthRound:"Test all destination Inbounds",rounds:"Destination health rounds",roundScope:"The server includes every Inbound in the applied destination configuration. This bounded LAB protocol test does not publish subscriptions or verify a public endpoint.",roundConfirm:"Run one bounded protocol test round for every destination Inbound with system probe identities? This does not publish subscriptions or renew the lease.",roundPending:"Round pending · every Inbound must return an accepted receipt",roundCurrent:"All destination Inbounds verified · current bounded protocol check",roundExpired:"Round expired · current health is not confirmed",roundChanged:"Destination or source state changed · this round is no longer current",roundHistorical:"Complete receipt set recorded · current health is not confirmed",roundUnconfirmed:"Current round status could not be verified",roundCoverage:"Accepted Inbounds",memberAccepted:"Receipt recorded",memberPending:"Receipt pending",
  health:"Request protocol health test",healthInbound:"Inbound to test",healthQueued:"Health request recorded · waiting for Agent or receipt",healthExpired:"Health request expired · no success confirmed",healthHistorical:"Historical protocol health receipt recorded · no subscription published",healthConfirm:"Run this bounded test with the system probe identity? This does not publish subscriptions or renew the lease.",
  title:"Transfer between LAB Nodes",open:"Open Node transfer",close:"Close transfer",lead:"Prepare the destination, review both Nodes, then request source settlement and destination execution separately. Only approved test Nodes are available.",
  disabled:"Node execution is disabled on this HUB. Recorded transfers remain readable.",missing:"Select two different approved Nodes.",source:"Previous Node",target:"New Node",refresh:"Refresh transfer status",preview:"Review transfer",prepare:"Prepare destination",create:"Record reviewed transfer",drain:"Stop and settle previous Node",apply:"Request destination execution",cancel:"Cancel before source stop",retry:"Retry exact request",discard:"Discard local retry",latest:"Latest transfers",older:"Older transfers",empty:"No recorded transfers.",history:"Transfer history",select:"View transfer",new:"New transfer",loading:"Checking transfer state…",error:"Transfer state could not be verified. Execution controls are blocked.",unknown:"The response was not confirmed. The exact request is retained. Refresh to reconcile or retry it; no next step runs automatically.",storage:"The request could not be saved in this tab. Nothing new was sent.",accepted:"Request recorded. Review the committed receipt and current Agent observation separately.",stale:"A fresh observation from both Nodes is required.",notReady:"The previous Node must report a current process and unsettled usage. The destination needs a complete valid draft and an acknowledged prepared configuration for the same Inbounds.",stage:"Prepared configuration",epoch:"Source usage epoch",ports:"Ports",inbounds:"Inbounds",receipt:"Committed command receipt",noReceipt:"No committed command receipt",publication:"Protocol health is a bounded LAB check. Public subscription publication and cross-Node rollback remain unavailable. Execution has a maximum 30-second lease without automatic renewal.",
  confirm:"Send this separately reviewed LAB action? Existing test connections may close.",confirmCreate:"Record this exact transfer and reserve both Nodes? This does not start or stop a process.",confirmDiscard:"Discard only the browser's saved retry? This does not cancel the server operation or prove non-execution. Review the history before another action.",
  nodeReady:"Agent reports ready · lease current",nodeStopped:"Agent reports process not ready",nodeExpired:"Lease expired · current process state unconfirmed",nodeStale:"Current process state unconfirmed · observation stale",
  phases:{awaiting_source_drain:"Awaiting previous Node stop",source_drain_pending:"Source stop queued · receipt pending",source_drained:"Source stop and settlement ACK recorded",target_apply_pending:"Destination execution queued · receipt pending",target_applied_health_required:"Destination Apply ACK recorded · review current health below",recovery_required:"Recovery required · execution may be unknown; both Nodes remain reserved",cancelled:"Cancelled before source stop"},
 },
 fa: {
  healthRound:"آزمون همهٔ اینباندهای مقصد",rounds:"دوره‌های آزمون سلامت مقصد",roundScope:"سرور همهٔ اینباندهای تنظیمات اجراشدهٔ مقصد را وارد آزمون می‌کند. این آزمون محدود پروتکل در LAB اشتراکی منتشر نمی‌کند و دسترسی عمومی را تأیید نمی‌کند.",roundConfirm:"یک دورهٔ محدود آزمون پروتکل برای همهٔ اینباندهای مقصد با هویت‌های سیستمی اجرا شود؟ این کار اشتراکی منتشر نمی‌کند و مجوز اجرا را تمدید نمی‌کند.",roundPending:"دوره در انتظار رسید است · رسید همهٔ اینباندها باید پذیرفته شود",roundCurrent:"همهٔ اینباندهای مقصد تأیید شدند · آزمون محدود پروتکل فعلاً معتبر است",roundExpired:"دوره منقضی شد · سلامت فعلی تأیید نیست",roundChanged:"وضعیت مبدأ یا مقصد تغییر کرده · این دوره دیگر معتبر نیست",roundHistorical:"مجموعهٔ کامل رسیدها ثبت شده · سلامت فعلی تأیید نیست",roundUnconfirmed:"وضعیت فعلی دورهٔ آزمون تأیید نشد",roundCoverage:"اینباندهای دارای رسید پذیرفته‌شده",memberAccepted:"رسید ثبت شده",memberPending:"در انتظار رسید",
  health:"درخواست آزمون سلامت پروتکل",healthInbound:"اینباند آزمون",healthQueued:"درخواست سلامت ثبت شد · در انتظار Agent یا رسید",healthExpired:"درخواست سلامت منقضی شد · موفقیت تأیید نشده",healthHistorical:"رسید تاریخی سلامت پروتکل ثبت شده · اشتراکی منتشر نشده",healthConfirm:"آزمون محدود با هویت سیستمی اجرا شود؟ این کار اشتراکی منتشر نمی‌کند و مجوز اجرا را تمدید نمی‌کند.",
  title:"انتقال بین نودهای LAB",open:"بازکردن انتقال نود",close:"بستن انتقال",lead:"نود مقصد را آماده کن و پیش‌نمایش دو نود را ببین؛ توقف و تسویهٔ نود قبلی و اجرای نود جدید هرکدام درخواست جداگانه دارند. فقط نودهای آزمایشی مجاز قابل انتخاب‌اند.",
  disabled:"اجرای نود در این HUB غیرفعال است؛ تاریخچهٔ انتقال‌ها قابل مشاهده است.",missing:"دو نود مجاز متفاوت انتخاب کن.",source:"نود قبلی",target:"نود جدید",refresh:"به‌روزرسانی وضعیت انتقال",preview:"بررسی انتقال",prepare:"آماده‌سازی نود مقصد",create:"ثبت انتقال بررسی‌شده",drain:"توقف و تسویهٔ نود قبلی",apply:"درخواست اجرای نود جدید",cancel:"لغو پیش از توقف نود قبلی",retry:"تلاش مجدد همان درخواست",discard:"کنارگذاشتن تلاش ذخیره‌شده",latest:"آخرین انتقال‌ها",older:"انتقال‌های قدیمی‌تر",empty:"هنوز انتقالی ثبت نشده است.",history:"تاریخچهٔ انتقال",select:"مشاهدهٔ انتقال",new:"انتقال تازه",loading:"در حال بررسی وضعیت انتقال…",error:"وضعیت انتقال تأیید نشد؛ کنترل‌های اجرا مسدودند.",unknown:"پاسخ تأیید نشد؛ همان درخواست حفظ شده است. برای تطبیق وضعیت، بازخوانی کن یا همان درخواست را دوباره بفرست. مرحلهٔ بعد خودکار اجرا نمی‌شود.",storage:"ذخیرهٔ درخواست در این تب ممکن نشد؛ درخواست تازه‌ای ارسال نشد.",accepted:"درخواست ثبت شد؛ رسید قطعی و آخرین مشاهدهٔ Agent را جداگانه بررسی کن.",stale:"مشاهدهٔ تازه از هر دو نود لازم است.",notReady:"نود قبلی باید فرآیند فعلی و مصرف تسویه‌نشده گزارش کند. نود مقصد به پیش‌نویس کامل معتبر و رسید آماده‌سازی برای همان اینباندها نیاز دارد.",stage:"تنظیمات آماده‌شده",epoch:"دورهٔ مصرف نود قبلی",ports:"پورت‌ها",inbounds:"اینباندها",receipt:"رسید قطعی فرمان",noReceipt:"رسید قطعی فرمان ثبت نشده",publication:"آزمون سلامت پروتکل محدود و مخصوص LAB است. انتشار اشتراک عمومی و بازگشت بین دو نود هنوز در دسترس نیستند. اجرای آزمایشی مجوز حداکثر ۳۰ ثانیه دارد و خودکار تمدید نمی‌شود.",
  confirm:"همین اقدام بررسی‌شدهٔ LAB ارسال شود؟ اتصال‌های آزمایشی ممکن است قطع شوند.",confirmCreate:"همین انتقال ثبت و هر دو نود رزرو شوند؟ این مرحله هیچ فرآیندی را شروع یا متوقف نمی‌کند.",confirmDiscard:"فقط درخواست ذخیره‌شدهٔ مرورگر کنار گذاشته شود؟ این کار عملیات سرور را لغو نمی‌کند و اثبات اجرا‌نشدن نیست. پیش از اقدام تازه، تاریخچه را بررسی کن.",
  nodeReady:"Agent آمادگی را گزارش می‌کند · مجوز معتبر",nodeStopped:"Agent آماده‌نبودن فرآیند را گزارش می‌کند",nodeExpired:"مجوز منقضی · وضعیت فعلی فرآیند نامشخص",nodeStale:"وضعیت فعلی فرآیند نامشخص · مشاهده قدیمی است",
  phases:{awaiting_source_drain:"در انتظار توقف نود قبلی",source_drain_pending:"توقف نود قبلی در صف · در انتظار رسید",source_drained:"رسید توقف و تسویهٔ نود قبلی ثبت شده",target_apply_pending:"اجرای مقصد در صف · در انتظار رسید",target_applied_health_required:"ACK اجرای مقصد ثبت شده · وضعیت فعلی سلامت را در ادامه بررسی کن",recovery_required:"نیازمند بازیابی · اجرای فرمان ممکن است نامعلوم باشد؛ هر دو نود رزرو می‌مانند",cancelled:"پیش از توقف نود قبلی لغو شده"},
 },
};
const base="/api/v1/workspace/metered-handovers";
const channelURL=(node:string)=>"/api/v1/workspace/native-nodes/"+encodeURIComponent(node)+"/metered-route-transactions";
type Snapshot={scope:Scope;page:Page;operation:Handover|null;source?:Channel;target?:Channel;mono:number;wall:number};
const now=(s:Snapshot,v:Channel)=>v.server_time_unix_milli+Math.max(0,performance.now()-s.mono,Date.now()-s.wall);
function fresh(s:Snapshot,v:Channel|undefined){return !!v&&v.observation_fresh&&v.reported_at_unix_nano>0&&v.reported_at_unix_nano/1e6<=now(s,v)+1000&&now(s,v)-v.reported_at_unix_nano/1e6<12000&&!v.commands.some(c=>(c.receipt&&c.receipt.observed_unix_nano>v.reported_at_unix_nano)||(c.state==="delivered"&&c.serial>v.status.serial));}
const uuid=()=>crypto.randomUUID().replaceAll("-","");
const key=(owner:string)=>"dark-pro-handover-attempt:"+owner;
const nodes=(scope:Scope)=>scope.nodes??(scope.enabled?[{node_id:scope.node_id!,installation_id:scope.installation_id!}]:[]);
function committed(a:HandoverAttempt,s:Snapshot){
 const b=JSON.parse(a.body),op=s.operation;
 if(a.action==="stage")return s.target?.commands.some(c=>c.id===b.id&&c.action==="stage"&&c.stage_id===b.id);
 if(!op||op.id!==a.operation||op.source_node!==a.pair.source_node||op.target_node!==a.pair.target_node||op.source_installation!==a.pair.source_installation||op.target_installation!==a.pair.target_installation)return false;
 if(a.action==="create")return op.source_stage_id===b.source_stage_id&&op.target_stage_id===b.target_stage_id&&sameUsage(op.source_usage,b.source_usage);
 if(a.action==="cancel")return op.cancelled;
 if(a.action==="health")return op.health_intents?.some(i=>i.id===b.id&&i.inbound_id===b.inbound_id);
 if(a.action==="health-round")return op.health_rounds?.some(r=>r.id===b.id);
 const c=a.action==="drain"?op.drain_command:op.apply_command;
 return !!c&&c.id===b.id&&c.action===a.action&&c.stage_id===b.stage_id;
}

export default function MeteredHandovers({language}:{language:Language}){
 const t=words[language],{session}=useSession(),owner=session?.user.role==="owner"?session.user.id:"";
 const [open,setOpen]=useState(false),[source,setSource]=useState(""),[target,setTarget]=useState(""),[selected,setSelected]=useState(""),[cursor,setCursor]=useState("");
 const [healthInbound,setHealthInbound]=useState("");
 const [snapshot,setSnapshot]=useState<Snapshot|null>(null),[preview,setPreview]=useState<Plan|null>(null),[attempt,setAttempt]=useState<HandoverAttempt|null>(null);
 const [busy,setBusy]=useState(false),[verified,setVerified]=useState(false),[error,setError]=useState(false),[storageError,setStorageError]=useState(false),[accepted,setAccepted]=useState(false),[refresh,setRefresh]=useState(0),[,tick]=useState(0);
 const retained=useRef<HandoverAttempt|null>(null),sending=useRef(false),reader=useRef<AbortController|null>(null),lifetime=useRef<AbortController|null>(null);
 function remember(a:HandoverAttempt|null){try{if(a)sessionStorage.setItem(key(owner),JSON.stringify(a));else sessionStorage.removeItem(key(owner));retained.current=a;setAttempt(a);setStorageError(false);return true;}catch{setStorageError(true);return false;}}
 useEffect(()=>{
  const ctrl=new AbortController();lifetime.current=ctrl;setSnapshot(null);setVerified(false);setPreview(null);setAccepted(false);setSource("");setTarget("");setSelected("");setCursor("");
  let a:HandoverAttempt|null=null;try{if(owner&&open)a=readHandoverAttempt(sessionStorage.getItem(key(owner)));}catch{setStorageError(true);}
  retained.current=a;setAttempt(a);if(a){setSource(a.pair.source_node);setTarget(a.pair.target_node);setSelected(a.operation);}
  return()=>ctrl.abort();
 },[owner,open]);
 useEffect(()=>{if(!open||!owner)return;const timer=setInterval(()=>tick(v=>v+1),500);return()=>clearInterval(timer);},[open,owner]);
 useEffect(()=>{
  if(!open||!owner)return;const ctrl=new AbortController();reader.current=ctrl;setVerified(false);let timer:ReturnType<typeof setTimeout>;
  async function read(){
   try{
    const [rawScope,rawPage]=await Promise.all([api<unknown>("/api/v1/workspace/metered-route-transactions",{signal:ctrl.signal}),api<unknown>(base+(cursor?"?before="+cursor:""),{signal:ctrl.signal})]);
    const scope=readScope(rawScope),page=readPage(rawPage),list=nodes(scope);
    let operation:Handover|null=null;
    if(selected){try{operation=readHandover(await api<unknown>(base+"/"+selected,{signal:ctrl.signal}));if(operation.id!==selected||(scope.health_rounds_enabled&&operation.health_rounds===undefined))throw Error("wrong_operation");}catch(e){if(!(e instanceof ApiError&&e.status===404&&retained.current?.action==="create"&&retained.current.operation===selected))throw e;}}
    const pair=operation??retained.current?.pair;
    const src=pair?.source_node??source,dst=pair?.target_node??target;
    const get=async(id:string)=>{const n=list.find(n=>n.node_id===id);return n?readChannel(await api<unknown>(channelURL(id),{signal:ctrl.signal}),{enabled:true,scope:scope.scope,...n}):undefined;};
    const [sourceView,targetView]=await Promise.all([get(src),get(dst)]);
    if(ctrl.signal.aborted)return;
    const next={scope,page,operation,source:sourceView,target:targetView,mono:performance.now(),wall:Date.now()};setSnapshot(next);setVerified(true);setError(false);
    if(retained.current&&committed(retained.current,next)){remember(null);setAccepted(true);}
   }catch{if(!ctrl.signal.aborted){setError(true);setVerified(false);}}
   finally{if(!ctrl.signal.aborted)timer=setTimeout(()=>void read(),3000);}
  }
  void read();return()=>{ctrl.abort();clearTimeout(timer);};
 },[owner,open,source,target,selected,cursor,refresh]);
 if(!owner)return null;
 const s=snapshot,op=s?.operation,src=s?.source,dst=s?.target;
 const pair:Pair|null=src&&dst&&src.node_id!==dst.node_id?{source_node:src.node_id,target_node:dst.node_id,source_installation:src.installation_id,target_installation:dst.installation_id}:null;
 const available=!!s&&verified&&!error&&!busy&&!attempt&&!!session?.csrf_token&&s.scope.enabled;
 const bothFresh=!!s&&fresh(s,src)&&fresh(s,dst);
 const matches=!!op&&!!pair&&Object.keys(pair).every(k=>pair[k as keyof Pair]===op[k as keyof Pair]);
 const quiet=(v:Channel)=>!v.commands.some(c=>c.state==="queued"||c.state==="delivered");
 const canPrepare=available&&bothFresh&&!selected&&!!pair&&!!dst?.controls.stage&&!dst.status.observed.process_ready&&quiet(dst);
 const canReview=available&&bothFresh&&!selected&&!!pair&&!!src?.drafts_valid&&!!dst?.drafts_valid&&!!src.controls.drain&&!!src.status.usage?.epoch_id&&src.status.observed.process_ready&&!!dst.controls.apply&&quiet(src)&&quiet(dst)&&src.review.length===dst.review.length&&src.review.every(r=>dst.review.some(d=>d.inbound_id===r.inbound_id&&d.inbound_revision===r.inbound_revision));
 const canDrain=available&&bothFresh&&matches&&op.phase==="awaiting_source_drain"&&!!src&&quiet(src)&&!!src.status.usage&&sameUsage(src.status.usage,op.source_usage)&&src.status.current_stage_id===op.source_stage_id;
 const canApply=available&&bothFresh&&matches&&op.phase==="source_drained"&&!!dst&&!!src&&!src.status.observed.process_ready&&src.status.usage?.settlement.sequence!=="0"&&dst.status.staged_id===op.target_stage_id&&quiet(dst);
 const canCancel=available&&matches&&!!op&&!op.cancelled&&!op.drain_command_id&&!op.apply_command_id;
 const roundsEnabled=s?.scope.health_rounds_enabled===true;
 const healthPending=!!op?.health_intents?.some(i=>!!s&&!!dst&&i.expires_unix_nano/1e6>now(s,dst));
 const roundActive=!!op?.health_rounds?.some(r=>!!s&&!!dst&&r.expires_unix_nano/1e6>now(s,dst));
 const healthReady=available&&bothFresh&&matches&&s.scope.health_enabled===true&&op?.phase==="target_applied_health_required"&&op.apply_command?.state==="acknowledged"&&op.apply_command.receipt?.outcome==="applied"&&!!dst&&quiet(dst)&&quiet(src!)&&!src!.status.observed.process_ready&&dst.status.observed.process_ready&&dst.status.observed.lease_expires_unix_nano/1e6>now(s,dst);
 const canHealth=healthReady&&!roundsEnabled&&!op.historical_health_evidence&&!healthPending&&dst.review.some(r=>String(r.inbound_id)===healthInbound);
 const canHealthRound=healthReady&&roundsEnabled&&!roundActive&&!healthPending&&dst.review.length>0;
 function roundStatus(r:HealthRound){
  if(r.state==="expired"||(s&&dst&&r.expires_unix_nano/1e6<=now(s,dst)))return t.roundExpired;
  if(r.state==="state_changed")return t.roundChanged;
  if(!s||!dst||!verified||error||!bothFresh||!matches||!dst.status.observed.process_ready||src?.status.observed.process_ready||dst.status.observed.lease_expires_unix_nano/1e6<=now(s,dst))return t.roundUnconfirmed;
  const coversDestination=r.members.length===dst.review.length&&dst.review.every(d=>r.members.some(m=>m.inbound_id===d.inbound_id&&m.port===d.direct_port&&d.tunnel_port===0));
  if(r.state==="complete")return r.current&&coversDestination?t.roundCurrent:t.roundHistorical;
  return t.roundPending;
 }
 async function send(a:HandoverAttempt){
  const ctrl=lifetime.current;if(sending.current||!ctrl||ctrl.signal.aborted||!session?.csrf_token||!remember(a))return;
  sending.current=true;setBusy(true);setVerified(false);setAccepted(false);reader.current?.abort();
  if(a.operation)setSelected(a.operation);
  const path=a.action==="stage"?channelURL(a.pair.target_node):a.action==="create"?base:base+"/"+a.operation+(a.action==="health-round"?"/health-rounds":a.action==="health"?"/health":a.action==="cancel"?"/cancel":"/commands");
  try{
   const raw=await api<unknown>(path,{method:"POST",headers:{"Content-Type":"application/json","X-CSRF-Token":session.csrf_token},body:a.body,signal:ctrl.signal});
   if(ctrl.signal.aborted)return;
   const b=JSON.parse(a.body);
   if(a.action==="health"){const v=readHealthIntent(raw,a.operation,a.pair.target_node,a.pair.target_installation);if(v.id!==b.id||v.inbound_id!==b.inbound_id)throw Error("mismatched_health_reply");}
   else if(a.action==="health-round"){const v=readHealthRound(raw,a.operation,a.pair.target_node,a.pair.target_installation);if(v.id!==b.id)throw Error("mismatched_health_round_reply");}
   else if(a.action==="create"||a.action==="cancel"){
    const v=readHandover(raw);if(v.id!==a.operation||v.source_node!==a.pair.source_node||v.target_node!==a.pair.target_node||v.source_installation!==a.pair.source_installation||v.target_installation!==a.pair.target_installation||(a.action==="cancel"&&!v.cancelled)||(a.action==="create"&&(v.source_stage_id!==b.source_stage_id||v.target_stage_id!==b.target_stage_id||!sameUsage(v.source_usage,b.source_usage))))throw Error("mismatched_plan_reply");
   }else{const c=readCommand(raw,commandScope(a.pair,a.action==="drain"));if(c.id!==b.id||c.action!==a.action||c.stage_id!==(a.action==="stage"?b.id:b.stage_id))throw Error("mismatched_step_reply");}
   remember(null);setAccepted(true);setPreview(null);
  }catch{/* Every unconfirmed response retains its exact request, including refusal after an earlier lost reply. */}
  finally{sending.current=false;if(!ctrl.signal.aborted){setBusy(false);setRefresh(n=>n+1);}}
 }
 function review(){if(!canReview||!pair||!src||!dst)return;setPreview({...pair,id:uuid(),source_stage_id:src.status.current_stage_id!,target_stage_id:dst.status.staged_id!,source_usage:structuredClone(src.status.usage!)});}
 function act(action:"stage"|"drain"|"apply"){
  if(!s||!pair||!(action==="stage"?canPrepare:action==="drain"?canDrain:canApply))return;
  if(!window.confirm(t.confirm+"\n"+pair.source_node+" → "+pair.target_node))return;
  if(!fresh(s,src)||!fresh(s,dst))return;
  const v=action==="drain"?src!:dst!;
  const body:CommandRequest={id:uuid(),action,stage_id:action==="stage"?"":action==="drain"?op!.source_stage_id:op!.target_stage_id,runtime_instance:v.status.instance,expected:expected(v.status.observed),usage:v.status.usage!,drafts:action==="stage"?v.review.map(r=>({inbound_id:r.inbound_id,inbound_revision:r.inbound_revision,draft_revision:r.draft_revision})):[]};
  void send({pair,action,operation:action==="stage"?"":op!.id,body:JSON.stringify(body)});
 }
 function record(){if(!canReview||!preview||!pair||!src||!dst||!sameUsage(preview.source_usage,src.status.usage!)||preview.source_stage_id!==src.status.current_stage_id||preview.target_stage_id!==dst.status.staged_id||!Object.keys(pair).every(k=>pair[k as keyof Pair]===preview[k as keyof Pair])||!window.confirm(t.confirmCreate)||!s||!fresh(s,src)||!fresh(s,dst))return;void send({pair,action:"create",operation:preview.id,body:JSON.stringify(preview)});}
 const retryAllowed=!!s&&verified&&!busy&&!!attempt&&s.scope.enabled&&(attempt.action!=="health-round"||roundsEnabled)&&(attempt.action!=="health"||s.scope.health_enabled===true)&&[commandScope(attempt.pair,true),commandScope(attempt.pair,false)].every(p=>nodes(s.scope).some(n=>n.node_id===p.node_id&&n.installation_id===p.installation_id));
 function requestHealth(){if(!canHealth||!s||!op||!pair||!dst||!window.confirm(t.healthConfirm)||!fresh(s,src)||!fresh(s,dst)||dst.status.observed.lease_expires_unix_nano/1e6<=now(s,dst))return;void send({pair,action:"health",operation:op.id,body:JSON.stringify({id:uuid(),inbound_id:Number(healthInbound),confirm_probe:true})});}
 function requestHealthRound(){if(!canHealthRound||!s||!op||!pair||!dst||!window.confirm(t.roundConfirm)||!fresh(s,src)||!fresh(s,dst)||dst.status.observed.lease_expires_unix_nano/1e6<=now(s,dst))return;void send({pair,action:"health-round",operation:op.id,body:JSON.stringify({id:uuid(),confirmed:true})});}
 return <section className="metered-handovers" aria-label="Metered Node transfer">
  <div className="handover-heading"><h3>{t.title}</h3><button type="button" disabled={busy} onClick={()=>setOpen(v=>!v)}>{open?t.close:t.open}</button></div>
  {open&&<><p>{t.lead}</p><p className="handover-boundary">{t.publication}</p>
   <div className="handover-actions"><button type="button" disabled={busy} onClick={()=>setRefresh(n=>n+1)}>{t.refresh}</button><button type="button" disabled={busy||!!attempt} onClick={()=>{setSelected("");setPreview(null);setAccepted(false);setRefresh(n=>n+1);}}>{t.new}</button></div>
   {error&&<p role="alert">{t.error}</p>}{storageError&&<p role="alert">{t.storage}</p>}{accepted&&<p role="status">{t.accepted}</p>}
   {attempt&&<div className="handover-unknown" role="status"><p>{t.unknown}</p><code>{attempt.operation||JSON.parse(attempt.body).id}</code><div className="handover-actions"><button type="button" disabled={!retryAllowed} onClick={()=>{if(attempt&&retryAllowed&&window.confirm((attempt.action==="health-round"?t.roundConfirm:t.confirm)+"\n"+t.retry))void send(attempt);}}>{t.retry}</button><button type="button" disabled={busy} onClick={()=>{if(window.confirm(t.confirmDiscard))remember(null);}}>{t.discard}</button></div></div>}
   {!s&&!error&&<p>{t.loading}</p>}{s&&!s.scope.enabled&&<p>{t.disabled}</p>}
   {s&&<><div className="handover-pair">{(["source","target"] as const).map(role=><label key={role}>{t[role]}<select aria-label={t[role]} value={op?(role==="source"?op.source_node:op.target_node):(role==="source"?source:target)} disabled={busy||!!attempt||!!selected} onChange={e=>{setVerified(false);setPreview(null);setAccepted(false);(role==="source"?setSource:setTarget)(e.target.value);}}><option value="">—</option>{nodes(s.scope).map(n=><option key={n.node_id} value={n.node_id}>{n.node_id}</option>)}</select></label>)}</div>
    {pair&&<div className="handover-preview">{[src!,dst!].map(v=><article key={v.node_id}><strong><bdi>{v.node_id}</bdi></strong><small>{!verified||!fresh(s,v)?t.nodeStale:!v.status.observed.process_ready?t.nodeStopped:v.status.observed.lease_expires_unix_nano/1e6<=now(s,v)?t.nodeExpired:t.nodeReady}</small><small>{t.stage}: <code>{v.node_id===src!.node_id?v.status.current_stage_id||"—":v.status.staged_id||"—"}</code></small><small>{t.inbounds}: {v.review.map(r=>`${r.name} (#${r.inbound_id} · ${r.inbound_revision}/${r.draft_revision})`).join(", ")}</small><small>{t.ports}: {v.review.flatMap(r=>[r.direct_port,r.tunnel_port]).filter(Boolean).join(", ")}</small></article>)}</div>}
    {!selected&&<><p>{!pair?t.missing:!bothFresh?t.stale:!canReview?t.notReady:""}</p><div className="handover-actions"><button type="button" disabled={!canPrepare} onClick={()=>act("stage")}>{t.prepare}</button><button type="button" disabled={!canReview} onClick={review}>{t.preview}</button></div>{preview&&<div className="handover-review" aria-label="Reviewed transfer"><p><bdi>{preview.source_node} → {preview.target_node}</bdi></p><small>{t.epoch}: <code>{preview.source_usage.epoch_id}</code></small><button type="button" disabled={!canReview} onClick={record}>{t.create}</button></div>}</>}
    {op&&<article className="handover-operation" aria-label="Selected transfer"><strong><bdi>{op.source_node} → {op.target_node}</bdi></strong><p>{t.phases[op.phase]}</p><code>{op.id}</code><small>{t.epoch}: <code>{op.source_usage.epoch_id}</code></small>{([op.drain_command,op.apply_command]).filter(Boolean).map(c=><div key={c!.id}><code>{c!.id}</code><small>{c!.action} · {c!.state}</small><small>{c!.receipt?`${t.receipt}: ${c!.receipt.outcome}`:t.noReceipt}</small></div>)}<div className="handover-actions"><button type="button" disabled={!canDrain} onClick={()=>act("drain")}>{t.drain}</button><button type="button" disabled={!canApply} onClick={()=>act("apply")}>{t.apply}</button><button type="button" disabled={!canCancel} onClick={()=>{if(op&&pair&&window.confirm(t.confirm))void send({pair,action:"cancel",operation:op.id,body:JSON.stringify({confirm_before_drain:true})});}}>{t.cancel}</button></div></article>}
    {op&&<div aria-label="Protocol health"><p role="status">{op.historical_health_evidence?t.healthHistorical:!roundsEnabled&&op.health_intents?.length?(healthPending?t.healthQueued:t.healthExpired):""}</p>{s.scope.health_enabled&&(roundsEnabled?<><p>{t.roundScope}</p><button type="button" disabled={!canHealthRound} onClick={requestHealthRound}>{t.healthRound}</button></>:<><label>{t.healthInbound}<select aria-label={t.healthInbound} value={healthInbound} disabled={busy||!!attempt} onChange={e=>setHealthInbound(e.target.value)}><option value="">—</option>{dst?.review.map(r=><option key={r.inbound_id} value={r.inbound_id}>{r.name} (#{r.inbound_id})</option>)}</select></label><button type="button" disabled={!canHealth} onClick={requestHealth}>{t.health}</button></>)}
     {!!op.health_rounds?.length&&<div aria-label={t.rounds}><h4>{t.rounds}</h4>{op.health_rounds.map(r=><article className="handover-operation" key={r.id}><code>{r.id}</code><p role="status">{roundStatus(r)}</p><p>{t.roundCoverage}: {r.members.filter(m=>m.state==="accepted").length}/{r.members.length}</p><ul>{r.members.map(m=><li key={m.inbound_id}>#{m.inbound_id} · {t.ports}: {m.port} · {m.state==="accepted"?t.memberAccepted:t.memberPending}</li>)}</ul></article>)}</div>}
    </div>}
    {op&&s.scope.lab_subscriptions_enabled===true&&<LabSubscriptionSnapshot key={owner+":"+op.id} language={language} operation={op} round={healthReady?op.health_rounds?.find(r=>r.current&&r.state==="complete"&&r.expires_unix_nano/1e6>now(s,dst!)&&r.members.length===dst!.review.length&&dst!.review.every(d=>r.members.some(m=>m.inbound_id===d.inbound_id&&m.port===d.direct_port&&d.tunnel_port===0))):undefined} ready={!!healthReady} retryReady={available&&!!matches} serverNow={dst?now(s,dst):Infinity}/>}
    <h4>{t.history}</h4>{s.page.operations.length===0&&<p>{t.empty}</p>}<div className="handover-history">{s.page.operations.map(o=><article key={o.id}><strong><bdi>{o.source_node} → {o.target_node}</bdi></strong><small>{t.phases[o.phase]}</small><code>{o.id}</code><button type="button" disabled={busy||!!attempt} onClick={()=>{setSelected(o.id);setPreview(null);setVerified(false);}}>{t.select}</button></article>)}</div><div className="handover-actions">{cursor&&<button type="button" disabled={busy} onClick={()=>setCursor("")}>{t.latest}</button>}{s.page.next_cursor&&<button type="button" disabled={busy} onClick={()=>setCursor(s.page.next_cursor)}>{t.older}</button>}</div>
   </>}
  </>}
 </section>;
}
