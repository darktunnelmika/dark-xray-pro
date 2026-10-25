import { ApiError } from "./session";
import type { CreationIssue } from "./InboundCreationModel";
import { assertPublicInboundData } from "./inboundTLS";

export class CreationApiError extends ApiError {
  constructor(status: number, message: string, public issue?: CreationIssue) { super(status, message); }
}

/** Keep the setup API's bounded field errors; never show raw server responses. */
export async function creationAPI<T>(path: string, options: RequestInit, onUnauthorized: () => void): Promise<T> {
  if (!/^\/api\/v1\/workspace\/native-inbound-plans(?:\/|$)/.test(path)) throw new CreationApiError(0, "invalid_api_path");
  const controller = new AbortController(), abort = () => controller.abort();
  options.signal?.addEventListener("abort", abort, { once: true });
  if (options.signal?.aborted) abort();
  const timer = window.setTimeout(abort, 8000);
  try {
    const response = await fetch(path, { ...options, cache: "no-store", credentials: "same-origin", signal: controller.signal,
      headers: { Accept: "application/json", ...(options.headers ?? {}) } });
    if (!response.ok) {
      if (response.status === 401) onUnauthorized();
      let message = "request_failed", issue: CreationIssue | undefined;
      try {
        const body = await response.json();
        if (typeof body?.error === "string" && /^[a-z_]{3,80}$/.test(body.error)) message = body.error;
        const problem = body?.issue;
        const validNodeID = (value: unknown) => value === undefined || typeof value === "string" && /^[A-Za-z0-9_-]{1,128}$/.test(value);
        if (problem && typeof problem.field === "string" && /^[A-Za-z_0-9.]{1,256}$/.test(problem.field) && typeof problem.code === "string" && /^[a-z_]{3,100}$/.test(problem.code) &&
          validNodeID(problem.node_id) && validNodeID(problem.destination_node_id) &&
          (problem.path === undefined || ["direct", "tunnel", "swap_source", "swap_destination"].includes(problem.path)) &&
          (problem.port === undefined || Number.isSafeInteger(problem.port) && problem.port >= 1 && problem.port <= 65535)) {
          const fields: Record<string, string> = {
            listen_ip: "listenIP", direct_hosts: "directHosts", tunnel_hosts: "tunnelHosts", direct_port: "directPort", tunnel_port: "tunnelPort",
            direct_endpoints: "directEndpoints", tunnel_endpoints: "tunnelEndpoints",
            swap_source_node_id: "sourceNode", swap_source_listen_ip: "sourceListenIP", swap_source_port: "sourcePort", swap_entry_address: "entryHost",
            swap_entry_port: "entryPort", swap_destination_port: "swapPort", swap_ports: "sourcePort", profile: "name", "profile.id": "name", "profile.name": "name",
            "profile.transport": "transport", "profile.grpc_service": "grpcService", "profile.reality_target": "realityTarget", "profile.server_names": "serverNames", "profile.short_ids": "shortIDs",
            "profile.security": "security", tls_credentials: "tlsCertificate", "tls_credentials.certificate_pem": "tlsCertificate", "tls_credentials.private_key_pem": "tlsPrivateKey",
            "profile.options": "realityMaxTimeDiff", "profile.options.sniffing": "sniffing.domainsExcluded", "profile.options.fallbacks": "fallbacks",
            "profile.options.xhttp": "xhttpPath", "profile.options.xhttp.path": "xhttpPath", "profile.options.xhttp.host": "xhttpHost",
            "profile.options.xhttp.mode": "xhttpMode", "profile.options.xhttp.x_padding_bytes": "xhttpPadding",
            "profile.options.client": "clientFingerprint", "profile.options.client.fingerprint": "clientFingerprint", "profile.options.client.spider_x": "clientSpiderX",
            "profile.options.websocket": "websocketPath", "profile.options.websocket.path": "websocketPath", "profile.options.websocket.host": "websocketHost",
            "profile.options.http_upgrade": "httpUpgradePath", "profile.options.http_upgrade.path": "httpUpgradePath", "profile.options.http_upgrade.host": "httpUpgradeHost",
          };
          const endpoint = problem.field.match(/^(direct|tunnel)_endpoints\.(\d{1,2})\.(address|port|label|enabled|sni|fingerprint|spider_x)$/);
          const field = endpoint ? `${endpoint[1]}Endpoints.${endpoint[2]}.${endpoint[3] === "spider_x" ? "spiderX" : endpoint[3]}` : problem.field.startsWith("profile.spec.") ? problem.field.slice("profile.".length) : fields[problem.field] ?? problem.field;
          issue = { nodeID: problem.node_id, destinationNodeID: problem.destination_node_id, path: problem.path, port: problem.port, field, code: problem.code };
        }
      } catch { /* Responses outside the structured contract are not surfaced. */ }
      throw new CreationApiError(response.status, message, issue);
    }
    const body = await response.json();
    assertPublicInboundData(body);
    return body as T;
  } finally { window.clearTimeout(timer); options.signal?.removeEventListener("abort", abort); }
}
