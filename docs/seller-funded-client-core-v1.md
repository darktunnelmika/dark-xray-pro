# Seller-funded native client core

This increment makes an existing seller reserve usable by a verified browser
owner or the matching representative. It is a business Store core, with no
HTTP routes, startup flag, Telegram receiver, retail checkout, payment provider,
customer delivery or runtime activation. No installed database is changed by
adding the source code.

## Explicit entry points and authorization

- `EnableSellerOrdersV1(ctx)` explicitly adds the immutable schema after seller
  management and the native usage ledger are already configured. It does not
  enable either prerequisite or migrate unrelated schemas.
- `CreateSellerClientV1(ctx, token, request)` creates one native client using
  the specified seller's existing reserve.
- `ExtendSellerClientV1(ctx, token, request)` renews only a client issued by that
  same seller. Existing non-seller clients cannot be adopted or rebound.

Only the opaque browser session token supplies caller authority. Role, account,
representative scope and current permissions are loaded inside the same SQLite
transaction as the debit. Representatives need both `manage_sellers` and
`create_clients` or `renew_clients`, as appropriate. Owners may operate across
representatives; the target representative and seller must still be enabled.
These checks also precede returning an idempotent receipt. A stored Telegram
user ID grants no authentication or permission.

## One transaction and one funding source

The transaction debits seller `reserved_bytes` or `reserved_units` directly,
creates or renews the native client, seeds usage for a new identity, appends
the native history marker, binds the client to its seller, and stores the
immutable order receipt. Failure rolls back every part, including the usage
seed. Representative free credit is unchanged by this order.

Creation and renewal keep the existing quota and cumulative user-month pricing
rules. Unlimited charges use the existing cumulative calculation; no unused
unlimited units are refunded. Renewal preserves UUID, historical usage and the
effective volume cap after earlier refunds.

`seller_order_clients_v1` records the immutable client UUID, representative,
seller and creation event. `seller_order_receipts_v1` records the verified actor,
canonical request fingerprint, exact reserve debit and before/after balances,
and immutable client snapshots. Large quantities are decimal JSON strings.
Replay returns that historical receipt, not a newly calculated balance, and a
changed actor or request with the same event ID is rejected.

The existing `events` and `client_operations` markers have zero representative
free-credit deltas. They preserve native creation/renewal history without
claiming a second debit. New order nonces cannot be reused by existing credit,
client, seller, profile, catalogue or representative-control mutations.

## Refund provenance

The volume-return verifier recognizes seller funding only when the schema,
immutable binding, typed debit facts, canonical request/receipt fingerprint and
the corresponding zero-debit native history all agree. It requires exactly one
initial seller funding record. An additional representative-funded creation is
ambiguous and is rejected. Ordinary representative-funded later renewals and
profile increases remain valid and are counted once with seller top-ups.

Expiry and manual archive still wait for authoritative final usage. Unused
volume is credited once to the same representative's free balance. No seller
reserve is also credited. Disabled sellers retain their historical provenance.
Provenance validation depends on stored data, not a UI or runtime enablement
flag, and works after reopening the Store. Missing or altered provenance cannot
invent a zero balance or a refund. Long receipt histories are read in bounded
pages within one transaction snapshot.

Existing non-seller paths keep their behavior when the new schema is absent.
An older binary cannot interpret seller-order funding for refunds; retaining an
old executable does not make it a complete reader for these new identities.

## Verification

Synthetic SQLite tests cover exact creation and renewal, zero representative
free credit, immutable historical replay, actor/request conflicts, permission
revocation, tenant and seller isolation, insufficient reserve, durable receipt
failures, independent-connection concurrency, preserved usage, finality before
refund, expiry and archive, representative reuse of returned credit, mixed
representative/seller funding, large integer precision, unlimited-unit isolation,
ambiguous or damaged provenance and receipt histories longer than one page.
