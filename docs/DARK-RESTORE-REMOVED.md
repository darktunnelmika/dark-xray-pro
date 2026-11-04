# Dark Restore removed from DARK XRAY PRO

Owner decision, 2026-11-04: remove the Dark Restore product feature because its complexity is no longer justified. This supersedes earlier migration/import completion plans and any pending Dark Restore acceptance work.

Removed product surfaces:

- Clients import/review/handoff/preparation/promotion/Apply-acceptance/snapshot UI and the old navigation alias.
- Owner HTTP routes, advertised workspace capabilities, Agent quarantine/runtime routes and their nginx allowlist.
- HUB snapshot/handoff startup flags, imported health-verifier startup setup, and Agent quarantine/runtime worker modes.
- Dedicated Dark Restore command executables and their release-bundle entries.
- Tests and CI steps exclusively exercising the removed feature. New regression checks require the former routes to be absent and stale runtime configuration to fail before normal startup. Shared native runtime/accounting tests remain.

Preserved compatibility boundary:

Existing financial history, historical budget reservations, imported-origin labels, usage totals, immutable journals and encrypted backup validation must not be deleted or silently reclassified as native accounts. Internal business/schema/runtime readers and their regression tests remain where they protect those records. They do not re-enable the removed owner/API/Agent product entry points. Their presence is compatibility debt, not an active feature roadmap.

Native Clients, Groups, Representatives, native inbound execution, initial-Apply health, subscriptions, Dark Swap and ordinary PRO Backup/Restore remain in scope. Old bookmarks no longer open an import workspace. Old Dark Restore command flags are unsupported; enabled retired environment switches or retained private imported-verifier inputs are rejected before opening stores or starting workers, rather than silently falling back to ordinary Agent operation.

No production resource or customer record is removed by this source change. Do not run destructive SQL or drop historical tables as part of installing it. Deployment remains a separate exact-head CI and isolated LAB acceptance step. Prior Dark Restore documents are historical and must not be used to resume development or reinstall that feature.
