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
