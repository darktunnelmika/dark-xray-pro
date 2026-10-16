# Native PRO usage integration and live-product acceptance

This revision integrates the usage modules with the actual main Go module, its unchanged `go.mod`/`go.sum` and pinned `modernc.org/sqlite`. It is a code change, not just another standalone kit. The baseline is main `13edab9df7e2757587809143fad8e59f26846dac`; real-core tests use the project's pinned Xray v26.9.9 and official archive digest already in CI.

## Runtime code now connected

The opt-in Store creates a known-zero baseline for NEW native clients in the same transaction as UUID, representative credit and purchase event. Existing client consumption is unknown, never seeded zero merely because accounting was enabled. Stored usage is sticky across restart; damaged policy views or missing append-only audit triggers prevent opening the metered store.

Only the TLS 1.3 Agent listener accepts native-usage reports, authenticated by the real Node Registry. A report cannot define an epoch, initial consumption, customer mapping or billable transit role. Complete cumulative reports are stored in an AES-GCM encrypted, fsynced Agent outbox. Matching receipts are issued only after atomic SQLite totals/cursor/receipt commit. Replay after a lost response or reopen cannot re-charge usage. Overflow, missing counters, epoch changes and uncertain tails do not become fabricated zero.

Committed quota eligibility filters the native UUID set without deleting customer identities, archival state, groups or sales-credit history. A separate owner/self-representative API and the actual Business UI display exact string-encoded byte totals, unknown/reconciliation state and exhaustion. BigInt formatting preserves values beyond JavaScript's safe-integer range.

## Real browser/core test, not route mocks

`TestRealBrowserNativeUsageXraySQLiteAndRevocation` is opt-in and fails when explicitly requested prerequisites are absent. `web/tests/live-usage.mjs` launches real Chromium against a temporary HTTP loopback server serving the compiled UI and real APIs. It does not intercept or mock requests. The browser logs in with synthetic credentials, creates a representative, grants sales credit and creates a native volumetric customer. The test then uses that persisted UUID, actual Registry enrollment over TLS, a managed real Xray server and a real VLESS/REALITY Vision client.

After a real application payload, Xray's actual cumulative user counters are collected without reset. The encrypted outbox sends them through certificate-verified, pinned TLS to the real Registry-authenticated ingestion handler. SQLite commits them; the next candidate removes the exhausted UUID and a fresh request with the same credential must fail. The browser refreshes and must display the exact committed total, exhaustion and zero remaining, including Persian at 320px. UUID and representative sales credit must remain unchanged.

Test-only exceptions are strictly confined to test files: disposable local REALITY target, loopback StatsService, single allowed test-origin port and a manually coordinated supervisor. No such networking or keys are installed in a real HUB/Node. Existing tests separately exercise lost COMMIT responses, cross-tenant rejection, 742 synthetic clients, independent DB writers and crash boundaries.

## Boundaries that are NOT completed here

1. The normal metered native-runtime endpoint STILL returns `503 metered_runtime_admission_pending`. Continuous Agent epoch admission, collection, final drains and recovery are not connected by this patch; it must not be enabled on a serving fleet.
2. This test replaces the managed Xray process to apply revocation. It does not prove interruption-free isolation of another user's existing connection. Exact per-user stream enforcement and multi-node hard budgets from the detached experimental kit are not included or declared production-ready.
3. Recorded Xray user counters are not NIC byte totals. Observation-based quota has delay and can overshoot; actual observed bytes remain recorded rather than clipped to the purchased limit.
4. The browser and Registry are real, but the test supervisor explicitly coordinates the core; this is not proof of the full continuously running Agent lifecycle or a physical remote Node deployment.
5. Subscription publishing, IP/HWID, first-use expiration, remote Direct/Tunnel/Dark Swap, all legacy transports, Telegram commerce and full migration remain separate acceptance blockers.

The five existing CI jobs remain required. An additional native-usage workflow builds the UI, installs its pinned browser and requires the named live-product test to PASS; `skip` and 'no tests' are not accepted. Local environments lacking a C compiler cannot claim Race success; GitHub CI runs its own Race suite.

No installed HUB/Node service, production database, customer, Bot, domain or public panel port is modified by this development PR. Migrating existing customers still requires the full release contract and explicit final operator approval.
