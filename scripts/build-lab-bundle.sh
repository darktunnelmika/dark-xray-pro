#!/usr/bin/env bash
# Build a LOCAL, checksum-verifiable LAB-only bundle from an exactly pinned
# DARK XRAY PRO commit. Does not install a service or modify any Xray config.
set -Eeuo pipefail
IFS=$'\n\t'
umask 077

fail() { printf 'DARK PRO LAB BUILD REFUSED: %s\n' "$*" >&2; exit 1; }
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$ROOT"
[[ -n "${DARK_PRO_PINNED_COMMIT:-}" && "${#DARK_PRO_PINNED_COMMIT}" == 40 ]] ||
  fail "set DARK_PRO_PINNED_COMMIT to the trusted exact 40-character Git commit SHA"
[[ "$(git rev-parse HEAD)" == "$DARK_PRO_PINNED_COMMIT" ]] ||
  fail "source checkout SHA does not match the trusted pinned Git commit"
[[ -z "$(git status --porcelain --untracked-files=no)" ]] ||
  fail "tracked sources were modified after checkout"
[[ "$(uname -s)" == "Linux" && "$(uname -m)" == "x86_64" ]] ||
  fail "this LAB builder targets Linux amd64 only"
for tool in git go node npm sha256sum find sort xargs install; do
  command -v "$tool" >/dev/null || fail "required builder tool not found: $tool"
done
[[ ! -e release ]] || fail "release directory already exists; use a fresh checkout"
mkdir -p release/bin release/scripts

# A release is built in CI, never by the installed server. Build frontend FIRST
# so one binary contains exactly this source revision's compiled UI.
echo 'Compiling and embedding the matching React UI...'
(cd web && npm install --no-audit --no-fund && npm run build)
[[ ! -L internal/webui/dist && ! -L internal/webui/dist/assets ]] || fail "unsafe generated asset path"
mkdir -p internal/webui/dist
# Only generated files inside the fixed source-local embedding directory.
find internal/webui/dist -mindepth 1 -maxdepth 1 ! -name README.txt -exec rm -rf -- {} +
cp -a -- web/dist/. internal/webui/dist/
[[ -s internal/webui/dist/index.html ]] || fail "compiled UI is missing"

echo "Building isolated HUB + Agent from pinned source $(git rev-parse --short HEAD)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' \
  -o release/bin/dark-pro-hub ./cmd/hub
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' \
  -o release/bin/dark-pro-agent ./cmd/node-agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' \
  -o release/bin/dark-pro-owner-setup ./cmd/owner-setup
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' \
  -o release/bin/dark-pro-backup ./cmd/pro-backup

install -m 0755 scripts/install-lab.sh release/scripts/install-lab.sh
install -m 0755 internal/nodebootstrap/install-new-pro-node.sh release/scripts/install-new-pro-node.sh

(cd release && find bin scripts -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
(cd release && sha256sum -c SHA256SUMS)
echo "Build verified from pinned commit: $DARK_PRO_PINNED_COMMIT"
echo "Bundle directory: $ROOT/release"
echo "Next: review scripts/install-lab.sh and run ONLY on the explicitly identified new LAB VPS."
