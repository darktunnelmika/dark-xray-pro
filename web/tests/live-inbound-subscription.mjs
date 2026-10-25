// Opt-in acceptance: compiled UI -> real session/API/SQLite -> owned runtime.
// No request mocks, response substitutions or direct API mutations.
import { chromium, expect } from '@playwright/test';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer, request as httpRequest } from 'node:http';
import { isAbsolute } from 'node:path';

const base = new URL(process.env.DARK_PRO_QA_UI_URL || '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port || base.username || base.password) throw Error('QA loopback only');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
if (typeof control.username !== 'string' || !control.username || typeof control.password !== 'string' || !control.password ||
  typeof control.result_path !== 'string' || !isAbsolute(control.result_path) || typeof control.client_id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/.test(control.client_id) ||
  !Number.isSafeInteger(control.inbound_id) || control.inbound_id < 1) throw Error('Invalid QA control file');
const subscription = '/api/v1/workspace/clients/' + encodeURIComponent(control.client_id) + '/inbound-subscription';
const diagnostics = [], mutations = [];
let stage = 'launch', browser, page, token = '', csrf = '';
const denyProxy = createServer((request, response) => {
  let target;
  try { target = new URL(request.url); } catch { response.writeHead(403).end(); return; }
  if (target.origin !== base.origin || target.username || target.password) { response.writeHead(403).end(); return; }
  const headers = { ...request.headers, host: base.host };
  delete headers['proxy-connection']; delete headers['proxy-authorization'];
  const upstream = httpRequest({ hostname: '127.0.0.1', port: base.port, method: request.method, path: target.pathname + target.search, headers }, incoming => {
    response.writeHead(incoming.statusCode, incoming.headers); incoming.pipe(response);
  });
  upstream.on('error', () => { diagnostics.push('Harness transport failed'); response.writeHead(502).end(); });
  request.on('aborted', () => upstream.destroy()); request.pipe(upstream);
});
denyProxy.on('connect', (_request, socket) => socket.end('HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n'));
await new Promise((resolve, reject) => { denyProxy.once('error', reject); denyProxy.listen(0, '127.0.0.1', resolve); });

try {
  browser = await chromium.launch({ headless: true, proxy: { server: 'http://127.0.0.1:' + denyProxy.address().port, bypass: '127.0.0.1' } });
  page = await browser.newPage({ viewport: { width: 1280, height: 900 }, locale: 'en-US', acceptDownloads: true });
  page.setDefaultTimeout(20000);
  await page.addInitScript(() => {
    localStorage.setItem('dark-pro-lang', 'en');
    const writes = []; Object.defineProperty(window, '__managedSubscriptionWrites', { value: writes });
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) { writes.push([String(key), String(value)]); return original.call(this, key, value); };
  });
  page.on('pageerror', () => diagnostics.push('Browser page error'));
  page.on('dialog', dialog => dialog.accept());
  page.on('request', request => {
    const url = new URL(request.url());
    if (url.origin !== base.origin) diagnostics.push('Request outside harness origin');
    if (url.pathname.startsWith('/api/v1/inbound-subscriptions/')) diagnostics.push('Browser implicitly fetched bearer URL');
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) {
      mutations.push({ method: request.method(), path: url.pathname });
      if (request.method() !== 'POST' || !['/api/v1/session/login', subscription + '/issue', subscription + '/reveal', subscription + '/revoke'].includes(url.pathname)) diagnostics.push('Unexpected mutation');
    }
  });
  const observe = (method, path) => page.waitForResponse(response => new URL(response.url()).origin === base.origin && new URL(response.url()).pathname === path && response.request().method() === method);
  stage = 'login'; await page.goto(base.origin + '/clients');
  await page.getByLabel('Username', { exact: true }).fill(control.username);
  await page.getByLabel('Password', { exact: true }).fill(control.password);
  const login = observe('POST', '/api/v1/session/login');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  const loginResponse = await login; expect(loginResponse.status()).toBe(200);
  const loginState = await loginResponse.json(); csrf = loginState.csrf_token ?? '';
  if (new URL(page.url()).pathname !== '/clients') await page.goto(base.origin + '/clients');
  stage = 'client routes';
  const directory = page.getByRole('region', { name: 'Client directory' });
  await directory.locator('.clients-explorer-row').filter({ hasText: control.client_id }).getByRole('button', { name: 'Manage', exact: true }).click();
  const section = page.getByRole('region', { name: 'Client inbound subscription' });
  await expect(section.getByText(/No link issued/)).toBeVisible();
  await expect(section.getByText('Configuration for the running routes is available.', { exact: true })).toBeVisible();
  stage = 'issue'; const issuing = observe('POST', subscription + '/issue');
  await section.getByRole('button', { name: 'Create subscription link', exact: true }).click();
  const issueResponse = await issuing; expect(issueResponse.status()).toBe(200);
  const issued = await issueResponse.json(); expect(issued.client_id).toBe(control.client_id); expect(issued.state).toBe('active'); expect(issued.generation).toBe(1);
  await expect(section.getByText(/Link active/)).toBeVisible();
  await expect(section.getByLabel('Inbound subscription URL', { exact: true })).toHaveCount(0);
  stage = 'reveal'; const revealing = observe('POST', subscription + '/reveal');
  await section.getByRole('button', { name: 'Show link', exact: true }).click();
  const revealResponse = await revealing; expect(revealResponse.status()).toBe(200);
  const reveal = await revealResponse.json(); token = reveal.token;
  if (typeof token !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(token)) throw Error('Invalid revealed token shape');
  const url = new URL(await section.getByLabel('Inbound subscription URL', { exact: true }).inputValue());
  if (url.origin !== base.origin || url.pathname !== '/api/v1/inbound-subscriptions/' + token || url.search !== '?format=uri') throw Error('Invalid revealed URL scope');
  stage = 'download';
  const downloading = page.waitForEvent('download');
  await section.getByRole('link', { name: 'Download configuration', exact: true }).click();
  const download = await downloading; if (await download.failure()) throw Error('Configuration download failed');
  const data = await readFile(await download.path());
  // The fixture owns an actual HTTP listener. Export must contain its usable
  // authenticated URI, not a status document or an empty placeholder.
  const rows = data.toString('utf8').trim().split(/\r?\n/).filter(Boolean);
  if (rows.length < 1 || !rows.some(row => { try { const route = new URL(row); return route.protocol === 'http:' && route.username && route.password && route.hostname && route.port; } catch { return false; } })) throw Error('HTTP client URI not delivered');
  const consumer = await browser.newContext();
  const before = await consumer.request.get(url.href); expect(before.status()).toBe(200);
  expect((await before.body()).equals(data)).toBe(true);
  stage = 'revoke'; const revoking = observe('POST', subscription + '/revoke');
  await section.getByRole('button', { name: 'Revoke link', exact: true }).click();
  const revokeResponse = await revoking; expect(revokeResponse.status()).toBe(200);
  const revoked = await revokeResponse.json(); expect(revoked.client_id).toBe(control.client_id); expect(revoked.generation).toBe(2); expect(revoked.state).toBe('revoked');
  await expect(section.getByLabel('Inbound subscription URL', { exact: true })).toHaveCount(0);
  const after = await consumer.request.get(url.href); expect(after.status()).toBe(404);
  const storage = await page.evaluate(() => JSON.stringify({ local: Object.entries(localStorage), session: Object.entries(sessionStorage), writes: window.__managedSubscriptionWrites }));
  if (storage.includes(token) || storage.includes('/api/v1/inbound-subscriptions/') || csrf && storage.includes(csrf)) throw Error('Credential entered browser persistence');
  expect(mutations).toEqual(['/api/v1/session/login', subscription + '/issue', subscription + '/reveal', subscription + '/revoke'].map(path => ({ method: 'POST', path })));
  if (diagnostics.length) throw Error('Unexpected browser behavior');
  await writeFile(control.result_path, JSON.stringify({ verified: true, client_id: control.client_id, inbound_id: control.inbound_id,
    issued_generation: issued.generation, revoked_generation: revoked.generation, download_bytes: data.length,
    bearer_before_status: before.status(), bearer_after_status: after.status(), token_stored: false, unexpected_requests: diagnostics.length }), { mode: 0o600 });
  await consumer.close(); console.log('MANAGED_UI_SUBSCRIPTION_VERIFIED');
} catch (error) {
  let message = String(error?.message ?? 'unknown failure');
  for (const privateValue of [token, csrf, control.password]) if (privateValue) message = message.split(privateValue).join('[REDACTED]');
  message = message.replace(/\/api\/v1\/inbound-subscriptions\/[A-Za-z0-9_-]+/g, '/api/v1/inbound-subscriptions/[REDACTED]');
  throw Error('Managed UI acceptance failed during ' + stage + ': ' + message);
} finally {
  if (browser) await browser.close();
  await new Promise(resolve => denyProxy.close(resolve));
}
