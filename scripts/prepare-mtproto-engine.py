#!/usr/bin/env python3
"""Create the owned MTProto adapter in an exact, clean upstream checkout."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

PIN = "a45ebefed80e16949426381ef53fb5f1c1e4958d"
if len(sys.argv) != 2:
    raise SystemExit("usage: prepare-mtproto-engine.py PINNED_MTG_CHECKOUT")
source = Path(sys.argv[1]).resolve()
repo = Path(__file__).resolve().parent.parent
if subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip() != PIN:
    raise SystemExit("wrong mtg-multi source revision")
if subprocess.check_output(["git", "-C", str(source), "status", "--porcelain", "--untracked-files=all"], text=True).strip():
    raise SystemExit("upstream checkout must be clean")
overlay = repo / "scripts/engine-overlay/mtproto"
target = source / "cmd/dark-pro-mtg"
target.mkdir(parents=True)
files = {"main.go.txt": target / "main.go", "network.go.txt": target / "network.go", "acceptance_test.go.txt": target / "acceptance_test.go", "darkpro.go.txt": source / "mtglib/darkpro.go", "README.md": source / "DARKPRO-MTPROTO.md"}
digests = {}
for name, destination in files.items():
    raw = (overlay / name).read_bytes()
    digests[name] = hashlib.sha256(raw).hexdigest()
    destination.write_bytes(raw)
original = subprocess.check_output(["git", "-C", str(source), "ls-files", "-z"]).decode().split("\0")
source_digests = {name: hashlib.sha256((source / name).read_bytes()).hexdigest() for name in original if name and (source / name).is_file()}
provenance = {"engine": "mtproto", "upstream": "https://github.com/MHSanaei/mtg-multi", "commit": PIN, "license": "MIT", "go": "1.27.3", "upstream_source_sha256": source_digests, "overlay_sha256": digests, "control_protocol": "dark-pro-adapter-v1"}
(source / "darkpro-mtproto-provenance.json").write_text(json.dumps(provenance, indent=2) + "\n")
print("Prepared standalone MTProto worker; no service installed")
