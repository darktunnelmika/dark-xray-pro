# N7 — multiple independently approved metered route Nodes

The HUB can now keep up to 64 operator-approved metered loopback execution
channels at once. Each retains its own paired installation, pinned Xray digest,
command serial, Stage/Apply/Rollback receipts, runtime observation and usage
epochs. The browser selects a channel and retains uncertain requests by owner,
Node and installation, so changing Nodes never sends a previous request to the
new selection. Current paired identity is checked on every request.

All startup claims are committed in one SQLite transaction. Invalid, duplicate,
conflicting or non-loopback members reject the complete set. Existing identical
claims may be reopened after restart; installation and binary claims remain
immutable. Browser and Agent APIs cannot modify the allowlist.

The existing explicit LAB, native Inbound, usage-ledger and dedicated Agent TLS
gates remain required. For multiple Nodes, set `DARK_PRO_METERED_ROUTE_NODES_JSON`
to a JSON array of `node_id`, `installation_id`, `binary_sha` objects. It cannot
be combined with the three legacy single-Node scope variables. The single-Node
configuration remains compatible. Invalid JSON/unknown fields/trailing data
and oversized lists fail startup. Supply all members before serving requests.

This enables separate existing Stage/Apply/ACK engines for old and candidate
Nodes in isolated tests. Cross-Node cutover orchestration, terminal usage
settlement and subscription publication still need their own durable operation.
No LAB runtime flag is enabled by installing this release. The operational fleet,
old HUB data and USA Xray remain outside the test scope.
