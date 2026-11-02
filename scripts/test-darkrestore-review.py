#!/usr/bin/env python3
"""End-to-end offline Dark Restore private-review CLI acceptance.

Synthetic SQLite only. No live HUB, PRO database, real customer or network.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import tempfile

ROOT=Path(__file__).resolve().parent.parent
GO=os.environ.get("GO","go")
DDL_RE=re.compile(r"const fixtureDDL = `(.*?)`",re.S)
def run(args):
    return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=60,check=False)

def main():
    fixture=(ROOT/"internal/darkrestore/preflight_test.go").read_text()
    match=DDL_RE.search(fixture)
    assert match,"synthetic schema source missing"
    with tempfile.TemporaryDirectory(prefix="darkrestore-review-cli-") as directory:
        work=Path(directory)
        os.chmod(work,0o700)
        source=work/"old-offline.sqlite"
        review=work/"private-plan.json"
        binary=work/"darkrestore-review"
        with sqlite3.connect(source) as db:
            db.executescript(match.group(1))
            db.execute("INSERT INTO restore_groups VALUES('grp-one','Synthetic group','synthetic')")
            db.execute("INSERT INTO restore_group_targets VALUES('grp-one','{}')")
            for i in range(3):
                rid=f"private_source_client_{i}"
                email=f"private_core_email_{i}"
                token=f"private_token_{i}"
                deleted=1720000000 if i==1 else 0
                promoted=1720005000 if i==2 else 0
                db.execute("""INSERT INTO restore_subscriptions
                    (id,public_token,legacy_url,legacy_host,legacy_path,legacy_query,core_email,
                     inbound_ids,node_ids,legacy_upload,legacy_download,legacy_total,legacy_expire,
                     scan_status,enabled,first_seen,last_seen,group_id,promoted_at,promoted_owner,
                     deleted_at,deleted_reason,node_mode,include_local)
                     VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""",
                     (rid,token,"https://private.invalid/"+rid+"?token="+token,
                      "private.invalid","/"+rid,"token="+token,email,
                      "[1]","[]",30,40,100,1893456000,"verified",1,0,0,"grp-one",
                      promoted,"",deleted,"","selected",1))
                db.execute("INSERT INTO core_clients VALUES(?)",(email,))
                db.execute("INSERT INTO restore_safety(restore_id,metadata_state) VALUES(?,'verified')",(rid,))
                db.execute("INSERT INTO restore_usage VALUES(?,?,?,?)",(rid,"local",3,7))
                db.execute("INSERT INTO restore_events VALUES(?,'synthetic',1720000000)",(rid,))
            db.commit()
        os.chmod(source,0o600)
        original=source.read_bytes()
        sha=hashlib.sha256(original).hexdigest()
        built=run([GO,"build","-o",str(binary),"./cmd/darkrestore-review"])
        assert built.returncode==0,(built.stdout+built.stderr)[-1500:]
        cmd=[str(binary),"-offline-copy-verified","-source",str(source),"-expected-sha256",sha,
             "-as-of-unix","1780000000","-private-output",str(review)]
        executed=run(cmd)
        assert executed.returncode==0,(executed.stdout+executed.stderr)[-1500:]
        published=json.loads(executed.stdout)
        assert published["status"]=="ready_for_review" and published["import_enabled"] is False
        assert published["subscriptions"]==3 and published["groups"]==1
        assert review.stat().st_mode&0o077==0 and review.stat().st_size>100
        private=review.read_bytes()
        plan=json.loads(private)
        assert plan["source_sha256"]==sha and plan["summary"]["total_consumed_bytes"]=="240"
        assert plan["groups"][0]["historical_bytes"]=="240"
        assert [c["status"] for c in plan["clients"]]==[
         "requires_manual_review","archive_history_only","skip_already_promoted"]
        assert [c["historical_bytes"] for c in plan["clients"]]==["80"]*3
        assert plan["import_enabled"] is False
        assert hashlib.sha256(private).hexdigest()==published["review_sha256"]
        assert source.read_bytes()==original and not list(work.glob("*.sqlite-*"))
        for secret in (b"private_token_",b"private_core_email_",b"private.invalid",b"token="):
            assert secret not in private,secret
        replay=run(cmd)
        assert replay.returncode!=0 and review.read_bytes()==private and source.read_bytes()==original
        unsafe=run([str(binary),"-offline-copy-verified","-source",str(source),
            "-expected-sha256","f"*64,"-as-of-unix","1780000000",
            "-private-output",str(work/"unsafe.json")])
        assert unsafe.returncode!=0 and not (work/"unsafe.json").exists()
        print("DARKRESTORE_REVIEW_CLI_PASS: 3 synthetic identities, exact historical bytes, no secrets, no writes, no replay overwrite")

if __name__=="__main__":
    main()
