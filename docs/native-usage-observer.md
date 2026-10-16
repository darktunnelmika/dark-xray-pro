# Supervised continuous usage observation and interrupted-epoch recovery

This increment builds on PR #31 in the actual PRO module. It does not deploy an
installed service, import customers, alter the old HUB, or enable production
metered runtime admission. The existing `metered_runtime_admission_pending`
response remains mandatory.

## Runtime integration

`Runtime.StartUsageObservation` attaches one autonomous collector to an already
supervised and leased Xray child and a HUB-registered epoch. It checks the exact
node, installation, configuration hash, kernel boot, process birth, configured
StatsService endpoint and complete set of native user counter names. It never
selects billability from a usage report or treats missing counters as zero.

An encrypted version-2 runtime intent is persisted before the encrypted outbox
is created. The outbox key is domain-separated from the runtime state key.
Version-1 journals without observation retain existing behavior. An old reader
which understands only journal version 1 refuses a version-2 observed runtime;
removing a flag or restarting the Agent must not drop the metering obligation.

The worker periodically calls the real, non-resetting collector, checkpoints the
cumulative snapshot, and sends exact receipts over the existing CA-verified,
leaf-pinned TLS 1.3 transport. A bounded send pass can drain an older backlog;
it does not manufacture a new sample for unchanged counters. A separate guard
bounds both the time since the last durable observation and continuous pending
backlog age. It does not take the Runtime or outbox mutex and remains effective
when sampling, HTTPS or state persistence is stalled. Failure stops only the
captured child object, never a newly substituted process, arbitrary reported PID,
other Runtime, systemd unit or old panel.

Configuration replacement/rollback is refused while an observation epoch still
needs settlement. Healthy same-epoch lease refresh continues; observation cannot
extend a lease. A dead observed child cannot trigger the old automatic fallback
which would otherwise start a new unobserved Xray process.

## Interruption and replay

On termination the worker attempts the Agent-only
`POST /api/v1/agent/native-usage/interrupted` endpoint. This endpoint uses the
existing TLS, Registry identity and tenant checks, accepts only a bounded epoch
identifier, and derives affected clients from the already registered epoch. The
ledger commits one idempotent unknown-tail hold without zeroing totals, deleting
UUIDs or modifying sales credit. Invalid/foreign epochs and a sealed epoch are
not silently adopted. Public/browser routes cannot submit this notice.

If the HUB is unavailable, the encrypted runtime intent and outbox remain.
`ReplayObservedUsage` first reasserts the interruption through the pinned sender,
then replays only persisted observations. Repeated notices do not add holds;
repeated receipts do not add usage. Replaying known bytes is NOT evidence that
the final unobserved tail is zero and does not lift the settlement fence.

The actual `cmd/node-agent` synchronization loop invokes this recovery hook
before requesting another configuration. It keeps liveness in its independent
worker and does not replace the unresolved runtime after a successful replay.

## Evidence and remaining boundary

Unit/integration tests cover periodic collection, complete scope matching,
independent timeout under a locked Runtime mutex, malformed statistics,
outage/backlog recovery, exact interruption receipts, wrong pins, registry and
public-listener restrictions, and preservation of unrelated runtime listeners.
The real-core CI step explicitly runs
`TestRealPinnedContinuousUsageObserverTLSReplay` using the same official Xray
v26.9.9 pin and modernc driver as the project. It drives actual VLESS/REALITY
traffic, observes automatically, loses a committed receipt, interrupts receipt
availability, reopens private state and reconciles exact persisted bytes without
recounting or clearing the unknown-tail hold. Its identity callback is a scoped
test fixture; actual Registry rejection cases are tested separately. A skip or
missing named real-core test is not acceptance.

This is NOT complete first-traffic admission, hitless per-user quota enforcement,
a zero-overshoot guarantee, a distributed quota allocation protocol or a final
settlement/reconciliation UI. Attaching the observer to a running test child does
not establish that every customer's first byte was gated. The selected stock
core's VLESS RemoveUser implementation only deletes validator authority, so this
change does not pretend that API success closes every existing user stream.
Neither the detached SOCKS prototype nor an unreviewed patched core is selected
as final architecture by this change. A shared core stopping on infrastructure
failure can disconnect all users in that core; preserving user B's existing
connection while revoking A remains a separate release gate.

Normal shutdown without independently verified draining is conservatively
interrupted, not sealed. Settlement is deliberately explicit and still required
before an observed runtime can be reused. The source is not ready for customer
migration merely because its CI succeeds. There is no new deployment job, public
listener, subscription publication flag or change to go.mod/go.sum.
