# Phase 7 — real Xray on an isolated loopback canary (never active)

**DARK XRAY PRO LAB ONLY — no old HUB, no production Node, no
subscription switch, no Xray service restart.**

## Implemented

The new Agent can optionally extend a successfully received encrypted,
SHA-verified Reality config with a **real, independently pinned Xray-core
process test** in addition to the existing offline syntax check.

1. The raw candidate/config manifest must be the exact previously approved
   encrypted stage. All inbound tags, ports, transport types and REALITY
   settings are verified before runtime.
2. Xray runs `run -test -config` against the candidate with a trusted,
   checksum-verified executable. The private key never appears in output.
3. The candidate is remapped to **randomly reserved loopback-only ports**.
   Every listener's `listen` is changed from `0.0.0.0` to
   `127.0.0.1`; its port changes to a fresh ephemeral port distinct from
   the real Node's planned Direct/Tunnel listening ports.
4. A separate, private 0600 canary config is passed to
   `xray run -config <temporary-config>` with a restricted environment.
5. All remapped TCP listeners must actually accept a local socket
   connection before the deadline. The canary **and its process group are
   killed**, and the temporary config is deleted before returning proof.
   Successful TCP bind is **not** proof of a working VLESS/REALITY session.
6. HUB receives two separate values,
   `canary_attempted` and `canary_loopback_bound`, on the syntax
   validation receipt. Only paired Agent credentials may report them.
   Impossible/contradictory evidence is rejected. These values are retained
   across HUB restart in the sealed Stage journal.
7. **`can_activate=false`** and both data-plane/Subscription release
   gates stay required, regardless of canary result.

The canary never calls systemctl, signals an existing process, takes over
production ports, writes active Xray config or imports any legacy tunnel.

## Hard safety requirements

- Explicit **`DARK_PRO_LAB_MODE=1`** and
  **`DARK_PRO_ENABLE_LOOPBACK_CANARY=1`** on the new LAB Agent.
- The canary **refuses to execute as root**. Run the new test Node Agent
  under a private unprivileged system user with access only to its own
  identity and separately pinned Xray executable.
- Remote Agent access still requires verified/pinned TLS1.3 and opt-in
  encrypted staging from Phase 6.
- Never point the validator to a production Xray executable, instance
  configuration directory, user database or existing production Agent.
- If anything fails, record a negative canary result without changing
  already verified syntax status; do not silently mark a Node Offline or
  promote its new route.
- CI pins the official artifact
  `XTLS/Xray-core v26.9.9 / Xray-linux-64.zip` to SHA-256
  `1eb9175d0f0a8f8149c9230a7fc5ae66ce332ed20a53155ce61fe62e3f58b7df`.
  This is a **specific test release**, not an automatic production upgrade
  or statement that a prerelease is appropriate for customer traffic.

## Automated checks

The standard Go Race/Vet tests exercise process startup/exit, cancellation,
tag tampering, altered config hashes, real loopback socket binding and
non-activating proof. An **additional CI job** verifies the exact official
Xray release archive SHA before extracting the executable and running:

- `TestRealPinnedXrayCoreOptIn` — actual Xray Reality config syntax.
- `TestRealPinnedXrayCanaryOptIn` — actual Xray listener startup on
  ephemeral loopback ports, followed by forced cleanup.

CI runs on disposable GitHub Actions instances. **The new VPS
213.142.132.207 has not been verified by this test**, and there is no
live Direct/Tunnel hop or user subscription under test.

## Must be done before even considering live traffic

1. Reconnect the **new** VPS to an authorized controller without changing
   any old HUB/Node. Confirm IP and ownership.
2. Verify this behavior again on a dedicated new, unoccupied Node with a
   production-appropriate pinned official Xray release.
3. Build a durable desired-revision journal and a rollback mechanism which
   owns only new PRO lab-managed services; no uncontrolled systemd changes.
4. Test a real VLESS+REALITY application-layer handshake and Direct/Tunnel
   data-plane route. TCP bind alone is insufficient.
5. Confirm exact-once per-user billing and subscription update/rollback,
   then require explicit owner approval for cutover.

[Official Xray CLI reference](https://xtls.github.io/en/document/command.html)
and [official Xray-core releases](https://github.com/XTLS/Xray-core/releases).
