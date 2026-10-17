import { api } from "./session";
export type NodeStatus = "online" | "offline";

export interface NodeSnapshot {
  node_id: string;
  name: string;
  interface: string;
  status: NodeStatus;
  cpu_cores: number;
  cpu_percent: number;
  ram_total_bytes: number;
  ram_used_bytes: number;
  rx_bits_per_second: number | null;
  tx_bits_per_second: number | null;
  last_seen: string | null;
  metrics_at: string | null;
  metrics_current: boolean;
  disk_total_bytes?: number | null;
  disk_used_bytes?: number | null;
  load_1?: number | null;
  load_5?: number | null;
  load_15?: number | null;
  uptime_seconds?: number | null;
  tcp_inuse?: number | null;
}

export interface FleetSnapshot {
  as_of: string;
  total_nodes: number;
  online_nodes: number;
  offline_nodes: number;
  interface_rx_bits_per_second: number;
  interface_tx_bits_per_second: number;
  metric_scope: "default_route_interface_not_billable";
  physical_rates_complete: boolean;
  nodes: NodeSnapshot[];
}

// Fail closed on malformed host telemetry. Never let a truncated backend
// response mutate the last verified fleet shown in the dashboard.
const validCount = (value: unknown): value is number => Number.isSafeInteger(value) && (value as number) >= 0;
const validRate = (value: unknown): value is number => typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 1e15;
const validBytes = (value: unknown): value is number => validCount(value);
const validTimestamp = (value: unknown): boolean =>
  value == null || (typeof value === "string" && Number.isFinite(Date.parse(value)));
const validLoad = (value: unknown): boolean =>
  value == null || (typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 1e6);

function validNode(node: NodeSnapshot): boolean {
  if (!node || typeof node.node_id !== "string" || !/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/.test(node.node_id) ||
      typeof node.name !== "string" || node.name.length > 80 ||
      (node.status !== "online" && node.status !== "offline") ||
      typeof node.interface !== "string" || node.interface.length > 32 ||
      !validCount(node.cpu_cores) || node.cpu_cores > 1024 ||
      typeof node.cpu_percent !== "number" || !Number.isFinite(node.cpu_percent) || node.cpu_percent < 0 || node.cpu_percent > 100 ||
      !validBytes(node.ram_total_bytes) || !validBytes(node.ram_used_bytes) || node.ram_used_bytes > node.ram_total_bytes ||
      (node.metrics_current && (node.status !== "online" || node.ram_total_bytes === 0)) ||
      typeof node.metrics_current !== "boolean" || !validTimestamp(node.last_seen) || !validTimestamp(node.metrics_at)) return false;
  if ((node.disk_total_bytes == null) !== (node.disk_used_bytes == null) ||
      (node.disk_total_bytes != null && (!validBytes(node.disk_total_bytes) || node.disk_total_bytes === 0)) ||
      (node.disk_used_bytes != null && (!validBytes(node.disk_used_bytes) || node.disk_total_bytes == null ||
        node.disk_used_bytes > node.disk_total_bytes)) ||
      !validLoad(node.load_1) || !validLoad(node.load_5) || !validLoad(node.load_15) ||
      (node.uptime_seconds != null && (!validCount(node.uptime_seconds) || node.uptime_seconds > 1e12)) ||
      (node.tcp_inuse != null && (!validCount(node.tcp_inuse) || node.tcp_inuse > 1e8)) ||
      (node.rx_bits_per_second !== null && !validRate(node.rx_bits_per_second)) ||
      (node.tx_bits_per_second !== null && !validRate(node.tx_bits_per_second))) return false;
  return true;
}

export async function getFleet(signal: AbortSignal): Promise<FleetSnapshot> {
  const body: unknown = await api<unknown>("/api/v1/fleet", { signal });
  if (!body || typeof body !== "object") throw new Error("Malformed fleet response");
  const data = body as Partial<FleetSnapshot>;
  if (!Array.isArray(data.nodes) || data.nodes.length > 4096 ||
      !validCount(data.total_nodes) || !validCount(data.online_nodes) || !validCount(data.offline_nodes) ||
      data.online_nodes + data.offline_nodes !== data.total_nodes ||
      data.total_nodes !== data.nodes.length || !validTimestamp(data.as_of) || typeof data.as_of !== "string" ||
      !validRate(data.interface_rx_bits_per_second) || !validRate(data.interface_tx_bits_per_second) ||
      data.metric_scope !== "default_route_interface_not_billable" ||
      typeof data.physical_rates_complete !== "boolean" ||
      data.nodes.some(node => !validNode(node))) {
    throw new Error("Invalid or incomplete fleet data; previous values retained");
  }
  const ids = new Set(data.nodes.map(node => node.node_id));
  const online = data.nodes.filter(node => node.status === "online").length;
  if (ids.size !== data.nodes.length || online !== data.online_nodes) {
    throw new Error("Conflicting Node identities or fleet totals; previous values retained");
  }
  return data as FleetSnapshot;
}
