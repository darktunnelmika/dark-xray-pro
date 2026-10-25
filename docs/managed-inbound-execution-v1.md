# Managed inbound execution

This capability executes the complete authored inbound plan on explicitly
selected NEW PRO installations. It uses a separate runtime, receipt and
subscription namespace from the historical single-route LAB experiments.
The HUB is an explicit target; selecting a remote node does not also create
a HUB listener. Saving a plan remains a draft operation. Activation requires
the reviewed execution command for the desired clients and target snapshot.

## Protocol and transport coverage

The catalog contains VLESS, VMess, Trojan, Shadowsocks, WireGuard, Hysteria,
HTTP, Mixed, Tunnel, TUN, MTProto, AmneziaWG, TUIC and MASQUE. Compatible
generic transports are TCP, KCP, WebSocket, gRPC, HTTPUpgrade, XHTTP and
XDrive. Security validation and rendering use the typed TLS/REALITY Spec.
Protocol compatibility is validated before execution; QUIC/TUIC requires
TLS 1.3, for example. Individual client export formats are the intersection
of formats supported by every active route, so an export cannot silently
omit an incompatible route.

Native protocols use the separately pinned Xray v2 overlay. TUIC, AmneziaWG
and MTProto run in separately pinned standalone executables. The main HUB
and Agent do not link the standalone engines' dependencies. CI artifacts
include the exact corresponding source trees, overlays, provenance and
license notices, including modified QUIC sources.

TUIC and userspace AmneziaWG currently require direct egress. Routing a
component through an unsupported outbound is rejected rather than ignored.
AmneziaWG host-interface source-alias provisioning is not implemented by
the userspace worker; its authoring fields are explicitly rejected. Tunnel
IPv6 traffic does not require that optional host-alias feature. MTProto can
use an owned authenticated SOCKS bridge to a configured native outbound.

TUN requires a Linux TUN device and the execution service's network
administration permission. A normal hardened HUB with `PrivateDevices=yes`
and no capabilities cannot execute TUN. Authoring or a passing unprivileged
unit test does not establish this machine prerequisite. The privileged CI
acceptance runs inside disposable network, mount and PID namespaces and
changes only its owned synthetic route and interface. Deployment must
review the executor's device/capability boundary separately from the owner
browser service; never infer permission from a successful build.

## Admission and accounting

Each installation receives a complete immutable sibling configuration. The
bundle binds node, installation, generation, principal identities, native
configuration and external component manifests. Its delivery signature and
short lease are verified before process creation. Executables are opened
and hashed through retained file descriptors, so a path replacement cannot
change an admitted process. The process identity includes actual Linux
birth identities, executable hashes and the derived process configuration.

The native control service uses a Unix socket inside the private birth
directory. External controls use private Unix sockets too. A new process
starts with admission closed. The HUB must acknowledge the exact durable
zero usage report before the Agent obtains a fresh lease and opens
admission. A timer independently fences the process if its lease expires.

Changes replace the complete sibling configuration: the old cohort fences
ingress, closes owned work and joins counter-producing descendants before
sealing a nonce-bound final snapshot. The exact final report is durably
saved before delivery. The successor starts only after the previous final
receipt is acknowledged. Per-inbound principal identities prevent two
inbounds sharing a customer from double-counting each other's counters.

The encrypted Agent journal has one writer, atomic replacement and fsync.
Lost zero/final responses replay the same report and receipt hash. A disk
write failure kills admission and never sends a speculative undurable
checkpoint. A crash without a durable final report remains an explicitly
unknown accounting tail. It is never represented as a measured zero.

Stale epochs provisionally hold affected customer eligibility across nodes.
A validated final resolves only that epoch's provisional hold. Permanent
unknown tails require an owner-only reconciliation preview and command,
including a reason, sequence CAS, review hash and unique event ID. The
owner explicitly accepts the unmeasured tail as owner loss; all measured
usage remains charged and late measured reports remain acceptable.
An individually signed epoch-specific clearance allows the Agent to clear
only that uncertain journal after known pending reports are acknowledged.
Clearance itself grants no execution lease.

Authenticated protocols use per-client accounting. TUN, forwarding Tunnel,
and unauthenticated listeners use explicit owner aggregate accounting;
the system does not invent a customer identity for unattributed traffic.

## Owner workflow and subscriptions

The four existing detail tabs show desired and effective client bindings,
draft and applied revisions, per-target process/accounting state, and
reviewed activate, apply, restart, stop and archive actions. An unsent draft
cannot change the active route or block stopping its applied snapshot.
Ambiguous mutation responses retain the exact event and request for retry.

The client view issues, reveals, rotates and revokes managed subscription
links. Secret links are not persisted to browser storage and are hidden
after one minute or when the page becomes hidden. Public downloads require
current eligible bindings, an active applied snapshot, fresh accounting
evidence and a current lease on every projected route. Withdrawal removes
publication. Revoking a subscription link prevents future downloads; it
does not revoke credentials already imported by a client. Disabling that
client removes its credentials from the next reviewed runtime generation.

`engine_ready` proves the worker is running. It does not prove an external
consumer path. The UI and exported metadata distinguish these facts; no
unperformed connectivity check receives a verified status.

## Deployment contract

The initial database migration accepts schema 8 only and creates schema
16. Old production/schema 9–15 migration is outside this release. Older
application binaries must refuse schema 16. Take a consistent private
backup before migration. After any new traffic, rollback must preserve the
new measured ledger and journals; restoring an old database would discard
usage and is not an acceptable automatic rollback.

Both HUB and Agent require:

```text
DARK_PRO_ENABLE_INBOUND_EXECUTION=1
DARK_PRO_INBOUND_RUNTIME_DIR=/absolute/private/execution-directory
DARK_PRO_INBOUND_CORE_PATH=/absolute/release/xray-inbound-v2
DARK_PRO_INBOUND_CORE_SHA256=<approved artifact SHA-256>
DARK_PRO_INBOUND_ENGINES_JSON={"tuic":{"path":"...","sha256":"..."},"amneziawg":{"path":"...","sha256":"..."},"mtproto":{"path":"...","sha256":"..."}}
```

Keep the directory private and preserve it across restarts and releases.
Never overwrite a live journal to make a failed generation appear fresh.
The existing paired installation and private Agent token remain unchanged.
The dedicated TLS 1.3 Agent transport must remain separate from the owner
browser surface. Do not enable old runtime ownership for the same node.

Add `ops/nginx/inbound-execution.conf` to the existing owner HTTPS allowlist
before broad regex locations. Preserve existing unrelated denials; do not
replace the effective legacy route policy with another template. The
managed subscription prefix disables access/error logging, caching and
buffering, including malformed token suffixes.

The `inbound-execution.yml` workflow requires positive native traffic,
instrumented core, privileged TUN, standalone encrypted traffic, actual
compiler-to-Agent lifecycle and compiled-browser subscription acceptance.
An opt-in test that skips is not accepted as proof. Release provenance and
checksums must match the exact reviewed commit and installed executables.
No capacity or sustained-load benchmark is implied by these correctness
tests.
