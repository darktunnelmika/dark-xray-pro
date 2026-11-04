"""Pure tests: no Go processes, network, databases or production code run."""

from collections import Counter
from contextlib import redirect_stderr, redirect_stdout
import io
import json
import re
import subprocess
import unittest
from unittest import mock

import run_business_race as shard


def listing(names):
    return "\n".join(names) + f"\nok  \t{shard.IMPORT_PATH}\t0.007s\n"


class PartitionTests(unittest.TestCase):
    def test_exact_exhaustive_disjoint_union_for_odd_and_even_inventories(self):
        for count in range(2, 80):
            names = tuple(f"TestCase{index:03}" for index in reversed(range(count)))
            one = shard.select_shard(names, 1, 2)
            two = shard.select_shard(names, 2, 2)
            self.assertEqual(Counter(one + two), Counter(names))
            self.assertFalse(set(one) & set(two))
            self.assertEqual(one, tuple(sorted(names))[::2])
            self.assertEqual(two, tuple(sorted(names))[1::2])
            self.assertLessEqual(abs(len(one) - len(two)), 1)

    def test_future_examples_fuzz_targets_bare_and_unicode_names_are_included(self):
        names = ("Test", "TestExisting", "Example", "ExampleStore", "ExampleStore_new",
                 "Fuzz", "FuzzFuture", "Testآزمایش", "FuzzΔ", "BenchmarkIgnored")
        discovered = shard.parse_discovery(listing(names))
        self.assertEqual(discovered.benchmarks, ("BenchmarkIgnored",))
        one = shard.select_shard(discovered.names, 1, 2)
        two = shard.select_shard(discovered.names, 2, 2)
        self.assertEqual(Counter(one + two), Counter(names[:-1]))
        self.assertFalse(set(one) & set(two))
        self.assertNotIn("BenchmarkIgnored", one + two)

    def test_reordering_does_not_change_membership_or_fingerprints(self):
        names = ["TestZulu", "ExampleDelta", "TestAlpha", "FuzzBeta"]
        first = shard.parse_discovery(listing(names))
        second = shard.parse_discovery(listing(list(reversed(names))))
        self.assertEqual(first, second)
        for number in (1, 2):
            a = shard.select_shard(first.names, number, 2)
            b = shard.select_shard(second.names, number, 2)
            self.assertEqual(a, b)
            self.assertEqual(shard.fingerprint(a), shard.fingerprint(b))
        self.assertNotEqual(shard.fingerprint(first.names), shard.fingerprint(first.names[:1]))

    def test_pattern_selects_exact_top_levels_without_subtest_filters(self):
        names = ("Test", "TestFoo", "ExampleFoo_case", "Fuzzآزمایش")
        pattern = shard.selection_pattern(names)
        self.assertNotIn("/", pattern)
        self.assertTrue(pattern.startswith("^(") and pattern.endswith(")$"))
        for name in names:
            self.assertIsNotNone(re.fullmatch(pattern, name))
        for name in ("TestFoobar", "PrefixTestFoo", "FuzzOther", "BenchmarkFoo"):
            self.assertIsNone(re.fullmatch(pattern, name))

    def test_invalid_parameters_and_vacuous_or_corrupt_selections_fail(self):
        for index, total in ((0, 2), (3, 2), (1, 0), (-1, 2), (1, -2), (True, 2), (1.5, 2)):
            with self.subTest(index=index, total=total), self.assertRaises(shard.ShardError):
                shard.select_shard(("TestOne", "TestTwo"), index, total)
        for names, index, total in (((), 1, 2), (("TestOne",), 2, 2),
                                     (("TestOne", "TestOne"), 1, 2), (("BenchmarkOnly",), 1, 1)):
            with self.subTest(names=names), self.assertRaises(shard.ShardError):
                shard.select_shard(names, index, total)
        with self.assertRaises(shard.ShardError):
            shard.selection_pattern(())

    def test_unknown_duplicate_incomplete_and_empty_discovery_fail_closed(self):
        invalid = [
            listing(["TestOne", "UnexpectedName"]), listing(["TestOne", "TestOne"]),
            listing(["TestOne", "BenchmarkA", "BenchmarkA"]), listing(["TestOne/subtest"]),
            listing([" TestOne"]), listing(["TestOne", "TestInjection;echo"]),
            listing(["BenchmarkOnly"]), listing([]), "TestOne\n",
            listing(["TestOne"]) + "TestTwo\n",
            listing(["TestOne"]) + f"ok\t{shard.IMPORT_PATH}\t(cached)\n",
            listing(["TestOne"]).replace(shard.IMPORT_PATH, "example.invalid/other"),
        ]
        for output in invalid:
            with self.subTest(output=output), self.assertRaises(shard.ShardError):
                shard.parse_discovery(output)


class CommandTests(unittest.TestCase):
    def call_main(self, args, responses):
        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.object(shard.subprocess, "run", side_effect=responses) as run:
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = shard.main(args)
        return code, stdout.getvalue(), stderr.getvalue(), run

    def discovered(self, names=("TestOne", "TestTwo", "ExampleNew", "FuzzNew")):
        return subprocess.CompletedProcess([], 0, stdout=listing(names), stderr="")

    def test_dry_run_uses_race_inventory_and_does_not_execute_tests(self):
        go = "/pinned toolchain/go"
        code, output, errors, run = self.call_main(
            ["--shard", "1", "--shards", "2", "--go", go, "--dry-run"],
            [self.discovered()],
        )
        self.assertEqual((code, errors), (0, ""))
        self.assertEqual(run.call_count, 1)
        discovery = run.call_args
        self.assertEqual(discovery.args[0], [go, "test", "-race", "-list", ".", shard.PACKAGE])
        self.assertEqual(discovery.kwargs["env"]["GOTOOLCHAIN"], "local")
        self.assertNotIn("shell", discovery.kwargs)
        plan = json.loads(output)
        self.assertEqual((plan["total"], plan["selected"]), (4, 2))
        self.assertEqual(plan["selected_names"], ["ExampleNew", "TestOne"])
        self.assertEqual(plan["selection_sha256"], shard.fingerprint(tuple(plan["selected_names"])))
        self.assertEqual(plan["command"][0], go)

    def test_full_race_argv_and_failure_exit_code_are_preserved(self):
        code, output, errors, run = self.call_main(
            ["--shard", "2", "--shards", "2"],
            [self.discovered(), subprocess.CompletedProcess([], 1)],
        )
        self.assertEqual((code, errors, run.call_count), (1, "", 2))
        command = run.call_args.args[0]
        for flag in ("-race", "-count=1", "-timeout=18m", "-short=false", "-skip=^$", "-bench=^$", "-fuzz=", "-list="):
            self.assertIn(flag, command)
        self.assertEqual(command[-1], shard.PACKAGE)
        pattern = command[command.index("-run") + 1]
        self.assertEqual(pattern, "^(FuzzNew|TestTwo)$")
        self.assertNotIn("shell", run.call_args.kwargs)
        self.assertEqual(json.loads(output)["selected"], 2)

    def test_inherited_list_only_flag_cannot_fake_a_successful_race_run(self):
        with mock.patch.dict(shard.os.environ, {"GOFLAGS": "-list=. -tags=ci"}):
            code, _, errors, run = self.call_main(
                ["--shard", "1", "--shards", "2"],
                [self.discovered(), subprocess.CompletedProcess([], 0)],
            )
        self.assertEqual((code, errors), (0, ""))
        self.assertIn("-list=", run.call_args.args[0])
        # Keep build tags consistent with discovery while overriding the
        # inherited list-only mode in the explicit execution arguments.
        self.assertEqual(run.call_args.kwargs["env"]["GOFLAGS"], "-list=. -tags=ci")

    def test_discovery_failures_never_reach_test_execution(self):
        failures = [
            subprocess.CompletedProcess([], 1, stdout=listing(["TestOne"]), stderr="build failed"),
            subprocess.CompletedProcess([], 0, stdout="unexpected output", stderr=""),
            FileNotFoundError("missing Go binary"),
            subprocess.TimeoutExpired(["go", "test", "-list", "."], 300),
        ]
        for failure in failures:
            with self.subTest(failure=failure):
                code, output, errors, run = self.call_main(["--shard", "1", "--shards", "2"], [failure])
                self.assertEqual(code, 2)
                self.assertEqual(output, "")
                self.assertTrue(errors)
                self.assertEqual(run.call_count, 1)

    def test_invalid_cli_parameters_do_not_invoke_go(self):
        for args in (["--shard", "0", "--shards", "2"], ["--shard", "3", "--shards", "2"],
                     ["--shard", "1", "--shards", "0"], ["--shard", "1", "--shards", "2", "--go", ""]):
            with self.subTest(args=args):
                code, output, errors, run = self.call_main(args, [])
                self.assertEqual(code, 2)
                self.assertEqual(output, "")
                self.assertTrue(errors)
                run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
