# Phase 5 — typed Xray candidate and offline validation (not a deployment)

DARK XRAY PRO now has a strict, private **nonactivating** Go renderer for selected
new-Node Inbounds. This work is based on the official XTLS/Xray-core VLESS TCP +
REALITY Vision and gRPC + REALITY examples:

- https://github.com/XTLS/Xray-examples/tree/main/VLESS-TCP-XTLS-Vision-REALITY
- https://github.com/XTLS/Xray-examples/tree/main/VLESS-gRPC-REALITY
- https://xtls.github.io/en/config/stats.html
- https://xtls.github.io/en/config/policy.html

## Model

An operator selects an already validated `routeplan.Preview` with explicit new-Node
Direct and/or 3-field Dark Swap, and separately provides a typed profile for
each selected Inbound:

- Transport: `tcp` with Reality + Vision, or `grpc` with Reality and a
  service name, without inappropriate Vision flow;
- Reality server parameters: target, SNI list, X25519 private key and short IDs;
- User identity: explicit UUID + an Xray email-tag, so standard per-user
  counters can be collected.

The renderer checks the route-plan SHA-256 against the canonical Direct/Swap
selection. A forged, modified or prematurely activated plan is rejected. It
rejects duplicate listeners on the same new-Node port, empty/bad UUIDs, bogus
private keys, malformed ports/targets/SNIs and conflicting profiles. Input
order is normalized to produce a deterministic config digest.

**No old Node credentials, tunnels, inbounds or client accounts are fetched**.
Only explicitly supplied new-Node users and routes are rendered.

## Traffic correctness

The output enables native Xray per-user and per-inbound uplink/downlink
statistics and gives distinct listener tags **when ports differ**:

- `dxp-in-<id>-direct`: Direct ingress listener;
- `dxp-in-<id>-swap`: tunnel ingress listener, if it has a different Node port;
- `dxp-in-<id>-shared`: a SINGLE listener if Direct and Swap use the SAME
  new-Node port.

For a shared port, attribution is explicitly
`ambiguous_shared_port`; we do **not** invent separate Direct/Tunnel traffic
figures. Also, even distinct inbound counters may double-count one physical
customer stream in multi-hop topologies. **They are for physical/path analytics,
not an automatic per-user billable ledger**. Billing boundary and verified
source-IP handling remain tracked in Issue #2.

Dark Swap's external tunnel address/port are used for connectivity/probe
metadata, **not** added as extra Xray inbound listeners. The actual listening
port on the selected new Node is the operator's Node Port.

## Offline Xray-core test (opt-in, no service changes)

`xrayconfig.ValidateOffline(ctx,candidate,absoluteXrayBinary,expectedSHA256)`
will:

1. Check the candidate's private JSON digest and non-activation gates.
2. Require a regular executable Xray file, not a symlink, with an exact
   SHA-256 hash supplied by the operator from a separately trusted source.
3. Write a `0600` temporary config inside a private temporary directory.
4. Execute ONLY `<pinned Xray> run -test -config <temporary file>`
   with a maximum eight-second timeout (or caller's shorter deadline).
5. Discard the full Xray stdout/stderr so private Reality credentials cannot
   leak into admin logs or GitHub CI.
6. Delete the temporary file/directory and return metadata showing
   `xray_syntax_validated=true` but **`can_activate=false`** and remaining
   data-plane/Subscription requirements.

The default CI tests the command execution and negative controls using
**synthetic Go/script fixtures**, not a downloaded Xray binary. Therefore it
does **not** yet prove that every rendered configuration is accepted by
the exact Xray-core release on a live new Node. That proof is required before
staging/deployment, together with an application-layer REALITY handshake and
route-specific accounting verification. `run -test` itself does not prove that
a listener can actually bind or forward client traffic.

## Remaining release gates

- Provision a new LAB Node and a separately trusted, checksummed Xray binary.
- Validate generated config with the real core and version-pinned integration
  test, with neither previous Node nor existing production routes involved.
- Introduce an owner-authenticated, CSRF-protected config-staging workflow
  that never leaks Reality keys to a public API or writes to a live config.
- Implement durable desired-revision ACK from Agent, safe staged Xray
  apply, health checks and rollback. Confirm Direct remains while Tunnel is
  being changed.
- Add real protocol and data-plane probes and correct source-IP / multi-node
  usage accounting. Publish subscriptions ONLY after verified healthy
  deployments and explicit owner confirmation.

**Current status:** source-level schema generation and a safe opt-in offline
validator. No Xray process, systemd unit, Node deployment, user record, old HUB,
or Subscription is modified by this milestone.
