# Node Manager N2 — read-only dependency inspection

Historical baseline: N3 extends this coverage with retained Stage/version journals
and authenticated command maps; see [N3](node-retained-dependencies-n3.md). The
exclusions and coverage identifier below describe N2 before that extension.

Owner-session GET `/api/v1/workspace/nodes/{id}/dependencies` provides an on-demand
snapshot for an existing paired PRO node. Representatives and anonymous callers
cannot read it. It has no mutation endpoint, migration, credential change, runtime
activation, delete/retire or replacement execution.

## What the snapshot means

Native database counts come from one read-only SQL transaction. They cover all
installations associated with the logical node ID, including archived/history
references. A separate count highlights native rows associated with a different
installation; it is **not** a count of distinct installations. Accounting has its
own foreign-installation epoch count.

Included: inbound targets (all and enabled/unarchived), distinct assigned clients,
route drafts and audit events, configuration revision/delivery/report records,
legacy and metered channels/commands, queued or delivered commands, explicitly
abandoned-unknown commands, metered births, usage epochs by state, historical
holds/receipts/deltas, and replacement operations (all/non-cancelled).

`swap_upstream_references` counts **current saved route drafts** that reference this
node as an enabled Dark Swap upstream. The implementation authenticates/decrypts
every draft before counting; it does not infer the graph from port columns.

Replacement history is read from its independent journal after the SQL snapshot.
Registry identity is read separately. `atomic_across_stores` is always false.
`observed_at` is the response observation time, not an assurance of current process
liveness or a durable reservation. Command states and queued counts are historical
database observations, not proof that a listener stopped or an ACK remains live.

## Explicit limitations — never a removal permit

`can_retire=false` and `can_replace=false` are unconditional, including when every
known count is zero. `lifecycle_blocker=atomic_node_lifecycle_not_implemented`.
The declared coverage is `native_sqlite_and_replacement_journal_only`.

This does not scan independent configstage/labrevision files, backup snapshots,
live sockets/processes, or reconstruct upstream graphs inside historical saved
command payloads. It does not authenticate the contents of every counted command
or usage row; it reports retained row references, not accounting correctness or
deployment readiness. Those exclusions must be resolved before a future atomic
lifecycle transition can authorize retirement or replacement.

## Unknown is not zero

- Disabled usage ledger: accounting counts are JSON null with
  `usage_ledger_disabled`. The read does not initialize tables or client baselines.
- Unavailable inbound key: upstream count is null with `inbound_key_unavailable`,
  even when the visible draft index is empty.
- More than 4096 drafts: upstream count is null with `draft_scan_limit` instead of
  returning a truncated count. Only 4097 index rows are materialized.
- Disabled replacement journal: replacement counts are null.
- SQL failure, cancellation or corrupt/wrong-key draft: the endpoint returns 503
  without partial counts. If the session store itself is unavailable,
  authentication fails first; no counts are exposed.

The request has a five-second context budget. It never builds a node config,
advances configuration sequence/generation, changes clients, resets usage, clears
holds or prunes historical rows. There are no schema changes or startup migrations.

## UI and validation

The owner explicitly opens or refreshes the inspector; roster polling does not
trigger dependency reads. The view shows observation time and coverage limitations.
Starting a refresh clears the old snapshot; failed refresh shows unavailable, not
old data or zero. It rejects another node's response, malformed/missing counts,
null without an explanation, or unexpected lifecycle permission claims.
Closing/unmounting aborts the request and ignores late replies. English/Persian,
320px layout and unknown-state display are browser-tested.

Race tests cover concurrent reads, original state/generation preservation,
cross-installation history, decrypted upstream references, disabled versus empty
ledger, retained usage/holds/receipts, command states, corruption, missing tables,
scan bounds, authentication and replacement cancellation preserving history.
Synthetic SQL fixtures are query tests, not a claim of live traffic acceptance.

This change does not deploy itself. N1's registry rollback compatibility rules
still apply to any combined release (`node-name-management-n1.md`). Before a PRO
LAB installation, validate fresh backups and restoration; old HUB and operational
nodes stay untouched. No users, representatives, credits, bots or real customer
data are migrated. USA Xray remains off without explicit approval.
