import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Bot, Check, ChevronLeft, ExternalLink, KeyRound, Pause, Play, Plus, RefreshCw, Search, ShieldCheck, Store, Wallet, X } from "lucide-react";
import { api, ApiError, useSession, type Language } from "./session";
import type { Page } from "./navigation";
import "./TelegramWorkspace.css";

type StoreSetup = {
  account_enabled: boolean; manager_registered: boolean;
  can_create_clients: boolean; can_renew_clients: boolean;
  published_products: number; priced_products: number; catalog_limit: number;
  trial_enabled: boolean; trial_duration_hours: 24; trial_starts_at: "issuance";
};
type BotStore = {
  id: string; bot_id: number; representative_id: string; name: string; manager_user_id: number;
  enabled: boolean; revision: number; connection_state: string; last_poll_unix: number;
  credential_configured?: boolean; bot_username?: string; connection_problem?: string;
  queue_status_available: boolean; inbox_pending: number; financial_retry: number;
  review_pending: number; outbox_pending: number; outbox_unknown: number; delivery_review: number;
  setup?: StoreSetup;
};
type Workspace = {
  enabled: boolean; can_configure: boolean; can_manage_connections?: boolean;
  runtime_revision?: number; stores: BotStore[];
};
type StoreForm = {
  id: string; bot: string; representative: string; name: string; manager: string;
  enabled: boolean; revision: number;
};
type BotIdentity = { bot_id: number; username: string };
type Verification = BotIdentity & { inputVersion: number };
type ConnectionReceipt = { revision: number; bot_id: number; username: string; state: string };
type Representative = { id: string; display_name: string; kind: string; enabled: boolean };
type NoticeCode = "saved" | "connected" | "checked" | "paused" | "resumed" | "invalid" | "verify_first" |
  "wrong_bot" | "invalid_token" | "webhook" | "conflict" | "denied" | "capacity" | "busy" |
  "unavailable" | "failed" | "partial" | "unconfirmed";
type Notice = { code: NoticeCode; store?: string };
const base = "/api/v1/workspace/telegram";
const idPattern = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$/;
const usernamePattern = /^[A-Za-z][A-Za-z0-9_]{0,31}$/;
const tokenPattern = /^[1-9][0-9]{0,19}:[A-Za-z0-9_-]{20,200}$/;
const positiveID = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value > 0;
const digits = (value: string) => value.replace(/[۰-۹٠-٩]/g, digit => String(digit.charCodeAt(0) - (digit <= "٩" ? 0x0660 : 0x06f0)));
const emptyForm = (): StoreForm => ({ id: "", bot: "", representative: "owner", name: "", manager: "", enabled: true, revision: 0 });
const states: Record<string, [string, string]> = {
  not_configured: ["Connection not configured", "اتصال تنظیم نشده"],
  configured: ["Saved; waiting to connect", "ذخیره شده؛ در انتظار اتصال"],
  starting: ["Checking bot identity", "در حال بررسی هویت ربات"],
  identity_verified: ["Identity verified; starting", "هویت تأیید شده؛ در حال شروع"],
  polling: ["Receiving updates", "در حال دریافت پیام‌ها"],
  error: ["Connection needs attention", "اتصال نیاز به بررسی دارد"],
  stopped: ["Connection stopped", "دریافت پیام متوقف است"],
  store_disabled: ["Store paused", "فروشگاه متوقف است"],
};
const noticeWords: Record<NoticeCode, [string, string]> = {
  saved: ["Store settings saved.", "تنظیمات فروشگاه ذخیره شد."],
  connected: ["Connection settings saved. Follow the live status below, then open the bot and send /start to try its menu.", "تنظیمات اتصال ذخیره شد. وضعیت زنده را پایین ببینید؛ سپس ربات را باز کنید و برای آزمودن منو /start بفرستید."],
  checked: ["Telegram verified this bot's identity and connection settings. Receiving updates and a working service are shown separately.", "تلگرام هویت و تنظیمات اتصال این ربات را تأیید کرد. دریافت پیام‌ها و آماده‌بودن سرویس جداگانه بررسی می‌شوند."],
  paused: ["Store paused. Existing payment records continue to be processed.", "فروشگاه متوقف شد. پردازش سوابق پرداخت‌های موجود ادامه دارد."],
  resumed: ["Store resumed. Its connection status is shown below.", "فروشگاه دوباره فعال شد. وضعیت اتصال آن را پایین ببینید."],
  invalid: ["Enter a store name, a valid account and the manager's numeric Telegram ID.", "نام فروشگاه، حساب فروشگاه و شناسهٔ عددی مدیر در تلگرام را درست وارد کنید."],
  verify_first: ["Verify the bot token before saving this connection.", "پیش از ذخیرهٔ اتصال، توکن ربات را بررسی کنید."],
  wrong_bot: ["This token belongs to a different bot. Use the token for this store's bot.", "این توکن متعلق به ربات دیگری است. توکن ربات همین فروشگاه را وارد کنید."],
  invalid_token: ["Telegram did not accept this bot token. Check the token from BotFather and try again.", "تلگرام این توکن را نپذیرفت. توکن دریافتی از BotFather را بررسی و دوباره امتحان کنید."],
  webhook: ["This bot is connected to another service through a webhook. Use a dedicated bot or disconnect it from that service first.", "این ربات از طریق وب‌هوک به سرویس دیگری وصل است. ربات جداگانه‌ای انتخاب کنید یا ابتدا اتصال آن سرویس را قطع کنید."],
  conflict: ["The settings changed in another session. The latest status has been requested; review it before trying again.", "تنظیمات در نشست دیگری تغییر کرده است. آخرین وضعیت دوباره درخواست شد؛ پیش از تلاش دوباره آن را بررسی کنید."],
  denied: ["Your session cannot perform this action. Sign in with the owner account.", "این نشست اجازهٔ انجام این کار را ندارد. با حساب مالک وارد شوید."],
  capacity: ["The configured bot limit has been reached.", "تعداد ربات‌های تنظیم‌شده به سقف مجاز رسیده است."],
  busy: ["A connection check is already running or Telegram asked us to wait. Try again shortly.", "بررسی دیگری در حال انجام است یا تلگرام زمان بیشتری خواسته است. کمی بعد دوباره امتحان کنید."],
  unavailable: ["The bot connection could not be checked right now. Refresh its status and try again.", "در حال حاضر بررسی اتصال ربات ممکن نشد. وضعیت را به‌روزرسانی و دوباره امتحان کنید."],
  failed: ["The save was not confirmed. Retry the same settings or refresh to see the latest version.", "ذخیره تأیید نشد. همین تنظیمات را دوباره ارسال کنید یا آخرین وضعیت را به‌روزرسانی کنید."],
  partial: ["The store was saved, but its connection was not confirmed. You can finish connecting this same store below.", "فروشگاه ذخیره شد، اما اتصال آن تأیید نشد. اتصال همین فروشگاه را از این بخش تکمیل کنید."],
  unconfirmed: ["The response was not confirmed. Refresh the status before trying again.", "پاسخ تأیید نشد. پیش از تلاش دوباره، وضعیت را به‌روزرسانی کنید."],
};
const successCodes: NoticeCode[] = ["saved", "connected", "checked", "paused", "resumed"];
function failureCode(error: unknown): NoticeCode {
  if (!(error instanceof ApiError)) return "unconfirmed";
  if (error.status === 401 || error.status === 403) return "denied";
  switch (error.message) {
    case "telegram_credentials_invalid": return "invalid_token";
    case "telegram_identity_mismatch": return "wrong_bot";
    case "telegram_webhook_exists": return "webhook";
    case "telegram_revision_conflict": return "conflict";
    case "telegram_capacity_reached": return "capacity";
    case "telegram_connection_busy":
    case "telegram_rate_limited": return "busy";
    case "telegram_management_unavailable":
    case "telegram_transport_unavailable":
    case "telegram_connection_unavailable":
    case "telegram_not_enabled": return "unavailable";
    default: return error.status === 409 ? "conflict" : error.status === 400 || error.status === 422 ? "invalid" : "failed";
  }
}
function readIdentity(value: BotIdentity): BotIdentity {
  if (!value || !positiveID(value.bot_id) || typeof value.username !== "string" || value.username !== "" && !usernamePattern.test(value.username)) throw new Error("invalid_bot_identity");
  return { bot_id: value.bot_id, username: value.username };
}
function validStoreSetup(value: unknown): value is StoreSetup {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const setup = value as StoreSetup;
  return (["account_enabled", "manager_registered", "can_create_clients", "can_renew_clients", "trial_enabled"] as const)
    .every(key => typeof setup[key] === "boolean") &&
    Number.isSafeInteger(setup.published_products) && setup.published_products >= 0 &&
    Number.isSafeInteger(setup.priced_products) && setup.priced_products >= 0 && setup.priced_products <= setup.published_products &&
    positiveID(setup.catalog_limit) && setup.trial_duration_hours === 24 && setup.trial_starts_at === "issuance";
}
function readWorkspace(value: Workspace): Workspace {
  if (!value || typeof value.enabled !== "boolean" || typeof value.can_configure !== "boolean" || !Array.isArray(value.stores) ||
      value.can_manage_connections !== undefined && typeof value.can_manage_connections !== "boolean" ||
      value.can_manage_connections === true && (!Number.isSafeInteger(value.runtime_revision) || value.runtime_revision! < 0)) throw new Error("invalid_workspace");
  for (const store of value.stores) {
    if (!store || !idPattern.test(store.id) || !positiveID(store.bot_id) || !positiveID(store.manager_user_id) ||
        !idPattern.test(store.representative_id) || typeof store.name !== "string" || typeof store.enabled !== "boolean" ||
        !positiveID(store.revision) || typeof store.connection_state !== "string" || !Number.isSafeInteger(store.last_poll_unix) ||
        value.can_manage_connections && typeof store.credential_configured !== "boolean") throw new Error("invalid_store");
    if (store.setup !== undefined && !validStoreSetup(store.setup)) throw new Error("invalid_store_setup");
  }
  return value;
}
function metadata(form: StoreForm) {
  return { id: form.id, bot_id: Number(digits(form.bot)), representative_id: form.representative,
    name: form.name.trim(), manager_user_id: Number(digits(form.manager)), enabled: form.enabled };
}
function fromStore(store: BotStore): StoreForm {
  return { id: store.id, bot: String(store.bot_id), representative: store.representative_id, name: store.name,
    manager: String(store.manager_user_id), enabled: store.enabled, revision: store.revision };
}

function StoreSetupDetails({ setup, fa, onCatalog, navigationDisabled }: {
  setup?: StoreSetup; fa: boolean; onCatalog: () => void; navigationDisabled: boolean;
}) {
  const tr = (en: string, persian: string) => fa ? persian : en;
  const count = (value: number) => value.toLocaleString(fa ? "fa-IR" : "en-US");
  const attention = !!setup && (!setup.account_enabled || !setup.manager_registered || !setup.can_create_clients ||
    !setup.can_renew_clients || setup.priced_products === 0 || setup.published_products > setup.catalog_limit);
  const summary = !setup ? tr("Setup details unavailable", "جزئیات آماده‌سازی در دسترس نیست") : setup.published_products === 0
    ? tr("No published products", "بستهٔ منتشرشده‌ای وجود ندارد")
    : tr(`${count(setup.priced_products)} of ${count(setup.published_products)} with Stars prices`, `${count(setup.priced_products)} از ${count(setup.published_products)} بسته قیمت Stars دارد`);
  return <details className={"telegram-setup" + (attention ? " telegram-setup-attention" : "")}>
    <summary><span>{tr("Store setup", "آماده‌سازی فروشگاه")}</span><small>{summary}</small></summary>
    {!setup ? <p>{tr("Store setup details could not be checked. Refresh the status to try again.", "جزئیات آماده‌سازی فروشگاه قابل بررسی نیست. وضعیت را دوباره به‌روزرسانی کنید.")}</p> : <>
      {!setup.account_enabled && <p className="telegram-setup-warning">{tr("This store's account is disabled or suspended. The owner must review its account access before customers can use the store.", "حساب این فروشگاه غیرفعال یا تعلیق شده است. مالک باید دسترسی حساب را بررسی کند تا مشتری‌ها بتوانند از فروشگاه استفاده کنند.")}</p>}
      {setup.account_enabled && (!setup.can_create_clients || !setup.can_renew_clients) && <p className="telegram-setup-warning">
        {!setup.can_create_clients && !setup.can_renew_clients
          ? tr("This account's permissions currently block new purchases and renewals.", "مجوزهای فعلی این حساب، خرید جدید و تمدید را مسدود می‌کنند.")
          : !setup.can_create_clients ? tr("This account's permissions currently block new purchases.", "مجوزهای فعلی این حساب، خرید جدید را مسدود می‌کنند.")
            : tr("This account's permissions currently block renewals.", "مجوزهای فعلی این حساب، تمدید را مسدود می‌کنند.")}
      </p>}
      <div className="telegram-setup-products">
        {setup.published_products === 0
          ? <p>{tr("Publish a product for this store's account in the plan catalog, then have the assigned manager set its Stars price in this bot.", "یک بسته برای حساب همین فروشگاه در کاتالوگ پلن‌ها منتشر کنید؛ سپس مدیر تعیین‌شده قیمت Stars آن را در همین ربات تنظیم کند.")}</p>
          : setup.priced_products < setup.published_products
            ? <p>{tr(`${count(setup.published_products - setup.priced_products)} published products still need a Stars price. Customers see published products with a Stars price.`, `${count(setup.published_products - setup.priced_products)} بستهٔ منتشرشده هنوز قیمت Stars ندارد. مشتری بسته‌های منتشرشدهٔ دارای قیمت Stars را می‌بیند.`)}</p>
            : <p>{tr("The published products have prices in Telegram Stars (XTR).", "بسته‌های منتشرشده با Telegram Stars (XTR) قیمت‌گذاری شده‌اند.")}</p>}
        {setup.priced_products > setup.catalog_limit
          ? <p className="telegram-setup-warning">{tr(`The customer catalog exceeds the bot's limit of ${count(setup.catalog_limit)} products. Reduce the published catalog before testing purchases.`, `فهرست خرید مشتری از سقف ${count(setup.catalog_limit)} بستهٔ ربات بیشتر است. پیش از آزمودن خرید، تعداد بسته‌های منتشرشده را کاهش دهید.`)}</p>
          : setup.published_products > setup.catalog_limit && <p className="telegram-setup-warning">{tr(`The manager's product list exceeds the bot's limit of ${count(setup.catalog_limit)}. Reduce published products before setting prices.`, `فهرست بسته‌های مدیر از سقف ${count(setup.catalog_limit)} بستهٔ ربات بیشتر است. پیش از تنظیم قیمت، تعداد بسته‌های منتشرشده را کاهش دهید.`)}</p>}
        <button type="button" className="telegram-text-button" onClick={onCatalog} disabled={navigationDisabled}>{tr("Open plan catalog", "مشاهدهٔ کاتالوگ پلن‌ها")}<ChevronLeft size={14}/></button>
      </div>
      <dl className="telegram-setup-status">
        <div><dt>{tr("Assigned manager", "مدیر تعیین‌شده")}</dt><dd>{setup.manager_registered ? tr("Registered with this bot", "در این ربات ثبت شده است") : tr("Waiting for the manager", "در انتظار ورود مدیر")}</dd></div>
        <div><dt>{tr("24-hour trial · optional", "تست ۲۴ساعته · اختیاری")}</dt><dd>{setup.trial_enabled ? tr("Enabled in store settings", "در تنظیمات فروشگاه فعال است") : tr("Disabled in store settings", "در تنظیمات فروشگاه غیرفعال است")}</dd></div>
      </dl>
      {!setup.manager_registered && <p>{tr("The assigned manager should open this bot and send ", "مدیر تعیین‌شده باید همین ربات را باز کند و ")}<bdi>/start</bdi>{tr(".", " بفرستد.")}</p>}
      <p>{tr("In the bot's Store management menu, Stars prices are under Products and prices, and trials are under Store settings. The assigned manager needs the corresponding permissions.", "در منوی «مدیریت فروشگاه» ربات، قیمت Stars در «محصولات و قیمت‌ها» و تست در «تنظیمات فروشگاه» تنظیم می‌شود. دسترسی مربوط برای مدیر تعیین‌شده لازم است.")}</p>
      <p className="telegram-setup-note">{setup.trial_enabled
        ? tr("The trial lasts 24 hours from issuance, including any wait for service delivery.", "زمان تست ۲۴ ساعت از صدور است و زمان انتظار برای تحویل سرویس را هم شامل می‌شود.")
        : tr("If enabled, the trial lasts 24 hours from issuance, including any wait for service delivery.", "در صورت فعال‌سازی، زمان تست ۲۴ ساعت از صدور است و زمان انتظار برای تحویل سرویس را هم شامل می‌شود.")}</p>
      <p className="telegram-setup-note">{tr("Each order checks permissions, available credit and service delivery.", "مجوزها، اعتبار موجود و تحویل سرویس برای هر سفارش بررسی می‌شود.")}</p>
    </>}
  </details>;
}

export default function TelegramWorkspace({ language, onNavigate }: { language: Language; onNavigate: (page: Page) => void }) {
  const { session } = useSession();
  const fa = language === "fa";
  const tr = (en: string, persian: string) => fa ? persian : en;
  const owner = session?.user.role === "owner";
  const [data, setData] = useState<Workspace | null>(null);
  const [error, setError] = useState(false);
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState<StoreForm>(emptyForm);
  const [token, setToken] = useState("");
  const [verified, setVerified] = useState<Verification | null>(null);
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState<Notice | null>(null);
  const [formNotice, setFormNotice] = useState<NoticeCode | null>(null);
  const [partial, setPartial] = useState(false);
  const [watchStore, setWatchStore] = useState("");
  const [representatives, setRepresentatives] = useState<Representative[]>([]);
  const [repQuery, setRepQuery] = useState("");
  const [repLoading, setRepLoading] = useState(false);
  const [repError, setRepError] = useState(false);
  const [repMore, setRepMore] = useState(false);
  const [repSearchOpen, setRepSearchOpen] = useState(false);
  const generation = useRef(0);
  const operation = useRef<AbortController | null>(null);
  const inputVersion = useRef(0);
  const pending = useRef<{ terms: string; event: string } | null>(null);
  const saved = useRef<{ terms: string; revision: number } | null>(null);
  const editor = useRef<HTMLFormElement>(null);
  const repGeneration = useRef(0);
  const managed = owner && data?.can_manage_connections === true;
  const selected = data?.stores.find(store => store.id === form.id);
  const configured = selected?.credential_configured === true;

  const clearSecret = useCallback(() => {
    inputVersion.current++;
    setToken(""); setVerified(null);
  }, []);
  const load = useCallback(async (signal?: AbortSignal, quiet = false): Promise<Workspace | null> => {
    const next = ++generation.current;
    if (!quiet) setLoading(true);
    setError(false);
    try {
      const value = readWorkspace(await api<Workspace>(base, { signal }));
      if (next !== generation.current || signal?.aborted) return null;
      setData(value);
      return value;
    } catch {
      if (next === generation.current && !signal?.aborted) { setError(true); setData(null); }
      return null;
    } finally { if (next === generation.current) setLoading(false); }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    operation.current?.abort(); operation.current = null;
    clearSecret(); setData(null); setEditing(false); setForm(emptyForm()); setBusy("");
    setNotice(null); setFormNotice(null); setPartial(false); setWatchStore("");
    setRepresentatives([]); setRepQuery(""); setRepError(false); setRepMore(false);
    pending.current = null; saved.current = null;
    void load(controller.signal);
    return () => {
      generation.current++; repGeneration.current++;
      controller.abort(); operation.current?.abort(); operation.current = null;
      inputVersion.current++;
    };
  }, [load, clearSecret, session?.user.id, session?.user.role, session?.csrf_token]);

  // Watch only a just-requested connection, for at most twenty seconds.
  // A successful save/check never substitutes for a real polling observation.
  useEffect(() => {
    if (!watchStore) return;
    const controller = new AbortController();
    let timer = 0, attempts = 0;
    const tick = async () => {
      attempts++;
      const value = document.hidden ? null : await load(controller.signal, true);
      if (controller.signal.aborted) return;
      const state = value?.stores.find(store => store.id === watchStore)?.connection_state;
      if (attempts >= 10 || state === "polling" || state === "error" || state === "store_disabled" || !value && !document.hidden) {
        setWatchStore(""); return;
      }
      timer = window.setTimeout(tick, 2000);
    };
    timer = window.setTimeout(tick, 2000);
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [watchStore, load]);

  useEffect(() => {
    if (!editing) return;
    const timer = window.setTimeout(() => {
      editor.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
      editor.current?.querySelector<HTMLInputElement>("[data-initial-focus]")?.focus({ preventScroll: true });
    }, 0);
    return () => window.clearTimeout(timer);
  }, [editing]);

  const loadRepresentatives = useCallback(async (query: string, offset = 0) => {
    const next = ++repGeneration.current;
    setRepLoading(true); setRepError(false);
    try {
      const params = new URLSearchParams({ limit: "50", offset: String(offset), state: "enabled", sort: "name", q: query.trim() });
      const value = await api<{ representatives: Representative[]; has_more: boolean }>("/api/v1/workspace/representatives?" + params);
      if (!Array.isArray(value.representatives) || typeof value.has_more !== "boolean" ||
          value.representatives.some(rep => !idPattern.test(rep.id) || typeof rep.display_name !== "string" || typeof rep.enabled !== "boolean")) throw new Error("invalid_representatives");
      if (next !== repGeneration.current) return;
      const eligible = value.representatives.filter(rep => rep.id !== "owner" && rep.enabled);
      setRepresentatives(previous => offset ? [...previous, ...eligible.filter(rep => !previous.some(old => old.id === rep.id))] : eligible);
      setRepMore(value.has_more);
    } catch { if (next === repGeneration.current) setRepError(true); }
    finally { if (next === repGeneration.current) setRepLoading(false); }
  }, []);
  useEffect(() => {
    if (!editing || !managed || form.revision !== 0 || form.representative === "owner") return;
    const timer = window.setTimeout(() => void loadRepresentatives(repQuery), repQuery ? 250 : 0);
    return () => { window.clearTimeout(timer); repGeneration.current++; };
  }, [editing, managed, form.revision, form.representative === "owner", repQuery, loadRepresentatives]);

  const start = (action: string) => {
    if (!session || !owner || operation.current) return null;
    const controller = new AbortController();
    operation.current = controller; setBusy(action);
    return controller;
  };
  const current = (controller: AbortController) => operation.current === controller && !controller.signal.aborted;
  const finish = (controller: AbortController) => {
    if (operation.current === controller) { operation.current = null; setBusy(""); }
  };
  const post = <T,>(path: string, body: unknown, controller: AbortController): Promise<T> =>
    api<T>(path, { method: "POST", signal: controller.signal,
      headers: { "Content-Type": "application/json", "X-CSRF-Token": session!.csrf_token }, body: JSON.stringify(body) }, 12000);
  const eventFor = (terms: string) => {
    if (pending.current?.terms !== terms) pending.current = { terms, event: "telegram-store-" + crypto.randomUUID() };
    return pending.current.event;
  };
  const closeEditor = () => {
    const refreshAfterClose = busy === "save";
    operation.current?.abort(); operation.current = null; setBusy("");
    clearSecret(); setEditing(false); setFormNotice(null); setPartial(false);
    pending.current = null; saved.current = null;
    if (refreshAfterClose) { setNotice({ code: "unconfirmed" }); void load(); }
  };
  const edit = (store?: BotStore) => {
    if (busy) return;
    const value = store ? fromStore(store) : { ...emptyForm(), id: managed ? "store-" + crypto.randomUUID() : "" };
    clearSecret(); setForm(value); setFormNotice(null); setPartial(false); setNotice(null);
    pending.current = null;
    saved.current = store ? { terms: JSON.stringify(metadata(value)), revision: store.revision } : null;
    setRepQuery(""); setRepSearchOpen(false); setEditing(true); setWatchStore("");
  };
  const verifyToken = async () => {
    if (!managed || !tokenPattern.test(token)) { setFormNotice("invalid_token"); return; }
    const controller = start("verify");
    if (!controller) return;
    const version = inputVersion.current;
    setFormNotice(null); setVerified(null);
    try {
      const identity = readIdentity(await post<BotIdentity>(base + "/verify", { token }, controller));
      if (!current(controller) || version !== inputVersion.current) return;
      if (form.revision > 0 && identity.bot_id !== Number(form.bot)) { setFormNotice("wrong_bot"); return; }
      setVerified({ ...identity, inputVersion: version });
      setForm(previous => ({ ...previous, bot: String(identity.bot_id), name: previous.name || identity.username }));
    } catch (e) {
      if (current(controller)) {
        const code = failureCode(e);
        setFormNotice(code);
        if (code === "denied") clearSecret();
      }
    }
    finally { finish(controller); }
  };

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!session || !owner || !data?.can_configure || busy) return;
    const input = metadata(form);
    if (!idPattern.test(input.id) || !idPattern.test(input.representative_id) || !positiveID(input.bot_id) ||
        !positiveID(input.manager_user_id) || !input.name || input.name.length > 128) { setFormNotice("invalid"); return; }
    const connect = managed && (form.revision === 0 || !!token || !configured);
    if (connect && (!token || !verified || verified.inputVersion !== inputVersion.current || verified.bot_id !== input.bot_id)) {
      setFormNotice("verify_first"); return;
    }
    const controller = start("save");
    if (!controller) return;
    const terms = JSON.stringify(input);
    let metadataSaved = false, connectionAttempted = false;
    setFormNotice(null); setPartial(false);
    try {
      if (saved.current?.terms !== terms) {
        const result = await post<BotStore>(base + "/stores", { ...input, expected_revision: form.revision,
          event_id: eventFor(JSON.stringify({ ...input, expected_revision: form.revision })) }, controller);
        if (!current(controller)) return;
        if (!result || result.id !== input.id || result.bot_id !== input.bot_id || result.representative_id !== input.representative_id ||
            result.manager_user_id !== input.manager_user_id || result.name !== input.name || result.enabled !== input.enabled ||
            !positiveID(result.revision) || result.revision <= form.revision) throw new Error("invalid_store_receipt");
        saved.current = { terms, revision: result.revision }; pending.current = null;
        setForm(previous => ({ ...previous, revision: result.revision }));
      }
      metadataSaved = true;
      if (connect) {
        connectionAttempted = true;
        const result = await post<ConnectionReceipt>(base + "/stores/" + encodeURIComponent(input.id) + "/connection",
          { token, expected_revision: data.runtime_revision }, controller);
        if (!current(controller)) return;
        if (!result || result.bot_id !== input.bot_id || !Number.isSafeInteger(result.revision) || result.revision < 0) throw new Error("invalid_connection_receipt");
        clearSecret();
      }
      if (!current(controller)) return;
      setEditing(false); setFormNotice(null); setPartial(false); clearSecret();
      saved.current = null; pending.current = null;
      setNotice({ code: connect ? "connected" : "saved", store: input.id });
      await load(controller.signal);
      if (current(controller) && connect) setWatchStore(input.id);
    } catch (e) {
      if (!current(controller)) return;
      setPartial(metadataSaved && connect);
      const code = failureCode(e);
      setFormNotice(code);
      if (code === "denied") clearSecret();
      const fresh = await load(controller.signal, true);
      if (current(controller) && fresh) {
        const stored = fresh.stores.find(store => store.id === input.id && store.bot_id === input.bot_id);
        // An interrupted metadata response can be reconciled without another
        // registration. Keep edited values; only advance the observed revision.
        if (stored) {
          const observed = fromStore(stored);
          setForm(previous => ({ ...previous, revision: stored.revision }));
          saved.current = { terms: JSON.stringify(metadata(observed)), revision: stored.revision };
          pending.current = null;
          if (managed && !stored.credential_configured) setPartial(true);
        }
      }
    } finally {
      if (current(controller) && connectionAttempted) clearSecret();
      finish(controller);
    }
  };

  const storeAction = async (store: BotStore, action: "check" | "connect" | "toggle") => {
    if (!managed || !data) return;
    const controller = start(action + ":" + store.id);
    if (!controller) return;
    setNotice(null); setWatchStore("");
    try {
      if (action === "toggle") {
        const input = { ...metadata(fromStore(store)), enabled: !store.enabled, expected_revision: store.revision };
        const result = await post<BotStore>(base + "/stores", { ...input, event_id: eventFor(JSON.stringify(input)) }, controller);
        if (!current(controller)) return;
        if (result.id !== store.id || !positiveID(result.revision) || result.enabled !== !store.enabled) throw new Error("invalid_store_receipt");
        pending.current = null;
      } else if (action === "check") {
        const result = readIdentity(await post<BotIdentity>(base + "/stores/" + encodeURIComponent(store.id) + "/check", {}, controller));
        if (result.bot_id !== store.bot_id) throw new Error("invalid_identity_receipt");
      } else {
        const result = await post<ConnectionReceipt>(base + "/stores/" + encodeURIComponent(store.id) + "/connection",
          { token: "", expected_revision: data.runtime_revision }, controller);
        if (result.bot_id !== store.bot_id || !Number.isSafeInteger(result.revision)) throw new Error("invalid_connection_receipt");
      }
      if (!current(controller)) return;
      setNotice({ code: action === "check" ? "checked" : action === "connect" ? "connected" : store.enabled ? "paused" : "resumed", store: store.id });
      await load(controller.signal);
      if (current(controller) && action === "connect") setWatchStore(store.id);
    } catch (e) {
      if (current(controller)) { setNotice({ code: failureCode(e), store: store.id }); await load(controller.signal, true); }
    } finally { finish(controller); }
  };

  const active = data?.stores.filter(store => store.enabled && store.connection_state === "polling").length ?? 0;
  const showNotice = (value: NoticeCode, key?: string) => <p key={key} className={successCodes.includes(value) ? "telegram-saved" : "telegram-notice"} role={successCodes.includes(value) ? "status" : "alert"}>
    {successCodes.includes(value) && <Check size={16}/>}<span>{tr(...noticeWords[value])}</span>
  </p>;
  const needsToken = managed && (!configured || !!token || form.revision === 0);
  return <section className="telegram-workspace" aria-label={tr("Telegram stores", "فروشگاه‌های تلگرام")}>
    <div className="telegram-toolbar">
      <div><span className="telegram-eyebrow"><Bot size={17}/>{tr("HUB & representative bots", "ربات هاب و نماینده‌ها")}</span><h2>{tr("Your Telegram stores", "فروشگاه‌های تلگرامی شما")}</h2></div>
      <button type="button" onClick={() => { setWatchStore(""); void load(); }} disabled={loading || !!busy}><RefreshCw size={16}/>{tr("Refresh", "به‌روزرسانی")}</button>
    </div>
    {error && <p className="telegram-notice" role="alert">{tr("Store status could not be loaded. Refresh to try again.", "وضعیت فروشگاه‌ها دریافت نشد. دوباره به‌روزرسانی کنید.")}</p>}
    {!data && loading && <p role="status">{tr("Loading stores…", "در حال دریافت فروشگاه‌ها…")}</p>}
    {data && !data.enabled && <div className="telegram-empty">
      <Bot size={30}/><h3>{tr("Telegram stores are not enabled", "فروشگاه‌های تلگرام فعال نشده‌اند")}</h3>
      <p>{tr("Bot management is not available on this HUB yet. Your clients and plan catalog remain available.", "مدیریت ربات هنوز در این هاب آماده نیست. مشتری‌ها و کاتالوگ پلن‌ها در دسترس‌اند.")}</p>
      <button type="button" onClick={() => onNavigate("marketplace")}>{tr("Open plan catalog", "مشاهدهٔ کاتالوگ پلن‌ها")}<ChevronLeft size={15}/></button>
    </div>}
    {data?.enabled && <>
      <div className="telegram-metrics">
        <div><Store size={18}/><span>{tr("Stores in your scope", "فروشگاه‌های در دسترس")}</span><strong>{data.stores.length.toLocaleString(fa ? "fa-IR" : "en-US")}</strong></div>
        <div><Bot size={18}/><span>{tr("Receiving updates", "در حال دریافت پیام")}</span><strong>{active.toLocaleString(fa ? "fa-IR" : "en-US")}</strong></div>
        <div><Wallet size={18}/><span>{tr("Telegram checkout", "پرداخت داخل تلگرام")}</span><strong>Stars · XTR</strong></div>
      </div>
      <div className="telegram-section-heading"><div><h3>{tr("Store connections", "اتصال فروشگاه‌ها")}</h3>{managed && <p>{tr("Connect a bot, choose its account and manage it here.", "ربات را متصل کنید، حسابش را انتخاب کنید و همین‌جا مدیریت کنید.")}</p>}</div>
        {data.can_configure && owner && <button type="button" className="telegram-primary" onClick={() => edit()} disabled={!!busy || loading || editing}><Plus size={16}/>{managed ? tr("Add bot", "افزودن ربات") : tr("Add store", "افزودن فروشگاه")}</button>}
      </div>
      {notice && (!notice.store || !data.stores.some(store => store.id === notice.store)) && showNotice(notice.code)}
      {editing && data.can_configure && owner && <form ref={editor} className="telegram-store-form" onSubmit={submit} aria-label={tr("Store settings", "تنظیمات فروشگاه")}>
        <header className="telegram-form-heading"><div><span className="telegram-eyebrow">{form.revision ? tr("Manage your bot", "مدیریت ربات شما") : tr("A new Telegram storefront", "یک فروشگاه تلگرامی جدید")}</span><h3>{form.revision ? form.name : managed ? tr("Connect your bot", "رباتت را متصل کن") : tr("Register store", "ثبت فروشگاه")}</h3></div><button type="button" className="telegram-close" onClick={closeEditor} aria-label={tr("Close store settings", "بستن تنظیمات فروشگاه")}><X size={18}/></button></header>
        {managed && <div className="telegram-connection-setup">
          <div className="telegram-step-title"><span>{fa ? "۱" : "1"}</span><div><h4>{tr("Bot connection", "اتصال ربات")}</h4><p>{tr("Use a bot dedicated to this test panel and paste its token from BotFather. Only the owner can enter or replace it.", "یک ربات مخصوص همین پنل آزمایشی انتخاب کنید و توکن دریافتی از BotFather را وارد کنید. واردکردن یا جایگزینی آن فقط در اختیار مالک است.")}</p></div><KeyRound size={19}/></div>
          {form.revision > 0 && <div className="telegram-existing-identity"><Bot size={16}/><bdi>{selected?.bot_username && usernamePattern.test(selected.bot_username) ? "@" + selected.bot_username : tr("Bot", "ربات") + " " + form.bot}</bdi><span>{configured ? tr("Token saved securely", "توکن به‌صورت خصوصی ذخیره شده") : tr("Token needed", "توکن لازم است")}</span></div>}
          <div className="telegram-token-row"><label>{configured ? tr("New bot token (optional)", "توکن جدید ربات (اختیاری)") : tr("Bot token", "توکن ربات")}<input data-initial-focus type="password" name="telegram-credential" autoComplete="new-password" autoCapitalize="none" autoCorrect="off" spellCheck={false} dir="ltr" maxLength={221} value={token}
            onChange={event => { inputVersion.current++; setToken(event.target.value.trim()); setVerified(null); setFormNotice(null); }}
            placeholder={configured ? tr("Leave empty to keep the saved token", "برای حفظ توکن فعلی خالی بگذارید") : tr("Paste the BotFather token", "توکن BotFather را وارد کنید")} disabled={!!busy}/></label>
            <button type="button" onClick={() => void verifyToken()} disabled={!!busy || !token}><ShieldCheck size={16}/>{busy === "verify" ? tr("Checking…", "در حال بررسی…") : tr("Verify bot", "بررسی ربات")}</button>
          </div>
          {verified && verified.inputVersion === inputVersion.current && <div className="telegram-verified" role="status"><ShieldCheck size={20}/><div><strong>{tr("Bot identity verified", "هویت ربات تأیید شد")}</strong><span><bdi>{verified.username ? "@" + verified.username : tr("Bot", "ربات")}</bdi><span> · </span><bdi>{verified.bot_id}</bdi></span></div></div>}
          <p className="telegram-input-hint">{tr("The saved token is never shown again. Verifying a token does not start the bot.", "توکن ذخیره‌شده دوباره نمایش داده نمی‌شود. بررسی توکن به‌تنهایی ربات را فعال نمی‌کند.")}</p>
        </div>}
        {managed && <div className="telegram-step-title telegram-details-title"><span>{fa ? "۲" : "2"}</span><div><h4>{tr("Store and manager", "فروشگاه و مدیر")}</h4><p>{tr("Choose which account owns this bot and who manages it in Telegram.", "حساب صاحب ربات و شخصی را که در تلگرام مدیریتش می‌کند انتخاب کنید.")}</p></div></div>}
        <div className="telegram-form-grid">
          {!managed && <label>{tr("Store ID", "شناسهٔ فروشگاه")}<input data-initial-focus dir="ltr" value={form.id} onChange={event => setForm({ ...form, id: event.target.value })} required maxLength={64} disabled={!!busy || form.revision > 0}/></label>}
          <label>{tr("Store name", "نام فروشگاه")}<input value={form.name} onChange={event => setForm({ ...form, name: event.target.value })} required maxLength={128} disabled={!!busy}/></label>
          {!managed && <label>{tr("Bot numeric ID", "شناسهٔ عددی ربات")}<input dir="ltr" inputMode="numeric" value={form.bot} onChange={event => setForm({ ...form, bot: digits(event.target.value) })} required disabled={!!busy || form.revision > 0}/></label>}
          <label>{tr("Manager numeric ID", "شناسهٔ عددی مدیر")}<input aria-label={tr("Manager numeric ID", "شناسهٔ عددی مدیر")} aria-describedby="telegram-manager-hint" dir="ltr" inputMode="numeric" value={form.manager} onChange={event => setForm({ ...form, manager: digits(event.target.value) })} required maxLength={16} disabled={!!busy}/><small id="telegram-manager-hint">{tr("The manager's personal Telegram ID, not a username or bot ID.", "شناسهٔ عددی حساب شخصی مدیر در تلگرام؛ نه نام کاربری یا شناسهٔ ربات.")}</small></label>
          {managed ? <fieldset className="telegram-account-field"><legend>{tr("Store account", "حساب فروشگاه")}</legend>
            <div className="telegram-account-options"><label><input type="radio" name="telegram-account" checked={form.representative === "owner"} onChange={() => setForm({ ...form, representative: "owner" })} disabled={!!busy || form.revision > 0}/><span><Bot size={16}/>{tr("HUB · Owner", "هاب · مالک")}</span></label><label><input type="radio" name="telegram-account" checked={form.representative !== "owner"} onChange={() => setForm({ ...form, representative: "" })} disabled={!!busy || form.revision > 0}/><span><Store size={16}/>{tr("Representative", "نماینده")}</span></label></div>
            {form.representative !== "owner" && <div className="telegram-representative-picker">
              <label>{tr("Representative account", "حساب نماینده")}<select value={form.representative} onChange={event => setForm({ ...form, representative: event.target.value })} disabled={!!busy || form.revision > 0} required>
                <option value="">{repLoading ? tr("Loading representatives…", "در حال دریافت نماینده‌ها…") : tr("Choose a representative", "یک نماینده انتخاب کنید")}</option>
                {form.representative && !representatives.some(rep => rep.id === form.representative) && <option value={form.representative}>{form.representative}</option>}
                {representatives.map(rep => <option key={rep.id} value={rep.id}>{rep.display_name + " · " + rep.id}</option>)}
              </select></label>
              {form.revision === 0 && <><button type="button" className="telegram-text-button" onClick={() => setRepSearchOpen(value => !value)}><Search size={14}/>{tr("Find a representative", "جست‌وجوی نماینده")}</button>
                {repSearchOpen && <label>{tr("Search name or ID", "جست‌وجوی نام یا شناسه")}<input value={repQuery} onChange={event => setRepQuery(event.target.value)} maxLength={128} disabled={!!busy}/></label>}
                {repError && <p className="telegram-input-hint" role="alert">{tr("The representative list could not be loaded.", "فهرست نماینده‌ها دریافت نشد.")} <button type="button" className="telegram-text-button" onClick={() => void loadRepresentatives(repQuery)} disabled={repLoading}>{tr("Try again", "تلاش دوباره")}</button></p>}
                {repMore && <button type="button" className="telegram-text-button" onClick={() => void loadRepresentatives(repQuery, representatives.length)} disabled={repLoading}>{repLoading ? tr("Loading…", "در حال دریافت…") : tr("Load more representatives", "نماینده‌های بیشتر")}</button>}
              </>}
            </div>}
          </fieldset> : <label>{tr("Representative ID or owner", "شناسهٔ نماینده یا owner")}<input dir="ltr" value={form.representative} onChange={event => setForm({ ...form, representative: event.target.value })} required maxLength={64} disabled={!!busy || form.revision > 0}/></label>}
          <label className="telegram-checkbox"><input type="checkbox" checked={form.enabled} onChange={event => setForm({ ...form, enabled: event.target.checked })} disabled={!!busy}/>{tr("Store open for customers", "فروشگاه برای مشتری‌ها فعال باشد")}</label>
        </div>
        {form.revision > 0 && !form.enabled && <p className="telegram-input-hint">{tr("Pausing the store stops new customer actions. Existing payment records keep processing.", "توقف فروشگاه، عملیات تازهٔ مشتری‌ها را متوقف می‌کند. پردازش سوابق پرداخت‌های موجود ادامه دارد.")}</p>}
        {partial && showNotice("partial")}
        {formNotice && showNotice(formNotice)}
        <div className="telegram-form-actions"><button type="submit" disabled={!!busy || managed && needsToken && (!verified || verified.inputVersion !== inputVersion.current)}>
          {busy === "save" ? tr("Saving…", "در حال ذخیره…") : managed && needsToken ? tr("Save & activate bot", "ذخیره و فعال‌سازی ربات") : tr("Save store", "ذخیرهٔ فروشگاه")}
        </button><button type="button" onClick={closeEditor}>{tr("Cancel", "انصراف")}</button></div>
      </form>}
      {data.stores.length === 0 && !editing && <div className="telegram-empty"><Store size={26}/><h3>{tr("No store registered yet", "هنوز فروشگاهی ثبت نشده است")}</h3><p>{managed ? tr("Add your HUB bot or a representative's bot to get started.", "برای شروع، ربات هاب یا ربات یکی از نماینده‌ها را اضافه کنید.") : data.can_configure ? tr("Register the bot and the person who will manage its store.", "ربات و شخصی را که مدیریت فروشگاه را بر عهده دارد ثبت کنید.") : tr("The owner can assign a store to your representative account.", "مالک می‌تواند یک فروشگاه به حساب نمایندگی شما اختصاص دهد.")}</p></div>}
      <div className="telegram-stores">{data.stores.map(store => {
        const username = store.bot_username && usernamePattern.test(store.bot_username) ? store.bot_username : "";
        return <article className="telegram-store-card" key={store.id}>
          <header><span className="telegram-store-icon"><Bot size={22}/></span><div><h3>{store.name}</h3>{username ? <bdi className="telegram-bot-username">{"@" + username}</bdi> : <span className="telegram-card-kind">{store.representative_id === "owner" ? tr("HUB bot", "ربات هاب") : tr("Representative bot", "ربات نماینده")}</span>}</div><span className={"telegram-state " + (store.connection_state === "polling" ? "telegram-state-active" : "")}>{states[store.connection_state]?.[fa ? 1 : 0] ?? tr("Status unavailable", "وضعیت نامشخص")}</span></header>
          <dl><div><dt>{tr("Store account", "حساب فروشگاه")}</dt><dd>{store.representative_id === "owner" ? tr("Owner", "مالک") : <bdi>{store.representative_id}</bdi>}</dd></div><div><dt>{tr("Bot ID", "شناسهٔ ربات")}</dt><dd><bdi>{store.bot_id}</bdi></dd></div><div><dt>{tr("Manager ID", "شناسهٔ مدیر")}</dt><dd><bdi>{store.manager_user_id}</bdi></dd></div><div><dt>{tr("Last update check", "آخرین بررسی پیام‌ها")}</dt><dd>{store.last_poll_unix > 0 ? new Date(store.last_poll_unix * 1000).toLocaleString(fa ? "fa-IR" : "en-GB") : tr("No check recorded", "بررسی ثبت نشده")}</dd></div></dl>
          {store.connection_problem && store.connection_state !== "polling" && <p className="telegram-input-hint">{tr("The connection needs attention. Check it again or update this bot's token.", "اتصال نیاز به بررسی دارد. آن را دوباره بررسی کنید یا توکن همین ربات را به‌روز کنید.")}</p>}
          {notice?.store === store.id && showNotice(notice.code)}
          {!store.enabled && <p className="telegram-input-hint">{tr("The storefront is paused; payment records continue to be processed.", "فروشگاه متوقف است؛ پردازش سوابق پرداخت ادامه دارد.")}</p>}
          <StoreSetupDetails setup={store.setup} fa={fa} onCatalog={() => onNavigate("marketplace")} navigationDisabled={loading || !!busy || editing}/>
          {store.queue_status_available && <details className="telegram-queues" open={store.review_pending > 0 || store.outbox_unknown > 0 || store.delivery_review > 0}>
            <summary>{tr("Queues and review", "صف‌ها و موارد نیازمند بررسی")}</summary>
            <dl>{([
              ["Incoming updates", "پیام‌های ورودی معطل", store.inbox_pending], ["Financial retries", "ثبت مالی در انتظار تکرار", store.financial_retry],
              ["Incoming items to review", "ورودی نیازمند بررسی", store.review_pending], ["Outgoing messages", "پیام‌های آمادهٔ ارسال", store.outbox_pending],
              ["Uncertain sends", "ارسال با نتیجهٔ نامشخص", store.outbox_unknown], ["Delivery proofs to review", "تأیید تحویل نیازمند بررسی", store.delivery_review],
            ] as [string, string, number][]).map(([en, persian, count]) => <div key={en}><dt>{tr(en, persian)}</dt><dd>{Number.isSafeInteger(count) && count >= 0 ? count.toLocaleString(fa ? "fa-IR" : "en-US") : "—"}</dd></div>)}</dl>
            {(store.review_pending > 0 || store.outbox_unknown > 0 || store.delivery_review > 0) && <p>{tr("Review is required. An uncertain send is not repeated automatically.", "این موارد باید بررسی شوند. ارسال با نتیجهٔ نامشخص خودکار تکرار نمی‌شود.")}</p>}
          </details>}
          <div className="telegram-card-actions">
            {data.can_configure && owner && <button type="button" onClick={() => edit(store)} disabled={!!busy || editing}>{managed ? tr("Manage bot", "مدیریت ربات") : tr("Manage store", "مدیریت فروشگاه")}</button>}
            {managed && store.credential_configured && <>
              <button type="button" onClick={() => void storeAction(store, "check")} disabled={!!busy || editing}><ShieldCheck size={15}/>{busy === "check:" + store.id ? tr("Checking…", "در حال بررسی…") : tr("Check connection", "بررسی اتصال")}</button>
              <button type="button" onClick={() => void storeAction(store, "connect")} disabled={!!busy || editing}><RefreshCw size={15}/>{busy === "connect:" + store.id ? tr("Connecting…", "در حال اتصال…") : tr("Reconnect", "اتصال دوباره")}</button>
              <button type="button" onClick={() => void storeAction(store, "toggle")} disabled={!!busy || editing}>{store.enabled ? <Pause size={15}/> : <Play size={15}/>}
                {store.enabled ? tr("Pause store", "توقف فروشگاه") : tr("Resume store", "فعال‌سازی فروشگاه")}</button>
            </>}
            {username && <a className="telegram-open-bot" href={"https://t.me/" + username} target="_blank" rel="noopener noreferrer"><ExternalLink size={15}/>{tr("Open bot", "بازکردن ربات")}</a>}
          </div>
        </article>;
      })}</div>
      <p className="telegram-footnote">{tr("Each store keeps its customers, orders and reward balance separate. A successful payment is followed by delivery only when PRO verifies the service is ready.", "مشتری‌ها، سفارش‌ها و موجودی پاداش هر فروشگاه مستقل است. پس از پرداخت موفق، تحویل زمانی انجام می‌شود که آماده‌بودن سرویس در PRO تأیید شود.")}</p>
    </>}
  </section>;
}
