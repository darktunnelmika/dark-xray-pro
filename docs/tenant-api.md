# Connected shop API (`/api/tenant/v1`)

[فارسی](tenant-api.fa.md)

A **connected shop** (tenant) is an outside sales system — a website with its own
resellers, wallets and bots — that keeps its customers on this panel. The panel is
the source of truth for each customer's credentials, volume, expiry and
subscription; the shop only sells.

A shop is not a reseller: it has no credit, no refunds, no Telegram bot and no
shop inside the panel. Its clients never mix with the owner's or the resellers':

- only the shop's own key can see or change them;
- resellers never see them;
- the owner sees a summary per shop (count, active, online, volume, usage). Seeing
  or changing a client's details needs the shop's *owner access* setting (`read` or
  `write`), and every such look or change is written to the change log;
- they are left out of the owner's client list, sales reports, reminders, the
  panel's Telegram bots and shop, and the panel's own webhook. The panel never sends
  a message to a shop's customer.

## Setting up a shop (owner)

In the panel: **People → Connected shops → Add**. Pick a fixed id (e.g. `tishtar`),
a name, optional caps (most clients, total volume) and the inbounds the shop may
give (none = every sellable inbound). Then **API keys → New key**: the key
(`dt_<id>_<secret>`) is shown once; only its SHA-256 is kept. A key may expire and
may be limited to a list of addresses (IPs or CIDR ranges) — the shop's server.

**Brand and page** sets what the shop's customers see on their subscription page
and in their apps (title, Telegram contact, notice, colour, logo, config names, and
the shop's own subscription address, e.g. `https://sub.myshop.com` pointed at the
panel's subscription address). Nothing of the panel's own brand is shown to them.

The same can be done with the owner API (owner login only):

| Method | Path | What |
|---|---|---|
| GET | `/api/v2/admin/tenants` | shops with their summaries |
| PUT | `/api/v2/admin/tenants/{id}` | create or change a shop |
| DELETE | `/api/v2/admin/tenants/{id}` | remove a shop (it must have no clients) |
| GET/POST | `/api/v2/admin/tenants/{id}/keys` | list keys / make one (`name`, `allow_ips`, `days`) |
| DELETE | `/api/v2/admin/tenants/{id}/keys/{key}` | revoke a key at once |
| GET/PUT | `/api/v2/admin/tenants/{id}/brand` | the shop's look |
| GET/PUT | `/api/v2/admin/tenants/{id}/webhook` | the shop's webhook |
| POST | `/api/v2/admin/tenants/{id}/webhook/test` | send a test event |
| POST | `/api/v2/admin/clients/move` | move clients between owner, resellers and shops |
| POST | `/api/v2/admin/migrate/foreign?tenant={id}` | import a 3x-ui / Marzban / PasarGuard database into a shop |

## Calling the API

Send the key in every request:

```
Authorization: Bearer dt_a1b2c3d4_…
```

(`X-API-Key: dt_…` works too.) Bodies and answers are JSON. Errors look like:

```json
{"error": "client \"ali\" already exists", "code": "conflict"}
```

| HTTP | code | meaning |
|---|---|---|
| 401 | `unauthorized` | missing, wrong, expired key, or a key used from an address it does not allow |
| 403 | `policy` | the shop is switched off, or a cap would be passed |
| 404 | `not_found` | no such client in this shop |
| 409 | `conflict` | the name, `external_ref`, UUID, Trojan password or subscription token is taken |
| 422 | `invalid` | a field is wrong (the message says which) |
| 503 | `unavailable` | try again shortly |

Every change is written to the panel's change log as `tenant:<id> (key <key id>)`.

Names are the shop's own: 1–100 English letters, digits or `. _ @ -`, starting with a
letter or digit. Two shops (or a shop and the owner) can use the same name.

## Clients

### The client object

```json
{
  "name": "ali",
  "external_ref": "reseller-17/order-5521",
  "status": "active",
  "enabled": true,
  "uuid": "1b2c3d4e-0000-4000-8000-0000000000aa",
  "trojan_password": "f3a1…",
  "sub_token": "abcdefgh12345678",
  "volume_bytes": 53687091200,
  "used_bytes": 1073741824,
  "up_bytes": 73741824,
  "down_bytes": 1000000000,
  "remaining_bytes": 52613349376,
  "expires_unix": 1803000000,
  "effective_expiry_unix": 1803000000,
  "max_ips": 2,
  "max_devices": 0,
  "inbounds": ["de-vless"],
  "online": false,
  "first_seen_unix": 1800000500,
  "last_seen_unix": 1800090000,
  "created_unix": 1800000000,
  "note": "",
  "rev": 48211,
  "subscription_url": "https://sub.myshop.com/sub/abcdefgh12345678"
}
```

`status` is one of `active`, `disabled`, `expired`, `limited` (volume used up) and
`on_hold` (the term starts at the first connection; `on_hold_seconds` is its
length). `volume_bytes` and `remaining_bytes` are `-1` for unlimited.

### Endpoints

| Method | Path | What |
|---|---|---|
| GET | `/me` | the shop: caps, allowed inbounds, counts, and the panel's current revision |
| GET | `/clients?after=<name>&limit=<n>` | clients by name, 100 (up to 500) a page; `next` is the `after` of the next page |
| GET | `/clients?external_ref=<ref>` | find a client by the shop's own reference |
| POST | `/clients` | create one client → `201` |
| GET | `/clients/{name}` | read one client |
| PATCH (or PUT) | `/clients/{name}` | change fields; missing fields stay as they are |
| DELETE | `/clients/{name}` | delete |
| POST | `/clients/{name}/enable`, `/disable` | switch on / off |
| POST | `/clients/{name}/reset` | clear what it used |
| POST | `/clients/{name}/renew` | add volume and/or days |
| POST | `/clients/{name}/rotate` | new subscription link (`{"link": true}`, the default) and/or new credentials (`{"credentials": true}`) |
| GET | `/clients/{name}/links` | the subscription URL in every format, and each config link on its own |

### Create

```json
POST /api/tenant/v1/clients
{
  "name": "ali",
  "external_ref": "reseller-17/order-5521",
  "volume_bytes": 53687091200,
  "days": 30,
  "max_ips": 2,
  "max_devices": 0,
  "inbounds": ["de-vless"],
  "note": "",

  "uuid": "1b2c3d4e-0000-4000-8000-0000000000aa",
  "trojan_password": "old-trojan-pass",
  "sub_token": "abcdefgh12345678"
}
```

Everything except `name` is optional:

- `volume_bytes`: bytes, `-1` = unlimited (the default);
- one of `expires_unix` (a Unix time, `0` = no expiry), `days` (a term starting now)
  or `on_hold_days` (a term starting at the first connection);
- `inbounds`: empty = every inbound the shop may give;
- `uuid`, `trojan_password`, `sub_token`: only when moving a customer from another
  panel, so their configs and subscription link keep working. Left out, new ones are
  made. They can only be set at creation; change them later with `rotate`.

### Change

```json
PATCH /api/tenant/v1/clients/ali
{"volume_bytes": 107374182400, "max_ips": 3, "enabled": true}
```

The same fields as create, without the credentials.

### Renew

```json
POST /api/tenant/v1/clients/ali/renew
{"add_bytes": 21474836480, "add_days": 30, "reset_usage": false, "enable": true}
```

Days are added to the current expiry, or to now when it has passed; an unstarted
(on-hold) term gets longer. Unlimited volume and "no expiry" stay as they are.

## Bulk calls (up to 500 items)

| Method | Path | Body |
|---|---|---|
| POST | `/bulk/create` | `{"clients": [<create>, …], "atomic": false}` |
| POST | `/bulk/update` | `{"clients": [{"name": "ali", <fields>}, …]}` |
| POST | `/bulk/renew` | `{"clients": [{"name": "ali", "add_bytes": …, "add_days": …}, …]}` |
| POST | `/bulk/delete` | `{"names": ["ali", …]}` |

A call runs in one transaction, each item on its own: by default a failed item is
reported and the rest go ahead. With `"atomic": true` one failure undoes the whole
call (HTTP 422, `"committed": false`).

```json
{
  "committed": true, "ok": 2, "failed": 1,
  "results": [
    {"name": "ali", "ok": true, "uuid": "…", "trojan_password": "…", "sub_token": "…", "subscription_url": "…"},
    {"name": "sara", "ok": false, "code": "conflict", "error": "client \"sara\" already exists"},
    {"name": "reza", "ok": true, "uuid": "…"}
  ]
}
```

## Change feed

```
GET /api/tenant/v1/changes?cursor=<cursor>&limit=<n>
```

Every client whose state or usage changed after the cursor, oldest change first,
plus the clients that were deleted or moved out of the shop:

```json
{
  "changes": [
    {"name": "ali", "rev": 48212, "status": "active", "enabled": true, "volume_bytes": 53687091200,
     "used_bytes": 1074000000, "up_bytes": 74000000, "down_bytes": 1000000000, "remaining_bytes": 52613091200,
     "expires_unix": 1803000000, "effective_expiry_unix": 1803000000, "external_ref": "reseller-17/order-5521",
     "first_seen_unix": 1800000500, "last_seen_unix": 1800090060},
    {"name": "old-user", "rev": 48213, "deleted": true}
  ],
  "cursor": "NDgyMTM6dGlzaHRhcitvbGQtdXNlcg",
  "more": false
}
```

Start without a cursor (everything), keep the returned `cursor`, and ask again with
it — every minute, say. `more: true` means another page is ready now. `limit` is up
to 1000 (500 by default). Usage arrives from the nodes every poll, and each report
changes the clients it covers once. Deletions are kept for 30 days; a shop that has
not read its feed for longer should read its whole list again.

## Webhook

```json
PUT /api/tenant/v1/webhook
{"enabled": true, "url": "https://shop.example.com/dark-webhook", "secret": "long-random-secret"}
```

(`GET` reads it, `POST /webhook/test` sends a test event; the owner can set it too.)
Leaving `secret` empty keeps the saved one; `"-"` removes it.

The shop receives only its own clients' events — `blocked:expired`,
`blocked:volume`, `blocked:ips` (and other `blocked:*`), `reset:*`, `next-plan` and
`device-refused` — up to 50 per POST:

```json
{"source": "dark-xray-pro", "tenant": "tishtar",
 "events": [{"id": 912, "kind": "client", "unix": 1800090000, "event": "blocked:volume", "client": "ali", "node": "de-1"}]}
```

The body is signed with HMAC-SHA256 of the secret in
`X-Dark-Signature: sha256=<hex>`. Answer with any 2xx: the panel moves on only then,
so nothing is lost while your site is down (after 20 failed tries a batch is skipped
and the error is shown in the panel). Use `id` to ignore a repeat.

PHP check:

```php
$body = file_get_contents('php://input');
$want = 'sha256=' . hash_hmac('sha256', $body, $secret);
if (!hash_equals($want, $_SERVER['HTTP_X_DARK_SIGNATURE'] ?? '')) { http_response_code(401); exit; }
```

## Moving customers in

From a 3x-ui, Marzban or PasarGuard database: **Settings → Backup and migration →
Import from another panel**, destination *Connected shop: …*. UUIDs, Trojan
passwords, subscription ids, volume, expiry and usage are kept, and the old
subscription links keep answering. Preview first: nothing is written until Import.
The old panel's logins and resellers are not moved (the shop keeps its own).

Between the owner, resellers and shops: **Connected shops → Move clients**, or
`POST /api/v2/admin/clients/move`:

```json
{"from": {"kind": "owner"}, "to": {"kind": "tenant", "id": "tishtar"}, "names": [], "dry_run": true}
```

An empty `names` list moves every client of the source. Credentials, links, usage
and history stay; reseller credit is not changed.
