# Representatives V2 — backed seller quota reservation (review-only)

This package extends **`internal/business.Store`**, not the standalone experimental `cv2_wallets`. It is the authoritative business database used by the existing HUB.

## State

- The SQL objects are created only after an explicit call to `Store.EnableSellerQuotaV2(ctx)`, which is **not invoked by the current HUB startup or API**. Default-off, no implicit schema migration.
- `CreateSellerV2` records one bounded seller identity scoped to exactly one representative and a Telegram user ID. It does **not** issue login or Telegram permission.
- `TransferSellerQuotaV2` performs `allocate` and `reclaim` as an **atomic transfer** between the current `representatives` row (free balance) and an escrowed seller reserve. The same event ID is idempotent, conflicting event replays are rejected; immutable audit triggers stop UPDATE and DELETE.
- Owner credits already consumed by another client are unavailable to allocate. Seller reserves are not spendable by the current `CreateClient` API. A future seller purchase must debit the seller reserve **and create the real client** in a single transaction; never move seller reserve back to the representative first and double charge.
- Every `SellerV2` read filters `representative_id`. A future API must enforce this scope from verified browser or Telegram principal, **never from a request body**.

## Acceptance before enabling live

1. Versioned, owner-reviewed migration after private SQLite/WAL backup, explicit opt-in, and schema 8/16 compatibility proof.
2. Dedicated seller-management permission (separate from current seven V9 permissions) and role checks in browser and Telegram API.
3. Allowance assignment, reclaim and suspension routes with owner/representative scope, session, origin, CSRF, unique event/revision, and history.
4. Purchase/renewal atomicity through original marketplace, order, client and usage ledger, including backend acknowledgment of delivery, refund and collision handling.
5. Detached Telegram sender, bot ownership/token handoff, callback reauthorization, quota safe retries and complete opt-in scope checks.
6. Race, chaos, V8 rollback, schema, real browser and on-LAB isolated Agent acceptance before activation.

**This file is not a production feature announcement or deployment instruction.**
