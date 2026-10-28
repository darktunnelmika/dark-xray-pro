import type { AppUser } from "../session";
import { exactAmount, maxSellerBytes, maxSellerUnits, sellerID } from "./sellerManagement";

export type SellerOrderKind = "client_create" | "client_extend";
export type SellerOrderAccess = { enabled: boolean; create: boolean; renew: boolean };
export type SellerOrderClient = {
  id: string; representative_id: string; name: string; uuid: string; plan_kind: "volume" | "unlimited";
  ip_count: number; volume_bytes: string; duration_days: number; charged_volume_bytes: string;
  charged_unlimited_units: string; expires_unix: number; created_unix: number; deployment_state: string; archived: boolean;
};
export type SellerOrderReceipt = {
  event_id: string; actor_user_id: string; representative_id: string; seller_id: string; action: "create" | "extend";
  client: SellerOrderClient; previous_client?: SellerOrderClient; volume_debit_bytes: string; unlimited_debit_units: string;
  reserved_bytes_before: string; reserved_bytes_after: string; reserved_units_before: string; reserved_units_after: string;
  created_unix: number; replayed: boolean;
};
export const sellerOrderEvent = /^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$/;
const text = (v: unknown, max: number): v is string => typeof v === "string" && !!v.trim() && [...v].length <= max && !/[\u0000-\u001f\u007f-\u009f]/u.test(v);
const integer = (v: unknown, min: number, max: number): v is number => typeof v === "number" && Number.isSafeInteger(v) && v >= min && v <= max;

// Refresh the authenticated account alongside seller data. Session/menu caches
// alone do not establish that this HUB currently enables seller orders.
export function sellerOrderAccess(value: unknown, actor: AppUser, representative: string, manage: boolean): SellerOrderAccess | null {
  if (!value || typeof value !== "object") return null;
  const v = value as { user?: AppUser; capabilities?: { seller_orders_v1?: unknown } };
  const u = v.user;
  if (!u || u.id !== actor.id || u.role !== actor.role || u.representative_id !== actor.representative_id ||
      u.role === "representative" && u.representative_id !== representative) return null;
  const enabled = v.capabilities?.seller_orders_v1 === true;
  const allowed = u.role === "owner" || manage && u.permissions?.manage_sellers === true;
  return { enabled, create: enabled && allowed && (u.role === "owner" || u.permissions?.create_clients === true),
    renew: enabled && allowed && (u.role === "owner" || u.permissions?.renew_clients === true) };
}

export function sellerOrderVolume(raw: string, unit: string): string | null {
  const factor = unit === "GiB" ? 1_073_741_824n : unit === "MiB" ? 1_048_576n : unit === "bytes" ? 1n : null;
  if (factor === null || !exactAmount(raw, maxSellerBytes)) return null;
  const amount = BigInt(raw) * factor;
  return amount <= maxSellerBytes ? amount.toString() : null;
}
export function sellerOrderInteger(raw: string, min: number, max: number): number | null {
  if (!/^(0|[1-9][0-9]{0,3})$/.test(raw)) return null;
  const value = Number(raw);
  return integer(value, min, max) ? value : null;
}
export function sellerOrderPath(representative: string, seller: string, client?: string): string {
  const base = "/api/v1/workspace/representatives/" + encodeURIComponent(representative) + "/sellers/" + encodeURIComponent(seller) + "/clients";
  return client === undefined ? base : base + "/" + encodeURIComponent(client) + "/extend";
}

// The recovery journal accepts the same complete flat contract as the server;
// it cannot restore caller-supplied authority or a modified endpoint.
export function validSellerOrderRequest(body: Record<string, unknown>, kind: SellerOrderKind, representative: string): boolean {
  const fields = ["event_id", "representative_id", "seller_id", "client_id", "reason", ...(kind === "client_create"
    ? ["name", "duration_days", "ip_count", "volume_bytes", "unlimited"] : ["additional_days", "additional_volume_bytes"])];
  if (Object.keys(body).length !== fields.length || fields.some(key => !(key in body)) ||
      typeof body.event_id !== "string" || !sellerOrderEvent.test(body.event_id) || body.representative_id !== representative ||
      typeof body.seller_id !== "string" || !sellerID.test(body.seller_id) || typeof body.client_id !== "string" || !sellerID.test(body.client_id) || !text(body.reason, 200)) return false;
  if (kind === "client_create") return text(body.name, 128) && integer(body.duration_days, 1, 3650) && integer(body.ip_count, 0, 1000) &&
    exactAmount(body.volume_bytes, maxSellerBytes) && typeof body.unlimited === "boolean" &&
    (body.unlimited ? body.volume_bytes === "0" && body.ip_count > 0 : body.volume_bytes !== "0");
  return integer(body.additional_days, 0, 3650) && exactAmount(body.additional_volume_bytes, maxSellerBytes) &&
    (body.additional_days > 0 || body.additional_volume_bytes !== "0");
}

function validClient(c: SellerOrderClient, representative: string, client: string): boolean {
  return !!c && c.id === client && c.representative_id === representative && text(c.name, 128) &&
    typeof c.uuid === "string" && /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i.test(c.uuid) &&
    (c.plan_kind === "volume" || c.plan_kind === "unlimited") && integer(c.ip_count, 0, 1000) && integer(c.duration_days, 1, 3650) &&
    exactAmount(c.volume_bytes, maxSellerBytes) && exactAmount(c.charged_volume_bytes, maxSellerBytes) && exactAmount(c.charged_unlimited_units, maxSellerUnits) &&
    integer(c.expires_unix, 1, Number.MAX_SAFE_INTEGER) && integer(c.created_unix, 1, Number.MAX_SAFE_INTEGER) &&
    text(c.deployment_state, 80) && c.archived === false;
}

export function validSellerOrderReceipt(value: unknown, body: Record<string, unknown>, kind: SellerOrderKind, actor: string): value is SellerOrderReceipt {
  if (!value || typeof value !== "object" || !validSellerOrderRequest(body, kind, String(body.representative_id))) return false;
  const v = value as SellerOrderReceipt, rep = String(body.representative_id), client = String(body.client_id);
  if (v.event_id !== body.event_id || v.actor_user_id !== actor || v.representative_id !== rep || v.seller_id !== body.seller_id ||
      v.action !== (kind === "client_create" ? "create" : "extend") || typeof v.replayed !== "boolean" ||
      !integer(v.created_unix, 1, Number.MAX_SAFE_INTEGER) || !validClient(v.client, rep, client) ||
      !exactAmount(v.volume_debit_bytes, maxSellerBytes) || !exactAmount(v.unlimited_debit_units, maxSellerUnits) ||
      !exactAmount(v.reserved_bytes_before, maxSellerBytes) || !exactAmount(v.reserved_bytes_after, maxSellerBytes) ||
      !exactAmount(v.reserved_units_before, maxSellerUnits) || !exactAmount(v.reserved_units_after, maxSellerUnits)) return false;
  const bytes = BigInt(v.volume_debit_bytes), units = BigInt(v.unlimited_debit_units), c = v.client;
  if (BigInt(v.reserved_bytes_before) - BigInt(v.reserved_bytes_after) !== bytes ||
      BigInt(v.reserved_units_before) - BigInt(v.reserved_units_after) !== units || bytes > 0n && units > 0n) return false;
  if (kind === "client_create") {
    const expectedUnits = body.unlimited ? (BigInt(Number(body.duration_days)) * BigInt(Number(body.ip_count)) + 29n) / 30n : 0n;
    return v.previous_client === undefined && c.name === body.name && c.duration_days === body.duration_days && c.ip_count === body.ip_count &&
      c.plan_kind === (body.unlimited ? "unlimited" : "volume") && c.volume_bytes === body.volume_bytes &&
      c.charged_volume_bytes === v.volume_debit_bytes && c.charged_unlimited_units === v.unlimited_debit_units &&
      bytes === BigInt(String(body.volume_bytes)) && units === expectedUnits && c.created_unix === v.created_unix &&
      c.expires_unix === c.created_unix + c.duration_days * 86400 && c.deployment_state === "pending_node_apply";
  }
  const p = v.previous_client, days = Number(body.additional_days), added = BigInt(String(body.additional_volume_bytes));
  if (!p || !validClient(p, rep, client) || p.uuid !== c.uuid || p.name !== c.name || p.plan_kind !== c.plan_kind ||
      p.ip_count !== c.ip_count || p.created_unix !== c.created_unix || p.deployment_state !== c.deployment_state ||
      c.duration_days !== p.duration_days + days || BigInt(c.volume_bytes) !== BigInt(p.volume_bytes) + added ||
      BigInt(c.charged_volume_bytes) !== BigInt(p.charged_volume_bytes) + bytes ||
      BigInt(c.charged_unlimited_units) !== BigInt(p.charged_unlimited_units) + units ||
      c.expires_unix !== (days > 0 ? Math.max(p.expires_unix, v.created_unix) : p.expires_unix) + days * 86400) return false;
  if (c.plan_kind === "volume") return bytes === added && units === 0n;
  return added === 0n && bytes === 0n && c.ip_count > 0 &&
    units === (BigInt(c.duration_days) * BigInt(c.ip_count) + 29n) / 30n - BigInt(p.charged_unlimited_units);
}
