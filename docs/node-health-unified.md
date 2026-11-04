# Unified node health observations

This integration is based on the installed Telegram-inclusive source
`0ebf2e61d07d9a7e4d4812ccd840444128cad224` and combines the independent channel
projection from PR #145 with the freshness UI from PR #146. The Telegram
implementation is preserved without modification.

## Behavior

The roster presents one status card per registered node. Agent connectivity,
host metrics, proxy-engine observations and accounting observations remain
independent. An online Agent cannot imply a healthy core or accepted accounting.
Engine readiness and verified protocol health have different labels; neither is
a new public-service delivery authorization.

The owner API returns `execution_fresh_for_ms`, bounded to the remaining
15-second observation lifetime. Re-reading the same durable report cannot renew
that lifetime. The UI uses elapsed monotonic time from request start, including
network delay, so a skewed browser wall clock or a delayed response cannot make
old evidence fresh again. Missing, malformed, unsupported and expired evidence
remain explicit. A failed roster request invalidates all displayed observations
without deleting registered rows. An unanswered snapshot expires at 30 seconds.

Superseded successful, malformed and failed requests cannot replace or invalidate
a newer response. Unmount/session cleanup also invalidates outstanding requests.

## Validation

- Focused owner Node Manager and observation-lifetime API tests passed.
- Durable node-channel business projection test passed.
- `go vet ./internal/api ./internal/business` passed.
- Production TypeScript/Vite build passed.
- All 27 Node Manager Chromium cases passed in the final run (40.0 seconds),
  including independent accounting uncertainty, observation expiry, wall-clock
  skew, delayed responses, superseded replies, refresh failure, role isolation,
  and the 320px Persian layout.
- The initial browser run had four fixture failures: the synthetic clock exceeded
  the mock session expiry, and response-race fixtures started before the initial
  roster completed. Fixtures were corrected without weakening product assertions.
- Host browser execution used `--no-zygote`. Local Go tests are non-Race because
  this host lacks a C compiler; GitHub CI must provide exact-source Race coverage.

This is source and bounded test evidence. It does not claim installation,
production readiness, public/WAN data-plane coverage or a completed 24–48-hour
soak. Runtime activation, leases, billing, subscriptions, database schema and
production routing are unchanged. Dark Restore remains removed.

## Release boundary

Preserve the concurrent Telegram work and recheck live LAB revision before any
deployment. The earlier f65377 soak was invalidated by the Telegram upgrade and
cannot certify a fixed version. A valid long-running stability baseline must
start after the combined release is settled. Source, CI, merge, installation and
installed acceptance remain separate facts.

## Full Foundation CI scheduling correction

At source `ccf928a197e544cd605911d3e35575b2f294e770`, Foundation run
41913993137 / job 126868679810 exhausted the whole-package 19-minute timer.
Its final test, `TestDarkRestore5000OfflineCustomersImportReplayAndRecovery`,
had run for 36 seconds when the package-wide alarm fired. No test assertion
or data-race failure is reported by that log. The two exhaustive business
partitions of managed-inbound run 41913993136 both succeeded on the same source.

Foundation now uses that existing deterministic partition runner and preserves
the named aggregate check. Both partitions must succeed, fail-fast is disabled,
and all tests, examples, fuzz seeds and historical compatibility fixtures remain
included. Each partition retains the existing 18-minute test deadline; this
does not skip tests or change application behavior. Historical compatibility
coverage does not re-enable the retired Dark Restore product.

The failed original run remains part of the evidence. The corrected revision
requires its own CI result before LAB installation.
