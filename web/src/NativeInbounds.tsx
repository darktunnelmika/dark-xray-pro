import "./NativeInbounds.css";
import { useSession, api, ApiError as Fault } from "./session";
import InboundLifecycle from "./InboundLifecycle";
import NativeReadiness from "./NativeReadiness";
import NativeCanary from "./NativeCanary";
import NativeRouteDraftEditor from "./NativeRouteDraftEditor";
import NativeRouteTransactions from "./NativeRouteTransactions";
import NativeMeteredRouteTransactions from "./NativeMeteredRouteTransactions";
import InboundDirectory from "./InboundDirectory";
import WorkspaceDialog from "./WorkspaceDialog";
import { profileOptionsDraft, profileOptionsPayload, type ProfileOptionsDraft } from "./inboundProfileOptions";
import InboundProfileForm, { type InboundProfileFormHandle, type InboundProfileValue } from "./InboundProfileForm";
import InboundCreation from "./InboundCreation";
import type { CreationCapabilities, CreationPlan, CreationPlanResponse } from "./InboundCreationModel";
import { navigateURL, registerNavigationGuard } from "./navigation";
import type { Inbound, InboundOverview as Overview, InboundStatus as Status } from "./inboundTypes";
import { useEffect, useRef, useState, type FormEvent, type ReactNode, type KeyboardEvent } from "react";
import { ArrowRightLeft, CheckCircle2, ChevronLeft, Copy, Plus, RefreshCw, Server, ShieldCheck, SlidersHorizontal, Users } from "lucide-react";

type Lang = "fa" | "en";
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
  listenersDisabled: "All managed listeners are disabled", enabledLabel: "Enabled", disabledLabel: "Disabled", archivedLabel: "Archived", status: "Apply reports", desired: "Desired revision", observed: "Reported revision", waiting: "Awaiting Agent report", applied: "Matching process reported", stale: "Report stale / lease expired", changed: "Configuration changed; awaiting Apply", stopped: "Process not ready", mismatch: "Reported revision differs", evidence: "An authenticated Agent report is not proof of customer connectivity or billing correctness. No subscription is published here.", phase: "Agent phase", last: "Last report", saved: "Saved. Waiting for a fresh Agent report; no migration occurred.", shared: "Direct and Dark Swap cannot use the same Node port. Choose separate ports to preserve route attribution; saving is blocked.",
 },
 fa: {
  title: "اینباندهای واقعی PRO", lead: "اتصال کلاینت‌ها و ارسال تنظیمات به نودهای جدید PRO.", login: "ورود مالک", user: "نام کاربری", pass: "رمز عبور", enter: "ورود",
  checking: "در حال بررسی دسترسی مالک…", ownerOnly: "مدیریت اینباند فقط در اختیار مالک است؛ نمایندگان مجوز تغییر نودها را ندارند.", unavailable: "اینباند واقعی در این HUB هنوز فعال نشده است.", error: "درخواست ناموفق بود؛ موفقیت Apply فرض نمی‌شود. پیش از تلاش دوباره، به‌روزرسانی کن.", retry: "به‌روزرسانی",
  create: "ساخت اینباند REALITY", name: "نام اینباند", transport: "ترنسپورت", dest: "مقصد REALITY (هاست:پورت)", sni: "نام‌های سرور (با کاما)", service: "سرویس gRPC", save: "ساخت اینباند",
  empty: "هنوز اینباند واقعی ساخته نشده است.", select: "انتخاب اینباند", bound: "کلاینت متصل", members: "اتصال کلاینت‌ها", search: "جست‌وجو در این صفحه", add: "اتصال انتخاب‌شده‌ها", remove: "جداسازی انتخاب‌شده‌ها", previous: "قبلی", next: "بعدی", all: "انتخاب این صفحه", noClients: "کلاینتی در این صفحه نیست.", bindingHelp: "جداسازی، دسترسی این اینباند را برمی‌دارد؛ کلاینت و سوابق مالی پاک نمی‌شوند.",
  nodes: "تنظیم مقصد نود", node: "نود جدید متصل", choose: "انتخاب نود", listen: "IP اختصاص‌یافته به نود", direct: "پورت مستقیم (صفر = غیرفعال)", swap: "پورت مقصد Dark Swap (صفر = غیرفعال)", targetSave: "ذخیره مقصد نود", noNodes: "نود جدیدی به PRO متصل نیست؛ نودهای قدیمی خودکار وارد نمی‌شوند.", targetHelp: "ذخیره، نسخهٔ درخواستی را تغییر می‌دهد. Agent فعال ممکن است Xray همین نود را بازراه‌اندازی کند؛ این کار تانل واسط نمی‌سازد.", confirm: "مقصد نود ذخیره شود؟ Agent فعال PRO ممکن است Xray همین نود را بازراه‌اندازی کند و اتصال‌های آن قطع شوند.",
  listenersDisabled: "همهٔ پورت‌های تحت مدیریت غیرفعال‌اند", enabledLabel: "فعال", disabledLabel: "غیرفعال", archivedLabel: "آرشیوشده", status: "گزارش اعمال تنظیمات", desired: "نسخهٔ درخواستی", observed: "نسخهٔ گزارش‌شده", waiting: "در انتظار گزارش Agent", applied: "اجرای نسخهٔ مطابق گزارش شد", stale: "گزارش قدیمی / مجوز منقضی", changed: "تنظیمات تغییر کرده؛ در انتظار Apply", stopped: "فرآیند آماده نیست", mismatch: "نسخهٔ گزارش‌شده متفاوت است", evidence: "گزارش معتبر Agent، اثبات اتصال مشتری یا صحت حسابداری نیست. در این بخش اشتراک منتشر نمی‌شود.", phase: "مرحلهٔ Agent", last: "آخرین گزارش", saved: "ذخیره شد؛ منتظر گزارش تازهٔ Agent هستیم. مهاجرتی انجام نشد.", shared: "پورت Direct و Dark Swap نباید یکسان باشد؛ برای تفکیک مسیرها پورت‌های مجزا بزن. ذخیره مسدود است.",
 },
};
function freshID(): number { return crypto.getRandomValues(new Uint32Array(1))[0] || 1; }
function shortID(): string { return Array.from(crypto.getRandomValues(new Uint8Array(4))).map(v => v.toString(16).padStart(2, "0")).join(""); }
const split = (s: string) => s.split(/[\s,]+/).filter(Boolean);
const workspaceWords = {
 en: {
  overview: "Overview", routes: "Routes", clients: "Clients", activity: "Activity", diagnostics: "Diagnostics", back: "Back to Inbounds", cancel: "Cancel", close: "Close",
  listLead: "Manage profiles, assigned Nodes and client access.", loading: "Loading Inbounds…", missing: "This Inbound is no longer available.",
  boundary: "Route execution and customer delivery have separate verification states.", swapBoundary: "Saved Dark Swap drafts are available. Live forwarding and customer delivery are not yet enabled.",
  copy: "Copy settings", copyHint: "Copy the public profile into a new Inbound. New keys are generated; clients and Node assignments are not copied.",
  nodeCount: "Assigned Nodes", clientCount: "Bound clients", profile: "Profile", targetAdd: "Add Node target", targetEdit: "Edit target", assigned: "Assigned Nodes and ports",
  directPort: "Direct", swapPort: "Destination listener", none: "No Node is assigned to this Inbound yet.", routeHelp: "Assign the destination Node first, then review and save its route draft below.",
  searchClients: "Search all clients", boundCount: "Currently bound", clientsFailed: "Client selection is unavailable. Refresh to try again.",
  unsaved: "Discard the unsaved changes to this Inbound?", lab: "Diagnostics / LAB", labHelp: "Readiness, controlled tests and runtime reports for the whole Node fleet.",
  editorLead: "Set up the profile, then choose its Nodes and routes.", checkForm: "Check form", formReady: "Profile is ready to save.", continueCreate: "Create and set up routes", created: "Inbound created. Choose a Node and its connection paths next.",
  nextSetup: "After saving, continue to Node and route setup. No listener is activated by creating the profile.", directEnabled: "Direct listener", swapEnabled: "Dark Swap destination listener", listenerHelp: "Choose the connections this Node should receive.", portRange: "Use separate ports from 1024 to 65535. Panel and Agent ports are reserved.", reservedPort: "2087, 8443 and 24443 are reserved. Choose another port.",
  unavailableTitle: "Inbound management is unavailable on this server version", unavailableHelp: "The server did not provide the Inbound management API. Update the PRO installation to use this editor.", legacyPreview: "Open the older route preview (experimental)",
  setupTitle: "Saved Inbound setup", setupHelp: "The complete profile and Node routes are saved and awaiting separate activation.", setupChanged: "The profile or assignments changed after this setup was saved. Open it to review the recorded configuration.", setupOpen: "Open saved setup", setupSaved: "The complete Inbound setup was saved paused. No listener or subscription was activated.", setupWaiting: "Some paths are waiting for Hosts.", setupUnavailable: "Setup capabilities could not be verified. Refresh before creating an Inbound.", setupClients: "This setup is paused. Client delivery follows separate activation.",
 },
 fa: {
  overview: "خلاصه", routes: "مسیرها", clients: "کلاینت‌ها", activity: "تاریخچه", diagnostics: "عیب‌یابی", back: "بازگشت به اینباندها", cancel: "انصراف", close: "بستن",
  listLead: "پروفایل‌ها، نودهای مقصد و دسترسی کلاینت‌ها را مدیریت کن.", loading: "در حال دریافت اینباندها…", missing: "این اینباند دیگر در دسترس نیست.",
  boundary: "اجرای مسیر و تحویل سرویس به مشتری، وضعیت تأیید جداگانه دارند.", swapBoundary: "ذخیره و بررسی پیش‌نویس دارک سواپ در دسترس است. عبور عملیاتی ترافیک و تحویل سرویس هنوز فعال نیست.",
  copy: "کپی تنظیمات", copyHint: "پروفایل عمومی در یک اینباند تازه کپی می‌شود؛ کلیدها تازه‌اند و کلاینت‌ها و نودهای مقصد کپی نمی‌شوند.",
  nodeCount: "نود مقصد", clientCount: "کلاینت متصل", profile: "پروفایل", targetAdd: "افزودن نود مقصد", targetEdit: "ویرایش مقصد", assigned: "نودها و پورت‌های مقصد",
  directPort: "مستقیم", swapPort: "پورت مقصد", none: "هنوز نودی به این اینباند اختصاص داده نشده است.", routeHelp: "ابتدا نود مقصد را مشخص کن؛ سپس پیش‌نویس مسیرش را در پایین بررسی و ذخیره کن.",
  searchClients: "جست‌وجو در همهٔ کلاینت‌ها", boundCount: "متصل به اینباند", clientsFailed: "فهرست کلاینت‌ها در دسترس نیست. دوباره به‌روزرسانی کن.",
  unsaved: "تغییرات ذخیره‌نشدهٔ این اینباند کنار گذاشته شوند؟", lab: "عیب‌یابی و آزمایش", labHelp: "آمادگی، تست‌های کنترل‌شده و گزارش اجرای ناوگان نودها.",
  editorLead: "پروفایل را بساز؛ سپس نودها و مسیرهایش را مشخص کن.", checkForm: "بررسی فرم", formReady: "پروفایل آمادهٔ ذخیره است.", continueCreate: "ساخت و تنظیم مسیر", created: "اینباند ساخته شد. حالا نود و مسیرهای اتصالش را مشخص کن.",
  nextSetup: "پس از ذخیره، وارد تنظیم نود و مسیر می‌شوی. ساخت پروفایل به‌تنهایی پورتی را فعال نمی‌کند.", directEnabled: "اتصال مستقیم", swapEnabled: "دریافت دارک‌سواپ", listenerHelp: "مشخص کن این نود کدام اتصال‌ها را دریافت کند.", portRange: "پورت‌های جدا از ۱۰۲۴ تا ۶۵۵۳۵ انتخاب کن؛ پورت پنل و Agent رزرو است.", reservedPort: "پورت‌های ۲۰۸۷، ۸۴۴۳ و ۲۴۴۴۳ رزرو هستند. پورت دیگری انتخاب کن.",
  unavailableTitle: "مدیریت اینباند در این نسخهٔ سرور در دسترس نیست", unavailableHelp: "سرور، رابط مدیریت اینباند را ارائه نکرد. برای استفاده از این ویرایشگر، نسخهٔ نصب‌شدهٔ PRO باید به‌روز شود.", legacyPreview: "بازکردن پیش‌نمایش قدیمی مسیر (آزمایشی)",
  setupTitle: "تنظیمات ذخیره‌شدهٔ اینباند", setupHelp: "پروفایل کامل و مسیرهای نودها ذخیره شده و منتظر فعال‌سازی جداگانه است.", setupChanged: "پروفایل یا اختصاص نودها پس از ذخیرهٔ این تنظیمات تغییر کرده؛ برای بازبینی پیکربندی ثبت‌شده آن را باز کن.", setupOpen: "بازکردن تنظیمات ذخیره‌شده", setupSaved: "تنظیمات کامل اینباند به‌صورت غیرفعال ذخیره شد؛ پورت یا اشتراکی فعال نشد.", setupWaiting: "بعضی مسیرها در انتظار هاست هستند.", setupUnavailable: "قابلیت ساخت یکپارچه تأیید نشد؛ پیش از ساخت اینباند دوباره به‌روزرسانی کن.", setupClients: "این تنظیمات غیرفعال است؛ تحویل سرویس به کلاینت پس از فعال‌سازی جداگانه انجام می‌شود.",
 },
} as const;
type DetailTab = "overview" | "routes" | "clients" | "activity";
const tabs: DetailTab[] = ["overview", "routes", "clients", "activity"];
function initialView(swapWorkspace: boolean): { id: number; tab: DetailTab } {
 const q = new URLSearchParams(location.search); const id = Number(q.get("inbound")); const tab = q.get("tab") as DetailTab;
 return { id: Number.isSafeInteger(id) && id > 0 ? id : 0, tab: tabs.includes(tab) ? tab : swapWorkspace ? "routes" : "overview" };
}
export default function NativeInbounds({ language, fallback, workspace = "inbounds" }: { language: Lang; fallback: ReactNode; workspace?: "inbounds" | "dark-swap" }) {
 const t = text[language]; const u = workspaceWords[language]; const swapWorkspace = workspace === "dark-swap";
 const { session, expire } = useSession(); const [disabled, setDisabled] = useState(false);
 const [data, setData] = useState<Overview | null>(null); const [clients, setClients] = useState<Client[]>([]); const [bound, setBound] = useState<string[]>([]);
 const [failure, setFailure] = useState(false); const [notice, setNotice] = useState(""); const [busy, setBusy] = useState(false);
 const [refresh, setRefresh] = useState(0); const [tick, setTick] = useState(Date.now()); const lastSuccess = useRef(0);
 const [chosen, setChosen] = useState(() => initialView(swapWorkspace).id);
 const [tab, setTab] = useState<DetailTab>(() => initialView(swapWorkspace).tab);
 const [visited, setVisited] = useState<DetailTab[]>(() => [initialView(swapWorkspace).tab]);
 const [diagnostics, setDiagnostics] = useState(false); const [creating, setCreating] = useState(false); const [copying, setCopying] = useState(false); const [targetOpen, setTargetOpen] = useState(false);
 const [diagnosticsVisited, setDiagnosticsVisited] = useState(false);
 const [canaryClientPresent, setCanaryClientPresent] = useState<boolean | null>(null);
 const [profileDirty, setProfileDirty] = useState(false); const [routeDirty, setRouteDirty] = useState(false);
 const [editorEpoch, setEditorEpoch] = useState(0);
 const [checked, setChecked] = useState<string[]>([]); const [offset, setOffset] = useState(0); const [search, setSearch] = useState(""); const [clientsReady, setClientsReady] = useState(false); const [clientsFailure, setClientsFailure] = useState(false);
 const [name, setName] = useState(""); const [transport, setTransport] = useState("tcp"); const [target, setTarget] = useState(""); const [sni, setSni] = useState(""); const [service, setService] = useState("");
 const [profileOptions, setProfileOptions] = useState<ProfileOptionsDraft>(profileOptionsDraft);
 const createIdentity = useRef<{ id: number; short: string } | null>(null);
 const profileForm = useRef<InboundProfileFormHandle>(null); const [formChecked, setFormChecked] = useState(false);
 const createInitial = useRef(""); const skipNavigationGuard = useRef(false);
 const [node, setNode] = useState(""); const [listen, setListen] = useState(""); const [direct, setDirect] = useState("0"); const [swap, setSwap] = useState("0");
 const [directEnabled, setDirectEnabled] = useState(true); const [swapEnabled, setSwapEnabled] = useState(false);
 const [setupCapabilities, setSetupCapabilities] = useState<CreationCapabilities | null>(null);
 const [setupPhase, setSetupPhase] = useState<"loading" | "available" | "legacy" | "error">("loading");
 const [setupSnapshot, setSetupSnapshot] = useState<string | null>(null);
 const [setupEditor, setSetupEditor] = useState<{ source?: Inbound; plan?: CreationPlan; referencesCurrent?: boolean } | null>(null);
 const [setupDirty, setSetupDirty] = useState(false); const [setupBusy, setSetupBusy] = useState(false);
 const setupLoad = useRef(0);
 const lifetime = useRef<AbortController | null>(null); const tabElements = useRef<(HTMLButtonElement | null)[]>([]);
 const createValue: InboundProfileValue = { name, transport, grpcService: service, realityTarget: target, serverNames: sni, options: profileOptions };
 const createDirty = creating && JSON.stringify(createValue) !== createInitial.current;
 const dirty = profileDirty || routeDirty || createDirty || setupDirty;
 const inboundFingerprint = data?.inbounds.map(inbound => inbound.id + ":" + inbound.revision).sort().join("|") ?? "";
 const setupStatusReady = setupPhase === "legacy" || setupPhase === "available" && setupSnapshot === inboundFingerprint;
 useEffect(() => { const c = new AbortController(); lifetime.current = c; const timer = setInterval(() => setTick(Date.now()), 1000); return () => { c.abort(); clearInterval(timer); }; }, []);
 useEffect(() => {
  if (!dirty) return;
  const prevent = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
  window.addEventListener("beforeunload", prevent); return () => window.removeEventListener("beforeunload", prevent);
 }, [dirty]);
 useEffect(() => registerNavigationGuard(destination => {
  const destinationID = Number(destination.searchParams.get("inbound"));
  const sameDetail = destination.pathname === "/" + workspace && destinationID === chosen;
  return skipNavigationGuard.current || !setupBusy && (!dirty || (sameDetail && !createDirty && !setupDirty) || window.confirm(u.unsaved));
 }), [dirty, createDirty, setupDirty, setupBusy, chosen, workspace, u.unsaved]);
 function applyView(id: number, nextTab: DetailTab) {
  if (id !== chosen) {
   setProfileDirty(false); setRouteDirty(false); setVisited([nextTab]); setChecked([]); setBound([]); setClientsReady(false);
   setOffset(0); setSearch(""); setNotice(""); setCreating(false); setTargetOpen(false); setSetupEditor(null); setSetupDirty(false); setupLoad.current++;
  } else setVisited(old => old.includes(nextTab) ? old : [...old, nextTab]);
  setChosen(id); setTab(nextTab);
 }
 useEffect(() => {
  const sync = () => { const view = initialView(swapWorkspace); applyView(view.id, view.tab); };
  window.addEventListener("popstate", sync); return () => window.removeEventListener("popstate", sync);
 }, [swapWorkspace, chosen]);
 function navigateDetail(id: number, nextTab: DetailTab, replace = false) {
  const params = new URLSearchParams(location.search);
  if (id) { params.set("inbound", String(id)); params.set("tab", nextTab); } else { params.delete("inbound"); params.delete("tab"); }
  const query = params.toString();
  if (navigateURL(location.pathname + (query ? "?" + query : ""), replace)) applyView(id, nextTab);
 }
 function switchTab(next: DetailTab) { navigateDetail(chosen, next, true); }
 function tabKey(event: KeyboardEvent<HTMLButtonElement>, index: number) {
  let next = index;
  if (event.key === "Home") next = 0; else if (event.key === "End") next = tabs.length - 1;
  else if (event.key === "ArrowRight") next = (index + (language === "fa" ? -1 : 1) + tabs.length) % tabs.length;
  else if (event.key === "ArrowLeft") next = (index + (language === "fa" ? 1 : -1) + tabs.length) % tabs.length; else return;
  event.preventDefault(); switchTab(tabs[next]); tabElements.current[next]?.focus();
 }
 useEffect(() => {
  if (!session?.authenticated || session.user?.role !== "owner") return;
  const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>;
  async function poll() {
   try {
    const overview = await request<Overview>("/api/v1/workspace/native-inbounds-state", controller.signal);
    if (controller.signal.aborted) return;
    if (![overview.inbounds, overview.nodes, overview.targets, overview.runtime].every(Array.isArray)) throw new Error("Invalid native response");
    setData(overview); setFailure(false); setDisabled(false); lastSuccess.current = Date.now();
   } catch (e) {
    if (controller.signal.aborted) return;
    if (e instanceof Fault && e.status === 401) { expire(); setData(null); }
    else if (e instanceof Fault && e.status === 404) { setDisabled(true); setData(null); } else setFailure(true);
   } finally { if (!controller.signal.aborted) timer = setTimeout(() => { void poll(); }, document.hidden ? 10000 : 5000); }
  }
  void poll(); return () => { controller.abort(); clearTimeout(timer); };
 }, [session, refresh, expire]);
 useEffect(() => {
  if (!session?.authenticated || session.user.role !== "owner") return;
  const controller = new AbortController();
  void request<CreationCapabilities | { available: false; runtime_apply: false }>("/api/v1/workspace/native-inbound-plans/capabilities", controller.signal).then(result => {
   if (controller.signal.aborted) return;
   if (result?.available === false && result.runtime_apply === false) { setSetupCapabilities(null); setSetupPhase("legacy"); setSetupSnapshot(inboundFingerprint); return; }
   if (!result || result.available !== true || result.version !== 1 || result.runtime_apply !== false || !Array.isArray(result.plans) || !Array.isArray(result.transports) || !result.transports.includes("tcp") || !result.protocols?.includes("vless") || !result.securities?.includes("reality")) throw new Error("invalid_setup_capability");
   setSetupCapabilities(result); setSetupPhase("available"); setSetupSnapshot(inboundFingerprint);
  }).catch(error => {
   if (controller.signal.aborted) return;
   if (error instanceof Fault && error.status === 404) { setSetupCapabilities(null); setSetupPhase("legacy"); setSetupSnapshot(inboundFingerprint); }
   else { setSetupPhase("error"); if (error instanceof Fault && error.status === 401) expire(); }
  });
  return () => controller.abort();
 }, [session, refresh, expire, inboundFingerprint]);
 useEffect(() => {
  if (!session?.authenticated || session.user?.role !== "owner" || !chosen || tab !== "clients") return;
  const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>; setClientsReady(false); setClientsFailure(false);
  async function poll() {
   try {
    const query = new URLSearchParams({ limit: "100", offset: String(offset), q: search });
    const [list, binding] = await Promise.all([
     request<{ clients: Client[] }>("/api/v1/workspace/clients?" + query, controller.signal),
     chosen ? request<{ client_ids: string[] }>("/api/v1/workspace/native-inbounds/" + chosen + "/clients", controller.signal) : Promise.resolve({ client_ids: [] }),
    ]);
    if (controller.signal.aborted) return;
    if (!Array.isArray(list.clients) || !Array.isArray(binding.client_ids)) throw new Error("Invalid clients response");
    setClients(list.clients); setBound(binding.client_ids); setClientsReady(true); setClientsFailure(false);
   } catch (e) {
    if (controller.signal.aborted) return;
    setClientsReady(false); setClientsFailure(true);
    if (e instanceof Fault && e.status === 401) expire();
   } finally { if (!controller.signal.aborted) timer = setTimeout(() => { void poll(); }, 5000); }
  }
  void poll(); return () => { controller.abort(); clearTimeout(timer); };
 }, [session, chosen, offset, search, tab, refresh, expire]);
 useEffect(() => {
  if (!session?.authenticated || session.user?.role !== "owner" || !diagnostics) return;
  const controller = new AbortController(); let timer: ReturnType<typeof setTimeout>; setCanaryClientPresent(null);
  async function pollCanary() {
   try {
    // The canary's identity check must not depend on the paged client selector.
    const result = await request<{ clients: Client[] }>("/api/v1/workspace/clients?limit=25&offset=0&q=pro-usa-qa01", controller.signal);
    if (!Array.isArray(result.clients)) throw new Error("Invalid canary client response");
    if (!controller.signal.aborted) setCanaryClientPresent(result.clients.some(client => client.id === "pro-usa-qa01"));
   } catch (error) {
    if (controller.signal.aborted) return;
    setCanaryClientPresent(null); if (error instanceof Fault && error.status === 401) expire();
   } finally { if (!controller.signal.aborted) timer = setTimeout(() => { void pollCanary(); }, 5000); }
  }
  void pollCanary(); return () => { controller.abort(); clearTimeout(timer); };
 }, [session, diagnostics, refresh, expire]);
 async function mutate(path: string, body: unknown, after: () => void, successMessage = t.saved) {
  const controller = lifetime.current; if (!controller || controller.signal.aborted || busy) return; setBusy(true); setNotice("");
  try { await request(path, controller.signal, body, session?.csrf_token); if (!controller.signal.aborted) { after(); setNotice(successMessage); setRefresh(n => n + 1); } }
  catch (error) { if (!controller.signal.aborted) { if (error instanceof Fault && error.status === 401) expire(); setFailure(true); setNotice(t.error); } }
  finally { if (!controller.signal.aborted) setBusy(false); }
 }
 function openCreate(source?: Inbound) {
  if (!setupStatusReady) { setNotice(u.setupUnavailable); return; }
  if (dirty) {
   if (!window.confirm(u.unsaved)) return;
   setProfileDirty(false); setRouteDirty(false); setEditorEpoch(value => value + 1);
  }
  const suffix = language === "fa" ? " — کپی" : " — copy";
  if (setupPhase === "available" && setupCapabilities) {
   setCreating(false); setNotice(""); setSetupEditor({ source: source ? { ...source, name: source.name.slice(0, 128 - suffix.length) + suffix } : undefined }); return;
  }
  createIdentity.current = null; setCopying(!!source); setName(source ? source.name.slice(0, 128 - suffix.length) + suffix : "");
  setTransport(source?.transport ?? "tcp"); setTarget(source?.reality_target ?? ""); setSni(source?.server_names.join(", ") ?? ""); setService(source?.grpc_service ?? ""); setProfileOptions(profileOptionsDraft(source?.options)); setCreating(true);
  createInitial.current = JSON.stringify({ name: source ? source.name.slice(0, 128 - suffix.length) + suffix : "", transport: source?.transport ?? "tcp", grpcService: source?.grpc_service ?? "", realityTarget: source?.reality_target ?? "", serverNames: source?.server_names.join(", ") ?? "", options: profileOptionsDraft(source?.options) });
  setFormChecked(false); setNotice("");
 }
 async function openSetupPlan() {
  if (!chosen || busy || setupBusy) return;
  if ((profileDirty || routeDirty) && !window.confirm(u.unsaved)) return;
  const controller = lifetime.current; if (!controller || controller.signal.aborted) return;
  const generation = ++setupLoad.current, id = chosen;
  setBusy(true); setNotice("");
  try {
   const result = await request<CreationPlanResponse>("/api/v1/workspace/native-inbound-plans/" + id, controller.signal);
   if (!result?.plan || result.plan.profile?.id !== id || result.plan.runtime_apply !== false || !Array.isArray(result.plan.nodes) || typeof result.plan.editable !== "boolean" || typeof result.references_current !== "boolean") throw new Error("invalid_setup_plan");
   if (!controller.signal.aborted && generation === setupLoad.current) { setProfileDirty(false); setRouteDirty(false); setEditorEpoch(value => value + 1); setSetupEditor({ plan: result.plan, referencesCurrent: result.references_current }); }
  } catch (error) { if (!controller.signal.aborted && generation === setupLoad.current) { setNotice(t.error); if (error instanceof Fault && error.status === 401) expire(); } }
  finally { if (!controller.signal.aborted) setBusy(false); }
 }
 function savedSetup(plan: CreationPlan) {
  setSetupEditor(null); setSetupDirty(false); setSetupBusy(false); setProfileDirty(false); setRouteDirty(false);
  setData(previous => previous ? { ...previous, inbounds: [...previous.inbounds.filter(item => item.id !== plan.profile.id), plan.profile] } : previous);
  setSetupCapabilities(previous => previous ? { ...previous, plans: [...previous.plans.filter(item => item.inbound_id !== plan.profile.id), { inbound_id: plan.profile.id, plan_revision: plan.plan_revision, inbound_revision: plan.inbound_revision, status: plan.status }] } : previous);
  skipNavigationGuard.current = true; navigateDetail(plan.profile.id, "routes"); skipNavigationGuard.current = false;
  setNotice(u.setupSaved); setRefresh(value => value + 1);
 }
 function changeCreate(value: InboundProfileValue) { setName(value.name); setTransport(value.transport); setService(value.grpcService); setTarget(value.realityTarget); setSni(value.serverNames); setProfileOptions(value.options ?? profileOptionsDraft()); setFormChecked(false); }
 function closeCreate() { if (!busy && (!createDirty || window.confirm(u.unsaved))) setCreating(false); }
 function create(event: FormEvent) {
  event.preventDefault(); if (busy || stale || !setupStatusReady || !profileForm.current?.validate()) return;
  if (!createIdentity.current) createIdentity.current = { id: freshID(), short: shortID() }; const identity = createIdentity.current;
  void mutate("/api/v1/workspace/native-inbounds", { id: identity.id, name, transport, grpc_service: transport === "grpc" ? service : "", reality_target: target, server_names: split(sni), short_ids: [identity.short], options: profileOptionsPayload(profileOptions, transport) }, () => {
   setCreating(false); createIdentity.current = null; setProfileDirty(false); setRouteDirty(false);
   skipNavigationGuard.current = true; navigateDetail(identity.id, "routes"); skipNavigationGuard.current = false;
  }, u.created);
 }
 function pickNode(id: string) {
  setNode(id); const existing = data?.targets.find(x => x.node_id === id && x.inbound_id === chosen);
  setListen(existing?.listen_ip ?? ""); setDirect(existing?.direct_port ? String(existing.direct_port) : ""); setSwap(existing?.swap_port ? String(existing.swap_port) : "");
  setDirectEnabled(existing ? existing.direct_port > 0 : true); setSwapEnabled((existing?.swap_port ?? 0) > 0); setNotice("");
 }
 function saveTarget(event: FormEvent) {
  event.preventDefault(); const n = data?.nodes.find(x => x.node_id === node); if (!setupStatusReady || !n || !chosen || !window.confirm(t.confirm)) return;
  void mutate("/api/v1/workspace/native-inbounds/" + chosen + "/target", { inbound_id: chosen, node_id: node, installation_id: n.installation_id, listen_ip: listen, direct_port: directEnabled ? Number(direct) : 0, swap_port: swapEnabled ? Number(swap) : 0 }, () => setTargetOpen(false));
 }
 const sharedNodePort = directEnabled && swapEnabled && Number(direct) > 0 && Number(swap) > 0 && Number(direct) === Number(swap);
 const reservedNodePort = (directEnabled && [2087, 8443, 24443].includes(Number(direct))) || (swapEnabled && [2087, 8443, 24443].includes(Number(swap)));
 const stale = failure || tick - lastSuccess.current > 12000;
 function matching(s: Status): boolean {
  const expires = Date.parse(s.lease_expires_at ?? ""); const received = Date.parse(s.reported_at ?? "");
  return !stale && s.state === "applied" && s.process_ready && s.desired_sequence > 0 && s.desired_sequence === s.reported_sequence && s.desired_sha === s.reported_sha && !!s.desired_sha &&
   Number.isFinite(expires) && expires > tick && Number.isFinite(received) && received <= tick + 1000 && tick - received <= 12000;
 }
 function statusLabel(s: Status): string {
  if (stale || (s.state === "applied" && !matching(s)) || (s.lease_expires_at && Date.parse(s.lease_expires_at) <= tick)) return t.stale;
  return ({ applied: t.applied, awaiting_report: t.waiting, stale: t.stale, configuration_changed: t.changed, stopped: t.stopped, listeners_disabled: t.listenersDisabled, out_of_sync: t.mismatch } as Record<string, string>)[s.state] ?? t.waiting;
 }
 const current = data?.inbounds.find(i => i.id === chosen); const assigned = data?.targets.filter(item => item.inbound_id === chosen) ?? [];
 const setupSummary = setupCapabilities?.plans.find(plan => plan.inbound_id === chosen);
 const setupUnchanged = !!setupSummary && !!current && !current.enabled && !current.archived && current.bound_clients === 0 && assigned.length === 0 && current.revision === setupSummary.inbound_revision;
 if (disabled) return <section className="native-workspace inbound-capability-missing"><div className="native-card"><Server size={24}/><h2>{u.unavailableTitle}</h2><p>{u.unavailableHelp}</p><button type="button" onClick={() => setRefresh(n => n + 1)}><RefreshCw size={16}/>{t.retry}</button></div><details><summary>{u.legacyPreview}</summary>{fallback}</details></section>;
 return <section className="native-workspace" aria-label="Native Inbounds">
  <div className="inbound-workspace-bar"><p>{swapWorkspace ? u.swapBoundary : u.boundary}</p><div>
   <button type="button" onClick={() => setRefresh(n => n + 1)} disabled={busy} aria-label={t.retry}><RefreshCw size={16}/><span>{t.retry}</span></button>
   <button type="button" aria-expanded={diagnostics} aria-controls="inbound-diagnostics" onClick={() => { setDiagnosticsVisited(true); setDiagnostics(value => !value); }}><SlidersHorizontal size={16}/>{u.diagnostics}</button>
  </div></div>
  {failure && <p className="native-warning" role="alert">{t.error}</p>}
  {setupPhase === "error" && <p className="native-warning" role="alert">{u.setupUnavailable}</p>}
  {!data && !failure && <p role="status">{u.loading}</p>}
  {session?.authenticated && session.user?.role !== "owner" && <p className="native-warning">{t.ownerOnly}</p>}
  {session?.authenticated && session.user?.role === "owner" && data && <>
   <div hidden={chosen > 0}><InboundDirectory data={data} language={language} blocked={busy || stale || !setupStatusReady} swapWorkspace={swapWorkspace}
    onOpen={item => navigateDetail(item.id, swapWorkspace ? "routes" : "overview")} onCreate={openCreate}/></div>
   {chosen > 0 && <>
    <div className="inbound-detail-heading"><button type="button" className="inbound-back" onClick={() => navigateDetail(0, "overview")}><ChevronLeft size={17}/>{u.back}</button>
     {current && <div><div><h2 tabIndex={-1} data-workspace-focus>{current.name}</h2><bdi dir="ltr">#{current.id}</bdi></div><button type="button" disabled={busy || stale || !setupStatusReady} onClick={() => openCreate(current)}><Copy size={16}/>{u.copy}</button></div>}
    </div>
    {!current ? <p className="native-warning">{u.missing}</p> : <>
     <nav className="inbound-detail-tabs" role="tablist" aria-label="Inbound sections">{tabs.map((item, index) => <button key={item} ref={element => { tabElements.current[index] = element; }} role="tab" id={"inbound-tab-" + item} aria-controls={"inbound-panel-" + item} aria-selected={tab === item} tabIndex={tab === item ? 0 : -1} onKeyDown={event => tabKey(event, index)} onClick={() => switchTab(item)}>{u[item]}</button>)}</nav>
     {setupSummary && (tab === "overview" || tab === "routes") && <section className="native-card creation-saved-plan"><ShieldCheck size={21}/><div><h3>{u.setupTitle}</h3><p>{setupUnchanged ? u.setupHelp : u.setupChanged}</p>{setupSummary.status === "WAITING_HOST" && <p className="creation-waiting">{u.setupWaiting}</p>}</div><button type="button" disabled={busy || setupBusy} onClick={() => void openSetupPlan()}>{u.setupOpen}</button></section>}
     <section hidden={tab !== "overview"} role="tabpanel" id="inbound-panel-overview" aria-labelledby="inbound-tab-overview" className="inbound-detail-panel">
      {visited.includes("overview") && <>
       <div className="inbound-summary"><div><span>{u.nodeCount}</span><strong>{assigned.length}</strong></div><div><span>{u.clientCount}</span><strong>{current.bound_clients}</strong></div><div><span>{u.profile}</span><strong>{current.archived ? t.archivedLabel : current.enabled ? t.enabledLabel : t.disabledLabel}</strong></div></div>
       <dl className="inbound-profile-facts"><div><dt>{t.transport}</dt><dd><bdi dir="ltr">{current.transport === "grpc" ? "gRPC" : "TCP"} / REALITY</bdi></dd></div><div><dt>{t.dest}</dt><dd><bdi dir="ltr">{current.reality_target}</bdi></dd></div><div><dt>{t.sni}</dt><dd><bdi dir="ltr">{current.server_names.join(", ")}</bdi></dd></div></dl>
       <InboundLifecycle key={chosen + ":" + editorEpoch + ":" + !!setupSummary} inbound={current} language={language} csrf={session.csrf_token ?? ""} blocked={busy || stale || !setupStatusReady} mode="controls" profileOptionsSupported={setupCapabilities?.profile_options === true} setupOnly={!!setupSummary} onDirtyChange={setProfileDirty} onChanged={() => setRefresh(n => n + 1)}/>
      </>}
     </section>
     <section hidden={tab !== "routes"} role="tabpanel" id="inbound-panel-routes" aria-labelledby="inbound-tab-routes" className="inbound-detail-panel">
      {visited.includes("routes") && !setupSummary && <>
       <section className="native-card inbound-targets"><div className="inbound-card-heading"><h3><Server size={19}/>{u.assigned}</h3><button type="button" disabled={busy || stale || !setupStatusReady || current.archived} onClick={() => { pickNode(""); setTargetOpen(true); }}><Plus size={16}/>{u.targetAdd}</button></div><p>{u.routeHelp}</p>
        {!assigned.length && <p>{u.none}</p>}
        {assigned.map(item => <article className="inbound-target-row" key={item.node_id}><div><strong>{data.nodes.find(n => n.node_id === item.node_id)?.name ?? item.node_id}</strong><bdi dir="ltr">{item.listen_ip}</bdi></div><span>{u.directPort}: <bdi dir="ltr">{item.direct_port || "—"}</bdi></span><span>{u.swapPort}: <bdi dir="ltr">{item.swap_port || "—"}</bdi></span><button type="button" disabled={busy || stale || !setupStatusReady || current.archived} onClick={() => { pickNode(item.node_id); setTargetOpen(true); }}>{u.targetEdit}</button></article>)}
       </section>
       <NativeRouteDraftEditor key={chosen + ":" + editorEpoch} inboundID={chosen} inboundRevision={current.revision} nodes={data.nodes} targets={data.targets} language={language} csrf={session.csrf_token ?? ""} blocked={busy || stale || !setupStatusReady || current.archived} onDirtyChange={setRouteDirty}/>
      </>}
     </section>
     <section hidden={tab !== "clients"} role="tabpanel" id="inbound-panel-clients" aria-labelledby="inbound-tab-clients" className="inbound-detail-panel">
      {visited.includes("clients") && (setupSummary ? <div className="native-card"><h3><Users size={19}/>{t.members}</h3><p>{setupUnchanged ? u.setupClients : u.setupChanged}</p></div> : <div className="native-card"><h3><Users size={19}/>{t.members}</h3><p>{t.bindingHelp}</p><span className="inbound-result-count">{u.boundCount}: {clientsReady ? bound.length : "—"}</span>
       <label>{u.searchClients}<input value={search} onChange={event => { setSearch(event.target.value); setOffset(0); setChecked([]); }}/></label>
       {clientsFailure && <p className="native-warning" role="alert">{u.clientsFailed}</p>}
       {!clientsReady && !clientsFailure && <p role="status">{u.loading}</p>}
       <div className="native-client-list" aria-busy={!clientsReady}>{clientsReady && clients.map(c => <label key={c.id} className="native-client-row"><input type="checkbox" checked={checked.includes(c.id)} onChange={event => setChecked(old => event.target.checked ? [...old, c.id] : old.filter(id => id !== c.id))}/><strong>{c.name}</strong><span>{c.id} · {c.representative_id}</span>{bound.includes(c.id) && <CheckCircle2 size={17} aria-label="Bound"/>}</label>)}{clientsReady && !clients.length && <p>{t.noClients}</p>}</div>
       <div className="native-actions"><button type="button" disabled={!clientsReady} onClick={() => setChecked(clients.map(c => c.id))}>{t.all}</button><button type="button" disabled={busy || stale || !setupStatusReady || !clientsReady || current.archived || !checked.length || checked.some(id => clients.find(c => c.id === id)?.archived)} onClick={() => void mutate("/api/v1/workspace/native-inbounds/" + chosen + "/clients", { client_ids: checked, attach: true }, () => setChecked([]))}>{t.add}</button><button type="button" disabled={busy || stale || !setupStatusReady || !clientsReady || !checked.length} onClick={() => void mutate("/api/v1/workspace/native-inbounds/" + chosen + "/clients", { client_ids: checked, attach: false }, () => setChecked([]))}>{t.remove}</button></div>
       <div className="native-actions"><button type="button" disabled={offset === 0 || busy} onClick={() => { setOffset(n => Math.max(0, n - 100)); setChecked([]); }}>{t.previous}</button><button type="button" disabled={!clientsReady || clients.length < 100 || busy} onClick={() => { setOffset(n => n + 100); setChecked([]); }}>{t.next}</button></div>
      </div>)}
     </section>
     <section hidden={tab !== "activity"} role="tabpanel" id="inbound-panel-activity" aria-labelledby="inbound-tab-activity" className="inbound-detail-panel">{visited.includes("activity") && <InboundLifecycle key={chosen} inbound={current} language={language} csrf={session.csrf_token ?? ""} blocked={busy || stale} mode="history" onChanged={() => setRefresh(n => n + 1)}/>}</section>
    </>}
   </>}
   <section id="inbound-diagnostics" hidden={!diagnostics} className="inbound-diagnostics" aria-label="Inbound diagnostics">
    {diagnosticsVisited && <><header><h2>{u.lab}</h2><p>{u.labHelp}</p></header><NativeReadiness preflight={data.preflight} stale={stale} language={language}/>
     <NativeCanary language={language} hasUSA={data.nodes.some(n => n.node_id === "usa-pro-01")} staged={data.inbounds.some(i => i.id === 901617) && data.targets.some(target => target.inbound_id === 901617 && target.node_id === "usa-pro-01") && canaryClientPresent === true} unavailable={busy || stale || canaryClientPresent === null} busy={busy} onStage={() => void mutate("/api/v1/workspace/native-canary/stage", { confirmation: "USA_PRO_LOOPBACK_QA_ONLY" }, () => navigateDetail(901617, "overview"))}/>
     <NativeRouteTransactions language={language}/><NativeMeteredRouteTransactions language={language}/>
     <div className="native-card"><h3><ArrowRightLeft size={19}/>{t.status}</h3><p>{t.evidence}</p><div className="native-report-list">{data.runtime.map(report => <article key={report.node_id + report.installation_id} className="native-report"><h4>{data.nodes.find(n => n.node_id === report.node_id)?.name ?? report.node_id}</h4><strong className={matching(report) ? "native-confirmed" : "native-pending"}>{statusLabel(report)}</strong><div><span>{t.desired}: {report.desired_sequence || "—"}</span><span>{t.observed}: {report.reported_sequence || "—"}</span></div><p>{t.phase}: {report.phase || "—"}</p><p>{t.last}: {report.reported_at ? new Date(report.reported_at).toLocaleTimeString(language === "fa" ? "fa-IR" : "en-GB") : "—"}</p></article>)}</div></div>
    </>}
   </section>
   {setupEditor && setupCapabilities && <InboundCreation language={language} nodes={data.nodes} targets={data.targets} capabilities={setupCapabilities} csrf={session.csrf_token ?? ""} blocked={stale || !setupStatusReady} source={setupEditor.source} initialPlan={setupEditor.plan} referencesCurrent={setupEditor.referencesCurrent} onClose={() => setSetupEditor(null)} onSaved={savedSetup} onDirtyChange={setSetupDirty} onBusyChange={setSetupBusy} onUnauthorized={expire}/>}
   <WorkspaceDialog open={creating} title={t.create} subtitle={u.editorLead} variant="inbound-editor" closeLabel={u.close} busy={busy} onClose={closeCreate}><form onSubmit={create} noValidate className="inbound-editor-form">
    <div className="inbound-editor-scroll">
     {copying && <p className="inbound-dialog-note inbound-copy-note">{u.copyHint}</p>}
     <InboundProfileForm ref={profileForm} language={language} value={createValue} onChange={changeCreate} disabled={busy} profileOptionsSupported={setupCapabilities?.profile_options === true}>
      <div className="inbound-next-step"><Server size={18}/><p>{u.nextSetup}</p></div>
     </InboundProfileForm>
     {notice && <p className="native-warning" role="alert">{notice}</p>}
    </div>
    <footer className="inbound-editor-footer">
     {formChecked && <p role="status"><CheckCircle2 size={15}/>{u.formReady}</p>}
     <div><button type="button" disabled={busy} onClick={closeCreate}>{u.cancel}</button><button type="button" disabled={busy} onClick={() => setFormChecked(profileForm.current?.validate() ?? false)}>{u.checkForm}</button><button className="native-primary" type="submit" disabled={busy || stale || !setupStatusReady}>{u.continueCreate}</button></div>
    </footer>
   </form></WorkspaceDialog>
   <WorkspaceDialog open={targetOpen} title={t.nodes} subtitle={u.listenerHelp} closeLabel={u.close} busy={busy} onClose={() => setTargetOpen(false)}><form onSubmit={saveTarget}>
    {!data.nodes.length && <p className="native-warning">{t.noNodes}</p>}
    <label>{t.node}<select data-initial-focus value={node} onChange={event => pickNode(event.target.value)} required><option value="">{t.choose}</option>{data.nodes.map(n => <option value={n.node_id} key={n.node_id}>{data.nodes.filter(other => other.name === n.name).length > 1 ? `${n.name} (${n.node_id})` : n.name}</option>)}</select></label><label>{t.listen}<input dir="ltr" value={listen} onChange={event => setListen(event.target.value)} required placeholder="203.0.113.10"/></label>
    <div className="inbound-listener-options">
     <div data-enabled={directEnabled}><label className="inbound-listener-switch"><input type="checkbox" checked={directEnabled} onChange={event => setDirectEnabled(event.target.checked)}/><span>{u.directEnabled}</span><bdi>DIRECT</bdi></label>{directEnabled && <label>{language === "fa" ? "پورت مستقیم" : "Direct port"}<input aria-label={t.direct} dir="ltr" type="number" min={1024} max={65535} step={1} value={direct} onChange={event => setDirect(event.target.value)} required placeholder="10443"/></label>}</div>
     <div data-enabled={swapEnabled}><label className="inbound-listener-switch"><input type="checkbox" checked={swapEnabled} onChange={event => setSwapEnabled(event.target.checked)}/><span>{u.swapEnabled}</span><bdi>SWAP</bdi></label>{swapEnabled && <label>{language === "fa" ? "پورت مقصد دارک‌سواپ" : "Dark Swap destination port"}<input aria-label={t.swap} dir="ltr" type="number" min={1024} max={65535} step={1} value={swap} onChange={event => setSwap(event.target.value)} required placeholder="18443"/></label>}</div>
    </div>
    <p className="inbound-dialog-note">{u.portRange}</p>
    {sharedNodePort && <p className="native-warning" role="alert">{t.shared}</p>}
    {reservedNodePort && <p className="native-warning" role="alert">{u.reservedPort}</p>}
    {notice && <p className="native-warning" role="alert">{notice}</p>}
    <div className="native-dialog-actions"><button type="button" disabled={busy} onClick={() => setTargetOpen(false)}>{u.cancel}</button><button type="submit" disabled={busy || stale || !setupStatusReady || !current || current.archived || !node || sharedNodePort || reservedNodePort || (!directEnabled && !swapEnabled)}>{t.targetSave}</button></div>
   </form></WorkspaceDialog>
  </>}
  {notice && <p className="native-notice" role="status">{notice}</p>}
 </section>;
}
