#!/usr/bin/env python3
"""Prepare isolated, source-complete TUIC/AWG workers from exact upstream pins.

Usage: prepare-sanaei-engines.py PINNED_SANAEI_CHECKOUT OUTPUT_DIRECTORY
The input stays clean. OUTPUT_DIRECTORY must not already exist. The resulting
module is self-contained for the modified QUIC fork and remains GPL-3.0.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile

PIN = "608ef4e4010bf83e2e78328e08f77efc051ad4d7"
QUIC_MODULE = "github.com/apernet/quic-go"
QUIC_VERSION = "v0.61.1-0.20260806010916-184d081eef3e"

if len(sys.argv) != 3:
    raise SystemExit(__doc__)
source, output = (Path(v).resolve() for v in sys.argv[1:])
repo = Path(__file__).resolve().parent.parent
if subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"], text=True).strip() != PIN:
    raise SystemExit("wrong Sanaei source revision")
if subprocess.check_output(["git", "-C", str(source), "status", "--porcelain", "--untracked-files=all"], text=True).strip():
    raise SystemExit("upstream checkout must be clean")
if output.exists():
    raise SystemExit("output already exists; select a new build directory")
go = os.environ.get("GO", "go")
quic = json.loads(subprocess.check_output([go, "mod", "download", "-json", QUIC_MODULE + "@" + QUIC_VERSION], cwd=source, text=True))
if "Error" in quic or not quic.get("Dir"):
    raise SystemExit("pinned QUIC source unavailable")
output.mkdir(parents=True)
with tempfile.TemporaryFile() as archive:
    subprocess.run(["git", "-C", str(source), "archive", PIN], stdout=archive, check=True)
    archive.seek(0)
    with tarfile.open(fileobj=archive) as tree:
        tree.extractall(output, filter="data")
quic_output = output / "_darkpro/quic-go"
shutil.copytree(quic["Dir"], quic_output)
for file in quic_output.rglob("*"):
    if file.is_file():
        file.chmod(0o644)

overlay = repo / "scripts/engine-overlay"
digests = {}
def install(directory, destination, excluded=()):
    destination.mkdir(parents=True, exist_ok=True)
    for file in sorted((overlay / directory).glob("*.go.txt")):
        if file.name in excluded:
            continue
        raw = file.read_bytes()
        digests[directory + "/" + file.name] = hashlib.sha256(raw).hexdigest()
        (destination / file.name.removesuffix(".txt")).write_bytes(raw)

install("sanaei-common", output / "internal/darkproadapter")
install("sanaei-test", output / "internal/darkprotest")
install("tuic", output / "cmd/darkpro-tuic", ("darkpro.go.txt", "quic_cubic.go.txt", "quic_ack_cubic.go.txt", "quic_ack_cubic_test.go.txt"))
install("amneziawg", output / "cmd/darkpro-amneziawg", ("darkpro.go.txt", "darkpro_udp.go.txt"))
for name, destination in {
    "tuic/darkpro.go.txt": output / "internal/tuic/darkpro.go",
    "tuic/quic_cubic.go.txt": quic_output / "darkpro_cubic.go",
    "tuic/quic_ack_cubic.go.txt": quic_output / "internal/ackhandler/darkpro_cubic.go",
    "amneziawg/darkpro.go.txt": output / "internal/amneziawgnet/darkpro.go",
    "amneziawg/darkpro_udp.go.txt": output / "internal/amneziawgnet/darkpro_udp.go",
}.items():
    raw = (overlay / name).read_bytes()
    digests[name] = hashlib.sha256(raw).hexdigest()
    destination.write_bytes(raw)
if (overlay / "tuic/quic_ack_cubic_test.go.txt").exists():
    raw = (overlay / "tuic/quic_ack_cubic_test.go.txt").read_bytes()
    digests["tuic/quic_ack_cubic_test.go.txt"] = hashlib.sha256(raw).hexdigest()
    (quic_output / "internal/ackhandler/darkpro_cubic_test.go").write_bytes(raw)

def replace(path, before, after, count=1):
    raw = path.read_text()
    if raw.count(before) != count:
        raise SystemExit("pinned overlay anchor changed: " + str(path.relative_to(output)))
    path.write_text(raw.replace(before, after))

replace(output / "internal/tuic/server.go", 's.logf(tuicLogDebug, "cubic is not available; applied new_reno congestion controller")', 'if !conn.DarkProUseCubic() { _ = conn.CloseWithError(0x100, "congestion unavailable"); return }; s.logf(tuicLogDebug, "applied cubic congestion controller")')
handler = quic_output / "internal/ackhandler/sent_packet_handler.go"
replace(handler, "congestionMutex sync.RWMutex", "congestionMutex sync.RWMutex\n\tdarkProCubic bool")
replace(handler, "\t\tinitialMaxDatagramSize,\n\t\ttrue, // use Reno\n\t\th.qlogger,", "\t\tinitialMaxDatagramSize,\n\t\t!h.darkProCubic, // preserve the selected native CUBIC controller\n\t\th.qlogger,")
# Path migration otherwise races the explicit controller setter in the fork.
replace(handler, "\th.congestion = congestion.NewCubicSender(\n", "\th.congestionMutex.Lock()\n\th.congestion = congestion.NewCubicSender(\n")
replace(handler, "\th.setLossDetectionTimer(now)\n}\n\nfunc (h *sentPacketHandler) getCongestionControl", "\th.congestionMutex.Unlock()\n\th.setLossDetectionTimer(now)\n}\n\nfunc (h *sentPacketHandler) getCongestionControl")
subprocess.run([go, "mod", "edit", "-replace=" + QUIC_MODULE + "=./_darkpro/quic-go"], cwd=output, check=True)
subprocess.run([str(Path(go).parent / "gofmt") if "/" in go else "gofmt", "-w", "cmd/darkpro-tuic", "cmd/darkpro-amneziawg", "internal/darkproadapter", "internal/tuic/darkpro.go", "internal/tuic/server.go", "internal/amneziawgnet/darkpro.go", "internal/amneziawgnet/darkpro_udp.go", "_darkpro/quic-go/darkpro_cubic.go", "_darkpro/quic-go/internal/ackhandler"], cwd=output, check=True)
provenance = {"engines": ["tuic", "amneziawg"], "upstream": "https://github.com/MHSanaei/3x-ui", "commit": PIN, "license": "GPL-3.0-or-later", "quic_module": QUIC_MODULE, "quic_version": QUIC_VERSION, "quic_sum": quic.get("Sum"), "quic_license": "MIT", "overlay_sha256": digests, "control_protocol": "dark-pro-adapter-v1", "toolchain": subprocess.check_output([go, "version"], text=True).strip()}
(output / "darkpro-sanaei-provenance.json").write_text(json.dumps(provenance, indent=2) + "\n")
print("Prepared isolated TUIC/AWG source tree: " + str(output))
