# DARK XRAY PRO — V8 Safe Client Profile Editor

## Scope
After V7's atomic bulk renewal, owners and representatives need a real
single-client editor. This adds a verified profile view directly under the
authentic client usage panel, not a client recreation/reset route.

### Allowed changes
- Manual Unicode display-name edits, up to 128 code points.
- Volumetric client: 0..1000 IP/User slots (0 means unlimited), changes
  incur **no new volume debit**.
- Unlimited client: existing paid seats may be **increased**, with the exact
  *cumulative user-month differential* (newDays x newSeats / 30 rounded up
  minus prior charged units) debited once. Decreasing paid seats is refused
  until a separate explicitly agreed downgrade/refund/carryover contract.
- Explicit positive additional days and/or new GB, exactly like existing
  ExtendClient. No amount is removed from an old plan.
- Combined edit commits all changes and representative debit atomically,
  after an independent server quote and explicit confirmation.

### Explicit limits
- Switching from Volume to Unlimited or vice versa is not attempted:
  the user-month/bytes ledger and committed native usage would need a
  reconciled plan migration, not a simple label edit.
- A single client cannot be silently moved to another representative,
  given a replacement UUID, reset for a fresh term or re-enabled after
  an operator-disabled state. Those are separately audited workflows.
- IP seat field is a stored policy. It is **not** live IP enforcement until
  the real Agent/Xray configuration is separately applied and verified.

## API contract
- POST /api/v1/workspace/clients/{id}/edit/quote
- POST /api/v1/workspace/clients/{id}/edit/commit
- GET /api/v1/workspace/clients/{id}/edits?limit=20&offset=0

Browser cookie scope, strict same-origin and CSRF POST, exact allowlisted
Nginx routing. Owner can manage permitted PRO accounts, representatives
their own only, double-checked inside SQLite transactions. Agent TLS
and owner-local bearer cannot issue browser edit mutations.

Quote is **read only**; it includes current complete Client and
Representative identities/balances in a SHA256 fingerprint and expires after
180 seconds. Stale debit/renewal/identity/expiry/disabled changes fail closed.
The commit matches a quote but applies any time anchoring to the actual
commit instant. No records are written until commit.

Repeated event_id with identical normalized payload replays the original
operation rather than charging twice. A reused ID with changed intent, or an
ID from a previous credit/client lifecycle event, is rejected.

## Audit and rollback compatibility
An additional private append-only client_profile_edits table stores original
and new name, seats, expiry, added days/volume, exact credit debits, operator
reason and event nonce. SQLite triggers deny updates/deletes.

No destructive existing-table rewrite and **no PRAGMA user_version bump**
from schema 7: the prior V7 server can open its unchanged existing client,
representative and credit data if a V8 release needs to roll back. Extra
V8 profile edit audit remains retained, though the old UI does not show it.
The Store checks the new event-ID namespace before old create/credit/extend
operations to prevent collisions.

SQLite usage ledger, original UUID, Client/Representative ID, group, native
runtime inbounds, disabled/archive state and old HUB/Node settings remain
untouched. Quota trigger already invalidates generation on added bytes.

## Release acceptance
- Real SQLite tests: representative debit / free volume IP / upgraded
  unlimited seats / old quota retention / Unicode name / expiry / owner
  exemption / invalid plan switch / downgrade refusal / insufficient and
  stale credit rejection / replays / immutable audit and schema 7.
- Authenticated API: 401/403/422, CSRF, tenant access, GET audit private,
  unknown field rejection, Agent isolation, receipt.
- Real Chromium tests: no writes until quote and confirmation, cancellation,
  invalidated quote after input changes, edit history and Persian 320px.
- Foundation CI + real pinned Xray / Native Metering Acceptance green on
  reviewed PR **and on merged SHA** prior to isolated NEW LAB installation.
- Snapshot private DB and systemd+Nginx exact allowlists. Pin new executable
  SHA, retain V7 binary and script, verify public POST 401 and GET 403,
  compare event/credit/client counts, restore on any health failure.
- OLD HUB untouched, USA Xray disabled and no public production cutover.
  Nodes and Backup/Restore remain separate P0 blockers (#1, #4, #22).
