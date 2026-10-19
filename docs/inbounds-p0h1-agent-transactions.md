# P0-H1: durable staged execution in the native Agent runtime

This increment implements the **internal execution engine**, not a public
Apply button or a completed distributed route rollout. It uses the existing
owned-process supervisor to change actual Xray listeners. It is restricted
to a separate private directory and loopback addresses. No installed Agent
worker, HUB HTTP route, browser action or deployment flag enables it yet.

## Implemented behavior

- `OpenTransactions(Options)` owns an independent native runtime. Root,
  public listening addresses, unsafe directories and ordinary native runtime
  roots containing `runtime.enc` are refused. The existing metering path is
  untouched; usage-admission candidates cannot enter this unmetered LAB engine.
- `SignTransactionCommand` authenticates the action, monotonic command serial,
  stage identity, exact Node/installation/binary, desired runtime revision,
  reviewed runtime condition, candidate payload digest, deadline and lease.
  Candidate hashing includes the listener sidecar as well as private bytes.
- Stage verifies the pinned core using real offline syntax validation and
  fsyncs an AES-GCM-encrypted candidate. It does not start or replace a child.
- Apply requires the same staged candidate, an unchanged reviewed base,
  an unexpired Stage and a fresh installation-bound runtime lease. The base
  is checked under the actual supervisor lock, including after validation.
  The encrypted command intent is durable **before** any process change.
- Rollback uses a fresh, higher runtime revision and a new lease. It restores
  the last known good candidate after a stopped failed apply, or the previous
  candidate after a successful apply. Existing revocation behavior is kept:
  a failed restrictive change cannot automatically reopen a revoked UUID or
  endpoint. Restoring it requires a new explicit signed rollback command.
- A complete result is signed with a different HMAC domain and fsynced before
  it is returned. The signature covers the entire observed state, outcome,
  command hash and executor instance. A lease signature by itself is not an ACK.
- Duplicate delivery returns the identical historical receipt; it does not
  restart the process or renew its lease. Renewal is a separate signed action.
  An expired lease still stops only the owned child.
- A crash between process change and durable receipt leaves an interrupted
  command. Restart opens no listener. Recovery requires a fresh Stage that
  explicitly names the interrupted command hash and the newly observed base.
  A failure to persist a receipt fences the executor and stops its child.

Stage, current and previous private candidates are stored in one bounded,
authenticated journal. Command serials and runtime revisions are independent.
The existing native runtime journal remains unchanged and separately owns
its process lock, last-good state and short lease.

## Evidence

Unprivileged fixture tests cover actual TCP ownership, staging without
activation, signed ACK tampering, idempotent lost-ACK replay, restart,
concurrent conflicting Stage commands, expired leases, corrupt ciphertext,
stale review conditions, wrong identities/binary, forbidden metering admission,
automatic last-good reconciliation, restrictive failures, explicit rollback,
and SIGKILL after Apply but before receipt persistence.

`TestRealPinnedRouteTransactionsRealityRollback` shares the original native
REALITY acceptance scenario: Stage/Apply on stock pinned Xray, an actual
disposable VLESS/REALITY client carrying a random local HTTP payload, removal
of that UUID, refusal of its traffic, and explicit rollback restoring it.
The CI runs this under Race and rejects a missing or skipped test. All TLS
targets, traffic origins, client sockets and server sockets are local test
fixtures; no production Node or existing customer is contacted.

## Remaining integration gates

1. Connect immutable owner-reviewed Native route drafts and whole-Node
   configuration snapshots to a durable HUB command queue.
2. Deliver through the paired, certificate-pinned Agent channel; commit and
   verify the exact signed receipt in the HUB before advancing workflow state.
3. Integrate the executor into the Agent worker, with mutually exclusive
   ownership versus ordinary native sync, then expose truthful owner controls.
4. Implement safe metered-epoch settlement and metered transitions. This engine
   must not be used as a workaround for an unsettled usage epoch.
5. Coordinate independent Dark Swap hops, prove real forwarding and exact
   destination-only billing, and validate rollback across partial failures.

An ACK is a historical observation by the trusted paired Agent. It is not
current liveness, a customer data-plane certificate, billing acceptance or
proof against a compromised Node administrator. Both data-plane and
subscription-publication fields remain false. Existing P0-G route readiness
continues to report its unresolved gates honestly.

No legacy HUB access, customer/representative/credit/bot migration, USA Xray
activation or live deployment is part of this increment.
