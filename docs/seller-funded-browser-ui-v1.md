# Seller-funded browser forms V1

The existing seller details panel now offers client creation and renewal using
the [seller-funded browser API](seller-funded-browser-api-v1.md). The UI remains
part of the same owner/representative workspace and uses its session, Origin and
CSRF flow. It introduces no Go, startup, bot, price catalogue or runtime changes.

## Availability and input

The seller panel refreshes the authenticated `/api/v1/workspace/me` response
alongside its seller page. `capabilities.seller_orders_v1` must be exactly `true`;
missing, false or malformed values leave order forms unavailable. The returned
actor must match the current session. Current representative grants require
`manage_sellers` and the corresponding `create_clients`/`renew_clients` value;
owner operation follows the existing owner policy. New orders also require the
selected seller to be enabled. The Store rechecks target representative/seller
state and all permissions transactionally.

Creation includes client ID, name, plan, duration, user/IP count and the required
audit reason. Volume uses whole bytes, MiB or GiB converted through `BigInt`, then
sends a canonical decimal string. Unlimited creation sends explicit zero volume.
Renewal accepts the exact existing client ID, added days, added volume and reason.
The server verifies that the client was issued by the same seller. There is no
invented seller-client list; no such listing API exists in this increment.

For unlimited or time-only renewal, added volume is zero. The UI does not invent
a price or estimate the unknown client's current unit charge. The returned
receipt supplies the actual volume/unit debit and reserve balances.

## Recovery and receipts

Both operations extend the existing seller panel's single request journal.
Before sending a new mutation, the browser must persist and read back its exact
nonce, endpoint and complete immutable payload in session storage. The journal
is scoped to the authenticated user and representative, contains no session,
CSRF or bot token, and validates its complete contract when restored.

An ambiguous transport failure or unverified success response retains the exact
request and locks new seller mutations. A manual retry reuses the same event and
payload, including after reload. A denied retry, including HTTP 403, cannot prove
that the original request did not commit and therefore does not clear the
journal. Feature/grant checks must be refreshed before order retries can run.
Definitive initial rejection or a verified receipt clears the matching journal;
a late response cannot clear another request's nonce.

The UI validates successful receipts against the trusted actor and requested
representative, seller, event, client and action. Exact decimal amounts must obey
reserve-before minus debit equals reserve-after. Creation snapshots match the
request; renewal snapshots preserve identity and reconcile cumulative days,
volume and unlimited charges against the previous snapshot. An invalid receipt
is treated as an unknown result, not a failed purchase that can be automatically
resubmitted under a new event.

Verified receipts show actual charged volume/units and the historical reserve
after the order. They are labelled separately from the refreshed current seller
balances. New billing success does not claim activation, working connectivity or
subscription delivery. English and Persian forms share the existing responsive
seller styles.

## Acceptance

Browser tests cover zero-free-credit representative issuance, decimal amounts
above JavaScript's exact integer range, owner unlimited issuance and cumulative
renewal, lost creation/renewal replies, repeated recovery through a denied retry
and reload, unverified actor/balance receipts, disabled or malformed capability,
current grants overriding stale session permissions, disabled sellers, unavailable
recovery storage, definitive insufficient-reserve rejection, and Persian mobile layout. The pre-existing seller management
browser suite runs alongside these cases to check permission, quota, state and
recovery behavior.

Tests serve the compiled UI only on loopback and replace API responses with
synthetic fixtures. No live HUB, customer data, seller reserve or bot is used.
