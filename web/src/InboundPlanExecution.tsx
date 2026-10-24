import { useEffect, useRef, useState } from "react";
import { CheckCircle2, FlaskConical, RefreshCw, Search, X } from "lucide-react";
import { api, ApiError, useSession, type Language } from "./session";
import { creationAPI, CreationApiError } from "./InboundCreationApi";
import type { CreationCapabilities, CreationPlanResponse } from "./InboundCreationModel";
import type { InboundNode } from "./inboundTypes";
import { creationNodeName } from "./InboundCreationModel";
import { canonicalProfileOptions } from "./inboundProfileOptions";
import { maxPromotionClients, promotionPath, readPromotionAttempt, readPromotionClientPage, readPromotionDiscovery, readPromotionPreview, readPromotionReceipt,
  type PromotionAttempt, type PromotionClient, type PromotionDiscovery, type PromotionInput, type PromotionIssue, type PromotionPreview, type PromotionReceipt } from "./inboundPlanExecution";
import "./InboundPlanExecution.css";

const words = {
  en: {
    title: "Prepare LAB execution", help: "Prepare this complete saved setup for one approved loopback Node. Select existing clients; Stage and Apply remain separate actions.",
    scope: "Direct TCP / REALITY · one LAB Node · no subscription publication", open: "Prepare LAB execution", refresh: "Refresh preparation", checking: "Checking this saved setup…", unavailable: "Preparation could not be verified. Refresh before continuing.", older: "This server does not provide saved-plan execution preparation.",
    disabled: "LAB preparation is disabled on this HUB. An operator must enable it at startup.", blocked: "This setup needs changes before LAB preparation.", edit: "Review required settings", close: "Close preparation",
    search: "Search clients for LAB execution", find: "Search clients", selected: "Selected clients", none: "Select at least one existing client.", noResults: "No matching clients.", previous: "Previous clients", next: "More clients", remove: "Remove selected client", loadingClients: "Loading clients…", clientsFailed: "Clients could not be verified. Search again or refresh.", clientUnavailable: "Unavailable", selectionHelp: "Selection does not bind clients yet. The server checks account status, representative access and available usage before preparation.", maxClients: "Up to 500 clients can be selected.",
    reason: "LAB preparation reason", review: "Review LAB preparation", reviewed: "Reviewed preparation", confirm: "Prepare reviewed plan", confirmText: "Prepare this saved plan and bind the reviewed clients for this LAB Node? The configuration becomes available to Stage. This does not start Xray or publish a subscription.", reasonRequired: "Enter a reason of 1–200 characters without control characters.",
    node: "LAB Node", listener: "Loopback listener", hosts: "Saved Hosts", hostsHelp: "These are saved connection addresses. Preparation does not verify their reachability.", clients: "Clients to bind", planRevision: "Plan revision", profileRevision: "Profile revision", noRuntime: "No process starts during preparation.",
    changed: "The setup or its prerequisites changed. Refresh and review again before preparing it.", unknown: "The response was not confirmed. This exact request is retained; refresh to reconcile it or retry it explicitly.", retry: "Retry exact preparation", retryConfirm: "Retry the same preparation with the same event, selected clients and reviewed revisions? A completed operation will be replayed without preparing it twice.", storage: "The exact request could not be retained in this tab. No new preparation request was sent.",
    prepared: "Prepared for LAB", preparedHelp: "Configuration and client bindings are prepared. Use the metered controls below to Stage and Apply; current process evidence is shown there.", bound: "Prepared client bindings", subscription: "Subscription publication is unavailable for this preparation.", historical: "The original saved setup remains available as a read-only record.", receipt: "Preparation record", copiedElsewhere: "Another preparation is already recorded. Its receipt does not match this retained request; no further request was sent.",
  },
  fa: {
    title: "آماده‌سازی اجرای LAB", help: "طرح کامل ذخیره‌شده را برای یک نود مجاز loopback آماده کن. کلاینت‌های موجود را انتخاب کن؛ Stage و Apply جداگانه انجام می‌شوند.",
    scope: "Direct با TCP / REALITY · یک نود LAB · بدون انتشار اشتراک", open: "آماده‌سازی اجرای LAB", refresh: "به‌روزرسانی آمادگی", checking: "در حال بررسی طرح ذخیره‌شده…", unavailable: "آمادگی تأیید نشد؛ پیش از ادامه به‌روزرسانی کن.", older: "این نسخهٔ سرور آماده‌سازی اجرای طرح ذخیره‌شده را ندارد.",
    disabled: "آماده‌سازی LAB در این HUB خاموش است؛ فعال‌کردن آن هنگام راه‌اندازی در اختیار اپراتور است.", blocked: "این طرح پیش از آماده‌سازی LAB نیاز به اصلاح دارد.", edit: "بررسی تنظیمات لازم", close: "بستن آماده‌سازی",
    search: "جست‌وجوی کلاینت برای اجرای LAB", find: "جست‌وجوی کلاینت", selected: "کلاینت‌های انتخاب‌شده", none: "حداقل یک کلاینت موجود انتخاب کن.", noResults: "کلاینتی پیدا نشد.", previous: "کلاینت‌های قبلی", next: "کلاینت‌های بیشتر", remove: "حذف کلاینت از انتخاب", loadingClients: "در حال دریافت کلاینت‌ها…", clientsFailed: "کلاینت‌ها تأیید نشدند؛ دوباره جست‌وجو یا به‌روزرسانی کن.", clientUnavailable: "غیرمجاز", selectionHelp: "انتخاب هنوز کلاینت را متصل نمی‌کند. سرور وضعیت حساب، دسترسی نماینده و سهمیهٔ قابل‌مصرف را پیش از آماده‌سازی بررسی می‌کند.", maxClients: "حداکثر ۵۰۰ کلاینت قابل انتخاب است.",
    reason: "دلیل آماده‌سازی LAB", review: "بررسی آماده‌سازی LAB", reviewed: "آماده‌سازی بررسی‌شده", confirm: "آماده‌سازی طرح بررسی‌شده", confirmText: "این طرح برای نود LAB آماده و کلاینت‌های بررسی‌شده به آن متصل شوند؟ تنظیمات در دسترس Stage قرار می‌گیرد؛ Xray اجرا و اشتراک منتشر نمی‌شود.", reasonRequired: "دلیل را در ۱ تا ۲۰۰ نویسه، بدون نویسهٔ کنترلی وارد کن.",
    node: "نود LAB", listener: "پورت loopback", hosts: "هاست‌های ذخیره‌شده", hostsHelp: "این‌ها نشانی‌های اتصال ذخیره‌شده‌اند؛ آماده‌سازی دسترسی‌پذیری آن‌ها را آزمایش نمی‌کند.", clients: "کلاینت‌های قابل اتصال", planRevision: "نسخهٔ طرح", profileRevision: "نسخهٔ پروفایل", noRuntime: "آماده‌سازی هیچ فرآیندی را اجرا نمی‌کند.",
    changed: "طرح یا پیش‌شرط‌های آن تغییر کرده؛ پیش از آماده‌سازی دوباره به‌روزرسانی و بررسی کن.", unknown: "پاسخ تأیید نشد. همان درخواست حفظ شده؛ برای تطبیق وضعیت به‌روزرسانی کن یا همان درخواست را تکرار کن.", retry: "تکرار همان آماده‌سازی", retryConfirm: "همان آماده‌سازی با همان شناسه، کلاینت‌ها و نسخه‌های بررسی‌شده تکرار شود؟ عملیات ثبت‌شده بدون آماده‌سازی دوباره بازخوانی می‌شود.", storage: "ذخیرهٔ درخواست دقیق در این تب ممکن نشد؛ درخواست آماده‌سازی تازه‌ای ارسال نشد.",
    prepared: "آماده‌شده برای LAB", preparedHelp: "تنظیمات و اتصال کلاینت‌ها آماده شده‌اند. Stage و Apply را با کنترل‌های مصرف در پایین انجام بده؛ شواهد اجرای فعلی همان‌جا نمایش داده می‌شود.", bound: "اتصال‌های آماده‌شدهٔ کلاینت", subscription: "انتشار اشتراک برای این آماده‌سازی در دسترس نیست.", historical: "طرح اصلی به‌صورت فقط‌خواندنی برای بازبینی حفظ می‌شود.", receipt: "رکورد آماده‌سازی", copiedElsewhere: "آماده‌سازی دیگری ثبت شده و رسیدش با درخواست حفظ‌شده مطابق نیست؛ درخواست دیگری ارسال نشد.",
  },
} as const;

const reasons: Record<string, [string, string]> = {
  promotion_disabled: ["LAB preparation is disabled at startup.", "آماده‌سازی LAB هنگام راه‌اندازی فعال نشده است."],
  node_not_approved: ["This Node is not approved for LAB preparation.", "این نود برای آماده‌سازی LAB مجاز نشده است."],
  pairing_changed: ["The Node installation changed. Review its saved assignment.", "شناسهٔ نصب نود تغییر کرده؛ اختصاص ذخیره‌شده را بررسی کن."],
  clients_required: ["Select at least one existing client.", "حداقل یک کلاینت موجود انتخاب کن."],
  reason_required: ["Enter a reason for this preparation.", "دلیل آماده‌سازی را وارد کن."],
  clients_ineligible: ["Some selected clients are no longer eligible.", "بعضی کلاینت‌های انتخاب‌شده دیگر مجاز نیستند."],
  client_ineligible: ["A selected client is no longer eligible.", "یکی از کلاینت‌های انتخاب‌شده دیگر مجاز نیست."],
  single_node_required: ["LAB preparation requires exactly one destination Node.", "آماده‌سازی LAB دقیقاً یک نود مقصد می‌خواهد."],
  direct_only_required: ["Only a Direct path can be prepared in this phase.", "در این مرحله فقط مسیر Direct قابل آماده‌سازی است."],
  loopback_required: ["The saved listener must use 127.0.0.1.", "IP دریافت ذخیره‌شده باید 127.0.0.1 باشد."],
  tcp_reality_required: ["This phase supports TCP / REALITY only.", "این مرحله فقط TCP / REALITY را پشتیبانی می‌کند."],
  profile_options_unsupported: ["Remove advanced profile options before this LAB preparation.", "برای این آماده‌سازی LAB تنظیمات پیشرفتهٔ پروفایل را حذف کن."],
  endpoint_options_unsupported: ["Rich Host overrides are not supported by this LAB preparation.", "تنظیمات اختصاصی Host در این آماده‌سازی LAB پشتیبانی نمی‌شوند."],
  hosts_required: ["Add at least one Direct Host in Dark Swap.", "در دارک‌سواپ حداقل یک Host برای Direct وارد کن."],
  plan_promoted: ["This plan has already been prepared.", "این طرح قبلاً آماده شده است."],
  profile_changed_or_materialized: ["The saved profile no longer matches this plan.", "پروفایل ذخیره‌شده دیگر با طرح مطابق نیست."],
  promotion_transport_unsupported: ["This phase supports TCP / REALITY only. Other networks remain saved and editable.", "این مرحله فقط TCP / REALITY را پشتیبانی می‌کند؛ شبکه‌های دیگر ذخیره‌شده و قابل ویرایش می‌مانند."],
  promotion_options_unsupported: ["Advanced profile settings are not supported by this first LAB execution path.", "تنظیمات پیشرفتهٔ پروفایل در این مسیر اولیهٔ اجرای LAB پشتیبانی نمی‌شوند."],
  promotion_single_node_required: ["Prepare the complete plan with exactly one destination Node.", "طرح کامل باید دقیقاً یک نود مقصد داشته باشد."],
  promotion_loopback_required: ["Set the destination listening IP to 127.0.0.1 in Dark Swap.", "در دارک‌سواپ IP دریافت مقصد را روی 127.0.0.1 قرار بده."],
  promotion_direct_required: ["Enable Direct and choose its listener port in Dark Swap.", "در دارک‌سواپ Direct را روشن و پورت دریافت آن را مشخص کن."],
  promotion_direct_only: ["Tunnel and Dark Swap paths cannot execute in this LAB phase.", "مسیرهای Tunnel و دارک‌سواپ در این مرحلهٔ LAB قابل اجرا نیستند."],
  promotion_endpoints_unsupported: ["Use simple Direct Hosts for this phase; rich Host overrides remain authoring-only.", "برای این مرحله Host سادهٔ Direct لازم است؛ تنظیمات اختصاصی Host فعلاً فقط ذخیره می‌شوند."],
  promotion_hosts_required: ["Add at least one Direct Host in Dark Swap.", "در دارک‌سواپ حداقل یک Host برای Direct وارد کن."],
  promotion_metering_unavailable: ["The required usage ledger is unavailable.", "دفتر ثبت مصرف موردنیاز در دسترس نیست."],
  promotion_node_not_reserved: ["An operator must reserve this empty Node for metered LAB execution at startup.", "اپراتور باید این نود خالی را هنگام راه‌اندازی برای اجرای متری LAB رزرو کند."],
  promotion_observation_stale: ["A fresh signed observation from the approved Agent is required.", "مشاهدهٔ امضاشده و تازهٔ Agent مجاز لازم است."],
  promotion_runtime_not_empty: ["This Node already has runtime state. First preparation requires an empty runtime channel.", "این نود از قبل وضعیت اجرایی دارد؛ آماده‌سازی اولیه کانال اجرای خالی می‌خواهد."],
  promotion_node_not_empty: ["This Node already has targets or route work. First preparation requires an exclusive empty Node.", "این نود مقصد یا عملیات مسیر دیگری دارد؛ آماده‌سازی اولیه نود خالی و اختصاصی می‌خواهد."],
  promotion_client_ineligible: ["A selected client is expired, disabled or lacks eligible account or usage state.", "یکی از کلاینت‌ها منقضی یا غیرفعال است، یا وضعیت مجاز حساب و سهمیه ندارد."],
  promotion_clients_invalid: ["Select between 1 and 500 distinct existing clients.", "بین ۱ تا ۵۰۰ کلاینت موجود و متفاوت انتخاب کن."],
  promotion_revision_changed: ["The saved plan or profile revision changed. Refresh and review it again.", "نسخهٔ طرح یا پروفایل تغییر کرده؛ به‌روزرسانی و دوباره بررسی کن."],
  promotion_review_changed: ["The reviewed Node or client state changed. Review again before preparing.", "وضعیت بررسی‌شدهٔ نود یا کلاینت تغییر کرده؛ پیش از آماده‌سازی دوباره بررسی کن."],
  promotion_review_required: ["Review this exact preparation before confirming it.", "پیش از تأیید، همین آماده‌سازی را بررسی کن."],
  reason_invalid: ["Enter a reason of 1–200 characters without control characters.", "دلیل را در ۱ تا ۲۰۰ نویسه و بدون نویسهٔ کنترلی وارد کن."],
  port_conflict: ["A listener port is already reserved. Review the Direct port in Dark Swap.", "پورت دریافت از قبل رزرو شده است؛ پورت Direct را در دارک‌سواپ بررسی کن."],
};
function issueText(issue: PromotionIssue, language: Language) { return reasons[issue.code]?.[language === "fa" ? 1 : 0] ?? (language === "fa" ? "این پیش‌شرط برای آماده‌سازی تأیید نشد." : "This preparation prerequisite could not be verified."); }
type Summary = CreationCapabilities["plans"][number];
type PreparedReview = { intent: PromotionInput; preview: PromotionPreview };

export default function InboundPlanExecution({ language, summary, nodes, blocked, onPromoted, onOpenSetup, onDirtyChange, onBusyChange }: {
  language: Language; summary: Summary; nodes: InboundNode[]; blocked: boolean; onPromoted: (receipt: PromotionReceipt) => void;
  onOpenSetup: (issue?: PromotionIssue) => void; onDirtyChange: (dirty: boolean) => void; onBusyChange: (busy: boolean) => void;
}) {
  const t = words[language], { session, expire } = useSession(), inboundID = summary.inbound_id;
  const [open, setOpen] = useState(false), [discovery, setDiscovery] = useState<PromotionDiscovery | null>(null), [phase, setPhase] = useState<"loading" | "ready" | "error" | "older">("loading");
  const [refresh, setRefresh] = useState(0), [busy, setBusy] = useState(false), [attempt, setAttempt] = useState<PromotionAttempt | null>(null), [storageFailed, setStorageFailed] = useState(false);
  const [selected, setSelected] = useState<Record<string, string>>({}), [reason, setReason] = useState(""), [review, setReview] = useState<PreparedReview | null>(null), [issues, setIssues] = useState<PromotionIssue[]>([]), [message, setMessage] = useState("");
  const [query, setQuery] = useState(""), [search, setSearch] = useState(""), [offset, setOffset] = useState(0), [clients, setClients] = useState<PromotionClient[] | null>(null), [more, setMore] = useState(false), [clientError, setClientError] = useState(false);
  const lifetime = useRef<AbortController | null>(null), sending = useRef(false), retained = useRef<PromotionAttempt | null>(null), notified = useRef(""), reasonInput = useRef<HTMLInputElement>(null), selectedHeading = useRef<HTMLHeadingElement>(null);
  const callbacks = useRef({ onPromoted, onDirtyChange, onBusyChange }); callbacks.current = { onPromoted, onDirtyChange, onBusyChange };
  const storageKey = `dark-pro-plan-promotion:${session?.user.id}:${inboundID}`;
  const receipt = summary.promotion ?? discovery?.receipt;
  const locked = busy || !!attempt || !!receipt;
  const eligible = !blocked && phase === "ready" && discovery?.available === true && discovery.blockers.length === 0 && !receipt && !storageFailed;
  const selectionIDs = Object.keys(selected).sort();
  function remember(value: PromotionAttempt | null) {
    try { if (value) sessionStorage.setItem(storageKey, JSON.stringify(value)); else sessionStorage.removeItem(storageKey); }
    catch { setStorageFailed(true); return false; }
    retained.current = value; setAttempt(value); setStorageFailed(false); return true;
  }
  function accept(value: PromotionReceipt) {
    setDiscovery(previous => ({ available: previous?.available ?? true, scope: "single_direct_tcp_reality_loopback", promoted: true, blockers: [], receipt: value }));
    if (notified.current !== value.event_id) { notified.current = value.event_id; callbacks.current.onPromoted(value); }
  }
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    try {
      const raw = sessionStorage.getItem(storageKey), previous = readPromotionAttempt(raw, inboundID);
      if (raw && !previous) setStorageFailed(true);
      if (previous) { retained.current = previous; setAttempt(previous); setSelected(Object.fromEntries(previous.review.client_ids.map(id => [id, id]))); setReason((JSON.parse(previous.body) as PromotionInput).reason); setOpen(true); }
    } catch { setStorageFailed(true); }
    return () => { controller.abort(); callbacks.current.onBusyChange(false); callbacks.current.onDirtyChange(false); };
  }, [storageKey, inboundID]);
  useEffect(() => { callbacks.current.onDirtyChange(!receipt && (selectionIDs.length > 0 || !!reason || !!attempt)); }, [selectionIDs.length, reason, attempt, receipt]);
  useEffect(() => { callbacks.current.onBusyChange(busy); }, [busy]);
  useEffect(() => {
    const controller = new AbortController(); setPhase("loading"); setReview(null);
    void creationAPI<unknown>(promotionPath(inboundID), { signal: controller.signal }, expire).then(raw => {
      const next = readPromotionDiscovery(raw, inboundID);
      if (controller.signal.aborted) return;
      if (next.receipt && retained.current) {
        try { readPromotionReceipt(next.receipt, inboundID, retained.current); remember(null); }
        catch { setMessage(t.copiedElsewhere); }
      }
      setDiscovery(next); setPhase("ready");
      if (next.receipt) accept(next.receipt);
    }).catch(error => {
      if (controller.signal.aborted) return;
      setPhase(error instanceof ApiError && error.status === 404 ? "older" : "error");
    });
    return () => controller.abort();
  }, [inboundID, summary.plan_revision, summary.inbound_revision, refresh, expire]);
  useEffect(() => {
    if (!open || receipt) return;
    const controller = new AbortController(); setClients(null); setClientError(false);
    const params = new URLSearchParams({ q: search, offset: String(offset), limit: "25", state: "all", plan: "all", sort: "name" });
    void api<unknown>("/api/v1/workspace/clients?" + params, { signal: controller.signal }).then(raw => {
      const next = readPromotionClientPage(raw, offset);
      if (!controller.signal.aborted) { setClients(next.clients); setMore(next.has_more); }
    }).catch(() => { if (!controller.signal.aborted) setClientError(true); });
    return () => controller.abort();
  }, [open, !!receipt, search, offset, refresh]);
  function invalidate() { setReview(null); setIssues([]); setMessage(""); }
  function changeSelection(client: PromotionClient, checked: boolean) {
    if (locked) return;
    invalidate(); setSelected(previous => { const next = { ...previous }; if (checked && Object.keys(next).length < maxPromotionClients) next[client.id] = client.name; else delete next[client.id]; return next; });
  }
  function errorMessage(error: unknown) {
    setReview(null);
    if (error instanceof CreationApiError && error.issue) {
      const issue = { node_id: error.issue.nodeID, field: error.issue.field, code: error.issue.code };
      setIssues([issue]);
      if (issue.field === "reason") reasonInput.current?.focus();
      else if (issue.field === "client_ids") selectedHeading.current?.focus();
    }
    setMessage(error instanceof ApiError && [400, 409, 422].includes(error.status) ? t.changed : t.unavailable);
  }
  async function check() {
    const controller = lifetime.current;
    if (!eligible || locked || sending.current || !controller || controller.signal.aborted || !session?.csrf_token) return;
    if (!selectionIDs.length) { setMessage(t.none); selectedHeading.current?.focus(); return; }
    const trimmed = reason.trim();
    if (!trimmed || [...trimmed].length > 200 || /[\u0000-\u001f\u007f-\u009f]/.test(trimmed)) { setMessage(t.reasonRequired); reasonInput.current?.focus(); return; }
    const intent: PromotionInput = { event_id: crypto.randomUUID().replaceAll("-", ""), expected_plan_revision: summary.plan_revision, expected_inbound_revision: summary.inbound_revision, client_ids: selectionIDs, reason: trimmed };
    sending.current = true; setBusy(true); setMessage(""); setIssues([]); setReview(null);
    try {
      const saved = await creationAPI<CreationPlanResponse>(`/api/v1/workspace/native-inbound-plans/${inboundID}`, { signal: controller.signal }, expire);
      const plan = saved?.plan;
      if (!plan || plan.profile?.id !== inboundID || plan.plan_revision !== intent.expected_plan_revision || plan.inbound_revision !== intent.expected_inbound_revision || !plan.editable || plan.runtime_apply !== false || saved.references_current !== true || plan.nodes?.length !== 1) throw new ApiError(409, "plan_changed");
      const raw = await creationAPI<unknown>(promotionPath(inboundID) + "/preview", { method: "POST", signal: controller.signal, headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token }, body: JSON.stringify(intent) }, expire);
      const preview = readPromotionPreview(raw, inboundID, intent), node = plan.nodes[0];
      if (preview.eligible && (preview.node_id !== node.node_id || preview.installation_id !== node.installation_id || preview.direct_port !== node.direct_port || JSON.stringify(preview.direct_hosts) !== JSON.stringify(node.direct_hosts) || node.listen_ip !== "127.0.0.1" || node.tunnel_port !== 0 || node.swap_enabled || !!node.direct_endpoints?.length || !!node.tunnel_endpoints?.length || plan.profile.transport !== "tcp" || plan.profile.security === "tls" || !!canonicalProfileOptions(plan.profile.options) || plan.profile.enabled || plan.profile.archived || plan.profile.bound_clients !== 0)) throw Error("mismatched_promotion_plan");
      if (!controller.signal.aborted) { setIssues(preview.blockers); if (preview.eligible) setReview({ intent, preview }); }
    } catch (error) { if (!controller.signal.aborted) errorMessage(error); }
    finally { sending.current = false; if (!controller.signal.aborted) setBusy(false); }
  }
  async function send(value: PromotionAttempt) {
    const controller = lifetime.current;
    if (sending.current || blocked || !controller || controller.signal.aborted || !session?.csrf_token || receipt || phase !== "ready") return;
    if (!remember(value)) return;
    sending.current = true; setBusy(true); setMessage(""); setIssues([]);
    try {
      const raw = await creationAPI<unknown>(promotionPath(inboundID), { method: "POST", signal: controller.signal, headers: { "Content-Type": "application/json", "X-CSRF-Token": session.csrf_token }, body: value.body }, expire);
      const result = readPromotionReceipt(raw, inboundID, value);
      if (!controller.signal.aborted) { remember(null); setReview(null); setSelected({}); setReason(""); accept(result); }
    } catch (error) {
      if (!controller.signal.aborted && error instanceof ApiError && [400, 401, 403, 404, 409, 422].includes(error.status)) { remember(null); errorMessage(error); setRefresh(count => count + 1); }
      // Lost or malformed replies can follow a commit. Keep the exact body and reviewed context.
    } finally { sending.current = false; if (!controller.signal.aborted) setBusy(false); }
  }
  function confirm() {
    if (!review || !eligible || locked || review.intent.expected_plan_revision !== summary.plan_revision || review.intent.expected_inbound_revision !== summary.inbound_revision || !window.confirm(t.confirmText + "\n" + review.preview.node_id + ":" + review.preview.direct_port + "\n" + t.clients + ": " + review.intent.client_ids.length)) return;
    const value: PromotionAttempt = { inbound_id: inboundID, review: review.preview, body: JSON.stringify({ ...review.intent, review_sha256: review.preview.review_sha256, confirmed: true }) };
    void send(value);
  }
  const allIssues = issues.length ? issues : discovery?.blockers ?? [];
  const displayedIssues = allIssues.filter((issue, index) => allIssues.findIndex(other => other.code === issue.code && other.field === issue.field && other.node_id === issue.node_id) === index);
  return <section className="native-card inbound-plan-execution" aria-label="Inbound LAB preparation">
    <header className="inbound-execution-heading"><div><h3><FlaskConical size={20}/>{receipt ? t.prepared : t.title}</h3><p>{receipt ? t.preparedHelp : t.help}</p></div><button type="button" disabled={busy} onClick={() => { setMessage(""); setIssues([]); setRefresh(count => count + 1); }}><RefreshCw size={15}/>{t.refresh}</button></header>
    {receipt ? <div className="inbound-promotion-result" data-promotion-state="prepared"><p><CheckCircle2 size={17}/>{t.historical}</p><dl><div><dt>{t.node}</dt><dd>{creationNodeName(nodes, receipt.node_id)}</dd></div><div><dt>{t.bound}</dt><dd>{receipt.client_ids.length}</dd></div><div><dt>{t.profileRevision}</dt><dd>{receipt.source_inbound_revision} → {receipt.inbound_revision}</dd></div></dl><p>{t.subscription}</p><details><summary>{t.receipt}</summary><p>{t.planRevision}: {receipt.plan_revision}</p><bdi>{receipt.event_id}</bdi><ul>{receipt.client_ids.map(id => <li key={id}><bdi>{id}</bdi></li>)}</ul></details></div> : <>
      <p className="creation-help">{t.scope}</p>
      {phase === "loading" && <p role="status">{t.checking}</p>}{phase === "older" && <p role="status">{t.older}</p>}{phase === "error" && <p role="alert">{t.unavailable}</p>}
      {phase === "ready" && displayedIssues.length > 0 && <div className="inbound-promotion-blockers" role="status"><p>{displayedIssues.some(issue => issue.code === "promotion_disabled") ? t.disabled : t.blocked}</p><ul>{displayedIssues.map((issue, index) => <li key={issue.code + index}><span>{issueText(issue, language)}</span>{issue.node_id && <small>{creationNodeName(nodes, issue.node_id)}</small>}<code>{issue.code}</code></li>)}</ul><button type="button" disabled={busy} onClick={() => onOpenSetup(displayedIssues.find(issue => issue.field !== "promotion" && issue.field !== "node_id" && issue.field !== "client_ids"))}>{t.edit}</button></div>}
      {!open && eligible && <button type="button" className="native-primary" onClick={() => setOpen(true)}>{t.open}</button>}
      {open && <div className="inbound-promotion-form">
        <form className="inbound-promotion-search" onSubmit={event => { event.preventDefault(); setOffset(0); setSearch(query); }}><label>{t.search}<input value={query} maxLength={128} disabled={locked} onChange={event => setQuery(event.target.value)}/></label><button type="submit" disabled={locked}><Search size={15}/>{t.find}</button></form>
        <p className="creation-help">{t.selectionHelp}</p>
        {clientError && <p role="alert">{t.clientsFailed}</p>}{!clients && !clientError && <p role="status">{t.loadingClients}</p>}
        <div className="inbound-promotion-clients">{clients?.map(client => { const unavailable = client.archived || client.deployment_state === "disabled" || client.expires_unix * 1000 <= Date.now(), chosen = Object.hasOwn(selected, client.id); return <label key={client.id}><input type="checkbox" aria-label={`Select LAB client: ${client.id}`} disabled={locked || unavailable || !chosen && selectionIDs.length >= maxPromotionClients} checked={chosen} onChange={event => changeSelection(client, event.target.checked)}/><span><strong>{client.name}</strong><bdi>{client.id}</bdi></span>{unavailable && <small>{t.clientUnavailable}</small>}</label>; })}{clients?.length === 0 && <p>{t.noResults}</p>}</div>
        <div className="native-actions"><button type="button" disabled={locked || offset === 0} onClick={() => setOffset(value => value - 25)}>{t.previous}</button><button type="button" disabled={locked || !clients || !more} onClick={() => setOffset(value => value + 25)}>{t.next}</button></div>
        <div className="inbound-promotion-selected"><h4 ref={selectedHeading} tabIndex={-1}>{t.selected} ({selectionIDs.length})</h4>{!selectionIDs.length && <p>{t.none}</p>}<div>{selectionIDs.map(id => <span key={id}><bdi>{selected[id]}{selected[id] !== id && ` (${id})`}</bdi><button type="button" disabled={locked} aria-label={`${t.remove}: ${id}`} onClick={() => { invalidate(); setSelected(previous => { const next = { ...previous }; delete next[id]; return next; }); }}><X size={14}/></button></span>)}</div>{selectionIDs.length >= maxPromotionClients && <p>{t.maxClients}</p>}</div>
        <label>{t.reason}<input ref={reasonInput} value={reason} maxLength={200} disabled={locked} onChange={event => { invalidate(); setReason(event.target.value); }}/></label>
        {review && <div className="inbound-promotion-review" data-promotion-review><h4>{t.reviewed}</h4><dl><div><dt>{t.node}</dt><dd>{creationNodeName(nodes, review.preview.node_id)}</dd></div><div><dt>{t.listener}</dt><dd><bdi>127.0.0.1:{review.preview.direct_port}</bdi></dd></div><div><dt>{t.clients}</dt><dd>{review.intent.client_ids.length}</dd></div><div><dt>{t.planRevision}</dt><dd>{review.preview.plan_revision}</dd></div><div><dt>{t.profileRevision}</dt><dd>{review.preview.inbound_revision}</dd></div><div><dt>{t.hosts}</dt><dd>{review.preview.direct_hosts.map(host => <bdi key={host}>{host}</bdi>)}</dd></div></dl><p>{t.hostsHelp}</p><p>{t.noRuntime}</p></div>}
        <div className="native-actions"><button type="button" disabled={locked} onClick={() => { setOpen(false); setSelected({}); setReason(""); invalidate(); }}>{t.close}</button><button type="button" disabled={!eligible || locked || clientError} onClick={() => void check()}>{t.review}</button>{review && <button type="button" className="native-primary" disabled={!eligible || locked} onClick={confirm}>{t.confirm}</button>}</div>
      </div>}
    </>}
    {attempt && !receipt && <div className="inbound-promotion-retry" role="status"><p>{t.unknown}</p><bdi>{(JSON.parse(attempt.body) as PromotionInput).event_id}</bdi><button type="button" disabled={busy || blocked || phase !== "ready" || storageFailed} onClick={() => { if (window.confirm(t.retryConfirm)) void send(attempt); }}>{t.retry}</button></div>}
    {storageFailed && <p role="alert">{t.storage}</p>}{message && <p className="native-warning" role="alert">{message}</p>}
  </section>;
}
