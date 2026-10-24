# DARK XRAY PRO — Inbound Workspace V3 (UI foundation)

## Scope of this increment (24 October 2026)

- Added directly reachable Network, Security, Dark Swap, Hosts and Advanced stages while preserving existing Profile, Nodes & routes and Review.
- Every supported Network/Security/Hosts control edits the **same creation plan state**, including gRPC service, REALITY target/SNI/Short IDs and each Node's Direct/Tunnel host list.
- Dark Swap receives a dedicated stage *inside* Inbound creation/editing. Each selected destination's relay source, listener ports and external entry remain editable through the existing validated single-plan API, with original per-Node controls still available.
- A clear unavailable-feature catalog shows legacy transport/security options that cannot yet be applied, without allowing unsupported saves or claiming runtime success.
- Stage tabs scroll horizontally on 320 px screens, support keyboard navigation and preserve the footer.
- Only front-end source, browser tests and documentation change. No registry, node, customer, tunnel, ledger or live Xray state is touched.

## Important non-completions

This is **not** full functional legacy parity. The current backend supports only VLESS/REALITY with TCP/Vision or gRPC; existing plan saves are disabled and do not start listeners. VMess/Trojan/Shadowsocks, WS/XHTTP/HTTPUpgrade, None/TLS, Sniffing/Fallback/advanced JSON, advanced per-Host metadata, target scanning, local HUB execution, and live Dark Swap apply/ack/diagnostics must be developed end-to-end (validator, renderer, metering, health, subscription and regression tests) before enabling their controls.

## Acceptance

- TypeScript and Vite production build must pass.
- Full browser creation suite must pass, including cross-stage state retention, saved-plan CAS, event replay and 320px RTL.
- Test new supported fields in browser fixtures without writing to operational DBs.
- Deploy only reviewed artifacts to isolated PRO LAB after CI passes. Old HUB and customer traffic remain strictly out of scope.
