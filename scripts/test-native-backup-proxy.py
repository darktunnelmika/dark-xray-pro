#!/usr/bin/env python3
"""Exercise the owner backup allowlist through a disposable real nginx server."""
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
        self.seen.append((self.command, self.path, self.headers.get("Host")))
        self.send_response(401)
        self.end_headers()

    do_POST = do_HEAD = do_PUT = do_PATCH = do_DELETE = do_OPTIONS = do_GET

    def log_message(self, *_args):
        pass


def request(port, path, method, body=None):
    req = urllib.request.Request(f"http://127.0.0.1:{port}{path}", method=method,
                                 headers={"Host": "lab-proxy.example"}, data=body)
    try:
        with urllib.request.urlopen(req, timeout=2) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code


def main():
    nginx = shutil.which("nginx")
    if not nginx and pathlib.Path("/usr/sbin/nginx").is_file():
        nginx = "/usr/sbin/nginx"
    if not nginx:
        raise SystemExit("nginx is required; no backup proxy acceptance was performed")
    root = pathlib.Path(__file__).resolve().parent.parent
    backend = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Backend)
    threading.Thread(target=backend.serve_forever, daemon=True).start()
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    try:
        with tempfile.TemporaryDirectory(prefix="dark-pro-backup-nginx-") as directory:
            tmp = pathlib.Path(directory)
            snippet = (root / "ops/nginx/native-backups.conf").read_text()
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
access_log off;
server {{
listen 127.0.0.1:{port};
server_name lab-proxy.example;
proxy_set_header Host $host;
{snippet}
location / {{ return 403; }}
}} }}
""")
            process = subprocess.Popen([nginx, "-p", directory + "/", "-c", str(conf)])
            try:
                for _ in range(200):
                    try:
                        assert request(port, "/", "GET") == 403
                        break
                    except urllib.error.URLError:
                        if process.poll() is not None:
                            raise RuntimeError((tmp / "error.log").read_text())
                        time.sleep(.05)
                else:
                    raise RuntimeError("nginx did not start: " + (tmp / "error.log").read_text()[-4096:])

                methods = ("GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS")

                def check(path, allowed):
                    for method in methods:
                        before = len(Backend.seen)
                        status = request(port, path, method)
                        assert status == (401 if method in allowed else 403), (path, method, status)
                        assert len(Backend.seen) == before + (method in allowed), (path, method)
                        if method in allowed:
                            assert Backend.seen[-1] == (method, path, "lab-proxy.example"), Backend.seen[-1]

                base = "/api/v1/workspace/owner/backups"
                job = base + "/jobs/" + "a" * 32
                archive = base + "/archives/" + "b" * 32
                for path, allowed in ((base, {"GET", "HEAD"}),
                                      (base + "/jobs", {"POST"}),
                                      (base + "/settings", {"PUT"}),
                                      (job, {"GET", "HEAD"}),
                                      (job + "/cancel", {"POST"}),
                                      (archive + "/download", {"GET", "HEAD"})):
                    check(path, allowed)
                    check(path + "/", set())
                    check(path + "/extra", set())
                for path, method in ((base + "/jobs", "POST"),
                                     (base + "/settings", "PUT"),
                                     (job + "/cancel", "POST")):
                    before = len(Backend.seen)
                    assert request(port, path, method, b"x" * 4097) == 413
                    assert len(Backend.seen) == before, "oversized backup request reached backend"
                for bad in ("a" * 31, "a" * 33, "A" * 32, "g" * 32,
                            "_" * 32, "a%2Fb", "a+b", "a;b"):
                    check(base + "/jobs/" + bad, set())
                    check(base + "/jobs/" + bad + "/cancel", set())
                    check(base + "/archives/" + bad + "/download", set())
                for path in (base + "/upload", base + "/restore", base + "/verify",
                             base + "/archives", archive, archive + "/delete",
                             job + "/retry", job + "/execute", "/v1/status",
                             "/v1/jobs", "/api/v1/admin/backups",
                             "/api/v1/agent/backups", "/api/v1/workspace/backups",
                             "/api/v1/workspace/representative/backups"):
                    check(path, set())
                print("NATIVE_BACKUP_PROXY_PASS: exact routes, methods, IDs, body limits, Host and private-route denial")
            finally:
                process.terminate()
                process.wait(timeout=5)
    finally:
        backend.shutdown()
        backend.server_close()


if __name__ == "__main__":
    main()
