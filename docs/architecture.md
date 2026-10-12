# DARK XRAY PRO — Architecture contract (foundation v0.1)

Status: **isolated laboratory prototype**. Source and tests may be public; production customer secrets must not be committed. The existing DARK XRAY HUB remains untouched.

## Technology

Go standard-library HUB and Node Agent, React + TypeScript + Vite UI, Linux telemetry. A future durable storage implementation will isolate current customer records, immutable billing ledgers and compact operational read models. This foundation deliberately introduces *no* live Xray/Node control actions and *no* user database.

The lab HUB binds **127.0.0.1:2087** only. It refuses to run without `DARK_PRO_LAB_MODE=1` and a random `DARK_PRO_LAB_TOKEN` of at least 32 bytes. The prototype Agent can only POST to that same-host lab endpoint. Never expose it as a public production management service. Real Node enrollment must use unique rotating credentials, verified TLS and replay protection; the shared lab token is not an acceptable public release mechanism.

## Domain boundaries

| Domain | Source of truth | Reads and safety |
|---|---|---|
| Physical fleet telemetry | Fresh verified Agent samples | O(nodes) in-memory snapshot, no DB scans |
| Billable per-user traffic | Future idempotent Xray charge ledger | **Not** the sum of physical RX/TX across multiple hops |
| Inbound and per-route traffic | Future uniquely tagged counters and routing graph | Distinct Direct/Tunnel, including transit duplication flags |
| Node desired config | Future revisioned transaction + signed ACK | No deletion upon temporary outage |
| Representative balance and history | Future immutable transactions and aggregate state | Unknown/API failure never rendered as empty/zero |
| Backup/migration | Future consistent encrypted snapshot and selective import | Old Node secrets/leases never imported into the new fleet |

**Telemetry contract:** Each sample includes node ID, per-process random boot ID, sequence number, logical CPU count, host CPU percentage, RAM total/used, default physical interface and cumulative RX/TX bytes. HUB receive time is authoritative for Online/Offline. New boot ID or decreasing network counters resets the *rate baseline*, not historical billed usage. Replayed/out-of-order sequences do not refresh heartbeat. Default offline deadline 15 seconds; a missed individual poll cannot toggle Online/Offline immediately. No "suspended" status or hiding Nodes.

**Explicit accuracy boundary:** The current lab dashboard reports only physical network interface bit rate and does not imply customer traffic charged. Per-client accounting, tunnel source-IP provenance and independently metered Direct/Tunnel totals are not implemented in foundation. Do not infer client IP from a GRE/relay's exit IP; mark unknown unless an authenticated trusted peer supplies verified original metadata. A public IP-limit feature is blocked until verified identity and topology-specific tests exist.

## Premium UI requirements

A single modern responsive visual system, not the legacy cyber/neon CSS. Theme: Light / Dark / System, persisted across refreshes. English / Persian (RTL) switch. Dashboard and Nodes display *only* authenticated/received samples, preserve last good data with a visible error on failure, and separate empty, loading and unreachable states. Five-second asynchronous polling never blocks route navigation; no fabricated representatives or counters.

Current foundation routes: Overview (live aggregate), Nodes (line monitor CPU/core count, RAM, physical RX/TX), Inbounds (truthful not-yet-built screen), Settings (working appearance/language choices). React is not yet authenticated for internet use: **local loopback lab only**.

## P0 implementation order

1. Durable schema and role authorization (owner/representative; no secondary owner model); replay-safe identity for per-node event streams.
2. New Node Manager: one-time pair, authenticated TLS, logical slot versus physical installation, secure rotation, stable health, diagnostics and safe replacement with **no legacy tunnel inheritance**.
3. Inbound deployment: independent Nodes & Deployment tab; Direct/Tunnel route-level counters; three-field Dark Swap (tunnel port, node port, tunnel address), validate actual reachability before displaying Ready.
4. Per-client volume, period/lifetime ledger and representative credits with transactional materialized aggregates and exact historical reconciliation.
5. Subscription updates with persistent token/URL, new route publication only after healthy new deployments. Preserve Restore and representative history, not legacy Node credentials.
6. Streaming encrypted multi-GiB backup and selective migration; historical customer data preserved, old Nodes excluded. Support manual SSH transfer from a consistent snapshot if full archival migration is unsuitable.
7. Browser and Go full-flow tests (replaced/offline Node, Direct/Tunnel billing and no-double-charge, authenticated representatives, mobile, slow API, backup corruption, rollback).
8. Fresh installation and on-new-Nodes load soak, then explicit user-approved cutover. No bot upgrades until core migration.

## Measurable acceptance goals (not current guarantees)

- Dashboard p95 initial screen **under 1.5s** with 750+ clients and a large historical ledger.
- Nodes p95 initial view **under 2s**, online/offline transition bounded by authentic heartbeat age, no connection-state flapping.
- Normal workspace navigation **under 1s** on lab reference hardware.
- New Node deployment and replacement without old Node pairing or manual server surgery.
- Billable user traffic reconciles exactly and is never summed across relay hops twice; attribution missing is explicitly unknown, not guessed.
- Full backup and restore proves archive hash, encryption, key inclusion, replay and source-state preservation for 1+ GiB snapshots.

No requirement is declared accepted merely because this document exists; tests and real authenticated QA must demonstrate it.
