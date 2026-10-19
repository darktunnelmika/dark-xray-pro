# DARK XRAY PRO — Inbounds P0-F graph safety

This feature prevents **cyclic Dark Swap transit drafts** within one
native Inbound before any plan may be saved. It does not apply or publish
routes and never changes Xray, client UUIDs, credits or Node leases.

P0-E already validates direct/tunnel/Swap port collisions, host syntax,
the paired target installation and stale Native Inbound revisions.
P0-F adds a bounded, per-Inbound directed graph of planned Swap hops.
Every previously saved stage is decrypted with the private Native key
and its original Inbound/Node/revision authenticated data, before checking
whether the proposed new edge forms a cycle. A corrupt or unreadable
encrypted upstream draft blocks the preview and write (fail closed).

Only previously saved planned hops are inspected. Existing Native
SwapPort values are not assumed to be an operational transit tunnel.
Two-Node and three-Node cycles are rejected before SQLite writes.
All graph components are checked, including a pre-existing cycle away from
currently selected Node. A saved draft's authenticated indexed ports and
installation must still match its current NEW native target. Tampered
ciphertext, altered indexed ports and replaced Node identities fail closed.
The owner HTTP preview uses the same authenticated key-aware check as
the save transaction, rather than trusting browser-provided topology.

Validation includes 750 synthetic Native clients across safe batches,
with unchanged identity fingerprint, immutable financial events,
Inbound bindings and native desired runtime revisions.

This is NOT the complete P0-F runtime gate. Independent paired NEW-Agent
health, provisioned upstream route, signed applied revision/ACK, real
VLESS+REALITY handshakes, previous-good automatic rollback and
exact-once downstream billing are still required before enabling any
customer traffic or considering the panel complete. OLD HUB, users,
representatives and bots remain untouched; no migration planning occurs.
