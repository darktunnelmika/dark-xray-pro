# DARK XRAY PRO — Representative Account Controls V9

## Scope and user experience
V9 upgrades the isolated NEW PRO representative management workspace without
changing the legacy DARK XRAY HUB. An authenticated owner selects a
representative in the server-wide V6 directory, sees its true saved client and
immutable transaction counts, and can review/change:
- Native account display name (UTF-8)
- Login suspension and forced session revocation (NOT customer shutdown)
- Seven granular access grants: client view, create, renew, edit, logical
  state, group management and private finance view
- Append-only owner-control audit with reason, previous and new settings
- Real summary: saved and archived clients, created clients, recorded
  renewals, profile edits, owner credit adjustments, available balances.
  These are **saved record counts**, not online IP/session or billable traffic.

Owner account cannot be targeted. There is no representative Backup, Node,
Inbound, API-key or privileged infrastructure permission. Nginx stays
deny-by-default for all unlisted API paths.

## Business safety
The existing representatives.enabled, client/UUID/expiry/consumption,
original credit ledger, groups, Node bindings, direct/tunnel/swap traffic and
Xray runtime NEVER change in a representative account control mutation.
Suspending login is not the same as suspending all customer service.

A single SQLite write transaction verifies the expected representative
revision, updates the display name and native access controls, purges *all*
representative sessions, persists one immutable audit event and commits
atomically. A lost acknowledgement reuses the same event_id; altered
payloads and other financial/lifecycle event-ID collisions are rejected.

Fine-grained rules are checked in every browser workspace API invocation,
not only hidden in the React UI. Representatives cannot grant themselves
rights, inspect other rep control history or modify owner-only services.
Their own activity report is available only if view_finance is allowed.
Unknown future workspace endpoints default to owner-only, not open.

The existing login credential (Argon2id) is not changed. A suspended rep
cannot log in and all old cookies are invalid. Login rechecks suspension
inside the same write transaction used to create a session, preventing an
interleaved logout/disable race.

## Rollback fail-closed
Schema version remains 7, with only additive representative_controls and
immutable representative_control_events. V8 binaries still understand the
original business ledger. V9 deliberately writes auth_users.disabled=1
for every login-suspended OR role-restricted account; V8 will therefore
deny access entirely rather than reopening previously forbidden actions.
V9 explicitly reads the new representative_controls row as its own
authoritative login policy, allowing restricted but non-suspended reps to
log in and exercise only allowed operations. Re-enabling access requires
fresh sessions and an owner control revision.

This is the intended conservative downgrade behavior; do not clear the
disabled fallback sentinel automatically during rollback.

## Tests
- Actual SQLite: no client loss/credit change/customer stop; suspend/revoke,
  simultaneous login fencing, owner revision, replay event collision,
  immutable audit, Unicode names, independent rep controls and exact activity.
- Browser cookie/CSRF/roles: owner-only POST, missing CSRF, foreign Origin,
  previous cookie 401, limited new session passes only allowed operations,
  disallowed creates/group/finance 403, Agent never exposes owner controls.
- Responsive Chromium: selection, review-confirm, cancel, 320px Persian,
  real-count labels and malformed/cross-account response fail closed.
- Foundation CI: Go race/vet, TypeScript/Vite, pinned genuine Xray, Chromium
  and non-production bundle; plus independent Native Metering Acceptance.
  Both workflows must pass on PR AND merge SHA.
- Deploy only to isolated NEW LAB: private SQLite snapshot and existing
  systemd drop-in/Nginx allowlist copies, pinned build SHA, on-failure
  rollback to V8 and authenticated method smoke. No old HUB, real customer,
  US Xray listener, domain, bot or migration changes.

## Deferred
Product Marketplace V10, price/catalog management, finer-grained accounting
exports, genuine online/per-IP/HWID from Xray, and full migration parity.
Node Manager (#1) and encrypted multi-GB Backup/Restore (#4) remain hard P0
pre-cutover release blockers alongside #22.
