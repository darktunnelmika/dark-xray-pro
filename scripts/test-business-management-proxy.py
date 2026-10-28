#!/usr/bin/env python3
"""Check the business allowlist with disposable nginx and a fake auth backend."""
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


def request(port, path, method):
    req = urllib.request.Request(f"http://127.0.0.1:{port}{path}", method=method,
                                 headers={"Host": "lab-proxy.example"})
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
        raise SystemExit("nginx is required; no proxy acceptance was performed")
    root = pathlib.Path(__file__).resolve().parent.parent
    backend = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Backend)
    threading.Thread(target=backend.serve_forever, daemon=True).start()
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    try:
        with tempfile.TemporaryDirectory(prefix="dark-pro-business-nginx-") as directory:
            tmp = pathlib.Path(directory)
            snippet = (root / "ops/nginx/business-management.conf").read_text()
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

                methods = ("GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS")
                def check(path, allowed):
                    for method in methods:
                        before = len(Backend.seen)
                        status = request(port, path, method)
                        assert status == (401 if method in allowed else 403), (path, method, status)
                        assert len(Backend.seen) == before + (method in allowed), (path, method)
                        if method in allowed:
                            assert Backend.seen[-1] == (method, path, "lab-proxy.example"), Backend.seen[-1]

                rep = "/api/v1/workspace/representatives/Rep.test_01-2"
                seller = rep + "/sellers/Seller.test_01-2"
                routes = ((rep + "/seller-permission", {"GET", "HEAD", "POST"}),
                          (rep + "/sellers", {"GET", "HEAD", "POST"}),
                          (seller, {"GET", "HEAD"}),
                          (seller + "/quota", {"POST"}),
                          (seller + "/state", {"POST"}),
                          ("/api/v1/workspace/clients/Client.test_01-2/volume-refund", {"GET", "HEAD"}),
                          ("/api/v1/workspace/volume-refunds", {"GET", "HEAD"}))
                for path, allowed in routes:
                    check(path, allowed)
                    check(path + "/extra/unknown", set())
                    check(path + "/", set())
                check(rep + "/sellers?limit=25&offset=0", {"GET", "HEAD", "POST"})
                check("/api/v1/workspace/volume-refunds?limit=25&offset=0", {"GET", "HEAD"})
                check("/api/v1/workspace/clients/" + "a" * 64 + "/volume-refund", {"GET", "HEAD"})
                check("/api/v1/workspace/representatives/" + "a" * 64 + "/sellers/" + "b" * 64,
                      {"GET", "HEAD"})
                for bad in ("_a", "-a", ".a", "a!b", "a+b", "a;b", "a" * 65, "a%2Fb"):
                    check("/api/v1/workspace/representatives/" + bad + "/seller-permission", set())
                    check(rep + "/sellers/" + bad + "/quota", set())
                    check("/api/v1/workspace/clients/" + bad + "/volume-refund", set())
                for path in ("/api/v1/admin/nodes", "/api/v1/agent/config",
                             "/api/v1/agent/volume-refunds", "/api/v1/admin/volume-refunds",
                             "/api/v1/workspace/volume-refund", "/api/v1/workspace/volume-refunds/enable",
                             rep + "/seller-permissions", seller + "/delete", seller + "/quota/execute",
                             "/api/v1/workspace/clients/a/volume-refunds"):
                    check(path, set())
                print("BUSINESS_MANAGEMENT_PROXY_PASS: exact routes, methods, IDs, Host and private-route denial")
            finally:
                process.terminate()
                process.wait(timeout=5)
    finally:
        backend.shutdown()
        backend.server_close()


if __name__ == "__main__":
    main()
