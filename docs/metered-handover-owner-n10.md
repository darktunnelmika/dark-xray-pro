# N10 — owner API and Node Manager transfer review

The owner can now open **Transfer between LAB Nodes** in Node Manager. Only
Nodes already approved by the operator for metered TCP loopback execution can
be selected. The browser cannot expand that allowlist, enable Agent transport,
start the USA test core or import any legacy identity.

## Workflow

1. Select different source and destination Nodes. The complete valid destination
   drafts can be explicitly staged from this view. Staging starts no listener.
2. Review both installations, Inbounds, draft/inbound revisions, ports, prepared
   stage IDs and the source's exact unsettled epoch. Recording reserves both
   Nodes through the N9 journal without starting or stopping either process.
3. Separately confirm source Drain. The UI shows the durable queued/delivered
   command and its receipt; it never substitutes a poll for a final accounting
   receipt or advances automatically.
4. After the exact source Drain ACK and HUB final ledger seal, review fresh
   observations and explicitly request destination Apply. The business-store
   N9 barrier is rechecked during queueing, delivery and birth authorization.
5. A destination Apply ACK is shown as **health verification pending**. There
   is no publication endpoint, implicit completion, automatic lease renewal or
   cross-Node rollback. LAB leases remain at most 30 seconds.

Each mutation body is saved in tab session storage before transmission. Lost
responses survive reload with identical operation/command IDs, body, Node pair
and installations. Refresh reconciles immutable operation links and public
command audit metadata; it never sends another mutation. Unconfirmed responses,
including refusals following an earlier lost response, retain the request.
Discarding a local retry is explicitly separate from cancelling a server
operation and proves no execution state. Storage failure prevents transmission.
Session storage contains no credentials, runnable config, private keys or signed
Agent packets. Stage drafts are complete revision references, not Xray bytes.

Cancellation is allowed only before a Drain step has committed. After an
unknown/failed transition, both reservations remain for a later reviewed
recovery workflow. Historical receipts do not prove current liveness.

## HTTP and installation fences

All routes below require the browser owner session. Mutations require same-site
Origin and CSRF. Representatives, Agent credentials and owner bearer tokens
cannot use them as browser authority.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/workspace/metered-handovers?before=<opaque-id>` | At most 50 immutable history entries, newest first |
| POST | `/api/v1/workspace/metered-handovers` | Record the exact reviewed pair and reserve both Nodes |
| GET | `/api/v1/workspace/metered-handovers/<id>` | Operation and public Drain/Apply audit metadata |
| POST | `/api/v1/workspace/metered-handovers/<id>/commands` | One explicit Drain or Apply |
| POST | `/api/v1/workspace/metered-handovers/<id>/cancel` | Confirm cancellation before Drain |

History remains readable when runtime flags are disabled. Every mutation
rechecks both immutable installation identities against the startup allowlist
and current registry under the replacement lifecycle lock. Legacy preflight
creation refuses any persistently metered-owned Node; startup refuses a
metered claim overlapping a noncancelled legacy operation in either role.
The owner creation/step handlers also repeat that exclusion. The exact shipped
nginx locations expose only the listed methods; Agent/admin/lookalike paths
retain default denial. No new public Agent endpoint is added.

The schema stays at v8, adding only a history index. Full pre-upgrade
state/config/binary backup and runtime-disabled PRO LAB deployment remain
required. No live user, representative, credit or bot data is migrated.

## Evidence and remaining work

Race tests cover owner/representative/CSRF/Origin/installation scope, immutable
retry, source ACK and accounting barrier, disabled-runtime history, legacy
cross-role conflicts, bounded cursor pagination and atomic pre-drain cancel.
Browser tests cover separate confirmations, preparation, no automatic next
step, lost response before/after commit, reload/exact retry, storage failure,
invalid publication claims, read failures, representative isolation and Persian
320px layout. The dedicated CI contract step repeats API/store/schema tests ten
times. The nginx matrix exercises real path/method forwarding and default denial.

The isolated real-traffic fixture selects customer ports outside the verified
kernel ephemeral range, preventing its TLS receiver, SOCKS client and outbound
connections from claiming a planned customer port before Apply. The strict
closed-listener accounting checks remain in place. CI additionally repeats the
real unknown-tail scenario twenty times in a disposable network namespace.

Complete two-Node real-traffic transfer acceptance, protocol health proof,
subscription generation/publication and an explicit cross-Node recovery and
rollback workflow remain required before production use or enabling any real
customer runtime.
