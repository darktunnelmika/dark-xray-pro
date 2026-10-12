# New Node WAN TLS transport — isolated laboratory Phase 2

**Not production-ready.** This checkpoint enables a separately controlled HTTPS Agent-only listener for **new lab Node installations**, without exposing the owner dashboard or connecting to the original DARK XRAY HUB.

## Two independent listeners

| Role | Default | Routes |
|---|---|---|
| Owner dashboard + invite creation | `127.0.0.1:2087` **only** | Owner UI, local admin, fleet view |
| Remote LAB Agent | **disabled**; opt-in `:24443` | Only `GET /api/v1/agent/health` and `POST /api/v1/agent/{enroll,pulse,heartbeat}` |

The Agent listener requires direct **TLS 1.3**. It refuses plaintext requests even if reverse-proxied. Admin APIs, fleet JSON, static frontend and unrelated paths return 404 on this listener. Never publish or proxy the `2087` owner interface onto the public network.

**Identity validation is two-layered:** Go's regular X.509 trust-chain + hostname/IP SAN verification *and* a pinned SHA-256 hash of the server's leaf certificate. Pin alone is not enough; there is no `InsecureSkipVerify` fallback. No unencrypted remote HTTP and no redirect following, even for error responses.

The Agent still has its own distinct 256-bit pairing/operating credential and pinned installation ID. Only a trusted, explicitly authorized operator creates the one-time pairing ticket over a local SSH tunnel. The remote TLS listener never accepts an owner-token management command.

## Optional dedicated new HUB LAB test

**Only on newly provisioned, isolated test machines. Do not run on old HUB or old Nodes.** The following example uses a temporary, private lab CA to issue a certificate valid for the **new HUB public IP** so no domain is required.

Prepare a private working directory with `umask 077`. Generate a lab CA and a separate leaf certificate with an IP SAN and serverAuth EKU using OpenSSL. Protect the CA key and node server key. The public CA cert may be distributed to the new test Node, but **never distribute the CA private key**.

Minimal example on the NEW HUB (replace `NEW_HUB_IP` with its real IP; no file in these commands belongs on production):

```bash
umask 077
mkdir -m 700 -p /root/dark-pro-lab-tls
cd /root/dark-pro-lab-tls

openssl req -x509 -newkey rsa:3072 -nodes -days 30 \
  -keyout ca.key -out ca.pem -subj '/CN=DARK-PRO-ISOLATED-LAB-CA' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign'

openssl req -newkey rsa:3072 -nodes -keyout agent.key \
  -out agent.csr -subj '/CN=DARK-PRO-AGENT-LAB'

cat > agent.ext <<'EOF'
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=IP:NEW_HUB_IP
EOF

openssl x509 -req -in agent.csr -CA ca.pem -CAkey ca.key -CAcreateserial \
  -out agent.pem -days 7 -sha256 -extfile agent.ext
chmod 600 ca.key agent.key
openssl x509 -in agent.pem -outform DER | sha256sum
```

Replace the literal `NEW_HUB_IP` in `agent.ext` **before signing**. The last command shows the pinned public leaf SHA256 fingerprint; this is not a secret. Never add certificates or private keys to Git.

After the frontend is built and the owner LAB token is set privately, start the new HUB in a controlled terminal:

```bash
export DARK_PRO_LAB_MODE=1 DARK_PRO_ENABLE_REMOTE_AGENT=1
go run ./cmd/hub \
  -listen 127.0.0.1:2087 \
  -agent-tls-listen :24443 \
  -agent-tls-cert /root/dark-pro-lab-tls/agent.pem \
  -agent-tls-key /root/dark-pro-lab-tls/agent.key
```

The owner still issues the invitation through the localhost `2087` SSH tunnel, as documented in `lab-quickstart.md`. **Do not connect an existing Node Agent, old Pair Code, old tunnel or previous HUB credentials.**

On a **new isolated Node test machine** with the public lab CA certificate securely copied into `/root/dark-pro-lab-ca.pem`, enter the fresh Pair Code without posting it in a chat:

```bash
export DARK_PRO_LAB_MODE=1 DARK_PRO_ENABLE_REMOTE_AGENT=1
read -rsp 'New one-time Pair Code: ' DARK_PRO_PAIR_CODE; echo
export DARK_PRO_PAIR_CODE
go run ./cmd/node-agent \
  -id new-lab-01 -name 'New LAB node' \
  -hub https://NEW_HUB_IP:24443 \
  -hub-ca-file /root/dark-pro-lab-ca.pem \
  -hub-cert-sha256 YOUR_64_CHARACTER_CERTIFICATE_SHA256 \
  -identity-file ./lab-state/new-lab-agent.json
unset DARK_PRO_PAIR_CODE
```

The Agent validates its pinned TLS server identity and sends Pair Code/credential only through this authenticated channel. On receiving a successful enrollment acknowledgment, it persists the paired identity; retries after a dropped HTTP response reuse the existing identity and credentials.

## Explicit release blockers

- **No general production remote pairing yet**: only isolated opt-in lab TLS. Identity protection is certificate pin + per-agent bearer, not true mutually authenticated client certificates; mTLS may be added before release.
- Agent health is not Xray health: lightweight 3-second pulses and 5-second metrics stay separate; no Xray deployment or Lease/Accounting policy is performed.
- No browser-based admin authentication/public UI, no production-installed firewall rules, no packet guard or rate limiting on unpaired Agent endpoints.
- Certificate rotation requires an explicit new trusted pin; a temporary self-issued lab CA is not a public PKI strategy.
- Before any real deployment: test across distinct new machines, bandwidth/capacity, deliberate WAN outages, correct TLS and firewalls, replay fencing across service restarts and staged Node replacement.
- **Never pair a legacy production Node or attach the original customer DB** to this LAB service. The original HUB is out of scope.
