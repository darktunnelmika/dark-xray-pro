# Native Inbounds and durable Xray supervisor — development acceptance

This change connects NEW PRO client records and native Inbound assignments to a separately paired NEW Node Agent. It is not a production release, legacy migration, public subscription publisher, or authorization to change the working DARK XRAY fleet.

## Implemented path

Owner session + CSRF → native Reality Inbound → existing PRO client UUID binding → explicit new physical Node installation/IP/ports → transactional node candidate → CA-verified, leaf-pinned TLS 1.3 Agent delivery → short installation/config/revision-bound lease → pinned Xray process on the ACTUAL assigned ports.

The renderer emits Direct and Swap-destination listener tags. It does not automatically configure the upstream tunnel/relay or claim full Dark Swap end-to-end acceptance. Shared Node ports remain explicitly ambiguous for route accounting.

## Durable runtime guarantees tested

- Candidate syntax/hash and node/interface/listener consistency are checked before interrupting the previous process.
- An encrypted, authenticated AES-GCM journal is fsynced before the intent is enacted, then committed after process-owned sockets become ready.
- Actual port ownership is checked in Linux /proc against the child process, not inferred from any socket accepting TCP.
- A failed replacement can restart the last-good configuration only while its earlier lease is still valid. Explicit rollback is a new monotonic signed operation.
- A failed revision may be retried with unchanged content and a strictly fresh lease; reusing a sequence with different content is rejected.
- Process-level expiry uses an independent timer, so a blocked controller mutex or slow validation cannot extend authority.
- Kernel parent-death signaling stops the owned Xray child when its supervisor is SIGKILLed. The creating OS thread remains pinned while the child runs.
- Restart never trusts persisted PID/readiness/lease fields. It waits for a fresh HUB lease and recovers only the committed last-good configuration, never an interrupted pending candidate.
- Double ownership of the runtime directory is refused with a process lock; wrong state key, wrong installation, unsafe path, foreign port and changed binary are refused.
- A failed journal write fences the controller and stops its own process rather than reporting a false commit.

No global killall/systemctl, old PID adoption or public-port remapping is used. Candidate files needed by a running core are private 0600 temporary files; the restart journal is encrypted. These guarantees do not make the process swap hitless: active connections to the changed core can be interrupted.

## Business/identity rules

SQLite upgrades forward from v4 to v5, retaining prior clients, UUIDs, expiry, sessions, representative credits, groups and immutable financial history. Native Reality private profiles are encrypted with a separately supplied 32-byte key. Browser responses contain only metadata and the public key.

Bindings use the existing PRO client UUIDs. Disabled/archived/expired clients and disabled representatives are excluded from generated credentials, without deleting their records or financial history. Removing the last user retains the listener with an empty clients array, so the previously deployed credential cannot remain active through a dropped Inbound. Node config sequence changes only when its content/manifest changes. The lease is capped at the earliest included client expiry.

## Explicit opt-ins only

HUB: DARK_PRO_LAB_MODE=1, existing private business database opt-in, DARK_PRO_ENABLE_NATIVE_INBOUNDS=1, independent DARK_PRO_NATIVE_INBOUND_KEY_HEX, and the existing separate verified TLS Agent listener.

Agent: existing fresh pairing + verified/pinned HTTPS, DARK_PRO_ENABLE_NATIVE_RUNTIME=1, independent DARK_PRO_NATIVE_RUNTIME_KEY_HEX, --managed-runtime-dir pointing at a NEW private directory, --managed-listen-ip assigned to this host, and existing explicit --stage-xray-binary/--stage-xray-sha256 pins. Management ports 2087, 8443 and 24443 are reserved by the CLI.

Without these opt-ins the installed system is unchanged. No installer, live service, Nginx rule, domain, port or bot token is changed by this commit.

Owner-only routes (browser session + same-origin JSON + CSRF):
- GET/POST /api/v1/workspace/native-inbounds
- POST /api/v1/workspace/native-inbounds/{id}/clients
- POST /api/v1/workspace/native-inbounds/{id}/target
- POST /api/v1/workspace/native-nodes/{id}/preview

Private Agent-only route:
- GET /api/v1/agent/native-runtime, requiring TLS 1.3 and the exact paired installation token.

## Acceptance evidence

Local non-root test suite: all Go packages pass. Tests cover real local socket ownership, failed Apply, actual same-port rollback, lease expiry independent of controller lock, forced journal failure, SIGKILL at the persisted-intent checkpoint, fresh-lease recovery, input/account isolation and v4→v5 database preservation.

Pinned upstream Xray-core v26.9.9 tests cover:
1. Real process Apply → new revision → Rollback → supervisor close/reopen → last-good recovery on the SAME ports.
2. Native owner/client binding → verified/pinned TLS Agent delivery → real Xray apply → client revoke/empty inbound → fresh TLS recovery.
3. Actual VLESS/REALITY client → random application payload → UUID removal denies traffic → signed rollback restores traffic.

The application-traffic test uses a disposable loopback TLS 1.3 target and a narrowly allowlisted disposable loopback HTTP echo port. That exception is test-only; production profile rendering does not enable general loopback/private-network access. Tests do not access real customer traffic or third-party echo services.

Runtime snapshots still report data_plane_verified=false and subscription_published=false: CI proving a synthetic local route is not evidence about an arbitrary customer's deployed Internet route.

## Remaining release gates

The existing GUI Inbounds page is still a draft editor; connect it to the secured native APIs with owner workflows and conflict handling. Add durable desired/applied reporting, enforced user traffic/limits, native subscription publishing/revocation and the complete representative/Telegram purchase flow. Validate a separate real remote Node, actual tunnel/Dark Swap reachability, realistic pressure, backup/restore and complete legacy data migration. Preserve the agreed old external panel port contract and only change the final domain after owner approval.

Current production DARK XRAY and the installed PRO preview are untouched. Passing this checkpoint is not completion of the entire PRO panel.
