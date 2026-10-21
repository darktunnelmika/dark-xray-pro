# N9 — durable two-Node accounting barrier

This phase implements the business-store coordinator for two **already
operator-approved metered TCP loopback LAB channels**. It adds no browser,
admin, Agent, startup activation or subscription API. The USA test core remains
disabled; production and legacy data are outside its scope.

## State and ordering

`CreateMeteredHandover` reviews the source's exact running stage and unsettled
registered epoch, plus an acknowledged, unexpired target stage for the same
inbounds, inbound revisions and currently eligible customers. Both Agent
observations must be fresh, with no pending command or interrupted operation.
The target must have no running listener and any previous epoch must be sealed.

One SQL transaction stores the immutable plan and reserves both Node IDs across
source/target roles. A repeated ID must carry the identical review. Concurrent
plans cannot partially reserve one Node. This internal coordinator must not be
exposed until the API checks current registry installations and excludes the
separate legacy replacement journal under its lifecycle lock.

`QueueMeteredHandoverCommand` explicitly queues just one reviewed action:

1. `drain` for the exact source stage/epoch.
2. `apply` for the reviewed target stage, only after the exact source drain ACK
   is durable, the HUB ledger independently verifies its final seal, and a fresh
   source observation after that ACK confirms the same epoch and stopped core.

Step identity, encrypted command, serial allocation and operation link commit
together. Normal Stage, Apply, Renew and Rollback on either reserved Node are
blocked. Emergency Drain remains available without releasing reservations.
The delivery and birth-authority checks repeat the source barrier; a received
ACK does not substitute for a HUB accounting seal. Global draft, eligibility,
revision and command-expiry checks continue to apply.

## Uncertain outcomes and rollback

An exact owner retry returns the same command. Lost drain ACK, missing final
seal, stale observation, unknown abandoned command or failed/stale ACK cannot
authorize target execution. Reservations survive HUB restart. Cancellation is
allowed only before any Drain step has committed; retries preserve its receipt.
After Drain has been queued, failed or unknown work remains `recovery_required`
until a later explicitly reviewed recovery workflow is implemented. No implicit
timeout unlock or source restart is performed.

Even a successful target Apply ends at `target_applied_health_required` with
`can_publish=false`. Reservations remain. Protocol health proof, cross-Node
rollback, fresh lease renewal, owner API/UI and subscription publication are
subsequent phases. This is not a deployable production migration wizard.

Business schema version is now **8**. Earlier binaries refuse this database;
rollback requires a matching full pre-upgrade state/config/binary backup.
Restoring an older backup after real external execution is not an accounting
recovery procedure. LAB deployment is permitted only with all runtimes disabled
and no active customer operation.

## Validation

The focused Race tests cover exact final accounting (333 synthetic bytes, no
double charge), lost owner response, lost Agent ACK across HUB reopen, forged
final seal, source resumption, explicit target birth/initial receipt, no false
health/subscription approval, emergency stop, cancellation, concurrent plans,
insert-failure atomicity, and late ACK after unknown abandonment. Existing
single-Node real pinned-core traffic checks remain required in CI. A complete
two-Node real-traffic transfer test remains required before enabling this path.
