#!/usr/bin/env python3
"""Real disposable nginx acceptance for the Telegram panel's exact routes."""
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
        raise SystemExit("nginx is required; no Telegram proxy acceptance was performed")
    repo = pathlib.Path(__file__).resolve().parent.parent
    backend = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Backend)
    threading.Thread(target=backend.serve_forever, daemon=True).start()
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    try:
        with tempfile.TemporaryDirectory(prefix="dark-pro-telegram-nginx-") as directory:
            tmp = pathlib.Path(directory)
            snippet = (repo / "ops/nginx/telegram-stores.conf").read_text()
            snippet = snippet.replace("127.0.0.1:2087", f"127.0.0.1:{backend.server_port}")
            conf = tmp / "nginx.conf"
            conf.write_text(f"""daemon off;
pid {tmp}/nginx.pid;
error_log {tmp}/error.log;
events {{ worker_connections 32; }}
http {{
client_body_temp_path {tmp}/client-body;
proxy_temp_path {tmp}/proxy;
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
                for _ in range(100):
                    try:
                        assert request(port, "/", "GET") == 403
                        break
                    except urllib.error.URLError:
                        if process.poll() is not None:
                            raise RuntimeError("disposable nginx did not start")
                        time.sleep(.05)
                else:
                    raise RuntimeError("disposable nginx startup timed out")

                def check(path, allowed):
                    for method in ("GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"):
                        before = len(Backend.seen)
                        status = request(port, path, method)
                        assert status == (401 if method in allowed else 403), (path, method, status)
                        assert len(Backend.seen) == before + (method in allowed)
                        if method in allowed:
                            assert Backend.seen[-1] == (method, path, "lab-proxy.example")

                base = "/api/v1/workspace/telegram"
                for path, allowed in ((base, {"GET", "HEAD"}), (base + "/stores", {"POST"}),
                                      (base + "/verify", {"POST"}),
                                      (base + "/stores/shop-a/connection", {"POST"}),
                                      (base + "/stores/shop-a/check", {"POST"})):
                    check(path, allowed)
                    check(path + "/", set())
                    check(path + "/extra", set())
                for bad in ("_shop", "a" * 65, "a%2Fb", "a+b", "a;b"):
                    check(base + "/stores/" + bad + "/connection", set())
                for path in (base + "/tokens", base + "/config", base + "/restart",
                             "/api/v1/agent/telegram", base + "/stores/shop-a/delete"):
                    check(path, set())
                for path, size in ((base + "/stores", 4097), (base + "/verify", 1025),
                                   (base + "/stores/shop-a/connection", 1025)):
                    before = len(Backend.seen)
                    assert request(port, path, "POST", b"x" * size) == 413
                    assert len(Backend.seen) == before
                # An unauthenticated credential request must reach HUB without
                # nginx waiting for/spooling its declared body to disk.
                for path in (base + "/verify", base + "/stores/shop-a/connection"):
                    with socket.create_connection(("127.0.0.1", port), timeout=2) as peer:
                        peer.settimeout(2)
                        peer.sendall((f"POST {path} HTTP/1.1\r\nHost: lab-proxy.example\r\n"
                                      "Content-Type: application/json\r\nContent-Length: 1024\r\n"
                                      "Connection: close\r\n\r\n").encode())
                        assert int(peer.recv(4096).split(b" ", 2)[1]) == 401
                assert not list((tmp / "client-body").glob("**/*")), "credential body spooled"
                print("TELEGRAM_PANEL_PROXY_PASS: exact routes, methods, IDs, body limits, Host and unbuffered credential authentication")
            finally:
                process.terminate()
                process.wait(timeout=5)
    finally:
        backend.shutdown()
        backend.server_close()


if __name__ == "__main__":
    main()
