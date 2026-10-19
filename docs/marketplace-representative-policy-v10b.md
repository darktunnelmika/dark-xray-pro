# DARK XRAY PRO — V10b Representative Marketplace Policy

V10b extends the isolated NEW PRO LAB V10a owner-defined catalog. It does
NOT enable checkout, order/payment, client creation, credit debit or refund,
subscription activation, Xray Apply, Telegram migration, or OLD HUB changes.

## Owner controls

New per-representative policy defaults to NO product management or publish:
- manage_products and can_publish are independent owner-granted capabilities
- volume/unlimited product type allowlist
- explicit minimum and maximum integer Toman prices
- maximum duration days, IP/user seat count and volumetric quota bytes
- immutable audit reason and event ID, expected revision and session revocation

Owner sets the price floor and ceiling; a representative cannot alter them,
create another representative's product, lower price under the owner floor,
exceed days/bytes/seats, or publish without can_publish. Price is merely
catalog metadata; existing credit and user-month accounting is untouched.

Owner GET/POST catalog-policy uses cookie/CSRF/Origin and strictly complete
required boolean+numeric fields, so omissions cannot zero grants.

## Representative experience

- Normal product list continues to show ONLY the representative's own
  PUBLISHED products (unless V9 view_clients permission is disabled).
- Representative with active manage_products sees a separate authenticated
  manage listing of their own drafts/published/archived, and may create/edit
  own products with owner-defined bounds enforced in SQLite write transaction.
- Publish is separately gated by can_publish. Archived products are terminal.
- Revocation blocks draft management immediately and clears representative
  login sessions. Previously published products remain visible; customer
  access and traffic accounting are not changed.
- User interface shows manual product names, durations, 0=unlimited IP
  for volumetric offers, 1-1000 paid seats for unlimited, bounds and
  320px Persian RTL; disabled checkout is explicitly labeled.

## Technical implementation

- Additive catalog_rep_policies and append-only catalog_policy_events tables;
  owner change is one atomic SQLite write + revocation of representative
  auth sessions. Financial and client tables are NOT modified.
- Business schema version remains 7 and V10a rollback is fail-closed:
  previous V10a binary ignores these policy rows but still forbids
  representative catalog POST, so rollback cannot accidentally expand access.
- Actor identity binds to server session and is embedded in idempotent event
  fingerprint. Product scope, current policy, account state and requested
  specifications are revalidated INSIDE the same write transaction, closing
  policy revocation races and avoiding trust in hidden UI inputs.
- Owner and representative roles are checked twice (browser auth + database).
  No Agent TLS, remote admin key, Node credentials or secrets in catalog API.
- Owner policy events are immutable, and their IDs cannot collide with prior
  client, representative, credit, bulk renewal or product event receipts.
- Catalog change audit retains original before/after JSON, never deletes
  historical products or customers, and never guesses billable online traffic.

## Release gate

PASS real SQLite business/API, CSRF/Origin/owner/rep isolation, explicit
permission and 320px Chromium UI, Go race/vet, TypeScript/Vite and real
pinned Xray lab smoke, Native Metering Acceptance, for both the PR and
merged main SHA. Then pin exact NEW PRO LAB binary, snapshot private
SQLite/Nginx/systemd, retain V10a for rollback, smoke auth & NO Xray US
listeners. DO NOT touch OLD HUB or migrate real customers.

## Remaining scope — V10c

Real order/purchase ledger and price rule, seller/storefront checkout and
payment reconciliation, exactly-once representative credit + customer
creation/renewal, cancellation/compensating states, bot and Mini App orders
and eventually native subscriptions. Requires separate approved financial
contract and verified actual Xray canary. P0 Nodes #1 and full multi-GB
Backup/Restore #4 remain hard pre-cutover blockers.
