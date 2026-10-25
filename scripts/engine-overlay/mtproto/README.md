# Managed MTProto worker

This is a separate executable over MIT-licensed
[`MHSanaei/mtg-multi`](https://github.com/MHSanaei/mtg-multi), pinned to
`a45ebefed80e16949426381ef53fb5f1c1e4958d`. The preparation script requires an
exact clean checkout and records every original source file and overlay hash.
The release uses Go 1.27.3 and includes the complete generated source,
vendored dependencies, upstream license, preparation scripts and provenance.

## Authentication and accounting

Only named `ee` FakeTLS secrets are accepted. Each principal has exactly one
counter name and one unique 16-byte secret key. Keys cannot be reused under
different hosts or user names. The exact serialized configured secret must
match the signed `credential_sha`. The worker does not enable upstream's
mutable public stats/secret API, configuration reloader, usage-state reset,
or automatic Telegram DC updater.

Owner aggregate mode instead accepts one non-billable `owner_aggregate`
principal bound to the component's inbound tag. It still authenticates every
named secret and requires unique keys, then sums all configured users into
that one owner counter. It never attributes these bytes to a customer account.

The counters measure application payload consumed from or accepted for an
authenticated client's transport, after FakeTLS and obfuscation handshakes.
They exclude TLS/obfuscation headers, scout traffic and unauthenticated cover
traffic. Downlink accepted into the upstream record scheduler can be counted
before the remote client acknowledges delivery. This is a transport payload
counter, not an acknowledgment or Telegram RPC-delivery receipt.

The wrapper registers every root before launching its goroutine under the
admission mutex. Finalization closes admission permanently, closes the public
listener, joins its accept loop, cancels the engine, closes all accepted
connections, joins all wrapper roots and the engine shutdown, and only then
copies cumulative counters. Upstream `ServeConn` joins both relay directions
and its doppelganger record producer before returning. The cached final is
immutable and bound to one 64-character lowercase hexadecimal nonce.

## Private control and readiness

The supervisor supplies a Unix socket path inside its private `0700` birth
directory; the executable sets the socket to `0600`. JSON POST operations
are `status`, `snapshot`, `activate` and `final`. Activation and finalization
require a 64-character nonce. Repeating the same operation/nonce is idempotent;
conflicting nonces and activation after final are rejected. A cancelled final
request does not cancel the permanent fence or background join.

The public TCP listener is bound at start but rejects every connection until
activation. Readiness confirms local listener/engine initialization, not
public Telegram reachability. `--check` validates configuration and identity
bindings without constructing an engine or binding any socket.

When `requires_socks_bridge` is set, only an authenticated `socks5` URL on
`127.0.0.1` with a valid nonzero port is accepted. Production uses upstream's
proxy network to traverse the supervisor's managed bridge. The worker itself
does not implement outbound route selection; the owning Xray bridge does.

## Positive acceptance

The generated `cmd/dark-pro-mtg/acceptance_test.go` establishes real HMAC
FakeTLS handshakes, verifies server authentication, and transfers AES-CTR
obfuscated payload through the actual engine to an owned local mock DC.
Two users have exact independent counters. The tests cover admission closed
before activation, rejected credentials, traffic in flight at final, joined
immutable counters, revoked credentials absent from a successor birth,
authenticated SOCKS routing, strict requests/nonces, private Unix control,
side-effect-free `--check`, and a separate process's SIGTERM lifecycle.
An additional two-user encrypted payload test proves exact owner aggregation
and its immutable final receipt.

The test network denies all unrelated HTTP/native/cover dials. The SOCKS
fixture recognizes only pinned DC2 addresses and redirects them locally.
No acceptance test contacts public Telegram, a cover site, or a customer
endpoint. CI requires explicit passing events for each positive test and
rejects skips, failures and race-detector warnings.

```sh
python3 scripts/prepare-mtproto-engine.py /path/to/clean/pinned/mtg-multi
cd /path/to/clean/pinned/mtg-multi
go test -race -buildvcs=false ./cmd/dark-pro-mtg ./mtglib -count=1 -timeout=180s
go mod vendor
CGO_ENABLED=0 go build -mod=vendor -buildvcs=false -trimpath -o dark-pro-mtg ./cmd/dark-pro-mtg
```
