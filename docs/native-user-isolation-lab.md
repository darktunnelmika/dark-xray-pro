# Native per-user isolation and live Vision counters — LAB-only core profile

This source-level experiment closes a specific blocker in release contract R03. It does **not** enable metered production admission, deploy a core, change an installed LAB service, or migrate old customers, representatives, Nodes, Telegram bots, domains or ports. The existing `503 metered_runtime_admission_pending` barrier remains unchanged. Successful conformance is not completion of all R03/R04 runtime and recovery conditions.

## Why an explicit core profile is needed

The exact official Xray baseline is `v26.9.9`, commit `52a412d9e2f5c2a5142b1b4e2ab3771dacb8b120`, with the same official archive SHA256 `1eb9175d0f0a8f8149c9230a7fc5ae66ce332ed20a53155ce61fe62e3f58b7df` already pinned by PRO. Its VLESS `RemoveUser` removes validator authority but does not close already authenticated forward streams. Source-IP blocking is not user isolation and is deliberately not used.

In addition, `proxy.CopyRawConnIfExist` updates user statistics only after an unbounded `net.TCPConn.ReadFrom` completes. A long-lived Vision/TLS stream can therefore relay substantial downlink without that portion appearing in current user counters. The stock negative control reproduces both behaviors; a passing stock-control test means the limitations were reproduced, NOT that the stock core has the new capability.

## Reviewable overlay, not a silent fork replacement

`tools/core-lab/apply_vless_sessions.py` accepts only an unprivileged, clean, exact-revision QA clone and requires `--lab-only`. It checks all source anchors before writing, refuses repeats/dirty inputs/symlinks/wrong revisions, and neither downloads nor builds nor installs a service. Six Xray files are affected: two existing Go files plus four new implementation/test files. The changes to Xray remain MPL-2.0; the upstream license and copyright notices remain with that source. PRO's module/dependency files are not replaced.

The VLESS Handler registers each logical stream after actual authentication. User mutation and registration share a lock. MemoryUser pointer identity is a generation token; even re-adding the same UUID/email through the same object creates a fresh generation. A stale pre-removal handshake cannot regain access after renewal. Removing a user cancels all of that user's registered streams outside the admission lock; a stuck Close does not block unrelated user admission. Handler shutdown cancels its sessions and refuses new admissions. No source IP is selected for closure.

For a metered raw-copy path, the overlay bounds each `ReadFrom` call with a 32 KiB `io.LimitedReader` and commits the number of bytes actually written after each completed batch. Partial progress accompanying an error is preserved; requested size or merely read bytes are not charged as written bytes. The pinned Go Linux implementation supports LimitedReader in the splice path, so this retains the existing TCP ReadFrom path rather than deliberately disabling Vision or introducing another outbound SOCKS hop. Throughput and CPU cost still require comparative benchmarking.

## Actual acceptance against the PRO Store and Registry

`TestRealCoreUserIsolationContract` starts real pinned core processes and the actual project Store/modernc driver, Registry enrollment, Agent TLS receiver and durable encrypted outbox. A has two existing connections; B has one. They use the same source IP and the same Xray inbound. All three connections relay application data before removal.

In patched mode, repeated real counter observations are persisted and sent to the actual receiver; the test controller waits for A's committed quota exhaustion before calling the core's HandlerService removal. Both existing A connections must close, a new A connection must not relay, and the **same existing B connection** must relay afterwards with the **same core PID/boot/start identity**. No replacement/restart is accepted as isolation. UUID records and representative sales credit are unchanged. No counter sample or commit receipt is fabricated.

The required transport matrix is VLESS/REALITY TCP, VLESS/REALITY gRPC, and TCP/REALITY Vision carrying an inner CA-verified TLS 1.3 application connection. The Vision case also requires at least one 32 KiB downlink counter advance while A's connection is still open, BEFORE removal. The stock Vision control separately demonstrates deferred downlink reporting and manual removal's old-stream gap; it does not falsely claim a quota-triggered decision.

Eight core lifecycle/raw-copy unit tests cover same-user fanout, unrelated users/inbounds, re-add/stale handshakes, blocked Close, concurrent admission/removal, shutdown, exact batches/tails, partial write failure and cancellation. CI requires all eight three times with Race. Five Python input-guard tests use synthetic Git fixtures and are explicitly not real-core acceptance. The real matrix rejects skipped/missing subtests. Both stock and patched jobs are mandatory additions to the prior Foundation and native-browser suites.

## What remains unproven / deliberately disabled

This is a test-supervised native-core integration, not the continuously running `cmd/node-agent` metering loop. Production still needs pre-listener meter admission, trustworthy initial counters for never-connected users, final draining before replacement/shutdown, recovery of pending epochs, unobserved-tail holds, signed per-node reservations, and a capability attestation for the exact admitted binary. A caller-supplied hash or successful CLI exit alone cannot claim user-isolation capability.

The test sends more data than A's small quota before sampling and records the full observed usage. Batching and periodic collection do **not** provide a zero-overshoot hard limit. An incomplete raw batch can remain unreported until it ends; protocol buffers and sampling delay also matter. Crash durability is not guaranteed for bytes never observed. Final settlement must not release an unresolved reservation just because an API call returned or an old lease expired.

Passing gRPC here does not prove several different user identities sharing the same underlying HTTP/2 carrier. UDP, MUX/XUDP/reverse, other legacy protocols and hostile resource-pressure/reboot conditions need separate acceptance; none are silently removed from the product requirements. Performance, full fleet deployment, Subscription, commerce/bots and legacy migration remain release blockers in #22.

## Source references

- Xray pinned `proxy/vless/inbound/inbound.go` and `proxy/vless/validator.go` define authentication and current user deletion.
- Xray pinned `proxy/proxy.go`, `CopyRawConnIfExist`, defines the original splice counter timing.
- Go `go1.27.2/src/net/splice_linux.go` defines bounded LimitedReader handling.
- PRO release contracts in `docs/release/` and the PR #31 Store/Registry/TLS integration remain authoritative; no installed flag is changed by this LAB profile.
