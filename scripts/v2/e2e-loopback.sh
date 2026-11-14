#!/bin/bash
# Real end-to-end acceptance of the v2 core with stock Xray, all on loopback.
# Proves: traffic flows, usage is exact, a HUB outage does not stop traffic,
# spooled usage arrives after recovery, and volume limits are enforced.
# Usage: XRAY=/path/xray HUB=/path/dark-hub2 NODE=/path/dark-node ./e2e-loopback.sh
set -u
X=${XRAY:-/usr/local/bin/xray}; HUB=${HUB:-./dark-hub2}; NODE=${NODE:-./dark-node}
E=${WORKDIR:-/tmp/dark-v2-e2e}
pkill -f "$E/" 2>/dev/null; sleep 1
rm -rf "$E"; mkdir -p "$E/node" "$E/tls"; cd "$E"
log(){ echo "[$(date -u +%T)] $*"; }
DB=$E/v2.sqlite
TOK=$($HUB node -db $DB -id de-test -name "loopback test")
cat > $E/inbound.json <<'J'
{"protocol":"vless","port":28580,"listen":"127.0.0.1","settings":{},"streamSettings":{"network":"tcp"}}
J
$HUB inbound -db $DB -tag de-test-vless -node de-test -file $E/inbound.json
$HUB client -db $DB -email alice -inbounds de-test-vless -volume-gb 1
$HUB client -db $DB -email bob -inbounds de-test-vless -volume-gb 0.00001
echo "$TOK" > $E/token
start_hub(){ setsid $HUB serve -db $DB -listen 127.0.0.1:18443 -tls-dir $E/tls -hosts 127.0.0.1 > $E/hub.log 2>&1 < /dev/null & echo $! > $E/hub.pid; sleep 1; }
start_hub
setsid $NODE -hub https://127.0.0.1:18443 -node-id de-test -token-file $E/token -hub-ca $E/tls/ca.pem -xray $X -state $E/node -stats 127.0.0.1:10085 > $E/node.log 2>&1 < /dev/null &
sleep 8
uuid(){ python3 -c "import sqlite3;print(sqlite3.connect('$DB').execute(\"select uuid from clients where email='$1'\").fetchone()[0])"; }
mkclient(){ cat > $E/client-$1.json <<J
{"log":{"loglevel":"warning"},"inbounds":[{"listen":"127.0.0.1","port":$2,"protocol":"socks","settings":{"udp":false}}],
"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":28580,"users":[{"id":"$3","encryption":"none"}]}]}}]}
J
setsid $X run -c $E/client-$1.json > $E/client-$1.log 2>&1 < /dev/null & }
mkclient alice 21080 "$(uuid alice)"; mkclient bob 21081 "$(uuid bob)"; sleep 2
fetch(){ curl -s -o /dev/null -w "%{http_code} %{size_download}" --max-time 20 --socks5-hostname 127.0.0.1:$1 "https://speed.cloudflare.com/__down?bytes=$2"; }
usage(){ python3 -c "import sqlite3;print(sqlite3.connect('$DB').execute(\"select coalesce(sum(up+down),0) from client_usage where email='$1'\").fetchone()[0])"; }
log "status:"; $HUB status -db $DB
log "alice 2MB: $(fetch 21080 2000000)"; sleep 22; log "hub usage alice=$(usage alice)"
log "--- HUB DOWN ---"; kill "$(cat $E/hub.pid)"; sleep 2
log "alice 3MB during outage: $(fetch 21080 3000000)"; sleep 22
log "spooled batches: $(ls $E/node/spool | grep -c batch)"
log "--- HUB UP ---"; start_hub; sleep 15; log "hub usage alice after recovery=$(usage alice)"
log "bob (10 KB limit): $(fetch 21081 200000)"; sleep 12
log "bob after limit (expect 000): $(fetch 21081 200000)"
restarts(){ $HUB status -db $DB | grep -o 'restarts=[0-9]*'; }
log "xray process $(restarts) (1 = never restarted)"
log "--- add carol while alice streams 20MB ---"
# Throttled to ~1 MB/s so the stream is still open when carol is added.
( r=$(curl -s -o /dev/null -w "%{http_code} %{size_download}" --limit-rate 1M --max-time 40 --socks5-hostname 127.0.0.1:21080 "https://speed.cloudflare.com/__down?bytes=15000000"); log "alice stream across the user change: $r" ) &
STREAM=$!
sleep 3
$HUB client -db $DB -email carol -inbounds de-test-vless -volume-gb 1
log "carol added at the HUB while alice streams"
wait $STREAM
mkclient carol 21082 "$(uuid carol)"; sleep 2
log "carol (added live): $(fetch 21082 100000)"
log "xray process $(restarts) after adding a user (expect 1)"
log "events:"; python3 -c "import sqlite3;[print(r) for r in sqlite3.connect('$DB').execute('select * from events')]"
log "status:"; $HUB status -db $DB
pkill -f "$E/"; log DONE
