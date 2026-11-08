#!/usr/bin/env python3
"""Linux/systemd integration: supervisor gets first chance to seal accounting.
Runs disposable transient units only; never restarts a deployed PRO service.
"""
import pathlib
import subprocess
import tempfile
import time
import uuid

def run():
    with tempfile.TemporaryDirectory(prefix="dark-pro-stop-order-") as directory:
        root = pathlib.Path(directory)
        parent = root / "parent.py"
        parent.write_text("""import subprocess,signal,time,sys,pathlib,os
child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(60)'])
def stop(*args):
 time.sleep(.3)
 pathlib.Path(sys.argv[1]).write_text(str(child.poll() is None))
 child.terminate();child.wait();sys.exit(0)
signal.signal(signal.SIGTERM, signal.SIG_IGN if sys.argv[2]=='hang' else stop)
pathlib.Path(sys.argv[1]+'.ready').write_text(str(child.pid))
while True:time.sleep(.05)
""")
        for mode, behavior in [("control-group", "seal"), ("mixed", "seal"), ("mixed", "hang")]:
            unit = "dark-pro-stop-test-" + uuid.uuid4().hex[:12]
            result = root / unit
            try:
                subprocess.run(["systemd-run", "--quiet", "--collect", "--unit=" + unit,
                                "--property=KillMode=" + mode, "--property=TimeoutStopSec=2",
                                "/usr/bin/python3", str(parent), str(result), behavior], check=True)
                ready = pathlib.Path(str(result) + ".ready")
                for _ in range(100):
                    if ready.exists():
                        break
                    time.sleep(.05)
                assert ready.exists(), "test supervisor did not start"
                child_pid = ready.read_text()
                subprocess.run(["systemctl", "stop", unit], check=True, timeout=10)
                if behavior == "seal":
                    assert result.read_text() == str(mode == "mixed")
                    print(mode, "child available for seal:", result.read_text(), flush=True)
                else:
                    stat = pathlib.Path("/proc") / child_pid / "stat"
                    assert not stat.exists() or stat.read_text().split(") ", 1)[1].startswith("Z"), "hung supervisor left engine alive"
                    print("mixed hung supervisor: final group kill enforced", flush=True)
            finally:
                subprocess.run(["systemctl", "stop", unit], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
if __name__ == "__main__":
    run()

