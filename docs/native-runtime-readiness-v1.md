# DARK XRAY PRO — Native runtime readiness V1

## Goal

Continue after Node Manager V2, USA PRO Agent pairing, HUB LIVE and Node Fleet
Dashboard, and the Native Inbound same-port guard. The owner needs an accurate
picture of what is complete before a genuine customer listener can safely run.
This is a **diagnostic UI only**, not a substitute for receiving a durable
metering acknowledgment from the HUB.

## Read-only owner's Inbounds preflight

The existing private GET `/api/v1/workspace/native-inbounds-state` includes
`preflight` with a strict source/scope contract:

- `scope: "new_pro_lab_only"`
- Registered paired Node count from Registry (not a guessed IP list)
- Online and fresh-metrics Node counts from the already-cached real heartbeat
  and CPU/memory sampler, intersected with authenticated paired installations
- Enabled/nonarchived Native Inbound profiles, saved Node targets and binding
  count (binding count is **not** distinct customer count)
- Real state of the private PRO Usage Ledger
- `remote_metered_admission: "trusted_remote_epoch_registration_pending"`
- Explicit false `deployment_ready`, `customer_data_plane_verified` and
  `subscription_published` flags with
  `blocking_reason: "remote_metered_admission_not_wired"`

The read performs no Node RPC, Xray launch, DB mutation, per-customer usage
recalculation, background polling, SSH command or data-plane change. It uses
the same 5-second owner-session-only Native Inbounds state request that
already existed. Anonymous users, representatives, browser bearer-only
sessions and the Agent TLS listener cannot retrieve this information.

The new prominent Inbounds card is placed before Inbound creation and shows
the real prerequisite values, including paired/online/fresh Nodes, native
profile/target counts and ledger status. It keeps the final admission and
data-plane validation steps clearly locked. If the owner API is delayed or
unavailable, it removes fresh/online numeric claims rather than treating
outdated values as current or inventing zeros. Mobile RTL and light/dark
are supported.

## Important actual runtime blocker — do not bypass

The production Agent-only GET `/api/v1/agent/native-runtime` currently
returns HTTP **503** with `metered_runtime_admission_pending` when the
business Store has its Usage Ledger enabled. It is enabled on NEW PRO LAB.

The existing `managedxray.PrepareUsageAdmission` and
`ActivateUsageAdmission` machinery is **already well-tested in isolated
process/birth/receipt CI**. What remains to be implemented and independently
accepted is its **trusted distributed Agent-to-HUB handshake**. A remote
Agent must not self-declare process identity or billing authorization.
The trusted registration must exactly match the delivered installation,
node generation, signed config, REALITY client bindings and process birth;
the first zero observation must be durably committed and acknowledged
before opening any customer-facing listener. Failures must fence the new
runtime and retain the immutable prior accounting ledger.

Turning the metering flag off, accepting a self-reported client port,
or sending credentials outside this verified path is *not* a fix.

## Verified NEW USA Node capability (not a traffic acceptance)

On 2026-10-17 the separately paired US server `216.9.224.134`
successfully validated a disposable **NO-listener** JSON config with
the existing stock Xray `v26.3.27` as the unprivileged `darkproagent`.
The test created no customer port, modified no Agent service or persistent
identity, and removed its private ephemeral file afterward. That server's
existing stock Xray binary is **not the pinned CI Xray release** and is not
yet approved for the new metered runtime. This test does not establish a
working customer connection or measured customer bytes.

## Acceptance and deployment

- Existing 61+ Chromium cases, plus new owner EN/FA 320px, stopped API,
  and live preflight gating regression coverage
- Owner-only Go HTTP preflight contract: no private credentials in JSON;
  success from already-paired fresh heartbeat/real Inbound does not remove
  the 503 metered runtime gate
- Full Go -race/vet, true native and real-Xray tests, TypeScript/Vite build
- Deploy to isolated NEW PRO LAB with original data, paired Node identity
  and two-release rollback discipline; **no old DARK XRAY changes**.

The next functional milestone is the distributed receipt-gated
`Native Runtime Activation` integration and a disposable REALITY client
test on the independent USA Node, with consented test-only identities and
an explicit roll-back fence before any production user migration.
