import { validClientFingerprint, validClientPath, validUTF8 } from "./inboundNetworkOptions";

export type InboundEndpoint = { address: string; port: number; label?: string; enabled: boolean; sni?: string; fingerprint?: string; spider_x?: string };
export type EndpointDraft = { key: string; address: string; port: string; label: string; enabled: boolean; sni: string; fingerprint: string; spiderX: string };
export type EndpointProfileDefaults = { serverNames: string[]; fingerprint: string; spiderX: string; security?: string };

function ipv4(value: string): string | undefined {
  if (!/^(?:0|[1-9][0-9]{0,2})(?:\.(?:0|[1-9][0-9]{0,2})){3}$/.test(value)) return undefined;
  return value.split(".").every(part => Number(part) <= 255) ? value : undefined;
}
export function canonicalEndpointAddress(value: string): string {
  const host = value.trim().toLowerCase();
  if (host.includes(":")) {
    try {
      const normalized = new URL("http://[" + host + "]/").hostname.slice(1, -1);
      const mapped = normalized.match(/^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/);
      if (!mapped) return normalized;
      const high = Number.parseInt(mapped[1], 16), low = Number.parseInt(mapped[2], 16);
      return [high >>> 8, high & 255, low >>> 8, low & 255].join(".");
    } catch { return host; }
  }
  return host;
}
export function validEndpointAddress(value: string): boolean {
  const host = value.trim().toLowerCase();
  if (!host || host.length > 253 || /[/@%?#\\\s]/.test(host)) return false;
  const canonical = canonicalEndpointAddress(host);
  if (host.includes(":")) {
    try { new URL("http://[" + host + "]/"); } catch { return false; }
    if (canonical.includes(":")) {
      const first = Number.parseInt(canonical.split(":")[0] || "0", 16);
      return canonical !== "::" && (first & 0xff00) !== 0xff00 && (first & 0xffc0) !== 0xfe80;
    }
  }
  if (ipv4(canonical)) {
    const [a, b] = canonical.split(".").map(Number);
    return canonical !== "0.0.0.0" && !(a >= 224 && a <= 239) && !(a === 169 && b === 254);
  }
  const labels = host.split(".");
  return labels.length >= 2 && labels[labels.length - 1].length >= 2 && labels.every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label));
}
export function endpointDraft(value?: InboundEndpoint): EndpointDraft {
  return { key: crypto.randomUUID(), address: value?.address ?? "", port: value?.port ? String(value.port) : "", label: value?.label ?? "",
    enabled: value?.enabled ?? true, sni: value?.sni ?? "", fingerprint: value?.fingerprint ?? "", spiderX: value?.spider_x ?? "" };
}
export function endpointPort(value: string): number {
  return value === "" || value === "0" ? 0 : /^[0-9]+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) >= 1 && Number(value) <= 65535 ? Number(value) : -1;
}
export function endpointPayload(value: EndpointDraft, security = "reality"): InboundEndpoint {
  return { address: canonicalEndpointAddress(value.address), port: endpointPort(value.port), enabled: value.enabled,
    ...(value.label.trim() ? { label: value.label.trim() } : {}), ...(value.sni ? { sni: value.sni } : {}),
    ...(value.fingerprint ? { fingerprint: value.fingerprint } : {}), ...(security !== "tls" && value.spiderX ? { spider_x: value.spiderX } : {}) };
}
/** Strict equality used for server receipts; missing and empty arrays are equivalent. */
export function canonicalEndpoints(values?: InboundEndpoint[] | null): InboundEndpoint[] | undefined {
  if (values === undefined || values === null) return undefined;
  if (!Array.isArray(values)) throw new Error("invalid endpoints");
  if (!values.length) return undefined;
  return values.map(value => {
    if (!value || typeof value !== "object" || Array.isArray(value) || Object.keys(value).some(key => !["address", "port", "enabled", "label", "sni", "fingerprint", "spider_x"].includes(key))) throw new Error("unknown endpoint field");
    if (typeof value.address !== "string" || typeof value.port !== "number" || !Number.isSafeInteger(value.port) || typeof value.enabled !== "boolean") throw new Error("invalid endpoint");
    for (const key of ["label", "sni", "fingerprint", "spider_x"] as const) if (value[key] !== undefined && typeof value[key] !== "string") throw new Error("invalid endpoint");
    return { address: canonicalEndpointAddress(value.address), port: value.port, enabled: value.enabled,
      ...(value.label?.trim() ? { label: value.label.trim() } : {}), ...(value.sni ? { sni: value.sni } : {}),
      ...(value.fingerprint ? { fingerprint: value.fingerprint } : {}), ...(value.spider_x ? { spider_x: value.spider_x } : {}) };
  });
}
export function endpointAddresses(values: EndpointDraft[]): string[] {
  return [...new Set(values.filter(value => value.enabled && value.address.trim()).map(value => canonicalEndpointAddress(value.address)))];
}
export function effectiveEndpoint(value: EndpointDraft, listenerPort: string, defaults: EndpointProfileDefaults) {
  return { address: canonicalEndpointAddress(value.address), port: endpointPort(value.port) || Number(listenerPort),
    sni: value.sni || defaults.serverNames[0] || "", fingerprint: value.fingerprint || defaults.fingerprint || "chrome", spiderX: defaults.security === "tls" ? "" : value.spiderX || defaults.spiderX || "/" };
}
export function endpointIssues(values: EndpointDraft[], listenerPort: string, defaults: EndpointProfileDefaults, prefix: string): Array<{ field: string; code: string }> {
  const issues: Array<{ field: string; code: string }> = [], seen = new Set<string>();
  if (values.length > 64) issues.push({ field: prefix, code: "endpoint_limit" });
  values.forEach((value, index) => {
    const add = (field: string, code: string) => issues.push({ field: `${prefix}.${index}.${field}`, code });
    if (!validEndpointAddress(value.address)) add("address", "host_invalid");
    if (endpointPort(value.port) < 0) add("port", "endpoint_port_invalid");
    if (!validUTF8(value.label) || Array.from(value.label.trim()).length > 128 || /[\u0000-\u001f\u007f-\u009f]/.test(value.label)) add("label", "endpoint_label_invalid");
    if (value.sni && !defaults.serverNames.includes(value.sni)) add("sni", "endpoint_sni_invalid");
    if (!validClientFingerprint(value.fingerprint)) add("fingerprint", "client_fingerprint_invalid");
    if (defaults.security !== "tls" && !validClientPath(value.spiderX, true)) add("spiderX", "client_spider_x_invalid");
    const effective = effectiveEndpoint(value, listenerPort, defaults);
    const identity = JSON.stringify(effective);
    if (seen.has(identity)) add("address", "endpoint_duplicate");
    seen.add(identity);
  });
  return issues;
}
