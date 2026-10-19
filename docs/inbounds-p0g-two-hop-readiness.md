# DARK XRAY PRO — P0-G two-hop operational readiness (LAB only)

This phase adds a truthful, owner-only READ-ONLY checkpoint for a saved
Native Inbound route draft. It does NOT stage Xray, apply a route, create
a tunnel, publish a subscription, debit credit or contact any legacy Node.

The endpoint is GET
/api/v1/workspace/native-inbounds/{inbound-id}/route-drafts/{node-id}/readiness
and requires a valid owner cookie/session. It is not exposed by the
separate pinned-TLS Agent listener. The response never contains URLs,
secrets, customer identifiers, private configs or Agent credentials.

Sources of evidence are independently read on each request:
- Existing AES-GCM-authenticated saved draft, with its expected Native
  Inbound revision and fresh same-Inbound graph validation.
- Actual NEW PRO Node registry installation IDs, per-node cached Agent
  heartbeat and metric freshness. Healthy destination cannot mask an
  offline or stale upstream.
- Native RuntimeStatus from the authenticated signed Agent-report
  journal for each installation. An old Native 'applied' report is NOT
  an ACK of this new, still unapplied Dark Swap draft.
- Current Native Inbound targets. Changed port, listen IP or installation
  invalidates plan consistency, even if the browser still holds an older
  preview or the original route draft is decryptable.

All reports deliberately set route signed ACK, real multi-hop forwarding,
client VLESS/REALITY data plane, exactly-once billable attribution,
automatic last-good rollback and customer subscription publication to
FALSE. ApplyAllowed is always FALSE.

Tests: owner/representative/anonymous access separation, wrong HTTP
method and Agent listener denial; independent destination/upstream
offline/online report with no fabricated ACK; changed target port
invalidates the saved plan without writing any Node/Xray/client/ledger
state. Real Chromium mobile 320px demonstrates truthful blockers.

Next milestone: on owner-approved **NEW independent test Nodes only**,
build durable versioned agent-side route stage with signed installation-
bound ACK, real connectivity probe, scope-bound usage comparison and
last-good rollback. No OLD HUB data, users, representatives or bots
may be accessed or migrated while the panel is unfinished.
