# Durable checkpoint isolation investigation

Baseline release: 6062c2cd150816d80bfb08fde4ccec2b04d8b827.
Candidate source: 0da4e0bac7c6f1c3bac398831c4d3fc1b13d9986.

## Reproduction

Independent owner-only inbounds 918 (HUB) and 919 (USA), port 18572. No shared-client bindings. Unused legacy draft 916 was archived after its spec incompatibility was found; it never ran. Client isolated-r2-916-20261108 was created but never bound or charged to a representative.

Six-minute HUB trace captured fsync fd=9 at 30224ms. /proc/20214/fd/9 identified business.sqlite; fd10 was business.sqlite-wal. Kernel task stack concurrently waited in folio_wait_writeback -> ext4_sync_file -> fsync. HUB-local Usage deadlines and lease runway drain aligned with that pause. Physical provider fault is not established; synchronous automatic checkpoint on the accounting writer is a supported hypothesis for propagating this delay to leases.

Baseline traffic: HUB 143/150, seven consecutive failures during the stall, then automatic recovery. USA 134/150 with a deliberate control-bridge interruption, then automatic recovery. Stop requested at UNIX1794098471.693052; transient systemd recovery timer requested +35s (actual timing includes timer accuracy and SSH reconnection). No customer subscription manipulation. Owner test shared client held=0 in all 70 five-second samples. HUB isolated held=0. USA isolated held=1 in four samples from UNIX1794098502.439192 to1794098517.4405386, cleared automatically; no owner-loss reconciliation used. Both isolated runtimes remained active/active afterward.

## Candidate

Automatic checkpoint disabled on every accounting writer connection through DSN, including replacement connections. Dedicated single-connection worker runs PASSIVE checkpoints every5s when WAL file >=1MiB. Both writer and checkpointer retain synchronous=FULL. No extra business writer, no schema change, no async acceptance of undurable usage, no lease relaxation. Worker is single-flight and joins on Store.Close. Errors retain WAL and retry; slow checkpoints are logged.

SQLite upgraded from modernc.org/sqlite v1.40.1 to v1.48.2 (SQLite3.51.3) with matching libc1.70.0 because the official WAL-reset correction is required before introducing concurrent checkpoint activity. Sources:
- https://sqlite.org/wal.html (automatic checkpoints run on the committing thread; separate-thread checkpoints supported; FULL WAL commits retain sync).
- https://sqlite.org/releaselog/3_51_3.html (WAL-reset correction).
- https://pkg.go.dev/modernc.org/sqlite@v1.48.2 (bundled3.51.3 and matching libc requirement).

Tests: independent accounting pool remains writable while checkpoint connection is occupied; PASSIVE checkpoint progresses; quick_check succeeds; SQLite version and FULL sync asserted; replacement writer connections retain wal_autocheckpoint=0 and FULL. Targeted business suite passed42.197s. Vet business/HUB/hubstate passed. Full-project suite initially reported one API login_unavailable failure under combined load; isolated exact test passed3.201s. Full-project run completed: 45 packages passed, business404.229s, inboundruntime11.039s, hubstate3.805s. Initial all-package command exited1 due to that API failure; complete API rerun passed93.708s. Race unrun (gcc unavailable).

Candidate artifacts (clean VCS metadata): HUB a2eb73260d49ea00ba9bfc688eec4fb3ddda1616056a74a514a6af789c5a96cb; agent7a0a5a604e23fa7874ad01e7ee12d8e18c6f10eaa7ce5a2a8aa271d733f7668e. Installed HUB and all five live PRO agents; disabled historical fixture untouched. Validated private SQLite backup at /root/dark-pro-checkpoint-r3-20261108/business-before.sqlite on HUB (quick_check=ok).

## Live rollout

Old HUB6062 shutdown at00:53:18-23UTC lost its isolated918 final proof after lease_expired; candidate boot inherited uncertain epoch3c5ccaf478778878f51296d1ed854ccf. Cause not proven. Owner-only reconciliation accepted unmeasured test tail as owner loss, preserving measured107220 upload/466270 download. Audited restart resumed918. Shared912 and USA919 never required reconciliation in this rollout. This is not evidence that candidate fixes shutdown uncertainty. Candidate two-path soak and candidate HUB restart verification pending.

## Candidate live limitation reproduced

At approximately00:59:40UTC, concurrent fsync waits on HUB reached30756ms on fd10 (business.sqlite-wal),29293ms on fd15 (same WAL via checkpoint connection),21795ms on transient fd38, and6310ms on fd7 (telemetry/history.sqlite-wal). Main file fd14 later2535ms. This disproves checkpoint isolation as a complete remedy: FULL durable accounting commits themselves stall. HUB isolated918 became uncertain again; USA919 recovered automatically (fresh client3/3 passed), shared912 remained active with no held usage. The local ext4 filesystem /dev/sda3 has29GiB free (16% occupied); available RAM1290MiB, swap1MiB used. I/O pressure full60s30.58%, some32.26%. No kernel errors in sampled15minutes. Physical provider fault not proven. No synchronous durability downgrade or fail-open applied.

Completed candidate soak: HUB73/120,47 failures from round73 onward (held uncertain); USA111/120,9 failures rounds59-67 then automatic recovery. Duration342.83s.65 accounting samples: shared0 holds, USA0 holds, HUB37 held samples. No deliberate network fault during candidate soak. Independent4KiB fsync probe outside the database ran afterward90 samples, max12.1ms, no >1s: intermittent filesystem/provider cause is still unproven, no claim that every write is slow. Post-stall iostat2s samples were low latency.

Shared subscription retest: six endpoints each20/20 HTTPS through VLESS Reality gRPC,120/120 aggregate; direct UK/Germany/Finland/Poland, Germany tunnel5.202.75.85:2400, Poland tunnel5.202.75.78:1190. This was a short post-stall test, not a zero-outage guarantee. HUB/USA remain excluded from shared plan912 pending stability acceptance.

## Handoff

All six live binaries rehashed after rollout; HUB matchesa2eb7326, all five agents7a0a5a60, release0da4e0b. Each service active, NRestarts0, KillMode=mixed. Known issue remains reproducible under ordinary owner test traffic. Do not re-add HUB/USA to shared912 until longer tests and durable final-capture under filesystem stalls pass. No durability weakening, no implicit acceptance of unknown customer usage. Independent probe does not establish provider fault; capture block-layer/fsync stacks during next incident before assigning root cause. Owner-only918 second uncertain tail reconciled as explicitly audited owner loss; both isolated test stop/archive commands issued. Temporary artifact HTTP18089 stopped. All measured traffic retained. No legacy production, DNS, representative, bot, or customer migration changes.
