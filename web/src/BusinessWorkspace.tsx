import { useCallback, useEffect, useState, type FormEvent } from "react";
import { CheckCircle2, CircleHelp, CreditCard, KeyRound, LockKeyhole, LogOut, RefreshCw, ShieldCheck, Users } from "lucide-react";

type Language = "en" | "fa";
type User = { id: string; username: string; role: "owner" | "representative"; representative_id: string };
type Session = { authenticated: boolean; user: User; csrf_token: string; expires_unix: number };
type Representative = { id: string; display_name: string; kind: string; enabled: boolean; volume_credit_bytes: number; unlimited_credit_units: number };
type Client = { id: string; representative_id: string; name: string; plan_kind: string; ip_count: number; volume_bytes: number; duration_days: number; deployment_state: string; expires_unix: number };
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
    loginHelp: "Owner and representative credentials are separate from Node pairing and the temporary HTTPS preview password.",
    account: "Account", role: "Role", volume: "Volume credit", unlimited: "Unlimited units",
    reps: "Representatives", clients: "Clients", history: "Financial audit",
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
    loginHelp: "ورود مالک و نماینده مستقل از Pair Code نودها و رمز موقت HTTPS است.",
    account: "حساب", role: "نقش", volume: "اعتبار حجمی", unlimited: "اعتبار نامحدود",
    reps: "نمایندگان", clients: "کلاینت‌ها", history: "تاریخچه مالی",
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
  const [actionBusy, setActionBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [me, setMe] = useState<Workspace | null>(null);
  const [reps, setReps] = useState<Representative[] | null>(null);
  const [clients, setClients] = useState<Client[] | null>(null);
  const [events, setEvents] = useState<LedgerEvent[] | null>(null);
  const [repOffset, setRepOffset] = useState(0);
  const [clientOffset, setClientOffset] = useState(0);
  const [eventOffset, setEventOffset] = useState(0);
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
      const [account, ownClients, ownEvents, ownerReps] = await Promise.all([
        api<Workspace>("/api/v1/workspace/me", { signal: controller.signal }),
        api<{ clients: Client[] }>("/api/v1/workspace/clients" + p + clientOffset, { signal: controller.signal }),
        api<{ events: LedgerEvent[] }>("/api/v1/workspace/events" + p + eventOffset, { signal: controller.signal }),
        session.user.role === "owner"
          ? api<{ representatives: Representative[] }>("/api/v1/workspace/representatives" + p + repOffset, { signal: controller.signal })
          : Promise.resolve(null),
      ]);
      if (controller.signal.aborted) return;
      if (!Array.isArray(ownClients.clients) || !Array.isArray(ownEvents.events) ||
        (ownerReps !== null && !Array.isArray(ownerReps.representatives))) {
        throw new Error("Malformed business response");
      }
      setMe(account); setClients(ownClients.clients); setEvents(ownEvents.events);
      setReps(ownerReps?.representatives ?? null); setNotice(null);
    };
    void fetchData().catch((error: unknown) => {
      if (controller.signal.aborted) return;
      if (error instanceof ApiError && error.status === 401) {
        setSession(null); setMode("signedout"); setMe(null); setReps(null); setClients(null); setEvents(null);
      } else {
        setNotice(words[language].unavailable);
      }
    });
    return () => controller.abort();
  }, [session, refreshKey, clientOffset, eventOffset, repOffset, language]);

  async function signIn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setNotice(null); setActionBusy(true);
    try {
      const result = await api<Session>("/api/v1/session/login", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username: loginName, password: loginPassword }),
      });
      if (!result.user || !result.csrf_token) throw new Error("Invalid login response");
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
      setSession(null); setMode("signedout"); setMe(null); setReps(null); setClients(null); setEvents(null);
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
    const eventId = uid();
    await runAction(async () => {
      await mutation("/api/v1/workspace/representatives/" + encodeURIComponent(creditRep) + "/credits", {
        event_id: eventId, representative_id: creditRep, volume_delta_bytes: bytes,
        unlimited_delta_units: units, reason: creditReason.trim(),
      }, session.csrf_token);
      setCreditReason("");
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
    const eventId = uid();
    await runAction(async () => {
      await mutation("/api/v1/workspace/clients", {
        event_id: eventId, client_id: clientId.trim(),
        representative_id: session.user.role === "owner" ? clientRep : session.user.representative_id,
        name: clientName.trim(), duration_days: duration, ip_count: ipCount,
        volume_bytes: volumeBytes, unlimited: clientKind === "unlimited",
      }, session.csrf_token);
      setClientId(""); setClientName(""); setClientOffset(0);
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
          <strong>{c.name} ({c.id})</strong><span>{c.plan_kind} · {c.duration_days}d · {dateTime(c.expires_unix, language)} · {c.deployment_state}</span>
        </div>)}</div> : <p>{clients ? t.empty : t.loading}</p>}
        <div className="business-pagination"><button disabled={clientOffset === 0} onClick={() => setClientOffset((x) => Math.max(0, x - 50))}>{t.previous}</button>
          <button disabled={!clients || clients.length < 50} onClick={() => setClientOffset((x) => x + 50)}>{t.next}</button></div>
      </div>
      <div className="business-panel">
        <h3><CreditCard size={17}/>{t.history}</h3>
        {events && events.length ? <div className="business-records">{events.map((e) => <div key={e.event_id}>
          <strong>{e.kind} · {e.event_id}</strong><span>{e.reason} · {gb(e.volume_delta_bytes)} · {e.unlimited_delta_units} {t.unlimited}</span>
        </div>)}</div> : <p>{events ? t.empty : t.loading}</p>}
        <div className="business-pagination"><button disabled={eventOffset === 0} onClick={() => setEventOffset((x) => Math.max(0, x - 50))}>{t.previous}</button>
          <button disabled={!events || events.length < 50} onClick={() => setEventOffset((x) => x + 50)}>{t.next}</button></div>
      </div>
      <p className="business-footnote"><CheckCircle2 size={16}/>{t.unsafe}</p>
    </>}
    {notice && <div className="business-notice" role="status">{notice}</div>}
  </section>;
}
