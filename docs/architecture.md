# DARK XRAY PRO — Architecture contract (foundation v0.1)

Status: **isolated laboratory prototype**. Source and tests may be public; production customer secrets must not be committed. The existing DARK XRAY HUB remains untouched.

**Historical foundation contract:** implementation statements below describe the
foundation and its early phases. Later modules supersede several "future" and
"not implemented" statements. The acceptance goals remain requirements; use the
[22 October 2026 source audit](release/feature-scope-and-status.fa.md) for current
feature status. WARP and ad blocking are entirely excluded from PRO. Existing
Telegram parity and the recorded representative-bot upgrades remain in scope.

The current [N18 subscription contract](native-lab-subscriptions-n18.fa.md) adds
revocable client tokens and immutable, generation/revision-fenced LAB snapshots.
Consumer reads revalidate the complete N17 evidence, applied candidate and fixed
deadline. This opt-in schema13 path leaves production publication and public
endpoint/lease lifecycle gates open; normal databases remain schema8.

[N19 consumer endpoint manifests](consumer-endpoint-manifests-n19.fa.md) adds an
explicit schema14 path with immutable complete mappings, per-inbound startup
approval, a distinct signed v3 probe and exact consumer-address URI output.
Replacement, revocation and approval withdrawal invalidate old evidence. The
probe originates on the Node; independent Internet reachability and production
publication remain separate acceptance gates. Default storage remains schema8.

## Technology

Go standard-library HUB and Node Agent, React + TypeScript + Vite UI, Linux telemetry. A future durable storage implementation will isolate current customer records, immutable billing ledgers and compact operational read models. This foundation deliberately introduces *no* live Xray/Node control actions and *no* user database.

The lab HUB binds **127.0.0.1:2087** only. It refuses to run without `DARK_PRO_LAB_MODE=1` and a random `DARK_PRO_LAB_TOKEN` of at least 32 bytes. The prototype Agent can only POST to that same-host lab endpoint. Never expose it as a public production management service. Real Node enrollment must use unique rotating credentials, verified TLS and replay protection; the shared lab token is not an acceptable public release mechanism.

## Domain boundaries

| Domain | Source of truth | Reads and safety |
|---|---|---|
| Network interface telemetry | Fresh verified Agent samples | O(nodes) in-memory snapshot, no DB scans |
| Billable per-user traffic | Future idempotent Xray charge ledger | **Not** the sum of physical RX/TX across multiple hops |
| Inbound and per-route traffic | Future uniquely tagged counters and routing graph | Distinct Direct/Tunnel, including transit duplication flags |
| Node desired config | Future revisioned transaction + signed ACK | No deletion upon temporary outage |
| Representative balance and history | Future immutable transactions and aggregate state | Unknown/API failure never rendered as empty/zero |
| Backup/migration | Future consistent encrypted snapshot and selective import | Old Node secrets/leases never imported into the new fleet |

**Telemetry contract:** Each sample includes node ID, per-process random boot ID, sequence number, logical CPU count, host CPU percentage, RAM total/used, default default-route interface and cumulative RX/TX bytes. HUB receive time is authoritative for Online/Offline. New boot ID or decreasing network counters resets the *rate baseline*, not historical billed usage. Replayed/out-of-order sequences do not refresh heartbeat. Default offline deadline 15 seconds; a missed individual poll cannot toggle Online/Offline immediately. No "suspended" status or hiding Nodes.

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
8. Fresh installation and on-new-Nodes load soak, then separately authorized cutover after full-product acceptance. Existing Telegram parity and the [recorded representative-bot upgrades](telegram-representative-upgrade.fa.md) are developed with test identities after the Node/Inbound gates; they are not deferred past final migration.

## Measurable acceptance goals (not current guarantees)

- Dashboard p95 initial screen **under 1.5s** with 750+ clients and a large historical ledger.
- Nodes p95 initial view **under 2s**, online/offline transition bounded by authentic heartbeat age, no connection-state flapping.
- Normal workspace navigation **under 1s** on lab reference hardware.
- New Node deployment and replacement without old Node pairing or manual server surgery.
- Billable user traffic reconciles exactly and is never summed across relay hops twice; attribution missing is explicitly unknown, not guessed.
- Full backup and restore proves archive hash, encryption, key inclusion, replay and source-state preservation for 1+ GiB snapshots.

No requirement is declared accepted merely because this document exists; tests and real authenticated QA must demonstrate it.

## Phase 1 pairing contract

The first durably registered LAB Nodes now use owner-issued, 10-minute one-time invites and self-generated per-installation Agent credentials. See [Node identity Phase 1](node-enrollment-phase1.md). This is **not** permission to run remote/internet Node pairing. The Node Manager replacement and production mTLS workflows remain blocked. Old node credentials and tunnels are never imported.

## Independent Node health sampling (Phase 1b)

A dedicated 3-second authenticated Pulse carries logical Node ID, pinned installation, random process boot ID and sequence. This request never calls CPU, RAM, Xray or network counters. A separate 5-second metrics loop can fail without removing a healthy Agent from Online. A Node becomes Offline after more than 15 seconds without a valid pulse **or** telemetry sample. Metrics older than 12 seconds render as unavailable with 'Stats delayed', not as current 0%. Replayed pulses and retired boot IDs do not extend Online; Node stays visible when offline. The last known CPU core count may remain displayed for identification, not as fresh telemetry. 

These protections are in-memory lab semantics. Durable WAN Node health, clock rollback handling, remote mTLS, signed leases and broader soak tests remain required before any real production Node deploy. The reported RX/TX values are for a default-route interface and are NOT unique billable per-user traffic or individually attributed Inbound Direct/Tunnel traffic.

## Phase 2 remote Agent transport boundary

The LAB HUB can optionally bind a distinct Agent-only TLS1.3 listener (no owner API or UI). The Node Agent refuses remote HTTP, requires normal chain+IP/DNS-SAN certificate verification plus a SHA256 leaf pin, and never follows redirects. The operator must explicitly enable remote Agent mode; default remains localhost-only. This feature has no Xray data-plane, production lease, IP-limit or Node replacement, and is not public-release-ready. See [Node WAN TLS LAB phase 2](node-remote-tls-lab-phase2.md).

## Phase 3 operator-approved replacement route draft

Explicit Direct/Swap choices and deterministic nonactivating SHA-256 previews live in Go `internal/routeplan`, with no previous tunnel inheritance. This foundation may be inspected in [Route preflight](node-replacement-route-preflight.md). Persistent replace orchestration, remote probes, Xray apply and subscription publication remain release blockers.

## Phase 4 replacement command integrity

Side-effect-free `routeplan` choices now have a persistent, revisioned journal, scoped pull and signed-per-identity (bearer authenticated) TCP evidence from the freshly paired Node Agent. The Admin API cannot activate Xray or publish subscriptions. A TCP listener alone does not validate Xray, tunnel forwarding or customer routes. See [Phase 4](node-replacement-phase4-tcp-preflight.md) for security and activation blockers.

## Phase 5 typed Xray rendering and usage scope

`internal/xrayconfig` accepts only an untampered, explicit `routeplan.Preview` with a matching profile for each selected Inbound. It renders VLESS TCP+REALITY Vision or gRPC+REALITY in a deterministic, private candidate, independently tags Direct/Tunnel when on different Node ports, and reports shared-port route attribution as **ambiguous**, not billed. User/Inbound counters are enabled but are NOT automatically an exact-once multi-hop accounting engine. Xray's actual `run -test` is opt-in with a verified locally pinned binary, private temporary file, bounded execution and no service impact. Runtime Xray apply, direct/tunnel protocol proof, durable desired revision and subscription publication remain blocked. [Details](xray-render-phase5.md).

## Phase 6 encrypted stage and Xray offline proof

Replacement status `tcp_preflight_passed_not_activated` may feed typed Reality config candidates into `internal/configstage`: a private 0600 AES-256-GCM encrypted journal. Only an owner on SSH-forwarded localhost can submit Reality profiles; no private config appears in owner API JSON. A paired, installation-scoped Agent over pinned TLS1.3 receives the config, verifies SHA and listener tags, and runs the independently pinned Xray binary only with `run -test -config` against temporary 0600 JSON. Success records `offline_syntax_validated_not_active`, never `can_activate=true`. Real protocol/data-plane proof, active Xray config changes, app user billing and Subscription publication remain unavailable. [Phase 6 contract](xray-stage-phase6.md).

## Phase 7 isolated core canary

`internal/xraycanary` remaps all explicitly validated Xray listener tags to new loopback-only ephemeral ports. A root user, missing LAB flag or missing explicit canary opt-in is refused. The pinned real Xray executable first passes offline syntax validation, then starts on the remapped ports for a short listener smoke test; the subprocess group is forcibly terminated afterward. Stage receipts separately preserve canary attempt/bind flags while retaining `can_activate=false` and outstanding data-plane/Subscription verification gates. CI checks a fixed official release archive digest before running real Xray inside disposable GitHub Actions runners. [Details](xray-canary-phase7.md).

## Phase 8 — prepare/rollback selection, never active Apply

After Stage metadata reports syntax verified and stopped loopback Xray canary passed, `internal/labrevision` keeps encrypted private candidate versions and a fully HMAC-authenticated, CAS-fenced selection/rollback journal. Only the SSH-tunneled owner API may prepare and change LAB selections. Multi-node/multi-instance cross-selection is prohibited. The current phase has **no** active Xray process controller, systemd touch, data-plane verification or Subscription publish; these are explicit blockers. [Phase 8](lab-revision-rollback-phase8.md).

## Phase 9 lab delivery boundary

The source-only Go/React application is now packaged as a checksum-verified Linux amd64 LAB release artifact by GitHub Actions. A guarded installer requires exact NEW VPS public IPv4 confirmation and an explicit empty-server acknowledgement before installing a separate non-root localhost-only HUB service. It never installs/updates old Xray or Nodes and never turns on public owner management. This is NOT a production installer or proof the new VPS is already accessible. [Phase 9 lab packaging](lab-bundle-phase9.md).
