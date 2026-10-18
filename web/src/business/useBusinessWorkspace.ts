import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { api, mutation, ApiError, useSession, type Language } from "../session";
import { useAccountPageOffset } from "./useAccountPageOffset";
import { words, uid, bytesFromGB, type Section, type Workspace, type Representative, type Client, type ClientOperation, type ClientGroup, type LedgerEvent } from "./shared";
export function useBusinessWorkspace(language: Language, section: Section) {
  const t = words[language];
  const { session, expire } = useSession();
  if (!session) throw new Error("Authenticated workspace required");
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  // Reuse an operation ID ONLY for an identical canonical payload after
  // an ambiguous network result. Editing the target, amount, name or plan
  // must never reuse an earlier financial/identity event ID.
  const pendingCreditKey = useRef<{ key: string; id: string } | null>(null);
  const pendingClientKey = useRef<{ key: string; id: string } | null>(null);
  const [actionBusy, setActionBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [me, setMe] = useState<Workspace | null>(null);
  const [reps, setReps] = useState<Representative[] | null>(null);
  const [clients, setClients] = useState<Client[] | null>(null);
  const [events, setEvents] = useState<LedgerEvent[] | null>(null);
  const [groups, setGroups] = useState<ClientGroup[] | null>(null);
  const [clientOps, setClientOps] = useState<ClientOperation[] | null>(null);
  const [repOffset, setRepOffset] = useAccountPageOffset(session.user.id, "representatives");
  const [clientOffset, setClientOffset] = useAccountPageOffset(session.user.id, "clients");
  const [eventOffset, setEventOffset] = useAccountPageOffset(session.user.id, "finance");
  const [groupOffset, setGroupOffset] = useAccountPageOffset(session.user.id, "groups");
  const [clientOpsOffset, setClientOpsOffset] = useAccountPageOffset(session.user.id, "client-operations");
  const [repId, setRepId] = useState("");
  const [repName, setRepName] = useState("");
  const [loginRep, setLoginRep] = useState("");
  const [repUser, setRepUser] = useState("");
  const [repPass, setRepPass] = useState("");
  const [creditRep, setCreditRep] = useState("");
  const [creditVolume, setCreditVolume] = useState("0");
  const [creditUnits, setCreditUnits] = useState("0");
  const [creditReason, setCreditReason] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientName, setClientName] = useState("");
  const [clientRep, setClientRep] = useState("owner");
  const [clientKind, setClientKind] = useState<"volume" | "unlimited">("volume");
  const [clientDuration, setClientDuration] = useState("30");
  const [clientIPs, setClientIPs] = useState("0");
  const [clientVolume, setClientVolume] = useState("10");
  const [groupID, setGroupID] = useState("");
  const [groupName, setGroupName] = useState("");
  const [groupColor, setGroupColor] = useState("");
  const [groupRep, setGroupRep] = useState("owner");
  const selectedGroupKey = "dark-pro-group-selection:" + session.user.id;
  const [groupTarget, setGroupTarget] = useState(() => {
    try {
      const id = sessionStorage.getItem(selectedGroupKey) || "";
      return /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(id) ? id : "";
    } catch { return ""; }
  });
  useEffect(() => {
    try {
      if (groupTarget) sessionStorage.setItem(selectedGroupKey, groupTarget);
      else sessionStorage.removeItem(selectedGroupKey);
    } catch { /* private browsing */ }
  }, [groupTarget, selectedGroupKey]);
  const [memberIDs, setMemberIDs] = useState("");
  const [groupEditID, setGroupEditID] = useState("");
  const [groupEditName, setGroupEditName] = useState("");
  const [groupEditColor, setGroupEditColor] = useState("");
  const [unassignID, setUnassignID] = useState("");
  const [lifeClientID, setLifeClientID] = useState("");
  const [lifeDays, setLifeDays] = useState("30");
  const [lifeVolumeGB, setLifeVolumeGB] = useState("0");
  const [lifeReason, setLifeReason] = useState("");
  const [lifeAction, setLifeAction] = useState<"disable" | "enable" | "archive">("disable");
  const [lifeStateReason, setLifeStateReason] = useState("");
  const pendingRenew = useRef<{ key: string; id: string } | null>(null);
  const pendingState = useRef<{ key: string; id: string } | null>(null);

  const reload = useCallback(() => setRefreshKey((n) => n + 1), []);
  useEffect(() => { setNotice(null); }, [section, session.user.id]);
  useEffect(() => {
    if (!session) return;
    const controller = new AbortController();
    const p = "?limit=50&offset=";
    const fetchData = async () => {
      const [account, ownClients, ownEvents, ownerReps, ownGroups, lifecycle] = await Promise.all([
        api<Workspace>("/api/v1/workspace/me", { signal: controller.signal }),
        section === "clients" ? api<{ clients: Client[] }>("/api/v1/workspace/clients" + p + clientOffset, { signal: controller.signal }) : Promise.resolve({ clients: [] as Client[] }),
        section === "finance" ? api<{ events: LedgerEvent[] }>("/api/v1/workspace/events" + p + eventOffset, { signal: controller.signal }) : Promise.resolve({ events: [] as LedgerEvent[] }),
        session.user.role === "owner" && ["clients", "groups", "representatives"].includes(section)
          ? api<{ representatives: Representative[] }>("/api/v1/workspace/representatives" + p + repOffset, { signal: controller.signal })
          : Promise.resolve(null),
        section === "groups" ? api<{ groups: ClientGroup[] }>("/api/v1/workspace/groups" + p + groupOffset, { signal: controller.signal }) : Promise.resolve({ groups: [] as ClientGroup[] }),
        section === "clients" ? api<{ operations: ClientOperation[] }>("/api/v1/workspace/client-operations" + p + clientOpsOffset, { signal: controller.signal }) : Promise.resolve({ operations: [] as ClientOperation[] }),
      ]);
      if (controller.signal.aborted) return;
      if (!Array.isArray(ownClients.clients) || !Array.isArray(ownEvents.events) ||
        (ownerReps !== null && !Array.isArray(ownerReps.representatives)) || !Array.isArray(ownGroups.groups) || !Array.isArray(lifecycle.operations)) {
        throw new Error("Malformed business response");
      }
      setMe(account); setClients(ownClients.clients); setEvents(ownEvents.events);
      // A successful authenticated data refresh must NOT erase the last
      // user-visible mutation receipt. The next explicit action or tab
      // change clears it, not the data read following a committed write.
      setReps(ownerReps?.representatives ?? null); setGroups(ownGroups.groups); setClientOps(lifecycle.operations);
    };
    void fetchData().catch((error: unknown) => {
      if (controller.signal.aborted) return;
      if (error instanceof ApiError && error.status === 401) {
        expire(); setMe(null); setReps(null); setClients(null); setEvents(null); setGroups(null); setClientOps(null);
        pendingCreditKey.current = null; pendingClientKey.current = null;
      } else {
        setNotice(words[language].unavailable);
      }
    });
    return () => controller.abort();
  }, [session, refreshKey, clientOffset, eventOffset, repOffset, groupOffset, clientOpsOffset, language, section, expire]);

  async function runAction(fn: () => Promise<void>, silent = false) {
    setActionBusy(true); setNotice(null);
    try { await fn(); if (!silent) setNotice(t.saved); reload(); }
    catch (error) {
      const details = error instanceof Error ? error.message : "request_failed";
      setNotice(t.failed + ": " + details);
    } finally { setActionBusy(false); }
  }
  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!session || newPassword.length < 12) { setNotice(t.missing); return; }
    await runAction(async () => {
      await mutation("/api/v1/session/change-password", {
        old_password: oldPassword, new_password: newPassword,
      }, session.csrf_token);
      setOldPassword(""); setNewPassword("");
      pendingCreditKey.current = null; pendingClientKey.current = null;
      expire();
      setMe(null); setReps(null); setClients(null); setEvents(null); setGroups(null);
    }, true);
  }
  async function createRepresentative(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!session) return;
    await runAction(async () => {
      await mutation("/api/v1/workspace/representatives", { id: repId, name: repName }, session.csrf_token);
      setRepId(""); setRepName(""); setRepOffset(0);
    });
  }
  async function createRepLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!session || !loginRep || !repUser || repPass.length < 12) { setNotice(t.missing); return; }
    await runAction(async () => {
      await mutation("/api/v1/workspace/representatives/" + encodeURIComponent(loginRep) + "/login",
        { username: repUser, password: repPass }, session.csrf_token);
      setRepPass(""); setRepUser("");
    });
  }
  async function adjustCredit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!session || !creditRep || !creditReason.trim()) { setNotice(t.missing); return; }
    const bytes = bytesFromGB(creditVolume, true);
    const units = Number(creditUnits);
    if (bytes === null || !Number.isSafeInteger(units) || Math.abs(units) > 1_000_000_000 || (bytes === 0 && units === 0)) {
      setNotice(t.missing); return;
    }
    const payload = { representative_id: creditRep, volume_delta_bytes: bytes,
      unlimited_delta_units: units, reason: creditReason.trim() };
    const key = JSON.stringify(payload);
    const eventId = pendingCreditKey.current?.key === key ? pendingCreditKey.current.id : uid();
    pendingCreditKey.current = { key, id: eventId };
    await runAction(async () => {
      await mutation("/api/v1/workspace/representatives/" + encodeURIComponent(payload.representative_id) + "/credits", {
        event_id: eventId, ...payload,
      }, session.csrf_token);
      setCreditReason(""); pendingCreditKey.current = null;
    });
  }
  async function createClient(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!session) return;
    const duration = Number(clientDuration);
    const ipCount = Number(clientIPs);
    const volumeBytes = clientKind === "volume" ? bytesFromGB(clientVolume) : 0;
    if (!clientId.trim() || !clientName.trim() || !Number.isSafeInteger(duration) || duration < 1 || duration > 3650 ||
      !Number.isSafeInteger(ipCount) || ipCount < 0 || ipCount > 1000 || (clientKind === "unlimited" && ipCount === 0) ||
      volumeBytes === null) { setNotice(t.missing); return; }
    const payload = { client_id: clientId.trim(),
      representative_id: session.user.role === "owner" ? clientRep : session.user.representative_id,
      name: clientName.trim(), duration_days: duration, ip_count: ipCount,
      volume_bytes: volumeBytes, unlimited: clientKind === "unlimited" };
    const key = JSON.stringify(payload);
    const eventId = pendingClientKey.current?.key === key ? pendingClientKey.current.id : uid();
    pendingClientKey.current = { key, id: eventId };
    await runAction(async () => {
      await mutation("/api/v1/workspace/clients", {
        event_id: eventId, ...payload,
      }, session.csrf_token);
      setClientId(""); setClientName(""); setClientOffset(0); pendingClientKey.current = null;
    });
  }

  async function extendExistingClient(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!session || !lifeClientID.trim() || !lifeReason.trim()) { setNotice(t.missing); return; }
    const client = clients?.find((c) => c.id === lifeClientID);
    if (!client || client.archived) { setNotice(t.missing); return; }
    const days = Number(lifeDays);
    const volumeBytes = bytesFromGB(lifeVolumeGB, true);
    if (!Number.isSafeInteger(days) || days < 0 || days > 3650 || volumeBytes === null || volumeBytes < 0 ||
      (days === 0 && volumeBytes === 0) || (client.plan_kind === "unlimited" && volumeBytes !== 0)) {
      setNotice(t.missing); return;
    }
    const payload = { client_id: client.id, additional_days: days,
      additional_volume_bytes: volumeBytes, reason: lifeReason.trim() };
    const key = JSON.stringify(payload);
    const eventID = pendingRenew.current?.key === key ? pendingRenew.current.id : uid();
    pendingRenew.current = { key, id: eventID };
    await runAction(async () => {
      await mutation("/api/v1/workspace/clients/" + encodeURIComponent(client.id) + "/extend",
        { event_id: eventID, ...payload }, session.csrf_token);
      pendingRenew.current = null; setLifeReason("");
    });
  }
  async function changeExistingClientState(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!session || !lifeClientID || !lifeStateReason.trim()) { setNotice(t.missing); return; }
    const client = clients?.find((c) => c.id === lifeClientID);
    if (!client || client.archived) { setNotice(t.missing); return; }
    if (lifeAction === "archive" && !window.confirm(t.archiveConfirm)) return;
    const payload = { client_id: client.id, action: lifeAction, reason: lifeStateReason.trim() };
    const key = JSON.stringify(payload);
    const eventID = pendingState.current?.key === key ? pendingState.current.id : uid();
    pendingState.current = { key, id: eventID };
    await runAction(async () => {
      await mutation("/api/v1/workspace/clients/" + encodeURIComponent(client.id) + "/state",
        { event_id: eventID, ...payload }, session.csrf_token);
      pendingState.current = null; setLifeStateReason("");
    });
  }

  async function addGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!session) return;
    await runAction(async () => {
      await mutation("/api/v1/workspace/groups", {
        id: groupID.trim(), name: groupName.trim(), color: groupColor.trim(),
        representative_id: session.user.role === "owner" ? groupRep : session.user.representative_id,
      }, session.csrf_token);
      setGroupID(""); setGroupName(""); setGroupColor(""); setGroupOffset(0);
    });
  }
  async function assignGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!session || !groupTarget.trim()) return;
    const ids = memberIDs.split(/[\s,]+/).map((v) => v.trim()).filter(Boolean);
    if (ids.length === 0 || ids.length > 500 || new Set(ids).size !== ids.length) { setNotice(t.missing); return; }
    await runAction(async () => {
      await mutation("/api/v1/workspace/groups/" + encodeURIComponent(groupTarget) + "/members",
        { client_ids: ids }, session.csrf_token);
      setMemberIDs("");
    });
  }
  async function editGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!session || !groupEditID.trim()) return;
    await runAction(async () => {
      await api("/api/v1/workspace/groups/" + encodeURIComponent(groupEditID), {
        method: "PATCH", headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token },
        body: JSON.stringify({ name: groupEditName, color: groupEditColor }),
      });
    });
  }
  async function deleteGroup(id: string, name: string) {
    if (!session || !window.confirm(t.confirmGroup + " " + name)) return;
    await runAction(async () => {
      await api("/api/v1/workspace/groups/" + encodeURIComponent(id), {
        method: "DELETE", headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token },
        body: JSON.stringify({}),
      });
      if (groupEditID === id) setGroupEditID("");
      if (groupTarget === id) setGroupTarget("");
    });
  }
  async function unassignMember(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!session || !groupTarget || !unassignID) return;
    await runAction(async () => {
      await api("/api/v1/workspace/groups/" + encodeURIComponent(groupTarget) + "/members/" + encodeURIComponent(unassignID), {
        method: "DELETE", headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token },
        body: JSON.stringify({}),
      });
      setUnassignID("");
    });
  }

  return { t, session, language, reload, oldPassword, setOldPassword, newPassword, setNewPassword, actionBusy, setActionBusy, notice, setNotice, refreshKey, setRefreshKey, me, setMe, reps, setReps, clients, setClients, events, setEvents, groups, setGroups, clientOps, setClientOps, repOffset, setRepOffset, clientOffset, setClientOffset, eventOffset, setEventOffset, groupOffset, setGroupOffset, clientOpsOffset, setClientOpsOffset, repId, setRepId, repName, setRepName, loginRep, setLoginRep, repUser, setRepUser, repPass, setRepPass, creditRep, setCreditRep, creditVolume, setCreditVolume, creditUnits, setCreditUnits, creditReason, setCreditReason, clientId, setClientId, clientName, setClientName, clientRep, setClientRep, clientKind, setClientKind, clientDuration, setClientDuration, clientIPs, setClientIPs, clientVolume, setClientVolume, groupID, setGroupID, groupName, setGroupName, groupColor, setGroupColor, groupRep, setGroupRep, groupTarget, setGroupTarget, memberIDs, setMemberIDs, groupEditID, setGroupEditID, groupEditName, setGroupEditName, groupEditColor, setGroupEditColor, unassignID, setUnassignID, lifeClientID, setLifeClientID, lifeDays, setLifeDays, lifeVolumeGB, setLifeVolumeGB, lifeReason, setLifeReason, lifeAction, setLifeAction, lifeStateReason, setLifeStateReason, runAction, changePassword, createRepresentative, createRepLogin, adjustCredit, createClient, extendExistingClient, changeExistingClientState, addGroup, assignGroup, editGroup, deleteGroup, unassignMember };
}
export type BusinessModel = ReturnType<typeof useBusinessWorkspace>;
