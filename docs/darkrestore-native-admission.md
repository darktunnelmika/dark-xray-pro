# Imported-client native admission

The isolated single-loopback TCP/REALITY path now supports a distinct owner
admission after an immutable native process birth has registered its billable
epoch. Offline syntax registration alone cannot release a customer listener.

The owner admission records the original source/destination snapshot, payer,
entitlement, historical reservation, prepared plan and registered process in
one immutable grant. Updating the client to pending_node_apply and inserting
that grant share one transaction. The ordinary enable operation retains its
M5 guard.

The usage receiver checks this grant inside the same SQLite write transaction
that accepts the initial zero report. The exact registered epoch, node,
installation, live lease, unchanged snapshot and current prepared profile must
match. Missing grants, changed quotas and changed profiles refuse that
acknowledgment. The native runtime installs customer inbounds only after it
receives the durable zero receipt.

Later usage reports remain payable after revocation or expiry. They do not
grant a new lease or open a listener. Historical bytes stay in their immutable
reservation, native bytes stay in the usage ledger, and wallet credit is not
debited by admission or usage receipts.

Birth and admission schema extensions are independently validated. Predecessor
databases without either extension remain supported. The extensions and their
receipts are preserved by offline backup validation. An active prepared import
requires an admission record. An admission is not final serving-health or
all-source custody verification.

## Acceptance scope

TestRealImportedQuarantineAdmissionAndTraffic uses a synthetic imported client,
a real pinned experimental Xray core, an isolated loopback namespace, the real
TLS usage sender and receiver, a canonical client URI and a final TCP seal.
It verifies refusal before owner admission, actual traffic after the zero
receipt, exact final-report replay, unchanged representative wallets,
remaining entitlement minus historical and native usage, and offline
backup/reopen after serving.

The Store owner operation is exposed through the owner/CSRF-protected
handoff/quarantine/admission endpoint. Real Agent offline quarantine transport
has separate pinned HTTPS acceptance. Installed consumer serving and the
long-lived Agent admission supervisor still require integrated LAB acceptance.
Synthetic tests do not establish production inventory completeness or source
retirement.
