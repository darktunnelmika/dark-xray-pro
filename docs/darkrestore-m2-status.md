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
- Read-only cutover readiness counts subscription delivery only when the authoritative delivery journal entry is strictly newer than the signed local-settlement receipt. The attachment keeps a canonical nanosecond timestamp so an earlier or simultaneous delivery cannot satisfy M4/M5.
- Reopen and native offline backup validation reject missing or contradictory import schema/history.

## Verified by automated tests

- Verified synthetic legacy SQLite snapshot to actual PRO Clients, preserving source bytes.
- Authenticated Chromium login, group mapping, preview, commit and receipt recovery after reload against a disposable Go server and SQLite.
- Atomic rollback on injected write failure; successful retry after rollback.
- Same-request replay and receipt lookup; receipt privacy.
- Abrupt process exit mid-transaction and immediately after commit, recovery through the same request, reopen and native offline backup validation.
- Archived/promoted/blocked historical totals and safe subset import.
- Ownership transfer, previous-manager rejection, current-manager renewal charging.
- Subscription readiness rejects missing settlement, disabled delivery schema, zero revision, and delivery before or simultaneous with settlement; only a later authoritative journal entry passes that single gate.
- Source authenticity, tampered accounting, uncertain UUID and group mapping rejection.
- Eight writers across two stores converge on one receipt. Native encrypted backup and restore preserves the imported client and decryptable legacy history, and rejects missing or wrong archive keys.
- Corruption rejection on reopen and backup validation.
- The staged artifact survived two SIGKILLs on a 3,200-row synthetic request. The first interrupted a transaction with uncommitted SQLite WAL frames. The second interrupted delivery of the response after commit. Recovery preserved one receipt and 3,041 distinct imported clients and usage seeds.

These automated tests use isolated synthetic databases. Installed synthetic LAB evidence is recorded separately below; neither is a production migration.

## Remaining gates

- Verified cutover/checkpoint design, remaining-quota policy, and native/subscription activation acceptance. No incomplete baseline may be marked complete by an API request.
- Approved legacy-unlimited plan policy; uncertain unlimited records currently remain blocked.
- Operator acceptance on representative production-scale offline fixtures. Staging refuses unknown event layouts, more than 100,000 legacy events or event input above 16 MiB.
- M4/M5: authoritative final counter settlement, positive subscription delivery after settlement, and full installed acceptance with evidence-backed source identities.
- Final-head CI and independent scope review before merge. The verified draft binary is installed only on the isolated LAB.
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

For an identity imported earlier from the original pre-epoch snapshot, the source host can sign a scope-limited result after it has verified the closed source epoch. Provision a private raw 64-byte Ed25519 key on that host and approve its raw 32-byte public key through an independent channel. The operator adds `-signed-handoff-output /private/handoff.json -handoff-signing-key-file /private/handoff.key` to the local settlement command above. The command creates a private immutable file and returns its SHA-256 and the public-key SHA-256. An identical retry verifies the existing file; a changed result cannot overwrite it. Neither the source runtime key nor the signing private key travels to the destination.

Place only the signed file and raw public key in owner-only directories at the destination, with their independently approved digests. On an isolated ownership-enabled LAB, start the HUB with `-darkrestore-signed-handoff`, `-darkrestore-handoff-sha256`, `-darkrestore-handoff-public-key` and `-darkrestore-handoff-public-key-sha256`. Startup validates both files and the signature. An authenticated owner can inspect `GET /api/v1/workspace/dark-restore/handoff` and attach once with `POST /api/v1/workspace/dark-restore/handoff/attach` using only `{ "handoff_sha256": "<approved digest>" }`. The request requires browser origin and CSRF checks. The server verifies the file again at commit. An uncertain signer, changed file, wrong pin, mismatched original import, or second settlement fails closed. There is no browser supplied source path or signing key.

The destination matches the original source ID, SHA, UUID, expiry, finite quota and pre-epoch consumption, then appends one immutable local-epoch attachment keyed by the existing client and terminal epoch. The receipt records the signed-file and public-key digests for offline audit. It does not change the original import receipt, client identity, native usage seed, representative balances or quarantine. Replaying the same signed proof, including after restart, returns the same attachment. A later renewal still charges the current representative and does not change the original source entitlement or attached history.

The optional attachment schema upgrades an earlier M2 database in place. A header marker prevents an attached database with a missing attachment table from being mistaken for the older schema. Native offline backup validation checks each receipt against the immutable original import. The usage projection adds the closed epoch once to historical bytes, retains zero post-migration traffic, and exposes `source_epoch_settled` while `cutover_verified` remains false. A local in-process verifier also exists for source-host tests; browser JSON alone cannot construct the verified proof.

The signed handoff covers only the specified, fully closed local source epoch. It does not establish exclusive retirement of every other source that could serve the credential. Existing stock Xray has no matching final receipt. Compatible positive subscription delivery, representative-scale legacy fixtures, and persistent LAB acceptance remain open. No production resource is touched.

## Independent client directory and scale acceptance — 2026-11-03

The main Clients directory now filters current group membership and origin
(created here or imported with Dark Restore) in the same SQLite read transaction
as the count and page. Origin follows the immutable import entry after
reassignment. Current group membership remains separate from historical source
group accounting. Representative scope intersects both filters; a guessed foreign
group returns no foreign rows. An invalid or damaged import schema is refused
rather than silently reclassifying its users as native. Before import opt-in, the
imported result is empty and ordinary native clients remain searchable.

The English/Persian directory puts these controls in a compact expandable area.
Changing either filter clears the page and previous bulk selection. An active
filter requires an exact server echo before any returned client becomes
actionable. The existing scoped, paged group picker is reused.

Verified locally on the isolated test host with Go 1.27.3:

- The public SHA-pinned legacy SQLite import processed 750 finite-volume rows,
  six source groups, and 750 encrypted legacy events. Two representatives received
  375 imported clients each. A complete 125-client group then moved to a third
  representative without a credit debit, while its original historical total
  remained unchanged.
- A separate private, SHA-pinned offline fixture processed 5,000 finite-volume
  rows, 20 source groups and 5,000 encrypted legacy events across three
  representatives. Exact pagination verified every preserved UUID and expiry and
  every imported identity remained disabled. Import, same-request replay, receipt
  and sealed-event recovery, offline backup validation and reopen/replay preserved
  one client per source identity, the historical/post-migration split and all
  representative balances. The source hash remained unchanged. On the isolated
  host the import itself completed in about 3.67 seconds and the focused test in
  about 10.9 seconds.
- Current scope, UUID, expiry, quarantine, exact pagination and origin survived
  reopen and offline backup validation. An identical import recovered the same
  receipt. A 4096-byte renewal charged only the new representative, once across
  replay; the previous representative was refused. The private source file hash
  was unchanged.
- Authenticated API tests rejected cross-representative group/origin enumeration,
  duplicate parameters and invalid values. Normal global client search also passed.
- Production TypeScript/Vite build passed. Three Chromium tests passed for owner
  and representative filters, stale-selection removal, ignored-filter refusal,
  and 320px Persian layout. The host required a temporary single-process Chromium
  launch because its default zygote/GPU launch failed before page creation; this
  temporary runner configuration is not part of the product.
- This host has no C compiler, so local Go race could not start. Race verification
  belongs to CI for the final published head; older green CI is not evidence for
  these newly added files.

The 5,000-row fixture adds representative-scale synthetic coverage, but it is
not representative production data or operator acceptance. It does not establish
a legacy-unlimited policy, final counter custody proof, positive imported
subscription delivery, or persistent LAB installation. The source-local signed
handoff and the imported-client quarantine are unchanged.

## Installed synthetic M2 import and rollback acceptance — 2026-11-03

The exact verified PR artifact remained installed only on the isolated PRO LAB as
HUB/backup revision `8468374354d8300958cde036cefb1a320baecc30`. Before this
acceptance, backup `a388f1601e7db4838d7d2dce624f85c6` (archive SHA-256
`142136594f35eb60feaa5c89fb7ec6fcac9673870abb86ee8ba0300f8fcbe040`)
was already compatibility-inspected and retained as the clean rollback point.

The deployed Nginx allowlist initially rejected the new owner routes even though
the staged HUB capability was healthy. The installed business-management snippet
was the pre-M2 version. The exact reviewed repository delta added only the five
method-restricted Dark Restore route families. The resulting installed snippet
matched repository SHA-256
`04c2c9f82ad93b972fdf13475db89f36ab2d48439640efcb5d24d5adcd335055`;
`nginx -t` passed and a reload left both services at `NRestarts=0`.

A private, finite-volume, single-customer synthetic SQLite fixture was staged
with source SHA-256
`cf985298acf4896f6e1296615fa7a8b267a8c352c28559ce672e0111078a1f5d`
and canonical review SHA-256
`8ac27cdf50c3b7c5975bd909b76ef9472bde081ba1a1565af15fb74878f12a5b`.
The server review preserved UUID
`01234567-89ab-4cde-8fab-0123456789ab`, expiry `1893456000`, a
5,000-byte entitlement and 310 historical bytes. The source hash was unchanged
before and after import.

Installed owner API acceptance then proved:

- Quote reported one importable row, zero blocked rows and no credit, usage-reset
  or Xray change. Commit receipt
  `restore-7e0d4d0e-5a36-4efc-8e44-202611030001` created exactly one
  disabled client and sealed one legacy event.
- Repeating the same request returned the same receipt with `replayed=true`.
  A different event for the consumed source returned HTTP 409. Directory totals
  became exactly seven native and one Dark Restore client; no representative
  balance changed during import.
- Two disposable LAB representatives exercised two successive owner assignments.
  Both assignments reported `balance_changed=false`,
  `usage_reset=false` and `credentials_changed=false`. After the final
  assignment, the previous representative received HTTP 403 and the current
  representative received HTTP 200 for the imported client.
- A 512-byte renewal by the previous representative was refused. The current
  representative's renewal succeeded, increased only the entitlement from 5,000
  to 5,512 bytes and reduced only that representative's synthetic allowance from
  4,096 to 3,584 bytes. Original representative balances remained 9,900 and
  9,975 bytes. UUID and expiry were unchanged; subscription and customer traffic
  remained unchanged.
- The installed usage response kept historical bytes `310`, post-migration
  bytes `0` and combined bytes `310` separate. It kept
  `source_epoch_settled=false`, `cutover_verified=false`, live collection
  disabled and the imported client ineligible.
- A real HUB restart preserved one receipt and one imported identity. Receipt
  lookup and replay after restart succeeded without a duplicate.
- A real 320px Chromium session opened `/clients?view=restore`, loaded the
  approved server snapshot, displayed the customer and historical usage in
  English and Persian, produced no workspace mutation or page error and had no
  horizontal overflow.

Post-import backup job `12d4974fecdcad1f7506aba3e3032453` succeeded. Its
archive SHA-256 is
`86a0c6a843e7ba5883febbf5f067550ed31c9309556c9c125d57816851d32053`.
Compatibility inspection recorded eight clients, seven representatives, one
import receipt, one import entry, one sealed event archive and one historical
group row.

The Stage flags were then removed before recovery. Official restore job
`d801ee2d1fc5efcf5a10f768a1f95440` restored the retained clean archive and
reported `state=succeeded`, `phase=complete`, 64 verified tables and
`data_restored=true`. The installed new binary stayed active. The synthetic
client and disposable representatives disappeared; the owner API again returned
exactly five representatives, seven native archived/disabled clients and zero
Dark Restore clients with the original balances.

A fresh post-restore backup job `7ca540d9cd04f486bf2de40327b27fa2`
succeeded. Its archive SHA-256 is
`5fb0c74330081d12738c94adc4dc0f173fecb26b8ae49b6fa9b4d2f9802bdf94`.
Inspection recorded schema version 8, fingerprint
`7631320fa57eaa06758d3c013aa0bae2aff90c178c1e2c41695cc415f756b401`,
five auth users, five representatives, seven clients, two groups, three
memberships, eleven events, seven usage accounts/seeds, two ownership receipts
and three ownership entries. Import receipt/entry counts were zero. Both HUB and
backup worker remained active with `NRestarts=0`; health remained isolated with
the data plane disabled.

This closes installed synthetic finite-volume import, idempotency, assignment,
current-payer renewal, restart, backup and planned rollback acceptance for the
reviewed draft artifact. It is not operator acceptance of real legacy data.
Exclusive source inventory/retirement and custody proof, the legacy-unlimited
policy, authoritative final counter settlement, positive imported subscription
delivery after settlement, representative production-data acceptance and
independent scope review remain open. Stage is inactive after rollback. No
production HUB, users, representatives, bots, DNS or Nodes were touched.

## M4/M5 deployment allowlist correction — code checkpoint

Installed acceptance exposed that the LAB bundle did not carry
`ops/nginx/business-management.conf`; the operator therefore had to copy the
reviewed Stage/Import allowlist independently. The same shipped snippet also
omitted the signed-handoff, readiness and attach routes even though HUB and UI
implemented them.

The bundle builder now includes the business-management snippet and CI requires
the packaged file to exist. The snippet adds only exact owner HTTPS routes:
GET/HEAD for `handoff` and `handoff/readiness`, and POST for
`handoff/attach` with a 4 KiB body limit. The real disposable-Nginx acceptance
now covers every Dark Restore Stage, quote, commit, receipt, history, recent and
handoff route, rejects wrong methods, suffixes, malformed receipt/history IDs and
oversized attachment bodies, and verifies the original Host reaches the backend.

The local real-Nginx proxy test, Python compilation, bundle shell syntax and
packaging assertions pass. This is code and local-test evidence only. It is not
yet a green final-head CI result, a new artifact, an installed handoff route or
positive signed-settlement/subscription acceptance.

## Current-head race timeout correction — code checkpoint

Managed Inbound run `41567644254`, job `125693072970`, reached the explicit
ten-minute Go test deadline while
`TestDarkRestore5000OfflineCustomersImportReplayAndRecovery` was still running.
The log contains neither a failed assertion nor a race report; the complete
business package had already consumed the bounded 600 seconds under race
instrumentation. This is a real failed current-head check and the artifact from
that head is not eligible for installation.

Both independent complete-business race invocations now retain their existing
20-minute job limits but give the Go test process a bounded 15-minute deadline.
No test, fixture, assertion or package is removed or split. YAML parsing,
workflow-command assertions and `git diff --check` pass locally. A new head must
complete both race jobs and all other current-head checks before its artifact can
be considered for LAB installation; this correction is not yet CI evidence or
an installed change.


## M5 durable evidence, budget and subscription transition — integrated code checkpoint

The integration based on PR #137 foundation head
`9fa5c1912244086991cce85d2eba168148a1d4a0` adds the transaction-bound M5
business gate without weakening the existing pure contract. This section records
code and local-test evidence only. It is not, by itself, current-head CI,
artifact verification, merge, LAB installation or operational acceptance.

The evidence contract requires an independently pinned inventory, one
signature-verified handoff for every declared source, and a second canonical
document signed by a separately pinned coverage authority. The approval also
binds the exact destination-state snapshot, native-baseline snapshot, immutable
import receipt, configured destination authority and independent
billing-attribution digest. Verification records the assertion but cannot prove
that an undeclared production source does not exist.
`global_source_custody_verified` therefore remains false.

Finite-volume budget semantics are overflow-checked: current entitlement minus
the imported historical baseline, all declared attributable source-epoch
deltas, and native destination usage. Measured zero is distinct from unknown.
Baseline completion is a separate owner-only transaction and accepts only a
disabled finite imported identity with a signed settlement, an existing zero
native account, zero holds and no native activity. Later paid renewal changes
only current entitlement; it does not rewrite history or the prior payer's
ledger. Legacy unlimited remains blocked until valid entitlement evidence
exists. New unlimited orders retain the agreed `months × user_count` policy.

The owner-only `DRM5` schema stores immutable evidence and final receipts.
Opening a transition transactionally re-reads every destination pin and writes a
domain-separated transition hash. The issuer guard rejects assignment,
entitlement, expiry, archive/state, receipt or counter drift after approval.
Finalization re-reads the same snapshot and binds the exact authoritative
handover delivery revision, scope, snapshot ID, event ID, snapshot revision and
nanosecond timestamp to that transition. Activation rechecks the exact tuple.
Import, attachment, baseline, evidence and finalization do not debit
representative credit.

Synthetic positive coverage includes baseline completion, evidence replay, 14
settled historical bytes, 1,000,000-byte entitlement, 999,986-byte remaining
budget, controlled issue, rejection before delivery, post-boundary delivery,
finalization, activation, history/post-migration separation and backup/reopen
validation. Negative coverage includes wrong pins/signers, missing/extra/
duplicate/unverified handoffs, incomplete assertions, stale destination/native
snapshots, state drift after approval, unknown baseline, holds, archive, expiry,
exhausted budget, legacy unlimited and stale/simultaneous delivery. The
synthetic delivery row is not proof of a real route, production subscription,
inventory completeness or physical source retirement.

The exact integrated non-race worktree passed:

- `./internal/darkrestore` in 5.237s
- `./internal/business` in 197.000s, including the 5,000-client suite
- `./internal/backupacceptance` in 3.565s

The combined log is
`/var/tmp/darkrestore-m5-integrated-tests-20261103.log`, SHA-256
`5fd069dabee5eb2e0d9f08b7ee9431967928db2e159bd06615668c960cb56d96`.
This host has no C compiler, so race authority belongs to CI for the exact
published head. CI from any predecessor must not be attributed to this
integration.

The first published integration head
`353c76b8a0f147d53ba3298ac4ff722c07c91a6d` exposed one real current-head CI
failure in Foundation job `125768941821`. All three repeated real-settlement
runs returned the new fail-closed readiness state correctly: declared inventory
coverage and independent assertion were absent, the native baseline was
incomplete, delivery was absent and no final receipt existed. The older test
still expected the removed single `global_source_custody_not_proven` label and
omitted the new final-receipt blocker. The follow-up changes only that assertion
to require every explicit blocker and false evidence flag. Local API compilation
and the focused test command pass; exact follow-up CI remains authoritative.

PRO LAB remains on revision
`6af62c09f4d2d6fcb089a7d90ec0b5687bb986ba`; no LAB or production resource
was changed.


## Independent native baseline strengthening (source/test checkpoint)

Fresh inspection found PR137 at `11830a42376e5abdc234f351a0d6752b02c3dc42`
with 19/19 checks green, Draft/open/unmerged. LAB HUB and worker are active on
`174af74c18d33d4366f4ae212060ddbbc84daa3c`, `vcs.modified=false`, zero restarts.
That install and its closed gate are predecessor acceptance, not validation of
this new change.

The zero-activity baseline completion shortcut is closed. A separately pinned
native-supervisor lifetime measurement/fence receipt now supplies the explicit
native evidence contract. Preparation is read-only; receipt persistence and the
baseline flag commit atomically with source coverage and the signed approval.
Signatures establish claimant identity, not operational truth or absence of
undeclared runtimes. Only synthetic authorities/measurement/fence anchors are
used in tests. No real native evidence or production fact has been asserted.

Tests exercise zero and nonzero signed measurements, independent pin mismatch,
tampering/canonicalization, unknown coverage, overflow, expired/future fence,
foreign/stale identity/state, holds, legacy unlimited, unauthorized preparation,
rollback, immutable replay, reopen and supported v16 offline backup validation.
The v15 handover fixture is outside the offline backup allowlist by design;
its initial offline rejection was isolated to that fixture version and the
allowlist remains unchanged. Existing financial rows/history are retained.
No related installed binary or physical restore has been changed by this work.

Final local candidate checks passed on the unchanged Go source: darkrestore
5.544s, complete business 204.018s and backupacceptance 2.819s, plus vet for
business/darkrestore and diff checks. Local race is not claimed; CI is the race
authority. Publication, CI, installation and installed acceptance are separate.


## Native eligibility budget enforcement (isolated source checkpoint)

An optional exact-DDL native-ledger extension reserves settled source history
against current entitlement while preserving separate native counters and wallet
history. M5 opt-in enables it and opening commits its immutable receipt binding
with the signed native proof and baseline flag. Actual native eligibility now
closes at the remaining budget; quota renewal does not charge history twice.
The real ledger boundary fixture and supported v16 backup/reopen fixture pass.
Predecessor M5 datasets with evidence but no reservation require a separate
reviewed migration; this change refuses to fabricate/backfill their history.
This is source/test work, not Merge, installed acceptance or production evidence.

Final exact-source local checks passed: usageledger 15.708s, darkrestore 5.204s,
complete business 251.351s and backupacceptance 2.978s, plus vet and diff checks.
An initial full run found pointer-identity inequality in an existing financial
rollback assertion; Account now keeps value equality while representing unknown
remaining budget accurately in JSON. The original assertion was not weakened.
Synthetic delivery-journal insertion remains a model test, not real prepared
subscription rendering or installed delivery acceptance; that integration is next.

## Exact-head CI fixture stabilization (source/test checkpoint)

PR137 head `7d36c2bdf2868f2f1d7bd41eec5a432ff28e3905`, tree
`cd869bf44baa6a908de69b25f4f4d7dd3774be55`, fixed the installed-browser
ownership expectation after a 1 MiB renewal. Its real Chromium ownership job
`125909188956` passed. The separate Foundation business race job
`125909188923` then exposed a second-boundary ambiguity in an older volume
refund safety fixture: the synthetic native delivery sampled the client's
second-resolution creation timestamp instead of the immutable usage seed's
timestamp. Under a boundary crossing, the row could precede the very authority
whose later issuance the fixture intended to model.

The fixture now reads the immutable seed timestamp and places the already-expired
synthetic issuance strictly after it. Product refund policy is unchanged:
missing final usage remains fail-closed, lease expiry is not zero usage, and no
credit is issued from an unknown tail. Both the original and corrected focused
test passed 100 consecutive non-race runs on the isolated host; the corrected
full business package and exact-head CI remain the publication gates. This is
test stabilization only, not Merge, installation, cutover, subscription delivery,
inventory completeness or production evidence. PRO LAB remains on the accepted
closed predecessor revision
`174af74c18d33d4366f4ae212060ddbbc84daa3c`.

## Canonical prepared delivery (isolated source checkpoint)

The prepared-not-applied Store path now renders actual configuration content
from an authenticated versioned managed plan. Its receipt binds the canonical
content hash, revisions/profile authority, UUID/financial state and M5 transition.
It preserves the native renderer's runtime eligibility gates; preparing never
enables a client, binds a runtime or claims a verified traffic path. Source and
native facts in tests are explicitly synthetic; no physical retirement or
undeclared-source absence is inferred.

The new v16 positive/negative fixture passes genuine rendering, owner scope,
rollback, unchanged balances/generation, separate pending activation and official
offline backup/reopen. Profile, current-payer identity or paid-quota drift blocks
stale preparation and activation without changing representative balances. The
preliminary full run rejected one existing health fixture after 31s under
concurrent browser/build load; its unchanged bounded test then passed
three isolated runs in 0.35/0.39/0.34s. No lease or assertion was weakened; the
final candidate receives an exclusive complete regression run below.

## Exact-head browser poll contention correction

Native Metering CI for public head
`0ad749e064a1f82b1b9820fa115b0591e3dbb631` exposed a fixture defect:
the background signed Agent poll treated a transient
`503 business_store_unavailable` during a concurrent browser/SQLite write as
a terminal protocol failure. The fixture now mirrors the production transport
retry boundary only for that exact decoded response, with a five-consecutive
response ceiling. Authentication failures, stale/conflicting observations,
unexpected command delivery and every other status still fail closed. Metering,
promotion, accounting and runtime-admission product logic are unchanged.


## M5 audited post-opening refresh (isolated source)

A new optional exact-DDL refresh journal preserves the original inventory,
coverage approval, native evidence and historical-budget reservation while
allowing one-at-a-time, already-committed renewal or ownership changes to be
attributed before the first prepared delivery. It never mutates business state.
Synthetic positive fixtures cover current-payer renewal and owner assignment;
negative fixtures cover unknown cause, representative scope, combined drift,
failed INSERT rollback, replay and attempts after final delivery. Prepared
content remains not applied/path-unverified and survives v16 offline backup and
reopen. This is isolated source validation, not installed acceptance or real
inventory/retirement evidence.

## M5 native admission evidence boundary (isolated source)

The imported-client execution path is deliberately distinct from the general
plan-promotion path. A new owner-only, read-only projection now binds the exact
prepared content/profile authority, transition, signed process birth, owner
admission grant, registered process epoch and the first accepted zero usage
checkpoint. The zero checkpoint is reconstructed from the canonical epoch and
must match the immutable sequence-1 receipt plus its zero delta.

Projection and independent validation are separate. The trusted pin set includes
client, transition, prepared content and authority, plan, birth/admission
receipt digests, epoch and initial report, plus the complete evidence-document
digest. Missing receipt, foreign owner, changed pin or repinned partial document
fails closed. Admission evidence always reports `native_apply_recorded=false`,
`serving_health_recorded=false` and `authorizes_mutation=false`. It cannot clear
the native-apply readiness blocker, enable an imported client, publish a
subscription or claim long-lived Agent supervision.

The owner browser surface exposes this projection only through exact GET/HEAD
`/api/v1/workspace/dark-restore/handoff/quarantine/admission/evidence`. It
re-verifies the pinned handoff before resolving the imported identity and never
accepts pins or authority booleans from the browser. The production Nginx
snippet now explicitly permits this read-only path and the two existing bounded
POST quarantine/admission paths; every suffix, wrong method and body over 4 KiB
is denied before the HUB.

Synthetic tests cover pre-admission/pre-receipt refusal, owner scope, exact
replay, positive independent validation, foreign report pin and attempted
self-promotion to native apply. The complete non-race business package and
backup-acceptance package pass on the isolated candidate. These local receipts
say nothing about undeclared sources, real inventory completeness, source
retirement or production-serving health.

### Agent CLI startup regression acceptance

The real Agent entry point had an older Final TCP guard that rejected the new
exclusive Dark Restore supervisor before it started. The guard now permits the
explicit Dark Restore runtime; existing runtime exclusivity checks still apply.

The isolated CLI probe starts actual HUB and Agent executables, drops both to
UID/GID 65534, uses a private HUB chroot and a loopback-only network namespace,
then exercises owner login, prepared URI delivery, quarantine approval, Agent
birth, owner admission, actual imported-URI traffic, lease renewal and SIGTERM
settlement. It verifies a sealed epoch, native usage and unchanged representative
credit. This synthetic binary acceptance is also required in the final-core CI
quarantine script. It does not install the candidate on persistent LAB or close
production migration acceptance.


## M5 closed native cycle evidence (isolated source checkpoint)

A new owner-only read-only projection binds one already-sealed imported-client
epoch to the prior admission evidence, transition, prepared content/profile
authority, plan, terminal report and exact per-epoch native byte deltas.
Projection requires a unique quiesced terminal receipt, a matching sealed ledger
cursor, one exact client binding and nonzero observed traffic. An open,
uncertain, zero-traffic, foreign-client or tampered epoch remains fail-closed.

Independent pins cover the complete evidence document, admission evidence,
transition, prepared profile, epoch, final sequence/report/receipt time and both
cycle counters. Replays are byte-stable. The result deliberately keeps
`native_apply_recorded=false`, `serving_health_recorded=false` and
`authorizes_mutation=false`: a completed past cycle is not authority for a
future apply, subscription publication, activation, inventory-completeness
claim or source-retirement claim.

Synthetic tests cover a positive 5-byte uplink/7-byte downlink sealed cycle,
pre-final refusal, owner scope, exact replay, wrong terminal pin, attempted
self-authorization, state tampering and a separately sealed zero-traffic cycle.
Exact local non-race validation passed: focused closed-cycle tests (1.954s),
darkrestore (3.953s), complete business (225.441s), backupacceptance (3.121s),
vet and diff checks. This is source/test evidence only until publication,
current-head CI, artifact verification and installed acceptance are separately
completed.
