# DARK Representatives V2 — seller API gate

## Scope

This PR uses **the native PRO business Store** and the existing owner-cookie authentication mechanism. It does not import the separate offline Phase 2 wallet.

Routes, available only *after* an explicit programmatic `EnableSellerQuotaV2Lab(ctx)` call:

| Verb | Private browser route | Permission |
|---|---|---|
| GET | `/api/v1/workspace/representatives/{id}/sellers?limit=25&offset=0` | owner only |
| GET | `/api/v1/workspace/representatives/{id}/sellers/{seller}` | owner only |
| POST | `/api/v1/workspace/representatives/{id}/sellers` | owner + valid Origin + CSRF |
| POST | `/api/v1/workspace/representatives/{id}/sellers/{seller}/quota` | owner + valid Origin + CSRF |

The server rejects unknown JSON members, wrong path/body tenant bindings, unknown sellers, invalid decimal amounts, replay ID reuse, insufficient credits and malformed page queries. Credit amounts are **decimal strings**, so JS cannot round large int64 amounts before reaching authoritative SQLite.

The default entrypoint **does not invoke the gate**. Thus all seller routes are absent on the installed HUB, database remains unmigrated, and no representative or Telegram bot can assign credit through them. No seller payment or real client provisioning is implemented here.

## Next security gates

- Explicit audited, backed-up migration lifecycle with schema compatibility and opt-in startup settings.
- Dedicated `manage_sellers` permission for representatives, separate from the existing V9 seven keys. Do not reuse `manage_clients` as implicit reseller admin.
- Seller identity and scope resolved from authenticated Telegram principals; never trust a `representative_id` from bot callback data.
- Separate bot token/webhook ownership and replay-protected dispatch; no old-bot token competition.
- Atomic issuance/renewal with Xray Agent acknowledgement and idempotent refund; all financial debits from the same Store.

## Deployment status

Not enabled in `cmd/hub`, not deployed, no modifications to old HUB, Xray, customers or active Telegram bots.
