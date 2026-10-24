import { ApiError } from "./session";
import type { CreationIssue } from "./InboundCreationModel";

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
        if (problem && typeof problem.field === "string" && /^[a-z_.]{1,80}$/.test(problem.field) && typeof problem.code === "string" && /^[a-z_]{3,80}$/.test(problem.code) &&
          (problem.node_id === undefined || typeof problem.node_id === "string" && /^[A-Za-z0-9_-]{1,128}$/.test(problem.node_id))) {
          const fields: Record<string, string> = { listen_ip: "listenIP", direct_hosts: "directHosts", tunnel_hosts: "tunnelHosts", direct_port: "directPort", tunnel_port: "tunnelPort", swap_source_node_id: "sourceNode", swap_source_listen_ip: "sourceListenIP", swap_source_port: "sourcePort", swap_entry_address: "entryHost", swap_entry_port: "entryPort", swap_destination_port: "swapPort", swap_ports: "sourcePort", profile: "name", "profile.options": "realityMaxTimeDiff", "profile.options.sniffing": "sniffing.domainsExcluded", "profile.options.fallbacks": "fallbacks" };
          issue = { nodeID: problem.node_id, field: fields[problem.field] ?? problem.field, code: problem.code };
        }
      } catch { /* Responses outside the structured contract are not surfaced. */ }
      throw new CreationApiError(response.status, message, issue);
    }
    return await response.json() as T;
  } finally { window.clearTimeout(timer); options.signal?.removeEventListener("abort", abort); }
}
