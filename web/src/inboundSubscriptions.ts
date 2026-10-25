import { ApiError } from "./session";
import { assertExecutionPublic } from "./inboundExecution";

export const inboundSubscriptionFormats = ["uri", "json", "xray", "sing-box", "wireguard"] as const;
export type InboundSubscriptionView = { enabled: false; client_id: string } | {
  enabled: true; client_id: string; generation: number; state: "absent" | "active" | "revoked";
  available: boolean; formats: string[]; route_count: number; expires_unix_nano: number;
  path_verified: boolean; server_time_unix_milli: number;
};
export type InboundSubscriptionAttempt = {
  action: "issue" | "rotate" | "revoke"; event_id: string; expected_generation: number;
};
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);
const natural = (v: unknown): v is number => Number.isSafeInteger(v) && Number(v) >= 0;
function publicOnly(raw: unknown): void {
  assertExecutionPublic(raw);
  const walk = (v: unknown) => {
    if (Array.isArray(v)) { v.forEach(walk); return; }
    if (!object(v)) return;
    for (const [key, value] of Object.entries(v)) {
      if (["path", "content", "url", "csrf", "csrftoken"].includes(key.replace(/[_-]/g, "").toLowerCase())) throw Error("private_subscription_response");
      walk(value);
    }
  };
  walk(raw);
}
export function readInboundSubscriptionView(raw: unknown, clientID: string): InboundSubscriptionView {
  publicOnly(raw);
  if (!object(raw) || raw.client_id !== clientID || typeof raw.enabled !== "boolean") throw Error("subscription_scope");
  if (!raw.enabled) return { enabled: false, client_id: clientID };
  if (!natural(raw.generation) || !["absent", "active", "revoked"].includes(String(raw.state)) || (raw.state === "absent") !== (raw.generation === 0) ||
    typeof raw.available !== "boolean" || typeof raw.path_verified !== "boolean" || !Array.isArray(raw.formats) ||
    raw.formats.some(f => !inboundSubscriptionFormats.includes(f as typeof inboundSubscriptionFormats[number])) || new Set(raw.formats).size !== raw.formats.length ||
    !natural(raw.route_count) || raw.route_count > 100000 || typeof raw.expires_unix_nano !== "number" || !Number.isInteger(raw.expires_unix_nano) || raw.expires_unix_nano < 0 || raw.expires_unix_nano > 9_223_372_036_854_775_000 ||
    !natural(raw.server_time_unix_milli) || !raw.server_time_unix_milli ||
    raw.available && (!raw.route_count || !raw.formats.length || !raw.expires_unix_nano) ||
    !raw.available && (raw.route_count !== 0 || raw.formats.length !== 0 || raw.expires_unix_nano !== 0 || raw.path_verified)) throw Error("subscription_state");
  return raw as InboundSubscriptionView;
}
export function readInboundSubscriptionAttempt(raw: string | null): InboundSubscriptionAttempt | null {
  if (!raw) return null;
  if (raw.length > 512) throw Error("subscription_attempt");
  const value: unknown = JSON.parse(raw);
  publicOnly(value);
  if (!object(value) || !["issue", "rotate", "revoke"].includes(String(value.action)) || typeof value.event_id !== "string" ||
    !/^managed-sub-[a-z0-9-]{20,80}$/.test(value.event_id) || !natural(value.expected_generation) || value.expected_generation === Number.MAX_SAFE_INTEGER ||
    Object.keys(value).length !== 3) throw Error("subscription_attempt");
  return value as InboundSubscriptionAttempt;
}
export function readInboundSubscriptionReceipt(raw: unknown, clientID: string, attempt: InboundSubscriptionAttempt): void {
  publicOnly(raw);
  if (!object(raw) || raw.event_id !== attempt.event_id || raw.client_id !== clientID || raw.generation !== attempt.expected_generation + 1 ||
    raw.state !== (attempt.action === "revoke" ? "revoked" : "active") || typeof raw.replayed !== "boolean" || typeof raw.superseded !== "boolean") throw Error("subscription_receipt");
}
export function readInboundSubscriptionSecret(raw: unknown, clientID: string, generation: number): string {
  if (!object(raw) || raw.client_id !== clientID || raw.generation !== generation || typeof raw.token !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(raw.token) ||
    raw.path !== "/api/v1/inbound-subscriptions/" + raw.token || Object.keys(raw).some(key => !["client_id", "generation", "token", "path"].includes(key))) throw Error("subscription_secret");
  const bytes = atob(raw.token.replace(/-/g, "+").replace(/_/g, "/") + "=");
  if (bytes.length !== 32 || btoa(bytes).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") !== raw.token) throw Error("subscription_token_encoding");
  return raw.path as string;
}
// A malformed success or a server/transport timeout can follow a committed
// write. Retain its exact event and CAS until a valid receipt is recovered.
export const subscriptionRequestRejected = (error: unknown): boolean => error instanceof ApiError &&
  [400, 401, 403, 404, 405, 409, 410, 413, 415, 422].includes(error.status);
