# N11: two Agent handover traffic acceptance

This CI-only acceptance uses two independently authenticated Agent runtimes,
separate encrypted journals, two customer ports, a pinned experimental core and
real VLESS Vision TLS streams. Both Agents run sequential cores inside one
disposable loopback-only Linux network namespace. The synthetic origin has no
route to the Internet. No LAB host, external Node or real customer is used.

Run `bash scripts/test-hub-active-traffic.sh --handover` on the disposable GitHub
runner with the same pinned binary environment as the existing active traffic
acceptance. The wrapper requires nonroot execution, verifies the binary hash,
compiles with the race detector, and removes only its own network namespace.
Customer ports remain outside the kernel ephemeral range.

Each of these cases must pass three times, with no skipped cases or race report:

- Normal owner-reviewed source drain, exact final settlement, fresh stopped
  source observation, then explicitly requested target Apply.
- Source drain ACK lost before HUB receipt acceptance.
- Source drain ACK accepted by the HUB but its response lost.
- Final usage accepted by the HUB but its response lost. Usage replay must
  preserve the failed transfer fence and never silently admit the target.

Successful transfers require both real streams to move bytes, the source stream
and process to stop before target birth, one initial complete zero receipt per
epoch, two distinct process epochs, exact per-Node final settlement seals, and
HUB accounting equal to the sum of both final reports. The bound idle customer
must remain at zero. Exact owner retries and ACK replay must not add a birth or
double bill usage. An explicit emergency target drain closes the acceptance.

Target Apply remains `target_applied_health_required`, with `can_publish=false`.
This is execution/accounting evidence, not protocol health, subscription
publication, cross-Node rollback, renewal or production readiness. Runtime
enablement, schema, APIs and deployment settings are unchanged by this step.
