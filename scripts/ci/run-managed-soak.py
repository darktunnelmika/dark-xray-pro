#!/usr/bin/env python3
"""Observe disposable loopback acceptance; never attach filesystems or open LAB data."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time


def go_digest(value):
    raw = json.dumps(value, separators=(',', ':'), ensure_ascii=False)
    for char, escaped in (('&', '\\u0026'), ('<', '\\u003c'), ('>', '\\u003e'), ('\u2028', '\\u2028'), ('\u2029', '\\u2029')):
        raw = raw.replace(char, escaped)
    return hashlib.sha256(raw.encode()).hexdigest()


def markers(log, name):
    prefix = 'MANAGED_SOAK_' + name + ' '
    return [json.loads(line.split(prefix, 1)[1]) for line in log.splitlines() if prefix in line]


def validate_process_window(begin, end, samples, core, seconds):
    """Bind samples to the admitted incarnation, retaining all setup samples."""
    if any(begin[k] != end[k] for k in ('epoch_id', 'generation', 'process_identity')):
        raise ValueError('window_identity_changed')
    if not begin['epoch_id'] or begin['generation'] <= 0:
        raise ValueError('missing_epoch')
    if end['unix_ns'] - begin['unix_ns'] < seconds * 10**9:
        raise ValueError('short_traffic_window')
    members = json.loads(begin['process_identity'])
    if len(members) != 1:
        raise ValueError('expected_one_native_member')
    member = json.loads(next(iter(members.values())))
    expected = (member['pid'], str(member['start_ticks']), core)
    selected = [row for row in samples if begin['unix_ns'] <= row['unix_ns'] <= end['unix_ns']]
    if not selected:
        raise ValueError('missing_window_samples')
    previous = begin['unix_ns']
    maximum_gap = 0
    for row in selected:
        gap = row['unix_ns'] - previous
        if gap < 0:
            raise ValueError('sample_clock_reversed')
        maximum_gap = max(maximum_gap, gap)
        previous = row['unix_ns']
        identities = [(p['pid'], str(p['start_ticks']), p['executable'])
                      for p in row['processes'] if p['executable'] == core]
        if identities != [expected]:
            raise ValueError('active_native_identity_changed_or_missing')
    maximum_gap = max(maximum_gap, end['unix_ns'] - previous)
    if maximum_gap > 2 * 10**9:
        raise ValueError('sampling_gap_over_two_seconds')
    return {'pid': member['pid'], 'start_ticks': str(member['start_ticks']),
            'window_samples': len(selected), 'maximum_gap_ms': maximum_gap / 10**6,
            'preparation_samples': sum(row['unix_ns'] < begin['unix_ns'] for row in samples)}


def validate_final(evidence, begin, committed):
    report, receipt = evidence['report'], evidence['receipt']
    if evidence['http_status'] != 200 or report['final'] is not True or receipt['final'] is not True:
        raise ValueError('missing_accepted_final')
    if report['epoch_id'] != begin['epoch_id'] or report['process_identity'] != begin['process_identity'] or report['generation'] != begin['generation']:
        raise ValueError('final_identity_changed')
    if receipt['epoch_id'] != report['epoch_id'] or receipt['sequence'] != report['sequence'] or report['sequence'] <= 0:
        raise ValueError('final_receipt_mismatch')
    # Go emits these fixture JSON objects in struct field order. Preserve the
    # received order; no sorting, counter rewriting or expected receipt synthesis.
    if receipt['sha256'] != go_digest(report):
        raise ValueError('receipt_digest_mismatch')
    proof = {'process': report['process_identity'], 'nonce': report['final_nonce'], 'counters': report['counters']}
    if not re.fullmatch('[0-9a-f]{64}', report['final_nonce']) or report['final_proof_sha'] != go_digest(proof):
        raise ValueError('native_final_proof_mismatch')
    counters = [c for c in report['counters'] if c['principal_id'] == evidence['principal_id']]
    if len(counters) != 1 or any(int(counters[0][k]) < 0 for k in ('upload', 'download')):
        raise ValueError('invalid_customer_counter')
    if sum(int(counters[0][k]) for k in ('upload', 'download')) != committed or evidence['committed_bytes'] != committed:
        raise ValueError('final_counter_mismatch')


def processes(core):
    found = []
    for entry in Path('/proc').iterdir():
        if not entry.name.isdigit():
            continue
        try:
            exe = os.readlink(entry / 'exe')
            if exe != core:
                continue
            stat = (entry / 'stat').read_text()
            fields = stat[stat.rfind(')') + 1:].split()
            found.append({'pid': int(entry.name), 'start_ticks': fields[19], 'executable': exe})
        except (OSError, ProcessLookupError):
            continue
    return found


def run(args):
    core, test = str(Path(args.core).resolve(strict=True)), str(Path(args.test).resolve(strict=True))
    # Refuse shared temporary roots, symlink aliases and long Unix socket paths.
    scratch = Path(args.tmpdir)
    if not scratch.is_absolute() or scratch.resolve() != scratch or len(os.fsencode(scratch)) + 51 > 100:
        raise ValueError('short_real_absolute_tmpdir_required')
    scratch.mkdir(mode=0o700)
    output = Path(args.output)
    if not output.is_absolute() or output.resolve() != output:
        raise ValueError('real_absolute_output_required')
    output.mkdir(mode=0o700)
    if processes(core):
        raise ValueError('another_fixture_uses_this_core')
    if not 5 <= args.seconds <= 3600:
        raise ValueError('seconds_must_be_5_to_3600')
    hashes = {p: hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in (test, core)}
    env = dict(os.environ, TMPDIR=str(scratch), DARK_PRO_TEST_INBOUND_CORE=core,
               DARK_PRO_TEST_OBSERVE_SOAK_SECONDS=str(args.seconds))
    start = time.time_ns()
    baseline = {'started_unix_ns': start, 'seconds': args.seconds, 'sha256': hashes,
                'scope': 'disposable_loopback_fixture', 'tmpdir': str(scratch)}
    (output / 'start.json').write_text(json.dumps(baseline, indent=2))
    timed_out = False
    deadline = time.monotonic() + args.seconds + 100
    with (output / 'soak.log').open('x') as log, (output / 'processes.jsonl').open('x', buffering=1) as samples:
        proc = subprocess.Popen([test, '-test.run=^TestManagedInboundFixedVersionLiveSoak$', '-test.v',
                                 '-test.count=1', '-test.timeout=' + str(args.seconds + 90) + 's'],
                                env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        while proc.poll() is None:
            samples.write(json.dumps({'unix_ns': time.time_ns(), 'processes': processes(core)}) + '\n')
            if time.monotonic() > deadline:
                timed_out = True
                os.killpg(proc.pid, signal.SIGKILL)
                break
            time.sleep(.25)
        proc.wait(timeout=10)
    result = dict(baseline, returncode=proc.returncode, finished_unix_ns=time.time_ns(), passed=False, failures=[])
    try:
        if proc.returncode or timed_out:
            raise ValueError('test_failed_or_timed_out')
        log = (output / 'soak.log').read_text()
        begin, end, finals = [markers(log, name) for name in ('PROCESS_BEGIN', 'PROCESS_END', 'FINAL_EVIDENCE')]
        if len(begin) != 1 or len(end) != 1 or len(finals) != 1:
            raise ValueError('missing_or_duplicate_wire_evidence')
        # Preserve evidence independently even if a later validation fails.
        (output / 'final-receipt.json').write_text(json.dumps(finals[0], indent=2) + '\n')
        (output / 'traffic-window.json').write_text(json.dumps({'begin': begin[0], 'end': end[0]}, indent=2))
        rows = [json.loads(line) for line in (output / 'processes.jsonl').read_text().splitlines()]
        result['process_window'] = validate_process_window(begin[0], end[0], rows, core, args.seconds)
        complete = re.findall(r'MANAGED_FIXED_VERSION_SOAK_WINDOW_COMPLETE elapsed_ms=(\d+) samples=(\d+) committed_bytes=(\d+) unexpected_epochs=0', log)
        passed = re.findall(r'MANAGED_FIXED_VERSION_SOAK_PASS seconds=(\d+) committed_bytes=(\d+) holds=0 planned_final=true', log)
        if len(complete) != 1 or len(passed) != 1 or int(complete[0][0]) < args.seconds * 1000 or int(passed[0][0]) != args.seconds or complete[0][2] != passed[0][1]:
            raise ValueError('incomplete_traffic_or_settlement')
        committed = int(complete[0][2])
        validate_final(finals[0], begin[0], committed)
        if processes(core):
            raise ValueError('fixture_process_remaining')
        result.update(passed=True, committed_bytes=committed, holds=0,
                      final_receipt_sha256=hashlib.sha256((output / 'final-receipt.json').read_bytes()).hexdigest())
    except (ValueError, KeyError, TypeError, IndexError) as error:
        result['failures'].append(str(error))
    with (output / 'result.json').open('x') as f:
        f.write(json.dumps(result, indent=2) + '\n')
        f.flush()
        os.fsync(f.fileno())
    print(json.dumps(result), flush=True)
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--test', required=True)
    parser.add_argument('--core', required=True)
    parser.add_argument('--seconds', type=int, required=True)
    parser.add_argument('--tmpdir', required=True)
    parser.add_argument('--output', required=True)
    raise SystemExit(run(parser.parse_args()))
