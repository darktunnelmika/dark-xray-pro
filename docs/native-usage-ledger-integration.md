# Native usage integration — recorded counters, SQLite receipts and quota filtering

This is an opt-in accounting integration in the independent NEW PRO database. It does not deploy, migrate legacy data, rotate old credentials, change panel ports/domain, open a production StatsService or contact an old Node. The installed LAB remains unchanged.

## Actual implementation

The previously isolated usage logic is now part of the main Go module, using its real pinned modernc.org/sqlite driver (not a replacement go.mod or a Cgo-only mock). A supervised process epoch binds node, installation, kernel boot, PID/start ticks, candidate hash and server-derived user email/identity mappings. Counter observations are cumulative int64 values serialized as strings, never floating point; the Xray CLI collector explicitly requests reset=false and verifies ownership of its loopback StatsService socket before and after reading.

The Agent outbox atomically persists a complete report before transmission in an AES-GCM encrypted, file/directory-synced, private and exclusively locked journal. Exact repeated samples are coalesced. Missing counters, counter regressions, queue overflow, ambiguous identities or uncertain storage failures are not silently treated as zero. Only the oldest report's exact committed receipt removes it from the queue.

The dedicated Agent TLS listener accepts POST /api/v1/agent/native-usage only after real Node registry authentication. The owner HTTP handler does not expose this path. TLS 1.3 and CA plus leaf pin verification precede bearer transmission; redirects, malformed responses, ambiguous JSON or wrong receipts cannot discard pending data. Neither account baselines nor epoch registrations can be supplied through the reporting endpoint.

SQLite stores report receipts, per-user deltas, cumulative totals and the cursor in ONE BEGIN IMMEDIATE transaction. Independent database connections cannot double-charge the same report; a lost response after commit can be retried after restart. Historical receipts are immutable and exact old repeats remain no-ops. The total includes all observed bytes even when it exceeds quota. Transit-role counter mappings, when independently registered as nonbillable, do not add a second customer charge; native VLESS receivers are terminal destinations and receive server-derived billable mappings.

Only clients created AFTER explicitly enabling the ledger get a known-zero baseline, in the SAME transaction as creation, UUID, representative debit and purchase event. Existing clients are unknown/ineligible until a future verified import or reconciliation seeds their historical state; enabling this feature never resets them to zero. Once the private accounting schema exists, Store.Open reloads it, so dropping an environment flag cannot bypass quota checks. Corrupt/partial/incompatible schemas fail closed.

BuildNodeConfig now consults pro_usage_eligibility whenever the ledger exists. Exhausted, unknown or reconciliation-held clients lose their UUID in the next candidate without deleting client records, manual states, groups or financial history. The last user's removal still produces an explicit empty user list. A volume top-up rechecks remaining quota without resetting consumed bytes; quota eligibility changes invalidate the previous Agent report.

GET /api/v1/workspace/clients/{id}/usage is owner/self-representative scoped and returns exact string-encoded counters. The selected-client UI labels recorded totals, unknown baselines, reconciliation and exhaustion separately; errors are not converted to zero or active state, and BigInt formatting preserves values above 2^53.

## Explicit activation and runtime admission boundary

DARK_PRO_ENABLE_USAGE_LEDGER=1 requires the existing explicit NEW PRO business storage feature. No installed configuration was changed. This opt-in MUST NOT be enabled on a serving fleet yet: metered native runtime requests return 503 metered_runtime_admission_pending. That is a deliberate fail-closed barrier, not successful metered admission. Existing runtime leases expire normally rather than continuing under a false claim of quota enforcement.

Automatic process-birth registration, periodic sampling, final draining on replacement/shutdown, restart recovery of all epoch outboxes and fencing when samples are missing still require integration into the continuously running Node Agent. No public API can self-register arbitrary epochs or clear unknown usage. A crash can lose bytes that were never observed; uncertain tails require a reconciliation hold, not fabricated consumption or an assumed zero.

The volume decision is based on COMMITTED observations. It does not provide zero-overshoot, distributed hard quota or per-user byte reservations across multiple Nodes. An authenticated process report alone does not prove current meter coverage or customer access. Subscription publication and automatic migration remain blocked.

## Acceptance evidence

Tests use the actual project Store, modernc driver, Node registry, verified TLS transport and encrypted outbox. They cover exact and old report replay, lost HTTP response after COMMIT, durable restart, atomic creation rollback, foreign tenant denial, explicit unknown baselines, sticky metering on restart, quota invalidation, preserved UUID/credit/usage after top-up and archival, independent simultaneous database writers and 742 SYNTHETIC accounts. Fault and SIGKILL tests preserve the encrypted outbox across file replacement boundaries.

A separately SHA-256-pinned real Xray test starts a disposable native VLESS/REALITY client and server, reads actual user counters through a TEST-ONLY loopback StatsService without resetting them, commits the observed bytes to the same PRO SQLite ledger, rebuilds the candidate after quota exhaustion and verifies that the SAME UUID can no longer carry the application payload. The server key, user and origin are disposable; the local target/API/freedom exception is confined to test code. This is a controlled end-to-end acceptance test, not a claim that automatic production metering has been enabled.

GitHub CI must pass existing Go/race/vet, strict TypeScript, Chromium, pinned core and no-deployment bundle checks before merge. Source/tree verification must match the locally tested revision.

## Remaining Stable blockers

Continuous metered Node admission and reliable final counters; safe volume reservations/overshoot policy; IP/HWID and first-connection timing; real remote Direct/Tunnel/Dark Swap forwarding; subscription delivery; complete Telegram customer/owner/representative commerce; encrypted legacy backup/import with exact balances, UUIDs and subscription identity; load, recovery and final operator-approved domain/port cutover. No green test run or this development PR authorizes migration of the existing customers.
