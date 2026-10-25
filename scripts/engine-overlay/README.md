# Owned external inbound workers

The TUIC and AmneziaWG overlays build separate executables from Sanaei commit
`608ef4e4010bf83e2e78328e08f77efc051ad4d7`. The hub and node agent communicate
with them through a private Unix HTTP socket. The control-plane Go module does
not import these GPL packages.

The MTProto worker has its own MIT upstream and preparation script under
`mtproto/`; its source and licensing are independent of this build.

## Reproducible source preparation

Use Go 1.27.3 and a clean checkout at the exact Sanaei revision above. The
preparation script copies the committed tree without modifying that checkout.
Its output directory must not exist.

```sh
python3 scripts/prepare-sanaei-engines.py /path/to/pinned-sanaei /path/to/worker-source
cd /path/to/worker-source
go test -race -buildvcs=false ./internal/darkproadapter ./cmd/darkpro-tuic ./cmd/darkpro-amneziawg -count=1 -timeout=240s
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o ./bin/darkpro-tuic ./cmd/darkpro-tuic
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o ./bin/darkpro-amneziawg ./cmd/darkpro-amneziawg
```

`GO` may name an absolute Go executable. Standard Go cache variables work as
usual. The generated `darkpro-sanaei-provenance.json` records the upstream
revision, toolchain, overlay SHA-256 values, and the exact QUIC module checksum.

The modified QUIC module is included at `_darkpro/quic-go`, with a relative
`replace` in the generated module. Its source version is
`github.com/apernet/quic-go@v0.61.1-0.20260806010916-184d081eef3e`.

The additional congestion-control acceptance test is:

```sh
cd /path/to/worker-source/_darkpro/quic-go
go test -buildvcs=false ./internal/ackhandler -run TestDarkPro -count=1
```

Release packaging must include the full generated source tree, its upstream
LICENSE, the modified QUIC source and MIT LICENSE, provenance, dependency
sources/licenses, and binary checksums. The execution CI vendors dependencies
and builds release binaries with `-mod=vendor` before archiving that source tree.
Copying only the executables is not this packaging workflow.

## Control and admission lifecycle

The executable supports these entry points:

```sh
darkpro-tuic --check --config /private/candidate.json
darkpro-tuic --config /private/adapter.json --control /private/control.sock
```

The AmneziaWG executable has the same arguments. Input is the v1 adapter
envelope from `internal/inboundruntime/group_linux.go`: engine, rendered config,
manifest and the complete principal bindings. JSON input and commands reject
unknown fields. The input and control socket are private to the supervisor.

| Request | Observed result |
| --- | --- |
| `POST /status {}` | Validated process readiness, admission state and cumulative counters. |
| `POST /snapshot {}` | Cumulative counters for every assigned principal, including zero-use and health principals. |
| `POST /activate {"nonce":"…"}` | Opens the listener only after the supervisor has accepted the zero-admission receipt. The same nonce is idempotent. |
| `POST /final {"nonce":"…"}` | Fences public ingress, closes owned relays, joins handlers, then seals the immutable final counters. |

A nonce is 64 lowercase hexadecimal characters. A conflicting nonce is rejected.
A sealed generation cannot be reactivated. Final snapshots remain readable;
they are never cleared or replaced with a new user's counters.

There is no public TUIC or AWG socket before activation. A prepared process can
therefore be ready while admission is closed. Readiness means the owned engine
is running; a connection-test result is a separate control-plane observation.

The supervisor owns binary/process identity, leases, generation fencing,
accounting acknowledgement and kill escalation. These workers do not renew
their own leases or publish subscriptions.

## Payload accounting and routing

Each worker creates an ephemeral, authenticated SOCKS relay bound to
`127.0.0.1`. Its password is randomly generated for the process. The public
protocol authenticates the client first; only then does the worker connect to
the private relay using that client's assigned identity.

Counters cover plaintext payload successfully written through owned TCP and
UDP relay sockets. SOCKS headers, QUIC framing, AWG transport encryption,
handshakes and keepalives are excluded. AWG device UAPI wire-byte counters are
not substituted for this payload basis. There is no second Xray user counter
for the same transfer.

An explicit owner-aggregate principal can receive the sum of all configured
users. Otherwise every configured user must have exactly one assigned counter.
Health principals remain distinct in the snapshot so the ledger can exclude
their traffic from billing.

The current private relay dials direct egress. An outbound or tunnel route that
requires a different relay must be supplied by the compiler/supervisor contract
before it is admitted. The worker does not infer an outbound from a missing
setting. AWG public per-peer IPv6 source aliases require host address
provisioning; requesting an alias interface is rejected by this userspace
worker. IPv6 tunnel addresses themselves are supported.

## TUIC behavior

The worker uses the pinned native TUIC v5 server for authentication, TCP,
datagram UDP, stream UDP, fragmentation and client lifecycle. BBR and New Reno
use the pinned engine's controllers. The CUBIC overlay selects QUIC's actual
native CUBIC implementation, with Reno disabled, and preserves that choice
after path migration. It does not relabel New Reno as CUBIC.

TLS is QUIC TLS 1.3. A maximum TLS version below 1.3 and a TLS 1.2 cipher-suite
override are incompatible and are rejected. Supported server options include:

- Inline or file certificates and keys, multiple server certificates, CA
  verification pools and dynamic certificate issuance with an optional chain.
- ALPN, unknown-SNI rejection, curve preferences and session resumption.
- ECH server keys, validated against their config/public key before admission.
- OCSP stapling, with signature/status/time validation and bounded refresh on
  fresh handshakes. An invalid requested staple fails the handshake.
- Explicit TLS key logging into a private regular file, opened without
  following a final-path symlink and closed with the generation.

Certificate/key file contents are frozen when the generation is prepared.
Only regular files up to 1 MiB each are accepted; group/world-writable sources
and devices/FIFOs are rejected without a blocking open. ACME symlinks may point
to an otherwise valid regular file.
Changing files cannot mutate the active generation; applying a new generation
loads the new contents. OCSP status is refreshed without changing key material.
The generic client trust options used for exported client configurations remain
the exporter/control plane's responsibility.

## AmneziaWG behavior

The worker uses the pinned AmneziaWG 3.1 userspace device and gVisor stack. It
attaches all TCP/UDP handlers before applying any peer configuration, then
activates the public UDP socket. Peer source addresses resolve to the assigned
principal; duplicate public keys and overlapping host-address ownership are
rejected.

The UAPI configuration includes junk packets, S1–S4, H1–H4, I1–I5, header
protection, content padding, rekey/handshake timings, random trailers, cookie
control, PSKs and per-peer persistent keepalive. IPv4 and configured IPv6
tunnel addresses are assigned inside the userspace stack. No host TUN or
unauthenticated public SOCKS listener is created by this adapter.

Shutdown closes the device's ingress, interrupts all owned relay connections,
waits for TCP/UDP callbacks and reply pumps, and only then allows a final
accounting receipt.

## Acceptance evidence

The standalone tests require real local network sockets. AWG tests also require
one local non-loopback IPv4 address for the relay destination. These are local
echo targets, with no external internet traffic or production credentials.

The acceptance suite exercises actual QUIC and AWG encrypted TCP/UDP traffic,
distinct client/health counters, wrong credentials, credential revocation in a
replacement generation, final accounting, ECH handshakes, SNI rejection, OCSP
success/failure, issued certificate chains, file snapshots and key-log
permissions. The QUIC submodule test separately proves native CUBIC selection
and persistence after path migration. The shared bridge is also tested for
closed admission and mandatory authentication.
