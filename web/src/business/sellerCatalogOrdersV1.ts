import { sellerID } from "./sellerManagement";
import { validSellerOrderReceipt, type SellerOrderReceipt } from "./sellerOrdersV1";

export type CatalogProduct = { id: string; representative_id: string; name: string; status: string; plan_kind: string;
  revision: number; duration_days: number; ip_count: number; volume_bytes: number; price_toman: number };
export type CatalogReceipt = { product_id: string; product_revision: number; price_toman: number; duration_days: number;
  months: number; user_count: number; checkout_enabled: false; payment_applied: false };
const integer = (v: unknown, lo: number, hi: number): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= lo && v <= hi;
const text = (v: unknown, n: number): v is string => typeof v === "string" && !!v.trim() && [...v].length <= n && !/[\u0000-\u001f\u007f-\u009f]/u.test(v);
export function validCatalogProduct(v: unknown, rep: string): v is CatalogProduct {
  if (!v || typeof v !== "object") return false;
  const p = v as CatalogProduct;
  return typeof p.id === "string" && sellerID.test(p.id) && p.representative_id === rep && text(p.name, 128) &&
    p.status === "published" && (p.plan_kind === "volume" || p.plan_kind === "unlimited") &&
    integer(p.revision, 1, 1_000_000_000) && integer(p.duration_days, 1, 3650) && integer(p.ip_count, 0, 1000) &&
    integer(p.volume_bytes, 0, 9_000_000_000_000_000) && integer(p.price_toman, 1, 1_000_000_000_000) &&
    (p.plan_kind === "unlimited" ? p.volume_bytes === 0 && p.ip_count > 0 : p.volume_bytes > 0);
}
export const catalogOrderEligible = (p: CatalogProduct) => p.plan_kind === "unlimited" && p.duration_days % 30 === 0;
export function catalogOrderPath(rep: string, seller: string, product: string): string {
  return "/api/v1/workspace/representatives/" + encodeURIComponent(rep) + "/sellers/" + encodeURIComponent(seller) +
    "/catalog/" + encodeURIComponent(product) + "/clients";
}
export function validCatalogOrderRequest(b: Record<string, unknown>, rep: string): boolean {
  const keys = ["event_id", "representative_id", "seller_id", "product_id", "expected_product_revision", "expected_price_toman",
    "expected_duration_days", "expected_user_count", "client_id", "name", "reason"];
  return Object.keys(b).length === keys.length && keys.every(k => k in b) &&
    typeof b.event_id === "string" && /^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/.test(b.event_id) &&
    b.representative_id === rep && [b.seller_id, b.product_id, b.client_id].every(v => typeof v === "string" && sellerID.test(v)) &&
    integer(b.expected_product_revision, 1, 1_000_000_000) && integer(b.expected_price_toman, 1, 1_000_000_000_000) &&
    integer(b.expected_duration_days, 30, 3650) && b.expected_duration_days % 30 === 0 &&
    integer(b.expected_user_count, 1, 1000) && text(b.name, 128) && text(b.reason, 200);
}
export function validCatalogOrderReceipt(v: unknown, b: Record<string, unknown>, actor: string): v is SellerOrderReceipt {
  if (!validCatalogOrderRequest(b, String(b.representative_id))) return false;
  const manual = { event_id: b.event_id, representative_id: b.representative_id, seller_id: b.seller_id, client_id: b.client_id,
    name: b.name, reason: b.reason, duration_days: b.expected_duration_days, ip_count: b.expected_user_count, volume_bytes: "0", unlimited: true };
  if (!validSellerOrderReceipt(v, manual, "client_create", actor)) return false;
  const c = v.catalog;
  return !!c && c.product_id === b.product_id && c.product_revision === b.expected_product_revision &&
    c.price_toman === b.expected_price_toman && c.duration_days === b.expected_duration_days &&
    c.months === Number(b.expected_duration_days) / 30 && c.user_count === b.expected_user_count &&
    c.checkout_enabled === false && c.payment_applied === false;
}
