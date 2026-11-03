#!/usr/bin/env bash
# Disposable CI only. Never change host interfaces, routes or firewall rules.
set -euo pipefail
test "$(id -u)" -ne 0
test -n "${RUNNER_TEMP:?GitHub runner temporary directory required}"
test -n "${DARK_PRO_TEST_XRAY_BINARY:?Pinned experimental core required}"
test -n "${DARK_PRO_TEST_XRAY_SHA256:?Core SHA256 required}"
test "${GITHUB_ACTIONS:-}" = true
test -x "$DARK_PRO_TEST_XRAY_BINARY"
test "$(sha256sum "$DARK_PRO_TEST_XRAY_BINARY" | cut -d' ' -f1)" = "$DARK_PRO_TEST_XRAY_SHA256"

task_run='^(TestImportedLoopbackRealityStaysQuarantined|TestQuarantineRealAgentTLS)$'
task_output='quarantine.jsonl'
task_count=1
# Compile before entering a network that has no route to module proxies or DNS.
go test -c ./internal/business -o "$RUNNER_TEMP/h7-api.test"
go test -c ./cmd/node-agent -o "$RUNNER_TEMP/quarantine-worker.test"
task_ns="dark-pro-h7-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
task_user="$(id -un)"
created=0
cleanup() {
  if test "$created" = 1; then
    sudo ip netns del "$task_ns"
  fi
}
trap cleanup EXIT
sudo ip netns add "$task_ns"
created=1
sudo ip -n "$task_ns" link set lo up
# Synthetic local fixture, never a route to the real public address owner.
sudo ip -n "$task_ns" addr add 11.0.0.1/32 dev lo
# N19 ingress lives at a different literal tuple and only forwards to this
# namespace's loopback origins. No route to either real public owner exists.
sudo ip -n "$task_ns" addr add 11.0.0.2/32 dev lo
test -z "$(sudo ip -n "$task_ns" route show table main)"
test -z "$(sudo ip -n "$task_ns" -6 route show default)"
test "$(sudo ip -n "$task_ns" -o link show | wc -l)" -eq 1

sudo ip netns exec "$task_ns" runuser -u "$task_user" -- env \
  DARK_PRO_TEST_QUARANTINE_WORKER="$RUNNER_TEMP/quarantine-worker.test" \
  DARK_PRO_TEST_XRAY_BINARY="$DARK_PRO_TEST_XRAY_BINARY" \
  DARK_PRO_TEST_XRAY_SHA256="$DARK_PRO_TEST_XRAY_SHA256" \
  "$RUNNER_TEMP/h7-api.test" -test.run="$task_run" \
  -test.count="$task_count" -test.timeout=360s -test.v 2>&1 \
  | go tool test2json -t -p github.com/darktunnelmika/dark-xray-pro/internal/business \
  > "$RUNNER_TEMP/$task_output"

python3 - "$RUNNER_TEMP/$task_output" <<'PYCHECK'
import json,sys
events=[json.loads(line) for line in open(sys.argv[1])]
required={"TestImportedLoopbackRealityStaysQuarantined","TestQuarantineRealAgentTLS"}
assert required <= {e.get("Test") for e in events if e.get("Action")=="pass"}
assert not any(e.get("Action") in ("fail","skip") for e in events)
for e in events:
 if e.get("Output"): print(e["Output"],end="")
PYCHECK
