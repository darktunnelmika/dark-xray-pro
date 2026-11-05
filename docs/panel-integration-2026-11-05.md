# Combined panel LAB candidate — 2026-11-05

This candidate combines node health (#147), client presets (#149), diagnostic handover coverage (#151) and Telegram store setup (#148) on the installed #139 baseline. It does not activate a bot, create clients, change schema or migrate production.

## Parallel node reconciliation

PR #150 uses the same #139 parent. Its business projection, projection test and node CSS match the retained node stack. Retain #147's server-bounded remaining observation lifetime and monotonic request-start expiry, conservative unknown/stale labels, engine-ready versus verified-health distinction, and protection against superseded successful/malformed/failed replies. The parallel stale-value browser expectation is superseded by stricter stale/unknown evidence cases; row retention and offline metrics remain covered. No parallel branch was force-updated.

## Handover evidence and limitation

The c79bc06 candidate had a real target Vision EOF in one lost-drain-ACK repetition. The cause remains unresolved. A separate non-race isolated run passed 30/30; source 634762e passed Foundation 41923434383 and Managed 41923434054 on the first attempt, including 30 required race-instrumented repetitions and all original accounting fences. This is improved acceptance evidence, not proof of a root-cause fix. Keep failure diagnostics and the failed historical run. The old c79 artifact remains ineligible for installation.

## Required release evidence

Require exact combined-source Foundation, Managed, Native and Telegram CI, verified artifact hashes/VCS stamps, fresh current-branch and running-executable checks, a shared release lock, official encrypted backups before/after, and preservation of business rows and settings. Install only on PRO LAB with all synthetic inbounds stopped and no configured bots. Verify the installed owner routes, English and Persian 320px layout, node freshness failure handling, presets without client creation, and Telegram store setup. CI success is not installed acceptance, a 24–48 hour fixed-version soak, or production approval.
