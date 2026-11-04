# Native client delivery closeout — 2026-11-04

This checkpoint follows removal of Dark Restore. It does not reopen import or restore product workflows. Telegram bot development remains owned by the separate chat; this change does not modify bot code, tokens, webhooks, or Store ownership.

## Implemented

Both the managed inbound subscription and existing LAB subscription view now display an on-device QR code alongside an explicitly revealed URL. The code encodes the exact URL and currently selected output format. The QR library is bundled locally; credentials are not sent to an image service. The QR shares the parent's reveal lifetime, hide action, scope and permission checks. It is not persisted or fetched automatically. A white background, four-module quiet zone and responsive canvas support scanning in either theme.

`qrcode.react` is pinned to 4.2.0. The independent decoder `jsqr` 1.4.0 is a development-only dependency used to verify actual rendered pixels rather than comparing encoder output to itself.

## Validation

- TypeScript and production Vite build passed. The pre-existing bundle-size warning remains.
- Existing subscription scenarios: 47 passed, including scope, permission loss, hidden tab, one-minute expiry, generation changes, lost mutation replies, and 320px Persian layout.
- New rendered QR decoding test passed separately after correcting the test's origin comparison (the initial navigation was incorrectly compared with about:blank). It verifies exact URL, format change, hide/expiry, no consumer fetch, no external request, and no credential storage.
- Server-side ownership tests passed with two representatives and actual authorization/balance checks.
- Server-side client lifecycle, bulk state/renewal, profile edit, representative controls and owner credit/client HTTP tests passed.

- 25 additional browser tests passed: bulk renewal, client finance permissions, client ownership, representative controls and business idempotency.

## Still open

This is a code and automated-test checkpoint, not installed product acceptance. Existing PRO LAB has data-plane activation disabled. The full installed native journey still needs a verified LAB route: create client, issue/scan subscription, connect, observe committed billable traffic, renew, disable/re-enable and archive, including quota and HUB-outage behavior. A generated QR or active token alone is not proof that connection delivery is available. Installation requires the exact-head release checks and existing guarded LAB deployment procedure.

No production customer, legacy HUB, node, DNS or Telegram integration was changed. The PR remains draft; completion of Clients/Representatives and overall product acceptance must not be inferred from this checkpoint.
