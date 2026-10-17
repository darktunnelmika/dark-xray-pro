# Native Inbound port safety — DARK XRAY PRO

The owner-facing Native Inbound target form previously *warned* when the
same nonzero port was selected for Direct and Dark Swap but still permitted
submitting the target. The business database also accepted the collision
within one profile, even though the deployment enforces port uniqueness
across different profiles.

This incremental fix rejects the ambiguous target **before any database
write** in both the core business validator and the owner browser. It does
not alter previously stored state, restart any Agent or Xray, migrate
legacy customers, or change the financial ledger. A Direct-only target
(swap=0) and Swap-only target (direct=0) remain valid. Distinct nonzero
ports remain valid, subject to existing reserved-port, installation and
cross-inbound protections.

The real Dark Swap transit tunnel and per-path accounting still require
separate validated functionality. Labeling an alternate listening port
"Dark Swap" does not prove transit configuration or billing attribution.

Acceptance:
- Go unit test proves a rejected shared-port attempt cannot mutate an
  existing valid native target or damage the two separately rendered
  listener configs.
- Owner-authenticated browser API must reject shared-port target (422),
  while permitting distinct ports.
- Chromium EN/FA 320px ensures saving remains disabled and no
  mutation API is requested for shared ports.
- All existing Go race/vet, Chromium, pinned real Xray and native
  metering acceptance CI must pass before isolated PRO installation.
- Old DARK XRAY HUB/Nodes and active USA PRO telemetry remain
  untouched.
