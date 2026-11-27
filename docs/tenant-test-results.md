# Connected shops: scale and isolation results

Run on the test server (213.142.156.117: 2 vCPU, 2 GB, its own test HUB
running beside it, the tests at `nice 19`), branch `feature/tenant`,
26 Nov 2026.

## Scale: `DARK_SCALE=20000 go test -run '^TestScale$' -v ./internal/v2/hubcore/`

20,000 panel clients plus 20,000 clients of one connected shop (each shop
client on an inbound with three hosts: three configs).

| What | Time |
|---|---|
| shop: bulk create 20,000 (500 a call) | 28.0 s (slowest call 0.92 s) |
| shop: single calls (read, update, renew), idle | p50 3.9 ms, p99 17 ms |
| shop: single calls while 4 nodes report usage for 20,000 | p50 26.5 ms, p99 161 ms |
| usage from 4 nodes × 20,000 clients at once | 5.2 s (chunked: writes never wait behind more than one chunk) |
| shop: list all (500 a page) | 2.3 s |
| shop: change feed, everything / after 10 % used / empty | 0.93 s / 110 ms / 43 ms |
| shop: `links_version` | 38 ms |
| shop: configs of all 20,000 (`/bulk/links`, 500 a call) | 17.0 s (slowest call 0.48 s) |
| **refresh storm: every one of 20,000 subscriptions, 50 at a time** | **17.7 s total, p50 42 ms, p99 77 ms, 0 errors, 0 empty** |
| shop writes during the storm | p50 15 ms, max 217 ms |
| the same 1,000 apps again within 30 s (cached) | p50 10 ms |
| writing 20,000 fetch times (one transaction) | 181 ms |
| shop: who fetched | 97 ms |

No database lock and no error in any step.

Two faults the storm found and that are fixed on this branch:

1. **Reader-pool deadlock** (also in v2.56.2): `Nodes()` asked for another
   setting while its rows held a reader connection, and `clientViews()`
   opened its second query before closing the first. With all four reader
   connections held by calls waiting for a fifth, every reader waited for
   ever. 50 concurrent fetches froze the HUB within seconds.
2. **Full table scan per fetch**: the subscription-token lookup could not use
   the partial unique index on `sub_token`, so each fetch read the whole
   clients table. Before the fix the storm took 211 s (p50 518 ms); after it,
   17.7 s.

## Isolation

`go test -run 'TestTenant|TestOwnerAccess|TestPGAPI|TestMove|TestClientQueries|TestImport3xui' ./internal/v2/hubcore/` — all pass:

- `TestTenantAPIIsolation`: two shops each with a customer `u1`; each key sees
  and changes only its own; neither reaches the owner's, a reseller's or the
  other shop's clients by name, list, bulk or feed; a shop key never opens the
  owner API.
- `TestTenantKeys`: no key / bad key / the owner's token → 401; IP allow-list
  and expiry enforced; the key's secret is not stored.
- `TestTenantClientsStayOutOfTheOwnersViews`, `TestPGAPIOwnerDoesNotSeeTenantClients`:
  shop clients are not in the owner's lists, bots, PasarGuard-compatible API,
  sales, reminders.
- `TestOwnerAccessToTenantClients`: the owner sees or changes a shop client
  only as far as the shop's `owner_access` allows, and every look is logged.
- `TestTenantNamesAreReserved`: nobody else can create `<shop>+…` names.
- `TestTenantWebhookGetsOnlyItsOwnEvents`: each shop's webhook gets only its
  clients' events.
- `TestTenantBulkLinksAndVersion`: `/bulk/links` returns only the shop's own
  clients (the other shop's `u1` is a different client).
- `TestTenantSubDomainOldLinks`: on a shop's domain only that shop's clients
  answer; another shop's token goes to the fallback or gets 404.
- `TestMoveIsOwnerOnly`, `TestClientQueriesAreReviewed` (every SQL statement
  that reads `clients` was checked for the shop separation, including the ones
  merged from v2.56).
