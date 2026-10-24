import type { Inbound, InboundNode } from "./inboundTypes";
import type { InboundProfileValue } from "./InboundProfileForm";
import { canonicalProfileOptions, profileOptionIssues, profileOptionsDraft, profileOptionsPayload } from "./inboundProfileOptions";

export type CreationPath = { enabled: boolean; port: string; hosts: string };
export type CreationNode = {
  nodeID: string;
  installationID: string;
  listenIP: string;
  direct: CreationPath;
  tunnel: CreationPath;
  swap: { enabled: boolean; sourceNodeID: string; sourceInstallationID: string; sourceListenIP: string; sourcePort: string; entryHost: string; entryPort: string; destinationPort: string };
};
export type CreationState = {
  profile: InboundProfileValue;
  selected: string[];
  nodes: Record<string, CreationNode>;
  reason: string;
  shortIDs: string;
  profileOptionsPresent: boolean;
};
export type CreationIssue = { nodeID?: string; field: string; code: string };
export type CreationPlanNode = {
  node_id: string; installation_id: string; listen_ip: string;
  direct_port: number; direct_hosts: string[]; tunnel_port: number; tunnel_hosts: string[];
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
  plans: Array<{ inbound_id: number; plan_revision: number; inbound_revision: number; status: string }>;
};
export type CreationPayload = {
  event_id: string; expected_plan_revision: number; expected_inbound_revision: number;
  profile: Pick<Inbound, "id" | "name" | "transport" | "grpc_service" | "reality_target" | "server_names" | "short_ids" | "options">;
  nodes: CreationPlanNode[]; reason: string;
};
export type CreationPreview = { valid: true; plan: CreationPlan; runtime_changed: false; topology_validated: true; topology_validation_only: true; topology_key_scope: "preview_only" | "saved_profile" };
export type CreationReceipt = { plan: CreationPlan; event_id: string; replayed: boolean; runtime_changed: false; clients_deleted: false;
  credit_changed: false; usage_reset: false; subscription_published: false };
export type CreationPlanResponse = { plan: CreationPlan; references_current: boolean };

export function stateFromCreationPlan(plan: CreationPlan): CreationState {
  const state = emptyCreation(plan.profile);
  state.reason = plan.reason; state.selected = plan.nodes.map(row => row.node_id);
  for (const row of plan.nodes) state.nodes[row.node_id] = { nodeID: row.node_id, installationID: row.installation_id, listenIP: row.listen_ip,
    direct: { enabled: row.direct_port > 0, port: row.direct_port ? String(row.direct_port) : "", hosts: row.direct_hosts.join("\n") },
    tunnel: { enabled: row.tunnel_port > 0, port: row.tunnel_port ? String(row.tunnel_port) : "", hosts: row.tunnel_hosts.join("\n") },
    swap: { enabled: row.swap_enabled, sourceNodeID: row.swap_source_node_id, sourceInstallationID: row.swap_source_installation_id,
      sourceListenIP: row.swap_source_listen_ip, sourcePort: row.swap_source_port ? String(row.swap_source_port) : "", entryHost: row.swap_entry_address,
      entryPort: row.swap_entry_port ? String(row.swap_entry_port) : "", destinationPort: row.swap_destination_port ? String(row.swap_destination_port) : "" } };
  return state;
}
export function creationPayload(state: CreationState, inboundID: number, planRevision: number, inboundRevision: number, eventID: string): CreationPayload {
  return { event_id: eventID, expected_plan_revision: planRevision, expected_inbound_revision: inboundRevision,
    profile: { id: inboundID, name: state.profile.name, transport: state.profile.transport,
      grpc_service: state.profile.transport === "grpc" ? state.profile.grpcService : "", reality_target: state.profile.realityTarget,
      server_names: state.profile.serverNames.split(/[\s,]+/).filter(Boolean), short_ids: creationShortIDs(state.shortIDs),
      options: profileOptionsPayload(state.profile.options, state.profile.transport) ?? (planRevision > 0 && state.profileOptionsPresent ? {} : undefined) },
    nodes: selectedCreationNodes(state).map(row => ({ node_id: row.nodeID, installation_id: row.installationID, listen_ip: row.listenIP,
      direct_port: row.direct.enabled ? creationPort(row.direct.port) : 0, direct_hosts: row.direct.enabled ? creationHosts(row.direct.hosts) : [],
      tunnel_port: row.tunnel.enabled ? creationPort(row.tunnel.port) : 0, tunnel_hosts: row.tunnel.enabled ? creationHosts(row.tunnel.hosts) : [],
      swap_enabled: row.swap.enabled, swap_source_node_id: row.swap.enabled ? row.swap.sourceNodeID : "",
      swap_source_installation_id: row.swap.enabled ? row.swap.sourceInstallationID : "",
      swap_source_listen_ip: row.swap.enabled ? row.swap.sourceListenIP : "",
      swap_source_port: row.swap.enabled ? creationPort(row.swap.sourcePort) : 0,
      swap_entry_address: row.swap.enabled ? row.swap.entryHost.trim().toLowerCase() : "",
      swap_entry_port: row.swap.enabled ? creationEntryPort(row.swap.entryPort) : 0,
      swap_destination_port: row.swap.enabled ? creationPort(row.swap.destinationPort) : 0 })), reason: state.reason.trim() };
}
export function creationWaitingHosts(row: CreationNode): boolean {
  return row.direct.enabled && !creationHosts(row.direct.hosts).length || row.tunnel.enabled && !creationHosts(row.tunnel.hosts).length || row.swap.enabled && !row.swap.entryHost.trim();
}

export function emptyCreation(source?: Inbound): CreationState {
  return {
    profile: { name: source?.name ?? "", transport: source?.transport ?? "tcp", grpcService: source?.grpc_service ?? "",
      realityTarget: source?.reality_target ?? "", serverNames: source?.server_names.join(", ") ?? "", options: profileOptionsDraft(source?.options) },
    selected: [], nodes: {}, reason: "", profileOptionsPresent: !!canonicalProfileOptions(source?.options), shortIDs: source?.short_ids.join(", ") ?? freshCreationShortID(),
  };
}
export function newCreationNode(node: InboundNode): CreationNode {
  return { nodeID: node.node_id, installationID: node.installation_id, listenIP: "",
    direct: { enabled: true, port: "", hosts: "" }, tunnel: { enabled: false, port: "", hosts: "" },
    swap: { enabled: false, sourceNodeID: "", sourceInstallationID: "", sourceListenIP: "", sourcePort: "", entryHost: "", entryPort: "", destinationPort: "" } };
}
export function creationHosts(raw: string): string[] { return raw.split(/[,\n\r]+/).map(value => value.trim().toLowerCase()).filter(Boolean); }
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

/** Editing checks are advisory; the atomic server preview/save repeats validation. */
export function creationIssues(state: CreationState, available: InboundNode[]): CreationIssue[] {
  const issues: CreationIssue[] = [];
  const profile = state.profile;
  if (!profile.name.trim()) issues.push({ field: "name", code: "name_required" });
  if (profile.transport === "grpc" && !/^[A-Za-z0-9_-]{1,64}$/.test(profile.grpcService)) issues.push({ field: "grpcService", code: "grpc_service_invalid" });
  const target = profile.realityTarget.match(/^(?:\[[0-9a-fA-F:.]+\]|[^:\s/\\@%\[\]]+):([0-9]+)$/);
  if (!target || Number(target[1]) < 1 || Number(target[1]) > 65535) issues.push({ field: "realityTarget", code: "target_invalid" });
  if (!profile.serverNames.split(/[\s,]+/).filter(Boolean).length) issues.push({ field: "serverNames", code: "server_names_required" });
  issues.push(...profileOptionIssues(profile.options, profile.transport));
  const shortIDs = creationShortIDs(state.shortIDs);
  if (!shortIDs.length || shortIDs.length > 16 || new Set(shortIDs).size !== shortIDs.length || shortIDs.some(id => !/^[0-9a-f]{2,16}$/.test(id) || id.length % 2 !== 0)) issues.push({ field: "shortIDs", code: "short_ids_invalid" });
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
      const hosts = creationHosts(path.hosts);
      if (hosts.some(host => /[\s/:@%?#\\]/.test(host))) add(key + "Hosts", "host_invalid");
      if (new Set(hosts).size !== hosts.length) add(key + "Hosts", "host_duplicate");
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
      if (row.swap.entryHost.trim() && /[\s/:@%?#\\]/.test(row.swap.entryHost.trim())) add("entryHost", "host_invalid");
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
