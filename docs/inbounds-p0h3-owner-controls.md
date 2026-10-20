# P0-H3 — owner controls for the isolated route queue

P0-H3 connects the Inbounds owner screen to the P0-H2 queue. It does not enable
the queue on a HUB or Agent, claim a Node, deploy a release, start the USA Xray,
or change any production/migration gate. Existing P0-E/P0-G readiness remains
advisory and unverified for customer traffic, Dark Swap forwarding, accounting
and subscription publication.

## Read contract

`GET /api/v1/workspace/route-transactions` requires an owner browser session.
It returns `enabled:false` when the startup opt-in is absent or its paired
installation no longer matches. When enabled, it identifies only the explicitly
configured `empty_loopback_lab` Node and installation. There is no browser write
endpoint for discovery or enabling the executor. It is not exposed on the Agent
listener. Older HUBs returning 404 show an unavailable/disabled card.

The existing Node-scoped GET now includes:

- `review`: every enabled, non-archived Inbound target, its current Inbound and
  draft revisions, name, listen IP, Direct and Tunnel ports;
- `drafts_valid`: full P0-H2 candidate validation against the same SQL snapshot;
- `controls`: advisory Stage availability and specific saved Stage IDs/ports for
  Apply, Rollback and one-shot Renew; Apply includes its original Stage expiry;
- `server_time_unix_milli`: an exactly representable browser clock anchor.

The review, command history and Agent observation come from one read-only SQL
transaction. Missing or outdated drafts disable Stage. Public metadata is
returned; private profiles, runnable config and encrypted payloads are not.
Saved Stage lookups are independent of the latest-50 history limit. In
particular, a stopped process can explicitly restore its saved current Stage,
and Rollback continues to name the old saved ports after today's draft changes.

Controls are suppressed for pending commands, stale observations, a receipt
newer than the last Agent observation, exhausted queue limits or unresolved
execution. An interrupted command permits only a new reviewed Stage. Every POST
still independently enforces the original P0-H2 transactional guards. The read
response is not an authorization token or proof of future execution.

## Browser behavior

The owner reviews the entire Node, then confirms Stage. Apply, Rollback and
Renew require separate explicit confirmations identifying the Node, Stage and
ports. Ordinary page loads and refreshes issue GETs only. No automatic Apply,
renewal, timer-driven mutation or claim is introduced. Stage covers all reviewed
revisions; the browser never accepts a raw Xray configuration or signs an Agent
packet.

The latest signed Agent observation and the command history are separate
sections. A stored ACK is historical evidence even after an Agent restart. A
delivered command with no committed receipt is not a success or a stop receipt.
An observation preceding the receipt, or preceding an unobserved delivered
command, cannot produce a current-ready badge. Read errors immediately demote
the display and block new commands. A current lease is checked independently of
the 12-second observation freshness window; expiry means state unconfirmed, not
an invented stop. The browser uses HUB time plus elapsed request/page time,
accounting for both a monotonic clock and suspension, so a skewed browser wall
clock cannot extend a lease. Signed nanosecond packets are never round-tripped
through JavaScript; the owner sends only IDs, safe integer revisions and the
explicit six-field runtime condition.

Before sending, the exact owner request body and its random 32-hex ID are retained
in sessionStorage, scoped to owner/Node/installation. This contains no credential,
CSRF token, private config or signed Agent message. Memory retention remains
available if browser storage is disabled; persistence across reload then cannot
be guaranteed. A timeout, malformed success or 5xx keeps that same request.
Read-only polling may reconcile it by its committed history ID. Otherwise the
owner explicitly retries the identical body. A definitive refusal clears the
attempt, refreshes state and requires a new review/confirmation. Navigating back
or reloading never sends a stored request automatically.

Abandon has its own explicit confirmation that execution may already have
occurred. The server must acknowledge `process_stopped:false`; the UI never
labels abandonment as stopping Xray. Late ACKs remain visible under
`abandoned_unknown`. The normal short Agent lease and subsequent fresh
observations remain independent.

## Verification

- Business/API Race coverage includes complete multi-Inbound review, missing and
  outdated drafts, read-only behavior, stale observations, ACK-before-observation,
  restoration after restart, Stage lookup outside the latest-50 history, owner
  authentication, representative denial and disabled discovery with no claim.
- Nine real Chromium scenarios exercise explicit Stage/Apply/Renew/Rollback,
  request-loss recovery across reload, exact retry identity, conflict handling,
  unknown abandonment and late ACKs, browser clock skew and lease expiry, failed
  reads, mismatched scope, incomplete drafts, disabled capability, representative
  isolation, and Persian 320px layout.
- Existing Foundation and Native Metering workflows still gate merge, including
  the real pinned Xray lost-ACK/rollback acceptance and repeated observer/executor
  Race fixtures. No deploy job or server configuration change is included.

Remaining boundaries: zero customer bindings, loopback-only listeners, no Dark
Swap forwarding or unique billing proof, no subscription publication, no durable
claim release/reassignment, and no old HUB/operational Node migration.
