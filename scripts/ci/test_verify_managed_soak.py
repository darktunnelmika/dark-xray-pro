import base64
import copy
import hashlib
import json
import unittest

from verify_managed_soak import validate, EvidenceError


def raw(obj):
    return json.dumps(obj, separators=(',', ':')).encode()


def fixture():
    member = {'pid': 42, 'start_ticks': 800, 'binary_sha': 'a'*64}
    process = raw({'native': raw(member).decode()}).decode()
    report = {'epoch_id': 'fixture-epoch', 'node_id': 'de', 'installation_id': 'b'*32,
              'generation': 1, 'sequence': 8, 'final': True, 'process_identity': process,
              'final_nonce': 'c'*64, 'counters': [{'principal_id': 'fixture', 'upload': '40', 'download': '60'}]}
    proof = {'process': process, 'nonce': report['final_nonce'], 'counters': report['counters']}
    report['final_proof_sha'] = hashlib.sha256(raw(proof)).hexdigest()
    receipt = {'epoch_id': report['epoch_id'], 'sequence': 8, 'final': True,
               'sha256': hashlib.sha256(raw(report)).hexdigest(), 'replayed': False}
    enc = lambda b: base64.b64encode(b).decode()
    ev = {'version': 1, 'captured_unix_ns': 105100000000, 'report_wire_b64': enc(raw(report)+b'\n'),
          'receipt_wire_b64': enc(raw(receipt)+b'\n'), 'report_canonical_b64': enc(raw(report)),
          'committed_bytes': 100, 'holds': 0}
    identity = hashlib.sha256(('fixture-epoch/'+process).encode()).hexdigest()
    log = (f'MANAGED_FIXED_VERSION_SOAK_BEGIN seconds=5 identity_sha={identity} generation=1 at_unix_ns=100000000000\n'
           f'MANAGED_FIXED_VERSION_SOAK_SAMPLE elapsed_ms=1 identity_sha={identity} generation=1\n'
           'MANAGED_FIXED_VERSION_SOAK_WINDOW_COMPLETE elapsed_ms=5000 samples=3 committed_bytes=100 unexpected_epochs=0 at_unix_ns=105000000000\n'
           'MANAGED_FIXED_VERSION_SOAK_FINAL_EVIDENCE '+json.dumps(ev)+'\n'
           'MANAGED_FIXED_VERSION_SOAK_PASS seconds=5 committed_bytes=100 holds=0 planned_final=true\n'
           '--- PASS: TestManagedInboundFixedVersionLiveSoak (6.00s)\nPASS\n')
    native = {'pid': 42, 'start_ticks': '800', 'executable': '/fixture/core'}
    test = {'pid': 41, 'start_ticks': '700', 'executable': '/fixture/test'}
    rows = [{'unix': 99.5, 'processes': [dict(native, pid=39, start_ticks='600'), test]}]
    rows += [{'unix': 100+i*.5, 'processes': [native.copy(), test.copy()]} for i in range(11)]
    rows += [{'unix': 105.5, 'processes': [test]}]
    result = {'passed': False, 'returncode': 0, 'sync_errors': 0, 'window_ms': 5000, 'traffic_samples': 3,
              'committed_bytes': 100, 'finished_unix': 106,
              'failures': [{'reason': 'native_process_identity_count', 'count': 2}],
              'verified_artifact_sha256': {'/fixture/core': 'a'*64, '/fixture/test': 'b'*64}, 'remaining_processes': []}
    return result, log, rows


class VerifyManagedSoakTests(unittest.TestCase):
    def test_offline_validator_is_separate_and_raw_failure_not_mutated(self):
        r, log, rows = fixture()
        before = copy.deepcopy(r)
        answer = validate(r, log, rows, 5)
        self.assertTrue(answer['verified'])
        self.assertEqual(answer['traffic_native_identity'], [42, '800'])
        self.assertEqual(len(answer['all_lifetime_native_identities']), 2)
        self.assertEqual(r, before)
        self.assertFalse(r['passed'])

    def test_failures_are_not_promoted(self):
        cases = ['missing_receipt', 'old_timestamps', 'short_window', 'wrong_bytes', 'wrong_epoch',
                 'wrong_generation', 'holds', 'no_pass', 'context_error', 'native_restart',
                 'pid_reuse', 'missing_core', 'additional_core', 'sampler_gap', 'no_coverage',
                 'backward_clock', 'other_raw_failure', 'wrong_raw_count', 'sync_error', 'exit_error',
                 'remaining_process', 'wrong_receipt', 'duplicate_end', 'wrong_pin', 'late_receipt']
        for case in cases:
            with self.subTest(case=case):
                r, log, rows = fixture()
                if case == 'missing_receipt': log = '\n'.join(l for l in log.splitlines() if 'FINAL_EVIDENCE' not in l)
                if case == 'old_timestamps': log = log.replace('at_unix_ns=', 'old_time=')
                if case == 'short_window': log = log.replace('elapsed_ms=5000', 'elapsed_ms=4999')
                if case == 'wrong_bytes': r['committed_bytes'] += 1
                if case == 'wrong_epoch': log = log.replace('unexpected_epochs=0', 'unexpected_epochs=1')
                if case == 'wrong_generation': log = log.replace('generation=1', 'generation=2')
                if case == 'holds': log = log.replace('holds=0', 'holds=1')
                if case == 'no_pass': log = log.replace('\nPASS\n', '\nFAIL\n')
                if case == 'context_error': log += 'context_error=deadline\n'
                if case == 'native_restart': rows[5]['processes'][0]['pid'] = 43
                if case == 'pid_reuse': rows[5]['processes'][0]['start_ticks'] = '801'
                if case == 'missing_core': rows[5]['processes'] = []
                if case == 'additional_core': rows[5]['processes'].append(dict(rows[5]['processes'][0], pid=43))
                if case == 'sampler_gap': del rows[3:6]
                if case == 'no_coverage': rows = rows[2:]
                if case == 'backward_clock': rows[3]['unix'] = rows[2]['unix'] - .1
                if case == 'other_raw_failure': r['failures'].append({'reason': 'service_identity_changed'})
                if case == 'wrong_raw_count': r['failures'][0]['count'] = 7
                if case == 'sync_error': r['sync_errors'] = 1
                if case == 'exit_error': r['returncode'] = 1
                if case == 'remaining_process': r['remaining_processes'] = [42]
                if case == 'wrong_receipt': log = log.replace('receipt_wire_b64', 'missing_receipt_wire_b64')
                if case == 'duplicate_end': log += next(l for l in log.splitlines() if 'WINDOW_COMPLETE' in l)+'\n'
                if case == 'wrong_pin': r['verified_artifact_sha256']['/fixture/core'] = 'd'*64
                if case == 'late_receipt': r['finished_unix'] = 105
                with self.assertRaises((EvidenceError, KeyError, ValueError)):
                    validate(r, log, rows, 5)


if __name__ == '__main__':
    unittest.main()
