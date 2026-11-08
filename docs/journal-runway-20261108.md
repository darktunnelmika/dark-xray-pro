# Journal wait must not prevent final sealing

Source249fb8021e584d949af7f782090d51d8c3cfd13e. Temporary diagnostic builda41004b based on deployed0da4e0b added only opt-in SIGUSR2 runtime.Stack capture to private0700 /run directory; no public endpoint.100 snapshots over200s, all returned promptly. Worker observed in os.CreateTemp/openat from privateJournal.save -> Worker.save -> Worker.Step line310, and in journal fsync; other samples included deadline-bounded Observe and gRPC wait. Healthy diagnostic traffic90/90. This identifies a synchronous journal wait in the lease-handling goroutine; it is not evidence of hardware failure or a global Go scheduler freeze. Samples retained privately on HUB /root/dark-pro-runtime-samples-20261108.

Fix: during active-state journal persistence only, one writer goroutine owns the unchanged state. Foreground worker holds its normal mutex and waits; at12s remaining runway it asks the owned engine to seal traffic with a3s budget independently of the pending write. It joins the original write before changing state or starting the final write. Ordinary outbox remains byte-exact; final waits separately for its receipt, or becomes the next final report after an acknowledged ordinary sample. No receipt is sent before full durable save, no lease extension, no speculative accounting acknowledgement. Write failure still stops the process and fences future delivery. Hard lease kill remains independent.

Tests: full inboundruntime with pinned actual core passed12.471s. Additional real native hard-lease kill during blocked journal write passed; exact final replay after restart verified. Journal-focused real suite3.624s, targeted execution API1.484s, vet runtime/HUB/node-agent. Race not run: no C compiler installed. Source clean build metadata.

HUB SHA6a82c6e6ec892f5da8a796a838a465b148dfdbafdf666c3be41e7c200bf8fa08; agent SHAc53c3aa7a4eb19f46033181f5b7904ab9b311203a943df580e74d3445d170044. HUB installed, temporary diagnostic enablement removed. All five agents subsequently activated and verified below. Owner test921 reused after successful graceful stop; no reconciliation required. Shared912 unchanged.180-request live test begins immediately after activation: first request timed out during startup, subsequent requests successful so far; completed results below.

## Fleet and endpoint verification

All six live services now run249fb802; every binary rehashed matching the above HUB/agent hashes. Temporary artifact relays stopped. Shared912 still active, zero reconciliations. Six published paths each20/20 (120/120 total). Owner-only USA922 created after activation,60 requests59 successes, one SSL connection timeout at round24, next request recovered. No uncertain accounting epoch or owner reconciliation. This timeout is not attributed to an application cause without further evidence.

Turkey live result180 requests179 successes over378.63s; only first request during activation timed out, then179 consecutive successful connections. Controlled HUB restart returned subscription200/6paths in1.226s; local final_capture and final_receipt succeeded during shutdown. After restart912/921/922 allactive with accounting accepted and0 reconciliations. No owner-loss intervention in this R5 acceptance.

## Final accepted operational state

Explicit HUB Turkey and USA targets restored to shared owner-test plan912 through preview, optimistic plan update and reviewed execution apply. Six targets active and accounting accepted, no reconciliation. Public subscription unchanged URL, now8 paths with Persian Turkey HUB/USA labels. Both isolated921/922 stopped and archived after exact final settlement; no owner-loss clearance needed. Port18572 closed.

Eight published paths each20/20 successful HTTPS-through-VLESS Reality gRPC requests (160/160): Turkey HUB, USA, Finland, Germany direct/tunnel, Poland direct/tunnel, UK.84 five-second accounting samples during R5 test had0 holds. Controlled restart acceptance recorded above. These bounded tests do not claim indefinite zero outages; prior startup timeout and isolated USA timeout remain recorded. No legacy production, customer/representative migration, bots or DNS changed.

## Subsequent verification

Follow-up at approximately2026-11-08 01:48UTC: shared912 active, all six targets accounting accepted,0 reconciliations and shared client holds0. HUB NRestarts0; all five agents active on249fb802 with NRestarts0 and no uncertain/proof/fatal log lines since01:37UTC. Repeated independent eight-path test20/20 per path,160/160; together with the initial restored-subscription check this is320/320 across two bounded test batches, not uninterrupted load or an indefinite availability guarantee. No service restart, configuration mutation, or accounting clearance was needed in this follow-up.
