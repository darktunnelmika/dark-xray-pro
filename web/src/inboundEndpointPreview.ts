import type { CreationPayload } from "./InboundCreationModel";
import type { InboundEndpoint } from "./inboundEndpointOptions";
import { canonicalEndpointAddress } from "./inboundEndpointOptions";
import { canonicalXHTTPOptions, type InboundXHTTPOptions } from "./inboundNetworkOptions";

export type CreationEndpointPreview = {
  node_id: string; path: "direct" | "tunnel" | "swap"; address: string; port: number; listen_port: number;
  label: string; enabled: boolean; server_name: string; transport: string; fingerprint: string; spider_x: string;
  grpc_service?: string; grpc_authority?: string; grpc_multi_mode?: boolean; xhttp?: InboundXHTTPOptions;
};
function canonical(value: CreationEndpointPreview) {
  const allowed = ["node_id", "path", "address", "port", "listen_port", "label", "enabled", "server_name", "transport", "fingerprint", "spider_x", "grpc_service", "grpc_authority", "grpc_multi_mode", "xhttp"];
  if (!value || typeof value !== "object" || Array.isArray(value) || Object.keys(value).some(key => !allowed.includes(key))) throw new Error("invalid endpoint preview");
  for (const key of ["node_id", "path", "address", "label", "server_name", "transport", "fingerprint", "spider_x"] as const) if (typeof value[key] !== "string") throw new Error("invalid endpoint preview");
  if (!Number.isSafeInteger(value.port) || !Number.isSafeInteger(value.listen_port) || typeof value.enabled !== "boolean") throw new Error("invalid endpoint preview");
  if (value.grpc_service !== undefined && typeof value.grpc_service !== "string" || value.grpc_authority !== undefined && typeof value.grpc_authority !== "string" || value.grpc_multi_mode !== undefined && typeof value.grpc_multi_mode !== "boolean") throw new Error("invalid endpoint preview");
  return { node_id: value.node_id, path: value.path, address: canonicalEndpointAddress(value.address), port: value.port, listen_port: value.listen_port,
    label: value.label, enabled: value.enabled, server_name: value.server_name, transport: value.transport, fingerprint: value.fingerprint, spider_x: value.spider_x,
    grpc_service: value.grpc_service ?? "", grpc_authority: value.grpc_authority ?? "", grpc_multi_mode: value.grpc_multi_mode ?? false, xhttp: canonicalXHTTPOptions(value.xhttp) };
}
export function expectedEndpointPreviews(body: Pick<CreationPayload, "profile" | "nodes">): CreationEndpointPreview[] {
  const result: CreationEndpointPreview[] = [], profile = body.profile, options = profile.options;
  function add(nodeID: string, path: "direct" | "tunnel" | "swap", listenPort: number, endpoint: InboundEndpoint) {
    result.push({ node_id: nodeID, path, address: canonicalEndpointAddress(endpoint.address), port: endpoint.port || listenPort, listen_port: listenPort,
      label: endpoint.label?.trim() || `${profile.name.trim()} · ${{ direct: "Direct", tunnel: "Tunnel", swap: "Swap" }[path]}`,
      enabled: endpoint.enabled, server_name: endpoint.sni || profile.server_names[0] || "", transport: profile.transport,
      fingerprint: endpoint.fingerprint || options?.client?.fingerprint || "chrome", spider_x: endpoint.spider_x || options?.client?.spider_x || "/",
      ...(profile.transport === "grpc" ? { grpc_service: profile.grpc_service, grpc_authority: options?.grpc_authority, grpc_multi_mode: options?.grpc_multi_mode } : {}),
      ...(profile.transport === "xhttp" && canonicalXHTTPOptions(options?.xhttp) ? { xhttp: canonicalXHTTPOptions(options?.xhttp) } : {}) });
  }
  for (const node of [...body.nodes].sort((a, b) => a.node_id < b.node_id ? -1 : a.node_id > b.node_id ? 1 : 0)) {
    for (const path of ["direct", "tunnel"] as const) {
      const port = node[`${path}_port`], endpoints = node[`${path}_endpoints`];
      if (!port) continue;
      if (endpoints?.length) for (const endpoint of endpoints) add(node.node_id, path, port, endpoint);
      else for (const address of node[`${path}_hosts`]) add(node.node_id, path, port, { address, port: 0, enabled: true });
    }
    if (node.swap_enabled && node.swap_entry_address) add(node.node_id, "swap", node.swap_destination_port, { address: node.swap_entry_address, port: node.swap_entry_port, enabled: true });
  }
  return result;
}
export function validEndpointPreviews(values: CreationEndpointPreview[] | undefined, body: CreationPayload): boolean {
  try { return Array.isArray(values) && JSON.stringify(values.map(canonical)) === JSON.stringify(expectedEndpointPreviews(body).map(canonical)); }
  catch { return false; }
}
