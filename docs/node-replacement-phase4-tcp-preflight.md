# DARK XRAY PRO — Phase 4: Durable new-Node TCP preflight

**Not an active replacement, not production-ready.** This milestone converts the earlier
operator-selected Direct/Tunnel preview into a durable and authenticated *preflight*
workflow for NEW, separately paired Node Agents. The original DARK XRAY HUB, old
Nodes, tunnels, subscriptions, customer usage and databases are not contacted.

## What works now

1. **Owner on localhost management port 2087** selects a retired PRO LAB Node, an
   independently paired new PRO Node, and an explicit set of per-Inbound Direct
   and/or Dark Swap destinations. Nothing is inherited from the old Node.
2. The Go HUB rebuilds the route plan using the existing strict routeplan
   validator. It stores a 0600 private operation journal with a random operation
   ID, pinned new installation ID, SHA-256 manifest, monotonic revision and
   explicit `pending_tcp_preflight` status. Writes use atomic replace + fsync.
3. The new Agent polls only its authenticated, installation-scoped pending task
   over the *existing TLS 1.3 pinned Agent-only listener*. This polling is an
   explicitly enabled LAB worker and never blocks the 3-second Pulse or the
   5-second host CPU/RAM/network stats.
4. The Agent checks selected TCP ports with strict time budgets:
   - Direct: `127.0.0.1:<node_port>` at the new Agent;
   - Swap: `<tunnel_address>:<tunnel_port>`, based solely on the 3 operator
     fields after selecting Inbound and new Node.
   TCP errors are reduced to a bounded set of codes; no shell commands execute.
5. Only the paired new Node may submit an exact set of expected endpoints,
   inbound IDs, route kinds and manifest/installation IDs. Duplicate identical
   reports are idempotent. A stale, forged, cancelled or conflicting report is
   rejected.
6. The owner can inspect the preserved evidence or cancel an operation with
   its expected revision. Cancellation does not stop, restart, delete or change
   Xray, client configs, an Inbound or another Node.
7. Successful TCP checks produce status **`tcp_preflight_passed_not_activated`**.
   The response **always** includes
   `can_activate=false`, `needs_xray_validation=true`,
   `needs_data_plane_probe=true`, `needs_subscription_commit=true`.
   Reachable TCP is not a validated VLESS/Reality tunnel or a working subscription.

## Local owner operations (development API, not consumer UI)

Authenticated with the temporary LAB owner bearer on the **loopback-only**
management port:

- `POST /api/v1/admin/replacements` — strictly validated
  `{old_node_id,new_node_id,deployments:[...]}`
- `GET /api/v1/admin/replacements` — durable roster and probe outcomes
- `GET /api/v1/admin/replacements/{id}` — one operation
- `POST /api/v1/admin/replacements/{id}/cancel` —
  `{expected_revision:<current_revision>}`

New Node Agent-only over separately verified TLS 1.3, per-Agent authentication:

- `GET /api/v1/agent/replacement-task?node_id=...&installation_id=...`
- `POST /api/v1/agent/replacement-report`

The lab Agent never polls replacement operations unless
`DARK_PRO_ENABLE_TCP_ROUTE_PROBES=1`. Remote TLS itself must also be explicitly
enabled. Legacy Node credentials cannot authenticate any of these routes.

## Remaining required functionality

This phase **does not generate or send a complete Xray config**, validate the
VLESS/Reality/TLS protocol, test end-to-end user data traffic, commit desired
Xray revisions, write customer subscriptions, or replace an actual host. No
activation endpoint exists deliberately.

Before a **real self-service replacement**, implement:

1. A complete typed Inbound/protocol profile with secret-handling rules and a
   deterministic Xray-core config renderer.
2. Staging on a freshly paired new Agent and a real `xray run -test` or
   equivalent pinned-binary validation; strictly preserve existing direct paths.
3. Apply with durable desired revision, rollback snapshot, verified health and
   real application-layer Direct/Tunnel probes, including multi-hop accounting.
4. A transactional subscription publication stage that changes only verified
   healthy endpoints, preserving tokens and historical per-user/representative
   accounting and offering reversible cutover.
5. Secure owner browser auth + CSRF, role-scoped UI wizard, proper WAN firewall,
   per-Node rate limits, TLS lifecycle and at least one actual new-node/VPS
   outage/cutover rehearsal.
6. Explicit retirement of old Node only *after* confirmed new route viability,
   with no inherited tunnels and no legacy credential import.

## Regression coverage

- Go durable journal restart, file permissions/symlinks, replay, manifest
  mismatch, wrong installation, overlapping operations and CAS cancellation.
- Real localhost TCP sockets for positive/negative bounded route probes.
- HTTP owner/Agent permission split, exact evidence, 404 on absent `activate`.
- Go end-to-end Agent pull + evidence POST under correct identity, and forbidden
  cross-Node credentials; all in a disposable test harness.
- Race detector / Vet / strict TypeScript build / real Chromium UI regression CI.

**The old HUB remains completely untouched. No PRO code was deployed to a VPS
by this commit.**
