// Compiled panel -> real cookie/CSRF API -> disposable SQLite acceptance.
import { chromium, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';

const base = new URL(process.env.DARK_PRO_QA_UI_URL ?? '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || !base.port) throw new Error('An isolated loopback test origin is required');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
for (const field of ['username', 'password', 'source_username', 'source_password', 'target_username', 'target_password']) {
  if (typeof control[field] !== 'string' || !control[field]) throw new Error('Invalid private test identity');
}
const browser = await chromium.launch({ headless: true });
const pageErrors = [];
const ownership = '/api/v1/workspace/clients/ownership';
const mainID = 'api-metered-client';
const ids = [mainID, 'ownership-batch-second', 'ownership-group-first', 'ownership-group-archived'];
const targetID = 'ownership-browser-target';
const consent = 'I reviewed the exact clients and destination. Move their management access.';
let stage = 'launch';

async function signedIn(username, password) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 1000 } });
  // Egress guard only: every same-origin response comes from the real Go HUB.
  await context.route('**/*', route => new URL(route.request().url()).origin === base.origin ? route.continue() : route.abort());
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  page.on('pageerror', error => pageErrors.push(error.message));
  await page.goto(new URL('/clients', base).href);
  await page.getByLabel('Username', { exact: true }).fill(username);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Client directory', exact: true })).toBeVisible();
  return page;
}

async function call(page, path, method = 'GET', body) {
  return page.evaluate(async ({ path, method, body }) => {
    const session = await fetch('/api/v1/session/status', { credentials: 'same-origin' }).then(r => r.json());
    const response = await fetch(path, {
      method, credentials: 'same-origin',
      headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-CSRF-Token': session.csrf_token },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    return { status: response.status, body: await response.json() };
  }, { path, method, body });
}

async function ok(page, path, method = 'GET', body) {
  const result = await call(page, path, method, body);
  expect(result.status, `${method} ${path}`).toBe(200);
  return result.body;
}

async function confirmTransfer(owner, panel) {
  await panel.getByRole('button', { name: 'Review assignment', exact: true }).click();
  const confirm = panel.getByRole('button', { name: 'Confirm assignment', exact: true });
  await expect(confirm).toBeDisabled();
  await expect(panel.locator('.ownership-members article')).toHaveCount(2);
  await panel.getByRole('checkbox', { name: consent, exact: true }).check();
  const posted = owner.waitForRequest(r => new URL(r.url()).pathname === ownership + '/commit' && r.method() === 'POST');
  await confirm.click();
  const request = (await posted).postDataJSON();
  await expect(panel.getByText('Management assignment recorded', { exact: true })).toBeVisible();
  const receipt = await ok(owner, ownership + '/receipts/' + request.event_id);
  expect(receipt).toMatchObject({ count: 2, representative_id: targetID, balance_changed: false, usage_reset: false, credentials_changed: false });
  return request;
}

try {
  stage = 'existing source and destination sessions';
  const source = await signedIn(control.source_username, control.source_password);
  const target = await signedIn(control.target_username, control.target_password);
  const owner = await signedIn(control.username, control.password);
  const before = new Map();
  for (const id of ids) before.set(id, await ok(source, '/api/v1/workspace/clients/' + id));
  const sourceBalance = (await ok(source, '/api/v1/workspace/me')).balance;
  const targetBalance = (await ok(target, '/api/v1/workspace/me')).balance;
  const usageBefore = (await ok(source, '/api/v1/workspace/clients/' + mainID + '/usage')).usage;
  expect(usageBefore).toMatchObject({ uplink_bytes: '20', downlink_bytes: '25', total_bytes: '45', known: true });
  expect((await call(target, '/api/v1/workspace/clients/' + mainID)).status).toBe(403);
  await expect(source.getByRole('option', { name: 'Assign to representative', exact: true })).toHaveCount(0);

  stage = 'owner selected batch through compiled controls';
  const bulk = owner.locator('.clients-bulk');
  await bulk.getByRole('combobox', { name: 'Action', exact: true }).selectOption('transfer');
  const panel = owner.getByRole('region', { name: 'Assign clients to a representative', exact: true });
  for (const id of ids.slice(0, 2)) await owner.getByRole('checkbox', { name: 'Select client ' + id, exact: true }).check();
  await bulk.getByLabel('Required audit reason', { exact: true }).fill('Transfer real selected batch');
  await panel.getByRole('button', { name: /Ownership destination/ }).click();
  await panel.getByLabel('Destination group', { exact: true }).selectOption('ownership-target-group');
  const batchRequest = await confirmTransfer(owner, panel);
  expect(batchRequest.client_ids.slice().sort()).toEqual(ids.slice(0, 2).sort());

  stage = 'owner full remaining group including archived member';
  await bulk.getByLabel('Required audit reason', { exact: true }).fill('Transfer complete remaining group');
  await panel.getByLabel('Selection', { exact: true }).selectOption('group');
  await panel.getByLabel('Source group', { exact: true }).selectOption('ownership-source-group');
  const groupRequest = await confirmTransfer(owner, panel);
  expect(groupRequest.client_ids).toEqual([]);
  expect(groupRequest.source_group_id).toBe('ownership-source-group');

  stage = 'identity, balances and usage preserved by assignment';
  for (const id of ids) {
    const current = await ok(target, '/api/v1/workspace/clients/' + id);
    expect(current).toEqual({ ...before.get(id), representative_id: targetID });
  }
  expect((await ok(source, '/api/v1/workspace/me')).balance).toEqual(sourceBalance);
  expect((await ok(target, '/api/v1/workspace/me')).balance).toEqual(targetBalance);
  expect((await ok(target, '/api/v1/workspace/clients/' + mainID + '/usage')).usage).toEqual({ ...usageBefore, representative_id: targetID });

  stage = 'old authenticated manager cannot read or mutate';
  for (const suffix of ['', '/usage', '/edits', '/subscription', '/inbound-subscription']) {
    expect((await call(source, '/api/v1/workspace/clients/' + mainID + suffix)).status).toBe(403);
  }
  const denied = [
    ['/extend', { event_id: 'ownership-browser-denied-renew-0001', client_id: mainID, additional_days: 1, additional_volume_bytes: 1, reason: 'Former manager denied' }],
    ['/state', { event_id: 'ownership-browser-denied-state-0001', client_id: mainID, action: 'disable', reason: 'Former manager denied' }],
    ['/edit/quote', { client_id: mainID, name: 'Unauthorized name', plan_kind: 'volume', ip_count: 1, additional_days: 0, additional_volume_bytes: 0, reason: 'Former manager denied' }],
  ];
  for (const [suffix, body] of denied) expect((await call(source, '/api/v1/workspace/clients/' + mainID + suffix, 'POST', body)).status).toBe(403);
  await source.reload();
  await expect(source.locator('.clients-explorer-list').getByText(before.get(mainID).name, { exact: true })).toHaveCount(0);

  stage = 'new representative renews through real compiled UI';
  await target.reload();
  const lifecycle = target.locator('.business-panel').filter({ has: target.getByRole('heading', { name: 'Renew and manage clients', exact: true }) });
  await lifecycle.getByRole('combobox').selectOption(mainID);
  const renewal = target.locator('form').filter({ has: target.getByRole('button', { name: 'Extend / add volume', exact: true }) });
  await renewal.getByLabel('Additional days', { exact: true }).fill('1');
  await renewal.getByLabel('Additional volume (GB)', { exact: true }).fill('0.0009765625');
  await renewal.getByLabel('Operation reason', { exact: true }).fill('Current representative funds one MiB');
  const renewed = target.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/workspace/clients/' + mainID + '/extend' && r.request().method() === 'POST');
  await renewal.getByRole('button', { name: 'Extend / add volume', exact: true }).click();
  expect((await renewed).status()).toBe(200);
  const current = await ok(target, '/api/v1/workspace/clients/' + mainID);
  expect(current).toMatchObject({ uuid: before.get(mainID).uuid, representative_id: targetID, volume_bytes: before.get(mainID).volume_bytes + 1048576, expires_unix: before.get(mainID).expires_unix + 86400 });
  expect((await ok(source, '/api/v1/workspace/me')).balance).toEqual(sourceBalance);
  expect((await ok(target, '/api/v1/workspace/me')).balance.volume_credit_bytes).toBe(targetBalance.volume_credit_bytes - 1048576);

  stage = 'identical ownership retries preserve later renewal';
  for (const request of [batchRequest, groupRequest]) {
    const receipt = await ok(owner, ownership + '/commit', 'POST', request);
    expect(receipt.replayed).toBe(true);
    expect(receipt.count).toBe(2);
  }
  expect(await ok(target, '/api/v1/workspace/clients/' + mainID)).toEqual(current);
  const usageAfter = (await ok(target, '/api/v1/workspace/clients/' + mainID + '/usage')).usage;
  expect(usageAfter).toEqual({
    ...usageBefore,
    representative_id: targetID,
    limit_bytes: String(BigInt(usageBefore.limit_bytes) + 1048576n),
    remaining_native_budget_bytes: String(BigInt(usageBefore.remaining_native_budget_bytes) + 1048576n),
  });
  if (pageErrors.length) throw new Error('Browser page errors: ' + pageErrors.join('; '));
  console.log('REAL_CLIENT_OWNERSHIP_BROWSER_PASS');
} catch (error) {
  console.error('Ownership acceptance stage: ' + stage);
  throw error;
} finally {
  await browser.close();
}
