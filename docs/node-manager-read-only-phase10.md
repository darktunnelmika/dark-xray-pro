# Phase 10 — read-only Node Manager for the isolated PRO LAB

This phase turns the Nodes page into a real **read-only inspector** for
independently paired NEW LAB Nodes. It does **not** add a public pairing API,
Xray activation, production accounts or a second data-plane.

## UI behavior

- Reuses the cached `GET /api/v1/fleet` endpoint, its existing 5-second bounded
  poll and last-good snapshot on temporary failures.
- The operator explicitly selects a Node by its stable `node_id`, not by its
  row index. A selection survives a page refresh **within the same browser tab**
  and does not accidentally jump to a different Node if rows reorder.
- The inspector shows the authoritative heartbeat status and last-seen time,
  actual CPU/RAM and default-interface RX/TX **only when current**.
- Online with stale measurements remains **Online / stats delayed**. Offline
  and unknown measurements display a dash, never a manufactured 0.
- New LAB Node enrollment stays in the owner-only localhost API. The browser
  never sees the owner bearer token, Pair Code, Agent token or Node identity.
- The inspector explicitly labels live Xray Apply/Rollback unavailable; selecting
  a Node does not mutate HUB state or contact an Agent.
- Layout supports RTL, light/dark and 320px mobile viewports.

## Verification

`web/tests/overview.spec.ts` covers identity selection, refresh persistence,
offline measurements, delayed-but-online heartbeat, empty fleet private-enrollment
guidance, public admin API nonuse and 320px overflow. The existing Go/Race/Vet,
pinned-Xray and isolated installer CI remain unchanged.

## Next checkpoints (separate acceptance and pull requests)

1. Pair a new, otherwise unused test Node over independently pinned TLS 1.3 on
   the **Agent-only** listener. Do not reuse old DARK XRAY installations.
2. Owner-authenticated Node Manager operations require session-based RBAC/CSRF
   and an explicitly restricted reverse proxy, not copying a bearer into JS.
3. Real Xray Apply/Rollback requires a disposable first-inbound rehearsal,
   pinned binary, immutable previous runtime snapshot, process health AND
   authenticated VLESS/REALITY end-to-end evidence. Failed Apply must restore
   the previous running config or stop safely; it must never touch legacy services.
4. Inbound drafts must explicitly bind Direct/Tunnel/Dark Swap per NEW Node,
   distinguish ambiguous per-route traffic, and forbid live publication until
   owner approval and rollback proof.

**No production migration, route activation or subscription publication.**
