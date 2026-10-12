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
