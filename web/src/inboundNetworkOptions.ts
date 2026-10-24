export type InboundXHTTPOptions = { path?: string; host?: string; mode?: string; x_padding_bytes?: string };
export type InboundClientOptions = { fingerprint?: string; spider_x?: string };
export type NetworkOptionsDraft = {
  xhttpPath: string; xhttpHost: string; xhttpMode: string; xhttpPadding: string; xhttpPresent: boolean;
  clientFingerprint: string; clientSpiderX: string; clientPresent: boolean;
};
export const clientFingerprints = ["chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"] as const;
export const xhttpModes = ["auto", "packet-up", "stream-up", "stream-one"] as const;

function canonicalStrings(value: unknown, fields: string[]): Record<string, string> | undefined {
  if (value === undefined || value === null) return undefined;
  if (typeof value !== "object" || Array.isArray(value)) throw new Error("invalid network options");
  const result: Record<string, string> = {}, source = value as Record<string, unknown>;
  if (Object.keys(source).some(key => !fields.includes(key))) throw new Error("unknown network option");
  for (const field of fields) {
    const item = source[field];
    if (item === undefined) continue;
    if (typeof item !== "string") throw new Error("invalid network option");
    if (item !== "") result[field] = item;
  }
  return Object.keys(result).length ? result : undefined;
}
export function canonicalXHTTPOptions(value?: InboundXHTTPOptions | null): InboundXHTTPOptions | undefined {
  return canonicalStrings(value, ["path", "host", "mode", "x_padding_bytes"]);
}
export function canonicalClientOptions(value?: InboundClientOptions | null): InboundClientOptions | undefined {
  return canonicalStrings(value, ["fingerprint", "spider_x"]);
}
export function networkOptionsDraft(options?: { xhttp?: InboundXHTTPOptions; client?: InboundClientOptions } | null): NetworkOptionsDraft {
  return { xhttpPath: options?.xhttp?.path ?? "", xhttpHost: options?.xhttp?.host ?? "", xhttpMode: options?.xhttp?.mode ?? "",
    xhttpPadding: options?.xhttp?.x_padding_bytes ?? "", xhttpPresent: !!canonicalXHTTPOptions(options?.xhttp),
    clientFingerprint: options?.client?.fingerprint ?? "", clientSpiderX: options?.client?.spider_x ?? "", clientPresent: !!canonicalClientOptions(options?.client) };
}
export function networkOptionsPayload(value: NetworkOptionsDraft, transport: string): { xhttp?: InboundXHTTPOptions; client?: InboundClientOptions } {
  const xhttp = transport === "xhttp" ? canonicalXHTTPOptions({ path: value.xhttpPath, host: value.xhttpHost, mode: value.xhttpMode, x_padding_bytes: value.xhttpPadding }) : undefined;
  const client = canonicalClientOptions({ fingerprint: value.clientFingerprint, spider_x: value.clientSpiderX });
  return { ...(xhttp || value.xhttpPresent ? { xhttp: xhttp ?? {} } : {}), ...(client || value.clientPresent ? { client: client ?? {} } : {}) };
}

export function validAuthority(value: string): boolean {
  if (!value) return true;
  if (value.length > 259 || /[^\x21-\x7e]|[/\\@%?#]/.test(value)) return false;
  let host = value, port = "";
  if (value.startsWith("[")) {
    const match = value.match(/^(\[[0-9a-fA-F:.]+\])(?::([0-9]+))?$/);
    if (!match) return false;
    host = match[1]; port = match[2] ?? "";
    try { if (!new URL("http://" + host).hostname.startsWith("[")) return false; } catch { return false; }
  } else {
    const parts = value.split(":");
    if (parts.length > 2 || parts.length === 2 && !parts[1]) return false;
    [host, port = ""] = parts;
    if (!host || host.length > 253 || !host.split(".").every(label => /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(label))) return false;
  }
  return !port || /^[1-9][0-9]{0,4}$/.test(port) && Number(port) <= 65535;
}
export function validUTF8(value: string): boolean {
  for (let index = 0; index < value.length; index++) {
    const code = value.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = value.charCodeAt(++index);
      if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (code >= 0xdc00 && code <= 0xdfff) return false;
  }
  return true;
}
export function validClientPath(value: string, query: boolean): boolean {
  if (!value) return true;
  if (!value.startsWith("/") || value.startsWith("//") || !validUTF8(value) || new TextEncoder().encode(value).length > 2048 || /[\\#\p{White_Space}\u0000-\u001f\u007f-\u009f]/u.test(value) || /%(?![0-9a-fA-F]{2})/.test(value)) return false;
  if (!query) return !value.includes("?");
  const search = value.split("?").slice(1).join("?");
  return !search.includes(";") && !Array.from(new URLSearchParams(search).keys()).some(key => ["p", "c", "t", "i", "r"].includes(key));
}
export function validClientFingerprint(value: string): boolean { return !value || (clientFingerprints as readonly string[]).includes(value); }
export function validXHTTPHost(value: string): boolean { return value.length <= 253 && !/[:\[\]]/.test(value) && validAuthority(value); }
export function validXHTTPPadding(value: string): boolean {
  if (!value) return true;
  if (!/^[1-9][0-9]{0,4}(?:-[1-9][0-9]{0,4})?$/.test(value)) return false;
  const [min, max = min] = value.split("-").map(Number);
  return min >= 1 && max <= 65536 && min <= max;
}
export function networkOptionIssues(value: NetworkOptionsDraft, transport: string): Array<{ field: string; code: string }> {
  const issues: Array<{ field: string; code: string }> = [];
  if (transport === "xhttp") {
    if (!validClientPath(value.xhttpPath, false)) issues.push({ field: "xhttpPath", code: "xhttp_path_invalid" });
    if (!validXHTTPHost(value.xhttpHost)) issues.push({ field: "xhttpHost", code: "xhttp_host_invalid" });
    if (value.xhttpMode && !(xhttpModes as readonly string[]).includes(value.xhttpMode)) issues.push({ field: "xhttpMode", code: "xhttp_mode_invalid" });
    if (!validXHTTPPadding(value.xhttpPadding)) issues.push({ field: "xhttpPadding", code: "xhttp_padding_invalid" });
  }
  if (!validClientFingerprint(value.clientFingerprint)) issues.push({ field: "clientFingerprint", code: "client_fingerprint_invalid" });
  if (!validClientPath(value.clientSpiderX, true)) issues.push({ field: "clientSpiderX", code: "client_spider_x_invalid" });
  return issues;
}
