#!/usr/bin/env bash
# DARK XRAY PRO NEW-NODE ONLY — reviewed, checksum-verifiable provisioning.
# Never fetches or executes remote scripts, installs Xray, opens ports, changes
# the firewall, replaces a Node, or imports legacy settings.
set -Eeuo pipefail
IFS=$'\n\t'
umask 077

refuse() { printf 'DARK PRO NEW NODE INSTALL REFUSED: %s\n' "$*" >&2; exit 1; }
say() { printf 'DARK PRO NEW NODE: %s\n' "$*"; }

[[ "$(id -u)" == 0 ]] || refuse "execute as root on the separately purchased NEW empty Node only"
[[ "${DARK_PRO_NEW_NODE_ACK:-}" == "I_CONFIRM_NEW_EMPTY_PRO_NODE" ]] ||
  refuse "set DARK_PRO_NEW_NODE_ACK=I_CONFIRM_NEW_EMPTY_PRO_NODE explicitly"
[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] ||
  refuse "only isolated Linux amd64 Nodes are supported in this LAB"
for tool in ip ss sha256sum install systemctl runuser useradd grep sed awk find sort cmp stat; do
  command -v "$tool" >/dev/null || refuse "required command missing: $tool"
done

bundle="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
[[ -f "$bundle/SHA256SUMS" && -f "$bundle/dark-pro-agent" ]] ||
  refuse "extract the complete owner-downloaded ZIP before running this installer"
[[ ! -L "$bundle/dark-pro-agent" && ! -L "$bundle/SHA256SUMS" &&
   ! -L "$bundle/install-new-pro-node.sh" ]] || refuse "symlink in install bundle"
(cd "$bundle" && sha256sum -c --status SHA256SUMS) ||
  refuse "Agent binary/script checksum verification failed before installation"
[[ "${DARK_PRO_EXPECT_NODE_IP:-}" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] ||
  refuse "DARK_PRO_EXPECT_NODE_IP must be the NEW server assigned IPv4"
[[ "${DARK_PRO_NODE_ID:-}" =~ ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$ ]] ||
  refuse "DARK_PRO_NODE_ID must equal the new invitation Node ID"
[[ "${DARK_PRO_HUB_AGENT_URL:-}" =~ ^https://[A-Za-z0-9.-]+:[0-9]{2,5}$ ]] ||
  refuse "HUB Agent URL must be an explicitly verified https://hostname-or-ip:port"
[[ "${DARK_PRO_HUB_PIN_SHA256:-}" =~ ^[a-f0-9]{64}$ ]] ||
  refuse "the verified HUB leaf SHA256 certificate fingerprint must be 64 lowercase hex characters"
IFS='.' read -r a b c e <<<"$DARK_PRO_EXPECT_NODE_IP"
for octet in "$a" "$b" "$c" "$e"; do
  [[ -n "$octet" && "$octet" =~ ^[0-9]{1,3}$ ]] ||
    refuse "invalid IPv4 octet"
  (( 10#$octet <= 255 )) || refuse "IPv4 octet out of range"
done
local_match="$(ip -4 -o addr show scope global | awk -v target="$DARK_PRO_EXPECT_NODE_IP" '
  {split($4,addr,"/"); if (addr[1]==target) found=1}
  END{print found+0}')"
[[ "$local_match" == 1 ]] || refuse "the specified NEW Node IP is not assigned to this machine"
[[ "${DARK_PRO_HUB_AGENT_URL}" != *"127.0.0.1"* ]] ||
  refuse "a separate Node must not point to a loopback HUB Agent listener"

# Refuse any old panel/Agent/Xray, including an already-created PRO service.
for name in dark-xray-node.service dark-xray-hub.service dark-xray.service             xray.service x-ui.service dark-pro-node-agent.service; do
  systemctl is-active --quiet "$name" &&
    refuse "existing Node/Xray service detected: $name"
done
for legacy in /opt/dark-xray /opt/dark-xray-node /etc/dark-xray               /var/lib/dark-xray /opt/dark-xray-pro-lab; do
  [[ ! -e "$legacy" && ! -L "$legacy" ]] ||
    refuse "this is not a fresh Node: existing DARK XRAY installation is present"
done

app=/opt/dark-pro-node-agent
state=/var/lib/dark-pro-node-agent
config=/etc/dark-pro-node-agent
unit=/etc/systemd/system/dark-pro-node-agent.service
[[ ! -e "$unit" && ! -L "$unit" ]] ||
  refuse "Node Agent service already exists; never overwrite a paired Node"
[[ ! -L "$app" && ! -L "$state" && ! -L "$config" ]] ||
  refuse "symlink detected at a protected PRO location"
if [[ -d "$app" ]]; then
  [[ -f "$app/dark-pro-agent" && ! -L "$app/dark-pro-agent" ]] ||
    refuse "unrecognized existing Agent installation"
  cmp -s "$bundle/dark-pro-agent" "$app/dark-pro-agent" ||
    refuse "candidate Agent differs from a partial installation; refuse silent replacement"
elif [[ -e "$app" ]]; then
  refuse "unexpected install path: $app"
fi

if [[ -n "${DARK_PRO_HUB_CA_FILE:-}" ]]; then
  [[ -f "$DARK_PRO_HUB_CA_FILE" && ! -L "$DARK_PRO_HUB_CA_FILE" ]] ||
    refuse "optional CA bundle must be an operator-verified local file"
  [[ "$(stat -c %a "$DARK_PRO_HUB_CA_FILE")" =~ ^[0-7]{3,4}$ ]] ||
    refuse "invalid CA bundle file mode"
fi

# First validate HTTPS chain + SAN + pin BEFORE requesting a Pair Code,
# writing identity, making any systemd changes or creating a Node.
probe_ca_args=()
if [[ -n "${DARK_PRO_HUB_CA_FILE:-}" ]]; then
  probe_ca_args=(-hub-ca-file "$DARK_PRO_HUB_CA_FILE")
fi
DARK_PRO_LAB_MODE=1 DARK_PRO_ENABLE_REMOTE_AGENT=1  "$bundle/dark-pro-agent" -id "$DARK_PRO_NODE_ID"  -hub "$DARK_PRO_HUB_AGENT_URL" -hub-cert-sha256 "$DARK_PRO_HUB_PIN_SHA256"  "${probe_ca_args[@]}" -probe-only ||
  refuse "pinned remote TLS Agent listener could not be verified; no service installed"

if [[ ! -d "$app" ]]; then install -d -m 0755 -o root -g root "$app"; fi
install -o root -g root -m 0755 "$bundle/dark-pro-agent" "$app/dark-pro-agent"
if ! id darkproagent >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home --home-dir "$state"     --shell /usr/sbin/nologin darkproagent
fi
install -d -m 0700 -o darkproagent -g darkproagent "$state"
install -d -m 0750 -o root -g darkproagent "$config"
identity="$state/identity.json"
ca_args=()
service_ca_line=""
if [[ -n "${DARK_PRO_HUB_CA_FILE:-}" ]]; then
  if [[ -e "$config/hub-ca.pem" ]]; then
    cmp -s "$DARK_PRO_HUB_CA_FILE" "$config/hub-ca.pem" ||
      refuse "previous CA differs; no silent trust rotation"
  else
    install -o root -g darkproagent -m 0640 "$DARK_PRO_HUB_CA_FILE" "$config/hub-ca.pem"
  fi
  ca_args=(-hub-ca-file "$config/hub-ca.pem")
  service_ca_line=" -hub-ca-file $config/hub-ca.pem"
fi

say "Verified Agent binary staged; enter the one-time ticket on this NEW Node only."
if [[ ! -t 0 ]]; then
  refuse "a real interactive terminal is required for the private Pair Code"
fi
read -r -s -p 'One-time Pair Code (hidden): ' secret
printf '\n'
[[ "$secret" =~ ^[a-f0-9]{64}$ ]] || refuse "invalid one-use Pair Code"
# Never put the Pair Code in a URL, installer command, file or process argv.
# The Agent persists its own identity with permission 0600, and removes the
# pending code after the HTTPS pairing acknowledgment.
export DARK_PRO_PAIR_CODE="$secret"
unset secret
export DARK_PRO_LAB_MODE=1 DARK_PRO_ENABLE_REMOTE_AGENT=1
runuser --preserve-environment -u darkproagent -- "$app/dark-pro-agent"   -id "$DARK_PRO_NODE_ID" -hub "$DARK_PRO_HUB_AGENT_URL"   -hub-cert-sha256 "$DARK_PRO_HUB_PIN_SHA256"   -identity-file "$identity" "${ca_args[@]}" -enroll-only ||
    refuse "pairing failed; pending private identity is preserved for an explicit retry"
unset DARK_PRO_PAIR_CODE
[[ -f "$identity" && ! -L "$identity" ]] ||
  refuse "paired Agent identity was not saved"
[[ "$(stat -c %a "$identity")" == 600 ]] ||
  refuse "Agent identity permissions are unsafe"

# No Pair Code or Agent Token is ever stored in this unit.
cat > "$unit" <<EOF
[Unit]
Description=DARK XRAY PRO - NEW isolated Agent (telemetry-only)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=darkproagent
Group=darkproagent
WorkingDirectory=$state
Environment=DARK_PRO_LAB_MODE=1
Environment=DARK_PRO_ENABLE_REMOTE_AGENT=1
ExecStart=$app/dark-pro-agent -id $DARK_PRO_NODE_ID -hub $DARK_PRO_HUB_AGENT_URL -hub-cert-sha256 $DARK_PRO_HUB_PIN_SHA256 -identity-file $identity$service_ca_line
Restart=on-failure
RestartSec=5
UMask=0077
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths=$state
LimitNOFILE=8192

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "$unit"
systemctl daemon-reload
systemctl enable --now dark-pro-node-agent.service
systemctl is-active --quiet dark-pro-node-agent.service ||
  refuse "pairing is preserved but the NEW Agent service is not healthy; inspect its own logs"
say "DONE: NEW PRO Node Agent paired and running without any Xray customer listeners."
say "Verify Online and fresh CPU/RAM in the owner Nodes panel."
