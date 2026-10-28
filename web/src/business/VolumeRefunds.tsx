import { useEffect, useRef, useState } from "react";
import { RefreshCw, RotateCcw } from "lucide-react";
import { api, ApiError } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";

type Lang = "en" | "fa";
type Load = "loading" | "ready" | "off" | "denied" | "failed";
const states = ["not_due", "not_volume", "owner_exempt", "already_refunded", "refunded", "funding_unproven", "usage_unknown", "waiting_final_usage", "credit_limit", "ready"] as const;
type RefundState = typeof states[number];
type Status = {
  request: { client_id: string; expected_expires_unix: number; expected_charged_volume_bytes: string };
  representative_id: string; generation: string; trigger?: "expired" | "archived";
  state: RefundState; volume_bytes: string; funded_bytes?: string; used_bytes?: string; refund_bytes?: string;
};
type Row = { generation: string; client_id: string; representative_id: string; trigger: "expired" | "archived"; refunded_bytes: string; used_bytes: string; volume_before_bytes: string; created_unix: number };
type Page = { representative_id: string; refunds: Row[]; limit: number; offset: number };
const words = {
  en: {
    title: "Unused volume credit", history: "Returned volume history", refresh: "Refresh volume returns", bytes: "bytes",
    loading: "Reading volume return accounting…", off: "Automatic volume returns have not been enabled on this HUB yet.",
    denied: "This account cannot view this financial report.", failed: "The volume return could not be verified. No refund or zero amount is assumed.",
    empty: "No completed volume returns in this page.", credited: "Returned to representative credit", pending: "Volume awaiting return", used: "Recorded usage",
    expired: "Expiry", archived: "Manual deletion", previous: "Previous", next: "Next",
    not_due: "Unused volume is returned automatically after expiry or manual deletion, once final usage is recorded.",
    not_volume: "This is an unlimited plan. Its token credit is unchanged.", owner_exempt: "Owner configurations do not use representative volume credit.",
    already_refunded: "The unused volume has been returned to the same representative.", refunded: "The unused volume has been returned to the same representative.",
    funding_unproven: "The payment history for this volume is incomplete; a return cannot yet be verified.",
    usage_unknown: "Historical consumption is not fully known. The return is waiting for verified usage.",
    waiting_final_usage: "Waiting for the final usage report. Credit will be returned automatically after settlement.",
    credit_limit: "The representative balance is at its allowed limit. The return remains pending.",
    ready: "Final usage is verified. The automatic return is queued.",
  },
  fa: {
    title: "برگشت حجم مصرف‌نشده", history: "سابقهٔ برگشت حجم", refresh: "به‌روزرسانی برگشت حجم", bytes: "بایت",
    loading: "در حال خواندن حسابداری برگشت حجم…", off: "برگشت خودکار حجم هنوز در این HUB فعال نشده است.",
    denied: "این حساب اجازهٔ مشاهدهٔ این گزارش مالی را ندارد.", failed: "برگشت حجم تأیید نشد؛ مبلغ صفر یا برگشت موفق فرض نمی‌شود.",
    empty: "در این صفحه برگشت حجم تکمیل‌شده‌ای وجود ندارد.", credited: "برگشته به اعتبار نماینده", pending: "حجم در انتظار برگشت", used: "مصرف ثبت‌شده",
    expired: "انقضا", archived: "حذف دستی", previous: "قبلی", next: "بعدی",
    not_due: "پس از انقضا یا حذف دستی، حجم مصرف‌نشده با ثبت مصرف نهایی خودکار به نماینده برمی‌گردد.",
    not_volume: "این کانفیگ نامحدود است؛ اعتبار توکنی آن تغییری نمی‌کند.", owner_exempt: "کانفیگ مالک از اعتبار حجمی نماینده استفاده نمی‌کند.",
    already_refunded: "حجم مصرف‌نشده به اعتبار همان نماینده برگشته است.", refunded: "حجم مصرف‌نشده به اعتبار همان نماینده برگشته است.",
    funding_unproven: "سابقهٔ پرداخت این حجم کامل نیست؛ برگشت اعتبار هنوز قابل تأیید نیست.",
    usage_unknown: "مصرف تاریخی کامل مشخص نیست؛ برگشت اعتبار منتظر تأیید مصرف است.",
    waiting_final_usage: "منتظر گزارش نهایی مصرف؛ پس از تسویه، اعتبار خودکار برمی‌گردد.",
    credit_limit: "اعتبار نماینده به سقف مجاز رسیده است؛ برگشت حجم در انتظار می‌ماند.",
    ready: "مصرف نهایی تأیید شده و برگشت خودکار حجم در صف ثبت است.",
  },
};
function bytes(v: unknown): v is string {
  return typeof v === "string" && /^(0|[1-9][0-9]{0,18})$/.test(v) && BigInt(v) <= 9_223_372_036_854_775_807n;
}
const hash = (v: unknown) => typeof v === "string" && /^[a-f0-9]{64}$/.test(v);
const id = (v: unknown) => typeof v === "string" && /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/.test(v);
const amount = (v: string, language: Lang) => BigInt(v).toLocaleString(language === "fa" ? "fa-IR" : "en-US");
function failure(error: unknown): Load {
  return error instanceof ApiError && error.status === 404 ? "off" : error instanceof ApiError && [401, 403].includes(error.status) ? "denied" : "failed";
}

export function ClientVolumeRefund({ model, clientID }: { model: BusinessModel; clientID: string }) {
  const t = words[model.language], scope = model.session.user.role === "owner" ? "" : model.session.user.representative_id;
  const [status, setStatus] = useState<Status | null>(null), [load, setLoad] = useState<Load>("loading"), [reload, setReload] = useState(0);
  const refreshedGeneration = useRef("");
  useEffect(() => {
    const c = new AbortController();
    const timer = setTimeout(() => { c.abort(); setStatus(null); setLoad("failed"); }, 8000);
    setStatus(null); setLoad("loading");
    void api<Status>("/api/v1/workspace/clients/" + encodeURIComponent(clientID) + "/volume-refund", { signal: c.signal }).then(value => {
      if (value?.request?.client_id !== clientID || !id(value.representative_id) || scope && value.representative_id !== scope ||
          !hash(value.generation) || !states.includes(value.state) || !bytes(value.volume_bytes) || !bytes(value.request.expected_charged_volume_bytes) ||
          !Number.isSafeInteger(value.request.expected_expires_unix) || value.request.expected_expires_unix <= 0 ||
          ![value.funded_bytes, value.used_bytes, value.refund_bytes].every(v => v === undefined || bytes(v)) ||
          value.trigger !== undefined && !["expired", "archived"].includes(value.trigger) ||
          ["ready", "refunded", "already_refunded"].includes(value.state) && (!bytes(value.refund_bytes) || !bytes(value.used_bytes))) throw new Error("unverified_refund");
      if (!c.signal.aborted) { setStatus(value); setLoad("ready"); }
    }).catch(error => { if (!c.signal.aborted) setLoad(failure(error)); }).finally(() => clearTimeout(timer));
    return () => { clearTimeout(timer); c.abort(); };
  }, [clientID, scope, model.refreshKey, reload]);
  useEffect(() => {
    if (!status || !["ready", "waiting_final_usage"].includes(status.state)) return;
    const timer = setTimeout(() => setReload(n => n + 1), 5000);
    return () => clearTimeout(timer);
  }, [status]);
  const completed = status && ["refunded", "already_refunded"].includes(status.state);
  useEffect(() => {
    if (completed && status && refreshedGeneration.current !== status.generation) {
      refreshedGeneration.current = status.generation;
      model.reload();
    }
  }, [completed, status, model]);
  return <section className="business-panel" aria-label="Client volume return">
    <h3><RotateCcw size={17}/>{t.title}</h3>
    <button type="button" disabled={load === "loading"} onClick={() => setReload(n => n + 1)}><RefreshCw size={14}/>{t.refresh}</button>
    <p role="status">{load === "ready" && status ? t[status.state] : load === "ready" ? t.failed : t[load]}</p>
    {load === "ready" && status?.refund_bytes !== undefined && <p><strong>{completed ? t.credited : t.pending}: </strong><bdi>{amount(status.refund_bytes, model.language)}</bdi> {t.bytes}</p>}
    {load === "ready" && status?.used_bytes !== undefined && <p>{t.used}: <bdi>{amount(status.used_bytes, model.language)}</bdi> {t.bytes}</p>}
  </section>;
}

export default function VolumeRefundHistory({ model }: { model: BusinessModel }) {
  const t = words[model.language], scope = model.session.user.role === "owner" ? "" : model.session.user.representative_id;
  const [rows, setRows] = useState<Row[] | null>(null), [load, setLoad] = useState<Load>("loading");
  const [offset, setOffset] = useState(0), [reload, setReload] = useState(0);
  useEffect(() => {
    const c = new AbortController();
    const timer = setTimeout(() => { c.abort(); setRows(null); setLoad("failed"); }, 8000);
    setRows(null); setLoad("loading");
    void api<Page>("/api/v1/workspace/volume-refunds?limit=25&offset=" + offset, { signal: c.signal }).then(value => {
      if (!value || value.representative_id !== scope || value.limit !== 25 || value.offset !== offset || !Array.isArray(value.refunds) || value.refunds.length > 25 ||
          !value.refunds.every(row => hash(row.generation) && id(row.client_id) && id(row.representative_id) && (!scope || row.representative_id === scope) &&
            ["expired", "archived"].includes(row.trigger) && [row.refunded_bytes, row.used_bytes, row.volume_before_bytes].every(bytes) &&
            BigInt(row.refunded_bytes) <= BigInt(row.volume_before_bytes) && Number.isSafeInteger(row.created_unix) && row.created_unix > 0) ||
          new Set(value.refunds.map(row => row.generation)).size !== value.refunds.length) throw new Error("unverified_refund_history");
      if (!c.signal.aborted) { setRows(value.refunds); setLoad("ready"); }
    }).catch(error => { if (!c.signal.aborted) setLoad(failure(error)); }).finally(() => clearTimeout(timer));
    return () => { clearTimeout(timer); c.abort(); };
  }, [scope, offset, reload, model.refreshKey]);
  return <section className="business-panel" aria-label="Volume return history">
    <h3><RotateCcw size={17}/>{t.history}</h3>
    <button type="button" disabled={load === "loading"} onClick={() => setReload(n => n + 1)}><RefreshCw size={14}/>{t.refresh}</button>
    {load !== "ready" ? <p role="status">{t[load]}</p> : rows?.length === 0 ? <p>{t.empty}</p> : <div className="business-records">
      {rows?.map(row => <div key={row.generation}><strong><bdi>{row.client_id}</bdi> · {t[row.trigger]}</strong>
        <strong><bdi>{amount(row.refunded_bytes, model.language)}</bdi> {t.bytes}</strong>
        <span>{t.credited} · <bdi>{row.representative_id}</bdi></span>
        <span>{new Date(row.created_unix * 1000).toLocaleString(model.language === "fa" ? "fa-IR" : "en-US")}</span>
      </div>)}
    </div>}
    <div className="business-pagination"><button type="button" disabled={load === "loading" || offset === 0} onClick={() => setOffset(n => Math.max(0, n - 25))}>{t.previous}</button>
      <button type="button" disabled={load !== "ready" || rows?.length !== 25} onClick={() => setOffset(n => n + 25)}>{t.next}</button></div>
  </section>;
}
