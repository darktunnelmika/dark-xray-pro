# DARK XRAY PRO — Client Sales / Directory V3

## Scope

Independent NEW PRO LAB only. No old HUB, customer, Node service, Xray,
accounting, Telegram token, active subscription or firewall mutation.
All improvements are to the authenticated operator React interface over
the existing scoped client and representative APIs. The still-pending owner
action for the USA loopback QA is **not** performed by this release.

## Representative / owner plan builder

- Keep manual config ID and name entry; random ID remains OPTIONAL.
- Duration presets 30, 60 and 90 days; input remains manually editable
  within existing backend 1–3650 day bounds.
- User/IP presets 1–5; an explicit **IP unlimited = 0** option exists only
  on volumetric plans. Unlimited plans always require at least 1 user slot.
- A compact credit **estimate** uses the existing exact NEW PRO
  CreateClient debit formula:
  - Volumetric: requested volume byte cap is charged once from the
    representative's volumetric credit, independent of IP count.
  - Unlimited: `ceil(duration_days * ip_count / 30)` units.
    Examples: 30 days × 2 users = 2 units; 60 days × 5 users = 10
    units; 45 days × 3 users = 5 units.
  - The owner itself is exempt from representative debit in the backend.
- Representative available balance is shown ONLY from last verified
  owner-visible representative row or the logged-in representative's
  own account; unknown balances are not invented. A low last-loaded
  balance is a warning, NOT a claim about the balance at commit time.
- Preset clicks and previews NEVER issue requests or debit credit.
  The existing authenticated CSRF-protected POST with an immutable event
  is still necessary; the server is authoritative on actual deduction.

## Client directory

- Compact responsive 320px Persian RTL + English LTR rows show name, ID,
  exact *saved* plan type/volume, user/IP limit, expiry and actual
  deployment-state or archived/expired label.
- **No fake online/session count** and no inferred billed traffic.
  Actual usage remains in the separately authenticated per-client view.
- Search and status filters operate on **the 50 authenticated records on
  the currently selected page**, not across all customer records; the
  scope is labeled and empty results are explicit.
- Manage only selects a verified client to the existing lifecycle panel;
  renewal, disable, enable, archive and billing have their existing
  deliberate CSRF submit/confirm controls.
- Server-side page offset is retained per authenticated account in
  sessionStorage; the local search/status filters reset when switching
  pages. No customer records, credentials or financial data are stored
  locally.

## Defense in depth

The source of truth is unchanged. In the client directory, incomplete
or malformed fields and representative-ID mismatches are rejected
before setting React state; a failed or cross-tenant client page clears
the old actionable list (fail closed) and disables stale lifecycle
submissions. Backend tenant filtering/authorization remains compulsory.
No new customer mutation routes or Nginx access added.

## Release gate

Only deploy after CI passes strict TypeScript/Vite, real Chromium tests
for preset cost equivalence, authorized scoped paging, representative
refusal and 320px Persian, Go race/vet, pinned Xray and Native Metering
Acceptance. Keep the prior PRO release as rollback and 2 operational
releases total.

The Telegram store/bot is intentionally not activated in this increment:
it is still absent from the production NEW PRO backend and requires
separate catalog/order/payment trust-boundary acceptance after core
metered customer traffic is ready.
