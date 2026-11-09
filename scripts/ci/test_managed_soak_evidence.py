import copy
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('managed_soak', Path(__file__).with_name('run-managed-soak.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class ProcessWindowTests(unittest.TestCase):
    def setUp(self):
        self.core = '/fixture/core'
        member = json.dumps({'pid': 42, 'start_ticks': 100})
        self.begin = {'epoch_id': 'epoch-1', 'generation': 1,
                      'process_identity': json.dumps({'xray': member}), 'unix_ns': 10 * 10**9}
        self.end = dict(self.begin, unix_ns=15 * 10**9)
        self.rows = [{'unix_ns': t * 10**9, 'processes': [
            {'pid': 42, 'start_ticks': '100', 'executable': self.core}]} for t in range(10, 16)]

    def validate(self):
        return runner.validate_process_window(self.begin, self.end, self.rows, self.core, 5)

    def test_short_preparation_process_is_not_a_restart(self):
        self.rows.insert(0, {'unix_ns': 9 * 10**9, 'processes': [
            {'pid': 41, 'start_ticks': '90', 'executable': self.core}]})
        result = self.validate()
        self.assertEqual(result['pid'], 42)
        self.assertEqual(result['preparation_samples'], 1)

    def test_active_restart_is_rejected(self):
        self.rows[3]['processes'][0]['pid'] = 43
        with self.assertRaisesRegex(ValueError, 'identity_changed'):
            self.validate()

    def test_pid_reuse_is_rejected(self):
        self.rows[3]['processes'][0]['start_ticks'] = '101'
        with self.assertRaises(ValueError): self.validate()

    def test_missing_process_is_rejected(self):
        self.rows[3]['processes'] = []
        with self.assertRaises(ValueError): self.validate()

    def test_unexpected_extra_process_is_rejected(self):
        self.rows[3]['processes'].append({'pid': 43, 'start_ticks': '90', 'executable': self.core})
        with self.assertRaises(ValueError): self.validate()

    def test_gap_is_rejected(self):
        del self.rows[1:4]
        with self.assertRaisesRegex(ValueError, 'sampling_gap'): self.validate()

    def test_missing_start_coverage_is_rejected(self):
        del self.rows[:3]
        with self.assertRaises(ValueError): self.validate()

    def test_missing_end_coverage_is_rejected(self):
        self.rows = self.rows[:2]
        with self.assertRaises(ValueError): self.validate()

    def test_changed_epoch_is_rejected(self):
        self.end['epoch_id'] = 'epoch-2'
        with self.assertRaises(ValueError): self.validate()

    def test_short_window_is_rejected(self):
        self.end['unix_ns'] -= 1
        with self.assertRaises(ValueError): self.validate()

    def test_reversed_clock_is_rejected(self):
        self.rows[1], self.rows[2] = self.rows[2], self.rows[1]
        with self.assertRaises(ValueError): self.validate()

    def test_no_samples_is_rejected(self):
        self.rows = []
        with self.assertRaises(ValueError): self.validate()


class FinalEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.begin = {'epoch_id': 'e', 'generation': 1, 'process_identity': 'p'}
        report = dict(self.begin, sequence=4, final=True, final_nonce='a' * 64,
                      counters=[{'principal_id': 'customer', 'upload': '120', 'download': '250'}])
        report['final_proof_sha'] = runner.go_digest({'process': 'p', 'nonce': report['final_nonce'], 'counters': report['counters']})
        receipt = {'epoch_id': 'e', 'sequence': 4, 'final': True, 'sha256': runner.go_digest(report)}
        self.evidence = {'http_status': 200, 'report': report, 'receipt': receipt,
                         'principal_id': 'customer', 'committed_bytes': 370}

    def test_valid_and_corrupted_wire_evidence(self):
        runner.validate_final(self.evidence, self.begin, 370)
        for section, key, value in [('report', 'final', False), ('receipt', 'final', False),
                                    ('receipt', 'epoch_id', 'other'), ('receipt', 'sequence', 5),
                                    ('receipt', 'sha256', 'f' * 64), ('report', 'generation', 2),
                                    ('report', 'final_nonce', 'invalid')]:
            with self.subTest(section=section, key=key):
                evidence = copy.deepcopy(self.evidence)
                evidence[section][key] = value
                with self.assertRaises(ValueError): runner.validate_final(evidence, self.begin, 370)

    def test_wrong_customer_total(self):
        with self.assertRaises(ValueError): runner.validate_final(self.evidence, self.begin, 371)

    def test_nonaccepted_response(self):
        self.evidence['http_status'] = 503
        with self.assertRaises(ValueError): runner.validate_final(self.evidence, self.begin, 370)

    def test_wrong_native_proof_even_with_valid_report_hash(self):
        self.evidence['report']['final_proof_sha'] = 'f' * 64
        self.evidence['receipt']['sha256'] = runner.go_digest(self.evidence['report'])
        with self.assertRaisesRegex(ValueError, 'native_final_proof'): runner.validate_final(self.evidence, self.begin, 370)


if __name__ == '__main__':
    unittest.main()
