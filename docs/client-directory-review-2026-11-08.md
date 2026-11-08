# Client directory continuation — 2026-11-08

Development branch: fix/client-directory-20261108. Not installed, published or merged.
Base is PR179 head 5ac9e58, not installed PR180 b45f65e. Integrate onto the current installation source before producing a release; do not roll back read-pool changes.

Implemented: compact responsive rows, direct QR and permission-gated renewal, lazy tab initialization with retained drafts, accessible search clearing, active-filter count and reset. Existing global server search, ownership, committed usage and bulk authorization remain in place.
Validation: TypeScript/Vite build and17 Chromium contact/profile/finance tests passed. Covers320/390/1024/1440px overflow, preserved search after QR, draft preservation, server-filter reset and representative finance visibility. This is not live installation or visual acceptance.

Remaining priorities from source review:
1. Actual client presence: ClientDirectoryInfo exposes memberships and committed usage, not active connection observations. Define an authenticated bounded per-client observation contract with timestamps and stale/unknown semantics before displaying online/offline. Do not derive presence from deployment status or historical traffic.
2. Near-expiry and low-remaining-volume server-side filters. ClientSearch supports expired/state, plan, representative, group and inbound, but has no such filter; avoid filtering only the loaded page.
3. Row representative names implemented through scoped directory summaries; the representative filter still needs a scoped searchable picker.
4. Implemented in continuation: permission-gated direct edit and approximate remaining hours/days beside the exact expiry; refresh every30s and on visibility changes. Unknown expiry is not presented as unlimited.
5. Subscription delivery simplification: existing copy/QR is available inside delivery, but first reveal and readiness have real authorization and generation checks. Do not automatically issue/rotate links on row selection.
6. Bulk selection clarity across pagination: current selection is cleared on query/page changes and capped at50. Explain scope explicitly before considering whole-result operations.
7. Preserve search/filter/scroll across route navigation per authenticated account, without retaining private search strings across logout.

Release gates: review screenshots with realistic long Persian/English names and200 rows; broader subscription/ownership tests; publish reviewable PR on current base; CI; deploy PRO LAB and verify actual browser behavior. No legacy production changes.

Continuation validation: TypeScript/Vite build and18 Chromium tests passed, including fixed-clock relative expiry and absence of edit/renew shortcuts for representatives lacking those permissions. Development only; not published, merged or installed.

Representative-label continuation: summary carries only ID/display_name for representatives of already-authorized page clients, read in the same transaction with per-representative caching. Frontend uses the name only when scope and representative ID match, otherwise retains the ID. Focused Go directory tests and16 Chromium contact/permission tests passed, including the TypeScript/Vite build. No schema, credit or accounting mutation; not installed.
