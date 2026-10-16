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

export async function getFleet(signal: AbortSignal): Promise<FleetSnapshot> {
  const body: unknown = await api<unknown>("/api/v1/fleet", { signal });
  if (!body || typeof body !== "object") throw new Error("Malformed fleet response");
  const data = body as Partial<FleetSnapshot>;
  if (
    !Array.isArray(data.nodes) ||
    typeof data.online_nodes !== "number" ||
    typeof data.offline_nodes !== "number" ||
    typeof data.total_nodes !== "number" ||
    typeof data.interface_rx_bits_per_second !== "number" ||
    typeof data.interface_tx_bits_per_second !== "number" ||
    data.metric_scope !== "default_route_interface_not_billable" ||
    typeof data.physical_rates_complete !== "boolean" ||
    data.nodes.some(
      (node) =>
        !node ||
        typeof node.node_id !== "string" ||
        (node.status !== "online" && node.status !== "offline") ||
        typeof node.cpu_cores !== "number" ||
        typeof node.cpu_percent !== "number" ||
        typeof node.ram_total_bytes !== "number" ||
        typeof node.ram_used_bytes !== "number" ||
        typeof node.metrics_current !== "boolean" ||
        (node.rx_bits_per_second !== null && typeof node.rx_bits_per_second !== "number") ||
        (node.tx_bits_per_second !== null && typeof node.tx_bits_per_second !== "number"),
    )
  ) {
    throw new Error("Invalid or incomplete fleet data; previous values retained");
  }
  return data as FleetSnapshot;
}
