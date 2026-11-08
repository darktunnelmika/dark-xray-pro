# Subscription quota/expiry fix — 2026-11-08

Code/deployed revision: 3bedb8d872b830db35c108b13a1bf3c0f4eee3b2.
HUB SHA256: 5ee632f5ee0aa7a92608f29dd1ee717f48c6285bd8e93cf301904fe8461dc5dd.
Agent SHA256: 31fa2cc5c63766ede1d58645d9561615790419f98c75e4c3d85b2da0fcb53c00.
All six PRO hosts activated this revision. Xray core unchanged.

The public subscription omitted Subscription-Userinfo although the owner client
had a 5368709120-byte allowance and expiry 1794694085. Add standard upload,
download, total (bytes), expire (Unix seconds) from the authorized transaction.
The existing ledger AccountTx includes historical reservations. Unknown/held
accounts retain rejection; unlimited data does not erase finite expiry. This is
per customer, independent of route count and runtime lease/readiness deadlines.

Both public and authenticated exports carry metadata. HEAD omits the body.
The deployed nginx subscription guard also rejected HEAD; the checked-in and
live guard now permits GET/HEAD only. nginx -t and reload succeeded.

Validation:
- Focused business/API subscription suites passed; go vet for affected packages passed.
- Tests cover actual usage reports, private/public parity, volume/unlimited,
  historical reservation, unknown/held usage, and GET/HEAD HTTP metadata.
- Live GET and HEAD both returned 200 and standard userinfo.
- Live total and expiry matched SQLite; counters did not exceed newer DB readings.
- Existing eight-route body SHA256 remained
  0351c41828144be0f91dd2109ff892981a6ccaa07d238b7c3351e99eac63e87d.
- Example remaining quota at verification: 3.5362392645 GiB.
- Final shared execution: six active/ready/accepted targets, no reconciliations.
- Eight real Reality gRPC paths: 5/5 each, 40/40 total (attached JSON).
- No GitHub merge/CI claim; no race test claim.

Separate operational finding: Germany renewal_refused began around 02:03 UTC,
before this deployment. Its clock lagged the NTP-synchronized HUB by
1.0172679424 seconds (bounded uncertainty 0.04870224 seconds). Its systemd NTP
service had zero received packets; four independent NTP endpoints timed out.
A bounded forward clock adjustment used the HUB TLS certificate SHA256 verified
through the authorized HUB device. Germany recovered without another restart;
final tests passed. This is recovery, not a permanent NTP connectivity fix.
Reliable ongoing clock synchronization remains an operations follow-up.

No legacy panel/customer/bot/DNS migration or ledger reset performed.
