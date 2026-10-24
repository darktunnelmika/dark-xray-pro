// Real compiled UI → authenticated API → SQLite TLS acceptance. No API mocks.
import { chromium, expect } from '@playwright/test';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer, request as httpRequest } from 'node:http';
import { isAbsolute } from 'node:path';

const base = new URL(process.env.DARK_PRO_QA_UI_URL || '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port || base.username || base.password) throw new Error('QA loopback only');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
if (!control.username || !control.password || !isAbsolute(control.result_path || '') || !control.first?.private_key_pem || !control.renewed?.private_key_pem) throw new Error('Invalid TLS QA control');
const api = '/api/v1/workspace/native-inbound-plans';
const diagnostics = [];
let stage = 'launch', inboundID = 0, browser, page;

const proxy = createServer((request, response) => {
  let target;
  try { target = new URL(request.url); } catch { response.writeHead(403).end(); return; }
  if (target.origin !== base.origin || target.username || target.password) { response.writeHead(403).end(); return; }
  const headers = { ...request.headers, host: base.host };
  delete headers['proxy-connection']; delete headers['proxy-authorization'];
  const upstream = httpRequest({ hostname: '127.0.0.1', port: base.port, method: request.method, path: target.pathname + target.search, headers }, incoming => {
    response.writeHead(incoming.statusCode, incoming.headers); incoming.pipe(response);
  });
  upstream.on('error', () => { diagnostics.push('Harness transport failure'); response.writeHead(502).end(); });
  request.on('aborted', () => upstream.destroy());
  request.pipe(upstream);
});
proxy.on('connect', (_request, socket) => { socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n'); });
await new Promise((resolve, reject) => { proxy.once('error', reject); proxy.listen(0, '127.0.0.1', resolve); });

function publicOnly(value) {
  if (typeof value === 'string') {
    if (value.includes('-----BEGIN') || value.includes(control.first.private_key_pem) || value.includes(control.renewed.private_key_pem)) throw new Error('Public response contains PEM credentials');
    return;
  }
  if (!value || typeof value !== 'object') return;
  for (const [key, nested] of Object.entries(value)) {
    if (['tlscredentials', 'certificatepem', 'privatekeypem', 'privatekey', 'realityprivatekey', 'config'].includes(key.replace(/[_-]/g, '').toLowerCase())) throw new Error('Private field in public TLS response');
    publicOnly(nested);
  }
}
function safePlan(plan, revision, sha, transport) {
  publicOnly(plan);
  expect(plan).toMatchObject({ plan_revision: revision, inbound_revision: revision, runtime_apply: false, editable: true, status: 'SAVED_DISABLED',
    profile: { security: 'tls', transport, revision, public_key: '', enabled: false, archived: false, bound_clients: 0,
      server_names: ['edge.example.org'], tls: { sha256: sha, dns_names: ['edge.example.org', 'tunnel.example.org'] } } });
  expect(plan.profile.reality_target || '').toBe('');
  expect(plan.profile.short_ids || []).toEqual([]);
  expect(plan.profile.tls.public_key_sha256).toMatch(/^[0-9a-f]{64}$/);
  expect(plan.profile.tls.not_before_unix).toBeLessThan(Date.now() / 1000);
  expect(plan.profile.tls.not_after_unix).toBeGreaterThan(Date.now() / 1000);
}

try {
  browser = await chromium.launch({ headless: true, proxy: { server: 'http://127.0.0.1:' + proxy.address().port, bypass: '127.0.0.1' } });
  page = await browser.newPage({ viewport: { width: 1280, height: 900 }, locale: 'en-US' });
  await page.addInitScript(() => { localStorage.setItem('dark-pro-lang', 'en'); });
  page.setDefaultTimeout(12000);
  page.on('pageerror', () => diagnostics.push('Browser page error'));
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.origin !== base.origin) diagnostics.push('Unexpected external request');
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
      const allowed = request.method() === 'POST' && ['/api/v1/session/login', api, api + '/preview'].includes(url.pathname) ||
        request.method() === 'PUT' && inboundID > 0 && url.pathname === api + '/' + inboundID;
      if (!allowed) diagnostics.push('Unexpected mutation ' + request.method() + ' ' + url.pathname);
    }
  });
  const observe = (method, path) => page.waitForResponse(response => new URL(response.url()).origin === base.origin && new URL(response.url()).pathname === path && response.request().method() === method);
  async function connectionGroup(dialog, group) {
    await dialog.getByRole('tab', { name: 'Connection', exact: true }).click();
    const details = dialog.locator('[data-connection-group="' + group + '"]');
    if (await details.getAttribute('open') === null) await details.locator(':scope > summary').click();
  }
  async function credentials(dialog, value) {
    await connectionGroup(dialog, 'security');
    await dialog.getByLabel('TLS certificate (PEM)', { exact: true }).fill(value.certificate_pem);
    await dialog.getByLabel('TLS private key (PEM)', { exact: true }).fill(value.private_key_pem);
  }
  async function preview(dialog, revision, sha, transport, status = 200) {
    await dialog.getByRole('tab', { name: 'Review', exact: true }).click();
    const pending = observe('POST', api + '/preview');
    await dialog.getByRole('button', { name: 'Check complete setup', exact: true }).click();
    const response = await pending;
    expect(response.status()).toBe(status);
    const payload = await response.json(); publicOnly(payload);
    if (status !== 200) return payload;
    expect(payload).toMatchObject({ valid: true, runtime_changed: false, topology_validated: true, topology_validation_only: true });
    safePlan(payload.plan, revision, sha, transport);
    await expect(dialog.getByRole('button', { name: 'Save paused setup', exact: true })).toBeEnabled();
    return { value: payload, body: response.request().postDataJSON() };
  }
  async function save(dialog, revision, sha, transport, checked) {
    const pending = observe(revision === 1 ? 'POST' : 'PUT', api + (revision === 1 ? '' : '/' + inboundID));
    await dialog.getByRole('button', { name: 'Save paused setup', exact: true }).click();
    const response = await pending;
    expect(response.status()).toBe(revision === 1 ? 201 : 200);
    const receipt = await response.json(); publicOnly(receipt);
    expect(receipt).toMatchObject({ runtime_changed: false, clients_deleted: false, credit_changed: false, usage_reset: false, subscription_published: false });
    safePlan(receipt.plan, revision, sha, transport);
    if (JSON.stringify(response.request().postDataJSON()) !== JSON.stringify(checked.body)) throw new Error('Preview/save request mismatch');
    expect(receipt.plan.profile.tls).toEqual(checked.value.plan.profile.tls);
    await expect(dialog).toBeHidden();
    return receipt;
  }
  async function reopen(revision, sha, transport) {
    await page.reload({ waitUntil: 'domcontentloaded' });
    const open = page.getByRole('button', { name: 'Open saved setup', exact: true });
    await expect(open).toBeEnabled();
    const pending = observe('GET', api + '/' + inboundID); await open.click();
    const response = await pending; expect(response.status()).toBe(200);
    const value = await response.json(); publicOnly(value); safePlan(value.plan, revision, sha, transport);
    const dialog = page.getByRole('dialog', { name: 'Edit Inbound setup', exact: true });
    await expect(dialog).toBeVisible();
    await connectionGroup(dialog, 'security');
    await expect(dialog.getByRole('radio', { name: 'TLS', exact: true })).toBeDisabled();
    await expect(dialog.getByLabel('TLS private key (PEM)', { exact: true })).toHaveCount(0);
    await expect(dialog.getByRole('button', { name: 'Replace certificate and key', exact: true })).toBeVisible();
    return dialog;
  }
  async function noBrowserStorageSecrets() {
    const raw = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }));
    if (raw.includes('-----BEGIN') || raw.includes('private_key_pem') || raw.includes('tls_credentials')) throw new Error('PEM persisted in browser storage');
  }

  stage = 'owner sign-in';
  await page.goto(new URL('/inbounds', base).href);
  await page.getByLabel('Username', { exact: true }).fill(control.username);
  await page.getByLabel('Password', { exact: true }).fill(control.password);
  const caps = observe('GET', api + '/capabilities');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const response = await caps; expect(response.status()).toBe(200);
  const capabilities = await response.json(); publicOnly(capabilities);
  expect(capabilities).toMatchObject({ version: 3, profile_tls: true, profile_websocket: true, profile_http_upgrade: true, runtime_apply: false });
  await page.getByRole('button', { name: 'Add Inbound', exact: true }).click();
  const created = page.getByRole('dialog', { name: 'Create Inbound', exact: true });
  await expect(created.getByRole('tab')).toHaveText(['Connection', 'Dark Swap', 'Advanced', 'Review']);
  stage = 'TLS WebSocket creation';
  await created.getByLabel('Inbound name', { exact: true }).fill('Real browser TLS');
  await connectionGroup(created, 'security');
  await created.getByRole('radio', { name: 'TLS', exact: true }).check();
  await created.getByLabel('Server names (comma-separated)', { exact: true }).fill('edge.example.org');
  await credentials(created, control.first);
  await connectionGroup(created, 'network');
  await created.getByRole('radio', { name: 'WebSocket / TLS', exact: true }).check();
  await created.getByLabel('WebSocket Path', { exact: true }).fill('/socket');
  await created.getByLabel('WebSocket Host', { exact: true }).fill('edge.example.org');
  await expect(created.getByLabel('REALITY target (host:port)', { exact: true })).toHaveCount(0);
  await expect(created.getByLabel('Short IDs', { exact: true })).toHaveCount(0);
  await created.getByRole('tab', { name: 'Dark Swap', exact: true }).click();
  await created.getByRole('checkbox', { name: 'Select destination: Germany', exact: true }).check();
  await created.getByLabel('Destination listening IP: de', { exact: true }).fill('198.51.100.10');
  await created.getByLabel('Direct port: de', { exact: true }).fill('10443');
  await created.getByLabel('Direct Hosts: de', { exact: true }).fill('edge.example.org');
  await created.getByRole('checkbox', { name: 'Tunnel: de', exact: true }).check();
  await created.getByLabel('Tunnel port: de', { exact: true }).fill('1185');
  await created.getByLabel('Tunnel Hosts: de', { exact: true }).fill('tunnel.example.org');
  await created.getByRole('tab', { name: 'Review', exact: true }).click();
  await created.getByLabel('Change note', { exact: true }).fill('Real TLS WebSocket setup');
  const initialPreview = await preview(created, 1, control.first_sha256, 'ws');
  const firstReceipt = await save(created, 1, control.first_sha256, 'ws', initialPreview);
  inboundID = firstReceipt.plan.profile.id;
  await noBrowserStorageSecrets();

  stage = 'retained TLS key across transport edit';
  const edited = await reopen(1, control.first_sha256, 'ws');
  await connectionGroup(edited, 'network');
  await edited.getByRole('radio', { name: 'HTTPUpgrade / TLS', exact: true }).check();
  await edited.getByLabel('HTTPUpgrade Path', { exact: true }).fill('/upgrade');
  await edited.getByLabel('HTTPUpgrade Host', { exact: true }).fill('edge.example.org');
  await edited.getByRole('tab', { name: 'Review', exact: true }).click();
  await edited.getByLabel('Change note', { exact: true }).fill('Retained certificate HTTPUpgrade');
  const retainedPreview = await preview(edited, 2, control.first_sha256, 'httpupgrade');
  expect(Object.hasOwn(retainedPreview.body, 'tls_credentials')).toBe(false);
  await save(edited, 2, control.first_sha256, 'httpupgrade', retainedPreview);

  stage = 'certificate/key mismatch is rejected by actual API';
  const renewal = await reopen(2, control.first_sha256, 'httpupgrade');
  await renewal.getByRole('button', { name: 'Replace certificate and key', exact: true }).click();
  await credentials(renewal, { certificate_pem: control.renewed.certificate_pem, private_key_pem: control.first.private_key_pem });
  await renewal.getByRole('tab', { name: 'Review', exact: true }).click();
  await renewal.getByLabel('Change note', { exact: true }).fill('Renewed TLS certificate');
  const invalid = await preview(renewal, 3, control.renewed_sha256, 'httpupgrade', 422);
  expect(invalid.issue).toMatchObject({ field: 'tls_credentials.private_key_pem', code: 'tls_private_key_mismatch' });
  await connectionGroup(renewal, 'security');
  await renewal.getByLabel('TLS private key (PEM)', { exact: true }).fill(control.renewed.private_key_pem);
  stage = 'explicit certificate and key replacement';
  const renewalPreview = await preview(renewal, 3, control.renewed_sha256, 'httpupgrade');
  const last = await save(renewal, 3, control.renewed_sha256, 'httpupgrade', renewalPreview);
  expect(last.plan.profile.tls.public_key_sha256).not.toBe(firstReceipt.plan.profile.tls.public_key_sha256);
  await reopen(3, control.renewed_sha256, 'httpupgrade');
  await noBrowserStorageSecrets();
  if (diagnostics.length) throw new Error(diagnostics.join('; '));
  await writeFile(control.result_path, JSON.stringify({ inbound_id: inboundID, event_id: last.event_id,
    original_sha256: firstReceipt.plan.profile.tls.sha256, renewed_sha256: last.plan.profile.tls.sha256 }), { mode: 0o600 });
  console.log('NATIVE_UI_TLS_CERTIFICATE_LIFECYCLE_VERIFIED');
} catch (error) {
  // Do not dump assertion objects or page HTML: the test has write-only PEM fields.
  console.error('TLS QA failed at ' + stage + ': ' + String(error?.message || error).replace(/-----BEGIN[\s\S]*?-----END[^-]*-----/g, '[fixture PEM]'));
  process.exitCode = 1;
} finally {
  await browser?.close();
  await new Promise(resolve => proxy.close(resolve));
}
