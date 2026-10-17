# DARK XRAY PRO — authentic host metrics and bounded history (NEW PRO only)

This increment extends *existing* Agent telemetry. It is not a Node
installer, customer traffic report, network tunnel, or an accounting feature.

## What is measured

- Live authenticated Linux Agent pulse is independent (3 seconds) from
  successful metric sampling (5 seconds). **Agent Online never depends on
  Disk/Load/Socket measurement or history writes.**
- CPU, host RAM and root filesystem used/total; Linux load averages over
  1, 5 and 15 minutes; kernel uptime in seconds; `TCP: inuse` from
  `/proc/net/sockstat`; physical default-route-interface RX/TX rates.
- `TCP inuse` counts **host TCP sockets**, not Xray users or subscriptions.
  Default-interface RX/TX is physical traffic (potentially unrelated to
  customers), NOT billable user usage, Direct/Tunnel/Swap allocation or a
  defensible customer connection count.
- Optional host stats fail independently and remain JSON `null`/absent.
  The baseline CPU/RAM/network heartbeat remains compatible with older
  new-PRO test Agents. Offline/stale stats never reappear as current or
  as an invented zero; the old heartbeat rules remain unchanged.

## Durable, bounded LAB-only history

- An **independent** modernc SQLite file under
  `/var/lib/dark-xray-pro-lab/telemetry/history.sqlite` is enabled only
  when `DARK_PRO_ENABLE_NODE_HISTORY=1` and the matching
  `-node-history-db` flag are deliberately configured.
- Separate 0700 directory / 0600 database permissions; symlinks and
  unsafe paths rejected. WAL, durable transactions and an explicit
  256 MiB max page budget; one accepted observation per Node installation
  per minute; 24-hour retention purged hourly. This database contains
  **no customer identity, tokens, traffic ledger, purchase or credits**.
  No Node or Inbound config/port is changed.
- History is keyed by **logical Node ID AND installation ID**. Replacing
  a Node never blends old installation measurements into the new one.
- Browser owner route:
  `GET /api/v1/workspace/nodes/{id}/history?window=1h|24h`
  requires the existing secured cookie session and OWNER role.
  Each response holds at most 60 one-minute points (1h) or
  288 five-minute aggregated buckets (24h). All observations use HUB
  receive time, not Agent-supplied clocks or fabricated fill values.
- Browser charts preserve holes in reported data, label physical RX/TX
  **non-billable**, and report missing/disabled history. The status
  card retains the previous authenticated snapshot when telemetry fails.

## Release and acceptance boundaries

Build React before compiling the Go embedded-UI HUB; keep a maximum of
the active and previous compatible release on the NEW PRO review host.
For the public Nginx allowlist, add **only**
`GET /api/v1/workspace/nodes/[A-Za-z0-9_-]+/history` and deny
all other methods. Do not open local owner Bearer routes or Agent ports
on public HTTPS.

Before activating on NEW PRO, verify Go/Race/Chrome CI, back up only
NEW-PRO SQLite and Nginx/systemd configuration, and start the HUB with
its pre-existing config/secret environment untouched.

The deployed NEW PRO may have zero paired Nodes and therefore **zero
history points**. Never use synthetic/demo data on the user's live
dashboard. Enrollment with a newly provisioned real independent test
Node remains a separate acceptance task; the current LAB Agent TLS
endpoint is deliberately loopback-only, and non-root loopback egress was
found blocked on the current NEW PRO VPS. Production legacy Nodes, HUB,
customers and Telegram accounts remain outside scope.

Remaining work: real multi-host enrollment with correctly secured TLS,
safe Node Replace/retire, actual Xray connections telemetry (not host
TCP socket count), source-accurate per-Node billable traffic and complete
automated metered admission.
