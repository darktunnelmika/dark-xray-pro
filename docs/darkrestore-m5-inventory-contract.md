# Dark Restore M5 evidence and cutover contract

Status: implemented as an isolated, fail-closed domain gate with synthetic
positive and negative tests. It is not production inventory evidence, is not a
deployment authorization, and is not installed on PRO LAB.

## Trust boundaries

M5 accepts three distinct evidence classes. None may substitute for another.

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

Cryptographic verification proves which authority made the third assertion. It
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
and final cutover receipts plus the activation guard. Baseline completion is a
separate fail-closed transaction: it accepts only a disabled finite imported
identity with a signed settlement, an existing native account, measured zero
counters, zero holds and no recorded native activity. A stored zero without the
completion marker remains unknown.

Opening the transition re-reads the import receipt, UUID, representative,
entitlement, expiry, state and native account in the same write transaction. The
signed approval must pin that exact destination-state digest, native-baseline
digest, import-receipt digest, configured destination authority and independent
billing-attribution digest. The evidence receipt binds those values, the
inventory and approval hashes, the remaining budget, and a domain-separated
transition hash. Any assignment, renewal, expiry edit, archive/state change,
counter change or receipt change after approval invalidates issuance until a
fresh approval is opened.

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
installed or accepted on PRO LAB. PRO LAB's installed revision and all
production resources remain unchanged.


## Signed coverage assertion and isolated transition model

`cutover_contract.go` adds a canonical, domain-separated Ed25519 assertion with separately pinned document and authority hashes. The assertion is explicitly an authority claim of inventory completeness and retirement of declared sources. Signature verification identifies the claimant; it cannot discover undeclared installations or independently prove physical retirement. Real acceptance still requires operator evidence, never synthetic signatures.

The assertion binds the exact inventory, source identity, destination authority fingerprint, immutable import-receipt digest, and separately pinned billing-attribution evidence. Epoch delta aggregation requires the explicit `disjoint_source_epoch_deltas` attribution contract. Coverage alone is insufficient: shared transit counters, overlapping epochs or unknown attribution must be refused rather than billed twice. Imported historical bytes are counted once, each attributable source delta once, and measured destination usage once. Current entitlement already includes recorded renewals. No ledger entry, historical amount or payer is rewritten.

The model requires a complete native usage snapshot with a matching expected digest. The baseline covers all destination consumption since creation of the imported identity, including pre-enable configuration tests; a known zero is valid only when actually measured. Model fields are trusted-adapter inputs, never browser claims. The Store adapter reads identity, receipt, current entitlement, holds, snapshot and expiry in one transaction and rejects stale approvals or delivery evidence after relevant changes.

A transition opens strictly after the nanosecond approval timestamp and no later than the authoritative current time. Issuance eligibility precedes delivery, while modeled activation requires a later delivery receipt bound to the same transition and never dated in the future. A timestamp or revision from another transition does not qualify. Unknown legacy unlimited entitlement stays blocked. New unlimited orders retain the agreed months-times-users pricing policy; this finite imported-identity model does not reinterpret them.

The domain decision remains pure and synthetic-testable; `authorizes_mutation` is always false. Store authority is supplied only by the separate transaction-bound business adapter, immutable receipts, issuer guard and SQLite activation trigger described above. No data-plane change or public operator API is implied. Positive model or Store fixtures do not constitute installed LAB acceptance, real inventory verification or source retirement.


## Destination snapshot adapter

`Store.DarkRestoreCutoverSnapshotV1` reads the immutable import receipt, UUID, current representative, current entitlement, history, expiry, deployment state and native account in one read transaction. It produces separate native-baseline and complete-state hashes. Assignment and paid renewal change the state hash while preserving the original import-receipt hash and history; replay and restart preserve it. The account may contain recorded zeros while `native_baseline_complete=false`: zero counters alone never establish complete measurement. Missing accounts remain explicitly missing. These hashes are stale-state detection tokens, not custody authority; no destination fingerprint is invented. The mutation-side adapter compare-and-commits them and still requires a separately trusted configured destination pin.


The signed approval also pins the complete destination-state hash and native-baseline hash. A subsequent representative change, entitlement renewal, account measurement or coverage-state change requires a fresh matching approval before transactional issuance. The adapter compares the hashes against its current read in the write transaction. Reusing an approval with a foreign or stale destination/native snapshot fails before any subscription mutation.
