# PRO Node Manager — Owner-only safe enrollment (new Nodes only)

This change is an authenticated *first slice* of the PRO Node manager. It
does not migrate any existing DARK XRAY Node or its clients, import old
identities, activate traffic, or mutate the older production panel.

## Supported owner flow

1. Owner signs in through the same unified session as all other sections.
2. The Nodes screen displays the live heartbeat fleet (3-second pulse,
   independent 5-second CPU/RAM/network measurements) plus the durable
   registration roster, including offline entries after a HUB restart.
3. Owner creates a **new** Node ID and name; the backend returns a random
   256-bit one-use Pair Code valid for ten minutes.
4. The code is shown *only* in the POST response and browser tab memory.
   Neither GET roster nor fleet contains the code, agent credential or
   owner bearer. Reloading or changing sections discards the displayed code.
5. Owner can explicitly revoke an **unclaimed** invitation by Node ID.
   Revocation is durable and cannot alter an already-paired Node, its Agent
   identity, traffic, accounting, users, tunnel or desired config.
6. Only a freshly installed Agent that locally generates its own installation
   identity, trusts the HUB's TLS certificate and verifies the pinned leaf
   fingerprint can redeem the code via the **separate TLS 1.3 Agent listener**.
   Server-side status comes from authenticated Agent pulses, never from the
   act of generating a Pair Code.

This is not a one-click Agent installer. The current isolated review
environment's TLS Agent listener is **loopback-only**, intentionally not
internet-accessible. Testing a genuinely separate external QA VPS needs a
planned dedicated public Agent listener, validated IP/DNS SAN, TLS pin,
firewall/rate-limit review and explicit approval. Do not point an old Node
or an existing customer installation at the PRO lab.

## Owner browser surface (new routes)

- GET `/api/v1/workspace/nodes` — owner-only safe roster; NO Pair Code.
- POST `/api/v1/workspace/nodes/invites` — owner session + same-origin JSON
  + CSRF, body `{"node_id":"new-pro-01","name":"New PRO"}`, one-time 201.
- POST `/api/v1/workspace/nodes/invites/{id}/cancel` — same guards, body
  `{}`; only revokes an unclaimed invitation.

Deployment must explicitly allowlist exactly these three route patterns and
their methods in the **new PRO** reverse proxy. The Agent-only and local
bearer routes must remain blocked on the owner HTTPS listener.

## Acceptance and next blockers

Go integration tests verify owner/representative scoping, strict origin and
CSRF, invalid request rejection, high entropy and one-use code, idempotency
policy, secretless roster, revocation and preserved paired identity. Real
Chromium tests cover login, ticket lifecycle, disappearing code after reload,
320px Persian and no representative access.

Full Node manager is NOT finished: safe delete/retire needs zero-reference
checks; replacement needs a truthful desired/observed switchover without
losing clients; uptime/disk/load/connections, 1h/24h persistent graphs and
multi-host live Agent testing still require separate accepted increments.
The usage-ledger admission gate remains closed for metered customer inbounds.
