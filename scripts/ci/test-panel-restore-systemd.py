#!/usr/bin/env python3
"""Run guarded disposable restore acceptance and bounded, secret-free diagnostics."""
import hashlib
import json
import os
from pathlib import Path
import pwd
import stat
import subprocess
import sys

sys.dont_write_bytecode = True
from panel_restore_fixture import main


def diagnose_runtime():
    marker = Path('/run/dark-pro-disposable-restore-acceptance')
    revision = os.environ.get('GITHUB_SHA', '')
    if (os.geteuid() != 0 or os.environ.get('GITHUB_ACTIONS') != 'true' or
        os.environ.get('RUNNER_ENVIRONMENT') != 'github-hosted' or
        os.environ.get('DARK_PRO_DISPOSABLE_RESTORE_ACCEPTANCE') != '1' or
        marker.is_symlink() or not marker.is_file() or marker.stat().st_uid != 0 or
        marker.read_text() != revision):
        return
    config = json.loads(Path('/etc/dark-xray-pro-backup-worker/config.json').read_text())
    user = pwd.getpwnam('darkprolab')
    print(json.dumps({'runtime_identity': {'euid': os.geteuid(), 'uid': user.pw_uid, 'gid': user.pw_gid}}), flush=True)
    for role in ('hub', 'backup'):
        path = Path(config[role + '_binary'])
        expected_root = Path('/opt/dark-xray-pro-lab/releases')
        if path.parent.parent != expected_root or path.parent.name != revision:
            print(json.dumps({'binary_role': role, 'fixed_path_valid': False}), flush=True)
            continue
        entries = []
        for parent in (path, *path.parents):
            st = parent.lstat()
            entries.append({'path': str(parent), 'uid': st.st_uid, 'gid': st.st_gid,
                'mode': oct(stat.S_IMODE(st.st_mode)), 'symlink': stat.S_ISLNK(st.st_mode), 'links': st.st_nlink})
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        metadata = []
        go_version = next((line.split()[1] for line in (Path(__file__).resolve().parents[2] / 'go.mod').read_text().splitlines() if line.startswith('go ')), '')
        tool = Path('/opt/hostedtoolcache/go') / go_version / 'x64/bin/go'
        if tool.is_file():
            result = subprocess.run([str(tool), 'version', '-m', str(path)], capture_output=True, text=True, timeout=15)
            metadata = [line.strip() for line in result.stdout.splitlines() if 'vcs.' in line or 'GOARCH=' in line]
        print(json.dumps({'binary_role': role, 'digest_matches': digest == config[role + '_sha256'],
            'expected_revision': config[role + '_revision'], 'build_metadata': metadata, 'ancestry': entries}), flush=True)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        try:
            diagnose_runtime()
        except Exception as diagnostic_error:
            print(json.dumps({'diagnostic_error_type': type(diagnostic_error).__name__}), flush=True)
        print(json.dumps({'status': 'failed', 'error_type': type(error).__name__,
            'reason': str(error) if type(error) is RuntimeError else 'acceptance_command_failed'}), flush=True)
        raise SystemExit(1)
