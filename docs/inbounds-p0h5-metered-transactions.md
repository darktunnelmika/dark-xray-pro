# P0-H5: internal metered Stage / Apply / ACK / Rollback

P0-H4B proved final counters for the opt-in pinned VLESS/Reality direct-TCP
core. This increment connects that proof to a durable transaction interpreter.
It is an **internal loopback acceptance path**, not customer rollout. No HUB
flag, Agent flag, public route, installer or service enables it.

## Execution and authority

`OpenMeteredTransactions` requires `ExperimentalFinalTCP`, a new private
executor directory, a pinned core, a usage sender and a trusted
`MeteredTransactionAuthority`. This authority is an internal interface; only
acceptance fixtures implement it currently. A future HUB adapter must recheck
current source revisions, topology, bindings and quota eligibility, then
atomically recheck them when registering the actual new birth. A browser must
never supply registration mappings or implement this authority.

The interpreter uses v2 signed commands. Besides the runtime condition, each
command binds the exact epoch ID, a hash of its complete billing bindings and
process identity, the durable admission intent hash and any confirmed final
receipt. A fresh executor instance is mandatory. Ordinary periodic samples do
not invalidate an owner's review; drain intent, final proof and settlement do.

Stage freezes the private bytes and sidecars, checks the bounded TCP grammar,
runs the pinned core's offline validation, checks current authority, then
encrypts and fsyncs the staged revision. It does not stop or start a listener.

Apply and Rollback require fresh signed monotonic revisions and leases. They:

1. Check the reviewed runtime/epoch and current authority before disruption.
2. Fsync the exact pending command before touching the process.
3. Drain and settle the old active TCP epoch, or prove a never-opened epoch.
   This includes final counters, durable outbox proof, old process exit and
   exact authenticated HUB confirmation. Unknown tails stay fenced.
4. Check current authority again after final accounting, since the final bytes
   can exhaust quota or a concurrent owner change can revoke access.
5. Prepare a new API-only birth under the new lease, ask the trusted HUB adapter
   to register it, then wait for its durable zero receipt before AddInbound.
6. Fsync a signed historical ACK containing the exact prior settlement receipt
   and new epoch identity. A persistence failure closes the owned runtime.

Rollback chooses a previously acknowledged Stage, but never reuses its old
lease, process birth, counter baseline or billing epoch. Revoked users require
a new eligible candidate; an old successful Stage is not continuing authority.
Renewal checks fresh authority and extends only the healthy existing epoch.

## Failure and replay

A transition failure after durable intent stops the owned child and persists
`failed_stopped`; it never automatically restores a previous configuration.
Registration uncertainty, lost zero receipt, missing final proof or unknown
HUB tail cannot be treated as a successful replacement. A registration failure
before local epoch persistence remains conservatively fenced; reconciling that
registration ambiguity is still future work.

The exact last receipt can be retrieved after expiry/restart without invoking
authority callbacks, renewing a lease or starting a listener. Returned pointer
fields are copied so callers cannot mutate the retained historical evidence.
`ReplayMeteredUsage` reconciles durable usage proof only. After a lost final seal
response, it can confirm the old seal, but a replacement still needs a newly
reviewed Stage and fresh Apply. An interrupted pending command requires a new
Stage naming its exact hash. It is never automatically resumed.

The v2 encrypted transaction journal has distinct authenticated context.
The v1 empty-route executor refuses it. The existing HUB queue, its permanent
exclusive claims, poll verifier, receipt verifier and Agent sync remain v1;
metered status/receipts cannot enter that channel. There is no implicit channel
release, takeover of ordinary native state or public metered activation.

## Verification and remaining boundary

Mandatory nonroot Linux CI runs the real pinned custom core under the existing
Race-enabled PRO harness, three times per case. Disposable VLESS/Reality Vision
streams stay active through Stage, then are drained on replacement and rollback.
Tests compare exact cumulative HUB totals with both final counter snapshots,
check idle users remain zero and verify the old process is gone and sealed
before new registration. Other cases cover lost seal response and explicit
restaging, revocation after final accounting, a competing unknown tail,
registration failure and historical ACK replay after restart. Protocol tests
reject missing/foreign seals, reused epochs, missing zero receipts, unhealthy
observers and v1/v2 confusion. Encrypted reopen, returned-proof mutation,
downgrade refusal and interrupted-intent restaging are also tested.

CI also exposed an existing intermittent failure in interrupted-epoch recovery.
A local cancellation stress reproducer confirmed that the pinned SQLite driver's
query cancellation can strand a native statement and leave a pooled connection
permanently returning `SQLITE_INTERRUPT`. Discarding that connection alone was
insufficient: its unfinished native transaction could still hold a lock.

Ledger reads and writes now honor cancellation between fixed SQL statements.
An in-flight statement finishes without the driver's cancellation race, under
the verified SQLite busy timeout (positive, at most five seconds). Cancellation
is checked again before COMMIT, and rollback runs independently. A cancel during
COMMIT remains a normal lost-receipt case, handled by exact replay. This is a
bounded ledger workaround, not a change to byte totals, settlement rules or
the dependency version. Regression tests alternate canceled reads/writes and
fresh recovery writes, assert exact committed totals, and prove rollback on
cancellation before COMMIT. CI additionally repeats active interruption
recovery twenty times, with credential-free timing/status diagnostics on failure.

Still required: a reviewed durable HUB/Agent v2 transport, owner UI for metered
evidence, channel release/registration reconciliation, coordinated multi-node
Dark Swap and support/proof for other transports. The custom core is not part
of the LAB bundle. This increment does not activate USA Xray, change operational
nodes, access the old HUB or migrate real identities, credits, bots or data.
