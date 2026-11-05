# Client service management and directory summaries

Built on the installed client workspace baseline `62b785325146fdba45c2e51c2b853a84bf79df58`.

The owner's client dialog gains a Services tab. Each reviewed action adds or removes only that client from one existing, active, per-client-accounted inbound. It reuses the authenticated execution preview/command/operation APIs, revision checks and audit history. The full membership list retains all other clients. This surface never activates, stops or edits infrastructure. Representatives do not receive owner execution permissions.

A command is retained before transmission in user/client-scoped session storage. A lost response is resolved through its operation receipt or an exact event replay, including after reload. Admission is not shown as runtime success: both saved/effective membership and active, non-pending execution must agree. Unknown results remain recoverable. The account, UUID, credit and usage are not reset by service edits. Multiple services are edited separately; no cross-inbound atomicity is claimed.

`GET /api/v1/workspace/clients?summary=1` opts into page-scoped service names and committed usage. The existing representative scope and filters apply before enrichment; only returned identities are enriched. Membership, counters, historical reservations and the page share one read transaction. The existing account reader is reused through `AccountTx`; no schema or accounting policy changes occur. Membership decoding retains the existing 256-inbound bound and authenticates managed bindings. No per-client HTTP fanout is introduced.

The directory displays recorded consumption in this panel and the ledger's remaining native budget, including historical reservations. Unknown baselines/holds never become known remaining quota. Amounts stay decimal strings/BigInt across the browser boundary. Saved service membership and infrastructure health do not imply a customer is online.

Remaining work: delegated representative inbound grants/delivery, actual user-online/last-use telemetry, first-use/date-based subscriptions and creation-time groups. This change does not claim those features.

Validation: focused Go tests cover scoped summaries, read-only financial equality, historical reservation deductions, disabled/unknown ledgers, managed membership wrong keys and strict query parsing. Chromium tests cover add/remove with other-member preservation, lost-response replay after reload, stopped-inbound refusal, representative isolation, pending runtime evidence, revision conflict and compact mobile summaries. Full CI, publication and deployment receipts are separate acceptance evidence.
