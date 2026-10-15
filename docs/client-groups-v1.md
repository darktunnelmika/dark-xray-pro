# DARK XRAY PRO — native client groups v1 (PRO-only)

This is an independently versioned and persisted PRO client-groups implementation. It is not a migration/import of legacy DARK XRAY groups, and it does not mutate any old HUB, Node, customer subscription, billing ledger or Telegram bot.

## Implemented functional features

- Forward-only private SQLite business schema v2→v3 adds client_groups and client_group_members. Existing native owner/representative logins, hashed sessions, credit balances, clients, UUIDs, expiry and immutable audit events remain unchanged.
- Groups have a unique ID, representative ownership, Unicode name, optional strictly validated hexadecimal color, creation/update timestamps and true member counts. Owner can manage any PRO tenant; representatives only their own.
- Bulk assign/move of up to 500 client IDs is one atomic transaction. Duplicate, nonexistent or cross-representative IDs reject the complete batch: no partial updates or incorrect member counts.
- One client belongs to at most one group at a time. Repeating the same assignment does not create a duplicate group member.
- Owner/representative may rename/recolor and remove individual group members, or delete the group to detach all membership links.
- **Deleting a group never deletes Clients, refunds consumed representative credit or alters financial usage/event history.** Deleting a group with existing members only removes their group assignment. There is no credit reversal.
- Paginated owner/representative group and membership reads (200 records max per request) and a mobile/RTL light/dark bilingual management UI with create, edit, bulk move and delete confirmation.
- No group traffic total is fabricated: billable historical/period usage must come only from the future traffic/accounting ledger.

## Role-scoped session API

All routes below are enabled only when the new private business database and browser session feature are deliberately enabled on the independent PRO HUB. The Agent TLS interface and unpaired public visitors cannot call them.

- GET /api/v1/workspace/groups
- POST /api/v1/workspace/groups
- PATCH /api/v1/workspace/groups/{id}
- DELETE /api/v1/workspace/groups/{id}
- GET /api/v1/workspace/groups/{id}/members
- POST /api/v1/workspace/groups/{id}/members
- DELETE /api/v1/workspace/groups/{id}/members/{client}

Every mutation requires same-origin JSON, a valid secure session and a matching session-bound CSRF token. Representative filtering and group ownership are verified in the backend, not inferred from visible browser controls.

## Regression acceptance

Go race and SQLite tests: persisted membership across close/reopen, exact member counts, move without duplicate membership, mixed-tenant atomic refusal, 500-entry cap, invalid ID/color/name rejection, group rename and deletion without touching client/credit/event tables, v2→v3 migration preserving owner login, sessions, clients and balances.

Browser Chromium tests: existing owner/representative credit UI remains functional, groups read from the actual PRO-only API contracts, new group with Unicode name/color, two-member bulk move, grouped deletion with operator confirmation, checked CSRF headers, Persian RTL and 320px no horizontal overflow.

## Remaining production blockers

- Old-to-PRO group IDs, historical membership, imported Restore group mapping and audit reconciliation; no old Node identity reuse.
- Owner/rep client edit/renew/delete, real Inbound Xray Apply/rollback, subscription links and exact traffic accounting.
- Group mass filtering, group membership bulk delete/import, retention/reporting and historical group consumption from the immutable billable ledger.
- Complete Telegram ecosystem and verified migration rehearsal before matching old public port and switching domain.

**This is development in the NEW PRO repo only. Production old DARK XRAY clients, representatives, balances, bots, nodes and domain remain unchanged.**
