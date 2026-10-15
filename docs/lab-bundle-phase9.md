# Phase 9 — verifiable NEW LAB HUB installation bundle

**Not a production installer.** The original DARK XRAY HUB, Nodes, clients,
tunnels, database, representatives, subscriptions and bot must remain
untouched. The new VPS `80.253.255.181` is not currently connected to
Desktop Commander; this procedure has **not** been executed on it.

## GitHub Actions produces the bundle

The `Linux amd64 LAB bundle (no deployment)` CI job builds:

- `bin/dark-pro-hub`: isolated static Go HUB
- `bin/dark-pro-agent`: new Agent executable (included for future testing,
  **not** installed as an Agent service)
- `web/`: compiled React/TypeScript UI
- `scripts/install-lab.sh`: guarded LAB-only installer
- `SHA256SUMS`: file checksums inside the archive

The workflow also publishes
`dark-xray-pro-lab-linux-amd64.tar.gz.sha256` for integrity checking the
outside archive. It uploads only compiled binaries/JS/installer — **no**
existing Xray executable, users, backups or Reality private keys. Artifacts
are retained for 7 days. CI never connects to any VPS or deploys services.

[GitHub Actions runs](https://github.com/darktunnelmika/dark-xray-pro/actions)

## Explicitly verify NEW VPS and artifact before running

Only when physical access or a trusted controller for the **new, empty VPS**
has been independently re-established, retrieve both files from the
successful GitHub Actions LAB-bundle job. These commands are provided for
**that new empty server only**; do not run them on the old HUB.

```bash
sha256sum -c dark-xray-pro-lab-linux-amd64.tar.gz.sha256
mkdir -p /root/dark-pro-lab-bundle
tar -xzf dark-xray-pro-lab-linux-amd64.tar.gz \
  -C /root/dark-pro-lab-bundle
cd /root/dark-pro-lab-bundle
sha256sum -c SHA256SUMS

# The installer refuses to continue on a server whose public IPv4 address
# does not match this independently selected new VPS.
DARK_PRO_EXPECT_PUBLIC_IP=80.253.255.181 \
DARK_PRO_INSTALL_LAB_ACK=I_CONFIRM_NEW_EMPTY_SERVER \
bash scripts/install-lab.sh
```

The script must run as root **only on the new, empty LAB VPS**. It checks
archive file hashes before any mutation; requires the expected IPv4 to be assigned to a global NIC and used as
outbound route source; it logs (but does not confuse) any different SNAT
Internet egress IP; rejects mismatched new-server identities; refuses active legacy/Xray services or an already
occupied port 2087; and refuses to overwrite an existing PRO LAB install.
It creates a separate unprivileged `darkprolab` service account and only:

- `/opt/dark-xray-pro-lab/`: isolated Go executables and static UI
- `/var/lib/dark-xray-pro-lab/`: private LAB Node/operation registry
- `/etc/dark-xray-pro-lab/lab.env`: new, private 0600 LAB owner bearer
- `/etc/systemd/system/dark-xray-pro-lab.service`: new, separate service

The management API is always on **127.0.0.1:2087**, never public.
It does not create an Agent TLS endpoint by default, does not install
Xray and does not use `/etc/xray` or a legacy configuration path.

The service runs non-root with no capabilities, systemd filesystem
protection and writes only to its own state directory. The installer
performs a localhost health request and stops its **new service only** if
that check fails.

## Viewing the LAB UI

From a trusted computer:

```bash
ssh -L 2087:127.0.0.1:2087 root@80.253.255.181
```

Browse `http://127.0.0.1:2087` on that computer. The current React
frontend supports light/dark/system theme, Persian/English and live
Node telemetry. **A production-safe browser owner-login system is not
implemented yet**. Keep it on SSH-tunneled localhost only.

## Release blockers

This LAB bundle is a delivery vehicle, not an installable VPN
production control plane. The encrypted staging, real Xray loopback canary,
revision selection and rollback journal do not yet apply or rollback a
running Xray service. Real VLESS+REALITY sessions, per-hop traffic
reconciliation, IP Limit, user/representative/Restore data models,
full encrypted backup, verified subscription migration, owner/rep RBAC
and a safe production installer are still required.

**Desktop Commander access to the new VPS is verified separately.** The
installer still requires on-host safety checks; a GitHub CI PASS does not
mean that the new VPS is installed.

## Building from source if GitHub Actions artifact download requires login

GitHub's `actions/artifacts/{id}/zip` API can require authentication even
for a public repository. Rather than exposing your GitHub API credentials
or using an untrusted artifact mirror, clone this public repository **on
the independently verified new LAB VPS** and build the same file structure
with `scripts/build-lab-bundle.sh`. On Ubuntu 24.04, the signed distribution
package `golang-1.24-go` is a supported way to supply Go for this lab. The
source checkout must be pinned to an explicitly verified Git commit SHA.

```bash
# ONLY on independently identified NEW LAB server 80.253.255.181.
sudo apt-get install -y golang-1.24-go
export PATH=/usr/lib/go-1.24/bin:"$PATH"
git clone https://github.com/darktunnelmika/dark-xray-pro.git /root/dark-pro-source
cd /root/dark-pro-source
git checkout EXACT_TRUSTED_PRO_COMMIT_SHA
DARK_PRO_PINNED_COMMIT=EXACT_TRUSTED_PRO_COMMIT_SHA bash scripts/build-lab-bundle.sh
cd release
sha256sum -c SHA256SUMS
DARK_PRO_EXPECT_PUBLIC_IP=80.253.255.181 \
DARK_PRO_INSTALL_LAB_ACK=I_CONFIRM_NEW_EMPTY_SERVER \
bash scripts/install-lab.sh
```

`EXACT_TRUSTED_PRO_COMMIT_SHA` must be replaced by the real verified 40
character commit from GitHub. The builder refuses modified tracked sources,
incorrect commit, wrong architecture or an existing `release` output.

**Target identity edge case:** on one new lab host the NIC IP was
`80.253.255.181` while the outward Internet egress appeared as
`80.253.254.48`. The installer checks the real IPv4 assigned to the
global network interface **and** the kernel-selected route source for
`1.1.1.1`, both of which must equal `DARK_PRO_EXPECT_PUBLIC_IP`.
It separately prints upstream SNAT as a notice. SNAT mismatch alone
does not cause it to mistake the LAB VPS for a different host. It does
not relax checks for already running Xray/HUB services.
