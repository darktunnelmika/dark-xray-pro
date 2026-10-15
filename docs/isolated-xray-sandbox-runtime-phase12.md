# Phase 12 — real-process isolated Xray Apply / Rollback rehearsal

**This is an in-process, non-root, localhost-only Xray rehearsal, NOT a
production Apply engine and NOT an Agent command.** It never changes
`/etc/xray`, any systemd service, public Node listener, legacy installation,
subscription, customer configuration, database, user counters or billing.

## What is genuinely exercised

The new `internal/xraycanary.SandboxRuntime` can:
1. Reject root, non-LAB processes, and calls without the explicit
   `DARK_PRO_ENABLE_SANDBOX_APPLY=1` opt-in.
2. Validate the exact typed VLESS/REALITY candidate with a separately
   SHA-256-pinned Xray executable (`xray run -test`).
3. Re-map **every** proposed inbound to a fresh ephemeral TCP
   `127.0.0.1` port with no original public Node socket.
4. Start an actual isolated Xray subprocess in a private 0700 temporary
   directory with a 0600 candidate file. Verify bound localhost listeners,
   maintain one selected running process and one previous running process.
5. Apply a second candidate only after successful startup, retaining the
   previous process. Reject another rollout with pending rollback until a
   conscious `Rollback` or `Finalize`.
6. `Rollback`: reselect the original still-running process, kill only the
   sandbox-started unsuccessful process group, prevent repeated toggling.
7. Automatically fall back to the previous still-running sandbox process
   if the newly selected one exits; otherwise mark no process active.
8. `Finalize`: stop the previous sandbox process after the new one remains
   running. **This does not publish a subscription.**
9. `Close`: stop only process groups started by this runtime manager and
   delete private temporary configs. No `systemctl` or global `killall`.

Every returned snapshot is **metadata-only** and explicitly declares
`customer_data_plane_verified=false` and
`can_publish_subscriptions=false`. A bound port does **not** prove a VLESS
handshake, Reality SNI integrity, Direct/Tunnel reachability, correct traffic
accounting or a deployable Internet-facing Xray route.

## Tests

The regular Go race suite covers actual disposable listener processes with
synthetic Reality configs, manual rollback, repeated rollback rejection,
unavailable/wrong core pin, tampered candidate, caller cancellation, stale
process cleanup, safe finalization and root/non-LAB opt-in rejection.

The pinned-Xray GitHub Actions job additionally downloads and verifies its
existing exact Xray-core release, then runs
`TestRealPinnedXraySandboxApplyRollbackOptIn`. It must successfully execute
**two real concurrently running Xray processes**, then rollback with the
previous real loopback listeners still accepting connections.

Nothing in this change enables a user-facing Apply button, public Node
listener, Agent remote apply command, subscription operation or
automatically deployed service.

## Production acceptance still blocked

- No independent new remote PRO Agent installation has been authorized and
  tested across pinned TLS.
- Runtime selection is **in-memory**, not a crash-safe desired/observed
  revision journal with encrypted at-rest recovery and signed Node ACKs.
- No real client handshake, Tunnel/Dark Swap path proof, route-level billable
  traffic attribution, durable per-user accounting or load/pressure tests.
- No browser owner session/RBAC and CSRF-safe rollout approval.
- No live fail-closed cutover design for customer subscriptions or a durable
  recovery plan for an interrupted process replacement.

Never treat Phase 12 as permission to replace Xray on either the legacy HUB
or an existing production Node. It is only a proof that a new real Xray
process can be started, selected, rejected or rolled back **in a disposable
loopback lab**.
