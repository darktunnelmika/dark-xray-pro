#!/usr/bin/env python3
"""Collect a NEW disposable fixture run. No service, mount or installed DB access.

Keep the legacy all-lifetime result in result.json; write the independently
verified traffic outcome to verification.json. Never reuse an evidence directory.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time

from verify_managed_soak import validate


def sample(executables):
    found = []
    for p in Path('/proc').iterdir():
        if not p.name.isdigit():
            continue
        try:
            exe = str((p / 'exe').resolve(strict=True))
            if exe not in executables:
                continue
            stat = (p / 'stat').read_text().rsplit(')', 1)[1].split()
            found.append({'pid': int(p.name), 'start_ticks': stat[19], 'executable': exe,
                          'argv': (p / 'cmdline').read_bytes().decode(errors='replace').rstrip('\0').split('\0')})
        except (FileNotFoundError, ProcessLookupError, PermissionError):
            continue
    return sorted(found, key=lambda p: p['pid'])


def collect(args):
    if not 5 <= args.seconds <= 3600:
        raise ValueError('duration must be 5..3600 seconds')
    pins = {}
    for path, digest in ((args.test, args.test_sha256), (args.core, args.core_sha256)):
        resolved = path.resolve(strict=True)
        if not re.fullmatch('[0-9a-f]{64}', digest) or hashlib.sha256(resolved.read_bytes()).hexdigest() != digest:
            raise ValueError('artifact hash mismatch: ' + str(path))
        pins[str(resolved)] = digest
    if len(pins) != 2:
        raise ValueError('test and core must be distinct')
    data = args.financial_tmpdir.resolve(strict=True)
    if data != args.financial_tmpdir.absolute() or not data.is_dir() or len(os.fsencode(data)) + 51 > 100:
        raise ValueError('financial TMPDIR must be a short real directory')
    if data.stat().st_uid != os.getuid() or data.stat().st_mode & 0o077:
        raise ValueError('financial TMPDIR must be private and owned by the runner')
    fs = json.loads(subprocess.check_output(['findmnt', '-J', '-T', str(data)], text=True))['filesystems'][0]
    if fs['fstype'] in ('tmpfs', 'ramfs'):
        raise ValueError('financial fixture cannot use memory-only storage')
    if sample(pins):
        raise ValueError('fixture executables already running')
    # A new directory is the overwrite guard, including for failed attempts.
    args.evidence_dir.mkdir(mode=0o700, parents=False, exist_ok=False)
    output = args.evidence_dir
    started = time.time()
    env = dict(os.environ, TMPDIR=str(data), DARK_PRO_TEST_INBOUND_CORE=str(args.core.resolve()),
               DARK_PRO_TEST_OBSERVE_SOAK_SECONDS=str(args.seconds))
    command = ['strace', '-ff', '-ttt', '-T', '-yy', '-e', 'trace=fsync,fdatasync',
               '-e', 'signal=none', '-o', str(output / 'sync.trace'), str(args.test.resolve()),
               '-test.run=^TestManagedInboundFixedVersionLiveSoak$', '-test.v',
               '-test.count=1', '-test.timeout=' + str(args.seconds + 100) + 's']
    rows, failures = [], []
    with (output / 'soak.log').open('x') as log, (output / 'strace.stderr').open('x') as err, (output / 'processes.jsonl').open('x') as samples:
        proc = subprocess.Popen(command, stdout=log, stderr=err, env=env, start_new_session=True)
        deadline = time.monotonic() + args.seconds + 150
        while True:
            row = {'unix': time.time(), 'mono_ns': time.monotonic_ns(), 'processes': sample(pins)}
            rows.append(row)
            samples.write(json.dumps(row) + '\n')
            samples.flush()
            if proc.poll() is not None:
                break
            if time.monotonic() >= deadline:
                failures.append({'reason': 'hard_test_deadline'})
                os.killpg(proc.pid, signal.SIGKILL)
                break
            time.sleep(.5)
        returncode = proc.wait(timeout=15)
    finished = time.time()
    identities = sorted({(p['pid'], p['start_ticks'], p['executable']) for row in rows for p in row['processes']})
    natives = [p for p in identities if p[2] == str(args.core.resolve())]
    if len(natives) != 1:
        failures.append({'reason': 'native_process_identity_count', 'count': len(natives)})
    if returncode != 0:
        failures.append({'reason': 'test_exit_nonzero', 'returncode': returncode})
    durations, sync_errors = [], 0
    for path in output.glob('sync.trace.*'):
        for line in path.read_text().splitlines():
            m = re.search(r'(?:f|fdata)sync\(.*\)\s+=\s+(-?\d+).*<([\d.]+)>', line)
            if m:
                durations.append(float(m[2]))
                sync_errors += m[1] != '0'
    log = (output / 'soak.log').read_text()
    window = re.search(r'SOAK_WINDOW_COMPLETE elapsed_ms=(\d+) samples=(\d+) committed_bytes=(\d+) unexpected_epochs=0', log)
    final = re.search(r'SOAK_PASS seconds=(\d+) committed_bytes=(\d+) holds=0 planned_final=true', log)
    if not window or int(window[1]) < args.seconds * 1000:
        failures.append({'reason': 'traffic_window_incomplete'})
    if not final or int(final[1]) != args.seconds or not window or final[2] != window[3]:
        failures.append({'reason': 'final_settlement_incomplete'})
    if not durations:
        failures.append({'reason': 'sync_trace_missing'})
    remaining = sample(pins)
    if remaining:
        failures.append({'reason': 'fixture_processes_remaining'})
    result = {'passed': not failures and not sync_errors and window is not None,
              'returncode': returncode, 'failures': failures, 'started_unix': started, 'finished_unix': finished,
              'verified_artifact_sha256': pins, 'financial_tmpdir': str(data), 'filesystem': fs,
              'process_identities': identities, 'process_samples': len(rows), 'remaining_processes': remaining,
              'sync_calls': len(durations), 'sync_errors': sync_errors, 'sync_max_seconds': max(durations, default=0)}
    if window:
        result.update(window_ms=int(window[1]), traffic_samples=int(window[2]), committed_bytes=int(window[3]))
    with (output / 'result.json').open('x') as target:
        json.dump(result, target, indent=2)
    try:
        verification = validate(result, log, rows, args.seconds)
    except (ValueError, KeyError, TypeError) as error:
        verification = {'verified': False, 'reason': str(error)}
    with (output / 'verification.json').open('x') as target:
        json.dump({'raw_runner_result': result, 'traffic_validation': verification}, target, indent=2)
    print(json.dumps({'raw_runner_passed': result['passed'], 'traffic_validation': verification}))
    return 0 if verification['verified'] else 1


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--test', type=Path, required=True)
    p.add_argument('--core', type=Path, required=True)
    p.add_argument('--test-sha256', required=True)
    p.add_argument('--core-sha256', required=True)
    p.add_argument('--financial-tmpdir', type=Path, required=True)
    p.add_argument('--evidence-dir', type=Path, required=True)
    p.add_argument('--seconds', type=int, default=3600)
    return collect(p.parse_args())


if __name__ == '__main__':
    raise SystemExit(main())
