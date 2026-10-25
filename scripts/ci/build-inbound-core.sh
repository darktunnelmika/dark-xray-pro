#!/usr/bin/env bash
# Builds a separate pinned runtime; never installs or starts a system service.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: build-inbound-core.sh CLEAN_XRAY_CHECKOUT OUTPUT_DIRECTORY" >&2
  exit 2
fi

task_repo=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
task_source=$(cd -- "$1" && pwd)
mkdir -p -- "$2"
task_output=$(cd -- "$2" && pwd)
task_go=${DXP_BUILD_GO:-go}

case "$($task_go version)" in
  'go version go1.27.3 '*) ;;
  *) echo "the inbound runtime requires the pinned Go 1.27.3 toolchain" >&2; exit 1 ;;
esac

python3 "$task_repo/scripts/prepare-inbound-core.py" "$task_source"
cd -- "$task_source"
"$task_go" mod verify
"$task_go" test -race ./common/darkprov2 ./app/stats/command
"$task_go" test -race ./transport/internet/xdrive -run '^TestDarkProStorageProbe' -count=1
CGO_ENABLED=0 "$task_go" build -buildvcs=false -trimpath -o "$task_output/xray-inbound-v2" ./main
chmod 0755 "$task_output/xray-inbound-v2"
cp darkpro-inbound-provenance.json "$task_output/xray-inbound-v2.provenance.json"
cp LICENSE "$task_output/xray-inbound-v2.upstream-license"

python3 - "$task_output" <<'PY'
import hashlib, json, pathlib, sys
out = pathlib.Path(sys.argv[1])
binary = out / 'xray-inbound-v2'
digest = hashlib.sha256(binary.read_bytes()).hexdigest()
(out / 'xray-inbound-v2.sha256').write_text(digest + '  xray-inbound-v2\n')
path = out / 'xray-inbound-v2.provenance.json'
value = json.loads(path.read_text())
value.update(binary_sha256=digest, toolchain='go1.27.3', build_flags=['-buildvcs=false', '-trimpath'], cgo_enabled=False)
path.write_text(json.dumps(value, sort_keys=True, indent=2) + '\n')
PY

echo "Built independently pinned native inbound runtime: $task_output/xray-inbound-v2"
echo "Run the real traffic matrix with DXP_INBOUND_CORE_V2_BINARY and DXP_INBOUND_CORE_V2_SHA256."
