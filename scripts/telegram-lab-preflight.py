#!/usr/bin/env python3
"""Read-only, fixed-path LAB metadata checks; never an end-to-end acceptance.

Only systemctl's selected public properties, /proc PID identity/start time, and
filesystem metadata are inspected. No config, DB, environment or command-line
contents are opened. Exit: 0 metadata passed, 1 blocked, 2 unknown/invalid CLI.
"""

import sys

sys.dont_write_bytecode = True

import argparse
from dataclasses import dataclass
import json
import os
from pathlib import PurePosixPath
import re
import stat
import subprocess


UNIT = "dark-xray-pro-lab.service"
CONFIG = "/etc/dark-xray-pro-lab/telegram-bots/config.json"
JOURNAL = "/var/lib/dark-xray-pro-lab/telegram-bots/journal.sqlite"
RELEASES = "/opt/dark-xray-pro-lab/releases"
SHA = re.compile(r"[0-9a-f]{40}", re.ASCII)
EXE = re.compile(re.escape(RELEASES) + r"/([0-9a-f]{40})/dark-pro-hub", re.ASCII)
PROPERTIES = ("LoadState", "ActiveState", "SubState", "MainPID", "User", "Group")
SYSTEMCTL = ["/usr/bin/systemctl", "--no-pager", "--no-ask-password", "show", UNIT,
             "--property=" + ",".join(PROPERTIES)]
SUBPROCESS_ENV = {"LC_ALL": "C", "PATH": "/usr/sbin:/usr/bin:/sbin:/bin",
                  "SYSTEMD_COLORS": "0", "SYSTEMD_PAGER": ""}


class ObservationError(Exception):
    def __init__(self, status, code, **details):
        super().__init__(code)
        self.result = {"status": status, "code": code, **details}


def observe(function, *args):
    try:
        return {"status": "passed", **function(*args)}
    except ObservationError as error:
        return error.result
    except OSError:
        return {"status": "unknown", "code": "metadata_unavailable"}


def service_snapshot():
    try:
        result = subprocess.run(SYSTEMCTL, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                text=True, encoding="utf-8", errors="strict",
                                timeout=5, check=False, env=SUBPROCESS_ENV)
    except (OSError, subprocess.SubprocessError, UnicodeError):
        raise ObservationError("unknown", "systemctl_unavailable") from None
    if result.returncode != 0:
        raise ObservationError("unknown", "systemctl_unavailable")
    if len(result.stdout) > 4096:
        raise ObservationError("unknown", "service_snapshot_invalid")
    fields = {}
    for line in result.stdout.splitlines():
        key, separator, value = line.partition("=")
        if not separator or key not in PROPERTIES or key in fields:
            raise ObservationError("unknown", "service_snapshot_invalid")
        fields[key] = value
    if set(fields) != set(PROPERTIES):
        raise ObservationError("unknown", "service_snapshot_incomplete")
    if fields["LoadState"] != "loaded":
        raise ObservationError("blocked", "service_not_loaded")
    if fields["ActiveState"] != "active" or fields["SubState"] != "running":
        raise ObservationError("blocked", "service_not_running")
    if fields["User"] != "darkprolab" or fields["Group"] != "darkprolab":
        raise ObservationError("blocked", "unexpected_service_account")
    if not re.fullmatch(r"[1-9][0-9]{0,9}", fields["MainPID"], re.ASCII):
        raise ObservationError("unknown", "service_pid_invalid")
    return fields


def proc_text(pid, name):
    """The only content reads: two bounded, fixed public /proc identity files."""
    if type(pid) is not int or pid <= 0 or name not in ("status", "stat"):
        raise ObservationError("unknown", "process_identity_invalid")
    path = f"/proc/{pid}/{name}"
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise ObservationError("unknown", "process_identity_invalid")
        data = os.read(fd, 32769)
        if len(data) > 32768:
            raise ObservationError("unknown", "process_identity_invalid")
        return data.decode("utf-8", errors="replace")
    finally:
        os.close(fd)


@dataclass(frozen=True)
class Process:
    pid: int
    uid: int
    gid: int
    groups: tuple
    starttime: int
    executable: str
    device: int
    inode: int


def process_snapshot(pid):
    selected = {}
    for line in proc_text(pid, "status").splitlines():
        key, separator, value = line.partition(":")
        if key not in ("Uid", "Gid", "Groups"):
            continue
        if not separator or key in selected:
            raise ObservationError("unknown", "process_identity_invalid")
        numbers = value.split()
        if any(not re.fullmatch(r"[0-9]{1,10}", n, re.ASCII) for n in numbers):
            raise ObservationError("unknown", "process_identity_invalid")
        selected[key] = tuple(int(n) for n in numbers)
    if set(selected) != {"Uid", "Gid", "Groups"} or any(
            len(selected[key]) != 4 for key in ("Uid", "Gid")):
        raise ObservationError("unknown", "process_identity_invalid")
    # File-system credentials can differ from effective credentials. Do not
    # claim a permission result for an unexpected setfsuid/setfsgid process.
    if any(selected[key][1] != selected[key][3] for key in ("Uid", "Gid")):
        raise ObservationError("unknown", "filesystem_identity_differs")
    raw_stat = proc_text(pid, "stat")
    end_name = raw_stat.rfind(")")
    fields = raw_stat[end_name + 1:].split() if end_name >= 0 else []
    if not raw_stat.startswith(f"{pid} (") or len(fields) < 20 or not re.fullmatch(
            r"[1-9][0-9]*", fields[19], re.ASCII):
        raise ObservationError("unknown", "process_starttime_invalid")
    executable = os.readlink(f"/proc/{pid}/exe")
    # Following this one kernel-owned link checks that the observed path still
    # names the actual running inode. Ordinary inspected paths use lstat only.
    executable_stat = os.stat(f"/proc/{pid}/exe")
    return Process(pid, selected["Uid"][1], selected["Gid"][1],
                   tuple(sorted(set(selected["Groups"]))), int(fields[19]),
                   executable, executable_stat.st_dev, executable_stat.st_ino)


def metadata(info):
    return {"uid": info.st_uid, "gid": info.st_gid,
            "mode": f"{stat.S_IMODE(info.st_mode):04o}", "links": info.st_nlink}


def signature(info):
    # Journal contents may change while HUB runs: neither read them nor mistake
    # an ordinary DB write for a change of file identity/permissions.
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, info.st_nlink)


def permitted(info, process, owner_bit):
    if info.st_uid == process.uid:
        return bool(info.st_mode & owner_bit)
    if info.st_gid in (process.gid, *process.groups):
        return bool(info.st_mode & (owner_bit >> 3))
    return bool(info.st_mode & (owner_bit >> 6))


def inspect_path(path, process, kind, inspected):
    ancestors = list(reversed(PurePosixPath(path).parents))
    rows = []
    for entry in [*ancestors, PurePosixPath(path)]:
        current = str(entry)
        try:
            info = os.lstat(current)
        except FileNotFoundError:
            raise ObservationError("blocked", "path_missing", path=current) from None
        except OSError:
            raise ObservationError("unknown", "path_metadata_unavailable", path=current) from None
        current_signature = signature(info)
        if current in inspected and inspected[current] != current_signature:
            raise ObservationError("unknown", "path_changed_during_observation", path=current)
        inspected[current] = current_signature
        details = {"path": current, **metadata(info)}
        if stat.S_ISLNK(info.st_mode):
            raise ObservationError("blocked", "symlink_rejected", **details)
        if current != path:
            if not stat.S_ISDIR(info.st_mode):
                raise ObservationError("blocked", "ancestor_not_directory", **details)
            if info.st_mode & 0o022:
                raise ObservationError("blocked", "ancestor_writable_by_others", **details)
            if process is not None:
                if info.st_uid not in (0, process.uid):
                    raise ObservationError("blocked", "ancestor_owner_untrusted", **details)
                if not permitted(info, process, 0o100):
                    raise ObservationError("blocked", "ancestor_not_traversable", **details)
                if entry == PurePosixPath(path).parent and kind != "executable":
                    if info.st_uid != process.uid or info.st_mode & 0o077:
                        raise ObservationError("blocked", "private_parent_required", **details)
                    if kind == "journal" and not permitted(info, process, 0o200):
                        raise ObservationError("blocked", "journal_parent_not_writable", **details)
            rows.append(details)
            continue
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise ObservationError("blocked", "regular_single_link_file_required", **details)
        if info.st_mode & 0o7000:
            raise ObservationError("blocked", "special_file_mode_rejected", **details)
        if kind != "executable" and info.st_mode & 0o177:
            raise ObservationError("blocked", "private_nonexecutable_file_required", **details)
        if kind == "executable" and info.st_mode & 0o022:
            raise ObservationError("blocked", "executable_writable_by_others", **details)
        if process is None:
            raise ObservationError("unknown", "process_identity_unavailable", **details)
        if kind == "executable":
            if info.st_uid not in (0, process.uid) or not permitted(info, process, 0o100):
                raise ObservationError("blocked", "executable_owner_or_mode_invalid", **details)
            if (info.st_dev, info.st_ino) != (process.device, process.inode):
                raise ObservationError("blocked", "running_executable_inode_differs", **details)
        elif info.st_uid != process.uid:
            raise ObservationError("blocked", "file_owner_differs_from_process", **details)
        elif not info.st_mode & 0o400 or (kind == "journal" and not info.st_mode & 0o200):
            raise ObservationError("blocked", "required_owner_access_missing", **details)
    return {"code": "posix_metadata_checked", "path": path, "file": metadata(info),
            "ancestors": rows}


def preflight(expected_revision):
    if not isinstance(expected_revision, str) or not SHA.fullmatch(expected_revision):
        raise ObservationError("blocked", "exact_expected_revision_required")
    checks, inspected = {}, {}
    before_service, before_process = None, None
    try:
        before_service = service_snapshot()
        checks["service"] = {"status": "passed", "unit": UNIT, "state": "active/running"}
        before_process = process_snapshot(int(before_service["MainPID"]))
        checks["process"] = {"status": "blocked" if before_process.uid == 0 else "passed",
                             "code": "root_process_rejected" if before_process.uid == 0 else "identity_observed",
                             "pid": before_process.pid, "uid": before_process.uid, "gid": before_process.gid}
    except ObservationError as error:
        checks["process" if before_service else "service"] = error.result
    except OSError:
        checks["process"] = {"status": "unknown", "code": "process_unavailable"}
    if before_process is None:
        checks["revision"] = {"status": "unknown", "code": "process_unavailable"}
    else:
        match = EXE.fullmatch(before_process.executable)
        if match is None:
            checks["revision"] = {"status": "blocked", "code": "unexpected_running_release_path"}
        elif match[1] != expected_revision:
            checks["revision"] = {"status": "blocked", "code": "running_revision_mismatch", "observed": match[1]}
        else:
            checks["revision"] = observe(inspect_path, before_process.executable, before_process,
                                          "executable", inspected)
            checks["revision"]["observed"] = match[1]
    checks["config"] = observe(inspect_path, CONFIG, before_process, "config", inspected)
    checks["journal"] = observe(inspect_path, JOURNAL, before_process, "journal", inspected)
    try:
        if before_service is None or before_process is None:
            raise ObservationError("unknown", "initial_process_unavailable")
        if service_snapshot() != before_service or process_snapshot(before_process.pid) != before_process:
            raise ObservationError("unknown", "process_changed_during_observation")
        if any(signature(os.lstat(path)) != value for path, value in inspected.items()):
            raise ObservationError("unknown", "path_changed_during_observation")
        checks["stable_snapshot"] = {"status": "passed"}
    except ObservationError as error:
        checks["stable_snapshot"] = {"status": "unknown", "code": error.result["code"]}
    except OSError:
        checks["stable_snapshot"] = {"status": "unknown", "code": "snapshot_recheck_unavailable"}
    statuses = {check["status"] for check in checks.values()}
    status = "blocked" if "blocked" in statuses else "unknown" if "unknown" in statuses else "passed"
    return {"scope": "host_metadata_only", "status": status, "expected_revision": expected_revision,
            "checks": checks, "acceptance": {key: "unknown" for key in (
                "end_to_end", "binary_build_provenance", "bot_identity", "store_registration",
                "feature_flags", "catalog_and_credit", "stars_payment_and_refund", "native_delivery",
                "telegram_mobile_session", "acl_lsm_and_runtime_access")}}


class Parser(argparse.ArgumentParser):
    def error(self, message):
        # argparse normally echoes offending argument values. They are not safe
        # diagnostics here (an operator might mistakenly pass a token/path).
        raise ObservationError("blocked", "invalid_arguments")


def main(argv=None):
    parser = Parser(description=__doc__, allow_abbrev=False)
    parser.add_argument("--expected-revision", required=True, metavar="FULL_40_CHAR_SHA")
    try:
        args = parser.parse_args(argv)
        report = preflight(args.expected_revision)
    except ObservationError as error:
        report = {"scope": "host_metadata_only", **error.result, "acceptance": {"end_to_end": "unknown"}}
        print(json.dumps(report, sort_keys=True, separators=(",", ":")))
        return 2
    except Exception:
        # No exception text, raw command output or process contents escape.
        report = {"scope": "host_metadata_only", "status": "unknown", "code": "observation_failed",
                  "acceptance": {"end_to_end": "unknown"}}
    print(json.dumps(report, sort_keys=True, separators=(",", ":")))
    return {"passed": 0, "blocked": 1, "unknown": 2}[report["status"]]


if __name__ == "__main__":
    raise SystemExit(main())
