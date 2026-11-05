import { useEffect, useState, type FormEvent } from "react";
import { api, type Language } from "../session";
import type { Seller } from "./sellerManagement";
import { catalogOrderEligible, validCatalogProduct, validCatalogOrderRequest, type CatalogProduct } from "./sellerCatalogOrdersV1";

const copy = {
 en: { title: "Order from catalog", help: "Select a published unlimited product. Each month is 30 days. This records a credit charge; payment and activation are separate.",
  loading: "Loading products…", failed: "Products could not be verified. Refresh to try again.", empty: "No eligible unlimited products on this page.",
  product: "Product", choose: "Select a product", refresh: "Refresh products", previous: "Previous products", next: "Next products",
  client: "Client ID", name: "Client name", reason: "Reason", submit: "Record catalog order", days: "days", users: "users",
  price: "Catalog price (toman)", debit: "Unlimited units to charge", invalid: "Check the product, client details and reason." },
 fa: { title: "سفارش از محصولات", help: "یک محصول نامحدود منتشرشده انتخاب کنید. هر ماه ۳۰ روز است. این عملیات سهمیه را کسر می‌کند؛ پرداخت و فعال‌سازی جداگانه‌اند.",
  loading: "در حال دریافت محصولات…", failed: "محصولات تأیید نشدند؛ دوباره دریافت کنید.", empty: "در این صفحه محصول نامحدود قابل سفارش نیست.",
  product: "محصول", choose: "انتخاب محصول", refresh: "تازه‌سازی محصولات", previous: "محصولات قبلی", next: "محصولات بعدی",
  client: "شناسه مشتری", name: "نام مشتری", reason: "دلیل", submit: "ثبت سفارش محصول", days: "روز", users: "کاربر",
  price: "قیمت محصول (تومان)", debit: "واحد نامحدود قابل کسر", invalid: "محصول، اطلاعات مشتری و دلیل را بررسی کنید." }
};
export default function SellerCatalogOrdersV1({ seller, language, blocked, allowed, onSubmit }: {
 seller: Seller; language: Language; blocked: boolean; allowed: boolean; onSubmit: (body: Record<string, unknown>) => void;
}) {
 const t = copy[language], rep = seller.representative_id;
 const [offset, setOffset] = useState(0), [refresh, setRefresh] = useState(0);
 const [products, setProducts] = useState<CatalogProduct[]>([]), [more, setMore] = useState(false);
 const [status, setStatus] = useState<"loading" | "ready" | "failed">("loading"), [loaded, setLoaded] = useState("");
 const [selected, setSelected] = useState(""), [client, setClient] = useState(""), [name, setName] = useState(""), [reason, setReason] = useState("");
 const [invalid, setInvalid] = useState(false);
 const key = rep + ":" + offset + ":" + refresh + ":" + allowed;
 const ready = loaded === key && status === "ready" && allowed;
 const chosen = ready ? products.find(p => p.id === selected) : undefined;
 useEffect(() => {
  const controller = new AbortController();
  setProducts([]); setSelected(""); setMore(false); setLoaded(""); setStatus("loading");
  if (!allowed) return () => controller.abort();
  const q = new URLSearchParams({ representative_id: rep, state: "published", limit: "100", offset: String(offset), q: "" });
  void api<unknown>("/api/v1/workspace/products?" + q, { signal: controller.signal }).then(value => {
   const v = value as { products?: unknown[]; representative_id?: string; state?: string; limit?: number; offset?: number; q?: string; total?: number; has_more?: boolean };
   if (!v || !Array.isArray(v.products) || v.products.length > 100 || v.representative_id !== rep || v.state !== "published" ||
     v.limit !== 100 || v.offset !== offset || v.q !== "" || !Number.isSafeInteger(v.total) || Number(v.total) < offset + v.products.length ||
     typeof v.has_more !== "boolean" || v.has_more !== (offset + v.products.length < Number(v.total)) ||
     v.products.some(p => !validCatalogProduct(p, rep)) || new Set(v.products.map(p => (p as CatalogProduct).id)).size !== v.products.length) throw new Error("invalid_catalog_page");
   if (controller.signal.aborted) return;
   setProducts((v.products as CatalogProduct[]).filter(catalogOrderEligible)); setMore(v.has_more); setLoaded(key); setStatus("ready");
  }).catch(() => { if (!controller.signal.aborted) { setStatus("failed"); setLoaded(""); setProducts([]); } });
  return () => controller.abort();
 }, [key, rep, offset, allowed]);
 function submit(e: FormEvent) {
  e.preventDefault();
  if (!chosen || blocked || !allowed || !seller.enabled) return;
  const body = { seller_id: seller.id, product_id: chosen.id, expected_product_revision: chosen.revision,
   expected_price_toman: chosen.price_toman, expected_duration_days: chosen.duration_days, expected_user_count: chosen.ip_count,
   client_id: client.trim(), name: name.trim(), reason: reason.trim() };
  if (!validCatalogOrderRequest({ ...body, event_id: "form-validation-only", representative_id: rep }, rep)) { setInvalid(true); return; }
  setInvalid(false); onSubmit(body);
 }
 if (!allowed) return null;
 return <section className="seller-client-orders" aria-label="Seller catalog orders">
  <h4>{t.title}</h4><p>{t.help}</p>
  <button type="button" disabled={blocked} onClick={() => setRefresh(n => n + 1)}>{t.refresh}</button>
  {!ready ? <p role="status">{status === "failed" ? t.failed : t.loading}</p> : <>
   <form aria-label="Create catalog client" onSubmit={submit}><fieldset disabled={blocked || !seller.enabled}>
    <label>{t.product}<select aria-label="Catalog order product" value={selected} onChange={e => setSelected(e.target.value)}>
     <option value="">{t.choose}</option>{products.map(p => <option key={p.id} value={p.id}>{p.name} · {p.duration_days} {t.days} · {p.ip_count} {t.users}</option>)}
    </select></label>
    {!products.length && <p>{t.empty}</p>}
    {chosen && <p aria-label="Catalog order terms">{t.price}: {chosen.price_toman.toLocaleString(language)} · {t.debit}: {chosen.duration_days / 30 * chosen.ip_count}</p>}
    <div className="seller-form-grid">
     <label>{t.client}<input aria-label="Catalog client ID" required maxLength={64} value={client} onChange={e => setClient(e.target.value)}/></label>
     <label>{t.name}<input aria-label="Catalog client name" required maxLength={128} value={name} onChange={e => setName(e.target.value)}/></label>
    </div>
    <label>{t.reason}<input aria-label="Catalog order reason" required maxLength={200} value={reason} onChange={e => setReason(e.target.value)}/></label>
    {invalid && <p role="alert">{t.invalid}</p>}
    <button type="submit" disabled={!chosen || BigInt(seller.reserved_units) < BigInt(chosen.duration_days / 30 * chosen.ip_count)}>{t.submit}</button>
   </fieldset></form>
   <div className="seller-pagination"><button type="button" disabled={blocked || offset === 0} onClick={() => setOffset(n => Math.max(0, n - 100))}>{t.previous}</button>
    <button type="button" disabled={blocked || !more || offset >= 1_000_000} onClick={() => setOffset(n => n + 100)}>{t.next}</button></div>
  </>}
 </section>;
}
