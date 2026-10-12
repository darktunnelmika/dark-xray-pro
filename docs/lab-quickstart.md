# First isolated LAB: one-time Node enrollment

DARK XRAY PRO is a **pre-production laboratory prototype**. These instructions are ONLY for the new, unoccupied test machine. The original HUB, Agents, customer database, subscriptions and tunnels are not accessed. The Agent is explicitly restricted to the same machine as the LAB HUB; remote pairing and production TLS are not implemented yet.

## Requirements
Ubuntu 24.04 or equivalent, Go 1.23+, Node.js 22+, npm, OpenSSL, git. Port 2087 must be available on **127.0.0.1**. Clone the new repository:

```sh
git clone https://github.com/darktunnelmika/dark-xray-pro.git
cd dark-xray-pro
go test ./...
cd web && npm install --no-audit --no-fund && npm run build && cd ..
```

## Terminal A — start the LAB HUB

```sh
export DARK_PRO_LAB_MODE=1
export DARK_PRO_LAB_TOKEN="$(openssl rand -hex 32)"
go run ./cmd/hub -listen 127.0.0.1:2087 -registry ./lab-state/registry.json -static web/dist
```

Keep that process running. On the same machine in another shell which has the **exact same owner token** (enter it privately; do not copy it to the Agent), an operator creates a one-time invitation:

```sh
curl -fsS -X POST http://127.0.0.1:2087/api/v1/admin/nodes/invites \
  -H "Authorization: Bearer $DARK_PRO_LAB_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"node_id":"new-lab-01","name":"First independent LAB Node"}'
```

The response prints a one-time, 64-character hexadecimal `pair_code`, valid for ten minutes. It is a secret. Keep it private and do not paste it into public chats or GitHub. The HUB stores only its hash.

## Terminal B — Agent initial pairing

In a separate LAB terminal on the **same machine**, enter the code securely (without command-line flags or a visible command history):

```sh
cd dark-xray-pro
export DARK_PRO_LAB_MODE=1
read -rsp "One-time LAB Pair Code: " DARK_PRO_PAIR_CODE; echo
export DARK_PRO_PAIR_CODE
go run ./cmd/node-agent -id new-lab-01 -name "First independent LAB Node" \
  -identity-file ./lab-state/agent-new-lab-01.json
```

On first run, the Agent generates a **new random installation ID and per-Node token**, persists them in a 0600 private file, claims the code once, and begins independent 3-second liveness heartbeats and 5-second CPU/RAM/network telemetry. If an HTTP reply is lost, the Agent reuses the same saved identity and code on the next run; it does not demand a new code. Once paired, remove `DARK_PRO_PAIR_CODE` from the shell and run the Agent again **without** that environment variable. Do not share the Agent identity file or Node token.

```sh
unset DARK_PRO_PAIR_CODE
curl -fsS http://127.0.0.1:2087/api/v1/fleet
```

The Node stays in the roster as `offline` after a HUB restart or Agent shutdown. It becomes `online` only after an authenticated new heartbeat. A missing or first network-rate sample is shown as unavailable, not invented zero usage.

To open the UI remotely, forward port 2087 over SSH from your trusted computer: `ssh -L 2087:127.0.0.1:2087 root@NEW_LAB_SERVER`, then browse `http://127.0.0.1:2087/`.

## Hard boundaries and next milestones

- **No live Node connection is possible from the Agent:** only `http://127.0.0.1:2087` is accepted. Do not expose this LAB listener on `0.0.0.0` or a public reverse proxy.
- LAB pairing has a private one-time code and per-Agent token, **not** remote mTLS, hardware attestation, per-Node Lease or production RBAC. These remain required before real multi-server deployment.
- No old Node registry, credentials, customer traffic/accounting, products or Restore history are loaded by this software.
- The `/api/v1/fleet` display reports the new Node's default-route interface counters, not per-user traffic or Direct/Tunnel billing.
- No representative, Inbound, client, route deployment or Xray configuration is activated by this prototype.
- Further work is tracked in GitHub issues #1-#5. Keep your old HUB live until a separately approved migration.

The heartbeat and statistics clocks are intentionally different: stopping the Agent makes the Node Offline after more than 15 seconds, while temporary Linux metric errors show **Stats delayed** without changing Online. Unknown CPU/RAM values are never reported as genuine 0% utilization.

## Optional second-machine TLS lab

The original same-host test is unchanged. For strictly new and isolated machines, the Agent supports an opt-in HTTPS connection to a dedicated Agent-only TLS 1.3 HUB port, independently pinned and chain-validated. The owner panel remains on SSH-tunneled localhost port 2087. See [Phase 2: new Node WAN TLS test](node-remote-tls-lab-phase2.md). Do **not** expose the owner API publicly or reuse any legacy Node identity.
