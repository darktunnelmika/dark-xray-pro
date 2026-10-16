import type { BusinessModel } from "./useBusinessWorkspace";
import { KeyRound } from "lucide-react";
export default function Account({ model }: { model: BusinessModel }) {
  const { t, oldPassword, setOldPassword, newPassword, setNewPassword, actionBusy, changePassword } = model;
  return <>
      <form className="business-panel business-password-form" onSubmit={(e) => { void changePassword(e); }}>
        <h3><KeyRound size={17}/>{t.rotate}</h3>
        <div className="business-fields">
          <label>{t.oldPassword}<input type="password" autoComplete="current-password" value={oldPassword}
            onChange={(e) => setOldPassword(e.target.value)} required/></label>
          <label>{t.newPassword}<input type="password" autoComplete="new-password" minLength={12}
            value={newPassword} onChange={(e) => setNewPassword(e.target.value)} required/></label>
        </div>
        <button type="submit" disabled={actionBusy}>{t.change}</button>
      </form>
  </>;
}
