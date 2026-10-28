import { useEffect, useState, type FormEvent } from "react";
import type { Language } from "../session";
import { exactText, type Seller } from "./sellerManagement";
import { sellerOrderInteger, sellerOrderVolume, validSellerOrderRequest, type SellerOrderAccess, type SellerOrderKind, type SellerOrderReceipt } from "./sellerOrdersV1";

const words = {
  en: {
    title: "Client orders", note: "Create or renew a client using this seller's reserved credit.",
    off: "Client orders from seller reserves are not enabled on this HUB.", unavailable: "Order access could not be verified. Refresh before continuing.",
    denied: "Client creation or renewal requires the corresponding permission and an enabled seller.",
    create: "Create client", renew: "Renew client", client: "Client ID", name: "Client name", plan: "Plan", volume: "Volume", unlimited: "Unlimited",
    days: "Duration (days)", addedDays: "Additional days", users: "User/IP limit", amount: "Volume amount", addedVolume: "Additional volume",
    unit: "Volume unit", bytes: "bytes", reason: "Reason", saveCreate: "Record client creation", saveRenew: "Record renewal",
    renewHelp: "Enter a client issued by this seller. Use zero added volume for a time-only renewal or an unlimited plan; its existing user count determines the unit charge.",
    billing: "The receipt records the credit charge. Client activation and subscription delivery follow separately.",
    invalid: "Check the client ID, name, whole-number duration, amount and reason.",
    receipt: "Recorded order receipt", replay: "Recovered existing receipt", event: "Request ID", charged: "Charged volume", units: "Charged unlimited units",
    reserve: "Reserved volume after this order", reserveUnits: "Reserved unlimited units after this order", historical: "These are the balances recorded for this order. Current seller balances are shown above.",
  },
  fa: {
    title: "سفارش مشتری", note: "ساخت یا تمدید مشتری با استفاده از سهمیهٔ رزروشدهٔ همین فروشنده",
    off: "ثبت سفارش از سهمیهٔ فروشنده روی این هاب فعال نیست.", unavailable: "دسترسی ثبت سفارش تأیید نشد؛ ابتدا اطلاعات را تازه‌سازی کنید.",
    denied: "ساخت یا تمدید مشتری به مجوز همان عملیات و فروشندهٔ فعال نیاز دارد.",
    create: "ساخت مشتری", renew: "تمدید مشتری", client: "شناسه مشتری", name: "نام مشتری", plan: "نوع سرویس", volume: "حجمی", unlimited: "نامحدود",
    days: "مدت (روز)", addedDays: "روز اضافه", users: "تعداد کاربر / IP", amount: "مقدار حجم", addedVolume: "حجم اضافه",
    unit: "واحد حجم", bytes: "بایت", reason: "دلیل", saveCreate: "ثبت ساخت مشتری", saveRenew: "ثبت تمدید",
    renewHelp: "شناسهٔ مشتری ساخته‌شده توسط همین فروشنده را وارد کنید. برای تمدید زمانی یا سرویس نامحدود، حجم اضافه را صفر بگذارید؛ هزینهٔ واحد بر اساس تعداد کاربر فعلی محاسبه می‌شود.",
    billing: "رسید، کسر سهمیه را ثبت می‌کند. فعال‌سازی مشتری و تحویل اشتراک مرحله‌ای جداگانه است.",
    invalid: "شناسه، نام، تعداد روز صحیح، مقدار حجم و دلیل را بررسی کنید.",
    receipt: "رسید سفارش ثبت‌شده", replay: "رسید قبلی بازیابی شد", event: "شناسه درخواست", charged: "حجم کسرشده", units: "واحد نامحدود کسرشده",
    reserve: "ماندهٔ حجم پس از این سفارش", reserveUnits: "ماندهٔ واحد نامحدود پس از این سفارش", historical: "این مانده‌ها مربوط به زمان همین سفارش است. ماندهٔ فعلی فروشنده در بالا نمایش داده می‌شود.",
  },
} as const;

export default function SellerClientOrdersV1({ seller, access, blocked, language, receipt, onSubmit }: {
  seller: Seller; access: SellerOrderAccess | null; blocked: boolean; language: Language; receipt: SellerOrderReceipt | null;
  onSubmit: (kind: SellerOrderKind, body: Record<string, unknown>) => void;
}) {
  const t = words[language];
  const [mode, setMode] = useState<SellerOrderKind>("client_create");
  const [client, setClient] = useState(""), [name, setName] = useState(""), [plan, setPlan] = useState("volume");
  const [days, setDays] = useState("30"), [users, setUsers] = useState("1"), [volume, setVolume] = useState("0"), [unit, setUnit] = useState("GiB");
  const [reason, setReason] = useState(""), [invalid, setInvalid] = useState(false);
  const create = mode === "client_create";
  const allowed = seller.enabled && (create ? access?.create : access?.renew) === true;
  useEffect(() => {
    if (!access?.create && access?.renew) setMode("client_extend");
  }, [access?.create, access?.renew]);
  useEffect(() => { setReason(""); setInvalid(false); }, [receipt?.event_id]);
  function submit(e: FormEvent) {
    e.preventDefault();
    if (blocked || !allowed) return;
    const duration = sellerOrderInteger(days, create ? 1 : 0, 3650), ip = sellerOrderInteger(users, create && plan === "unlimited" ? 1 : 0, 1000);
    const amount = create && plan === "unlimited" ? "0" : sellerOrderVolume(volume, unit);
    const body: Record<string, unknown> = { seller_id: seller.id, client_id: client.trim(), reason: reason.trim(), ...(create
      ? { name: name.trim(), duration_days: duration, ip_count: ip, volume_bytes: amount, unlimited: plan === "unlimited" }
      : { additional_days: duration, additional_volume_bytes: amount }) };
    if (!validSellerOrderRequest({ ...body, representative_id: seller.representative_id, event_id: "form-validation-only" }, mode, seller.representative_id)) {
      setInvalid(true); return;
    }
    setInvalid(false); onSubmit(mode, body);
  }
  return <section className="seller-client-orders" aria-label="Seller client orders">
    <h4>{t.title}</h4><p>{t.note}</p>
    {!access ? <p role="status">{t.unavailable}</p> : !access.enabled ? <p role="status">{t.off}</p> : <>
      {(!seller.enabled || !access.create && !access.renew) && <p role="status">{t.denied}</p>}
      <div className="seller-order-tabs">
        <button type="button" aria-pressed={create} disabled={blocked || !seller.enabled || !access.create} onClick={() => { setMode("client_create"); setInvalid(false); }}>{t.create}</button>
        <button type="button" aria-pressed={!create} disabled={blocked || !seller.enabled || !access.renew} onClick={() => { setMode("client_extend"); setVolume("0"); setInvalid(false); }}>{t.renew}</button>
      </div>
      <form aria-label={create ? "Create seller client" : "Renew seller client"} onSubmit={submit}>
        <fieldset disabled={blocked || !allowed}>
          <div className="seller-form-grid">
            <label>{t.client}<input aria-label="Order client ID" autoComplete="off" required maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,63}" value={client} onChange={e => setClient(e.target.value)}/></label>
            {create && <label>{t.name}<input aria-label="Order client name" required maxLength={128} value={name} onChange={e => setName(e.target.value)}/></label>}
            {create && <label>{t.plan}<select aria-label="Order plan" value={plan} onChange={e => setPlan(e.target.value)}><option value="volume">{t.volume}</option><option value="unlimited">{t.unlimited}</option></select></label>}
            <label>{create ? t.days : t.addedDays}<input aria-label="Order days" inputMode="numeric" required pattern="(0|[1-9][0-9]*)" maxLength={4} value={days} onChange={e => setDays(e.target.value)}/></label>
            {create && <label>{t.users}<input aria-label="Order users" inputMode="numeric" required pattern="(0|[1-9][0-9]*)" maxLength={4} value={users} onChange={e => setUsers(e.target.value)}/></label>}
            {(!create || plan === "volume") && <>
              <label>{create ? t.amount : t.addedVolume}<input aria-label="Order volume" inputMode="numeric" required pattern="(0|[1-9][0-9]*)" maxLength={19} value={volume} onChange={e => setVolume(e.target.value)}/></label>
              <label>{t.unit}<select aria-label="Order volume unit" value={unit} onChange={e => setUnit(e.target.value)}><option>GiB</option><option>MiB</option><option value="bytes">{t.bytes}</option></select></label>
            </>}
          </div>
          {!create && <p>{t.renewHelp}</p>}
          <label>{t.reason}<input aria-label="Order reason" required maxLength={200} value={reason} onChange={e => setReason(e.target.value)}/></label>
          {invalid && <p role="alert">{t.invalid}</p>}
          <p>{t.billing}</p><button type="submit">{create ? t.saveCreate : t.saveRenew}</button>
        </fieldset>
      </form>
    </>}
    {receipt && <section className="seller-order-receipt" aria-label="Seller order receipt" role="status">
      <h4>{receipt.replayed ? t.replay : t.receipt}</h4>
      <dl><div><dt>{t.client}</dt><dd><bdi>{receipt.client.id}</bdi> · {receipt.client.name}</dd></div>
        <div><dt>{t.event}</dt><dd><bdi>{receipt.event_id}</bdi></dd></div>
        <div><dt>{t.charged}</dt><dd><bdi>{exactText(receipt.volume_debit_bytes)}</bdi> {t.bytes}</dd></div>
        <div><dt>{t.units}</dt><dd><bdi>{exactText(receipt.unlimited_debit_units)}</bdi></dd></div>
        <div><dt>{t.reserve}</dt><dd><bdi>{exactText(receipt.reserved_bytes_after)}</bdi> {t.bytes}</dd></div>
        <div><dt>{t.reserveUnits}</dt><dd><bdi>{exactText(receipt.reserved_units_after)}</bdi></dd></div></dl>
      <p>{t.historical}</p><p>{t.billing}</p>
    </section>}
  </section>;
}
