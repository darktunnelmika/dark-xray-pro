#!/usr/bin/env bash
# Real per-peer IPv6 source tests in disposable net/mount/pid namespaces only.
# The worker is userspace AWG; this fixture needs no host TUN device or daemon.
set -euo pipefail

task_script=$(realpath -- "${BASH_SOURCE[0]}")
if [[ ${1:-} == --inside ]]; then
  shift
  [[ $# == 2 && $(id -u) == 0 ]] || { echo 'invalid isolated AWG invocation' >&2; exit 2; }
  task_test=$1 task_source=$2
  [[ -n ${DXP_AWG_OUTER_NETNS:-} && -n ${DXP_AWG_OUTER_MNTNS:-} ]] || exit 2
  [[ $(readlink /proc/self/ns/net) != "$DXP_AWG_OUTER_NETNS" && $(readlink /proc/self/ns/mnt) != "$DXP_AWG_OUTER_MNTNS" ]] || exit 2
  mount --make-rprivate /
  ip link set dev lo up
  cd -- "$task_source"
  export DXP_AMNEZIAWG_IPV6_NAMESPACE=1
  exec "$task_test" -test.run='^TestManagedAmneziaWGPublicIPv6NamespaceTrafficAndFinal$' -test.v -test.timeout=120s
fi

[[ $# == 1 ]] || { echo 'usage: test-amneziawg-ipv6.sh GENERATED_SANAEI_SOURCE' >&2; exit 2; }
task_source=$(realpath -- "$1")
[[ -f $task_source/darkpro-sanaei-provenance.json && -f $task_source/cmd/darkpro-amneziawg/ipv6_namespace_linux_test.go ]] || { echo 'prepared standalone source required' >&2; exit 2; }
task_go=${DXP_TEST_GO:-go}
task_directory=$(mktemp -d "${TMPDIR:-/tmp}/darkpro-awg-ipv6-acceptance.XXXXXXXX")
trap 'rm -rf -- "$task_directory"' EXIT
cd -- "$task_source"
"$task_go" test -race -c -o "$task_directory/amneziawg-ipv6.test" ./cmd/darkpro-amneziawg
if [[ -n ${DARK_PRO_AWG_IPV6_CLIENT_OUTPUT:-} ]]; then
  [[ $DARK_PRO_AWG_IPV6_CLIENT_OUTPUT == /* ]] || { echo 'client helper output must be absolute' >&2; exit 2; }
  install -D -m 0755 -- "$task_directory/amneziawg-ipv6.test" "$DARK_PRO_AWG_IPV6_CLIENT_OUTPUT"
fi
task_outer_net=$(readlink /proc/self/ns/net)
task_outer_mount=$(readlink /proc/self/ns/mnt)
task_admin=()
if [[ $(id -u) != 0 ]]; then task_admin=(sudo --non-interactive); fi
"${task_admin[@]}" env DXP_AWG_OUTER_NETNS="$task_outer_net" DXP_AWG_OUTER_MNTNS="$task_outer_mount" \
  unshare --net --mount --pid --fork --kill-child --mount-proc --propagation private \
  bash "$task_script" --inside "$task_directory/amneziawg-ipv6.test" "$task_source"
