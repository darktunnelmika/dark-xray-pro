#!/usr/bin/env python3
"""Run an exhaustive, deterministic shard of the business package's race tests."""

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import unicodedata


ROOT = Path(__file__).resolve().parents[2]
PACKAGE = "./internal/business"
IMPORT_PATH = "github.com/darktunnelmika/dark-xray-pro/internal/business"
DISCOVERY_TIMEOUT_SECONDS = 300
SUMMARY = re.compile(
    r"ok\s+" + re.escape(IMPORT_PATH)
    + r"\s+(?:[0-9]+(?:\.[0-9]+)?s|\(cached\))(?:\s+\[no tests to run\])?"
)


class ShardError(ValueError):
    """Discovery or selection could not establish complete coverage."""


@dataclass(frozen=True)
class Discovery:
    names: tuple[str, ...]
    benchmarks: tuple[str, ...]


def name_kind(name: str) -> str | None:
    # Go identifiers permit Unicode letters and decimal digits. A package
    # example may be named exactly Example; bare Test/Fuzz are also valid.
    if not name or any(
        char != "_" and not unicodedata.category(char).startswith("L")
        and unicodedata.category(char) != "Nd" for char in name
    ):
        return None
    for prefix in ("Test", "Example", "Fuzz", "Benchmark"):
        if name.startswith(prefix):
            return prefix
    return None


def parse_discovery(output: str) -> Discovery:
    names, benchmarks, seen = [], [], set()
    summary_seen = False
    for number, line in enumerate(output.splitlines(), 1):
        if not line:
            continue
        if SUMMARY.fullmatch(line):
            if summary_seen:
                raise ShardError("duplicate package summary in discovery")
            summary_seen = True
            continue
        if summary_seen:
            raise ShardError("unexpected output after the package summary")
        kind = name_kind(line)
        if kind is None:
            raise ShardError(f"unknown discovery line {number}: {line[:160]!r}")
        if line in seen:
            raise ShardError(f"duplicate discovered name: {line!r}")
        seen.add(line)
        # Normal `go test -race` does not run benchmarks. They are accounted
        # for explicitly, rather than mistaking them for runnable tests.
        (benchmarks if kind == "Benchmark" else names).append(line)
    if not summary_seen:
        raise ShardError("discovery did not finish with the expected package summary")
    if not names:
        raise ShardError("discovery found no runnable tests, examples or fuzz targets")
    return Discovery(tuple(sorted(names)), tuple(sorted(benchmarks)))


def validate_shard(shard: int, shards: int) -> None:
    if (type(shard) is not int or type(shards) is not int
            or shards < 1 or shard < 1 or shard > shards):
        raise ShardError("require 1 <= --shard <= --shards and positive integers")


def select_shard(names: tuple[str, ...], shard: int, shards: int) -> tuple[str, ...]:
    validate_shard(shard, shards)
    ordered = tuple(sorted(names))
    if len(set(ordered)) != len(ordered) or any(
        name_kind(name) not in ("Test", "Example", "Fuzz") for name in ordered
    ):
        raise ShardError("selection requires unique runnable top-level names")
    selected = ordered[shard - 1::shards]
    if not selected:
        raise ShardError("selected shard is empty; refusing a vacuous success")
    return selected


def selection_pattern(names: tuple[str, ...]) -> str:
    if not names or any(name_kind(name) not in ("Test", "Example", "Fuzz") for name in names):
        raise ShardError("cannot construct a pattern from invalid or empty names")
    # No slash components are added: matching a top-level test retains all
    # its subtests. Anchors prevent a selected name matching a longer name.
    return "^(" + "|".join(re.escape(name) for name in names) + ")$"


def fingerprint(names: tuple[str, ...]) -> str:
    return hashlib.sha256(("\n".join(names) + "\n").encode("utf-8")).hexdigest()


def discover(go: str, env: dict[str, str]) -> Discovery:
    command = [go, "test", "-race", "-list", ".", PACKAGE]
    try:
        result = subprocess.run(
            command, cwd=ROOT, env=env, check=False, capture_output=True,
            text=True, encoding="utf-8", timeout=DISCOVERY_TIMEOUT_SECONDS,
        )
    except (OSError, subprocess.TimeoutExpired, UnicodeError) as exc:
        raise ShardError(f"Go test discovery could not complete: {exc}") from exc
    if result.returncode != 0:
        diagnostic = (result.stderr + "\n" + result.stdout).strip()[-4000:]
        raise ShardError(f"Go test discovery exited {result.returncode}: {diagnostic}")
    if result.stderr:
        print(result.stderr, end="" if result.stderr.endswith("\n") else "\n", file=sys.stderr)
    return parse_discovery(result.stdout)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--shard", required=True, type=int, help="one-based shard index")
    parser.add_argument("--shards", required=True, type=int, help="total number of shards")
    parser.add_argument("--go", default="go", help="pinned Go executable (one argv element)")
    parser.add_argument("--dry-run", action="store_true", help="discover and print the JSON execution plan only")
    args = parser.parse_args(argv)
    try:
        validate_shard(args.shard, args.shards)
        if not args.go or args.go.isspace() or "\0" in args.go:
            raise ShardError("--go must name a Go executable")
        env = dict(os.environ)
        # Use the workflow-selected binary; never silently download or switch
        # to another toolchain. Preserve build tags and other build settings.
        env["GOTOOLCHAIN"] = "local"
        inventory = discover(args.go, env)
        selected = select_shard(inventory.names, args.shard, args.shards)
        command = [
            args.go, "test", "-race", "-count=1", "-timeout=18m",
            "-short=false", "-skip=^$", "-bench=^$", "-fuzz=", "-list=",
            "-run", selection_pattern(selected), PACKAGE,
        ]
        plan = {
            "package": PACKAGE, "shard": args.shard, "shards": args.shards,
            "total": len(inventory.names), "selected": len(selected),
            "excluded_benchmarks": len(inventory.benchmarks),
            "inventory_sha256": fingerprint(inventory.names),
            "selection_sha256": fingerprint(selected),
        }
        if args.dry_run:
            plan.update(selected_names=list(selected), command=command)
        print(json.dumps(plan, ensure_ascii=False, sort_keys=True), flush=True)
        if args.dry_run:
            return 0
        try:
            result = subprocess.run(command, cwd=ROOT, env=env, check=False)
        except OSError as exc:
            raise ShardError(f"Go race test command could not start: {exc}") from exc
        return result.returncode if result.returncode >= 0 else 128 - result.returncode
    except ShardError as exc:
        print(f"business race shard: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
