# Native Inbound lifecycle — edit, disable, resume and safe archive

This change is for the independent NEW PRO business database and paired Agent only. It does not deploy to an installed service, change old DARK XRAY, migrate users/bots, or change a public panel port/domain.

## Owner workflow

The authenticated Native Inbounds UI now supports profile editing, disabling, re-enabling and safe deletion by archival. Mutations require the owner session, same-origin JSON, CSRF, an operator reason, a stable operation ID and the expected profile revision. Concurrent edits and stale tabs cannot overwrite newer settings. Exact retries do not rotate keys, increment the revision again, or add duplicate audit entries. The editor preserves unsaved fields and rejects saving against a changed revision.

Normal profile edits keep the original REALITY private/public key and all existing client bindings. The key remains encrypted at rest and absent from browser/audit responses. Transport, target, SNI, gRPC service and name are validated using the existing typed renderer. Archive is terminal: it preserves identity, bindings and historical operations but cannot be silently re-enabled. Archived port reservations can be reused on the same physical installation and assigned IP; disabled (resumable) profiles continue to reserve their ports.

Private SQLite schema v6→v7 adds enabled/archived/revision fields and an immutable inbound operation journal. Existing profiles start enabled at revision 1; all existing client IDs, UUIDs, groups, credit balances, historical billing and sessions remain unchanged. No financial event is rewritten, removed or refunded by this lifecycle.

## Explicit removal of listeners

Disabling one Inbound removes its listeners from the complete authoritative Node config without dropping enabled sibling Inbounds. When no enabled Inbounds remain, the HUB sends an explicit signed zero-listener revision, never a missing/204 task that might leave an old configuration alive.

The zero-listener configuration is one exact canonical JSON document with no inbounds and a blackhole outbound. A typed no_listeners marker is accepted only with that exact config; arbitrary API/reverse listeners cannot hide behind an empty array. The Agent syntax-tests the pinned core, applies on actual ports, stores the encrypted last-good journal, and checks its own process owns no TCP listeners. Restart requires a fresh lease and does not re-open archived ports. No-listener reports have a distinct listeners_disabled state, not a claim of customer connectivity.

Any restrictive revision that removes a former listener or UUID fences automatic rollback to the removed authority. A syntactically accepted restrictive candidate that fails startup stops rather than resurrecting revoked users. An operator-authorized higher signed rollback is still a separate explicit action; the previous encrypted configuration is retained. Same-port process replacement is not hitless.

## API

POST /api/v1/workspace/native-inbounds/{id}/lifecycle accepts action edit/disable/enable/archive, event_id, expected_revision, reason and an explicit profile only for edit.
GET /api/v1/workspace/native-inbounds/{id}/operations returns paginated metadata-only immutable history (maximum 200 records).

All mutations remain owner-only. Representative and Agent-plane requests cannot manage owner profiles. The public deployment remains gated by existing native/business feature flags; no flag or installed binary is changed here.

## Acceptance

Database tests cover optimistic concurrency, immutable audit, lost-response retry, key/client/credit retention, sibling listeners, archived port reuse, no implicit archive resurrection and v6→v7 upgrades. API tests cover session/CSRF/role scope, mismatched revisions, history and a pinned TLS Agent flow for disable/resume/archive. Actual pinned Xray tests close a previously reachable port, kill the empty core, recover with the port still closed, and reopen only through an explicitly authorized newer revision. Browser tests cover owner forms, retained unsaved edits, stale-version refusal, Persian 320px layout and confirmation before destructive actions.

## Still not Stable / no migration

This does not complete production subscription publishing, traffic accounting/quota enforcement, IP/HWID policy, remote-node/tunnel/Dark Swap forwarding, all legacy protocols, Telegram commerce or encrypted old-panel import. Those remain release blockers in issue #22. A green CI or matching process report is not permission to migrate the existing customers or bots.
