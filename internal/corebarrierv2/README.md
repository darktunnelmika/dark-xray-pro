# Managed native inbound runtime v2

This package is copied into a separate Xray build pinned to
`701af60772cda123492f86e383e1fdba066614a9`, with Go **1.27.3**. It is independent
of the existing managed-Xray v1 overlay and its final-receipt contract.
The source of the patched upstream files remains MPL-2.0; distribute Xray's
license and the corresponding modified source with the binary.

## Admission and final accounting

Set `DARK_PRO_INBOUND_V2=1` only for a supervisor-owned process. Customer
listeners and resources may be created during preparation, but admission
starts closed. The supervisor must durably acknowledge an initial zero
counter receipt before it sends activation. The exact inbound tag
`dark-pro-usage-api` is reserved for the supervisor's private API. The managed
supervisor uses a mode-0600 Unix socket inside the mode-0700 process birth
directory and checks that the child owns it. Candidate validation rejects
customer use of this tag. Loopback API fixtures are only used by core tests.

All wire requests use `StatsService.QueryStats`, with the following `pattern`
and `reset=false`. A nonce is exactly 64 lowercase hexadecimal characters.

| Pattern after `darkpro-inbound-v2/` | Response |
| --- | --- |
| `capabilities` | One counter `darkpro-inbound-v2/scope/native-inbounds-v2=1`. |
| `health` | `api-ready`, `engine-ready`, `admission-open`, and `final-sealed`, under the protocol prefix, each 0 or 1. |
| `activate/<nonce>` | One `darkpro-inbound-v2/activated/<nonce>=1`. Requires engine readiness. The same nonce replays; conflicting activation and activation after finalization fail. |
| `final/<nonce>` | One `darkpro-inbound-v2/final/<nonce>=1`, then sorted cumulative user and inbound traffic counters. The same nonce replays the immutable snapshot; another nonce fails. |

The final snapshot includes `user>>>...>>>traffic>>>uplink|downlink` and
`inbound>>>...>>>traffic>>>uplink|downlink`. The private control inbound is
excluded and receives no aggregate counters. Counter resets through
`GetStats`, `GetUsersStats`, and ordinary `QueryStats` are rejected while v2
is enabled. A stock binary or a process without v2 enabled cannot provide
the capability marker. A successful ordinary stats request is not evidence
of v2 support.

Finalization permanently fences admission, stops customer handlers, cancels
and closes admitted roots, joins their registered asynchronous descendants,
and waits for every admitted counter-producing I/O operation. Only then does
it read counters once. An RPC timeout does not cancel draining, reopen the
gate, change the nonce, or authorize another snapshot. Failure to close an
owned resource prevents a successful final receipt. The API handler remains
available for receipt retries.

## Source coverage and byte meanings

The source overlay covers TCP/Unix workers, UDP flows, WireGuard packet I/O
and inner connections, MASQUE inner connections, and TUN inner TCP/UDP.
`task.Run`, asynchronous dispatch, inbound mux/XUDP, SOCKS UDP and SS2022
response descendants retain proof ownership. Timeout-only contexts in native
outbounds retain their parent ownership even when upstream intentionally
detaches cancellation. Counter-bearing wrappers also fence each I/O, so a
retained writer cannot produce a new update after the gate closes.

The counters retain Xray's meanings. Authenticated user counters measure the
bytes at the protocol dispatch boundary. Inbound aggregates measure bytes
at that inbound's counter boundary, which can include protocol/transport
overhead. WireGuard's aggregate is packet traffic; TUN and forwarding
inbounds are owner network roles, with aggregate accounting and no invented
per-user identity. The supervisor must select one reviewed accounting basis;
adding user and inbound counters would double-charge overlapping traffic.

Managed v2 disables Linux splice in the audited raw-copy helper and uses the
buffered path. Upstream splice credits a long-lived copy only when it ends;
the buffered path makes ongoing consumption visible for quota enforcement.
This is a throughput tradeoff that should be measured on the target LAB.

The upstream concurrency fixes include Vision retaining reflected TLS
pointers as `unsafe.Pointer` with `unsafe.Add`, atomic Hysteria UDP close
flags, and synchronization of Vision's shared TLS recognition state across
its reader and writer. The recognition lock is released before network I/O,
so duplex traffic and backpressure cannot deadlock. The actual instrumented
traffic tests exposed the first two issues; GitHub CI exposed the shared
Vision packet-filter state race. A focused concurrent reader/writer test also
fails on the unpatched source and checks that packet state is not lost.
Neither race detection nor checkptr instrumentation is disabled.

## Readiness

`api-ready` proves the private service is responding. `engine-ready` is set
only after all configured core features start successfully. For socket
inbounds, startup must successfully bind their sockets; for TUN it must create
and initialize the requested interface and configured routes. The supervisor
also verifies process/socket/resource ownership before reporting a deployment
ready.

XDrive has no listening socket. Its startup performs a bounded Put/Get/Delete
round trip in the configured storage using an unpredictable, owned
`darkpro-probe-<nonce>` object and exact byte comparison. The total deadline
is five seconds, with the final portion reserved for cleanup. Failed write,
read, comparison or cleanup prevents readiness. No existing storage object
is touched. Storage construction remains subject to the supervisor's process
startup deadline.

## TUN system DNS activation

The bounded Linux DNS path is gated by privileged acceptance. Authoring must
remain blocked until the dedicated namespace test below passes; ordinary
kernel TUN traffic alone is insufficient evidence.

For an explicitly selected `autoSystemDnsToGateway`, the compiler supplies an
ordered list of concrete upstream IP resolvers, a DNS feature tagged
`dark-pro-inbound-dns-upstream`, and an appended `dark-pro-inbound-dns`
outbound. It puts an exact inbound-tag/TUN-DNS-address/port-53 TCP-and-UDP rule
ahead of sibling routing. The DNS address is the address after the first
IPv4 gateway, or after the first IPv6 gateway when no IPv4 gateway exists.
The address must remain usable inside that gateway prefix. Captured upstream
resolver addresses, implicit system resolvers, hostname bootstrap and
conflicting sibling policies are rejected. A/AAAA use the explicit DNS
feature; other record types retain TCP/UDP forwarding to the first configured
resolver. Unknown record types are not replaced with empty successful answers.

The native core creates its own nonpersistent TUN with `IFF_TUN_EXCL` and
refuses inherited descriptors or an already existing device. During initial
preparation, the interface can exist but host DNS remains untouched. A
nonce-bound activation hook applies the verified per-link resolver settings
after durable zero acknowledgement and before admission opens. Apply has a
two-second total deadline; each `resolvectl` subprocess has a one-second
deadline. Any failed or ambiguously acknowledged command triggers rollback.
Final cancels and joins an in-flight activation, joins DNS request/response
descendants, and waits for resolver, route, address and device cleanup. A
failed cleanup cannot produce a successful final receipt.

Internal DNS lookups have gated, tracked roots rather than a dispatcher
exemption. Their intentionally detached contexts retain proof ownership.
They introduce no new financial identity: actual DNS payload crossing a TUN
is included in that TUN's existing owner aggregate.

Before using this configuration, the supervisor must read the exact
`darkpro-inbound-v2/tun-dns-activation-v1` marker from the hash-pinned
binary's side-effect-free `version` command. The general v2 API marker does
not distinguish older overlays that applied DNS during startup; checking
that API only after launching customer configuration would be too late.

The selected NodeAgent needs access to `/dev/net/tun`, permission to manage
its owned network interface/routes, a running and authorized systemd-resolved
system bus, and `resolvectl`. These privileges belong to the selected agent;
this feature does not require or justify granting them to the HUB. Normal
finalization explicitly reverts DNS before closing the device. On SIGKILL,
the exclusive nonpersistent TUN disappears with its file descriptor and
systemd-resolved removes its per-link state. The namespace test must prove
that crash path too; a crash still supplies no valid accounting final.
See the upstream [resolvectl revert contract](https://www.freedesktop.org/software/systemd/man/latest/resolvectl.html)
and [per-link DNS routing](https://www.freedesktop.org/software/systemd/man/latest/systemd-resolved.service.html).

## Reproducible build

Use a completely clean checkout at the exact pin:

```sh
DXP_BUILD_GO=/path/to/go1.27.3/bin/go \
  scripts/ci/build-inbound-core.sh /path/to/clean-xray /path/to/output
```

The helper verifies upstream commit, source hashes, exact patch anchors and
module checksums; runs barrier/stats/XDrive/TUN-DNS/Vision tests under the race detector;
and builds a separate binary. It emits the binary SHA256, source/overlay
provenance and upstream license. It does not install or start a service.
`prepare-inbound-core.py` rejects a dirty checkout and validates every source
anchor before changing any source file.

## Actual traffic acceptance

```sh
DXP_INBOUND_CORE_V2_BINARY=/path/to/xray-inbound-v2 \
DXP_INBOUND_CORE_V2_SHA256=<verified-binary-sha256> \
  go test -race ./internal/corebarrierv2 \
  -run '^TestPinnedNativeInboundTrafficAndFinalReceipt$' -count=1 -v
```

The matrix covers native protocols other than privileged TUN; all seven
generic transports; TLS on TCP, WebSocket, gRPC, HTTPUpgrade and XHTTP;
REALITY on TCP/Vision, gRPC and XHTTP; and mux/XUDP. A separate inner TLS 1.3
case sends application records through Vision's raw-copy transition. It creates real server
and client processes with local TCP/UDP echo targets. Each authenticated
protocol is exercised by two distinct concurrent users. It checks zero
pre-admission consumption, activation, current counters, user isolation,
final shutdown with active traffic, immutable replay, unchanged ordinary
counters after finalization, and continued API availability. All credentials
and certificates in these tests are public synthetic fixtures.

The privileged TUN test is separate:

```sh
scripts/ci/test-inbound-tun.sh /path/to/xray-inbound-v2 <sha256>
```

It requires Linux namespace administration and a kernel with TUN support.
The harness compiles its test first, then uses fresh network, mount and PID
namespaces with a private `/dev/net/tun`. Only loopback exists initially.
The test creates its own TUN and one route for `198.18.0.2/32`; both TCP and
UDP go through that actual kernel interface into local echo fixtures. It
checks owner aggregate counters, the admission/final barriers and removal
of the interface. No host route, DNS, firewall or service is changed.
Without this privileged environment, a skipped TUN test is not an acceptance
result. The dedicated CI step must pass the positive test.

The additional DNS lifecycle test uses real D-Bus and systemd-resolved:

```sh
scripts/ci/test-inbound-tun-dns.sh /path/to/xray-inbound-v2 <sha256>
```

It uses private `/etc`, `/run`, `/dev`, network, mount and PID namespaces,
its own D-Bus daemon and resolver, and an owned upstream DNS fixture. If the
runner lacks the tools, it downloads/extracts packages without installing or
starting any host service. Acceptance requires real A/AAAA/TXT TCP/UDP
payload, a resolved D-Bus lookup and stub query through the kernel TUN,
unchanged DNS before activation, correct owner counters, resolver/interface
removal after both finalization and SIGKILL, and rejection of an existing
TUN device. Its positive marker is mandatory in CI; a skip cannot pass it.
