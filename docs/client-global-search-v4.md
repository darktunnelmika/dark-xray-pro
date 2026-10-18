# DARK XRAY PRO — Clients Global Directory V4

## Scope

After the new PRO Client Sales V3 launch, the authenticated client search and
status filters still operated **only on the 50 records on the current page**.
This prevented finding client names on another page and hid expired, disabled
or archived records unless an operator paged through everything manually.
Other panels such as 3x-ui provide global client search and bulk tools.

This increment makes server-side global search a first-class, authenticated
feature. It does not authorize additional customer management, alter historic
accounting, activate Xray, publish a subscription or migrate OLD DARK XRAY
customers. No change to the old production HUB or Nodes is necessary.

## New API contract

GET /api/v1/workspace/clients, owner/representative cookie only:
- q: optional case-insensitive **literal substring** of client name or ID.
  Search does NOT treat SQL %/_ as wildcard.
- representative_id: owner may request one group/representative or all;
  representative may request their own ID only (or omit it).
- state: all, pending, disabled, expired, archived, other; precedence is
  archived > manually disabled > expired > pending.
- plan: all, volume, unlimited.
- sort: newest, oldest, name, expiry_soon, expiry_late; all sort keys are
  fixed SQL expressions with a stable ID tiebreaker.
- limit: 1–200 rows; offset: 0–1,000,000; defaults remain 50/0.

Responses include real page rows plus total, has_more, limit, offset, the
applied search/filter/sort and the authoritative effective representative.
Count and paginated rows use one **read-only SQLite transaction snapshot**.
The query is parameterized, rejects unknown/repeated params and invalid
values, controls output size, and preserves the existing Agent-only API
separation. Admin API query contract remains unchanged.

No paid/billable usage or live-online claims are synthesized. API failure,
cross-representative data or malformed page metadata cannot leave the
previous page clickable for client extension/archive.

## Operator UI

- Full-search input with 300 ms debounce and an explicit global label.
- Real status, plan, expiry/name/date sort and owner-entered representative
  ID filter (not limited to the visible first 50 representatives).
- 25/50/100/200 page-size presets; only the chosen page size and bounded
  offset persist per authenticated account in sessionStorage. No client
  names/UUIDs/search terms, financial data or credentials persist there.
- Search and status/plan/sort changes reset offset to page zero.
- Backend truth, not a partial-page filter or invented total, controls the
  current page and "Next". A stale filter response is never actionable.
- Mobile 320px Persian RTL and English LTR; compact customer cards and
  existing actual Usage view are preserved.

## Guardrails and future parity

Competing Xray panels offer bulk create/disable/adjust, renewal policies,
online/IP history, subscriptions, QR and client links. These are separate
functional milestones requiring properly audited, tenant-scoped operations,
tested process quiescence/usage receipts and active Node traffic.
Blindly deleting an expired client or resetting its usage would corrupt
immutable accounting and is **not** a legitimate feature-parity shortcut.

## Acceptance gates

- Go DB tests: >=126 real records, literal wildcard search, owner/rep scope,
  exact totals, 25/100/200 pagination, Persian, disabled/expired/archive,
  stable sort and rejection of SQL input.
- Browser owner/rep scoped API tests: cookies, auth, repeated/unknown keys,
  no Agent read, no cross-representative leak.
- Chromium: client beyond initial 50, 25/200 row settings, owner rep/sort,
  exact counts, Persian RTL, no credit mutation and fail-closed stale response.
- Go race/vet, existing real pinned Xray/Native Metering Acceptance and full
  Chromium regressions must all pass before isolated NEW PRO release.
- Preserve one prior HUB release for rollback; **never** change OLD HUB,
  new USA Agent, Xray listeners or private financial balances for this UI.
