import type { Inbound, InboundNode } from "./inboundTypes";
import type { InboundProfileValue } from "./InboundProfileForm";
import { canonicalProfileOptions, profileOptionIssues, profileOptionsDraft, profileOptionsPayload } from "./inboundProfileOptions";
import { canonicalEndpointAddress, endpointAddresses, endpointDraft, endpointIssues, endpointPayload, validEndpointAddress, type EndpointDraft, type InboundEndpoint } from "./inboundEndpointOptions";
import { validUTF8 } from "./inboundNetworkOptions";
import type { CreationEndpointPreview } from "./inboundEndpointPreview";
import { assertPublicInboundData, canonicalTLSMetadata, profileSecurity, tlsCredentialIssues, type InboundSecurity, type TLSCredentials, type TLSMetadata } from "./inboundTLS";

export type CreationPath = { enabled: boolean; port: string; hosts: string; endpoints?: EndpointDraft[]; endpointsPresent?: boolean };
export type CreationNode = {
  nodeID: string;
  installationID: string;
  listenIP: string;
  direct: CreationPath;
  tunnel: CreationPath;
  swap: { enabled: boolean; sourceNodeID: string; sourceInstallationID: string; sourceListenIP: string; sourcePort: string; entryHost: string; entryPort: string; destinationPort: string };
};
export type CreationState = {
  profile: InboundProfileValue & { security: InboundSecurity };
  selected: string[];
  nodes: Record<string, CreationNode>;
  reason: string;
  shortIDs: string;
  profileOptionsPresent: boolean;
  tlsMetadata?: TLSMetadata;
  tlsCredentials: TLSCredentials;
  replaceTLS: boolean;
};
export type CreationIssue = { nodeID?: string; field: string; code: string; destinationNodeID?: string; path?: "direct" | "tunnel" | "swap_source" | "swap_destination"; port?: number };
export type CreationPlanNode = {
  node_id: string; installation_id: string; listen_ip: string;
  direct_port: number; direct_hosts: string[]; tunnel_port: number; tunnel_hosts: string[];
  direct_endpoints?: InboundEndpoint[]; tunnel_endpoints?: InboundEndpoint[];
  swap_enabled: boolean; swap_source_node_id: string; swap_source_installation_id: string;
  swap_source_listen_ip: string; swap_source_port: number; swap_entry_address: string; swap_entry_port: number; swap_destination_port: number;
};
export type CreationPlan = {
  profile: Inbound; nodes: CreationPlanNode[]; plan_revision: number; inbound_revision: number;
  status: "SAVED_DISABLED" | "WAITING_HOST"; created_unix: number; updated_unix: number; reason: string; runtime_apply: false;
  last_event_id: string; editable: boolean; edit_blocker?: string;
};
export type CreationCapabilities = {
  available: true; version: number; max_nodes: number; max_hosts_per_node: number;
  protocols: string[]; securities: string[]; transports: string[]; runtime_apply: false;
  profile_options?: boolean;
  profile_sniffing?: boolean;
  profile_fallbacks?: boolean;
  profile_traffic_options_scope?: "creation_plan";
  profile_xhttp?: boolean; profile_client_options?: boolean; profile_network_options_scope?: "creation_plan";
  profile_tls?: boolean; profile_tls_scope?: "creation_plan"; profile_websocket?: boolean; profile_http_upgrade?: boolean;
  endpoint_options?: boolean; max_endpoints_per_path?: number;
  listener_port_min?: number; listener_port_max?: number; reserved_listener_ports?: number[];
  plans: Array<{ inbound_id: number; plan_revision: number; inbound_revision: number; status: string; node_ids?: string[] }>;
};
export type CreationPayload = {
  event_id: string; expected_plan_revision: number; expected_inbound_revision: number;
  profile: Pick<Inbound, "id" | "name" | "transport" | "grpc_service" | "server_names" | "options" | "security"> & Partial<Pick<Inbound, "reality_target" | "short_ids">>;
  nodes: CreationPlanNode[]; reason: string;
  tls_credentials?: TLSCredentials;
};
export type CreationPreview = { valid: true; plan: CreationPlan; runtime_changed: false; topology_validated: true; topology_validation_only: true; topology_key_scope: "preview_only" | "saved_profile"; endpoints?: CreationEndpointPreview[] };
export type CreationReceipt = { plan: CreationPlan; event_id: string; replayed: boolean; runtime_changed: false; clients_deleted: false;
  credit_changed: false; usage_reset: false; subscription_published: false };
export type CreationPlanResponse = { plan: CreationPlan; references_current: boolean };

export function stateFromCreationPlan(plan: CreationPlan): CreationState {
  assertPublicInboundData(plan);
  const state = emptyCreation(plan.profile);
  if (state.profile.security === "tls") state.tlsMetadata = canonicalTLSMetadata(plan.profile.tls);
  else if (plan.profile.tls !== undefined) throw new Error("unexpected_tls_metadata");
  state.replaceTLS = false;
  state.reason = plan.reason; state.selected = plan.nodes.map(row => row.node_id);
  for (const row of plan.nodes) state.nodes[row.node_id] = { nodeID: row.node_id, installationID: row.installation_id, listenIP: row.listen_ip,
    direct: { enabled: row.direct_port > 0, port: row.direct_port ? String(row.direct_port) : "", hosts: row.direct_hosts.join("\n"), endpoints: row.direct_endpoints?.length ? row.direct_endpoints.map(endpointDraft) : undefined, endpointsPresent: !!row.direct_endpoints?.length },
    tunnel: { enabled: row.tunnel_port > 0, port: row.tunnel_port ? String(row.tunnel_port) : "", hosts: row.tunnel_hosts.join("\n"), endpoints: row.tunnel_endpoints?.length ? row.tunnel_endpoints.map(endpointDraft) : undefined, endpointsPresent: !!row.tunnel_endpoints?.length },
    swap: { enabled: row.swap_enabled, sourceNodeID: row.swap_source_node_id, sourceInstallationID: row.swap_source_installation_id,
      sourceListenIP: row.swap_source_listen_ip, sourcePort: row.swap_source_port ? String(row.swap_source_port) : "", entryHost: row.swap_entry_address,
      entryPort: row.swap_entry_port ? String(row.swap_entry_port) : "", destinationPort: row.swap_destination_port ? String(row.swap_destination_port) : "" } };
  return state;
}
export function creationPayload(state: CreationState, inboundID: number, planRevision: number, inboundRevision: number, eventID: string): CreationPayload {
  return { event_id: eventID, expected_plan_revision: planRevision, expected_inbound_revision: inboundRevision,
    profile: { id: inboundID, name: state.profile.name, transport: state.profile.transport, security: state.profile.security,
      grpc_service: state.profile.transport === "grpc" ? state.profile.grpcService : "",
      ...(state.profile.security === "tls" ? {} : { reality_target: state.profile.realityTarget, short_ids: creationShortIDs(state.shortIDs) }),
      server_names: state.profile.serverNames.split(/[\s,]+/).filter(Boolean),
      options: profileOptionsPayload(state.profile.options, state.profile.transport, state.profile.security) ?? (planRevision > 0 && state.profileOptionsPresent ? {} : undefined) },
    ...(state.profile.security === "tls" && (state.replaceTLS || !state.tlsMetadata) ? { tls_credentials: { ...state.tlsCredentials } } : {}),
    nodes: selectedCreationNodes(state).map(row => ({ node_id: row.nodeID, installation_id: row.installationID, listen_ip: row.listenIP,
      direct_port: row.direct.enabled ? creationPort(row.direct.port) : 0, direct_hosts: row.direct.enabled ? creationPathHosts(row.direct) : [],
      tunnel_port: row.tunnel.enabled ? creationPort(row.tunnel.port) : 0, tunnel_hosts: row.tunnel.enabled ? creationPathHosts(row.tunnel) : [],
      ...(row.direct.endpoints !== undefined || row.direct.endpointsPresent ? { direct_endpoints: row.direct.enabled ? row.direct.endpoints?.map(endpoint => endpointPayload(endpoint, state.profile.security)) ?? [] : [] } : {}),
      ...(row.tunnel.endpoints !== undefined || row.tunnel.endpointsPresent ? { tunnel_endpoints: row.tunnel.enabled ? row.tunnel.endpoints?.map(endpoint => endpointPayload(endpoint, state.profile.security)) ?? [] : [] } : {}),
      swap_enabled: row.swap.enabled, swap_source_node_id: row.swap.enabled ? row.swap.sourceNodeID : "",
      swap_source_installation_id: row.swap.enabled ? row.swap.sourceInstallationID : "",
      swap_source_listen_ip: row.swap.enabled ? row.swap.sourceListenIP : "",
      swap_source_port: row.swap.enabled ? creationPort(row.swap.sourcePort) : 0,
      swap_entry_address: row.swap.enabled ? canonicalEndpointAddress(row.swap.entryHost) : "",
      swap_entry_port: row.swap.enabled ? creationEntryPort(row.swap.entryPort) : 0,
      swap_destination_port: row.swap.enabled ? creationPort(row.swap.destinationPort) : 0 })), reason: state.reason.trim() };
}
export function creationWaitingHosts(row: CreationNode): boolean {
  return row.direct.enabled && !creationPathHosts(row.direct).length || row.tunnel.enabled && !creationPathHosts(row.tunnel).length || row.swap.enabled && !row.swap.entryHost.trim();
}

export function emptyCreation(source?: Inbound): CreationState {
  assertPublicInboundData(source);
  return {
    profile: { name: source?.name ?? "", transport: source?.transport ?? "tcp", grpcService: source?.grpc_service ?? "", security: profileSecurity(source?.security),
      realityTarget: source?.reality_target ?? "", serverNames: source?.server_names.join(", ") ?? "", options: profileOptionsDraft(source?.options) },
    selected: [], nodes: {}, reason: "", profileOptionsPresent: !!canonicalProfileOptions(source?.options), shortIDs: source?.short_ids?.join(", ") ?? freshCreationShortID(),
    tlsCredentials: { certificate_pem: "", private_key_pem: "" }, replaceTLS: false,
  };
}
export function newCreationNode(node: InboundNode): CreationNode {
  return { nodeID: node.node_id, installationID: node.installation_id, listenIP: "",
    direct: { enabled: true, port: "", hosts: "" }, tunnel: { enabled: false, port: "", hosts: "" },
    swap: { enabled: false, sourceNodeID: "", sourceInstallationID: "", sourceListenIP: "", sourcePort: "", entryHost: "", entryPort: "", destinationPort: "" } };
}
export function creationHosts(raw: string): string[] { return raw.split(/[,\n\r]+/).map(canonicalEndpointAddress).filter(Boolean); }
export function creationPathHosts(path: CreationPath): string[] { return path.endpoints !== undefined ? endpointAddresses(path.endpoints) : creationHosts(path.hosts); }
/** Match Go's net.ParseIP(...).String() when comparing valid, server-normalized bind IPs. */
export function canonicalCreationIP(value: string): string {
  const ip = value.trim();
  if (!ip.includes(":")) return ip;
  try {
    const compressed = new URL("http://[" + ip + "]/").hostname.slice(1, -1);
    const mapped = compressed.match(/^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/);
    if (!mapped) return compressed;
    const high = Number.parseInt(mapped[1], 16), low = Number.parseInt(mapped[2], 16);
    return [high >>> 8, high & 255, low >>> 8, low & 255].join(".");
  } catch { return ip; }
}
export function freshCreationShortID(): string { return Array.from(crypto.getRandomValues(new Uint8Array(4))).map(value => value.toString(16).padStart(2, "0")).join(""); }
export function creationShortIDs(raw: string): string[] { return raw.split(/[\s,]+/).filter(Boolean); }
export function creationPort(raw: string): number { const value = Number(raw); return /^\d+$/.test(raw) && Number.isSafeInteger(value) && value >= 1024 && value <= 65535 ? value : 0; }
export function creationEntryPort(raw: string): number { const value = Number(raw); return /^\d+$/.test(raw) && Number.isSafeInteger(value) && value >= 1 && value <= 65535 ? value : 0; }
export function creationNodeName(nodes: InboundNode[], id: string): string {
  const name = nodes.find(node => node.node_id === id)?.name || id;
  return nodes.filter(node => node.name === name).length > 1 ? `${name} (${id})` : name;
}
export function selectedCreationNodes(state: CreationState): CreationNode[] { return state.selected.map(id => state.nodes[id]).filter((node): node is CreationNode => !!node); }

function validCreationServerName(value: string): boolean {
  const labels = value.split(".");
  const ip = /^(?:0|[1-9][0-9]{0,2})(?:\.(?:0|[1-9][0-9]{0,2})){3}$/.test(value) && labels.every(label => Number(label) <= 255);
  if (!value || value.length > 253 || labels.length < 2 || ip || value.includes(":")) return false;
  return labels.every(label => /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(label));
}

/** Editing checks are advisory; the atomic server preview/save repeats validation. */
export function creationIssues(state: CreationState, available: InboundNode[]): CreationIssue[] {
  const issues: CreationIssue[] = [];
  const profile = state.profile;
  if (!profile.name.trim()) issues.push({ field: "name", code: "name_required" });
  else if (!validUTF8(profile.name) || Array.from(profile.name).length > 128 || /[\u0000-\u001f\u007f-\u009f]/.test(profile.name)) issues.push({ field: "name", code: "name_invalid" });
  const tls = profile.security === "tls";
  if (!(tls ? ["tcp", "grpc", "xhttp", "ws", "httpupgrade"] : ["tcp", "grpc", "xhttp"]).includes(profile.transport)) issues.push({ field: "transport", code: "transport_invalid" });
  if (profile.transport === "grpc" && !/^[A-Za-z0-9_-]{1,64}$/.test(profile.grpcService)) issues.push({ field: "grpcService", code: "grpc_service_invalid" });
  const target = profile.realityTarget.match(/^(?:\[[0-9a-fA-F:.]+\]|[^:\s/\\@%\[\]]+):([0-9]+)$/);
  if (!tls && (!target || Number(target[1]) < 1 || Number(target[1]) > 65535)) issues.push({ field: "realityTarget", code: "target_invalid" });
  const serverNames = profile.serverNames.split(/[\s,]+/).filter(Boolean);
  if (!serverNames.length) issues.push({ field: "serverNames", code: "server_names_required" });
  else if (serverNames.length > 16 || serverNames.some(name => !validCreationServerName(name))) issues.push({ field: "serverNames", code: "server_names_invalid" });
  issues.push(...profileOptionIssues(profile.options, profile.transport, profile.security));
  if (tls && (state.replaceTLS || !state.tlsMetadata)) issues.push(...tlsCredentialIssues(state.tlsCredentials));
  const shortIDs = creationShortIDs(state.shortIDs);
  if (!tls && (!shortIDs.length || shortIDs.length > 16 || new Set(shortIDs).size !== shortIDs.length || shortIDs.some(id => !/^[0-9a-f]{2,16}$/.test(id) || id.length % 2 !== 0))) issues.push({ field: "shortIDs", code: "short_ids_invalid" });
  if (!state.selected.length) issues.push({ field: "nodes", code: "node_required" });
  for (const row of selectedCreationNodes(state)) {
    const add = (field: string, code: string) => issues.push({ nodeID: row.nodeID, field, code });
    if (!available.some(node => node.node_id === row.nodeID && node.installation_id === row.installationID)) add("node", "pairing_changed");
    if (!row.listenIP.trim()) add("listenIP", "listen_ip_required");
    if (!row.direct.enabled && !row.tunnel.enabled && !row.swap.enabled) add("paths", "path_required");
    const ports: number[] = [];
    const allHosts: string[] = [];
    for (const key of ["direct", "tunnel"] as const) {
      const path = row[key]; if (!path.enabled) continue;
      const port = creationPort(path.port);
      if (!port) add(key + "Port", "port_invalid"); else ports.push(port);
      const hosts = creationPathHosts(path);
      if (path.endpoints !== undefined) {
        const defaults = { serverNames, fingerprint: profile.options?.clientFingerprint ?? "", spiderX: tls ? "" : profile.options?.clientSpiderX ?? "", security: profile.security };
        issues.push(...endpointIssues(path.endpoints, path.port, defaults, key + "Endpoints").map(issue => ({ ...issue, nodeID: row.nodeID })));
      } else {
        if (hosts.some(host => !validEndpointAddress(host))) add(key + "Hosts", "host_invalid");
        if (new Set(hosts).size !== hosts.length) add(key + "Hosts", "host_duplicate");
      }
      allHosts.push(...hosts);
    }
    if (new Set(allHosts).size > 64) add("hosts", "host_limit");
    if (row.swap.enabled) {
      const port = creationPort(row.swap.destinationPort);
      if (!port) add("swapPort", "port_invalid");
      else if (!row.tunnel.enabled || port !== creationPort(row.tunnel.port)) ports.push(port);
      const source = available.find(node => node.node_id === row.swap.sourceNodeID && node.installation_id === row.swap.sourceInstallationID);
      if (!source || source.node_id === row.nodeID) add("sourceNode", "source_required");
      if (!row.swap.sourceListenIP.trim()) add("sourceListenIP", "source_listen_ip_required");
      const sourceRow = state.selected.includes(row.swap.sourceNodeID) ? state.nodes[row.swap.sourceNodeID] : undefined;
      if (sourceRow && sourceRow.listenIP.trim() && row.swap.sourceListenIP.trim() && canonicalCreationIP(sourceRow.listenIP) !== canonicalCreationIP(row.swap.sourceListenIP)) add("sourceListenIP", "source_listen_ip_conflict");
      if (!creationPort(row.swap.sourcePort)) add("sourcePort", "port_invalid");
      if (!creationEntryPort(row.swap.entryPort)) add("entryPort", "entry_port_invalid");
      if (row.swap.entryHost.trim() && !validEndpointAddress(row.swap.entryHost)) add("entryHost", "host_invalid");
      if ([2087, 8443, 24443].includes(creationPort(row.swap.sourcePort))) add("sourcePort", "port_reserved");
    }
    if (ports.some(port => [2087, 8443, 24443].includes(port))) add("ports", "port_reserved");
    if (new Set(ports).size !== ports.length) add("ports", "port_collision");
  }
  const rows = selectedCreationNodes(state);
  const edges = new Map(rows.filter(row => row.swap.enabled && row.swap.sourceNodeID).map(row => [row.nodeID, row.swap.sourceNodeID]));
  const relayPorts = new Set<string>();
  for (const row of rows.filter(row => row.swap.enabled)) {
    const source = state.selected.includes(row.swap.sourceNodeID) ? state.nodes[row.swap.sourceNodeID] : undefined;
    const port = creationPort(row.swap.sourcePort);
    const key = row.swap.sourceNodeID + ":" + port;
    if (port && (relayPorts.has(key) || source?.direct.enabled && port === creationPort(source.direct.port) || source?.tunnel.enabled && port === creationPort(source.tunnel.port) || source?.swap.enabled && port === creationPort(source.swap.destinationPort))) issues.push({ nodeID: row.nodeID, field: "sourcePort", code: "source_port_collision" });
    relayPorts.add(key);
  }
  for (const row of rows) {
    let next: string | undefined = row.nodeID; const visited = new Set<string>();
    while (next && edges.has(next)) {
      if (visited.has(next)) { issues.push({ nodeID: row.nodeID, field: "sourceNode", code: "route_cycle" }); break; }
      visited.add(next); next = edges.get(next);
    }
  }
  if (!state.reason.trim() || state.reason.trim().length > 200) issues.push({ field: "reason", code: "reason_required" });
  return issues;
}
