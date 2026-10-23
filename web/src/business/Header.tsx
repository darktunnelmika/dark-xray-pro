import type { BusinessModel } from "./useBusinessWorkspace";
import { RefreshCw, ShieldCheck } from "lucide-react";
import { gb } from "./shared";
export default function Header({ model }: { model: BusinessModel }) {
  const { t, session, reload, me } = model;
  return <>
      <header className="business-topbar">
        <div><span className="business-eyebrow"><ShieldCheck size={15}/>{t.account}</span>
          <h2>{session.user.username}</h2><p>{session.user.role === "owner" ? t.ownerScope : t.accountScope}</p>
        </div>
        <div className="business-topbar-actions">
          <button type="button" onClick={reload}><RefreshCw size={15}/>{t.refresh}</button>
        </div>
      </header>
      {me && <div className="business-metrics">
        <div><span>{t.volume}</span><strong><bdi dir="ltr">{gb(me.balance.volume_credit_bytes)}</bdi></strong></div>
        <div><span>{t.unlimited}</span><strong><bdi dir="ltr">{me.balance.unlimited_credit_units.toLocaleString()}</bdi></strong></div>
        <div><span>{t.role}</span><strong>{session.user.role}</strong></div>
      </div>}
  </>;
}
