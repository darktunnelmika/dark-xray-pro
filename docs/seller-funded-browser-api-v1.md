# Seller-funded browser issuance and renewal V1

This increment exposes the reviewed [seller order core](seller-funded-client-core-v1.md)
through two explicitly gated browser endpoints. Both operate on the native PRO
business Store and debit the selected seller's reserved quota directly. A
representative can issue and renew a client while their free balance is zero.

## Gate and ownership

`Server.EnableSellerOrdersV1Lab(ctx)` requires an already configured business
Store, authoritative usage ledger, `EnableSellerQuotaV2Lab`, `EnableVolumeRefunds`
and its 32-byte inbound identity key. The gate installs only the seller order
schema. It sets its registration flag after schema validation succeeds; a failed
or cancelled call exposes no route. Repeated API enablement returns an error.

The refund supervisor must be configured before the gate. Its existing worker is
launched as part of normal server startup, after configuration. The gate does not
launch a second worker or require the supervisor to be running during startup.

The routes are absent before explicit enablement and are never admitted by the
separate TLS Agent listener, even with valid owner browser credentials. Build the
HTTP handlers after gate configuration: their route registration is a snapshot.

The existing authenticated `GET /api/v1/workspace/me` response now includes
`"capabilities":{"seller_orders_v1":false}` before enablement and `true` only
after successful configuration. This boolean is emitted for both owners and
representatives. The UI treats missing/false as unavailable, combines true with
the actor's current operation/management grants and target state, and never
probes a mutation route for feature discovery. The capability is not a grant;
the backend reauthorizes every request.

| Method | Browser path | Authorization |
|---|---|---|
| POST | `/api/v1/workspace/representatives/{rep}/sellers/{seller}/clients` | Owner, or the same representative with both `manage_sellers` and `create_clients` |
| POST | `/api/v1/workspace/representatives/{rep}/sellers/{seller}/clients/{client}/extend` | Owner, or the same representative with both `manage_sellers` and `renew_clients` |

Both require a valid opaque browser session cookie, same-origin JSON request and
the session's CSRF token. An owner bearer token, caller-supplied actor ID, stored
Telegram ID or callback identity is not browser authentication. The Store checks
the current session, role, tenant, grants and enabled representative/seller again
inside the same transaction as the debit. Retries are subject to these checks.

Creation binds a new client identity to its seller permanently. Renewal cannot
adopt an ordinary client, change tenant or move a client between sellers; an
owner cannot bypass this funding identity constraint.

## Exact wire contracts

Bodies are limited to 4096 bytes. Every field shown below is required, including
zero/false values. Unknown fields, duplicate keys, case aliases, null values,
trailing JSON and any query string are rejected. Representative and seller IDs
must exactly match the URL; renewal also binds the client ID to the URL.

Creation:

```json
{
  "event_id": "seller-browser-create-0001",
  "representative_id": "repExample",
  "seller_id": "sellerExample",
  "client_id": "clientExample",
  "name": "Example client",
  "duration_days": 30,
  "ip_count": 2,
  "volume_bytes": "10737418240",
  "unlimited": false,
  "reason": "Reviewed browser order"
}
```

Renewal:

```json
{
  "event_id": "seller-browser-extend-0001",
  "representative_id": "repExample",
  "seller_id": "sellerExample",
  "client_id": "clientExample",
  "additional_days": 30,
  "additional_volume_bytes": "1073741824",
  "reason": "Reviewed browser renewal"
}
```

Byte amounts must be canonical nonnegative decimal strings: `"0"` is valid,
whereas JSON numbers, signs, leading zeros, decimals, exponents and overflow are
rejected. The Store still applies its plan-specific limits and business rules.
Unlimited creation uses `"volume_bytes":"0"` and `"unlimited":true`; unlimited
renewal uses `"additional_volume_bytes":"0"`.

Success returns the Store's immutable `SellerOrderReceiptV1` directly: creation
uses HTTP 201 and renewal uses HTTP 200, including exact retries. Debit amounts,
reserve before/after amounts, client volume and charged quantities are decimal
strings, preserving values above JavaScript's exact integer range. The response
also includes the trusted actor, tenant, seller, action and original client
snapshot; renewal includes the previous snapshot. Exact retries set
`replayed:true` and return historical amounts, rather than current balances.
Changing the request or authenticated actor while reusing an event returns 409.

The receipt describes a durable billing operation. A new identity remains
`pending_node_apply`; this API does not enqueue runtime activation, issue a
subscription, send a bot message or claim successful delivery.

| Result | HTTP status |
|---|---|
| Malformed, ambiguous, incomplete or oversized JSON | 400 |
| Missing/revoked browser session | 401 |
| Denied tenant, grant, seller state, Origin or CSRF | 403 |
| Missing target object | 404 |
| Conflicting event/identity or insufficient seller reserve | 409 |
| Path/body mismatch, query string or invalid business amount | 422 |
| Failed storage or funding verification | 503 |

## Explicit LAB startup

`cmd/hub/main.go` recognizes `DARK_PRO_ENABLE_SELLER_ORDERS_V1` with these
requirements. The flag is disabled by default and does not grant permissions or
allocate credit:

1. Accept only unset, `0`, or `1`; reject any other value before opening storage.
2. Value `1` requires `DARK_PRO_LAB_MODE=1`, `DARK_PRO_ENABLE_BUSINESS_CORE=1`,
   `DARK_PRO_ENABLE_USAGE_LEDGER=1`,
   `DARK_PRO_ENABLE_SELLER_MANAGEMENT_V2=1` and
   `DARK_PRO_ENABLE_VOLUME_REFUNDS_V1=1`. Existing refund prerequisites continue
   to require `DARK_PRO_ENABLE_NATIVE_INBOUNDS=1`, its private 32-byte key, and the
   entrypoint's separate verified TLS Agent setup.
3. Keep the existing isolated LAB/private database path checks. No fallback
   database and no automatic grant or quota allocation is permitted.
4. Initialize in this order: open business Store → `EnableUsageLedger` →
   `EnableBusinessLab` → `EnableSellerQuotaV2Lab` → `EnableNativeInbounds` →
   `EnableVolumeRefunds` → **`EnableSellerOrdersV1Lab`**. Call the final gate only
   for flag value `1`, immediately after refund configuration and before any
   handler/listener is constructed. Treat errors as startup failure.
5. Preserve the existing single `RunVolumeRefunds(ctx)` worker and
   `WaitVolumeRefunds(graceful)` shutdown join; do not create another refund
   supervisor for seller orders.

Omitting the new flag keeps these routes absent, even if their schema exists
from an earlier run. Existing seller funding stays provable after reopen for
refund safety; that durable provenance is independent of API enablement.

## Accounting and acceptance

The API does not alter core billing formulas: volume consumes reserved bytes;
unlimited creation charges user-month units and renewal preserves the existing
cumulative formula. Unused unlimited units are never refunded.

Expired or archived seller-issued volume remains subject to authoritative usage
finality. Verified unused bytes are returned once to the same representative's
free balance. The seller reserve is not credited a second time. The returned
credit is spendable through the existing representative issuance API.

Focused API acceptance covers default/prerequisite/cancelled gates, TLS Agent
isolation, the exact representative route allowlist, owner and representative
creation/renewal from zero free credit, historical receipts and nonce conflicts,
tenant/seller/client binding, revoked grants and disabled sellers, cookie/CSRF/
Origin boundaries, strict bounded JSON, exact large decimal amounts, insufficient
reserve rollback, finality-gated refund plus representative reuse, and unchanged
unlimited unit charging. All fixtures use disposable local storage and synthetic
usage checkpoints; no live HUB, customer, Node or bot is contacted.
