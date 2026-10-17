# DARK XRAY PRO — Distributed Native metered admission V1

## Goal

Harden and integrate the missing Agent-to-HUB trust/receipt boundary. A
working NEW PRO Node cannot accept client traffic without a durable verified
accounting epoch. The original DARK XRAY production HUB, clients, Xray and
financial ledger are not part of this feature.

## Strict two-sided opt-in

This protocol is disabled by default, including on an upgraded binary.

HUB opt-in: `DARK_PRO_LAB_MODE=1`,
`DARK_PRO_ENABLE_BUSINESS_CORE=1`, `DARK_PRO_ENABLE_USAGE_LEDGER=1`,
`DARK_PRO_ENABLE_NATIVE_INBOUNDS=1` and
`DARK_PRO_ENABLE_REMOTE_METERED_ADMISSION=1` with exactly one
`DARK_PRO_NATIVE_METERED_NODE_ID` already in the NEW PRO Registry.

Node opt-in: `DARK_PRO_LAB_MODE=1`, remote verified HTTPS 1.3, existing
paired installation, `DARK_PRO_ENABLE_NATIVE_RUNTIME=1`,
`DARK_PRO_ENABLE_NATIVE_METERED_ADMISSION=1`, a fresh *dedicated* private
runtime directory, separately secret independent 32-byte state key, an
explicit currently assigned local interface IP, an independently pinned
stock Xray binary and SHA256, and existing CA-verified/leaf-pinned TLS Agent
URL. The Node must not have legacy Xray services or customer listeners.

No browser request can toggle either opt-in. Existing ordinary
`GET /api/v1/agent/native-runtime` remains 503-gated under the ledger,
and an old Agent is never silently switched to metered mode.

## Authenticated remote state machine

1. The dedicated Agent HTTPS listener (not the owner browser) serves GET
   `/api/v1/agent/native-metered-runtime` only to the explicitly allowlisted
   paired Node. HUB builds a REALITY client configuration from currently
   eligible NEW PRO client UUIDs, inserts a **private 127.0.0.1:29761**
   Xray StatsService/HandlerService using `WithUsageAPI` before signing,
   records the exact sequence/manifest/expiry in the private business DB,
   and signs a maximum 30-second installation-scoped lease.
2. The Agent revalidates HTTPS trust, TLS 1.3 and the independent leaf
   fingerprint. It verifies the lease HMAC, config/manifest, physical listen
   interface and Xray syntax. `PrepareUsageAdmission` persists an encrypted
   intent and starts *API-only* Xray **without any customer listener**.
   The Agent's own kernel observation yields boot ID, PID and start ticks.
3. The Agent signs this birth with a domain-separated HMAC using its private
   installation token and the exact HUB lease and posts it through a separate
   authenticated TLS POST `/api/v1/agent/native-metered-register`.
   The HUB verifies paired installation, sender HMAC, signed lease time,
   current config generation and precisely recorded delivery. It generates
   a deterministic per-process epoch ID from the private HUB key, then
   derives all bindings from private eligible database UUID mappings. It
   refuses a *second unsealed epoch* on the same Node installation, even
   with another kernel process identity.
4. The Agent checks the returned epoch against its actual owned /proc
   process and frozen config, then writes a complete initial zero report
   to its fsynced encrypted local outbox. The existing pinned TLS sender
   submits it to the HUB's actual usage ledger and verifies the exact
   committed receipt. This is NOT a lifetime usage reset.
5. `ActivateUsageAdmission` starts real collection from the same captured
   stock Xray process and proves a valid initial sample. Only then does
   the private Xray API add the signed customer listener(s); actual bound
   ports must be owned by that same child. A failed, delayed or lost receipt
   never opens a customer port. Observer/replay and quota ledger continue.
6. Renewals use new short signed leases with the same config and process.
   A changed profile, unknown previous epoch, restart or ambiguous accounting
   cannot invoke ordinary `Apply` / `Recover` to bypass settlement.
   Short lease expiry and parent-death fencing still stop owned Xray.

The HUB cannot independently read /proc of a different physical Node;
this protocol trusts the **explicitly paired native Agent** to measure the
process birth on its own host and authenticate it over pinned TLS. A malicious
administrator of the Node can compromise the Agent/token; this is not a
hardware attestation system or cryptographic proof against a compromised
Node OS. This trust assumption is made explicit instead of claiming
remote kernel provenance without evidence.

## Verification and unresolved production gates

The existing native metering and real pinned Xray tests already prove
first-byte receipt hold, wrong-pin failure, lost receipt, cancellation,
lease expiry, restart settlement and true VLESS REALITY accounting on
isolated temporary Linux processes.

This increment adds:
- HTTPS-only GET and POST scope; browser/representative/owner token cannot
  read or mint private metered Agent delivery
- Version-2 signed meter candidate; original unmetered delivery remains
  intentionally separate and barred while ledger is enabled
- Signed Agent birth; database refuses changing the birth for a second
  still-unsealed epoch even if a new delivery/short lease is requested
- Real unprivileged stock Xray e2e in CI (API-only bootstrap -> authenticated
  remote registration -> **committed zero receipt** -> owned bound customer
  listener -> same-process renewal -> clean stop)
- No new client subscription publishing, reseller APIs, Telegram functions,
  public control routes, or migration of historical usage

These tests do not alone prove public US Internet routes, realistic
high-volume pressure or customer-specific hard limits under failures.
The canary MUST remain a disposable NEW PRO client on a loopback-only
assigned listener until actual REALITY client handshake, measured payload,
quota/revoke and explicit final settlement have been independently verified.
Do not remove the metering flag as a workaround.

## Deployment discipline

PR must be green across real pinned Xray, full Go -race/vet, Chromium and
native usage receipt acceptance. Stage and verify exact release SHA,
back up ONLY NEW PRO data/config (never old HUB), preserve one rollback
release, do not enable the above opt-ins on live Nodes without an isolated
canary and dedicated private Xray binary. Keep the existing US PRO telemetry
Agent, its certificate-pinned SSH bridge, and all old DARK XRAY customer
services unchanged until each live admission acceptance passes.
