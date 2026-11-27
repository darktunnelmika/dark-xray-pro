# SDKs for the Dark Xray Pro external API

One file each, no dependencies:

- [`php/DarkXrayPro.php`](php/DarkXrayPro.php) — PHP 7.4+ with curl: `DarkXrayPro\Partner`, `DarkXrayPro\Reseller`, `DarkXrayPro\Webhook`
- [`python/darkxraypro.py`](python/darkxraypro.py) — Python 3.8+: `Partner`, `Reseller`, `verify_webhook`

Both send an `Idempotency-Key` with every change (yours, or a random one), try a request again with the same key after a
network error, 429 or 5xx, and raise an error with `status`, `code` and `request_id`.

The API itself is described in [docs/api](../docs/api/README.md) ([فارسی](../docs/api/README.fa.md)); each panel also
serves its exact OpenAPI description at `/api/v2/openapi.json`.
