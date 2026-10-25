// Real compiled UI → authenticated API → SQLite TLS acceptance. No API mocks.
import { chromium, expect } from '@playwright/test';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer, request as httpRequest } from 'node:http';
import { isAbsolute } from 'node:path';
import { assertCapabilities, assertNewSelectors, assertSpecOnly, field, input, publicOnly, reveal } from './live-inbound-authoring.mjs';

const base = new URL(process.env.DARK_PRO_QA_UI_URL || '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port || base.username || base.password) throw new Error('QA loopback only');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
if (!control.username || !control.password || !isAbsolute(control.result_path || '') || !control.first?.private_key_pem || !control.renewed?.private_key_pem) throw new Error('Invalid TLS QA control');
const api = '/api/v1/workspace/native-inbound-plans';
const inspectAPI = '/api/v1/workspace/inbound-security/certificates/inspect';
const diagnostics = [];
let stage = 'launch', inboundID = 0, certificateID = '', browser, page;

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

function safePlan(plan, revision, sha, transport) {
  publicOnly(plan);
  assertSpecOnly(plan.profile);
  expect(plan).toMatchObject({ plan_revision: revision, inbound_revision: revision, runtime_apply: false, editable: true, status: 'SAVED_DISABLED',
    profile: { security: 'tls', transport, revision, public_key: '', enabled: false, archived: false, bound_clients: 0,
      server_names: ['edge.example.org'], spec: { security: 'tls', network: transport,
        tls: { serverName: 'edge.example.org', minVersion: '1.2', maxVersion: '1.3', alpn: ['http/1.1'], certificates: [{ usage: 'encipherment', id: expect.stringMatching(/^[0-9a-f]{32}$/) }] },
        client: { pinnedPeerCertSha256: [sha] } } } });
  expect(plan.profile.reality_target || '').toBe('');
  expect(plan.profile.short_ids || []).toEqual([]);
  expect(plan.profile.spec.reality ?? {}).toEqual({});
  expect(plan.profile.spec_secret_paths).toEqual(expect.arrayContaining(['tls.certificates.0.certificate', 'tls.certificates.0.key']));
  expect(plan.profile.spec.tls.certificates[0].certificate).toBeUndefined();
  expect(plan.profile.spec.tls.certificates[0].key).toBeUndefined();
  if (certificateID) expect(plan.profile.spec.tls.certificates[0].id).toBe(certificateID);
  expect(plan.profile.spec.stream[transport + 'Settings']).toMatchObject({ path: transport === 'ws' ? '/socket' : '/upgrade', host: 'edge.example.org' });
  expect(plan.profile.spec.stream[transport === 'ws' ? 'httpupgradeSettings' : 'wsSettings']).toBeUndefined();
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
      const allowed = request.method() === 'POST' && ['/api/v1/session/login', api, api + '/preview', inspectAPI].includes(url.pathname) ||
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
    const certificates = dialog.locator('[data-spec-rows="tls.certificates"]');
    if (await certificates.locator('[data-spec-row]').count() === 0) await certificates.getByRole('button', { name: 'Add', exact: true }).click();
    await (await input(dialog, 'tls.certificates.0.certificate')).fill(value.certificate_pem);
    await field(dialog, 'tls.certificates.0.key').fill(value.private_key_pem);
  }
  async function inspectCertificate(dialog, sha) {
    const button = dialog.getByRole('button', { name: 'Inspect entered certificate', exact: true, includeHidden: true });
    await reveal(button); await expect(button).toBeEnabled();
    const pending = observe('POST', inspectAPI); await button.click();
    const response = await pending; expect(response.status()).toBe(200);
    const inspected = await response.json(); publicOnly(inspected);
    expect(inspected.certificates).toHaveLength(1);
    const cert = inspected.certificates[0];
    expect(cert.sha256_hex).toBe(sha);
    expect(cert.spki_sha256_hex).toMatch(/^[0-9a-f]{64}$/);
    expect(cert.dns_names).toEqual(['edge.example.org', 'tunnel.example.org']);
    expect(Date.parse(cert.not_before)).toBeLessThan(Date.now());
    expect(Date.parse(cert.not_after)).toBeGreaterThan(Date.now());
    expect(inspected).toMatchObject({ hostname_valid: true, time_valid: true, server_name: 'edge.example.org' });
    await expect(dialog.locator('.spec-tool-details')).toContainText(cert.sha256_hex);
    await dialog.getByRole('button', { name: 'Use certificate pin for clients', exact: true }).click();
    await expect(field(dialog, 'client.pinnedPeerCertSha256')).toHaveValue(sha);
    return cert;
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
    // A new row receives its permanent identity on save; later edits preserve it.
    const publicSpec = value => ({ ...value, tls: { ...value.tls, certificates: value.tls.certificates.map(({ id, ...cert }) => cert) } });
    expect(publicSpec(receipt.plan.profile.spec)).toEqual(publicSpec(checked.value.plan.profile.spec));
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
    await expect(field(dialog, 'security')).toHaveValue('tls');
    await expect(field(dialog, 'tls.certificates.0.certificate')).toHaveValue('');
    await expect(field(dialog, 'tls.certificates.0.key')).toHaveValue('');
    await expect(field(dialog, 'tls.certificates.0.key')).toHaveAttribute('placeholder', 'Blank keeps saved material');
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
  assertCapabilities(capabilities);
  await page.getByRole('button', { name: 'Add Inbound', exact: true }).click();
  const created = page.getByRole('dialog', { name: 'Create Inbound', exact: true });
  await expect(created.getByRole('tab')).toHaveText(['Connection', 'Dark Swap', 'Advanced', 'Review']);
  await assertNewSelectors(created);
  stage = 'TLS WebSocket creation';
  await created.getByLabel('Inbound name', { exact: true }).fill('Real browser TLS');
  await connectionGroup(created, 'security');
  await field(created, 'security').selectOption('tls');
  await field(created, 'tls.serverName').fill('edge.example.org');
  await field(created, 'tls.alpn').fill('http/1.1');
  await credentials(created, control.first);
  const firstCertificate = await inspectCertificate(created, control.first_sha256);
  await connectionGroup(created, 'network');
  await field(created, 'network').selectOption('ws');
  await field(created, 'stream.wsSettings.path').fill('/socket');
  await field(created, 'stream.wsSettings.host').fill('edge.example.org');
  await expect(field(created, 'reality.target')).toHaveCount(0);
  await expect(field(created, 'reality.shortIds')).toHaveCount(0);
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
  certificateID = firstReceipt.plan.profile.spec.tls.certificates[0].id;
  await noBrowserStorageSecrets();

  stage = 'retained TLS key across transport edit';
  const edited = await reopen(1, control.first_sha256, 'ws');
  await connectionGroup(edited, 'network');
  await field(edited, 'network').selectOption('httpupgrade');
  await field(edited, 'stream.httpupgradeSettings.path').fill('/upgrade');
  await field(edited, 'stream.httpupgradeSettings.host').fill('edge.example.org');
  await edited.getByRole('tab', { name: 'Review', exact: true }).click();
  await edited.getByLabel('Change note', { exact: true }).fill('Retained certificate HTTPUpgrade');
  const retainedPreview = await preview(edited, 2, control.first_sha256, 'httpupgrade');
  expect(Object.hasOwn(retainedPreview.body, 'tls_credentials')).toBe(false);
  expect(retainedPreview.body.profile.spec.tls.certificates[0]).toEqual({ id: certificateID, usage: 'encipherment' });
  expect(retainedPreview.body.profile.options).toBeUndefined();
  await save(edited, 2, control.first_sha256, 'httpupgrade', retainedPreview);

  stage = 'certificate/key mismatch is rejected by actual API';
  const renewal = await reopen(2, control.first_sha256, 'httpupgrade');
  await credentials(renewal, { certificate_pem: control.renewed.certificate_pem, private_key_pem: control.first.private_key_pem });
  const renewedCertificate = await inspectCertificate(renewal, control.renewed_sha256);
  await renewal.getByRole('tab', { name: 'Review', exact: true }).click();
  await renewal.getByLabel('Change note', { exact: true }).fill('Renewed TLS certificate');
  const invalid = await preview(renewal, 3, control.renewed_sha256, 'httpupgrade', 422);
  expect(invalid.issue).toMatchObject({ field: 'profile.spec.tls.certificates.0.key', code: 'certificate_key_mismatch' });
  await connectionGroup(renewal, 'security');
  await (await input(renewal, 'tls.certificates.0.key')).fill(control.renewed.private_key_pem);
  stage = 'explicit certificate and key replacement';
  const renewalPreview = await preview(renewal, 3, control.renewed_sha256, 'httpupgrade');
  const last = await save(renewal, 3, control.renewed_sha256, 'httpupgrade', renewalPreview);
  expect(renewedCertificate.spki_sha256_hex).not.toBe(firstCertificate.spki_sha256_hex);
  expect(last.plan.profile.spec.client.pinnedPeerCertSha256).toEqual([renewedCertificate.sha256_hex]);
  await reopen(3, control.renewed_sha256, 'httpupgrade');
  await noBrowserStorageSecrets();
  if (diagnostics.length) throw new Error(diagnostics.join('; '));
  await writeFile(control.result_path, JSON.stringify({ inbound_id: inboundID, event_id: last.event_id,
    original_sha256: firstCertificate.sha256_hex, renewed_sha256: renewedCertificate.sha256_hex }), { mode: 0o600 });
  console.log('NATIVE_UI_TLS_CERTIFICATE_LIFECYCLE_VERIFIED');
} catch (error) {
  // Do not dump assertion objects or page HTML: the test has write-only PEM fields.
  console.error('TLS QA failed at ' + stage + ': ' + String(error?.message || error).replace(/-----BEGIN[\s\S]*?-----END[^-]*-----/g, '[fixture PEM]'));
  process.exitCode = 1;
} finally {
  await browser?.close();
  await new Promise(resolve => proxy.close(resolve));
}
