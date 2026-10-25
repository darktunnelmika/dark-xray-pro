import { useEffect, useRef, useState } from "react";
import { Check, KeyRound, LoaderCircle, Search, ShieldCheck } from "lucide-react";
import type { InboundNode } from "./inboundTypes";
import { readSpecPath, writeSpecPath, type AuthoringSpec, type SpecObject } from "./inboundAuthoringSpec";

type Capabilities = { enabled: boolean; keys: { x25519: boolean; mldsa65: boolean; ech: boolean }; certificates: { inspect: boolean; generate: boolean; remote: boolean }; reality: { scan: boolean; find: boolean; max_targets: number; max_cidr_addresses: number }; node_probe_available: boolean; unavailable_reasons?: Record<string, string>; default_targets?: Array<{ target: string; server_name: string }> };
type Certificate = { subject: string; issuer: string; not_before: string; not_after: string; dns_names: string[]; ip_addresses: string[]; sha256_hex: string; sha256_base64: string; spki_sha256_hex: string; spki_sha256_base64: string };
type Inspection = { certificates: Certificate[]; chain_valid: boolean; hostname_valid: boolean; time_valid: boolean; valid: boolean; server_name: string; errors: string[] };
type ScanResult = { scope: string; node_id: string; target: string; host: string; ip: string; port: number; server_name: string; server_name_source?: string; ok: boolean; suitable: boolean; error?: string; reason?: string; tls13: boolean; tls_version: string; h2: boolean; alpn: string; x25519: boolean; hybrid_key_exchange: string; latency_ms: number; certificate?: Inspection };
type Props = { mode: "security" | "protocol-key"; language: "fa" | "en"; spec: AuthoringSpec; nodes: InboundNode[]; csrf: string; disabled: boolean; onChange: (value: AuthoringSpec) => void; onUnauthorized: () => void };
const endpoint = "/api/v1/workspace/inbound-security";
function base64Standard(value: string) { const raw = value.replace(/-/g, "+").replace(/_/g, "/"); return raw + "=".repeat((4 - raw.length % 4) % 4); }
function pemText(value: unknown): string { return Array.isArray(value) ? value.join("\n") : typeof value === "string" ? value : ""; }

export default function InboundSecurityTools({ mode, language, spec, nodes, csrf, disabled, onChange, onUnauthorized }: Props) {
  const fa = language === "fa", current = useRef(spec); current.current = spec;
  const lifetime = useRef<AbortController | null>(null);
  const actionController = useRef<AbortController | null>(null), inspectionOrigin = useRef("local");
  const [caps, setCaps] = useState<Capabilities | null>(null), [capsFailed, setCapsFailed] = useState(false);
  const [busy, setBusy] = useState(""), [message, setMessage] = useState("");
  const [scope, setScope] = useState("hub"), [searchMode, setSearchMode] = useState("single");
  const [targets, setTargets] = useState(""), [target, setTarget] = useState(""), [sni, setSni] = useState(""), [cidr, setCidr] = useState(""), [port, setPort] = useState("443");
  const [results, setResults] = useState<ScanResult[]>([]), [inspection, setInspection] = useState<Inspection | null>(null);
  const [certificateNames, setCertificateNames] = useState(""), [validDays, setValidDays] = useState("365"), [echName, setEchName] = useState("");
  useEffect(() => { setResults([]); if (inspectionOrigin.current === "remote") setInspection(null); }, [scope, searchMode, targets, target, sni, cidr, port]);
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    void fetch(endpoint + "/capabilities", { signal: controller.signal, credentials: "same-origin", cache: "no-store", headers: { Accept: "application/json" } }).then(async response => {
      if (response.status === 401) { onUnauthorized(); throw new Error("unauthorized"); }
      if (!response.ok) throw new Error("capabilities_unavailable");
      const value = await response.json() as Capabilities;
      if (!value || value.enabled !== true || !value.keys || !value.certificates || !value.reality) throw new Error("invalid_capabilities");
      if (!controller.signal.aborted) { setCaps(value); setCapsFailed(false); }
    }).catch(() => { if (!controller.signal.aborted) setCapsFailed(true); });
    return () => controller.abort();
  }, [onUnauthorized]);
  async function request<T>(path: string, body: unknown): Promise<T> {
    const response = await fetch(endpoint + path, { method: "POST", credentials: "same-origin", cache: "no-store", signal: actionController.current?.signal ?? lifetime.current?.signal, headers: { Accept: "application/json", "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(body) });
    if (response.status === 401) { onUnauthorized(); throw new Error("unauthorized"); }
    if (!response.ok) {
      const error = await response.json().catch(() => ({})) as { error?: string };
      throw new Error(typeof error.error === "string" && /^[a-z_]{3,100}$/.test(error.error) ? error.error : "security_tool_failed");
    }
    return response.json() as Promise<T>;
  }
  async function run(action: string, work: () => Promise<void>) {
    if (busy || disabled || !csrf || !caps?.enabled) return;
    const controller = new AbortController(), abort = () => controller.abort(); actionController.current = controller;
    lifetime.current?.signal.addEventListener("abort", abort, { once: true });
    setBusy(action); setMessage("");
    try { await work(); }
    catch (error) { if (!lifetime.current?.signal.aborted) setMessage(controller.signal.aborted ? fa ? "بررسی متوقف شد." : "The operation was cancelled." : (fa ? "ابزار کامل نشد؛ ورودی و دسترسی این سرور را بررسی کن." : "The tool did not complete. Check its input and availability on this server.") + (error instanceof Error ? ` (${error.message})` : "")); }
    finally { lifetime.current?.signal.removeEventListener("abort", abort); if (actionController.current === controller) actionController.current = null; if (!lifetime.current?.signal.aborted) setBusy(""); }
  }
  const locked = disabled || !!busy || !caps?.enabled;
  async function keys(kind: "x25519" | "mldsa65" | "ech") {
    await run(kind, async () => {
      const response = await request<{ kind: string; private_key?: string; public_key?: string; seed?: string; verify?: string; config?: string }>(`/keys/${kind}`, kind === "ech" ? { server_name: echName || String(current.current.tls.serverName ?? current.current.client.serverName ?? "") } : {});
      let next = current.current;
      if (kind === "x25519") {
        if (!response.private_key || !response.public_key) throw new Error("invalid_key_result");
        if (next.protocol === "wireguard") next = writeSpecPath(next, "settings.secretKey", base64Standard(response.private_key));
        else if (next.protocol === "amneziawg") { next = writeSpecPath(next, "settings.server.privateKey", base64Standard(response.private_key)); next = writeSpecPath(next, "settings.server.publicKey", base64Standard(response.public_key)); }
        else { next = writeSpecPath(next, "reality.privateKey", response.private_key); next = writeSpecPath(next, "client.publicKey", response.public_key); }
      } else if (kind === "mldsa65") {
        if (!response.seed || !response.verify) throw new Error("invalid_key_result");
        next = writeSpecPath(next, "reality.mldsa65Seed", response.seed); next = writeSpecPath(next, "client.mldsa65Verify", response.verify);
      } else {
        if (!response.private_key || !response.config) throw new Error("invalid_key_result");
        next = writeSpecPath(next, "tls.echServerKeys", response.private_key); next = writeSpecPath(next, "client.echConfigList", response.config);
      }
      if (!lifetime.current?.signal.aborted) { onChange(next); setMessage(fa ? "مقادیر تازه در فرم قرار گرفتند؛ با ذخیرهٔ طرح اعمال می‌شوند." : "Generated values are in the form. Save the setup to retain them."); }
    });
  }
  function requestScope() {
    if (scope !== "hub" && (!caps?.node_probe_available || !nodes.some(node => node.node_id === scope && node.kind !== "hub"))) throw new Error("node_probe_unavailable");
    return { scope: scope === "hub" ? "hub" : "node", node_id: scope === "hub" ? "" : scope };
  }
  async function scan(remoteCertificate = false) {
    await run(remoteCertificate ? "remote" : "scan", async () => {
      setResults([]); const origin = requestScope();
      if (searchMode === "single" || remoteCertificate) {
        const address = target || String(current.current.reality.target ?? "");
        if (!address) throw new Error("target_required");
        const result = await request<ScanResult>(remoteCertificate ? "/certificates/remote" : "/reality/scan", { ...origin, target: address, server_name: sni });
        if (!result || typeof result.ok !== "boolean" || typeof result.suitable !== "boolean") throw new Error("invalid_scan_result");
        if (!lifetime.current?.signal.aborted) { setResults([result]); if (remoteCertificate && result.certificate) { inspectionOrigin.current = "remote"; setInspection(result.certificate); } }
      } else {
        const candidates = targets.split(/\r?\n/).filter(row => row.trim()).map(row => { const [target, server_name = ""] = row.trim().split(/\s+/); return { target, server_name }; });
        const input = searchMode === "cidr" ? { cidr, port: Number(port), server_name: sni } : searchMode === "list" ? { targets: candidates } : {};
        if (searchMode === "list" && (!candidates.length || candidates.length > (caps?.reality.max_targets ?? 32))) throw new Error("target_count_invalid");
        if (searchMode === "cidr" && (!cidr || !/^\d+$/.test(port) || Number(port) < 1 || Number(port) > 65535)) throw new Error("cidr_or_port_invalid");
        const result = await request<{ results: ScanResult[]; completed: number; total: number; cancelled: boolean }>("/reality/find", { ...origin, ...input });
        if (!Array.isArray(result.results)) throw new Error("invalid_scan_result");
        if (!lifetime.current?.signal.aborted) { setResults(result.results); setMessage(`${result.completed}/${result.total} ${fa ? "هدف بررسی شد" : "targets checked"}${result.cancelled ? fa ? " · متوقف‌شده" : " · stopped" : ""}`); }
      }
    });
  }
  async function inspect() {
    await run("inspect", async () => {
      const pem = pemText(readSpecPath(current.current, "tls.certificates.0.certificate")) || pemText(readSpecPath(current.current, "settings.server.certificate"));
      if (!pem) throw new Error("certificate_pem_required");
      const result = await request<Inspection>("/certificates/inspect", { pem, server_name: String(current.current.tls.serverName ?? current.current.client.serverName ?? "") });
      if (!Array.isArray(result.certificates)) throw new Error("invalid_certificate_result");
      if (!lifetime.current?.signal.aborted) { inspectionOrigin.current = "local"; setInspection(result); }
    });
  }
  async function generateCertificate() {
    await run("certificate", async () => {
      const names = (certificateNames || String(current.current.tls.serverName ?? current.current.client.serverName ?? "")).split(/[\s,]+/).filter(Boolean), days = Number(validDays);
      if (!names.length || names.length > 16 || !Number.isInteger(days) || days < 1 || days > 3650) throw new Error("certificate_input_invalid");
      const result = await request<{ certificate_pem: string; private_key_pem: string; certificate: Inspection }>("/certificates/generate", { server_names: names, valid_days: days });
      if (!result.certificate_pem?.includes("BEGIN CERTIFICATE") || !result.private_key_pem?.includes("PRIVATE KEY")) throw new Error("invalid_certificate_result");
      let next = current.current;
      {
        const certificates = Array.isArray(next.tls.certificates) ? next.tls.certificates : [];
        const certificate: SpecObject = { certificate: result.certificate_pem.trim().split("\n"), key: result.private_key_pem.trim().split("\n"), usage: "encipherment" };
        next = writeSpecPath(next, "tls.certificates", [...certificates, certificate]);
      }
      if (!next.tls.serverName) next = writeSpecPath(next, "tls.serverName", names[0]);
      if (!lifetime.current?.signal.aborted) { onChange(next); inspectionOrigin.current = "local"; setInspection(result.certificate); setMessage(fa ? "گواهی خودامضا ساخته شد. اعتماد کلاینت را با Pin همین گواهی تنظیم کن." : "Self-signed certificate generated. Configure client trust using this certificate’s pin."); }
    });
  }
  const firstCert = inspection?.certificates?.[0];
  return <details className="spec-disclosure spec-tools-disclosure"><summary><KeyRound size={15}/>{mode === "protocol-key" ? fa ? "تولید جفت کلید" : "Generate key pair" : fa ? "ابزارهای امنیت و یافتن هدف" : "Security tools and target finder"}</summary><div className="spec-group-body spec-tools">
    {capsFailed ? <p className="creation-help" role="status">{fa ? "ابزارهای امنیت این سرور در دسترس نیستند." : "Security tools are unavailable on this server."}</p> : !caps && <p className="creation-help">{fa ? "دریافت قابلیت‌های ابزار…" : "Loading available tools…"}</p>}
    {(mode === "protocol-key" || spec.security === "reality") && <div className="spec-actions"><button type="button" disabled={locked || !caps?.keys.x25519} onClick={() => void keys("x25519")}><KeyRound size={14}/>X25519</button>{spec.security === "reality" && <button type="button" disabled={locked || !caps?.keys.mldsa65} onClick={() => void keys("mldsa65")}>ML-DSA-65</button>}</div>}
    {spec.security === "reality" && caps && !caps.keys.mldsa65 && <p className="creation-help">ML-DSA-65 · {fa ? "تولید روی این سرور در دسترس نیست" : "Generation unavailable on this server"}{caps.unavailable_reasons?.mldsa65 ? ` (${caps.unavailable_reasons.mldsa65})` : ""}</p>}
    {mode === "security" && spec.security === "tls" && <><div className="spec-fields"><label className="creation-field"><span>{fa ? "نام عمومی ECH" : "ECH public name"}</span><input aria-label="ECH public name" disabled={locked} dir="ltr" value={echName} onChange={event => setEchName(event.target.value)} placeholder="example.org"/></label><div className="spec-actions"><button type="button" disabled={locked || !caps?.keys.ech} onClick={() => void keys("ech")}>ECH</button></div></div>{caps && !caps.keys.ech && <p className="creation-help">ECH · {fa ? "تولید روی این سرور در دسترس نیست" : "Generation unavailable on this server"}{caps.unavailable_reasons?.ech ? ` (${caps.unavailable_reasons.ech})` : ""}</p>}
      <details className="spec-disclosure"><summary>{fa ? "گواهی و اعتماد کلاینت" : "Certificate and client trust"}</summary><div className="spec-group-body"><div className="spec-fields"><label className="creation-field"><span>{fa ? "نام‌های گواهی با کاما" : "Certificate names, comma-separated"}</span><input aria-label="Certificate names" disabled={locked} dir="ltr" value={certificateNames} onChange={event => setCertificateNames(event.target.value)} placeholder="example.org, www.example.org"/></label><label className="creation-field"><span>{fa ? "روزهای اعتبار" : "Validity in days"}</span><input aria-label="Validity in days" disabled={locked} dir="ltr" inputMode="numeric" value={validDays} onChange={event => setValidDays(event.target.value)}/></label></div><div className="spec-actions"><button type="button" disabled={locked || !caps?.certificates.generate} onClick={() => void generateCertificate()}>{fa ? "تولید گواهی خودامضا" : "Generate self-signed certificate"}</button><button type="button" disabled={locked || !caps?.certificates.inspect} onClick={() => void inspect()}><ShieldCheck size={14}/>{fa ? "بررسی گواهی واردشده" : "Inspect entered certificate"}</button></div></div></details>
    </>}
    {mode === "security" && <details className="spec-disclosure"><summary><Search size={14}/>{fa ? "اسکن و یافتن هدف" : "Scan and find targets"}</summary><div className="spec-group-body">
      <div className="spec-fields"><label className="creation-field"><span>{fa ? "محل بررسی اتصال" : "Probe from"}</span><select aria-label="Probe from" disabled={locked} value={scope} onChange={event => setScope(event.target.value)}><option value="hub">HUB — {fa ? "همین سرور" : "this server"}</option>{nodes.filter(node => node.kind !== "hub" && node.node_id !== "hub-local").map(node => <option value={node.node_id} key={node.node_id} disabled={!caps?.node_probe_available}>{node.name}</option>)}</select></label><label className="creation-field"><span>{fa ? "روش جست‌وجو" : "Search mode"}</span><select aria-label="Search mode" disabled={locked} value={searchMode} onChange={event => setSearchMode(event.target.value)}><option value="single">{fa ? "یک هدف" : "Single target"}</option><option value="presets">{fa ? "هدف‌های پیشنهادی" : "Suggested targets"}</option><option value="list">{fa ? "فهرست دلخواه" : "Candidate list"}</option><option value="cidr">{fa ? "محدودهٔ IP عمومی" : "Public IP range"}</option></select></label>
      {searchMode === "single" && <label className="creation-field"><span>{fa ? "هدف با پورت" : "Target with port"}</span><input aria-label="Scan target" dir="ltr" disabled={locked} value={target} placeholder={String(spec.reality.target ?? "example.org:443")} onChange={event => setTarget(event.target.value)}/></label>}
      {searchMode === "cidr" && <><label className="creation-field"><span>CIDR</span><input aria-label="Scan CIDR" dir="ltr" disabled={locked} value={cidr} onChange={event => setCidr(event.target.value)}/></label><label className="creation-field"><span>{fa ? "پورت" : "Port"}</span><input aria-label="Scan port" inputMode="numeric" dir="ltr" disabled={locked} value={port} onChange={event => setPort(event.target.value)}/></label></>}
      {["single", "cidr"].includes(searchMode) && <label className="creation-field"><span>SNI</span><input aria-label="Scan SNI" dir="ltr" disabled={locked} value={sni} onChange={event => setSni(event.target.value)} placeholder="example.org"/></label>}
      {searchMode === "list" && <label className="creation-field spec-wide"><span>{fa ? "در هر خط هاست:پورت و SNI اختیاری" : "One host:port and optional SNI per line"}</span><textarea aria-label="Scan candidates" rows={4} dir="ltr" disabled={locked} value={targets} onChange={event => setTargets(event.target.value)} placeholder="example.org:443 example.org"/></label>}</div>
      {searchMode === "presets" && caps?.default_targets?.length ? <p className="creation-help"><bdi>{caps.default_targets.map(item => item.target).join(" · ")}</bdi></p> : null}
      {caps && !caps.node_probe_available && <p className="creation-help">{fa ? "بررسی از نودهای راه‌دور در این سرور آماده نیست؛ محل انتخاب‌شده خودکار عوض نمی‌شود." : "Remote-node probing is unavailable on this server. The selected probe location is never substituted."}</p>}
      <div className="spec-actions"><button type="button" disabled={locked || !(searchMode === "single" ? caps?.reality.scan : caps?.reality.find)} onClick={() => void scan()}><Search size={14}/>{fa ? "شروع بررسی هدف‌ها" : "Check targets"}</button>{searchMode === "single" && <button type="button" disabled={locked || !caps?.certificates.remote} onClick={() => void scan(true)}>{fa ? "بررسی گواهی هدف" : "Inspect target certificate"}</button>}</div>
      {!!results.length && <ul className="spec-scan-results">{results.map((result, index) => <li key={`${result.target}-${index}`}><header><bdi>{result.target}</bdi><span data-suitable={result.suitable}>{result.suitable ? fa ? "مناسب" : "Suitable" : fa ? "تأیید نشد" : "Not confirmed"}</span></header><small>{fa ? "محل اجرا" : "Probed from"}: <bdi>{result.scope === "hub" ? "HUB" : nodes.find(node => node.node_id === result.node_id)?.name ?? result.node_id}</bdi> · SNI: <bdi>{result.server_name || "—"}</bdi>{result.server_name_source === "certificate_san" && <> ({fa ? "از گواهی" : "from certificate"})</>}</small><small><bdi>{result.ip || "—"} · {result.tls_version || "—"} · ALPN {result.alpn || "—"} · {result.latency_ms} ms · X25519 {result.x25519 ? "✓" : "—"}</bdi></small>{result.certificate && <small>{fa ? "زنجیره / نام / زمان گواهی" : "Certificate chain / name / time"}: {[result.certificate.chain_valid, result.certificate.hostname_valid, result.certificate.time_valid].map(value => value ? "✓" : "—").join(" / ")}</small>}{(result.reason || result.error) && <small>{result.reason || result.error}</small>}<small>{fa ? "تبادل کلید ترکیبی اندازه‌گیری نشده است." : "Hybrid key exchange was not measured."}</small>{result.suitable && spec.security === "reality" && <button type="button" disabled={disabled} onClick={() => { let next = writeSpecPath(current.current, "reality.target", result.target); next = writeSpecPath(next, "reality.serverNames", [result.server_name]); onChange(next); }}><Check size={14}/>{fa ? "استفاده از این هدف و SNI" : "Use target and SNI"}</button>}</li>)}</ul>}
    </div></details>}
    {firstCert && <section className="spec-group"><h5>{fa ? "نتیجهٔ بررسی گواهی" : "Certificate inspection"}</h5><dl className="spec-tool-details"><div><dt>{fa ? "نام‌ها" : "Names"}</dt><dd><bdi>{[...(firstCert.dns_names ?? []), ...(firstCert.ip_addresses ?? [])].join(", ")}</bdi></dd></div><div><dt>{fa ? "صادرکننده" : "Issuer"}</dt><dd>{firstCert.issuer}</dd></div><div><dt>{fa ? "اعتبار" : "Validity"}</dt><dd><bdi>{firstCert.not_before} → {firstCert.not_after}</bdi></dd></div><div><dt>SHA-256</dt><dd><bdi>{firstCert.sha256_hex}</bdi></dd></div><div><dt>SPKI SHA-256</dt><dd><bdi>{firstCert.spki_sha256_hex}</bdi></dd></div><div><dt>{fa ? "گواهی / نام / زمان" : "Chain / name / time"}</dt><dd>{[inspection?.chain_valid, inspection?.hostname_valid, inspection?.time_valid].map(value => value ? "✓" : "—").join(" / ")}</dd></div></dl>{firstCert.sha256_hex && spec.security === "tls" && <button type="button" disabled={disabled} onClick={() => onChange(writeSpecPath(current.current, "client.pinnedPeerCertSha256", [firstCert.sha256_hex]))}>{fa ? "استفاده از هش برای Pin کلاینت" : "Use certificate pin for clients"}</button>}</section>}
    {busy && <button type="button" className="spec-generate" onClick={() => actionController.current?.abort()}>{fa ? "توقف عملیات" : "Cancel operation"}</button>}{(busy || message) && <p className="spec-tool-state" role="status">{busy && <LoaderCircle size={14} className="creation-spinner"/>}{message || (fa ? "در حال انجام…" : "Working…")}</p>}
  </div></details>;
}
