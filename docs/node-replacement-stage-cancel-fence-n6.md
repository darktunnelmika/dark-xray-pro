# Node replacement N6 — cancellation fences retained private Stage

The existing replacement Stage validates a candidate offline. It does not
apply a customer Xray configuration. N6 makes cancellation terminal across the
replacement, encrypted Stage, and private LAB selection HTTP workflows.

- Owner cancellation through either local-admin or browser workspace is
  serialized with private Stage creation, TLS delivery/ACK, LAB preparation,
  selection and rollback-selection. A cancelled operation cannot create a new
  Stage, redeliver a pending private candidate, accept late Agent syntax proof,
  prepare a retained verified Stage, or select its retained LAB version.
- Delivery filters pending Stage metadata against the exact current replacement
  operation, manifest and paired installation before decrypting a single
  candidate. Historical pending stages remain encrypted and do not shadow a
  fresh operation for the same Node. An ambiguous pair of eligible candidates
  fails closed.
- Cancellation and all Stage/LAB records remain intact for audit and rollback;
  there is no data deletion, Xray apply, subscription publication or customer
  migration. If a candidate was already delivered before cancellation, the
  Agent may finish its *offline* local check; its late report is rejected.

This is a prerequisite, **not** Stage/Apply/ACK traffic cutover. A future real
Apply must use a durable, jointly fenced receipt across the replacement and
runtime/accounting journals, verify full data-plane health, and preserve old
routes until rollback is proven. The current LAB remains data-plane disabled.
