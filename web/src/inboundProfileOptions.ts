import { canonicalFallbacks, canonicalSniffingOptions, trafficOptionIssues, trafficOptionsDraft, trafficOptionsPayload, type InboundSniffingOptions, type InboundFallback, type TrafficOptionsDraft } from "./inboundTrafficOptions";
import { canonicalClientOptions, canonicalHTTPTransportOptions, canonicalXHTTPOptions, networkOptionIssues, networkOptionsDraft, networkOptionsPayload, validAuthority, type InboundClientOptions, type InboundHTTPTransportOptions, type InboundXHTTPOptions, type NetworkOptionsDraft } from "./inboundNetworkOptions";

/** Public profile settings only; REALITY identity remains owned by the server. */
export type InboundProfileOptions = {
  grpc_authority?: string;
  grpc_multi_mode?: boolean;
  reality_xver?: number;
  reality_max_time_diff?: number;
  reality_min_client_ver?: string;
  reality_max_client_ver?: string;
  reality_show?: boolean;
  xhttp?: InboundXHTTPOptions;
  client?: InboundClientOptions;
  websocket?: InboundHTTPTransportOptions;
  http_upgrade?: InboundHTTPTransportOptions;
  sniffing?: InboundSniffingOptions;
  fallbacks?: InboundFallback[];
};

/** Text is retained while editing so partial or invalid input is never lost. */
export type ProfileOptionsDraft = TrafficOptionsDraft & NetworkOptionsDraft & {
  grpcAuthority: string;
  grpcMultiMode: boolean;
  realityXver: string;
  realityMaxTimeDiff: string;
  realityMinClientVer: string;
  realityMaxClientVer: string;
  realityShow: boolean;
};
export type ProfileOptionIssue = { field: string; code: string };
export const maxRealityTimeDiff = 9_223_372_036_854;

export function profileOptionsDraft(options?: InboundProfileOptions | null): ProfileOptionsDraft {
  return {
    ...trafficOptionsDraft(options),
    ...networkOptionsDraft(options),
    grpcAuthority: options?.grpc_authority ?? "", grpcMultiMode: options?.grpc_multi_mode ?? false,
    realityXver: String(options?.reality_xver ?? 0), realityMaxTimeDiff: String(options?.reality_max_time_diff ?? 0),
    realityMinClientVer: options?.reality_min_client_ver ?? "", realityMaxClientVer: options?.reality_max_client_ver ?? "",
    realityShow: options?.reality_show ?? false,
  };
}

/** Stable, strict comparison for exact preview/receipt and revision validation. */
export function canonicalProfileOptions(options?: InboundProfileOptions | null): InboundProfileOptions | undefined {
  if (options == null) return undefined;
  if (typeof options !== "object" || Array.isArray(options)) throw new Error("invalid profile options");
  const fields = { grpc_authority: "string", grpc_multi_mode: "boolean", reality_xver: "number", reality_max_time_diff: "number",
    reality_min_client_ver: "string", reality_max_client_ver: "string", reality_show: "boolean" } as const;
  if (Object.keys(options).some(key => !Object.prototype.hasOwnProperty.call(fields, key) && !["sniffing", "fallbacks", "xhttp", "client", "websocket", "http_upgrade"].includes(key))) throw new Error("unknown profile option");
  const result: Record<string, string | number | boolean | InboundSniffingOptions | InboundFallback[] | InboundXHTTPOptions | InboundClientOptions> = {};
  for (const key of Object.keys(fields) as Array<keyof typeof fields>) {
    const value = options[key];
    if (value === undefined) continue;
    if (typeof value !== fields[key] || typeof value === "number" && !Number.isSafeInteger(value)) throw new Error("invalid profile option");
    if (value !== "" && value !== false && value !== 0) result[key] = value;
  }
  const sniffing = canonicalSniffingOptions(options.sniffing), fallbacks = canonicalFallbacks(options.fallbacks);
  const xhttp = canonicalXHTTPOptions(options.xhttp), client = canonicalClientOptions(options.client);
  const websocket = canonicalHTTPTransportOptions(options.websocket), httpUpgrade = canonicalHTTPTransportOptions(options.http_upgrade);
  if (xhttp) result.xhttp = xhttp;
  if (client) result.client = client;
  if (websocket) result.websocket = websocket;
  if (httpUpgrade) result.http_upgrade = httpUpgrade;
  if (sniffing) result.sniffing = sniffing;
  if (fallbacks) result.fallbacks = fallbacks;
  return Object.keys(result).length ? result : undefined;
}

export function profileOptionsPayload(options: ProfileOptionsDraft | undefined, transport: string, security = "reality"): InboundProfileOptions | undefined {
  const value = options ?? profileOptionsDraft();
  const traffic = trafficOptionsPayload(security === "tls" ? { ...value, fallbacks: [], fallbacksPresent: false } : value);
  const network = networkOptionsPayload(value, transport, security);
  const result = canonicalProfileOptions({
    grpc_authority: transport === "grpc" ? value.grpcAuthority : "",
    grpc_multi_mode: transport === "grpc" && value.grpcMultiMode,
    ...(security === "tls" ? {} : { reality_xver: Number(value.realityXver), reality_max_time_diff: Number(value.realityMaxTimeDiff),
      reality_min_client_ver: value.realityMinClientVer, reality_max_client_ver: value.realityMaxClientVer, reality_show: value.realityShow }),
    ...traffic,
    ...network,
  }) ?? {};
  // Empty features carry clear intent on edits; comparison still treats them as defaults.
  if (traffic.sniffing !== undefined && !result.sniffing) result.sniffing = {};
  if (traffic.fallbacks !== undefined && !result.fallbacks) result.fallbacks = [];
  if (network.xhttp !== undefined && !result.xhttp) result.xhttp = {};
  if (network.client !== undefined && !result.client) result.client = {};
  if (network.websocket !== undefined && !result.websocket) result.websocket = {};
  if (network.http_upgrade !== undefined && !result.http_upgrade) result.http_upgrade = {};
  return Object.keys(result).length ? result : undefined;
}

function version(value: string): number[] | null {
  if (!/^(?:0|[1-9][0-9]{0,2})\.(?:0|[1-9][0-9]{0,2})\.(?:0|[1-9][0-9]{0,2})$/.test(value)) return null;
  const parts = value.split(".").map(Number);
  return parts.every(part => part <= 255) ? parts : null;
}

export function profileOptionIssues(options: ProfileOptionsDraft | undefined, transport: string, security = "reality"): ProfileOptionIssue[] {
  const value = options ?? profileOptionsDraft(), issues: ProfileOptionIssue[] = [];
  if (transport === "grpc" && !validAuthority(value.grpcAuthority)) issues.push({ field: "grpcAuthority", code: "grpc_authority_invalid" });
  if (security !== "tls") {
  if (!/^[012]$/.test(value.realityXver)) issues.push({ field: "realityXver", code: "reality_xver_invalid" });
  if (!/^[0-9]+$/.test(value.realityMaxTimeDiff) || !Number.isSafeInteger(Number(value.realityMaxTimeDiff)) || Number(value.realityMaxTimeDiff) > maxRealityTimeDiff) issues.push({ field: "realityMaxTimeDiff", code: "reality_max_time_diff_invalid" });
  const min = version(value.realityMinClientVer), max = version(value.realityMaxClientVer);
  if (value.realityMinClientVer && !min) issues.push({ field: "realityMinClientVer", code: "reality_version_invalid" });
  if (value.realityMaxClientVer && !max) issues.push({ field: "realityMaxClientVer", code: "reality_version_invalid" });
  if (min && max && (min[0] * 65536 + min[1] * 256 + min[2]) > (max[0] * 65536 + max[1] * 256 + max[2])) issues.push({ field: "realityMaxClientVer", code: "reality_version_order" });
  }
  issues.push(...trafficOptionIssues(value, transport).filter(issue => security !== "tls" || !issue.field.startsWith("fallbacks")));
  issues.push(...networkOptionIssues(value, transport, security));
  return issues;
}

export function profileOptionSection(field: string): "network" | "security" | "advanced" | undefined {
  if (field === "grpcAuthority" || field === "grpcMultiMode" || field.startsWith("xhttp") || field.startsWith("websocket") || field.startsWith("httpUpgrade")) return "network";
  if (["realityXver", "realityMaxTimeDiff", "realityMinClientVer", "realityMaxClientVer", "realityShow", "clientFingerprint", "clientSpiderX", "clientOptions"].includes(field)) return "security";
  if (field.startsWith("sniffing") || field.startsWith("fallbacks")) return "advanced";
  return undefined;
}
