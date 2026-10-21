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

case "${1-}" in
  '') task_run='^TestRealPinnedMeteredRouteActiveTraffic$'; task_count=3; task_output='hub-active-traffic.jsonl' ;;
  --unknown-tail-stress) task_run='^TestRealPinnedMeteredRouteActiveTraffic$/^unknown-tail$'; task_count=20; task_output='hub-active-traffic-unknown-tail.jsonl' ;;
  --handover) task_run='^TestRealPinnedMeteredHandoverActiveTraffic$'; task_count=3; task_output='hub-active-handover.jsonl' ;;
  --handover-health) task_run='^TestRealPinnedMeteredHandoverProtocolHealth$'; task_count=3; task_output='hub-handover-health.jsonl' ;;
  *) exit 2 ;;
esac

# Compile before entering a network that has no route to module proxies or DNS.
go test -race -c ./internal/api -o "$RUNNER_TEMP/h7-api.test"
task_ns="dark-pro-h7-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
task_user="$(id -un)"
created=0
cleanup() {
  if test "$created" = 1; then
    # Kill only survivors in this exact namespace, then discard the namespace.
    while read -r task_pid; do
      if test -n "$task_pid"; then sudo kill -KILL "$task_pid" 2>/dev/null || true; fi
    done < <(sudo ip netns pids "$task_ns")
    sudo ip netns del "$task_ns"
  fi
}
trap cleanup EXIT
sudo ip netns add "$task_ns"
created=1
sudo ip -n "$task_ns" link set lo up
# Synthetic local fixture, never a route to the real public address owner.
sudo ip -n "$task_ns" addr add 11.0.0.1/32 dev lo
test -z "$(sudo ip -n "$task_ns" route show table main)"
test -z "$(sudo ip -n "$task_ns" -6 route show default)"
test "$(sudo ip -n "$task_ns" -o link show | wc -l)" -eq 1

sudo ip netns exec "$task_ns" runuser -u "$task_user" -- env \
  DARK_PRO_TEST_FINAL_TCP=1 DARK_PRO_TEST_HUB_TRAFFIC=1 \
  DARK_PRO_TEST_XRAY_BINARY="$DARK_PRO_TEST_XRAY_BINARY" \
  DARK_PRO_TEST_XRAY_SHA256="$DARK_PRO_TEST_XRAY_SHA256" \
  "$RUNNER_TEMP/h7-api.test" -test.run="$task_run" \
  -test.count="$task_count" -test.timeout=360s -test.v 2>&1 \
  | go tool test2json -t -p github.com/darktunnelmika/dark-xray-pro/internal/api \
  > "$RUNNER_TEMP/$task_output"
