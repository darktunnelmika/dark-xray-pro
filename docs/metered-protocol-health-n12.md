# N12: bounded destination protocol-health primitive

`TransactionRuntime.ProbeMeteredProtocol` checks a live metered Direct TCP
Reality/Vision listener using a separate, short-lived client of the exact pinned
core. It derives client settings from the owned active candidate; the caller
selects a credential by SHA256, never supplies arbitrary Xray configuration.
The selected credential must exist in the exact reviewed inbound and port.

A signed request binds the handover, destination installation, runtime instance,
Apply command, stage, runtime condition, usage epoch/admission, credential hash,
verifier configuration and unique challenge. Its lifetime is at most eight
seconds and cannot extend beyond the server lease. The runtime checks the owned
child, healthy observer and exact conditions before and after network I/O.
One probe per runtime is allowed. Network I/O does not hold the controller
mutex: an emergency drain remains possible while a verifier response is held.

The verifier is operator-approved configuration, not a browser-supplied URL.
It must use a literal public IPv4 address, TLS 1.3, certificate hostname and chain
verification, plus an exact leaf-certificate SHA256 pin. Custom CA bundles are
bounded and invalid bundles fail closed. There is no proxy environment, DNS
lookup, redirect, compression or direct outbound fallback.
The temporary SOCKS listener has a default blackhole route and permits only the
approved verifier IPv4 address and TCP port through its VLESS outbound. The fixed
request is
`GET /dark-pro/protocol-health/v1/{challenge-id}`. The exact bounded response is
`DARK-PRO-PROTOCOL-HEALTH-v1\n{challenge-id}\n`. An open TCP socket or execution
ACK cannot produce a successful proof. The private probe client is stopped and
its private configuration removed before the method returns.

The receipt binds the complete signed request hash, response hash and completion
time under a separate MAC domain. It contains no UUID, Reality private key or
Xray candidate. `VerifyProtocolHealthReceipt` verifies cryptographic evidence
within the challenge lifetime; it is **not** current readiness or publication
authority. Existing `DataPlaneVerified`, `SubscriptionPublished` and
`can_publish=false` behavior is unchanged.

## CI acceptance

`scripts/test-hub-active-traffic.sh --handover-health` uses the same nonroot,
loopback-only disposable network namespace and immutable core pin as N11.
Synthetic customers send actual traffic through both source and destination;
the source must already be drained and sealed before destination admission.
Three cases run three times each with the race detector:

- Real protocol success; receipt verification; rejection of changed operation,
  instance, epoch, Apply command, inbound and credential; no implicit publication.
- Rejection of a wrong challenge response, redirects, wrong certificate pin and
  wrong TLS hostname. Untrusted TLS must not reach the HTTP verifier handler.
- Hold the verifier response, complete an emergency destination drain, then
  release the response. No success receipt may escape the stopped epoch.

Both successful traffic epochs are explicitly drained and their final HUB
accounting must still equal the sum of exact final reports, including probe
traffic. The bound idle client remains at zero.

## Remaining integration

This internal primitive has no HTTP/Agent endpoint, worker or UI trigger yet.
There is no database/schema migration or deployment in this step. CI uses only
synthetic credentials; probe bytes currently belong to the selected credential's
normal usage and must not be silently assigned to a real customer's account.

Before rollout, implement a persisted HUB-issued single-use challenge, exact
handover/Apply binding and atomic receipt consumption; recheck source settlement,
destination observation, current customer eligibility and unexpired authority
at consumption. Define dedicated probe identity/accounting and an approved TLS
verifier. Then add explicit owner controls, unknown-response recovery and
expiration. Subscription publication, renewal, reservation completion and
cross-Node recovery remain separate gates. No production or USA Xray activation
is authorized by this primitive or by its tests.
