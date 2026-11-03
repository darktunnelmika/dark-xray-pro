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
has separate pinned HTTPS acceptance. The paired Agent runtime endpoint and the long-lived Agent supervisor now
have real pinned TLS acceptance, including renewed leases, owner revocation,
authority outage, a replacement HUB API instance, full outage with saved final
receipt recovery, lost birth replies and owner logout. Installed-binary
acceptance remains a separate gate.
Synthetic tests do not establish production inventory completeness or source
retirement.

## Agent supervisor opt-in

The owner approves the exact prepared plan, node installation, core SHA and
signer public key with NativeRuntime enabled on the quarantine approval route.
After the Agent registers its real API-only birth, the owner admits that exact
plan through the CSRF-protected quarantine/admission route.

The Agent uses DARK_PRO_ENABLE_DARKRESTORE_RUNTIME=1 and
DARK_PRO_ENABLE_FINAL_TCP=1, with the existing pinned HTTPS HUB transport,
independent DARK_PRO_QUARANTINE_PLAN_SHA, private
DARK_PRO_QUARANTINE_SIGNER_FILE and DARK_PRO_NATIVE_RUNTIME_KEY_HEX.
The managed-runtime-dir must be new private PRO state, and managed-listen-ip
is restricted to 127.0.0.1 for this accepted scope. Other native executors are
mutually exclusive with this supervisor.

The supervisor obtains a signed startup lease, registers the real signed birth
including its pinned core SHA, waits for owner admission, and acknowledges the
initial zero report before installing customer inbounds. Later leases retain
the same process epoch and check current ownership, entitlement, history,
holds, profile and remaining budget. Renewals follow the durable owner grant
and the paired node, independently of the owner's browser session.

Revocation or unavailable authority stops renewal and triggers final TCP
settlement. A saved exact final receipt can be recovered after Agent restart
without a new listener or epoch. An unproven tail remains held and closed.
An owner approval must be staged again after HUB restart. Automatic service
reactivation after recovery is outside this acceptance.
