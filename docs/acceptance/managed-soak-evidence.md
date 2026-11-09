# Disposable managed soak evidence

The acceptance fixture now records exact wall-clock bounds for its monotonic
traffic window, followed by the actual final Usage request and HUB receipt.
The request, canonical request and response are base64 encoded so their original
bytes survive log transport. The witness checks the seal, receipt hash, epoch,
sequence, generation, counters and zero holds before emitting evidence. It never
creates a receipt, clears a hold or changes production runtime code.

`run_managed_soak.py` is a separate collector for NEW isolated runs. It requires
explicit test/core SHA256 pins, a new evidence directory and a short, private
financial TMPDIR on non-tmpfs storage. It does not mount filesystems, restart
services, open installed ledgers or reuse the old LAB runner.

- `result.json` retains the old all-lifetime process-counting outcome.
- `verification.json` keeps that full result and a separate traffic-window verdict.
- `processes.jsonl` records independent PID/start-tick/executable/argv samples.
- `soak.log` contains the actual final HTTP evidence after successful settlement.
- `sync.trace.*` retain the durable-write timings.

The verifier requires one native process matching the final report throughout
the sampled traffic window. A preparation process does not count as a traffic
restart. Missing samples, large gaps, wrong receipts, mismatched counters, other
raw failures, clock inconsistencies and missing final evidence fail closed.
It does not infer or synthesize a receipt for historical logs.

Run `python3 scripts/ci/run_managed_soak.py --help` for the required explicit
paths/pins. Default duration is one hour. CI explicitly uses 15 seconds and must
not be described as one-hour acceptance. Re-read an existing NEW run with:

    python3 scripts/ci/verify_managed_soak.py /path/to/evidence --seconds 3600

This verifier only prints a separate result. It never writes existing evidence.
Source-service identity checks remain an independent LAB acceptance obligation;
this generic fixture collector does not claim to verify installed services.

## Historical boundaries

The original same-HUB control at ad3afd940b631578b0e0faaffbd69bdb2d7f6a0c
completed a 3,600,826 ms window with 99,221,504 bytes and internal PASS. Its raw
runner FAIL (two native identities from preparation onward) remains intact.
That run did not retain the final HTTP bodies or exact window timestamps; this
new verifier must not retroactively promote it. The earlier 778-second HUB
failure with a 30.608-second journal fsync and the independent Finland PASS also
remain separate outcomes. None accepts existing Direct/Tunnel/Swap routes or
resolves the original client's historical hold.

## Development validation, 2026-11-09

Receipt unit scenarios run under the Go race detector; Python regressions cover
preparation versus traffic identities and fail-closed evidence validation.
In the development sandbox, syscall tracing is forbidden (PTRACE_TRACEME).
An untraced native smoke also failed before the traffic window with
`inbound_process_identity_mismatch`; the exact original, SHA-verified ad3 test
binary reproduced the same failure. These are retained as failed development
smokes, not claimed as integration passes. Native integration remains a CI/LAB
gate; no original LAB service, fixture, journal or outcome was modified.
