# Automatic representative volume returns

Unused volume purchased with a representative's credit returns to that same
representative after either actual expiry or manual deletion. In PRO, deletion
archives the client and keeps its identity and accounting history. A plain
disable is not a refund trigger. Unlimited plans retain their existing token
accounting and never enter this worker.

## Accounting contract

The transaction verifies the immutable representative debit history, known
usage baseline and final receipts for every process that could have served the
client. The returned amount is the current volume limit minus total finalized
usage, bounded by proven paid capacity that has not already been returned.
Unknown consumption never becomes zero consumption. An offline or interrupted
runtime remains pending until the required final proof exists; clearing an
administrative hold or recording a lost owner is not final consumption proof.

Fresh PRO identities that were never activated can be returned only when their
immutable complete-zero creation seed and absence of issued runtime capacity
can be positively verified. An ambiguous historical delivery stays pending.

The transaction credits representative free bytes and reduces the client's
remaining volume entitlement by exactly the same amount. UUID, cumulative
consumption, original purchase records and token fields are preserved. This
prevents a time-only renewal from restoring bytes that were already credited
back. Further paid top-ups still buy new capacity through the existing debit
path.

The immutable receipt is shared by deletion and expiry for the same purchased
generation. Replays return that receipt; they do not add credit again. A stale
renewal/expiry generation cannot write against a newer entitlement. Representative
balance limits and arithmetic overflow remain enforced.

## Automatic operation and visibility

`DARK_PRO_ENABLE_VOLUME_REFUNDS_V1=1` enables this additive schema, a bounded
five-second supervisor and the read-only browser reports. It requires the private
PRO business store, authoritative usage ledger and native inbound identity key.
Absent or `0` leaves the feature off; invalid values stop startup. This does not
enable a runtime, deploy a client or import an old HUB database.

The supervisor rotates through batches of 50 eligible clients, committing each
client separately. Pending finality does not starve later clients. A restart
revisits the same durable records safely. Managed runtime polling already removes
expired/archived clients and obtains final receipts during generation replacement;
the refund worker does not clear unknown usage or call administrative loss
reconciliation to manufacture a refund.

Browser reads never trigger a refund. The automatic transaction is independent
of an open page or a representative being signed in.

| Read-only browser route | Scope |
| --- | --- |
| `GET /api/v1/workspace/clients/{id}/volume-refund` | Owner or matching representative with `view_finance`; current eligibility, pending reason and exact amount. |
| `GET /api/v1/workspace/volume-refunds?limit=25&offset=0` | Owner's global history or the signed-in representative's own history; caller-supplied representative filters are rejected. |

Both routes are absent from the separate Agent listener. The API uses decimal
strings for byte amounts. History omits private runtime proofs and installation
identities. The panel distinguishes waiting, completed, unconfigured, denied,
unknown and failed verification; a failed read never displays a successful zero
refund. Archived clients remain visible in the financial return history.

## Release and acceptance

Before activation on an existing isolated LAB, preserve a verified private
SQLite backup and service configuration, install the reviewed artifact and run
synthetic acceptance. Disabling the flag stops future returns without reversing
completed refunds or erasing receipts. A database must not be rolled back to
before a committed return while retaining newer account activity.

Required coverage includes expiry and deletion, plain-disable exclusion,
unchanged unlimited tokens, exact partial and full returns, concurrent and
repeated processing, renewal races and later paid top-ups, fresh unused clients,
missing historical usage, all native/managed finality boundaries, lost owner
tails, durable restart, tenant and financial-permission isolation, and browser
display of exact amounts above JavaScript's safe integer range.

This source increment does not authorize or perform production migration. Real
HUB activation and runtime verification are reported separately from source and
synthetic test completion.
