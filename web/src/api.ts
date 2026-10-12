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
  rx_bits_per_second: number;
  tx_bits_per_second: number;
  last_seen: string;
}

export interface FleetSnapshot {
  as_of: string;
  total_nodes: number;
  online_nodes: number;
  offline_nodes: number;
  physical_rx_bits_per_second: number;
  physical_tx_bits_per_second: number;
  metric_scope: "physical_interface_not_billable";
  nodes: NodeSnapshot[];
}

export async function getFleet(signal: AbortSignal): Promise<FleetSnapshot> {
  const response = await fetch("/api/v1/fleet", {
    signal,
    credentials: "same-origin",
    cache: "no-store",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) throw new Error("Fleet service returned HTTP " + response.status);
  const body: unknown = await response.json();
  if (!body || typeof body !== "object") throw new Error("Malformed fleet response");
  const data = body as Partial<FleetSnapshot>;
  if (
    !Array.isArray(data.nodes) ||
    typeof data.online_nodes !== "number" ||
    typeof data.offline_nodes !== "number" ||
    typeof data.total_nodes !== "number" ||
    typeof data.physical_rx_bits_per_second !== "number" ||
    typeof data.physical_tx_bits_per_second !== "number" ||
    data.metric_scope !== "physical_interface_not_billable" ||
    data.nodes.some(
      (node) =>
        !node ||
        typeof node.node_id !== "string" ||
        (node.status !== "online" && node.status !== "offline") ||
        typeof node.cpu_cores !== "number" ||
        typeof node.cpu_percent !== "number" ||
        typeof node.ram_total_bytes !== "number" ||
        typeof node.ram_used_bytes !== "number",
    )
  ) {
    throw new Error("Invalid or incomplete fleet data; previous values retained");
  }
  return data as FleetSnapshot;
}
