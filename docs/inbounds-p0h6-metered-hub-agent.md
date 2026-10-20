# P0-H6 — independent metered HUB/Agent command transport

## Boundary

This opt-in channel connects the P0-H5 v2 interpreter to durable HUB commands,
trusted TLS Check/Register calls and an owner-only receipt interface. It does
not upgrade the v1 empty-loopback channel or an ordinary native runtime.
Only a never-delivered, separately claimed loopback installation qualifies.
The claim, commands, births and receipts survive removal of startup flags.

Scope: native direct VLESS / REALITY / Vision TCP, eligible disposable PRO
identities, no Tunnel or Dark Swap forwarding, no UDP, no subscription publish.
No old HUB, operational Node, customer migration or USA Xray activation is
authorized by this change. All new switches remain OFF on deployed PRO LAB.
The experimental core is not part of the LAB distribution bundle.

## Trust and ordering

- Separate encrypted queue, MAC poll domain, immutable Node/install/core claim,
  v2 usage precondition and exclusive ownership; ordinary delivery remains
  blocked by SQLite triggers. Browser cannot enable or reassign a channel.
- Stage renders all enabled inbound drafts and eligible bindings in one SQL
  snapshot. Owner requests contain revisions, hashes and usage receipt IDs,
  never private Xray config, UUID maps or signed Agent commands.
- Queue, delivery, Check and Register re-evaluate source generation, draft
  snapshot, current profiles, loopback topology and current usage eligibility.
  Expiry also bounds the lease. Agent cannot submit an invented command.
- Register resolves the exact delivered command, validates its signed birth,
  derives billing bindings on the HUB, and commits epoch + command-bound birth
  atomically. A second birth for the same command is rejected.
- Replacement requires the prior ledger final seal; the interpreter also
  verifies old process stop and counter drain. Check repeats after settlement.
  The real listener opens only after the new epoch's initial durable receipt.
- HUB verifies historical ACKs against its own epoch registration, initial
  receipt and prior final seal, not just the Agent HMAC.
- Delivered packets remain replayable after source changes solely to recover an
  existing exact historical ACK. Fresh Check/Register authority is still denied.
  A lost registration response remains fail-closed and requires reconciliation;
  no guessed-zero epoch or automatic admission retry is introduced.
- Rollback requires freshly reviewed current drafts rendering exactly the saved
  candidate, current eligibility, a new lease and a distinct process epoch.
  This deliberately cannot restore stale/revoked profiles automatically.
- Polling never creates Apply/Renew. Automatic historical usage replay may
  reconcile durable final proof but never restarts a listener.

## Interfaces and opt-in

Owner discovery: GET /api/v1/workspace/metered-route-transactions.
Owner channel: GET/POST /api/v1/workspace/native-nodes/{node}/metered-route-transactions,
plus explicit /{id}/abandon. Owner role, same-origin and CSRF required for writes.

Private TLS 1.3 Agent-only POST paths under
/api/v1/agent/metered-route-transactions/: poll, receipt, check, register.
Existing verified CA/leaf-pinned Agent transport is reused; redirects denied.

HUB opt-in: DARK_PRO_ENABLE_METERED_ROUTE_TRANSACTIONS=1 with LAB, usage-ledger,
native-inbound and remote-Agent gates, plus DARK_PRO_METERED_ROUTE_NODE_ID,
DARK_PRO_METERED_ROUTE_INSTALLATION_ID and DARK_PRO_METERED_ROUTE_XRAY_SHA256.

Agent opt-in: DARK_PRO_ENABLE_METERED_ROUTE_TRANSACTIONS=1 and
DARK_PRO_ENABLE_FINAL_TCP=1, paired pinned TLS/core, independent private runtime
directory and key, loopback listener. Cannot combine with ordinary runtime,
ordinary remote metered admission or v1 route executor.

Owner UI separates latest observation from historical ACKs, displays exact
epoch/seal identifiers and string-valued uint64 receipt sequences, retains
same-ID retry identity and requires separate action confirmations. A HUB-
registered epoch is explicitly not proof that a listener opened.

## Verification

- Repeated Race: v1/v2 separation, permanent claims, encrypted persistence,
  changed review/idempotency, forged command refusal, current revocation,
  atomic birth registration/replay, and ledger-backed receipt verification.
- Browser: owner-only controls, CSRF, stale observations, server-clock lease
  expiry, exact retry after reload, historical ACKs, responsive Persian UI.
- Mandatory nonroot pinned-core CI: actual HUB/Agent TLS and SQLite, zero-before-
  admission, replacement/rollback/renew, ACK loss before/after commit, revoked
  authorization after final seal, lost registration response, lost seal replay.
  Six scenarios run three times; skipped tests fail the acceptance gate.
- Existing H4/H5 suites continue proving active Vision streams and late exact
  final counters. New transport fixture intentionally generates no customer
  traffic; full traffic-bearing HUB-command acceptance remains a separate gate
  before any wider deployment.
