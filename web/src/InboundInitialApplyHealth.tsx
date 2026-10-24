import { useEffect, useRef, useState } from "react";
import { api, ApiError, useSession, type Language } from "./session";
import type { PromotionReceipt } from "./inboundPlanExecution";
import {
 initialHealthBinding, initialHealthPath, readInitialHealthAttempt, readInitialHealthPreparation, readInitialHealthRound,
 readInitialHealthSnapshot, readInitialHealthState, sameInitialBinding,
 type InitialHealthAttempt, type InitialHealthRound, type InitialHealthState,
} from "./inboundInitialApplyHealth";
import {
 readSubscription, readSubscriptionSecret, readTokenAttempt, readTokenReceipt, subscriptionEventID, subscriptionPath,
 type InitialLabSnapshot, type SubscriptionView, type TokenAttempt,
} from "./labSubscriptions";
import { useServerClock } from "./useServerClock";
import "./LabSubscriptions.css";
import "./InboundInitialApplyHealth.css";

const words = {
 en: {
  title: "Verify the first LAB execution", scope: "Check this prepared Inbound's first Apply, then record one bound client's local LAB output with the same short health deadline.",
  disabled: "First-Apply health and delivery are disabled on this HUB.", loading: "Checking the prepared execution…", failed: "The execution or retained request could not be verified. Refresh before a new action.",
  refresh: "Refresh first-Apply health", prepare: "Prepare the protocol probe", prepareHelp: "Prepare the nonbillable test identity before Stage. Then Stage and Apply with the pinned execution controls above.",
  prepareConfirm: "Prepare a nonbillable protocol-test identity for this exact promoted Inbound before its first Stage? This does not start Xray or issue subscription output.", prepared: "Protocol-test identity prepared", awaiting: "Awaiting the reviewed first Apply", ready: "First Apply is ready for a protocol test", blocked: "This execution is not eligible for a new test.",
  client: "Bound subscription client", tokenAbsent: "No subscription token issued", tokenActive: "Subscription token active", tokenRevoked: "Subscription token revoked", tokenDisabled: "Subscription access is unavailable.", tokenLoading: "Checking the selected client's token and revisions…", tokenFailed: "The selected client's subscription state could not be verified.",
  issue: "Issue subscription token", replace: "Replace revoked subscription token", issueConfirm: "Issue subscription access for this selected LAB client? A health-bound snapshot is still required before output can be fetched.",
  selectHelp: "Choose the client and prepare its token before the test. Recording output replaces that client's previous LAB delivery and keeps the original health deadline.",
  test: "Review and test the connection", testConfirm: "Test the real protocol connection for this exact first Apply and selected client? The proof lasts at most eight seconds. Recording output remains a separate explicit action.",
  pending: "Protocol test queued · proof pending", verified: "Protocol proof accepted · original deadline applies", expired: "Original proof deadline expired", changed: "Execution changed · previous proof is historical", unconfirmed: "Historical proof · current execution is not confirmed", seconds: "seconds remaining", deadline: "Original deadline (Tehran)",
  record: "Record this client's LAB output", recorded: "LAB snapshot recorded", current: "LAB delivery checked within the original proof window", historical: "Snapshot retained · current delivery is not confirmed", snapshotHelp: "Record output only while this proof and the reviewed client revisions remain current.",
  retry: "Retry the exact retained request", retryToken: "Retry the exact token request", discard: "Discard local retry", discardConfirm: "Discard only this local retry? This does not undo an operation already recorded by the HUB.", unknown: "The response was not confirmed. The exact request is retained for explicit retry or readback.", conflict: "The retained request does not match the current reviewed state. Its result remains unconfirmed.", storage: "The exact request could not be saved in this tab. No new write was sent.",
  reveal: "Show subscription URL", hide: "Hide URL", copy: "Copy URL", copied: "URL copied", fetch: "Open checked LAB output", secret: "The URL grants subscription access. It is hidden when this section closes, the tab is hidden, or after one minute.", url: "Subscription URL", fetchHelp: "Every fetch rechecks the selected delivery, proof, client and original deadline.", details: "Reviewed identity and audit details", node: "Node", port: "Loopback listener", revisions: "Inbound / draft revision", noProof: "Process readiness alone does not verify the protocol connection.",
 },
 fa: {
  title: "بررسی اولین اجرای LAB", scope: "اولین Apply همین اینباند آماده‌شده را بررسی کنید؛ سپس خروجی محلی یک کلاینت متصل را با همان مهلت کوتاه سلامت ثبت کنید.",
  disabled: "سلامت اولین Apply و تحویل خروجی در این HUB فعال نیست.", loading: "در حال بررسی اجرای آماده‌شده…", failed: "اجرا یا درخواست ذخیره‌شده تأیید نشد؛ پیش از اقدام تازه به‌روزرسانی کنید.",
  refresh: "به‌روزرسانی سلامت اولین Apply", prepare: "آماده‌سازی آزمون پروتکل", prepareHelp: "هویت آزمون بدون محاسبهٔ مصرف را پیش از Stage آماده کنید؛ سپس از کنترل‌های اجرای همین نود در بالا، Stage و Apply کنید.",
  prepareConfirm: "هویت آزمون پروتکل بدون محاسبهٔ مصرف برای همین اینباند آماده‌شده و پیش از اولین Stage ساخته شود؟ این اقدام Xray را اجرا نمی‌کند و خروجی اشتراک صادر نمی‌کند.", prepared: "هویت آزمون پروتکل آماده شد", awaiting: "در انتظار اولین Apply بررسی‌شده", ready: "اولین Apply برای آزمون پروتکل آماده است", blocked: "این اجرا برای آزمون تازه شرایط لازم را ندارد.",
  client: "کلاینت متصل برای اشتراک", tokenAbsent: "هنوز توکن اشتراک صادر نشده", tokenActive: "توکن دریافت اشتراک فعال است", tokenRevoked: "توکن دریافت اشتراک لغو شده", tokenDisabled: "دسترسی اشتراک در دسترس نیست.", tokenLoading: "در حال بررسی توکن و نسخه‌های کلاینت انتخاب‌شده…", tokenFailed: "وضعیت اشتراک کلاینت انتخاب‌شده تأیید نشد.",
  issue: "صدور توکن اشتراک", replace: "جایگزینی توکن لغوشده", issueConfirm: "دسترسی اشتراک برای همین کلاینت LAB صادر شود؟ دریافت خروجی همچنان به نسخهٔ ثبت‌شده با اثبات سلامت نیاز دارد.",
  selectHelp: "پیش از آزمون، کلاینت را انتخاب و توکنش را آماده کنید. ثبت خروجی، تحویل قبلی LAB همان کلاینت را جایگزین می‌کند و مهلت اولیهٔ سلامت را حفظ می‌کند.",
  test: "بررسی و آزمون اتصال", testConfirm: "اتصال واقعی پروتکل برای همین اولین Apply و کلاینت انتخاب‌شده آزموده شود؟ اثبات حداکثر هشت ثانیه اعتبار دارد. ثبت خروجی همچنان اقدام جداگانهٔ شماست.",
  pending: "آزمون پروتکل در صف است؛ در انتظار اثبات", verified: "اثبات پروتکل پذیرفته شد؛ همان مهلت اولیه برقرار است", expired: "مهلت اولیهٔ اثبات تمام شد", changed: "اجرا تغییر کرده؛ اثبات قبلی تاریخی است", unconfirmed: "اثبات تاریخی؛ اجرای فعلی تأیید نیست", seconds: "ثانیه باقی‌مانده", deadline: "پایان اعتبار اولیه (تهران)",
  record: "ثبت خروجی LAB همین کلاینت", recorded: "نسخهٔ اشتراک LAB ثبت شد", current: "دریافت LAB در بازهٔ اولیهٔ اثبات بررسی شد", historical: "نسخه حفظ شده؛ دریافت فعلی تأیید نیست", snapshotHelp: "خروجی را فقط در مهلت همین اثبات و با نسخه‌های بررسی‌شدهٔ کلاینت ثبت کنید.",
  retry: "تکرار همان درخواست ذخیره‌شده", retryToken: "تکرار همان درخواست توکن", discard: "حذف تکرار محلی", discardConfirm: "فقط تکرار محلی حذف شود؟ عملیاتی که قبلاً در HUB ثبت شده برگردانده نمی‌شود.", unknown: "پاسخ تأیید نشد؛ همان درخواست برای تکرار صریح یا تطبیق نتیجه حفظ شده است.", conflict: "درخواست ذخیره‌شده با وضعیت بررسی‌شدهٔ فعلی مطابقت ندارد؛ نتیجهٔ آن هنوز تأیید نیست.", storage: "ذخیرهٔ درخواست دقیق در این تب ممکن نشد؛ درخواست نوشتن تازه ارسال نشد.",
  reveal: "نمایش لینک اشتراک", hide: "پنهان‌کردن لینک", copy: "کپی لینک", copied: "لینک کپی شد", fetch: "بازکردن خروجی بررسی‌شدهٔ LAB", secret: "این لینک مجوز دریافت اشتراک است؛ با بستن این بخش، پنهان‌شدن تب یا پس از یک دقیقه پنهان می‌شود.", url: "لینک اشتراک", fetchHelp: "هر دریافت، تحویل انتخاب‌شده، اثبات، کلاینت و مهلت اولیه را دوباره بررسی می‌کند.", details: "هویت بررسی‌شده و جزئیات حسابرسی", node: "نود", port: "پورت loopback", revisions: "نسخهٔ اینباند / پیش‌نویس", noProof: "آمادگی فرآیند به‌تنهایی اثبات اتصال پروتکل نیست.",
 },
};
type RoundReview = Extract<InitialHealthAttempt, { action: "round" }>;
type TokenRetry = { client_id: string; request: TokenAttempt };
type CheckedState = { view: InitialHealthState; mono: number; wall: number };
const date = (value: number, language: Language) => new Intl.DateTimeFormat(language === "fa" ? "fa-IR" : "en-GB", { timeZone: "Asia/Tehran", dateStyle: "short", timeStyle: "medium" }).format(value / 1e6);

export default function InboundInitialApplyHealth({ language, promotion, active = true }: { language: Language; promotion: PromotionReceipt; active?: boolean }) {
 const t = words[language], { session } = useSession(), owner = session?.user.role === "owner";
 const base = initialHealthPath(promotion.inbound_id), storageKey = `dark-pro-initial-health:${session?.user.id ?? ""}:${promotion.inbound_id}:${promotion.event_id}`;
 const [checked, setChecked] = useState<CheckedState | null>(null), [phase, setPhase] = useState("loading"), [refresh, setRefresh] = useState(0), [, tick] = useState(0);
 const [clockSample, setClockSample] = useState(0), [round, setRound] = useState<InitialHealthRound | null>(null), [review, setReview] = useState<RoundReview | null>(null);
 const [attempt, setAttempt] = useState<InitialHealthAttempt | null>(null), [storageFailed, setStorageFailed] = useState(false), [busy, setBusy] = useState(false), [notice, setNotice] = useState("");
 const [clientID, setClientID] = useState(promotion.client_ids[0] ?? ""), [subscription, setSubscription] = useState<SubscriptionView | null>(null), [clientPhase, setClientPhase] = useState("loading"), [clientRefresh, setClientRefresh] = useState(0);
 const [tokenRetry, setTokenRetry] = useState<TokenRetry | null>(null), [result, setResult] = useState<InitialLabSnapshot | null>(null), [secret, setSecret] = useState(""), [copied, setCopied] = useState(false);
 const lifetime = useRef<AbortController | null>(null), sending = useRef(false), retained = useRef<InitialHealthAttempt | null>(null), reviewRef = useRef<RoundReview | null>(null), tokenRetained = useRef<TokenRetry | null>(null), secretEpoch = useRef(0);
 const secretIdentity = useRef<{ clientID: string; generation: number } | null>(null);
 const now = useServerClock(clockSample), view = checked?.view;
 const latest = useRef({ checked, phase, clientID, subscription, clientPhase, round, review, active });
 latest.current = { checked, phase, clientID, subscription, clientPhase, round, review, active };
 function observeClock(serverTime: number | undefined, started: number, wall: number) {
  if (serverTime) setClockSample(serverTime + Math.max(0, performance.now() - started, Date.now() - wall));
 }
 function save(key: string, value: unknown): boolean {
  try { if (value === null) sessionStorage.removeItem(key); else sessionStorage.setItem(key, JSON.stringify(value)); setStorageFailed(false); return true; }
  catch { setStorageFailed(true); return false; }
 }
 function remember(value: InitialHealthAttempt | null) {
  if (!save(storageKey, value)) return false;
  retained.current = value; setAttempt(value); return true;
 }
 function rememberReview(value: RoundReview | null) {
  if (!save(storageKey + ":review", value)) return false;
  reviewRef.current = value; setReview(value); return true;
 }
 function rememberToken(value: TokenRetry | null) {
  if (!save(storageKey + ":token", value)) return false;
  tokenRetained.current = value; setTokenRetry(value); return true;
 }
 function hideSecret() { secretEpoch.current++; secretIdentity.current = null; setSecret(""); setCopied(false); }
 function retryAvailable() {
  const s = latest.current;
  return owner && s.active && s.phase === "ready" && s.checked?.view.enabled === true && s.checked.view.available && !sending.current && !storageFailed && !!session?.csrf_token;
 }
 function liveExecution() {
  const s = latest.current, c = s.checked;
  return !!c && s.active && s.phase === "ready" && c.view.enabled && c.view.available && c.view.eligible && Math.max(0, performance.now() - c.mono, Date.now() - c.wall) < 12000;
 }
 function proofCurrent(r: InitialHealthRound | null = latest.current.round) {
  const s = latest.current;
  return !!r && liveExecution() && s.checked?.view.enabled === true && r.current && r.state === "complete" && sameInitialBinding(r, s.checked.view) && r.members[0].port === s.checked.view.direct_port && r.expires_unix_nano / 1e6 > now();
 }
 function acceptRound(value: InitialHealthRound) {
  const a = retained.current;
  if (a?.action === "round" && value.id === a.body.id) {
   readInitialHealthRound(value, promotion, { id: a.body.id, binding: a.binding });
   if (!reviewRef.current && !rememberReview(a)) return;
   remember(null);
  }
  setRound(value);
 }
 function acceptSnapshot(value: unknown, a: Extract<InitialHealthAttempt, { action: "snapshot" }>) {
  const snapshot = readInitialHealthSnapshot(value, promotion, a);
  setResult(snapshot); remember(null); setClientRefresh(n => n + 1); return snapshot;
 }
 useEffect(() => {
  const c = new AbortController(); lifetime.current = c; sending.current = false; setBusy(false); hideSecret(); setNotice("");
  retained.current = null; reviewRef.current = null; tokenRetained.current = null; setAttempt(null); setReview(null); setTokenRetry(null); setRound(null); setResult(null); setStorageFailed(false);
  try {
   const a = readInitialHealthAttempt(sessionStorage.getItem(storageKey), promotion);
   const r = readInitialHealthAttempt(sessionStorage.getItem(storageKey + ":review"), promotion);
   if (r && r.action !== "round") throw Error("invalid_round_review");
   const rawToken = sessionStorage.getItem(storageKey + ":token");
   let token: TokenRetry | null = null;
   if (rawToken) {
    const parsed = JSON.parse(rawToken), request = readTokenAttempt(JSON.stringify(parsed.request));
    if (!parsed || Object.keys(parsed).sort().join(",") !== "client_id,request" || !promotion.client_ids.includes(parsed.client_id) || !request || request.action === "revoke") throw Error("invalid_token_retry");
    token = { client_id: parsed.client_id, request };
   }
   if (a?.action === "round" && r && (a.body.id !== r.body.id || a.client_id !== r.client_id || !sameInitialBinding(a.binding, r.binding))) throw Error("mismatched_round_review");
   retained.current = a; setAttempt(a); reviewRef.current = r; setReview(r); tokenRetained.current = token; setTokenRetry(token);
   setClientID(token?.client_id ?? (a?.action === "snapshot" ? a.body.client_id : a?.action === "round" ? a.client_id : r?.client_id) ?? promotion.client_ids[0] ?? "");
  } catch { setStorageFailed(true); }
  return () => c.abort();
 }, [storageKey, session?.csrf_token]);
 useEffect(() => {
  const timer = setInterval(() => tick(n => n + 1), 250);
  const visible = () => { hideSecret(); if (!document.hidden && active) { setRefresh(n => n + 1); setClientRefresh(n => n + 1); } };
  document.addEventListener("visibilitychange", visible);
  return () => { clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
 }, [active]);
 useEffect(() => { if (!active) hideSecret(); }, [active]);
 useEffect(hideSecret, [clientID, storageKey]);
 useEffect(() => { if (!secret) return; const timer = setTimeout(hideSecret, 60000); return () => clearTimeout(timer); }, [secret]);
 useEffect(() => {
  if (!owner || !active) return;
  const c = new AbortController(); let timer: ReturnType<typeof setTimeout>; setPhase("loading");
  async function read() {
   const started = performance.now(), wall = Date.now();
   try {
    const next = readInitialHealthState(await api<unknown>(base, { signal: c.signal }), promotion);
    if (c.signal.aborted) return;
    observeClock(next.server_time_unix_milli, started, wall); setChecked({ view: next, mono: started, wall }); setPhase(next.enabled ? "ready" : "disabled");
    if (next.enabled) {
     const a = retained.current;
     if (a?.action === "prepare" && next.preparation) {
      readInitialHealthPreparation(next.preparation, promotion, a.body); remember(null);
     }
     const wanted = reviewRef.current?.body.id ?? (a?.action === "round" ? a.body.id : undefined);
     if (next.latest_round && (!wanted || next.latest_round.id === wanted)) acceptRound(next.latest_round);
    }
   } catch (error) { if (!c.signal.aborted) { setPhase(error instanceof ApiError && error.status === 404 ? "disabled" : "error"); if (retained.current) setNotice(t.conflict); } }
   finally { if (!c.signal.aborted) timer = setTimeout(() => void read(), 3000); }
  }
  void read(); return () => { c.abort(); clearTimeout(timer); };
 }, [base, promotion.event_id, owner, active, refresh, session?.csrf_token]);
 const watchedRoundID = review?.body.id ?? (attempt?.action === "round" ? attempt.body.id : "");
 useEffect(() => {
  if (!owner || !active || !watchedRoundID || phase !== "ready") return;
  const c = new AbortController(); let timer: ReturnType<typeof setTimeout>;
  async function read() {
   const started = performance.now(), wall = Date.now();
   try {
    const r = reviewRef.current ?? (retained.current?.action === "round" ? retained.current : null);
    if (!r || r.body.id !== watchedRoundID) return;
    const next = readInitialHealthRound(await api<unknown>(base + "/rounds/" + watchedRoundID, { signal: c.signal }), promotion, { id: r.body.id, binding: r.binding });
    if (c.signal.aborted) return;
    observeClock(next.server_time_unix_milli, started, wall); acceptRound(next);
    const estimated = next.server_time_unix_milli ? next.server_time_unix_milli + Math.max(0, performance.now() - started, Date.now() - wall) : now();
    if (next.state === "pending" && next.expires_unix_nano / 1e6 > estimated) timer = setTimeout(() => void read(), Math.min(500, Math.max(1, next.expires_unix_nano / 1e6 - estimated)));
   } catch { if (!c.signal.aborted && retained.current?.action === "round") setNotice(t.unknown); }
  }
  void read(); return () => { c.abort(); clearTimeout(timer); };
 }, [base, promotion.event_id, owner, active, watchedRoundID, phase, refresh]);
 useEffect(() => {
  setSubscription(null); setClientPhase("loading");
  if (!owner || !active || !view?.enabled || !view.available || !promotion.client_ids.includes(clientID)) return;
  const c = new AbortController(); let timer: ReturnType<typeof setTimeout>;
  async function read() {
   try {
    const next = readSubscription(await api<unknown>(subscriptionPath(clientID), { signal: c.signal }), clientID);
    if (c.signal.aborted) return;
    setSubscription(next); setClientPhase("ready");
    const a = retained.current;
    if (a?.action === "snapshot" && a.body.client_id === clientID && next.enabled && next.snapshot?.event_id === a.body.event_id) acceptSnapshot(next.snapshot, a);
    else if (next.enabled && next.snapshot?.scope === "lab_initial_apply" && next.snapshot.inbound_id === promotion.inbound_id && next.snapshot.promotion_event_id === promotion.event_id) setResult(readInitialHealthSnapshot(next.snapshot, promotion));
    const identity = secretIdentity.current;
    if (identity && (identity.clientID !== clientID || !next.enabled || next.state !== "active" || identity.generation !== next.generation)) hideSecret();
   } catch { if (!c.signal.aborted) setClientPhase("error"); }
   finally { if (!c.signal.aborted) timer = setTimeout(() => void read(), 3000); }
  }
  void read(); return () => { c.abort(); clearTimeout(timer); };
 }, [owner, active, clientID, clientRefresh, storageKey, view?.enabled === true && view.available, session?.csrf_token]);

 async function send(a: InitialHealthAttempt) {
  const c = lifetime.current;
  if (!retryAvailable() || !c || c.signal.aborted || tokenRetained.current) return;
  if (a.action === "round" && !rememberReview(a) || !remember(a)) return;
  sending.current = true; setBusy(true); setNotice("");
  const started = performance.now(), wall = Date.now();
  try {
   const raw = await api<unknown>(base + (a.action === "prepare" ? "/prepare" : a.action === "round" ? "/rounds" : "/snapshot"), { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": session!.csrf_token }, body: JSON.stringify(a.body), signal: c.signal });
   if (c.signal.aborted) return;
   if (a.action === "prepare") { const receipt = readInitialHealthPreparation(raw, promotion, a.body); observeClock(receipt.server_time_unix_milli, started, wall); remember(null); setRefresh(n => n + 1); }
   else if (a.action === "round") { const receipt = readInitialHealthRound(raw, promotion, { id: a.body.id, binding: a.binding }); observeClock(receipt.server_time_unix_milli, started, wall); acceptRound(receipt); setRefresh(n => n + 1); }
   else { const receipt = acceptSnapshot(raw, a); observeClock(receipt.server_time_unix_milli, started, wall); }
  } catch { if (!c.signal.aborted) setNotice(t.unknown); }
  finally { if (lifetime.current === c && !c.signal.aborted) { sending.current = false; setBusy(false); } }
 }
 const locked = busy || !!attempt || !!tokenRetry;
 const tokenReady = clientPhase === "ready" && subscription?.enabled === true && subscription.scope === "lab_initial_apply" && subscription.state === "active" && subscription.delivery_revision !== undefined && subscription.initial_apply_revision !== undefined;
 const roundLive = !!round && round.expires_unix_nano / 1e6 > now() && round.state !== "expired" && round.state !== "state_changed";
 const idle = retryAvailable() && !locked;
 const canProbe = idle && liveExecution() && tokenReady && !roundLive;
 const selectedReview = review && round?.id === review.body.id && clientID === review.client_id && subscription?.enabled === true && subscription.state === "active" && subscription.generation === review.token_generation && subscription.initial_apply_revision === review.expected_revision && subscription.delivery_revision === review.expected_delivery_revision;
 const canRecord = idle && proofCurrent() && !!selectedReview && result?.round_id !== round?.id;
 const currentResult = !!result?.current && result.client_id === clientID && proofCurrent() && result.round_id === round?.id && result.initial_apply_sha === round.initial_apply_sha && result.expires_unix_nano / 1e6 > now() && subscription?.enabled === true && subscription.state === "active" && subscription.delivery_scope === "lab_initial_apply" && subscription.delivery_revision === result.delivery_revision && subscription.snapshot?.event_id === result.event_id && subscription.snapshot.current;
 function prepareProbe() {
  const current = latest.current.checked?.view;
  if (!idle || !current?.enabled || !current.can_prepare || !window.confirm(t.prepareConfirm)) return;
  const fresh = latest.current.checked?.view;
  if (!retryAvailable() || !fresh?.enabled || !fresh.can_prepare || retained.current || tokenRetained.current) return;
  void send({ action: "prepare", body: { event_id: subscriptionEventID(), promotion_event_id: promotion.event_id, confirmed: true } });
 }
 function requestProof() {
  if (!canProbe || !view?.enabled || !subscription?.enabled) return;
  const binding = initialHealthBinding(view), chosen = clientID, generation = subscription.generation, revision = subscription.initial_apply_revision!, delivery = subscription.delivery_revision!;
  if (!binding || !window.confirm(`${t.testConfirm}\n${t.node}: ${promotion.node_id}\n${t.port}: 127.0.0.1:${view.direct_port}\n${t.client}: ${chosen}`)) return;
  const s = latest.current;
  if (!liveExecution() || !s.checked?.view.enabled || !sameInitialBinding(binding, s.checked.view) || s.clientID !== chosen || s.clientPhase !== "ready" || !s.subscription?.enabled || s.subscription.state !== "active" || s.subscription.generation !== generation || s.subscription.initial_apply_revision !== revision || s.subscription.delivery_revision !== delivery || retained.current || tokenRetained.current || s.round && s.round.expires_unix_nano / 1e6 > now() && !["expired", "state_changed"].includes(s.round.state)) return;
  setResult(null); setRound(null);
  void send({ action: "round", body: { id: subscriptionEventID(), initial_apply_sha: binding.initial_apply_sha, confirmed: true }, binding, client_id: chosen, token_generation: generation, expected_revision: revision, expected_delivery_revision: delivery });
 }
 function recordOutput() {
  const r = latest.current.round, prepared = reviewRef.current, s = latest.current.subscription;
  if (!canRecord || !r || !prepared || !proofCurrent(r) || !s?.enabled || s.state !== "active" || s.generation !== prepared.token_generation || s.initial_apply_revision !== prepared.expected_revision || s.delivery_revision !== prepared.expected_delivery_revision || prepared.client_id !== latest.current.clientID || prepared.body.id !== r.id || !sameInitialBinding(prepared.binding, r)) return;
  void send({ action: "snapshot", body: { event_id: subscriptionEventID(), client_id: prepared.client_id, round_id: r.id, initial_apply_sha: r.initial_apply_sha, expected_revision: prepared.expected_revision, expected_delivery_revision: prepared.expected_delivery_revision, confirmed: true }, round: structuredClone(r) });
 }
 async function sendToken(a: TokenRetry) {
  const c = lifetime.current;
  if (!retryAvailable() || !c || c.signal.aborted || retained.current || !promotion.client_ids.includes(a.client_id) || !rememberToken(a)) return;
  sending.current = true; setBusy(true); hideSecret(); setNotice("");
  try {
   const raw = await api<unknown>(subscriptionPath(a.client_id) + "/" + a.request.action, { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": session!.csrf_token }, body: JSON.stringify(a.request.body), signal: c.signal });
   readTokenReceipt(raw, a.client_id, a.request);
   if (!c.signal.aborted) { rememberToken(null); setClientRefresh(n => n + 1); }
  } catch { if (!c.signal.aborted) setNotice(t.unknown); }
  finally { if (lifetime.current === c && !c.signal.aborted) { sending.current = false; setBusy(false); } }
 }
 function issueToken() {
  if (!idle || clientPhase !== "ready" || !subscription?.enabled || subscription.state === "active" || !window.confirm(t.issueConfirm)) return;
  const s = latest.current.subscription;
  if (!s?.enabled || s.generation !== subscription.generation || s.state !== subscription.state || latest.current.clientID !== clientID) return;
  void sendToken({ client_id: clientID, request: { action: s.state === "absent" ? "issue" : "rotate", body: { event_id: subscriptionEventID(), expected_generation: s.generation } } });
 }
 async function reveal() {
  const c = lifetime.current;
  if (!idle || !subscription?.enabled || subscription.state !== "active" || !c || c.signal.aborted) return;
  const selected = clientID, generation = subscription.generation, epoch = ++secretEpoch.current;
  sending.current = true; setBusy(true); setSecret(""); setCopied(false);
  try {
   const raw = await api<unknown>(subscriptionPath(selected) + "/reveal", { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": session!.csrf_token }, body: JSON.stringify({ expected_generation: generation }), signal: c.signal });
   const path = readSubscriptionSecret(raw, selected, generation), s = latest.current;
   if (!c.signal.aborted && epoch === secretEpoch.current && s.active && !document.hidden && s.clientID === selected && s.subscription?.enabled && s.subscription.generation === generation) { secretIdentity.current = { clientID: selected, generation }; setSecret(path); }
  } catch { if (!c.signal.aborted) { setNotice(t.tokenFailed); setClientRefresh(n => n + 1); } }
  finally { if (lifetime.current === c && !c.signal.aborted) { sending.current = false; setBusy(false); } }
 }
 if (!owner) return null;
 const healthText = !round ? (view?.enabled && view.eligible ? t.ready : view?.enabled && view.prepared ? t.awaiting : t.noProof) : round.state === "state_changed" ? t.changed : round.state === "expired" || round.expires_unix_nano / 1e6 <= now() ? t.expired : round.state === "pending" ? t.pending : proofCurrent() ? t.verified : t.unconfirmed;
 const url = secret ? new URL(secret, window.location.origin).href : "";
 return <section className="native-card lab-subscription initial-apply-health" aria-label="First-Apply health and LAB delivery">
  <div className="lab-subscription-heading"><h3>{t.title}</h3><button type="button" disabled={busy} onClick={() => { setRefresh(n => n + 1); setClientRefresh(n => n + 1); }}>{t.refresh}</button></div>
  <p>{t.scope}</p>
  {phase === "disabled" ? <p role="status">{t.disabled}</p> : <>
   {phase === "loading" && <p role="status">{t.loading}</p>}{phase === "error" && <p role="alert">{t.failed}</p>}
   {view?.enabled && <>
    <div className="initial-health-context"><span>{t.node}: <bdi>{promotion.node_id}</bdi></span><span>{t.port}: <bdi>127.0.0.1:{view.direct_port}</bdi></span></div>
    {!view.prepared ? <div><p>{t.prepareHelp}</p><button type="button" disabled={!idle || !view.can_prepare} onClick={prepareProbe}>{t.prepare}</button></div> : <p>{t.prepared}</p>}
    <div className="initial-health-delivery-review"><label>{t.client}<select aria-label="First-Apply subscription client" value={clientID} disabled={locked || roundLive} onChange={event => { setClientID(event.target.value); setResult(null); setRound(null); rememberReview(null); }}>
     {promotion.client_ids.map(id => <option key={id} value={id}>{id}</option>)}
    </select></label><p>{t.selectHelp}</p>
     {clientPhase === "loading" && <p>{t.tokenLoading}</p>}{clientPhase === "error" && <p role="alert">{t.tokenFailed}</p>}
     {subscription && <p>{subscription.enabled ? subscription.state === "active" ? t.tokenActive : subscription.state === "absent" ? t.tokenAbsent : t.tokenRevoked : t.tokenDisabled}</p>}
     {subscription?.enabled && <div className="lab-subscription-actions">{subscription.state !== "active" ? <button type="button" disabled={!idle || clientPhase !== "ready" || roundLive} onClick={issueToken}>{subscription.state === "absent" ? t.issue : t.replace}</button> : <button type="button" disabled={!idle} onClick={() => void reveal()}>{t.reveal}</button>}</div>}
    </div>
    <div className="initial-health-result" data-health-state={round ? round.expires_unix_nano / 1e6 <= now() ? "expired" : round.state : view.eligible ? "ready" : "awaiting"}>
     <p role="status">{healthText}</p>
     {round && <><p>{t.deadline}: <bdi>{date(round.expires_unix_nano, language)}</bdi></p>{roundLive && <p><bdi>{Math.max(0, Math.ceil((round.expires_unix_nano / 1e6 - now()) / 1000))}</bdi> {t.seconds}</p>}</>}
     <div className="lab-subscription-actions"><button type="button" disabled={!canProbe} onClick={requestProof}>{t.test}</button><button type="button" disabled={!canRecord} onClick={recordOutput}>{t.record}</button></div>
     <p>{t.snapshotHelp}</p>
    </div>
    {result && <div className="lab-subscription-snapshot" role="status" data-initial-snapshot-state={currentResult ? "current" : "historical"}><strong>{t.recorded}</strong><p>{currentResult ? t.current : t.historical}</p><p>{t.deadline}: <bdi>{date(result.expires_unix_nano, language)}</bdi></p></div>}
    {secret && <div className="lab-subscription-secret"><p>{t.secret}</p><label>{t.url}<input aria-label="First-Apply subscription URL" value={url} readOnly autoComplete="off" spellCheck={false} dir="ltr"/></label><div className="lab-subscription-actions"><button type="button" onClick={() => { void navigator.clipboard.writeText(url).then(() => { if (!lifetime.current?.signal.aborted) setCopied(true); }).catch(() => setNotice(t.tokenFailed)); }}>{copied ? t.copied : t.copy}</button><button type="button" onClick={hideSecret}>{t.hide}</button>{currentResult && <a href={url} target="_blank" rel="noreferrer">{t.fetch}</a>}</div><p>{t.fetchHelp}</p></div>}
    <details><summary>{t.details}</summary><dl><div><dt>{t.revisions}</dt><dd><bdi>{promotion.inbound_revision} / {promotion.draft_revision}</bdi></dd></div><div><dt>Promotion</dt><dd><bdi>{promotion.event_id}</bdi></dd></div><div><dt>Installation</dt><dd><bdi>{promotion.installation_id}</bdi></dd></div>{view.initial_apply_sha && <><div><dt>Apply / Stage</dt><dd><bdi>{view.apply_command_id} / {view.stage_id}</bdi></dd></div><div><dt>Epoch</dt><dd><bdi>{view.epoch_id}</bdi></dd></div><div><dt>Execution SHA</dt><dd><bdi>{view.initial_apply_sha}</bdi></dd></div></>}{round && <div><dt>Round</dt><dd><bdi>{round.id}</bdi></dd></div>}</dl>{view.blockers.length > 0 && <><p>{t.blocked}</p><ul>{view.blockers.map((b, i) => <li key={i}><bdi>{b.field}: {b.code}</bdi></li>)}</ul></>}</details>
   </>}
  </>}
  {storageFailed && <p role="alert">{t.storage}</p>}
  {attempt && <div role="status"><p>{notice || t.unknown}</p><div className="lab-subscription-actions"><button type="button" disabled={!retryAvailable() || !!tokenRetry} onClick={() => { if (window.confirm(t.unknown + "\n" + t.retry)) void send(attempt); }}>{t.retry}</button><button type="button" disabled={busy} onClick={() => { if (window.confirm(t.discardConfirm) && remember(null)) setNotice(""); }}>{t.discard}</button></div></div>}
  {tokenRetry && <div role="status"><p>{t.unknown}</p><div className="lab-subscription-actions"><button type="button" disabled={!retryAvailable() || !!attempt} onClick={() => void sendToken(tokenRetry)}>{t.retryToken}</button><button type="button" disabled={busy} onClick={() => { if (window.confirm(t.discardConfirm) && rememberToken(null)) setNotice(""); }}>{t.discard}</button></div></div>}
  {notice && !attempt && !tokenRetry && <p role="status">{notice}</p>}
 </section>;
}
