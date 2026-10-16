import "./NativeInbounds.css";
import { useSession, api, ApiError as Fault } from "./session";
import InboundLifecycle from "./InboundLifecycle";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { ArrowRightLeft, CheckCircle2, KeyRound, RefreshCw, Server, ShieldCheck, Users, Waypoints } from "lucide-react";

type Lang = "fa" | "en";
type Inbound = { revision: number; enabled: boolean; archived: boolean; id: number; name: string; transport: string; grpc_service: string; reality_target: string; server_names: string[]; short_ids: string[]; public_key: string; bound_clients: number };
type Target = { inbound_id: number; node_id: string; installation_id: string; listen_ip: string; direct_port: number; swap_port: number };
type Node = { node_id: string; installation_id: string; name: string };
type Status = { node_id: string; installation_id: string; desired_sequence: number; desired_sha: string; reported_sequence: number; reported_sha: string; state: string; phase: string; reported_at?: string; lease_expires_at?: string; process_ready: boolean };
type Overview = { inbounds: Inbound[]; nodes: Node[]; targets: Target[]; runtime: Status[] };
type Client = { id: string; name: string; representative_id: string; archived: boolean; deployment_state: string };
async function request<T>(path: string, signal: AbortSignal, body?: unknown, csrf?: string): Promise<T> {
 return api<T>(path, { signal, method: body === undefined ? "GET" : "POST",
  headers: body === undefined ? {} : { "Content-Type": "application/json", ...(csrf ? { "X-CSRF-Token": csrf } : {}) },
  ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
}
const text = {
 en: {
  title: "Native Inbounds", lead: "Real client bindings and configuration delivery to new PRO Nodes.", login: "Owner sign-in", user: "Username", pass: "Password", enter: "Sign in",
  checking: "Checking owner access…", ownerOnly: "Only the owner can manage Inbounds. Representatives cannot change Node deployments.", unavailable: "Native Inbounds are not enabled on this HUB.", error: "Request failed. No successful Apply is assumed. Refresh before retrying.", retry: "Refresh",
  create: "Create REALITY Inbound", name: "Inbound name", transport: "Transport", dest: "REALITY target (host:port)", sni: "Server names (comma-separated)", service: "gRPC service", save: "Create Inbound",
  empty: "No native Inbounds yet.", select: "Select Inbound", bound: "bound clients", members: "Client bindings", search: "Search this page", add: "Attach selected", remove: "Detach selected", previous: "Previous", next: "Next", all: "Select this page", noClients: "No clients on this page.", bindingHelp: "Detaching removes access from this Inbound; it never deletes the client or financial history.",
  nodes: "Node deployment", node: "New paired Node", choose: "Choose a Node", listen: "Node's assigned IP", direct: "Direct port (0 = disabled)", swap: "Dark Swap destination port (0 = disabled)", targetSave: "Save Node target", noNodes: "No new paired PRO Nodes. Legacy Nodes are not imported.", targetHelp: "Saving changes desired configuration. An opted-in Agent may restart its own Xray process. This does not create the transit tunnel.", confirm: "Save this Node target? An opted-in PRO Agent may restart Xray on this Node. Existing connections can be interrupted.",
  listenersDisabled: "All managed listeners are disabled", enabledLabel: "Enabled", disabledLabel: "Disabled", archivedLabel: "Archived", status: "Apply reports", desired: "Desired revision", observed: "Reported revision", waiting: "Awaiting Agent report", applied: "Matching process reported", stale: "Report stale / lease expired", changed: "Configuration changed; awaiting Apply", stopped: "Process not ready", mismatch: "Reported revision differs", evidence: "An authenticated Agent report is not proof of customer connectivity or billing correctness. No subscription is published here.", phase: "Agent phase", last: "Last report", saved: "Saved. Waiting for a fresh Agent report; no migration occurred.", shared: "Shared Direct/Swap port: route-specific usage is ambiguous.",
 },
 fa: {
  title: "اینباندهای واقعی PRO", lead: "اتصال کلاینت‌ها و ارسال تنظیمات به نودهای جدید PRO.", login: "ورود مالک", user: "نام کاربری", pass: "رمز عبور", enter: "ورود",
  checking: "در حال بررسی دسترسی مالک…", ownerOnly: "مدیریت اینباند فقط در اختیار مالک است؛ نمایندگان مجوز تغییر نودها را ندارند.", unavailable: "اینباند واقعی در این HUB هنوز فعال نشده است.", error: "درخواست ناموفق بود؛ موفقیت Apply فرض نمی‌شود. پیش از تلاش دوباره، به‌روزرسانی کن.", retry: "به‌روزرسانی",
  create: "ساخت اینباند REALITY", name: "نام اینباند", transport: "ترنسپورت", dest: "مقصد REALITY (هاست:پورت)", sni: "نام‌های سرور (با کاما)", service: "سرویس gRPC", save: "ساخت اینباند",
  empty: "هنوز اینباند واقعی ساخته نشده است.", select: "انتخاب اینباند", bound: "کلاینت متصل", members: "اتصال کلاینت‌ها", search: "جست‌وجو در این صفحه", add: "اتصال انتخاب‌شده‌ها", remove: "جداسازی انتخاب‌شده‌ها", previous: "قبلی", next: "بعدی", all: "انتخاب این صفحه", noClients: "کلاینتی در این صفحه نیست.", bindingHelp: "جداسازی، دسترسی این اینباند را برمی‌دارد؛ کلاینت و سوابق مالی پاک نمی‌شوند.",
  nodes: "تنظیم مقصد نود", node: "نود جدید متصل", choose: "انتخاب نود", listen: "IP اختصاص‌یافته به نود", direct: "پورت مستقیم (صفر = غیرفعال)", swap: "پورت مقصد Dark Swap (صفر = غیرفعال)", targetSave: "ذخیره مقصد نود", noNodes: "نود جدیدی به PRO متصل نیست؛ نودهای قدیمی خودکار وارد نمی‌شوند.", targetHelp: "ذخیره، نسخهٔ درخواستی را تغییر می‌دهد. Agent فعال ممکن است Xray همین نود را بازراه‌اندازی کند؛ این کار تانل واسط نمی‌سازد.", confirm: "مقصد نود ذخیره شود؟ Agent فعال PRO ممکن است Xray همین نود را بازراه‌اندازی کند و اتصال‌های آن قطع شوند.",
  listenersDisabled: "همهٔ پورت‌های تحت مدیریت غیرفعال‌اند", enabledLabel: "فعال", disabledLabel: "غیرفعال", archivedLabel: "آرشیوشده", status: "گزارش اعمال تنظیمات", desired: "نسخهٔ درخواستی", observed: "نسخهٔ گزارش‌شده", waiting: "در انتظار گزارش Agent", applied: "اجرای نسخهٔ مطابق گزارش شد", stale: "گزارش قدیمی / مجوز منقضی", changed: "تنظیمات تغییر کرده؛ در انتظار Apply", stopped: "فرآیند آماده نیست", mismatch: "نسخهٔ گزارش‌شده متفاوت است", evidence: "گزارش معتبر Agent، اثبات اتصال مشتری یا صحت حسابداری نیست. در این بخش اشتراک منتشر نمی‌شود.", phase: "مرحلهٔ Agent", last: "آخرین گزارش", saved: "ذخیره شد؛ منتظر گزارش تازهٔ Agent هستیم. مهاجرتی انجام نشد.", shared: "پورت Direct و Swap مشترک است؛ مصرف هر مسیر جداگانه قابل انتساب قطعی نیست.",
 },
};
function freshID(): number { return crypto.getRandomValues(new Uint32Array(1))[0] || 1; }
function shortID(): string { return Array.from(crypto.getRandomValues(new Uint8Array(4))).map(v => v.toString(16).padStart(2, "0")).join(""); }
const split = (s: string) => s.split(/[\s,]+/).filter(Boolean);
export default function NativeInbounds({ language, fallback }: { language: Lang; fallback: ReactNode }) {
 const t = text[language];
 const { session, expire } = useSession();const [disabled, setDisabled] = useState(false);
 const [data, setData] = useState<Overview | null>(null);const [clients, setClients] = useState<Client[]>([]);const [bound, setBound] = useState<string[]>([]);
 const [failure, setFailure] = useState(false);const [notice, setNotice] = useState("");const [busy, setBusy] = useState(false);
 const [refresh, setRefresh] = useState(0);const [tick, setTick] = useState(Date.now());const lastSuccess = useRef(0);
 const [chosen, setChosen] = useState(() => { try { const n = Number(sessionStorage.getItem("dark-pro-native-inbound"));return Number.isSafeInteger(n) && n > 0 ? n : 0; } catch { return 0; } });
 const [checked, setChecked] = useState<string[]>([]);const [offset, setOffset] = useState(0);const [search, setSearch] = useState("");
 const [name, setName] = useState("");const [transport, setTransport] = useState("tcp");const [target, setTarget] = useState("");const [sni, setSni] = useState("");const [service, setService] = useState("");
 const createIdentity = useRef<{ id: number; short: string } | null>(null);
 const [node, setNode] = useState("");const [listen, setListen] = useState("");const [direct, setDirect] = useState("0");const [swap, setSwap] = useState("0");
 const lifetime = useRef<AbortController | null>(null);
 useEffect(() => { try { sessionStorage.setItem("dark-pro-native-inbound", String(chosen)); } catch { /* private browsing */ } }, [chosen]);
 useEffect(() => {
  const controller = new AbortController();lifetime.current = controller;
  const timer = setInterval(() => setTick(Date.now()), 1000);return () => { controller.abort();clearInterval(timer); };
 }, [refresh]);
 useEffect(() => {
  if (!session?.authenticated || session.user?.role !== "owner") return;
  const controller = new AbortController();let timer: ReturnType<typeof setTimeout>;
  async function poll() {
   try {
    const [overview, list, bindings] = await Promise.all([
     request<Overview>("/api/v1/workspace/native-inbounds-state", controller.signal),
     request<{ clients: Client[] }>("/api/v1/workspace/clients?limit=100&offset=" + offset, controller.signal),
     chosen ? request<{ client_ids: string[] }>("/api/v1/workspace/native-inbounds/" + chosen + "/clients", controller.signal) : Promise.resolve({ client_ids: [] }),
    ]);
    if (controller.signal.aborted) return;
    if (![overview.inbounds, overview.nodes, overview.targets, overview.runtime, list.clients, bindings.client_ids].every(Array.isArray)) throw new Error("Invalid native response");
    setData(overview);setClients(list.clients);setBound(bindings.client_ids);setFailure(false);setDisabled(false);lastSuccess.current = Date.now();
    if (chosen && !overview.inbounds.some(i => i.id === chosen)) { setChosen(0);setChecked([]); }
   } catch (e) {
    if (controller.signal.aborted) return;
    if (e instanceof Fault && e.status === 401) { expire();setData(null);setClients([]);setChecked([]); }
    else if (e instanceof Fault && e.status === 404) { setDisabled(true);setData(null); }else setFailure(true);
   } finally { if (!controller.signal.aborted) timer = setTimeout(() => { if (document.hidden) timer = setTimeout(poll, 5000);else void poll(); }, 5000); }
  }
  void poll();return () => { controller.abort();clearTimeout(timer); };
 }, [session, chosen, offset, refresh, expire]);
 async function mutate(path: string, body: unknown, after: () => void) {
  const controller = lifetime.current;if (!controller || controller.signal.aborted || busy) return;setBusy(true);setNotice("");
  try { await request(path, controller.signal, body, session?.csrf_token);if (!controller.signal.aborted) { after();setNotice(t.saved);setRefresh(n => n + 1); } }
  catch { if (!controller.signal.aborted) { setFailure(true);setNotice(t.error); } }finally { if (!controller.signal.aborted) setBusy(false); }
 }
 function create(e: FormEvent) {
  e.preventDefault();if (!createIdentity.current) createIdentity.current = { id: freshID(), short: shortID() };const identity = createIdentity.current;
  void mutate("/api/v1/workspace/native-inbounds", { id: identity.id, name, transport, grpc_service: transport === "grpc" ? service : "", reality_target: target, server_names: split(sni), short_ids: [identity.short] }, () => { setChosen(identity.id);createIdentity.current = null;setName("");setChecked([]); });
 }
 function pickNode(id: string) {
  setNode(id);const existing = data?.targets.find(x => x.node_id === id && x.inbound_id === chosen);
  setListen(existing?.listen_ip ?? "");setDirect(String(existing?.direct_port ?? 0));setSwap(String(existing?.swap_port ?? 0));
 }
 function saveTarget(e: FormEvent) {
  e.preventDefault();const n = data?.nodes.find(x => x.node_id === node);if (!n || !chosen || !window.confirm(t.confirm)) return;
  void mutate("/api/v1/workspace/native-inbounds/" + chosen + "/target", { inbound_id: chosen, node_id: node, installation_id: n.installation_id, listen_ip: listen, direct_port: Number(direct), swap_port: Number(swap) }, () => {});
 }
 const stale = failure || tick - lastSuccess.current > 12000;
 function matching(s: Status): boolean {
  const expires = Date.parse(s.lease_expires_at ?? "");const received = Date.parse(s.reported_at ?? "");
  return !stale && s.state === "applied" && s.process_ready && s.desired_sequence > 0 && s.desired_sequence === s.reported_sequence && s.desired_sha === s.reported_sha && !!s.desired_sha &&
   Number.isFinite(expires) && expires > tick && Number.isFinite(received) && received <= tick + 1000 && tick - received <= 12000;
 }
 function statusLabel(s: Status): string {
  if (stale || (s.state === "applied" && !matching(s)) || (s.lease_expires_at && Date.parse(s.lease_expires_at) <= tick)) return t.stale;
  return ({ applied: t.applied, awaiting_report: t.waiting, stale: t.stale, configuration_changed: t.changed, stopped: t.stopped, listeners_disabled: t.listenersDisabled, out_of_sync: t.mismatch } as Record<string,string>)[s.state] ?? t.waiting;
 }
 const current = data?.inbounds.find(i => i.id === chosen);
 const visible = clients.filter(c => (c.name + " " + c.id).toLowerCase().includes(search.toLowerCase()));
 if (disabled) return <>{fallback}</>;
 return <section className="native-workspace" aria-label="Native Inbounds">
  <header className="native-heading"><div><span className="eyebrow">DARK XRAY PRO</span><h2><Waypoints size={24}/>{t.title}</h2><p>{t.lead}</p></div>
   <button type="button" onClick={() => setRefresh(n => n + 1)} disabled={busy}><RefreshCw size={16}/>{t.retry}</button></header>
  {failure && <p className="native-warning" role="alert">{t.error}</p>}
  {!session && !failure && <p>{t.checking}</p>}
  {session?.authenticated && session.user?.role !== "owner" && <p className="native-warning">{t.ownerOnly}</p>}
  {disabled && <p className="native-warning">{t.unavailable}</p>}
  {session?.authenticated && session.user?.role === "owner" && data && <>
   <div className="native-grid">
    <form className="native-card" onSubmit={create}><h3><KeyRound size={19}/>{t.create}</h3>
     <label>{t.name}<input value={name} onChange={e => setName(e.target.value)} required maxLength={128}/></label>
     <label>{t.transport}<select value={transport} onChange={e => setTransport(e.target.value)}><option value="tcp">TCP / REALITY</option><option value="grpc">gRPC / REALITY</option></select></label>
     {transport === "grpc" && <label>{t.service}<input value={service} onChange={e => setService(e.target.value)} required pattern="[A-Za-z0-9_-]{1,64}"/></label>}
     <label>{t.dest}<input value={target} onChange={e => setTarget(e.target.value)} required placeholder="example.org:443"/></label>
     <label>{t.sni}<input value={sni} onChange={e => setSni(e.target.value)} required placeholder="example.org"/></label>
     <button type="submit" disabled={busy || stale}>{t.save}</button>
    </form>
    <div className="native-card"><h3>{t.select}</h3>{!data.inbounds.length && <p>{t.empty}</p>}
     <div className="native-inbound-list">{data.inbounds.map(i => <button type="button" key={i.id} aria-pressed={chosen === i.id} onClick={() => { setChosen(i.id);setChecked([]);setNode("");setListen("");setDirect("0");setSwap("0"); }}>
      <strong>{i.name}</strong><span>#{i.id} · {i.transport} · {i.bound_clients} {t.bound} · {i.archived ? t.archivedLabel : i.enabled ? t.enabledLabel : t.disabledLabel}</span></button>)}</div>
    </div>
   </div>
   {chosen > 0 && current && <>
    <InboundLifecycle key={chosen} inbound={current} language={language} csrf={session.csrf_token ?? ""} blocked={busy || stale} onChanged={() => setRefresh(n => n + 1)} />
    <div className="native-card"><h3><Users size={19}/>{t.members}</h3><p>{t.bindingHelp}</p>
     <label>{t.search}<input value={search} onChange={e => setSearch(e.target.value)}/></label>
     <div className="native-client-list">{visible.map(c => <label key={c.id} className="native-client-row"><input type="checkbox" checked={checked.includes(c.id)} onChange={e => setChecked(old => e.target.checked ? [...old, c.id] : old.filter(id => id !== c.id))}/>
      <strong>{c.name}</strong><span>{c.id} · {c.representative_id}</span>{bound.includes(c.id) && <CheckCircle2 size={17} aria-label="Bound"/>}</label>)}{!visible.length && <p>{t.noClients}</p>}</div>
     <div className="native-actions"><button type="button" onClick={() => setChecked(visible.map(c => c.id))}>{t.all}</button>
      <button type="button" disabled={busy || stale || current.archived || !checked.length || checked.some(id => clients.find(c => c.id === id)?.archived)} onClick={() => { void mutate("/api/v1/workspace/native-inbounds/" + chosen + "/clients", { client_ids: checked, attach: true }, () => setChecked([])); }}>{t.add}</button>
      <button type="button" disabled={busy || stale || !checked.length} onClick={() => { void mutate("/api/v1/workspace/native-inbounds/" + chosen + "/clients", { client_ids: checked, attach: false }, () => setChecked([])); }}>{t.remove}</button></div>
     <div className="native-actions"><button type="button" disabled={offset === 0 || busy} onClick={() => { setOffset(n => Math.max(0, n - 100));setChecked([]); }}>{t.previous}</button><button type="button" disabled={clients.length < 100 || busy} onClick={() => { setOffset(n => n + 100);setChecked([]); }}>{t.next}</button></div>
    </div>
    <form className="native-card" onSubmit={saveTarget}><h3><Server size={19}/>{t.nodes}</h3><p>{t.targetHelp}</p>
     {!data.nodes.length && <p className="native-warning">{t.noNodes}</p>}
     <div className="native-fields"><label>{t.node}<select value={node} onChange={e => pickNode(e.target.value)} required><option value="">{t.choose}</option>{data.nodes.map(n => <option value={n.node_id} key={n.node_id}>{n.name} ({n.node_id})</option>)}</select></label>
      <label>{t.listen}<input value={listen} onChange={e => setListen(e.target.value)} required placeholder="203.0.113.10"/></label>
      <label>{t.direct}<input type="number" min={0} max={65535} step={1} value={direct} onChange={e => setDirect(e.target.value)} required/></label>
      <label>{t.swap}<input type="number" min={0} max={65535} step={1} value={swap} onChange={e => setSwap(e.target.value)} required/></label></div>
     {Number(direct) > 0 && direct === swap && <p className="native-warning">{t.shared}</p>}
     <button type="submit" disabled={busy || stale || current.archived || !node || (Number(direct) === 0 && Number(swap) === 0)}>{t.targetSave}</button>
    </form>
   </>}
   <div className="native-card"><h3><ArrowRightLeft size={19}/>{t.status}</h3><p>{t.evidence}</p>
    <div className="native-report-list">{data.runtime.map(s => <article key={s.node_id + s.installation_id} className="native-report">
     <h4>{data.nodes.find(n => n.node_id === s.node_id)?.name ?? s.node_id}</h4><strong className={matching(s) ? "native-confirmed" : "native-pending"}>{statusLabel(s)}</strong>
     <div><span>{t.desired}: {s.desired_sequence || "—"}</span><span>{t.observed}: {s.reported_sequence || "—"}</span></div>
     <p>{t.phase}: {s.phase || "—"}</p><p>{t.last}: {s.reported_at ? new Date(s.reported_at).toLocaleTimeString(language === "fa" ? "fa-IR" : "en-GB") : "—"}</p>
    </article>)}</div>
   </div>
   <p className="native-footnote"><ShieldCheck size={16}/>{t.evidence}</p>
  </>}
  {notice && <p className="native-notice" role="status">{notice}</p>}
 </section>;
}
