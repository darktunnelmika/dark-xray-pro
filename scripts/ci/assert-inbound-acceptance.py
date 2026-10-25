#!/usr/bin/env python3
"""Require positive Go test events; a skipped opt-in test is never acceptance."""
import argparse
import collections
import json
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("events", type=Path)
parser.add_argument("--matrix", action="store_true")
parser.add_argument("--require", action="append", default=[])
args = parser.parse_args()
events = [json.loads(line) for line in args.events.read_text().splitlines() if line]
required = set(args.require)
if args.matrix:
    base = "TestPinnedNativeInboundTrafficAndFinalReceipt"
    names = ["vless", "vmess", "trojan", "shadowsocks", "wireguard", "hysteria", "http", "mixed", "masque", "tunnel"]
    names += ["transport_" + v for v in ("kcp", "ws", "grpc", "httpupgrade", "xhttp", "xdrive")]
    names += ["tls_" + v for v in ("tcp", "ws", "grpc", "httpupgrade", "xhttp")]
    names += [v + "_mux_xudp" for v in ("vless", "vmess", "trojan")]
    names += ["reality_" + v for v in ("tcp", "grpc", "xhttp")]
    names += ["reality_tcp_vision_tls"]
    required |= {base} | {base + "/" + name for name in names}
if not required:
    raise SystemExit("at least one required positive test must be specified")
passed = collections.Counter(e.get("Test") for e in events if e.get("Action") == "pass")
missing = sorted(name for name in required if passed[name] != 1)
failed = [e.get("Test", e.get("Package", "unknown")) for e in events if e.get("Action") in ("skip", "fail")]
race = any("WARNING: DATA RACE" in e.get("Output", "") for e in events)
for event in events:
    if event.get("Output"):
        print(event["Output"], end="")
if missing or failed or race:
    raise SystemExit(f"positive inbound acceptance missing={missing}, skipped/failed={failed}, race={race}")
print(f"Verified {len(required)} required positive inbound acceptance events")
