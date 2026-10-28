export type SellerPermission = { representative_id: string; manage_sellers: boolean; revision: number };
export type Seller = {
  id: string; representative_id: string; name: string; telegram_user_id: string;
  enabled: boolean; reserved_bytes: string; reserved_units: string; revision: number;
};
export type SellerPage = { representative_id: string; sellers: Seller[]; limit: number; offset: number };
export type SellerQuotaReceipt = {
  event_id: string; representative_id: string; seller_id: string; action: string;
  representative_free_bytes: string; representative_free_units: string;
  seller_reserved_bytes: string; seller_reserved_units: string; replayed: boolean;
};
export const sellerID = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/;
export const maxSellerBytes = 1_000_000_000_000_000_000n;
export const maxSellerUnits = 1_000_000_000n;
export function exactAmount(value: unknown, max: bigint): value is string {
  return typeof value === "string" && /^(0|[1-9][0-9]{0,18})$/.test(value) && BigInt(value) <= max;
}
export function validSellerPermission(v: SellerPermission, representative: string): boolean {
  return !!v && v.representative_id === representative && typeof v.manage_sellers === "boolean" &&
    Number.isSafeInteger(v.revision) && v.revision >= 0;
}
export function validSeller(v: Seller, representative: string): boolean {
  return !!v && typeof v.id === "string" && sellerID.test(v.id) && v.representative_id === representative &&
    typeof v.name === "string" && [...v.name].length > 0 && [...v.name].length <= 128 &&
    typeof v.enabled === "boolean" && exactAmount(v.telegram_user_id, 9_223_372_036_854_775_807n) &&
    v.telegram_user_id !== "0" && exactAmount(v.reserved_bytes, maxSellerBytes) &&
    exactAmount(v.reserved_units, maxSellerUnits) && Number.isSafeInteger(v.revision) && v.revision >= 0;
}
export function validSellerPage(v: SellerPage, representative: string, offset: number): boolean {
  return !!v && v.representative_id === representative && v.limit === 25 && v.offset === offset &&
    Array.isArray(v.sellers) && v.sellers.length <= 25 && v.sellers.every(s => validSeller(s, representative)) &&
    new Set(v.sellers.map(s => s.id)).size === v.sellers.length;
}
export function validQuotaReceipt(v: SellerQuotaReceipt, representative: string, seller: string, event: string, action: string): boolean {
  return !!v && v.representative_id === representative && v.seller_id === seller && v.event_id === event &&
    v.action === action && typeof v.replayed === "boolean" &&
    exactAmount(v.representative_free_bytes, maxSellerBytes) && exactAmount(v.representative_free_units, maxSellerUnits) &&
    exactAmount(v.seller_reserved_bytes, maxSellerBytes) && exactAmount(v.seller_reserved_units, maxSellerUnits);
}
export function quotaInput(volume: string, unit: string, units: string): { bytes: string; units: string } | null {
  const factors: Record<string, bigint> = { GiB: 1_073_741_824n, MiB: 1_048_576n, bytes: 1n };
  if (!Object.prototype.hasOwnProperty.call(factors, unit) || !exactAmount(volume, maxSellerBytes) || !exactAmount(units, maxSellerUnits)) return null;
  const bytes = BigInt(volume) * factors[unit];
  if (bytes > maxSellerBytes || (bytes === 0n && units === "0")) return null;
  return { bytes: bytes.toString(), units };
}
export function exactText(value: string): string { return BigInt(value).toLocaleString("en-US"); }
export function gibText(value: string): string {
  const bytes = BigInt(value), unit = 1_073_741_824n;
  return `${(bytes / unit).toLocaleString("en-US")}.${((bytes % unit) * 100n / unit).toString().padStart(2, "0")}`;
}
