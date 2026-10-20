# P0-H2: durable HUB queue and pinned Agent transaction transport

P0-H1's executor is now connected to a durable HUB command queue, owner HTTP
controls and an explicitly enabled Agent worker. This is an isolated acceptance
path for **loopback Nodes with no bound clients**, not customer rollout or
multi-node Dark Swap. It is disabled on installed systems by default.

## Exact transaction boundary

An owner Stage request references the current revision of every enabled Native
Inbound and saved route draft on the selected Node. The HUB reads all profiles,
targets and drafts from one SQLite transaction and renders a whole-Node candidate.
Missing targets, changed revisions, mixed/public addresses, any bound client and
Swap forwarding are rejected. Private Xray configuration is never accepted from
browser JSON and never returned to an owner view.

SQLite stores the immutable candidate in an AES-GCM envelope, bound to command,
Node, installation and serial. The snapshot binds both Native configuration
generation and all current draft revisions. Delivery, Apply and renewal recheck
this snapshot. Rollback explicitly selects an earlier acknowledged Stage while
binding a new command to the owner's current reviewed runtime and source state.
It never silently changes Native targets, draft acceptance flags or billing.
Current/previous transaction Stage IDs are observed separately from the core
supervisor's SHA history, including after an interrupted Apply and explicit
restaging; rollback authority follows the acknowledged transaction Stage.

Each Node has at most one pending command. Agent polling carries a signed fresh
observation over the existing certificate-pinned TLS 1.3 transport. The first
poll persists the complete signed command before returning it. Every subsequent
attempt returns exactly those same bytes, including the original lease. Runtime
instance, reviewed condition, serial, revision, payload and action are signed.
A previous executor instance cannot use the command for new execution.

The Agent executes one command and sends the complete signed receipt. The HUB
verifies that receipt against its exact persisted command before committing a
result. A lost response before or after commit is idempotent. An ACK is historical;
it cannot replace a new observation after a restart or prove current liveness,
customer connectivity, subscription publication or billing acceptance.

## Expiry and recovery

Stage commands expire within two minutes. Apply must still fit inside that
Stage's lifetime; runtime leases last at most 30 seconds. Polling does not
implicitly Apply, renew or recover a process. Renewal is an explicit owner
command. Thus this acceptance path stops after its lease unless explicitly renewed.

Expired, stale or interrupted deliveries do not advance to success. The owner
can explicitly abandon a queued/delivered command while acknowledging that it
**may have executed**. Abandon does not claim the process stopped. Late receipts
remain historical on that abandoned command. A new Stage requires a fresh Agent
observation and, following a crash with pending intent, the exact interrupted
command hash. Existing short leases still fence owned children.

Commands, immutable receipts and the exclusive Node claim survive HUB restart.
The current observation is separately age-limited to 12 seconds. Owner history
returns at most 50 redacted commands; a channel stops accepting additional
commands at 10,000 records, pending a separately reviewed archival policy.

## Opt-in and API contract

The HUB requires Native Inbounds and its dedicated TLS Agent listener, plus:

- `DARK_PRO_ENABLE_ROUTE_TRANSACTIONS=1`
- `DARK_PRO_ROUTE_NODE_ID`, `DARK_PRO_ROUTE_INSTALLATION_ID`
- `DARK_PRO_ROUTE_XRAY_SHA256`

The Node must already be paired, have only loopback targets, have zero client
bindings and have no ordinary runtime delivery or usage epoch history. The
persistent claim blocks ordinary native delivery and customer attachment, even
if the HUB later restarts with the route flag off. There is deliberately no
implicit release or installation reassignment; a release protocol will need
proof of stopped state and settlement before customer integration.

The Agent separately requires `DARK_PRO_ENABLE_ROUTE_TRANSACTIONS=1`, pinned
remote TLS and core, its independent state key, and explicit
`-managed-runtime-dir` / `-managed-listen-ip`. It opens P0-H1's separate private
runtime and refuses simultaneous ordinary native or metered runtime opt-in.
No installer, deployment script or existing service enables these flags.

Owner-cookie, same-origin and CSRF protected endpoints:

- `GET /api/v1/workspace/native-nodes/{node}/route-transactions`
- `POST` to that path: `{id, action, stage_id, runtime_instance, expected, drafts}`;
  `id` is an owner request's stable 32-character lowercase hexadecimal ID.
  `action` is `stage`, `apply`, `rollback` or `renew`. Only Stage includes the
  complete `{inbound_id, inbound_revision, draft_revision}` list.
- `POST .../{id}/abandon` with `confirm_execution_may_have_occurred: true`.

The private poll and receipt endpoints exist only on the Agent TLS listener:
`POST /api/v1/agent/route-transactions/poll` and `.../receipt`. Representatives,
anonymous clients, another installation, wrong pin/signature and plaintext
transport cannot use this channel. The browser has no new activation button yet.

## Verification and remaining work

Tests cover encrypted persistence/reopen, frozen delivery, full-ACK tampering,
commit failure and replay, stale drafts, conflicting owners, incomplete Node
snapshots, permanent exclusion of ordinary delivery/customer binding, owner
permissions, CSRF/origin and public/private route separation. The mandatory
stock-Xray test uses real loopback sockets and pinned TLS: Stage without opening,
Apply with lost ACK before commit, replay, a second port with lost response after
commit, fresh-revision Rollback, and stopped state after Agent restart. CI runs
this under Race and rejects skipped or missing acceptance.

Remaining: owner UI controls and evidence display, a reviewed channel release
protocol, metered-epoch settlement and customer rollout, coordinated Dark Swap
forwarding and exact destination accounting. Existing P0-G readiness continues
to report those unresolved gates honestly. No old HUB access, operational Node
change, USA Xray activation or real-data migration is part of this increment.
