#!/usr/bin/env python3
"""Exercise the shipped allowlist through real nginx, without a real login."""
import http.server
import pathlib
import shutil
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request


class Backend(http.server.BaseHTTPRequestHandler):
    seen = []

    def do_GET(self):
        self.seen.append((self.command, self.path))
        self.send_response(401)
        self.end_headers()

    do_POST = do_GET
    do_HEAD = do_GET

    def log_message(self, *_args):
        pass


def request(port, path, method):
    try:
        with urllib.request.urlopen(urllib.request.Request(
                f"http://127.0.0.1:{port}{path}", method=method), timeout=2) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code


def main():
    nginx = shutil.which("nginx") or "/usr/sbin/nginx"
    root = pathlib.Path(__file__).resolve().parent.parent
    backend = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Backend)
    worker = threading.Thread(target=backend.serve_forever, daemon=True)
    worker.start()
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    try:
        with tempfile.TemporaryDirectory(prefix="dark-pro-nginx-") as directory:
            tmp = pathlib.Path(directory)
            snippet = (root / "ops/nginx/node-manager.conf").read_text()
            snippet = snippet.replace("127.0.0.1:2087", f"127.0.0.1:{backend.server_port}")
            conf = tmp / "nginx.conf"
            conf.write_text(f"""daemon off;
pid {tmp}/nginx.pid;
error_log {tmp}/error.log;
events {{ worker_connections 32; }}
http {{
client_body_temp_path {tmp}/client-body;
proxy_temp_path {tmp}/proxy;
fastcgi_temp_path {tmp}/fastcgi;
uwsgi_temp_path {tmp}/uwsgi;
scgi_temp_path {tmp}/scgi;
access_log off; server {{
listen 127.0.0.1:{port};
{snippet}
location / {{ return 403; }}
}} }}
""")
            process = subprocess.Popen([nginx, "-p", directory + "/", "-c", str(conf)])
            try:
                for _ in range(50):
                    try:
                        assert request(port, "/", "GET") == 403
                        break
                    except urllib.error.URLError:
                        if process.poll() is not None:
                            raise RuntimeError((tmp / "error.log").read_text())
                        time.sleep(.05)
                else:
                    raise RuntimeError("nginx did not start")
                for endpoint, allowed in (("name", "GET"), ("dependencies", "GET"), ("rename", "POST"),
                                          ("replacements", "GET"), ("replacements/" + "a" * 32 + "/cancel", "POST")):
                    path = "/api/v1/workspace/nodes/lab-test_01/" + endpoint
                    for method in ("GET", "POST", "PUT", "DELETE", "PATCH"):
                        before = len(Backend.seen)
                        status = request(port, path, method)
                        assert status == (401 if method == allowed else 403), (path, method, status)
                        assert len(Backend.seen) == before + (method == allowed)
                metered = "/api/v1/workspace/native-nodes/lab-test_01/metered-route-transactions"
                promotion = "/api/v1/workspace/native-inbound-plans/8123/promotion"
                handovers = "/api/v1/workspace/metered-handovers"
                for path, allowed in ((promotion, {"GET", "POST"}),
                                      (promotion + "/preview", {"POST"}),
                                      ("/api/v1/workspace/metered-route-transactions", {"GET"}),
                                      (metered, {"GET", "POST"}),
                                      (handovers, {"GET", "POST"}),
                                      (handovers + "/" + "a" * 32, {"GET"}),
                                      (handovers + "/" + "a" * 32 + "/commands", {"POST"}),
                                      (handovers + "/" + "a" * 32 + "/cancel", {"POST"}),
                                      (handovers + "/" + "a" * 32 + "/health", {"POST"}),
                                      (handovers + "/" + "a" * 32 + "/health-rounds", {"POST"}),
                                      (handovers + "/" + "a" * 32 + "/consumer-manifest", {"GET", "POST"}),
                                      (handovers + "/" + "a" * 32 + "/consumer-health-rounds", {"POST"}),
                                      (handovers + "/" + "a" * 32 + "/lab-subscription-snapshot", {"POST"}),
                                      ("/api/v1/workspace/clients/synthetic-a/subscription", {"GET"}),
                                      *(("/api/v1/workspace/clients/synthetic-a/subscription/" + action, {"POST"})
                                        for action in ("issue", "reveal", "rotate", "revoke")),
                                      (metered + "/" + "a" * 32 + "/abandon", {"POST"})):
                    for method in ("GET", "POST", "PUT", "DELETE", "PATCH"):
                        before = len(Backend.seen)
                        status = request(port, path, method)
                        assert status == (401 if method in allowed else 403), (path, method, status)
                        assert len(Backend.seen) == before + (method in allowed)
                consumer = "/api/v1/subscriptions/" + "a" * 43
                for method in ("GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"):
                    before = len(Backend.seen)
                    assert request(port, consumer + "?format=raw", method) == (401 if method == "GET" else 405)
                    assert len(Backend.seen) == before + (method == "GET")
                for path in ("/api/v1/agent/config", "/api/v1/admin/nodes", "/api/v1/workspace/nodes/a/delete",
                             "/api/v1/workspace/nodes/a/name/extra", "/api/v1/workspace/nodes/a.b/name",
                             "/api/v1/workspace/nodes/a/b/dependencies",
                             "/api/v1/workspace/nodes/a/replacements/abc/cancel",
                             "/api/v1/workspace/nodes/a/replacements/" + "a" * 32 + "/activate",
                             "/api/v1/workspace/nodes/a/replacements/" + "a" * 32 + "/cancel/extra",
                             "/api/v1/workspace/metered-route-transactions/enable",
                             promotion + "/enable", promotion + "/preview/extra",
                             promotion + "/activate", promotion + "/extra",
                             "/api/v1/workspace/native-inbound-plans/0/promotion",
                             "/api/v1/workspace/native-inbound-plans/08123/promotion",
                             "/api/v1/workspace/native-inbound-plans/-1/promotion",
                             "/api/v1/workspace/native-inbound-plans/1e3/promotion",
                             "/api/v1/workspace/native-inbound-plans/10000000000000000/promotion",
                             metered + "/apply", metered + "/abc/abandon",
                             handovers + "/enable", handovers + "/abc/commands",
                             handovers + "/" + "a" * 32 + "/apply",
                             handovers + "/" + "a" * 32 + "/cancel/extra",
                             handovers + "/" + "a" * 32 + "/health/extra",
                             handovers + "/" + "a" * 32 + "/health-rounds/extra",
                             handovers + "/" + "A" * 32 + "/health-rounds",
                             handovers + "/" + "a" * 32 + "/health-round",
                             handovers + "/abc/health-rounds",
                             handovers + "/" + "a" * 32 + "/consumer-manifest/extra",
                             handovers + "/" + "a" * 32 + "/consumer-manifests",
                             handovers + "/" + "A" * 32 + "/consumer-manifest",
                             handovers + "/abc/consumer-manifest",
                             handovers + "/" + "a" * 32 + "/consumer-health-rounds/extra",
                             handovers + "/" + "a" * 32 + "/consumer-health-round",
                             handovers + "/" + "A" * 32 + "/consumer-health-rounds",
                             handovers + "/abc/consumer-health-rounds",
                             handovers + "/" + "a" * 32 + "/lab-subscription-snapshot/extra",
                             "/api/v1/workspace/clients/a/subscription/enable",
                             "/api/v1/workspace/clients/a/subscription/reveal/extra",
                             consumer + "/extra", consumer[:-1], consumer + "a",
                             "/api/v1/subscriptions/enable",
                             metered + "/" + "a" * 32 + "/abandon/extra"):
                    for method in ("GET", "POST"):
                        before = len(Backend.seen)
                        assert request(port, path, method) == 403, (path, method)
                        assert len(Backend.seen) == before, (path, method)
                print("NODE_MANAGER_PROXY_PASS: exact paths/methods, auth forwarding, private routes denied")
            finally:
                process.terminate()
                process.wait(timeout=5)
    finally:
        backend.shutdown()
        backend.server_close()


if __name__ == "__main__":
    main()
