#!/usr/bin/env bash
# Dark Xray Pro — one-line installer:
#   bash <(curl -fsSL https://raw.githubusercontent.com/darktunnelmika/dark-xray-pro/main/scripts/install.sh)
# It installs the "dxp" manager and runs "dxp install". Answers can be given
# ahead as DXP_* variables (see the README); DXP_YES=1 takes every default.
set -e
[ "$(id -u)" = 0 ] || { echo "run as root (sudo -i)"; exit 1; }
REF="${DXP_REF:-main}"
command -v curl >/dev/null || { apt-get update -q >/dev/null && apt-get install -y -q curl >/dev/null; }
curl -fsSL -m 60 -o /usr/local/bin/dxp "https://raw.githubusercontent.com/darktunnelmika/dark-xray-pro/${REF}/scripts/dxp"
chmod 0755 /usr/local/bin/dxp
exec /usr/local/bin/dxp install "$@"
