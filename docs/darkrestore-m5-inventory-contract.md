# Dark Restore M5 evidence and cutover contract

Status: the prior Store gate is installed on isolated PRO LAB at revision
`174af74c18d33d4366f4ae212060ddbbc84daa3c`, with zero cutover records and its
positive gate closed. The independent native-baseline strengthening below is
source/test work, not installed acceptance or production evidence.

## Trust boundaries

M5 accepts four distinct evidence classes. None may substitute for another.

1. **Pinned inventory.** An independently checkpointed inventory binds the
   imported snapshot SHA and source identity to every declared source's epoch,
   sequence, terminal receipt SHA, authority/journal SHA, signed handoff SHA and
   signer public-key SHA. Its domain-separated digest is stable under source
   ordering.
2. **Verified source retirement receipts.** Every inventory row must match one
   opaque, signature-verified handoff. Missing, extra, duplicate, unverified or
   mismatched rows fail closed.
3. **Independent completeness/retirement assertion.** A separately pinned
   canonical document, signed by a separately pinned Ed25519 coverage authority,
   must bind the exact inventory digest, source identity, source count and sorted
   epoch list. It must explicitly assert both inventory completeness and
   retirement of all declared sources.

4. **Independent native lifetime measurement and active fence receipt.** A
   canonical Ed25519 native-supervisor assertion binds the exact destination,
   source/import receipt, UUID, pre-completion destination/native snapshot,
   measured uplink/downlink, declared native inventory, measurement archive and
   active fence. Its document, key, destination, inventory, measurement and fence
   digests must come from independent trusted local configuration. No browser
   endpoint selects these pins or marks a baseline complete.

Cryptographic verification proves which authority made the third or fourth assertion. It
does **not** prove that an undeclared installation does not exist. A full match
for one inventory or a valid signature for one epoch must never be described as
proof of global absence. Real inventory collection, key custody, source
retirement and the authority's operational diligence remain external acceptance
evidence; no production fact is inferred by this code.

## Baseline and remaining budget

The native baseline means the destination usage counters are complete from the
imported client's creation through the opening of the controlled transition.
Unknown/incomplete counters or any usage hold block subscription issuance.

For a finite-volume import, the gate computes:

```text
settled historical = imported historical baseline
                   + sum(up + down for every declared retired-source epoch)

native post-migration = destination uplink + destination downlink

remaining budget = current finite entitlement
                 - settled historical
                 - native post-migration
```

All terms are non-negative signed 64-bit integers and every addition is checked
for overflow. A zero or negative remainder is blocked. Source receipts repeat
the same imported historical baseline; it is counted once, while each declared
epoch's up/down values are counted once. This is intentionally conservative.

A later paid renewal may increase current entitlement. It does not rewrite
settled history, native usage or the original payer's financial ledger. Import,
assignment and M5 evidence verification do not debit representative credit.

Legacy `unlimited` is rejected by this gate until a valid entitlement is
established. The agreed policy for **new** unlimited orders remains
`months × user_count`; it is not retroactively guessed for unknown legacy
records.

## Controlled subscription transition

The decision has two separate gates:

- `subscription_issue_allowed`: exact inventory coverage, independent signed
  completeness/retirement assertion, finite positive remaining budget, complete
  native baseline, no holds, non-archived and non-expired client, and a
  transition opening recorded after the approval.
- `activation_allowed`: all issuance conditions plus an authoritative
  subscription delivery revision timestamped strictly after the transition
  opening.

A missing, simultaneous or older delivery never authorizes activation. This
keeps activation closed while allowing an isolated, auditable subscription
handoff to be prepared after the evidence boundary.

## Durable Store gate and remaining operational evidence

The isolated business layer now consumes the pure decision inside one SQLite
transaction. An explicit owner-only schema upgrade creates append-only evidence
and final cutover receipts plus the activation guard. The former
`CompleteDarkRestoreZeroBaselineV1` path is permanently closed: recorded zeros
and absent activity are not measurement coverage.

`PrepareDarkRestoreNativeBaselineV1` is owner-only and read-only. It projects the
post-completion snapshot only after an opaque verified native receipt matches the
current UUID/import receipt, pre-completion state, counters, zero holds and the
whole identity lifetime starting at destination creation. The receipt explicitly
asserts a native traffic fence continuing from measurement through opening; its
lease is bounded to five minutes, cannot be future-measured or expired, and is
rechecked before commit. The signature identifies the attesting authority, not
an independent proof that its operational claims or inventory are true.

Opening re-verifies the prepared state and atomically inserts the complete signed
native bundle and completes the baseline flag. Failure of either write rolls
back both. It does not change counters, entitlement, historical bytes, payer or
credit. The transition hash also binds the native document hash. Stored native
signatures, subject/UUID, lifetime, measured amounts and pre/post native hashes
are checked during read, reopen and backup validation.

Opening the transition re-reads the import receipt, UUID, representative,
entitlement, expiry, state and native account in the same write transaction. The
signed approval must pin that exact destination-state digest, native-baseline
digest, import-receipt digest, configured destination authority and independent
billing-attribution digest. The evidence receipt binds those values, the
inventory and approval hashes, the remaining budget, and a domain-separated
transition hash. Any assignment, renewal, expiry edit, archive/state change,
counter change or receipt change after approval invalidates issuance until a
separately reviewed transition refresh is available; V1 never rewrites its immutable evidence.

Finalization re-reads the same state and accepts only the exact current
authoritative handover-delivery head strictly after transition opening. The
immutable final receipt binds its delivery revision, scope, snapshot ID, event
ID, snapshot revision and nanosecond timestamp to the transition hash. Activation
rechecks the exact state and delivery tuple. Backup/reopen validation checks the
receipt relationships; state-hash comparison is intentionally limited to the
pre-activation quarantine because activation itself changes deployment state.

These methods still do not establish real inventory completeness, physical
source retirement, destination-key custody or actual production delivery. The
positive delivery is a synthetic journal fixture. No public operator API is
claimed here, no real imported subscription is activated, and M5 is not
accepted as a positive transfer on PRO LAB. The independent native-baseline
changes are not yet installed; the installed predecessor remains closed and all
production resources are unchanged.


## Signed coverage assertion and isolated transition model

`cutover_contract.go` adds a canonical, domain-separated Ed25519 assertion with separately pinned document and authority hashes. The assertion is explicitly an authority claim of inventory completeness and retirement of declared sources. Signature verification identifies the claimant; it cannot discover undeclared installations or independently prove physical retirement. Real acceptance still requires operator evidence, never synthetic signatures.

The assertion binds the exact inventory, source identity, destination authority fingerprint, immutable import-receipt digest, and separately pinned billing-attribution evidence. Epoch delta aggregation requires the explicit `disjoint_source_epoch_deltas` attribution contract. Coverage alone is insufficient: shared transit counters, overlapping epochs or unknown attribution must be refused rather than billed twice. Imported historical bytes are counted once, each attributable source delta once, and measured destination usage once. Current entitlement already includes recorded renewals. No ledger entry, historical amount or payer is rewritten.

The model requires a complete native usage snapshot with a matching expected digest. The baseline covers all destination consumption since creation of the imported identity, including pre-enable configuration tests; a known zero is valid only when actually measured. Model fields are trusted-adapter inputs, never browser claims. The Store adapter reads identity, receipt, current entitlement, holds, snapshot and expiry in one transaction and rejects stale approvals or delivery evidence after relevant changes.

A transition opens strictly after the nanosecond approval timestamp and no later than the authoritative current time. Issuance eligibility precedes delivery, while modeled activation requires a later delivery receipt bound to the same transition and never dated in the future. A timestamp or revision from another transition does not qualify. Unknown legacy unlimited entitlement stays blocked. New unlimited orders retain the agreed months-times-users pricing policy; this finite imported-identity model does not reinterpret them.

The domain decision remains pure and synthetic-testable; `authorizes_mutation` is always false. Store authority is supplied only by the separate transaction-bound business adapter, immutable receipts, issuer guard and SQLite activation trigger described above. No data-plane change or public operator API is implied. Positive model or Store fixtures do not constitute installed LAB acceptance, real inventory verification or source retirement.


## Destination snapshot adapter

`Store.DarkRestoreCutoverSnapshotV1` reads the immutable import receipt, UUID, current representative, current entitlement, history, expiry, deployment state and native account in one read transaction. It produces separate native-baseline and complete-state hashes. Assignment and paid renewal change the state hash while preserving the original import-receipt hash and history; replay and restart preserve it. The account may contain recorded zeros while `native_baseline_complete=false`: zero counters alone never establish complete measurement. Missing accounts remain explicitly missing. These hashes are stale-state detection tokens, not custody authority; no destination fingerprint is invented. The mutation-side adapter compare-and-commits them and still requires a separately trusted configured destination pin.


The signed approval also pins the complete destination-state hash and native-baseline hash. A subsequent representative change, entitlement renewal, account measurement or coverage-state change requires a fresh matching approval before transactional issuance. The adapter compares the hashes against its current read in the write transaction. Reusing an approval with a foreign or stale destination/native snapshot fails before any subscription mutation.


## Compatibility and actual validation boundary

Historical pre-attestation evidence remains canonically readable and exportable
without inventing a native receipt or changing its bytes. It cannot authorize a
new issuance or activation. Replays return the original immutable receipt and do
not refresh authority; changed destination state still blocks mutation guards.

The synthetic v15 native handover fixture tests issue/delivery/finalization and
reopen. That old fixture schema is deliberately outside the official offline
backup allowlist; no backup rule was weakened. A separate supported v16 managed
schema fixture verifies official offline backup validation, reopen, exact native
proof preservation and unchanged balances. Its undelivered client remains
blocked. These are isolated temporary databases, not installed LAB recovery or
real native-supervisor, source-retirement or production-delivery evidence.

A real native-supervisor emitter, independently collected native inventory,
measurement/fence receipts, trusted local pin provisioning and operator acceptance
are still required for real transfers. This implementation supplies no production
signing authority and creates no assertion about currently unknown data. New
unlimited pricing remains months times users; unknown legacy entitlement remains
blocked. Post-opening state drift currently closes V1; a future controlled refresh
must preserve all prior receipts and financial history rather than replacing them.


## Enforced native budget reservation

The explicit M5 schema opt-in also enables the exact-DDL optional usage-ledger
historical-budget extension. One immutable per-client reservation binds settled
historical bytes to the complete canonical M5 evidence receipt. Opening records
that reservation, the native receipt and the baseline flag in the same write
transaction. Any failure rolls back all three. The reservation never alters
native counters or representative credit. No generation is changed merely for a
quarantined client's reservation; the ordinary activation path changes runtime
generation.

Actual `pro_usage_eligibility` now compares measured native consumption against
`current entitlement - reserved settled history`. It therefore closes at the
remaining budget rather than granting historical bytes a second time. Arithmetic
uses subtraction before comparison, avoiding addition overflow. Native overshoot
is retained. Renewals raise current entitlement once and retain the same immutable
historical reservation. Native unlimited clients without reservations retain their
existing behavior; adding a reservation to unlimited entitlement is refused.

`Account.Total` remains native consumption and `Account.Limit` remains current
entitlement. Separate reserved-history and remaining-native-budget fields expose
the distinction; remaining is absent for unknown/held accounts or unlimited plans.
All account/budget reads use one WAL snapshot. Exact DDL verification detects a
missing immutable trigger, partial feature or altered eligibility view at startup
and official offline validation. Old binaries reject the extra feature objects
rather than silently ignoring the reservation.

M5 guards and readiness require the exact reservation/receipt binding. Offline
validation checks it whenever this extension is enabled. Upgrading a predecessor
M5 database that already has evidence but no reservations is refused transactionally;
its history is not backfilled or rewritten by guessing. No such rows exist on the
last observed installed LAB. A separate evidence-preserving forward migration
would be required for such an older opted-in dataset.

Tests use a real native ledger: 700 settled bytes against 1000 entitlement leave
300 native bytes; 299 stays eligible and 300 closes eligibility. Renewal to 1200
leaves 200 after that native consumption. Replay/reopen retain the reservation and
native counters; invalid/unlimited reservations, changed receipt bindings, schema
tampering and transaction rollback fail closed. The v16 backup fixture preserves
the enforced 14/0/999986 historical/native/remaining split without issuing delivery.

Serving startup is stricter than historical export: an active imported identity
without a valid native attestation and historical-budget reservation is refused.
A synthetic predecessor-schema test verifies that upgrade refusal preserves the
original receipt bytes and that runtime startup stays closed. No reservation or
financial history is backfilled from absent data.

Account remains value-comparable for financial rollback snapshots. Optional
remaining budget uses a value plus known bit internally, with exact string JSON
when known and an absent field when unknown. This preserves financial before/after
checks without comparing pointer addresses. Round-trip tests include measured
zero and values beyond JavaScript's exact integer range.

## Canonical pre-enable configuration delivery

`DeliverPreparedDarkRestoreSubscriptionV1` is an owner-only Store operation with
scope `darkrestore_prepared_not_applied_v1`. It requires the full accepted source,
native-baseline and historical-budget chain. It authenticates a saved, versioned
managed inbound plan using the existing inbound key, requires exact expected
plan/inbound revisions and complete endpoint metadata, and uses the same
`inboundspec.ExportClient` renderer and deterministic execution identity as normal
managed subscriptions. It accepts no browser-provided credentials, configuration
bytes, evidence flags or trust pins.

This records a preparation/delivery boundary when the Store returns the real
canonical content to its authorized caller. It is not a recipient network ACK,
proof of an applied route, health/path verification or an active subscription
service endpoint. The owner/CSRF-protected download route exposes only this same
bounded operation. Its result always reports `applied=false` and
`path_verified=false`. The ordinary applied-route renderer still rejects the
disabled identity.

The immutable final receipt binds content hash, output format, current encrypted
plan/profile authority hash, exact revisions, client/financial transition and
current remaining budget. No plaintext configuration or new private key is
stored in the receipt. Failed receipt insertion/commit returns no content.
An exact authorized replay regenerates the same content only while its plan and
financial authority match. Profile, entitlement, payer, identity or expiry drift
blocks reuse/activation rather than rewriting the original receipt.

Preparing a configuration never binds or enables the client, queues a runtime,
changes generation, alters native eligibility or debits representative credit.
It therefore cannot authorize even the logical `pending_node_apply` transition:
readiness reports the explicit `native_apply_not_recorded` blocker, and the
enable transaction fails closed after revalidating the prepared receipt. A later
append-only receipt must bind the exact promoted plan/client association and
native apply before that blocker can be removed. Current leases, accounting and
path health remain independent data-plane gates. Preparation is never labeled
healthy/applied merely to close the pre-enable delivery cycle.

The real renderer fixture uses supported schema v16 and synthetic evidence. It
checks canonical VLESS content with the preserved UUID, explicit prepared scope,
unchanged disabled state/generation/balances, ordinary-renderer quarantine,
unauthorized/stale/unsupported refusals, insertion rollback with no content,
idempotent retry, stale profile, current-payer identity and paid-quota
invalidation, refused pre-apply activation, official offline backup validation
and recovery. No direct insertion of a fake subscription journal is used for
this new positive path.


## Native promotion/apply evidence contract

`ValidateDarkRestoreNativeApplyEvidenceV1` is a pure fail-closed boundary for
the next Store integration. It accepts no browser completion flag and performs
no database or runtime mutation. Independently supplied pins must match the
original transition, prepared content and profile authority, exact inbound and
plan revisions, immutable promotion event/receipt, initial-apply digest, apply
command, stage, health round and accepted health receipt.

Revision and time order are structural: promotion advances the prepared inbound
revision exactly once, then prepare < promotion < apply < accepted health. A
future acceptance timestamp, a repinned document with a foreign client, or a
mismatch in any source receipt fails closed. A valid projection reports recorded
apply and accepted health only within the pinned document scope and always
returns `authorizes_mutation=false`. It is not an emitter, operational truth,
proof of undeclared-source absence or permission to clear
`native_apply_not_recorded`; the Store must later derive it from its own
immutable promotion/apply/health rows in one coherent read.

## Post-opening destination refresh

A legitimate renewal or owner assignment after opening changes destination state,
so the original approval hash must not be edited or silently reinterpreted.
`RefreshDarkRestoreCutoverV1` appends a separate immutable receipt only before
the first subscription-delivery receipt. The caller supplies the previous
snapshot preimage; the Store canonicalizes it and requires its hash to equal the
original approval or the immediately preceding refresh.

Each refresh is bound to exactly one existing append-only cause:

- a client extension must reproduce the exact entitlement and expiry transition
  and names the representative that paid it;
- an ownership receipt must reproduce the exact previous/current payer while
  proving balance, usage and credentials were unchanged.

UUID, import receipt, historical bytes, finite plan, native baseline/counters,
zero holds, disabled state and archive state must remain identical. Unknown
causes, combined drift, counter drift, stale preimages and reused causes fail
closed. The refresh changes no client, balance, evidence, reservation, runtime
or subscription and has `authorizes_mutation=false`. An existing delivery
receipt closes this V1 refresh path; supporting a later re-delivery requires a
separately versioned append-only delivery chain.


## Imported serving-health evidence boundary

`DarkRestoreServingHealthEvidenceV1` is a historical, read-only contract for one exact
external protocol challenge after the signed listener-installation receipt. The
document and independent pins bind the import transition, client, quarantine plan,
current config and epoch, admission projection, listener receipt, approved verifier
endpoint, complete signed request, complete signed response and authoritative record
time. Validation uses the paired Agent HMAC at the response completion time so an
expired challenge is never extended or reissued.

A valid document sets only `serving_health_recorded=true`. It keeps
`native_apply_recorded=false` and `authorizes_mutation=false`. The signature proves
possession of the paired Agent key, not the operational truth of the emitter, complete
inventory, retirement of other sources, subscription publication or continuing
health.

The optional Store adapter records one canonical document only after owner
authorization, current admission/listener re-derivation, an exact independently
selected verifier-endpoint hash, paired-Agent receipt verification and the
listener-to-request-to-receipt-to-record time order. Its table and triggers are
append-only; replay returns the original bytes and a conflicting replay fails.
Reopen and offline-backup validation re-derive every source relationship without
claiming that an HMAC key is recoverable from the database.

The adapter does not manufacture the request. A reviewed Agent/HUB emitter must
still derive the system-probe credential, epoch/admission hashes and live runtime
identity from the exact quarantine candidate and persist the challenge before
execution. Until that real path and native apply evidence are complete, imported
activation and delivery remain closed.


## Quarantine system-probe contract

An imported serving-health proof is structurally impossible unless the exact quarantine candidate and epoch contain a separately provisioned nonbillable system probe. Provisioning is owner-only, idempotent, pre-birth and bound to the authoritative node, installation and inbound. It creates no listener, epoch, wallet event, customer eligibility, challenge or subscription.

A probe-enabled plan binds:

- the probe ID and one-way credential digest;
- the candidate/config and manifest produced with both imported and probe users;
- the exact post-zero usage-admission digest;
- a canonical epoch with exactly one billable imported binding and one `system_probe` binding.

The initial report must contain both identities with zero counters. Customer and probe deltas remain separate; the probe ledger cannot charge customer credit. Birth, listener, admission and serving-health validation recheck the probe account, zero delta, epoch digest, admission digest and request credential. Pure protocol-health support is validated against the real rendered candidate.

The managed-execution metadata row remains immutable. Probe accounting uses an exact additive overlay whose marker causes the effective underlying schema to become probe-aware only after complete DDL verification. Partial overlays, holds, stale identity, second probes and post-birth provisioning fail closed. This contract still grants no emitter truth, activation, publication, inventory completeness or retirement authority.

## Atomic prepared promotion and native-Apply projection

The isolated M5 candidate adds one owner-only transition from a disabled,
finite imported identity with a completed prepared export to
`pending_node_apply`. It revalidates the current immutable import, source
settlement, native lifetime measurement, historical-budget reservation,
entitlement, payer, UUID, expiry and exact prepared plan in one writer
transaction. The same transaction performs the ordinary reviewed native plan
promotion and records a canonical append-only transition receipt. Failure to
insert either receipt rolls back both the client state and promotion.

The transition preserves UUID, expiry, ownership, historical usage, native
counters and representative balances. It does not queue Apply, create an epoch,
record health, issue credentials, publish a subscription or authorize another
mutation. Preview always rolls back its temporary eligibility projection. The
stored receipt and update/delete guards are immutable; exact replay returns the
same receipt.

The read-only native-Apply Store adapter then re-derives, from one SQLite
snapshot, the prepared content and encrypted authority, atomic promotion
receipt and nanosecond timestamp, actual acknowledged first Apply, accepted
health receipt, current payer/entitlement and complete historical reservation.
The promotion pin is the digest of the immutable promotion receipt, not the
request fingerprint. Independent pins must match every source. Payer,
entitlement, identity, counter, hold, receipt, profile, revision or timing drift
fails closed. A valid projection still has `authorizes_mutation=false`.

Managed schema v16 can retain the exact v15 Apply/health feature chain only
through an explicit base-schema pin. The forward migration is allowed before
any initial-Apply or delivery protocol row exists; an in-flight preparation,
round, challenge, receipt, snapshot or delivery refuses migration without
creating v16 objects. Business readers resolve the immutable v16 base pin on
every use. Reopen and offline validation therefore see the same v15 feature
chain without downgrading `user_version` or guessing capabilities.

Synthetic positive coverage performs the real prepared render, atomic
promotion, metered Stage/Apply, registered usage epoch and zero receipt, signed
health round, exact projection, replay, restart and official offline-backup
validation. Stale entitlement and payer, unauthorized review, receipt insertion
failure, mutation/deletion and incomplete prepared-only or native-only chains
are negative. Normal credential issuance and subscription publication remain
closed after the projection. No fixture claims real inventory completeness,
source retirement, production health or permission to activate an imported
identity. Legacy unlimited remains blocked.

## Append-only native-Apply acceptance and bounded initial delivery

The next isolated M5 layer persists the exact Store-derived native-Apply
projection only after an owner submits independent pins for every runtime
receipt and for the current payer, import receipt, native baseline, finite
entitlement, settled history, native counters and remaining budget. The Store
re-derives the whole projection in the same writer transaction that appends the
receipt. The receipt and its schema are immutable; exact replay returns the
original bytes, while a conflicting event, payer, entitlement, profile,
counter, health receipt or document pin fails closed. Insert failure leaves no
partial acceptance.

Acceptance is not delivery or activation. It only permits the existing
first-Apply renderer to materialize one real `lab_initial_apply` snapshot from
the already applied profile and exact client binding. That snapshot must retain
the prepared content hash and match the same transition, promotion, Apply,
health round, payer and entitlement. Renewal or assignment drift before
materialization blocks the operation without a journal row. Readiness exposes
prepared export, native Apply acceptance and real initial delivery as separate
facts and retains the explicit `native_activation_not_authorized` blocker even
after delivery.

Synthetic coverage includes positive render-to-delivery bytes, independent
pin mismatch, unauthorized recorder, idempotent replay, immutable triggers,
transaction rollback, stale payer/entitlement, restart, official offline
backup and unchanged financial/identity state. The ordinary imported issuer
and final activation guard remain closed. This receipt does not prove inventory
completeness, retirement of undeclared sources, continuing health, production
delivery or valid legacy-unlimited entitlement.

## Controlled native activation after accepted Apply delivery

The isolated final M5 gate adds a separate owner-only, append-only activation
receipt. It can be recorded only after the Store re-derives the current native
Apply acceptance and the genuine first-Apply subscription snapshot in the same
transaction. Independent pins explicitly bind the inventory, coverage approval,
destination document and state, billing attribution, import receipt, native
baseline and prepared authority in addition to the transition, accepted Apply
evidence, current payer, finite entitlement, settled history, native counters,
remaining budget, inbound, health round, delivery revision, snapshot identity,
rendered content hash and delivery expiry. Stale payer, entitlement, counters,
delivery, profile or content fails closed.

Recording this receipt does not change the client, runtime, wallet, usage,
ownership or subscription snapshot. It authorizes the existing final-cutover
guard and existing credential issuer only for that exact current chain. Exact
replay is idempotent; conflicting replay, representative access, schema
mutation and insertion failure are rejected. Startup and offline-backup
validation rebind the receipt to the immutable Apply acceptance and delivery
rows without treating later expiry as corruption; current issuance still
requires a nonexpired, unchanged chain.

Synthetic fixtures cover positive authorization, real credential issuance,
negative pin and entitlement drift, replay, rollback, immutable triggers,
restart and offline backup. They do not establish a real production inventory,
retirement of undeclared sources, continuing health, operational approval or a
legacy-unlimited entitlement. No public activation route is added, and no
installed or production identity is activated by this source change.

## Independently pinned activation approval envelope

`VerifySignedNativeActivationApprovalV1` defines the next fail-closed operator
boundary without exposing an activation route. A canonical Ed25519 envelope is
bound to an independently pinned document digest, public-key digest, authority
document, activation-evidence digest, destination and declared inventory. The
incoming envelope cannot choose or replace those pins.

The signed body repeats the complete current financial and prepared-delivery
state: transition, native Apply, coverage, destination state, billing
attribution, import receipt, native baseline, prepared authority, current payer,
finite entitlement, settled history, native counters, remaining budget, inbound,
health round, delivery revision, snapshot, content and subscription expiry. This
prevents an operator approval for a loose client identity from being replayed
after payer, quota, history, profile or delivery drift.

Approval is valid only for a precise authoritative clock inside a maximum
five-minute lease and strictly after the prepared delivery. Finite-budget
arithmetic is recomputed from entitlement minus settled history minus native
post-migration use. Unknown legacy entitlement, incomplete claimed inventory,
unretired declared sources, expired delivery, stale approval and contradictory
remaining budget are refused.

The verifier deliberately returns `authorizes_mutation=false`. A signature proves
only which key made the claims. It does not prove there are no undeclared serving
sources, that a declared source actually stopped serving, or that an unknown
legacy unlimited identity has a valid entitlement. The trusted Store adapter
obtains the opaque verified value outside browser input, re-reads the same
current state in one transaction and persists the canonical envelope with its
immutable activation receipt. No HTTP activation route or positive installed
activation is introduced by this contract.

Synthetic tests cover a positive finite-budget envelope, copy isolation, every
independent-pin mismatch, unknown inventory/retirement/entitlement, stale and
future clocks, excessive/reversed leases, financial contradictions, canonical
encoding and re-pinned tampering. These fixtures are not production inventory or
retirement evidence.

## Declared-source retirement receipts

Inventory coverage and the coverage authority's boolean retirement assertion
are not operational retirement evidence. Each declared inventory entry may now
be paired with two canonical, independently pinned Ed25519 documents:

1. the source statement binds the exact source/inventory/destination, terminal
   epoch receipt, authority, journal, signed handoff, source key, retirement
   time and an opaque active-fence document; and
2. a separate witness binds that exact source document and source key to the
   same inventory and destination within a bounded five-minute review window.

`CheckLocalHandoffRetirementsV1` requires exactly one verified pair for every
declared inventory epoch and rejects missing, duplicate, foreign-destination,
foreign-key or stale-fence evidence. Its digest is deterministic and includes
every source and witness document pin.

Linux startup configuration can load each pair only from canonical absolute,
owner-private, non-symlink files through `VerifySourceRetirementFilesV1`.
`VerifySourceRetirementBundleFilesV1` then checks the complete trusted file
selection against the independently pinned inventory and destination in one
read-only operation; a parsed subset, duplicate epoch, stale inventory pin or
foreign destination is rejected. These loaders are not browser/import inputs
and return no mutation or activation authority.

This coverage is intentionally limited to the independently pinned declared
inventory. Two valid signatures identify two claimants; they do not prove the
truth of an operational fence or the absence of undeclared installations. The
result always returns `authorizes_activation=false`.

The source-host `dark-pro-darkrestore-local-settlement` command can emit the
source half of this pair only after it re-verifies the current closed settlement
and the exact existing signed handoff. It derives the terminal receipt,
authority, journal, handoff and source-key bindings rather than accepting them
as free-form retirement input, and hashes an owner-private opaque fence file
against an explicit pin. Output is immutable and byte-identical retries replay;
stale settlement, a foreign handoff/key, unsafe fence file, pin drift and an
existing different output fail closed. The command does not emit the independent
witness or trusted pins, interpret the fence, discover inventory, or authorize
mutation. Positive fixtures remain synthetic claims, not real retirement.

The separate `dark-pro-darkrestore-retirement-witness` command consumes that
canonical source document, its source public key and the same opaque fence only
from owner-private canonical files. It verifies independently supplied hashes
for the source document, key, fence, inventory and destination before a distinct
Ed25519 key may sign the bounded witness statement. The output is immutable,
byte-identical retries replay and same-key, stale-window, unsafe-file, pin-drift
or conflicting-output attempts fail closed. This command still does not inspect
the fence semantics, establish actual source retirement, discover undeclared
resources, certify inventory completeness or authorize activation.

The read-only `dark-pro-darkrestore-retirement-preflight` command accepts only
one canonical owner-private manifest plus its independently approved SHA-256.
That manifest pins the entire declared inventory, destination and exact private
source/witness document and key selection; every referenced file and both
signatures are re-verified before a deterministic coverage report is emitted.
Missing or duplicate epochs, a stale inventory, foreign destination, unsafe
path, noncanonical manifest or any document/key/pin drift fails closed. The
preflight never derives pins from the supplied files, never mutates Store state
and always reports `authorizes_activation=false`. Pinning one manifest still
does not discover undeclared installations or prove its declared inventory is
complete.

An optional all-or-nothing approval mode additionally loads one canonical
owner-private signed coverage approval and its Ed25519 public key against
independently supplied document and key hashes. The approval must name the
same inventory, source, destination, declared epochs and deterministic
retirement bundle, and its timestamp must be strictly later than every
witness. Partial approval inputs, an equal-time approval, unsafe files or any
binding drift fail closed. This remains a read-only assertion check with
`authorizes_activation=false`; it does not issue an approval, validate the
approval's destination-state/native/billing claims against live systems, or
grant Store mutation.

The Store cutover boundary now requires this bundle in addition to the signed
coverage approval. It persists the exact canonical source/witness documents,
their public keys and independent pins, re-verifies them on every read/restart
and includes the deterministic bundle digest in both the signed coverage
approval and the transition hash. The coverage approval must be strictly later
than every independent witness.
Missing, tampered, foreign, duplicate or replay-conflicting documents keep the
transition closed; an insert failure rolls back both evidence and financial
reservation. Historical pre-attestation receipts remain readable but cannot
authorize mutation.

This Store binding still covers only the declared inventory. It does not turn a
source or witness signature into verified operational truth, discover an
undeclared installation, enable an imported client, or establish an unknown
legacy entitlement. Native Apply, delivery and activation remain separate
append-only gates over the same transition and current financial state.
