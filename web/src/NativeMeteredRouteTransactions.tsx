import { useEffect, useRef, useState } from "react";
import { History, RefreshCw, Route } from "lucide-react";
import { api, ApiError, useSession } from "./session";
import { expected, readAttempt, readChannel, readCommand, readScope, type Action, type Attempt, type Channel, type CommandRequest, type Scope } from "./meteredRouteTransactions";
import "./NativeRouteTransactions.css";

const words = {
 en: {
  title: "Metered TCP LAB transactions", boundary: "Separately approved loopback Nodes with disposable PRO identities. Each Node keeps its own commands and usage epochs. Replacement requires an exact prior usage seal and a new epoch. No migration, UDP, Dark Swap forwarding or subscription publication.",
  disabled: "Route transactions are disabled on this HUB.", loading: "Checking the approved LAB Node…", unavailable: "Transaction status could not be verified. Controls are blocked.", refresh: "Refresh transactions",
  current: "Latest Agent observation", running: "Process reported ready · lease current", stopped: "Agent reports process not ready", expired: "Lease expired · current process state unconfirmed", stale: "Observation stale · current process state unconfirmed", empty: "Agent reports all managed listeners disabled", waiting: "Waiting for a signed Agent observation",
  revision: "Runtime revision", remaining: "Lease remaining", seconds: "s", report: "Last Agent observation", node: "Approved Node", review: "Complete Node draft review", draft: "Draft revision", inbound: "Inbound revision", direct: "Direct", tunnel: "Tunnel", missing: "Save a valid Direct/Tunnel draft for every enabled Inbound before Stage.", noDrafts: "No enabled Inbounds to stage.",
  stage: "Review and Stage", apply: "Apply staged configuration", rollback: "Rollback to saved configuration", renew: "Renew once (30s max)", abandon: "Abandon pending command", retry: "Retry same request",
  help: "Stage prepares only. Apply and Rollback require a separate confirmation. Viewing or refreshing this page never renews a lease.",
  accepted: "Command recorded by HUB. Check its receipt and the latest Agent observation separately.", conflict: "State changed or the request was refused. Review the refreshed state before creating another command.", unknown: "The response was not confirmed. This exact request is retained; refresh to reconcile it or retry the same request. No new command will be created automatically.", abandoned: "Command abandoned with execution unknown. This does not stop Xray; a fresh Agent observation is still required.",
  history: "Command history (latest 50)", none: "No commands recorded.", historical: "Receipts record what happened at that time. They do not prove the process is running now.", receipt: "Signed receipt verified and stored by HUB", noReceipt: "No committed receipt", outcome: "Recorded outcome", details: "Audit details", stageID: "Stage", ports: "Loopback ports", interrupted: "An interrupted command requires a new reviewed Stage before further execution.",
  confirmStage: "Stage all the reviewed Inbounds on this LAB Node? This prepares the configuration without starting it.",
  confirmRun: "Start or replace the managed Xray process on this loopback LAB Node? Existing LAB connections may close. The lease lasts at most 30 seconds and will not renew automatically.",
  confirmRenew: "Extend this LAB process lease once, for at most 30 seconds? No recurring renewal will be enabled.",
  confirmAbandon: "Abandon this pending command? It may already have executed. Abandoning does not stop Xray and does not prove non-execution.",
  confirmRetry: "Retry the exact retained request with the same ID and reviewed state? A committed request will be replayed without a new execution or lease extension.",
  states: { queued: "Queued · not yet delivered", delivered: "Delivered · awaiting committed receipt", acknowledged: "ACK recorded", failed: "Failure receipt recorded", acknowledged_stale: "ACK recorded for an outdated draft", abandoned_unknown: "Abandoned · execution unknown" },
  outcomes: { staged: "Prepared", applied: "Applied", rolled_back: "Rolled back", renewed: "Renewed once", failed_stopped: "Failed; stopped at receipt time", failed_previous_retained: "Failed; previous process retained at receipt time" },
 },
 fa: {
  title: "تراکنش‌های متری TCP در LAB", boundary: "فقط نود مجاز مستقل روی loopback و هویت‌های آزمایشی PRO. جایگزینی نیازمند تسویهٔ قطعی دورهٔ قبلی و دورهٔ مصرف تازه است. انتقال اطلاعات واقعی، UDP، عبور Dark Swap و انتشار اشتراک مجاز نیست.",
  disabled: "تراکنش‌های مسیر در این HUB فعال نشده‌اند.", loading: "در حال بررسی نود مجاز LAB…", unavailable: "وضعیت تراکنش تأیید نشد؛ کنترل‌ها مسدودند.", refresh: "به‌روزرسانی تراکنش‌ها",
  current: "آخرین مشاهدهٔ Agent", running: "آمادگی فرآیند گزارش شده · مجوز معتبر", stopped: "Agent آماده‌نبودن فرآیند را گزارش می‌کند", expired: "مجوز منقضی · وضعیت فعلی فرآیند نامشخص", stale: "گزارش قدیمی · وضعیت فعلی فرآیند نامشخص", empty: "Agent غیرفعال‌بودن همهٔ پورت‌های مدیریت‌شده را گزارش می‌کند", waiting: "در انتظار مشاهدهٔ امضاشدهٔ Agent",
  revision: "نسخهٔ اجرا", remaining: "زمان باقی‌ماندهٔ مجوز", seconds: "ثانیه", report: "آخرین مشاهدهٔ Agent", node: "نود مجاز", review: "بررسی کامل پیش‌نویس‌های نود", draft: "Revision پیش‌نویس", inbound: "Revision اینباند", direct: "Direct", tunnel: "Tunnel", missing: "پیش از Stage، برای همهٔ اینباندهای فعال یک پیش‌نویس معتبر Direct/Tunnel ذخیره کن.", noDrafts: "اینباند فعالی برای Stage وجود ندارد.",
  stage: "بررسی و Stage", apply: "Apply تنظیمات آماده‌شده", rollback: "Rollback به تنظیمات ذخیره‌شده", renew: "تمدید یک‌باره (حداکثر ۳۰ ثانیه)", abandon: "رهاکردن فرمان در انتظار", retry: "تلاش مجدد همان درخواست",
  help: "Stage فقط آماده‌سازی می‌کند. Apply و Rollback تأیید جداگانه می‌خواهند. بازکردن یا به‌روزرسانی صفحه هرگز مجوز اجرا را تمدید نمی‌کند.",
  accepted: "فرمان در HUB ثبت شد. رسید آن و آخرین مشاهدهٔ Agent را جداگانه بررسی کن.", conflict: "وضعیت تغییر کرده یا درخواست پذیرفته نشده است. پیش از فرمان جدید، وضعیت به‌روز را بررسی کن.", unknown: "پاسخ تأیید نشد. همین درخواست حفظ شده است؛ برای تطبیق وضعیت به‌روزرسانی کن یا همان درخواست را دوباره بفرست. فرمان جدیدی خودکار ساخته نمی‌شود.", abandoned: "فرمان با نتیجهٔ اجرای نامشخص رها شد. این کار Xray را متوقف نمی‌کند؛ هنوز مشاهدهٔ تازهٔ Agent لازم است.",
  history: "تاریخچهٔ فرمان‌ها (۵۰ مورد آخر)", none: "هنوز فرمانی ثبت نشده است.", historical: "رسیدها نتیجهٔ همان لحظه را ثبت می‌کنند؛ اثبات اجرای فعلی فرآیند نیستند.", receipt: "رسید امضاشده در HUB بررسی و ثبت شده", noReceipt: "رسید قطعی ثبت نشده", outcome: "نتیجهٔ ثبت‌شده", details: "جزئیات حسابرسی", stageID: "Stage", ports: "پورت‌های loopback", interrupted: "فرمان قطع‌شده نیازمند بررسی و Stage تازه است؛ سپس اجرای بعدی ممکن می‌شود.",
  confirmStage: "همهٔ اینباندهای بررسی‌شده روی این نود LAB آماده شوند؟ این مرحله تنظیمات را آماده می‌کند و آن‌ها را اجرا نمی‌کند.",
  confirmRun: "فرآیند Xray تحت مدیریت روی loopback همین نود LAB شروع یا جایگزین شود؟ اتصال‌های LAB ممکن است قطع شوند. مجوز حداکثر ۳۰ ثانیه است و خودکار تمدید نمی‌شود.",
  confirmRenew: "مجوز این فرآیند LAB فقط یک بار و حداکثر ۳۰ ثانیه تمدید شود؟ تمدید دوره‌ای فعال نخواهد شد.",
  confirmAbandon: "فرمان در انتظار رها شود؟ ممکن است قبلاً اجرا شده باشد. رهاکردن، Xray را متوقف نمی‌کند و اثبات اجرا‌نشدن نیست.",
  confirmRetry: "همین درخواست با همان شناسه و وضعیت بررسی‌شده دوباره فرستاده شود؟ اگر قبلاً ثبت شده باشد، بدون اجرای تازه یا تمدید مجوز بازخوانی می‌شود.",
  states: { queued: "در صف · هنوز تحویل نشده", delivered: "تحویل‌شده · در انتظار رسید قطعی", acknowledged: "ACK ثبت‌شده", failed: "رسید شکست ثبت‌شده", acknowledged_stale: "ACK ثبت‌شده برای پیش‌نویس قدیمی", abandoned_unknown: "رهاشده · نتیجهٔ اجرا نامشخص" },
  outcomes: { staged: "آماده‌شده", applied: "اعمال‌شده", rolled_back: "بازگردانده‌شده", renewed: "یک‌بار تمدیدشده", failed_stopped: "ناموفق؛ در لحظهٔ رسید متوقف", failed_previous_retained: "ناموفق؛ در لحظهٔ رسید فرآیند قبلی حفظ شده" },
 },
};
type Snapshot = { scope: Scope; view: Channel; mono: number; wall: number };
type Notice = "accepted" | "conflict" | "abandoned" | "";
const base = (node: string) => "/api/v1/workspace/native-nodes/" + encodeURIComponent(node) + "/metered-route-transactions";
const storageKey = (owner: string, scope: Scope) => "dark-pro-metered-route-attempt:" + JSON.stringify([owner, scope.node_id, scope.installation_id]);
const serverNow = (s: Snapshot) => s.view.server_time_unix_milli + Math.max(0, performance.now() - s.mono, Date.now() - s.wall);
function isFresh(s: Snapshot) {
 const now = serverNow(s), report = Math.floor(s.view.reported_at_unix_nano / 1e6);
 return s.view.observation_fresh && report > 0 && report <= now + 1000 && now - report < 12000 &&
  !s.view.commands.some(c => (c.receipt && c.receipt.observed_unix_nano > s.view.reported_at_unix_nano) || (c.state === "delivered" && c.serial > s.view.status.serial));
}

export default function NativeMeteredRouteTransactions({ language }: { language: "fa" | "en" }) {
 const t = words[language];
 const { session } = useSession();
 const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
 const [nodes,setNodes]=useState<{node_id:string;installation_id:string}[]>([]),[selected,setSelected]=useState("");
 const [availability, setAvailability] = useState<"loading" | "enabled" | "disabled" | "error">("loading");
 const [refresh, setRefresh] = useState(0), [, setTick] = useState(0);
 const [busy, setBusy] = useState(false), [verifiedRead, setVerifiedRead] = useState(false);
 const [attempt, setAttempt] = useState<Attempt | null>(null), [notice, setNotice] = useState<Notice>("");
 const inFlight = useRef(false), retained = useRef<Attempt | null>(null), activeRead = useRef<AbortController | null>(null);
 const lifetime = useRef<AbortController | null>(null);
 const owner = session?.user.id ?? "";
 function remember(a: Attempt | null, scope: Scope) {
  retained.current = a; setAttempt(a);
  try { const key = storageKey(owner, scope); if (a) sessionStorage.setItem(key, JSON.stringify(a)); else sessionStorage.removeItem(key); } catch { /* Memory still preserves this request while mounted. */ }
 }
 useEffect(() => {
  const ctrl = new AbortController(); lifetime.current = ctrl;
  const timer = setInterval(() => setTick(n => n + 1), 500);
  const visible = () => { setTick(n => n + 1); if (!document.hidden && !inFlight.current) setRefresh(n => n + 1); };
  document.addEventListener("visibilitychange", visible);
  return () => { ctrl.abort(); clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
 }, [owner]);
 useEffect(() => {
  if (!session || session.user.role !== "owner") return;
  const ctrl = new AbortController(); activeRead.current = ctrl;
  let timer: ReturnType<typeof setTimeout>;
  setVerifiedRead(false);
  async function poll() {
   try {
    let scope: Scope;
    try { scope = readScope(await api<unknown>("/api/v1/workspace/metered-route-transactions", { signal: ctrl.signal })); }
    catch (e) { if (e instanceof ApiError && e.status === 404) scope = { enabled: false, scope: "metered_tcp_loopback_lab" }; else throw e; }
    if (ctrl.signal.aborted) return;
    if (!scope.enabled) { setAvailability("disabled"); setSnapshot(null); setNodes([]); setVerifiedRead(false); return; }
    const choices=scope.nodes??[{node_id:scope.node_id!,installation_id:scope.installation_id!}];
    setNodes(choices);
    const chosen=choices.find(node=>node.node_id===(selected||scope.node_id));
    if(!chosen)throw Error("selected_node_no_longer_approved");
    scope={...scope,...chosen};
    // Charge the entire request time against freshness and lease lifetimes.
    const mono = performance.now(), wall = Date.now();
    const view = readChannel(await api<unknown>(base(scope.node_id!), { signal: ctrl.signal }), scope);
    if (ctrl.signal.aborted) return;
    setSnapshot({ scope, view, mono, wall }); setAvailability("enabled"); setVerifiedRead(true);
    let a = retained.current;
    if (!a || a.node_id !== scope.node_id || a.installation_id !== scope.installation_id) {
     try { a = readAttempt(sessionStorage.getItem(storageKey(owner, scope)), scope); } catch { a = null; }
     retained.current = a; setAttempt(a);
    }
    if (a) {
     const found = view.commands.find(c => c.id === a.id);
     if (found && (a.action === "abandon" ? found.state === "abandoned_unknown" : found.action === a.action)) {
      remember(null, scope); setNotice(a.action === "abandon" ? "abandoned" : "accepted");
     }
    }
   } catch { if (!ctrl.signal.aborted) { setAvailability("error"); setVerifiedRead(false); } }
   finally { if (!ctrl.signal.aborted) timer = setTimeout(() => void poll(), 3000); }
  }
  void poll();
  return () => { ctrl.abort(); clearTimeout(timer); };
 }, [session, owner, refresh, selected]);

 async function send(a: Attempt, s: Snapshot) {
  const ctrl = lifetime.current;
  if (inFlight.current || !ctrl || ctrl.signal.aborted || !session?.csrf_token) return;
  inFlight.current = true; setBusy(true); setVerifiedRead(false); setNotice(""); activeRead.current?.abort();
  remember(a, s.scope); // Persist the exact body before a request can leave the browser.
  try {
   const value = await api<unknown>(base(a.node_id) + (a.action === "abandon" ? "/" + a.id + "/abandon" : ""), {
    method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token }, body: a.body, signal: ctrl.signal,
   });
   if (ctrl.signal.aborted) return;
   if (a.action === "abandon") {
    const v = value as { state?: string; process_stopped?: boolean; requires_fresh_observation?: boolean } | null;
    if (!v || v.state !== "abandoned_unknown" || v.process_stopped !== false || v.requires_fresh_observation !== true) throw Error("invalid_abandon_receipt");
   } else {
    const c = readCommand(value, s.scope);
    const body = JSON.parse(a.body) as CommandRequest;
    if (c.id !== a.id || c.action !== a.action || c.stage_id !== (a.action === "stage" ? a.id : body.stage_id)) throw Error("mismatched_command_receipt");
   }
   remember(null, s.scope); setNotice(a.action === "abandon" ? "abandoned" : "accepted");
  } catch (e) {
   if (!ctrl.signal.aborted && e instanceof ApiError && [400, 401, 403, 404, 409, 422].includes(e.status)) { remember(null, s.scope); setNotice("conflict"); }
   // Timeout, malformed success and 5xx may follow a durable commit. Retain ID/body.
  } finally {
   inFlight.current = false;
   if (!ctrl.signal.aborted) { setBusy(false); setRefresh(n => n + 1); }
  }
 }
 const s = snapshot, v = s?.view, observed = v?.status.observed;
 const fresh = !!s && verifiedRead && availability === "enabled" && isFresh(s);
 const leaseRemaining = s && observed ? Math.max(0, Math.floor(observed.lease_expires_unix_nano / 1e6) - serverNow(s)) : 0;
 const pending = v?.commands.find(c => c.state === "queued" || c.state === "delivered");
 const idle = !!s && verifiedRead && availability === "enabled" && !busy && !attempt && !!session?.csrf_token;
 function allowed(action: Action) {
  if (!s || !fresh || !idle || pending) return false;
  if (action === "stage") return s.view.controls.stage;
  const target = s.view.controls[action];
  if (!target) return false;
  if (action === "apply") return target.expires_unix_milli! > serverNow(s);
  if (action === "renew") return s.view.status.observed.process_ready && leaseRemaining > 0;
  return true;
 }
 function act(action: Action) {
  if (!s || !s.view.status.usage || !allowed(action)) return;
  const target = action === "stage" ? undefined : s.view.controls[action];
  const ports = action === "stage" ? s.view.review.flatMap(r => [r.direct_port, r.tunnel_port]).filter(Boolean) : target!.ports;
  const prompt = action === "stage" ? t.confirmStage : action === "renew" ? t.confirmRenew : t.confirmRun;
  if (!window.confirm(`${t[action]}\n${prompt}\n${t.node}: ${s.view.node_id}\n${t.ports}: ${ports.join(", ") || "—"}${target ? "\nStage: " + target.stage_id : ""}`) || !isFresh(s) || (action === "apply" && target!.expires_unix_milli! <= serverNow(s)) || (action === "renew" && s.view.status.observed.lease_expires_unix_nano / 1e6 <= serverNow(s))) return;
  const body: CommandRequest = { usage: s.view.status.usage!, id: crypto.randomUUID().replaceAll("-", ""), action, stage_id: target?.stage_id ?? "", runtime_instance: s.view.status.instance, expected: expected(s.view.status.observed),
   drafts: action === "stage" ? s.view.review.map(r => ({ inbound_id: r.inbound_id, inbound_revision: r.inbound_revision, draft_revision: r.draft_revision })) : [] };
  void send({ node_id: s.view.node_id, installation_id: s.view.installation_id, action, id: body.id, body: JSON.stringify(body) }, s);
 }
 function abandon() {
  if (!s || !pending || !idle || !window.confirm(t.confirmAbandon + "\n" + s.view.node_id + "\n" + pending.id)) return;
  void send({ node_id: s.view.node_id, installation_id: s.view.installation_id, action: "abandon", id: pending.id, body: JSON.stringify({ confirm_execution_may_have_occurred: true }) }, s);
 }
 function retry() {
  if (!s || !attempt || busy || !verifiedRead || availability !== "enabled" || attempt.node_id !== s.view.node_id || attempt.installation_id !== s.view.installation_id || !window.confirm(t.confirmRetry + "\n" + t[attempt.action] + "\n" + attempt.node_id + "\n" + attempt.id)) return;
  void send(attempt, s);
 }
 const time = (nano: number) => nano > 0 ? new Date(nano / 1e6).toLocaleString(language === "fa" ? "fa-IR" : "en-GB") : "—";
 const current = !v || !v.reported_at_unix_nano ? t.waiting : !fresh ? t.stale : !observed!.process_ready ? t.stopped : leaseRemaining <= 0 ? t.expired : observed!.no_listeners ? t.empty : t.running;
 return <section className="native-card route-transactions" aria-label="Metered TCP LAB transactions">
  <div className="route-transactions-heading"><h3><Route size={19}/>{t.title}</h3><button type="button" disabled={busy} onClick={() => setRefresh(n => n + 1)}><RefreshCw size={15}/>{t.refresh}</button></div>
  <p>{t.boundary}</p>
  {nodes.length>1&&<label className="metered-route-node-picker">{t.node}<select aria-label={t.node} disabled={busy} value={selected||s?.scope.node_id||nodes[0].node_id} onChange={event=>{
   if(inFlight.current)return;
   activeRead.current?.abort();setVerifiedRead(false);setSnapshot(null);retained.current=null;setAttempt(null);setNotice("");setAvailability("loading");setSelected(event.target.value);
  }}>{nodes.map(node=><option key={node.node_id} value={node.node_id}>{node.node_id}</option>)}</select></label>}
  {availability === "loading" && <p>{t.loading}</p>}
  {availability === "disabled" && <p>{t.disabled}</p>}
  {availability === "error" && <p className="native-warning" role="alert">{t.unavailable}</p>}
  {v && <>
   <div className="route-live" aria-label="Latest route observation">
    <h4>{t.current}</h4><span>{t.node}: <bdi>{v.node_id}</bdi></span>
    <strong className={fresh && observed!.process_ready && leaseRemaining > 0 ? "native-confirmed" : "native-pending"}>{current}</strong>
    <span>{t.revision}: {observed!.active_sequence || "—"}</span><span>{t.report}: {time(v.reported_at_unix_nano)}</span>
    {fresh && observed!.process_ready && leaseRemaining > 0 && <span>{t.remaining}: {Math.ceil(leaseRemaining / 1000)} {t.seconds}</span>}
   </div>
   {v.status.usage && <div className="route-live" aria-label="Current metered epoch"><h4>{language === "fa" ? "شواهد دورهٔ مصرف" : "Usage epoch evidence"}</h4><span>Epoch: <bdi>{v.status.usage.epoch_id || "—"}</bdi></span><span>Epoch SHA: <bdi>{v.status.usage.epoch_sha || "—"}</bdi></span><span>{language === "fa" ? "رسید تسویه" : "Settlement"}: <bdi>{v.status.usage.settlement.sequence === "0" ? "—" : v.status.usage.settlement.sha256}</bdi></span></div>}
   <div><h4>{t.review}</h4><div className="route-draft-review">
    {v.review.map(r => <article key={r.inbound_id}><strong>{r.name} · #{r.inbound_id}</strong><span>{t.inbound}: {r.inbound_revision} · {t.draft}: {r.draft_revision || "—"}</span><span><bdi>{r.listen_ip}</bdi> · {t.direct}: {r.direct_port || "—"} · {t.tunnel}: {r.tunnel_port || "—"}</span></article>)}
    {!v.review.length && <p>{t.noDrafts}</p>}
   </div></div>
   {!v.drafts_valid && <p className="native-warning">{t.missing}</p>}
   {v.status.interrupted_command_sha && <p className="native-warning">{t.interrupted}</p>}
   <p>{t.help}</p>
   <div className="native-actions">{(["stage", "apply", "rollback", "renew"] as const).map(action => <button key={action} type="button" disabled={!allowed(action)} onClick={() => act(action)}>{t[action]}</button>)}</div>
   {(["apply", "rollback", "renew"] as const).filter(a => v.controls[a]).map(a => <small key={a}>{t[a]} · {t.stageID}: <bdi>{v.controls[a]!.stage_id}</bdi> · {t.ports}: <bdi>{v.controls[a]!.ports.join(", ") || "—"}</bdi></small>)}
   {pending && <button type="button" disabled={!idle} onClick={abandon}>{t.abandon}</button>}
   {attempt && <div className="native-warning" role="status"><p>{t.unknown}</p><p>{t[attempt.action]} · <bdi>{attempt.id}</bdi></p><button type="button" disabled={busy || !verifiedRead || availability !== "enabled"} onClick={retry}>{t.retry}</button></div>}
   <div className="route-command-history"><h4><History size={16}/>{t.history}</h4><p>{t.historical}</p>
    {!v.commands.length && <p>{t.none}</p>}
    {v.commands.map(c => <article key={c.id} aria-label={"Route command " + c.id}>
     <strong>{c.action.toUpperCase()} · #{c.serial}</strong><span>{t.states[c.state]}</span>
     <span>{c.receipt ? t.receipt : t.noReceipt}</span>
     {c.registered_epoch_id && <span>{language === "fa" ? "دورهٔ ثبت‌شده در HUB؛ نه اثبات بازبودن پورت" : "HUB registered epoch · not proof of listener admission"}: <bdi>{c.registered_epoch_id}</bdi></span>}
     {c.receipt && <p>{t.outcome}: {t.outcomes[c.receipt.outcome as keyof typeof t.outcomes]} · {time(c.receipt.observed_unix_nano)}</p>}
     {c.receipt && <div className="route-live" aria-label="Verified metered evidence"><span>Epoch: <bdi>{c.receipt.metered.after.epoch_id || "—"}</bdi></span><span>{language === "fa" ? "دورهٔ قبلیِ تسویه‌شده" : "Prior sealed epoch"}: <bdi>{c.receipt.metered.prior_settlement.epoch_id || "—"}</bdi></span><span>{language === "fa" ? "شمارهٔ رسید نهایی" : "Final sequence"}: <bdi>{c.receipt.metered.prior_settlement.sequence}</bdi></span><span>Final SHA: <bdi>{c.receipt.metered.prior_settlement.sha256 || "—"}</bdi></span></div>}
     <details><summary>{t.details}</summary><span>ID: <bdi>{c.id}</bdi></span><span>Stage: <bdi>{c.stage_id}</bdi></span><span>{time(c.created_unix_nano)}</span>{c.receipt && <span>Agent: <bdi>{c.receipt.runtime_instance}</bdi></span>}</details>
    </article>)}
   </div>
  </>}
  {notice && <p className="native-notice" role="status">{t[notice]}</p>}
 </section>;
}
