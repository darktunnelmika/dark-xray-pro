// Real compiled UI → authenticated API → SQLite promotion acceptance.
// The Go harness owns the disposable saved plan and signed empty Agent observations.
// This browser performs no Stage, Apply, renewal, runtime start or subscription operation.
import { chromium, expect } from '@playwright/test';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer, request as httpRequest } from 'node:http';
import { isAbsolute } from 'node:path';

const base = new URL(process.env.DARK_PRO_QA_UI_URL || '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port || base.username || base.password) throw new Error('QA loopback only');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
if (typeof control.username !== 'string' || !control.username || typeof control.password !== 'string' || !control.password || !isAbsolute(control.result_path || '') || !Number.isSafeInteger(control.inbound_id) || control.inbound_id <= 0) throw new Error('Invalid promotion QA control');
const inboundID = control.inbound_id, planPath = '/api/v1/workspace/native-inbound-plans/' + inboundID, promotionPath = planPath + '/promotion';
const diagnostics = [], mutations = [];
let stage = 'launch', browser, page;
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
  request.on('aborted', () => upstream.destroy()); request.pipe(upstream);
});
proxy.on('connect', (_request, socket) => socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n'));
await new Promise((resolve, reject) => { proxy.once('error', reject); proxy.listen(0, '127.0.0.1', resolve); });
function publicOnly(value) {
  if (typeof value === 'string') { if (/-----BEGIN\s+[^-]+-----/i.test(value)) throw new Error('PEM material in public response'); return; }
  if (!value || typeof value !== 'object') return;
  for (const [key, nested] of Object.entries(value)) {
    if (['tlscredentials', 'certificatepem', 'privatekeypem', 'privatekey', 'realityprivatekey', 'config', 'uuid'].includes(key.replace(/[_-]/g, '').toLowerCase())) throw new Error('Private configuration in public promotion response');
    publicOnly(nested);
  }
}
try {
  browser = await chromium.launch({ headless: true, proxy: { server: 'http://127.0.0.1:' + proxy.address().port, bypass: '127.0.0.1' } });
  page = await browser.newPage({ viewport: { width: 1280, height: 900 }, locale: 'en-US' });
  await page.addInitScript(() => { localStorage.setItem('dark-pro-lang', 'en'); }); page.setDefaultTimeout(15000);
  page.on('pageerror', () => diagnostics.push('Browser page error'));
  page.on('request', request => {
    const url = new URL(request.url()); if (url.origin !== base.origin) diagnostics.push('Unexpected external request');
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
      mutations.push({ method: request.method(), path: url.pathname });
      if (request.method() !== 'POST' || !['/api/v1/session/login', promotionPath, promotionPath + '/preview'].includes(url.pathname)) diagnostics.push('Unexpected mutation ' + request.method() + ' ' + url.pathname);
    }
  });
  const observe = (method, path) => page.waitForResponse(response => new URL(response.url()).origin === base.origin && new URL(response.url()).pathname === path && response.request().method() === method);
  const panel = () => page.getByRole('region', { name: 'Inbound LAB preparation', exact: true });
  stage = 'owner sign-in';
  await page.goto(new URL('/inbounds?inbound=' + inboundID + '&tab=overview', base).href);
  await page.getByLabel('Username', { exact: true }).fill(control.username); await page.getByLabel('Password', { exact: true }).fill(control.password);
  const discover = observe('GET', promotionPath); await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const discovery = await discover; expect(discovery.status()).toBe(200); const available = await discovery.json(); publicOnly(available);
  expect(available).toMatchObject({ available: true, scope: 'single_direct_tcp_reality_loopback', promoted: false, blockers: [] });
  stage = 'existing client intent';
  await panel().getByRole('button', { name: 'Prepare LAB execution', exact: true }).click();
  await panel().getByLabel('Select LAB client: promotion-client-0', { exact: true }).check();
  await panel().getByLabel('Select LAB client: promotion-client-1', { exact: true }).check();
  await panel().getByLabel('LAB preparation reason', { exact: true }).fill('Real browser saved-plan LAB preparation');
  expect(mutations.filter(mutation => mutation.path !== '/api/v1/session/login')).toHaveLength(0);
  stage = 'revision-bound preview';
  const previewResponse = observe('POST', promotionPath + '/preview'); await panel().getByRole('button', { name: 'Review LAB preparation', exact: true }).click();
  const checked = await previewResponse; expect(checked.status()).toBe(200);
  const preview = await checked.json(), intent = checked.request().postDataJSON(); publicOnly(preview); publicOnly(intent);
  expect(preview).toMatchObject({ eligible: true, scope: 'single_direct_tcp_reality_loopback', inbound_id: inboundID, plan_revision: 1, inbound_revision: 1, node_id: 'promotion-qa', client_ids: ['promotion-client-0', 'promotion-client-1'], blockers: [], runtime_changed: false, subscription_published: false });
  expect(preview.review_sha256).toMatch(/^[a-f0-9]{64}$/); expect(preview.installation_id).toMatch(/^[a-f0-9]{32}$/);
  expect(intent).toMatchObject({ expected_plan_revision: 1, expected_inbound_revision: 1, client_ids: ['promotion-client-0', 'promotion-client-1'], reason: 'Real browser saved-plan LAB preparation' });
  expect(intent).not.toHaveProperty('confirmed'); expect(intent).not.toHaveProperty('review_sha256');
  await expect(panel().locator('[data-promotion-review]')).toBeVisible();
  stage = 'confirmed atomic promotion';
  const commitResponse = observe('POST', promotionPath); page.once('dialog', dialog => dialog.accept()); await panel().getByRole('button', { name: 'Prepare reviewed plan', exact: true }).click();
  const committed = await commitResponse; expect(committed.status()).toBe(201); const receipt = await committed.json(); publicOnly(receipt);
  expect(committed.request().postDataJSON()).toEqual({ ...intent, review_sha256: preview.review_sha256, confirmed: true });
  expect(receipt).toMatchObject({ event_id: intent.event_id, inbound_id: inboundID, plan_revision: 1, source_inbound_revision: 1, inbound_revision: 2, node_id: preview.node_id, installation_id: preview.installation_id, draft_revision: 1, client_ids: intent.client_ids, review_sha256: preview.review_sha256, promoted: true, runtime_changed: false, subscription_published: false, replayed: false });
  await expect(panel().locator('[data-promotion-state="prepared"]')).toBeVisible();
  await expect(page.locator('#inbound-panel-overview .inbound-summary').getByText('Prepared for LAB', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Archive saved setup', exact: true })).toHaveCount(0); await expect(page.getByRole('button', { name: 'Enable Inbound', exact: true })).toHaveCount(0); await expect(page.getByRole('button', { name: 'Edit profile', exact: true })).toHaveCount(0);
  stage = 'read-only retained plan after reload';
  await page.reload({ waitUntil: 'domcontentloaded' }); await expect(panel().locator('[data-promotion-state="prepared"]')).toBeVisible();
  const readResponse = observe('GET', planPath); await page.getByRole('button', { name: 'Open saved setup', exact: true }).click();
  const read = await readResponse; expect(read.status()).toBe(200); const saved = await read.json(); publicOnly(saved);
  expect(saved.plan).toMatchObject({ plan_revision: 1, inbound_revision: 1, editable: false, edit_blocker: 'plan_promoted', runtime_apply: false, promotion: { ...receipt, replayed: false } });
  const dialog = page.getByRole('dialog', { name: 'Edit Inbound setup', exact: true }); await expect(dialog.getByLabel('Inbound name', { exact: true })).toBeDisabled();
  await expect(dialog.getByRole('tab')).toHaveText(['Connection', 'Dark Swap', 'Advanced', 'Review']); await dialog.getByRole('tab', { name: 'Review', exact: true }).click(); await expect(dialog.getByRole('button', { name: 'Save paused setup', exact: true })).toBeDisabled(); await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('tab', { name: 'Routes', exact: true }).click(); await expect(page.getByRole('button', { name: 'Add Node target', exact: true })).toHaveCount(0); await expect(page.getByRole('button', { name: 'Edit target', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Clients', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Prepared client bindings', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Attach selected', exact: true })).toHaveCount(0); await expect(page.getByRole('button', { name: 'Detach selected', exact: true })).toHaveCount(0);
  expect(mutations.filter(mutation => mutation.path === promotionPath + '/preview')).toHaveLength(1); expect(mutations.filter(mutation => mutation.path === promotionPath)).toHaveLength(1);
  const storage = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }));
  if (/-----BEGIN|private_key|tls_credentials/.test(storage)) throw new Error('Private material in browser storage');
  if (diagnostics.length) throw new Error(diagnostics.join('; '));
  await writeFile(control.result_path, JSON.stringify({ pass: true, marker: 'PLAN_PROMOTION_BROWSER_PASS', inbound_id: inboundID, event_id: receipt.event_id, plan_revision: receipt.plan_revision, source_inbound_revision: receipt.source_inbound_revision, inbound_revision: receipt.inbound_revision, node_id: receipt.node_id, installation_id: receipt.installation_id, draft_revision: receipt.draft_revision, client_ids: receipt.client_ids, review_sha256: receipt.review_sha256 }), { mode: 0o600 });
  console.log('PLAN_PROMOTION_BROWSER_PASS');
} catch (error) {
  console.error('Promotion QA failed at ' + stage + ': ' + String(error?.message || error).replace(/-----BEGIN[\s\S]*?-----END[^-]*-----/g, '[fixture PEM]'));
  process.exitCode = 1;
} finally {
  await browser?.close(); await new Promise(resolve => proxy.close(resolve));
}
