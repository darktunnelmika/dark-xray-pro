import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { Bot, Check, ChevronLeft, Plus, RefreshCw, Store, Wallet } from "lucide-react";
import { api, mutation, useSession, type Language } from "./session";
import type { Page } from "./navigation";
import "./TelegramWorkspace.css";

type BotStore = { id: string; bot_id: number; representative_id: string; name: string; manager_user_id: number; enabled: boolean; revision: number; connection_state: string; last_poll_unix: number; queue_status_available: boolean; inbox_pending: number; financial_retry: number; review_pending: number; outbox_pending: number; outbox_unknown: number; delivery_review: number };
type Workspace = { enabled: boolean; can_configure: boolean; stores: BotStore[] };
type StoreForm = { id: string; bot: string; representative: string; name: string; manager: string; enabled: boolean; revision: number };
const emptyForm = (): StoreForm => ({ id: "", bot: "", representative: "owner", name: "", manager: "", enabled: true, revision: 0 });
const states: Record<string, [string,string]> = {
  not_configured: ["Credential not configured", "اتصال تنظیم نشده"],
  configured: ["Configured; waiting to start", "تنظیم شده؛ در انتظار شروع"],
  starting: ["Checking bot identity", "در حال بررسی هویت ربات"],
  identity_verified: ["Identity verified", "هویت تأیید شده"],
  polling: ["Receiving updates", "در حال دریافت پیام‌ها"],
  error: ["Connection needs attention", "اتصال نیاز به بررسی دارد"],
  stopped: ["Worker stopped", "دریافت پیام متوقف است"],
  store_disabled: ["Store paused", "فروشگاه غیرفعال است"],
};

export default function TelegramWorkspace({ language, onNavigate }: { language: Language; onNavigate: (page: Page) => void }) {
  const { session } = useSession();
  const fa = language === "fa";
  const tr = (en: string, persian: string) => fa ? persian : en;
  const [data, setData] = useState<Workspace | null>(null);
  const [error, setError] = useState(false);
  const [loading, setLoading] = useState(false);
  const [editing, setEditing] = useState(false);
  const [form, setForm] = useState<StoreForm>(emptyForm);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<"saved" | "invalid" | "failed" | "">("");
  const generation = useRef(0);
  const pending = useRef<{ terms: string; event: string } | null>(null);
  const load = useCallback(async (signal?: AbortSignal) => {
    const next = ++generation.current;
    setLoading(true); setError(false);
    try {
      const value = await api<Workspace>("/api/v1/workspace/telegram", { signal });
      if (typeof value.enabled !== "boolean" || typeof value.can_configure !== "boolean" || !Array.isArray(value.stores)) throw new Error("invalid_response");
      if (next === generation.current) setData(value);
    } catch {
      if (next === generation.current && !signal?.aborted) { setError(true); setData(null); }
    } finally { if (next === generation.current) setLoading(false); }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    setData(null); setEditing(false); pending.current = null;
    void load(controller.signal);
    return () => { generation.current++; controller.abort(); };
  }, [load, session?.user.id]);

  const edit = (store?: BotStore) => {
    setForm(store ? { id: store.id, bot: String(store.bot_id), representative: store.representative_id, name: store.name, manager: String(store.manager_user_id), enabled: store.enabled, revision: store.revision } : emptyForm());
    setMessage(""); setEditing(true); pending.current = null;
  };
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!session || session.user.role !== "owner" || !data?.can_configure || saving) return;
    const bot = Number(form.bot), manager = Number(form.manager);
    if (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$/.test(form.id) || !/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$/.test(form.representative) || !Number.isSafeInteger(bot) || bot <= 0 || !Number.isSafeInteger(manager) || manager <= 0 || !form.name.trim()) { setMessage("invalid"); return; }
    const input = { id: form.id, bot_id: bot, representative_id: form.representative, name: form.name.trim(), manager_user_id: manager, expected_revision: form.revision, enabled: form.enabled };
    const terms = JSON.stringify(input);
    if (pending.current?.terms !== terms) pending.current = { terms, event: "telegram-store-" + crypto.randomUUID() };
    setSaving(true); setMessage("");
    try {
      await mutation("/api/v1/workspace/telegram/stores", { ...input, event_id: pending.current.event }, session.csrf_token);
      pending.current = null; setEditing(false); setMessage("saved"); await load();
    } catch { setMessage("failed"); } finally { setSaving(false); }
  };
  const active = data?.stores.filter(item => item.enabled && item.connection_state === "polling").length ?? 0;
  return <section className="telegram-workspace" aria-label={tr("Telegram stores", "فروشگاه‌های تلگرام")}>
    <div className="telegram-toolbar">
      <div><span className="telegram-eyebrow"><Bot size={17}/>{tr("HUB & representative bots", "ربات هاب و نماینده‌ها")}</span><h2>{tr("Your Telegram stores", "فروشگاه‌های تلگرامی شما")}</h2></div>
      <button type="button" onClick={() => void load()} disabled={loading || saving}><RefreshCw size={16}/>{tr("Refresh", "به‌روزرسانی")}</button>
    </div>
    {error && <p className="telegram-notice" role="alert">{tr("Store status could not be loaded. Refresh to try again.", "وضعیت فروشگاه‌ها دریافت نشد. دوباره به‌روزرسانی کنید.")}</p>}
    {!data && loading && <p role="status">{tr("Loading stores…", "در حال دریافت فروشگاه‌ها…")}</p>}
    {data && !data.enabled && <div className="telegram-empty">
      <Bot size={30}/><h3>{tr("Telegram stores are not enabled", "فروشگاه‌های تلگرام فعال نشده‌اند")}</h3>
      <p>{tr("This HUB has not enabled store bots. Client records and the plan catalog remain available in the panel.", "ربات فروشگاهی در این هاب هنوز فعال نشده است. مدیریت مشتری‌ها و کاتالوگ پلن‌ها در پنل در دسترس‌اند.")}</p>
      <button type="button" onClick={() => onNavigate("marketplace")}>{tr("Open plan catalog", "مشاهدهٔ کاتالوگ پلن‌ها")}<ChevronLeft size={15}/></button>
    </div>}
    {data?.enabled && <>
      <div className="telegram-metrics">
        <div><Store size={18}/><span>{tr("Stores in your scope", "فروشگاه‌های در دسترس")}</span><strong>{data.stores.length.toLocaleString(fa ? "fa-IR" : "en-US")}</strong></div>
        <div><Bot size={18}/><span>{tr("Receiving updates", "در حال دریافت پیام")}</span><strong>{active.toLocaleString(fa ? "fa-IR" : "en-US")}</strong></div>
        <div><Wallet size={18}/><span>{tr("Telegram checkout", "پرداخت داخل تلگرام")}</span><strong>Stars · XTR</strong></div>
      </div>
      <div className="telegram-section-heading"><h3>{tr("Store connections", "اتصال فروشگاه‌ها")}</h3>{data.can_configure && session?.user.role === "owner" && <button type="button" onClick={() => edit()} disabled={saving}><Plus size={16}/>{tr("Add store", "افزودن فروشگاه")}</button>}</div>
      {message === "saved" && <p className="telegram-saved" role="status"><Check size={16}/>{tr("Store settings saved.", "تنظیمات فروشگاه ذخیره شد.")}</p>}
      {data.stores.length === 0 && <div className="telegram-empty"><Store size={26}/><h3>{tr("No store registered yet", "هنوز فروشگاهی ثبت نشده است")}</h3><p>{data.can_configure ? tr("Register the bot's numeric identity and the person who will manage this store.", "شناسهٔ عددی ربات و شخصی را که مدیریت فروشگاه را بر عهده دارد ثبت کنید.") : tr("The owner can assign a store to your representative account.", "مالک می‌تواند یک فروشگاه به حساب نمایندگی شما اختصاص دهد.")}</p></div>}
      <div className="telegram-stores">{data.stores.map(store => <article className="telegram-store-card" key={store.id}>
        <header><span className="telegram-store-icon"><Bot size={22}/></span><div><h3>{store.name}</h3><code>{store.id}</code></div><span className={"telegram-state " + (store.connection_state === "polling" ? "telegram-state-active" : "")}>{states[store.connection_state]?.[fa ? 1 : 0] ?? tr("Status unavailable", "وضعیت نامشخص")}</span></header>
        <dl><div><dt>{tr("Store account", "حساب فروشگاه")}</dt><dd>{store.representative_id === "owner" ? tr("Owner", "مالک") : <bdi>{store.representative_id}</bdi>}</dd></div><div><dt>{tr("Bot ID", "شناسهٔ ربات")}</dt><dd><bdi>{store.bot_id}</bdi></dd></div><div><dt>{tr("Manager ID", "شناسهٔ مدیر")}</dt><dd><bdi>{store.manager_user_id}</bdi></dd></div><div><dt>{tr("Last update check", "آخرین بررسی پیام‌ها")}</dt><dd>{store.last_poll_unix > 0 ? new Date(store.last_poll_unix * 1000).toLocaleString(fa ? "fa-IR" : "en-GB") : tr("No check recorded", "بررسی ثبت نشده")}</dd></div></dl>
        {store.queue_status_available && <details className="telegram-queues" open={store.review_pending > 0 || store.outbox_unknown > 0 || store.delivery_review > 0}>
          <summary>{tr("Queues and review", "صف‌ها و موارد نیازمند بررسی")}</summary>
          <dl>{([
            ["Incoming updates", "پیام‌های ورودی معطل", store.inbox_pending],
            ["Financial retries", "ثبت مالی در انتظار تکرار", store.financial_retry],
            ["Incoming items to review", "ورودی نیازمند بررسی", store.review_pending],
            ["Outgoing messages", "پیام‌های آمادهٔ ارسال", store.outbox_pending],
            ["Uncertain sends", "ارسال با نتیجهٔ نامشخص", store.outbox_unknown],
            ["Delivery proofs to review", "تأیید تحویل نیازمند بررسی", store.delivery_review],
          ] as [string, string, number][]).map(([en, persian, count]) => <div key={en}><dt>{tr(en, persian)}</dt><dd>{count.toLocaleString(fa ? "fa-IR" : "en-US")}</dd></div>)}</dl>
          {(store.review_pending > 0 || store.outbox_unknown > 0 || store.delivery_review > 0) && <p>{tr("Review is required. An uncertain send is not repeated automatically.", "این موارد باید بررسی شوند. ارسال با نتیجهٔ نامشخص خودکار تکرار نمی‌شود.")}</p>}
        </details>}
        {data.can_configure && <button type="button" onClick={() => edit(store)} disabled={saving}>{tr("Manage store", "مدیریت فروشگاه")}</button>}
      </article>)}</div>
      {editing && data.can_configure && session?.user.role === "owner" && <form className="telegram-store-form" onSubmit={submit} aria-label={tr("Store settings", "تنظیمات فروشگاه")}>
        <h3>{form.revision ? tr("Edit store", "ویرایش فروشگاه") : tr("Register store", "ثبت فروشگاه")}</h3>
        <p>{tr("Bot and manager IDs are numeric Telegram identities. A username is not a substitute. Connection credentials are configured privately on the HUB.", "شناسهٔ ربات و مدیر باید عددی باشند؛ نام کاربری جای آن‌ها را نمی‌گیرد. اطلاعات محرمانهٔ اتصال به‌صورت خصوصی روی هاب تنظیم می‌شود.")}</p>
        <div className="telegram-form-grid">
          <label>{tr("Store ID", "شناسهٔ فروشگاه")}<input aria-label={tr("Store ID", "شناسهٔ فروشگاه")} dir="ltr" value={form.id} onChange={e => setForm({ ...form, id: e.target.value })} required maxLength={64} disabled={saving || form.revision > 0}/></label>
          <label>{tr("Store name", "نام فروشگاه")}<input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} required maxLength={128} disabled={saving}/></label>
          <label>{tr("Bot numeric ID", "شناسهٔ عددی ربات")}<input dir="ltr" inputMode="numeric" pattern="[0-9]+" value={form.bot} onChange={e => setForm({ ...form, bot: e.target.value })} required disabled={saving || form.revision > 0}/></label>
          <label>{tr("Representative ID or owner", "شناسهٔ نماینده یا owner")}<input dir="ltr" value={form.representative} onChange={e => setForm({ ...form, representative: e.target.value })} required maxLength={64} disabled={saving || form.revision > 0}/></label>
          <label>{tr("Manager numeric ID", "شناسهٔ عددی مدیر")}<input dir="ltr" inputMode="numeric" pattern="[0-9]+" value={form.manager} onChange={e => setForm({ ...form, manager: e.target.value })} required disabled={saving}/></label>
          <label className="telegram-checkbox"><input type="checkbox" checked={form.enabled} onChange={e => setForm({ ...form, enabled: e.target.checked })} disabled={saving}/>{tr("Enable store", "فروشگاه فعال باشد")}</label>
        </div>
        {message === "invalid" && <p className="telegram-notice" role="alert">{tr("Check the identifiers and enter a store name.", "شناسه‌ها را بررسی کنید و نام فروشگاه را وارد کنید.")}</p>}
        {message === "failed" && <p className="telegram-notice" role="alert">{tr("Save was not confirmed. Retry the same form, or refresh before editing a newer version.", "ذخیره تأیید نشد. همین فرم را دوباره ارسال کنید؛ برای ویرایش نسخهٔ جدید ابتدا وضعیت را به‌روزرسانی کنید.")}</p>}
        <div className="telegram-form-actions"><button type="submit" disabled={saving}>{saving ? tr("Saving…", "در حال ذخیره…") : tr("Save store", "ذخیرهٔ فروشگاه")}</button><button type="button" onClick={() => { setEditing(false); setMessage(""); }} disabled={saving}>{tr("Cancel", "انصراف")}</button></div>
      </form>}
      <p className="telegram-footnote">{tr("Each store keeps its customers, orders and reward balance separate. A successful payment is followed by delivery only when PRO verifies the service is ready.", "مشتری‌ها، سفارش‌ها و موجودی پاداش هر فروشگاه مستقل است. پس از پرداخت موفق، تحویل زمانی انجام می‌شود که آماده‌بودن سرویس در PRO تأیید شود.")}</p>
    </>}
  </section>;
}
