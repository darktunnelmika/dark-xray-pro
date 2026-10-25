import type { NativePreflight } from "./NativeReadiness";
import type { InboundProfileOptions } from "./inboundProfileOptions";
import type { TLSMetadata } from "./inboundTLS";
import type { AuthoringSpec } from "./inboundAuthoringSpec";
import type { ExecutionState } from "./inboundExecution";

export type Inbound = {
  revision: number; enabled: boolean; archived: boolean; id: number; name: string;
  transport: string; grpc_service: string; reality_target: string; server_names: string[];
  short_ids: string[]; public_key: string; bound_clients: number;
  options?: InboundProfileOptions;
  security?: "reality" | "tls" | "none"; tls?: TLSMetadata;
  spec?: AuthoringSpec; spec_secret_paths?: string[];
};
export type InboundTarget = {
  inbound_id: number; node_id: string; installation_id: string;
  listen_ip: string; direct_port: number; swap_port: number;
};
export type InboundNode = { node_id: string; installation_id: string; name: string; kind?: "hub" | "node" };
export type InboundStatus = {
  node_id: string; installation_id: string; desired_sequence: number; desired_sha: string;
  reported_sequence: number; reported_sha: string; state: string; phase: string;
  reported_at?: string; lease_expires_at?: string; process_ready: boolean;
};
export type InboundOverview = {
  inbounds: Inbound[]; nodes: InboundNode[]; targets: InboundTarget[];
  runtime: InboundStatus[]; preflight?: NativePreflight;
  executions?: ExecutionState[];
  execution_enabled?: boolean;
};
