"""Dark Xray Pro external API client: the partner API (dp_ keys, selling
reseller panels) and the reseller API (dr_ keys, a reseller's own bot or
site). One file, Python 3.8+, standard library only.

    from darkxraypro import Partner, Reseller, ApiError, verify_webhook

    partner = Partner("https://panel.example.com:2087", "dp_xxxx_yyyy")
    panel = partner.create_panel({"name": "Shop A", "package": "gold", "external_ref": "order-77"},
                                 idempotency_key="order-77")
    print(panel["message"])

    reseller = Reseller("https://panel.example.com:2087", "dr_xxxx_yyyy")
    r = reseller.create_client({"name": "ali", "gb": 30, "days": 30}, idempotency_key="order-1001")
    print(r["client"]["sub_url"])

Every change is sent with an Idempotency-Key (yours, or a random one), and a
request that fails on the network, with 429 or with 5xx is tried again with
the same key, so a retry never makes something twice. Errors raise ApiError
with the HTTP status, the error code and the request id.
"""

import hashlib
import hmac
import json
import secrets
import time
import urllib.error
import urllib.parse
import urllib.request

__all__ = ["ApiError", "Partner", "Reseller", "verify_webhook"]


class ApiError(Exception):
    """An error answer. status is 0 when the server was not reached."""

    def __init__(self, status, code, detail, request_id="", body=None):
        super().__init__(f"{status} {code}: {detail}")
        self.status = status
        self.code = code
        self.detail = detail
        self.request_id = request_id
        self.body = body


def _seg(v):
    return urllib.parse.quote(str(v), safe="")


class _Client:
    prefix = ""

    def __init__(self, base_url, key, timeout=30, retries=3):
        self.base = base_url.rstrip("/")
        self.key = key
        self.timeout = timeout
        self.retries = retries
        self.last_headers = {}

    def request(self, method, path, body=None, query=None, idempotency_key=None):
        """Sends a request; returns the decoded JSON, or bytes for a non-JSON answer."""
        url = self.base + path
        q = {k: v for k, v in (query or {}).items() if v is not None and v != ""}
        if q:
            url += "?" + urllib.parse.urlencode(q)
        headers = {"Authorization": "Bearer " + self.key, "Accept": "application/json", "User-Agent": "darkxraypro-python/1"}
        if method != "GET":
            headers["Idempotency-Key"] = idempotency_key or "sdk-" + secrets.token_hex(12)
        data = None
        if body is not None:
            data = json.dumps(body, ensure_ascii=False).encode()
            headers["Content-Type"] = "application/json"
        attempt = 0
        while True:
            attempt += 1
            req = urllib.request.Request(url, data=data, method=method, headers=headers)
            status, raw, hdrs, net_err = 0, b"", {}, ""
            try:
                with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                    status, raw, hdrs = resp.status, resp.read(), {k.lower(): v for k, v in resp.headers.items()}
            except urllib.error.HTTPError as e:
                status, raw, hdrs = e.code, e.read(), {k.lower(): v for k, v in e.headers.items()}
            except (urllib.error.URLError, TimeoutError, ConnectionError) as e:
                net_err = str(e)
            self.last_headers = hdrs
            retry = net_err != "" or status == 429 or status >= 500
            if retry and attempt <= self.retries:
                try:
                    wait = int(hdrs.get("retry-after", "0")) or attempt * 2
                except ValueError:
                    wait = attempt * 2
                time.sleep(max(1, min(wait, 30)))
                continue
            if net_err:
                raise ApiError(0, "network_error", net_err)
            out = raw
            if hdrs.get("content-type", "").startswith("application/json"):
                out = json.loads(raw.decode() or "null")
            if status >= 400:
                d = out if isinstance(out, dict) else {}
                raise ApiError(status, d.get("code", f"http_{status}"), d.get("detail", raw.decode(errors="replace")),
                               d.get("request_id", hdrs.get("x-request-id", "")), out)
            return out

    # ---- events and webhook (both APIs) ----

    def events(self, after=0, types=None, limit=100):
        return self.request("GET", self.prefix + "/events", query={"after": after, "types": ",".join(types or []), "limit": limit})

    def webhook(self):
        return self.request("GET", self.prefix + "/webhook")

    def set_webhook(self, url, events=None, rotate_secret=False, enabled=None):
        """The answer has the signing secret when the webhook is new or rotate_secret is true."""
        b = {"url": url, "rotate_secret": rotate_secret}
        if events is not None:
            b["events"] = events
        if enabled is not None:
            b["enabled"] = enabled
        return self.request("PUT", self.prefix + "/webhook", b)

    def delete_webhook(self):
        return self.request("DELETE", self.prefix + "/webhook")

    def test_webhook(self):
        return self.request("POST", self.prefix + "/webhook/test")

    def webhook_deliveries(self, limit=50):
        return self.request("GET", self.prefix + "/webhook/deliveries", query={"limit": limit})


class Partner(_Client):
    """The partner API: make and run reseller panels with a dp_ key."""

    prefix = "/api/v2/partner"

    def _rep(self, rep_id, tail=""):
        return f"{self.prefix}/reps/{_seg(rep_id)}{tail}"

    def me(self):
        return self.request("GET", self.prefix)

    def packages(self):
        return self.request("GET", self.prefix + "/packages")

    def stats(self, days=30):
        return self.request("GET", self.prefix + "/stats", query={"days": days})

    def ledger(self, before=0, limit=100):
        return self.request("GET", self.prefix + "/ledger", query={"before": before or None, "limit": limit})

    def create_panel(self, data, idempotency_key=None):
        """data: name, id, username, password, credit_gb, units, package, policy, telegram_ids, external_ref, note, low_credit_gb"""
        return self.request("POST", self.prefix + "/reps", data, idempotency_key=idempotency_key)

    def panels(self, **query):
        """query: q, enabled, external_ref, limit, offset. The total is in last_headers['x-total-count']."""
        return self.request("GET", self.prefix + "/reps", query=query)

    def panel(self, rep_id):
        return self.request("GET", self._rep(rep_id))

    def find_by_external_ref(self, ref):
        found = self.panels(external_ref=ref)
        return found[0] if found else None

    def update_panel(self, rep_id, data):
        return self.request("PATCH", self._rep(rep_id), data)

    def delete_panel(self, rep_id, with_clients=False):
        return self.request("DELETE", self._rep(rep_id), query={"clients": "1" if with_clients else None})

    def add_credit(self, rep_id, gb, units=0, note="", idempotency_key=None):
        return self.request("POST", self._rep(rep_id, "/credit"), {"credit_gb": gb, "units": units, "note": note}, idempotency_key=idempotency_key)

    def revoke_credit(self, rep_id, gb, units=0, note="", idempotency_key=None):
        return self.request("POST", self._rep(rep_id, "/credit/revoke"), {"credit_gb": gb, "units": units, "note": note}, idempotency_key=idempotency_key)

    def panel_ledger(self, rep_id, before=0, limit=100):
        return self.request("GET", self._rep(rep_id, "/ledger"), query={"before": before or None, "limit": limit})

    def overview(self, rep_id):
        return self.request("GET", self._rep(rep_id, "/overview"))

    def panel_clients(self, rep_id, **query):
        return self.request("GET", self._rep(rep_id, "/clients"), query=query)

    def enable(self, rep_id):
        return self.request("POST", self._rep(rep_id, "/enable"))

    def disable(self, rep_id):
        return self.request("POST", self._rep(rep_id, "/disable"))

    def reset_password(self, rep_id, password=None):
        return self.request("POST", self._rep(rep_id, "/password"), None if password is None else {"password": password})

    def revoke_sessions(self, rep_id):
        return self.request("POST", self._rep(rep_id, "/sessions/revoke"))

    def login_link(self, rep_id):
        return self.request("POST", self._rep(rep_id, "/login-link"))

    def api_keys(self, rep_id):
        return self.request("GET", self._rep(rep_id, "/apikeys"))

    def create_api_key(self, rep_id, data):
        """data: name, scopes, allowed_ips, days, rate_per_min. The key is in the answer once."""
        return self.request("POST", self._rep(rep_id, "/apikeys"), data)

    def delete_api_key(self, rep_id, key_id):
        return self.request("DELETE", self._rep(rep_id, "/apikeys/" + _seg(key_id)))


class Reseller(_Client):
    """The reseller API: a reseller's own bot or site, with a dr_ key."""

    prefix = "/api/v2/reseller"

    def _c(self, name, tail=""):
        return f"{self.prefix}/clients/{_seg(name)}{tail}"

    def me(self):
        return self.request("GET", self.prefix + "/me")

    def inbounds(self):
        return self.request("GET", self.prefix + "/inbounds")

    def plans(self):
        return self.request("GET", self.prefix + "/plans")

    def quote(self, data):
        """data: action (create|renew), name (renew), plan, gb, days, start_on_first_use, max_ips, inbounds, product"""
        return self.request("POST", self.prefix + "/quote", data)

    def overview(self):
        return self.request("GET", self.prefix + "/overview")

    def ledger(self, before=0, limit=100):
        return self.request("GET", self.prefix + "/ledger", query={"before": before or None, "limit": limit})

    def clients(self, **query):
        """query: status, q, limit, offset"""
        return self.request("GET", self.prefix + "/clients", query=query)

    def create_client(self, data, idempotency_key=None):
        """data: name, plan (volume|unlimited), gb, days, start_on_first_use, max_ips, device_limit, inbounds, note, telegram_id, product"""
        return self.request("POST", self.prefix + "/clients", data, idempotency_key=idempotency_key)

    def bulk_create(self, data, idempotency_key=None):
        """data: prefix, count, start and the create_client fields"""
        return self.request("POST", self.prefix + "/clients/bulk", data, idempotency_key=idempotency_key)

    def client(self, name):
        return self.request("GET", self._c(name))

    def update_client(self, name, data):
        """data: enabled, max_ips, inbounds, note, device_limit, telegram_id"""
        return self.request("PATCH", self._c(name), data)

    def delete_client(self, name):
        return self.request("DELETE", self._c(name))

    def renew(self, name, data, idempotency_key=None):
        """data: plan, gb, days or product"""
        return self.request("POST", self._c(name, "/renew"), data, idempotency_key=idempotency_key)

    def reset_usage(self, name):
        return self.request("POST", self._c(name, "/reset"))

    def revoke_link(self, name):
        return self.request("POST", self._c(name, "/revoke"))

    def enable(self, name):
        return self.request("POST", self._c(name, "/enable"))

    def disable(self, name):
        return self.request("POST", self._c(name, "/disable"))

    def links(self, name):
        return self.request("GET", self._c(name, "/links"))

    def qr_png(self, name, size=320):
        """PNG bytes"""
        return self.request("GET", self._c(name, "/qr.png"), query={"size": size})

    def usage(self, name, days=30):
        return self.request("GET", self._c(name, "/usage"), query={"days": days})

    def devices(self, name):
        return self.request("GET", self._c(name, "/devices"))

    def delete_device(self, name, hwid):
        return self.request("DELETE", self._c(name, "/devices/" + _seg(hwid)))

    def ips(self, name):
        return self.request("GET", self._c(name, "/ips"))

    def online(self):
        return self.request("GET", self.prefix + "/online")

    def sellers(self):
        return self.request("GET", self.prefix + "/sellers")

    def create_seller(self, data):
        return self.request("POST", self.prefix + "/sellers", data)

    def update_seller(self, seller_id, data):
        return self.request("PATCH", f"{self.prefix}/sellers/{_seg(seller_id)}", data)

    def seller_credit(self, seller_id, gb, units=0, note=""):
        return self.request("POST", f"{self.prefix}/sellers/{_seg(seller_id)}/credit", {"credit_gb": gb, "units": units, "note": note})

    def seller_revoke(self, seller_id, gb, units=0, note=""):
        return self.request("POST", f"{self.prefix}/sellers/{_seg(seller_id)}/credit/revoke", {"credit_gb": gb, "units": units, "note": note})


def verify_webhook(secret, timestamp, body, signature, tolerance=300):
    """True when X-Dark-Signature matches "<X-Dark-Timestamp>.<body>" and the
    timestamp is recent. body is the raw request body (bytes or str)."""
    if isinstance(body, str):
        body = body.encode()
    try:
        ts = int(timestamp)
    except (TypeError, ValueError):
        return False
    if abs(time.time() - ts) > tolerance:
        return False
    want = "sha256=" + hmac.new(secret.encode(), str(ts).encode() + b"." + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(want, signature or "")
