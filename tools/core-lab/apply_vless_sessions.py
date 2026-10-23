#!/usr/bin/env python3
"""Apply the reviewable VLESS session-lifecycle experiment to a CLEAN QA clone.
No download, build, service operation, dependency change or production install.
The modified Xray files remain MPL-2.0. This does NOT certify a core for release.
"""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

UPSTREAM = "52a412d9e2f5c2a5142b1b4e2ab3771dacb8b120"


def apply(source: Path) -> None:
    if os.geteuid() == 0:
        raise ValueError("run as an unprivileged QA user")
    if not source.is_absolute() or source.is_symlink() or source.resolve() != source:
        raise ValueError("absolute, non-symlink QA clone required")
    def git(*args: str) -> bytes:
        return subprocess.check_output(["git", "-c", "core.hooksPath=/dev/null", "-C", str(source), *args])
    if git("rev-parse", "HEAD").decode().strip() != UPSTREAM:
        raise ValueError("unreviewed Xray source revision")
    if git("status", "--porcelain", "--untracked-files=all"):
        raise ValueError("input source is not clean; no overwrite permitted")
    path = source / "proxy/vless/inbound/inbound.go"
    original = git("show", "HEAD:proxy/vless/inbound/inbound.go")
    if path.is_symlink() or path.read_bytes() != original:
        raise ValueError("upstream handler does not match locked source")
    text = original.decode("utf-8")
    changes = [
        ('\t"strings"\n', '\t"strings"\n\t"sync"\n'),
        ('type Handler struct {\n', 'type Handler struct {\n\tproUserMu sync.Mutex\n\tproUserClosed bool\n\tproUserSessions map[*protocol.MemoryUser]map[*proUserSession]struct{}\n'),
        ('func (h *Handler) Close() error {\n', 'func (h *Handler) Close() error {\n\th.stopUserSessions()\n'),
        ('\treturn h.validator.Add(u)\n', '\th.proUserMu.Lock()\n\tdefer h.proUserMu.Unlock()\n\tif h.proUserClosed || u == nil { return errors.New("inbound is closed or user invalid") }\n\tcopy := *u // Every Add creates a fresh post-authentication generation.\n\treturn h.validator.Add(&copy)\n'),
        ('\th.RemoveReverse(h.validator.GetByEmail(e))\n\treturn h.validator.Del(e)\n', '\treturn h.removeTrackedUser(e)\n'),
        ('\tinbound := session.InboundFromContext(ctx)\n', '\tctx, releaseUser, err := h.trackUserSession(ctx, request.User, connection)\n\tif err != nil { return err }\n\tdefer releaseUser()\n\n\tinbound := session.InboundFromContext(ctx)\n'),
    ]
    for old, new in changes:
        if text.count(old) != 1:
            raise ValueError("patch anchor mismatch; source left unchanged")
        text = text.replace(old, new, 1)
    proxy_path = source / "proxy/proxy.go"
    proxy_original = git("show", "HEAD:proxy/proxy.go")
    if proxy_path.is_symlink() or proxy_path.read_bytes() != proxy_original:
        raise ValueError("upstream raw-copy source mismatch")
    proxy_text = proxy_original.decode("utf-8")
    raw_anchor = "\t\t\tw, err := tc.ReadFrom(readerConn)\n"
    raw_change = """\t\t\tif statWriter != nil && statWriter.Counter != nil {
\t\t\t\treturn proCopyMeteredRaw(ctx, tc, readerConn, func(w int64) {
\t\t\t\t\tif readCounter != nil { readCounter.Add(w) }
\t\t\t\t\tif writeCounter != nil { writeCounter.Add(w) }
\t\t\t\t\tstatWriter.Counter.Add(w)
\t\t\t\t\ttimer.Update()
\t\t\t\t\tif inTimer != nil { inTimer.Update() }
\t\t\t\t})
\t\t\t}
""" + raw_anchor
    if proxy_text.count(raw_anchor) != 1:
        raise ValueError("raw-copy anchor mismatch")
    proxy_text = proxy_text.replace(raw_anchor, raw_change, 1)
    assets = Path(__file__).resolve().parent
    writes: dict[Path, bytes] = {path: text.encode(), proxy_path: proxy_text.encode()}
    for name in ("vless_sessions.go", "vless_sessions_test.go", "metered_splice.go", "metered_splice_test.go"):
        parent = path.parent if name.startswith("vless_") else proxy_path.parent
        target = parent / ("pro_" + name)
        asset = assets / (name + ".inc")
        if target.exists() or target.is_symlink() or asset.is_symlink():
            raise ValueError("overlay input/output already exists or is indirect")
        writes[target] = asset.read_bytes()
    # All validation precedes any mutation. A failed partial write leaves a dirty
    # QA tree which a repeated invocation rejects, rather than hiding the failure.
    for target, data in writes.items():
        if target not in (path, proxy_path):
            with target.open("xb") as f:
                f.write(data)
        else:
            with target.open("wb") as f:
                f.write(data)
    print(json.dumps({"upstream_commit": UPSTREAM, "profile": "lab-vless-session-close-metered-splice-v1",
        "production_certified": False, "changed": {
            str(p.relative_to(source)): hashlib.sha256(raw).hexdigest() for p, raw in writes.items()
        }}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--lab-only", action="store_true", required=True)
    args = parser.parse_args()
    try:
        apply(args.source)
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        parser.exit(1, f"Core overlay refused: {exc}\n")
