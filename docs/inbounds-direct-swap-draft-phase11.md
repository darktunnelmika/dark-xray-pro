# Phase 11 — Direct + Dark Swap Inbound draft preview (LAB ONLY)

This phase replaces the Inbounds placeholder with a **client-side preview** for
exactly one explicitly selected **NEW** paired PRO Node and Inbound ID. It does
not create, apply, persist, migrate or publish an Inbound. A browser refresh
discards the draft. The existing HUB and every production Node remain untouched.

## Contract

- Source of target Nodes: the verified cached `GET /api/v1/fleet` snapshot
  (no invented Nodes, no owner bearer token in the browser).
- Direct toggle: one Node port.
- Dark Swap toggle: exactly **three** operator inputs — destination tunnel
  port, Node port and tunnel address. Both toggles may be active simultaneously.
- No route is implied by another Node's settings and no old tunnel is inherited.
- Client-side checks reject invalid/empty Inbound IDs, ports outside 1–65535,
  missing routes and malformed DNS/IPv4 hosts. This simplified browser draft
  currently accepts IPv4/DNS only; Go server validation remains authoritative.
- If Direct and Dark Swap share the same Node port, the preview warns that
  per-route traffic attribution is ambiguous, not an independent billable metric.
- Every preview is explicitly **activation blocked** and requires independent
  Agent identity, TCP preflight, Xray syntax, real VLESS/REALITY data-plane
  evidence, previous runtime snapshot, tested rollback and owner approval.

## Browser safety

The draft performs **no POST or mutation**, does not use localStorage to save
candidate routes, does not request Node credentials, and does not call the owner
admin API. The standalone React component is not a replacement for
`internal/routeplan` or `internal/xrayconfig`. Public reverse-proxy API rules
remain unchanged, as do the owner token and private encrypted staging.

## Verification

Playwright tests cover zero new Nodes, exactly three Dark Swap inputs, combined
Direct + Swap routes, shared-port ambiguity, distinct-port warning, invalid
host, absence of POST/admin calls and 320px RTL layout. Go/Race/Vet, TypeScript,
pinned real Xray and the LAB bundle CI must remain green.

## Next independent milestones

1. Pair an **unused remote test Node** through the dedicated pinned TLS 1.3
   Agent listener, without authorizing the legacy fleet.
2. Add owner-authenticated server-side **versioned, encrypted** Inbound draft
   persistence and compare-and-swap staging, matched to exact new Node installation.
3. Rehearse a **real isolated Xray process Apply and automatic Rollback** with
   a last-good snapshot and signed revision ACK. Verify a real VLESS/REALITY
   handshake, not just listener bind.
4. Only after the test route is proven, design an atomic subscription cutover
   separately. Billing must avoid counting the two hops of Dark Swap twice.

No production Apply, no live subscription publication, and no changes to
previous DARK XRAY clients or tunnels occur in this phase.
