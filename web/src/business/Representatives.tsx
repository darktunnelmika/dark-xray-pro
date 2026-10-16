import type { BusinessModel } from "./useBusinessWorkspace";
import { CreditCard, KeyRound, Users } from "lucide-react";
import { gb } from "./shared";
export default function Representatives({ model }: { model: BusinessModel }) {
  const { t, session, actionBusy, reps, repOffset, setRepOffset, repId, setRepId, repName, setRepName, loginRep, setLoginRep, repUser, setRepUser, repPass, setRepPass, creditRep, setCreditRep, creditVolume, setCreditVolume, creditUnits, setCreditUnits, creditReason, setCreditReason, createRepresentative, createRepLogin, adjustCredit } = model;
  return <>
      {session.user.role === "owner" && <>
        <div className="business-panel">
          <h3><Users size={17}/>{t.reps}</h3>
          {reps && reps.length ? <div className="business-records">{reps.map((r) => (
            <div key={r.id}><strong>{r.display_name} ({r.id})</strong><span>{gb(r.volume_credit_bytes)} · {r.unlimited_credit_units.toLocaleString()} {t.unlimited}</span></div>
          ))}</div> : <p>{reps ? t.empty : t.loading}</p>}
          <div className="business-pagination">
            <button disabled={repOffset === 0} onClick={() => setRepOffset((x) => Math.max(0, x - 50))}>{t.previous}</button>
            <button disabled={!reps || reps.length < 50} onClick={() => setRepOffset((x) => x + 50)}>{t.next}</button>
          </div>
        </div>
        <div className="business-form-grid">
          <form className="business-panel" onSubmit={(e) => { void createRepresentative(e); }}>
            <h3><Users size={17}/>{t.createRep}</h3>
            <label>{t.repId}<input value={repId} onChange={(e) => setRepId(e.target.value)} required/></label>
            <label>{t.displayName}<input value={repName} onChange={(e) => setRepName(e.target.value)} required/></label>
            <button type="submit" disabled={actionBusy}>{t.createRep}</button>
          </form>
          <form className="business-panel" onSubmit={(e) => { void createRepLogin(e); }}>
            <h3><KeyRound size={17}/>{t.provision}</h3>
            <label>{t.repId}<input value={loginRep} onChange={(e) => setLoginRep(e.target.value)} required/></label>
            <label>{t.repUsername}<input value={repUser} onChange={(e) => setRepUser(e.target.value)} required/></label>
            <label>{t.repPassword}<input type="password" autoComplete="new-password" value={repPass} onChange={(e) => setRepPass(e.target.value)} required minLength={12}/></label>
            <button type="submit" disabled={actionBusy}>{t.setLogin}</button>
          </form>
          <form className="business-panel" onSubmit={(e) => { void adjustCredit(e); }}>
            <h3><CreditCard size={17}/>{t.grant}</h3>
            <label>{t.repId}<input value={creditRep} onChange={(e) => setCreditRep(e.target.value)} required/></label>
            <label>{t.addVolume}<input type="number" step="any" value={creditVolume} onChange={(e) => setCreditVolume(e.target.value)} required/></label>
            <label>{t.addUnits}<input type="number" step="1" value={creditUnits} onChange={(e) => setCreditUnits(e.target.value)} required/></label>
            <label>{t.reason}<input value={creditReason} onChange={(e) => setCreditReason(e.target.value)} required/></label>
            <button type="submit" disabled={actionBusy}>{t.applyCredit}</button>
          </form>
        </div>
      </>}
  </>;
}
