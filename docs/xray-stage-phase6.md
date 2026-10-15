# DARK XRAY PRO — Phase 6: encrypted candidate staging + offline Agent validation

**LAB ONLY / NO LIVE XRAY ACTIVATION / NO CUSTOMER SUBSCRIPTION CHANGES**

Phase 6 follows Phase 4's durable per-Inbound TCP preflight and Phase 5's
explicit VLESS+REALITY config renderer. It adds a controlled, **privately
encrypted config delivery** pipeline to an independently paired NEW Node.

## What now works

1. The owner uses ONLY the SSH-forwarded **127.0.0.1:2087** API to attach
   explicit Reality profile(s) and a separately trusted Xray executable
   SHA-256 to a replacement operation whose TCP probes already passed.
2. The HUB checks the replacement journal's pinned installation ID and
   canonical route-plan SHA, generates a private VLESS/REALITY candidate,
   and seals it with **AES-256-GCM** at rest. The application key is supplied
   separately and must remain the same across HUB restarts. The private
   config **never** appears in owner list/detail/Create responses.
3. The authenticated new Agent retrieves its own pending encrypted-stage
   payload only from the dedicated **TLS 1.3** Agent listener over a normally
   verified and SHA-256-pinned server certificate. No plaintext HTTP delivery,
   no agent cross-Node access and no redirects.
4. Agent verifies the exact config SHA, route manifest and independently
   pinned Xray executable. The supplied Listener metadata is authenticated
   with the encrypted candidate and checked against its actual tags/ports.
5. Agent executes only a **bounded offline** `xray run -test -config ...`
   against a private temporary config. The binary's own SHA must match
   the owner-pinned digest. Only a bounded, secret-free success/error code
   returns to HUB; raw Xray output and Reality private keys are not logged.
6. HUB accepts evidence only from the same paired installation, with an
   idempotent validation receipt. Status becomes either
   `offline_syntax_validated_not_active` or
   `offline_validation_failed_not_active`.
7. **`can_activate` stays FALSE**, and both
   `needs_data_plane_proof` and `needs_subscription_commit` stay TRUE.
   No live Xray process/systemd service, Node lease, Inbound config or
   Subscription record is changed.

The staging store itself never holds an owner login session, Node secret
bearer, historical customer DB or traffic ledger. Its metadata is safe to
show the owner; confidential JSON bytes remain encrypted on disk and are
only unsealed for a paired Agent's HTTPS request.

## Disabled by default

This is NOT a ready-to-publish feature. It will not run unless both Hub and
Agent are explicitly opted in on separately provisioned NEW LAB machines.

**New lab HUB environment**, in a private root terminal (do not use on old HUB):

```bash
export DARK_PRO_LAB_MODE=1 DARK_PRO_ENABLE_REMOTE_AGENT=1
export DARK_PRO_ENABLE_CONFIG_STAGE=1

# Generate ONCE and keep securely OUTSIDE Git and the panel export.
# Restore the SAME value after every restart. Losing it makes encrypted
# staging artifacts unrecoverable.
umask 077
openssl rand -hex 32 > /root/dark-pro-stage-key.hex
chmod 600 /root/dark-pro-stage-key.hex
export DARK_PRO_STAGE_KEY_HEX="$(cat /root/dark-pro-stage-key.hex)"
```

Start the isolated HUB with an actual lab TLS listener, see
[node remote TLS instructions](node-remote-tls-lab-phase2.md):

```bash
go run ./cmd/hub \
  -listen 127.0.0.1:2087 \
  -registry ./lab-state/registry.json \
  -replacements ./lab-state/replacements.json \
  -staging ./lab-state/private-staging/configs.json \
  -agent-tls-listen :24443 \
  -agent-tls-cert /root/dark-pro-lab-tls/agent.pem \
  -agent-tls-key /root/dark-pro-lab-tls/agent.key
```

**New LAB Node environment:**

```bash
export DARK_PRO_LAB_MODE=1 DARK_PRO_ENABLE_REMOTE_AGENT=1
export DARK_PRO_ENABLE_XRAY_STAGE_VALIDATION=1

go run ./cmd/node-agent \
  -id new-lab-01 \
  -hub https://NEW_HUB_IP:24443 \
  -hub-ca-file /root/dark-pro-lab-ca.pem \
  -hub-cert-sha256 PINNED_TLS_LEAF_SHA256 \
  -stage-xray-binary /opt/isolated-new-node/xray \
  -stage-xray-sha256 TRUSTED_XRAY_EXECUTABLE_SHA256
```

The new Agent must already be paired. The Xray path is an independently
provisioned, **trusted, pinned** LAB binary; never copy the old production
binary or take a mutable unverified `latest`. The Agent worker polls staged
validation only; no config upload to Xray is executed.

### Owner staging request (private SSH tunnel only)

After the new Node is paired and a replacement operation has already
completed authenticated TCP preflight:

```http
POST /api/v1/admin/replacements/OPERATION_ID/candidate
Authorization: Bearer <LOCAL_LAB_OWNER_TOKEN>
Content-Type: application/json

{
  "trusted_xray_sha256": "TRUSTED_64_CHARACTER_EXECUTABLE_SHA256",
  "profiles": [
    {
      "inbound_id": 1,
      "transport": "tcp",
      "reality_target": "www.example.org:443",
      "server_names": ["www.example.org"],
      "reality_private_key": "<PRIVATE_X25519_KEY_FROM_A_NEW_TEST_NODE>",
      "short_ids": ["0123456789abcdef"],
      "users": [
        {"uuid":"af469f46-e18a-45f4-bf57-c7f649b10db5","email":"lab@pro.invalid"}
      ]
    }
  ]
}
```

All required profiles must match the operation's selected Inbound IDs. Do
not copy this sample into a live service: replace placeholders securely
in a local private file and never put real private keys in tickets, Git
commits, terminal history or public API calls.

The owner may inspect metadata using:

- `GET /api/v1/admin/config-stages`
- `GET /api/v1/admin/config-stages/{operation_id}`

The Agent-only TLS listener exposes:

- `GET /api/v1/agent/config-stage?node_id=...&installation_id=...`
- `POST /api/v1/agent/config-stage-report`

Owner/admin routes are never exposed on the Agent listener. The private
candidate is NEVER available over loopback HTTP (HTTP 426 for Agent fetch).

## Tests and known release blockers

Default GitHub CI verifies confidential AES-GCM persistence, wrong/corrupt
key rejection, disk/metadata tampering, owner/Agent permission separation,
TLS-only private delivery, wrong installation, wrong Xray SHA, successful
and failed offline candidate receipts, replay/idempotency and concurrent
stage creation. A disposable synthetic validator executable verifies the
agent-to-HUB flow, **not** real Xray operational compatibility.

Still missing:
- Real Xray-core syntax test with a separately pinned binary on the NEW VPS.
- Persistent *applied* desired revision, complete rollback journaling and
  health-checked **staged** Xray process replacement.
- Real Reality/VLESS TLS handshake/data-plane proof and tunnel traversal.
- Exact-once traffic billing across Nodes and per-route accounting attribution.
- Atomic, owner-approved subscription cutover and historical reconciliation.
- Browser owner sessions/RBAC/CSRF, WAN rate limiting and secure backup key
  escrow/rotation/recovery.
- Exhaustive tests of disconnect during config validation and Linux resource
  pressure.

No stage output authorizes activation or retirement of old Nodes. The
original DARK XRAY HUB is not contacted, and **nothing is deployed to
either real production or new test VPS** by GitHub changes in this phase.
