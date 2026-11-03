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
