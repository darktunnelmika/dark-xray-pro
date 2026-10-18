# DARK XRAY PRO — Financial/idempotent client form hardening

## What was wrong

Owner and representative client creation, and owner representative-credit
changes, already carry server-enforced append-only `event_id` identifiers.
This is essential: a network interruption after a server commit is ambiguous
to a browser, and an **identical retry must carry the same event ID**, so that
the server cannot double-charge or create another client.

The browser previously stored only `pendingCreditKey: string` and
`pendingClientKey: string`. When an HTTP call failed (e.g. timeout/503),
a user who edited the representative, credit, amount, account name,
plan type or duration would *reuse* the earlier key with a different
operation payload. A correct backend rejects this conflicting identity,
leaving the UI in a confusing failure loop until page reload.

## Change

Each pending operation is now represented as a local, **in-memory only**
`{key,id}` pair:

- `key = JSON.stringify(server_request_payload_without_event_id)`
  after normal validation and GB→integer-byte conversion.
- If the payload is **identical** to the unresolved prior request, reuse
  the same `event_id` on retry. Numeric equivalent inputs (`1.0` and
  `1.00` GB) normalize to the same bytes and therefore the same key.
- If ANY meaningful payload field changes, allocate a fresh event ID.
  No API call is performed until the user explicitly presses Submit.
- On confirmed success clear the pending key. On failure retain it.
- On real session expiry/password change discard pending keys, as before.
- The actual security and financial source of truth remains the existing
  HUB business operation table with atomic idempotent fingerprints.

No financial/account/customer secret is persisted in localStorage or
sessionStorage. Representative credit, client records, quotas and Xray
are untouched until an authorized form submission by the owner or rep.

## Tests and deployment

Two new Chromium tests simulate a **server-committed ambiguous HTTP 503**
on the first two writes and reject any later reuse of the same event ID
with different canonical payload. They verify identical retries (no second
debit), edited amount/plan/name (fresh ID), success resetting the key,
and proper CSRF on all user-triggered calls.

No Go backend, Node Agent, metering ledger, old DARK XRAY production state,
API, Nginx or Xray runtime changes. Full Foundation CI and real native
metering acceptance must pass before isolated NEW PRO installation.
Keep exactly one prior PRO release for rollback.
