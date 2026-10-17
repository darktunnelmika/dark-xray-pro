# DARK XRAY PRO — Groups and Representative UX V2

## Goal

Improve the hard-to-use Groups and Representatives sections while the owner
cannot currently sign into the new PRO panel. This is a **frontend-only**
improvement over existing authorized APIs; no session, data plane, traffic,
client identity, quota, account ownership or billing rules change.

## Groups — manage members without manually typing customer IDs

- From a real group card select **Manage members / مدیریت اعضا**. The
  selected group ID and independent per-group page offsets are held only as
  small, validated per-account IDs in sessionStorage, not customer records,
  credentials, UUIDs, API tokens or billing data.
- A new responsive charcoal/emerald group panel fetches 50 candidates
  from the **selected group's representative ID** through the existing
  authenticated `GET /api/v1/workspace/clients?representative_id=...`
  route, and independently fetches 50 current membership IDs from
  `GET /api/v1/workspace/groups/{id}/members`.
- Each display validates row types, explicitly checks candidate ownership
  against the selected representative and ties response snapshots to the
  exact group, candidate offset and membership offset. A stale, unavailable
  or incorrectly scoped response **never** activates the mutation controls.
- Search is explicitly **on the current client page**; paging is server-side,
  50 at a time. The selection can only include existing nonarchived
  candidates from the loaded page and is discarded on page navigation.
- Explicit confirmation precedes the existing atomic
  `POST /api/v1/workspace/groups/{id}/members` bulk operation. The actual
  server enforces representative scope, validates ALL supplied client IDs,
  and moves the selected clients transactionally from any prior group.
- Current members appear in a separately paged list, each with an explicit
  confirm-before-`DELETE /api/v1/workspace/groups/{id}/members/{client}`
  button. The operation detaches only this membership.
- Original manual ID assignment and removal forms remain available for
  administrators who prefer them, but are no longer required for everyday
  use. Deleting a selected group clears its selected ID and hides its
  membership panel; it does not erase client records or historical usage.

## Representatives

A representative card now offers **Select for credit / انتخاب برای اعتبار**
and **Select for login / انتخاب برای ورود** actions to fill the intended
account ID in the existing secure forms. These buttons **only change local
form state** — they cannot grant credits, set passwords or change server
state. The owner still has to enter an actual amount/reason or password
and submit the authorized CSRF-bound POST.

## Safety and testing

- No new Go routes, Nginx locations, Xray behavior, source of billing truth,
  Agent pairing or runtime flags.
- No old DARK XRAY node, customer, group, ledger, accounting or Xray code
  is modified; no PRO customer is automatically imported or created.
- No new cross-representative capability: group owner ID is obtained from
  server, and existing backend `allowedGroup` and tenant/CSRF checks
  independently enforce scope.
- Chrome browser coverage includes representative-scoped owner picker,
  atomic move/detach without finances, refusal of partial/stale API
  responses, per-account page persistence after refresh, RTL 320px and
  no hidden credit/login writes.
- Full GitHub Foundation CI, Go race/vet, pinned Xray, Chromium and
  Native Metering acceptance must pass prior to an isolated PRO release.
  Preserve the known-good PRO release and verify rollback. The
  still-pending owner-only USA loopback canary remains **unexecuted**.
