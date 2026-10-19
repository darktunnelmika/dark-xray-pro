# DARK XRAY PRO — Atomic Bulk Renewal & Credit Preview V7

## What changes
PR #51 offered safe bulk state changes, but no atomic billable renewal.
V7 adds one to fifty explicitly checked native PRO clients from a verified
authenticated client directory page. It neither deletes users nor activates
Xray or changes old production services.

User-facing modes:
1. `Renew / top up` in the bulk action selector: additional days 0–3650,
   volume in GB, and a required audit reason.
2. `Review exact charges` calls the owner/rep cookie + CSRF POST
   `/api/v1/workspace/clients/bulk/extend/quote`. A read-only SQLite
   transaction returns exact decimal-string, per-client cost, per-rep credit
   balance/cost, 180-second optimistic snapshot fingerprint and estimated
   expiry. This does NOT change credits or client records.
3. `Confirm and renew all` requires browser confirmation and calls
   `/api/v1/workspace/clients/bulk/extend/commit`. One SQLite transaction
   validates the current snapshot, applies all charges and client extensions
   and inserts immutable operations. Any failure rolls back the whole set.

## Financial invariants
- Uses the same cumulative user-month formula as the existing ExtendClient:
  newUnits = ceil((oldDays+addedDays)*slots/30) - chargedUnits.
  At 5 slots: 30 days cost 5, +15 days cost 3 more, +15 days cost
  2 more. Splitting the renewal cannot overcharge.
- Volume credit debit equals newly added volume, never duration-only.
  Unlimited clients cannot request added volume. Volumetric plans can
  retain IP-count 0 (unlimited); no resets or auto-refunds.
- Owner-created clients are exempt from representative credit debit;
  charge counters remain consistent with existing single-client semantics.
- Owner can select different representatives in the same atomic batch;
  each representative's credit is checked and debited independently but
  no participant is committed if another lacks credit. A representative
  is tenant-scoped in both HTTP handling and inside the transaction.
- Archived clients, disabled representatives, unknown plans, 51+ items,
  duplicate IDs, stale quotes, missing/unknown JSON keys, insufficient
  balances or invalid reason fail closed.
- A lost HTTP acknowledgement retries with one parent event ID for the
  complete sorted set, plus immutable deterministic child event IDs.
  Modified selections/amounts/reasons with a reused event ID return 409.
  No changes to underlying credit/usage ledger schema.

## Runtime scope
All previous client UUIDs, usage ledger, groups, subscriptions, deployment
flags, Agent credentials, historical records and old DARK XRAY services
are preserved. Renewing an account is **not** applying it to real Xray.
USA Xray stays off until an independently approved real metering acceptance.

## Release gates
- Go real SQLite/API tests: exact user-month differential, 50 bounds, owner,
  rep and cross-tenant isolation, CSRF/origin, stale/changed balance,
  insufficient balance, archive, expiry, replay/collision and no data loss.
- Playwright Chromium: preview cannot bill, cancel cannot bill, confirmed
  batch can bill once, changed price/selection hides confirmation, Persian
  320px and existing operator regressions.
- GitHub Foundation CI, Go race/vet, pinned real Xray, bundle and separate
  Native Metering Acceptance must ALL be green on PR and merged main.
- Install only the NEW LAB with private consistent SQLite backup and
  existing systemd override + Nginx allowlist snapshot. Keep V6 release,
  exact merged SHA and auto-rollback on bad HTTP/service/authorization.
- Nodes (#1) and full Backup/Restore (#4) remain hard release gates.
