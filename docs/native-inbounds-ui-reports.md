# Native Inbounds UI and durable Agent reports — PRO-only acceptance

## Functional scope

The existing Inbounds entry now opens a real owner-authenticated native manager when the private PRO business/session and native-Inbound services are enabled. The previous non-persisted planner remains available only when the business/session API is absent. This change neither enables those services by default nor migrates any legacy accounts or Nodes.

Owner workflows include creating encrypted REALITY profiles with generated keys, selecting existing client IDs, attaching/detaching clients without deleting them, selecting an independently paired Node installation, assigning its explicit IP and Direct/Dark Swap destination ports, and seeing fresh runtime reports. Creating an identical Inbound after an uncertain response returns the original public key rather than rotating it. Saving a target explicitly warns that the opted-in Agent may restart that Node's Xray process; it is not a hitless update. The Swap input configures only the destination listener, not the intervening tunnel.

The screen supports Persian/English, mobile/RTL and existing theme tokens. It retains selection while polling and across tab refresh. On failed/stale refresh it keeps prior information visibly stale and disables changes until revalidated. A representative never sees owner deployment controls and is denied by the backend even if they construct their own requests.

## Desired state is not applied state

SQLite schema 5→6 adds an independent configuration generation, last-delivery receipt and last Agent observation. Relevant client, representative, binding and target changes invalidate displayed success immediately, before the next Agent pull. Generation invalidation is intentionally conservative (global to the new PRO business database); refinement per Node is future optimization, not a reason to show old success.

The verified TLS Agent fetch records an exact installation/revision/config/manifest/lease receipt before sending its private payload. After Apply/Recover/Refresh, the Agent sends only a bounded metadata snapshot. The reporter's credential, installation and lease signature are checked. An unknown, altered, stale or superseded delivery cannot acknowledge another revision. Exact retries are no-ops and never refresh receipt time.

Owner status distinguishes awaiting_report, configuration_changed, stopped, out_of_sync, stale and applied. Applied means a matching process was reported by an authenticated Agent while both the server receipt (12 seconds) and lease remain fresh. It NEVER means independently verified customer connectivity, correct traffic billing, route availability or published subscription. The two customer-proof/publication flags remain false. When the HUB cannot obtain a newer report, last-good information expires visibly rather than remaining green indefinitely.

## Routes

- GET /api/v1/workspace/native-inbounds-state — owner-only aggregate, no secret config.
- GET /api/v1/workspace/native-inbounds/{id}/clients — owner-only binding IDs.
- POST /api/v1/agent/native-runtime-report — installation-authenticated TLS 1.3, bounded metadata only.

The existing native create/binding/target endpoints retain secure cookie, same-origin JSON and CSRF checks. The Agent-only handler rejects all owner workspace routes. No report includes a private REALITY key, serialized Xray config, client UUID or bearer credential.

## Verification

Tests cover exact report retry, stale receipt refusal, wrong signature/installation/receipt rejection, independent generation invalidation, immediate report declassification after configuration edits, schema upgrade preserving client UUIDs and representative balances, idempotent Inbound creation, and owner/representative access isolation.

The prior pinned-Xray owner→Agent integration now also verifies that successful runtime execution produces a durable matching owner-visible report. Browser tests cover actual form requests, binding and target payloads, CSRF headers, pending versus acknowledged status, failed-poll demotion and 320px Persian layout. GitHub must pass the full existing Go/race/vet, pinned real Xray, browser, frontend and LAB bundle suites before merge.

## Explicit limits

No production installation, remote-Node acceptance, tunnel setup, traffic/IP/HWID enforcement, subscription publishing, Telegram bot migration, DNS cutover or public-panel port change is performed by this development change. Full stable product and migration remain gated by issue #22. Old DARK XRAY data and services stay untouched.
