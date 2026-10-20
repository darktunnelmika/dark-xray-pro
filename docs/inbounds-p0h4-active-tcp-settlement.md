# P0-H4B: experimental final counters for active direct TCP

This change adds an opt-in internal supervisor path. It does not install a
custom Xray, enable a customer route, enable the public route-transaction flag,
or migrate any old HUB data. The deployed PRO LAB remains at its previous
reviewed release until a separate deployment receipt says otherwise.

## Why stock stop operations cannot seal active usage

At pinned Xray v26.9.9, removing an inbound closes its listener but does not
join existing requests. `common/task.Run` can return on cancellation or a first
error while sibling I/O tasks still run. Linux Vision splice adds user counters
after `TCPConn.ReadFrom` returns. Neither a successful RemoveInbound nor an
unchanged StatsService sample proves the final billable counter value.

## Precisely bounded core change

`scripts/prepare-barrier-core.py` checks upstream commit
`52a412d9e2f5c2a5142b1b4e2ab3771dacb8b120` and SHA-256 for every modified file.
It applies four small source changes and copies the tracker and stats adapter.
The build retains upstream's module checksums and Go toolchain requirement;
the main PRO module and its dependencies stay unchanged. Modified upstream
files remain under Xray's MPL-2.0 license. The patch and added source are in this
repository so the exact modified build can be reproduced and reviewed.

The new mode requires `ExperimentalFinalTCP` and the child-only environment
option `DARK_PRO_FINAL_TCP_V1=1`. It is not exposed as an Agent CLI or fleet
setting. A normal core/client invocation preserves ordinary behavior.

The admitted configuration is the existing strict native VLESS/Reality profile
with TCP transport, user-level counters and one direct freedom outbound.
gRPC profiles are rejected at prepare. The core rejects UDP, Mux, XUDP and
reverse request commands before dispatch, and rejects asynchronous dispatcher
escape from a tracked session. It does not claim support for arbitrary Xray
configs, transports, routing chains or detached workers.

Each VLESS Process enters a process-local gate before reading a request. Each
`task.Run` child joins the same tracked lifetime **before** its goroutine starts.
Drain irreversibly closes admission, cancels and closes captured connections,
and waits for every tracked task. Only then does it read user counters. An
in-flight splice therefore performs its final Add before the snapshot.
Counter resets are refused in this mode. A 256-bit drain nonce identifies one
immutable snapshot; timeouts do not reopen admission, change the nonce or read
a second snapshot. Existing private StatsService transport carries capability
and final receipts; no new network listener is added.

## Supervisor and durability order

1. Before activation, prove capability on the same pinned binary, kernel birth
   and private API owner used for the initial zero receipt.
2. Persist the drain nonce in encrypted journal v5. Old binaries reject v5.
3. The collector serializes the final request after ordinary observations while
   the independent sample, receipt and child lease guards remain enabled.
4. Validate the exact nonce marker and all counters. Previously seen directions
   cannot disappear; only never-seen users with a proven zero baseline remain
   zero. Persist the terminal report, then its exact identity in the journal.
5. Stop and wait for the captured child. Only the durable final proof suppresses
   interruption notification. A failed, interrupted or unproven tail remains
   uncertain; no dead process is queried and no missing outbox is recreated.
6. Replay reports in order and obtain the separate authenticated HUB seal for
   that exact terminal report. An ordinary receipt is insufficient; pre-existing
   uncertainty remains held even when final known bytes are charged.
7. Only the exact sealed receipt allows a fresh higher signed revision and a
   new registered process birth through the existing initial-receipt gate.

`SettleActiveTCP` is internal, and does not itself grant Apply, Rollback, quota,
subscription publication or route-queue authority. The owner transaction path
still needs explicit integration with this narrower metered lifecycle.

## Verification

Pure Race tests delay a final counter update until after the parent returns and
fork another child during drain. They cover timeout continuation, nonce
conflicts, immutable replay, missing counters and invalid journal proofs.
Foundation CI builds the separate pinned overlay and runs six real
Reality/Vision TLS-stream scenarios three times with the PRO Race detector:
replacement, lost terminal receipt, lost seal confirmation, pre-existing unknown
tail, interruption without proof, and operation without opt-in. Every selected
case must execute and pass; a skip is a gate failure. Stock-core acceptance,
browser, queue and observer regression jobs continue independently.

A separate real-core protocol test checks exact bidirectional payload bytes,
UDP/Mux/reverse rejection, reset refusal, immutable snapshot replay, conflicting
nonce refusal and post-drain admission closure. Its plain loopback VLESS fixture
tests the core primitive only; native runtime admission still requires Reality.

During local upstream test exploration, `common/task/TestPeriodicTaskStop`
reported an existing unsynchronized test-variable race. The overlay does not
change periodic tasks. Its targeted upstream gate runs `TestExecuteParallel*`
and all custom tracker tests; it does not claim that upstream's full test suite
passes Race. Real final-counter acceptance remains a separate mandatory gate.
