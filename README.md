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
- **Out of scope:** WARP, ad blocking, reuse of old Nodes and Telegram-bot feature upgrades before core migration.

## Safety rules

1. Keep the existing HUB, clients, representatives, subscriptions and database untouched.
2. Use entirely new Nodes for PRO; test with isolated or synthetic data only.
3. Explicitly distinguish `unavailable` from `empty`; preserve customer and representative history.
4. GitHub CI and real-browser, agent and disaster-recovery tests must pass before a public release.
5. No production cutover without a verified rehearsal and explicit operator approval.

See [docs/architecture.md](docs/architecture.md) for the versioned acceptance contracts after the foundation is committed.

## Current implementation checkpoint: Phase 1 LAB Node identity

New LAB Nodes can be enrolled with a one-time pairing ticket and private per-installation credentials; the HUB keeps Node registrations across restarts. The telemetry API rejects mismatched Node IDs, installations, tokens and replayed samples. **This is a same-host development protocol, not WAN TLS or complete Node Manager.** See [isolated LAB quickstart](docs/lab-quickstart.md) and [identity guarantees](docs/node-enrollment-phase1.md). Public installation, production users and Node replacement are not ready.

## Phase 2: opt-in Agent-only HTTPS for NEW lab machines

The Node Agent may connect over verified TLS 1.3 with a manually pinned certificate and per-installation credential to a **separate** Agent listener. The owner dashboard remains local-only. Remote transport is disabled by default and must be explicitly enabled for isolated machines. See [Node WAN TLS LAB contract](docs/node-remote-tls-lab-phase2.md). This is **not a production-ready remote Node Manager**.

## Phase 3: safe Node replacement preflight (not live)

An operator-defined route plan can now be validated for a new Node, preserving Direct and Tunnel simultaneously and accepting the three-field Dark Swap. It refuses to mark routes activated before remote checks. See [Node replacement plan](docs/node-replacement-route-preflight.md). Actual replace/apply and subscription updates are not yet implemented.

## Phase 4: durable replacement TCP preflight (no activation)

The new HUB now stores replacement drafts and exact Agent-generated TCP reachability reports in a private durable journal. An explicitly opted-in *new* Agent polls only tasks for its paired installation, and the owner sees actual per-route evidence. **This is not Xray protocol validation, activation, or Subscription publication.** Read [Phase 4 boundary and API contract](docs/node-replacement-phase4-tcp-preflight.md).

## Phase 5: typed Xray REALITY configuration with offline validation

The new Go `internal/xrayconfig` package now renders deterministic, explicitly selected **VLESS+REALITY TCP/Vision or gRPC** candidates for fresh PRO Nodes. It preserves both Direct and Tunnel, enables native inbound/user counters, refuses duplicate Node ports, and explicitly marks shared-port route usage as **ambiguous** instead of inventing Direct/Tunnel totals. A pinned local Xray binary can run `run -test` against a temporary private config without any Xray restart or Subscription publication. Tests include 1000 synthetic clients. **Not an active config staging/deploy system and not production-ready.** [Phase 5 contract](docs/xray-render-phase5.md).

## Phase 6 — AES-256-GCM Xray candidate staging (LAB ONLY)

The private Reality candidate can now be stored encrypted on the NEW HUB and delivered only to the newly paired Agent over verified TLS 1.3. The Agent validates exact config and inbound tags/ports with a separately pinned local Xray executable, then returns a bounded, secret-free validation receipt. This is **offline-only**: it never writes to a running Xray, activates Inbounds, charges traffic or publishes subscriptions. Explicit enable flags and a persistent external staging encryption key are required. [Phase 6 instructions](docs/xray-stage-phase6.md).
