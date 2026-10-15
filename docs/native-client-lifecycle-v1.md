# DARK XRAY PRO — native client lifecycle v1

A REAL database-backed client lifecycle for the NEW PRO-only business domain. It does not connect, modify, migrate, revoke or publish any old DARK XRAY account, Node, customer configuration, representative balance, Telegram bot or external subscription.

## Forward-only storage and correctness

Private business SQLite schema v3→v4 adds an archived flag (0/1) to native clients and an immutable client_operations journal with per-client/per-representative indexes, checksummed idempotency intent, operator reason, additional service/volume and charged credit amounts. Existing clients, UUIDs, expiry, representative balances, hashed logins/sessions, group membership and immutable original event history remain unchanged.

**Renewal is transactional.** Each accepted operation adds days and/or volume and deducts only necessary representative credit in one DB transaction. The owner does not consume representative credit. Missing or insufficient credit, archived accounts, invalid amounts and reused event IDs with different intent are refused with no partial updates.

Unlimited duration charges are cumulative: ceil(total_days × allowed_users / 30) minus the previously charged user-month units. An existing 30-day five-user plan costs 5 units; one extension of 15 days charges 3 and a second 15-day extension charges **only 2 more**. Thus splitting 30 days into smaller renewal operations never overcharges. Volumetric plan top-ups consume exactly the additional volume credit, while 0 IP slots remains a valid volumetric unlimited-IP policy. Time renewals after expiry resume from current time, not a date in the past.

Client disabling sets the logical deployment_state to disabled; re-enabling returns to pending_node_apply **without activating an Xray process or publishing a subscription**. Archive is a logical terminal state, marked archived=true and disabled, preserving original UUID, group mappings, credit expenditure, immutable billing history and purchased expiry/volume. No deletion of physical customer data or automatic credit refunds.

Every operation has a unique event ID; exactly replaying an event does not re-charge or reset expiry. Cross-domain event ID collision with existing creation/credit events is refused. Audit records cannot be updated or deleted by SQL.

## Scoped browser API and GUI

- GET /api/v1/workspace/clients/{id}
- POST /api/v1/workspace/clients/{id}/extend
- POST /api/v1/workspace/clients/{id}/state (disable / enable / archive)
- GET /api/v1/workspace/client-operations

Each endpoint requires an authenticated, non-revoked secure browser session. State-changing requests also require matching Origin, JSON and HMAC-bound CSRF. Representatives can act only on their own native client IDs. Owner can manage any NEW PRO native client. Agent TLS and unpaired public API callers remain denied.

The bilingual mobile Business workspace offers per-client management selection, additional days, optional volume, immutable operator reasons, state action with archive confirmation and paginated operation history. On uncertain response failure it retains the operation event ID for same-form retries. It reports accurately that the Node Apply and subscription have not happened.

## Verification gates

Unit/race and browser regression coverage includes: two consecutive 15-day renewals versus one full 30-day renewal, same-ID exact retry and conflict on altered payload, insufficient credit atomicity, volumetric unlimited IP top-up, cancellation/archival preserving group membership and original spending, restart persistence, schema v3→v4, immutable client operations and owner/rep tenant ACL. Browser tests ensure no Xray or Telegram action can be triggered by database-only client changes.

## Missing before Stable / migration

Full customer-facing client deployment, real Xray Apply and crash-persistent rollback, individual user traffic/limits, route accounting, native subscription links, client import/Restore preservation, edit IP limits/HWID and name, mass actions, representative marketplace, Telegram orders/bots/Mini App, and comprehensive real-role production load/uptime tests.

**No old production HUB/Node/Client/Representative/Bot/domain/port changes are authorized by this phase.**
