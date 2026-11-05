# Client / inbound boundary audit

## Evidence

Legacy reference: `darktunnelmika/dark-xray` commit `ed66ded4ec09c9d023635f64d9b05eb592ce06a1`, `web/clients-v4.js` (`clientFormV4`, `clientsView`, `detail`, `deliveryV4`) and `web/index.html`, inspected through GitHub read-only. This is a source comparison, not confirmation of the old production server's installed revision.

PRO baseline: local redesign commit `9b2d81a78fa0c10f40c293b27a3dc51bb65089d8`. Actual PRO service ExecStart still points to release `4db8a7b9a8598c93a6ef9f630ed8474770156394`; uninstalled work must not be described as live.

## Findings and changes

| Responsibility | Legacy source | PRO finding / action |
| --- | --- | --- |
| Customer form | Identity, service selection, plan; advanced fields separate | Reordered into the same three explicit sections; retained manual/preset fields and Telegram contact |
| Inbound selection | Choose existing service memberships | Retain named inbound choices; protocol/transport/security are read-only descriptions, not client-editable inbound fields |
| Start/stop inbound | Infrastructure responsibility | Creation previously generated `activate` for stopped inbounds. New creation requires active/observed-active, per-client-accounted, apply-capable inbounds. Membership commands remain `apply` only; stopped services are handled in Inbounds |
| Delivery | Separate delivery/QR dialog | Removed subscription management from the bottom of the creation form; View saved client opens the Configuration & QR section |
| Lost creation reply | Financial request reliability must be preserved in PRO | Retry of an already-sent creation replays the same event without blocking on a newly stopped inbound. A confirmed account remains pending delivery; no new financial request is generated |
| Protocol, TLS/Reality, listening ports, nodes, Tunnel/Swap | Inbound/infrastructure controls | PRO `App.tsx` renders `NativeInbounds` only under Inbounds/Dark Swap. No such editor was found mounted under Clients. The literal singular `Select Inbound` belongs to `NativeInbounds`; an exact live-screen report of those controls inside Clients needs its route/screenshot to identify a separate issue |

No old HUB changes, migrations, client removals, billing reset or infrastructure deployment were performed.

## Remaining parity gaps (not implemented by this patch)

- Representative-approved inbound grants and delegated immediate delivery.
- Client-centric edit of existing service memberships with runtime convergence and audit.
- Service labels on individual directory rows; the saved-membership inbound filter is implemented in the follow-up below.
- Inline verified usage/remaining quota and actual online/last-use data in the directory.
- Explicit expiry date / first-use start semantics; device/HWID policy; traffic-reset scheduling.
- Group assignment during creation and broader individual configuration delivery formats where the runtime genuinely supports them.

These must not be replaced with decorative fields, guessed telemetry, or owner-only execution permissions exposed to representatives.

## Validation

Focused creation/contact/idempotency Chromium suite: 15 passed, including stopped-inbound zero-charge rejection, exact lost-response replay after inbound stop, `apply`-only membership, existing membership retention, form sections, and separate delivery navigation. TypeScript/Vite rebuild is included by the browser test server. General business regression is recorded separately in the remote test log. Publication, CI and installation remain separate and pending.

Final results: general business Chromium regression 25/25 passed; final creation/runtime-boundary suite 8/8 passed, including stop racing apply (never marks delivery ready). Initial unused-import build failure was corrected before these clean TypeScript/Vite-backed test runs. Financial/backend database schema is unchanged by this increment.


## Follow-up: saved-membership directory filter

The directory now offers a lazy-loaded Client inbound filter under More filters. Filtering is performed in the same database read transaction as total/page selection and intersects existing representative, text, state, plan and group restrictions. Changing the inbound resets pagination, selected client and bulk selection.

Managed inbound assignments come from the authenticated execution head and its matching event; static and historical promotion assignments are used only when no managed head exists. These are saved assignments, not a claim of active runtime delivery or online status. Missing/wrong managed encryption keys fail closed. No accounting mutation, schema migration or inbound activation is involved.

GET /api/v1/workspace/client-inbound-options returns only ID/name/archive flag and scoped client count, with view_clients permission. Representatives only see services assigned to their own clients; query overrides are rejected. Both option enumeration (256 inbounds) and execution membership parsing retain bounded limits. The frontend rejects a filtered response without the exact inbound_id echo, preventing an older backend from silently presenting an unfiltered page.

Publication and installation remain pending; this is development checkout work.

Follow-up validation: focused business tests passed (static/managed membership, scoped counts, pagination, unknown inbound and wrong key); strict API parsing and owner/representative isolation tests passed after correcting the expected invalid-query status to the project's 422 convention. Exact proxy route/method tests passed. Chromium business/contact regression: 33/33 passed with one worker and a 30-second test timeout; an earlier two-worker run hit two existing contact-test/teardown timeouts. TypeScript/Vite build passed. Full-suite result: API passed (87.574s); business failed once in TestInitialApplyDeliveryAuthoritySwitchBothOriginsAndNoFallback with an identity/event conflict. The failing test passed standalone (0.747s), then passed together with the new inbound tests across three repeated runs (3.896s). The cause of the full-run transient is not established; do not describe the full business suite as green. Logs: /var/tmp/dark-pro-client-inbound-full-go.log, /var/tmp/dark-pro-client-inbound-delivery-retest.log, /var/tmp/dark-pro-client-inbound-business-retest.log. CI and release acceptance remain outstanding.

CI follow-up: first publication 9e466907 matched local tree c8d666a exactly. Native Metering Acceptance passed, including real browser creation and committed Xray usage. Foundation browser run had 549 passes, three failures from obsolete always-open creation/history selectors, and one ownership recovery retry. The shell and financial-permission tests now assert the compact directory/collapsed history and retain their authorization checks. All 33 shell/finance/ownership tests passed locally after this test-only correction.

A synthetic database (no live customer data copied) verified that the old business store opens the additive contact schema, but its old offline backup validator rejects it. A release rollback must therefore retain a compatible backup tool, or installation must remain blocked. Live-database cloning for a rehearsal was rejected by automatic approval review and was not performed.
