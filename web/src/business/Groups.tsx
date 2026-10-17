import type { BusinessModel } from "./useBusinessWorkspace";
import { Users } from "lucide-react";
import { uid } from "./shared";
import GroupMemberManager from "./GroupMemberManager";
export default function Groups({ model }: { model: BusinessModel }) {
  const { t, session, actionBusy, reps, groups, groupOffset, setGroupOffset, groupID, setGroupID, groupName, setGroupName, groupColor, setGroupColor, groupRep, setGroupRep, groupTarget, setGroupTarget, memberIDs, setMemberIDs, groupEditID, setGroupEditID, groupEditName, setGroupEditName, groupEditColor, setGroupEditColor, unassignID, setUnassignID, addGroup, assignGroup, editGroup, deleteGroup, unassignMember } = model;
  return <>
      <div className="business-panel">
        <h3><Users size={17}/>{t.groups}</h3>
        <p className="business-warning">{t.groupWarning}</p>
        {groups && groups.length ? <div className="business-records">
          {groups.map((g) => <div className="business-group-row" key={g.id}>
            <div className="business-group-name">
              <span className="business-group-color" style={{ backgroundColor: g.color || "var(--accent)" }} />
              <strong>{g.name} ({g.id})</strong>
              <span>{g.representative_id} · {g.member_count} {t.groupCount}</span>
            </div>
            <div className="business-group-actions">
              <button type="button" className="group-members-select-button"
                aria-pressed={groupTarget === g.id} onClick={() => setGroupTarget(g.id)}>
                {model.language === "fa" ? "مدیریت اعضا" : "Manage members"}
              </button>
              <button type="button" onClick={() => {
                setGroupEditID(g.id); setGroupEditName(g.name); setGroupEditColor(g.color); setGroupTarget(g.id);
              }}>{t.groupEdit}</button>
              <button type="button" disabled={actionBusy} onClick={() => { void deleteGroup(g.id,g.name); }}>{t.groupDelete}</button>
            </div>
          </div>)}
        </div> : <p>{groups ? t.empty : t.loading}</p>}
        <div className="business-pagination">
          <button disabled={groupOffset === 0} onClick={() => setGroupOffset((x) => Math.max(0, x - 50))}>{t.previous}</button>
          <button disabled={!groups || groups.length < 50} onClick={() => setGroupOffset((x) => x + 50)}>{t.next}</button>
        </div>
      </div>
      {groups?.filter(g => g.id === groupTarget).map(g =>
        <GroupMemberManager key={g.id} group={g} language={model.language}
          session={session} refresh={model.reload}/>)}
      <div className="business-form-grid">
        <form className="business-panel" onSubmit={(e) => { void addGroup(e); }}>
          <h3><Users size={17}/>{t.newGroup}</h3>
          <label>{t.groupId}<div className="business-inline"><input value={groupID} onChange={(e) => setGroupID(e.target.value)} required/>
            <button type="button" onClick={() => setGroupID(uid())}>Random</button></div></label>
          <label>{t.groupName}<input value={groupName} onChange={(e) => setGroupName(e.target.value)} required/></label>
          <label>{t.groupColor}<input value={groupColor} onChange={(e) => setGroupColor(e.target.value)} placeholder="#123ABC" pattern="(#[A-Fa-f0-9]{6})?"/></label>
          {session.user.role === "owner" && <label>{t.repId}<select value={groupRep} onChange={(e) => setGroupRep(e.target.value)}>
            <option value="owner">owner</option>
            {reps?.filter((r) => r.kind === "representative").map((r) => <option key={r.id} value={r.id}>{r.display_name}</option>)}
          </select></label>}
          <button type="submit" disabled={actionBusy}>{t.newGroup}</button>
        </form>
        <form className="business-panel" onSubmit={(e) => { void assignGroup(e); }}>
          <h3>{t.groupAssign}</h3>
          <label>{t.groupId}<input value={groupTarget} onChange={(e) => setGroupTarget(e.target.value)} required/></label>
          <label>{t.clientIDs}<textarea value={memberIDs} onChange={(e) => setMemberIDs(e.target.value)} required rows={3}/></label>
          <button type="submit" disabled={actionBusy}>{t.groupAssign}</button>
        </form>
        <form className="business-panel" onSubmit={(e) => { void editGroup(e); }}>
          <h3>{t.groupEdit}</h3>
          <label>{t.groupId}<input value={groupEditID} onChange={(e) => setGroupEditID(e.target.value)} required/></label>
          <label>{t.groupName}<input value={groupEditName} onChange={(e) => setGroupEditName(e.target.value)} required/></label>
          <label>{t.groupColor}<input value={groupEditColor} onChange={(e) => setGroupEditColor(e.target.value)} pattern="(#[A-Fa-f0-9]{6})?"/></label>
          <button type="submit" disabled={actionBusy}>{t.groupSave}</button>
        </form>
        <form className="business-panel" onSubmit={(e) => { void unassignMember(e); }}>
          <h3>{t.groupRemove}</h3>
          <label>{t.groupId}<input value={groupTarget} onChange={(e) => setGroupTarget(e.target.value)} required/></label>
          <label>{t.groupSingleClient}<input value={unassignID} onChange={(e) => setUnassignID(e.target.value)} required/></label>
          <button type="submit" disabled={actionBusy}>{t.groupRemove}</button>
        </form>
      </div>
  </>;
}
