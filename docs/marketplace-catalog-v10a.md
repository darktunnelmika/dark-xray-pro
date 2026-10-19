# DARK XRAY PRO V10a — Isolated owner product catalog

V10a introduces owner-defined plan metadata, not actual checkout,
orders, payments, credit movements, Node activation or subscriptions.
No existing OLD DARK XRAY HUB, customer, representative or Telegram
identity is imported or altered.

## Capability
- New /marketplace route for owner and representatives.
- Owner creates a draft product assigned to an existing representative;
  product fields: identifier, name, plan volume/unlimited, duration days,
  allowed seats/IP (volume 0=unlimited), gigabytes and owner-set toman price.
- Published offers must have a positive entered price and a valid plan.
- Owner edits/publishes/archives with expected product revision,
  immutable per-operation event ID, audit reason and before/after JSON.
- Archived products cannot be restored silently. Draft and archived
  products are never exposed through representative listing.
- Representative views only own published products, scoped in SQL,
  authenticated with existing V9 view_clients rights.
- UI explicitly warns that purchase and payment are DISABLED.

## Security and accounting
- New additive SQLite tables catalog_products and catalog_events, protected
  against audit UPDATE/DELETE. PRAGMA schema version remains 7 for V9 rollback.
- Owner-only CSRF/Origin protected POST, exact private browser Nginx paths.
- No Node Agent/API key route exposes the private product catalog.
- No direct or indirect writes to clients, balance, credit events, real usage,
  groups, Xray inbounds or subscriptions.
- Shared event nonce validation includes all existing credit, profile, client,
  representative controls and the new catalog namespace.
- No invented retail prices or new automatic financial formula.

## Acceptance
- Go SQLite tests: owner create, revision/fingerprint, publish/archive,
  literal search beyond page 50, per-rep visibility, no credit or client
  changes and immutable audit.
- Browser/API: owner-only writes, representative published scope,
  deny drafts/foreign/archived, CSRF and Origin, Xray isolation and 320px RTL.
- Foundation Go Race/Vet, real Chromium, build, pinned Xray and Native
  Metering Acceptance must pass on both PR and merged main.
- NEW isolated PRO HUB only: maintain V9 known-good binary, private SQLite
  snapshot, systemd+Nginx configuration and automatic rollback.
- No OLD production changes or US Xray activation.

## Not yet implemented
Representative-managed offers with independent manage_products permission,
cart/checkout/order receipts and payment provider integration, owner price
floors, bot/Mini App storefront, subscription publication and real Node
activation. Nodes #1 and Backup/Restore #4 remain pre-cutover P0 blockers.
