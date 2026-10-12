# Node identity and pairing — isolated Phase 1

This milestone removes the shared owner token from Node heartbeat authentication. The public repository contains **no** credential values. The code remains LAB ONLY with loopback networking.

## Invariants

1. The owner issues a fresh high-entropy 32-byte Pair Code, valid for 10 minutes, for a **new** logical Node ID. The HUB stores only SHA-256(code), the requested display name and deadline in a private 0600 registry.
2. The Node Agent generates its own 128-bit installation ID and 256-bit credential, writes them durably to a private 0600 identity **before** its first enrollment request, and sends the code and credential together only to localhost.
3. HUB atomically persists the binding (logical ID, installation ID, SHA-256 credential) and a receipt. An identical enrollment retry succeeds even if the original HTTP response was lost. Any different credential/installation is rejected; a duplicate Node ID is never silently reassigned.
4. Telemetry requires BOTH the correct per-Node bearer token and the pinned installation ID. Heartbeats carry sequence and boot ID; duplicates and stale sequences cannot refresh Online status. Agent-supplied display names cannot overwrite owner-approved names.
5. Registered Nodes survive HUB restarts in the roster. After restart they appear **offline with unknown metrics** until an authentic fresh heartbeat arrives. This is not a data-plane outage or config reapply.
6. Every read of the entire fleet is O(Nodes) and reads no SQLite traffic ledger. Default-route physical interface bitrates are **not** user-billable traffic and may include host unrelated traffic.
7. Failed filesystem writes do not claim successful new pairing. Pair Codes are not stored in plaintext on the HUB; the Agent holds the pending code only until its successful ACK.
8. Parent directory must be private (0700), credential/registry files must be regular 0600 files. Symlinks and world-readable files are refused rather than silently repaired.

## Intentionally not implemented

No remote Node enrollment, verified TLS/mTLS, public admin session, secure browser-based Pair wizard, self-service Node replacement, IP Limit, per-user billing, Direct/Tunnel measurement, durable telemetry time series, Xray controls, fleet shutdown policy, old Node import, DB migration, or backup execution. **Do not deploy this phase into customer production.**

Node replacement will have separate `prepare -> validate fresh routes -> explicit activate -> optionally retire old` transitions. A new Node never inherits an old tunnel merely because it reused a logical slot; the product must preserve historical accounting independently. Real multi-host pairing requires verified HTTPS, explicit origin/identity pinning and rollback-tested credential exchange, rather than weakening this local-only constraint.

## Acceptance tests

- Fresh enrollment / retransmission / expired or reused Pair Code / no unauthorized owner API.
- Replay-fenced heartbeat, owner token forbidden as Agent token, mismatched installation or logical ID refused.
- HUB restart preserves 0700/0600 private roster, but never invents online status or fake zero telemetry.
- Concurrent invitation conflicts, symlink/world-readable credentials rejected, no secret fields returned in fleet API.
- Go race/vet, Go HTTP integration tests, TypeScript production build, Chromium browser tests at 320px and 375px, RTL, Light/Dark, route persistence and stale API error.

## Liveness independent of telemetry (Phase 1b)

Authenticated lightweight `POST /api/v1/agent/pulse` runs at 3 seconds and can prove the Agent is Online even if Linux resource measurements fail or are delayed. `POST /api/v1/agent/heartbeat` remains the independent full 5-second metrics path. The API rejects old sequences and mismatched boot/installation identities; the HUB keeps the last registered node and marks outdated resource readings as unavailable. The Online/Offline state cannot be inferred from CPU pressure, tunnel traffic or an Xray failure. This is still a localhost-only lab and does not claim a remote WAN safety guarantee.
