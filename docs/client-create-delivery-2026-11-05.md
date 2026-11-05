# Client creation and delivery — 2026-11-05

The old client form recorded a billable account but did not select an inbound or
apply the client. This increment adds manual/preset entry, optional custom UUID,
explicit account-only mode, and owner multi-inbound activation followed by the
existing subscription/QR component.

Financial creation uses a persisted, account-scoped request ID. Ambiguous replies
lock the payload and exact retries survive reload. UUIDs and subscription secrets
are not persisted in browser storage. Manual UUID recovery requires re-entry.
Each selected inbound uses current revisions, an additive client union and the
existing reviewed command API. Pending authoring changes and owner-resume holds
are refused. An accepted command is not displayed as applied until observed state
includes the client. Pending delivery never repeats the credit debit.

Validation: full business and API Go suites passed; targeted manual UUID tests
cover replay, tenant scope, invalid identity, collision and rollback of credit.
Four new compiled-browser cases cover owner additive activation, lost-response
reload, preflight refusal before debit, and scoped representative creation at
320px. Existing business/idempotency tests were updated for explicit modes and
the actual create response contract. The root LAB Chromium launcher needs a
QA-only --no-zygote flag; initial launch failure was retained in local logs.

Boundaries: representative inbound delegation still requires a separate durable,
owner-controlled access policy and scoped activation API. This change does not
give representatives owner execution access; they can create manual pending
accounts. Duration still starts at creation; no first-use expiry is claimed.
Custom credentials use the existing common UUID-based principal mapping, not
arbitrary protocol-specific password editing. No production migration, node
configuration, Telegram change, quota relaxation or accounting reconciliation.
