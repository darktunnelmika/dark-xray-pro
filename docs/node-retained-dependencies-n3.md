# Node Manager N3 — retained dependency evidence

Extends N2's owner-only read endpoint with coverage identifier
`native_sqlite_and_retained_journals`. The browser and API ship together.
No lifecycle mutation or deployment is enabled. `can_retire` and `can_replace`
remain false; independent journal reads are not an atomic lifecycle reservation.

## Retained journals

The configured configstage store contributes total, pending and foreign-installation
Stages. The configured labrevision store contributes saved versions, selected and
previous versions, selection/rollback events, and foreign-installation versions.
These are lock-protected metadata snapshots from stores validated when opened;
the request does not reopen files or return private configurations. Counts include
old installations. Selection is LAB rehearsal metadata, not proof of a live process.
Preparation events are excluded from the selection/rollback event count.
Disabled stores produce null counts with explicit reasons, never fabricated zero.

## Historical command maps

New legacy-route and metered Stage producers save `dependency_nodes` inside their
existing authenticated encrypted payload. Both current candidate builders reject
Dark Swap, so the map contains only the destination logical node. A future
multi-node renderer MUST extend the map from the actual candidate at construction.
Apply, rollback and renew copy the original Stage map, including an absent legacy
map; current drafts are never used to guess historical dependencies.

In the same read-only SQL transaction as N2, the inspector authenticates both
command families across all nodes and installations, validates identities/maps,
and verifies each command references a same-family Stage with matching owner,
installation and map. It reports distinct source Stages owned by the requested
node, upstream command references from other owners, and the **global** number of
legacy commands without maps. Any legacy map absence makes upstream references
unknown (`historical_node_map_missing`), even if current drafts are empty.

The scan is limited to 512 commands and 8 MiB per encrypted payload, using a
513-row index probe. Exceeding either limit produces null for all three historical
fields with `command_history_limit`. Authentication/binding/Stage consistency
failure uses `command_history_unverifiable`; missing key uses
`inbound_key_unavailable`. No truncation is presented as a complete count.
Other independent index/journal counts may remain available. Existing N2 draft
authentication failures still fail the whole endpoint. The five-second SQL request
budget remains in effect. History and accounting records are never removed.

The optional encrypted JSON field needs no database migration. Old readers can
ignore it; their newly written commands will be explicitly unmapped on upgrade.
N1 registry rollback rules still apply to any combined release.

## Verification and remaining scope

Race contracts cover new and legacy Stage/apply propagation, metered propagation,
synthetic upstream references, invalid maps/ciphertext/bindings, scan limits,
missing keys, retained versions across selection rollback, foreign installations,
disabled stores and unchanged snapshots. Browser contracts cover the expanded
English/Persian view, 320px layout and explicit unknown historical maps.
Synthetic upstream fixtures do not implement or validate multi-node traffic.

Backups, live sockets/processes, and atomic retire/replace authorization remain
outside inspection. This is not an accounting audit or deployment approval.
No server is deployed by this change; old HUB and operational nodes remain
untouched. No real users, representatives, credits, bots or customer data migrate.
USA Xray stays disabled without explicit approval.
