# Dark Xray Pro external API

[فارسی](README.fa.md) · [PHP SDK](../../sdk/php/DarkXrayPro.php) · [Python SDK](../../sdk/python/darkxraypro.py)

Two APIs for other programs, next to the panel's own:

| | Partner API | Reseller API |
| --- | --- | --- |
| For | a website or bot that **sells reseller panels** | a reseller's **own bot or website** that sells to customers |
| Key | `dp_<id>_<secret>`, made by the owner (Resellers → Panel sales API) | `dr_<id>_<secret>`, made by the reseller (Settings → My sales API), the owner, or the partner that sold the panel |
| Base | `/api/v2/partner` | `/api/v2/reseller` |
| Sees | only the panels it made | only that reseller's customers |

Both follow the panel's own rules: a partner key never hands out more credit than its limit and never loosens the rules
of its template or package; a reseller key pays from the reseller's credit under its policy, with the same refunds and the
same credit ledger as the panel. The panel serves its exact description as OpenAPI 3 at `GET /api/v2/openapi.json`
(for a panel session or any key; `?api=partner` or `?api=reseller`), ready for Postman or a code generator.

## Conventions

**Authentication.** `Authorization: Bearer <key>` (or `X-API-Key: <key>`). The secret panel path does not apply to API
calls.

**JSON.** Requests and answers are JSON. Volumes are GB (decimal, 1 GB = 2³⁰ bytes), times are Unix seconds, an
unlimited volume is `-1`.

**Safe retries.** Send `Idempotency-Key: <your order id>` with every change (POST, PATCH, DELETE). The same key with the
same request within 24 hours runs once: a retry gets the first answer again with `Idempotent-Replayed: true`, so a
timeout never makes a second panel or takes credit twice. The same key with a different request is refused
(`idempotency_key_reused`); one still running answers 409 (`idempotency_in_progress`). Only successful answers are kept,
so a refused request can be fixed and sent again with the same key. The SDKs add a key to every change on their own.

**Errors.** Every error is

```json
{"detail": "not enough credit", "code": "insufficient_credit", "request_id": "3f2a9c1b7d4e8a60"}
```

| HTTP | code | meaning |
| --- | --- | --- |
| 400 | `invalid_json` | the body is not JSON |
| 401 | `unauthorized`, `key_expired` | wrong, turned-off or expired key |
| 402 | `insufficient_credit` | the reseller's (or panel's) credit does not cover it |
| 403 | `scope_missing`, `ip_not_allowed`, `policy_denied`, `credit_limit`, `account_disabled` | the key may not, the rules forbid it, the partner key's limit is used up, the reseller is turned off |
| 404 | `not_found` | not there, or not yours |
| 409 | `conflict` | a name or `external_ref` is taken, a panel still has customers |
| 422 | `validation_failed`, `idempotency_key_reused` | a field is wrong |
| 429 | `rate_limited` | too fast; wait `Retry-After` seconds |
| 500 | `internal_error` | send the `request_id` to the owner |

Every answer has `X-Request-ID` (send your own to have it echoed).

**Rate.** 120 requests a minute per key by default (the owner can set 1–6000). Answers carry `X-RateLimit-Limit` and
`X-RateLimit-Remaining`. An address that sends 20 wrong keys in a minute is refused for a while.

**Lists.** `limit` and `offset`; answers are `{"items": [...], "total": n, "limit": l, "offset": o}`. The partner
`GET /reps` answers a plain array (as it always has) with the total in `X-Total-Count`. Histories page with
`before=<next_before>`, events with `after=<next_after>`.

## Partner API

Scopes: `panels:read`, `panels:write`, `credit:add`, `events` (on by default); `credit:revoke`, `panels:delete`,
`clients:read` (the owner turns them on per key). The owner can also limit a key to some addresses, give it an expiry and
a rate, and pick the reseller-sale packages it may sell.

| Method | Path | Scope | |
| --- | --- | --- | --- |
| GET | `/api/v2/partner` | | the key: limits, `remaining_gb`, `remaining_units` (-1 = no limit), scopes |
| GET | `/packages` | | packages it may sell (credit, units, rules) |
| GET | `/stats?days=30` | panels:read | panels, customers, credit, and what was added, taken back, sold and refunded in the period |
| GET | `/ledger?before=&limit=` | panels:read | credit added or taken back, every panel |
| POST | `/reps` | panels:write | make a panel |
| GET | `/reps?q=&enabled=&external_ref=&limit=&offset=` | panels:read | its panels, newest first |
| GET | `/reps/{id}` | panels:read | one panel |
| PATCH | `/reps/{id}` | panels:write | name, note, external_ref, enabled, policy, telegram_ids, low_credit_gb |
| DELETE | `/reps/{id}?clients=1` | panels:delete | delete (with its customers when `clients=1`); its unused credit can be handed out again |
| POST | `/reps/{id}/credit` | credit:add | `{credit_gb, units, note}` |
| POST | `/reps/{id}/credit/revoke` | credit:revoke | take unused credit back |
| GET | `/reps/{id}/ledger` | panels:read | its credit history |
| GET | `/reps/{id}/overview` | panels:read | customers by state, this month, the last 30 days |
| GET | `/reps/{id}/clients?status=&q=` | clients:read | its customers (read only) |
| POST | `/reps/{id}/enable`, `/disable` | panels:write | off = no sign-in and no sales; customers keep working |
| POST | `/reps/{id}/password` | panels:write | `{password}` or empty for a random one; the answer has it and a ready-to-send message |
| POST | `/reps/{id}/sessions/revoke` | panels:write | sign the panel out everywhere |
| POST | `/reps/{id}/login-link` | panels:write | a one-time sign-in link for 10 minutes (never for a login with two-step sign-in) |
| GET, POST | `/reps/{id}/apikeys` | panels:read, panels:write | the panel's reseller API keys; sell a panel with its API |
| DELETE | `/reps/{id}/apikeys/{key}` | panels:write | |

Making a panel:

```json
POST /api/v2/partner/reps
Idempotency-Key: order-1001

{
  "name": "Shop A",
  "package": "gold",
  "external_ref": "order-1001",
  "telegram_ids": [123456789],
  "policy": {"max_clients": 100, "max_days": 90},
  "low_credit_gb": 20
}
```

`package` gives the package's credit and rules; without it give `credit_gb` and `units` (the rules then come from the
key's template reseller). `policy` can only make the rules tighter: a cap is never raised or removed, a permission never
given, an inbound never added. `id`, `username` and `password` are optional. The answer is the panel with `password`,
`panel_url` and `message`, a ready-to-send text with the address and the sign-in. `external_ref` is unique per key, so it
also finds the panel again: `GET /reps?external_ref=order-1001`.

## Reseller API

Scopes: `account:read`, `clients:read`, `clients:write`, `clients:delete`, `events` (on by default); `sellers`.

| Method | Path | Scope | |
| --- | --- | --- | --- |
| GET | `/api/v2/reseller/me` | account:read | credit, rules, the key, the subscription base address |
| GET | `/inbounds` | account:read | inbounds it may give |
| GET | `/plans` | account:read | its published plans (products) |
| POST | `/quote` | account:read | what a create or renew costs and whether it passes; changes nothing |
| GET | `/overview` | account:read | dashboard numbers |
| GET | `/ledger?before=&limit=` | account:read | credit history |
| GET | `/clients?status=&q=&limit=&offset=` | clients:read | customers |
| POST | `/clients` | clients:write | make one |
| POST | `/clients/bulk` | clients:write | `prefix-1 … prefix-n`; nothing is made unless every name is free and the credit covers all |
| GET | `/clients/{name}` | clients:read | one customer, with `sub_url` |
| PATCH | `/clients/{name}` | clients:write | enabled, max_ips, inbounds, note, device_limit, telegram_id |
| DELETE | `/clients/{name}` | clients:delete | unused credit comes back when the rules say so |
| POST | `/clients/{name}/renew` | clients:write | a new period; unused volume of the old one comes back |
| POST | `/clients/{name}/reset` | clients:write | usage to zero; a volume plan pays the used part |
| POST | `/clients/{name}/revoke` | clients:write | new link and credentials |
| POST | `/clients/{name}/enable`, `/disable` | clients:write | |
| GET | `/clients/{name}/links` | clients:read | subscription address and every config link |
| GET | `/clients/{name}/qr.png?size=` | clients:read | QR code (PNG) |
| GET | `/clients/{name}/usage?days=` | clients:read | traffic per day |
| GET, DELETE | `/clients/{name}/devices[/{hwid}]` | clients:read, clients:write | devices seen |
| GET | `/clients/{name}/ips` | clients:read | addresses seen in 30 days |
| GET | `/online` | clients:read | customers online now |
| GET, POST | `/sellers` | sellers | sub-sellers (rules never looser than the reseller's own) |
| PATCH | `/sellers/{id}` | sellers | name, enabled, policy, password |
| POST | `/sellers/{id}/credit`, `/credit/revoke` | sellers | move credit to it or take it back |

Making a customer:

```json
POST /api/v2/reseller/clients
Idempotency-Key: order-1001

{"name": "ali", "gb": 30, "days": 30, "max_ips": 2, "note": "Telegram @ali"}
```

`plan` is `volume` (default, needs `gb`) or `unlimited` (one unit per 30 days). The term starts at the first connection
unless `start_on_first_use` is `false`. `inbounds` defaults to every inbound the reseller may give. `product` (a plan id
from `/plans`) fills plan, gb, days, IP limit and inbounds from one of its plans; a sub-seller can only sell those. The
answer is `{"client": {…, "sub_url": "…"}, "charge": {"gb": 30, "units": 0}, "balance": {"credit_gb": 170, "units": 5}}`.

Ask first when the price matters: `POST /quote {"action": "renew", "name": "ali", "gb": 50, "days": 30}` answers
`allowed`, `charge_gb`, `refund_gb`, `credit_after_gb` and, when not allowed, `code` and `reason`.

## Events and webhooks

Every partner key and every reseller has an event list (kept 30 days): `GET …/events?after=<id>&types=a,b`. With
`PUT …/webhook {"url": "https://…", "events": [...]}` they are also POSTed to you (events left out: all). A new webhook,
or `"rotate_secret": true`, answers the signing secret once.

```
POST <your url>
X-Dark-Timestamp: 1767225600
X-Dark-Signature: sha256=<hex HMAC-SHA256 of "<timestamp>.<body>" with the secret>
X-Dark-Delivery: 41

{"target": "rep:shop", "events": [{"id": 41, "event": "client.expiring", "unix": 1767225600, "data": {…}}]}
```

Up to 50 events per POST, oldest first; any 2xx moves past them. Otherwise it is tried again after 10 s, 20 s, 40 s …
up to an hour between tries; after 25 failed tries the batch is skipped (it stays in the event list). Webhook addresses
must be public. `POST …/webhook/test` sends a ping, `GET …/webhook/deliveries` lists recent tries. Check the signature
and the time, and keep the last event id to skip a repeat:

```php
$want = "sha256=" . hash_hmac("sha256", $_SERVER["HTTP_X_DARK_TIMESTAMP"] . "." . $body, $secret);
$ok = hash_equals($want, $_SERVER["HTTP_X_DARK_SIGNATURE"]) && abs(time() - (int)$_SERVER["HTTP_X_DARK_TIMESTAMP"]) < 300;
```

**Partner events** (`data` has the panel's `id`, `name`, `username`, `external_ref`, `credit_gb`, `units`, `enabled`):

| event | extra data |
| --- | --- |
| `panel.created` | `package` |
| `panel.updated`, `panel.enabled`, `panel.disabled`, `panel.password_reset` | |
| `panel.credit_added` | `added_gb`, `added_units` |
| `panel.credit_revoked` | `revoked_gb`, `revoked_units` |
| `panel.credit_low` | `threshold_gb` (credit fell under the panel's `low_credit_gb`, told once each time) |
| `panel.deleted` | `clients_deleted`, `released_gb`, `released_units` |

**Reseller events** cover every customer however it was sold (panel, Telegram bot, shop or API):

| event | data |
| --- | --- |
| `client.created` | `name`, `plan`, `volume_gb`, `term_days`, `expires_unix`, `charged_gb`, `charged_units`, `by` |
| `client.updated`, `client.enabled`, `client.disabled`, `client.revoked` | `name` |
| `client.deleted` | `name`, `refund_gb`, `refund_units`, `by` |
| `client.renewed` | `name`, `automatic`, `plan`, `volume_gb`, `expires_unix` |
| `client.usage_reset` | `name`, `reason` |
| `client.first_used`, `client.expired`, `client.volume_exhausted` | `name`, `plan`, `volume_gb`, `used_gb`, `expires_unix` |
| `client.expiring` | the same and `days_left` (3 days or less, told once per period) |
| `client.volume_low` | the same and `left_gb` (under 10% left, told once per period) |
| `credit.added`, `credit.removed` | `volume_gb`, `units`, `credit_gb`, `credit_units`, `note` |
| `credit.low` | `credit_gb`, `threshold_gb` |

State events (`first_used`, `expired`, `volume_exhausted`, `expiring`, `volume_low`) are found by a check every five
minutes.

## SDKs

One file each, no dependencies: [PHP 7.4+](../../sdk/php/DarkXrayPro.php) and [Python 3.8+](../../sdk/python/darkxraypro.py).
Both add an `Idempotency-Key` to every change, retry network errors, 429 and 5xx with the same key, raise an error with
`status`, `code` and `request_id`, and check webhook signatures.

```php
$partner = new DarkXrayPro\Partner("https://panel.example.com:2087", "dp_…");
$panel = $partner->createPanel(["name" => "Shop A", "package" => "gold", "external_ref" => "order-77"], "order-77");
$key = $partner->createApiKey($panel["id"], ["name" => "bot"]);      // sell the panel with its API

$reseller = new DarkXrayPro\Reseller("https://panel.example.com:2087", $key["key"]);
$r = $reseller->createClient(["name" => "ali", "gb" => 30, "days" => 30], "order-1001");
echo $r["client"]["sub_url"];
```

```python
from darkxraypro import Reseller, ApiError
api = Reseller("https://panel.example.com:2087", "dr_…")
try:
    r = api.create_client({"name": "ali", "gb": 30, "days": 30}, idempotency_key="order-1001")
except ApiError as e:
    print(e.status, e.code, e.detail)
```
