# Node Manager N5 — owner preflight journal

The owner workspace now shows nonactivating replacement preflights scoped to an
existing paired PRO Node. Both old and new node views show the same immutable
operation, manifest, exact revision and phase. TCP evidence is explicitly
labelled **not activated**. An empty or unavailable journal is never presented
as permission to retire a node.

Owner cancellation requires the browser session, same-origin CSRF token and the
observed revision. A lost reply can be retried at the journal layer; the UI
clears stale rows if the outcome is unknown and requires a fresh read. History
is retained, and no delete/create/activation or Agent report route is exposed
through the browser workspace. Other representatives cannot inspect or cancel.

The exact HTTPS allowlist adds GET per-node history and POST per-operation
cancel only. The shipped real-nginx test asserts allowed methods reach a
401 backend and all other methods, malformed paths, Agent/admin and activation
paths are denied before the HUB. Tests verify all owner/representative/CSRF,
other-node and revision boundaries under Race.

This is a management surface for the LAB preflight journal, **not** safe
replacement or cutover. Real Xray staging, route health, accurate usage
settlement, subscription publication and rollback remain separate gates. No
old HUB credentials, live customers or operational nodes are imported.
