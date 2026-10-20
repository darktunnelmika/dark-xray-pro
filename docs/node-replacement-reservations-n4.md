# Node Manager N4 — replacement reservations and cancellation replay

The replacement preflight journal must reserve both node roles. Previously A→B
and B→C could coexist because only old→old and new→new collisions were checked.
Creating an operation now conflicts if either logical node participates in any
non-cancelled operation, regardless of role or installation. Pending, failed-probe
and checked operations keep reservations until explicit cancellation; disjoint
node pairs remain independent. The journal lock serializes simultaneous requests.

Cancellation is terminal. A retry with the original cancellation revision returns
the same cancelled operation without rewriting history, including after restart.
The current cancelled revision is still accepted as before. Other revisions are
rejected, and late Agent evidence cannot revive a cancelled operation.

Opening an existing journal also rejects overlapping non-cancelled reservations.
There is no silent cancellation or data repair. If upgrading a LAB that already
contains such overlapping preflights, cancel the conflicting operation using the
previous version before upgrading, retaining the journal and its audit history.
The verified PRO LAB has no replacement operations; check again before deployment.

Race tests cover every role collision, restart preservation, concurrent creation,
independent pairs, release after cancellation, original-revision replay after a
probe result, unchanged history and malformed persisted overlaps. API regression
tests cover a repeated owner cancellation and retained Agent denial.

This increment does not activate replacement, move clients, change credentials,
remove nodes or settle usage. Atomic traffic cutover, final usage receipts and
subscription publication still require their own implementation and acceptance.
