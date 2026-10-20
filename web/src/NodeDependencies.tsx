import { useEffect, useRef, useState } from "react";
import { api, type Language } from "./session";
import "./NodeDependencies.css";

const labels = {
  historical_stage_references:["Distinct retained command Stages","Stageهای یکتای محفوظ در فرمان‌ها"],historical_upstream_command_references:["Historical commands using this node upstream","فرمان‌های تاریخی وابسته به این نود بالادستی"],unmapped_historical_commands:["Commands without node maps (all nodes)","فرمان‌های بدون نقشهٔ وابستگی؛ همهٔ نودها"],
  saved_config_stages:["Saved configuration Stages","Stageهای تنظیمات ذخیره‌شده"],pending_config_stages:["Pending configuration Stages","Stageهای تنظیمات در انتظار تأیید"],foreign_config_stages:["Configuration Stages for other installations","Stageهای تنظیمات نصب‌های دیگر"],
  saved_lab_versions:["Saved LAB versions","نسخه‌های ذخیره‌شدهٔ LAB"],selected_lab_versions:["Selected LAB versions (not process state)","نسخه‌های منتخب LAB؛ مستقل از وضعیت فرایند"],previous_lab_versions:["Previous LAB versions","نسخه‌های قبلی LAB"],lab_selection_events:["LAB selection and rollback events","رویدادهای انتخاب و بازگردانی LAB"],foreign_lab_versions:["LAB versions for other installations","نسخه‌های LAB متعلق به نصب‌های دیگر"],
  inbound_targets:["Inbound targets (including archived)","اتصال‌های اینباند، شامل آرشیوشده"],
  active_inbound_targets:["Enabled non-archived targets","اتصال‌های فعال و آرشیونشده"],
  assigned_clients:["Distinct assigned clients","کلاینت‌های اختصاص‌یافتهٔ یکتا"],
  route_drafts:["Route drafts","پیش‌نویس‌های مسیر"], draft_events:["Draft audit events","سابقهٔ تغییر پیش‌نویس"],
  swap_upstream_references:["Drafts using this node for Dark Swap","پیش‌نویس‌های وابسته به این نود برای Dark Swap"],
  config_revisions:["Configuration revision records","رکوردهای نسخهٔ تنظیمات"],runtime_deliveries:["Runtime delivery records","رکوردهای تحویل تنظیمات"],runtime_reports:["Runtime report records","رکوردهای گزارش اجرا"],
  route_channels:["Route channels","کانال‌های مسیر"],route_commands:["All route commands","همهٔ فرمان‌های مسیر"],pending_route_commands:["Queued/delivered route commands","فرمان‌های مسیر در صف یا تحویل‌شده"],unknown_route_commands:["Route commands abandoned with unknown outcome","فرمان‌های مسیر رهاشده با نتیجهٔ نامعلوم"],
  metered_channels:["Metered channels","کانال‌های مسیر حسابداری‌شده"],metered_commands:["All metered commands","همهٔ فرمان‌های حسابداری‌شده"],pending_metered_commands:["Queued/delivered metered commands","فرمان‌های حسابداری‌شده در صف یا تحویل‌شده"],unknown_metered_commands:["Metered commands abandoned with unknown outcome","فرمان‌های حسابداری‌شده رهاشده با نتیجهٔ نامعلوم"],metered_births:["Metered process birth records","رکوردهای ایجاد فرایند حسابداری‌شده"],
  foreign_installation_rows:["Native records for other installations","رکوردهای Native متعلق به نصب‌های دیگر"],
  usage_epochs:["All usage epochs","همهٔ دوره‌های حسابداری"],open_usage_epochs:["Open usage epochs","دوره‌های حسابداری باز"],uncertain_usage_epochs:["Uncertain usage epochs","دوره‌های حسابداری نامطمئن"],sealed_usage_epochs:["Sealed usage epochs (retained history)","دوره‌های بسته‌شده؛ سابقهٔ محفوظ"],usage_holds:["Usage hold records","رکوردهای توقف تأیید حسابداری"],usage_receipts:["Usage receipt records","رسیدهای مصرف"],usage_deltas:["Usage delta records","رکوردهای تغییر مصرف"],foreign_usage_epochs:["Usage epochs for other installations","دوره‌های مصرف متعلق به نصب‌های دیگر"],
  replacement_operations:["Replacement history","سابقهٔ جایگزینی"],noncancelled_replacements:["Non-cancelled replacement operations","عملیات جایگزینی لغونشده"],
} as const;
type Snapshot = { node_id:string;installation_id:string;observed_at:string;coverage:string;atomic_across_stores:false;can_retire:false;can_replace:false;lifecycle_blocker:string;counts:Record<string,number|null>;unknown_reasons:Record<string,string> };
const words = {
  en:{open:"Inspect node dependencies",title:"Node dependency snapshot",refresh:"Refresh dependencies",close:"Close dependencies",loading:"Reading dependencies…",failed:"Dependencies could not be verified. Counts are unavailable, not zero.",unknown:"Unknown",boundary:"Read-only snapshot across separate stores, not atomic. Empty counts do NOT authorize deletion or replacement. Saved Stages and LAB selections are included. Legacy command node maps may be unknown. Backups and live process/socket state are outside this inspection.",at:"Observed at",key:"Encryption key unavailable",ledger:"Usage ledger disabled",journal:"Replacement journal disabled",limit:"Draft inspection limit reached"},
  fa:{open:"بررسی وابستگی‌های نود",title:"تصویر وابستگی‌های نود",refresh:"بازخوانی وابستگی‌ها",close:"بستن وابستگی‌ها",loading:"در حال بررسی وابستگی‌ها…",failed:"وابستگی‌ها قابل تأیید نیستند؛ شمارنده‌ها نامعلوم‌اند، نه صفر.",unknown:"نامعلوم",boundary:"گزارش فقط‌خواندنی از مخزن‌های جداگانه است و یکپارچهٔ اتمیک نیست. صفر بودن شمارنده‌ها مجوز حذف یا جایگزینی نیست. Stageهای ذخیره‌شده و انتخاب نسخهٔ LAB بررسی می‌شوند. نقشهٔ وابستگی فرمان‌های قدیمی ممکن است نامعلوم باشد. بکاپ‌ها و وضعیت زندهٔ فرایند و پورت در این بررسی نیستند.",at:"زمان مشاهده",key:"کلید رمزگشایی در دسترس نیست",ledger:"دفتر حسابداری غیرفعال است",journal:"دفتر جایگزینی غیرفعال است",limit:"تعداد پیش‌نویس‌ها از سقف بررسی بیشتر است"},
};
function validate(raw:Snapshot,node:string) {
  if (!raw || raw.node_id!==node || !/^[a-f0-9]{32}$/.test(raw.installation_id) || !Number.isFinite(Date.parse(raw.observed_at)) || raw.coverage!=="native_sqlite_and_retained_journals" || raw.atomic_across_stores!==false || raw.can_retire!==false || raw.can_replace!==false || raw.lifecycle_blocker!=="atomic_node_lifecycle_not_implemented" || !raw.counts || !raw.unknown_reasons) throw new Error("invalid_dependencies");
  for (const key of Object.keys(labels)) {
    const n=raw.counts[key];
    if(n===null ? typeof raw.unknown_reasons[key]!=="string" || !raw.unknown_reasons[key] : !Number.isSafeInteger(n) || n<0) throw new Error("invalid_count");
  }
  return raw;
}
export default function NodeDependencies({nodeID,language}:{nodeID:string;language:Language}) {
  const t=words[language],index=language==="fa"?1:0;
  const [open,setOpen]=useState(false),[busy,setBusy]=useState(false),[failed,setFailed]=useState(false);
  const [snapshot,setSnapshot]=useState<Snapshot|null>(null);
  const active=useRef<AbortController|null>(null);
  useEffect(()=>()=>{active.current?.abort();active.current=null;},[]);
  async function read(){
    active.current?.abort();const controller=new AbortController();active.current=controller;
    setBusy(true);setFailed(false);setSnapshot(null);
    try{const next=validate(await api<Snapshot>("/api/v1/workspace/nodes/"+encodeURIComponent(nodeID)+"/dependencies",{signal:controller.signal}),nodeID);if(active.current===controller)setSnapshot(next);}
    catch{if(active.current===controller)setFailed(true);}
    finally{if(active.current===controller){active.current=null;setBusy(false);}}
  }
  const extraReasons:Record<string,readonly [string,string]>={config_stage_store_disabled:["Configuration Stage store disabled","مخزن Stage تنظیمات غیرفعال است"],lab_revision_store_disabled:["LAB version journal disabled","دفتر نسخه‌های LAB غیرفعال است"],command_history_limit:["Command history inspection limit reached","سابقهٔ فرمان‌ها از سقف بررسی بیشتر است"],command_history_unverifiable:["Command history could not be authenticated","اصالت سابقهٔ فرمان‌ها قابل تأیید نیست"],historical_node_map_missing:["Legacy commands lack saved node maps","فرمان‌های قدیمی نقشهٔ نودهای وابسته ندارند"]};
  const reason=(key:string)=>extraReasons[key]?.[index]??({inbound_key_unavailable:t.key,usage_ledger_disabled:t.ledger,replacement_journal_disabled:t.journal,draft_scan_limit:t.limit}[key]??t.unknown);
  if(!open)return <button type="button" onClick={()=>{setOpen(true);void read();}}>{t.open}</button>;
  return <section className="pro-node-dependencies" aria-label={t.title}>
    <h4>{t.title}</h4><p>{t.boundary}</p>
    <div className="pro-node-dependency-actions"><button type="button" disabled={busy} onClick={()=>void read()}>{t.refresh}</button><button type="button" onClick={()=>{active.current?.abort();active.current=null;setOpen(false);setBusy(false);setSnapshot(null);setFailed(false);}}>{t.close}</button></div>
    {busy&&<p role="status">{t.loading}</p>}{failed&&<p role="alert">{t.failed}</p>}
    {snapshot&&<><p>{t.at}: <time dateTime={snapshot.observed_at}>{new Date(snapshot.observed_at).toLocaleString(language==="fa"?"fa-IR":"en-GB")}</time></p>
      <dl>{Object.entries(labels).map(([key,label])=><div key={key} data-dependency={key}><dt>{label[index]}</dt><dd>{snapshot.counts[key]===null?<><strong>{t.unknown}</strong><small>{reason(snapshot.unknown_reasons[key])}</small></>:snapshot.counts[key]!.toLocaleString(language==="fa"?"fa-IR":"en-GB")}</dd></div>)}</dl>
    </>}
  </section>;
}
