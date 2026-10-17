# DARK XRAY PRO — Dashboard & Design System V1 (NEW PRO only)

## Scope

This is a frontend-only refactor of the existing authenticated Go + React LAB
application. It does not install an Agent, connect a legacy Node, make a
financial mutation, read any legacy database, activate Xray, or add new routes,
secrets or third-party UI/telemetry dependencies.

## Shared design

- Single light/dark color system in `web/src/DesignSystem.css`. Warm neutral
  graphite/green instead of a heavy navy/cyber treatment. Responsive compact
  cards, soft corners, accessible focus ring, bilingual RTL/LTR.
- Existing routes, roles, financial forms, browser session and pagination
  remain unchanged. Sidebar now groups **infrastructure**, **management** and
  **account/settings** without duplicating an action or showing owner pages to
  representatives.
- Mobile drawer closes via Escape, backdrop, navigation or a wide viewport;
  scroll is contained in the drawer with temporary background scroll lock.

## Source-accurate dashboard

- `DashboardOverview.tsx`: actual fleet totals, online/offline and fresh-only
  CPU average. A stale fetch keeps the last verified fleet visible but marks
  it stale rather than falsely indicating fresh live values.
- Physical RX/TX are summed by the existing fleet API. If the API reports
  incomplete rates, missing readings display an em dash, **never 0 Mbps**.
  Root-disk/load/uptime/TCP inuse remain in the Node inspector. TCP inuse
  is not the Xray customer connection count.
- Real 1h/24h historical charts use the existing owner-session-only
  `GET /api/v1/workspace/nodes/{id}/history`. They are fetched lazily only
  after owner opens them, with Node selection revealed inside the expanded
  inspector for an authenticated Node.
  No fabricated time series, synthetic sales/user counts or billing labels.
- Missing CPU/RAM readings produce an unknown bar, not a fabricated empty 0%.
- Browser transport validates fleet counts, Node identities, finite CPU, RAM
  and network rates; malformed responses never replace the verified snapshot.

## Request budget

- Fleet polling (one GET every 5s) runs only on **Dashboard**, **Nodes** and
  **Inbounds**, the only pages dependent on its data. No fleet fetch when
  signed out or signed in as representative, or when the owner visits
  Clients/Groups/Representatives/Finance/Account/Settings.
- Hidden tabs do not poll; returning to a visible tab or network reconnect
  immediately refreshes the active infrastructure page.
- One in-flight request maximum, 4s abort timeout. On network errors preserve
  the last verified snapshot with an explicit interrupted-status banner.
- Live sparkline memory is bounded to the last 20 real polled rates per Node
  and pruned when a Node leaves the roster. This ephemeral small series must
  never be presented as persisted 1h/24h or customer billing history.
- No new polling endpoint, backend change, financial read, or external asset.

## Testing and release

- Keep existing Playwright owner/representative, RTL, CSRF, Node Manager and
  native inbound tests green.
- New Chromium cases cover unavailable rates, fresh-only CPU, no eager history
  API, genuine 1h/24h selection, 320px RTL mobile drawer and idle business
  request budget.
- `npm run build` and complete Go + Chromium + Native Metering CI must pass
  on the exact PR head. Deployment only to the newly isolated PRO instance,
  with the previous release kept as rollback.
- LIVE external Node and real per-client accounting accuracy cannot be proven
  by a frontend design change. With zero PRO Nodes, the dashboard must show
  zero authenticated registrations and unknown host rates, never demo data.

## Deferred independent work

Authenticated real external Agent pairing, per-user billable traffic charts,
real Xray connection counts and business KPI widgets require a separately
reviewed backend data contract. Do not reinterpret host TCP or interface
traffic as any of those metrics.
