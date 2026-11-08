#!/usr/bin/env python3
"""Destructive acceptance ONLY on a fresh GitHub-hosted disposable runner.

Uses the real HTTPS API, installed CLI, Unix worker and systemd HUB. All data
and keys are generated fixtures. Existing installation paths and units refuse
before any mutation. Never run this script on PRO LAB or customer machines.
"""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import socket
import sqlite3
import ssl
import subprocess
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path('/var/lib/dark-xray-pro-lab')
CONTROL = Path('/var/lib/dark-xray-pro-backup-worker')
CONFIG = Path('/etc/dark-xray-pro-backup-worker')
ARCHIVES = Path('/var/backups/dark-xray-pro-native')
RELEASES = Path('/opt/dark-xray-pro-lab/releases')
HUB = 'dark-xray-pro-lab.service'
WORKER = 'dark-xray-pro-backup-worker.service'
BASE = '/api/v1/workspace/owner/backups'
ORIGIN = 'https://127.0.0.1:28443'
LAB_IP = '213.142.156.117'


def require(condition, code):
    if not condition:
        raise RuntimeError(code)


def command(*args, timeout=60, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, timeout=timeout, **kwargs)


def systemctl(action, unit):
    return command('/usr/bin/systemctl', action, unit)


def pid(unit):
    return int(command('/usr/bin/systemctl', 'show', unit, '--property=MainPID', '--value').stdout.strip())


def write(path, data, mode=0o600):
    with path.open('xb') as f:
        os.fchmod(f.fileno(), mode)
        f.write(data.encode() if isinstance(data, str) else data)
        f.flush()
        os.fsync(f.fileno())


def digest(data):
    return hashlib.sha256(data).hexdigest()


def fingerprint():
    # One read transaction, real SQLite types and no float conversion of credits.
    with sqlite3.connect(ROOT.as_uri() + '/business.sqlite?mode=ro', uri=True) as db:
        db.execute('PRAGMA query_only=ON')
        db.execute('PRAGMA trusted_schema=OFF')
        db.execute('BEGIN')
        names = [r[0] for r in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT GLOB 'sqlite_*' ORDER BY name")]
        result = {}
        for name in names:
            cursor = db.execute('SELECT * FROM "' + name.replace('"', '""') + '"')
            rows = []
            for row in cursor:
                values = [[type(v).__name__, v.hex() if isinstance(v, bytes) else v] for v in row]
                rows.append(json.dumps(values, separators=(',', ':'), ensure_ascii=True))
            result[name] = digest(json.dumps([list(cursor.description[i][0] for i in range(len(cursor.description))), sorted(rows)], separators=(',', ':')).encode())
        return result


class Browser:
    def __init__(self, cert):
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(cert))),
            urllib.request.HTTPCookieProcessor(self.cookies), urllib.request.ProxyHandler({}))
        self.csrf = ''

    def request(self, method, path, body=None, binary=None, headers=None):
        request_headers = {'Origin': ORIGIN, 'Sec-Fetch-Site': 'same-origin', 'X-CSRF-Token': self.csrf}
        if binary is None:
            data = json.dumps(body).encode() if body is not None else None
            request_headers['Content-Type'] = 'application/json'
        else:
            data = binary
            request_headers['Content-Type'] = 'application/octet-stream'
        request_headers.update(headers or {})
        req = urllib.request.Request(ORIGIN + path, data=data, headers=request_headers, method=method)
        try:
            with self.opener.open(req, timeout=15) as response:
                raw = response.read(8 * 1024 * 1024 + 1)
                require(len(raw) <= 8 * 1024 * 1024, 'bounded_ci_response')
                if response.headers.get_content_type() == 'application/octet-stream':
                    return response.status, raw
                return response.status, json.loads(raw)
        except urllib.error.HTTPError as error:
            error.read(4096)
            return error.code, None

    def login(self):
        code, data = self.request('POST', '/api/v1/session/login', {
            'username': 'backup-owner', 'password': 'synthetic-offline-backup-fixture-password'})
        require(code == 200 and data.get('authenticated'), 'synthetic_owner_login')
        self.csrf = data['csrf_token']

    def wait(self, kind, event, seconds=100):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            try:
                code, data = self.request('GET', BASE + ('/restores' if kind == 'restore' else ''))
                if code == 200:
                    for job in data['jobs']:
                        if job['event_id'] == event and job['state'] in ('succeeded', 'failed', 'recovery_required'):
                            return job
            except (OSError, urllib.error.URLError, ValueError):
                pass
            time.sleep(.2)
        raise RuntimeError('operation_did_not_reach_terminal_state')

    def record(self, path, body):
        # A lost response is reconciled by event ID, never blindly resubmitted.
        try:
            code, _ = self.request('POST', path, body)
            require(code in (200, 202, 502, 503, 504), 'operation_admission_refused')
        except (OSError, urllib.error.URLError):
            pass

    def upload(self, raw):
        sha = digest(raw)
        code, data = self.request('POST', BASE + '/uploads', {
            'event_id': 'ci-upload-' + uuid.uuid4().hex, 'size_bytes': len(raw), 'sha256': sha})
        require(code == 201, 'upload_allocate')
        identifier = data['upload']['id']
        code, data = self.request('PUT', BASE + '/uploads/' + identifier + '/content', binary=raw,
                                  headers={'X-Backup-SHA256': sha})
        require(code == 200 and data['upload']['state'] == 'received', 'upload_receive')
        return {'source_kind': 'upload', 'source_id': identifier, 'sha256': sha}


def wait_health(seconds=30):
    deadline = time.monotonic() + seconds
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    while time.monotonic() < deadline:
        try:
            with opener.open('http://127.0.0.1:2087/api/v1/health', timeout=2) as response:
                if response.status == 200 and json.load(response)['status'] == 'ok':
                    return
        except (OSError, urllib.error.URLError, ValueError):
            pass
        time.sleep(.2)
    raise RuntimeError('hub_health_unconfirmed')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--bundle', required=True)
    parser.add_argument('--fixture', required=True)
    args = parser.parse_args()
    os.umask(0o077)
    require(os.geteuid() == 0 and os.environ.get('GITHUB_ACTIONS') == 'true' and
            os.environ.get('RUNNER_ENVIRONMENT') == 'github-hosted' and
            os.environ.get('DARK_PRO_DISPOSABLE_RESTORE_ACCEPTANCE') == '1', 'disposable_runner_only')
    revision = os.environ.get('GITHUB_SHA', '')
    require(len(revision) == 40 and all(c in '0123456789abcdef' for c in revision), 'exact_ci_revision_required')
    bundle = Path(args.bundle).resolve()
    fixture = Path(args.fixture).resolve()
    repo = Path(__file__).resolve().parents[2]
    require(command('git', '-C', str(repo), 'rev-parse', 'HEAD').stdout.strip() == revision, 'source_revision_changed')
    paths = [ROOT, CONTROL, CONFIG, ARCHIVES, RELEASES.parent, Path('/run/dark-pro-disposable-restore-acceptance')]
    require(all(not p.exists() and not p.is_symlink() for p in paths), 'existing_installation_refused')
    for unit in (HUB, WORKER):
        state = command('/usr/bin/systemctl', 'show', unit, '--property=LoadState', '--value').stdout.strip()
        require(state == 'not-found', 'existing_unit_refused')
    addresses = json.loads(command('ip', '-j', '-4', 'address', 'show').stdout)
    require(not any(a.get('local') == LAB_IP for n in addresses for a in n.get('addr_info', [])), 'real_lab_address_refused')
    for port in (2087, 2088, 28443):
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', port))
    try:
        pwd.getpwnam('darkprolab')
    except KeyError:
        pass
    else:
        raise RuntimeError('existing_lab_user_refused')

    command('useradd', '--system', '--user-group', '--no-create-home', '--shell', '/usr/sbin/nologin', 'darkprolab')
    account = pwd.getpwnam('darkprolab')
    for path in (ROOT, CONTROL, CONFIG, ARCHIVES, RELEASES / revision):
        path.mkdir(parents=True, mode=0o700)
    os.chown(ROOT, account.pw_uid, account.pw_gid)
    os.chmod(ARCHIVES, 0o710)
    os.chown(ARCHIVES, 0, account.pw_gid)
    for p in (RELEASES.parent, RELEASES, RELEASES / revision):
        os.chmod(p, 0o755)
    write(Path('/run/dark-pro-disposable-restore-acceptance'), revision)
    history_key = secrets.token_hex(32)
    command(str(fixture), '-test.run=^TestExportDisposablePanelRestoreFixture$', '-test.v', timeout=90,
            env={**os.environ, 'DARK_PRO_TEST_FIXTURE_HISTORY_KEY': history_key})
    os.chown(ROOT / 'business.sqlite', account.pw_uid, account.pw_gid)
    write(ROOT / 'hub-state.lock', '')
    os.chown(ROOT / 'hub-state.lock', account.pw_uid, account.pw_gid)
    # Native inbound identity keeps its real TLS prerequisite. This listener
    # is loopback-only and the registry has no customer nodes.
    command('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
            '-keyout', str(ROOT / 'agent-key.pem'), '-out', str(ROOT / 'agent-cert.pem'),
            '-days', '1', '-subj', '/CN=127.0.0.1', '-addext', 'subjectAltName=IP:127.0.0.1')
    for name in ('agent-key.pem', 'agent-cert.pem'):
        os.chmod(ROOT / name, 0o600)
        os.chown(ROOT / name, account.pw_uid, account.pw_gid)
    names = ('dark-pro-hub', 'dark-pro-backup', 'dark-pro-backup-worker')
    binaries = {}
    for name in names:
        src = bundle / 'bin' / name
        dst = RELEASES / revision / name
        shutil.copyfile(src, dst)
        os.chmod(dst, 0o755)
        binaries[name] = str(dst)
    write(CONFIG / 'fixture.env', '\n'.join([
        'DARK_PRO_LAB_MODE=1', 'DARK_PRO_ENABLE_BUSINESS_CORE=1', 'DARK_PRO_ENABLE_USAGE_LEDGER=1',
        'DARK_PRO_ENABLE_REMOTE_AGENT=1',
        'DARK_PRO_ENABLE_NATIVE_INBOUNDS=1', 'DARK_PRO_NATIVE_INBOUND_KEY_HEX=' + history_key,
        'DARK_PRO_ENABLE_SELLER_MANAGEMENT_V2=1', 'DARK_PRO_ENABLE_VOLUME_REFUNDS_V1=1',
        'DARK_PRO_ENABLE_SELLER_ORDERS_V1=1', 'DARK_PRO_ENABLE_OFFLINE_BACKUP_V1=1',
        'DARK_PRO_ENABLE_BACKUP_CONTROL_V1=1', 'DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1=0',
        'DARK_PRO_LAB_TOKEN=' + secrets.token_hex(32)]) + '\n')
    env = dict(line.split('=', 1) for line in (CONFIG / 'fixture.env').read_text().splitlines())
    token = env['DARK_PRO_LAB_TOKEN']
    key = json.loads(command(binaries['dark-pro-backup'], 'keygen', '--out', str(CONFIG / 'recovery-identity')).stdout)
    worker_config = {
        'version': 1, 'restore_enabled': True,
        'hub_binary': binaries['dark-pro-hub'], 'hub_sha256': digest(Path(binaries['dark-pro-hub']).read_bytes()), 'hub_revision': revision,
        'backup_binary': binaries['dark-pro-backup'], 'backup_sha256': digest(Path(binaries['dark-pro-backup']).read_bytes()), 'backup_revision': revision,
        'public_recipient': key['recipient'], 'max_payload_bytes': 1 << 30,
        'backup_timeout_seconds': 90, 'stop_timeout_seconds': 30, 'resume_timeout_seconds': 30,
    }
    write(CONFIG / 'config.json', json.dumps(worker_config))
    hub_unit = f'''[Unit]
Description=Disposable native restore acceptance HUB
[Service]
Type=simple
User=darkprolab
Group=darkprolab
UMask=0077
EnvironmentFile={CONFIG}/fixture.env
WorkingDirectory={ROOT}
ExecStart={binaries['dark-pro-hub']} -listen 127.0.0.1:2087 -business-db {ROOT}/business.sqlite -registry {ROOT}/registry.json -replacements {ROOT}/replacements.json -staging {ROOT}/configs.json -lab-revisions {ROOT}/lab-revisions.json -agent-tls-listen 127.0.0.1:2088 -agent-tls-cert {ROOT}/agent-cert.pem -agent-tls-key {ROOT}/agent-key.pem
KillMode=control-group
TimeoutStopSec=30
Restart=no
'''
    write(Path('/etc/systemd/system') / HUB, hub_unit, 0o644)
    worker_unit = (repo / 'ops/systemd/dark-xray-pro-backup-worker.service').read_text()
    worker_unit = worker_unit.replace('/opt/dark-xray-pro-lab/bin/dark-pro-backup-worker', binaries['dark-pro-backup-worker'])
    # Explicit disposable-test grant; the shipped unit remains read-only.
    worker_unit = worker_unit.replace('ReadOnlyPaths=/var/lib/dark-xray-pro-lab', 'ReadWritePaths=/var/lib/dark-xray-pro-lab')
    write(Path('/etc/systemd/system') / WORKER, worker_unit, 0o644)
    command('ip', 'address', 'add', LAB_IP + '/32', 'dev', 'lo')
    command('/usr/bin/systemctl', 'daemon-reload')
    nginx = None
    opt = Path('/opt')
    require(opt.is_dir() and not opt.is_symlink() and opt.stat().st_uid == 0, 'unsafe_disposable_opt')
    original_opt_mode = opt.stat().st_mode & 0o7777
    try:
        # Hosted runners expose /opt as 0777 for tool installation. The real
        # worker correctly refuses writable executable ancestors. Tighten this
        # disposable host only, after all empty-host guards, not the validator.
        os.chmod(opt, original_opt_mode & ~0o022)
        systemctl('start', HUB)
        wait_health()
        systemctl('start', WORKER)
        command('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', str(CONFIG / 'https.key'),
                '-out', str(CONFIG / 'https.crt'), '-days', '1', '-subj', '/CN=127.0.0.1', '-addext', 'subjectAltName=IP:127.0.0.1')
        snippet = (repo / 'ops/nginx/native-backups.conf').read_text()
        conf = f'''daemon off;
pid {CONFIG}/nginx.pid;
error_log {CONFIG}/nginx-error.log;
events {{ worker_connections 128; }}
http {{ access_log off;
client_body_temp_path {CONFIG}/client-body;
server {{ listen 127.0.0.1:28443 ssl;
ssl_certificate {CONFIG}/https.crt; ssl_certificate_key {CONFIG}/https.key;
proxy_set_header Host $http_host;
location = /api/v1/session/login {{ proxy_pass http://127.0.0.1:2087; }}
location = /api/v1/session/status {{ proxy_pass http://127.0.0.1:2087; }}
{snippet}
location / {{ return 403; }}
}} }}
'''
        write(CONFIG / 'nginx.conf', conf)
        nginx = subprocess.Popen(['/usr/sbin/nginx', '-c', str(CONFIG / 'nginx.conf'), '-p', str(CONFIG)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        browser = Browser(CONFIG / 'https.crt')
        for attempt in range(100):
            try:
                code, status = browser.request('GET', '/api/v1/session/status')
                if code == 200:
                    break
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(.1)
        else:
            raise RuntimeError('https_start_failed')
        browser.login()
        code, status = browser.request('GET', BASE)
        require(code == 200 and status['capabilities']['restore'] and not status['settings']['schedule']['enabled'], 'restore_capability_or_schedule')
        baseline = fingerprint()
        create_event = 'ci-create-' + uuid.uuid4().hex
        browser.record(BASE + '/jobs', {'event_id': create_event, 'action': 'create'})
        backup = browser.wait('create', create_event)
        print(json.dumps({'fixture_backup': {k: backup.get(k) for k in ('state', 'phase', 'error_code', 'warning_code')}}), flush=True)
        require(backup['state'] == 'succeeded', 'real_backup_failed')
        require(fingerprint() == baseline, 'backup_changed_economic_data')
        code, status = browser.request('GET', BASE)
        require(code == 200, 'backup_inventory_read')
        archive = next(a for a in status['archives'] if a['id'] == backup['archive_id'])
        code, raw = browser.request('GET', BASE + '/archives/' + archive['id'] + '/download')
        require(code == 200 and digest(raw) == archive['sha256'], 'real_download_changed')

        def change_credits():
            body = {'event_id': 'ci-after-snapshot-' + uuid.uuid4().hex, 'representative_id': 'backup-rep-a',
                    'volume_delta_bytes': 137, 'unlimited_delta_units': 0, 'reason': 'Disposable acceptance after snapshot'}
            req = urllib.request.Request('http://127.0.0.1:2087/api/v1/admin/business/representatives/backup-rep-a/credits',
                data=json.dumps(body).encode(), headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token})
            with urllib.request.urlopen(req, timeout=10) as response:
                require(response.status == 200, 'post_snapshot_credit_change')
                response.read(4096)
            require(fingerprint() != baseline, 'target_not_changed_before_restore')

        restored_ids = []
        for source in ({'source_kind': 'archive', 'source_id': archive['id'], 'sha256': archive['sha256']}, browser.upload(raw)):
            change_credits()
            event = 'ci-restore-' + uuid.uuid4().hex
            request = dict(source, event_id=event, confirmation='RESTORE')
            browser.record(BASE + '/restores', request)
            result = browser.wait('restore', event)
            print(json.dumps({'fixture_restore': dict(source_kind=source['source_kind'], **{k: result.get(k) for k in ('state', 'phase', 'error_code', 'table_count', 'data_restored')})}), flush=True)
            require(result['state'] == 'succeeded' and result['data_restored'] and result['table_count'] == len(baseline), 'full_systemd_restore_failed')
            require(fingerprint() == baseline, 'restored_economic_rows_differ')
            before = pid(HUB)
            code, replay = browser.request('POST', BASE + '/restores', request)
            require(code == 202 and replay['replayed'] and replay['job']['id'] == result['id'], 'restore_replay_changed')
            time.sleep(.6)
            require(pid(HUB) == before and fingerprint() == baseline, 'completed_restore_reexecuted')
            restored_ids.append(result['id'])
        corrupt = bytearray(raw)
        corrupt[-8] ^= 1
        source = browser.upload(bytes(corrupt))
        before = pid(HUB)
        event = 'ci-corrupt-' + uuid.uuid4().hex
        browser.record(BASE + '/restores', dict(source, event_id=event, confirmation='RESTORE'))
        invalid = browser.wait('restore', event)
        require(invalid['state'] == 'failed' and not invalid['data_restored'], 'corrupt_archive_accepted')
        require(pid(HUB) == before and fingerprint() == baseline, 'validation_failure_stopped_or_changed_hub')
        systemctl('restart', WORKER)
        for _ in range(100):
            code, status = browser.request('GET', BASE + '/restores')
            if code == 200:
                break
            time.sleep(.1)
        require(code == 200 and all(any(j['id'] == identifier and j['state'] == 'succeeded' for j in status['jobs']) for identifier in restored_ids), 'restore_receipts_lost')
        require(pid(HUB) == before and fingerprint() == baseline, 'worker_restart_changed_hub')
        print(json.dumps({'status': 'passed', 'revision': revision, 'full_https_systemd_roundtrips': 2,
            'table_count': len(baseline), 'ciphertext_bytes': len(raw), 'same_request_replayed': True,
            'corrupt_archive_refused_before_stop': True, 'worker_restart_preserved_receipts': True,
            'economic_fingerprint': digest(json.dumps(baseline, sort_keys=True).encode()),
            'synthetic_only': True, 'production_touched': False}), flush=True)
    except Exception:
        # Only reached after the fresh-host guards and fixture creation. Keep
        # diagnostics bounded and never print environment values or private keys.
        for unit in (HUB, WORKER):
            state = subprocess.run(['/usr/bin/systemctl', 'show', unit,
                '--property=ActiveState,SubState,ExecMainCode,ExecMainStatus,Result', '--no-pager'],
                text=True, capture_output=True, timeout=10).stdout[:4096]
            journal = subprocess.run(['journalctl', '--unit', unit, '--no-pager',
                '--output=cat', '--lines=16'], text=True, capture_output=True, timeout=10).stdout[-8192:]
            for value in env.values():
                if len(value) >= 16:
                    journal = journal.replace(value, '[redacted fixture value]')
            print(json.dumps({'fixture_service': unit, 'state': state, 'journal': journal}), flush=True)
        raise
    finally:
        if nginx is not None:
            nginx.terminate()
            nginx.wait(timeout=10)
        for unit in (WORKER, HUB):
            subprocess.run(['/usr/bin/systemctl', 'stop', unit], capture_output=True, timeout=120)
        subprocess.run(['ip', 'address', 'del', LAB_IP + '/32', 'dev', 'lo'], capture_output=True, timeout=10)
        os.chmod(opt, original_opt_mode)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(json.dumps({'status': 'failed', 'error_type': type(error).__name__,
                          'reason': str(error) if type(error) is RuntimeError else 'acceptance_command_failed'}), flush=True)
        raise SystemExit(1)
