# Fleet audit — 2026-11-08 02:24 UTC

Live executable paths and SHA256 verified on Turkey HUB, Germany, Poland, UK,
USA and Finland. All run revision 3bedb8d872b830db35c108b13a1bf3c0f4eee3b2.
All five agents match SHA256 31fa2cc5c63766ede1d58645d9561615790419f98c75e4c3d85b2da0fcb53c00.
All six native cores match 6861c6275ce1ca8891e8b6b6432cac94a5859e870a86224dabcd4bf9d3a4ff69.
Services active, NRestarts=0 since activation (not a claim of continuous child uptime).

Eight real subscription paths passed 20/20 requests each, 160/160 in this batch.
Public GET/HEAD returned 200 with exact ledger usage, quota and expiry.
SQLite quick_check returned ok. Final snapshot: six active/ready/accepted targets,
zero reconciliations, client holds=0. Usage increased after traffic tests.

Regression: inboundexec and usageledger suites passed. Selected business/API
subscription and client directory tests passed. Full inboundruntime initially
failed TestWorkerEmptySaveFailureStillFencesReconciliation during fixture setup
with inbound_lease_scope_mismatch after 30.92s. Three isolated repetitions passed;
full inboundruntime retry passed in 14.443s. Timing sensitivity remains recorded,
not erased by the retry. No race, full product UI, GitHub CI or merge claim.

OPEN HIGH PRIORITY: HUB local native process hit lease_expired three times after
02:07 activation: 02:13:03, 02:19:30, 02:23:34 UTC. Final receipt succeeded each
time and current service recovered. At 02:23, login returned HTTP 503. Background
checkpoint logging recorded 27246 ms at 02:13 and 28745 ms at 02:23. These are
correlated observations; underlying cause is NOT established. No disk fault
claim. Requires root-cause repair of HUB latency/lease interruptions.

OPEN: Germany NTP remains unsynchronized. No renewal_refused/lease errors after
02:12 UTC following the earlier bounded clock correction. Other five hosts report
NTP synchronized. Ongoing clock synchronization is not permanently repaired.

This turn performed checks only; no fleet restart, ledger change or legacy
migration. Healthy bounded traffic results do not establish uninterrupted service.
