import ClientUsage from "./ClientUsage";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { CheckCircle2, CircleHelp, CreditCard, KeyRound, LockKeyhole, LogOut, RefreshCw, ShieldCheck, Users } from "lucide-react";

type Language = "en" | "fa";
type User = { id: string; username: string; role: "owner" | "representative"; representative_id: string };
type Session = { authenticated: boolean; user: User; csrf_token: string; expires_unix: number };
type Representative = { id: string; display_name: string; kind: string; enabled: boolean; volume_credit_bytes: number; unlimited_credit_units: number };
type Client = { id: string; representative_id: string; name: string; plan_kind: string; ip_count: number; volume_bytes: number; duration_days: number; deployment_state: string; expires_unix: number; archived: boolean };
type ClientOperation = { event_id: string; client_id: string; representative_id: string; kind: string; reason: string; added_days: number; added_volume_bytes: number; volume_credit_debit: number; unlimited_credit_debit: number; created_unix: number };
type ClientGroup = { id: string; representative_id: string; name: string; color: string; member_count: number; created_unix: number; updated_unix: number };
type LedgerEvent = { event_id: string; kind: string; representative_id: string; object_id: string; reason: string; volume_delta_bytes: number; unlimited_delta_units: number };
type Workspace = { user: User; balance: Representative; data_plane: "not_activated" };
type Mode = "loading" | "disabled" | "signedout" | "signedin" | "unavailable";

class ApiError extends Error {
  constructor(public status: number, message: string) { super(message); }
}
async function api<T>(url: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(url, {
    ...options, cache: "no-store", credentials: "same-origin",
    headers: { Accept: "application/json", ...(options.headers ?? {}) },
  });
  if (!response.ok) {
    let code = "HTTP " + response.status;
    try {
      const payload: unknown = await response.json();
      if (payload && typeof payload === "object" && "error" in payload) {
        const value = (payload as { error: unknown }).error;
        if (typeof value === "string" && /^[a-z_]{3,80}$/.test(value)) code = value;
      }
    } catch { /* no server details */ }
    throw new ApiError(response.status, code);
  }
  return response.json() as Promise<T>;
}
function mutation<T>(url: string, payload: unknown, csrf: string): Promise<T> {
  return api<T>(url, {
    method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    body: JSON.stringify(payload),
  });
}
function uid(): string { return "pro-" + crypto.randomUUID().slice(0, 18); }
const words = {
  en: {
    loading: "Checking the private business service…",
    disabled: "Business storage is not enabled on this LAB HUB yet.",
    disabledNote: "The private owner setup, database and authenticated API must be enabled first. No fabricated accounts or credits are shown.",
    unavailable: "Business service unavailable. Previous verified information is retained.",
    login: "Sign in", logout: "Sign out", username: "Username", password: "Password",
    rotate: "Change password", oldPassword: "Current password", newPassword: "New password (12+ characters)", change: "Change and sign out",
    loginHelp: "Owner and representative credentials are separate from Node pairing and the temporary HTTPS preview password.",
    account: "Account", role: "Role", volume: "Volume credit", unlimited: "Unlimited units",
    reps: "Representatives", clients: "Clients", history: "Financial audit",
    groups: "Client groups", newGroup: "Create group", groupId: "Group ID", groupName: "Group name",
    groupColor: "Color (optional #RRGGBB)", groupCount: "members",
    groupAssign: "Move clients to group", clientIDs: "Client IDs (comma-separated, max 500)",
    groupEdit: "Rename / edit group", groupSave: "Save group", groupDelete: "Delete group",
    groupRemove: "Remove member from group", groupSingleClient: "Client ID to detach",
    confirmGroup: "Delete this group? All clients and financial history will remain intact.",
    groupWarning: "Deleting a group never deletes clients, consumed traffic or credit history.",
    manageClient: "Manage", clientLifecycle: "Renew and manage clients", renew: "Extend / add volume",
    extraDays: "Additional days", extraVolume: "Additional volume (GB)",
    archive: "Archive", disable: "Disable", enable: "Enable", stateAction: "Client action",
    applyState: "Record state change", archiveConfirm: "Archive client? UUID, history, spent credit and group memberships remain preserved.",
    clientOps: "Client operation history", archived: "Archived", chooseClient: "Choose an existing client",
    safeLifecycle: "Enable only returns to pending Node Apply; no Xray service is activated.",
    newClientReason: "Operation reason",
    createRep: "Create representative", repId: "Representative ID", displayName: "Display name",
    provision: "Set representative login", setLogin: "Create login", repUsername: "Representative username", repPassword: "New password (12+ characters)",
    grant: "Adjust representative credit", reason: "Reason", addVolume: "Volume delta (GB, +/-)", addUnits: "Unlimited-unit delta (+/-)",
    applyCredit: "Record credit transaction", createClient: "Create client record", clientId: "Client ID",
    clientName: "Client name", duration: "Duration (days)", slots: "IP/User slots (0 = unlimited for volume)",
    plan: "Plan type", volumePlan: "Volume", unlimitedPlan: "Unlimited",
    volumeGB: "Plan volume (GB)", saveClient: "Create pending client",
    pending: "Client records are pending Xray Node Apply; no active subscription is issued.",
    refresh: "Refresh", previous: "Previous", next: "Next", empty: "No records in this view.",
    accountScope: "Scoped to your representative account.", ownerScope: "Owner-only operations",
    failed: "Request failed", saved: "Saved in private PRO database",
    unsafe: "LAB ONLY · no old HUB or Telegram migration · no live Xray deployment",
    missing: "Complete the required fields with valid values.", loginError: "Sign-in rejected or temporarily unavailable.",
  },
  fa: {
    loading: "در حال بررسی سرویس خصوصی حسابداری…",
    disabled: "دیتابیس مالی در HUB آزمایشی هنوز فعال نشده است.",
    disabledNote: "ابتدا باید حساب مالک، دیتابیس خصوصی و API امن فعال شوند. اطلاعات و اعتبار ساختگی نمایش نمی‌دهیم.",
    unavailable: "سرویس حسابداری در دسترس نیست. آخرین اطلاعات معتبر حفظ شده است.",
    login: "ورود", logout: "خروج", username: "نام کاربری", password: "رمز عبور",
    rotate: "تغییر رمز عبور", oldPassword: "رمز فعلی", newPassword: "رمز جدید (حداقل ۱۲ کاراکتر)", change: "تغییر رمز و خروج",
    loginHelp: "ورود مالک و نماینده مستقل از Pair Code نودها و رمز موقت HTTPS است.",
    account: "حساب", role: "نقش", volume: "اعتبار حجمی", unlimited: "اعتبار نامحدود",
    reps: "نمایندگان", clients: "کلاینت‌ها", history: "تاریخچه مالی",
    groups: "گروه‌های کلاینت‌ها", newGroup: "ساخت گروه", groupId: "شناسه گروه", groupName: "نام گروه",
    groupColor: "رنگ (اختیاری #RRGGBB)", groupCount: "عضو",
    groupAssign: "انتقال گروهی کلاینت‌ها", clientIDs: "شناسه کلاینت‌ها (با کاما، حداکثر ۵۰۰)",
    groupEdit: "ویرایش گروه", groupSave: "ذخیره گروه", groupDelete: "حذف گروه",
    groupRemove: "حذف عضو از گروه", groupSingleClient: "شناسه کلاینت برای جداسازی",
    confirmGroup: "این گروه حذف شود؟ کلاینت‌ها و سوابق مالی محفوظ می‌مانند.",
    groupWarning: "حذف گروه هرگز کلاینت‌ها، ترافیک مصرف‌شده و اعتبار تاریخی را پاک نمی‌کند.",
    manageClient: "مدیریت", clientLifecycle: "تمدید و مدیریت کلاینت‌ها", renew: "تمدید / افزایش حجم",
    extraDays: "روز اضافه", extraVolume: "حجم اضافه (گیگ)",
    archive: "آرشیو", disable: "غیرفعال", enable: "فعال‌سازی رکورد", stateAction: "عملیات کلاینت",
    applyState: "ثبت تغییر وضعیت", archiveConfirm: "کلاینت آرشیو شود؟ UUID، تاریخچه، اعتبار مصرف‌شده و عضویت گروه‌ها محفوظ می‌مانند.",
    clientOps: "تاریخچه عملیات کلاینت", archived: "آرشیوشده", chooseClient: "انتخاب کلاینت",
    safeLifecycle: "فعال‌سازی فقط رکورد را در انتظار Node Apply قرار می‌دهد؛ Xray فعال نمی‌شود.",
    newClientReason: "دلیل عملیات",
    createRep: "ساخت نماینده", repId: "شناسه نماینده", displayName: "نام نمایشی",
    provision: "ساخت ورود نماینده", setLogin: "ثبت ورود", repUsername: "نام کاربری نماینده", repPassword: "رمز جدید (حداقل ۱۲ کاراکتر)",
    grant: "تغییر اعتبار نماینده", reason: "دلیل تغییر", addVolume: "تغییر اعتبار حجمی (گیگ، +/-)", addUnits: "تغییر اعتبار نامحدود (+/-)",
    applyCredit: "ثبت تراکنش اعتبار", createClient: "ساخت رکورد کلاینت", clientId: "شناسه کلاینت",
    clientName: "نام کلاینت", duration: "مدت (روز)", slots: "تعداد IP/کاربر (صفر = نامحدود برای حجمی)",
    plan: "نوع اشتراک", volumePlan: "حجمی", unlimitedPlan: "نامحدود",
    volumeGB: "حجم پلن (گیگ)", saveClient: "ساخت کلاینت در انتظار اعمال",
    pending: "کلاینت‌ها تا قبل از Apply واقعی Xray فعال نمی‌شوند و لینک اشتراک صادر نمی‌شود.",
    refresh: "به‌روزرسانی", previous: "قبلی", next: "بعدی", empty: "هنوز رکوردی برای این بخش وجود ندارد.",
    accountScope: "فقط اطلاعات نمایندگی خودت قابل‌دسترسی است.", ownerScope: "عملیات مخصوص مالک",
    failed: "عملیات ناموفق بود", saved: "در دیتابیس خصوصی PRO ثبت شد",
    unsafe: "فقط LAB · بدون مهاجرت HUB قدیمی یا ربات‌ها · بدون اعمال زنده Xray",
    missing: "فیلدهای ضروری را با مقدار معتبر تکمیل کن.", loginError: "ورود پذیرفته نشد یا سرویس در دسترس نیست.",
  },
};

function gb(bytes: number): string {
  return Number.isFinite(bytes) ? (bytes / (1024 ** 3)).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " GB" : "—";
}
function dateTime(sec: number, lang: Language): string {
  return Number.isFinite(sec) ? new Date(sec * 1000).toLocaleDateString(lang === "fa" ? "fa-IR" : "en-GB") : "—";
}
function bytesFromGB(value: string, allowNegative = false): number | null {
  const number = Number(value);
  if (!value.trim() || !Number.isFinite(number) || (!allowNegative && number <= 0)) return null;
  const bytes = Math.round(number * (1024 ** 3));
  return Number.isSafeInteger(bytes) && Math.abs(bytes) <= 1_000_000_000_000_000_000 ? bytes : null;
}

export default function BusinessWorkspace({ language }: { language: Language }) {
  const t = words[language];
  const [mode, setMode] = useState<Mode>("loading");
  const [session, setSession] = useState<Session | null>(null);
  const [loginName, setLoginName] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const pendingCreditKey = useRef<string | null>(null);
  const pendingClientKey = useRef<string | null>(null);
  const [actionBusy, setActionBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [me, setMe] = useState<Workspace | null>(null);
  const [reps, setReps] = useState<Representative[] | null>(null);
  const [clients, setClients] = useState<Client[] | null>(null);
  const [events, setEvents] = useState<LedgerEvent[] | null>(null);
  const [groups, setGroups] = useState<ClientGroup[] | null>(null);
  const [clientOps, setClientOps] = useState<ClientOperation[] | null>(null);
  const [repOffset, setRepOffset] = useState(0);
  const [clientOffset, setClientOffset] = useState(0);
  const [eventOffset, setEventOffset] = useState(0);
  const [groupOffset, setGroupOffset] = useState(0);
  const [clientOpsOffset, setClientOpsOffset] = useState(0);
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
  const [groupTarget, setGroupTarget] = useState("");
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

  useEffect(() => {
    let alive = true;
    void api<{enabled: boolean; authenticated: boolean; user?: User; csrf_token?: string; expires_unix?: number}>("/api/v1/session/status")
      .then((r) => {
        if (!alive) return;
        if (r.authenticated && r.user && r.csrf_token && r.expires_unix) {
          setSession({ authenticated: true, user: r.user, csrf_token: r.csrf_token, expires_unix: r.expires_unix });
          setMode("signedin");
        } else {
          setSession(null); setMode("signedout");
        }
      })
      .catch((error: unknown) => {
        if (!alive) return;
        setMode(error instanceof ApiError && error.status === 404 ? "disabled" : "unavailable");
      });
    return () => { alive = false; };
  }, []);

  const reload = useCallback(() => setRefreshKey((n) => n + 1), []);
  useEffect(() => {
    if (!session) return;
    const controller = new AbortController();
    const p = "?limit=50&offset=";
    const fetchData = async () => {
      const [account, ownClients, ownEvents, ownerReps, ownGroups, lifecycle] = await Promise.all([
        api<Workspace>("/api/v1/workspace/me", { signal: controller.signal }),
        api<{ clients: Client[] }>("/api/v1/workspace/clients" + p + clientOffset, { signal: controller.signal }),
        api<{ events: LedgerEvent[] }>("/api/v1/workspace/events" + p + eventOffset, { signal: controller.signal }),
        session.user.role === "owner"
          ? api<{ representatives: Representative[] }>("/api/v1/workspace/representatives" + p + repOffset, { signal: controller.signal })
          : Promise.resolve(null),
        api<{ groups: ClientGroup[] }>("/api/v1/workspace/groups" + p + groupOffset, { signal: controller.signal }),
        api<{ operations: ClientOperation[] }>("/api/v1/workspace/client-operations" + p + clientOpsOffset, { signal: controller.signal }),
      ]);
      if (controller.signal.aborted) return;
      if (!Array.isArray(ownClients.clients) || !Array.isArray(ownEvents.events) ||
        (ownerReps !== null && !Array.isArray(ownerReps.representatives)) || !Array.isArray(ownGroups.groups) || !Array.isArray(lifecycle.operations)) {
        throw new Error("Malformed business response");
      }
      setMe(account); setClients(ownClients.clients); setEvents(ownEvents.events);
      setReps(ownerReps?.representatives ?? null); setGroups(ownGroups.groups); setClientOps(lifecycle.operations); setNotice(null);
    };
    void fetchData().catch((error: unknown) => {
      if (controller.signal.aborted) return;
      if (error instanceof ApiError && error.status === 401) {
        setSession(null); setMode("signedout"); setMe(null); setReps(null); setClients(null); setEvents(null); setGroups(null); setClientOps(null);
        pendingCreditKey.current = null; pendingClientKey.current = null;
      } else {
        setNotice(words[language].unavailable);
      }
    });
    return () => controller.abort();
  }, [session, refreshKey, clientOffset, eventOffset, repOffset, groupOffset, clientOpsOffset, language]);

  async function signIn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setNotice(null); setActionBusy(true);
    try {
      const result = await api<Session>("/api/v1/session/login", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username: loginName, password: loginPassword }),
      });
      if (!result.user || !result.csrf_token) throw new Error("Invalid login response");
      setMe(null); setReps(null); setClients(null); setEvents(null); setGroups(null);
      setSession(result); setMode("signedin"); setLoginPassword("");
    } catch { setNotice(t.loginError); }
    finally { setActionBusy(false); }
  }
  async function runAction(fn: () => Promise<void>, silent = false) {
    setActionBusy(true); setNotice(null);
    try { await fn(); if (!silent) setNotice(t.saved); reload(); }
    catch (error) {
      const details = error instanceof Error ? error.message : "request_failed";
      setNotice(t.failed + ": " + details);
    } finally { setActionBusy(false); }
  }
  async function signOut() {
    if (!session) return;
    await runAction(async () => {
      await mutation("/api/v1/session/logout", {}, session.csrf_token);
      setSession(null); setMode("signedout"); setMe(null); setReps(null); setClients(null); setEvents(null); setGroups(null);
    }, true);
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
      setSession(null); setMode("signedout");
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
    const eventId = pendingCreditKey.current ?? uid();
    pendingCreditKey.current = eventId;
    await runAction(async () => {
      await mutation("/api/v1/workspace/representatives/" + encodeURIComponent(creditRep) + "/credits", {
        event_id: eventId, representative_id: creditRep, volume_delta_bytes: bytes,
        unlimited_delta_units: units, reason: creditReason.trim(),
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
    const eventId = pendingClientKey.current ?? uid();
    pendingClientKey.current = eventId;
    await runAction(async () => {
      await mutation("/api/v1/workspace/clients", {
        event_id: eventId, client_id: clientId.trim(),
        representative_id: session.user.role === "owner" ? clientRep : session.user.representative_id,
        name: clientName.trim(), duration_days: duration, ip_count: ipCount,
        volume_bytes: volumeBytes, unlimited: clientKind === "unlimited",
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

  return <section className="business-workspace" aria-label="Business workspace">
    {mode === "loading" && <div className="business-panel"><strong>{t.loading}</strong></div>}
    {(mode === "disabled" || mode === "unavailable") && <div className="business-panel business-alert">
      <CircleHelp size={22}/><div><strong>{mode === "disabled" ? t.disabled : t.unavailable}</strong><p>{t.disabledNote}</p></div>
    </div>}
    {mode === "signedout" && <div className="business-panel business-login">
      <LockKeyhole size={28}/>
      <h2>{t.login}</h2><p>{t.loginHelp}</p>
      <form onSubmit={(event) => { void signIn(event); }}>
        <label>{t.username}<input autoComplete="username" value={loginName} onChange={(e) => setLoginName(e.target.value)} required minLength={3}/></label>
        <label>{t.password}<input type="password" autoComplete="current-password" value={loginPassword} onChange={(e) => setLoginPassword(e.target.value)} required minLength={12}/></label>
        <button type="submit" disabled={actionBusy}>{t.login}</button>
      </form>
    </div>}
    {mode === "signedin" && session && <>
      <header className="business-topbar">
        <div><span className="business-eyebrow"><ShieldCheck size={15}/>{t.account}</span>
          <h2>{session.user.username}</h2><p>{session.user.role === "owner" ? t.ownerScope : t.accountScope}</p>
        </div>
        <div className="business-topbar-actions">
          <button type="button" onClick={reload}><RefreshCw size={15}/>{t.refresh}</button>
          <button type="button" disabled={actionBusy} onClick={() => { void signOut(); }}><LogOut size={15}/>{t.logout}</button>
        </div>
      </header>
      {me && <div className="business-metrics">
        <div><span>{t.volume}</span><strong>{gb(me.balance.volume_credit_bytes)}</strong></div>
        <div><span>{t.unlimited}</span><strong>{me.balance.unlimited_credit_units.toLocaleString()}</strong></div>
        <div><span>{t.role}</span><strong>{session.user.role}</strong></div>
      </div>}
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
        <p className="business-warning">{t.pending}</p>
        <button type="submit" disabled={actionBusy}>{t.saveClient}</button>
      </form>
      <div className="business-panel">
        <h3>{t.clients}</h3>
        {clients && clients.length ? <div className="business-records">{clients.map((c) => <div key={c.id}>
          <strong>{c.name} ({c.id})</strong><span>{c.plan_kind} · {c.duration_days}d · {dateTime(c.expires_unix, language)} · {c.deployment_state}{c.archived ? " · " + t.archived : ""}</span>
          <button type="button" className="business-manage-client" onClick={() => setLifeClientID(c.id)}>{t.manageClient}</button>
        </div>)}</div> : <p>{clients ? t.empty : t.loading}</p>}
        <div className="business-pagination"><button disabled={clientOffset === 0} onClick={() => setClientOffset((x) => Math.max(0, x - 50))}>{t.previous}</button>
          <button disabled={!clients || clients.length < 50} onClick={() => setClientOffset((x) => x + 50)}>{t.next}</button></div>
      </div>
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
      <div className="business-panel">
        <h3><CreditCard size={17}/>{t.history}</h3>
        {events && events.length ? <div className="business-records">{events.map((e) => <div key={e.event_id}>
          <strong>{e.kind} · {e.event_id}</strong><span>{e.reason} · {gb(e.volume_delta_bytes)} · {e.unlimited_delta_units} {t.unlimited}</span>
        </div>)}</div> : <p>{events ? t.empty : t.loading}</p>}
        <div className="business-pagination"><button disabled={eventOffset === 0} onClick={() => setEventOffset((x) => Math.max(0, x - 50))}>{t.previous}</button>
          <button disabled={!events || events.length < 50} onClick={() => setEventOffset((x) => x + 50)}>{t.next}</button></div>
      </div>
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
      <p className="business-footnote"><CheckCircle2 size={16}/>{t.unsafe}</p>
    </>}
    {notice && <div className="business-notice" role="status">{notice}</div>}
  </section>;
}
