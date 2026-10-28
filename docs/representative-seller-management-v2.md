# Representatives V2: scoped seller management

This increment connects seller management to the existing representative credit
ledger and the panel. It follows the owner-only API in PR #116. Credit remains
in the native business Store: allocation moves representative free credit into
a seller reserve; reclaim moves only an unspent reserve back.

## Access and accounting

`manage_sellers` is a separate, initially denied permission. Existing V9 client,
group and financial permissions do not implicitly grant it. The owner grants or
revokes it through a revision-checked, audited operation that invalidates the
representative's saved sessions. Existing V9 seven-key request bodies and their
historical request fingerprints retain their meaning.

Representatives with this permission can list, create and enable/disable their
own sellers and allocate/reclaim their own credit. The API resolves the actor
from the authenticated session. The Store checks the saved session, current
grant and representative binding again in the same transaction as each write
and financial replay. A stale browser cannot use a revoked grant or access
another representative's reserve.

Disabling a seller preserves its identity, historical events and reserved
amounts. It blocks new allocations; unused reserve can still be reclaimed.
Seller state changes have an independent revision and append-only event stream.

## Browser API

All paths below are relative to
`/api/v1/workspace/representatives/{representative_id}`.

| Method and path | Access and behavior |
| --- | --- |
| `GET /seller-permission` | Owner or the matching representative; returns `representative_id`, `manage_sellers`, `revision`. |
| `POST /seller-permission` | Owner only; requires `event_id`, `representative_id`, `expected_revision`, `manage_sellers`, `reason`. |
| `GET /sellers?limit=25&offset=0` | Owner or authorized matching representative; scoped paginated records. |
| `GET /sellers/{seller_id}` | Same scope; exact reserve and state revision. |
| `POST /sellers` | Same scope; creates one seller with a numeric Telegram identity. This is not Telegram authentication. |
| `POST /sellers/{seller_id}/quota` | Same scope; idempotent allocation/reclaim with canonical decimal-string byte and unit amounts. |
| `POST /sellers/{seller_id}/state` | Same scope; requires explicit `enabled`, `expected_revision`, a reason and stable event ID. |

All mutations require the existing same-origin JSON and session CSRF checks.
The separate Agent listener never mounts these management routes. Missing,
unknown and conflicting input is rejected before changes are committed.

## Panel behavior

The owner's representative profile has a functional Sellers tab with the
separate permission control. Representatives manage their own sellers from My
account. Lists and detail cards show exact byte/unit amounts; approximate GiB
display is supplementary. Inputs use whole GiB, MiB or bytes and convert with
BigInt, avoiding JavaScript number rounding in financial amounts.

An unconfigured feature, denied access, failed read and a genuine empty list
have distinct states. A malformed or cross-representative response disables
the workspace. An ambiguous mutation response locks new changes and offers a
retry of the same recorded request. A per-account session-storage journal keeps
that request across page reload; it contains no login, CSRF or bot token. The
server remains authoritative when a stored request is replayed.

## Explicit startup and release

`DARK_PRO_ENABLE_SELLER_MANAGEMENT_V2=1` opts the isolated LAB HUB into the
additive schema and mounts the routes before constructing the HTTP handler.
It requires `DARK_PRO_LAB_MODE=1`, the existing
`DARK_PRO_ENABLE_BUSINESS_CORE=1` and the private PRO business database path.
Absent or `0` leaves the API unmounted. Invalid flag values stop startup.

Before first activation on an existing LAB, preserve a verified private SQLite
backup and the current service configuration. Install the reviewed CI artifact,
then opt in and test with synthetic representative/seller identities. Never
copy an old HUB database or bot credentials to activate this feature.

Rollback disables the startup flag and uses a reviewed compatible release;
retained seller reserves and immutable histories are not discarded or restored
from an older database. An older owner-only seller API cannot grant a new
representative permission.

This increment does not spend seller reserves on orders, provision a customer's
runtime, receive Telegram updates, send messages, or migrate existing customers.
Those integrations consume the same authoritative reserve in subsequent work.

## Acceptance

Backend coverage includes denied defaults, owner-only grants, tenant isolation,
revocation before debit/replay, concurrent events and revisions, old V9 replay
compatibility, reserve conservation, disabled-seller reclaim and Agent isolation.
Browser coverage exercises owner grants, scoped creation, exact large balances,
lost-response retry across reload, state changes, failed/absent/denied data and
the Persian mobile flow. Tests use isolated synthetic data.
