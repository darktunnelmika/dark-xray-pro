# DARK XRAY PRO — Dashboard Node Fleet V3 (layout + read-only UI)

## User-requested change

The previous Dashboard placed the Node monitor **below** all other fleet
summary tiles and history, making the actual Node row hard to find. This
change places a prominent independent **NODE FLEET / live monitor**
immediately after **HUB LIVE**, followed by existing aggregate analytics:

1. Independent real HUB telemetry and history (unchanged).
2. New Node Fleet monitoring card deck, authentic status/CPU/RAM/Disk/host
   network, with a clear per-Node navigation action.
3. Existing aggregate fleet metrics and owner-controlled durable 1h/24h
   historical graph below the Node cards.

Only the Dashboard summary page changes. The detailed Nodes page remains
unchanged and still supports the Node Inspector and Node Manager V2.

## Visual design

- Unified charcoal/graphite, emerald and aqua accents consistent with HUB
  LIVE; compact rounded panels, CPU ring, resource bars, physical RX/TX and
  a miniature recent-telemetry trace, graceful empty and stale states.
- One card per authenticated PRO Node. For each card, the explicit button
  opens /nodes and selects its true Node ID. No duplicate list at the bottom.
- Fully responsive mobile 320px, Persian RTL and English LTR, light/dark
  themes and reduced motion supported.
- Only the already-polling GET /api/v1/fleet is consumed; the new component
  never sends its own API request. The ring/bar values use fresh metrics only.
  A failed request does not label the prior snapshot "live".
- The ephemeral trend uses real bounded samples from the existing UI cache;
  it is never described as persistent 1h/24h history. If fewer than two
  samples exist, an explicit waiting state is shown.
- Physical host RX/TX and host TCP socket counts are NOT billable customer
  traffic or current Xray sessions. Missing host metrics are **unknown**,
  never synthetic zero.

## No production side effects

No Go/backend routes, Agent data-plane, runtime apply, SQLite files,
Node identities, old DARK XRAY HUB, customer billing, tunnels, Proxy,
service configuration or access policies change. Existing PRO USA Agent
and its SSH tunnel remain unchanged.

## Acceptance

- TypeScript and Vite production build.
- Existing complete Foundation Chromium, Go -race/vet, pinned Xray and
  Native Metering acceptance tests.
- New DOM order, no duplicate Node rows, genuine rate/CPU display,
  details navigation, stale/unknown values, explicit empty state,
  Persian 320px RTL without page overflow.
- Migrate as a new PRO release only if every CI check passes; retain the
  prior binary and restore systemd service and Nginx config on any failed
  smoke check. Keep at most two PRO release directories.
