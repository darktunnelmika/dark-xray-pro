# Telegram stores v1: review and LAB runbook

This feature adds private-chat Telegram storefronts to the existing DARK XRAY
PRO business store. It is explicitly gated to an independent Linux LAB. This
document describes the implementation and review procedure; it does **not**
authorize production deployment, a production restore, or reuse of an existing
live bot token. No live-token acceptance or real Stars payment/refund is claimed
by the synthetic tests below.

Implementation entry points: [HUB gating](../cmd/hub/telegram_bots.go),
[HUB integration](../cmd/hub/main.go),
[transport](../internal/telegrambot/),
[commerce schema](../internal/business/telegram_commerce_schema.go), and
[workspace API](../internal/api/telegram_workspace.go).

## 1. Roles, stores, and customer isolation

| Actor | Implemented capabilities and scope |
|---|---|
| Browser owner | Register or update store metadata, assign its Telegram manager, enable/disable it, and list all stores. Native catalog and representative administration remain available through the existing panel. |
| Browser representative | Read only stores belonging to the current representative. Cannot register a bot or change store ownership. |
| Owner of the HUB Telegram store | List/create representatives, inspect their native credit, and confirm native credit adjustments. Also has the management capabilities of its own store. The store must be bound to native `representative_id: "owner"`. |
| Representative store manager | Inspect permitted native finance, price published products in Stars, invite/revoke store administrators, configure trials and referral terms, handle support, and prepare/approve permitted broadcasts. Current native grants still apply. |
| Delegated store administrator | Only the granted subset of store operations. Invitation acceptance is bound to the named numeric Telegram user and the same bot; administrators cannot grant themselves store/administrator management. |
| Customer | Buy and renew, view owned services and usage availability, retrieve verified connection configuration, inspect orders and retry paid fulfillment, view wallet/reward entries, use support, manage marketing consent, and claim a trial when enabled. Persian and English menus are available. |

The authenticated bot ID selects the store. The Telegram user ID is accepted
only from the transport's normalized private-chat updates. A username, forwarded
message, callback payload, or browser-supplied Telegram identity is not authority.
Customer and management operations recheck current store, representative,
membership, and permissions in their transactions. Capturing an already-bound
financial event has the specific exception described below. The transport also
re-resolves access and binds opaque callback tokens to the user, bot, chat, and
current access digest.

One Telegram user has a separate customer identity in each store. Prices,
customer/client bindings, wallet entries, support, referral attribution, and
campaign audiences are store-scoped. Multiple brands may share one native
representative: they then share that representative's native catalog and credit
ledger, while each store selects offers through its own Stars prices. There is
no duplicated representative balance. The runtime allowlist supports 1–16 bots;
there is no special four-brand bypass or global customer lookup.

## 2. Native accounting and checkout invariants

Native representative credit and customer retail payments are separate facts:

- A volume service debits its allocated bytes from native representative credit.
  Observed customer traffic remains in the authoritative usage ledger; renewal
  does not reset recorded consumption.
- Unlimited credit uses cumulative `ceil(duration_days * user_slots / 30)` units.
  Renewal charges the difference from the previous cumulative charge. For
  example, 15 days for 3 slots costs 2 units; extending by 15 days increases the
  cumulative cost to 3 units, so that renewal costs 1 additional unit.
- The native `owner` storefront deliberately uses owner catalog/client semantics
  and has no fabricated representative credit balance.
- Customer checkout uses integer Telegram Stars, currency `XTR`. A store manager
  explicitly prices a published native product; a Toman catalog price is not
  converted automatically. Unpriced products appear in the manager pricing view
  but cannot be bought from the customer storefront.

A checkout intent snapshots server-selected product terms and price, binds the
exact bot/customer and optional owned renewal target, and expires after 15
minutes. Renewal additionally binds the client's expected terms. The customer
does not submit a trusted price or arbitrary client identity. Pre-checkout checks
the bound invoice, current authority, publication and native credit conditions.
It is not a reservation of representative credit; concurrent spending can still
leave an already-paid order awaiting fulfillment.

| Persisted order status | Meaning and operator/customer action |
|---|---|
| `quoted` | No successful payment has been captured. Check the quote expiry and normal purchase flow. |
| `paid_pending_fulfillment` | An exact successful payment is retained. Use **Orders and payment status → Retry fulfillment** after resolving the native credit/permission/terms problem. Do not issue another payment request for this paid order. |
| `paid_pending_provisioning` | Native creation/renewal and the immutable receipt committed. Connection readiness must still be verified through the native delivery bridge. This status alone is not delivery evidence. |
| `refunded` | An authenticated matching Telegram refund was recorded. It does not assert that the client was stopped or that native quota was returned. |

`RecordTelegramPayment` binds the invoice, bot, customer, currency, amount, and
Telegram charge ID. One order cannot acquire a second accepted payment, and a
charge cannot be rebound to another order. `FulfillTelegramOrder` commits native
credit, client creation/renewal, usage initialization where required, customer
ownership, and the immutable order receipt together. Repeating the same order
does not create a second client or debit twice. A failure rolls back that native
transaction while preserving the earlier captured payment.

Financial capture deliberately precedes current role resolution. Disabling a
store must not erase a successful payment or refund for its exact existing
invoice. Operational capture failures stay in the durable inbox with backoff,
capped at five minutes between attempts. A refund arriving before its preceding
payment has been captured is retryable; a different charge for an already-paid
order is a permanent mismatch requiring review. Fulfillment and replies still
require current authority. There is no endpoint that accepts a manual
"payment successful" flag, and this adapter does not initiate Stars refunds.

See [orders and recovery](../internal/business/telegram_commerce_orders.go) and
[native transaction extraction](../internal/business/lifecycle.go).

## 3. Delivery, trials, referrals, and messaging

Connection configuration comes only from the current verified native
subscription routes. Customer ownership, entitlement, applied identity/membership,
and the short-lived native proof are checked before preparation and again when
the definite Telegram send receipt is acknowledged. The stored handoff binds the
content SHA, exact client terms SHA, generation, proof validity, and Telegram
message ID. An old prepared URI cannot qualify a subsequent renewal merely
because the URI or shared Xray configuration stayed unchanged.

Delivery evidence is separate from the order status. An uncertain send, an
expired proof, or a missing native apply is not promoted to delivered. The bot
does not invent an online state, a first-connection timestamp, or zero usage when
authoritative usage is unavailable. See the
[delivery bridge](../internal/business/telegram_delivery_v1.go).

Trials are disabled until the manager explicitly saves a policy. A claim creates
at most one native volume client per customer/store, with one slot and a fixed
24-hour duration. **Time starts at issuance**, including time spent waiting for
native readiness. Representative trial volume is debited from existing native
credit. Trial allocation and its receipt are atomic.

Referral rewards are disabled by default. A store's reviewed policy controls
first/repeat purchase rates, caps, attribution window, hold, and optional expiry.
Terms freeze when a valid referral is attributed before the customer's first
successful paid order. Changing a policy does not rewrite earlier attributions.
Rewards remain provisional until payment, native fulfillment, verified handoff,
and the policy's release conditions are satisfied. Verified refunds reconcile
reward reversals in the same refund transaction; spent promotional credit can
leave bonus debt that future bonuses offset. Paid money is not silently used to
cover that debt.

The wallet is a store ledger, not the customer's Telegram account balance.
**Bonus redemption, wallet-funded checkout, and cash withdrawal are not enabled.**
The current UI states this limitation and still charges the full quoted Stars
amount. The rewards view is not a loyalty-tier or permanent-discount system.

Support is a durable store-scoped ticket/message flow. Broadcasts require
explicit marketing consent, a reviewed content/audience snapshot, and current
draft/send permissions; consent and authority are checked again for dispatch.
The worker persists incoming updates before advancing the polling cursor and
uses a durable outbox. A send interrupted after dispatch can become `unknown`;
it is not blindly replayed or treated as a definite delivery. Investigate retained
review/unknown work rather than deleting or resetting the journal.

Worker status exposes inbox, financial-retry, review, outbox, unknown-send, and
delivery-review counts. `queue_status_available` becomes true only after those
counts are read successfully from the journal. When false, zero/default or
previously retained counts do not establish that the queues are empty.

## 4. Exact LAB configuration boundary

The feature is off by default. The HUB requires these settings together:

| Setting | Required value or constraint |
|---|---|
| `DARK_PRO_LAB_MODE` | `1` |
| `DARK_PRO_ENABLE_BUSINESS_CORE` | `1` |
| `DARK_PRO_ENABLE_USAGE_LEDGER` | `1` |
| `DARK_PRO_ENABLE_TELEGRAM_BOTS_V1` | `1`; empty or `0` disables it; other values are rejected |
| `-business-db` | A private `.sqlite` file under `/var/lib/dark-xray-pro-lab/`; the existing selected native business store is used |
| `-telegram-bots-config` | Exactly `/etc/dark-xray-pro-lab/telegram-bots/config.json` |
| Runtime journal | Fixed at `/var/lib/dark-xray-pro-lab/telegram-bots/journal.sqlite`, with its sibling `.lock` |

Supplying `-telegram-bots-config` without the explicit enable switch is rejected.
The disabled transport returns before reading credential files or contacting
Telegram. Enabling through the HUB initializes the optional commerce, support,
campaign, and rewards schema families; it does not grant rewards or enable trial
policies. Disabling the feature does not delete existing financial history.

Private config shape, shown with a deliberately non-executable token placeholder:

```json
{
  "version": 1,
  "purpose": "isolated-pro-lab-telegram-commerce",
  "bots": [
    {
      "expected_bot_id": 123456789,
      "token": "<DEDICATED_LAB_BOT_TOKEN>"
    }
  ]
}
```

Each bot ID must be positive and unique. Config is limited to 16 KiB. Use
mode-0700 immediate parent directories and mode-0600 regular files, owned by the
HUB's effective user. Symlinked paths, shared/non-private files, executable files,
and hard-linked credential files are rejected. Tokens belong only in this private
config, never in metadata requests, the browser UI, environment examples, logs,
or the business database.

`Open` performs local validation only. Starting the worker performs network I/O
against the fixed `https://api.telegram.org` origin, verifies `getMe` against each
expected bot ID, and refuses an existing webhook. It does not delete a webhook.
There is no configurable API origin or redirect-following fallback. Use a
dedicated LAB identity, never an existing production token or the same bot as
another active polling worker. The older owner-report bot remains a separate
adapter with separate configuration and storage.

Native connection delivery additionally requires the existing
`DARK_PRO_ENABLE_INBOUND_EXECUTION=1` setup and
`DARK_PRO_NATIVE_INBOUND_KEY_HEX` (a privately supplied 32-byte hex key). Existing
native inbound, runtime, Agent TLS, and usage requirements still apply; the
Telegram flag does not provision a node or satisfy them. Follow the
[native inbound lifecycle](native-inbound-lifecycle.md) and
[managed execution](managed-inbound-execution-v1.md) runbooks separately. Without
the verified delivery bridge, payment/native registration can remain pending.

## 5. Metadata registration is not runtime activation

The panel's Telegram page uses `GET /api/v1/workspace/telegram`. The owner sees
all stores; an enabled, unsuspended representative sees only its own stores. It
returns feature availability, store metadata, connection state, and last poll
time without credentials. `polling` means update reception, not service readiness.

Only an authenticated browser owner with the existing CSRF-protected mutation
flow can call `POST /api/v1/workspace/telegram/stores`. All eight fields are
required; create uses revision zero and update uses the last returned revision:

```json
{
  "event_id": "telegram-store-example-0001",
  "id": "lab-brand-a",
  "bot_id": 123456789,
  "representative_id": "owner",
  "name": "Independent LAB store",
  "manager_user_id": 987654321,
  "expected_revision": 0,
  "enabled": true
}
```

These identifiers are illustrative, not credentials or verified accounts.
The bot/representative binding is immutable after registration. Name, manager,
and enabled state use revision-checked updates; replacing a manager revokes the
old manager membership. No browser session is fabricated from a Telegram user.

For a separately approved LAB rehearsal: establish the native owner/representative
and published catalog, register reviewed metadata, and independently provision
the private runtime allowlist. An allowed bot without an enabled store binding
cannot grant customer access. A registered store whose bot is absent from the
runtime allowlist remains `not_configured`; registration does not add a token,
launch a new worker, or contact Telegram. The current worker loads its allowlist
at startup; metadata changes are checked dynamically.

At restart, new allowlisted bot IDs receive durable cursors. Removing a bot from
the active allowlist stops its worker without deleting its retained cursor or
history; validation accepts this historical superset. There are at most 16
active configured bots and 256 retained bot identities. Do not reset the journal
to change the allowlist or discard pending financial records.

## 6. Encrypted backup and restore boundary

Commerce and engagement tables remain in the native business database and are
validated as optional schema families, including immutable payments, refunds,
order/trial receipts, wallet provenance, and historical handoff evidence. A
later refund does not make a valid historical handoff invalid for backup.

The native backup inventory also includes these fixed resources:

| Role | Wire role | Archive name | Source |
|---|---|---|---|
| `business` | 1 | `data/business.sqlite` | Selected native business database |
| `telegram_bot_config` | 16 | `config/telegram-bots.json` | Fixed private bot config, including tokens |
| `telegram_bot_db` | 17 | `bot/telegram-journal.sqlite` | Fixed durable transport journal |

The existing container uses age X25519 encryption over the compressed payload;
the inventory and credentials are inside that encrypted payload. Source files
are filesystem-private, not independently encrypted SQLite databases. Older
readers that do not recognize the new wire roles reject these archives.

Use the reviewed native offline snapshot flow. The collector requires stopped
bot writers and acquires their journal locks; copying a live SQLite file or
omitting the bot journal is not equivalent. Retained bot state remains part of
inventory even when the feature has subsequently been disabled. Offline
validation checks configured bot identities, journal structure, integrity and
scope without calling Telegram or dispatching queued work.

Restore publishes validated files into quarantine. It does not start polling,
send retained messages, or authorize production activation. Preserve the
business database, config, transport history, and quarantine/recovery procedure
together. See [container format and restore contract](../internal/probackup/README.md)
and [native snapshot integration](../internal/hubstate/).

## 7. Acceptance commands and evidence

Use the Go toolchain declared in `go.mod` and the Node version in the workflow.
The following commands exercise synthetic fixtures and injected transports;
they are not instructions to start the HUB or supply a live token. Listing a
command here is not a claim that it passed on a particular revision.

```sh
go test -race ./internal/business -run '^TestTelegram' -count=1
go test -race ./internal/telegrambot -count=1
go test ./internal/api ./internal/hubstate ./cmd/hub -run 'Telegram' -count=1
go test -race ./internal/telegrambot ./internal/telegramhub ./internal/telegramreport ./internal/business ./internal/api ./internal/hubstate ./internal/probackup ./internal/backupworker ./cmd/hub ./cmd/pro-backup -count=1 -timeout=18m
go vet ./internal/telegrambot ./internal/business ./internal/api ./internal/hubstate ./cmd/hub
```

For panel review, run from `web/`:

```sh
npm ci
npm run build
npx playwright install --with-deps chromium
npx playwright test tests/telegram-stores.spec.ts
```

Record the tested revision and each result separately. Review scenarios include
cross-store access, stale/revoked callbacks, unpriced products, duplicate and
late payment/refund capture, rollback after receipt failure, paid-order retry,
cumulative native renewal charges, trial uniqueness, old delivery evidence after
renewal, consent revocation, and backup schema/data tampering. The workflow is
[PRO Telegram stores](../.github/workflows/telegram-bots.yml).

The managed development environment has pre-existing process-epoch failures in
two native business tests and four API usage tests. The business tests are
`TestNativeQuotaReceiptInvalidatesConfigPreservesHistoryAndRenewal`
and `TestNativeUsageEpochRequiresCurrentDeliveryAndServerDerivedMappings`, both
reporting `process epoch differs; explicit registration required`. This was
reproduced on pristine base `253f8a163208571f8ee4a57e90cd33f2189dbc98` as well as
the feature checkout; the API failures were also reproduced on clean parent
`703d722`. Do not describe those broader suites as passing, weaken process-epoch
validation, or silently skip those checks; record the environment and rerun the
full gate on a supported host.

## 8. Remaining limits

This bot increment does not include a Mini App, a bot QR generation/delivery
action, loyalty levels/permanent discounts, wallet funding/redemption/withdrawal,
or a live-token acceptance run. Panel subscription QR functionality is separate.
Checkout is Stars-only; native catalog publication remains a panel operation.
A trial's 24 hours start at issuance, and a paid order may await native
fulfillment or verified provisioning. Unknown sends and permanent financial
mismatches require investigation. Synthetic test success does not establish
production readiness, real-world connectivity, real-money settlement, or
permission to reuse any live credential.
