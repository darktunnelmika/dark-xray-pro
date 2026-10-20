# Node Manager N1 — display name receipts

This increment implements **display-name management only** for independently
paired PRO nodes. It does not implement dependency inspection, retire/delete,
replacement activation, route apply, credential rotation, migration, Telegram
connections or traffic activation. Old HUB, operational nodes and USA Xray remain
untouched. The Telegram admin/broadcast roadmap remains in
`telegram-representative-upgrade.fa.md`; it is not a shipped bot feature.

## Contract

- Owner browser session only. GET `/api/v1/workspace/nodes/{id}/name`
  returns node identity, current revision, total audit count and latest 20 events.
- POST `/api/v1/workspace/nodes/{id}/rename` requires same-origin CSRF and exact
  `event_id` (32 lowercase hex), `installation_id`, `expected_revision`, `name`.
  The actor is taken from the authenticated user, never the request body.
- A new request must match the installation and current revision. Revisions begin
  at 1 and remain monotonic even after A → B → A. Names are trimmed valid UTF-8,
  1–80 code points, without control characters. Unchanged names are rejected.
- Retrying the exact event, actor and payload returns the original historical
  receipt, even after later edits. Reusing an event with different input conflicts.
  A receipt is not a claim about the current name: the UI reads fresh state.
- The registry lock serializes writers inside the one HUB process. Running two
  independent HUB writers against one registry file is unsupported.
- Display name and audit receipt are saved atomically in the same private file.
  If rename committed but directory sync failed, memory follows the visible file;
  an exact retry saves/syncs the state again before acknowledging the receipt.
  A 503 means unknown outcome, not "nothing changed".
- Node ID, installation, pairing timestamp, token hash and enrollment receipts
  stay unchanged. The method does not access clients, routes or usage stores.
- At most 10,000 total rename receipts, with the existing 4 MiB file limit.
  Capacity fails closed; no receipt eviction. Open validates contiguous revision
  chains and installation/name consistency; corrupt audit state is rejected.

## Browser behavior

Details load only when the owner opens the editor. A stale revision or unreadable
state locks editing. An ambiguous reply retains the exact request in memory for
explicit retry, locking payload edits. Refresh obtains current name/history before
allowing a new intent. There is no automatic mutation or stored browser secret.
Reload/navigation discards an in-memory pending intent: inspect current state and
audit before making a new edit. This is not persistent browser operation recovery.
Persian/English and 320px mobile layout are covered by browser tests.

## Upgrade and rollback

Older version-1 registry files remain readable and begin with revision 1.
`renames` is an optional top-level extension, omitted until first rename.
**Older binaries reject the extension once used.** Before deployment back up the
whole private registry plus all LAB state/config and validate a restoration copy.
Rollback must restore the matching pre-upgrade registry/state snapshot together
with the previous binary; never remove the audit field to make an old binary read
new state. Do not pair nodes or make business writes during a rollback window:
restoring an older snapshot would discard later writes. No deployment is claimed
by this document.

## Validation and next boundary

Race tests cover concurrent CAS, restart replay, ABA, request substitution,
identity preservation, bad input, failed persistence and corrupt audit. API tests
cover owner/representative/anonymous boundaries, CSRF, origin, strict JSON,
stale revision and credential non-disclosure. Browser tests cover unknown outcome,
identical retry, conflict refresh and read-error locking.

Next: read-only dependency inspection across inbounds, encrypted swap references,
route commands, accounting epochs/holds and replacement history. Unknown or
unavailable data must not be shown as zero. Even an empty inspection will not
authorize removal until an atomic lifecycle transition and rollback are built.
