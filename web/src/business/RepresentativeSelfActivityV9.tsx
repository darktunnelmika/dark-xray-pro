import { useEffect, useState } from "react";
import { Activity } from "lucide-react";
import { api } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import "./RepresentativeControlsV9.css";

type Summary={
 representative_id:string;saved_clients:number;archived_clients:number;
 recorded_client_creations:number;recorded_client_renewals:number;
 recorded_profile_edits:number;recorded_credit_adjustments:number;
 available_volume_bytes:string;available_unlimited_units:string;
 metric_scope:string;
};
function valid(s:Summary,id:string):boolean{
 if(!s||s.representative_id!==id||
    s.metric_scope!=="saved_records_and_immutable_events_not_live_xray_traffic"||
    !/^\d+$/.test(s.available_volume_bytes)||!/^\d+$/.test(s.available_unlimited_units))return false;
 return ["saved_clients","archived_clients","recorded_client_creations",
  "recorded_client_renewals","recorded_profile_edits","recorded_credit_adjustments"]
  .every(k=>Number.isSafeInteger((s as unknown as Record<string,number>)[k])&&
    (s as unknown as Record<string,number>)[k]>=0);
}
function formatVolume(value:string):string{
 try{
  const n=BigInt(value),gb=1024n**3n;
  return (n/gb).toLocaleString("en-US")+"."+
   ((n%gb)*100n/gb).toString().padStart(2,"0")+" GB";
 }catch{return "—";}
}
export default function RepresentativeSelfActivityV9({model}:{model:BusinessModel}){
 const [result,setResult]=useState<Summary|null>(null);
 const [loading,setLoading]=useState(true);
 const repID=model.session.user.representative_id;
 useEffect(()=>{
  const ctrl=new AbortController();
  setLoading(true);setResult(null);
  void api<Summary>("/api/v1/workspace/representatives/"+
   encodeURIComponent(repID)+"/activity",{signal:ctrl.signal}).then(data=>{
    if(ctrl.signal.aborted)return;
    if(!valid(data,repID))throw Error("unverified_rep_activity");
    setResult(data);
  }).catch(()=>{if(!ctrl.signal.aborted)setResult(null)})
   .finally(()=>{if(!ctrl.signal.aborted)setLoading(false)});
  return ()=>ctrl.abort();
 },[repID,model.refreshKey]);
 const labels=model.language==="fa"?{
  title:"گزارش واقعی حساب نماینده",notice:"فقط رکوردهای ذخیره‌شده، نه ترافیک آنلاین یا میزان مصرف حدسی",
  clients:"کلاینت‌ها",archived:"آرشیوشده",created:"فروش ثبت‌شده",
  renewed:"تمدیدها",edits:"ویرایش کلاینت",credit:"تغییر اعتبار",
  balance:"اعتبار موجود",unknown:"گزارش معتبر در دسترس نیست.",
 }:{
  title:"My verified representative activity",notice:"Saved ledger records only, never fabricated online/Xray traffic",
  clients:"Clients",archived:"Archived",created:"Recorded sales",
  renewed:"Renewals",edits:"Client edits",credit:"Credit changes",
  balance:"Available credit",unknown:"Verified activity report unavailable.",
 };
 return <section className="business-panel rep-admin-stat-panel" aria-label="Representative self activity">
  <header><h4><Activity size={16}/>{labels.title}</h4></header>
  <p>{labels.notice}</p>
  {loading?<p role="status">{model.t.loading}</p>:
    !result?<p role="status">{labels.unknown}</p>:<>
    <div className="rep-admin-metrics">
      {([
       [labels.clients,result.saved_clients],[labels.archived,result.archived_clients],
       [labels.created,result.recorded_client_creations],[labels.renewed,result.recorded_client_renewals],
       [labels.edits,result.recorded_profile_edits],[labels.credit,result.recorded_credit_adjustments],
      ] as [string,number][]).map(([title,n])=><div key={title}>
       <span>{title}</span><strong>{n.toLocaleString("en-US")}</strong>
      </div>)}
    </div>
    <p>{labels.balance}: {formatVolume(result.available_volume_bytes)} · {
      Number(result.available_unlimited_units).toLocaleString("en-US")}</p>
  </>}
 </section>;
}
