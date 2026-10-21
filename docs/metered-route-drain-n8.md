# N8 — stop a metered LAB runtime with an exact final receipt

The owner can queue `drain` for the reviewed current Stage and exact unsealed
usage epoch. The command has no runtime lease and cannot register or start a
new child. It records execution intent, invokes the existing final TCP barrier,
stops the old process, waits for its terminal usage report and HUB settlement,
then saves a signed `drained` receipt. Saved configuration remains available
for a separately reviewed rollback with fresh eligibility and a new epoch.

Stopping remains available after client revocation, expired products or stale
drafts: those conditions must block new admission, not the closure of old
traffic. The command still binds Node, installation, runtime instance, source
Stage, current epoch, expected runtime state and serial. The HUB verifies the
exact final receipt against its own ledger before recording success.

An uncertain/failed final counter or settlement keeps the process stopped and
preserves the usage recovery fence. Retries replay the same durable receipt;
they never start a replacement process or zero usage. An interrupted intent
requires existing explicit recovery, and cannot fabricate a terminal seal.

CI includes actual active Vision traffic, revocation before drain, lost ACK,
restart replay, exact ledger totals and no new process birth. Browser tests
cover explicit confirmation and binding to the reviewed epoch.

Compatibility: `drain` is supported only by the metered v2 supervisor in this
release. Older supervisors reject the command and cannot read a transaction
journal whose last command/receipt is `drain`; rollback must use matching full
state/config backups. No DB schema changes are introduced. Installation alone
does not enable a LAB runtime or activate the USA test server.
