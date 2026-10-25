import { assertPublicSpec } from "./inboundAuthoringSpec";
export type InboundSecurity = "reality" | "tls" | "none";
export type TLSMetadata = {
  sha256: string; public_key_sha256: string; dns_names: string[];
  not_before_unix: number; not_after_unix: number; key_type: string;
};
export type TLSCredentials = { certificate_pem: string; private_key_pem: string };
export const maxTLSCertificateBytes = 64 * 1024;
export const maxTLSPrivateKeyBytes = 16 * 1024;
function certificateDNSName(value: unknown): value is string {
  if (typeof value !== "string" || !value || value.length > 253) return false;
  const hostname = value.startsWith("*.") ? value.slice(2) : value, labels = hostname.split(".");
  return labels.length >= 2 && labels.every(label => /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(label));
}

export function profileSecurity(value?: string): InboundSecurity {
  if (value === undefined || value === "" || value === "reality") return "reality";
  if (value === "tls") return "tls";
  if (value === "none") return "none";
  throw new Error("invalid_profile_security");
}

/** Reject write-only material before accepting any public response into UI state. */
export function assertPublicInboundData(value: unknown): void {
  if (typeof value === "string") {
    if (/-----BEGIN\s+[^-]+-----/i.test(value)) throw new Error("unexpected_private_material");
    return;
  }
  if (!value || typeof value !== "object") return;
  if (Array.isArray(value)) { value.forEach(assertPublicInboundData); return; }
  for (const [key, item] of Object.entries(value)) {
    if (key === "spec") { assertPublicSpec(item); continue; }
    const canonical = key.replace(/[_-]/g, "").toLowerCase();
    if (["tlscredentials", "certificatepem", "privatekeypem", "privatekey", "realityprivatekey", "tlsprivatekey"].includes(canonical)) throw new Error("unexpected_private_material");
    assertPublicInboundData(item);
  }
}

export function canonicalTLSMetadata(value: unknown): TLSMetadata {
  assertPublicInboundData(value);
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("invalid_tls_metadata");
  const item = value as TLSMetadata;
  const fields = ["sha256", "public_key_sha256", "dns_names", "not_before_unix", "not_after_unix", "key_type"];
  if (Object.keys(item).some(field => !fields.includes(field)) || !/^[a-f0-9]{64}$/.test(item.sha256) || !/^[a-f0-9]{64}$/.test(item.public_key_sha256) ||
      !Array.isArray(item.dns_names) || !item.dns_names.length || item.dns_names.length > 1024 || item.dns_names.some(name => !certificateDNSName(name)) ||
      !Number.isSafeInteger(item.not_before_unix) || !Number.isSafeInteger(item.not_after_unix) || Math.abs(item.not_before_unix) > 8_640_000_000_000 || Math.abs(item.not_after_unix) > 8_640_000_000_000 || item.not_after_unix <= item.not_before_unix ||
      !["RSA", "ECDSA", "Ed25519"].includes(item.key_type)) throw new Error("invalid_tls_metadata");
  return { sha256: item.sha256, public_key_sha256: item.public_key_sha256, dns_names: [...item.dns_names],
    not_before_unix: item.not_before_unix, not_after_unix: item.not_after_unix, key_type: item.key_type };
}
export function sameTLSMetadata(left: unknown, right: unknown): boolean {
  try { return JSON.stringify(canonicalTLSMetadata(left)) === JSON.stringify(canonicalTLSMetadata(right)); } catch { return false; }
}

export function tlsCredentialIssues(credentials?: TLSCredentials): Array<{ field: string; code: string }> {
  const issues: Array<{ field: string; code: string }> = [];
  const certificate = credentials?.certificate_pem ?? "", key = credentials?.private_key_pem ?? "";
  if (!certificate.trim()) issues.push({ field: "tlsCertificate", code: "tls_certificate_required" });
  else if (new TextEncoder().encode(certificate).length > maxTLSCertificateBytes || !/^\s*-----BEGIN CERTIFICATE-----[\s\S]*-----END CERTIFICATE-----\s*$/.test(certificate)) issues.push({ field: "tlsCertificate", code: "tls_certificate_invalid" });
  if (!key.trim()) issues.push({ field: "tlsPrivateKey", code: "tls_private_key_required" });
  else if (new TextEncoder().encode(key).length > maxTLSPrivateKeyBytes || !/^\s*-----BEGIN (?:RSA |EC )?PRIVATE KEY-----[\s\S]*-----END (?:RSA |EC )?PRIVATE KEY-----\s*$/.test(key)) issues.push({ field: "tlsPrivateKey", code: "tls_private_key_invalid" });
  return issues;
}

/** Hash only the public leaf certificate; private keys are never parsed or displayed. */
export async function tlsLeafSHA256(certificatePEM: string): Promise<string> {
  if (new TextEncoder().encode(certificatePEM).length > maxTLSCertificateBytes) throw new Error("invalid_tls_certificate");
  const block = certificatePEM.match(/^\s*-----BEGIN CERTIFICATE-----([A-Za-z0-9+/=\r\n\t ]+)-----END CERTIFICATE-----/);
  if (!block) throw new Error("invalid_tls_certificate");
  const base64 = block[1].replace(/\s/g, "");
  const bytes = Uint8Array.from(atob(base64), value => value.charCodeAt(0));
  if (!bytes.length || bytes[0] !== 0x30 || btoa(String.fromCharCode(...bytes)) !== base64) throw new Error("invalid_tls_certificate");
  return Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)), byte => byte.toString(16).padStart(2, "0")).join("");
}
