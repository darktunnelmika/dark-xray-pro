# Node replacement V2 — operator-controlled routes, Phase 3 draft

**No deployment or Xray mutation.** The pure Go `internal/routeplan` package is a side-effect-free, deterministic preview/validator for the user's new-server replacement flow. It is a building block, **not** an activated replacement workflow.

## Intended user flow

1. Open **Nodes → Replace Server** and select the logical Node being replaced.
2. Pair a completely **new** Agent/installation and verify its TLS identity. The candidate receives no legacy Node credentials.
3. Open **Inbound → Nodes & Deployment** for each selected Inbound. Explicitly select Direct and/or Tunnel for the NEW Node. There is no old configuration import or default route copy.
4. For Dark Swap, after selecting the Inbound and Node, supply exactly **Tunnel Port, Node Port and Tunnel Address**. The system validates syntax immediately; future remote preflight must also check tunnel reachability, listening ports, Xray compatibility and topology.
5. Show the complete **Preview** and its SHA-256 manifest. The preview carries the old logical ID for context and the new Node ID as the intended target, but does **not** copy any old tunnel.
6. Future stages will prepare desired Xray configuration on the new Agent, verify Agent revision ACK, probe Direct/Tunnel paths, confirm stable per-route accounting and render the exact impacted subscriptions. Only after explicit operator approval may a guarded transition publish the new endpoints.
7. The old Node/endpoint is retired **only after** successful cutover; failure must leave the previous deployment untouched. Production fleet has no relation to this lab.

## Phase 3 draft invariants

- `New(oldNodeID,newNodeID)` rejects same-ID/unsafe input. It begins with **no routes**.
- `SetDirect(inboundID,nodePort)` and `SetSwap(inboundID,tunnelPort,nodePort,address)` preserve both route choices on the same Inbound.
- `RemoveDirect` never removes Swap, and `RemoveSwap` never removes Direct.
- Tunnel and Node ports accept the full 1..65535 range; a same-port mapping (e.g. 1185→1185) is not incorrectly prohibited.
- Tunnel address is a **bare IP or DNS name**; no schemes, embedded ports, path segments, userinfo, whitespace or control characters.
- Preview is sorted, detached from internal mutable state and hashed with a deterministic manifest.
- `CanActivate` is always **false**. `NeedsNewAgentVerification` and `NeedsReachabilityProbes` are always **true** at this phase; syntactically valid settings are **not** proof that a real tunnel exists.
- Preview has no access to Node runtime, customer database, old Node configuration, old tunnels or subscription links.

## Required before implementing activation

- Add persistent replacement operation journal and revision fencing, independent per-node operations and race-safe cancellation/retry.
- Add a precise remote route reachability/real Xray probe, no guessed health or success.
- Preserve and reattribute all historical customer usage, representative quota and lifetime/period consumption without Node double-billing.
- New canonical Subscription URLs must publish only verified healthy routes; static-IP configs may need user resubscription.
- Test 750+ client fanout, tunnel/direct simultaneous paths, partial deployment failure, Agent crash, HUB outage, mis-ordered response, user declines confirmation and full rollback.
- Owner authorization, CSRF, immutable audit and secure per-Node desired-state checks are mandatory.

This phase is explicitly **not** a functional self-service replacement yet.

## Phase 4 follow-on

The initial hash-stable preflight preview is now durably journaled. A new Agent can opt into bounded TCP checks and submit exact authenticated port evidence, but `can_activate=false` remains mandatory until real Xray config staging, end-to-end protocol/data-plane checks and safe Subscription publication are implemented. See [Phase 4](node-replacement-phase4-tcp-preflight.md).
