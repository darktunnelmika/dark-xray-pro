#!/usr/bin/env python3
"""Read-only successor to all-lifetime native counting; never rewrites raw results.

Requires new exact window markers and a captured final HTTP exchange. Historical
runs without those witnesses remain unaccepted by this verifier.
"""
import argparse
import base64
import hashlib
import json
import re
from pathlib import Path


class EvidenceError(ValueError):
    pass


def require(ok, reason):
    if not ok:
        raise EvidenceError(reason)


def one(log, marker):
    lines = [line.split(marker + ' ', 1)[1] for line in log.splitlines() if marker + ' ' in line]
    require(len(lines) == 1, 'missing or repeated ' + marker)
    return lines[0]


def fields(line):
    return dict(re.findall(r'(\w+)=([^ ]+)', line))


def decode(encoded):
    return base64.b64decode(encoded, validate=True)


def validate(result, log, rows, seconds=3600, max_gap_ms=1000):
    require(type(seconds) is int and 5 <= seconds <= 3600, 'invalid required duration')
    require(result.get('returncode') == 0 and result.get('sync_errors') == 0, 'test exit or sync errors')
    require('--- PASS: TestManagedInboundFixedVersionLiveSoak ' in log and '\nPASS\n' in log,
            'missing internal test PASS')
    require(not re.search(r'--- FAIL:|panic:|context_error=(?!<nil>)', log), 'test/request error')
    begin = fields(one(log, 'MANAGED_FIXED_VERSION_SOAK_BEGIN'))
    end = fields(one(log, 'MANAGED_FIXED_VERSION_SOAK_WINDOW_COMPLETE'))
    passed = fields(one(log, 'MANAGED_FIXED_VERSION_SOAK_PASS'))
    evidence = json.loads(one(log, 'MANAGED_FIXED_VERSION_SOAK_FINAL_EVIDENCE'))
    start, finish = int(begin['at_unix_ns']), int(end['at_unix_ns'])
    elapsed = int(end['elapsed_ms'])
    require(int(begin['seconds']) == seconds and int(passed['seconds']) == seconds,
            'wrong requested duration')
    require(elapsed >= seconds * 1000 and finish > start and abs((finish-start)/1e6-elapsed) < 1000,
            'incomplete or inconsistent traffic window')
    want = int(end['committed_bytes'])
    require(want > 0 and want == int(passed['committed_bytes']) == result['committed_bytes'] == evidence['committed_bytes'],
            'committed byte totals differ')
    require(result['window_ms'] == elapsed and int(end['samples']) == result['traffic_samples'] > 0,
            'runner/window disagreement')
    require(end['unexpected_epochs'] == '0' and passed['holds'] == '0' and passed['planned_final'] == 'true'
            and evidence['holds'] == 0, 'unsettled final accounting')
    require(evidence['version'] == 1 and finish <= evidence['captured_unix_ns'] <= result['finished_unix'] * 1e9,
            'final receipt outside settlement interval')
    wire = decode(evidence['report_wire_b64'])
    canonical = decode(evidence['report_canonical_b64'])
    report, receipt = json.loads(wire), json.loads(decode(evidence['receipt_wire_b64']))
    require(json.loads(canonical) == report, 'canonical report differs from actual HTTP request')
    require(report['final'] is True and receipt['final'] is True and report['sequence'] > 0 and report['final_nonce'],
            'not a final receipt')
    require(receipt['epoch_id'] == report['epoch_id'] and receipt['sequence'] == report['sequence']
            and receipt['sha256'] == hashlib.sha256(canonical).hexdigest(), 'receipt/report mismatch')
    proof = {'process': report['process_identity'], 'nonce': report['final_nonce'], 'counters': report['counters']}
    # Match encoding/json's HTML escaping (fixture identities are ASCII).
    proof_raw = json.dumps(proof, ensure_ascii=False, separators=(',', ':')).replace('&', '\\u0026').replace('<', '\\u003c').replace('>', '\\u003e').replace('\u2028', '\\u2028').replace('\u2029', '\\u2029').encode()
    require(report['final_proof_sha'] == hashlib.sha256(proof_raw).hexdigest(), 'final seal mismatch')
    total, principals = 0, set()
    for counter in report['counters']:
        require(counter['principal_id'] and counter['principal_id'] not in principals, 'duplicate final principal')
        principals.add(counter['principal_id'])
        for key in ('upload', 'download'):
            require(isinstance(counter[key], str) and counter[key].isdigit(), 'invalid final counter')
            total += int(counter[key])
    require(total == want and 0 < total <= 2**63-1, 'final counters differ from committed bytes')
    identity = hashlib.sha256((report['epoch_id'] + '/' + report['process_identity']).encode()).hexdigest()
    require(begin['identity_sha'] == identity and int(begin['generation']) == report['generation'], 'final incarnation differs')
    for line in log.splitlines():
        if 'MANAGED_FIXED_VERSION_SOAK_SAMPLE ' in line:
            sample = fields(line)
            require(sample['identity_sha'] == identity and int(sample['generation']) == report['generation'], 'traffic incarnation changed')
    members = json.loads(report['process_identity'])
    require(len(members) == 1, 'expected one native identity')
    member = json.loads(next(iter(members.values())))
    expected = (member['pid'], str(member['start_ticks']))
    cores = [path for path, sha in result['verified_artifact_sha256'].items() if sha == member['binary_sha']]
    require(len(cores) == 1, 'unverified final native binary')
    core = cores[0]
    require(rows and rows[0]['unix'] * 1e9 <= start and rows[-1]['unix'] * 1e9 >= finish, 'sampler does not cover window')
    times = [round(row['unix'] * 1e9) for row in rows]
    require(all(b > a for a, b in zip(times, times[1:])), 'non-monotonic process samples')
    inside = [row for row, at in zip(rows, times) if start <= at <= finish]
    require(inside, 'no independent traffic samples')
    window_times = [start] + [round(row['unix'] * 1e9) for row in inside] + [finish]
    require(max(b-a for a, b in zip(window_times, window_times[1:])) <= max_gap_ms * 1e6, 'traffic sampler gap')
    all_native = {(p['pid'], str(p['start_ticks'])) for row in rows for p in row['processes'] if p['executable'] == core}
    for row in inside:
        native = [(p['pid'], str(p['start_ticks'])) for p in row['processes'] if p['executable'] == core]
        require(native == [expected], 'missing, additional or restarted native process in traffic window')
    failures = result.get('failures', [])
    require(all(f.get('reason') == 'native_process_identity_count' and f.get('count') == len(all_native) for f in failures),
            'unresolved raw runner failure')
    require(result.get('passed') is True or failures, 'unexplained raw runner failure')
    require(not result.get('remaining_processes'), 'fixture processes remain')
    return {'verified': True, 'traffic_native_identity': list(expected), 'traffic_process_samples': len(inside),
            'all_lifetime_native_identities': [list(p) for p in sorted(all_native)],
            'window_ms': elapsed, 'committed_bytes': want, 'final_receipt': receipt,
            'scope': 'disposable fixture only; raw runner outcome retained separately'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    parser.add_argument('--seconds', type=int, default=3600)
    args = parser.parse_args()
    result = json.loads((args.directory / 'result.json').read_text())
    output = {'raw_runner_result': result}
    try:
        output['traffic_validation'] = validate(result, (args.directory / 'soak.log').read_text(),
            [json.loads(line) for line in (args.directory / 'processes.jsonl').read_text().splitlines()], args.seconds)
    except (EvidenceError, ValueError, KeyError, TypeError, OSError) as error:
        output['traffic_validation'] = {'verified': False, 'reason': str(error)}
    print(json.dumps(output, indent=2))
    return 0 if output['traffic_validation']['verified'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
