# DARK XRAY PRO — Representative Global Directory V6

## Why
Client global search V4 and atomic nonfinancial bulk state V5 are already
shipped (PR #50/#51). Representatives still had only an unfiltered 50-row
list without exact totals or per-account client record counts.

## Change
- Existing authenticated owner-only GET /api/v1/workspace/representatives now
  accepts optional q, state (all/enabled/disabled), sort
  (id/newest/name/volume/unlimited), and limit/offset (1–200/0–1,000,000).
- All parameters are single-valued, allowlisted and bounded. Free-text searches
  by account ID or display name are literal substrings, not SQL wildcards.
  Sort expressions are fixed, with ID tiebreaks for stable pagination.
- The new database method uses one read-only SQLite transaction for the
  exact total and page. Per-representative saved client_count and archived_count
  come from real client records via indexed correlated queries, not simulated
  online sessions or traffic accounting. No credit table, ledger, archived UUID,
  application receipt, Xray listener or quota is modified.
- Browser directory is independent of the 50-row representative picker used
  by Clients/Groups. It supports account-scoped page persistence and global
  search, status, sorting and mobile RTL. Malformed/stale/error pages never
  leave former representative action buttons clickable.
- Clicking Select for credit/login only fills the existing owner form; money
  and credentials still require the prior, explicitly confirmed, CSRF-bound POST.
- Old GET pageParams public contract remains backward-compatible for
  limit/offset. The private bearer admin route is untouched.

## Verification / release gates
1. Go real SQLite: 70+ accounts, exact count/pages, Persian and %_ literals,
   actual client/archive aggregates, stable sort, disabled filter, invalid
   queries rejected without credit/billing mutations.
2. Browser auth: owner only; anonymous, representative and Node Agent denied.
3. Chromium: after page 50, global filter, sorting, form-only selection,
   Persian RTL 320px and fail-closed malformed responses.
4. Strict Vite/TypeScript, Foundation CI race/vet/full Chromium and Native
   Metering Acceptance green before merge.
5. Isolated NEW PRO HUB only: snapshot current installed commit and preserve
   prior known-good release. Deploy only matching merged SHA, verify HTTP,
   service health and release markers, rollback if verification fails.

## Explicitly unfinished
- Representative activation/deactivation with a guarded revocation contract,
  account activity metrics, per-rep plan marketplace/catalog, advanced credit
  history filters and reports.
- Native client editing/IP-limit changes and charge-audited atomic bulk renewal.
- True online/IP/session history from traffic-connected Xray, subscriptions,
  Telegram bot storefront and payment contracts, legacy migration.
- Private owner-only US canary / metering acceptance; do not turn on US Xray.

This does not authorize any change to the legacy production HUB or fleet.
