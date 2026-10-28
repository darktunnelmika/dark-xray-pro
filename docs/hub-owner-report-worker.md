# Private PRO LAB owner report worker

This Linux worker adds a Telegram Bot API transport to the reviewed owner
report adapter. It prepares reports through the existing shared business Store.
The only command is `/rep_credit <representative_id>` (or the same command with
the configured bot username). It has no financial mutation, seller/customer
messaging, broadcast, provisioning or administration endpoint.

The helper has a non-Linux stub that preserves disabled startup with zero I/O
and rejects enablement. The repository's existing business-to-managed-runtime
dependency independently prevents full non-Linux builds; the stub does not claim
broader platform support.
The code is disabled by default. The implementation and tests do not activate a
bot. All transport acceptance tests use fake `http.RoundTripper` implementations,
synthetic tokens and temporary journals; no Telegram traffic is used as test
evidence. A separately supplied PRO LAB bot configuration is required before any
operator activation. Credentials or bots from the old HUB must not be reused.

## Startup boundary

`OpenLABOwnerReportBot(LABOwnerReportBotOptions, RepresentativeReporter)` is the
only production constructor. An empty or `0` `EnableFlag` returns `(nil, nil)`
before inspecting the reporter or performing filesystem/network I/O. Enabled
requires all of:

- Dedicated `DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1=1` flag.
- The startup caller's existing isolated LAB and business gates both enabled.
- Explicit config path `/etc/dark-xray-pro-lab/owner-report-bot/config.json`.
- Dedicated token file `/etc/dark-xray-pro-lab/owner-report-bot/token`.
- Dedicated journal `/var/lib/dark-xray-pro-lab/owner-report-bot/inbox.sqlite`.

Paths cannot be supplied through a common bot/token environment variable or an
arbitrary API endpoint. Config/token files must already be regular private files
owned by the process UID, without symlinks or hard links. Their parent directories
must be private and owned by that UID. The journal directory must likewise be
created privately beforehand; opening the worker creates only its own journal and
lock. It never opens a separate business database or reconstructs a wallet.

Configuration has exactly these fields; the numbers below are synthetic examples:

```json
{
  "version": 1,
  "purpose": "isolated-pro-lab-owner-reports",
  "expected_bot_id": 123456,
  "owner_user_id": 900001,
  "owner_chat_id": 900001,
  "bot_username": "DarkLabHubBot",
  "token_file": "/etc/dark-xray-pro-lab/owner-report-bot/token",
  "journal_file": "/var/lib/dark-xray-pro-lab/owner-report-bot/inbox.sqlite"
}
```

No token is present in JSON. The token file contains only the independent LAB
token, optionally followed by one LF. IDs are positive JSON integers; usernames
do not grant authority. `getMe` must return the configured numeric bot ID, bot
status and username before any persisted command is prepared or any updates are
polled.

The constructor performs no network requests. The HUB entrypoint accepts the
explicit `-owner-report-bot-config` path with the dedicated enable flag, passes its
already-open shared business Store, calls `Run(ctx)` once with its worker context,
and calls `Close()` before closing that Store. `Close()` cancels and waits for Run,
then releases the journal. `Wait(ctx)` is available for bounded shutdown
coordination after Run has started. Run errors are static or context errors and
may be logged; token values, request objects, response bodies and prepared reports
must not be logged. A terminal bot error is logged independently of the HUB
listener error channel, so it does not stop the financial/API service. Correct
the private bot configuration and restart the LAB service explicitly if its
worker has stopped; the process does not silently replace its credentials.

## Accepted input and protocol bounds

Only a newly composed ordinary private text message from the configured numeric
owner in the configured numeric chat reaches the report adapter. Commands sent as
replies, edits, forwards, via another bot, on behalf of a chat/business, in guest or
ephemeral mode, or inside a topic are rejected. Unknown message fields fail
closed. This is intentionally a narrow command surface; send the command as a
new message. Unsupported update kinds are durably rejected even if Telegram
returns an older queued kind outside the requested update filter.

The client uses POST JSON to fixed `https://api.telegram.org`, verifies TLS, uses
no proxy configuration, and follows no redirects. Each request has a 35-second
timeout. Polls request only `message`, at most 25 updates, with a 20-second long
poll timeout. Each response is limited to 1 MiB, with duplicate JSON keys,
trailing JSON and excessive nesting rejected. IDs stay signed 64-bit integers.

Outgoing requests contain only the configured private chat, adapter-produced HTML
text, the original message ID in `reply_parameters`, and disabled link previews.
The adapter's exact decimal strings and escaped text are sent without formatting
changes. Success requires a returned positive message ID, the configured bot as
sender, the configured private chat and the requested reply message ID. The
returned rendered text is not compared with the submitted HTML source.

HTTP errors, response descriptions and transport errors are reduced to static
errors and reason tokens. In particular, `net/http` URL errors are never wrapped:
the Telegram URL contains the token. Polling uses cancellable backoff, honors
bounded `retry_after` delays, and stops after five consecutive transient errors.
Per-message send rejection (400/403) records `failed`, waits one second, and
continues with later commands. It never retries that failed command. Authentication
failure (401), polling conflict (409), or malformed identity/protocol stops the bot
worker for operator correction. HUB integration logs that static error
without stopping its independent financial/API service. There is no webhook
deletion or takeover.

## Durable delivery semantics

The journal is independent of financial ledgers. It records a binding digest for
bot ID, owner ID, private chat ID and normalized bot username. A changed binding
fails before recovery transitions; old pending commands cannot be delivered to a
new principal. An exclusive file lock prevents concurrent workers sharing it.

An entire bounded update batch and its next polling offset commit together before
a later poll can acknowledge those updates. The durable pending queue drains
before further polling, including after restart. Repeated IDs with the same
canonical fingerprint are ignored; conflicting content or an unseen ID below the
cursor stops the worker. The cursor never rewinds automatically. The journal caps
pending/dispatching rows at 1,000 and total history at 100,000; reaching either
limit stops ingestion without acknowledging an uncommitted batch. History is not
automatically pruned.

| Durable state | Meaning and restart behavior |
| --- | --- |
| `pending` | Stored input; preparation may be attempted after identity verification. |
| `rejected` | Unsupported input, denied scope, invalid command or unavailable report; no send was attempted. |
| `dispatching` | The bound prepared reply committed before entering the transport. On restart it becomes `unknown`. |
| `sent` | A valid Telegram success response was received and committed, including its message ID. |
| `failed` | Telegram explicitly rejected the send, or the local reply scope was rejected. It is not automatically retried. |
| `unknown` | A send may have reached Telegram, but delivery was not proven. It is never automatically resent. |

Preparation is not delivery. Cancellation before the dispatch claim leaves the
command pending. After the claim, a timeout, 5xx, malformed receipt or response
loss is unknown. The worker tries to commit the outcome with a short independent
context even during shutdown; if it cannot, restart converts the surviving
dispatch claim to unknown. Terminal outcomes are immutable. An owner can request a
fresh report with a new command; that is a separate operation, not replay of an
ambiguous external send. There is no claim of exactly-once Telegram delivery.

Telegram may choose a nonsequential update ID after a long idle period. A high
saved offset may then require an operator to establish a new polling boundary.
The worker does not guess by rewinding or deleting history. Before any manual
journal rotation, stop the worker, retain the old journal/outcomes and review the
dedicated bot's queued-update boundary; prefer a newly isolated LAB bot if that
boundary cannot be established. The fixed private journal path must not simply
be cleared to retry unknown sends.

## Local verification

```sh
go test -race ./internal/telegramhub ./internal/telegramreport
```

The package tests cover identity and owner/chat checks before ledger reads;
unsupported origins; bounded/strict JSON; fixed URL and no redirects; exact
escaped report text; 429/5xx/timeout/cancellation classification; durable claim
ordering; pending restart drain; duplicate update suppression; ambiguous-send
restart behavior; private files; journal binding/schema/lock checks; atomic batch
rollback and limits; and static credential-safe errors. They intentionally use no
real token or external network.

Protocol references: [Telegram Bot API requests](https://core.telegram.org/bots/api#making-requests),
[`getMe`](https://core.telegram.org/bots/api#getme),
[`getUpdates`](https://core.telegram.org/bots/api#getupdates),
[`Update`](https://core.telegram.org/bots/api#update),
[`Message`](https://core.telegram.org/bots/api#message), and
[`sendMessage`](https://core.telegram.org/bots/api#sendmessage).
