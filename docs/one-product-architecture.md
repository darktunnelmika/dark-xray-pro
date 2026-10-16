# One PRO product, bounded deployment footprint

This is an incremental restructuring of the existing Go HUB/Agent and React/TypeScript product. Existing financial, customer, group, native runtime and metering semantics remain in their original Go modules. It is not a rewrite of working accounting and does not enable unfinished VPN, bot, subscription or migration features.

## Implemented boundaries

- One browser session provider owns sign-in, expiry and sign-out for the whole application. No credentials or CSRF tokens are written to browser storage. Local storage contains appearance choices and a non-secret cross-tab session-change notification only.
- A common HTTP client supplies bounded requests, abort handling and session invalidation. Client and representative operations continue to use the existing CSRF-protected workspace endpoints. No second credit database or frontend financial authority exists.
- Clients, groups, representatives, finance and account views are separate typed components with a shared request/state hook. A page fetches only the data needed by its section; it no longer fetches every business list on every page. Owner-only routes are omitted for representatives and direct links are restricted too. Server permissions remain authoritative.
- Root login precedes the application. Navigation uses real, allowlisted application paths and supports refresh and browser back/forward. Unknown or external next links cannot redirect outside the application.
- Public fleet telemetry requires an authenticated owner session in the HUB browser handler. Removing temporary reverse-proxy Basic authentication must happen only when this exact code is deployed and the public route allowlist is verified. Separate TLS Agent and local owner-token contracts are not converted into browser permissions.
- Release builds compile the UI first and embed its files into the HUB executable. The runtime needs neither Node.js, npm, node_modules, a browser, a source checkout nor a separate web directory. The explicit `-static` development override remains for existing isolated acceptance tests, not normal releases.
- CI checks the embedded HTML/JS/CSS and rejects development caches in the release bundle. Ordinary Go checks work without a frontend build; the placeholder is not a usable UI and responds unavailable rather than pretending success.

## Paths and retention policy

Installed code belongs under `/opt/dark-xray-pro-lab`. Data and private state remain under `/var/lib/dark-xray-pro-lab`; private settings remain under `/etc/dark-xray-pro-lab`. An update never creates a second copy of live business data inside its release directory.

A reviewed install should retain the current version and one compatible previous version, with the previous service configuration required for rollback. Unknown or unverified release directories must never be blindly deleted. The deployment review must inventory actual releases before switching; an unbounded sequence of timestamped releases is not acceptable.

Development work uses one explicitly identified workspace. Compilation caches, package dependencies and browser downloads are not user data and must have a cleanup policy; unpublished source and test evidence must be preserved until their Git provenance is established. This change does not install an automatic destructive cleanup daemon. Deleting WAL files, resetting usage, replacing private settings or blanket-deleting old QA directories is prohibited.

Monitoring history, application logs and financial history need different retention rules. The full historical monitoring rollup and self-service backup lifecycle are still pending features; they are not implemented merely by this policy. Existing financial/usage history is never pruned as cache.

## Acceptance

Required evidence is the existing full Go/Race/Vet and browser suite, plus root-login isolation, section navigation, back/refresh, representative restrictions, shared session/logout and 320px Persian rendering. The real browser-to-Store/Xray test must continue to create the same real client and verify its consumption after navigating across the new sections. UI tests serve the compiled build, not a separate mock application.

The new release packaging must include executables and installation metadata only. The old working panel and its data, domain, bots and Nodes are outside this change. A successful shell/build refactor does not close the release parity gate or authorize migration.
