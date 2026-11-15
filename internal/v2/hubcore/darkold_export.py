#!/usr/bin/env python3
"""Export a DARK XRAY (0.10) panel for moving to Dark v2.

Runs on the old server. The live database is opened read-only; nothing on
this server is changed. Only what the move needs is copied (resellers,
logins, clients, Restore links, shop and wallets; not the traffic history),
Telegram bot tokens are decrypted with this panel's own key so the new
panel can run the same bots, and the result is uploaded to the new panel
once (the link dies after the upload).
"""
import gzip, json, os, sqlite3, sys, tempfile, urllib.request

SRC = os.environ.get("DARK_OLD_DB", "/var/lib/dark-xray/dark.sqlite3")
KEY = os.environ.get("DARK_OLD_KEY", "/var/lib/dark-xray/secret.key")
CONFIG = os.environ.get("DARK_OLD_CONFIG", "/etc/dark-xray/config.json")
TABLES = [
    "owners", "owner_profiles", "api_admins", "clients", "core_clients", "managed_clients",
    "unlimited_entitlements", "client_groups", "restore_subscriptions", "restore_usage", "restore_safety",
    "restore_domains", "customer_wallets", "customer_wallet_ledger", "customer_referrals",
    "commerce_products", "commerce_prices", "commerce_gateways", "commerce_orders",
    "telegram_customer_settings", "resource_credit_ledger",
]


def main():
    if len(sys.argv) < 2:
        print("usage: darkold_export.py UPLOAD_URL")
        sys.exit(2)
    url = sys.argv[1]
    src = sqlite3.connect("file:%s?mode=ro" % SRC, uri=True, timeout=30)
    have = {r[0] for r in src.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    fd, path = tempfile.mkstemp(suffix=".sqlite")
    os.close(fd)
    dst = sqlite3.connect(path)
    counts = {}
    for t in TABLES:
        if t not in have:
            continue
        ddl = src.execute("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", (t,)).fetchone()[0]
        dst.execute(ddl)
        cur = src.execute('SELECT * FROM "%s"' % t)
        cols = len(cur.description)
        n = 0
        while True:
            rows = cur.fetchmany(2000)
            if not rows:
                break
            dst.executemany('INSERT INTO "%s" VALUES (%s)' % (t, ",".join("?" * cols)), rows)
            n += len(rows)
        counts[t] = n
    # Bot tokens, decrypted with this panel's key (Fernet).
    dst.execute("CREATE TABLE export_bot_tokens(owner TEXT PRIMARY KEY, token TEXT NOT NULL, enabled INTEGER, admin_telegram_id INTEGER, bot_username TEXT)")
    if "telegram_bots" in have:
        try:
            from cryptography.fernet import Fernet
            f = Fernet(open(KEY, "rb").read())
            for owner, enc, en, admin, uname in src.execute("SELECT owner, token_enc, enabled, admin_telegram_id, bot_username FROM telegram_bots"):
                if not enc:
                    continue
                try:
                    tok = f.decrypt(enc.encode()).decode()
                except Exception:
                    continue
                dst.execute("INSERT INTO export_bot_tokens VALUES(?,?,?,?,?)", (owner, tok, en, admin, uname))
            counts["bots"] = dst.execute("SELECT count(*) FROM export_bot_tokens").fetchone()[0]
        except Exception as e:
            print("bot tokens not exported:", e)
    meta = {"panel": "dark-xray"}
    try:
        cfg = json.load(open(CONFIG))
        meta["public_origin"] = cfg.get("public_origin", "")
    except Exception:
        pass
    try:
        meta["version"] = open("/opt/dark-xray/VERSION").read().strip()
    except Exception:
        pass
    dst.execute("CREATE TABLE export_meta(k TEXT PRIMARY KEY, v TEXT)")
    dst.executemany("INSERT INTO export_meta VALUES(?,?)", list(meta.items()))
    dst.commit()
    dst.close()
    with open(path, "rb") as fh:
        body = gzip.compress(fh.read(), 6)
    os.remove(path)
    print("exported:", ", ".join("%s %d" % kv for kv in counts.items()))
    print("uploading %.1f MB..." % (len(body) / 1e6))
    req = urllib.request.Request(url, data=body, method="POST", headers={"Content-Type": "application/gzip"})
    with urllib.request.urlopen(req, timeout=300) as r:
        print(r.read().decode())


if __name__ == "__main__":
    main()
