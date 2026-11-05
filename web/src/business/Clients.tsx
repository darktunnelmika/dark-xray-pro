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
  return <>
      {canCreate && <ClientCreateFlow model={model}/>}
      <ClientListExplorer model={model}/>
      <div className="business-panel">
        <h3>{t.clientLifecycle}</h3>
        <label>{t.chooseClient}
          <select value={lifeClientID} onChange={(e) => setLifeClientID(e.target.value)}>
            <option value="">{t.chooseClient}</option>
            {clients?.map((c) => <option key={c.id} value={c.id} disabled={c.archived}>{c.name} ({c.id})</option>)}
          </select>
        </label>
        <p className="business-warning">{t.safeLifecycle}</p>
      </div>
      {lifeClientID && <ClientUsage key={session.user.id + ":" + lifeClientID} clientID={lifeClientID} language={language} refresh={refreshKey} />}
      {canViewFinance && lifeClientID && <ClientVolumeRefund key={session.user.id + ":" + lifeClientID} clientID={lifeClientID} model={model}/>}
      {lifeClientID && <ClientInboundSubscription key={session.user.id + ":" + lifeClientID} clientID={lifeClientID} language={language} session={session} canManage={canManage} refresh={refreshKey}/>}
      {canEdit && lifeClientID && <ClientProfileEditor key={session.user.id + ":" + lifeClientID} clientID={lifeClientID} model={model}/>}
      <div className="business-form-grid">
        {canRenew && <form className="business-panel" onSubmit={(e) => { void extendExistingClient(e); }}>
          <h3>{t.renew}</h3>
          <label>{t.extraDays}<input type="number" step="1" min="0" max="3650"
            value={lifeDays} onChange={(e) => setLifeDays(e.target.value)} required/></label>
          <label>{t.extraVolume}<input type="number" step="any" min="0"
            value={lifeVolumeGB} onChange={(e) => setLifeVolumeGB(e.target.value)} required/></label>
          <label>{t.newClientReason}<input value={lifeReason} onChange={(e) => setLifeReason(e.target.value)} required/></label>
          <button type="submit" disabled={actionBusy || !lifeClientID}>{t.renew}</button>
        </form>}
        {canManage && <form className="business-panel" onSubmit={(e) => { void changeExistingClientState(e); }}>
          <h3>{t.stateAction}</h3>
          <label>{t.stateAction}<select value={lifeAction} onChange={(e) => setLifeAction(e.target.value as "disable" | "enable" | "archive")}>
            <option value="disable">{t.disable}</option><option value="enable">{t.enable}</option><option value="archive">{t.archive}</option>
          </select></label>
          <label>{t.newClientReason}<input value={lifeStateReason} onChange={(e) => setLifeStateReason(e.target.value)} required/></label>
          <button type="submit" disabled={actionBusy || !lifeClientID}>{t.applyState}</button>
        </form>}
      </div>
      {canViewFinance && <div className="business-panel">
        <h3>{t.clientOps}</h3>
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
      </div>}
  </>;
}
