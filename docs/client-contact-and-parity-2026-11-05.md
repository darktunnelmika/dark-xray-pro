# Client contact and client-management review

Baseline: installed PRO 4db8a7b9; reviewed 2026-11-05 against the public 3x-ui feature list and release notes (https://github.com/MHSanaei/3x-ui and https://github.com/MHSanaei/3x-ui/releases). This is a targeted comparison, not a claim of complete feature parity.

Implemented in this increment:
- Optional Telegram username, numeric user ID (string, positive, at most 52 bits), and private note in manual creation.
- Canonical case-insensitive username storage with optional leading @ accepted; server-side scoped search across names, client IDs, Telegram fields and notes.
- Contact visible in the client directory and editable after creation. Copy action is explicit. No bot account linking, messaging, ownership claim or subscription disclosure is inferred from typed Telegram data.
- Optimistic revision and exact event replay for contact edits; immutable private contact edit receipts. No charge or usage mutation.
- Existing client/credit schema and financial event fingerprints remain unchanged when optional contact is absent. Contact tables are a separate additive schema family, independently checked by offline backup validation. Older snapshots remain readable without migration; new contact metadata survives reopening/restoring the database.
- Manual mode also displays the existing credit estimate; the quick presets remain optional.

Already present: manual/preset creation, optional UUID, owner inbound selection, subscription/QR delivery controls, global filtering/pagination, lifecycle edits, bulk renewal/state actions, ownership transfer and scoped representative permissions.

Still requires distinct backend/runtime work: durable owner-approved inbound grants for representatives; time starting at first verified customer use; verified first-connection indicator; device/HWID policy independent of IP count. These are not enabled or claimed by this change. First-use expiry must exclude probes, preserve replay and concurrent-node accounting, and propagate a durable expiry to all active routes. Representative activation must never expose owner execution authority or overwrite another client's membership.

Validation evidence and installation status are recorded in the PR; implementation is not deployment.

Client page layout follow-up:
- Create client is an explicit button inside Clients; the creation form is collapsed on entry, with unfinished requests reopened for recovery. Hiding the form retains its draft and in-flight request.
- Client management panels render only after choosing Manage on a directory row; Close details returns to the compact directory.
- Financial operation history is collapsed by default. English/Persian labels and narrow mobile layout are retained.
- This follow-up remains local pending source-publication authorization; no installed-version claim.

Compact client workspace redesign:
- Create action sits in the directory header; a native modal retains drafts when closed and uses native keyboard/focus containment.
- Directory rows replace oversized cards. Search/status stay visible; additional filters and group/origin filters are explicit disclosures.
- Bulk selection is off by default. Its labeled toggle reveals checkboxes and actions; hiding it preserves any unresolved selection/request, with a selected count on the toggle. Pending ownership transfer recovery opens the bulk controls.
- Selected-client management uses Overview, Configuration & QR, Details & Telegram, Renew, and Account state sections; a client-data refresh action stays inside the modal. Existing permissions and subscription reveal/expiry semantics remain enforced.
- Creation mode is a named select instead of a changing-label checkbox. Inbound selection retains named multi-select checkboxes with protocol/transport/security metadata.
- Financial and route logic are unchanged. Usage remains in its authenticated verified view; list fields never fabricate live consumption or online status.
- Native build and mocked Chromium acceptance are separate from publication/CI/installation. This redesign is not yet installed.
