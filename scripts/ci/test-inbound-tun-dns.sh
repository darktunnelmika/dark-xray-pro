#!/usr/bin/env bash
# Real kernel TUN + real systemd-resolved + a private D-Bus, entirely inside
# disposable network/mount/PID namespaces. No host resolver or service is used.
set -euo pipefail

task_script=$(realpath -- "${BASH_SOURCE[0]}")
if [[ ${1:-} == --inside ]]; then
  shift
  [[ $# == 6 && $(id -u) == 0 ]] || { echo 'invalid isolated DNS invocation' >&2; exit 2; }
  task_test=$1 task_binary=$2 task_pin=$3 task_workdir=$4 task_directory=$5 task_tools=$6
  [[ $(readlink /proc/self/ns/net) != "$DXP_TUN_OUTER_NETNS" && $(readlink /proc/self/ns/mnt) != "$DXP_TUN_OUTER_MNTNS" ]] || exit 2
  mount --make-rprivate /
  # Populate a sparse, owned /etc before hiding host configuration. The test
  # does not read host resolver settings, D-Bus policy, network units or hosts.
  mkdir -p "$task_directory/etc/systemd/resolved.conf.d"
  cp /etc/passwd /etc/group /etc/nsswitch.conf "$task_directory/etc/"
  python3 - "$task_directory/etc" <<'PY'
import pathlib,sys
root=pathlib.Path(sys.argv[1]);passwd=(root/'passwd').read_text();group=(root/'group').read_text()
if not any(line.startswith('systemd-resolve:') for line in passwd.splitlines()):
    used={int(line.split(':')[2]) for text in (passwd,group) for line in text.splitlines() if len(line.split(':'))>2 and line.split(':')[2].isdigit()}
    uid=next(value for value in range(65000,65534) if value not in used)
    with (root/'passwd').open('a') as f: f.write(f'systemd-resolve:x:{uid}:{uid}:Owned test resolver:/run/systemd/resolve:/usr/sbin/nologin\n')
    with (root/'group').open('a') as f: f.write(f'systemd-resolve:x:{uid}:\n')
PY
  printf '127.0.0.1 localhost\n::1 localhost\n' > "$task_directory/etc/hosts"
  ln -s /run/systemd/resolve/stub-resolv.conf "$task_directory/etc/resolv.conf"
  dbus-uuidgen > "$task_directory/etc/machine-id"
  cat > "$task_directory/etc/systemd/resolved.conf" <<'CONF'
[Resolve]
DNS=
FallbackDNS=
Domains=
LLMNR=no
MulticastDNS=no
DNSSEC=no
DNSOverTLS=no
Cache=no
ReadEtcHosts=no
DNSStubListener=yes
CONF
  cp "$task_directory/etc/systemd/resolved.conf" "$task_directory/etc/systemd/resolved.conf.d/zzzz-darkpro-isolated.conf"
  mount --bind "$task_directory/etc" /etc
  for task_dropins in /usr/lib/systemd/resolved.conf.d /usr/local/lib/systemd/resolved.conf.d; do
    if [[ -d $task_dropins ]]; then mount -t tmpfs -o mode=0755,nosuid tmpfs "$task_dropins"; fi
  done
  mount -t tmpfs -o mode=0755,nosuid tmpfs /run
  mount -t tmpfs -o mode=0755,nosuid tmpfs /dev
  mkdir -p /dev/net /run/dbus /run/systemd
  mknod -m 0666 /dev/null c 1 3
  mknod -m 0666 /dev/zero c 1 5
  mknod -m 0666 /dev/random c 1 8
  mknod -m 0666 /dev/urandom c 1 9
  mknod -m 0600 /dev/net/tun c 10 200
  ip link set dev lo up
  ip address add 198.18.0.53/32 dev lo
  cat > "$task_directory/bus.conf" <<'CONF'
<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN" "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>system</type>
  <listen>unix:path=/run/dbus/system_bus_socket</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow user="*"/>
    <allow own="*"/>
    <allow send_destination="*"/>
    <allow receive_sender="*"/>
  </policy>
</busconfig>
CONF
  task_bus_pid= task_resolved_pid=
  task_cleanup() {
    task_status=$?
    trap - EXIT
    if [[ -n $task_resolved_pid ]]; then kill "$task_resolved_pid" 2>/dev/null || true; wait "$task_resolved_pid" 2>/dev/null || true; fi
    if [[ -n $task_bus_pid ]]; then kill "$task_bus_pid" 2>/dev/null || true; wait "$task_bus_pid" 2>/dev/null || true; fi
    if [[ $task_status != 0 ]]; then
      cat "$task_directory/dbus.log" "$task_directory/resolved.log" 2>/dev/null || true
    fi
    exit "$task_status"
  }
  trap task_cleanup EXIT
  export DBUS_SYSTEM_BUS_ADDRESS=unix:path=/run/dbus/system_bus_socket
  export SYSTEMD_LOG_TARGET=console SYSTEMD_LOG_LEVEL=notice SYSTEMD_PAGER=cat
  dbus-daemon --config-file="$task_directory/bus.conf" --nofork > "$task_directory/dbus.log" 2>&1 &
  task_bus_pid=$!
  for ((task_attempt=0; task_attempt<100; task_attempt++)); do
    kill -0 "$task_bus_pid"
    [[ ! -S /run/dbus/system_bus_socket ]] || break
    sleep 0.05
  done
  [[ -S /run/dbus/system_bus_socket ]]
  "$task_tools/systemd-resolved" > "$task_directory/resolved.log" 2>&1 &
  task_resolved_pid=$!
  for ((task_attempt=0; task_attempt<100; task_attempt++)); do
    kill -0 "$task_bus_pid" && kill -0 "$task_resolved_pid"
    if timeout 1 "$task_tools/resolvectl" status > "$task_directory/resolver-ready.log" 2>&1; then break; fi
    sleep 0.05
  done
  timeout 2 "$task_tools/resolvectl" status
  export DXP_INBOUND_TUN_NAMESPACE=1 DXP_TUN_DNS_NAMESPACE=1
  export DXP_TUN_DNS_TOOLS="$task_tools" DXP_TUN_DNS_RESOLVED_PID="$task_resolved_pid"
  export DXP_INBOUND_CORE_V2_BINARY="$task_binary" DXP_INBOUND_CORE_V2_SHA256="$task_pin"
  export PATH="$task_tools:/usr/sbin:/usr/bin:/sbin:/bin"
  cd -- "$task_workdir"
  "$task_test" -test.run='^TestPinnedNativeTUNDNSNamespaceLifecycle$' -test.v -test.timeout=120s
  exit 0
fi

[[ $# == 2 && $2 =~ ^[0-9a-f]{64}$ ]] || { echo 'usage: test-inbound-tun-dns.sh PINNED_NATIVE_BINARY SHA256' >&2; exit 2; }
task_binary=$(realpath -- "$1") task_pin=$2
task_repo=$(cd -- "$(dirname -- "$task_script")/../.." && pwd)
task_go=${DXP_TEST_GO:-go}
task_directory=$(mktemp -d "${TMPDIR:-/tmp}/darkpro-tun-dns.XXXXXXXX")
trap 'rm -rf -- "$task_directory"' EXIT
mkdir -p "$task_directory/tools"
task_resolved=
for task_candidate in /usr/lib/systemd/systemd-resolved /lib/systemd/systemd-resolved; do
  if [[ -x $task_candidate ]]; then task_resolved=$task_candidate; break; fi
done
if [[ -n $task_resolved ]] && command -v resolvectl >/dev/null; then
  ln -s "$task_resolved" "$task_directory/tools/systemd-resolved"
  ln -s "$(command -v resolvectl)" "$task_directory/tools/resolvectl"
else
  # Download/extract only. Installing the distro package could restart a host
  # resolver or rewrite its resolv.conf, which this acceptance must never do.
  mkdir -p "$task_directory/packages" "$task_directory/sysroot"
  (
    cd "$task_directory/packages"
    apt-get download systemd-resolved libsystemd-shared
    for task_deb in ./*.deb; do dpkg-deb --extract "$task_deb" "$task_directory/sysroot"; done
  )
  python3 - "$task_directory" <<'PY'
import pathlib,shlex,sys
root=pathlib.Path(sys.argv[1]);source=root/'sysroot'
libraries=sorted({str(p.parent) for p in source.rglob('libsystemd-shared-*.so')})
if not libraries: raise SystemExit('extracted systemd shared library missing')
for name in ('resolvectl','systemd-resolved'):
    candidates=[p for p in source.rglob(name) if p.is_file() and not p.is_symlink()]
    if len(candidates)!=1: raise SystemExit('ambiguous extracted systemd tool: '+name)
    script=root/'tools'/name
    script.write_text('#!/bin/sh\nexec env LD_LIBRARY_PATH='+shlex.quote(':'.join(libraries))+' '+shlex.quote(str(candidates[0]))+' "$@"\n')
    script.chmod(0o755)
PY
fi
"$task_directory/tools/resolvectl" --version
cd -- "$task_repo"
"$task_go" test -race -c -o "$task_directory/tun-dns.test" ./internal/corebarrierv2
task_outer_net=$(readlink /proc/self/ns/net) task_outer_mount=$(readlink /proc/self/ns/mnt)
task_admin=()
if [[ $(id -u) != 0 ]]; then task_admin=(sudo --non-interactive); fi
"${task_admin[@]}" env DXP_TUN_OUTER_NETNS="$task_outer_net" DXP_TUN_OUTER_MNTNS="$task_outer_mount" \
  unshare --net --mount --pid --fork --kill-child --mount-proc --propagation private \
  bash "$task_script" --inside "$task_directory/tun-dns.test" "$task_binary" "$task_pin" "$task_repo/internal/corebarrierv2" "$task_directory" "$task_directory/tools"
