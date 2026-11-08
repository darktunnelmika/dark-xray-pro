# Idle accounting and subscription read isolation — 2026-11-08

Deployed fleet revision: 5cbd5055c2385a590bf070a7ab30eb6b8e8d8fd2.
Changes: 47c2baa (idle accounting heartbeat), 5cbd505 (subscription read pool).
HUB SHA256 e777987fc703488e080801a0179acc3605ebc5bce52d21d13bf6037c4f85382c.
Agent SHA256 152615013669b158383103d4b0493b6a1ce4a6c53a54d692978889a40d4eda02.
Turkey HUB and all five agents verified active at this exact revision/hash.
Existing native Xray binaries, credentials, endpoints and quotas retained.

Observed old behavior: sampling /proc task states at 0.5s intervals captured
three HUB threads waiting in jbd2_log_wait_commit on fsync (syscall 74): the
business WAL writer, WAL checkpointer, and runtime state journal. The independent
sampler continued (max gap 0.505s), ruling out a whole-process/host pause for this
event. This establishes the wait location, not hardware failure or its cause.
A 180s public subscription probe had 78 samples, five 4-second timeouts.

Safe changes:
1. Unchanged, exactly acknowledged counters refresh within five seconds instead
   of creating a new receipt and two journal writes each active one-second step.
   Actual counter changes still report each step. Poll, authorization, lease and
   health checks retain their cadence. Final capture/replay/persistence fencing
   remain unchanged. The freshness witness is volatile and epoch-specific.
2. Public/private subscription exports and metadata read authorization, token,
   entitlement, aggregate usage and routes in one fresh read-only snapshot.
   They no longer occupy/wait for the durable accounting writer. No admission or
   lease issuance moved to the read pool; no shared cache or stale snapshot cache.

Tests:
- Full inboundruntime suite with pinned real core: passed (15.246s), vet passed.
- Subscription/business read-pool/API regression suites passed; vet passed.
- Business and API diagnostic suites passed.
- A held accounting writer test verifies public/private/metadata reads continue.
- Idle tests verify unchanged inode/ciphertext, continued polls/renewals, immediate
  changed-byte reporting, heartbeat deadline, stop/final settlement and epoch fences.
- Existing active accounting cadence test now explicitly adds traffic each step.
- Eight live Reality gRPC direct/tunnel routes passed 20/20 each (160/160).
- Idle receipt rates fell from roughly 36-58/min to 10-11/min. Poland retained
  roughly 44/min while its counters continued changing.

Limits: this reduces needless durable writes and isolates subscription reads;
underlying occasional long commit latency is not declared eliminated. FULL
accounting durability and fail-closed lease limits were not weakened. Germany
NTP remains unsynchronized: multiple public/provider UDP NTP probes timed out,
configured UFW is disabled and its output policy is ACCEPT. Live firewall policy
was not inspected after the controller rejected the read command. No blanket
firewall changes or bespoke periodic clock-stepping workaround was installed.

A separate diagnostic-only binary was built but never activated. No GitHub CI,
merge, race test, legacy migration or indefinite availability claim.

Final live acceptance: 88/88 public HEAD responses succeeded over 179.54s,
maximum observed local HUB HTTP response 0.10245s; all runtime-ready flags true.
Latest six targets active/ready/accounting accepted, no reconciliations and no
usage holds. Latest idle receipts/min: Finland10, Germany11, HUB11, UK10, USA10;
Poland45 with ongoing counter changes. After 02:41:37 UTC HUB activation, the
observed log contained one 4.462s checkpoint and no lease-expiry/drain event by
final acceptance. This is a bounded observation, not indefinite stability proof.
Germany's pinned-HUB clock difference was bounded to 0.0824..0.1783s (read only).
Temporary artifact HTTP servers stopped; diagnostic binary was not installed.
