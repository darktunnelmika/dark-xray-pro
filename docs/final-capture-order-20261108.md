# Seal before journal I/O

Source970104f59f40c93e532f1793f333ccd084b2dd09. Prior candidate0da4e0b retained FULL durable checkpoints but still lost HUB-local final proof during long fsync waits.

Both drain and captureFinal saved the drain nonce before asking the engine to close admission and seal counters. That fsync can consume the hard lease runway. Remove only that preliminary save: retain nonce in memory, request authenticated final, then persist nonce and complete report together before any usage transmission. A crash before durable final still leaves an uncertain epoch. Persistence failure still closes the owned process and prevents later usage delivery. Hard lease kill unchanged.

Regression test initially failed on0da4e0b: journal write failure prevented final capture. Updated tests exercise both drain and capture with an unavailable journal directory and assert no undurable usage escapes. Real engine test kills the owned process after authenticated final but before journal save, then reopens worker and checks exact measured tail settlement.

Full inboundruntime suite with pinned actual core6861c627 passed13.901s. Real sample-timeout/final replay and engine-loss cases passed2.030s. Targeted API execution suite passed1.494s. Vet runtime/HUB/node-agent passed. No race result claimed.

Build clean VCS metadata: HUB d86082f55acff3d034a5d85606483e821bae6342ab8ae0376f780784d69b40f6; agent a8b9cbb58f0ec2730f92f44358cfa82bec7afdbadeaeba529d0d02aedda0e164. HUB installed; other five agents staged pending live check. Owner-only inbound920, port18572, independent one-day1GiB client isolated-r4-920-20261108. Shared912 unchanged. Live180-request soak pending.

## Canary failed acceptance

Actual HUB run: first32 requests passed, then persistent uncertain tail after29.489s fsync on business WAL fd10.73 attempts total,32 passed; stopped early after41 failures to avoid pointless repeated traffic to a fenced epoch. Epoch95073a5d19df831b7c3a30443979c691, generation51. Known14355 upload/96018 download preserved. Owner-only tail explicitly reconciled as owner loss; stop/archive issued.

At01:12:29UTC log delivery and many TLS handshakes resumed together; native lease expiry and final capture proof failure occurred within~11ms of each other. This points to delayed execution beyond the preliminary nonce write; exact blocking path not proven. A later31.001s fsync on fd14 was sampled in kernel folio_wait_writeback/ext4_sync_file. No ptrace tracer, injected strace fault, CPU quota, memory limit, or OOM found. An independent same-filesystem fsync probe runs concurrently to separate process-local and storage-wide effects.

Do not promote this change as the Turkey outage fix. Remaining agents have not been activated. Four downloaded candidate successfully; Germany direct artifact download timed out. Tested source retained for review.

## Safe final deployment state

HUB rolled back to0da4e0b; all five agents remained0da4e0b throughout. Shared912 active/active, four targets accounting accepted, no reconciliations.920 archived desired state, observed blocked with settled accounting (historical owner-resume latch); listener18572 closed. Shared and isolated holds0 after explicit audited owner reconciliation. Public subscription200 with6 paths. R4 artifact server stopped. New source970104f remains committed but unaccepted for fleet deployment.

Concurrent independent fsync: first166 samples maximum13.35ms despite captured slow database-file fsync. Block request trace saw two~2.11s completions, not evidence explaining the entire31s fsync. Kernel sampled database threads waiting in folio_wait_writeback/ext4_sync_file. Next investigation must correlate database syscall, worker goroutine scheduling/locks, page writeback and block completion; neither a blanket provider fault nor a completed software fix is established.
