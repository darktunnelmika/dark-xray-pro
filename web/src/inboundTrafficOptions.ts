export type InboundSniffingOptions = {
  enabled?: boolean;
  dest_override?: string[];
  metadata_only?: boolean;
  route_only?: boolean;
  domains_excluded?: string[];
  ips_excluded?: string[];
};
export type InboundFallback = { path?: string; alpn?: string; dest: string; xver?: number };

export type SniffingDraft = {
  enabled: boolean;
  destOverride: string[];
  metadataOnly: boolean;
  routeOnly: boolean;
  domainsExcluded: string;
  ipsExcluded: string;
};
export type FallbackDraft = { key: string; path: string; alpn: string; dest: string; xver: string };
export type TrafficOptionsDraft = {
  sniffing: SniffingDraft;
  fallbacks: FallbackDraft[];
  /** Keep per-feature clear intent separate from normalized saved values. */
  sniffingPresent: boolean;
  fallbacksPresent: boolean;
};
export type TrafficOptionIssue = { field: string; code: string };
export const sniffingProtocols = ["http", "tls", "quic", "fakedns"] as const;
export const maxFallbacks = 32;

function plainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function exactKeys(value: Record<string, unknown>, keys: string[]): void {
  if (Object.keys(value).some(key => !keys.includes(key))) throw new Error("unknown traffic option");
}
function strings(value: unknown): string[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.some(item => typeof item !== "string")) throw new Error("invalid traffic option array");
  return value.length ? [...value] : undefined;
}

export function canonicalSniffingOptions(value?: InboundSniffingOptions | null): InboundSniffingOptions | undefined {
  if (value == null) return undefined;
  if (!plainObject(value)) throw new Error("invalid sniffing options");
  exactKeys(value, ["enabled", "dest_override", "metadata_only", "route_only", "domains_excluded", "ips_excluded"]);
  const result: InboundSniffingOptions = {};
  for (const key of ["enabled", "metadata_only", "route_only"] as const) {
    if (value[key] !== undefined && typeof value[key] !== "boolean") throw new Error("invalid sniffing flag");
    if (value[key]) result[key] = true;
  }
  for (const key of ["dest_override", "domains_excluded", "ips_excluded"] as const) {
    const list = strings(value[key]); if (list) result[key] = list;
  }
  return Object.keys(result).length ? result : undefined;
}

export function canonicalFallbacks(value?: InboundFallback[] | null): InboundFallback[] | undefined {
  if (value == null) return undefined;
  if (!Array.isArray(value)) throw new Error("invalid fallbacks");
  return value.length ? value.map(row => {
    if (!plainObject(row)) throw new Error("invalid fallback");
    exactKeys(row, ["path", "alpn", "dest", "xver"]);
    if (typeof row.dest !== "string" || row.path !== undefined && typeof row.path !== "string" || row.alpn !== undefined && typeof row.alpn !== "string" ||
      row.xver !== undefined && (typeof row.xver !== "number" || !Number.isSafeInteger(row.xver))) throw new Error("invalid fallback field");
    const result: InboundFallback = { dest: row.dest };
    if (row.path) result.path = row.path;
    if (row.alpn) result.alpn = row.alpn;
    if (row.xver) result.xver = row.xver;
    return result;
  }) : undefined;
}

export function trafficOptionsDraft(value?: { sniffing?: InboundSniffingOptions; fallbacks?: InboundFallback[] } | null): TrafficOptionsDraft {
  const sniffing = value?.sniffing;
  return {
    sniffingPresent: sniffing !== undefined,
    fallbacksPresent: value?.fallbacks !== undefined && value.fallbacks.length > 0,
    sniffing: {
      enabled: sniffing?.enabled ?? false,
      destOverride: [...(sniffing ? sniffing.dest_override ?? [] : ["http", "tls"])],
      metadataOnly: sniffing?.metadata_only ?? false,
      routeOnly: sniffing ? sniffing.route_only ?? false : true,
      domainsExcluded: sniffing?.domains_excluded?.join("\n") ?? "",
      ipsExcluded: sniffing?.ips_excluded?.join("\n") ?? "",
    },
    // Stable keys keep separate source-copy initialization calls equivalent.
    fallbacks: value?.fallbacks?.map((row, index) => ({ key: "saved-" + index,
      path: row.path ?? "", alpn: row.alpn ?? "", dest: row.dest, xver: String(row.xver ?? 0) })) ?? [],
  };
}

export function trafficLines(value: string): string[] {
  return value.split(/[\r\n]+/).filter(Boolean);
}

export function trafficOptionsPayload(value: TrafficOptionsDraft): { sniffing?: InboundSniffingOptions; fallbacks?: InboundFallback[] } {
  const sniffing = canonicalSniffingOptions({
    enabled: value.sniffing.enabled, dest_override: value.sniffing.destOverride,
    metadata_only: value.sniffing.metadataOnly, route_only: value.sniffing.routeOnly,
    domains_excluded: trafficLines(value.sniffing.domainsExcluded), ips_excluded: trafficLines(value.sniffing.ipsExcluded),
  });
  return {
    sniffing: value.sniffingPresent || value.sniffing.enabled ? sniffing ?? {} : undefined,
    fallbacks: value.fallbacks.length || value.fallbacksPresent ? value.fallbacks.map(row => ({ path: row.path, alpn: row.alpn, dest: row.dest, xver: Number(row.xver) })) : undefined,
  };
}

const controlCharacters = /[\u0000-\u001f\u007f-\u009f]/u;
function bytes(value: string): number { return new TextEncoder().encode(value).length; }
function validIP(value: string): boolean {
  if (!value || /[%\s[\]/]/u.test(value)) return false;
  if (value.includes(":")) {
    try { return new URL("http://[" + value + "]/").hostname.startsWith("["); } catch { return false; }
  }
  const parts = value.split(".");
  return parts.length === 4 && parts.every(part => /^(?:0|[1-9][0-9]{0,2})$/.test(part) && Number(part) <= 255);
}
function validExcludedIP(value: string): boolean {
  const parts = value.split("/");
  if (parts.length > 2 || !validIP(parts[0])) return false;
  const ipv6 = parts[0].includes(":"), mapped = ipv6 && /^\[::ffff:[0-9a-f]{1,4}:[0-9a-f]{1,4}\]$/i.test(new URL("http://[" + parts[0] + "]/").hostname);
  return parts.length === 1 || /^[0-9]{1,3}$/.test(parts[1]) && Number(parts[1]) <= (ipv6 && !mapped ? 128 : 32);
}
function validDestination(value: string): boolean {
  const validPort = (port: string) => /^[1-9][0-9]{0,4}$/.test(port) && Number(port) <= 65535;
  if (/^[0-9]+$/.test(value)) return validPort(value);
  if (value.length > 259 || /[^\x21-\x7e]|[/\\@%?#]/.test(value)) return false;
  if (value.startsWith("[")) {
    const match = value.match(/^\[([0-9a-fA-F:.]+)\]:([0-9]+)$/);
    return !!match && match[1].includes(":") && validIP(match[1]) && validPort(match[2]);
  }
  const parts = value.split(":");
  return parts.length === 2 && validPort(parts[1]) && parts[0].length > 0 && parts[0].length <= 253 &&
    parts[0].split(".").every(label => /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(label));
}

export function trafficOptionIssues(value: TrafficOptionsDraft, transport: string): TrafficOptionIssue[] {
  const issues: TrafficOptionIssue[] = [];
  if (value.sniffingPresent || value.sniffing.enabled) {
    const overrides = value.sniffing.destOverride;
    if (new Set(overrides).size !== overrides.length || overrides.some(item => !(sniffingProtocols as readonly string[]).includes(item))) issues.push({ field: "sniffing.destOverride", code: "sniffing_protocol_invalid" });
    for (const [field, ip] of [["domainsExcluded", false], ["ipsExcluded", true]] as const) {
      const rules = trafficLines(value.sniffing[field]);
      if (rules.length > 256 || rules.some(rule => rule !== rule.trim() || bytes(rule) > 512 || controlCharacters.test(rule) || ip && !validExcludedIP(rule))) issues.push({ field: "sniffing." + field, code: ip ? "sniffing_ip_invalid" : "sniffing_domain_invalid" });
      if (!ip && rules.some(rule => {
        if (!rule.includes(":")) return false;
        if (!/^(?:full|domain|keyword|regexp):.+$/u.test(rule)) return true;
        if (!/^(?:full|domain):/u.test(rule)) return false; // RE2 syntax is checked by the server.
        const host = rule.slice(rule.indexOf(":") + 1);
        return !validIP(host) && !validDestination(host + ":443");
      })) issues.push({ field: "sniffing." + field, code: "sniffing_domain_invalid" });
    }
  }
  if (value.fallbacks.length > maxFallbacks) issues.push({ field: "fallbacks", code: "fallback_limit" });
  if (value.fallbacks.length && transport !== "tcp") issues.push({ field: "fallbacks.0.dest", code: "fallback_transport" });
  const matches = new Set<string>();
  value.fallbacks.forEach((row, index) => {
    const issue = (field: string, code: string) => issues.push({ field: `fallbacks.${index}.${field}`, code });
    if (row.path && (!row.path.startsWith("/") || bytes(row.path) > 55 || /[\s?#]/u.test(row.path) || controlCharacters.test(row.path))) issue("path", "fallback_path_invalid");
    if (row.alpn) issue("alpn", "fallback_alpn_unsupported");
    if (!validDestination(row.dest)) issue("dest", "fallback_dest_invalid");
    if (!/^[012]$/.test(row.xver)) issue("xver", "fallback_xver_invalid");
    const match = row.path;
    if (matches.has(match)) issue("path", "fallback_duplicate");
    matches.add(match);
  });
  return issues;
}
