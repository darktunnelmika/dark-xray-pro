"""Mocked host evidence only: no LAB, service, secret, database or network access."""

from contextlib import ExitStack, redirect_stderr, redirect_stdout
import importlib.util
import io
import json
import os
from pathlib import Path, PurePosixPath
import stat
import subprocess
import sys
from types import SimpleNamespace
import unittest
from unittest import mock

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location(
    "telegram_lab_preflight", Path(__file__).resolve().parents[1] / "telegram-lab-preflight.py")
probe = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = probe
SPEC.loader.exec_module(probe)

REVISION = "e89ce8430426443f92875aa876aee81e37e8d669"
PID, UID, GID = 4321, 991, 992
SENTINEL = "SECRET_MUST_NEVER_BE_READ_OR_PRINTED"


def info(mode, uid=0, gid=0, links=1, inode=1):
    return SimpleNamespace(st_mode=mode, st_uid=uid, st_gid=gid, st_nlink=links,
                           st_dev=8, st_ino=inode)


class Host:
    def __init__(self):
        self.exe = f"{probe.RELEASES}/{REVISION}/dark-pro-hub"
        self.paths = {}
        for path in (self.exe, probe.CONFIG, probe.JOURNAL):
            for ancestor in PurePosixPath(path).parents:
                self.paths[str(ancestor)] = info(stat.S_IFDIR | 0o755, links=2)
        self.paths[self.exe] = info(stat.S_IFREG | 0o755, inode=100)
        for index, path in enumerate((probe.CONFIG, probe.JOURNAL)):
            self.paths[str(PurePosixPath(path).parent)] = info(stat.S_IFDIR | 0o700, UID, GID, 2)
            self.paths[path] = info(stat.S_IFREG | 0o600, UID, GID, inode=200 + index)
        self.paths["/etc/dark-xray-pro-lab"].st_mode = stat.S_IFDIR | 0o711
        self.service = ("LoadState=loaded\nActiveState=active\nSubState=running\n"
                        f"MainPID={PID}\nUser=darkprolab\nGroup=darkprolab\n")
        # Deliberately private-looking unrelated fields must never escape.
        self.status = (f"Name:\t{SENTINEL}\nUid:\t{UID}\t{UID}\t{UID}\t{UID}\n"
                       f"Gid:\t{GID}\t{GID}\t{GID}\t{GID}\nGroups:\t{GID} 993\n")
        self.starttime = 123456
        self.reads, self.stats, self.commands = [], [], []
        self.fd_path = {}

    def lstat(self, path):
        self.stats.append(str(path))
        if str(path) not in self.paths:
            raise FileNotFoundError(SENTINEL)
        return self.paths[str(path)]

    def open(self, path, flags, *args, **kwargs):
        allowed = {f"/proc/{PID}/status", f"/proc/{PID}/stat"}
        if path not in allowed:
            raise AssertionError(f"Unexpected content read: {path}")
        self.assert_read_flags(flags)
        fd = 20 + len(self.fd_path)
        self.fd_path[fd] = path
        self.reads.append(path)
        return fd

    @staticmethod
    def assert_read_flags(flags):
        if flags != os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK:
            raise AssertionError("Unexpected open flags")

    def read(self, fd, limit):
        if limit != 32769:
            raise AssertionError("Unbounded process read")
        if self.fd_path[fd].endswith("/status"):
            return self.status.encode()
        # Linux comm can contain spaces and closing parentheses. Start time is
        # field22, not an index into a naive space split of the whole record.
        return (f"{PID} ({SENTINEL} name) with ) parentheses) S "
                + "0 " * 18 + f"{self.starttime} 0 0\n").encode()

    def run(self, command, **kwargs):
        self.commands.append((command, kwargs))
        return subprocess.CompletedProcess(command, 0, stdout=self.service, stderr=SENTINEL)

    def patch(self):
        stack = ExitStack()
        for name, replacement in (
                ("lstat", self.lstat), ("open", self.open), ("read", self.read),
                ("close", lambda fd: None),
                ("fstat", lambda fd: info(stat.S_IFREG | 0o444)),
                ("readlink", lambda path: self.exe),
                ("stat", lambda path: self.paths[self.exe])):
            stack.enter_context(mock.patch.object(probe.os, name, side_effect=replacement))
        stack.enter_context(mock.patch.object(probe.subprocess, "run", side_effect=self.run))
        return stack

    def report(self):
        with self.patch():
            return probe.preflight(REVISION)


class PreflightTests(unittest.TestCase):
    def assert_no_acceptance(self, report):
        self.assertTrue(report["acceptance"])
        self.assertEqual(set(report["acceptance"].values()), {"unknown"})
        self.assertEqual(report["scope"], "host_metadata_only")
        self.assertNotIn("ready", report)
        self.assertNotIn(SENTINEL, json.dumps(report))

    def test_complete_metadata_is_only_metadata_pass_never_end_to_end(self):
        host = Host()
        report = host.report()
        self.assertEqual(report["status"], "passed")
        self.assertEqual(report["checks"]["process"]["uid"], UID)
        self.assertEqual(report["checks"]["revision"]["observed"], REVISION)
        self.assert_no_acceptance(report)
        self.assertEqual(len(host.commands), 2)
        self.assertEqual(host.reads, [f"/proc/{PID}/status", f"/proc/{PID}/stat"] * 2)

    def test_no_secret_environment_db_read_write_or_network_operations(self):
        host = Host()
        forbidden = AssertionError("Forbidden side effect or content access")
        with host.patch(), ExitStack() as guards:
            for name in ("chmod", "chown", "mkdir", "makedirs", "unlink", "remove", "rename",
                         "replace", "symlink", "link", "truncate", "write", "system", "getenv"):
                guards.enter_context(mock.patch.object(probe.os, name, side_effect=forbidden))
            guards.enter_context(mock.patch("builtins.open", side_effect=forbidden))
            guards.enter_context(mock.patch("io.open", side_effect=forbidden))
            guards.enter_context(mock.patch("socket.socket", side_effect=forbidden))
            guards.enter_context(mock.patch("sqlite3.connect", side_effect=forbidden))
            guards.enter_context(mock.patch.dict(os.environ, {"DARK_BOT_TOKEN": SENTINEL}))
            report = probe.preflight(REVISION)
        self.assertEqual(report["status"], "passed")
        self.assert_no_acceptance(report)
        for command, kwargs in host.commands:
            self.assertEqual(command, ["/usr/bin/systemctl", "--no-pager", "--no-ask-password",
                                      "show", "dark-xray-pro-lab.service",
                                      "--property=LoadState,ActiveState,SubState,MainPID,User,Group"])
            self.assertEqual(kwargs["env"], probe.SUBPROCESS_ENV)
            self.assertNotIn(SENTINEL, json.dumps(kwargs["env"]))
            self.assertNotIn("shell", kwargs)
            self.assertLessEqual(kwargs["timeout"], 5)

    def test_ownership_uses_effective_service_uid_not_invoking_root(self):
        host = Host()
        with mock.patch.object(probe.os, "geteuid", return_value=0):
            self.assertEqual(host.report()["status"], "passed")
            host.paths[probe.CONFIG].st_uid = 0
            report = host.report()
        self.assertEqual(report["status"], "blocked")
        self.assertEqual(report["checks"]["config"]["code"], "file_owner_differs_from_process")

    def test_ancestor_traversal_uses_owner_group_and_supplementary_groups(self):
        for gid, mode, allowed in ((0, 0o700, False), (993, 0o710, True), (994, 0o710, False),
                                   (0, 0o701, True), (GID, 0o710, True)):
            with self.subTest(gid=gid, mode=oct(mode)):
                host = Host()
                host.paths["/etc/dark-xray-pro-lab"] = info(stat.S_IFDIR | mode, 0, gid, 2)
                report = host.report()
                self.assertEqual(report["status"], "passed" if allowed else "blocked")
                if not allowed:
                    self.assertEqual(report["checks"]["config"]["code"], "ancestor_not_traversable")
        host = Host()
        # Owner class wins over group/other bits; do not OR all permission bits.
        host.paths["/etc/dark-xray-pro-lab"] = info(stat.S_IFDIR | 0o011, UID, GID, 2)
        self.assertEqual(host.report()["status"], "blocked")

    def test_symlinks_never_followed_and_hardlinks_nonregular_files_rejected(self):
        for path in (probe.CONFIG, probe.JOURNAL, "/etc/dark-xray-pro-lab",
                     "/var/lib/dark-xray-pro-lab/telegram-bots", probe.RELEASES):
            with self.subTest(path=path):
                host = Host()
                host.paths[path] = info(stat.S_IFLNK | 0o777)
                report = host.report()
                self.assertEqual(report["status"], "blocked")
                self.assertTrue(any(c.get("code") == "symlink_rejected" for c in report["checks"].values()))
                self.assertFalse(any(p.startswith(path + "/") for p in host.stats))
        for path in (probe.CONFIG, probe.JOURNAL):
            for mode, links in ((stat.S_IFREG | 0o600, 2), (stat.S_IFIFO | 0o600, 1),
                                (stat.S_IFDIR | 0o700, 1)):
                with self.subTest(path=path, mode=mode, links=links):
                    host = Host()
                    host.paths[path] = info(mode, UID, GID, links)
                    self.assertEqual(host.report()["status"], "blocked")

    def test_private_modes_and_trusted_ancestors_are_required(self):
        for path, uid, mode in (
                (probe.CONFIG, UID, 0o640), (probe.CONFIG, UID, 0o700),
                (probe.CONFIG, UID, 0o4600), (probe.CONFIG, UID, 0o200),
                (probe.JOURNAL, UID, 0o400),
                ("/etc/dark-xray-pro-lab/telegram-bots", UID, 0o755),
                ("/var/lib/dark-xray-pro-lab/telegram-bots", UID, 0o500),
                ("/etc", 0, 0o777), ("/var/lib", 555, 0o755)):
            with self.subTest(path=path, uid=uid, mode=oct(mode)):
                host = Host()
                host.paths[path].st_mode = stat.S_IFMT(host.paths[path].st_mode) | mode
                host.paths[path].st_uid = uid
                self.assertEqual(host.report()["status"], "blocked")
        host = Host()
        host.paths[probe.CONFIG].st_mode = stat.S_IFREG | 0o400
        self.assertEqual(host.report()["status"], "passed")

    def test_mismatch_deleted_unexpected_and_unversioned_process_paths_block(self):
        for path in (f"{probe.RELEASES}/{'a' * 40}/dark-pro-hub",
                     f"{probe.RELEASES}/{REVISION}/bin/dark-pro-hub",
                     f"{probe.RELEASES}/{REVISION}/dark-pro-hub (deleted)",
                     "/opt/dark-xray-pro-lab/bin/dark-pro-hub", f"/{SENTINEL}"):
            with self.subTest(path=path):
                host = Host()
                # Kernel inode observation is independent of the returned path.
                original = host.paths[host.exe]
                host.exe = path
                host.paths[path] = original
                report = host.report()
                self.assertEqual(report["status"], "blocked")
                self.assert_no_acceptance(report)
        host = Host()
        with host.patch(), mock.patch.object(probe.os, "stat", return_value=info(stat.S_IFREG | 0o755, inode=999)):
            report = probe.preflight(REVISION)
        self.assertEqual(report["checks"]["revision"]["code"], "running_executable_inode_differs")

    def test_missing_files_block_unavailable_service_or_evidence_is_unknown(self):
        for path in (probe.CONFIG, probe.JOURNAL, "/etc/dark-xray-pro-lab/telegram-bots"):
            host = Host()
            del host.paths[path]
            self.assertEqual(host.report()["status"], "blocked")
        for failure in (FileNotFoundError(SENTINEL), PermissionError(SENTINEL),
                        subprocess.TimeoutExpired(SENTINEL, 5, output=SENTINEL)):
            host = Host()
            with host.patch(), mock.patch.object(probe.subprocess, "run", side_effect=failure):
                report = probe.preflight(REVISION)
            self.assertEqual(report["status"], "unknown")
            self.assert_no_acceptance(report)
        host = Host()
        original = host.lstat
        def denied(path):
            if path == probe.CONFIG:
                raise PermissionError(SENTINEL)
            return original(path)
        host.lstat = denied
        self.assertEqual(host.report()["status"], "unknown")

    def test_inactive_wrong_account_and_root_process_cannot_pass(self):
        for old, new in (("LoadState=loaded", "LoadState=not-found"),
                         ("ActiveState=active", "ActiveState=inactive"),
                         ("SubState=running", "SubState=exited"),
                         ("User=darkprolab", "User=root"), ("Group=darkprolab", "Group=")):
            host = Host()
            host.service = host.service.replace(old, new)
            self.assertEqual(host.report()["status"], "blocked")
        host = Host()
        host.status = host.status.replace(f"Uid:\t{UID}\t{UID}\t{UID}\t{UID}", "Uid:\t0\t0\t0\t0")
        report = host.report()
        self.assertEqual(report["checks"]["process"]["code"], "root_process_rejected")
        self.assertEqual(report["status"], "blocked")

    def test_partial_malformed_or_credential_mismatch_evidence_is_not_pass(self):
        for bad in ("", "LoadState=loaded\n", SENTINEL,
                    Host().service + "Environment=" + SENTINEL + "\n",
                    Host().service + "MainPID=4321\n"):
            host = Host()
            host.service = bad
            report = host.report()
            self.assertEqual(report["status"], "unknown")
            self.assert_no_acceptance(report)
        for bad in ("", "Uid:\t991\t991\t991\t991\n",
                    Host().status + "Uid:\t0\t0\t0\t0\n",
                    Host().status.replace(f"Uid:\t{UID}\t{UID}\t{UID}\t{UID}", f"Uid:\t{UID}\t{UID}\t{UID}\t0")):
            host = Host()
            host.status = bad
            self.assertEqual(host.report()["status"], "unknown")

    def test_pid_restart_reuse_credential_or_path_change_during_check_is_unknown(self):
        for mutation in ("pid", "starttime", "credentials", "inode"):
            with self.subTest(mutation=mutation):
                host = Host()
                calls = 0
                def changed(command, **kwargs):
                    nonlocal calls
                    calls += 1
                    if calls == 2:
                        if mutation == "pid":
                            host.service = host.service.replace(f"MainPID={PID}", f"MainPID={PID + 1}")
                        elif mutation == "starttime":
                            host.starttime += 1
                        elif mutation == "credentials":
                            host.status = host.status.replace("Groups:\t992 993", "Groups:\t992 994")
                        else:
                            host.paths[probe.JOURNAL].st_ino += 1
                    return host.run(command, **kwargs)
                with host.patch(), mock.patch.object(probe.subprocess, "run", side_effect=changed):
                    report = probe.preflight(REVISION)
                self.assertEqual(report["status"], "unknown")
                self.assertEqual(report["checks"]["stable_snapshot"]["status"], "unknown")

    def test_invalid_cli_never_echoes_values_or_accepts_path_overrides(self):
        for args in ([], ["--expected-revision", "e89ce84"], ["--expected-revision", SENTINEL],
                     ["--expected-revision", REVISION, "--config", f"/{SENTINEL}"],
                     ["--expected-rev", REVISION], ["--root", "/", "--expected-revision", REVISION]):
            stdout, stderr = io.StringIO(), io.StringIO()
            with mock.patch.object(probe, "service_snapshot", side_effect=AssertionError("Host inspection forbidden")):
                with redirect_stdout(stdout), redirect_stderr(stderr):
                    result = probe.main(args)
            self.assertEqual(result, 2)
            self.assertEqual(stderr.getvalue(), "")
            report = json.loads(stdout.getvalue())
            self.assertEqual(report["status"], "blocked")
            self.assert_no_acceptance(report)

    def test_main_exit_codes_follow_metadata_and_never_emit_secret_diagnostics(self):
        for status, expected in (("passed", 0), ("blocked", 1), ("unknown", 2)):
            stdout = io.StringIO()
            value = {"scope": "host_metadata_only", "status": status, "acceptance": {"end_to_end": "unknown"}}
            with mock.patch.object(probe, "preflight", return_value=value), redirect_stdout(stdout):
                self.assertEqual(probe.main(["--expected-revision", REVISION]), expected)
            self.assert_no_acceptance(json.loads(stdout.getvalue()))
        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.object(probe, "preflight", side_effect=RuntimeError(SENTINEL)):
            with redirect_stdout(stdout), redirect_stderr(stderr):
                self.assertEqual(probe.main(["--expected-revision", REVISION]), 2)
        self.assertEqual(stderr.getvalue(), "")
        self.assert_no_acceptance(json.loads(stdout.getvalue()))


if __name__ == "__main__":
    unittest.main()
