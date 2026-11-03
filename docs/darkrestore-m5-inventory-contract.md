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

## What the implementation does not do yet

The domain gate does not mutate the database, mark a baseline, issue a token,
publish a subscription, enable a client, change ownership or alter credit. A
durable immutable business receipt, backup/reopen validation, and the database
transition that consumes this decision remain required before M5 can be
installed or accepted on LAB.

PRO LAB's installed revision and all production resources remain unchanged.


## Signed coverage assertion and isolated transition model

`cutover_contract.go` adds a canonical, domain-separated Ed25519 assertion with separately pinned document and authority hashes. The assertion is explicitly an authority claim of inventory completeness and retirement of declared sources. Signature verification identifies the claimant; it cannot discover undeclared installations or independently prove physical retirement. Real acceptance still requires operator evidence, never synthetic signatures.

The assertion binds the exact inventory, source identity, destination authority fingerprint, immutable import-receipt digest, and separately pinned billing-attribution evidence. Epoch delta aggregation requires the explicit `disjoint_source_epoch_deltas` attribution contract. Coverage alone is insufficient: shared transit counters, overlapping epochs or unknown attribution must be refused rather than billed twice. Imported historical bytes are counted once, each attributable source delta once, and measured destination usage once. Current entitlement already includes recorded renewals. No ledger entry, historical amount or payer is rewritten.

The model requires a complete native usage snapshot with a matching expected digest. The baseline covers all destination consumption since creation of the imported identity, including pre-enable configuration tests; a known zero is valid only when actually measured. Model fields are trusted-adapter inputs, never browser claims. A future Store adapter must read identity, receipt, current entitlement, holds, snapshot and expiry in one authoritative transaction and must invalidate stale approvals or delivery evidence after relevant changes.

A transition opens strictly after the nanosecond approval timestamp and no later than the authoritative current time. Issuance eligibility precedes delivery, while modeled activation requires a later delivery receipt bound to the same transition and never dated in the future. A timestamp or revision from another transition does not qualify. Unknown legacy unlimited entitlement stays blocked. New unlimited orders retain the agreed months-times-users pricing policy; this finite imported-identity model does not reinterpret them.

This is a pure, synthetic-testable transition model. `authorizes_mutation` is always false. No product API, SQLite activation trigger, subscription issuer or data plane is changed by this model. Persistent receipt storage, transaction-bound baselines and controlled issuer integration remain required before any imported identity can be enabled. Positive model fixtures do not constitute installed LAB acceptance, inventory verification or source retirement.


## Destination snapshot adapter

`Store.DarkRestoreCutoverSnapshotV1` reads the immutable import receipt, UUID, current representative, current entitlement, history, expiry, deployment state and native account in one read transaction. It produces separate native-baseline and complete-state hashes. Assignment and paid renewal change the state hash while preserving the original import-receipt hash and history; replay and restart preserve it. The account may contain recorded zeros while `native_baseline_complete=false`: zero counters alone never establish complete measurement. Missing accounts remain explicitly missing. These hashes are stale-state detection tokens, not custody authority; no destination fingerprint is invented. Mutation-side compare-and-commit and a trusted configured destination pin remain required.


The signed approval also pins the complete destination-state hash and native-baseline hash. A subsequent representative change, entitlement renewal, account measurement or coverage-state change requires a fresh matching approval before the modeled issuance step. The trusted adapter must compare the hashes against its current transactional read. Reusing an approval with a foreign or stale destination/native snapshot fails before any eligibility result is returned.
