# P0-H7 — traffic-bearing owner HUB command acceptance

This acceptance extends P0-H6 without enabling a production route or changing
the renderer, interpreter, authority, core overlay or accounting rules.

## Real path

Authenticated owner API → encrypted HUB queue → pinned TLS Agent poll → real
Check/Register authority → registered process birth → initial durable zero
receipt → actual VLESS/REALITY/Vision TLS stream → final counters → HUB seal →
replacement/rollback ACK. Two disposable clients are bound: active and idle.

The candidate is rendered from actual Store profiles and drafts. No config
bytes, signatures, billing map, egress exception or authority implementation
is substituted by the fixture. Stage and Rollback pass through owner requests.

## Isolated transport

The CI script creates one temporary network namespace containing only `lo`.
No veth, default route, DNS dependency, host-interface changes or Internet
access is added. The synthetic address `11.0.0.1/32` exists only inside that
namespace for the owned REALITY target and streaming TLS origin; it never
contacts the real-world owner of that address. A public-shaped fixture address
avoids weakening the upstream core's private-address policy. The test binary
is compiled beforehand, then all tests and Xray children run as the ordinary
unprivileged runner. Namespace creation/removal alone uses runner sudo.

## Mandatory three-pass Race gate

- Replacement and Rollback while a verified TLS stream is actively growing.
- ACK loss before and after HUB commit: same epoch, one birth, live stream kept.
- Final receipt response loss and final seal response loss: stop, durable replay
  after reopening the Agent, unchanged historical failure, no implicit Apply.
- Revocation after final settlement: current authority blocks the next birth.
- Concurrent unknown-tail hold after successful Check: final counters cannot
  forgive uncertainty or make the client eligible again.
- Agent interruption with active traffic and a lost interruption notification:
  preserve prior known consumption, retain the unknown-tail hold, never resume.

Every admitted birth is checked for exactly one complete initial zero report
before listeners open. Later births require all prior processes stopped and
their exact final reports sealed by the real HUB ledger. Uplink and downlink
are compared independently to the sum of terminal epoch counters; an idle
client remains exactly zero. Owner ACKs carry the exact prior final seal.

The existing H4/H5/H6 suites remain mandatory and unchanged in scope. Skips or
missing subtests fail CI. This is not a throughput benchmark, multi-node test,
UDP/Tunnel/Swap implementation, subscription publication, production security
sign-off or migration approval. No LAB installation is needed for test-only
changes; new runtime gates remain off. Old HUB, live nodes, real accounts,
representatives, credits and bots remain untouched; USA activation is excluded.
