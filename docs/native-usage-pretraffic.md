# Receipt-gated first activation of a fresh native Xray process

Baseline: PR #33 / `48980119278d4fa4d2841ca157b89037edc2257b`.
This increment is inside the actual PRO module. It does not install the LAB,
change old services or customers, publish subscriptions, or lift the ordinary
`metered_runtime_admission_pending` gate.

## Runtime transaction

`xrayconfig.WithUsageAPI` derives a new candidate BEFORE it is signed. It
preserves desired credentials, transport and assigned customer ports while
adding only the selected private loopback HandlerService/StatsService endpoint.
It produces a different config/manifest digest; an old lease cannot authorize it.

`Runtime.PrepareUsageAdmission` is limited to a pristine runtime. It validates
the signed final candidate, persists a version-3 encrypted preparation intent,
and launches the pinned stock Xray with NO customer inbounds. Only its private
API listener exists. Its readiness descriptor is internal and must never be
interpreted as a client listener or successful data-plane test. The returned
kernel-bound process identity is registered by the trusted HUB against the exact
previously delivered revision and complete server-derived customer bindings.

`Runtime.ActivateUsageAdmission` persists the epoch, creates its domain-separated
encrypted outbox and writes a complete sequence-1 zero report. This is zero for
this newly born process, NOT a reset of lifetime usage or imported customer data.
The exact initial receipt must be returned through the normal CA-verified,
leaf-pinned TLS sender and committed by the real ledger. An unavailable, wrong
or lost initial receipt never causes a customer port to open.

The original process's statistics endpoint is then checked, a fresh counter
tracker and independent observer are started, and a successful initial sample
is required. Only then does the pinned binary add the exact signed inbounds via
its local API. The process does not restart: birth, counters, authority and
outbox keep the same scope. The control socket's exact ownership and lease are
rechecked before mutation; CLI secrets stay in private temporary files and
stdout/stderr are discarded. API completion is not enough: the captured process
must own the intended customer listeners before the final state is recorded.

An expired lease, cancellation, partial API failure, persistence failure or
ambiguous start stops the captured child and retains the settlement fence.
Ordinary Apply/Recover cannot bypass the special admission marker. A prepared
version-3 journal cannot be accepted by the previous version-1/2 reader. A new
reader also rejects descriptor tampering, missing descriptors and downgrades.
Restart does not execute the stored final candidate without this obligation.

## Lazy counters are explicitly scoped, not a generic zero fallback

Stock Xray creates user counters on first use. `FreshCounters` is constructed
only in the trusted API-only startup path after its complete zero report has
been acknowledged. It permits BOTH never-seen directions to be absent in a
successful QueryStats result while a new user remains idle. Once a direction
exists, disappearance or regression is an error, including a previously seen
counter whose value was zero. Unknown users, duplicate JSON fields, partial
pairs, malformed data and failed API calls are never treated as zero. Integer
precision above JavaScript's safe range remains intact.

Generic Collect/ParseStats have not gained an idle exception. The fresh tracker
is not restored for a previous process. Crash recovery only replays durable
reports and retains unknown-tail holds; it does not infer a final zero tail.
The short activation call's context does not own the observer's entire lifetime;
the Runtime, independent guard and original short lease do.

## Evidence and remaining boundaries

`TestRealPinnedPretrafficUsageAdmission` uses stock Xray v26.9.9, the actual
Registry enrollment route, the real TLS Agent receipt handler, the project's
modernc Store and real VLESS/REALITY Vision clients. It requires these six named
cases: receipt withheld before first listener, initial receipt lost after COMMIT,
wrong certificate pin, lease expiry while waiting, restart while prepared, and
cancellation while waiting. Success also checks an idle second user does not
break observation and both users later receive independent exact accounting.
The new CI step runs this test with Race and rejects missing/skip/fail results.
Existing real browser-to-API/Xray/accounting tests remain mandatory.

The trusted test supervisor still registers the process birth using Store APIs.
Remote preparation/registration delivery and distributed volume admission are
NOT automatically enabled by these methods. This is not a per-user hard-quota
allocator or hitless revocation implementation; two successful first-use users
are not proof that user B survives revoking user A. Graceful final settlement,
reconciliation controls, transport-capability/performance acceptance and the
remaining full-panel/migration gates are still required. No patched core,
SOCKS egress architecture, public control API or deployment flag is introduced.

Two pre-existing negative permission tests now explicitly chmod their unsafe
temporary fixtures. They previously depended on an inherited permissive umask,
so under umask 077 they tested safe files while expecting unsafe-file rejection.
The production permission checks were not weakened or changed.
