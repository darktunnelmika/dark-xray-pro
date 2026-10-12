# DARK XRAY PRO — independent Node heartbeat contract

Phase 1b isolates Agent liveness from CPU/RAM/network collection. It is an **isolated localhost-only prototype**.

## Two independent paths

| Path | Expected cadence | Reads | UI behavior |
|---|---:|---|---|
| `POST /api/v1/agent/pulse` | 3s | immutable identity + sequence only | Online until 15s after final valid receipt |
| `POST /api/v1/agent/heartbeat` | 5s | Linux CPU, memory, default-route interface counters | Resource/traffic values current for 12s |
| `GET /api/v1/fleet` | 5s browser polling | Go in-memory snapshot | O(Nodes), no per-user ledger/SQL or Node WAN query |

Both writes require the same individually paired Agent identity and bearer credential; the owner token does not authenticate a Node. Node boot IDs and sequence numbers reject duplicates and stale boot replays in the running HUB process. The HUB receives and timestamps every accepted message; the Agent clock is not authoritative.

If CPU collection fails repeatedly while Pulse succeeds, the Node stays **Online**; CPU percentage, RAM in use and RX/TX rates become **unknown**, and the visible "Stats delayed" label appears. When both stop for more than 15s, the registered Node remains visible as **Offline** and shows no fake live rates.

## Testing

- Pure Go state-machine tests for pulse-only Node, duplicate after elapsed deadline, fresh recovery, retired process boots and changing interfaces.
- Real authenticated HTTP tests for bad credential and fresh pulse, plus agent-to-HUB enrollment and pulse end-to-end.
- Playwright real Chromium: viewport 320/375, RTL after refresh, light/dark, stale-but-online metrics, no fake 0 Mbps and no disappearance after API error.

## Not production features yet

The LAB Node Agent **cannot contact remote servers**. Production mTLS, durable heartbeat replay boundaries across HUB restarts, lease expiry, forced fail-close policy, rate limiting, replacement FSM, time-series retention and Direct/Tunnel usage attribution are future work. This milestone must not be installed on the live legacy HUB.
