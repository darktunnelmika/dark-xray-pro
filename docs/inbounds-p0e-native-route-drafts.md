# DARK XRAY PRO — P0-E Native route-draft integration

This phase implements a real Go Business Store and owner-authenticated API plus a compact native React editor. It stores only a planned route, never applies Xray or bills traffic.

The owner selects one Inbound and already paired NEW PRO Node; Direct and ordinary Tunnel use separate ports and Hosts, with up to 64 valid Hosts. Dark Swap is a distinct three-input planned route: upstream tunnel host, upstream tunnel port and destination Node port. The operator selects the upstream Node separately. Previous Native swap_port is not automatically considered a working tunnel.

All drafts use AES-256-GCM with the private Native key via HMAC domain separation, plus revision-bound AAD. The draft ledger is add-only with an immutable audit. Editing requires owner cookie, same Origin and CSRF; input is strictly validated and size bounded. Earlier real client/credit/Native target tables remain untouched. No Agent Apply or Xray restart takes place; ACK, data plane and accounting acceptance flags remain false.

Acceptance: real SQLite tests for multi-node isolation, collision, wrong installation, stale revision, bad Host, loop and event replay; owner API security, Go race/vet, 320px Chromium. Release requires Foundation CI and Native Metering success on both PR and main; new isolated HUB only, with consistent private DB/Nginx/systemd backups and previous V10b executable for rollback. OLD HUB, production Nodes, Bot and USA Xray are unchanged.

Pending: full P0-D signed ledger/Node registry bridge; Agent Apply+Rollback, REALITY handshake and accurate Direct/Tunnel/Swap accounting; full Inbounds/Nodes parity and encrypted multi-GB Backup/Dark Restore. Issues #1, #2, #3, #4, #22 and #60 remain blockers.
