# Dark Restore M2 — implementation checkpoint

This branch remains a draft. It must not be merged or installed as a completed M2 feature.

## Implemented

- Owner-only read-only quotes with group mapping and exact historical aggregates.
- Clients can load a server-approved snapshot, map groups, preview the import, commit it and look up the resulting receipt as the owner. Local JSON remains a private preview.
- Stage an independent snapshot at HUB startup with `-darkrestore-snapshot`, `-darkrestore-sha256` and `-darkrestore-as-of` on an explicitly ownership-enabled LAB. The copy and its parent directory must be private. Requests carry the review hash and mappings.
- Rebuild the review from the private SHA-pinned source snapshot before import; internally consistent browser JSON is insufficient.
- Preserve verified UUID, exact expiry, quota, archive flag, source review and historical group totals, including promoted and blocked records.
- For snapshots containing legacy core tombstones, a manually archived identity requires a matching subscription, deletion record, archive event and per-user owner audit. The deletion must fall between the recorded start and completion. A repeated, aggregate-only or conflicting operation leaves the identity unverified. The archived row retains its historical bytes in the review.
- Keep imported identities disabled with incomplete native usage baseline until independently verified traffic cutover.
- Store historical bytes separately from post-import native counters; expose a read-only historical/current/combined projection with an explicit cutover-verification flag.
- Preserve exact legacy event IDs, timestamps and details in a sealed archive bound to the receipt and snapshot. The owner can page through the history from the receipt. LAB staging requires the existing native encryption key.
- Atomically persist immutable receipts, source identities, groups, skips and usage seeds.
- Replay the same event without duplication; reject changed requests, reimport of a consumed source, and UUID collisions across source snapshots.
- Import and assignment do not debit representative credit. Existing ownership transfer enforces current management scope; later renewal charges the current representative.
- Quarantine rejects individual/bulk enable and both subscription token issuers.
- Reopen and native offline backup validation reject missing or contradictory import schema/history.

## Verified by automated tests

- Verified synthetic legacy SQLite snapshot to actual PRO Clients, preserving source bytes.
- Authenticated Chromium login, group mapping, preview, commit and receipt recovery after reload against a disposable Go server and SQLite.
- Atomic rollback on injected write failure; successful retry after rollback.
- Same-request replay and receipt lookup; receipt privacy.
- Abrupt process exit mid-transaction and immediately after commit, recovery through the same request, reopen and native offline backup validation.
- Archived/promoted/blocked historical totals and safe subset import.
- Ownership transfer, previous-manager rejection, current-manager renewal charging.
- Source authenticity, tampered accounting, uncertain UUID and group mapping rejection.
- Eight writers across two stores converge on one receipt. Native encrypted backup and restore preserves the imported client and decryptable legacy history, and rejects missing or wrong archive keys.
- Corruption rejection on reopen and backup validation.
- The staged artifact survived two SIGKILLs on a 3,200-row synthetic request. The first interrupted a transaction with uncommitted SQLite WAL frames. The second interrupted delivery of the response after commit. Recovery preserved one receipt and 3,041 distinct imported clients and usage seeds.

These tests use isolated synthetic databases. They are not installed LAB acceptance or production migration.

## Remaining gates


- Verified cutover/checkpoint design, remaining-quota policy, and native/subscription activation acceptance. No incomplete baseline may be marked complete by an API request.
- Approved legacy-unlimited plan policy; uncertain unlimited records currently remain blocked.
- Operator acceptance on representative production-scale offline fixtures. Staging refuses unknown event layouts, more than 100,000 legacy events or event input above 16 MiB.
- M4/M5: authoritative final counter settlement, positive subscription delivery after settlement, and installed M2 acceptance.
- Full CI and independent scope review before merge; versioned LAB installation afterward.
- Telegram Bot and Store integration only after these gates, using the panel's existing wallet/order/usage logic.

No production HUB, users, representatives, bots, DNS or Nodes are modified by this work.

## M4 local source accounting rehearsal

The Linux LAB bundle includes `dark-pro-darkrestore-local-settlement`. It reads an independently pinned pre-admission legacy SQLite copy, an offline copy of the dedicated source HUB accounting database, and the source Agent's encrypted final runtime journal. The default output is a private accounting result for one source epoch. With an explicit export path, it can also prepare a new SQLite input for M2.

The supported replica is a single finite-volume Restore identity, one local VLESS Reality/TCP inbound, and one registered source process. The replica must bind the same UUID and the source client ID derived by `darkrestore.LocalSourceID(snapshotSHA, restoreID)` before admitting traffic. The rehearsal provisions this identity in a disposable source. Existing stock Xray deployments do not emit the required evidence.

Run the verifier as the owner of the private source-journal copy on the same boot of the source host. Place the private SQLite copies in owner-only directories. Provide each digest from the approved checkpoint. The state key is supplied as a private raw 32-byte file.

    dark-pro-darkrestore-local-settlement \
      -source /private/pre-admission.sqlite -source-sha256 "$SOURCE_SHA" \
      -authority /private/source-authority.sqlite -authority-sha256 "$AUTHORITY_SHA" \
      -journal /private/runtime.enc -journal-sha256 "$JOURNAL_SHA" \
      -state-key-file /private/source-state.key -source-listen-ip "$SOURCE_LISTEN_IP" \
      -node "$SOURCE_NODE" -installation "$SOURCE_INSTALLATION" \
      -source-id "$RESTORE_ID" -as-of-unix "$SNAPSHOT_TIME"

The dedicated source identity must already be archived through its normal lifecycle API. The verifier checks the zero-debit archive operation, invalidation of the last delivered configuration, and expiration of its signed lease. A successful result ties the historical bytes to the snapshot, the epoch's uplink and downlink to the source's initial zero and terminal receipt, and the exact UUID to the credential served by the exited process. The finite quota remaining is floored at zero. Replaying the same inputs yields the same result. An uncertain epoch, second registration, missing receipt, changed credential, wrong key, or unmatched journal blocks the calculation.

For a first destination import, pass `-export-copy /private/settled-import.sqlite` to the same command. The export accepts just one legacy identity and one group. It adds the closed epoch as its own historical usage row in a new copy and records the original snapshot, authority and journal digests. Stage the returned `import_sha256` and copy with the existing M2 flow. Repeating the export verifies the existing file. An unrelated destination file is preserved.

The isolated acceptance imports this copy with an owner session into a separate PRO database. Replaying the request recovers the original receipt. The client retains its UUID and expiry, and the source's final bytes are displayed as historical while the destination starts with zero post-migration usage.

For an identity imported earlier from the original pre-epoch snapshot, a source-local owner operation can directly call `AttachVerifiedLocalSettlementV1` with the private pinned source inputs and the in-process `LocalTCPSeal`. It runs the complete verifier again. The destination matches the original source ID, SHA, UUID, expiry, finite quota and pre-epoch consumption, then appends one immutable local-epoch attachment keyed by the existing client and terminal epoch. It does not change the original import receipt, client identity, native usage seed, representative balances or quarantine. Replaying the same proof, including after restart, returns the same attachment. A later renewal still charges the current representative and does not change the original source entitlement or attached history.

The optional attachment schema upgrades an earlier M2 database in place. A header marker prevents an attached database with a missing attachment table from being mistaken for the older schema. Native offline backup validation checks each receipt against the immutable original import. The usage projection adds the closed epoch once to historical bytes, retains zero post-migration traffic, and exposes `source_epoch_settled` while `cutover_verified` remains false. This is an internal source-local method with no HTTP or signed remote handoff; it cannot be invoked merely by submitting JSON.

This proves accounting for the one closed local source epoch and attachment to an earlier import in an isolated source/destination test. Exclusive retirement of every source that could serve the credential, a trusted cross-host handoff, compatible positive subscription delivery, and persistent LAB acceptance remain open. No production resource is touched.
