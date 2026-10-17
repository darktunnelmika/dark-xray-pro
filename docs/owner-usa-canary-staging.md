# DARK XRAY PRO — owner-authorized USA private canary staging

## Reason for this milestone

The first remote Native metering protocol is merged and installed on NEW PRO,
with both runtime opt-ins still OFF. Its pinned 26.9.9 real-Xray and durable
zero-receipt CI prove the protocol in an isolated environment, not yet over
the real USA VPS to the live NEW PRO HUB.

The actual NEW PRO business DB has **zero** clients and Inbounds. The
deployment tool must not extract owner passwords or bearer token secrets
from a service, nor change the live private SQLite directly to create a
canary. Instead, the **authenticated owner/browser control plane** stages
one safe test identity via existing audited and idempotent business APIs.

## Owner action

Page: /inbounds, owner login, section **USA loopback canary**.

- The owner must check a visible explicit consent sentence. The UI then
  sends a POST with JSON `{"confirmation":"USA_PRO_LOOPBACK_QA_ONLY"}`,
  same-origin and the existing session CSRF token.
- Endpoint: `POST /api/v1/workspace/native-canary/stage`, with exact
  **POST-only** Nginx allowlist, and owner-role checks in Go.
- Only Node ID `usa-pro-01` with current registered installation and
  real fresh heartbeat/metrics is permitted. No old Node or external IP.
- Staging is refused if the independent private PRO ledger is not active,
  if live remote metered admission is enabled, or if any different existing
  clients, Inbounds or Node targets are present.
- Idempotent business calls create **one** owner-billed dummy test client
  `pro-usa-qa01` with one-day expiry and a **1 MiB** volume limit, and
  one independent REALITY TCP Inbound `901617` with the direct target
  `127.0.0.1:19666`. Swap stays DISABLED (port=0).
- The stage routine verifies the private account baseline is known-zero,
  the correct one user is present, and an independently usable metered
  candidate can be derived. The API response contains NO REALITY private
  key, client UUID, Agent token, Session cookie, pairing code or volume
  history.
- Source and installation IDs must come exclusively from NEW PRO Registry.

**No Xray service is started, customer port is opened, or subscription is
published by this owner action.** Partial, interrupted stages remain on
a disabled data-plane; retry with the same consent is idempotent and cannot
create an extra identity. Billing events remain append-only.

## Remaining real canary acceptance

After the owner activates this audited staging action from a real browser
session, validate DB counts, Node installation and zero account baseline,
then under a separate explicitly authorized and rollback-ready deployment:
1. Activate the short metered HUB lease for the allowlisted USA Node.
2. Configure the Agent only with its own dedicated private runtime
   directory, secure state key and hash-pinned stock Xray 26.9.9 binary.
3. Confirm API-only boot and no listener before the first durable zero
   receipt; only then permit a loopback customer-facing port.
4. Exercise an owned local REALITY handshake on USA with disposable
   credentials, record verifiable counters and quota, revoke and close
   the runtime, and verify no customer-visible public port remains.
5. Fail closed and retain immutable receipts under every outage/rollback.
6. Do not import any old production customer, reset old traffic, or
   publish a customer subscription during QA.

## Security/CI

Full Foundation CI, Go -race/vet, Chromium 320px FA owner consent, and real
pinned Xray/native accounting acceptance must be green before merge.
Existing production Agent flags remain OFF throughout this PR.

Nginx review snippet must add **one exact POST-only location**, preserve
Nginx `-t` and proxy deny default:

```nginx
location = /api/v1/workspace/native-canary/stage {
    limit_except POST { deny all; }
    proxy_pass http://127.0.0.1:2087;
}
```
