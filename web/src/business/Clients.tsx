import type { BusinessModel } from "./useBusinessWorkspace";
import { Users } from "lucide-react";
import { uid, gb } from "./shared";
import ClientPlanWorkbench from "./ClientPlanWorkbench";
import ClientListExplorer from "./ClientListExplorer";
import ClientUsage from "../ClientUsage";
export default function Clients({ model }: { model: BusinessModel }) {
  const { t, session, language, actionBusy, refreshKey, reps, clients, clientOps, clientOpsOffset, setClientOpsOffset, clientId, setClientId, clientName, setClientName, clientRep, setClientRep, clientKind, setClientKind, clientDuration, setClientDuration, clientIPs, setClientIPs, clientVolume, setClientVolume, lifeClientID, setLifeClientID, lifeDays, setLifeDays, lifeVolumeGB, setLifeVolumeGB, lifeReason, setLifeReason, lifeAction, setLifeAction, lifeStateReason, setLifeStateReason, createClient, extendExistingClient, changeExistingClientState } = model;
  return <>
      <form className="business-panel business-client-form" onSubmit={(e) => { void createClient(e); }}>
        <h3><Users size={17}/>{t.createClient}</h3>
        <div className="business-fields">
          <label>{t.clientId}<div className="business-inline"><input value={clientId} onChange={(e) => setClientId(e.target.value)} required/>
            <button type="button" onClick={() => setClientId(uid())}>Random</button></div></label>
          <label>{t.clientName}<input value={clientName} onChange={(e) => setClientName(e.target.value)} required/></label>
          {session.user.role === "owner" && <label>{t.repId}<select value={clientRep} onChange={(e) => setClientRep(e.target.value)}>
            <option value="owner">owner</option>
            {reps?.filter((r) => r.kind === "representative").map((r) => <option key={r.id} value={r.id}>{r.display_name}</option>)}
          </select></label>}
          <label>{t.plan}<select value={clientKind} onChange={(e) => setClientKind(e.target.value as "volume" | "unlimited")}>
            <option value="volume">{t.volumePlan}</option><option value="unlimited">{t.unlimitedPlan}</option></select></label>
          <label>{t.duration}<input type="number" min={1} max={3650} step={1} value={clientDuration} onChange={(e) => setClientDuration(e.target.value)} required/></label>
          <label>{t.slots}<input type="number" min={clientKind === "unlimited" ? 1 : 0} max={1000} step={1} value={clientIPs} onChange={(e) => setClientIPs(e.target.value)} required/></label>
          {clientKind === "volume" && <label>{t.volumeGB}<input type="number" step="any" min="0.01" value={clientVolume} onChange={(e) => setClientVolume(e.target.value)} required/></label>}
        </div>
        <ClientPlanWorkbench model={model}/>
        <p className="business-warning">{t.pending}</p>
        <button type="submit" disabled={actionBusy}>{t.saveClient}</button>
      </form>
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
      <div className="business-form-grid">
        <form className="business-panel" onSubmit={(e) => { void extendExistingClient(e); }}>
          <h3>{t.renew}</h3>
          <label>{t.extraDays}<input type="number" step="1" min="0" max="3650"
            value={lifeDays} onChange={(e) => setLifeDays(e.target.value)} required/></label>
          <label>{t.extraVolume}<input type="number" step="any" min="0"
            value={lifeVolumeGB} onChange={(e) => setLifeVolumeGB(e.target.value)} required/></label>
          <label>{t.newClientReason}<input value={lifeReason} onChange={(e) => setLifeReason(e.target.value)} required/></label>
          <button type="submit" disabled={actionBusy || !lifeClientID}>{t.renew}</button>
        </form>
        <form className="business-panel" onSubmit={(e) => { void changeExistingClientState(e); }}>
          <h3>{t.stateAction}</h3>
          <label>{t.stateAction}<select value={lifeAction} onChange={(e) => setLifeAction(e.target.value as "disable" | "enable" | "archive")}>
            <option value="disable">{t.disable}</option><option value="enable">{t.enable}</option><option value="archive">{t.archive}</option>
          </select></label>
          <label>{t.newClientReason}<input value={lifeStateReason} onChange={(e) => setLifeStateReason(e.target.value)} required/></label>
          <button type="submit" disabled={actionBusy || !lifeClientID}>{t.applyState}</button>
        </form>
      </div>
      <div className="business-panel">
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
      </div>
  </>;
}
