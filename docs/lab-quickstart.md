# First isolated lab run

**Never run these instructions on the existing DARK XRAY HUB.** This is a disconnected, pre-production Go+React prototype.

## Requirements

- Ubuntu 24.04 or modern Linux; Go >=1.23; Node.js >=22, npm; OpenSSL.
- No live Node Agents, previous HUB data, public bind, DNS or production secrets.

## Start the lab

Open a terminal on the **new isolated test machine**:

```bash
git clone https://github.com/darktunnelmika/dark-xray-pro.git
cd dark-xray-pro
# Use the foundation branch until CI passes and it is reviewed.
git switch feature/foundation-go-react-telemetry

export DARK_PRO_LAB_MODE=1
export DARK_PRO_LAB_TOKEN="$(openssl rand -hex 32)"

cd web
npm install --no-audit --no-fund
npm run build
cd ..
go test ./...
go run ./cmd/hub -listen 127.0.0.1:2087 -static web/dist
```

In **another terminal on the same machine**, export the **same random lab token** (do not paste it into tickets or screenshots) and run:

```bash
export DARK_PRO_LAB_MODE=1
# Set DARK_PRO_LAB_TOKEN to the exact random value from the HUB terminal.
go run ./cmd/node-agent -id new-lab-01 -name "New lab server"
```

Access the UI from a trusted computer using an SSH tunnel:

```bash
ssh -L 2087:127.0.0.1:2087 root@NEW_LAB_SERVER
```

Browse `http://127.0.0.1:2087/`. If the agent is not running, the Nodes view correctly shows no data. Turning off the agent marks it Offline **after 15 seconds**, without deleting it from the list. Network interface traffic is displayed separately from billable user accounting (not implemented yet).

Test read-only APIs:

```bash
curl -fsS http://127.0.0.1:2087/api/v1/health
curl -fsS http://127.0.0.1:2087/api/v1/fleet
```

**Do not place the lab API on a public network.** The production-grade new Agent protocol will require verified TLS, per-Node identity, enrollment and stronger authentication. Do not copy old Node credentials or customer databases into this prototype.
