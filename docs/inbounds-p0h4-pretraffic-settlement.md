# P0-H4A: exact settlement of a never-installed metered birth

A lost initial receipt previously left even an API-only Xray birth permanently
fenced and later recovery marked its clients uncertain. This increment permits
settlement only when a new authenticated journal proves that customer Inbounds
were never installed. It does not implement final draining of active traffic.

## Proof and ordering

New metered preparation writes a version-4 encrypted journal with
`traffic_barrier=never_installed_v1`. Before starting the observer or attempting
AddInbound, activation fsyncs `activation_possible_v1`. That transition is
one-way. A crash after it conservatively retains the unknown-tail fence, even
if the customer listener had not opened yet. Version-3 admissions have no such
proof and cannot use this exception. Older binaries reject version 4.

`SettleUnopenedUsage` serializes with activation/Apply/Refresh under the Runtime
lock, persists settlement intent, stops and waits for the captured child, and
opens its encrypted outbox. Previously saved nonzero counters, corrupt state,
or an uncertain activation are rejected. Zero is justified by durable absence
of any customer admission, not by missing/idle StatsService counters. No PID is
adopted or killed after a supervisor restart.

The terminal report uses the same epoch and monotonic outbox sequence. All
earlier reports and the terminal report need exact durable receipts. A separate
CA-verified, leaf-pinned request to the Agent-only
`POST /api/v1/agent/native-usage/settled` then confirms that the HUB's exact
cursor is sealed. This read-only endpoint checks Registry identity, installation,
receipt hash/sequence and the committed quiesced cursor in one SQL snapshot.
An ordinary report receipt is insufficient: an uncertain epoch may accept known
bytes while its hold and unsealed state remain. The confirmation cannot clear
such a hold or modify balances.

Only after the seal response is persisted does status expose `settled=true` and
`settlement_required=false`. Lost terminal ACKs, lost seal responses and crashes
before the local commit can retry the same outbox/cursor. Persistence failure
fences the runtime. No fallback infers a seal from an empty pending queue.

The Agent recovery worker uses this exception only for proven unopened births.
It may subsequently request a new delivery. A second Prepare still requires a
strictly higher signed revision and newer lease, a fresh API-only process birth,
fresh HUB epoch registration and the zero-receipt barrier before first traffic.
The old configuration/lease never gains automatic recovery authority.

## Verification

- Ledger tests distinguish open, sealed, wrong/stale receipt, foreign principal
  and uncertain epochs; exact confirmations survive SQLite reopen and preserve
  existing charged bytes/holds.
- TLS sender tests reject wrong hash/sequence/epoch, ordinary receipts, false
  seal, duplicate JSON fields, redirects, wrong pin and installation.
- Runtime tests exercise legacy/activation proof refusal, nonzero saved bytes,
  lost receipts, reopen and failure to persist the final seal.
- Registry API tests cover anonymous/foreign tokens, public listener denial,
  old TLS, malformed/oversized bodies and idempotent exact responses.
- Pinned Xray v26.9.9 acceptance exercises restart, lost terminal receipt, lost
  seal confirmation, preserved unknown hold and a new process/epoch admission.
  The Native Metering workflow requires all five cases to pass three times under
  Race; missing/skipped cases fail the gate. Existing first-byte admission and
  active-observer tests remain in place, with active pretraffic settlement refusal.

## Active traffic remains blocked

Source review of the pinned upstream shows why RemoveInbound is not sufficient:
[tcpWorker.Close](https://github.com/XTLS/Xray-core/blob/v26.9.9/app/proxyman/inbound/worker.go)
closes the listener/proxy but does not wait for every existing callback;
[VLESS RemoveUser](https://github.com/XTLS/Xray-core/blob/v26.9.9/proxy/vless/inbound/inbound.go)
removes validator authority; and
[task.Run](https://github.com/XTLS/Xray-core/blob/v26.9.9/common/task/task.go)
can return on an error/cancellation while other tasks still unwind. An unchanged
sample, absent listener or successful API call is therefore not selected as a
final-counter barrier.

Active/legacy epochs still require a separately verified data-plane drain and
final receipt before metered Apply/Rollback. No guessed zero tail, automatic
hold clearance, patched Xray selection, route-channel customer admission,
multi-node accounting claim, production readiness or migration is added here.
This PR has no deployment job and does not enable the USA test Xray or modify
the old HUB/operational Nodes.
