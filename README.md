# DARK XRAY PRO

A clean-room Xray control-plane rewrite. **Work in progress — not production-ready.**

This repository is separate from the existing DARK XRAY HUB. Do not connect legacy Node Agents, copy production credentials, or change customer traffic routes while PRO is under development.

## Target architecture

- **HUB and Node Agent:** Go, with explicit contracts and isolated telemetry/accounting/control paths.
- **Web:** React + TypeScript; an original premium light/dark UI, no cyber-theme carryover.
- **Data:** exact once-only *billable* per-user traffic, separate physical node/link telemetry, verified source-IP enforcement and immutable audit history.
- **Node replacement:** self-service with fresh pairing, a route preview, validation and rollback; never silently inherit a retired tunnel.
- **Inbound editor:** distinct Nodes & Deployment area, with Direct/Tunnel usage and minimal three-field Dark Swap.
- **Backups:** streamed, encrypted, verifiable multi-gigabyte backup and selective historical migration; new Node identities only.
- **Out of scope:** WARP and ad blocking are entirely excluded from PRO: no installation, UI/API settings, optional later phase, or migration of those features. Old Node installation identities are not reused.
- **Telegram scope:** Existing bot/shop/Mini App functionality and the requested representative-bot admin/broadcast workflows remain part of the complete product. Development follows the Node/Inbound/runtime gates with separate synthetic/test identities; see the [recorded Telegram requirements](docs/telegram-representative-upgrade.fa.md).

## Current feature scope and status — 22 October 2026

The [source-linked feature audit](docs/release/feature-scope-and-status.fa.md)
is the current cross-module reference, audited at `c740c48` after [N17 / PR #96](https://github.com/darktunnelmika/dark-xray-pro/pull/96)
and extended by [N18 native LAB subscription delivery](docs/native-lab-subscriptions-n18.fa.md).
It distinguishes implemented source, bounded LAB acceptance and remaining
product work. Native customer/representative management, credit and usage
ledgers, Node enrollment/metrics, Inbound lifecycle and isolated metered
handover/health have substantial implementations. N17 verifies the complete
inbound set derived from the exact applied destination candidate in a bounded
loopback health round. N18 adds persistent revocable client tokens, immutable
CAS-fenced snapshots, raw/base64 VLESS delivery and scoped browser controls.
Every fetch rechecks the exact applied candidate and the original health/lease
deadline. Delivery is explicitly LAB-only, with at most the remaining eight-second
round window; public customer ingress and production publication are not proven.

Still open: public subscription delivery and atomic route publication; complete
Node lifecycle and real Tunnel/Dark Swap forwarding; verified IP/HWID and
first-connection timing; strict shared multi-Node quota and operational recovery;
Marketplace checkout and Telegram/Mini App; full Backup/Restore and Dark Restore;
release security, installer, load and disaster-recovery acceptance. WARP and ad
blocking are excluded requirements, not missing release features.

Source completion is separate from the installed LAB version. Development and
acceptance use synthetic/isolated data; operational legacy migration planning,
real-data import and cutover remain frozen until the full product is accepted
and the owner separately authorizes that phase. [Release gate #22](https://github.com/darktunnelmika/dark-xray-pro/issues/22).

## Safety rules

1. Keep the existing HUB, clients, representatives, subscriptions and database untouched.
2. Use entirely new Nodes for PRO; test with isolated or synthetic data only.
3. Explicitly distinguish `unavailable` from `empty`; preserve customer and representative history.
4. GitHub CI and real-browser, agent and disaster-recovery tests must pass before a public release.
5. No production cutover without a verified rehearsal and explicit operator approval.

See [docs/architecture.md](docs/architecture.md) for the versioned acceptance contracts after the foundation is committed.

## Historical implementation checkpoints

The notes below describe each increment when it was introduced. Statements such
as "not yet implemented" are historical boundaries, not the current feature
inventory. Use the dated audit above and its source references for current status.

### Phase 1 LAB Node identity

New LAB Nodes can be enrolled with a one-time pairing ticket and private per-installation credentials; the HUB keeps Node registrations across restarts. The telemetry API rejects mismatched Node IDs, installations, tokens and replayed samples. **This is a same-host development protocol, not WAN TLS or complete Node Manager.** See [isolated LAB quickstart](docs/lab-quickstart.md) and [identity guarantees](docs/node-enrollment-phase1.md). Public installation, production users and Node replacement are not ready.

### Phase 2: opt-in Agent-only HTTPS for NEW lab machines

The Node Agent may connect over verified TLS 1.3 with a manually pinned certificate and per-installation credential to a **separate** Agent listener. The owner dashboard remains local-only. Remote transport is disabled by default and must be explicitly enabled for isolated machines. See [Node WAN TLS LAB contract](docs/node-remote-tls-lab-phase2.md). This is **not a production-ready remote Node Manager**.

### Phase 3: safe Node replacement preflight (not live)

An operator-defined route plan can now be validated for a new Node, preserving Direct and Tunnel simultaneously and accepting the three-field Dark Swap. It refuses to mark routes activated before remote checks. See [Node replacement plan](docs/node-replacement-route-preflight.md). Actual replace/apply and subscription updates are not yet implemented.

### Phase 4: durable replacement TCP preflight (no activation)

The new HUB now stores replacement drafts and exact Agent-generated TCP reachability reports in a private durable journal. An explicitly opted-in *new* Agent polls only tasks for its paired installation, and the owner sees actual per-route evidence. **This is not Xray protocol validation, activation, or Subscription publication.** Read [Phase 4 boundary and API contract](docs/node-replacement-phase4-tcp-preflight.md).

### Phase 5: typed Xray REALITY configuration with offline validation

The new Go `internal/xrayconfig` package now renders deterministic, explicitly selected **VLESS+REALITY TCP/Vision or gRPC** candidates for fresh PRO Nodes. It preserves both Direct and Tunnel, enables native inbound/user counters, refuses duplicate Node ports, and explicitly marks shared-port route usage as **ambiguous** instead of inventing Direct/Tunnel totals. A pinned local Xray binary can run `run -test` against a temporary private config without any Xray restart or Subscription publication. Tests include 1000 synthetic clients. **Not an active config staging/deploy system and not production-ready.** [Phase 5 contract](docs/xray-render-phase5.md).

### Phase 6 — AES-256-GCM Xray candidate staging (LAB ONLY)

The private Reality candidate can now be stored encrypted on the NEW HUB and delivered only to the newly paired Agent over verified TLS 1.3. The Agent validates exact config and inbound tags/ports with a separately pinned local Xray executable, then returns a bounded, secret-free validation receipt. This is **offline-only**: it never writes to a running Xray, activates Inbounds, charges traffic or publishes subscriptions. Explicit enable flags and a persistent external staging encryption key are required. [Phase 6 instructions](docs/xray-stage-phase6.md).

### Phase 7 — real Xray lab-only loopback canary

After Phase 6's encrypted Reality staging and offline validation, a new unprivileged LAB Agent may additionally prove that a checksum-pinned **real Xray-core** process starts and binds *only ephemeral localhost listeners*. A disposable process is always stopped and private configs deleted before returning a separate `canary_loopback_bound` receipt. It **never** validates customer VLESS data-plane, applies a public Node config, changes users or publishes Subscription links. [Phase 7 runbook](docs/xray-canary-phase7.md).

### Phase 8 — private LAB revision selection + rollback journal

The new HUB can now hold multiple encrypted versions of an already Xray-syntax-verified, loopback-canary-verified Reality candidate. Owner-only API requests use compare-and-swap generation numbers to choose a **LAB rehearsal** version and reverse that choice without deleting the previous encrypted snapshot. AES-GCM protects confidential config and HMAC authenticates journal pointers, generations and audit events. **This is a reversible metadata/config selection, NOT live Xray apply or service rollback.** [Safety/runbook](docs/lab-revision-rollback-phase8.md).

### Phase 9 — isolated Linux amd64 LAB bundle (not a production installer)

GitHub Actions can now compile the Go HUB/Node Agent, the React/TypeScript frontend and a guarded install script into a checksum-verified **LAB-only** archive. The installer refuses to operate without the exact independently verified NEW VPS public IPv4 and an explicit acknowledgement, and refuses existing production/Xray services or existing LAB installation paths. It starts a separate non-root localhost:2087 HUB service; **no Xray-core binary, production Node, user data, tunnel or Subscription is included or changed**. [LAB release and installation safety](docs/lab-bundle-phase9.md).

### Phase 10 — live cached Node Manager inspection (LAB ONLY)

The Nodes page now supports explicit per-Node selection by stable identity,
refresh-safe inspection of real metrics, and correct Online/Offline/Stats-delayed
distinctions. It cannot pair Nodes in the browser, issue credentials, publish
Inbounds, or perform live Xray Apply/Rollback. See
[Phase 10 Node Manager boundary](docs/node-manager-read-only-phase10.md).

### Phase 11 — Direct + Dark Swap Inbound draft (LAB ONLY)

The Inbounds page now offers a browser-only, non-persistent preview for a
**new paired Node**, combining an independently optional Direct port and
Dark Swap's exactly three fields (tunnel port, Node port and tunnel host).
Shared-port ambiguity is explicitly disclosed. No Apply button, privileged
admin request or subscription publication is enabled. Read
[Phase 11 safety boundary](docs/inbounds-direct-swap-draft-phase11.md).

### Phase 12 — real isolated Xray process apply/rollback rehearsal (LAB ONLY)

An opt-in, non-root Go sandbox now validates checksum-pinned Xray candidates,
remaps every selected listener to ephemeral localhost ports and tests
**real process-level Apply, previous-process Rollback, automatic fallback on
process exit, and safe shutdown**. The previous process stays alive during
a candidate trial. This is entirely in-memory and never manages systemd,
public inbounds, customers or subscriptions. A live VLESS/REALITY handshake
and crash-safe production rollout remain unavailable. See
[Phase 12 controlled process runtime](docs/isolated-xray-sandbox-runtime-phase12.md).


### P0 business identity and role-scoped workspace — experimental (not migration-ready)

The new PRO-only SQLite business ledger now supports a forward-only identity
schema migration, local unprivileged first-owner bootstrap, unique representative
logins, salted Argon2id passwords, revocable secure cookie sessions and CSRF-
protected owner/representative business APIs. A real bilingual mobile-friendly
**Business** UI can create PRO-only representative accounts and pending clients
and inspect their actual credit/audit state. Representatives cannot read/write
other tenants, and Agent credentials cannot authenticate to the owner workspace.

The feature is **OFF by default** on the installed LAB. Its private database
does not read or migrate the legacy HUB. Active Xray subscriptions, Telegram
bots, 2FA and the full old-to-PRO migration are separate release blockers.
See [private owner and representative access](docs/owner-rep-auth-business-v1.md).

### P0 client groups — durable, scoped, non-destructive (PRO-only)

The new business workspace now includes persisted groups with representative ownership,
Unicode names/colors, transactional bulk membership transfer, true member counts and
safe group deletion that never deletes clients or credit/usage history. The SQLite
schema upgrades forward to version 3 without discarding existing PRO sessions,
accounts or financial events. [Native client group contract](docs/client-groups-v1.md).

### Native Client lifecycle — idempotent renewal and non-destructive archive

The owner and scoped representatives can now renew native PRO client duration,
top up volume, disable, logically re-enable and archive client records without
deleting UUIDs or historical balances. Renewals charge only the additional
cumulative user-month capacity, and use immutable request IDs to avoid double
charges. Re-enabling still marks the record **pending Xray Apply** rather than
pretending to activate a Node. Historical group membership and audit are
preserved. [Lifecycle v1 contract](docs/native-client-lifecycle-v1.md).

This is **not** subscription or Telegram production cutover functionality.

Legacy group migration, real Xray Apply, historical usage attribution, Telegram bots
and full production parity remain pending. No old service or customer is changed.
