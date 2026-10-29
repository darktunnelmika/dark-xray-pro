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
                upload = base + "/uploads/" + "c" * 32
                for path, allowed in ((base, {"GET", "HEAD"}),
                                      (base + "/jobs", {"POST"}),
                                      (base + "/settings", {"PUT"}),
                                      (job, {"GET", "HEAD"}),
                                      (job + "/cancel", {"POST"}),
                                      (archive + "/download", {"GET", "HEAD"}),
                                      (base + "/uploads", {"GET", "HEAD", "POST"}),
                                      (upload, {"GET", "HEAD"}),
                                      (upload + "/content", {"PUT"}),
                                      (upload + "/cancel", {"POST"}),
                                      (upload + "/remove", {"POST"}),
                                      (upload + "/download", {"GET", "HEAD"})):
                    check(path, allowed)
                    check(path + "/", set())
                    check(path + "/extra", set())
                for path, method in ((base + "/jobs", "POST"),
                                     (base + "/settings", "PUT"),
                                     (job + "/cancel", "POST"),
                                     (base + "/uploads", "POST"),
                                     (upload + "/cancel", "POST"),
                                     (upload + "/remove", "POST")):
                    before = len(Backend.seen)
                    assert request(port, path, method, b"x" * 4097) == 413
                    assert len(Backend.seen) == before, "oversized backup request reached backend"
                for bad in ("a" * 31, "a" * 33, "A" * 32, "g" * 32,
                            "_" * 32, "a%2Fb", "a+b", "a;b"):
                    check(base + "/jobs/" + bad, set())
                    check(base + "/jobs/" + bad + "/cancel", set())
                    check(base + "/archives/" + bad + "/download", set())
                    check(base + "/uploads/" + bad, set())
                    for action in ("content", "cancel", "remove", "download"):
                        check(base + "/uploads/" + bad + "/" + action, set())
                for path in (base + "/upload", base + "/restore", base + "/verify",
                             base + "/archives", archive, archive + "/delete",
                             job + "/retry", job + "/execute", "/v1/status",
                             "/v1/jobs", "/api/v1/admin/backups",
                             "/api/v1/agent/backups", "/api/v1/workspace/backups",
                             "/api/v1/workspace/representative/backups"):
                    check(path, set())
                # Send only headers for a large upload. Authentication must
                # reach HUB and return before nginx reads/spools the body.
                # A buffered location would hang here waiting for all bytes.
                for size, expected in ((3 << 30, 401), ((66 << 30) + 1, 413)):
                    before = len(Backend.seen)
                    with socket.create_connection(("127.0.0.1", port), timeout=2) as peer:
                        peer.settimeout(2)
                        peer.sendall((f"PUT {upload}/content HTTP/1.1\r\n"
                                      "Host: lab-proxy.example\r\n"
                                      "Content-Type: application/octet-stream\r\n"
                                      f"Content-Length: {size}\r\n"
                                      "Connection: close\r\n\r\n").encode())
                        header = peer.recv(4096)
                        assert int(header.split(b" ", 2)[1]) == expected, header
                    assert len(Backend.seen) == before + (expected == 401)
                assert not list((tmp / "client-body").glob("**/*")), "upload body was spooled before authentication"
                print("NATIVE_BACKUP_PROXY_PASS: exact routes, methods, IDs, limits, Host, private-route denial and unbuffered upload authentication")
            finally:
                process.terminate()
                process.wait(timeout=5)
    finally:
        backend.shutdown()
        backend.server_close()


if __name__ == "__main__":
    main()
