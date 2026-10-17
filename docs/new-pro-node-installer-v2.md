# DARK XRAY PRO — New Node Manager V2: verified bootstrap

## Scope and outcome

This increment completes the safe **installer/package workflow** for a genuinely
new, separately provisioned Linux amd64 VPS. It does not claim to connect an
external Node where none has been supplied. No old DARK XRAY Nodes, users,
HUB, tunnels, Xray config, or financial data may be imported, modified, or
used for network testing.

The existing NEW PRO Agent already supports HTTPS TLS 1.3, trusted hostname/IP
certificate SAN verification, private CA (when explicitly supplied), an
independent SHA-256 leaf certificate pin, one-use Pair Codes, private persistent
Agent installation identity, a 3s pulse, a 5s host-metrics update and nonbillable
history. This release adds only the missing clean installer distribution and
pair-then-run supervision, without enabling native customer Xray automatically.

## Owner UI: exact existing Node Manager page

1. Sign in as the owner. In **Nodes**, download the release-matched
   `dark-pro-new-node-installer.zip`. The source is the exact NEW PRO release
   Agent binary adjacent to the running HUB executable, never the old system.
   An owner-only, GET-only endpoint issues the package.
2. Keep the bundle and transfer it to an empty NEW VPS using an already
   authorized private SSH/SFTP method. The UI never requests or stores the
   machine's root password or SSH keys.
3. On a **different** Linux amd64 VPS, verify the assigned server IP, and
   obtain an independently verified dedicated Agent TLS endpoint with
   hostname/IP SAN, proper trusted CA and SHA256 leaf pin. The UI reports
   the real listener status. A loopback-only endpoint is **not** remote-ready.
4. Create a fresh logical Node ID and Pair Code **after** the package is on
   the new VPS. Pair Codes expire in ten minutes and display only once.
   The copied command never embeds the Pair Code; installer prompts privately.
5. The installer checks its SHA256SUMS manifest, full host/IP acknowledgement,
   existing legacy/NODE/Xray services and pinned TLS health before creating
   a dedicated unprivileged system user or a private identity.
6. With successful pairing ACK it stores the NEW Node identity privately, drops
   the one-time code and starts `dark-pro-node-agent.service`. systemd has no
   Pair Code, password or Agent token in ExecStart/Environment.
7. The owner sees the node Online only after authenticated pulse. CPU/RAM/etc.
   are Current independently of the heartbeat. First physical interface rates
   are unknown, not zero; 1h/24h history fills only with real points.

## Exact private browser APIs

- Existing owner-only `GET /api/v1/workspace/nodes` includes
  `installer_available` and `agent_transport_scope`.
- New `GET /api/v1/workspace/nodes/installer-bundle` returns a ZIP of:
  `dark-pro-agent`, `install-new-pro-node.sh` and `SHA256SUMS`.
- The ZIP never contains a Pair Code, private Agent identity, TLS secret,
  customer information or billing record; all checksums are verified on
  destination BEFORE privileged changes.
- Anonymous, bearer-token-only, representatives and Agent TLS sessions
  cannot fetch a binary. Other methods are rejected.
- The production Nginx reverse proxy must allowlist **exactly GET**, no
  new routes or client mutation:

```nginx
location = /api/v1/workspace/nodes/installer-bundle {
    limit_except GET { deny all; }
    proxy_pass http://127.0.0.1:2087;
}
```

## Agent flags and safety

- `-probe-only` validates trusted TLS 1.3 Agent listener and pin; exits
  without creating an identity, enrolling or launching any Xray.
- `-enroll-only` uses the one-use code and self-generated installation
  credential; persists the paired identity; exits before telemetry and all
  native runtime facilities. The installer then starts normal telemetry-only
  Agent under systemd with no Pair Code environment.
- A lost pairing ACK can be retried using the identical pending private
  identity; unsafe replacement with a different Agent binary or CA is refused.
- The install script never opens host ports, changes firewall, downloads
  arbitrary scripts, installs Xray or deletes old Node data. It refuses to run
  on an existing HUB, existing Node, or with stale/unknown local IP.
- The separate native Xray runtime and customer data-plane flags remain
  **disabled**; an isolated Node connection is not an activated subscription.

## Live prerequisite: external TLS reachability

The current NEW PRO LAB still binds its Agent-only TLS port to
`127.0.0.1:24443`. That endpoint is intentionally NOT reachable from a
different VPS. The installer and new UI correctly block false success rather
than weakening TLS or silently publishing Agent APIs through the owner web
proxy. A separate reviewed change must install an externally reachable,
firewall-restricted dedicated TLS 1.3 listener with trusted SAN, independent
pin verification, rate limits and the new VPS source address approved.
Only then can the actual NEW external VPS pair.

## Release discipline

Require targeted Go tests, full Go -race and vet, pinned Xray acceptance,
Chromium owner+representative+320px RTL, and real native metering CI.
Deploy only to NEW PRO HUB, keep its previous release for rollback and at
most two operational release directories. Back up only the NEW PRO SQLite
databases and Nginx/systemd config. No legacy migration or test with old Nodes.
