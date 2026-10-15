# DARK XRAY PRO — functional owner and representative browser authentication

Status: real owner/representative account and financial workspace implementation on the NEW independent PRO database. **Not a production-ready replacement or authorization to migrate old accounts and Telegram bots.**

## Security and durable state

A forward-only SQLite schema v1→v2 migration creates auth_users and auth_sessions and preserves existing PRO native clients, owner/representative credit and append-only immutable ledger. Authentication uses Argon2id with unique salts, random 256-bit opaque session cookies stored as SHA-256 digests, Secure/HttpOnly/SameSite Strict cookie attributes and a bounded 12-hour session lifetime. Twenty active sessions maximum per account; password changes revoke all old sessions and disabled representatives lose access immediately.

The application checks same-origin Origin and Fetch Metadata on mutating requests, strictly requires JSON and a per-session HMAC-bound CSRF header, and bounds password hashing concurrency and login attempts per username/global. These do not replace the additional production requirements of edge throttling, TOTP and security audit.

## New first-owner bootstrap (private local setup)

New isolated Linux amd64 LAB releases include a checksum-pinned executable at /opt/dark-xray-pro-lab/bin/dark-pro-owner-setup. It runs as unprivileged darkprolab, with DARK_PRO_LAB_MODE=1, a NEW database path /var/lib/dark-xray-pro-lab/private/business.sqlite, a username, and a password-file path. The password file must be a *private regular file, mode 0600 or 0400, owned by that service user* containing a 12+ character password and an optional single trailing newline.

The password must not appear in command-line arguments, shell history, GitHub, Telegram or bot messages. The setup refuses root, non-LAB mode, unapproved DB paths, symlink password files and a second owner account. Remove the one-time password file after the bootstrap according to secure host procedures.

**Do not execute bootstrap on the old production HUB or Nodes and do not reuse old credentials.**

## Explicit feature gate and routes

The HUB requires BOTH DARK_PRO_ENABLE_BUSINESS_CORE=1 and an explicit new private business database path. Without both, browser session/workspace endpoints return 404. The already installed new HUB has not enabled this flag; current public Nginx explicitly blocks sensitive APIs.

Browser login:
- GET /api/v1/session/status
- POST /api/v1/session/login
- POST /api/v1/session/logout
- POST /api/v1/session/change-password

Role-scoped workspace:
- GET /api/v1/workspace/me
- GET/POST /api/v1/workspace/representatives (owner)
- POST /api/v1/workspace/representatives/{id}/login (owner)
- POST /api/v1/workspace/representatives/{id}/credits (owner)
- GET/POST /api/v1/workspace/clients (owner/all or representative/self)
- GET /api/v1/workspace/events (owner/all or representative/self)

The existing localhost-only Bearer admin endpoints remain independent and blocked by the public proxy. The Agent-only TLS handler rejects *all* owner/workspace/session routes, including requests with a valid session cookie. A dedicated reviewed HTTPS proxy allowlist is required before opening the browser business interface remotely, without exposing owner Bearer or Node credentials.

## Functional UI

English/Persian, RTL/mobile Business navigation. Owner can create representatives, provision individual logins, grant/debit balances, create client records, view credit events. Each representative sees their own balance, own clients and financial history; the server rejects cross-tenant operations regardless of UI form state. Both can sign out or change passwords. Malformed/unavailable server results remain visibly unavailable, never fabricated zero or fake users.

Unlimited accounting remains ceil(duration_days × allowed_user_slots / 30); e.g., five users × 60 days costs 10 units. Volumetric plans support zero IP count as no IP-count limit. Every new client record stays pending_node_apply; no link is published and no live Xray service is touched. Browser credit/client operations retain their event IDs on uncertain same-page retries.

## Mandatory release blockers

Security: TOTP, persistent account lockout/edge throttling, role and security audit, secret rotation, independent security review.
Product: full Inbounds and Node runtime/real Apply with crash-safe rollback, Clients edit/renew/delete, Groups, representatives marketplace, Telegram owner/rep/customer bots, Mini App, billing/traffic, checkout and payments.
Migration: complete legacy schema and subscription identity mapping, repeatable offline backup/import, credit/event/account reconciliation, historical users and Telegram business identity preservation.
Release: load and reboot recovery acceptance, legacy public port/domain compatibility, final explicit owner approval and safe cutover/rollback.

No existing production DARK XRAY data, users, bots, services, domain or Node identities are modified here.
