#!/usr/bin/env bash
# Runs a real kernel-TUN acceptance test entirely inside disposable namespaces.
# No host route, DNS setting, firewall, interface or service is changed.
set -euo pipefail

task_script=$(realpath -- "${BASH_SOURCE[0]}")
if [[ ${1:-} == --inside ]]; then
  shift
  [[ $# == 4 && $(id -u) == 0 ]] || { echo 'invalid isolated TUN invocation' >&2; exit 2; }
  task_test=$1 task_binary=$2 task_pin=$3 task_workdir=$4
  [[ $(readlink /proc/self/ns/net) != "$DXP_TUN_OUTER_NETNS" && $(readlink /proc/self/ns/mnt) != "$DXP_TUN_OUTER_MNTNS" ]] || exit 2
  mount --make-rprivate /
  # Mount a private device directory. Creating a missing /dev/net in the host
  # filesystem would escape mount isolation, so no host /dev path is modified.
  mount -t tmpfs -o mode=0755,nosuid tmpfs /dev
  mkdir -p /dev/net
  mknod -m 0666 /dev/null c 1 3
  mknod -m 0666 /dev/zero c 1 5
  mknod -m 0666 /dev/random c 1 8
  mknod -m 0666 /dev/urandom c 1 9
  mknod -m 0600 /dev/net/tun c 10 200
  ip link set dev lo up
  cd -- "$task_workdir"
  export DXP_INBOUND_TUN_NAMESPACE=1
  export DXP_INBOUND_CORE_V2_BINARY=$task_binary
  export DXP_INBOUND_CORE_V2_SHA256=$task_pin
  exec "$task_test" -test.run='^TestPinnedNativeTUNNamespaceTrafficAndFinalReceipt$' -test.v -test.timeout=90s
fi

[[ $# == 2 && $2 =~ ^[0-9a-f]{64}$ ]] || { echo 'usage: test-inbound-tun.sh PINNED_NATIVE_BINARY SHA256' >&2; exit 2; }
task_binary=$(realpath -- "$1")
task_pin=$2
task_repo=$(cd -- "$(dirname -- "$task_script")/../.." && pwd)
task_go=${DXP_TEST_GO:-go}
task_directory=$(mktemp -d "${TMPDIR:-/tmp}/darkpro-tun-acceptance.XXXXXXXX")
trap 'rm -rf -- "$task_directory"' EXIT
cd -- "$task_repo"
"$task_go" test -race -c -o "$task_directory/tun.test" ./internal/corebarrierv2
task_outer_net=$(readlink /proc/self/ns/net)
task_outer_mount=$(readlink /proc/self/ns/mnt)
task_admin=()
if [[ $(id -u) != 0 ]]; then task_admin=(sudo --non-interactive); fi
"${task_admin[@]}" env DXP_TUN_OUTER_NETNS="$task_outer_net" DXP_TUN_OUTER_MNTNS="$task_outer_mount" \
  unshare --net --mount --pid --fork --kill-child --mount-proc --propagation private \
  bash "$task_script" --inside "$task_directory/tun.test" "$task_binary" "$task_pin" "$task_repo/internal/corebarrierv2"
