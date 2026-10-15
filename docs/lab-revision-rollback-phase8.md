# Phase 8 — encrypted, revisioned LAB config selection and rollback

**This is a crash-safe rehearsal journal, NOT a running Xray apply/rollback
engine.** Its `selected_version` is a private LAB pointer; it is NOT an
applied Xray service version. Nothing changes any service, Node, user,
representative credit, live Inbound, tunnel, subscription or billing record.

## Current end-to-end prerequisites

1. A NEW, independently paired PRO LAB Node has a valid Node installation ID.
2. An owner explicitly created the replacement draft with Direct/Tunnel
   settings. It is sealed to that new installation, and bounded TCP probes
   passed (Phase 4).
3. The owner supplied a fully validated typed new Xray candidate and an
   independently trusted Xray executable SHA; the config is stored
   AES-GCM encrypted on the NEW HUB (Phase 6).
4. The paired Agent verified the real pinned Xray syntax and an optionally
   enabled, non-root, disposable Xray process bound all ephemeral loopback
   listeners before it was fully terminated (Phase 7).
5. The staged metadata has
   `offline_syntax_validated_not_active`,
   `canary_attempted=true`,
   `canary_loopback_bound=true`,
   `can_activate=false`,
   `needs_data_plane_proof=true`,
   and `needs_subscription_commit=true`.

If even one condition is absent, the new LAB revision is refused.

## Durable journal

`internal/labrevision` encrypts the exact confidential candidate JSON at
rest with AES-256-GCM. A domain-separated key is derived from the existing
external staging key; the key is NOT stored in the JSON journal or Git.

The metadata contains only the new Node ID, installation, selected Stage ID,
config/manifest/Xray hashes, listener count, and creation time.
Every field of each version is authenticated with the ciphertext, and
an independent HMAC-SHA256 authenticates the *entire journal*: generation
number, selected/previous pointers and audit events. Altered secrets,
pointers, revision generations or audit events fail closed on restart.

The journal uses a private directory (0700), private file (0600), temporary
write, fsync and atomic rename. The previous candidate is **never deleted**
when another LAB candidate is selected, and all state survives restart.
Different new-Node installations cannot be selected in the same single-Node
LAB journal. Multi-Node independent journals remain future work.

## Owner-only API (SSH tunnel, localhost 2087)

All endpoints are disabled by default. They require the existing Phase 6
staging feature and explicit `DARK_PRO_ENABLE_LAB_REVISION_ROLLBACK=1`.

- `POST /api/v1/admin/lab-revisions/{operation_id}/prepare`: decrypts a
  previously verified Stage **internally on HUB**, re-encrypts the exact
  candidate as a prepared LAB version, and returns metadata only.
- `GET /api/v1/admin/lab-revisions`: generation, versions and
  audit events without Reality private credentials.
- `POST /api/v1/admin/lab-revisions/select`:
  `{"version_id":"ID","expected_generation":2}`. Compare-and-swap picks
  the candidate for **future LAB rehearsal**, preserves the previous
  selection, increments generation and records an immutable audit event.
- `POST /api/v1/admin/lab-revisions/rollback-selection`:
  `{"expected_generation":5}`. Restores the previous **LAB selection**
  and clears the rollback pointer so a repeated request cannot toggle
  back to the failed version.

Stale expected generations return conflict. Tokens from an Agent or the old
HUB cannot access owner endpoints; the separate remote TLS Agent port
does not expose them.

### Nonactivation guarantees

Every API snapshot contains:

- `actual_xray_process_changed=false`
- `needs_live_health_and_protocol_proof=true`
- `needs_subscription_commit=true`

Every prepared version contains `live_applied=false` and
`can_activate=false`. The endpoints never execute `systemctl`,
write `/etc/xray`, open public sockets, or send an Xray apply command.
This **is not a live rollback** despite its durable recovery properties.

## Testing and remaining actions

Go race tests exercise two distinct generations, restoring the old
encrypted version after a bad LAB selection, stale/racing owner CAS, old
Node credential rejection, encryption-key/cipher tampering, journal
generation/audit HMAC tampering, symlinks/permissions and private data
redaction. GitHub's pinned real Xray job still validates syntax and
ephemeral localhost binds on a disposable machine.

Before production, implement a distinct, **explicitly approved** new-Node
Xray process supervisor with desired-revision ACKs and a secure previous
runtime snapshot, real VLESS+REALITY handshake and per-route data-plane
tests, automatic rollback of a *live* failed apply, and transactional
subscription publication only after a verified new destination. Multi-hop
usage must be reconciled without double-billing.

**No changes have been made to the old DARK XRAY HUB, existing Nodes,
existing clients, or the not-yet-paired new VPS 213.142.132.207.**
