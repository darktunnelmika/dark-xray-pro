# Owner representative ledger reports

The existing authenticated owner workspace exposes a read-only report at
`GET /api/v1/workspace/owner/reports/representatives/{id}`. Representative
sessions, anonymous callers and the separate Agent listener cannot use it.
The endpoint accepts no query parameters and does not change credit, enable
seller routes, migrate the database or contact Telegram.

## Accounting contract

Each report reads the representative and all of that representative's seller
reserves in one SQLite transaction. Free credit and reserved seller quota are
separate values. Bytes and unlimited units use canonical decimal JSON strings
to preserve integers beyond JavaScript's safe numeric range.

`seller_quota_state=not_enabled` means the optional seller schema is absent;
reserved amounts are omitted. An installed, valid seller schema with no sellers
returns `enabled` and explicit `"0"` amounts. This describes the stored ledger,
not whether a seller API, Telegram bot or sales workflow is enabled. Partial
schemas, missing or unsupported version metadata, and overflowing aggregates
fail with an unavailable response instead of inventing a zero balance.

## Telegram formatting boundary

`internal/telegramreport` prepares a bounded Persian HTML card. Names and
identifiers are validated and HTML-escaped. Compact GiB values are marked as
approximate and accompanied by the exact byte amount; unlimited units remain
exact integers. Invalid UTF-8, control characters, invalid representative IDs
and out-of-range ledger integers are rejected.

The formatter does not authenticate a Telegram sender, read a bot token, receive
updates, send a message or authorize a financial operation. Wiring it to the
main HUB bot requires a separately authenticated owner principal and bot
transport. This change itself neither deploys nor activates a bot.
