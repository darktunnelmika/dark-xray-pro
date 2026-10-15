# P0 business core — persistent representative credits and client records

Status: first **real functional database slice**, NOT a release, production account system or migration. This independent SQLite database never reads the legacy DARK XRAY database and never connects its Nodes or Telegram bots.

## Implemented behavior
- Pure-Go SQLite, new private 0700 parent, 0600 file, no symlinks; WAL, FULL synchronous writes, enforced foreign keys and schema version gate.
- Durable representative billing records and the special protected primary owner billing identity.
- Transactional owner credits, including positive and negative volume/unlimited delta adjustments; replayed event IDs with identical intents are idempotent and mismatched intents are rejected.
- Append-only credit/client event audit: database triggers reject update/delete of past events.
- Creating a native client debits representative credit **in the same transaction** as account and audit insertion. A failed create cannot leave a half-charged account.
- User-month billing: ceil(number of allowed users × duration_days / 30), e.g. 60 days × 5 users = 10 unlimited units; 45 days × 5 users = 8 units.
- Volumetric plans permit IP count 0 to represent no IP-count limit; unlimited accounts require positive user slots. No false billable traffic reports.
- Idempotent client create preserves original UUID and expiry on retries. All new clients remain explicitly pending_node_apply and cannot yet serve a subscription.

## Explicitly opt-in owner-only API (localhost, no public browser credentials)
- GET/POST /api/v1/admin/business/representatives
- POST /api/v1/admin/business/representatives/{id}/credits
- GET/POST /api/v1/admin/business/clients
- GET /api/v1/admin/business/events

A new HUB must have DARK_PRO_ENABLE_BUSINESS_CORE=1 AND a NEW absolute private business SQLite file under /var/lib/dark-xray-pro-lab. All endpoints require the existing private owner bearer; Agent TLS handler cannot access them. Nginx public admin API remains blocked. No business database is opened by the currently installed LAB service by default.

## Acceptance coverage
Automated Go and HTTP tests assert user-month charging, owner scope, event idempotency and collision handling, insufficient credits, atomic client debit, concurrent requests, reboot persistence, immutable events, private filesystem checks and owner/Agent isolation. Go race/vet, real Xray and browser CI remain mandatory.

## Next release blockers
- Secure owner/representative sessions/RBAC/CSRF and full browser Clients/Groups and Representatives management.
- Actual user subscription token handling, Inbound binding and persistent Xray apply/rollback ACKs.
- Verified traffic-accounting, customer balances/expiry, store products, payments, order idempotency, Telegram bots/Mini App.
- Full old-panel data migration with exact token/usage/credit retention, multi-GB encrypted backup and release/performance rehearsal.
- Production encryption, secret rotation, consistent snapshots, recovery and soak tests.

Never claim a new client record equals an activated Xray user. No old HUB/Node/representative/bot operations are performed by this PR.
