# Managed node disable and removal

Owner-only `POST /api/v1/workspace/nodes/{id}/lifecycle` accepts an installation-pinned, revision-checked `disable`, `remove`, or `enable` intent. GET on the same path returns the current administrative state and any pending intent for recovery. Use the exact event and body when retrying an ambiguous result.

The registry durably fences new deployment before SQLite detaches references. The registry and SQLite are separate stores: this is a recoverable fenced sequence, not a distributed atomic transaction. Failures retain the fence and pending request. Retrying after a process restart completes detachment; no offline node response is required. Genuine historical usage and late final receipts remain authenticated. No epoch is synthetically sealed, no uncertain usage is discarded, and lease durations are unchanged.

Within one business transaction, saved plan destinations and relay references are removed together with reviewed desired execution references. The old node is omitted from replacement operation targets. Surviving direct and tunnel paths are preserved; removing a relay source clears only its dependent Swap mapping. Immutable audit records, customer bindings, credits, UUIDs, quotas, subscription credentials, usage accounts, and receipts remain. Removing the final destination retains an editable inbound and its customers with no available path. Unapplied draft profile edits are not activated by detachment.

Disabled nodes remain administratively visible and cannot be selected as destinations. Removed nodes disappear from the roster and dashboard; their old identity remains reserved for accounting and cannot be re-enabled. Enabling a disabled node never reattaches its previous inbound memberships. Transient transport loss does not invoke this operation.

A node can continue until its already-issued 30-second lease expires. Disabling does not extend that lease. Existing accounting uncertainty remains subject to the accounting system's fail-closed rules. Live legacy handover/replacement protocols are separate from managed inbound execution and must be resolved before claiming their retirement complete.

Acceptance must distinguish unit/API tests, exact-head CI, installed binary revision, and actual LAB traffic. Passing a fixture is not evidence of fleet deployment.
