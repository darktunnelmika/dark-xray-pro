#!/usr/bin/env bash
# Install ONLY a separately provisioned DARK XRAY PRO LAB HUB.
# No production HUB/Node, existing Xray, tunnel, DNS or customer data is read.
set -Eeuo pipefail
IFS=$'\n\t'
umask 077

abort() { printf 'DARK PRO LAB INSTALL REFUSED: %s\n' "$*" >&2; exit 1; }
APP="/opt/dark-xray-pro-lab"
STATE="/var/lib/dark-xray-pro-lab"
ENV_DIR="/etc/dark-xray-pro-lab"
UNIT="/etc/systemd/system/dark-xray-pro-lab.service"
SERVICE="dark-xray-pro-lab.service"
BUNDLE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"

[[ "$(id -u)" == "0" ]] || abort "run as root only on your NEW empty LAB VPS"
[[ "${DARK_PRO_INSTALL_LAB_ACK:-}" == "I_CONFIRM_NEW_EMPTY_SERVER" ]] ||
  abort "set DARK_PRO_INSTALL_LAB_ACK=I_CONFIRM_NEW_EMPTY_SERVER first"
[[ -n "${DARK_PRO_EXPECT_PUBLIC_IP:-}" ]] ||
  abort "set DARK_PRO_EXPECT_PUBLIC_IP to the exact IP of the NEW LAB server"
[[ "${DARK_PRO_EXPECT_PUBLIC_IP}" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] ||
  abort "only an explicit IPv4 LAB target is accepted"
command -v curl >/dev/null || abort "curl is required for verifying the NEW target IP"
command -v systemctl >/dev/null || abort "systemd is required"
command -v openssl >/dev/null || abort "openssl is required"
command -v sha256sum >/dev/null || abort "sha256sum is required"
command -v ss >/dev/null || abort "iproute2 ss command is required"
command -v ip >/dev/null || abort "iproute2 ip command is required"
command -v useradd >/dev/null || abort "useradd is required"
command -v runuser >/dev/null || abort "runuser is required"
command -v find >/dev/null || abort "find is required"

[[ -f "$BUNDLE/SHA256SUMS" && -s "$BUNDLE/bin/dark-pro-hub" &&
   -s "$BUNDLE/bin/dark-pro-agent" && -s "$BUNDLE/web/index.html" ]] ||
  abort "expected checked LAB release bundle is incomplete"
(cd "$BUNDLE" && sha256sum -c --status SHA256SUMS) ||
  abort "bundle checksum verification failed — no installation performed"

# Verify the IP that is REALLY ASSIGNED to this new VPS's global NIC,
# and the selected outbound route's source address. An upstream NAT can make
# the Internet-visible egress IP differ, so never mistake that for host identity.
# Both local checks are required, not optional; neither accepts a second host.
LOCAL_IP_MATCH="$(ip -4 -o addr show scope global | awk -v target="$DARK_PRO_EXPECT_PUBLIC_IP" '
  {split($4, cidr, "/"); if (cidr[1] == target) found=1}
  END {print found+0}
')"
[[ "$LOCAL_IP_MATCH" == "1" ]] ||
  abort "expected LAB IP is NOT assigned to any global IPv4 interface"

ROUTE_SRC="$(ip -4 route get 1.1.1.1 | awk '
  {for (i=1;i<NF;i++) if ($i=="src") {print $(i+1); exit}}
')" || abort "cannot determine source IP for selected outbound route"
[[ "$ROUTE_SRC" == "$DARK_PRO_EXPECT_PUBLIC_IP" ]] ||
  abort "route source $ROUTE_SRC does not match intended new LAB IP $DARK_PRO_EXPECT_PUBLIC_IP"

# Egress is diagnostic only. It may legitimately be SNATed by the upstream.
ACTUAL_EGRESS_IP="$(curl --noproxy '*' -4fsS --connect-timeout 5 --max-time 10 https://api.ipify.org)" ||
  abort "cannot contact external IP reflector over HTTPS"
if [[ "$ACTUAL_EGRESS_IP" != "$DARK_PRO_EXPECT_PUBLIC_IP" ]]; then
  printf 'NOTICE: lab interface IP %s exits the Internet as %s (upstream NAT).\n' \
    "$DARK_PRO_EXPECT_PUBLIC_IP" "$ACTUAL_EGRESS_IP" >&2
fi
ACTUAL_IP="$DARK_PRO_EXPECT_PUBLIC_IP"

# Check for any pre-existing user service or old production panel.
for unit in dark-xray-hub.service dark-xray-node.service dark-xray.service \
            xray.service x-ui.service; do
  if systemctl is-active --quiet "$unit"; then
    abort "an existing Xray/production service $unit is running; refuse to modify this server"
  fi
done
[[ ! -e "$UNIT" && ! -e "$APP" && ! -e "$STATE" && ! -e "$ENV_DIR" ]] ||
  abort "a LAB or production install path already exists; never overwrite it"
if ss -lnt | grep -Eq '(^|[[:space:]])(127\.0\.0\.1|0\.0\.0\.0|\[::\]|\[::1\]):2087[[:space:]]'; then
  abort "management port 2087 is already occupied"
fi

# Confirm installed binary architecture. The release bundle intentionally
# carries only amd64 Linux assets and no Xray-core executable/old secrets.
[[ "$(uname -s)" == "Linux" && "$(uname -m)" == "x86_64" ]] ||
  abort "this LAB package is built only for Linux amd64"

if ! id darkprolab >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home \
    --home-dir "$STATE" --shell /usr/sbin/nologin darkprolab
fi
install -d -o root -g root -m 0755 "$APP" "$APP/bin" "$APP/web"
install -d -o darkprolab -g darkprolab -m 0700 "$STATE"
install -d -o root -g root -m 0700 "$ENV_DIR"

install -o root -g root -m 0755 "$BUNDLE/bin/dark-pro-hub" "$APP/bin/dark-pro-hub"
install -o root -g root -m 0755 "$BUNDLE/bin/dark-pro-agent" "$APP/bin/dark-pro-agent"
cp -a -- "$BUNDLE/web/." "$APP/web/"
# The source bundle is intentionally built with umask 077. Public static UI
# assets must be readable by the NON-ROOT darkprolab process, though private
# LAB state, owner token, Agent credentials and Reality keys stay 0600/0700.
find "$APP/web" -type d -exec chmod 0755 {} +
find "$APP/web" -type f -exec chmod 0644 {} +
chmod -R go-w "$APP"
chown -R root:root "$APP"

# A distinct random LAB owner token; this is NOT a credential from legacy HUB.
TOKEN="$(openssl rand -hex 32)"
cat > "$ENV_DIR/lab.env" <<EOF
DARK_PRO_LAB_MODE=1
DARK_PRO_LAB_TOKEN=$TOKEN
EOF
chmod 0600 "$ENV_DIR/lab.env"
unset TOKEN

cat > "$UNIT" <<EOF
[Unit]
Description=DARK XRAY PRO - ISOLATED LAB HUB (not production)
After=network-online.target
Wants=network-online.target
ConditionPathExists=$ENV_DIR/lab.env

[Service]
Type=simple
User=darkprolab
Group=darkprolab
EnvironmentFile=$ENV_DIR/lab.env
WorkingDirectory=$APP
ExecStart=$APP/bin/dark-pro-hub -listen 127.0.0.1:2087 -static $APP/web -registry $STATE/registry.json -replacements $STATE/replacements.json
Restart=on-failure
RestartSec=5
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
ReadWritePaths=$STATE
CapabilityBoundingSet=
AmbientCapabilities=
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "$UNIT"
systemctl daemon-reload
systemctl enable --now "$SERVICE"

# systemctl may return before the new Go process has entered ListenAndServe.
# Poll only localhost with a bounded cold-start deadline; never infer failure
# from the first instant of Connection Refused.
READY=0
for attempt in $(seq 1 30); do
  if curl --noproxy '*' -fsS --connect-timeout 1 --max-time 2 \
       http://127.0.0.1:2087/api/v1/health 2>/dev/null | grep -q '"status":"ok"'; then
    READY=1
    break
  fi
  systemctl is-active --quiet "$SERVICE" || break
  sleep 0.3
done
if [[ "$READY" != "1" ]]; then
  systemctl stop "$SERVICE" || true
  abort "LAB did not become healthy within the startup window; ONLY its own service was stopped"
fi
# Also verify that the unprivileged service can really read the compiled UI.
if ! runuser -u darkprolab -- test -r "$APP/web/index.html"; then
  systemctl stop "$SERVICE" || true
  abort "LAB UI is not readable by its unprivileged service account"
fi

printf '\nDARK XRAY PRO isolated LAB installed successfully on NEW server %s.\n' "$ACTUAL_IP"
printf 'The admin UI is restricted to localhost:2087 (not public).\n'
printf 'From your trusted PC, open an SSH tunnel:\n  ssh -L 2087:127.0.0.1:2087 root@%s\n' "$ACTUAL_IP"
printf 'Then open http://127.0.0.1:2087/ locally.\n'
printf 'Service: systemctl status %s --no-pager\n' "$SERVICE"
printf 'LAB credentials are private in %s/lab.env. Never share them.\n' "$ENV_DIR"
printf 'No Xray production service, Node, Inbound, DNS or Subscription was touched.\n'
