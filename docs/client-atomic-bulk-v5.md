# DARK XRAY PRO — Atomic Bulk Client Actions V5

## Operator problem

Advanced Xray panels such as 3x-ui allow users to select multiple clients
and perform a grouped action. In NEW PRO until V5, owner and representatives
had to disable, enable or archive records one by one via separate lifecycle
forms. Naive loops of individual requests can partially succeed during
outages, hide failures, overwrite previous accounting or cause ambiguous
billing states. This implementation preserves immutable records.

## Scope and UX

- Up to **50 individually selected, verified nonarchived client IDs** from
  one authenticated page. The page may itself display 25, 50, 100 or 200
  filtered rows; selecting visible checks only the first 50 permissible
  items. Page/filter/representative changes clear selected IDs.
- Operator explicitly chooses disable / logical enable / archive and
  must provide an audit reason. Every operation requires an explicit
  confirmation; archive warns that UUID, spent credit, group membership
  and historical bytes **remain intact**.
- Authenticated owner may operate on the explicitly selected clients.
  A representative is restricted to their own representative ID, both
  before the transaction and again within the transaction.
- NO delete, counter reset, automatic subscription issue, Xray Apply,
  external Node listener or billing refund is performed by these actions.

## Atomic, retry-safe API

POST /api/v1/workspace/clients/bulk/state

Request includes immutable event_id, explicitly selected client_ids (1–50,
unique), action (disable/enable/archive) and audited reason. This remains an
owner/representative cookie + strict same-origin + CSRF-only POST; no
Bearer, Telegram/Node API or representative impersonation.

The private Store creates ONE SQLite transaction for the full validated
selection. Every client must exist, belong to the permitted representative
and satisfy the requested state transition. If ANY member is not eligible,
the entire transaction is rolled back; no partial state or audit.

Uses the already immutable client_operations records: sorted first
client carries the unique parent batch event ID with a hash of the
ENTIRE sorted selection, action and reason; each other ID receives a
deterministic HMAC-free SHA256-derived child event ID. Repeating the
identical complete batch returns a replay without additional writes,
even when the first HTTP acknowledgment was lost. Reusing an event ID
with a changed selection, action or reason is a 409 conflict, even if
both selections are disjoint. The existing client UUIDs and quota
balance ledger are never edited.

Enabling one client only moves its logical business record back to
pending_node_apply. It does NOT bypass the first-zero-accounting-receipt
barrier or start its Xray listener. Archived records retain all history
and are not automatically deleted.

## Safety and tests

- Real SQLite Go acceptance: mixed-representative batch rollback,
  1–50 client bounds, 2-client all-or-nothing update, exact replay,
  changed selection refusal, archive/enable transitions, immutable
  operation audit, no credit refund and UUID preservation.
- Cookie/CSRF authenticated API: representative cannot modify another
  tenant, anonymous/CSRF/cross-origin/Agent denied, owner scope validated.
- Chromium browser: explicit checkbox selection and archive
  confirmation, first 50 of a 200-row page, 320px Persian and zero API
  mutations before confirmation. Ambiguous 503 retries preserve ID only
  for identical payload; changed reason rotates event.
- Full Foundation CI race/vet/Chromium, pinned real Xray and Native
  Metering Acceptance MUST pass before NEW PRO LAB upgrade.
- Nginx exact POST-only owner/representative route required, matching
  existing conservative API proxy authorization.
- No legacy DARK XRAY production change and no USA PRO Xray activation.

Bulk extension/renewal, automatic renewals and group subscription
publication are **separate** financial/data-plane milestones, not
implicitly available with logical bulk state updates.
