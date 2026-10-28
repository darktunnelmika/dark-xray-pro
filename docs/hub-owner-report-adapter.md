# Owner HUB report command adapter

`internal/telegramhub` prepares the Persian owner credit report from the existing
native PRO business Store and `internal/telegramreport` formatter. It adds no
network receiver, public route, credential loading, worker or real message send.
No deployed bot is activated by importing or building the package.

## Trust boundary and command

Construct `NewOwnerReportHandler` with trusted server-side numeric owner-user
and private-chat IDs and an optional bot username. Names, forwarded messages and
user-supplied representative IDs never confer owner authority. The configured
identity must belong to the independently authorized PRO owner; browser or
representative credentials are not accepted in a message.

A future receiver must first verify that an update came through its authenticated
Telegram transport, reject unsupported update kinds and faithfully populate the
normalized `OwnerReportMessage` including origin flags. Numeric fields from an
arbitrary public request are not authenticated Telegram identity. In particular,
edited/channel/callback updates must not be converted into ordinary messages.

`Prepare` checks the configured sender, exact private chat, non-bot sender and
message origin before parsing commands or reading the ledger. Group, channel,
forwarded, edited, via-bot and on-behalf-of-chat messages are rejected without a
reply or ledger lookup. The only accepted command is:

```text
/rep_credit representative-id
/rep_credit@ConfiguredBotUsername representative-id
```

Mentioned commands require the configured username. Commands are limited to 160
bytes, one valid representative ID and printable ASCII; extra arguments, control
characters and invisible Unicode separators are rejected. The representative ID
is checked again against the returned report before formatting. Unavailable or
invalid reports return bounded errors without SQL details or partial finances.

The existing Store performs the consistent read. Free credit and seller reserves
remain separate, as do volume bytes and unlimited units. Exact decimal amounts
and escaped Persian HTML are retained. The adapter never writes financial data.

## Delivery and replay are explicitly caller-owned

The result is `PreparedOwnerReply`, not a sent-message receipt. It binds the
proposal to the approved owner/chat, input update and message, representative and
HTML parse mode. Repeated calls may read newer ledger balances; the package has
no in-memory duplicate suppression and promises no exactly-once delivery.

Before connecting a real bot, an independently reviewed receiver/dispatcher must
provide durable update and outcome tracking, define behavior after an uncertain
network send, and revalidate the current owner/chat authorization before delayed
dispatch. It must not treat a prepared response as successful delivery or blindly
resend an outcome that is unknown. No existing HUB bot token is needed for this
increment and none should be reused for LAB integration.

## Validation

Focused tests use synthetic identities and an injectable read interface. They
cover unauthorized origins before ledger access, malformed and oversized
commands, bot mentions, cancellation, unavailable and mismatched reports, exact
amounts, escaping and explicit replay behavior. A real temporary SQLite Store
test verifies free/reserved credit and financial events remain unchanged.

```sh
go test -race ./internal/telegramhub ./internal/telegramreport
```

The existing Foundation CI `go test -race ./...` includes this package. These
tests do not contact Telegram, use live data or send messages.
