# DARK XRAY PRO — HUB LIVE / Dashboard V2

This increment enhances the existing Go + React NEW PRO application. It does not connect
the old DARK XRAY, copy Nodes or customer data, activate Xray listeners, grant
representative access to infrastructure data, or change the financial ledger.

## Missing feature fixed: HUB is not a Node

Previously the dashboard consumed only the authenticated `GET /api/v1/fleet`
Node roster. No independent measurements of the HUB host existed. A healthy
HUB therefore looked like an empty dashboard when zero NEW PRO Agents were paired.

V2 introduces a separate *local* `internal/hubmonitor` sampler at two-second
intervals, with cached, read-only results:

- Linux host CPU % calculated from successive `/proc/stat` measurements
- CPU core count, host RAM available/used, root filesystem used/total
- Load averages, Linux kernel uptime, host TCP in-use sockets
- Default-route physical interface RX/TX bitrates from real counter deltas
- **HUB process** uptime since current Go program start and actual Linux VmRSS
- Up to 90 actual samples in a bounded in-process ring (about 3 minutes)

Initial network/CPU intervals, NIC changes, counters that reset, read failures and
stale samples are **null/unknown**, never interpolated or fabricated zero.
A failed sampler never blocks the HUB/Agent/finance route. Since history is
in-memory, a HUB restart legitimately resets the live 3-minute chart. The
Node-specific durable 1h/24h charts are unchanged.

## Safe API contract

Only `GET /api/v1/workspace/hub/metrics` on the existing BrowserHandler is
allowed. It requires the owner cookie session even from local requests. Browser
Bearer-only, unauthenticated and representative requests are refused; the
separate TLS AgentHandler rejects this endpoint. The response contains only
non-billable local host telemetry, never IP/credentials, customer IDs, Xray
config, payment/credit or traffic accounting.

The public Nginx proxy must add **exactly**:

```nginx
location = /api/v1/workspace/hub/metrics {
    limit_except GET { deny all; }
    proxy_pass http://127.0.0.1:2087;
}
```

Do not open owner bearer, Agent TLS or other /api paths.

## Premium dashboard UI

The top of Dashboard now has a prominent graphite/emerald HUB console showing
live host CPU/RAM/disk, physical RX/TX, process RSS and uptime, and an
interactive real CPU/RX/TX recent chart. More restrained neon/graphite accents
without external images, tracking, fonts, animation libraries or third-party
dependencies. The separate Node fleet remains below, and its zero-Nodes state
is not confused with HUB health.

The HUB component only runs in owner Dashboard, polling at most every 2.5s,
one in-flight request at a time; it pauses when hidden and resumes on return.
The last verified response stays visible when the API fails, but the status
changes to unavailable and live readings change to unknown.

## Production safety

- All Go+React tests, race checks, Chromium RTL 320px regression and CI must pass.
- Do not silently call host TCP sockets “Xray connections”. Likewise default
  interface traffic is not billed subscriber traffic.
- Validate exactly the NEW PRO pinned source tree and full binary hash.
- Back up only NEW PRO databases, systemd override and Nginx snippet.
- Keep one current and one previous verified release, with automatic rollback
  of systemd AND Nginx if any new smoke-test condition fails.
- True 1h/24h HUB charts would require a separate bounded persistence design,
  not fabricated history; this release only promises the truthful recent view.
