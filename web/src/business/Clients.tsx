import ClientServices from "./ClientServices";
import RepresentativeClientServices from "./RepresentativeClientServices";
import ClientDialog from "./ClientDialog";
import {useEffect,useRef,useState} from "react";
import {createStorageKey,readCreateAttempt} from "./clientCreateDelivery";
import "./Clients.css";
import {ClientContactEditor} from "./ClientContact";
import type { BusinessModel } from "./useBusinessWorkspace";
import ClientCreateFlow from "./ClientCreateFlow";
import { gb } from "./shared";
import ClientListExplorer from "./ClientListExplorer";
import ClientUsage from "../ClientUsage";
import ClientProfileEditor from "./ClientProfileEditor";
import ClientInboundSubscription from "./ClientInboundSubscription";
import { businessGranted } from "./permissionsV9";
import { ClientVolumeRefund } from "./VolumeRefunds";
export default function Clients({ model }: { model: BusinessModel }) {
  const { t, session, language, actionBusy, refreshKey, clients, clientOps, clientOpsOffset, setClientOpsOffset, lifeClientID, setLifeClientID, lifeDays, setLifeDays, lifeVolumeGB, setLifeVolumeGB, lifeReason, setLifeReason, lifeAction, setLifeAction, lifeStateReason, setLifeStateReason, extendExistingClient, changeExistingClientState } = model;
  const canCreate=businessGranted(session.user,"create_clients");
  const canRenew=businessGranted(session.user,"renew_clients");
  const canManage=businessGranted(session.user,"manage_clients");
  const canEdit=businessGranted(session.user,"edit_clients");
  const canViewFinance=businessGranted(session.user,"view_finance");
  const [creating,setCreating]=useState(()=>{try{return !!readCreateAttempt(sessionStorage.getItem(createStorageKey(session.user.id)),session.user.id);}catch{return false;}});
  const fa=language==="fa";
  const nextPanel=useRef<string|null>(null);
  const [panel,setPanel]=useState("overview");
  const [visited,setVisited]=useState<{clientID:string;panels:string[]}>({clientID:"",panels:[]});
  useEffect(()=>{setVisited(current=>({clientID:lifeClientID,panels:current.clientID===lifeClientID?Array.from(new Set([...current.panels,panel])):[panel]}));},[lifeClientID,panel]);
  const loaded=(target:string)=>panel===target||(visited.clientID===lifeClientID&&visited.panels.includes(target));

  useEffect(()=>{setPanel(nextPanel.current??"overview");nextPanel.current=null;if(lifeClientID)setCreating(false);},[lifeClientID]);
  const tabs=[...((session.user.role==="owner"||canManage)?[["services",fa?"سرویس‌ها":"Services"]]:[]),['overview',fa?'وضعیت و مصرف':'Overview'],['delivery',fa?'کانفیگ و QR':'Configuration & QR'],['edit',fa?'مشخصات و تلگرام':'Details & Telegram'],...(canRenew?[['renew',fa?'تمدید':'Renew']]:[]),...(canManage?[['state',fa?'وضعیت حساب':'Account state']]:[])];
  return <>
      {canCreate && <ClientDialog open={creating} onClose={()=>setCreating(false)} title={fa?"ایجاد کلاینت":"New client"} closeLabel={fa?"بازگشت به فهرست":"Back to client list"}><ClientCreateFlow model={model} onInspect={(id,target)=>{const destination=target==="services"&&(session.user.role==="owner"||(session.user.role==="representative"&&canManage))?"services":"delivery";nextPanel.current=destination;setPanel(destination);setLifeClientID(id);setCreating(false);}}/></ClientDialog>}
      <ClientListExplorer model={model} onInspect={(id,target)=>{nextPanel.current=target;setPanel(target);setLifeClientID(id);}} onCreate={canCreate&&!creating?()=>setCreating(true):undefined}/>
      <ClientDialog onRefresh={model.reload} refreshLabel={fa?"تازه‌سازی اطلاعات":"Refresh client data"} open={!!lifeClientID} onClose={()=>setLifeClientID("")} title={clients?.find(c=>c.id===lifeClientID)?.name || lifeClientID || (fa?"کلاینت":"Client")} closeLabel={fa?"بستن جزئیات":"Close details"}>
      <nav className="client-detail-tabs" aria-label={fa?"بخش‌های کلاینت":"Client sections"}>{tabs.map(([id,label])=><button type="button" key={id} aria-pressed={panel===id} onClick={()=>setPanel(id)}>{label}</button>)}</nav>
      {lifeClientID&&panel==="services"&&session.user.role==="owner"&&<ClientServices key={session.user.id+":"+lifeClientID} clientID={lifeClientID} session={session} language={language} onChanged={model.reload}/>}
      {lifeClientID&&panel==="services"&&session.user.role==="representative"&&canManage&&<RepresentativeClientServices key={session.user.id+":"+lifeClientID} clientID={lifeClientID} session={session} language={language}/>}
      <div hidden={panel!=="overview"}>
      {lifeClientID && loaded("overview") && <ClientUsage key={session.user.id + ":" + lifeClientID} clientID={lifeClientID} language={language} refresh={refreshKey} />}
      </div>
      <div hidden={panel!=="renew"}>
      {canViewFinance && lifeClientID && loaded("renew") && <ClientVolumeRefund key={session.user.id + ":" + lifeClientID} clientID={lifeClientID} model={model}/>}
      </div>
      <div hidden={panel!=="delivery"}>
      {lifeClientID && loaded("delivery") && <ClientInboundSubscription key={session.user.id + ":" + lifeClientID} clientID={lifeClientID} language={language} session={session} canManage={canManage} refresh={refreshKey}/>}
      </div>
      <div hidden={panel!=="edit"}>
      {lifeClientID && loaded("edit") && <ClientContactEditor key={session.user.id+":"+lifeClientID} model={model} clientID={lifeClientID} canEdit={canEdit}/>}
      {canEdit && lifeClientID && loaded("edit") && <ClientProfileEditor key={session.user.id + ":" + lifeClientID} clientID={lifeClientID} model={model}/>}
      </div>
      {lifeClientID && <div>
        {canRenew && <form hidden={panel!=="renew"} className="business-panel" onSubmit={(e) => { void extendExistingClient(e); }}>
          <h3>{t.renew}</h3>
          <label>{t.extraDays}<input type="number" step="1" min="0" max="3650"
            value={lifeDays} onChange={(e) => setLifeDays(e.target.value)} required/></label>
          <label>{t.extraVolume}<input type="number" step="any" min="0"
            value={lifeVolumeGB} onChange={(e) => setLifeVolumeGB(e.target.value)} required/></label>
          <label>{t.newClientReason}<input value={lifeReason} onChange={(e) => setLifeReason(e.target.value)} required/></label>
          <button type="submit" disabled={actionBusy || !lifeClientID}>{t.renew}</button>
        </form>}
        {canManage && <form hidden={panel!=="state"} className="business-panel" onSubmit={(e) => { void changeExistingClientState(e); }}>
          <h3>{t.stateAction}</h3>
          <label>{t.stateAction}<select value={lifeAction} onChange={(e) => setLifeAction(e.target.value as "disable" | "enable" | "archive")}>
            <option value="disable">{t.disable}</option><option value="enable">{t.enable}</option><option value="archive">{t.archive}</option>
          </select></label>
          <label>{t.newClientReason}<input value={lifeStateReason} onChange={(e) => setLifeStateReason(e.target.value)} required/></label>
          <button type="submit" disabled={actionBusy || !lifeClientID}>{t.applyState}</button>
        </form>}
      </div>}
      {lifeClientID&&model.notice&&<p className="business-notice" role="status">{model.notice}</p>}
      </ClientDialog>
      {canViewFinance && <details className="business-panel clients-history">
        <summary>{t.clientOps}</summary>
        {clientOps && clientOps.length ? <div className="business-records">
          {clientOps.map((op) => <div key={op.event_id}>
            <strong>{op.kind} · {op.client_id}</strong>
            <span>{op.reason} · {op.added_days}d · +{gb(op.added_volume_bytes)} · -{op.unlimited_credit_debit} {t.unlimited}</span>
          </div>)}
        </div> : <p>{clientOps ? t.empty : t.loading}</p>}
        <div className="business-pagination">
          <button disabled={clientOpsOffset === 0} onClick={() => setClientOpsOffset((x) => Math.max(0, x - 50))}>{t.previous}</button>
          <button disabled={!clientOps || clientOps.length < 50} onClick={() => setClientOpsOffset((x) => x + 50)}>{t.next}</button>
        </div>
      </details>}
  </>;
}
